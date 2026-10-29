# Proposal: Provision a Kyma baseline on SAP BTP for a dev deployment

## Why

The repository currently has one deployment story (AWS, under `deploy/api/`
and `deploy/cron/`). SAP evaluates Scorecard as an internal security
scanner, and the hosting target is **SAP BTP**, not AWS directly. Before
the application plane (Deployment, APIRule, CronJobs) can land, the
account plane has to exist and be reproducible: entitlements turned into
service instances, service bindings turned into Kubernetes secrets, and
a namespace the workloads can use.

This change provisions that **baseline only** — the pieces a workload
consumes but does not own. It is explicitly scoped to a **single dev
subaccount** on AWS Frankfurt (`eu10`); staging and production are later
changes and will not share this change's state.

Without the baseline, every follow-up change (`deploy-kyma-api`,
`deploy-kyma-cron`) would have to repeat the service-instance wiring in
its own manifests, and credentials would have to be pasted by hand into
Kubernetes secrets on every rebuild. Both are failure modes the AWS
plane already paid to avoid.

## What changes

A new `deploy/btp/` tree, parallel to `deploy/api/` and `deploy/cron/`:

```text
deploy/btp/
  README.md               prerequisites, apply order, verify commands
  dev/
    namespace.yaml        Kyma namespace, labels, ResourceQuota
    services.yaml         BTP ServiceInstance CRs (XSUAA, Object Store x2,
                          IAS, Credential Store, Alert Notification,
                          Cloud Logging)
    bindings.yaml         ServiceBinding CRs → Kubernetes secrets, one per
                          instance
    kustomization.yaml    (optional, keeps order deterministic)
```

Decisions and rationale are in `design.md` (records **K1**–**K10**). The
step-by-step work list is in `tasks.md`.

**Delivery method:** pure Kubernetes manifests, applied with `kubectl
apply -k deploy/btp/dev/`. The Kyma cluster already runs the BTP Service
Operator (`btp-manager` in `kyma-system`), which reconciles the CRs
against the subaccount's Service Manager. **No Terraform, no `btp` CLI
in the deploy path** (see **K1**).

Scope boundary (what this change does **not** do):

- **No Deployment / APIRule / HTTPRoute / CronJob** — those are
  `deploy-kyma-api` and `deploy-kyma-cron`.
- **No container image publishing changes** — `publish-images.yml`
  already produces the images the follow-up changes will reference.
- **No DNS, no custom domain** — the dev deployment uses the
  Kyma-provided `*.<cluster>.kyma.ondemand.com` hostname (user
  decision, previous session).
- **No staging / production** — a dev subaccount is enough to prove the
  shape of the thing. Promotion to higher environments is a separate
  change.
- **No AWS teardown** — the AWS plane is untouched. `deploy/api/` and
  `deploy/cron/` continue to describe production until a cutover is
  proposed separately.

## Prerequisites

- Entitlements assigned to the dev subaccount (verified live via
  `btp list accounts/entitlement --subaccount …`): Object Store
  `s3-standard` quota 2, Cloud Logging `dev`, Alert Notification
  `free`, XSUAA, IAS `application`, Credential Store `proxy`,
  Destination `lite`.
- Kyma Environment provisioned on the subaccount; kubeconfig fetched
  to the operator's machine; BTP Service Operator (`btp-manager`)
  running in `kyma-system` (verified: operator pods `Running`,
  `services.cloud.sap.com` CRDs installed).

## Impact

- **New code / IaC:** `deploy/btp/` (new tree, Kubernetes manifests
  only).
- **No code changes** in `api/`, `cron/`, `internal/`. Not even bucket
  URL defaults: those are already environment-driven
  (`configure-result-buckets`, archived 2026-08-30), and the follow-up
  deploy will reference the operator-generated secrets as env sources.
- **Developer workflow:** engineers who want to deploy to the dev
  subaccount run `kubectl apply -k deploy/btp/dev/` against the
  cluster kubeconfig from the cockpit, then verify with
  `kubectl -n scorecard-infra-dev get servicebindings`.
- **CI impact:** none in this change. `deploy-kyma-api` will introduce
  the `kubectl apply` step for workloads; the baseline is deliberately
  applied by a human from a workstation with the cockpit kubeconfig
  (same "bootstrap is a human action" model as `deploy/api/bootstrap/`).
