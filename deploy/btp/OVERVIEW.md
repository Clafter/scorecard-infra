# Scorecard on SAP BTP — Deployment Overview (dev)

> Status: **live and working** on the dev Kyma cluster as of 2026-10-29.
> A full scan of `github.com/expressjs/express` is reachable over the
> public BTP endpoint (HTTP 200, score 7.9, 14 checks, engine
> scorecard v5.6.0).
>
> This document is the human-readable map of the deployment. The exact
> apply steps live in [`DEPLOYMENT.md`](DEPLOYMENT.md); the design
> decisions live under `openspec/changes/`.

---

## 1. What this is

A fork of `ossf/scorecard`'s infrastructure (the results **API** and the
batch **cron** pipeline) deployed to **SAP BTP / Kyma** (AWS Frankfurt,
`eu10`) instead of the upstream AWS/GCP target. Everything runs inside a
single Kyma namespace, `scorecard-infra-dev`, provisioned through the BTP
Service Operator — no AWS or GCP account of our own, no Terraform for the
services, no BigQuery.

Guiding rules (the "golden rules" this deployment was built under):
1. Everything on BTP — no AWS/GCP of our own.
2. Minimal, without losing features.
3. New entitlements only when truly unavoidable.
4. Don't stop until a real blocker (then ask).
5. Everything reproducible and documented.

---

## 2. What was done (chronological summary)

1. **Baseline** — namespace, ResourceQuota, and BTP service instances +
   bindings provisioned as Kyma CRs (`deploy-kyma-baseline`).
2. **API binary rename** — `scorecard-webapp` → `scorecard-api`
   (`rename-api-binary`, shipped).
3. **API workload** — `scorecard-api` Deployment + Service + APIRule
   (public HTTPS) + NetworkPolicy (`deploy-kyma-api`).
4. **Cloud-agnostic storage** — the engine already uses
   `gocloud.dev/blob`; wired to BTP Object Store (S3) via the bindings.
5. **NATS queue** — replaced the upstream AWS SQS / GCP Pub/Sub with an
   in-cluster NATS broker as the cron message queue
   (`add-nats-pubsub-driver`), so the pipeline needs no cloud queue.
6. **Single-bucket prefixes (KC7)** — Object Store gives one hard-scoped
   credential per bucket; the cron worker needs several logical buckets.
   Solved by putting every role under one bucket at a distinct key
   prefix (`cron-bucket-prefix`), driven by the `?prefix=` query param
   the gocloud blob driver already honours natively.
7. **Cron pipeline deployed** — controller CronJob + worker Deployment +
   NATS + config, all on BTP (`deploy-kyma-cron`).
8. **Dropped-scan root cause fixed** — see §4
   (`controller-metadata-before-publish`).
9. **End-to-end verified** — express scan reachable over the endpoint.

---

## 3. What had to change for BTP compatibility

| Area | Upstream (AWS/GCP) | BTP change | Where |
|---|---|---|---|
| Message queue | AWS SQS / GCP Pub/Sub | In-cluster **NATS** via gocloud `natspubsub` | `add-nats-pubsub-driver`, `cron-nats.yaml` |
| Bucket model | one bucket per role | **one bucket, key prefixes** (`?prefix=role/`) — Object Store isolates one credential per bucket | `cron-bucket-prefix`, `cron/data/blob.go` |
| Controller order | publish, then write `.shard_metadata` (relies on queue redelivery) | **write metadata, then publish** — NATS is at-most-once, a nack is a no-op and the request would be lost | `controller-metadata-before-publish` |
| API ↔ worker results | separate buckets | **one shared results bucket + credential** (worker writes `api-results/…`, API reads the same bucket+prefix) — a cross-bucket read 403s under hard credential isolation | `api-deployment.yaml` |
| NATS + Istio mTLS | n/a | NATS **kept out of the Istio mesh** + `DestinationRule DISABLE` for clients; NATS "speaks first" deadlocks against sidecar mTLS | `cron-nats-istio.yaml`, `cron-nats.yaml` |
| ResourceQuota | n/a | every container must set **both requests and limits** for cpu+memory (quota rule); quota raised to fit the Istio sidecar overhead (~1 CPU/1Gi per injected pod) | `namespace.yaml`, worker/controller manifests |
| BigQuery transfer | `bq-transfer` job | **removed** — no BigQuery on BTP; the worker writes the per-repo `results.json` directly | (no manifest) |
| Deploy tooling | Terraform/gcloud | BTP services as **Kyma `ServiceInstance`/`ServiceBinding` CRs**, workloads as plain manifests, `kubectl apply -k` | `deploy/btp/dev/` |

