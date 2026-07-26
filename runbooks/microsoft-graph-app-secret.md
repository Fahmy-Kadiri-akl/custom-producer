# Runbook: Microsoft Graph Application Client Secret Rotation

> **Rotator:** `microsoft_graph_app_secret` in this repo. See the [main README](../README.md#microsoft_graph_app_secret) for where this runbook fits.
> **Scope:** Rotating the client secret of a Microsoft Entra ID (Azure AD) application registration through the Microsoft Graph `addPassword` / `removePassword` API, with the rotator authenticated as a dedicated app registration using a certificate.
> **Status:** Verified end to end against the custom-producer rotator in an Akeyless lab Entra tenant.
> **Estimated time:** ~10 minutes for the one-time Azure setup; seconds per rotation.
> **Prerequisites:** Global Administrator (or Application Administrator + Cloud Application Administrator) on the Entra tenant to grant admin consent and assign app-role ownership; `az` CLI signed in to the tenant. No interactive sign-in by a delegated user at any point.

---

## What this runbook solves

An automation system needs an Entra app registration's client secret to call Microsoft Graph or another Azure resource. Left static, that secret expires on a 1-2 year cadence and renewal becomes a manual ticket. This rotator rotates it on a schedule and stores the live value in Akeyless, so consumers read the current secret on demand and no long-lived secret sits in consumer config.

The rotator does **not** authenticate as the target application. It authenticates as a dedicated **rotator app registration** whose certificate and identity live in the rotator environment, and it calls Graph's application `addPassword` / `removePassword` on the target app. This keeps the target apps free of any directory-management permission: only the rotator app holds it, and only over the apps it owns.

---

## Auth model and the one design decision

To call `addPassword` you need a Graph token carrying `Application.ReadWrite.OwnedBy` (least privileged) or `Application.ReadWrite.All`. That token is normally minted with the very client secret you are rotating, so the bootstrap identity must be separate. This rotator uses a **dedicated rotator app, certificate-authenticated**:

- The rotator app `R` holds `Application.ReadWrite.OwnedBy` and is an **owner** of each target app. `OwnedBy` lets `R` manage only the apps it owns, not the whole tenant.
- `R` authenticates with a certificate (client_credentials + RFC 7519 client_assertion signed by the cert). Because the cert is `R`'s own credential, it is the one thing with its own rotation story; the weekly target-secret rotation is fully non-interactive.
- Target apps `A` and `B` need no extra permission. They are just ordinary app registrations whose secret is rotated.

The per-secret payload carries only the target app's `tenant_id`, `client_id`, the rotated secret value, and the previous `key_id`. It never carries the rotator's cert.

> **Two notes that bite.** Graph's `{id}` in `applications/{id}/addPassword` is the application **object ID**, not the appId. This rotator uses the `applications(appId='{appId}')` function form so the payload needs only the client id. And both `addPassword` and `removePassword` are **POST**; `removePassword` is not DELETE.

---

## Azure setup (runs once)

All commands run with `az` signed in to the target tenant.

### 1. Create the rotator app and its service principal

```bash
R_APPID=$(az ad app create --display-name akeyless-msgraph-rotator --query appId -o tsv)
R_OID=$(az ad app show --id "$R_APPID" --query id -o tsv)
R_SP_OID=$(az ad sp create --id "$R_APPID" --query id -o tsv)
```

### 2. Generate a certificate and upload it to R

```bash
openssl req -x509 -newkey rsa:2048 -nodes -keyout R.key -out R.crt -days 365 \
  -subj "/CN=akeyless-msgraph-rotator"
openssl x509 -in R.crt -outform der -out R.cer
KEYB64=$(base64 -w0 R.cer)        # macOS: base64 -i R.cer | tr -d '\n'
KEYID=$(uuidgen)
BODY=$(printf '{"keyCredentials":[{"keyId":"%s","type":"AsymmetricX509Cert","usage":"Verify","key":"%s","displayName":"akeyless-rotator-cert"}]}' "$KEYID" "$KEYB64")
az rest --method PATCH --url "https://graph.microsoft.com/v1.0/applications/$R_OID" --body "$BODY"
```

Keep `R.crt` and `R.key`; the rotator needs them. The certificate is `R`'s own credential, so give it a long life and rotate it on a slow cadence.

### 3. Grant R `Application.ReadWrite.OwnedBy` and admin-consent

```bash
GRAPH_API=00000003-0000-0000-c000-000000000000
ROLEID=$(az ad sp show --id $GRAPH_API \
  --query "appRoles[?value=='Application.ReadWrite.OwnedBy'].id" -o tsv)
az ad app permission add --id "$R_APPID" --api $GRAPH_API --api-permissions "$ROLEID=Role"
```

`az ad app permission admin-consent` does not always create the service-principal app-role assignment. Verify, and if empty, create it directly:

```bash
GRAPH_SP_OID=$(az ad sp show --id $GRAPH_API --query id -o tsv)
az rest --method GET -o table \
  --url "https://graph.microsoft.com/v1.0/servicePrincipals/$R_SP_OID/appRoleAssignments"
# if the OwnedBy role is missing:
BODY=$(printf '{"principalId":"%s","resourceId":"%s","appRoleId":"%s"}' "$R_SP_OID" "$GRAPH_SP_OID" "$ROLEID")
az rest --method POST \
  --url "https://graph.microsoft.com/v1.0/servicePrincipals/$R_SP_OID/appRoleAssignments" --body "$BODY"
```

### 4. Make R an owner of each target app

```bash
az ad app owner add --id "<target-app-A-appId>" --owner-object-id "$R_SP_OID"
az ad app owner add --id "<target-app-B-appId>" --owner-object-id "$R_SP_OID"
```

`OwnedBy` only lets `R` manage apps it owns, so this ownership is what authorizes the rotation.

---

## Rotator environment

The rotator reads `R`'s identity and certificate from the environment, never from the payload. Each value may be inline PEM or a path to a PEM file (a mounted k8s secret):

| Variable | Meaning |
|----------|---------|
| `MSGRAPH_ROTATOR_TENANT_ID` | Entra tenant of the rotator app (and the target apps, single tenant) |
| `MSGRAPH_ROTATOR_CLIENT_ID` | Rotator app `R` appId |
| `MSGRAPH_ROTATOR_CERT` | Rotator cert PEM, or a path to it |
| `MSGRAPH_ROTATOR_KEY` | Rotator private key PEM, or a path to it |

This mirrors the OpenObserve target's env-sourced admin credentials. In the cluster, mount the cert and key from a k8s secret and point the env at the mounted paths.

---

## Akeyless wiring

### Web target

Create a web target pointing at the rotator's `/sync/rotate` URL:

```bash
akeyless create-web-target --profile admin \
  --name /Targets/msgraph-rotator \
  --url http://custom-producer.rotator.svc.cluster.local:8080/sync/rotate
```

### Rotated secret

Create one rotated secret per target app, seeding the payload with the target's current secret value and keyId (Microsoft never redisplays an existing secret, so seed both at create time):

```bash
PAYLOAD='{"type":"microsoft_graph_app_secret","tenant_id":"<tenant>","client_id":"<target-app-appId>","client_secret":"<current-secret>","key_id":"<current-keyId>"}'

akeyless create-rotated-secret --profile admin --gateway-url http://localhost:18000 \
  --name /3-Rotated_Secrets/o365-client-secret \
  --target-name /Targets/msgraph-rotator \
  --rotator-type custom \
  --authentication-credentials use-user-creds \
  --custom-payload "$PAYLOAD" \
  --rotation-interval 10080
```

`--rotation-interval` is **minutes** for custom rotators; `10080` is weekly. New secrets are created with a 14-day `endDateTime` in Graph, so a single failed rotation leaves the live secret valid through the next scheduled attempt.

Payload fields:

| Field | Meaning |
|-------|---------|
| `type` | `microsoft_graph_app_secret` |
| `tenant_id` | Target app's Entra tenant (must match the rotator tenant) |
| `client_id` | Target application's appId |
| `client_secret` | Rotated. The live value consumers read. Seed at create time. |
| `key_id` | Rotated. Current secret's keyId, removed on the next rotation. Seed at create time. |
| `display_name` | Optional. Secret display name in Graph, defaults to `akeyless-rotated`. |

---

## Verification

```bash
akeyless rotate-secret --name /3-Rotated_Secrets/o365-client-secret \
  --profile admin --gateway-url http://localhost:18000
akeyless get-rotated-secret-value --name /3-Rotated_Secrets/o365-client-secret --profile admin
```

The returned `key_id` should match a `passwordCredential` on the target app:

```bash
az rest --method GET \
  --url "https://graph.microsoft.com/v1.0/applications(appId='<target-app-appId>')?\$select=passwordCredentials" \
  --query 'passwordCredentials[].keyId' -o tsv
```

Rotate twice; after the second rotation (and Graph convergence, a few seconds), the previous `key_id` should be gone and only the new one remain.

---

## Behavior notes

- **Create-before-revoke.** Each rotation adds the new secret first, then removes the previous one, so a valid credential is always present. Removal is best-effort: if it fails the rotation still succeeds.
- **HTTP 409 concurrency.** Entra returns `Directory_ConcurrencyViolation` when `removePassword` runs immediately after `addPassword` on the same app, because the object change has not converged. The rotator retries 409/429/5xx with backoff per Entra's own "wait briefly and retry" guidance.
- **Read-after-write.** Graph reads of `passwordCredentials` can lag writes by several seconds. When verifying, wait ~10-15s after a rotation before reading.

---

## Decommissioning

Remove the rotated secret and web target, then clean up Azure:

```bash
akeyless delete-rotated-secret --name /3-Rotated_Secrets/o365-client-secret --profile admin --gateway-url http://localhost:18000
akeyless delete-web-target --name /Targets/msgraph-rotator --profile admin --gateway-url http://localhost:18000   # if CLI supports; else via the console
az ad app delete --id <rotator-app-R-appId>
```
