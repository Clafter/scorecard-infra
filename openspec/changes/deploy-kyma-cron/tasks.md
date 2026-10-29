# Tasks: deploy-kyma-cron

> **DESIGN ONLY.** No manifest is applied in this change. The tasks
> below are the design work and the decisions to surface; the
> apply-time tasks are listed but deliberately left unchecked and
> blocked on **KC1**/**KC2** (see `design.md`).

Decisions in `design.md` (**KC1**–**KC6**). Rationale in `proposal.md`.

## 1. Pipeline analysis (design)

- [x] 1.1 Enumerate the batch workloads and their dependencies from
      `cron/k8s/` and `cron/internal/` (controller, worker, CII,
      BQ-transfer, webhook/github-server).
- [x] 1.2 Identify the message-queue coupling: `cron/internal/pubsub`
      registers only `awssnssqs://` and `gcppubsub://`;
      `CreateSubscriber` rejects any other scheme. Recorded as KC1.
- [x] 1.3 Identify the scan-credential requirement (GitHub App key,
      GitLab, Fastly) and the "no plaintext credentials" constraint.
      Recorded as KC2.
- [x] 1.4 Confirm the BigQuery conflict (AGENTS.md "no BigQuery in new
      code"; AWS tree leaves BQ transfer un-ported). Recorded as KC3.
- [x] 1.5 Confirm the AWS-equivalence rule (`cron/README.md`) and its
      implication: no new pubsub driver in this change.

## 2. Decision records (design)

- [x] 2.1 KC1 — message queue options (cross-cloud SQS vs. Event Mesh
      vs. in-cluster), provisional decision + the open question for
      Fabian.
- [x] 2.2 KC2 — scan-credential custody options, provisional decision,
      the provisioning ask for Fabian/Sebastian.
- [x] 2.3 KC3 — BigQuery transfer dropped for BTP dev.
- [x] 2.4 KC4 — ServiceAccounts without Pod Identity; S3 creds from the
      binding secret.
- [x] 2.5 KC5 — config as a ConfigMap of BTP bucket URLs mirroring
      `config-aws.yaml`.
- [x] 2.6 KC6 — no capability spec in a design-only change.

## 3. Surface the blockers

- [x] 3.1 Record the four open questions for Fabian/Sebastian in
      `design.md` (queue, credentials, Event Mesh entitlement,
      analytics sink).
- [ ] 3.2 Get answers. **Blocked on Fabian (architecture) and
      Sebastian (entitlements/GitHub App).** This is the gate between
      design and apply.

## 4. Manifests (apply phase — BLOCKED, not done in this change)

- [ ] 4.1 `deploy/btp/cron/rbac.yaml` — ServiceAccounts + the
      controller's Role/RoleBinding (KC4). **Blocked on KC1.**
- [ ] 4.2 `deploy/btp/cron/config.yaml` — ConfigMap with BTP bucket
      URLs (KC5). **Blocked on KC1.**
- [ ] 4.3 `deploy/btp/cron/controller-cronjob.yaml` — the producer
      CronJob. **Blocked on KC1 (queue URL).**
- [ ] 4.4 `deploy/btp/cron/worker-deployment.yaml` — the consumer, env
      from the cron-results binding + scan-credential secrets.
      **Blocked on KC1 + KC2.**
- [ ] 4.5 `deploy/btp/cron/cii-cronjob.yaml` — CII enrichment.
      **Blocked on KC1.**
- [ ] 4.6 No `bq-transfer` manifest (KC3).

## 5. Verification (apply phase — BLOCKED)

- [ ] 5.1 Controller CronJob publishes to the queue; worker consumes.
      **Blocked.**
- [ ] 5.2 Worker writes a result into `scorecard-cron-results`; the API
      then serves it via its cron fallback path. **Blocked.**
- [ ] 5.3 `make lint` / `make test` green (will be no-ops once the
      manifests are added; no Go changes).
