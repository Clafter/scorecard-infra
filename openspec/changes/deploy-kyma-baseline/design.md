# Design: deploy-kyma-baseline

Decision records for the BTP/Kyma baseline. The records are numbered **K1**
onwards and mirror the style of `provision-aws` (`A1`–`A16`): each one is a
real choice between realistic alternatives, not a restatement of the plan.

## K1 — IaC strategy: BTP Terraform provider for the account plane, Kubernetes manifests for the workload plane

**Decision.** Service instances, service bindings, and anything that touches
the subaccount are declared with the Terraform BTP provider. The Kyma
namespace, resource quota, and the Kubernetes secrets that mirror the
bindings are declared with the Kubernetes Terraform provider in the same
root module. Workloads (`Deployment`, `APIRule`, `CronJob`) stay out of
Terraform entirely — they ship as plain Kubernetes manifests under
`deploy/btp/manifests/` and are applied by `kubectl` from CI.

**Alternatives considered.**

1. **Everything in Kubernetes (BTP Service Operator).** The operator reconciles
   `ServiceInstance` / `ServiceBinding` CRDs against the subaccount and
   generates the secrets directly. Appealing because the whole stack would be
   one `kubectl apply`.
   - Rejected because the account plane is **shared across workloads** (API
     and cron both consume the same XSUAA, the same buckets). Putting
     service instances in a workload namespace makes the lifecycle of a
     cluster-wide resource dependent on a namespace, and tearing down the
     namespace loses the account state.
2. **Everything in Terraform (Kubernetes provider owns Deployments too).**
   Rejected for the opposite reason: Deployments change on every image build,
   state churn would be enormous, and `kubectl rollout` ergonomics are what
   CI already knows.

The split keeps the slow-changing plane in state and the fast-changing plane
out of it. The AWS deploy made the same split (`modules/service` carries task
definitions, not container digests).

## K2 — Scope limited to a single dev environment in `eu10`

**Decision.** One root module, `environments/dev/`. No `staging/`,
`production/`, or `workspaces`.

**Why not three environments like AWS?** The AWS tree pays for three because
the service is live and the rehearsal environment must behave like
production. This change is pre-cutover: nothing is live on BTP yet. Shipping
dev-only now, and promoting to staging and production as separate changes
later, keeps the number of simultaneously-moving pieces honest.

**Workspace alternative rejected** for the same reason the AWS tree rejected
it (**A6**): a workspace share one backend, which makes it possible to apply
to the wrong environment while believing you are in dev. Separate root
modules per environment is the convention here.

## K3 — Terraform state lives in a Kyma-provisioned S3-compatible bucket, not in BTP's own storage

**Decision.** Create one bucket in the Object Store service instance,
dedicated to Terraform state, and point the Terraform S3 backend at it.
Access via the binding's HMAC credentials.

**Alternatives.**

1. **Local state only.** Dev-scale, one operator, maybe fine. Rejected
   because the baseline is meant to be reproducible by anyone with the
   kubeconfig, and local state means losing the laptop loses the state.
2. **BTP Credential Store or Document Store.** Not an object store with the
   lock semantics Terraform needs. Rejected.
