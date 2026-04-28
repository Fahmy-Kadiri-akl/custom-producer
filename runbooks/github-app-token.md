# GitHub App installation tokens — `github_app_token`

This runbook covers the complete setup for rotating GitHub App **installation access tokens** (`ghs_...`) through the unified custom-producer rotator. Each rotation mints a fresh ~1h-lived token using the App's private key.

> **Why an App, not a PAT?** GitHub does not expose any REST endpoint for creating fine-grained or classic PATs — those can only be created interactively in user settings. App installation tokens are the only short-lived credential primitive GitHub lets you mint programmatically.

---

## What you will end up with

- A GitHub App owned by your user or org, with a chosen permission set.
- The App installed on one or more repositories (or "all repos") under your user/org.
- An RSA private key (PEM) that the rotator uses to sign JWTs.
- An Akeyless `Rotated Secret` whose payload contains the App ID, installation ID, private key, and any scope overrides — and whose value, after rotation, is a fresh installation access token.

---

## Prerequisites

- The rotator is deployed and reachable per the main [README](../README.md) (`/sync/rotate` endpoint, valid `AKEYLESS_ACCESS_ID`, single Akeyless Web Target pointing at it).
- An Akeyless CLI session with permission to create rotated secrets.
- Access to your GitHub user or org's *Developer Settings → GitHub Apps* page.

---

## Step 1 — Create the GitHub App

App creation is a one-time interactive step on `github.com`.

### 1.1 — Open the new-App page

For a user-owned App (lives under your personal account):
- https://github.com/settings/apps/new

For an org-owned App (lives under an organization you administer):
- https://github.com/organizations/`<org>`/settings/apps/new

### 1.2 — Fill in the form

| Field | Value |
|------|-------|
| GitHub App name | Anything globally unique. Example: `akeyless-rotator-<your-handle>` |
| Homepage URL | Anything valid. Example: your GitHub profile URL |
| Webhook → Active | **Uncheck.** The rotator does not consume webhooks. |
| Webhook URL | Leave blank (greyed out once "Active" is unchecked) |
| Repository permissions | Select the permissions you want minted tokens to carry. Start minimal — you can broaden later. Common starting set: `Contents: Read-only`, `Metadata: Read-only`. |
| Organization permissions | Leave at *No access* unless you specifically need org-scoped access. |
| Account permissions | Leave at *No access* unless you specifically need user-scoped access. |
| Where can this GitHub App be installed? | *Only on this account* is the simplest for testing. |

Click **Create GitHub App**.

### 1.3 — Capture the App ID

On the App's settings page (after creation), copy the numeric **App ID** at the top — you will need this verbatim.

### 1.4 — Generate a private key

Scroll to *Private keys* → **Generate a private key**. GitHub downloads a `.pem` file. Treat it like a secret — anyone with this file can authenticate as your App.

---

## Step 2 — Install the App

### 2.1 — Install on the target account/org

On the App's settings page, click **Install App** in the left rail. Choose the user or org to install on. Choose either *All repositories* or *Only select repositories* and pick the repos the rotator should be able to scope tokens to.

### 2.2 — Capture the installation ID

After installation, GitHub redirects to a URL of the form:

```
https://github.com/settings/installations/<installation_id>
```

(Or `/organizations/<org>/settings/installations/<installation_id>` for org-owned Apps.)

The numeric `<installation_id>` is what you need. Alternatively, with a JWT signed by the App you can list installations via:

```bash
curl -H "Authorization: Bearer $APP_JWT" \
     -H "Accept: application/vnd.github+json" \
     https://api.github.com/app/installations
```

---

## Step 3 — Build the payload

The rotator's `github_app_token` payload looks like this:

```json
{
  "type": "github_app_token",
  "app_id": 1234567,
  "installation_id": 87654321,
  "private_key": "-----BEGIN RSA PRIVATE KEY-----\n...PEM body...\n-----END RSA PRIVATE KEY-----\n",
  "repositories": ["my-repo"],
  "permissions": {"contents": "read", "metadata": "read"}
}
```

