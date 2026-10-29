# Tasks: deploy-kyma-baseline

Decisions and alternatives are in `design.md` (**K1**–**K10**). Rationale
for the overall change is in `proposal.md`.

Applied against the dev subaccount `I767568` (`eu10`) on 2026-10-29. Five
of the seven originally-planned service instances are live; IAS and
Credential Store are deferred/removed with reasons recorded in §7.

## 1. Namespace manifest (K7)

- [x] 1.1 `deploy/btp/dev/namespace.yaml`:
      - `v1 Namespace` named `scorecard-infra-dev`.
      - Labels: `app.kubernetes.io/part-of=scorecard-infra`,
        `environment=dev`, `istio-injection=enabled`.
      - `v1 ResourceQuota` (`scorecard-infra-dev-quota`): pods 20,
        CPU request 2 / limit 4, memory request 4Gi / limit 8Gi.
- [x] 1.2 No `NetworkPolicy` here (K7); YAML comment points at
      `deploy-kyma-api` as the owner of egress policy.

## 2. Service instances manifest (K4, K5, K8)

- [x] 2.1 `deploy/btp/dev/services.yaml`, one
      `services.cloud.sap.com/v1 ServiceInstance` per live entry below,
      all in namespace `scorecard-infra-dev`:
      - [x] `xsuaa` (`serviceOfferingName: xsuaa`, `servicePlanName:
        application`): `parameters` embed the xs-security.json:
        two scopes `$XSAPPNAME.admin` + `$XSAPPNAME.read`, two role
        templates, one role collection `scorecard-infra-dev`.
        **Ready.**
      - [x] `scorecard-api-results` (`objectstore` / `s3-standard`).
        **Ready.**
      - [x] `scorecard-cron-results` (`objectstore` / `s3-standard`).
        **Ready.**
      - [x] `alert-notification` (`alert-notification` / `free`).
        **Ready.**
      - [x] `cloud-logging` (`cloud-logging` / `dev`). **Ready.**
      - [ ] ~~`ias` (`identity` / `application`)~~ — **deferred**,
        see §7.1.
      - [ ] ~~`credential-store` (`credstore` / `proxy`)~~ —
        **removed**, see §7.2.
- [x] 2.2 Each ServiceInstance has `externalName` set for operator-side
      identification; `btpAccessCredentialsSecret` left unset (defaults
      to the operator's own secret).

## 3. Service bindings manifest (K6)

- [x] 3.1 `deploy/btp/dev/bindings.yaml`: one
      `services.cloud.sap.com/v1 ServiceBinding` per live instance from
      §2, same namespace. Names: `<instance>-binding`.
- [x] 3.2 Each binding sets `secretName: <instance>-binding` so the
      operator writes credentials to a predictable Kubernetes secret.

## 4. Kustomization (ordering)

- [x] 4.1 `deploy/btp/dev/kustomization.yaml`: resources in the order
      `namespace.yaml`, `services.yaml`, `bindings.yaml`. Kustomize
      applies them in one batch; the operator handles the
      eventual-consistency between namespace→instance→binding.

## 5. Documentation

- [x] 5.1 `deploy/btp/README.md`: prerequisites, apply order, verify
      commands, rotation, teardown.
- [ ] 5.2 Add a short section under `deploy/README.md` (if present) or
      create one, mapping each environment-directory to its target
      hyperscaler/platform so new readers see AWS vs. BTP at a glance.

## 6. Verification

- [x] 6.1 `kubectl apply -k deploy/btp/dev/ --dry-run=server` validates
      against the live cluster (the namespace-first ordering needed a
      real namespace apply first — a known server-dry-run limitation
      for dependent resources, not a manifest defect).
- [x] 6.2 `kubectl apply -k deploy/btp/dev/`; every live
      `ServiceInstance` reaches `Ready=True`.
- [x] 6.3 `kubectl -n scorecard-infra-dev get servicebindings`:
      all five bindings `Ready=True`.
- [x] 6.4 `kubectl -n scorecard-infra-dev get secrets`: one secret per
      binding; `xsuaa-binding` carries keys `clientid`, `clientsecret`,
      `url`, `verificationkey`, `xsappname` (23 keys total).
- [ ] 6.5 A JWT fetched via `curl` against the XSUAA token URL from the
      binding, with the admin client credentials, decodes to a JWT
      containing scope `$XSAPPNAME.admin`. **Deferred to
      `deploy-kyma-api`** — no workload consumes XSUAA yet, so a
      token smoke-test here would be throwaway.
- [ ] 6.6 `make lint` and `make test` pass at repo root (should be
      no-ops: no Go code changed).
- [ ] 6.7 `openspec` directory lint (if the CLI is installed):
      proposal references all decision records that exist in
      `design.md`; tasks reference the decisions they implement.

## 7. Deferred / removed services (recorded for the follow-up changes)

- [ ] 7.1 **IAS deferred.** `identity / application` failed to
      provision with `OIDC Trust missing`: the subaccount has no
      OIDC business-user trust to an SAP Cloud Identity Services
      tenant, which the operator CR cannot establish. Establishing
      trust (`Security → Trust Configuration → Establish Trust`) is a
      one-time cockpit action requiring a tenant choice that belongs
      to `deploy-kyma-api` (the first change that cares who logs in as
      a human). CI uploads use XSUAA `client_credentials`, which is
      live, so IAS is not on the critical path for the baseline.
- [x] 7.2 **Credential Store removed.** `credstore / proxy` failed
      with `Mandatory parameters for service origin are not specified`.
      The `proxy` plan is a *consumer proxy* onto a shared original
      Credential Store (requires its service GUID); the subaccount has
      no non-proxy plan entitled to act as that origin, so `proxy` is
      not provisionable standalone. K9 flagged this instance as
      provisional ("remove in a cleanup if no workload needs it"); the
      cleanup is folded in here. If `deploy-kyma-api` needs a secret
      store beyond XSUAA, it raises the entitlement and reintroduces
      the instance with its own decision record.
