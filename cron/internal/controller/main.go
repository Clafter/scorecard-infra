// Copyright 2021 OpenSSF Scorecard Authors
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

// Package main implements the PubSub controller.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ossf/scorecard/v5/clients"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
	"sigs.k8s.io/release-utils/version"

	"github.com/ossf/scorecard-infra/cron/config"
	"github.com/ossf/scorecard-infra/cron/data"
	"github.com/ossf/scorecard-infra/cron/internal/pubsub"
)

var headSHA = clients.HeadSHA

// buildShards materialises the iterator into a slice of batch requests of
// at most shardSize repos each. The count is known once this returns, which
// lets the caller write the run's shard metadata BEFORE publishing anything
// -- required on at-most-once transports (in-cluster NATS) where a worker's
// nack of a metadata-less request is a no-op and drops the request. See
// openspec/changes/controller-metadata-before-publish.
func buildShards(iter data.Iterator, shardSize int, datetime time.Time) ([]*data.ScorecardBatchRequest, error) {
	var shards []*data.ScorecardBatchRequest
	newShard := func(n int32) *data.ScorecardBatchRequest {
		shardNum := n
		return &data.ScorecardBatchRequest{
			JobTime:  timestamppb.New(datetime),
			ShardNum: &shardNum,
		}
	}
	current := newShard(0)
	for iter.HasNext() {
		repoURL, err := iter.Next()
		if err != nil {
			return nil, fmt.Errorf("error reading repoURL: %w", err)
		}
		current.Repos = append(current.GetRepos(), &data.Repo{
			Url: &repoURL.Repo,
			// TODO(controller): pass in non-HEAD commitSHA here.
			Commit:   &headSHA,
			Metadata: repoURL.Metadata.ToString(),
		})
		if len(current.GetRepos()) < shardSize {
			continue
		}
		shards = append(shards, current)
		current = newShard(int32(len(shards)))
	}
	// Flush a trailing partial shard.
	if len(current.GetRepos()) > 0 {
		shards = append(shards, current)
	}
	return shards, nil
}

// publishShards publishes the pre-built shards and closes the publisher.
func publishShards(shards []*data.ScorecardBatchRequest, topicPublisher pubsub.Publisher) error {
	for _, request := range shards {
		if err := topicPublisher.Publish(request); err != nil {
			return fmt.Errorf("error running topicPublisher.Publish: %w", err)
		}
	}
	if err := topicPublisher.Close(); err != nil {
		return fmt.Errorf("error running topicPublisher.Close: %w", err)
	}
	return nil
}

func localFiles(filenames []string) (data.Iterator, error) {
	var iters []data.Iterator
	for _, filename := range filenames {
		f, err := os.Open(filename)
		if err != nil {
			return nil, fmt.Errorf("unable to open input file: %w", err)
		}
		i, err := data.MakeIteratorFrom(f)
		if err != nil {
			return nil, fmt.Errorf("data.MakeIteratorFrom: %w", err)
		}
		iters = append(iters, i)
	}
	iter, err := data.MakeNestedIterator(iters)
	if err != nil {
		return nil, fmt.Errorf("data.MakeNestedIterator: %w", err)
	}
	return iter, nil
}

func main() {
	ctx := context.Background()
	t := time.Now().UTC()

	flag.Parse()
	if err := config.ReadConfig(); err != nil {
		panic(err)
	}

	topic, err := config.GetRequestTopicURL()
	if err != nil {
		panic(err)
	}
	topicPublisher, err := pubsub.CreatePublisher(ctx, topic)
	if err != nil {
		panic(err)
	}

	shardSize, err := config.GetShardSize()
	if err != nil {
		panic(err)
	}

	bucket, err := config.GetResultDataBucketURL()
	if err != nil {
		panic(err)
	}

	rawBucket, err := config.GetRawResultDataBucketURL()
	if err != nil {
		panic(err)
	}

	var reader data.Iterator
	if useLocalFiles := len(flag.Args()) > 0; useLocalFiles {
		reader, err = localFiles(flag.Args())
	} else {
		reader, err = bucketFiles(ctx)
	}
	if err != nil {
		panic(err)
	}

	shards, err := buildShards(reader, shardSize, t)
	if err != nil {
		panic(err)
	}
	shardNum := int32(len(shards)) - 1
	if shardNum < 0 {
		shardNum = 0
	}

	// Write the `.shard_metadata` marker(s) BEFORE publishing, so a worker
	// that receives a request always finds the marker and never nacks it.
	// On at-most-once NATS a nack is a no-op, so a metadata-less request
	// would be dropped forever. See
	// openspec/changes/controller-metadata-before-publish.
	metadata := data.ShardMetadata{
		NumShard:  new(int32),
		ShardLoc:  new(string),
		CommitSha: new(string),
	}
	*metadata.NumShard = shardNum + 1
	*metadata.ShardLoc = bucket + "/" + data.GetBlobFilename("", t)
	*metadata.CommitSha = version.GetVersionInfo().GitCommit
	metadataJSON, err := protojson.Marshal(&metadata)
	if err != nil {
		panic(fmt.Errorf("error during protojson.Marshal: %w", err))
	}
	err = data.WriteToBlobStore(ctx, bucket, data.GetShardMetadataFilename(t), metadataJSON)
	if err != nil {
		panic(fmt.Errorf("error writing to BlobStore: %w", err))
	}

	if rawBucket != "" {
		// Raw data.
		*metadata.ShardLoc = rawBucket + "/" + data.GetBlobFilename("", t)
		metadataJSON, err = protojson.Marshal(&metadata)
		if err != nil {
			panic(fmt.Errorf("error during protojson.Marshal raw: %w", err))
		}
		err = data.WriteToBlobStore(ctx, rawBucket, data.GetShardMetadataFilename(t), metadataJSON)
		if err != nil {
			panic(fmt.Errorf("error writing to BlobStore raw: %w", err))
		}
	}

	// Now publish; every request's metadata is already in place.
	if err := publishShards(shards, topicPublisher); err != nil {
		panic(err)
	}
}
