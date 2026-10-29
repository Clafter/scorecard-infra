# Proposal: Provision a Kyma baseline on SAP BTP for a dev deployment

## Why

The repository currently has one deployment story (AWS, under `deploy/api/` and
`deploy/cron/`). SAP evaluates Scorecard as an internal security scanner, and
the hosting target is **SAP BTP**, not AWS directly. Before the application
plane (deployment, APIRule, cron jobs) can land, the account plane has to
exist and be reproducible: entitlements turned into service instances, service
bindings turned into Kubernetes secrets, and a namespace the workloads can use.

This change provisions that **baseline only** — the pieces a workload consumes
but does not own. It is explicitly scoped to a **single dev subaccount** on
AWS Frankfurt (`eu10`); staging and production are later changes and will not
share this change's state.

Without the baseline, every follow-up change (`deploy-kyma-api`,
`deploy-kyma-cron`) would have to repeat the service-instance wiring in its
own manifests, and credentials would have to be pasted by hand into
Kubernetes secrets on every rebuild. Both are failure modes the AWS plane
already paid to avoid.

## What changes

A new `deploy/btp/` tree, parallel to `deploy/api/` and `deploy/cron/`:

```text
deploy/btp/
  README.md                   usage, prerequisites, apply order
  bootstrap/                  one-time: state bucket for the Terraform state
    main.tf, variables.tf, outputs.tf
  environments/
    dev/                      the only environment this change delivers
      main.tf                 the BTP + Kubernetes providers wired together
      variables.tf            subaccount id, region, cluster kubeconfig path
      outputs.tf              namespace name, bound-secret names
      modules.tf              composition of the modules below
  modules/
    services/                 BTP service instances (XSUAA, Object Store x2,
                              IAS, Credential Store, Alert Notification,
                              Cloud Logging)
    bindings/                 service bindings → Kubernetes secrets, one per
                              instance
    namespace/                the Kyma namespace, labels, resource quota
```

Decisions and rationale go in `design.md` (records **K1**–**Kn**). The
step-by-step work list is in `tasks.md`.

Scope boundary (what this change does **not** do):

- **No Deployment / APIRule / HTTPRoute / CronJob** — those are
  `deploy-kyma-api` and `deploy-kyma-cron`.
- **No container image publishing changes** — `publish-images.yml` already
  produces the images the follow-up changes will reference.
- **No DNS, no custom domain** — the dev deployment uses the Kyma-provided
  `*.<cluster>.kyma.ondemand.com` hostname (user decision, previous session).
- **No staging / production** — a dev subaccount is enough to prove the shape
  of the thing. Promotion to higher environments is a separate change.
- **No AWS teardown** — the AWS plane is untouched. `deploy/api/` and
  `deploy/cron/` continue to describe production until a cutover is proposed
  separately.

## Prerequisites

- Entitlements already assigned to the dev subaccount (verified in previous
  session): Kyma runtime `aws`, Object Store `s3-standard` quota 2, Cloud
  Logging `dev`, Alert Notification `free`, XSUAA, IAS, Credential Store
  `proxy`, Destination `lite`.
- Kyma cluster already provisioned on the subaccount; kubeconfig fetched to
  the operator's machine.
- `btp` CLI logged in; Terraform BTP provider authenticated with the same
  account (same credentials, different surface).

## Impact

- **New code / IaC:** `deploy/btp/` (new tree, Terraform roots and modules).
- **No code changes** in `api/`, `cron/`, `internal/`. Not even bucket URL
  defaults: those are already environment-driven (`configure-result-buckets`,
  archived 2026-08-30), and the baseline change will emit them as outputs,
  not bake them into code.
- **Developer workflow:** engineers who want to deploy to the dev subaccount
  run `tofu init && tofu apply` in `deploy/btp/environments/dev/`, then
  `kubectl --kubeconfig …` against the resulting namespace.
- **CI impact:** none in this change. `deploy-kyma-api` will introduce the
  `kubectl apply` step; the baseline is deliberately applied by a human from
  a workstation (same model as `deploy/api/bootstrap/`).
