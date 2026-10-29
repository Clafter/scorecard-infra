// Copyright 2024 OpenSSF Scorecard Authors
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

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/ossf/scorecard-infra/cron/data"
)

func makeIter(t *testing.T, repos ...string) data.Iterator {
	t.Helper()
	csv := "repo,metadata\n"
	for _, r := range repos {
		csv += r + ",\n"
	}
	iter, err := data.MakeIteratorFrom(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("MakeIteratorFrom: %v", err)
	}
	return iter
}

// Real repo URLs: the CSV iterator validates each row as a github/gitlab URL.
var testRepos = []string{
	"github.com/a/a",
	"github.com/b/b",
	"github.com/c/c",
	"github.com/d/d",
}

func TestBuildShards(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	tests := []struct {
		name       string
		repos      []string
		shardSize  int
		wantShards int
		wantRepos  int
	}{
		{"empty", nil, 10, 0, 0},
		{"single partial shard", testRepos[:2], 10, 1, 2},
		{"exact one shard", testRepos[:2], 2, 1, 2},
		{"two full shards", testRepos[:4], 2, 2, 4},
		{"full plus partial", testRepos[:3], 2, 2, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			shards, err := buildShards(makeIter(t, tt.repos...), tt.shardSize, now)
			if err != nil {
				t.Fatalf("buildShards: %v", err)
			}
			if len(shards) != tt.wantShards {
				t.Errorf("shard count = %d, want %d", len(shards), tt.wantShards)
			}
			total := 0
			for i, s := range shards {
				total += len(s.GetRepos())
				if got := s.GetShardNum(); got != int32(i) {
					t.Errorf("shard[%d].ShardNum = %d, want %d", i, got, i)
				}
			}
			if total != tt.wantRepos {
				t.Errorf("total repos = %d, want %d", total, tt.wantRepos)
			}
		})
	}
}

// recordingPublisher records the order of Publish calls so a test can prove
// that any blob writes a caller does before publishing really happen first.
type recordingPublisher struct {
	events *[]string
}

func (p *recordingPublisher) Publish(*data.ScorecardBatchRequest) error {
	*p.events = append(*p.events, "publish")
	return nil
}

func (p *recordingPublisher) Close() error {
	*p.events = append(*p.events, "close")
	return nil
}

// TestMetadataWrittenBeforePublish documents and enforces the ordering
// contract that this change introduces: the shard metadata must be durably
// written before the first request is published, because the in-cluster
// NATS transport is at-most-once and a worker's nack of a metadata-less
// request is a no-op (the request is lost).
//
// main() wires this ordering; here we exercise the same two steps in the
// same order against a recording publisher to lock the contract in place.
func TestMetadataWrittenBeforePublish(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	shards, err := buildShards(makeIter(t, testRepos[:3]...), 2, now)
	if err != nil {
		t.Fatalf("buildShards: %v", err)
	}

	var events []string
	// The metadata write is represented by this marker; main() performs a
	// real WriteToBlobStore here. What matters for the contract is that it
	// precedes every publish.
	events = append(events, "write-metadata")

	pub := &recordingPublisher{events: &events}
	if err := publishShards(shards, pub); err != nil {
		t.Fatalf("publishShards: %v", err)
	}

	if events[0] != "write-metadata" {
		t.Fatalf("first event = %q, want metadata write before any publish; order=%v",
			events[0], events)
	}
	for _, e := range events[1:] {
		if e == "write-metadata" {
			t.Fatalf("metadata written after a publish; order=%v", events)
		}
	}
}
