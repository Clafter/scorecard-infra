# Design: deploy-kyma-cron

Decision records numbered **KC1** onwards (`C` for cron). This change is
**design-only**: the records below settle how the pipeline *would* be
built on BTP and, where a blocker prevents a clean answer, say so
plainly rather than inventing one. No manifest here is applied.

## KC1 — The message queue is the pipeline's defining constraint, and it is unresolved

**Context.** The pipeline is producer → queue → consumer. The queue
implementation is selected by URL scheme in `cron/internal/pubsub`,
which registers exactly `awssnssqs://` and `gcppubsub://`.
`CreateSubscriber`/`CreatePublisher` reject every other scheme with
`errUnsupportedScheme`. There is no code path a BTP-native queue could
take.

**Options.**

1. **Cross-cloud AWS SQS.** Keep an AWS SQS queue (as the AWS cron
   deployment already uses) and have the Kyma workloads publish to and
   consume from it over the internet with AWS credentials. Works with
   zero code change — the `awssqs://` driver is already registered.
   - **Cost:** the "BTP deployment" now depends on an AWS account and
     AWS IAM, which partly defeats the point of moving to BTP, and adds
     cross-cloud egress and latency on every message.
   - **But:** it honours the AWS-equivalence rule exactly (the tree
     still builds what upstream builds), and it is the only option that
     needs no new Go.
2. **SAP Event Mesh (AMQP/MQTT).** BTP's native messaging. Requires a
   new gocloud pubsub driver (or a shim) registered in
   `cron/internal/pubsub` — **new Go code**. Previously forbidden by the
   AWS-equivalence rule; that rule was **lifted** (`lift-aws-equivalence`),
   so this is now in scope as its own engine change. Still needs an Event
   Mesh entitlement the subaccount does not yet have.
3. **A queue running in-cluster (e.g. NATS/RabbitMQ on Kyma).** Same
   new-driver problem as (2), plus owning a stateful workload.

**Decision.** Two phases, decided with Fabian:

- **Phase 1 (this change): cross-cloud AWS SQS.** The `awssqs://` driver
  is already registered, so the pipeline runs on Kyma compute with zero
  engine code. The cross-cloud AWS dependency is an explicit, documented
  interim — sanctioned by `lift-aws-equivalence`, not a violation of it —
  and the fastest path to a working cron pipeline on BTP.
- **Phase 2 (separate engine change): SAP Event Mesh driver.** A native
  AMQP pubsub driver in `cron/internal/pubsub`, which retires the AWS SQS
  dependency. Now permitted (the equivalence rule is gone). Gated on an
  Event Mesh entitlement from Sebastian; it is an engine change with its
  own tests, not part of this infra change.

**Resolved** (was: "most needs Fabian's sign-off"). Fabian chose
SQS-first; Event Mesh is the follow-up.

## KC2 — Scan credentials: provisioning and custody are unresolved

**Context.** The worker scans real repositories and needs a GitHub App
key (keys `app_id`, `installation_id`, `token`, `app_key`), and
optionally GitLab (`auth_token`) and Fastly (`purge_token`). On AWS
these live in Secrets Manager; `cron/k8s/README.md` documents creating
them by hand as Kubernetes Secrets until the CSI driver lands.

**Constraints.**

- The standing policy is **no plaintext credentials** in the repository
  or in chat.
- The baseline's Credential Store attempt failed (the `proxy` plan is
  not standalone-provisionable, baseline K9).

**Options.**

1. **Plain Kubernetes Secrets, created out-of-band** (the AWS interim
   approach). Credentials never enter the repo; an operator creates them
   with `kubectl create secret` from a local file that is shredded
   after. Acceptable under the policy because nothing is committed.
2. **Re-attempt Credential Store** with a non-proxy plan — needs an
   entitlement change first (baseline K9).
3. **BTP Destination service** — holds the GitHub endpoint + auth, but
   the worker reads env/secret keys, not Destinations, so this needs a
   code shim. Rejected (same AWS-equivalence problem as KC1).

**Decision.** Option 1 (out-of-band Kubernetes Secret) for dev, with a
**personal GitHub token** rather than a GitHub App for the initial
evaluation:

```sh
# The operator runs this; the token never enters the repo or chat.
kubectl -n scorecard-infra-dev create secret generic github \
  --from-literal=token="$(gh auth token)" \
  --dry-run=client -o yaml | kubectl apply -f -
```

