# Custom Producer for Akeyless

A single container that rotates credentials across 19 target systems. One Docker image, one Akeyless Web Target, unlimited rotated secrets -- distinguished by the `type` field in the Akeyless payload.

## Table of Contents

- [Architecture](#architecture)
- [Supported Targets](#supported-targets)
- [Prerequisites](#prerequisites)
- [Container Image](#container-image)
- [Running Locally](#running-locally)
- [Deploying to Kubernetes](#deploying-to-kubernetes)
- [Configuring Akeyless](#configuring-akeyless)
- [End-to-End Walkthrough](#end-to-end-walkthrough)
- [Payload Reference](#payload-reference)
- [Adding a New Target](#adding-a-new-target)
- [Rotation Patterns](#rotation-patterns)
- [Troubleshooting](#troubleshooting)
- [Environment Variables](#environment-variables)
- [API Endpoints](#api-endpoints)

---

## Architecture

### The Problem

Akeyless provides native rotation for a handful of secret types (AWS IAM keys, database passwords, etc.), but many services that organizations rely on daily have no built-in rotator. Azure DevOps PATs, GitLab tokens, Grafana service account keys, Cloudflare API tokens -- these all need to be rotated, but each would traditionally require its own standalone webhook service. That means separate containers, separate deployments, separate monitoring, and separate maintenance for every service you want to cover.

### The Solution

This project solves that with a single container that handles all of them. Instead of deploying N rotators for N services, you deploy one. The secret itself carries all the context the rotator needs -- which service to talk to, what credentials to use for admin access, what scopes to apply -- encoded as a JSON payload inside the Akeyless rotated secret. The rotator reads a `type` field from that payload to decide which handler to invoke, performs the rotation against the target service's API, and returns the updated payload with the new credential back to Akeyless.

### How It Fits Into Akeyless

Akeyless custom producers work via webhooks. The Akeyless Gateway calls out to an HTTP endpoint whenever it needs to create, rotate, or revoke a secret. This project implements that webhook contract:

- `/sync/create` -- called when a new rotated secret is first created
- `/sync/rotate` -- called on each rotation interval (or manual trigger)
- `/sync/revoke` -- called when credentials need to be cleaned up

The Gateway authenticates itself to the rotator using the `AkeylessCreds` header, which the rotator validates against the Akeyless auth service (`auth.akeyless.io`). This ensures only your Gateway can trigger rotations.

### Design Principles

**One container, one Web Target, many secrets.** You create a single Akeyless Web Target pointing to the rotator's URL. Every rotated secret uses that same target -- they are distinguished entirely by the `type` field in their payload. Adding a new credential to rotate means creating a new rotated secret in Akeyless with the right payload JSON. No redeployment, no config changes.

**Configuration lives in the payload, not the environment.** Each rotated secret's payload contains everything needed to reach and authenticate with the target service (base URLs, admin tokens, scopes, user IDs). The rotator itself has no service-specific config -- it reads it all from the payload at rotation time. This means the same container image works for every environment without modification.

**Create-before-revoke by default.** Most targets create the new credential first, then revoke the old one. This ensures zero downtime -- there is always a valid credential available. The old credential revocation is best-effort; if it fails, the rotator logs a warning but still returns success because the new credential is already active.

```mermaid
%%{init: {'theme': 'base', 'themeVariables': {'primaryColor': '#4A90D9', 'primaryTextColor': '#fff', 'primaryBorderColor': '#2E6BA4', 'lineColor': '#5C6BC0', 'secondaryColor': '#81C784', 'tertiaryColor': '#FFB74D', 'noteTextColor': '#333', 'noteBkgColor': '#FFF9C4'}}}%%
flowchart TB
    subgraph GW["Akeyless Gateway"]
        direction TB
        SA["Rotated Secret A\ntype: pat"]
        SB["Rotated Secret B\ntype: gitlab_token"]
        SC["Rotated Secret C\ntype: grafana_token"]
    end

    GW -- "POST /sync/rotate\nPOST /sync/create\nPOST /sync/revoke" --> R

    subgraph R["Unified Rotator - single container"]
        direction TB
        P["1. Parse type field"]
        D["2. Dispatch to target handler"]
        U["3. Return updated payload"]
        P --> D --> U
    end

    R --> T1["Azure DevOps"]
    R --> T2["GitLab"]
    R --> T3["Grafana"]
    R --> T4["Cloudflare"]
    R --> T5["... 19 targets"]

    style GW fill:#4A90D9,stroke:#2E6BA4,color:#fff
    style R fill:#81C784,stroke:#4CAF50,color:#fff
    style T1 fill:#FFB74D,stroke:#FF9800,color:#333
    style T2 fill:#FFB74D,stroke:#FF9800,color:#333
    style T3 fill:#FFB74D,stroke:#FF9800,color:#333
    style T4 fill:#FFB74D,stroke:#FF9800,color:#333
    style T5 fill:#FFB74D,stroke:#FF9800,color:#333
```

### Rotation Flow (step by step)

1. An operator creates a **Web Target** in Akeyless pointing to the rotator's `/sync/rotate` URL (e.g., `http://custom-producer.rotator.svc.cluster.local:8080/sync/rotate` for an in-cluster rotator on the default port; see [Exposing to the Akeyless Gateway](#exposing-to-the-akeyless-gateway) for non-cluster topologies).
2. The operator creates a **Rotated Secret** using that Web Target, with a JSON payload containing a `type` field and all the service-specific configuration (admin credentials, token names, scopes, etc.).
3. When the rotation interval fires (or the operator triggers it manually), the Akeyless Gateway sends `POST /sync/rotate` to the rotator with the payload.
4. The rotator parses the `type` field (e.g., `"gitlab_token"`), looks up the matching handler in its internal registry, and calls it.
5. The handler authenticates to the target service using the admin credentials from the payload, creates a new token/key, and (best-effort) revokes the old one.
6. The handler returns the updated payload with the new credential values filled in.
7. Akeyless stores the updated payload as the new rotated secret value. Applications retrieve the current credential via `akeyless get-rotated-secret-value`.

### Internal Code Structure

The rotator uses a registry pattern internally. Each target implements a `Target` interface with `Create`, `Revoke`, and `Rotate` methods. At startup, `main.go` registers all 19 targets. The HTTP handler parses incoming requests, extracts the `type` field, and dispatches to the matching target. This makes adding new targets straightforward -- implement the interface, register it, rebuild.

---

## Supported Targets

### Tested and Validated

| Service | Type | What It Rotates | Tested On |
|---------|------|-----------------|-----------|
| (test) | `echo` | Returns payload with `rotated_at` timestamp | Local |
| Azure DevOps | [`pat`](runbooks/azuredevops-pat.md) | Personal access tokens via PAT Lifecycle API | Azure VM + Akeyless Gateway |
| GitHub | [`github_app_token`](runbooks/github-app-token.md) | App installation access tokens via `/app/installations/{id}/access_tokens` | github.com (Fahmy-Kadiri-akl App) |
| ArgoCD | `argocd_token` | Account tokens via ArgoCD API | Self-hosted ArgoCD |
| GitLab | `gitlab_token` | Personal access tokens via Admin API | Self-hosted GitLab |
| Grafana | `grafana_token` | Service account tokens via Grafana API | Grafana Cloud |
| Cloudflare | `cloudflare_token` | API tokens via Cloudflare v4 API | Cloudflare (user-scoped) |

### Built, Not Yet Tested

These targets are implemented and compile but have not been validated against a live instance. They require free-tier account signups to test.

| Service | Type | What It Rotates | Signup URL |
|---------|------|-----------------|------------|
| Ansible AWX/AAP | `password` | User passwords | Requires AWX/AAP instance |
| Ansible AWX/AAP | `api_key` | Personal access tokens | Requires AWX/AAP instance |
| JFrog Artifactory | `jfrog_token` | Access tokens | jfrog.com/start-free |
| Datadog | `datadog_key` | API keys and application keys | datadoghq.com/free-datadog-trial |
| Terraform Cloud | `tfc_token` | Team/org API tokens | app.terraform.io/signup |
| Confluent Cloud | `confluent_key` | API keys | confluent.io/get-started |
| PagerDuty | `pagerduty_key` | REST API keys | pagerduty.com/sign-up |
| ServiceNow | `servicenow_cred` | OAuth client secrets | developer.servicenow.com |
| Slack | `slack_token` | Bot/user tokens (token rotation API) | api.slack.com/apps |
| SendGrid | `sendgrid_key` | API keys | signup.sendgrid.com |
| Okta | `okta_key` | SSWS API tokens | developer.okta.com/signup |
| New Relic | `newrelic_key` | User and ingest API keys | newrelic.com/signup |
| Aerospike | `aerospike_password` | User passwords (admin wire protocol) | Requires Aerospike EE (security enabled); CE returns a clear SECURITY_NOT_ENABLED error |

### Runbooks

Per-rotator runbooks live under [`runbooks/`](runbooks/). Each covers the complete end-to-end setup for that target: external identity/app registration, any interactive bootstrap, Akeyless Target and Rotated Secret creation, verification, troubleshooting, and decommissioning.

| Rotator | Runbook |
|---|---|
| `pat` (Azure DevOps) | [runbooks/azuredevops-pat.md](runbooks/azuredevops-pat.md) |
| `github_app_token` | [runbooks/github-app-token.md](runbooks/github-app-token.md) |

More runbooks will be added as each rotator is validated in production.

---

## Prerequisites

- **Go 1.25+** (for local development only)
- **Akeyless Gateway** v4.x+ (any deployment: Docker, Kubernetes)
- **Network access** from the rotator to:
  - The Akeyless auth service (`https://auth.akeyless.io`)
  - Each target system's API (e.g., `https://dev.azure.com`, `https://api.cloudflare.com`)

---

## Container Image

The container image is built and pushed automatically by GitHub Actions on every push to `master`.

```
ghcr.io/fahmy-kadiri-akl/custom-producer/rotator:latest
```

Tags available:
- `latest` -- always points to the latest `master` build
- `<short-sha>` -- pinned to a specific commit (e.g., `ghcr.io/fahmy-kadiri-akl/custom-producer/rotator:ee392f4`)
- `<version>` -- semver tags when a release is created (e.g., `v1.0.0` produces `1.0.0` and `1.0`)

Pull it directly for deployment:

```bash
docker pull ghcr.io/fahmy-kadiri-akl/custom-producer/rotator:latest
```

Dependencies are updated automatically via a monthly GitHub Actions workflow and Dependabot.

---

## Running Locally

### Start the rotator

```bash
cd go
SKIP_AUTH=true PORT=9999 go run ./rotator/bin/cmd
```

`SKIP_AUTH=true` disables Akeyless JWT validation so you can test without a gateway.

### Verify with the echo target

```bash
# Health check
curl -s http://localhost:9999/health
# {"status":"healthy"}

# Test rotation
curl -s -X POST http://localhost:9999/sync/rotate \
  -H 'Content-Type: application/json' \
  -d '{
    "payload": "{\"type\":\"echo\",\"message\":\"hello world\"}"
  }' | jq .
# {
#   "payload": "{\"message\":\"hello world\",\"rotated_at\":\"2025-01-15T10:30:00Z\",\"type\":\"echo\"}"
# }

# Test create
curl -s -X POST http://localhost:9999/sync/create \
  -H 'Content-Type: application/json' \
  -d '{
    "payload": "{\"type\":\"echo\",\"key\":\"value\"}",
    "client_info": {"access_id":"p-test","sub_claims":{}}
  }' | jq .
# {
#   "id": "echo-1705312200000000000",
#   "response": "{\"type\":\"echo\",\"key\":\"value\"}"
# }

# Test revoke
curl -s -X POST http://localhost:9999/sync/revoke \
  -H 'Content-Type: application/json' \
  -d '{
    "payload": "{\"type\":\"echo\"}",
    "ids": ["echo-123"]
  }' | jq .
# {
#   "revoked": ["echo-123"],
#   "message": "echo revoke acknowledged"
# }
```

### Run with Docker locally

```bash
# Default port (8080)
docker run -p 8080:8080 -e SKIP_AUTH=true ghcr.io/fahmy-kadiri-akl/custom-producer/rotator:latest

# Custom port
docker run -p 9090:9090 -e PORT=9090 -e SKIP_AUTH=true ghcr.io/fahmy-kadiri-akl/custom-producer/rotator:latest
```

---

## Deploying to Kubernetes

### Deployment manifest

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: custom-producer
  namespace: rotator
  labels:
    app: custom-producer
spec:
  replicas: 1
  selector:
    matchLabels:
      app: custom-producer
  template:
    metadata:
      labels:
        app: custom-producer
    spec:
      containers:
      - name: rotator
        image: ghcr.io/fahmy-kadiri-akl/custom-producer/rotator:latest
        ports:
        - containerPort: 8080          # Must match PORT env var
          name: http
        env:
        # REQUIRED. The Akeyless Gateway's access_id. Without this set
        # correctly, every gateway call is rejected with "invalid credentials".
        # Find it on the gateway you intend to use:
        #   kubectl -n <gateway-ns> get secret akeyless-gateway-conf-secret \
        #     -o jsonpath='{.data.gateway-access-id}' | base64 -d
        # Or pull it from the gateway's UI at Settings > Cluster Settings.
        - name: AKEYLESS_ACCESS_ID
          value: "p-REPLACE-ME"
        # Optional: override the default listen port (8080).
        # If you change this, also update the containerPort, probes, and
        # the Service's port/targetPort below.
        # - name: PORT
        #   value: "9090"
        # Optional: restrict to a specific rotated secret name. When set,
        # only that rotated secret may invoke this rotator.
        # - name: AKEYLESS_ITEM_NAME
        #   value: "/Rotated/my-secret"
        # DEV ONLY: bypass Akeyless JWT validation. Never set in production.
        # - name: SKIP_AUTH
        #   value: "true"
        resources:
          requests:
            cpu: 50m
            memory: 32Mi
          limits:
            cpu: 200m
            memory: 128Mi
        livenessProbe:
          httpGet:
            path: /health
            port: 8080                 # Must match PORT env var
          initialDelaySeconds: 5
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /health
            port: 8080                 # Must match PORT env var
          initialDelaySeconds: 3
          periodSeconds: 10
---
apiVersion: v1
kind: Service
metadata:
  name: custom-producer
  namespace: rotator
spec:
  selector:
    app: custom-producer
  ports:
  - port: 8080                         # Must match PORT env var
    targetPort: 8080                   # Must match PORT env var
    name: http
  type: ClusterIP
```

Apply it:

```bash
kubectl create namespace rotator
kubectl apply -f deployment.yaml
```

Verify the pod is running:

```bash
kubectl -n rotator get pods
kubectl -n rotator logs deployment/custom-producer
```

Test from inside the cluster:

```bash
kubectl -n rotator run curl --rm -i --restart=Never --image=curlimages/curl -- \
  curl -s http://custom-producer.rotator.svc.cluster.local:8080/health
# {"status":"healthy"}
```

### Exposing to the Akeyless Gateway

The Akeyless Gateway must be able to reach the rotator over HTTP(S). The URL you give to the Web Target depends entirely on **where your gateway runs relative to the rotator**. Pick the scenario that matches your topology — this is the URL you will use for `<rotator-base-url>` in every later example.

The Web Target URL is always `<rotator-base-url>/sync/rotate`.

**Scenario A: Gateway and rotator in the same Kubernetes cluster (any namespaces)**

Use the rotator's in-cluster service DNS name. Cross-namespace DNS works out of the box.

```
<rotator-base-url> = http://custom-producer.rotator.svc.cluster.local:8080
Web Target URL    = http://custom-producer.rotator.svc.cluster.local:8080/sync/rotate
```

**Scenario B: Gateway runs outside the cluster (different cluster, VM, Akeyless SaaS gateway, Docker on another host)**

The rotator must be reachable from where the gateway runs. Expose the rotator's `Service` with Ingress, LoadBalancer, or NodePort and use that publicly-reachable URL — TLS is strongly recommended.

NodePort example:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: custom-producer-external
  namespace: rotator
spec:
  selector:
    app: custom-producer
  ports:
  - port: 8080
    targetPort: 8080
    nodePort: 30080  # any free port in 30000-32767
  type: NodePort
```

```
<rotator-base-url> = http://<node-or-lb-ip>:30080
Web Target URL    = http://<node-or-lb-ip>:30080/sync/rotate
```

For an Ingress with TLS:

```
<rotator-base-url> = https://rotator.example.com
Web Target URL    = https://rotator.example.com/sync/rotate
```

**Scenario C: Rotator running as a standalone Docker container (no Kubernetes)**

Run the container with a port published to the host (or to a routable network), and use that host:port in the URL.

```bash
docker run -d --name custom-producer \
  -p 8080:8080 \
  -e AKEYLESS_ACCESS_ID=p-XXXXXXXXXXXXXXXX \
  ghcr.io/fahmy-kadiri-akl/custom-producer/rotator:latest
```

```
<rotator-base-url> = http://<docker-host>:8080
Web Target URL    = http://<docker-host>:8080/sync/rotate
```

The gateway must be able to resolve and reach `<docker-host>` over the network. If both the gateway and the rotator run in the same Docker network, use the container name as the host (e.g., `http://custom-producer:8080`).

> **Replace `<rotator-base-url>` everywhere below.** The remaining examples assume an in-cluster service URL for brevity, but every command works for any topology — just substitute your `<rotator-base-url>`.

---

## Configuring Akeyless

This section walks through creating the Web Target and Rotated Secret in Akeyless. You do this once per target type.

### Step 1: Create a Web Target

The Akeyless Gateway calls the URL on the Web Target verbatim, so the URL **must point at a specific endpoint** on the rotator. For rotated secrets, that endpoint is `/sync/rotate`.

In the Akeyless Console:

1. Navigate to **Targets > New > Web Target**
2. Configure:
   - **Name:** `/Targets/custom-producer` (or any path you prefer)
   - **URL:** `http://custom-producer.rotator.svc.cluster.local:8080/sync/rotate`
   - Leave all other fields as defaults

Using the CLI:

```bash
akeyless target create web \
  --name "/Targets/custom-producer" \
  --url "http://custom-producer.rotator.svc.cluster.local:8080/sync/rotate"
```

> **Note on `<port>`:** examples in this README use `8080`, the rotator's default `PORT`. If you override `PORT` in the deployment, use that value instead. The rotator must listen on a single port; the URL must reflect it.

> **One Web Target, many rotated secrets.** A single Web Target works for every rotated secret you create with this rotator. The rotator inspects each request's `type` field to pick the correct handler — the Web Target itself is just the rotation endpoint. Create/revoke webhooks (rare for custom rotated secrets) require their own Web Targets pointing at `/sync/create` and `/sync/revoke` respectively.

### Step 2: Create a Rotated Secret

In the Akeyless Console:

1. Navigate to **Rotated Secrets > New > Custom**
2. Configure:
   - **Name:** `/Rotated/azure-devops-pat` (descriptive name for this credential)
   - **Target:** Select the Web Target created in Step 1
   - **Rotation interval:** `30` days (or your preferred interval)
   - **Payload:** Paste the JSON payload for your target type (see [Payload Reference](#payload-reference))

Using the CLI:

```bash
akeyless create-rotated-secret \
  --name "/Rotated/azure-devops-pat" \
  --target-name "/Targets/custom-producer" \
  --rotator-type "custom" \
  --auto-rotate true \
  --rotation-interval 30 \
  --rotation-hour 3 \
  --custom-payload '{
    "type": "pat",
    "organization": "my-org",
    "display_name": "akeyless-managed-pat",
    "scope": "app_token",
    "valid_days": 30,
    "tenant_id": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
    "client_id": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
    "username": "svc-account@example.com",
    "password": "service-account-password"
  }'
```

### Step 3: Trigger a manual rotation (optional)

```bash
akeyless rotate-secret --name "/Rotated/azure-devops-pat"
```

### Step 4: Retrieve the rotated credential

```bash
akeyless get-rotated-secret-value --name "/Rotated/azure-devops-pat"
```

The response is the full payload JSON. Parse the credential field for your target type (e.g., `token` for PATs, `key` for API keys).

### Step 5: Set up rotation notifications (optional)

```bash
# Create an email notification forwarder
akeyless create-event-forwarder \
  --name "rotation-alerts" \
  --event-type "rotation" \
  --email "team@example.com" \
  --runner-type "gateway"

# Attach it to the rotated secret
akeyless update-rotated-secret \
  --name "/Rotated/azure-devops-pat" \
  --add-notification "rotation-alerts" \
  --notification-days-before-expiration 7 \
  --notification-days-before-expiration 1
```

---

## End-to-End Walkthrough

This walkthrough creates a GitLab PAT rotation from scratch.

### 1. Deploy the rotator

```bash
kubectl create namespace rotator
kubectl -n rotator apply -f deployment.yaml
```

The deployment manifest references the GHCR image directly. See [Deploying to Kubernetes](#deploying-to-kubernetes) for the full manifest.

### 2. Verify the rotator is healthy

`/health` is the only endpoint that does not require Akeyless auth — useful for k8s probes and connectivity checks.

```bash
kubectl -n rotator run curl --rm -i --restart=Never --image=curlimages/curl -- \
  curl -s http://custom-producer.rotator.svc.cluster.local:8080/health
# {"status":"healthy"}
```

### 3. (Optional) Smoke-test the rotation handler locally

`/sync/rotate` requires a valid `AkeylessCreds` JWT in production. To smoke-test against the echo target without a gateway, temporarily enable `SKIP_AUTH=true` on the deployment:

```bash
# Enable bypass
kubectl -n rotator set env deployment/custom-producer SKIP_AUTH=true
kubectl -n rotator rollout status deployment/custom-producer

# Smoke test (echo)
kubectl -n rotator run curl --rm -i --restart=Never --image=curlimages/curl -- \
  curl -s -X POST http://custom-producer.rotator.svc.cluster.local:8080/sync/rotate \
    -H 'Content-Type: application/json' \
    -d '{"payload":"{\"type\":\"echo\",\"hello\":\"world\"}"}'

# IMPORTANT: turn auth back on before you create any real rotated secret
kubectl -n rotator set env deployment/custom-producer SKIP_AUTH-
kubectl -n rotator rollout status deployment/custom-producer
```

### 4. Create the Akeyless resources

```bash
# Web Target (skip if already created)
akeyless target create web \
  --name "/Targets/custom-producer" \
  --url "http://custom-producer.rotator.svc.cluster.local:8080/sync/rotate"

# Rotated Secret (example for github_app_token)
akeyless create-rotated-secret \
  --name "/Rotated/github-app-token" \
  --target-name "/Targets/custom-producer" \
  --rotator-type "custom" \
  --auto-rotate true \
  --rotation-interval 1 \
  --custom-payload '{
    "type": "github_app_token",
    "app_id": 1234567,
    "installation_id": 87654321,
    "private_key": "-----BEGIN RSA PRIVATE KEY-----\n...PEM with literal \\n line breaks...\n-----END RSA PRIVATE KEY-----\n",
    "repositories": ["my-repo"],
    "permissions": {"contents": "read", "metadata": "read"}
  }'
```

GitHub installation tokens auto-expire after ~1 hour — set `--rotation-interval 1` (one day) at the longest, or shorter for high-security setups.

### 5. Trigger rotation and verify

```bash
akeyless rotate-secret --name "/Rotated/github-app-token"
akeyless get-rotated-secret-value --name "/Rotated/github-app-token"
```

The payload now contains a fresh `token` (`ghs_...`) and `expires_at`. On every rotation the rotator mints a new installation token and best-effort revokes the previous one.

---

## Payload Reference

Every payload must include a `type` field. All other fields are target-specific. Fields labeled "managed by rotator" are updated automatically during rotation -- set them to empty/zero on first use.

### echo

Test/validation target. Returns the payload with a `rotated_at` timestamp.

```json
{
  "type": "echo",
  "any_field": "any_value"
}
```

---

### pat (Azure DevOps)

Rotates Azure DevOps personal access tokens via the PAT Lifecycle Management API.

Microsoft restricts PAT minting to delegated user tokens only. Service principals, managed identities, and workload identity federation are rejected by the PATs API. The rotator supports three auth modes in order of preference:

- **`refresh_token`** (recommended). OAuth 2.0 `offline_access` flow. One-time interactive sign-in produces a refresh token with up to 90 days of life; each rotation rolls it. Works with MFA and Conditional Access.
- **`username` / `password`** (ROPC). For non-interactive service accounts without MFA.
- **`bearer_token`** (testing only). Pre-obtained 1-hour access token. Not suitable for unattended rotation.

Auth precedence: if `refresh_token` is set it is used; otherwise `bearer_token`; otherwise `username`/`password`.

**Full setup:** see [runbooks/azuredevops-pat.md](runbooks/azuredevops-pat.md). Covers the Entra app registration, the device-code bootstrap helper, Akeyless Target + Rotated Secret creation, verification, troubleshooting, and decommissioning.

Example minimal payload for refresh-token mode:

```json
{
  "type": "pat",
  "organization": "<your-ado-org>",
  "valid_days": 30,
  "tenant_id": "<your-tenant-id>",
  "client_id": "<your-app-client-id>",
  "refresh_token": "<rt-from-bootstrap>"
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `organization` | Yes | -- | Azure DevOps organization name |
| `display_name` | No | `akeyless-rotated-pat` | PAT display name |
| `scope` | No | `app_token` | PAT scope string |
| `valid_days` | No | `30` | PAT validity in days |
| `all_orgs` | No | `false` | Apply to all accessible organizations |
| `tenant_id` | refresh_token / ROPC | -- | Entra tenant ID |
| `client_id` | refresh_token / ROPC | -- | Entra app registration client ID |
| `refresh_token` | refresh_token | -- | Delegated refresh token; rolled on each rotation |
| `client_secret` | refresh_token (confidential clients) | -- | Client secret for confidential public clients only |
| `username` | ROPC | -- | Service account UPN |
| `password` | ROPC | -- | Service account password |
| `bearer_token` | bearer | -- | Pre-obtained Azure AD access token (short-lived) |
| `authorization_id` | Managed | -- | Current PAT authorization ID (set by rotator) |
| `token` | Managed | -- | Current PAT value (set by rotator) |

---

### argocd_token

Rotates ArgoCD account tokens via the ArgoCD API.

```json
{
  "type": "argocd_token",
  "base_url": "https://argocd.example.com",
  "admin_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "account": "ci-bot",
  "expiry_seconds": 2592000,
  "token_id": "",
  "token": ""
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `base_url` | Yes | -- | ArgoCD server URL |
| `admin_token` | Yes | -- | Admin JWT or bearer token (note: ArgoCD uses cookie-based auth internally, the client handles this) |
| `account` | Yes | -- | ArgoCD account name to generate a token for |
| `expiry_seconds` | No | `0` (no expiry) | Token TTL in seconds |
| `token_id` | Managed | -- | Current token ID (`iat`-based) |
| `token` | Managed | -- | Current JWT token |

---

### gitlab_token

Rotates GitLab personal access tokens via the Admin API.

```json
{
  "type": "gitlab_token",
  "base_url": "https://gitlab.example.com",
  "admin_token": "glpat-XXXXXXXXXXXXXXXXXXXX",
  "user_id": 2,
  "token_name": "akeyless-managed",
  "scopes": ["api", "read_repository"],
  "expiry_days": 30,
  "token_id": 0,
  "token": ""
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `base_url` | Yes | -- | GitLab instance URL (`https://gitlab.com` or self-hosted) |
| `admin_token` | Yes | -- | Admin PAT with `api` scope |
| `user_id` | Yes | -- | Numeric user ID to create the PAT for |
| `token_name` | Yes | -- | Display name for the token |
| `scopes` | No | `["api"]` | Token scopes |
| `expiry_days` | No | `30` | Token TTL in days |
| `token_id` | Managed | -- | Current PAT ID |
| `token` | Managed | -- | Current PAT value |

---

### grafana_token

Rotates Grafana service account tokens. Uses **revoke-before-create** because Grafana enforces unique token names within a service account.

```json
{
  "type": "grafana_token",
  "base_url": "https://myorg.grafana.net",
  "admin_token": "glsa_XXXXXXXXXXXXXXXXXXXX",
  "service_account_id": 15,
  "token_name": "akeyless-managed",
  "expiry_seconds": 2592000,
  "token_id": 0,
  "token": ""
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `base_url` | Yes | -- | Grafana instance URL (cloud or self-hosted) |
| `admin_token` | Yes | -- | Admin API key or service account token |
| `service_account_id` | Yes | -- | Numeric ID of the service account |
| `token_name` | Yes | -- | Base name for the token (a timestamp suffix is appended for uniqueness) |
| `expiry_seconds` | No | `0` (no expiry) | Token TTL in seconds |
| `token_id` | Managed | -- | Current token ID |
| `token` | Managed | -- | Current token value |

---

### cloudflare_token

Rotates Cloudflare API tokens. Requires a **user-scoped** admin token (not account-scoped).

```json
{
  "type": "cloudflare_token",
  "admin_token": "existing-api-token-with-token-edit-permission",
  "token_name": "akeyless-managed-worker-token",
  "policies": [
    {
      "effect": "allow",
      "permission_groups": [
        {"id": "abcdef1234567890abcdef1234567890"}
      ],
      "resources": {
        "com.cloudflare.api.user.abcdef1234567890abcdef1234567890": "*"
      }
    }
  ],
  "condition": {
    "request.ip": {
      "in": ["192.168.1.0/24"]
    }
  },
  "token_id": "",
  "token": ""
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `admin_token` | Yes | -- | API token with `User API Tokens:Edit` permission |
| `token_name` | Yes | -- | Display name for the token |
| `policies` | Yes | -- | Array of permission policies (permission groups + resources) |
| `condition` | No | -- | IP filtering conditions |
| `token_id` | Managed | -- | Current token ID |
| `token` | Managed | -- | Current token value |

---

### password (Ansible AWX/AAP)

Rotates an Ansible AWX/AAP user's password. Generates a random 24-character password.

```json
{
  "type": "password",
  "ansible_url": "https://awx.example.com",
  "admin_user": "admin",
  "admin_password": "admin-password",
  "target_username": "svc-deploy",
  "target_user_id": 0,
  "password": "",
  "skip_tls_verify": false
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `ansible_url` | Yes | -- | AWX/AAP controller URL |
| `admin_user` | Yes | -- | Admin username for API authentication |
| `admin_password` | Yes | -- | Admin password for API authentication |
| `target_username` | Yes | -- | Username whose password to rotate |
| `target_user_id` | No | `0` (auto-lookup) | Numeric user ID (looked up from username if 0) |
| `password` | Managed | -- | Current password |
| `skip_tls_verify` | No | `false` | Skip TLS certificate verification |

---

### api_key (Ansible AWX/AAP)

Rotates an Ansible AWX/AAP personal access token using create-before-revoke.

```json
{
  "type": "api_key",
  "ansible_url": "https://awx.example.com",
  "admin_user": "admin",
  "admin_password": "admin-password",
  "target_user_id": 5,
  "token_id": 0,
  "token": "",
  "token_scope": "write",
  "description": "ci-deploy-token",
  "skip_tls_verify": false
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `ansible_url` | Yes | -- | AWX/AAP controller URL |
| `admin_user` | Yes | -- | Admin username |
| `admin_password` | Yes | -- | Admin password |
| `target_user_id` | Yes | -- | Numeric user ID to create a token for |
| `token_scope` | No | `write` | `write` or `read` |
| `description` | No | -- | Token description |
| `token_id` | Managed | -- | Current token ID |
| `token` | Managed | -- | Current token value |
| `skip_tls_verify` | No | `false` | Skip TLS certificate verification |

---

### github_app_token

Mints GitHub App **installation access tokens** (the `ghs_...` short-lived tokens, ~1h lifetime). Each rotation generates a fresh JWT signed with the App's private key and exchanges it via `POST /app/installations/{installation_id}/access_tokens`.

> GitHub does not expose any REST API for creating fine-grained or classic PATs — those can only be created interactively in the user's settings. Installation access tokens are the supported short-lived primitive and are what this rotator targets.

Full setup (creating the App, obtaining the App ID, installation ID, and private key, scoping permissions): see [runbooks/github-app-token.md](runbooks/github-app-token.md).

```json
{
  "type": "github_app_token",
  "app_id": 1234567,
  "installation_id": 87654321,
  "private_key": "-----BEGIN RSA PRIVATE KEY-----\n...PEM body with literal \\n line breaks...\n-----END RSA PRIVATE KEY-----\n",
  "repositories": ["my-repo"],
  "permissions": {
    "contents": "read",
    "metadata": "read"
  }
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `app_id` | Yes | -- | Numeric GitHub App ID (from the App's settings page) |
| `installation_id` | Yes | -- | Numeric installation ID for the user/org where the App is installed |
| `private_key` | Yes | -- | PEM-encoded RSA private key (PKCS#1 or PKCS#8). JSON-escape newlines as `\n` |
| `repositories` | No | (all installed repos) | Repository names to scope the token to |
| `repository_ids` | No | (all installed repos) | Repository IDs to scope the token to |
| `permissions` | No | (App's full installed perms) | Subset of the App's permissions to grant the minted token |
| `token` | Managed | -- | The minted `ghs_...` token (set by rotator) |
| `expires_at` | Managed | -- | Token expiry timestamp (set by rotator) |

Tokens auto-expire in ~1 hour, so set `--rotation-interval 1` (one day, the Akeyless minimum) at the longest. For high-security use cases, retrieve a fresh token on every consumer call by triggering rotation just-in-time.

---

### jfrog_token

Rotates JFrog Artifactory access tokens.

```json
{
  "type": "jfrog_token",
  "base_url": "https://mycompany.jfrog.io",
  "admin_token": "existing-admin-access-token",
  "username": "svc-ci",
  "scope": "applied-permissions/user",
  "expires_in_secs": 2592000,
  "description": "akeyless-managed",
  "token_id": "",
  "token": ""
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `base_url` | Yes | -- | Artifactory instance URL |
| `admin_token` | Yes | -- | Admin access token with token management permissions |
| `username` | Yes | -- | Subject user for the token |
| `scope` | No | `applied-permissions/user` | Token scope |
| `expires_in_secs` | No | `3600` | Token TTL in seconds |
| `description` | No | -- | Token description |
| `token_id` | Managed | -- | Current token ID |
| `token` | Managed | -- | Current access token value |

---

### datadog_key

Rotates Datadog API keys or application keys.

```json
{
  "type": "datadog_key",
  "site": "datadoghq.com",
  "key_type": "api",
  "admin_api": "existing-api-key",
  "admin_app": "existing-application-key",
  "key_name": "akeyless-managed",
  "key_id": "",
  "key": ""
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `site` | Yes | -- | Datadog site (`datadoghq.com`, `us5.datadoghq.com`, `datadoghq.eu`, etc.) |
| `key_type` | No | `api` | `api` or `application` |
| `admin_api` | Yes | -- | Existing API key for authentication |
| `admin_app` | Yes | -- | Existing application key for authentication |
| `key_name` | Yes | -- | Display name for the key |
| `key_id` | Managed | -- | Current key ID |
| `key` | Managed | -- | Current key value |

---

### tfc_token

Rotates Terraform Cloud or Terraform Enterprise API tokens.

```json
{
  "type": "tfc_token",
  "base_url": "https://app.terraform.io",
  "admin_token": "existing-token-with-manage-permissions",
  "token_type": "team",
  "team_id": "team-XXXXXXXXXXXXXXXX",
  "org_name": "",
  "description": "akeyless-managed",
  "token_id": "",
  "token": ""
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `base_url` | No | `https://app.terraform.io` | TFC or TFE instance URL |
| `admin_token` | Yes | -- | Token with manage permissions |
| `token_type` | Yes | -- | `team`, `organization`, or `user` |
| `team_id` | If `team` | -- | Team ID (for team tokens) |
| `org_name` | If `organization` | -- | Organization name (for org tokens) |
| `description` | No | -- | Token description |
| `token_id` | Managed | -- | Current token ID |
| `token` | Managed | -- | Current token value |

Note: Team and organization tokens are singletons in Terraform Cloud. Creating a new one replaces the existing one automatically.

---

### confluent_key

Rotates Confluent Cloud API keys.

```json
{
  "type": "confluent_key",
  "cloud_api_key": "XXXXXXXXXXXXXXXX",
  "cloud_api_secret": "XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
  "owner": "sa-xxxxxx",
  "resource_id": "lkc-xxxxxx",
  "description": "akeyless-managed",
  "key_id": "",
  "key_secret": ""
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `cloud_api_key` | Yes | -- | Admin cloud API key |
| `cloud_api_secret` | Yes | -- | Admin cloud API secret |
| `owner` | Yes | -- | Owner resource ID (`sa-xxxxx` for service accounts, `u-xxxxx` for users) |
| `resource_id` | No | -- | Cluster or environment to scope the key to (e.g., `lkc-xxxxx`) |
| `description` | No | -- | Key description |
| `key_id` | Managed | -- | Current API key ID |
| `key_secret` | Managed | -- | Current API key secret |

---

### pagerduty_key

Rotates PagerDuty REST API keys.

```json
{
  "type": "pagerduty_key",
  "admin_token": "existing-full-access-api-key",
  "key_name": "akeyless-managed",
  "key_type": "full_access",
  "key_id": "",
  "key": ""
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `admin_token` | Yes | -- | Existing API key with full access |
| `key_name` | Yes | -- | Description for the key |
| `key_type` | No | `full_access` | `read_only` or `full_access` |
| `key_id` | Managed | -- | Current key ID |
| `key` | Managed | -- | Current key value |

---

### servicenow_cred

Rotates ServiceNow OAuth application client secrets.

```json
{
  "type": "servicenow_cred",
  "instance_url": "https://myinst.service-now.com",
  "admin_user": "admin",
  "admin_pass": "admin-password",
  "client_id": "oauth-app-client-id",
  "client_name": "My OAuth App",
  "client_secret": "",
  "app_sys_id": "sys-id-of-oauth-app-record"
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `instance_url` | Yes | -- | ServiceNow instance URL |
| `admin_user` | Yes | -- | Admin username |
| `admin_pass` | Yes | -- | Admin password |
| `client_id` | Yes | -- | OAuth application client ID |
| `client_name` | Yes | -- | OAuth application name |
| `app_sys_id` | Yes | -- | Sys ID of the OAuth application record |
| `client_secret` | Managed | -- | Current client secret (rotator generates a 32-character secret) |

---

### slack_token

Rotates Slack tokens using the [token rotation API](https://api.slack.com/authentication/rotation). Requires a Slack app with token rotation enabled.

```json
{
  "type": "slack_token",
  "client_id": "1234567890.1234567890",
  "client_secret": "abcdef1234567890abcdef1234567890",
  "refresh_token": "xoxe-1-...",
  "token": ""
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `client_id` | Yes | -- | Slack app client ID |
| `client_secret` | Yes | -- | Slack app client secret |
| `refresh_token` | Managed | -- | Current refresh token (updated each rotation) |
| `token` | Managed | -- | Current access token (`xoxe-...`) |

Note: Both `refresh_token` and `token` are updated on every rotation. The Slack token rotation API returns a new refresh token each time.

---

### sendgrid_key

Rotates SendGrid API keys.

```json
{
  "type": "sendgrid_key",
  "admin_key": "SG.XXXXXXXXXXXXXXXXXXXX.XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
  "key_name": "akeyless-managed",
  "scopes": ["mail.send", "alerts.read"],
  "key_id": "",
  "key": ""
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `admin_key` | Yes | -- | Existing API key with full access |
| `key_name` | Yes | -- | Display name for the key |
| `scopes` | No | -- | List of permission scopes (empty = full access) |
| `key_id` | Managed | -- | Current key ID |
| `key` | Managed | -- | Current API key value |

---

### okta_key

Rotates Okta API tokens (SSWS tokens).

```json
{
  "type": "okta_key",
  "org_url": "https://myorg.okta.com",
  "admin_token": "00XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
  "token_name": "akeyless-managed",
  "token_id": "",
  "token": ""
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `org_url` | Yes | -- | Okta organization URL |
| `admin_token` | Yes | -- | SSWS token with `okta.apiTokens.manage` scope |
| `token_name` | Yes | -- | Display name for the token |
| `token_id` | Managed | -- | Current token ID |
| `token` | Managed | -- | Current SSWS token value |

---

### newrelic_key

Rotates New Relic API keys via the NerdGraph GraphQL API.

```json
{
  "type": "newrelic_key",
  "admin_api_key": "NRAK-XXXXXXXXXXXXXXXXXXXX",
  "account_id": 1234567,
  "key_type": "USER",
  "ingest_type": "",
  "key_name": "akeyless-managed",
  "key_id": "",
  "key": ""
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `admin_api_key` | Yes | -- | User API key with key management permissions |
| `account_id` | Yes | -- | New Relic account ID |
| `key_type` | No | `USER` | `USER` or `INGEST` |
| `ingest_type` | If `INGEST` | `LICENSE` | `BROWSER` or `LICENSE` (only for INGEST keys) |
| `key_name` | Yes | -- | Display name for the key |
| `key_id` | Managed | -- | Current key ID (`NRAK-...` or `NRII-...`) |
| `key` | Managed | -- | Current key value |

---

### aerospike_password

Rotates an Aerospike Database user's password via the admin wire protocol
(ChangePassword command) using [aerospike-client-go v8](https://github.com/aerospike/aerospike-client-go).
Generates a random 24-character password by default.

```json
{
  "type": "aerospike_password",
  "seeds": "aerospike-0.aerospike.svc:3000,aerospike-1.aerospike.svc:3000",
  "tls_name": "",
  "auth_mode": "internal",
  "admin_user": "admin",
  "admin_password": "admin-password",
  "target_user": "svc-app",
  "password": "",
  "password_length": 24
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `seeds` | Yes | -- | Comma-separated seed list of `host:port` entries |
| `tls_name` | No | -- | Expected TLS certificate name (when TLS is enabled on the cluster) |
| `auth_mode` | No | `internal` | One of `internal`, `external`, or `pki` |
| `admin_user` | Yes | -- | Aerospike admin account used to perform the rotation |
| `admin_password` | Yes | -- | Password for `admin_user` |
| `target_user` | Yes | -- | Aerospike user whose password will be rotated |
| `password` | Managed | -- | Current password (overwritten on each rotation) |
| `password_length` | No | `24` | Length of the generated password |

**Notes.** Aerospike security (users, roles, passwords) is an Enterprise
Edition feature; it is not present in Community Edition, and a feature-key
enabling security is required on Enterprise clusters. This rotator ships
EE-shaped so it can be deployed today against CE while EE licensing is
arranged. A rotation call against a cluster without security enabled returns
a wrapped `SECURITY_NOT_ENABLED` error from the producer. The message
explains that Aerospike security is not enabled on the cluster and
that Enterprise Edition (or a feature-key-enabled build) is required
for password rotation. Once the cluster has security enabled,
rotations succeed with no code changes.

---

## Adding a New Target

### Step 1: Create the target package

Create a new directory under `go/rotator/internal/targets/yourservice/` with three files:

**`types.go`** -- Define the payload struct:

```go
package yourservice

type TokenPayload struct {
    Type       string `json:"type"`        // "yourservice_token"
    BaseURL    string `json:"base_url"`    // Target service URL
    AdminToken string `json:"admin_token"` // Admin credential for API auth
    TokenName  string `json:"token_name"`
    TokenID    string `json:"token_id"`    // Managed by rotator
    Token      string `json:"token"`       // Managed by rotator
}
```

**`client.go`** -- Implement the API calls:

```go
package yourservice

import (
    "context"
    "fmt"
    "net/http"
    "encoding/json"
    "bytes"
)

type Client struct {
    baseURL    string
    adminToken string
    http       *http.Client
}

func NewClient(baseURL, adminToken string) *Client {
    return &Client{
        baseURL:    baseURL,
        adminToken: adminToken,
        http:       &http.Client{},
    }
}

func (c *Client) CreateToken(ctx context.Context, name string) (id, token string, err error) {
    // Call the target service API to create a new token
    // Return the new token ID and value
}

func (c *Client) DeleteToken(ctx context.Context, id string) error {
    // Call the target service API to delete/revoke a token
}
```

**`target.go`** -- Implement the `registry.Target` interface:

```go
package yourservice

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/akeylesslabs/custom-producer/go/pkg/types"
    "github.com/rs/zerolog/log"
)

type Target struct{}

func New() *Target { return &Target{} }

func (t *Target) Type() string { return "yourservice_token" }

func (t *Target) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
    var p TokenPayload
    if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
        return nil, fmt.Errorf("parse payload: %w", err)
    }
    return &types.CreateResponse{
        ID:       p.TokenID,
        Response: req.Payload,
    }, nil
}

func (t *Target) Revoke(_ context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error) {
    return &types.RevokeResponse{Revoked: req.IDs, Message: "acknowledged"}, nil
}

func (t *Target) Rotate(ctx context.Context, req *types.RotateRequest) (*types.RotateResponse, error) {
    var p TokenPayload
    if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
        return nil, fmt.Errorf("parse payload: %w", err)
    }

    client := NewClient(p.BaseURL, p.AdminToken)

    // Create new token
    newID, newToken, err := client.CreateToken(ctx, p.TokenName)
    if err != nil {
        return nil, fmt.Errorf("create token: %w", err)
    }

    // Revoke old token (best-effort)
    if p.TokenID != "" {
        if err := client.DeleteToken(ctx, p.TokenID); err != nil {
            log.Warn().Err(err).Str("old_id", p.TokenID).Msg("failed to revoke old token")
        }
    }

    // Update payload with new credentials
    p.TokenID = newID
    p.Token = newToken
    out, _ := json.Marshal(p)
    return &types.RotateResponse{Payload: string(out)}, nil
}
```

### Step 2: Register the target

Add the import and registration call in `go/rotator/bin/cmd/main.go`:

```go
import (
    // ...
    "github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/yourservice"
)

func main() {
    // ...
    reg.Register(yourservice.New())
    // ...
}
```

### Step 3: Build and test

```bash
cd go
SKIP_AUTH=true PORT=9999 go run ./rotator/bin/cmd
# Verify your type appears in the startup log

curl -s -X POST http://localhost:9999/sync/rotate \
  -H 'Content-Type: application/json' \
  -d '{
    "payload": "{\"type\":\"yourservice_token\",\"base_url\":\"https://...\",\"admin_token\":\"...\",\"token_name\":\"test\",\"token_id\":\"\",\"token\":\"\"}"
  }' | jq .
```

### Step 4: Push and let CI build

Commit your changes and push to `master`. The GitHub Actions Docker workflow builds and pushes the updated image automatically.

---

## Rotation Patterns

Targets use one of two rotation strategies:

### Create-before-revoke (default)

Used by most targets. Ensures zero downtime by creating a new credential before revoking the old one.

```
1. Parse current payload (contains old token ID)
2. Call target API to create a new token
3. Call target API to revoke the old token (best-effort -- logged but not fatal)
4. Return updated payload with new token ID and value
```

Targets: `pat`, `gitlab_token`, `api_key`, `cloudflare_token`, `github_app_token`, `jfrog_token`, `datadog_key`, `confluent_key`, `pagerduty_key`, `okta_key`, `sendgrid_key`, `newrelic_key`

### Revoke-before-create

Used when the target system enforces unique constraints (e.g., unique token names).

```
1. Parse current payload
2. Revoke the old token first
3. Create a new token (may append a timestamp to the name for uniqueness)
4. Return updated payload
```

Targets: `grafana_token`

### In-place replacement

Used when the target system's API replaces the credential atomically (singletons).

```
1. Parse current payload
2. Call API to create/rotate -- the old credential is automatically replaced
3. Return updated payload
```

Targets: `tfc_token` (team/org tokens are singletons), `password` (password is overwritten), `servicenow_cred` (client secret is regenerated), `slack_token` (refresh token exchange)

---

## Troubleshooting

### The rotator returns "unknown target type"

The `type` field in your payload does not match any registered target. Check the startup log for the list of registered types:

```
registered rotation targets  targets=["echo","password","api_key","pat",...]
```

Valid type values: `echo`, `password`, `api_key`, `pat`, `github_app_token`, `gitlab_token`, `cloudflare_token`, `tfc_token`, `argocd_token`, `jfrog_token`, `datadog_key`, `grafana_token`, `pagerduty_key`, `newrelic_key`, `slack_token`, `sendgrid_key`, `confluent_key`, `servicenow_cred`, `okta_key`

### Authentication fails with "missing AkeylessCreds header"

The Akeyless Gateway sends this header automatically. If you see this error:
- You are calling the rotator directly without `SKIP_AUTH=true`
- Your Gateway is not configured to send the credentials header

For local testing, set `SKIP_AUTH=true`.

### Authentication fails with "invalid credentials"

The `AKEYLESS_ACCESS_ID` environment variable on the rotator must match the access ID that your Akeyless Gateway authenticates with. Verify:

```bash
# Check what access ID the rotator expects
kubectl -n rotator logs deployment/custom-producer | grep access
```

### Rotation succeeds but the old credential is still active

Most targets use create-before-revoke. The old credential revocation is best-effort -- if it fails, the rotator logs a warning but still returns success (because the new credential is valid). Check the logs:

```bash
kubectl -n rotator logs deployment/custom-producer | grep "failed to revoke"
```

The old credential may remain active until it expires naturally.

### Grafana rotation fails with "token name already exists"

Grafana enforces unique token names per service account. The rotator handles this by appending a timestamp, but if a previous rotation was interrupted mid-way, a stale token with the same name may exist. Manually delete the old token from the Grafana UI, then retry.

### Azure DevOps rotation issues

All Azure DevOps auth and rotation failures are covered in the [Azure DevOps PAT runbook troubleshooting section](runbooks/azuredevops-pat.md#troubleshooting), including ROPC's `interaction_required` error, expired refresh tokens (`AADSTS70000`), the HTML-sign-in-page symptom of a dead bearer token, and the Akeyless-side errors (`401 invalid credentials`, `405 Method Not Allowed`, stale Web Target URLs).

### Container starts but no rotation happens

1. Verify the Web Target URL is correct and reachable from the Gateway
2. Verify the Rotated Secret is associated with the correct Web Target
3. Trigger a manual rotation: `akeyless rotate-secret --name "/Rotated/your-secret"`
4. Check Gateway logs for outbound HTTP errors

### How to read rotator logs

The rotator uses structured JSON logging via zerolog. Key fields:

```
level=info type=pat endpoint=rotate msg="dispatching request"     -- rotation started
level=error type=pat msg="rotate failed" error="..."              -- rotation failed
level=warn msg="failed to revoke old token" old_id=...            -- revoke failed (non-fatal)
```

---

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `PORT` | No | `8080` | HTTP server listen port |
| `AKEYLESS_ACCESS_ID` | Production | -- | The access ID of the Akeyless auth method used by the Gateway. Used to validate the `AkeylessCreds` header on incoming requests. If not set, credential validation is skipped with a warning. |
| `AKEYLESS_ITEM_NAME` | No | -- | If set, restricts the rotator to only accept requests for this specific rotated secret name. Useful for deployments that serve a single secret. |
| `SKIP_AUTH` | No | `false` | Set to `true` to disable `AkeylessCreds` header validation entirely. **For local development and testing only.** |

---

## API Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/health` | None | Returns `{"status":"healthy"}`. Use for liveness/readiness probes. |
| `POST` | `/sync/create` | AkeylessCreds | Called by Akeyless when a new rotated secret is created. Returns the current credential from the payload. |
| `POST` | `/sync/revoke` | AkeylessCreds | Called by Akeyless when credentials expire. Acknowledges revocation. |
| `POST` | `/sync/rotate` | AkeylessCreds | Called by Akeyless on rotation events. Performs credential rotation and returns the updated payload. |
| `POST` | `/webhook/rotation-event` | None | Optional webhook endpoint for rotation event notifications. Logs the event. |

### Request/response formats

**Rotate request** (sent by Akeyless Gateway):

```json
{
  "payload": "{\"type\":\"pat\",\"organization\":\"my-org\",...}"
}
```

The `payload` field is a JSON-encoded string (double-serialized). The outer object is the Akeyless envelope; the inner string is your target-specific configuration.

**Rotate response** (returned to Akeyless Gateway):

```json
{
  "payload": "{\"type\":\"pat\",\"organization\":\"my-org\",\"token\":\"new-value\",...}"
}
```

The rotator returns the full payload with updated credential fields. Akeyless stores this as the new rotated secret value.

**Create request:**

```json
{
  "payload": "{\"type\":\"echo\",...}",
  "client_info": {
    "access_id": "p-1234567890ab",
    "sub_claims": {}
  }
}
```

**Create response:**

```json
{
  "id": "unique-credential-id",
  "response": "{\"username\":\"...\",\"password\":\"...\"}"
}
```

**Revoke request:**

```json
{
  "payload": "{\"type\":\"echo\",...}",
  "ids": ["credential-id-1", "credential-id-2"]
}
```

**Revoke response:**

```json
{
  "revoked": ["credential-id-1", "credential-id-2"],
  "message": "acknowledged"
}
```

---

## Project Structure

```
.github/
  dependabot.yml                        # Monthly Dockerfile and Actions version bumps
  workflows/
    build.yaml                          # Lint, build, vet, govulncheck on every push/PR
    docker.yaml                         # Build and push container image to GHCR
    deps.yaml                           # Monthly Go dependency update PRs
go/
  rotator/
    Dockerfile                          # Multi-stage Go 1.25 + Alpine 3.21 build
    bin/cmd/main.go                     # Entrypoint: registers all 19 targets, starts HTTP server
    internal/
      handler/handler.go                # HTTP routes, auth middleware, type-based dispatch
      registry/registry.go              # Target interface and type registry
      targets/
        echo/target.go                  # Test/validation (no external deps)
        ansible/                        # AWX/AAP password + API key rotation
        argocd/                         # ArgoCD account token rotation
        azuredevops/                    # Azure DevOps PAT rotation (see runbooks/azuredevops-pat.md)
        cloudflare/                     # Cloudflare API token rotation
        confluent/                      # Confluent Cloud API key rotation
        datadog/                        # Datadog API + application key rotation
        github/                         # GitHub fine-grained PAT rotation
        gitlab/                         # GitLab PAT rotation
        grafana/                        # Grafana SA token rotation (revoke-first)
        jfrog/                          # JFrog Artifactory access token rotation
        newrelic/                       # New Relic API key rotation (NerdGraph)
        okta/                           # Okta SSWS API token rotation
        pagerduty/                      # PagerDuty REST API key rotation
        sendgrid/                       # SendGrid API key rotation
        servicenow/                     # ServiceNow OAuth client secret rotation
        slack/                          # Slack token rotation (refresh token exchange)
        terraform/                      # Terraform Cloud/Enterprise token rotation
  pkg/
    auth/auth.go                        # Akeyless credential validation (calls auth.akeyless.io)
    types/types.go                      # Shared request/response envelope types
```