No schema change was made (that would break the upstream contract that
`schema_gen_test.go` guards). The NATS and prefix changes are additive:
a URL without `?prefix=` and an SQS/GCP deployment behave exactly as
before, so upstream equivalence holds off BTP.

---

## 4. The dropped-scan bug (why no scan ran at first)

The worker refuses a scan request whose `.shard_metadata` marker is not
yet in the bucket, **nacks** it, and relies on the queue redelivering it
once the marker lands. Upstream that is safe — SQS/GCP redeliver.

Core NATS (the only mode the gocloud `natspubsub` v0.46 driver speaks —
even its `UseV2` path is `SubscribeSync`, not JetStream) is
**at-most-once**: a nack is a documented no-op, so the request was
dropped forever and no scan ever ran.

**Fix:** the controller now writes the marker(s) **before** publishing,
so a received request always finds its marker and the nack never trips.
SQS/GCP behaviour is unchanged (marker present before the worker acts
either way); the nack stays as a backstop for transports that redeliver.
Scoped divergence, documented in `cron/README.md`.

---

## 5. API — what you can reach and how

Public base URL (APIRule, HTTPS via the Kyma/Istio ingress):

```
https://scorecard-api.bfcc835.kyma.ondemand.com
```

| Method & path | What it does | Auth |
|---|---|---|
| `GET /projects/{platform}/{org}/{repo}` | Returns the repo's ScorecardResult JSON (score + checks). `?commit=<sha>` for a pinned commit. | public (read) |
| `GET /projects/{platform}/{org}/{repo}/badge` | Returns the SVG score badge. | public (read) |
| `POST /projects/{platform}/{org}/{repo}` | Publishes a result; the caller proves identity with an **OIDC-verified** GitHub Actions token. | OIDC (CI) |
| `GET /version` | Build info of the running server. | public |

Example (verified working):

```sh
curl https://scorecard-api.bfcc835.kyma.ondemand.com/projects/github.com/expressjs/express
# -> HTTP 200, {"score":7.9,"checks":[...14...],"scorecard":{"version":"v5.6.0"},...}
```

A score is a **signal, not a verdict**: `-1` means inconclusive, never
"insecure".

### Read path (how a GET finds a result)

1. The API reads the **primary** results bucket for
   `host/org/repo/results.json` (or `.../<commit>/results.json`).
2. If absent, it falls back to the **cron** results bucket.
3. In this deployment both point at the **same** bucket+prefix
   (`…?prefix=api-results/`) with the same credential, so CI-published
   results (POST) and cron-produced results coexist and are both found.

---

## 6. Caching

Every GET response already carries cache headers (set by the API
handlers, verified live through the Kyma ingress):

- `Cache-Control: max-age=600` — browsers/clients may reuse a result for
  **10 minutes**.
- `Surrogate-Control: max-age=31557600` + `Surrogate-Key` — directives
  for a CDN/edge cache (Fastly-style) to hold the object for a year and
  purge it by key.

So read caching is **built in** at the HTTP layer; a result is cacheable
the moment it is served. What is *not* wired on BTP:

- **CDN purge on write (disabled).** Upstream, after writing a result the
  worker asks Fastly to purge the stale copy (`FASTLY_PURGE_TOKEN`).
  There is no Fastly here, so the worker logs
  `API result CDN purging disabled, FASTLY_PURGE_TOKEN not set` and skips
  it — harmless, because no CDN is in front yet. The `Surrogate-*`
  headers mean a CDN can be dropped in later with no code change.
- **No server-side result cache.** The API has no in-memory cache in
  front of Object Store; each uncached GET reads the object (the API
  does keep the blob *bucket handle* open across requests — just the
  connection, not the data). A result changes only when a cron run or a
  CI POST overwrites `results.json`, so reads are always current.

---

## 7. Workers — what the cron pipeline does

```
CronJob controller ──(NATS publish)──▶ NATS broker ──(pull)──▶ worker Deployment
        │                                                          │
        └── writes .shard_metadata to the bucket FIRST             ├─ runs Scorecard per repo
                                                                   ├─ writes result-data/<run>/shard-N  (canonical shard)
                                                                   ├─ writes raw/<run>/shard-N          (raw)
                                                                   └─ writes api-results/host/org/repo/results.json
                                                                       (+ commit-pinned + raw.json)  ◀── the API reads this
```

- **Controller** (`scorecard-batch-controller`, CronJob, Mondays 02:00
  UTC): reads the project list, splits it into shards, **writes the run's
  `.shard_metadata` marker, then publishes** one request per shard to
  NATS. Short-lived; exits when done.
