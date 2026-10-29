# Proposal: Support a `prefix` query parameter on cron bucket URLs

## Why

The BTP cron deployment (`deploy-kyma-cron`) hit a hard constraint
(its KC7): the batch worker writes several logical buckets
(`result-data`, `raw-result-data`, `api-results`, …) from one process,
but BTP Object Store gives one hard-scoped credential per bucket and a
pod has a single AWS credential pair. The worker cannot write two
separately-credentialed buckets.

The chosen resolution (golden rules 1+2+3: all-BTP, minimal, no new
entitlement) is to put every logical bucket in **one** Object Store
instance, separated by **key prefix**. That needs the cron code to
accept a prefix on a bucket URL — which `gocloud.dev/blob/s3blob` does
not do natively (the S3 bucket is the URL host; there is no path/prefix).

This change adds prefix support in the cron `data` package's bucket
open path, so `result-data`, `raw-result-data`, etc. can share one
physical bucket without colliding.

## What changes

- **`cron/data/blob.go`** — a single internal `openBucket(ctx, url)`
  helper replaces the five direct `blob.OpenBucket(ctx, bucketURL)`
  calls. It reads an optional `prefix` query parameter from the URL,
  strips it, opens the bucket, and wraps it with `blob.PrefixedBucket`
  when a prefix is present. Every key written or read through that
  bucket is transparently namespaced under the prefix.
- **Tests** — `blob_test.go` cases: a URL with `?prefix=` isolates keys;
  a URL without it behaves exactly as before (no regression for the
  SQS/GCP deployments).

## How it resolves KC7

With prefixes, the cron `deploy/btp/dev/cron-config.yaml` points all
roles at the **one** `cron-results` bucket under distinct prefixes:

```
result-data-bucket-url:      s3://<bucket>?region=…&prefix=result-data/
raw-result-data-bucket-url:  s3://<bucket>?region=…&prefix=raw/
input-bucket-url:            s3://<bucket>?region=…&prefix=input/
cii-data-bucket-url:         s3://<bucket>?region=…&prefix=cii/
api-results-bucket-url:      s3://<bucket>?region=…&prefix=api-results/
```

One bucket, one credential pair, no collisions, no new entitlement. The
`result-data`/`raw` key collision (identical `GetBlobFilename` keys)
disappears because they now live under different prefixes.

**Note on the API fallback read.** The live API reads its fallback from
`SCORECARD_CRON_RESULTS_BUCKET_URL`. For the API to find what the cron
worker writes under the `api-results/` prefix, the API's env must point
at the same bucket+prefix. That wiring is in `deploy-kyma-cron`'s apply
step, not this code change.

## Scope boundary

- **Minimal, single-file code change** plus tests. No change to the
  pubsub path, the controller, or the SQS/GCP deployments (a URL with no
  `prefix` is unchanged).
- **No new capability behaviour** — this is an internal plumbing option.
  No `specs/` delta.

## Impact

- **`cron/data`** gains prefix-aware bucket opening. `make test` must
  stay green; the change is additive and guarded by "no prefix = old
  behaviour."
- **Unblocks** `deploy-kyma-cron`'s apply: one Object Store instance
  serves the whole pipeline (golden rules satisfied).
