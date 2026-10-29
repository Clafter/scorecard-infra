# BTP Deployment — Reproduction Guide

> **Status: live in dev, end-to-end verified 2026-10-29.** A full scan of
> `github.com/expressjs/express` is served over the public BTP endpoint
> (`GET /projects/github.com/expressjs/express` → HTTP 200, score 7.9).
> For the big-picture orientation — architecture, BTP compatibility
> changes, API surface, caching, products in use, and open items — read
> [`OVERVIEW.md`](OVERVIEW.md). This file is the step-by-step reproduction.

Living record of everything done to deploy Scorecard on SAP BTP (Kyma),
dev environment. Follow top-to-bottom to reproduce from scratch. Updated
as work proceeds (golden rule 5).

## Golden rules (operating constraints)

1. **Everything on BTP** — no AWS, no Google Cloud in the running system.
2. **Minimal setup without losing features** — never overcomplicate.
3. **New entitlements only when strictly necessary** (i.e. when rule 1
   or 2 would otherwise break).
4. **Don't stop** until a real stop condition or a deep decision not
   derivable from these rules.
5. **Document everything** — reproducibly, as we go.
6. **Keep working** until the deploy is finished or a stop is truly
   necessary.

## Target

- **Subaccount:** `I767568` (`95c7cf5e-21b8-42c7-8bde-7e7c97f53aba`)
- **Region:** `eu10` (AWS Frankfurt — the IaaS under Kyma; the workloads
  and services are all BTP-managed, no AWS APIs used)
- **Kyma cluster:** `*.bfcc835.kyma.ondemand.com`
- **Namespace:** `scorecard-infra-dev`
- **Fork:** `github.com/Clafter/scorecard-infra`
- **Images:** `ghcr.io/clafter/scorecard-*` (built by `publish-images.yml`
  on every push to `main`)

## Prerequisites (one-time, by hand)

1. **Fork** `ossf/scorecard-infra` → `Clafter/scorecard-infra`. Enable
   Actions on the fork (browser: Actions tab → "I understand … enable").
2. **Tools:** `kubectl`, `btp` CLI (`brew install --cask btp`),
   `opentofu` (`brew install opentofu`) — though Terraform is no longer
   on the deploy path (see change `deploy-kyma-baseline` K1).
3. **Kyma Environment** provisioned on the subaccount (cockpit →
   Kyma Environment → Create; ~45 min). Download the kubeconfig:
   ```sh
   mkdir -p ~/.kube/scorecard-infra
   # cockpit → Kyma → Download Kubeconfig → save as:
   export KUBECONFIG=~/.kube/scorecard-infra/kubeconfig-kyma-dev.yaml
   kubectl config current-context    # must be *.kyma.ondemand.com
   ```
4. **btp login:**
   ```sh
   btp login --sso
   btp target --subaccount 95c7cf5e-21b8-42c7-8bde-7e7c97f53aba
   ```
