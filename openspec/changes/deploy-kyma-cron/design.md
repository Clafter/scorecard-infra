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

## Remaining verify-at-apply items

- **SQS provisioning:** a dev AWS SQS queue and the AWS credentials the
  Kyma workloads use to reach it (whose AWS account, which IAM policy).
- **Worker token-auth:** confirm the cron worker accepts a bare
  `github.token` secret without the GitHub App keys (`app_id`,
  `installation_id`, `app_key`), and tolerates absent GitLab/Fastly
  secrets for GitHub-only runs.
- **Bucket name templating:** the generated `hcp-<uuid>` names come from
  the binding secret (KC5); finalise ConfigMap templating when writing
  the manifests.
