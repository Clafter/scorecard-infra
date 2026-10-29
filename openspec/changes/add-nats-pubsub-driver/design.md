# Design: add-nats-pubsub-driver

Decision records **KN1** onwards.

## KN1 — NATS over a from-scratch SAP Event Mesh driver

**Decision.** Use NATS (`gocloud.dev/pubsub/natspubsub`) as the cron
queue backend, run as a pod in the Kyma cluster.

**Alternatives.**

1. **SAP Event Mesh (managed BTP service).** The "most BTP-native"
   option. Rejected for effort: Event Mesh speaks AMQP 1.0 **over
   WebSocket**, for which the Go CDK has no driver (`azuresb` is AMQP 1.0
   but bound to the Azure SDK and Azure's auth, not reusable). A driver
   would be written from scratch — AMQP-1.0-over-WebSocket transport plus
   Event Mesh OAuth — and needs an Event Mesh entitlement the subaccount
   does not have. A multi-session engineering project.
2. **NATS in-cluster.** The Go CDK ships `natspubsub`, matching the
   pinned `gocloud.dev v0.46.0`. The code change is *registering* an
   existing driver. NATS runs as a small pod on Kyma — still "on BTP,"
   no extra entitlement. **Chosen.**
3. **Redis/Valkey in-cluster.** No Go CDK pubsub driver exists; would be
   as much from-scratch work as Event Mesh. Rejected.
4. **Kafka in-cluster** (`kafkapubsub` exists). A heavier broker than the
   workload needs for a dev evaluation; NATS is lighter. Rejected for
   dev, reconsiderable later.

"Everything on BTP" is satisfied: the broker is a workload in the BTP
Kyma cluster, not an AWS service and not a cross-cloud dependency.

## KN2 — NATS core (at-most-once); Ack/Nack are safe no-ops

**Context.** The pipeline was built for SQS/GCP, which are at-least-once:
the worker `Ack()`s a processed message and `Nack()`s one to retry
(three call sites in `cron/worker/worker.go` — missing metadata file,
processing error, result-exists check). The generic `gocloudSubscriber`
forwards both to the Go CDK message.

The pinned `natspubsub v0.46.0` driver is **NATS core only**:
`CanNack() == false`, and the Go CDK **panics** if `Nack()` is called on
a subscription that cannot nack. Core is also at-most-once — a message is
delivered at most once and is gone after delivery, acked or not.

**Decision.** A dedicated `natsSubscriber` (not the generic one) whose
`Ack()` and `Nack()` are no-ops. `Nack()` logs a warning so a lost retry
is visible in the worker logs. `SynchronousPull()` and `Close()` reuse
the generic gocloud receive/shutdown path.

**Trade-off accepted, flagged.** At-most-once means a scan request whose
metadata file is not yet written (the `Nack()`-to-retry-later case) is
**dropped**, not retried — that repository is simply not scanned this
run. For a weekly dev evaluation this is tolerable: the next run picks it
up. For production it is not, which is why JetStream (at-least-once) is
the named follow-up.

**Alternative** (newer `natspubsub` with JetStream): rejected for now
because it means bumping past the repo's pinned `gocloud.dev v0.46.0`
across the whole module, a wider change than the dev evaluation needs.
Recorded as the production follow-up.

## KN3 — Additive only; SQS and GCP paths untouched

**Decision.** The change adds a `case` to the subscriber's scheme switch
and a blank import to the publisher. The `awssnssqs` and `gcppubsub`
cases, their subscriber implementations, and their tests are not
touched. A deployment that still uses SQS or GCP behaves identically.

## KN4 — `NATS_SERVER_URL` is the connection point; the URL carries the subject

**Decision.** The driver connects to the server named in the
`NATS_SERVER_URL` environment variable (the driver's own convention) and
the `nats://<subject>` URL names the subject. In the Kyma deployment,
`NATS_SERVER_URL` points at the in-cluster NATS Service
(`nats://scorecard-nats.scorecard-infra-dev.svc.cluster.local:4222`);
the config file's topic/subscription URLs are `nats://scorecard-batch`.
Wiring these is `deploy-kyma-cron`'s job, not this change's.

## Open follow-ups

- **JetStream (at-least-once)** for production, when message loss on
  retry is no longer tolerable — needs a `gocloud.dev` bump.
- **SAP Event Mesh driver**, if the managed service is later preferred
  over an in-cluster broker — a separate, larger engine change.
