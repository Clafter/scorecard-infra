# btp-serving Specification

## Purpose
Defines how the results API is deployed and served from SAP BTP's Kyma
runtime: what binary runs, how it reaches object storage, how the public
read path and the authenticated write path are separated at the mesh
edge, and how the serving workload is isolated on the cluster. This is
the BTP analogue of `aws-serving`; it governs the dev deployment and the
contract any higher environment must also satisfy.

## Requirements

### Requirement: The deployed serving binary is the imported API

The BTP deployment SHALL run the imported results API (`api/`), the
server that ships, and SHALL NOT deploy the provider-agnostic hybrid
server, which is off the deployment path.

#### Scenario: A workload is provisioned for the serving path

- **WHEN** the serving path is deployed to Kyma
- **THEN** the artifact deployed SHALL be the imported API's image
  (`scorecard-api`), and SHALL NOT be the hybrid server's

### Requirement: Object storage is reached through the blob abstraction, configured from the environment

The API SHALL reach result storage through its `gocloud.dev/blob`
bucket URLs (`SCORECARD_RESULTS_BUCKET_URL`,
`SCORECARD_CRON_RESULTS_BUCKET_URL`), and those URLs SHALL be supplied
by the deployment from the bound service instance rather than hardcoded
in the image or the application code.

#### Scenario: The results bucket is addressed

- **WHEN** the API opens a result bucket
- **THEN** the bucket URL SHALL be composed from the Object Store
  binding's own values (bucket name, region), and SHALL NOT be a
  literal baked into the application

#### Scenario: Storage credentials are provided

- **WHEN** the API authenticates to object storage
- **THEN** the credentials SHALL resolve through the backend driver's
  default credential chain from the binding's secret, and SHALL NOT be
  embedded in the image or the manifest

### Requirement: The public read path and the authenticated write path are separated at the edge

Read access SHALL be reachable without a token, and write access SHALL
require a valid token verified before the request reaches the
application. The application performs no authentication of its own, so
the separation SHALL be enforced at the ingress/mesh layer.

#### Scenario: A result is read

- **WHEN** a client issues a GET to the API's public host
- **THEN** the request SHALL be served without requiring a token

#### Scenario: A result is uploaded without a token

- **WHEN** a client issues a POST without a valid token
- **THEN** the request SHALL be rejected at the edge before reaching the
  application

#### Scenario: A result is uploaded with a token

- **WHEN** a client issues a POST with a token the configured issuer
  signed
- **THEN** the token SHALL be verified against that issuer's published
  key set before the request reaches the application

### Requirement: The serving workload is isolated on the cluster

The serving workload SHALL accept ingress only from the mesh edge and
SHALL restrict egress to the dependencies it needs. A workload that
accepts traffic from the internet warrants a bounded blast radius.

#### Scenario: Ingress is constrained

- **WHEN** traffic reaches the serving pod
- **THEN** it SHALL be admitted only from the Istio ingress path, and
  SHALL NOT be admissible directly from arbitrary namespaces

#### Scenario: Egress is constrained

- **WHEN** the serving pod opens an outbound connection
- **THEN** it SHALL reach only DNS, the mesh control plane, and the
  object-storage / identity endpoints it depends on, and SHALL NOT have
  unrestricted egress

### Requirement: Deployments name a reproducible artifact

The serving workload SHALL be deployed by an immutable image reference
so that what is running can be named exactly and a rollback targets a
known build.

#### Scenario: A workload is deployed

- **WHEN** the serving workload is deployed
- **THEN** its image SHALL be referenced by an immutable tag or digest,
  and SHALL NOT track a mutable tag that an unrelated publish can move
