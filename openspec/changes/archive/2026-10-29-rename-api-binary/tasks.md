# Tasks: Rename the results API binary to `scorecard-api`

## 1. Build targets

- [x] 1.1 `api/Makefile`: rename the `scorecard-webapp` target to
      `scorecard-api`; `go build -o scorecard-api`. Update the help text
      on the target line (`## Runs go build on repo`) to match.
- [x] 1.2 `api/Makefile`: rename `scorecard-webapp-docker` to
      `scorecard-api-docker`; update its dependency on the renamed
      build target, the `--tag` value, and any `.PHONY` entry.
- [x] 1.3 `Makefile` (root): update the `api-build` target's recipe to
      invoke `$(MAKE) -C api scorecard-api`; update its help text.

## 2. Container image

- [x] 2.1 `api/Dockerfile`: builder stage invokes
      `make -C api scorecard-api GIT_VERSION=... GIT_HASH=...`.
- [x] 2.2 `api/Dockerfile`: `COPY --from=builder` sources
      `/opt/app-root/src/api/scorecard-api` into
      `/usr/local/bin/scorecard-api`.
- [x] 2.3 `api/Dockerfile`: `ENTRYPOINT` invokes `scorecard-api` with
      the same flags (`--scheme=http --host="0.0.0.0" --port=8080`).

## 3. Documentation

- [x] 3.1 `api/README.md`: drop the "One thing that looks like a mistake
      and is not" paragraph; the mistake no longer exists.
- [x] 3.2 `api/README.md`: update the file table entry for `api/main.go`
      to read "Builds as `scorecard-api`".
- [x] 3.3 `api/README.md`: update the `make api-build` comment line to
      reference the new binary path (`api/scorecard-api`).
- [x] 3.4 Keep the upstream-provenance line
      ("imported from `ossf/scorecard-webapp`") unchanged.
- [x] 3.5 `.gitignore`: add `/api/scorecard-api` and update the comment
      block above `/api/scorecard-webapp` to record the rename. Keep the
      old entry so a stray local build on an older checkout does not
      get committed.

## 4. Verification

- [x] 4.1 `make -C api scorecard-api` produces an executable at
      `api/scorecard-api`; running it with `--help` prints the server's
      usage (same as the old binary). **Verified locally.**
- [x] 4.2 `docker build -f api/Dockerfile .` succeeds; the resulting
      image lists `/usr/local/bin/scorecard-api` and no
      `/usr/local/bin/scorecard-webapp`. **Verified in CI**
      (`api-docker` job, PR #1, 1m54s).
- [x] 4.3 `docker run --rm <image> --help` prints the usage; running
      the image with no args starts the server on port 8080 (same
      behavior as before the rename). **Verified in CI** via the
      `api-docker` job's smoke test.
- [x] 4.4 `grep -rn "scorecard-webapp" api/ Makefile` returns only the
      upstream-provenance line in `api/README.md` and the regenerated
      swagger artifacts if any (none expected). No other matches.
      **Verified:** remaining matches are `api/README.md` (upstream
      provenance line) and `api/initial-graft.md` (upstream import log);
      both are required by decision R3.
- [x] 4.5 `make build`, `make test`, `make lint` all pass at repository
      root. **All three verified in CI** (PR #1): `build` 3m21s,
      `test` 4m0s, `lint` (golangci-lint) 2m50s.
- [ ] 4.6 After merge to the fork's `main`, `publish-images.yml`
      produces a `scorecard-api` image whose entrypoint binary is
      `scorecard-api` (verified with
      `docker run --rm --entrypoint sh <image> -c 'ls /usr/local/bin'`).
