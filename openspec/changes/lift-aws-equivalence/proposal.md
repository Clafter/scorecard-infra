# Proposal: Lift the AWS-equivalence rule — BTP is the deployment target

## Why

`cron/README.md` has carried a rule since the GCP→AWS migration: *"Keep
the tree equivalent to what `ossf/scorecard` builds until the production
cutover to AWS completes."* Its purpose was to keep the imported `cron/`
tree a faithful mirror of upstream so the AWS cutover had a clean
acceptance test and rollback path.

That premise no longer holds. The production target is **SAP BTP
(Kyma)**, not AWS. The `deploy-kyma-baseline` and `deploy-kyma-api`
changes already put the account plane and the serving API on BTP. The
batch pipeline (`deploy-kyma-cron`) is blocked by exactly this rule: the
message-queue driver the pipeline needs for BTP (SAP Event Mesh, AMQP)
is **new engine code**, which the equivalence rule forbids.

This change lifts the rule so BTP-enabling engine work is legitimate,
while preserving the one constraint that is independent of deployment
target: the `cron/internal/format` schema coupling.

## What changes

- **`cron/README.md`** — the "keep equivalent to `ossf/scorecard` until
  AWS cutover" rule is replaced by: *BTP is the deployment target;
  engine changes needed to run on BTP are in scope; a temporary
  cross-cloud AWS SQS dependency is accepted for the dev evaluation
  until the Event Mesh driver lands.* The `cron/internal/format` schema
  warning is kept and explicitly scoped as target-independent.
- **AGENTS.md** — no change to the BigQuery rule (still forbidden; the
  cron analytics sink is dropped, not ported). The `cron/internal/format`
  gotcha already there is reinforced in `cron/README.md`; AGENTS.md
  itself carries no AWS-equivalence clause to remove.

## What this unblocks

- A SAP Event Mesh pubsub driver in `cron/internal/pubsub` (its own
  engine change) stops being a policy violation.
- `deploy-kyma-cron` can proceed: SQS-bridge now (interim, explicitly
  sanctioned), Event Mesh driver as the follow-up engine change.

## Scope boundary

- **No code changes.** This is a documentation/policy change only. The
  Event Mesh driver is a separate engine change; the SQS-bridge
  deployment is `deploy-kyma-cron`.
- **BigQuery stays forbidden.** Lifting the equivalence rule does not
  re-open BigQuery; the analytics sink is dropped for BTP (see
  `deploy-kyma-cron` KC3).
- **The schema coupling stays.** `cron/internal/format` edits still
  require the matching engine change.

## Impact

- **Governance:** a documented rule is rewritten to match the actual
  target. Done as an OpenSpec change so the policy shift is reviewable
  and attributable, not a silent edit to a protected file.
- **Downstream:** `deploy-kyma-cron` references this change as the thing
  that unblocks its KC1 decision.
