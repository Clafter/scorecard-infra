# Proposal: Rename the results API binary to `scorecard-api`

## Why

`api/README.md` already flags the name as the one thing in the tree that
looks like a mistake and is not:

> **The binary is named `scorecard-webapp`** while this repository's own
> server binary is `scorecard-api` — which inverts what each one actually
> is. Renaming changes image contents, so it is deferred: it does nothing
> for the migration and costs a rebuild of the artifact every consumer is
> served from.

The deferral held for the AWS cutover (live 2026-09-06). The reason to
pick the rename up now is the fork's BTP Kyma deployment: the Kyma
manifests, Helm charts, and APIRules we are about to write will embed the
binary name in `command:`, image labels, and probe paths. Shipping those
with `scorecard-webapp` would carry the mislabel into a second production
environment and double the surface that eventually has to be renamed.

The published Docker image is **already** named `scorecard-api`
(`.github/workflows/publish-images.yml` line 83). Only the binary
*inside* the image, and the targets that build it, still carry the old
name. Aligning the binary with the image is a self-contained correction.

## What Changes

- **`api/Makefile`**: rename the `scorecard-webapp` target to
  `scorecard-api`, and the `scorecard-webapp-docker` target to
  `scorecard-api-docker`. The `go build -o` output changes with them.
- **`api/Dockerfile`**: build and copy `scorecard-api` instead of
  `scorecard-webapp`; `ENTRYPOINT` invokes `scorecard-api`.
- **Root `Makefile`**: the `api-build` target's `$(MAKE) -C api` call
  invokes the new target name; help text updates with it.
- **`api/README.md`**: drop the "looks like a mistake" paragraph; update
  the binary-name references in the file table and build instructions.

The published image name, image tags, image labels, OCI metadata, image
digests consumed by `deploy/api/`, `cmd/scorecard-api/`'s existing name,
and every reference to the upstream project `ossf/scorecard-webapp` are
**not touched**: the first is already correct, the rest are either
unaffected or refer to the upstream repository and must keep their name.

## Capabilities

None. This is a build artifact rename; no HTTP surface, no behavior, no
spec requirement changes. The `api-server` spec remains as it is.

## Impact

- `api/Makefile`
- `api/Dockerfile`
- `api/README.md`
- `Makefile` (root)

The image produced by `publish-images.yml` keeps its name
(`scorecard-api`) and its tags. Its contents change: `/usr/local/bin/`
contains `scorecard-api` where it previously contained
`scorecard-webapp`. Consumers that run the image through its
`ENTRYPOINT` (every Kubernetes `Deployment`, `kubectl run`, `docker run`
without a command override) are unaffected. Consumers that invoke the
binary by name — if any — must update to the new name; none are known in
this repository.

CI picks up the new target name automatically because the Dockerfile
drives the build; no workflow file changes.

## Non-goals

- **Renaming the Docker image.** It is already `scorecard-api`; the
  mismatch this change fixes is in the other direction.
- **Changing any upstream reference.** `ossf/scorecard-webapp` is a
  separate repository and its name must be preserved in every mention
  (`api/README.md`'s provenance line, `cmd/scorecard-api/`'s contract
  references, `docs/`, archived OpenSpec changes).
- **Changing the entrypoint path or port.** `scorecard-api` runs on the
  same `--host 0.0.0.0 --port 8080` the current ENTRYPOINT uses.
- **Renaming `cmd/scorecard-api`.** It already has the right name. The
  two binaries now simply match: `cmd/scorecard-api` builds the
  cloud-agnostic hybrid server, `api/` builds the results API that
  ships; both produce a `scorecard-api` executable with distinct
  contracts, which is a separate convergence problem (designs **C11**
  and **W10**) deferred to its own change.
- **Any BTP-specific wiring.** The Kyma deployment is the *reason*
  this change is picked up now, but no BTP manifests are added here.
  They land in `deploy-kyma-baseline` and its successors.
