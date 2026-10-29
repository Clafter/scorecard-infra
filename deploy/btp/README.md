# deploy/btp — SAP BTP/Kyma deployment

The baseline for running Scorecard on SAP BTP's Kyma runtime. This
directory covers the **account plane** (service instances, bindings,
namespace) that the application plane (`deploy-kyma-api`,
`deploy-kyma-cron`, forthcoming) consumes.

Rationale and decision records are in
[`openspec/changes/deploy-kyma-baseline/`](../../openspec/changes/deploy-kyma-baseline/).

> **Scope:** this is the **dev** environment only, on the subaccount
> `I767568` (`eu10`, AWS Frankfurt). Staging and production are later
> changes.

## Prerequisites

1. **Kyma Environment provisioned** on the target subaccount. The
   environment is created via the BTP Cockpit (Overview → Enable Kyma)
   and takes ~45 minutes the first time. The cluster hostname has the
   shape `*.<shoot-id>.kyma.ondemand.com`.
2. **Entitlements assigned** to the subaccount. Verify with:
   ```sh
   btp list accounts/entitlement --subaccount <subaccount-id>
   ```
   Required: `xsuaa / application`, `objectstore / s3-standard` (quota
   ≥ 2), `alert-notification / free`, `cloud-logging / dev`. (IAS and
   Credential Store are not part of this baseline — see "Deferred
   services" below.)
3. **BTP Service Operator** installed in the cluster (default for Kyma
   Environments). Verify:
   ```sh
   kubectl get crd serviceinstances.services.cloud.sap.com \
                   servicebindings.services.cloud.sap.com
   ```
4. **Kubeconfig** downloaded from the cockpit (Kyma Environment →
   Download Kubeconfig). Export:
   ```sh
   export KUBECONFIG=~/.kube/scorecard-infra/kubeconfig-kyma-dev.yaml
   kubectl config current-context    # should point at *.kyma.ondemand.com
   ```
5. **`btp` CLI** logged in and targeted at the subaccount (optional
   for applying this baseline, required for subsequent inspection):
   ```sh
   btp login --sso
   btp target --subaccount <subaccount-id>
   ```

## Apply

```sh
# Dry-run against the live cluster (catches CRD field typos).
kubectl apply -k deploy/btp/dev/ --dry-run=server

# Real apply.
kubectl apply -k deploy/btp/dev/
```

The operator reconciles asynchronously; the first apply typically takes
2–10 minutes depending on the service (XSUAA is fast; Object Store
provisions an S3 bucket and takes the longest).

## Verify

```sh
# Every ServiceInstance Ready=True.
kubectl -n scorecard-infra-dev wait --for=condition=Ready \
  serviceinstance --all --timeout=15m

# Every ServiceBinding Ready=True.
kubectl -n scorecard-infra-dev wait --for=condition=Ready \
  servicebinding --all --timeout=15m

# Credentials materialized as secrets.
kubectl -n scorecard-infra-dev get secrets

# Inspect one (e.g. XSUAA — contains clientid, clientsecret, url).
kubectl -n scorecard-infra-dev get secret xsuaa-binding -o json \
  | jq '.data | keys'
```

## What is here

| File | What |
|---|---|
| `dev/namespace.yaml` | `scorecard-infra-dev` namespace + ResourceQuota. Istio-injection enabled. |
| `dev/services.yaml`  | Five `ServiceInstance` CRs (XSUAA, 2× Object Store, Alert Notification, Cloud Logging). |
| `dev/bindings.yaml`  | Five `ServiceBinding` CRs projecting credentials into same-named Kubernetes secrets. |
| `dev/kustomization.yaml` | Resource ordering + common labels. |

## Operational notes

### Rotating a binding's credentials

Delete and re-create the `ServiceBinding`; the operator rewrites the
Kubernetes secret with fresh credentials.

```sh
kubectl -n scorecard-infra-dev delete servicebinding xsuaa-binding
kubectl -n scorecard-infra-dev apply -f dev/bindings.yaml
```

Workloads consuming the secret via `envFrom.secretRef` restart
automatically only if the secret contents cause a pod-template hash
change — for a safe forced rotation, roll the consumer Deployment
after.

### Adding a new service instance

1. Append the `ServiceInstance` to `dev/services.yaml`.
2. Append the matching `ServiceBinding` to `dev/bindings.yaml`.
3. `kubectl apply -k dev/`.
4. Check `kubectl -n scorecard-infra-dev describe serviceinstance <name>`
   for `NotEntitled` events if the service does not provision — the
   entitlement must be present in the subaccount.

### Teardown

```sh
kubectl delete -k deploy/btp/dev/
```

The operator deprovisions service instances through the Service
Manager; order is handled by Kubernetes finalizers. Deleting the
namespace alone **does not** release the subaccount's service
instances; always use the kustomization.

## Deferred services (not in this baseline)

Two of the originally-planned instances are intentionally absent:

- **IAS (`identity / application`).** Failed to provision with
  `OIDC Trust missing` — the subaccount has no OIDC business-user trust
  to an SAP Cloud Identity Services tenant, which the operator CR
  cannot create. Establishing trust (cockpit → *Security → Trust
  Configuration → Establish Trust*) is a one-time action that picks a
  shared IAS tenant; it is handled in `deploy-kyma-api`, the first
  change that needs human (browser) login. Machine uploads (CI) use
  XSUAA `client_credentials`, which is live here, so IAS is not on the
  baseline's critical path.
- **Credential Store (`credstore / proxy`).** The `proxy` plan is a
  consumer proxy onto a shared *original* Credential Store and needs
  that origin's service GUID; the subaccount has no non-proxy plan
  entitled to be the origin, so a standalone `proxy` instance is not
  provisionable. Reintroduced by a follow-up change only if a workload
  needs a secret store beyond XSUAA (and only after the entitlement is
  raised).

## Follow-up work

- `deploy-kyma-api` — **shipped.** Deployment, Service, APIRule,
  NetworkPolicy for the results API. See "The API workload" below.
- `deploy-kyma-cron` — scorecard-cron pipeline CronJobs, Alert
  Notification subscribers.
- IAS browser-login path — separate change (OIDC trust + tenant
  choice).

## The API workload (deploy-kyma-api)

On top of the baseline, `deploy/btp/dev/` carries the results API:

| File | What |
|---|---|
| `api-deployment.yaml` | `scorecard-api` Deployment, one replica, image pinned to an immutable `sha-<commit>` tag. |
| `api-service.yaml` | ClusterIP Service on `:8080`. |
| `api-apirule.yaml` | APIRule (`gateway.kyma-project.io/v2`): GET public (`noAuth`), POST gated by XSUAA JWT. |
| `api-networkpolicy.yaml` | Ingress from the mesh only; egress to DNS, istio-system, and 443. |

### How the bucket URL is composed

The API wants one `s3://…` URL per bucket; the Object Store binding
secret exposes the parts separately (`bucket`, `region`,
`access_key_id`, `secret_access_key`). The Deployment maps the parts to
intermediate env vars and assembles the URL with Kubernetes env-var
reference expansion:

```yaml
- name: BUCKET            # from secret key `bucket`
- name: BUCKET_REGION     # from secret key `region`
- name: SCORECARD_RESULTS_BUCKET_URL
  value: "s3://$(BUCKET)?region=$(BUCKET_REGION)&awssdk=v2"
```

The BTP Object Store is backed by real AWS S3 in `eu-central-1`;
credentials resolve through the AWS SDK v2 default chain from
`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`, both mapped from the same
binding secret.

### Where the APIRule issuer / jwksUri come from

The `jwt` access strategy's `issuer` and `jwksUri` are the XSUAA
tenant's values, read once from the `xsuaa-binding` secret:

- `issuer`  = secret key `url`
  (`https://<subdomain>.authentication.<region>.hana.ondemand.com`)
- `jwksUri` = that URL + `/token_keys`

An APIRule field cannot reference a secret, so these are inlined in
`api-apirule.yaml`. A tenant change is a documented manifest edit.

### Reaching the API

```sh
# Public read (no token):
curl https://scorecard-api.bfcc835.kyma.ondemand.com/

# Upload (requires an XSUAA client_credentials token):
TOKEN=$(curl -s -u "$CLIENTID:$CLIENTSECRET" \
  "https://<subdomain>.authentication.eu10.hana.ondemand.com/oauth/token?grant_type=client_credentials" \
  | jq -r .access_token)
curl -X POST -H "Authorization: Bearer $TOKEN" \
  https://scorecard-api.bfcc835.kyma.ondemand.com/projects/github.com/OWNER/REPO ...
```

`$CLIENTID` / `$CLIENTSECRET` come from the `xsuaa-binding` secret
(keys `clientid`, `clientsecret`).

### Bumping the image

Edit the `sha-<commit>` tag in `api-deployment.yaml` to a newer
`publish-images` output and `kubectl apply -k deploy/btp/dev/`. The tag
is immutable per publish, so a redeploy is reproducible.
