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

- [x] 3.1 Rebuilt + published the controller image (CI, publish-images
      run 40903028749 success).
- [x] 3.2 Triggered a one-repo run (github.com/expressjs/express); the
      worker consumed the request without nacking (log: "Running
      Scorecard for repo: github.com/expressjs/express").
- [x] 3.3 The scorecard result landed under
      api-results/github.com/expressjs/express/results.json (and the
      commit-pinned path) in the results bucket.
- [x] 3.4 The live API serves it over the BTP endpoint: GET
      /projects/github.com/expressjs/express -> HTTP 200, score 7.9,
      14 checks, engine scorecard v5.6.0. Commit-pinned GET also 200.

## 4. Docs

- [x] 4.1 Divergence + reason noted in `cron/README.md`.
- [x] 4.2 Updated `deploy/btp/DEPLOYMENT.md` + added
      `deploy/btp/OVERVIEW.md` with the final working state.
