// Copyright 2026 OpenSSF Scorecard Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pubsub

import (
	"context"
	"log"

	"github.com/ossf/scorecard-infra/cron/data"
)

// natsSubscriber adapts the pipeline's at-least-once assumptions to NATS's
// at-most-once reality. The gocloud NATS driver (natspubsub) reports
// CanNack() == false, and the gocloud runtime panics if Message.Nack is
// called on a subscription that cannot nack. The worker calls Nack() to
// request a retry (cron/worker/worker.go), so routing NATS through the
// generic gocloudSubscriber -- which forwards Nack() to the message --
// would crash the worker on the first retry.
//
// Ack() and Nack() are therefore no-ops here. NATS core delivers a message
// at most once and drops it after delivery regardless of acknowledgement,
// so there is nothing for Ack() to confirm and nothing Nack() can requeue.
// Nack() logs a warning so a dropped retry is visible. This at-most-once
// trade-off is the dev-evaluation decision recorded in KN2; at-least-once
// would need a JetStream-capable driver (a gocloud.dev bump).
//
// Receive and Close reuse the generic gocloud path via the embedded
// gocloudSubscriber; only the acknowledgement semantics differ.
type natsSubscriber struct {
	*gocloudSubscriber
}

func createNATSSubscriber(ctx context.Context, subscriptionURL string) (Subscriber, error) {
	inner, err := createGocloudSubscriber(ctx, subscriptionURL)
	if err != nil {
		return nil, err
	}
	return &natsSubscriber{gocloudSubscriber: inner}, nil
}

// SynchronousPull delegates to the embedded gocloud receive path.
func (s *natsSubscriber) SynchronousPull() (*data.ScorecardBatchRequest, error) {
	return s.gocloudSubscriber.SynchronousPull()
}

// Ack is a no-op: NATS core has no per-message acknowledgement to send.
func (s *natsSubscriber) Ack() {}

// Nack is a no-op: NATS core cannot requeue a delivered message. The lost
// retry is logged so it is visible in the worker output.
func (s *natsSubscriber) Nack() {
	log.Print("nats: Nack is a no-op under at-most-once delivery; message will not be retried")
}
