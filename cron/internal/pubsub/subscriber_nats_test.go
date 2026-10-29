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
	"errors"
	"testing"
)

// TestCreateNATSSubscriberScheme pins that the nats:// scheme routes to the
// NATS subscriber rather than falling through to errUnsupportedScheme. The
// gocloud NATS driver dials its server eagerly in OpenSubscription and reads
// the address from NATS_SERVER_URL, so the test points that at a closed local
// port: the dial fails, but the failure proves the NATS path was taken (an
// unrouted scheme would return errUnsupportedScheme before any dial). The
// error is specifically NOT errUnsupportedScheme.
func TestCreateNATSSubscriberScheme(t *testing.T) {
	// Not parallel: sets a process-wide environment variable.
	t.Setenv("NATS_SERVER_URL", "nats://127.0.0.1:1") // port 1: nothing listens

	_, err := CreateSubscriber(t.Context(), "nats://scorecard-batch")
	if err == nil {
		t.Fatal("CreateSubscriber(nats://...): expected a dial error, got nil")
	}
	if errors.Is(err, errUnsupportedScheme) {
		t.Errorf("nats:// routed to errUnsupportedScheme; want the NATS path: %v", err)
	}
}

// TestNATSAckNackAreNoops is the reason natsSubscriber exists: the gocloud
// NATS driver reports CanNack() == false, and the gocloud runtime panics if
// Nack() reaches such a message. The worker calls Nack() for retry, so Ack()
// and Nack() must be safe no-ops that never touch the underlying message.
// A nil embedded gocloudSubscriber would panic if either method forwarded to
// it, so this also proves they do not.
func TestNATSAckNackAreNoops(t *testing.T) {
	t.Parallel()

	// No message pulled, embedded subscriber unused: Ack/Nack must not deref
	// anything. If they forwarded to the message, this would panic.
	s := &natsSubscriber{}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Ack/Nack panicked, want no-op: %v", r)
		}
	}()

	s.Ack()
	s.Nack()
}
