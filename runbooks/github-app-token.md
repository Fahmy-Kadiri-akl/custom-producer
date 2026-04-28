# Runbook: GitHub Installation Token Rotation via GitHub App

> **Rotator:** `github_app_token` in this repo. See the [main README](../README.md#github_app_token) for where this runbook fits.
> **Scope:** GitHub App registration, App installation, Akeyless Rotated Secret wiring. Covers everything needed to make GitHub installation-token rotation work end to end.
> **Status:** Verified end to end against a Fahmy-Kadiri-akl App on github.com.
> **Estimated time:** ~10 minutes for initial bootstrap; ~2 minutes to rotate the App's signing key.
> **Prerequisites:** GitHub account that owns (user) or administers (organization) the App, plus admin rights on at least one repository the App will be installed on; `kubectl` access to the rotator's namespace; an Akeyless CLI profile with permission to create Web Targets and Rotated Secrets.

---

## What this runbook solves

You have an automation system (a CI job, a deploy worker, a script that pushes to a GitHub repo) that needs **short-lived GitHub credentials it can mint on demand**. The obvious design (service account → personal access token → REST API) does not work for any new credential, because GitHub does not expose a creation endpoint for personal access tokens at all.

> GitHub REST API documentation, *Personal access tokens (organization)*:
> *"Updates the access organization members have to organization resources via fine-grained personal access tokens. Limited to revoking a token's existing access."*

That endpoint is the only PAT-shaped one in the public REST API. There is no fine-grained PAT creation endpoint, no classic PAT creation endpoint, no `POST /user/tokens` of any kind. PATs (classic or fine-grained) can only be created through the github.com web UI by a signed-in human.

The supported automation path is:

1. A human creates a **GitHub App** once, picks the permissions and repository scope, and downloads an **RSA private key**.
2. The App is **installed** on a target user account or organization, producing an **installation ID**.
3. The automation signs a short JWT (RS256) with the private key, sends it to `POST /app/installations/{installation_id}/access_tokens`, and receives a **`ghs_...` installation access token** valid for one hour.
4. Steps 3 repeats indefinitely. There is no token to persist across cycles; the App ID + installation ID + private key are the long-lived state.

This runbook covers steps 1 through 4 in isolation, so the output (an `app_id` + `installation_id` + private-key triple) can be consumed by any downstream system. Section 4 wires that triple into an Akeyless Rotated Secret with the `github_app_token` rotator.

---

## Failure mode you are avoiding

The alternative is putting a long-lived classic PAT into the automation and pretending it is rotated. It "works" until one of:

- the human who issued it leaves the org and the PAT is revoked (silent breakage on the next CI run),
- the PAT hits its expiry (silent breakage),
- a security review notices a 90-day-lifetime PAT in a vault and asks why it is not rotated (audit finding),
- the PAT is accidentally committed somewhere and someone has to do an emergency revoke at 02:00 (incident).

Installation access tokens auto-expire in ~1 hour, are scoped to a specific App's installed permissions on specific repositories, and cannot be created by anyone who does not hold the App's private key. A leaked installation token is fully expired before most credential scanners have indexed the leak.

If you see GitHub-side automation failing because of expired or revoked PATs, that is the symptom of the alternative. The fix is this runbook.

---

## Architecture

### Bootstrap flow (runs once)

```mermaid
sequenceDiagram
    actor Human as Human Operator
    participant GH as github.com
    participant Disk as Local Disk
    participant Store as Secret Store

    Human->>GH: Settings > Developer settings > GitHub Apps > New
    Human->>GH: choose name, permissions, "Webhook Active = off"
    GH-->>Human: App created (App ID printed)
    Human->>GH: Generate a private key
    GH-->>Disk: download <app-name>.<date>.private-key.pem
    Human->>GH: Install App on user/org, pick repos
    GH-->>Human: redirect to /settings/installations/<id> (Installation ID printed)
    Human->>Store: seed app_id + installation_id + private_key
```

### Rotation flow (runs every cycle)

```mermaid
sequenceDiagram
    participant Rotator as Rotator
    participant Store as Secret Store
    participant GH as github.com (api.github.com)

    Rotator->>Store: read app_id, installation_id, private_key, prev_token
    Store-->>Rotator: payload
    Rotator->>Rotator: sign RS256 JWT (iat-30s, exp+9m, iss=app_id)
    Rotator->>GH: POST /app/installations/{installation_id}/access_tokens<br/>(Bearer JWT, optional repos+permissions scope)
    GH-->>Rotator: 201 Created<br/>{ token: ghs_..., expires_at: ... }
    Rotator->>Store: persist new ghs_token, expires_at
    opt prev_token present
        Rotator->>GH: DELETE /installation/token<br/>(Bearer prev_token)
        GH-->>Rotator: 204 No Content (best effort)
    end
    Rotator-->>Store: rotated payload (token + expires_at)
```

The new token is written to the secret store **before** the old token is revoked. If revoke fails, rotation still succeeds: the old token will auto-expire within an hour anyway, and the new token is already live. This is the same create-before-revoke pattern the ADO PAT rotator uses, applied to a credential family that auto-expires.

### Components

| Component | Role | Where it lives |
|---|---|---|
| **GitHub App** | Account-scoped trust object holding the App ID, permissions, and signing key. One per automation chain. | github.com (under your user or org's *Developer settings*) |
| **App private key** | RSA private key (PEM, PKCS#1 or PKCS#8). The only credential that authenticates as the App. | Generated once by GitHub, downloaded to disk, then transferred straight into the secret store and deleted from disk. |
| **Installation** | A binding between an App and one user or org, with a list of repositories the App can act on. The installation has its own numeric ID, used in the token endpoint URL. | github.com (under *Settings > Installations*) |
| **Installation access token** | Short-lived (~1h) `ghs_...` token. Authenticates API calls as the App acting on the installation's behalf. **This is the value the rotator produces.** | Akeyless rotated secret payload, regenerated on every rotation |
| **`github_app_token` rotator** | Code in `go/rotator/internal/targets/github/` that signs the JWT and exchanges it. | This repo, packaged in the rotator container image |
| **Akeyless Web Target** | Akeyless object pointing at the rotator's `/sync/rotate` URL. One per rotator deployment. | Akeyless Console / API |
| **Akeyless Rotated Secret** | Akeyless object holding the JSON payload with `app_id`, `installation_id`, `private_key`, and (after first rotation) `token` and `expires_at`. | Akeyless Console / API |

### Key constants for every account

| Constant | Value | What it is |
|---|---|---|
| **GitHub API base** | `https://api.github.com` | All REST calls. |
| **App-as-app endpoint** | `POST /app/installations/{installation_id}/access_tokens` | Mints an installation token. Authenticated by App JWT. |
| **Token revoke endpoint** | `DELETE /installation/token` | Invalidates the bearer token immediately. Authenticated by the token itself. |
| **Required JWT signing alg** | `RS256` | GitHub rejects HS256 and ES256. |
| **JWT max `exp` window** | 10 minutes | The rotator uses 9 minutes. Anything 10+ is rejected with HTTP 401. |
| **JWT clock-skew tolerance** | 60 seconds | The rotator backdates `iat` by 30 seconds to stay safe. |
| **`X-GitHub-Api-Version`** | `2022-11-28` | The pinned API version this rotator targets. |

Tokens cannot be extended past their expiry. To get a fresh token, mint a new one. There is no refresh-token concept; the App private key plays the role of the long-lived secret.

---

## Prerequisites

- You know whether the App should live under a **user** or an **organization** account. User-owned Apps are simpler; org-owned Apps are required if any consumer needs the token to act on org-only resources (org secrets, org rulesets, etc.). This runbook covers both; pick one in Step 1a.
- You have the GitHub login of the account that will own the App.
- You have admin rights on the repositories you intend to install the App on. For org-owned Apps, you need *Owner* or *Member with App-management rights*; for user-owned Apps installed on org repos, you also need org admin to approve the install.
- The rotator container image is built and the deployment is reachable per the [main README](../README.md). Health check returns `{"status":"healthy"}` from inside the cluster.
- An Akeyless CLI profile (named `admin` in this runbook) configured against your gateway with permission to create `/Targets/*` and `/Rotated/*` items.

---

## Roles and responsibilities

This runbook involves up to **three different people**.

| Role | Who / what permissions | Steps they perform |
|---|---|---|
| **A. GitHub account owner / org admin** | The user themselves (for user-owned Apps), or an *Owner* of the org (for org-owned Apps). Holds the right to create and configure GitHub Apps under that account. | 1a, 1b, 1c, 1d, 2a, 2b; Decommission steps D1, D2, D3 |
| **B. Repository admin** | A user with admin rights on each repository the App will act on. May be the same person as Role A. For "Install on all repos" with personal accounts this is automatic. | 2a (only when installing on a third party's repos that need cross-account approval) |
| **C. Automation operator / Akeyless admin** | Whoever owns the Akeyless account and the rotator deployment. Needs admin access in Akeyless and write access to the rotator's Kubernetes namespace. Needs no GitHub permissions beyond access to the private key file Role A produced. | 3a, 3b, 3c, 3d, Step 4, Verification, ongoing operations |

If Role A is not you: skip to [Step 1](#step-1-create-the-github-app), copy the steps verbatim into a ticket, ask Role A to perform them and send back the App ID, the `.pem` file, and the installation ID. Resume from Step 3.

### Quick permission check (Role A, org case only)

For an org-owned App you need GitHub-Apps management rights:

```bash
gh api -H "Accept: application/vnd.github+json" \
  "orgs/<org>/memberships/$(gh api user --jq .login)" \
  --jq '{role, state}'
# expected: {"role":"admin","state":"active"}
```

A `role` other than `admin` means you cannot create org-owned Apps. Either ask an org Owner to do Step 1, or fall back to a user-owned App.

### Verify `gh` context (Role A or Role C)

```bash
gh auth status
```

Confirm the active account is the one you intend to use. If not: `gh auth switch` or `gh auth login`.

---

## Step 1: Create the GitHub App

> **👤 Performed by: Role A (GitHub account owner / org admin)**
> **Required permissions:** owner of the user account, or *Owner* of the org.
> **If this is not you:** copy the form values below to your account owner. Have them complete steps 1a to 1d and send back the **App ID** (a number) and the **`.pem` file** they downloaded in 1d. Resume from Step 2.

GitHub App creation is one-time and interactive. It cannot be fully scripted because GitHub does not expose an unauthenticated "create-App" endpoint; the App-manifest flow exists but still requires a human click to confirm. Plan for ~3 minutes of clicking.

### 1a. Decide owner: user or org *(Role A)*

| Use case | Pick |
|---|---|
| Tokens only need to act on the App owner's repos | User-owned. URL: `https://github.com/settings/apps/new` |
| Tokens need to act on an org's repos OR org-level resources (secrets, rulesets, members) | Org-owned. URL: `https://github.com/organizations/<org>/settings/apps/new` |
| Multiple consumers across orgs | Multiple installations of one user-owned App, or one App per org |

User-owned Apps are simpler and the rest of this runbook assumes that path. Org-owned Apps differ only in the *Where can this GitHub App be installed?* default (org-owned defaults to *This account only*; both produce the same rotator payload).

### 1b. Create the App *(Role A)*

Open the URL from 1a. Fill in:

| Field | Value |
|---|---|
| GitHub App name | Any globally unique name. Example: `akeyless-rotator-<short-handle>`. |
| Description | Optional. A line about what consumes the tokens helps future you. |
| Homepage URL | Anything valid. Your GitHub profile URL is fine. |
| Identifying and authorizing users | Leave the Callback URL blank. The rotator does not use it. |
| Webhook → **Active** | **Uncheck.** The rotator does not consume webhooks. Leaving Active checked with no URL fails server-side. |
| Webhook URL | Greyed out once Active is unchecked. |
| Webhook secret | Empty. |
| Repository permissions | Pick the minimum set the consumer needs. Common starting point: *Contents: Read-only*, *Metadata: Read-only* (auto-set). Add *Contents: Read and write* if the consumer pushes commits, etc. |
| Organization permissions | Default *No access* unless a consumer needs an org-only resource. Granting org permissions to a user-owned App still requires org-side install approval in 2a. |
| Account permissions | Default *No access*. |
| Where can this GitHub App be installed? | *Only on this account* for the simplest setup. Switch to *Any account* only if multiple users or orgs will install the same App. |
| Subscribe to events | None. The rotator does not consume webhooks. |

Click **Create GitHub App**.

### 1c. Capture the App ID *(Role A)*

After creation, GitHub redirects to the App's settings page. The **App ID** is a number (typically 6-8 digits) printed near the top of the page. Save it; you will paste it verbatim into the rotator payload in Step 4.

```text
App ID: 1234567   <-- numeric, paste this into payload
```

> The "Client ID" shown on the same page is for OAuth user-flows. Do not confuse it with the App ID. The `github_app_token` payload uses **App ID**.

### 1d. Generate a private key *(Role A)*

Scroll the App settings page to *Private keys* and click **Generate a private key**. GitHub downloads a single PEM file named `<app-name>.<date>.private-key.pem`. The browser does not show the file contents; you must save it locally.

Treat this file like a secret. Anyone with it can authenticate as your App with the App's full installed permissions across every installation. Two reasonable handling patterns:

- **Direct seed.** Move it straight to the host where you will create the Akeyless Rotated Secret in Step 4 (`scp` is fine), seed the secret, then `shred -u` the file.
- **Vault transit.** If your secret store has its own out-of-band onboarding (e.g. an Akeyless dynamic secret bootstrap or a HashiCorp Vault seed), use that.

If you ever rotate the App's signing key (highly recommended every 6-12 months), you generate a new one here, swap it into the Akeyless payload (Step 4 again), and delete the old one from the App's settings page after a brief overlap window.

---

## Step 2: Install the App

> **👤 Performed by: Role A (and Role B for cross-account installs)**
> **Required permissions:** Owner of the account/org you are installing on. For installs that touch a third-party org, that org's Owner must approve.

### 2a. Choose install scope and trigger install *(Role A)*

On the App settings page, click **Install App** in the left rail. GitHub lists every account and org you can install on.

| Choice | Effect |
|---|---|
| *All repositories* | Tokens minted by this installation can target any current or future repository in the chosen account. |
| *Only select repositories* | Tokens are constrained to the chosen list. Adding a repo later is a separate UI click. |

Pick *Only select repositories* whenever you can. The rotator can further narrow scope per-rotation via the `repositories` field in the payload, but it cannot **broaden** beyond the install's scope.

If you are installing a user-owned App on an org's repos: the org must allow user-owned Apps, and an org Owner (Role B) must approve the install. The UI handles this with an "Approval needed" prompt sent to org admins.

### 2b. Capture the installation ID *(Role A)*

After installing, GitHub redirects you to:

```
https://github.com/settings/installations/<installation_id>            (user-owned)
https://github.com/organizations/<org>/settings/installations/<id>     (org-owned)
```

The trailing number is the **installation ID**. Save it.

You can also list installations from the command line, using a JWT signed by the App:

```bash
APP_ID=1234567
PEM=/path/to/akeyless-rotator-<short-handle>.<date>.private-key.pem

JWT=$(python3 - <<PY
import base64, json, time, sys
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import padding
key = serialization.load_pem_private_key(open("$PEM","rb").read(), password=None)
now = int(time.time())
header = base64.urlsafe_b64encode(json.dumps({"alg":"RS256","typ":"JWT"}).encode()).rstrip(b"=").decode()
claims = base64.urlsafe_b64encode(json.dumps({"iat":now-30,"exp":now+540,"iss":$APP_ID}).encode()).rstrip(b"=").decode()
signing_input = f"{header}.{claims}".encode()
sig = key.sign(signing_input, padding.PKCS1v15(), hashes.SHA256())
print(f"{header}.{claims}." + base64.urlsafe_b64encode(sig).rstrip(b"=").decode())
PY
)

curl -sS -H "Authorization: Bearer $JWT" \
        -H "Accept: application/vnd.github+json" \
        -H "X-GitHub-Api-Version: 2022-11-28" \
        https://api.github.com/app/installations \
  | jq '.[] | {id, account: .account.login, target_type, repo_selection}'
# expected:
# {
#   "id": 87654321,
#   "account": "Fahmy-Kadiri-akl",
#   "target_type": "User",
#   "repo_selection": "selected"
# }
```

The `id` field is the installation ID.

---

## Step 3: Smoke-test the credentials directly

> **👤 Performed by: Role C (automation operator)**
> **Required permissions:** read access to the `.pem` file. No GitHub permissions beyond holding the key.

Before wiring Akeyless, prove the App ID + installation ID + private key triple actually works. This catches mismatched IDs, broken PEM escaping, and missing permissions before they show up as Akeyless rotation failures.

### 3a. Sign a JWT and mint a token *(Role C)*

Reuse `JWT` from 2b, or sign a new one. Then:

```bash
INSTALLATION_ID=87654321

curl -sS -X POST \
  -H "Authorization: Bearer $JWT" \
  -H "Accept: application/vnd.github+json" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  "https://api.github.com/app/installations/${INSTALLATION_ID}/access_tokens" \
  | jq '{token_prefix: (.token[:4]), expires_at, repository_selection}'
# expected:
# {
#   "token_prefix": "ghs_",
#   "expires_at":   "2026-04-28T16:58:05Z",
#   "repository_selection": "selected"
# }
```

If `token_prefix` is `ghs_` and `expires_at` is roughly an hour in the future, the App side is healthy.

### 3b. Use the token *(Role C)*

```bash
GHS_TOKEN=$(curl -sS -X POST \
  -H "Authorization: Bearer $JWT" \
  -H "Accept: application/vnd.github+json" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  "https://api.github.com/app/installations/${INSTALLATION_ID}/access_tokens" \
  | jq -r .token)

curl -sS -H "Authorization: Bearer $GHS_TOKEN" \
        -H "Accept: application/vnd.github+json" \
        https://api.github.com/installation/repositories \
  | jq '{total_count, repos: [.repositories[].full_name][:5]}'
# expected: total_count > 0 and at least one repo from the install scope
```

### 3c. Optional: scope the mint *(Role C)*

If the consumer needs less than the install's full scope (e.g. one repo out of many, or read-only when the install grants read+write), pass the scope in the body:

```bash
curl -sS -X POST \
  -H "Authorization: Bearer $JWT" \
  -H "Accept: application/vnd.github+json" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  -H "Content-Type: application/json" \
  --data '{"repositories":["my-repo"],"permissions":{"contents":"read","metadata":"read"}}' \
  "https://api.github.com/app/installations/${INSTALLATION_ID}/access_tokens" \
  | jq '{token_prefix: (.token[:4]), permissions, repository_selection, repos: [.repositories[].name]}'
```

The same `repositories` and `permissions` fields go into the rotator payload.

### 3d. Revoke the smoke-test token *(Role C)*

```bash
curl -sS -X DELETE \
  -H "Authorization: Bearer $GHS_TOKEN" \
  -H "Accept: application/vnd.github+json" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  https://api.github.com/installation/token \
  -w "revoke: HTTP %{http_code}\n"
# expected: HTTP 204
```

This is best-effort; the token would auto-expire in an hour either way. The rotator does the same call after each rotation.

---

## Step 4: Wire the credentials into an Akeyless rotated secret

> **👤 Performed by: Role C (automation operator)**
> **Required permissions:** Akeyless admin (create/update Web Targets and Rotated Secrets), and `kubectl` access to the rotator's namespace.

### 4a. Select an Akeyless admin profile *(Role C)*

```bash
~/code/akeyless/akeyless configure --profile admin \
  --access-id "<your-access-id>" \
  --access-type access_key \
  --access-key "<your-access-key>" \
  --gateway-url "<your-gateway-url>"
```

Verify:

```bash
cat ~/.akeyless/profiles/admin.toml
# expected shape:
#   access_id  = 'p-xxxxxxxxxxxxxx'
#   access_key = '...'
#   gateway_url = 'https://api.akeyless.io'   # or your on-prem gateway
```

The rest of this runbook calls the CLI with `--token=admin` to use this profile.

### 4b. Match the rotator's expected access ID to your gateway *(Role C)*

The rotator pod validates every inbound `AkeylessCreds` JWT against the access ID in its `AKEYLESS_ACCESS_ID` env var. If that does not match the access ID your gateway uses to sign outbound webhook calls, every rotation fails with `unauthorized access for access id ...`.

```bash
# 1. Get the gateway's access ID
GW_NS=infra-security              # adjust to your gateway's namespace
GW_ACCESS_ID=$(kubectl -n "$GW_NS" get secret akeyless-gateway-conf-secret \
  -o jsonpath='{.data.gateway-access-id}' | base64 -d)
echo "$GW_ACCESS_ID"
# expected: p-xxxxxxxxxxxxxx

# 2. Patch the rotator's deployment env var to match
kubectl -n rotator set env deployment/custom-producer \
  AKEYLESS_ACCESS_ID="$GW_ACCESS_ID"

# 3. Wait for the rollout
kubectl -n rotator rollout status deployment/custom-producer
```

### 4c. Create the Web Target *(Role C)*

The Akeyless gateway calls the Web Target URL **verbatim**. For rotation, that URL must resolve to the rotator's `/sync/rotate` endpoint.

```bash
ROTATOR_BASE_URL="http://custom-producer.rotator.svc.cluster.local:8080"   # adjust per topology

~/code/akeyless/akeyless target create web --token=admin \
  --name "/Targets/custom-producer" \
  --url "${ROTATOR_BASE_URL}/sync/rotate"
# expected: A new target named /Targets/custom-producer was successfully created
```

For Docker / cross-cluster / external gateway topologies see the main README's [Exposing to the Akeyless Gateway](../README.md#exposing-to-the-akeyless-gateway) section. Whatever the host:port, the URL ends in `/sync/rotate`.

### 4d. Build the initial payload *(Role C)*

The PEM file's newlines must be JSON-escaped as `\n`. Use Python's `json.dumps` rather than hand-editing:

```bash
APP_ID=1234567
INSTALLATION_ID=87654321
PEM_PATH=~/akeyless-rotator-<short-handle>.<date>.private-key.pem

PRIVATE_KEY_JSON=$(python3 -c '
import json, sys
print(json.dumps(open(sys.argv[1]).read()), end="")
' "$PEM_PATH")

# Sanity-check: starts with quote+BEGIN, ends with quote+END+\n
echo "$PRIVATE_KEY_JSON" | head -c 40 ; echo ...
echo "$PRIVATE_KEY_JSON" | tail -c 40
```

Pick a payload shape. Drop `repositories` to use the install's full repo scope; drop `permissions` to use the App's full installed permissions:

```bash
PAYLOAD=$(cat <<JSON
{
  "type": "github_app_token",
  "app_id": ${APP_ID},
  "installation_id": ${INSTALLATION_ID},
  "private_key": ${PRIVATE_KEY_JSON},
  "permissions": {"contents":"read","metadata":"read"}
}
JSON
)

# Sanity-check: parses as JSON
echo "$PAYLOAD" | jq '{type, app_id, installation_id, has_pk: (.private_key|startswith("-----BEGIN")), perms: .permissions}'
```

### 4e. Create the Rotated Secret *(Role C)*

```bash
~/code/akeyless/akeyless create-rotated-secret --token=admin \
  --name "/Rotated/github-app-token-<short-handle>" \
  --target-name "/Targets/custom-producer" \
  --rotator-type custom \
  --auto-rotate true \
  --rotation-interval 1 \
  --custom-payload "$PAYLOAD"
# expected: A new rotated secret named /Rotated/github-app-token-<short-handle> was successfully created
```

Notes on flags:

- `--rotator-type custom`: required for any custom-producer-backed rotated secret. Other types (`api_key`, `password`, etc.) route to Akeyless-native rotators.
- `--rotation-interval 1`: one day. The Akeyless minimum. Installation tokens auto-expire in an hour anyway, so the practical minimum is whatever cadence matches your consumer's caching strategy.
- `--auto-rotate true`: schedules background rotation. Set to `false` if every consumer call rotates just-in-time.

### 4f. Trigger first rotation and verify *(Role C)*

```bash
~/code/akeyless/akeyless rotate-secret --token=admin \
  --name "/Rotated/github-app-token-<short-handle>"
# expected: The Rotated Secret named /Rotated/github-app-token-<short-handle> was successfully rotated
```

Read the rotated value:

```bash
TOKEN=$(~/code/akeyless/akeyless get-rotated-secret-value --token=admin \
  --name "/Rotated/github-app-token-<short-handle>" \
  | jq -r '.value.payload | fromjson | .token')

echo "${TOKEN:0:4}..."
# expected: ghs_...

curl -sS -H "Authorization: Bearer $TOKEN" \
        -H "Accept: application/vnd.github+json" \
        https://api.github.com/installation/repositories \
  | jq '{total_count, sample: [.repositories[].full_name][:3]}'
# expected: total_count > 0, sample contains repos from install scope
```

If both checks pass, rotation works end to end.

### 4g. Ongoing operations *(Role C)*

```bash
AK="~/code/akeyless/akeyless --token=admin"
SECRET=/Rotated/github-app-token-<short-handle>

# Trigger an unscheduled rotation (smoke test)
$AK rotate-secret --name "$SECRET"

# Read the current ghs_... token (consumer workflow)
$AK get-rotated-secret-value --name "$SECRET" | jq -r '.value.payload | fromjson | .token'

# Check rotator status + last error
$AK describe-item --name "$SECRET" | jq '{rotator_status, last_rotation_error}'

# Rebase the payload after rotating the App's signing key (Step 1d)
$AK update-rotated-secret-val --name "$SECRET" --new-custom-payload "$PAYLOAD"
```

```bash
# Tail rotator logs while triggering
kubectl -n rotator logs deploy/custom-producer --tail=50
# expected on success:
#   dispatching request endpoint=rotate type=github_app_token
#   minted GitHub App installation token app_id=... installation_id=... expires_at=...
```

---

## Token lifecycle

| Event | Effect on the installation token |
|---|---|
| Successful `POST /access_tokens` | New `ghs_...` token issued. Previous token, if any, is **not** invalidated by the mint itself; the rotator issues a separate `DELETE /installation/token` to revoke it. |
| ~1 hour since the token was minted | Token expires automatically. Subsequent API calls return HTTP 401. |
| `DELETE /installation/token` | Token is revoked immediately. Returns HTTP 204. |
| App's signing key is regenerated (Step 1d again) and the new key is in the payload | Existing in-flight tokens remain valid until expiry. New tokens are minted by the new key. There is no need to coordinate cutover. |
| App is suspended | Existing tokens remain valid until expiry. Mint calls return HTTP 403 with `Bad credentials`. |
| App is uninstalled from the installation | Existing tokens are immediately revoked. Mint calls against that installation return HTTP 404. |
| App's owner deletes the App | All keys, all installations, all tokens invalidated immediately. |

**Monitoring recommendation:** alarm on `rotator_status: RotationFailed` for `/Rotated/github-app-token-*`. Tokens auto-expire so a missed rotation has at most one hour of consumer impact, but a *persistent* failure (bad payload, key revoked) needs human attention before consumers fail.

### How to rotate the App's signing key

Treat as low-risk and do it on a schedule:

1. Step 1d again, on the same App. GitHub lets you have multiple active keys.
2. Update the Akeyless payload with the new PEM (Step 4g, `update-rotated-secret-val`).
3. Trigger a rotation (Step 4f) and confirm a `ghs_` token comes back.
4. Delete the old key from the App settings page.

No consumer downtime; existing in-flight `ghs_` tokens still work until expiry.

---

## Verification checklist

> **👤 Performed by: Role C**

After Step 4f, confirm:

```bash
SECRET=/Rotated/github-app-token-<short-handle>
AK="~/code/akeyless/akeyless --token=admin"

# 1. Akeyless says rotation is healthy
$AK describe-item --name "$SECRET" \
  | jq '{rotator_status, last_rotation_error}'
# expected: {"rotator_status":"Done","last_rotation_error":null}

# 2. The minted token is a ghs_... and 40 chars
TOKEN=$($AK get-rotated-secret-value --name "$SECRET" | jq -r '.value.payload | fromjson | .token')
[ "${TOKEN:0:4}" = "ghs_" ] && [ ${#TOKEN} -eq 40 ] && echo "✅ token shape OK" || echo "❌ token shape wrong"

# 3. The minted token works against the GitHub API
curl -sS -o /dev/null -w "GitHub API: HTTP %{http_code}\n" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Accept: application/vnd.github+json" \
  https://api.github.com/installation/repositories
# expected: HTTP 200

# 4. A second rotation produces a different token
OLD="$TOKEN"
$AK rotate-secret --name "$SECRET" >/dev/null
NEW=$($AK get-rotated-secret-value --name "$SECRET" | jq -r '.value.payload | fromjson | .token')
[ "$OLD" != "$NEW" ] && echo "✅ token rolls" || echo "❌ token did not change"

# 5. The previous token is revoked, not just stale
curl -sS -o /dev/null -w "old token: HTTP %{http_code}\n" \
  -H "Authorization: Bearer $OLD" https://api.github.com/installation/repositories
# expected: HTTP 401  (revoked)
```

All five passing means: the App is reachable, the JWT is signed correctly, the install scope matches expectations, payload state survives across rotations, and the create-before-revoke contract is intact.

---

## Troubleshooting

### Mint fails with `HTTP 401: A JSON web token could not be decoded`

The JWT GitHub received is malformed. Common causes:

- The PEM was edited, line-wrapped, or saved with CRLF line endings. Re-download from the App settings page and JSON-escape it via `python3 -c 'import json,sys;print(json.dumps(open(sys.argv[1]).read()))' <pem>`.
- The PEM in the payload escaped its newlines as literal `\n` in the JSON string but as actual newlines in the underlying value (or vice versa). Print and inspect: `jq -r '.value.payload | fromjson | .private_key' | head -1` should print exactly `-----BEGIN RSA PRIVATE KEY-----`.

### Mint fails with `HTTP 401: 'Expiration time' claim ('exp') is too far in the future`

The JWT's `exp` is more than 10 minutes from now. The rotator uses 9 minutes; if you signed a JWT manually for a smoke test, drop the window.

### Mint fails with `HTTP 401: 'Issued at' claim ('iat') is in the future`

Clock skew between the host signing the JWT and GitHub's servers. The rotator already backdates `iat` by 30 seconds; if you still see this from the rotator pod, run `chronyc tracking` or equivalent on the node and fix NTP.

### Mint fails with `HTTP 404: Not Found`

The installation ID does not match the App, or the App was uninstalled. Confirm:

```bash
curl -sS -H "Authorization: Bearer $JWT" \
        -H "Accept: application/vnd.github+json" \
        https://api.github.com/app/installations \
  | jq '.[] | {id, account: .account.login}'
```

If your `installation_id` is missing from the list, the App is not installed under that account. Reinstall in 2a and capture the new ID.

### Mint succeeds but token returns `HTTP 401` immediately

The minted token revoked itself: the most common cause is the rotator (or someone) calling `DELETE /installation/token` against it. Check rotator logs. The `github_app_token` rotator only issues that DELETE for the *previous* token in the payload, so this typically only happens if you passed a token into payload `token` field and triggered a rotation.

### Mint returns `HTTP 422: permissions field references unknown permissions`

The payload's `permissions` map names a permission the App was not granted at install time. Either:

- Drop the field (mint at the App's full installed scope), or
- Update the App in *Settings > Repository permissions*, then **re-install** so the new permissions take effect (existing installations carry their old permission set until re-install).

### Mint returns `HTTP 422: Unprocessable Entity` with `repositories must be an array of strings`

The `repositories` field has the wrong type or contains repository objects. Use bare names (`["my-repo"]`), not `owner/name` and not full repo objects.

### Akeyless: `creds rotation failed: unexpected response code 404: 404 page not found`

The Web Target URL is wrong. The gateway calls it verbatim, and the rotator only mounts handlers at `/sync/{create,revoke,rotate}` and `/health`. Confirm the URL is `<base>/sync/rotate`:

```bash
~/code/akeyless/akeyless --token=admin get-target-details --name "/Targets/custom-producer" \
  | jq '.value.web_target_details.url'
```

Fix with `target update web --name ... --url <correct-url>`.

### Akeyless: `creds rotation failed: unexpected response code 401: {"error":"missing AkeylessCreds header"}`

The gateway is making the call without an `AkeylessCreds` JWT, OR the rotator pod has `SKIP_AUTH=true` removed and the gateway version pre-dates `AkeylessCreds`. Check:

```bash
kubectl -n rotator describe deployment/custom-producer | grep -A1 -E "SKIP_AUTH|AKEYLESS_ACCESS_ID"
```

Make sure `SKIP_AUTH` is not set (production), and `AKEYLESS_ACCESS_ID` matches the gateway access ID (4b).

### Akeyless: `creds rotation failed: rotate request failed: ... no such host`

DNS resolution from the gateway to the Web Target URL is failing. Either the URL was set when the rotator was in a different namespace, or the gateway and rotator are in different clusters with no DNS bridge.

```bash
# From inside the gateway pod
kubectl -n <gw-ns> exec deploy/<gw-deploy> -- \
  curl -sS -o /dev/null -w "%{http_code}\n" \
  http://custom-producer.rotator.svc.cluster.local:8080/health
# expected: 200
```

If that fails, the gateway cannot reach the rotator at all. Use a Service exposed via NodePort/LoadBalancer/Ingress reachable from the gateway's network and update the Web Target URL.

### Akeyless: `creds rotation failed: unexpected response code 500: {"error":"mint installation token (HTTP ...): ..."}`

The rotator successfully reached GitHub and bubbled GitHub's error back. Read the parenthesised HTTP code and the body. Match against the GitHub-side troubleshooting entries above (404 = bad installation ID; 401 = bad JWT or revoked App; 422 = unsupported permissions or bad shape).

### Akeyless: `parse private key (tried PKCS#1 and PKCS#8): asn1: ...`

The `private_key` field is not a valid PEM RSA key. Common causes:

- File got truncated by `cat | head` somewhere.
- Newlines were stripped by an env-var or YAML pipeline that did not preserve them.
- A non-RSA key (Ed25519) was generated on a private GitHub Enterprise Server. The default GitHub Cloud signing key is RSA; if you're on GHES with non-RSA keys, the rotator will need a code change.

Verify the field starts and ends correctly:

```bash
~/code/akeyless/akeyless --token=admin get-rotated-secret-value --name "$SECRET" \
  | jq -r '.value.payload | fromjson | .private_key' \
  | head -1
# expected: -----BEGIN RSA PRIVATE KEY-----   (or -----BEGIN PRIVATE KEY-----)
```

### Akeyless: rotation works manually but silently fails on schedule

Same shape as the ADO runbook entry. Likely causes:

- Akeyless gateway pod restarted and is still warming caches; trigger one manual rotation post-restart.
- Gateway memory pressure; check `kubectl top pod`.
- Rotated Secret's `timeout-sec` is too tight; bump to 30s+. JWT signing + GitHub round-trip + revoke can take 5-10s under load.

---

## Decommissioning

> **👤 Performed by: Role A (GitHub) + Role C (Akeyless / k8s)**
> **Required permissions:**
> - Role A needs the same account/org rights as Step 1 to suspend/uninstall/delete the App.
> - Role C needs Akeyless write access on `/Rotated/*` and `/Targets/*`, plus k8s write on the rotator namespace.

To fully disable the automation's ability to mint installation tokens:

**Step D1: scale down the rotator OR stop using this rotated secret *(Role C)***

If this is the only rotated secret on this rotator deployment, you can scale the rotator deployment to zero. Otherwise just delete the rotated secret in D6 and leave the rotator running for other tenants.

```bash
kubectl -n rotator scale deployment/custom-producer --replicas=0
# or, when other rotated secrets share this deployment, skip and proceed to D6
```

**Step D2: delete the Akeyless Rotated Secret *(Role C)***

```bash
~/code/akeyless/akeyless delete-item --token=admin \
  --name "/Rotated/github-app-token-<short-handle>"
# expected: Item /Rotated/... was successfully deleted
```

This drops the stored payload, including the App's private key.

**Step D3: delete the Akeyless Web Target *(Role C, only if no other rotated secret uses it)***

```bash
~/code/akeyless/akeyless delete-target --token=admin \
  --name "/Targets/custom-producer"
```

Skip if other rotated secrets share the target.

**Step D4: revoke the App's signing keys *(Role A)***

In the App settings page (URL from 1a) under *Private keys*, click **Delete** on every active key. This immediately invalidates any JWT signed by them, so any in-flight rotation attempts on a stale payload will fail with HTTP 401.

**Step D5: uninstall the App *(Role A)***

Under *Settings > Installations* (user) or *Org settings > Installations* (org), find the App and click **Uninstall**. This invalidates any still-valid `ghs_...` tokens immediately and removes the App's repository access.

**Step D6: suspend or delete the App *(Role A)***

Two options on the App settings page:

- *Suspend*: pauses the App without losing its registration. Useful if you might re-enable it. Existing installations remain present but inert.
- *Delete*: removes the App entirely. Recovers the App name for reuse, breaks any documentation that references the App ID.

Once D5 and D6 complete, the credential triple in the (now-deleted) Akeyless secret is fully neutralized. Any backups of the payload are useless; the JWT path requires the App to exist *and* be installed.

**Verification *(Role C)***

```bash
~/code/akeyless/akeyless --token=admin describe-item \
  --name "/Rotated/github-app-token-<short-handle>"
# expected: Item not found
```

In GitHub, the App's *Advanced* page should show *Suspended* or 404 depending on D6.

---

## Reference values

All values below are GitHub-Cloud constants and safe to hardcode. Self-hosted GitHub Enterprise Server users should substitute their server hostname for `api.github.com` in the rotator's source.

| Name | Value |
|---|---|
| GitHub REST API base | `https://api.github.com` |
| GitHub App-as-app endpoint | `POST /app/installations/{installation_id}/access_tokens` |
| Token revoke endpoint | `DELETE /installation/token` |
| List installations (App-as-app) | `GET /app/installations` |
| Required JWT signing alg | `RS256` |
| JWT max `exp` window | 10 minutes (rotator uses 9) |
| JWT clock-skew safety margin | 30 seconds (rotator backdates `iat` by this much) |
| `X-GitHub-Api-Version` (this rotator) | `2022-11-28` |
| Installation token prefix | `ghs_` |
| Installation token length | 40 characters (as of 2026-04) |
| Installation token TTL | ~1 hour |

---

## Related

- GitHub REST API: [Authenticating as a GitHub App installation](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/authenticating-as-a-github-app-installation)
- GitHub REST API: [Apps endpoints](https://docs.github.com/en/rest/apps)
- GitHub REST API: [Personal access tokens (organization)](https://docs.github.com/en/rest/orgs/personal-access-tokens). The endpoint that does **not** create PATs.
- This repo: `go/rotator/internal/targets/github/` for the implementation.
- Sibling runbook: [`azuredevops-pat.md`](azuredevops-pat.md), same shape applied to the ADO PAT rotator.