5. **Entitlements** assigned to the subaccount (verify, don't assume):
   ```sh
   btp list accounts/entitlement --subaccount 95c7cf5e-21b8-42c7-8bde-7e7c97f53aba
   ```
   Required and present: `xsuaa/application`, `objectstore/s3-standard`
   (quota 2), `alert-notification/free`, `cloud-logging/dev`. The BTP
   Service Operator (`btp-manager` in `kyma-system`) comes with the Kyma
   Environment.

## Shipped changes (in order)

Each is an OpenSpec change under `openspec/changes/` (or archived). All
merged to the fork's `main`.

| # | Change | What | Status |
|---|---|---|---|
| 1 | `rename-api-binary` | `scorecard-webapp` → `scorecard-api` | merged, archived |
| 2 | `deploy-kyma-baseline` | namespace + 5 service instances + bindings | merged, **live** |
| 3 | `deploy-kyma-api` | API Deployment/Service/APIRule/NetworkPolicy | merged, **live** |
| 4 | `deploy-kyma-cron` | batch pipeline (design → apply) | in progress |
| 5 | `lift-aws-equivalence` | drop the AWS-equivalence rule; BTP is target | merged |
| 6 | `add-nats-pubsub-driver` | NATS queue backend for cron | merged |

## Step 1 — Baseline (account plane)

Pure Kubernetes manifests reconciled by the BTP Service Operator (no
Terraform — the operator does natively what Terraform would duplicate).

```sh
export KUBECONFIG=~/.kube/scorecard-infra/kubeconfig-kyma-dev.yaml
kubectl apply -k deploy/btp/dev/      # baseline files only, at first
kubectl -n scorecard-infra-dev wait --for=condition=Ready \
  serviceinstance --all --timeout=15m
kubectl -n scorecard-infra-dev get servicebindings
```

Live: `xsuaa`, `scorecard-api-results`, `scorecard-cron-results`,
`alert-notification`, `cloud-logging` — each with a binding secret of
the same name.

**Not in the baseline** (documented in `deploy/btp/README.md`):
- **IAS** — needs an OIDC business-user trust the operator CR cannot
  create; deferred to the (future) human-login change.
- **Credential Store** — the `proxy` plan is a consumer proxy needing a
  shared origin the subaccount is not entitled to; removed.

## Step 2 — API workload

Same `deploy/btp/dev/` tree, workload manifests. Image pinned to an
immutable `sha-<commit>` tag.

```sh
kubectl apply -k deploy/btp/dev/
kubectl -n scorecard-infra-dev rollout status deploy/scorecard-api --timeout=5m
# Public read:
curl https://scorecard-api.bfcc835.kyma.ondemand.com/          # 200
# S3 wiring (clean 404, not 5xx):
curl https://scorecard-api.bfcc835.kyma.ondemand.com/projects/github.com/ossf/scorecard  # 404
# Auth split (APIRule jwt handler):
curl -X POST https://scorecard-api.bfcc835.kyma.ondemand.com/...  # 403 without token
```

Bucket URL is composed in the pod spec from the Object Store binding
secret: `s3://$(BUCKET)?region=$(BUCKET_REGION)&awssdk=v2` (the Object
Store is S3-compatible storage *managed by BTP*; the pod uses no AWS
APIs beyond the S3 protocol the binding provides).

**Gotcha that cost a debug cycle:** the NetworkPolicy must allow egress
to `istio-system`, or the pod deadlocks in Init (the istio-proxy sidecar
cannot reach istiod for its CA/xDS bootstrap). See `deploy-kyma-api` KA7.

## Step 3 — Cron batch pipeline (in progress)

Queue decision (golden rules 1+2): **NATS as an in-cluster pod**, not
AWS SQS (rule 1) and not SAP Event Mesh (rule 3 — Event Mesh would need
an entitlement and a from-scratch AMQP-1.0-over-WebSocket driver; NATS
has a ready Go CDK driver and runs as one small pod). Engine support
added in `add-nats-pubsub-driver`.

Scan credentials (golden rule 2): a personal GitHub token via
`gh auth token` in an out-of-band Kubernetes Secret — never committed.

BigQuery analytics sink: dropped (no GCP — rule 1).

Manifests and apply steps are filled in as Step 3 proceeds below.

Manifests in `deploy/btp/dev/` (applied via `apply-cron.sh`):
- `cron-nats.yaml` — NATS broker (Deployment + Service). **Live and
  verified:** pod 2/2, `/healthz` ok, NATS 2.10.29 core.
- `cron-rbac.yaml` — ServiceAccounts + controller rollout-restart Role.
  **Applied.**
- `cron-controller.yaml` — weekly producer CronJob (NATS publish + worker
  rollout). Built, validated, not yet applied (KC7).
- `cron-worker.yaml` — consumer Deployment (replicas 2, `GITHUB_AUTH_TOKEN`
  auth, NATS subscribe). Built, validated, not yet applied (KC7).
- `cron-config.yaml.template` + `apply-cron.sh` — ConfigMap rendered from
  the binding secrets at apply time.

**Verified facts (empirical, 2026-10-29):**
- Scorecard v5.6.0 reads `GITHUB_AUTH_TOKEN` — personal token works, no
  GitHub App needed (minimal, golden rule 2).
- BTP Object Store credentials are **hard-scoped per bucket**: the cron
  keypair cannot write the api bucket and vice versa (`AccessDenied`,
  both directions, proven via in-cluster aws-cli pods).

**RESOLVED — KC7 (golden rule 4 decision taken by the user: option A).**
The worker's multi-bucket credential problem is solved by a single
Object Store instance holding every result role under a distinct key
prefix (`cron-bucket-prefix` engine change). All roles point at the one
`cron-results` bucket (`?prefix=result-data/`, `raw/`, `input/`, `cii/`,
`api-results/`); one hard-scoped keypair writes them all, no collisions,
no new entitlement (golden rules 1+2+3).

**FULLY LIVE — end-to-end scan verified 2026-10-29.** A scan of
`github.com/expressjs/express` is reachable over the public BTP endpoint:
`GET /projects/github.com/expressjs/express` → HTTP 200, score 7.9,
14 checks, engine scorecard v5.6.0 (latest and commit-pinned both 200).

Running workloads:
- `scorecard-nats` 1/1 — in-cluster queue, kept out of the Istio mesh.
- `scorecard-batch-controller` CronJob installed (Mon 02:00 UTC).
- `scorecard-batch-worker` 1/1 — consumes from NATS, runs the engine,
  writes `api-results/host/org/repo/results.json`.
- `scorecard-api` 1/1 — serves results from the shared bucket+prefix.

**Three fixes were needed to get the first scan through (full story in
[`OVERVIEW.md`](OVERVIEW.md) §3–§4):**
1. **Controller writes `.shard_metadata` before publishing**
   (`controller-metadata-before-publish`). Core NATS is at-most-once, so
   the worker's nack of a metadata-less request was a no-op and dropped
   every request — no scan ran. Writing the marker first removes the race.
2. **NATS out of the Istio mesh** (`cron-nats-istio.yaml` +
   `sidecar.istio.io/inject: "false"`). NATS speaks first on connect,
   which deadlocks against sidecar mTLS (worker → nats 4222 i/o timeout).
3. **API + worker share one results bucket + credential**
   (`api-deployment.yaml`). The worker writes to the cron bucket; a
   separate api-results bucket would 403 the API's cross-bucket read
   under hard credential isolation. Both now use the same bucket+prefix.

**The one manual prerequisite (the user's to do — the GitHub token must
not pass through the agent, credentials policy):**
```sh
export KUBECONFIG=~/.kube/scorecard-infra/kubeconfig-kyma-dev.yaml
kubectl -n scorecard-infra-dev create secret generic github \
  --from-literal=token="$(gh auth token)" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n scorecard-infra-dev rollout status deploy/scorecard-batch-worker
```
Trigger a run without waiting for Monday:
```sh
kubectl -n scorecard-infra-dev create job \
  --from=cronjob/scorecard-batch-controller cron-manual-$(date +%s)
```

**ResourceQuota note:** raised from 4 CPU / 8Gi to 8 CPU / 16Gi once the
Istio sidecar overhead (~1 CPU + ~1Gi limits per injected pod) was
measured against three workloads. A namespace-internal limit, not a BTP
entitlement.

**A quota gotcha worth remembering:** the baseline ResourceQuota forces
every container to set BOTH `requests` and `limits` for cpu and memory —
a pod missing `limits.cpu` is rejected with `must specify limits.cpu`.
The upstream cron manifests deliberately omit the worker's CPU limit;
on a quota'd namespace that must be added back.
