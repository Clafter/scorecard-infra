# Proposal: Deploy the Scorecard results API to Kyma (dev)

## Why

`deploy-kyma-baseline` provisioned the account plane — namespace,
XSUAA, two Object Store buckets, Alert Notification, Cloud Logging, and
the Kubernetes secrets their bindings produced. Nothing consumes them
yet. This change puts the first workload on the cluster: the Scorecard
results API (`scorecard-api`), serving read and upload traffic from the
dev subaccount.

It is the change that turns "the baseline exists" into "the API answers
a request."

## What changes

A new `deploy/btp/dev/` set of workload manifests, applied on top of the
baseline already in that directory:

```text
deploy/btp/dev/
  api-deployment.yaml     Deployment: scorecard-api, env wired from the
                          baseline's secrets
  api-service.yaml        Service: ClusterIP :8080
  api-apirule.yaml        APIRule (gateway.kyma-project.io/v2): public
                          GET via noAuth, POST via jwt against XSUAA
  api-networkpolicy.yaml  NetworkPolicy: the egress surface the baseline
                          deferred (K7)
  kustomization.yaml      extended to include the four files above
```

### The container image

`ghcr.io/clafter/scorecard-api`, pinned to an immutable
`sha-<commit>` tag (not the mutable `main` tag), so a redeploy is
reproducible and a registry change to `main` cannot silently alter what
runs. `publish-images.yml` builds and pushes this image from the fork on
every push to `main`.

### Bucket configuration (the central wiring problem)

The API reads two environment variables — `SCORECARD_RESULTS_BUCKET_URL`
and `SCORECARD_CRON_RESULTS_BUCKET_URL` — and opens each as a
`gocloud.dev/blob` bucket. The Object Store bindings created in the
baseline expose the parts (`bucket`, `region`, `access_key_id`,
`secret_access_key`, `host`) as separate secret keys, not as a single
URL. The BTP Object Store is backed by real AWS S3 in `eu-central-1`, so
the bucket URL has the form `s3://<bucket>?region=<region>&awssdk=v2`
and credentials resolve through the AWS SDK's default chain from
`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`.

The Deployment composes the URL from the secret keys using Kubernetes
env-var reference expansion (`$(BUCKET)`, `$(BUCKET_REGION)`) — no init
container, no transform script.

### Authentication

The API does **no** authentication of its own (verified: it reads no
JWT, checks no bearer token). Access control is enforced at the Istio
layer by the APIRule:

- **Public read** (`GET`) — `noAuth`. The `/badge` and `GET
  /projects/*` paths must stay reachable without a token, same contract
  as the AWS deployment.
- **Authenticated write** (`POST`) — `jwt`, validated against the XSUAA
  tenant's issuer and JWKS URL (both read from the `xsuaa-binding`
  secret). CI uploads present an XSUAA `client_credentials` token.

The human (browser, IAS `authorization_code`) login path is **not** in
this change — it needs an OIDC trust the baseline could not establish
and a tenant choice that is its own decision. It is a separate change.

## Scope boundary (what this change does **not** do)

- **No IAS / browser login** — deferred to a dedicated change.
- **No cron pipeline** — `scorecard-cron` workloads are
  `deploy-kyma-cron`.
- **No custom domain / DNS** — uses the Kyma-provided
  `scorecard-api.bfcc835.kyma.ondemand.com` host on the shared
  `kyma-system/kyma-gateway`.
- **No horizontal autoscaling / PodDisruptionBudget** — dev runs one
  replica; production concerns are a promotion change.
- **No AWS teardown** — the AWS plane is untouched.

## Prerequisites

- `deploy-kyma-baseline` applied and healthy (five service instances
  `Ready`, five binding secrets present in `scorecard-infra-dev`).
- `ghcr.io/clafter/scorecard-api` image present for the chosen SHA
  (verified: `publish-images` run for `92d24d8` succeeded).
- The cluster can pull from `ghcr.io/clafter/*`. If the fork's packages
  are private, an image pull secret is required (documented in the
  README; the image is public by default for a public fork).

## Impact

- **New IaC:** four workload manifests in `deploy/btp/dev/`.
- **No Go code changes.** The API already reads the env vars it needs;
  this change supplies them.
- **New capability spec:** this is the first change that serves traffic
  from BTP, so it introduces the `btp-serving` capability under
  `openspec/specs/` (the baseline deliberately did not — K10).
- **CI impact:** none required to apply; applying is a human `kubectl
  apply -k` against the cluster kubeconfig. A later change may add a
  CI-driven apply step.
