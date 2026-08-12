# Runbook: Apigee X Management-API Token Rotation via Custom Producer

> **Rotator:** `apigee_x_token` in this repo. See the [main README](../README.md#supported-targets) for where this runbook fits.
> **Scope:** Short-lived OAuth2 access tokens for the Apigee X management API, minted from a GCP service-account key and rotated through an Akeyless Rotated Secret.
> **Status:** Validated end to end against a live Apigee X organization through an Akeyless Gateway. The minted token returned HTTP 200 from `apigee.googleapis.com`, and the stored value contained no service-account private key.
> **Estimated time:** about 15 minutes, given a deployed rotator and a reachable Akeyless gateway.
> **Prerequisites:** an Apigee X organization provisioned in a GCP project; permission to create a service account and grant IAM roles in that project; write access to the rotator Kubernetes deployment; an Akeyless gateway that can reach the rotator; an Akeyless access ID with admin on `/Targets/*` and `/3-Rotated_Secrets/*`.

---

## What this runbook solves

Apigee X has no native Akeyless rotator. The `apigee_x_token` target mints a fresh OAuth2 access token for the Apigee management API on each rotation and stores it in Akeyless. Consumers fetch the current token with `akeyless get-rotated-secret-value` and use it as an HTTP `Authorization: Bearer` value against `https://apigee.googleapis.com`.

The token is derived from a GCP service-account key using the JWT-bearer grant defined in RFC 7523. The rotator signs a short-lived assertion with the service-account private key, exchanges it at `https://oauth2.googleapis.com/token`, and stores the returned access token with its expiry.

### Why the service-account key lives in the rotator environment, not the payload

A custom producer payload is round-tripped: the rotator output becomes the next rotation input, and any principal that can read the rotated secret value reads the entire payload. If the long-lived service-account private key were part of the payload, every consumer with read access to the secret would receive the private key.

The design separates the two. The payload carries only non-secret context plus the minted short-lived token, and is what consumers read. The service-account key lives in the rotator deployment environment, set once by the operator, and never enters the payload or the stored secret value. The unit tests assert this guarantee directly: the rotation response must not contain `private_key` or `service_account`.

---

## Failure modes you are avoiding

**Broad-purpose service account.** Use a dedicated least-privilege service account that holds only the single Apigee IAM role it needs. Do not reuse a service account with wider cloud permissions, because anyone who reads the rotated token can act with that service account's Apigee authority for the token's lifetime.

**Over-scoped token.** The default scope is `https://www.googleapis.com/auth/cloud-platform`, which authorizes any API the service account can call. The Apigee management API has no narrower public OAuth2 scope, so scope narrowing is done through IAM, not through the OAuth2 scope. Grant the service account the minimal Apigee role: `roles/apigee.admin` for management operations, or a viewer role for read-only consumers.

**Rotation interval longer than the token lifetime.** GCP OAuth2 access tokens are short-lived, about one hour, and cannot be revoked individually before they expire. The `Revoke` endpoint acknowledges the request and takes no action. Set the Akeyless rotation interval below the token lifetime so a fresh token is always available before the old one expires. Because the interval unit for a custom rotator is minutes, an interval of 45 to 50 minutes is a reasonable default for a one-hour token.

---

## How it works

```mermaid
sequenceDiagram
    participant GW as Akeyless Gateway
    participant WT as Web Target (/sync/rotate)
    participant R as Rotator
    participant Auth as auth.akeyless.io
    participant OAuth as oauth2.googleapis.com

    GW->>WT: POST /sync/rotate (AkeylessCreds header + clean payload)
    WT->>R: deliver request
    R->>Auth: validate-producer-credentials (expected_access_id)
    Auth-->>R: access_id
    Note over R: reject with 401 if access_id != AKEYLESS_ACCESS_ID
    R->>R: read service-account key from env (APIGEE_SERVICE_ACCOUNT_JSON)
    R->>R: sign RS256 JWT assertion (iss, scope, aud, iat, exp, jti)
    R->>OAuth: POST grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer
    OAuth-->>R: access_token + expires_in
    R-->>GW: updated payload (token + expires_at set; no private key)
    GW->>GW: store new secret value
```

The service-account key never appears in the request or response body. The gateway stores only the clean payload that the rotator returns.

---

## Payload reference

The rotated secret payload is a JSON object. The rotator reads `type` to dispatch, resolves the service-account key from the named environment variable, and writes `token` and `expires_at` back on each rotation. All other fields are optional.

| Field | Required | Description |
|-------|----------|-------------|
| `type` | yes | Must be `apigee_x_token`. Drives handler dispatch. |
| `service_account_ref` | no | Name of the environment variable holding the service-account key JSON. Defaults to `APIGEE_SERVICE_ACCOUNT_JSON`. Set this only when one rotator serves multiple Apigee orgs with different service accounts. |
| `scope` | no | OAuth2 scope to request. Defaults to `https://www.googleapis.com/auth/cloud-platform`. Override only with a valid Google OAuth2 scope. |
| `organization` | no | The Apigee org the consumer targets, for human and audit context. The rotator does not use this field for the token exchange. |
| `token` | managed | The minted access token. Set by the rotator on each rotation. |
| `expires_at` | managed | RFC3339 timestamp marking when `token` stops being valid. Set by the rotator. |

Payload at create time, before the first rotation. Note what is absent: the service-account key.

```json
{
  "type": "apigee_x_token",
  "organization": "your-apigee-org"
}
```

Payload after a rotation:

```json
{
  "type": "apigee_x_token",
  "organization": "your-apigee-org",
  "token": "ya29.example",
  "expires_at": "2026-08-12T15:45:00Z"
}
```

---

## 1. Create the service account in GCP

Run these commands with `gcloud`. The GCP project that hosts your Apigee organization and the Apigee org name are not always identical, so set both explicitly.

```bash
GCP_PROJECT=your-gcp-project        # project that hosts the Apigee org
APIGEE_ORG=your-apigee-org          # the Apigee organization name
SA=apigee-rotator@${GCP_PROJECT}.iam.gserviceaccount.com

gcloud iam service-accounts create apigee-rotator --project=${GCP_PROJECT}

# Grant the least privilege the consumer needs. apigee.admin is full
# management access; use roles/apigee.readOnlyAdmin for read-only consumers.
gcloud projects add-iam-policy-binding ${GCP_PROJECT} \
  --member="serviceAccount:${SA}" \
  --role="roles/apigee.admin"

# Create the key the rotator will use. Treat key.json as a long-lived secret.
gcloud iam service-accounts keys create key.json --iam-account=${SA}
```

Confirm the service account can reach the Apigee management API before wiring it into Akeyless. If you have the Google auth Python libraries installed, exchange the key directly:

```bash
TOKEN=$(python3 -c "import google.auth.transport.requests, google.oauth2.service_account as sa; c=sa.Credentials.from_service_account_file('key.json', scopes=['https://www.googleapis.com/auth/cloud-platform']); c.refresh(google.auth.transport.requests.Request()); print(c.token)")
curl -s -H "Authorization: Bearer ${TOKEN}" \
  https://apigee.googleapis.com/v1/organizations/${APIGEE_ORG}
```

A 200 response confirms the key and IAM role are correct. If you do not have the Python libraries handy, the rotator itself performs this same exchange in step 5, so you can defer the check until then.

## 2. Put the service-account key in the rotator environment

The rotator reads the service-account key from the environment variable named by `service_account_ref`, defaulting to `APIGEE_SERVICE_ACCOUNT_JSON`. Load the key into the rotator Kubernetes deployment as a Kubernetes Secret referenced by env, never as a literal container env value and never in the payload.

Create a Secret in the rotator namespace from the key file produced in step 1:

```bash
kubectl -n rotator create secret generic apigee-rotator-sa \
  --from-file=APIGEE_SERVICE_ACCOUNT_JSON=key.json
```

Then add an env entry to the rotator Deployment that references it. Place this alongside the existing `AKEYLESS_ACCESS_ID` env in the container spec:

```yaml
        env:
        - name: AKEYLESS_ACCESS_ID
          value: "p-REPLACE-ME"
        # Service-account key for apigee_x_token rotation. Sourced from the
        # Secret created above. The key JSON is a long-lived credential.
        - name: APIGEE_SERVICE_ACCOUNT_JSON
          valueFrom:
            secretKeyRef:
              name: apigee-rotator-sa
              key: APIGEE_SERVICE_ACCOUNT_JSON
```

Apply and confirm the new env is present on the running pod:

```bash
kubectl apply -f deployment.yaml
kubectl -n rotator rollout restart deployment/custom-producer
kubectl -n rotator get pods
kubectl -n rotator exec deployment/custom-producer -- sh -c 'test -n "$APIGEE_SERVICE_ACCOUNT_JSON" && echo set || echo missing'
```

The last command must print `set`. If it prints `missing`, the Secret was not mounted before the pod started; check the Secret name and key, then restart the rollout again.

### Serving multiple Apigee orgs from one rotator

One rotator can mint tokens for several service accounts. Give each a distinct environment variable, then point each rotated secret at its own variable with `service_account_ref`. For two orgs:

```bash
kubectl -n rotator create secret generic apigee-rotator-sa \
  --from-file=APIGEE_SERVICE_ACCOUNT_JSON=key-org-a.json \
  --from-file=APIGEE_SA_ORG_B=key-org-b.json
```

Mount both as env, and set `"service_account_ref": "APIGEE_SA_ORG_B"` in the second org's payload. The default `APIGEE_SERVICE_ACCOUNT_JSON` covers the single-org case without a ref.

## 3. Create the Web Target and Rotated Secret in Akeyless

The gateway posts each rotation to the Web Target URL exactly as configured. The URL must end in `/sync/rotate`, because that is the route the rotator serves and the gateway appends nothing. If a Web Target pointing at `<rotator-base-url>/sync/rotate` already exists from another runbook, reuse it. Otherwise create one now. See the [Deploying to Kubernetes](../README.md#deploying-to-kubernetes) section of the README for the value of `<rotator-base-url>` in your topology.

Commands that create or rotate a secret run against the gateway Configuration Management port. Port-forward that port if it is not directly reachable from where you run the CLI:

```bash
kubectl -n infra-security port-forward svc/akeyless-gateway 18000:8000 &
GW=http://localhost:18000
ROTATOR_BASE=http://custom-producer.rotator.svc.cluster.local:8080
TARGET=/Targets/custom-producer-apigee
NAME=/3-Rotated_Secrets/apigee-x-token
APIGEE_ORG=your-apigee-org

akeyless create-web-target \
  --name "$TARGET" \
  --url "${ROTATOR_BASE}/sync/rotate" \
  --profile admin

akeyless create-rotated-secret \
  --name "$NAME" \
  --target-name "$TARGET" \
  --rotator-type custom \
  --auto-rotate true \
  --rotation-interval 45 \
  --custom-payload "{\"type\":\"apigee_x_token\",\"organization\":\"${APIGEE_ORG}\"}" \
  --gateway-url "$GW" \
  --profile admin
```

The payload deliberately omits the service-account key. The `--rotation-interval` is in minutes. Set it below 60 so a fresh token is minted before the previous one-hour token expires.

## 4. Trigger the first rotation and read the value

Force an immediate rotation rather than waiting for the interval. `rotate-secret` requires the gateway URL:

```bash
akeyless rotate-secret --name "$NAME" --gateway-url "$GW" --profile admin
```

Read the rotated value. `get-rotated-secret-value` does not take `--gateway-url` or `-u`; it reads through the standard API:

```bash
akeyless get-rotated-secret-value --name "$NAME" --profile admin --json
```

The `value.payload` field is a JSON string containing `type`, `organization`, `token`, and `expires_at`. Confirm there is no private key:

```bash
akeyless get-rotated-secret-value --name "$NAME" --profile admin --json \
  | grep -c private_key        # expected: 0
```

## 5. Confirm the token works against the Apigee API

Extract the minted token and call the Apigee management API. The `--jq-expression` flag runs jq against the full response object. Because `value.payload` is a JSON string, `fromjson` parses it before `.token` selects the field:

```bash
APIGEE_ORG=your-apigee-org
TOKEN=$(akeyless get-rotated-secret-value --name "$NAME" --profile admin \
  --jq-expression '.value.payload | fromjson | .token')

curl -s -H "Authorization: Bearer ${TOKEN}" \
  https://apigee.googleapis.com/v1/organizations/${APIGEE_ORG}
```

A 200 response with the organization details is the end-to-end proof that the rotator works. This is the same exchange the validation at the top of this runbook performed.

Consumers in production should read the token the same way: select `.value.payload | fromjson | .token` from the `get-rotated-secret-value` JSON output and use it as the bearer token. There is no `--field` flag on `get-rotated-secret-value`; jq is the extraction mechanism.

---

## Troubleshooting

**`service account key not found: environment variable "APIGEE_SERVICE_ACCOUNT_JSON" is not set` from the rotator.** The rotator pod does not have the key in its environment. Confirm step 2: the Secret exists in the rotator namespace, the Deployment env references it, and the pod restarted after the apply. If you set `service_account_ref` in the payload, confirm that variable name matches an env var actually mounted on the pod.

**`jwt-bearer exchange (HTTP 400)` from the rotator.** The assertion was rejected. The most common cause is a malformed or truncated service-account key, such as a value split across lines by the shell when the Secret was created. Create the Secret with `--from-file=` so the key is ingested verbatim. A disabled or deleted service-account key also produces this error.

**`jwt-bearer exchange (HTTP 403)` from the rotator.** The token exchange succeeded conceptually but Google refused the grant. This points to a key whose service account lacks access, or to an org policy blocking the JWT-bearer grant.

**403 from `apigee.googleapis.com` with a valid-looking token.** The token minted but the service account lacks the Apigee IAM role. Confirm the binding from step 1 with `gcloud projects get-iam-policy ${GCP_PROJECT} --flatten='bindings[].members' --filter=bindings.members:${SA}`.

**The token works but expires before the next rotation.** Lower the `--rotation-interval`. The interval is minutes, so `45` rotates roughly three quarters of the way through a one-hour token.

**404 `page not found` from the gateway on rotation.** The Web Target URL does not end in `/sync/rotate`. The gateway posts to the target URL as-is and appends nothing, so the path must be present in the URL. Update the target with `akeyless target update web --name "$TARGET" --url "${ROTATOR_BASE}/sync/rotate" --profile admin`.

**`unknown target type: apigee_x_token`.** The deployed rotator image predates this target. Rebuild and redeploy the rotator image from the commit that includes `apigee_x_token`.
