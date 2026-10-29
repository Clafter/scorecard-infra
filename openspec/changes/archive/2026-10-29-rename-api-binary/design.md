# Design: Rename the results API binary to `scorecard-api`

Decision IDs are **R\*** here, distinct from the other changes' **A\***,
**B\***, **C\***, **D\***, **E\***, **F\***, **I\***, **L\***, **V\***,
and **W\***.

## Decisions

### R1 — Rename the binary, not the image

The published Docker image is already `scorecard-api`
(`.github/workflows/publish-images.yml`). The misalignment is one level
deeper: the image's `ENTRYPOINT` runs an executable named
`scorecard-webapp`. The two options were:

1. Rename the image to `scorecard-webapp` to match the binary.
2. Rename the binary to `scorecard-api` to match the image.

(2) is the direction chosen because the image name is the user-visible
identifier — Kubernetes manifests, `docker pull` commands, release
dashboards, badge URLs, and SBOM entries all name the image, not the
binary inside it. The image is also the one with a stable published
history on the fork's and upstream registry. Renaming the image would
invalidate every deployment reference, every pinned digest in a
GitOps repo, and every reader of `publish-images.yml`'s matrix. Renaming
the binary invalidates nothing outside this repository, because nothing
outside invokes `scorecard-webapp` by name — `ENTRYPOINT` runs it, and
every known consumer runs the image through its entrypoint.

### R2 — Do not rename `cmd/scorecard-api`

`cmd/scorecard-api` already has the right name. After this change, the
two binaries both named `scorecard-api` share a name — one in `api/`
builds the deployment target, one in `cmd/` builds the cloud-agnostic
hybrid server (`internal/`). This is deliberate collision: the two
converge in a later change (designs **C11** and **W10**), and giving
them already-matching names makes the convergence a code merge, not an
additional rename. Until then, `api/scorecard-api` is the one that ships
(the context in `openspec/config.yaml` is explicit about this), and
`cmd/scorecard-api` builds but is not deployed.

Users who build both locally end up with two identically-named binaries
in different subdirectories. This is a minor local-development
annoyance, judged acceptable: the alternative is to rename one of them
to something that will have to be renamed again when they converge.

### R3 — Keep every reference to `ossf/scorecard-webapp` unchanged

`scorecard-webapp` is a real upstream repository (`ossf/scorecard-webapp`)
and the ancestor of `api/`. Every mention of it in `api/README.md`'s
provenance line, in `cmd/scorecard-api/`'s contract documentation, in
`docs/`, in `internal/` comments, and in archived OpenSpec changes
refers to that repository. Those names must be preserved; the rename
touches only the local build artifact's filename.

The verification step in `tasks.md:4.4` enforces this: after the change,
the only `scorecard-webapp` matches in `api/` and the root `Makefile`
should be the provenance line, no others.

### R4 — Why now, after the deferral held through the AWS cutover

`api/README.md` justified deferring the rename on the grounds that it
"does nothing for the migration and costs a rebuild of the artifact
every consumer is served from." Both halves were true for the AWS
migration: that migration preserved contract compatibility, not build
artifact names, so a rename would have added noise without value.

The fork's BTP Kyma deployment changes the calculation. The upcoming
`deploy-kyma-baseline`, `deploy-kyma-api`, and `deploy-kyma-cron`
changes will embed the binary name in Kubernetes probe commands, Helm
value defaults, and operator-facing documentation. Writing them with
`scorecard-webapp` would ship the mislabel into a second production
environment and double the surface that eventually gets corrected.
Picking the rename up before those changes land is strictly cheaper
than picking it up after.

### R5 — No capability change

The server's HTTP surface, flags, environment variables, and exit codes
are unchanged. The `api-server` specification therefore has no delta,
and no `specs/` directory is included with this change. This matches
the pattern of changes that touch build or packaging only — none in the
active set, but consistent with how archived packaging changes
(`unify-release-versioning`) kept their spec deltas narrow.
