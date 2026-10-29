# Tasks

## 1. Controller reordering

- [x] 1.1 Materialise the request stream into shards up front
      (`buildShards` returns `[]*data.ScorecardBatchRequest`), so the count
      is known before any publish.
- [x] 1.2 Write the `.shard_metadata` marker to the result-data bucket
      (and the raw bucket when configured) using that count, BEFORE
      publishing.
- [x] 1.3 Publish the materialised shards; close the publisher
      (`publishShards`).
- [x] 1.4 Metadata ShardLoc/NumShard/CommitSha fields unchanged.

## 2. Tests

- [x] 2.1 Unit test: `TestMetadataWrittenBeforePublish` locks the write
      -before-publish order; `TestBuildShards` covers sharding/counts.
- [x] 2.2 `make test` (`-race`) green across the repo.

## 3. Verification (live)

- [ ] 3.1 Rebuild + publish the controller image (CI).
- [ ] 3.2 Trigger a one-repo run (github.com/expressjs/express); the
      worker consumes without nacking.
- [ ] 3.3 A scorecard result for expressjs/express lands in the bucket
      under the api-results prefix.
- [ ] 3.4 The live API serves that result over the BTP endpoint.

## 4. Docs

- [x] 4.1 Divergence + reason noted in `cron/README.md`.
- [ ] 4.2 Update `deploy/btp/DEPLOYMENT.md` with the final working state.
