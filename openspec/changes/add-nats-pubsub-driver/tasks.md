# Tasks: add-nats-pubsub-driver

Decisions in `design.md` (**KN1**–**KN4**). Rationale in `proposal.md`.
This is the engine half; the deploy (NATS pod + cron manifests) is
`deploy-kyma-cron`'s apply phase.

## 1. Dependency

- [x] 1.1 `go get gocloud.dev/pubsub/natspubsub@v0.46.0` (matches the
      pinned `gocloud.dev v0.46.0`); `go mod tidy`. Adds the NATS client
      deps (`nats.go`, `nkeys`, `nuid`).

## 2. Publisher (KN3)

- [x] 2.1 `publisher.go`: blank-import `gocloud.dev/pubsub/natspubsub`
      so `nats://` topics open. No other change — the publisher is
      already provider-agnostic.

## 3. Subscriber (KN2, KN3)

- [x] 3.1 `subscriber.go`: import `natspubsub`, add
      `case natspubsub.Scheme` routing to `createNATSSubscriber`.
- [x] 3.2 `subscriber_nats.go` (new): `natsSubscriber` embedding
      `gocloudSubscriber` for Receive/Close; `Ack()` and `Nack()` as
      safe no-ops (NATS core `CanNack() == false`; the gocloud runtime
      panics on `Nack()` otherwise). `Nack()` logs a warning so a
      dropped retry is visible (KN2).

## 4. Tests

- [x] 4.1 `subscriber_nats_test.go`:
      - `TestCreateNATSSubscriberScheme` — `nats://` routes to the NATS
        path, not `errUnsupportedScheme` (dial fails against a closed
        port, which proves the route).
      - `TestNATSAckNackAreNoops` — `Ack()`/`Nack()` do not panic and do
        not deref the embedded subscriber. This is the whole reason the
        wrapper exists.
- [x] 4.2 SQS/GCP tests untouched and still pass.

## 5. Verify

- [x] 5.1 `go build ./cron/internal/pubsub/` clean.
- [x] 5.2 `go test ./cron/internal/pubsub/` passes (NATS + SQS cases).
- [x] 5.3 `make test` green across the repo.
- [x] 5.4 `gofmt -l` clean, `go vet ./cron/internal/pubsub/
      ./cron/worker/` clean. (`make lint`'s `golangci-lint` is not
      installed locally; full lint runs on CI.)
- [ ] 5.5 CI `make lint` green on push. **Pending push.**

## 6. Capability spec

- [x] 6.1 No `specs/` delta: this adds a backend option to an existing
      capability, not a new product behaviour. The cron pipeline's
      `btp-batch` capability is born when `deploy-kyma-cron` applies.
