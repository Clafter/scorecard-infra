# Tasks: lift-aws-equivalence

Rationale in `proposal.md`. This is a documentation/policy change; no
code, no spec delta.

## 1. Rewrite the rule

- [x] 1.1 `cron/README.md`: replace the "keep the tree equivalent to
      `ossf/scorecard` until the AWS cutover completes" rule with the
      BTP-target rule (engine changes for BTP in scope; interim
      cross-cloud SQS accepted for dev).
- [x] 1.2 `cron/README.md`: keep the `cron/internal/format` schema
      coupling warning, scoped explicitly as target-independent.
- [x] 1.3 AGENTS.md: confirm it carries no separate AWS-equivalence
      clause to remove (it does not); the BigQuery rule stays.

## 2. Consistency check

- [ ] 2.1 Grep the tree for other statements of the old rule
      ("equivalent", "cutover to AWS") and confirm none contradict the
      new policy. `docs/upstream-graft.md` references the *historical*
      GCP→AWS import cutover (complete), not the forward AWS-equivalence
      rule — left as historical context.

## 3. Verify

- [ ] 3.1 `make lint` / `make test` green (no-ops: no code changed).
- [ ] 3.2 `deploy-kyma-cron` design references this change as the KC1
      unblock.
