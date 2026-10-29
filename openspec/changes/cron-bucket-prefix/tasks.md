# Tasks: cron-bucket-prefix

Rationale in `proposal.md`. Resolves `deploy-kyma-cron` KC7 (golden-rule
option A: code change over a new entitlement). No `specs/` delta.

## 1. Code

- [x] 1.1 `cron/data/blob.go`: add an internal `openBucket(ctx, url)`
      helper that reads an optional `prefix` query parameter, strips it
      from the URL, opens the bucket, and wraps it with
      `blob.PrefixedBucket` when a prefix is present. No prefix =
      `blob.OpenBucket` behaviour unchanged.
- [x] 1.2 Route the five `blob.OpenBucket` call sites
      (`GetBlobKeysWithPrefix`, `GetBlobContent`, `BlobExists`,
      `WriteToBlobStore`) through `openBucket`.

## 2. Tests

- [x] 2.1 `blob_test.go` `TestOpenBucketPrefix`: writing the same key
      through two different prefixes does not collide; keys land under
      the prefix on disk (verified through an unprefixed handle); a
      no-prefix URL writes at the root; a prefixed read sees its key by
      the unprefixed name.
- [x] 2.2 No regression: the existing blob tests and the whole
      `cron/data` package still pass.

## 3. Verify

- [x] 3.1 `go build ./cron/data/` clean.
- [x] 3.2 `make test` (`go test ./... -race`) green across the repo.
- [x] 3.3 `gofmt -l cron/data/` clean, `go vet ./cron/data/` clean.
      (`golangci-lint` not installed locally; full lint on CI.)
- [ ] 3.4 CI `make lint` green on push. **Pending push.**

## 4. Downstream (not this change)

- [ ] 4.1 `deploy-kyma-cron`: point every bucket role at the one
      cron-results bucket under a distinct `?prefix=…`; the worker uses
      only the cron-results credentials. (Manifest change, separate
      branch.)
- [ ] 4.2 The live API's `SCORECARD_CRON_RESULTS_BUCKET_URL` must carry
      the matching `?prefix=api-results/` so its fallback read finds
      what the worker writes.
