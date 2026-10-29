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
   `cron/internal/pubsub` — **new Go code**, which the AWS-equivalence
   rule in `cron/README.md` explicitly exists to prevent before the AWS
   cutover completes. Also an entitlement the subaccount does not have.
3. **A queue running in-cluster (e.g. NATS/RabbitMQ on Kyma).** Same
   new-driver problem as (2), plus owning a stateful workload.

**Decision (provisional, for Fabian).** For a dev evaluation, **option
1 (cross-cloud AWS SQS)** is the only one that does not require code the
AWS-equivalence rule forbids. It is ugly but honest: it proves the
pipeline runs on Kyma compute while leaving the queue where the code can
already talk to it. Options 2/3 are a post-cutover project with their
own change and an engine-code decision, not an infra change.

**This is the decision that most needs Fabian's sign-off** — it
determines whether `deploy-kyma-cron` is even an infra-only change or
needs an engine change first.

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

**Decision (provisional).** Option 1 (out-of-band K8s Secrets) for dev,
matching the documented AWS interim. **Needs Fabian/Sebastian to
actually provision a GitHub App** for the evaluation; until then the
worker has nothing to authenticate with and the pipeline cannot scan.

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

## Open questions for Fabian / Sebastian

- **(KC1) Queue:** Accept cross-cloud AWS SQS for the dev evaluation, or
  hold the cron pipeline until a post-cutover messaging decision? This
  is the gating question.
- **(KC2) Credentials:** Provision a GitHub App for the evaluation? Who
  owns the key, and is the out-of-band K8s Secret approach acceptable
  under the credentials policy?
- **(KC1/entitlements) Event Mesh:** If cross-cloud SQS is rejected, is
  SAP Event Mesh entitlement + an engine-code change (new pubsub
  driver) in scope — and is that a separate engine change rather than
  this infra change?
- **(KC3) Analytics:** Is dropping the results-analytics sink acceptable
  for the dev evaluation, or is a non-BigQuery replacement wanted?
