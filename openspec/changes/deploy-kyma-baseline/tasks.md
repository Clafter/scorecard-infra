# Tasks: deploy-kyma-baseline

Decisions and alternatives are in `design.md` (**K1**–**K10**). Rationale
for the overall change is in `proposal.md`.

## 1. Bootstrap: Terraform state bucket (K3, K4')

- [ ] 1.1 Create `deploy/btp/bootstrap/{main,variables,outputs}.tf`:
      one `btp_subaccount_service_instance` of Object Store `s3-standard`,
      named `scorecard-infra-state`, with its binding. Backend block
      starts commented out; the first apply runs with local state.
- [ ] 1.2 `deploy/btp/bootstrap/README.md`: document the one-time
      sequence (local apply → fill backend values → `init -migrate-state`
      → delete local state file), mirroring `deploy/api/bootstrap/`.
- [ ] 1.3 Verify: after bootstrap, the state object is listable in the
      Object Store instance; a second `tofu plan` in `bootstrap/` reads
      from the remote backend.

## 2. Services module (K4, K4', K5, K8, K9)

- [ ] 2.1 `deploy/btp/modules/services/main.tf`:
      - `xsuaa` (plan `application`): two scopes `scorecard.admin`,
        `scorecard.read`; two role templates; one default role collection
        named `scorecard-infra-dev`.
      - `object_store_api` (plan `s3-standard`): bucket for API uploads.
      - `object_store_cron` (plan `s3-standard`): bucket for cron
        producer output.
      - `ias` (plan `application`): trust configuration placeholder for
        the dev subaccount (consumers wired later).
      - `credential_store` (plan `proxy`): instance only; no secret
        shapes yet (K10 open question).
      - `alert_notification` (plan `free`): instance + default action
        set to NOOP (consumers come in `deploy-kyma-cron`).
      - `cloud_logging` (plan `dev`): instance only (consumers come in
        `deploy-kyma-api`).
- [ ] 2.2 Each resource has `lifecycle { ignore_changes = [parameters] }`
      only where the BTP provider is known to churn on empty-but-equal
      inputs; document any such carve-out inline.
- [ ] 2.3 `modules/services/outputs.tf`: expose each instance ID and
      plan name so the `bindings/` module can consume them without
      cross-module name mangling.

## 3. Bindings module (K6)

- [ ] 3.1 `deploy/btp/modules/bindings/main.tf`: one
      `btp_subaccount_service_binding` per instance from §2. Names:
      `scorecard-infra-dev-<instance>`.
- [ ] 3.2 Project each binding into a Kubernetes secret in the workload
      namespace via `kubernetes_secret_v1`, with
      `type = "Opaque"` and `data = jsondecode(binding.credentials)`.
- [ ] 3.3 Secrets land in the namespace from §4; the module takes
      `namespace` as an input, not a hardcoded value.
- [ ] 3.4 `outputs.tf`: emit a map of
      `{ instance_name → secret_name }` for the follow-up changes to
      consume via `terraform_remote_state`.

## 4. Namespace module (K7)

- [ ] 4.1 `deploy/btp/modules/namespace/main.tf`:
      - `kubernetes_namespace_v1` named `scorecard-infra-dev`.
      - Labels: `app.kubernetes.io/part-of=scorecard-infra`,
        `environment=dev`, `istio-injection=enabled`.
      - `kubernetes_resource_quota_v1`: pods 20, CPU request 2 /
        limit 4, memory request 4Gi / limit 8Gi (sized for the API +
        a few cron pods, revisit when the workload is in).
- [ ] 4.2 No `NetworkPolicy` here (K7); add a comment pointing at
      `deploy-kyma-api` as the owner of egress policy.
- [ ] 4.3 `outputs.tf`: expose the namespace name.

## 5. Environment root: `environments/dev/`

- [ ] 5.1 `deploy/btp/environments/dev/main.tf`: configure the BTP
      provider (`sap/btp`), the Kubernetes provider (points at the
      Kyma kubeconfig via `KUBECONFIG` env or an explicit var), and
      the `terraform` backend block (points at the bootstrap bucket).
- [ ] 5.2 `modules.tf`: wire `namespace` → `services` → `bindings`,
      passing the namespace name through.
- [ ] 5.3 `variables.tf`:
      - `subaccount_id` (required; no default)
      - `cluster_kubeconfig` (optional; defaults to `$KUBECONFIG`)
      - `btp_region` (default `eu10`)
      - `state_bucket_binding_secret` (required; the binding name from
        the bootstrap step)
- [ ] 5.4 `outputs.tf`: namespace name, map of secret names, service
      instance IDs. These are the handshake with `deploy-kyma-api` /
      `deploy-kyma-cron`.

## 6. Documentation

- [ ] 6.1 `deploy/btp/README.md`: prerequisites (entitlements already
      assigned, Kyma cluster, `btp login` done, kubeconfig fetched),
      apply order (`bootstrap/` once, `environments/dev/` to roll),
      operational notes (how to rotate a binding, how to add a new
      service instance, how to tear down cleanly).
- [ ] 6.2 Add a short section under `deploy/README.md` (if present) or
      create one, mapping each environment-directory to its target
      hyperscaler/platform so new readers see AWS vs. BTP at a glance.
- [ ] 6.3 Record the dev subaccount's URL and the chosen Kyma cluster
      hostname in the `deploy/btp/environments/dev/README.md` (not in
      `main.tf`; the hostname is an input to follow-up changes, not a
      source of truth here).

## 7. Verification

- [ ] 7.1 `tofu init && tofu validate` passes in `bootstrap/` and in
      `environments/dev/`.
- [ ] 7.2 `tofu plan` in `environments/dev/` lists exactly the resources
      enumerated in §§2-4; no unexpected drift.
- [ ] 7.3 `tofu apply` succeeds end-to-end; `kubectl get ns
      scorecard-infra-dev -o json` returns the labels from §4.1.
- [ ] 7.4 `kubectl -n scorecard-infra-dev get secrets` lists one secret
      per service binding; `kubectl -n ... describe secret
      scorecard-infra-dev-xsuaa` shows keys `clientid`, `clientsecret`,
      `url`.
- [ ] 7.5 A JWT fetched via `curl` against the XSUAA token URL from the
      binding, with the admin client credentials, decodes to a JWT
      containing scope `scorecard.admin`.
- [ ] 7.6 `make lint` and `make test` pass at repo root (should be
      no-ops: no Go code changed).
- [ ] 7.7 `openspec` directory lint (if the CLI is installed): proposal
      references all decision records that exist in `design.md`; tasks
      reference the decisions they implement.