| Field | Required | Notes |
|------|---------|-------|
| `type` | Yes | Must be `github_app_token`. |
| `app_id` | Yes | Numeric App ID from Step 1.3. |
| `installation_id` | Yes | Numeric installation ID from Step 2.2. |
| `private_key` | Yes | PEM-encoded RSA key (PKCS#1 or PKCS#8). JSON-escape every newline as `\n`. |
| `repositories` | No | Repository names to scope minted tokens to. Omit (or empty) to mint at the App's full installed scope. |
| `repository_ids` | No | Repository IDs — alternative to `repositories`. |
| `permissions` | No | Map of permission → access level. Must be a subset of the App's installed permissions. Omit to mint with the App's full installed permissions. |
| `token` | Managed | Set by rotator to the most recently-minted `ghs_...` token. |
| `expires_at` | Managed | Set by rotator to the token's expiry (RFC 3339). |

### Escaping the PEM for JSON

The `.pem` file has real newlines. To embed it in a JSON string, every newline becomes `\n`:

```bash
PRIVATE_KEY_JSON=$(python3 -c '
import json, sys
print(json.dumps(open(sys.argv[1]).read()), end="")
' /path/to/your-app.private-key.pem)
echo "$PRIVATE_KEY_JSON" | head -c 80; echo …
```

`$PRIVATE_KEY_JSON` now begins with `"` and is safe to drop straight into a JSON payload's `private_key` value.

---

## Step 4 — Create the rotated secret

```bash
APP_ID=1234567
INSTALLATION_ID=87654321
PRIVATE_KEY_JSON=$(python3 -c 'import json,sys;print(json.dumps(open(sys.argv[1]).read()),end="")' /path/to/your-app.private-key.pem)

akeyless create-rotated-secret \
  --name "/Rotated/github-app-token-<your-handle>" \
  --target-name "/Targets/custom-producer" \
  --rotator-type custom \
  --auto-rotate true \
  --rotation-interval 1 \
  --custom-payload "{
    \"type\": \"github_app_token\",
    \"app_id\": $APP_ID,
    \"installation_id\": $INSTALLATION_ID,
    \"private_key\": $PRIVATE_KEY_JSON,
    \"repositories\": [\"my-repo\"],
    \"permissions\": {\"contents\":\"read\",\"metadata\":\"read\"}
  }"
```

`--rotation-interval 1` is the Akeyless minimum (one day). For the highest-security setups, omit `--auto-rotate true` and trigger `rotate-secret` immediately before each consumer call instead — installation tokens last only ~1h regardless.

---

## Step 5 — First rotation and verification

```bash
akeyless rotate-secret --name "/Rotated/github-app-token-<your-handle>"
akeyless get-rotated-secret-value --name "/Rotated/github-app-token-<your-handle>" \
  | jq -r '.value | fromjson'
```

The payload now contains `token` (`ghs_...`) and `expires_at`. Verify the token works:

```bash
TOKEN=$(akeyless get-rotated-secret-value --name "/Rotated/github-app-token-<your-handle>" \
  | jq -r '.value | fromjson | .token')

curl -s -H "Authorization: Bearer $TOKEN" \
        -H "Accept: application/vnd.github+json" \
        https://api.github.com/installation/repositories | jq '.repositories[].full_name'
```

If the listed repositories match the ones you installed the App on (and `repositories` you scoped to in the payload), rotation is working end-to-end.

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| `creds rotation failed: unexpected response code 404: 404 page not found` | Web Target URL is wrong (missing `/sync/rotate`) or rotator unreachable from gateway | Update target URL to `<rotator-base-url>/sync/rotate`; verify connectivity from the gateway's network. |
| `mint installation token (HTTP 401): {"message":"A JSON web token could not be decoded"}` | `app_id` doesn't match the key, or the key wasn't generated for this App | Confirm `app_id` from the App settings page; regenerate the private key if unsure which `.pem` is current. |
| `mint installation token (HTTP 404): Not Found` | Installation ID belongs to a different App, or the App has been uninstalled | Re-list installations for the App and use the matching `installation_id`. |
| `private_key is not valid PEM` / `parse private key (tried PKCS#1 and PKCS#8)` | PEM newlines weren't escaped, or the key was wrapped/edited | Re-escape with the `python3 -c 'import json'` pattern in Step 3. |
| `mint installation token (HTTP 422): {"message":"...permissions...not permitted..."}` | The payload's `permissions` requests something the App was not granted at install time | Either remove the field (use the App's full installed permissions) or update the App's repository permissions in GitHub and reinstall. |

Akeyless gateway and rotator logs both bubble up the underlying GitHub error message verbatim, so check both:

```bash
kubectl -n rotator logs deployment/custom-producer --tail=50
kubectl -n <gateway-ns> logs deployment/<gateway-deployment> --since=2m | grep github-app-token
```

---

## Decommissioning

```bash
akeyless delete-item --name "/Rotated/github-app-token-<your-handle>"
```

To fully remove the App's ability to authenticate:

1. *App settings → Private keys* → delete the active private key (this immediately invalidates any in-flight JWTs).
2. *App settings → Advanced → Suspend* (temporary) or **Delete GitHub App** (permanent).
3. *Org/User settings → Installations* → uninstall.

Suspending the App immediately stops new tokens from minting; existing tokens auto-expire within ~1h and cannot be refreshed.
