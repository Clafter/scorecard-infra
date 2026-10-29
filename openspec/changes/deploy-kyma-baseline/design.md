# Design: deploy-kyma-baseline

Decision records for the BTP/Kyma baseline. The records are numbered **K1**
onwards and follow the style of `provision-aws` (`A1`–`A16`): each one is a
real choice between realistic alternatives, not a restatement of the plan.

## K1 — Pure Kubernetes manifests via the BTP Service Operator; no Terraform

**Decision.** Service instances and bindings are declared as
`services.cloud.sap.com/v1 ServiceInstance` and `ServiceBinding` custom
resources, applied with `kubectl` against the Kyma cluster. The BTP Service
Operator (`btp-manager`, present in `kyma-system` from the Kyma Environment
provisioning) reconciles them against the subaccount's Service Manager and
projects every binding's credentials into a `kubernetes.io/opaque` secret
in the workload namespace. The Kyma namespace, labels, and resource quota
are plain Kubernetes manifests in the same tree.

**Alternatives considered.**

1. **Terraform BTP provider + Kubernetes provider in one root module.** The
   original K1 (revised here). Rejected after the Kyma cluster was found
   to already run the BTP Service Operator — Terraform would duplicate
   what the operator does natively, double the state surface (Terraform
   state **and** cluster etcd), and add a second toolchain (`tofu`, `btp`
   CLI) to the deploy path that the operator makes unnecessary. The AWS
   tree uses Terraform because AWS has no in-cluster service broker; BTP
   via Kyma does.
2. **Terraform only for XSUAA, operator for everything else.** The XSUAA
   `application` plan takes rich `parameters` (xs-security.json: scopes,
   role templates, role collections). The operator's `ServiceInstance`
   CR supports the same `parameters` field verbatim, so there is no
   capability gap to work around. Rejected.
3. **`btp` CLI scripts invoked from CI.** Imperative, non-idempotent,
   state in whoever last ran the script. Rejected.

Trade-off accepted: `kubectl apply` cannot represent dependencies the
way Terraform's resource graph does. A `ServiceBinding` that references
a `ServiceInstance` not yet reconciled will loop with `TransientError`
until the instance is `Ready`. This is operator-native behaviour, not a
bug, and `kubectl wait` covers it in CI.

**Trade-off accepted (state).** Terraform state's audit trail (who
changed what, when) moves to Kubernetes events + git history of the
manifests. For a dev environment that is enough; a `staging`/`prod`
change may revisit this.

## K2 — Scope limited to a single dev environment in `eu10`

**Decision.** One directory, `deploy/btp/dev/`. No overlays, no
Kustomize bases shared with a yet-nonexistent staging. The subaccount
is `I767568` (`95c7cf5e-21b8-42c7-8bde-7e7c97f53aba`), region `eu10`,
Kyma cluster host `*.bfcc835.kyma.ondemand.com`.

Promotion to staging and production is a separate change. When it
happens, this directory becomes `base/` and `dev`/`staging`/`prod` sit
as overlays — but inventing that structure now, with no second
environment to pressure-test it, is premature.

## K3 — The Kyma cluster already exists; this change does not provision it

**Decision.** The Kyma Environment is provisioned once per subaccount
through the cockpit (Kyma Environment → Create). This repository treats
the cluster as a prerequisite, documented in `deploy/btp/README.md`,
not as a managed resource.

**Why not automate cluster creation?** Provisioning Kyma is a
45-minute, subaccount-wide, cockpit-level action that charges the
subaccount's Kyma runtime entitlement. Automating it means owning the
lifecycle of a thing the user and their colleagues share, and getting
the teardown story wrong once is more expensive than the automation
saves. Cockpit-first, manifests-second is the Kyma convention.

## K4 — Two Object Store instances, not one with two buckets

**Decision.** Two `ServiceInstance` resources of
`objectstore / s3-standard`: one named `scorecard-cron-results`, one
`scorecard-api-results`. Each has its own `ServiceBinding` and
therefore its own set of S3 credentials.

**Why two.** Upstream `api/README.md` splits them for IAM-boundary
reasons: a compromised API pod must not be able to write into the cron
pipeline's producer bucket. The service instance **is** the IAM
boundary on BTP (one HMAC credential pair per binding), so two buckets
= two instances. One instance with two bucket paths would share
credentials and defeat the point.

**Quota reality.** The subaccount entitlement is `s3-standard, 2`,
which exactly matches the two instances this change creates. **This
change exhausts the Object Store quota.** If a future change needs a
third bucket (e.g. a dedicated state store, or a log archive), the
entitlement must be raised first.

## K5 — XSUAA with two scopes

