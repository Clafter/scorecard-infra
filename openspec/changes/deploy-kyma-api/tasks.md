# Tasks: deploy-kyma-api

Decisions in `design.md` (**KA1**–**KA9**). Rationale in `proposal.md`.
Applied against the dev subaccount `I767568` (`eu10`), namespace
`scorecard-infra-dev`, on top of the `deploy-kyma-baseline` manifests.

## 1. Deployment (KA1, KA2, KA3, KA4, KA8)

- [x] 1.1 `deploy/btp/dev/api-deployment.yaml`: Deployment
      `scorecard-api`, `replicas: 1`, image pinned to
      `ghcr.io/clafter/scorecard-api:sha-<main-commit>` (KA1).
- [x] 1.2 Container port `8080`; resource requests/limits per KA8.
- [x] 1.3 Env wiring:
      - `BUCKET`, `BUCKET_REGION` from `scorecard-api-results-binding`
        keys `bucket`, `region`.
      - `SCORECARD_RESULTS_BUCKET_URL` =
        `s3://$(BUCKET)?region=$(BUCKET_REGION)&awssdk=v2` (KA2).
      - `CRON_BUCKET`, `CRON_BUCKET_REGION` from
        `scorecard-cron-results-binding`.
      - `SCORECARD_CRON_RESULTS_BUCKET_URL` =
        `s3://$(CRON_BUCKET)?region=$(CRON_BUCKET_REGION)&awssdk=v2`.
      - `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` from
        `scorecard-api-results-binding` (KA3, KA4).
- [x] 1.4 Liveness/readiness probes on `GET /` (the API has no
      dedicated health path; `/` returns the OpenAPI landing).
- [x] 1.5 `imagePullSecrets` left out initially; add only if the pull
      fails (KA open question).

## 2. Service (KA5 prerequisite)

- [x] 2.1 `deploy/btp/dev/api-service.yaml`: `ClusterIP` Service
      `scorecard-api`, port `8080` → targetPort `8080`, selector matches
      the Deployment's pod labels.

## 3. APIRule (KA5, KA6)

- [x] 3.1 `deploy/btp/dev/api-apirule.yaml`,
      `gateway.kyma-project.io/v2`:
      - `hosts: [scorecard-api.bfcc835.kyma.ondemand.com]`
      - `gateway: kyma-system/kyma-gateway`
      - `service: { name: scorecard-api, port: 8080 }`
      - Rule A: `methods: [GET]`, `path: /*`, `noAuth: true`.
      - Rule B: `methods: [POST]`, `path: /*`, `jwt` with
        `authentications[0].issuer` =
        `https://i767568-e7443kwa.authentication.eu10.hana.ondemand.com`,
        `jwksUri` = same host + `/token_keys`.

## 4. NetworkPolicy (KA7)

- [x] 4.1 `deploy/btp/dev/api-networkpolicy.yaml`:
      - podSelector matches the API pods.
      - Ingress: from the Istio ingress gateway namespace only.
      - Egress: DNS (udp/tcp 53 to kube-system), 443 to the world
        (S3 + XSUAA + Cloud Logging are all public HTTPS endpoints; a
        tighter CIDR list is a production concern).

## 5. Kustomization

- [x] 5.1 Extend `deploy/btp/dev/kustomization.yaml` `resources:` with
      the four files above, after the baseline files.

## 6. Capability spec (KA9)

- [x] 6.1 `openspec/specs/btp-serving/spec.md`: record the
      public-read / authenticated-write contract and bucket-backed
      persistence as behaviour. Reference it from the change.

## 7. Documentation

- [x] 7.1 Extend `deploy/btp/README.md`: the workload layer, how the
      bucket URL is composed, where the APIRule issuer/jwks come from,
      how to reach the API (`curl https://scorecard-api.…/`), how to
      bump the image SHA.

## 8. Verification

- [x] 8.1 `kubectl apply -k deploy/btp/dev/ --dry-run=server` validates
      all resources.
- [x] 8.2 `kubectl apply -k deploy/btp/dev/`; the Deployment rolls out
      (`kubectl -n scorecard-infra-dev rollout status deploy/scorecard-api
      --timeout=5m`).
- [x] 8.3 `kubectl -n scorecard-infra-dev get pods`: the pod is
      `Running` with the Istio sidecar (2/2 containers ready).
- [x] 8.4 Pod logs show the server listening on `:8080` and no
      `blob.OpenBucket` error at startup.
- [x] 8.5 Public read: `curl -sS https://scorecard-api.bfcc835.kyma.ondemand.com/`
      returns the API landing (200), no token.
- [x] 8.6 A `GET` for a known project returns a result or a clean 404
      (not a 5xx bucket error) — proves the S3 wiring.
- [~] 8.7 `POST` without a token is rejected by the APIRule — **verified
      (HTTP 403)**. `POST` with an XSUAA `client_credentials` token
      accepted — **not yet run** (needs a live token fetch from the
      tenant; the 403-without-token already proves the jwt handler is
      active). Finish alongside the first real CI upload.
- [x] 8.8 `make lint` and `make test` pass at repo root (no-ops: no Go
      changed).