3. **A pre-existing company S3 / Nexus / Artifactory.** Would couple this
   repository to internal infrastructure we deliberately kept out of the
   cloud-agnostic path (AGENTS.md: *"cloud-agnostic. No hardcoded bucket
   URLs; configuration comes from the environment."*).

The dedicated state bucket is created in `bootstrap/` with local state, same
chicken-and-egg move as the AWS tree, and the main root module uses it from
the first `init` onwards.

## K4 — Two Object Store instances, not one with two buckets

**Decision.** Provision **two** Object Store service instances, each with
its own binding and Kubernetes secret: one for the cron pipeline's results
(`cron-results`) and one for the API's uploaded results (`api-results`).
This matches the upstream `api/README.md` contract
(`SCORECARD_RESULTS_BUCKET_URL` and `SCORECARD_CRON_RESULTS_BUCKET_URL`,
different buckets by design).

**Alternatives.**

1. **One instance, two bucket paths.** The S3 driver in `gocloud.dev/blob`
   supports path-based separation, but the service instance **is** the IAM
   boundary on BTP — same instance means same credentials, which means a
   compromised API pod could write into the cron pipeline's producer bucket
   and silently corrupt a weekly scan. The upstream model separates the
   two for exactly this reason.
2. **Two bucket entitlements of plan `standard`.** Rejected on cost for a
   dev environment; `s3-standard` with quota 2 is already in the dev
   subaccount's entitlements.

A third bucket for Terraform state (K3) is a separate instance again, same
reasoning. Total: three Object Store instances inside the dev subaccount's
quota of 2 — **this is why the proposal flags Object Store quota 2 as the
tightest constraint and K3 reuses the state bucket for both planes.**

**Correction (K4').** With quota 2, the two serving buckets exhaust the
budget. The Terraform state bucket (K3) is therefore provisioned as a
subdirectory inside one of the two instances — picking `api-results` for
state because it is the smaller workload. If the entitlement is raised
later, state moves to its own instance in a follow-up change.

## K5 — XSUAA instance in `application` plan, two scopes

**Decision.** One XSUAA service instance, plan `application`, with two
scopes: `scorecard.admin` (write access to POST uploads) and `scorecard.read`
(public GET). Role templates map each scope to a role; role collections
compose them for humans. CI service accounts use the `client_credentials`
grant with `scorecard.admin`.

**Alternative** (one scope, authenticated-equals-admin): rejected because
the API's public GET endpoint has no auth today and must stay reachable
without a token. See the upstream `api/README.md` on the `/badge` and
`GET /projects/*` paths. The two-scope split keeps the public read path
public and the write path authenticated, same as the AWS deployment plans
to.

## K6 — Service bindings surface as Kubernetes secrets inside the workload namespace

**Decision.** For every service instance (XSUAA, both Object Stores, IAS,
Credential Store), the Terraform root creates a `btp_subaccount_service_binding`
and projects it as a `kubernetes_secret` into the Kyma workload namespace.
The follow-up change (`deploy-kyma-api`) consumes those secret names via
`envFrom` in its Deployment.

**Alternative** (binding → `btp_subaccount_service_credentials` → environment
variables straight into the Deployment): rejected because the Deployment
belongs to a different change and must not reach back into the baseline's
state. Secrets are a Kubernetes-native handoff and survive the Deployment
being recreated.

**Secret rotation.** The binding, not the secret, is the source of truth.
Rotating credentials is `tofu taint && tofu apply` on the binding resource;
the secret follows. The Kubernetes secret has `ignore_changes = [data]`
left out on purpose: Terraform **should** overwrite a drifted secret.

## K7 — Namespace resources: quota yes, network policy later

**Decision.** The namespace carries a `ResourceQuota` sized for the dev
workloads (small). It does **not** carry `NetworkPolicy` yet; Kyma's
default Istio sidecar is doing the ingress gating, and adding a
`NetworkPolicy` without a matching policy for egress to the Object Store
S3 endpoint would break the API on first request.

Network policy is deferred to `deploy-kyma-api`, which is the first change
that knows the egress surface (S3 endpoint, IAS token endpoint, GitHub).

## K8 — Alert Notification and Cloud Logging bind at the baseline, consumers subscribe later

**Decision.** The Alert Notification (`free` plan) and Cloud Logging (`dev`
plan) instances are created here and bound; the Kubernetes secrets exist
and are empty of consumers. `deploy-kyma-api` and `deploy-kyma-cron` will
wire pod logging to Cloud Logging and emit events into Alert Notification.

Rationale: both services have quota 1 and are shared across workloads, so
the account plane owns them. Creating them lazily inside each workload
change would race if the two changes land close together.

## K9 — The `btp` provider's auth is a short-lived user token, not a long-lived credential

**Decision.** The Terraform BTP provider authenticates with the operator's
own `btp login` session (environment variables sourced from
`btp get subaccount-env`). No service user is created for Terraform.

**Why not a technical user?** Dev-only, one operator, and SAP's technical
user plans for BTP cost an entitlement and a role assignment we have not
budgeted. When this change promotes to staging, a technical user becomes
appropriate and gets its own decision record.

## K10 — No OpenSpec `specs/` delta

**Decision.** This change introduces infrastructure, not a product
capability. No new or changed capability belongs in `openspec/specs/`. The
closest existing capability — `aws-serving` — stays unchanged; a future
`btp-serving` capability will be born when a Deployment actually serves
traffic from BTP (that is `deploy-kyma-api`, not this change).

Same precedent as `rename-api-binary` (**R5**).

## Open questions (resolve during tasks or in follow-ups)

- **Which Kyma plane logs land where?** Decision deferred to
  `deploy-kyma-api` (**K8** covers the service plumbing; the choice of
  which log streams to subscribe is a workload concern).
- **Is a BTP Destination needed for anything in this baseline?** Current
  answer: no (public GitHub, no on-prem). The entitlement is retained in
  case `deploy-kyma-cron` needs it for a GHE webhook later.
- **Credential Store `proxy` plan — do we actually need it in dev?**
  Entitlement is there; this change provisions the instance but does not
  bind it to anything. If `deploy-kyma-api` ends up using it for the Fastly
  purge token (currently in AWS Secrets Manager), the binding lands there.
  If not, this instance should be removed in a cleanup change.
