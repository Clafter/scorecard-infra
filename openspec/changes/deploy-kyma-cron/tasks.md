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
- [x] 3.2 Get answers. **Resolved:** SQS-bridge first (KC1), personal
      GitHub token for dev (KC2), Event Mesh as a follow-up engine
      change (now permitted by `lift-aws-equivalence`), BigQuery dropped
      (KC3). The apply phase (§4/§5) is unblocked; remaining items are
      verify-at-apply, not decisions.

## 4. Manifests (apply phase — DONE)

Manifests live in `deploy/btp/dev/` (same tree as the baseline/API), not
a separate `cron/` subdir — one kustomize-able directory per environment.

- [x] 4.1 `cron-rbac.yaml` — ServiceAccounts + the controller's
      Role/RoleBinding (KC4). Applied.
- [x] 4.2 `cron-config.yaml.template` + `apply-cron.sh` — ConfigMap
      rendered from the binding secret, single-bucket prefix layout
      (KC5/KC7). Applied.
- [x] 4.3 `cron-controller.yaml` — the producer CronJob (NATS publish +
      worker rollout). Applied (schedule Mon 02:00 UTC).
- [x] 4.4 `cron-worker.yaml` — the consumer, env from the cron-results
      binding + `GITHUB_AUTH_TOKEN`. Applied; rolled out to 1/2, waiting
      only on the `github` secret (KC2, user-provided).
- [x] 4.5 `cron-nats.yaml` — the in-cluster NATS broker (KC1). Applied,
      2/2 Running.
- [x] 4.6 No `bq-transfer` manifest (KC3).

## 5. Verification

- [ ] 5.1 Controller CronJob publishes to the queue; worker consumes.
      **Pending the `github` secret** (user step), then a manual job
      trigger.
- [ ] 5.2 Worker writes a result under the `api-results/` prefix; the
      API serves it via its cron fallback path. **Pending 5.1.**
- [x] 5.3 `make test` (`-race`) green across the repo; `cron-bucket-prefix`
      engine change covered by `TestOpenBucketPrefix`.
- [x] 5.4 All cron manifests validate (`--dry-run`) and are applied; the
      only not-Ready workload is the worker, blocked solely on the
      user-provided `github` secret.
