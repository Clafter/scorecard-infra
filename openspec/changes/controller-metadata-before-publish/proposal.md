# Write the shard metadata file before publishing scan requests

## Why

The cron controller publishes scan requests to the queue first and writes
the run's `.shard_metadata` marker afterward (`cron/internal/controller/
main.go`: `publishToRepoRequestTopic` then `WriteToBlobStore`). The worker
refuses to process a request whose `.shard_metadata` is not yet present and
**nacks** it, relying on the message being redelivered once the marker
lands (`cron/worker/worker.go`, the `hasMetadataFile` guard). Upstream this
is safe: GCP Pub/Sub and SQS redeliver nacked messages.

On SAP BTP the queue is in-cluster NATS through the gocloud `natspubsub`
v0.46 driver (the `add-nats-pubsub-driver` change). That driver only ever
speaks **core NATS** — even its `UseV2` path uses `SubscribeSync`, not
JetStream — which is at-most-once. A nack is a documented no-op
("Nack is a no-op under at-most-once delivery; message will not be
retried"), so every request lost the metadata race and was dropped. No
scan ever ran.

Verified end to end: the controller published, the worker received the
message, `hasMetadataFile` returned false (the marker was written
milliseconds later), the worker nacked, and the message was gone. A local
harness against the real Object Store confirmed that once the marker
exists the same worker code finds it and proceeds (`BlobExists -> true`).

## What changes

- The controller writes the `.shard_metadata` marker(s) **before** it
  publishes any scan request, so a received request always has its marker
  present and the worker's guard never trips.
- The shard count that the marker records is computed up front (the
  request stream is materialised into shards first), decoupling the count
  from the act of publishing.
- No worker change; no transport change; no gocloud bump. The nack path
  stays as a correctness backstop for deployments that *do* redeliver
  (SQS/GCP), so upstream behaviour there is unchanged.

## Impact

- Affected: `cron/internal/controller/main.go` (ordering + shard
  pre-count). Possibly a small helper in `cron/internal/controller`.
- This is a deliberate divergence from the upstream controller ordering,
  which `cron/README.md` asks to keep equivalent. The divergence is scoped
  to the NATS deployment's correctness and is documented here and in the
  design; the SQS/GCP result is identical (metadata present before the
  worker acts either way).
- No schema change, no new entitlement, no BigQuery.