- **NATS** (`scorecard-nats`, Deployment): the in-cluster message queue
  between controller and worker. Kept out of the Istio mesh so its
  "server speaks first" handshake isn't broken by sidecar mTLS.
- **Worker** (`scorecard-batch-worker`, Deployment, long-running):
  subscribes to NATS, and for each request runs the Scorecard engine
  against every repo in the shard, then writes the shard result, the raw
  result, and the **per-repo `api-results/.../results.json`** the API
  serves. Needs a GitHub token (`github` secret, user-provided) to query
  repos. A request without its metadata marker is nacked (backstop). It
  serves `net/http/pprof` on `:8080`; a **TCP liveness probe** on that
  port restarts the pod if its HTTP loop deadlocks.

To trigger a run without waiting for Monday:

```sh
kubectl -n scorecard-infra-dev create job \
  --from=cronjob/scorecard-batch-controller cron-manual-$(date +%s)
```

---

## 8. BTP products in use

| Product (service / plan) | Role in this deployment |
|---|---|
| **Kyma runtime** | The managed Kubernetes cluster everything runs on. Provides the Istio ingress (APIRule → public HTTPS) and the BTP Service Operator. |
| **Object Store** (`objectstore` / `s3-standard`) ×2 | S3-backed result storage. Two instances provisioned (`scorecard-api-results`, `scorecard-cron-results`); the live path uses the cron one as the single shared results bucket (key prefixes per role). Reached through `gocloud.dev/blob` with the binding's credentials. |
| **Authorization & Trust Management** (`xsuaa` / `application`) | OAuth scopes (`scorecard.admin`, `scorecard.read`) for the API — `client_credentials` for CI, the human/OIDC path deferred (see §9). |
| **Alert Notification** (`alert-notification` / `free`) | Baseline alert channel for the namespace (provisioned; wiring to alerts is future work). |
| **Cloud Logging** (`cloud-logging` / `dev`) | Central log sink for the workloads (provisioned baseline). |
| **BTP Service Operator** (`btp-manager`, kyma-system) | Turns the `ServiceInstance`/`ServiceBinding` CRs above into real BTP services and writes each binding into a same-named Opaque secret. This is how services are provisioned — **not** Terraform or the btp CLI. |

Each binding writes a Kubernetes secret (`<name>-binding`) that the
workloads consume via `secretKeyRef`.

---

## 9. Open / future work

**Recently hardened (this round, no user action needed):**
- Worker now has a TCP **liveness probe** on `:8080` — a deadlocked
  worker is restarted instead of silently consuming nothing.
- Controller CronJob gained `activeDeadlineSeconds: 1800` (a run can't
  hang forever holding the `Forbid` lock), `startingDeadlineSeconds: 3600`
  (a missed window still starts within the hour), and job history limits.
- Confirmed read caching is already in place (§6) — `Cache-Control` +
  `Surrogate-*` headers on every GET, verified live.

**Still open (each needs a decision, a credential, or a dependency bump
— not doable unattended):**
- **IAS / human login (OIDC `authorization_code`).** Deferred — needs an
  OIDC business-user trust configured in the BTP cockpit. CI's
  `client_credentials` path works; interactive human login does not yet.
- **Per-bucket credentials (KA4).** Dev consolidated API + worker onto
  **one** results bucket + credential to exchange results. Production
  should restore separate api-results and cron-results buckets and give
  the API a read credential for each — a real engine change, since
  `blob.OpenBucket` is process-wide today (one keypair per process).
- **NATS durability.** Core NATS is at-most-once: if the controller
  publishes while the worker is between `SynchronousPull` calls, that
  request is missed (observed — the weekly rerun covers it). JetStream
  (durable, redelivering) removes both this gap and the
  metadata-ordering workaround, but needs a JetStream-capable gocloud
  driver (a `gocloud.dev` bump) — too invasive to do unattended.
- **Cloud Logging wiring.** The `cloud-logging` instance and its
  `ingest-mtls-*` binding exist, but nothing ships pod logs to it: the
  Kyma **Telemetry module is not installed** (only btp-operator,
  api-gateway, istio are), and a privileged log-collector DaemonSet would
  fight Kyma PodSecurity. Enabling the Telemetry module + a `LogPipeline`
  is a cluster-level change for the cockpit/module owner.
- **Alert Notification wiring.** Instance exists; alert rules not yet
  configured.
- **Custom domain / TLS.** Using the default `*.kyma.ondemand.com` host.
- **Prod scale.** Dev runs 1 worker replica, a modest quota, and a
  weekly schedule. Prod sizing (replicas, shard size, schedule) is
  untuned.
- **`scorecard-webapp` binary name.** The API binary was renamed to
  `scorecard-api`; the deployed container/image name cleanup is its own
  change.
