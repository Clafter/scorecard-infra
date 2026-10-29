# BTP Deployment — Reproduction Guide

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

**OPEN DECISION (golden rule 4 stop) — KC7 bucket/credential model:**
The worker writes three bucket roles (`api-results`, `result-data`,
`raw-result-data`) where `result-data`/`raw` collide on identical keys
and `api-results` lives in a separately-credentialed bucket — but a pod
has one keypair and each bucket is hard-isolated. Resolving this needs
either a code change to the worker's write path (golden rule 2) or an
entitlement change (golden rule 3); the two rules conflict, so the
choice is surfaced to the user rather than guessed. Full analysis in
`openspec/changes/deploy-kyma-cron/design.md` KC7.

**What is NOT blocked and is live:** the NATS queue and RBAC. The
controller/worker manifests are complete and validated; applying them
waits on the KC7 decision (which bucket layout the worker writes to).