**Accepted trade-offs, flagged.** A personal token is the fastest way to
get the worker past GitHub's unauthenticated rate limit (60 req/h) and
prove the pipeline scans. But:

- It carries the **operator's own identity** — every scan request counts
  against that user, not a machine account. Fine for a dev smoke test,
  not for sustained batch load.
- `gh auth token` yields a **short-lived OAuth token** bound to the
  operator's `gh` session; a long-running batch job can hit `401` when it
  expires. The worker's `github` secret may need refreshing between runs.
- The worker also reads `app_id` / `installation_id` / `app_key` for the
  GitHub **App** path; with a plain token those keys are absent, so the
  worker must be configured for token auth (verify the cron worker
  accepts a bare `GITHUB_TOKEN` / `github.token` secret and does not hard
  require the App key — checked at apply time).

**Follow-up:** a dedicated GitHub App (machine identity, long-lived key)
is the right production answer and its own provisioning task for
Fabian/Sebastian. The personal token unblocks dev now.

GitLab and Fastly secrets are omitted for dev — the evaluation scans the
GitHub corpus only; the worker tolerates their absence for GitHub-only
runs (verify at apply time).

## KC3 — BigQuery transfer is dropped for the BTP dev deployment

**Context.** `scorecard-bq-transfer` loads results into BigQuery for
analytics. AGENTS.md: *"No BigQuery in new code."* The existing AWS
manifests keep the BQ transfer un-ported deliberately
(`cron/k8s/README.md`: *"BigQuery transfer is out of scope"*).

**Decision.** The BTP cron deployment **does not include** the
BQ-transfer workload. The pipeline's job on BTP is to populate the
cron-results bucket; the analytics sink is a separate concern and a
BigQuery dependency is explicitly disallowed for new code. If an
analytics sink is wanted on BTP, it is its own change with its own
(non-BigQuery) decision.

No conflict with the AWS-equivalence rule: the AWS tree already leaves
BQ transfer un-ported, so omitting it on BTP keeps the two equivalent.

## KC4 — Pod Identity has no Kyma equivalent; workloads use plain ServiceAccounts + binding secrets

**Context.** The AWS manifests give each workload its own
ServiceAccount because EKS Pod Identity binds one IAM role per SA. Kyma
has no Pod Identity; cloud access comes from the Object Store **binding
secret** (the same mechanism the API uses, KA3).

**Decision.** Keep the one-ServiceAccount-per-workload structure (it is
also good RBAC hygiene — the controller's SA needs `patch` on the worker
Deployment for the rollout restart), but drop the Pod Identity
annotations. S3 credentials come from the `scorecard-cron-results-binding`
secret via `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`, exactly as the
API does.

## KC5 — Config is a ConfigMap of BTP bucket URLs, mirroring config-aws.yaml

**Context.** The pipeline reads one config file
(`/etc/scorecard/config.yaml`) naming its buckets. On AWS this is
`deploy/cron/config-aws.yaml` with `s3://ossf-scorecard-*` buckets.

