# Proposal: Deploy the Scorecard batch (cron) pipeline to Kyma (dev)

> **Status: DESIGN ONLY.** This change writes the proposal and the
> decision records. It does **not** apply any manifest. Three
> dependencies must be resolved first (a message queue, scan
> credentials, and the BigQuery question); the design exists to make
> those decisions explicit and reviewable before any cluster is
> touched. See "Blockers" below.

## Why

The baseline (`deploy-kyma-baseline`) and the serving API
(`deploy-kyma-api`) are live on Kyma dev. The remaining system is the
**batch scanning pipeline** (`cron/`): a weekly producer/consumer job
that scans a corpus of repositories and writes their results to the
cron-results bucket the API reads as a fallback. Until it runs, the
`scorecard-cron-results` bucket provisioned in the baseline stays empty
and the API only serves Action-published results.

This change is the plan for putting that pipeline on Kyma. It is
written design-first because the pipeline, unlike the API, does **not**
port to BTP without resolving architectural questions that are cheaper
to settle on paper than in a half-built cluster.

## What the pipeline is

From `cron/k8s/` and `cron/internal/`:

- **Controller** — a `CronJob` (Mondays 02:00 UTC). Reads the input
  corpus, shards it, and **publishes** scan requests to a message
  queue. Then rolls the worker Deployment to pick up the new shard.
- **Worker** — a `Deployment` that **consumes** from the queue, runs
  Scorecard against each repository (needs GitHub/GitLab credentials),
  and writes results to the cron-results bucket.
- **CII worker** — enriches results with CII best-practices data.
- **BQ transfer** — a `CronJob` that loads results into BigQuery.
- **Webhook / GitHub server** — auxiliary services for the release-test
  tier.

The producer→queue→consumer shape is the heart of it, and the queue is
where BTP and AWS diverge.

## What this change decides (no apply)

A `deploy/btp/cron/` manifest tree **designed** but not applied, plus the
decision records that make the three blockers explicit:

```text
deploy/btp/cron/            (planned, not written until blockers clear)
  controller-cronjob.yaml
  worker-deployment.yaml
  cii-cronjob.yaml
  config.yaml               ConfigMap, BTP bucket URLs
  rbac.yaml                 ServiceAccounts (no Pod Identity on Kyma)
  README.md
```

The decision records (`design.md`, **KC1**–**KCn**) cover: how the
message queue is provided on BTP, how scan credentials are held under
the "no plaintext credentials" policy, what happens to the BigQuery
transfer given AGENTS.md forbids BigQuery in new code, and how the
AWS-equivalence rule in `cron/README.md` constrains all of the above.

## Blockers (must resolve before any apply)

1. **Message queue.** `cron/internal/pubsub` registers exactly two
   drivers — `awssnssqs://` and `gcppubsub://` — and
   `CreateSubscriber` rejects any other scheme with
   `errUnsupportedScheme`. BTP has neither SQS nor GCP Pub/Sub. SAP
   Event Mesh speaks AMQP/MQTT, which no registered driver understands.
   Bridging this needs either (a) a cross-cloud AWS SQS the Kyma
   workloads reach, or (b) new Go code registering a BTP-native driver
   — and (b) collides with the AWS-equivalence rule (below). **Needs a
   decision + likely an entitlement.**
2. **Scan credentials.** The worker needs a GitHub App key (and
   optionally GitLab/Fastly tokens) to scan. These do not exist for
   this evaluation, and the standing policy is no plaintext credentials
   in repo or chat. **Needs Fabian/Sebastian to provision and a
   decision on where they live (K8s Secret vs. a re-attempted
   Credential Store).**
3. **BigQuery.** The `bq-transfer` workload loads results into
   BigQuery. AGENTS.md: *"No BigQuery in new code."* A BTP deployment
   cannot port it as-is. **Needs a decision: drop it for the dev
   evaluation, or replace the analytics sink.**

## Scope boundary

- **No apply, no deploy** in this change. Manifests are designed, not
  shipped.
- **No Go changes.** Especially no new pubsub driver — that is the
  thing the AWS-equivalence rule (`cron/README.md`: *"Keep the tree
  equivalent to what `ossf/scorecard` builds until the production
  cutover to AWS completes"*) exists to prevent, and it is not this
  change's call to make.
- **No BigQuery.** Per AGENTS.md.

## Impact

- **New docs only:** this proposal, `design.md`, `tasks.md`. No
  manifests, no code.
- **Surfaces three decisions** for Fabian (architecture) and Sebastian
  (entitlements) that block a real cron deployment, so they can be
  resolved deliberately rather than discovered mid-apply.