**Decision.** One `ServiceInstance` of `xsuaa / application`,
parameters embedded as an `xs-security.json`-shaped block. Two scopes:
`$XSAPPNAME.admin` (write access to POST uploads) and
`$XSAPPNAME.read` (public GET). Two role templates. One role
collection, `scorecard-infra-dev`, composing both roles — attached to
users via IAS group mapping in a follow-up change.

**Alternative** (one scope, authenticated-equals-admin): rejected
because the API's `GET /badge` and `GET /projects/*` paths are public
and must stay reachable without a token. Two-scope split keeps the
public read path public and the write path authenticated.

**Alternative** (OAuth client in Credential Store instead of XSUAA):
rejected because APIRule's `jwt` handler expects XSUAA-shaped tokens
with `scope` claims; Credential Store issues different token shapes
and would need a token-exchange hop.

## K6 — Service bindings surface as Kubernetes secrets in the workload namespace

**Decision.** Every `ServiceBinding` sets
`secretName: <instance>-binding` and lands in the workload namespace
`scorecard-infra-dev`. The follow-up change `deploy-kyma-api` consumes
these secret names via `envFrom.secretRef` in the API Deployment.

The operator writes the full binding's credentials JSON **and**
flattens each top-level key into its own secret entry (e.g. `clientid`,
`clientsecret`, `url` for XSUAA; `bucket`, `access_key_id`,
`secret_access_key`, `uri` for Object Store). Both forms are
available; `envFrom` consumes the flattened keys as env vars.

**Rotation.** The binding, not the secret, is the source of truth.
Rotating credentials is
`kubectl delete servicebinding <name> && kubectl apply -f …` on the
manifest; the secret is recreated from the new binding.

## K7 — Namespace carries labels and a ResourceQuota; no NetworkPolicy yet

**Decision.** `scorecard-infra-dev` namespace with labels
`app.kubernetes.io/part-of=scorecard-infra`, `environment=dev`, and
`istio-injection=enabled` (opts the namespace into Kyma's Istio mesh).
A `ResourceQuota` sized for the API + a handful of cron pods.

No `NetworkPolicy` here. The egress surface — S3 endpoints, IAS token
endpoint, GitHub — is only knowable once the first workload is in. The
`deploy-kyma-api` change is where the policy lands, together with the
Deployment that defines what it allows.

## K8 — Alert Notification and Cloud Logging bind at the baseline; consumers subscribe later

**Decision.** Both services (`alert-notification / free`,
`cloud-logging / dev`) are provisioned and bound here. The Kubernetes
secrets exist, consumers do not. `deploy-kyma-api` and
`deploy-kyma-cron` wire pod logs to the Cloud Logging endpoint and
emit events into Alert Notification using those secrets.

Alert Notification and Cloud Logging are shared across workloads, so
the baseline owns them. Creating them lazily inside each workload
change would race if two workload changes land close together.

## K9 — Credential Store removed from the baseline (was: provisioned but unbound)

**Decision.** The `credstore / proxy` instance is **not** part of the
baseline. It was attempted and removed.

**Why it could not be provisioned.** The `proxy` plan is a *consumer
proxy* onto a shared, already-existing Credential Store instance — it
takes a mandatory "service origin" (the GUID of the original instance)
and has no credential store of its own. The dev subaccount is entitled
to `credstore / proxy` only; no `free`/`standard` plan is entitled to
act as the origin the proxy would consume. A standalone `proxy`
instance therefore fails with `Mandatory parameters for service origin
are not specified`.

**Consequence.** If a later change needs a secret store for things
XSUAA cannot hold (Fastly purge token, GitHub App key for cron), it
raises the Credential Store entitlement to a non-proxy plan and
reintroduces the instance with its own decision record. Until then,
the baseline carries no Credential Store.

**This supersedes the original K9** ("provisioned but unbound"), which
assumed `proxy` was a standalone plan.

## K10 — No OpenSpec `specs/` delta

**Decision.** This change introduces infrastructure, not a product
capability. Same precedent as `rename-api-binary` (**R5**). A future
`btp-serving` capability spec is born in `deploy-kyma-api`, which is
the first change that actually serves traffic from BTP.

## Open questions (resolve during tasks or in follow-ups)

- **IAS trust configuration beyond provisioning.** The
  `identity / application` instance is created, but mapping an IAS
  group to the `scorecard-infra-dev` role collection is a cockpit
  action the operator CR does not express. Documented in
  `deploy/btp/README.md`; the actual mapping is a `deploy-kyma-api`
  concern (the first change that cares who logs in).
- **Alert Notification subscriptions.** The service exposes an API;
  the pattern (which events to subscribe to, which actions to fire)
  is a workload concern, deferred to `deploy-kyma-cron`.
- **Should the manifests be plain YAML or Kustomized?** Current
  choice: plain YAML in `deploy/btp/dev/`. When the second environment
  lands, Kustomize overlays are the obvious refactor.