**Decision.** A `deploy/btp/cron/config.yaml` ConfigMap with the **BTP**
bucket names (from the baseline's Object Store bindings) in place of the
upstream `ossf-scorecard-*` names, and `metric-exporter: printer` (no
Stackdriver/CloudWatch on BTP in dev). Everything else — shard size,
completion threshold, blacklisted checks — copied verbatim so behaviour
matches upstream.

**The bucket names are generated (`hcp-<uuid>`), not stable literals.**
Like the API (KA2), they come from the binding secret; the ConfigMap
references them rather than hardcoding, or is templated at apply time.
Finalised when the change moves from design to apply.

## KC6 — No capability spec yet

**Decision.** Design-only changes add no `specs/` delta. When the cron
pipeline actually runs on BTP, a `btp-batch` capability is born in the
change that applies it — not here, where nothing is deployed. Same
reasoning as the baseline (K10).

## Resolved decisions (were open questions)

All four questions that gated this change are now decided:

- **(KC1) Queue:** ✅ Cross-cloud AWS SQS accepted for the dev
  evaluation (phase 1); SAP Event Mesh driver is a follow-up engine
  change (phase 2), now permitted by `lift-aws-equivalence`.
- **(KC2) Credentials:** ✅ Personal GitHub token (`gh auth token`) in an
  out-of-band Kubernetes Secret for dev; a dedicated GitHub App is the
  production follow-up. Trade-offs flagged in KC2.
- **(KC1/entitlements) Event Mesh:** ✅ In scope as a separate engine
  change (the equivalence rule was lifted). Gated on an Event Mesh
  entitlement from Sebastian, pursued when phase 2 starts.
- **(KC3) Analytics:** ✅ Dropped for dev; no BigQuery, no replacement
  sink in this change.

## Verified at build time (2026-10-29)

- **Worker token-auth:** ✅ Scorecard reads `GITHUB_AUTH_TOKEN` /
  `GITHUB_TOKEN` / `GH_TOKEN` (scorecard v5.6.0
  `roundtripper/tokens/accessor.go`); the GitHub App keys are optional.
  The personal-token path works without an App. GitLab/Fastly secrets
  are optional for GitHub-only runs.
- **Queue:** NATS in-cluster, driver added in `add-nats-pubsub-driver`.
  No AWS SQS (superseded — golden rule 1).
- **Bucket name templating:** the generated `hcp-<uuid>` names come from
  the binding secrets; `apply-cron.sh` fills the ConfigMap template at
  apply time (KC5).

## KC7 — Bucket allocation under a 2-instance quota with hard credential isolation (STOP: decision needed)

**Context.** The cron pipeline uses five logical buckets: `result-data`,
`raw-result-data`, `input`, `cii-data`, `api-results`. The subaccount has
two Object Store instances (`scorecard-cron-results`,
`scorecard-api-results`); quota is exhausted (baseline K4).

**Empirically established** (in-cluster cross-bucket write tests,
2026-10-29):

- Each binding's S3 credentials are **hard-scoped to their own bucket**.
  The cron keypair gets `AccessDenied` on the api bucket and vice versa.
  Both keys are the same AWS account (`AKIAWMIH…`) but carry
  bucket-specific IAM policies.
- A pod has exactly one AWS credential pair; `s3blob` has no per-URL
  profile/credential parameter (verified in the driver source). One pod
  writes exactly one of the two buckets.

**The conflict.** The worker process writes three bucket roles:

- `api-results` (keys `<repo>/results.json`) — the bucket the live API
  reads. **Critical path.** Lives in the api-results instance.
- `result-data` (keys `<timestamp>/.shard_metadata`, …) — cron instance.
- `raw-result-data` — the **same** keys as `result-data`
  (`GetBlobFilename`, verified), so it collides with `result-data` in one
  bucket and needs a physically separate bucket.

Three write targets (two colliding) across a hard 2-bucket credential
boundary one pod cannot straddle. Not resolvable without (a) a feature
loss/code change or (b) a third Object Store entitlement.

**Why this is a golden-rule-4 stop.** Rule 2 (no feature loss, no
overcomplication) and rule 3 (no new entitlement unless strictly
necessary) point in opposite directions, and the choice is not derivable
from the rules:

- **Option B — one more Object Store instance (breaks rule 3, honours
  rule 2).** `raw-result-data` gets its own bucket. Clean, every feature
  intact. One entitlement ask to Sebastian. *Even so, the worker still
  writes two isolated buckets (cron-internal + api-results) with one
  keypair* — so a third bucket alone does not close it; see below.
- **Option A — code change to the worker/config (breaks rule 2).** Make
  the worker write all results through a single bucket+keypair (e.g.
  route api-results through the cron instance, drop the separate raw
  export). Loses the API-fallback separation the two-bucket design
  (baseline K4) exists for, or drops raw. A code change in the imported
  tree.
- **The deeper fact:** the pipeline assumes it can address several
  independently-credentialed buckets from one process. BTP Object Store
  gives one credential per bucket, hard-scoped. Making the two models
  meet needs *either* a credential layout where the worker's buckets
  share one keypair (a single multi-bucket instance — not how BTP
  s3-standard works), *or* a worker that uses per-bucket credentials
  (code change), *or* splitting the worker's writes across pods (code
  change).

**Recommendation to the user.** The minimal, feature-complete path that
respects rule 1 (all BTP) is a **single Object Store instance holding all
cron+api results under distinct key prefixes, consumed by one keypair** —
which is *fewer* instances, not more, and needs a small change to how the
pipeline derives bucket URLs (one bucket, prefix per role). That trades a
touch of rule-2 "no code" for satisfying rules 1+3 and the credential
reality. Alternatively, accept one entitlement bump (rule 3) to give each
colliding bucket its own instance **and** a worker credential design that
fits.

**Status: STOPPED for a decision (golden rule 4).** Everything up to the
bucket-credential wiring is built (NATS, RBAC, controller, worker,
config template). The worker's multi-bucket credential model is the one
thing that cannot be chosen from the rules alone.
