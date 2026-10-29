# Proposal: Add a NATS pubsub driver to the cron pipeline

## Why

`deploy-kyma-cron` needs a message queue, and the decision (its KC1) is
that everything runs on BTP — no cross-cloud AWS SQS. The pipeline's
queue is selected by URL scheme in `cron/internal/pubsub`, which today
registers only `awssnssqs://` and `gcppubsub://`. To run the queue on
BTP/Kyma, the binary must understand a scheme a BTP-hosted broker
speaks.

Evaluated options (see `design.md` for the full comparison): SAP Event
Mesh (managed, but AMQP-1.0-over-WebSocket with no Go CDK driver — a
from-scratch driver plus an entitlement), versus an in-cluster broker.
The in-cluster broker wins on effort because the Go CDK already ships a
**NATS** driver (`gocloud.dev/pubsub/natspubsub`), so the code change is
registering an existing driver, not writing one. NATS runs as a pod in
the Kyma cluster — on BTP, no extra entitlement.

This change makes that registration, with the one semantic adaptation
NATS forces (below). It is the engine half; the deploy half (NATS pod +
cron manifests) is `deploy-kyma-cron`'s apply phase.

## What changes

- **`go.mod` / `go.sum`** — add `gocloud.dev/pubsub/natspubsub v0.46.0`
  (matches the pinned `gocloud.dev v0.46.0`) and its NATS client
  transitive deps.
- **`cron/internal/pubsub/publisher.go`** — blank-import the NATS driver
  so `nats://` topics open. The publisher is already provider-agnostic
  (`pubsub.OpenTopic` + `topic.Send`); no other change.
- **`cron/internal/pubsub/subscriber.go`** — route the `nats://` scheme
  to a NATS subscriber.
- **`cron/internal/pubsub/subscriber_nats.go`** (new) — a NATS subscriber
  that adapts the pipeline's at-least-once assumptions to NATS's
  at-most-once reality (see the semantics decision in `design.md`).
- **Tests** — `subscriber_nats_test.go` covering scheme selection and
  the no-op Ack/Nack semantics.

## The semantic adaptation (why a new file, not the generic subscriber)

The existing generic `gocloudSubscriber` calls `msg.Ack()` and
`msg.Nack()`. The Go CDK NATS driver reports `CanNack() == false`
(NATS core is at-most-once), and calling `Nack()` on such a subscription
**panics**. The worker (`cron/worker/worker.go`) calls `Nack()` in three
places for retry. Routing NATS through the generic subscriber would
crash the worker on the first retry.

The NATS subscriber therefore makes `Ack()` and `Nack()` safe no-ops,
logging a warning on `Nack()` so the lost-retry is visible. This is the
accepted at-most-once trade-off for the dev evaluation, recorded as a
decision, not a silent behaviour change.

## Scope boundary

- **No deploy.** The NATS pod and the cron manifests are
  `deploy-kyma-cron`'s apply phase, not this change.
- **No change to SQS/GCP paths.** Their schemes, code, and tests are
  untouched; this is purely additive.
- **No JetStream.** The pinned `natspubsub v0.46.0` driver is NATS-core
  only (at-most-once). At-least-once via JetStream would need a newer
  driver and a dependency bump; deferred unless the dev evaluation shows
  message loss matters.

## Impact

- **Engine code changes** in `cron/internal/pubsub` — the first such
  change permitted after `lift-aws-equivalence` removed the
  AWS-equivalence rule.
- **`make test` / `make lint`** must stay green; the change adds tests
  and touches no existing test's expectations.
- **Unblocks** `deploy-kyma-cron`'s apply phase: with `nats://`
  understood, the pipeline can run its queue in-cluster on BTP.
