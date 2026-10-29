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

// Package data contains util fns for reading and writing data handled by the cron job.
package data

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"

	"gocloud.dev/blob"
	_ "gocloud.dev/blob/fileblob" // Needed to read file:/// buckets. Intended primarily for testing, though needed here for tests outside the package.
	_ "gocloud.dev/blob/gcsblob"  // Needed to link in GCP drivers.
	_ "gocloud.dev/blob/s3blob"   // Needed to link in AWS drivers, for the s3:// buckets the batch plane writes.

	"github.com/ossf/scorecard-infra/cron/config"
)

const (
	// filePrefixFormat uses ISO 8601 standard, i.e - YYYY-MM-DDTHH:MM:SS.
	// This format guarantees that lexicographically sorted files are chronologically sorted.
	filePrefixFormat = "2006.01.02/150405/"
)

var (
	errShortBlobName = errors.New("input key length is shorter than expected")
	errParseBlobName = errors.New("error parsing input blob name")
)

// openBucket opens a blob bucket from a URL, with one addition over
// blob.OpenBucket: an optional `prefix` query parameter. When present, the
// returned bucket is wrapped so every key is transparently namespaced under
// that prefix (blob.PrefixedBucket), and the `prefix` parameter is stripped
// before the URL reaches the driver.
//
// This lets several logical buckets share one physical bucket under distinct
// prefixes -- needed on SAP BTP, where Object Store gives one hard-scoped
// credential per bucket and a single-credential pod must address all of the
// pipeline's buckets through one instance (see deploy-kyma-cron KC7). A URL
// without `prefix` behaves exactly as blob.OpenBucket did, so the SQS/GCP
// deployments are unaffected.
func openBucket(ctx context.Context, bucketURL string) (*blob.Bucket, error) {
	parsed, err := url.Parse(bucketURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing bucket URL: %w", err)
	}

	q := parsed.Query()
	prefix := q.Get("prefix")
	if prefix == "" {
		// No prefix: open the URL as-is, unchanged behaviour.
		bucket, err := blob.OpenBucket(ctx, bucketURL)
		if err != nil {
			return nil, fmt.Errorf("error from blob.OpenBucket: %w", err)
		}
		return bucket, nil
	}

	// Strip `prefix` so the driver sees only parameters it understands.
	q.Del("prefix")
	parsed.RawQuery = q.Encode()

	bucket, err := blob.OpenBucket(ctx, parsed.String())
	if err != nil {
		return nil, fmt.Errorf("error from blob.OpenBucket: %w", err)
	}
	return blob.PrefixedBucket(bucket, prefix), nil
}

func blobKeysPrefix(ctx context.Context, bucket *blob.Bucket, filePrefix string) ([]string, error) {
	keys := make([]string, 0)
	iter := bucket.List(&blob.ListOptions{
		Prefix: filePrefix,
	})
	for {
		next, err := iter.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error during iter.Next: %w", err)
		}
		keys = append(keys, next.Key)
	}
	return keys, nil
}

// GetBlobKeysWithPrefix returns all object keys for a given bucketURL which start with prefix.
// The prefix can be used to specify directories.
func GetBlobKeysWithPrefix(ctx context.Context, bucketURL, filePrefix string) ([]string, error) {
	bucket, err := openBucket(ctx, bucketURL)
	if err != nil {
		return nil, err
	}
	defer bucket.Close()
	return blobKeysPrefix(ctx, bucket, filePrefix)
}

// GetBlobKeys returns all object keys for a given bucketURL.
func GetBlobKeys(ctx context.Context, bucketURL string) ([]string, error) {
	return GetBlobKeysWithPrefix(ctx, bucketURL, "")
}

// GetBlobContent returns the file content given a bucketURL and object key.
func GetBlobContent(ctx context.Context, bucketURL, key string) ([]byte, error) {
	bucket, err := openBucket(ctx, bucketURL)
	if err != nil {
		return nil, err
	}
	defer bucket.Close()

	keyData, err := bucket.ReadAll(ctx, key)
	if err != nil {
		return keyData, fmt.Errorf("error during bucket.ReadAll: %w", err)
	}
	return keyData, nil
}

// BlobExists checks whether a given `bucketURL/key` blob exists.
func BlobExists(ctx context.Context, bucketURL, key string) (bool, error) {
	bucket, err := openBucket(ctx, bucketURL)
	if err != nil {
		return false, err
	}
	defer bucket.Close()

	ret, err := bucket.Exists(ctx, key)
	if err != nil {
		return ret, fmt.Errorf("error during bucket.Exists: %w", err)
	}
	return ret, nil
}

// WriteToBlobStore creates and writes data to filename in bucketURL.
func WriteToBlobStore(ctx context.Context, bucketURL, filename string, data []byte) error {
	bucket, err := openBucket(ctx, bucketURL)
	if err != nil {
		return err
	}
	defer bucket.Close()

	blobWriter, err := bucket.NewWriter(ctx, filename, nil)
	if err != nil {
		return fmt.Errorf("error from bucket.NewWriter: %w", err)
	}
	if _, err = blobWriter.Write(data); err != nil {
		return fmt.Errorf("error from blobWriter.Write: %w", err)
	}
	if err := blobWriter.Close(); err != nil {
		return fmt.Errorf("error from blobWriter.Close: %w", err)
	}
	return nil
}

// GetBlobFilename returns a blob key for a shard. Takes Time object and filename as input.
func GetBlobFilename(filename string, datetime time.Time) string {
	return datetime.Format(filePrefixFormat) + filename
}

// GetShardNumFilename returns shard_num filename for a shard.
func GetShardNumFilename(datetime time.Time) string {
	return GetBlobFilename(config.ShardNumFilename, datetime)
}

// GetTransferStatusFilename returns transfer_status filename for a shard.
func GetTransferStatusFilename(datetime time.Time) string {
	return GetBlobFilename(config.TransferStatusFilename, datetime)
}

// GetShardMetadataFilename returns shard_metadata filename for a shard.
func GetShardMetadataFilename(datetime time.Time) string {
	return GetBlobFilename(config.ShardMetadataFilename, datetime)
}

// ParseBlobFilename parses a blob key into a Time object.
func ParseBlobFilename(key string) (time.Time, string, error) {
	if len(key) < len(filePrefixFormat) {
		return time.Now(), "", fmt.Errorf("%w: %s", errShortBlobName, key)
	}
	prefix := key[:len(filePrefixFormat)]
	objectName := key[len(filePrefixFormat):]
	t, err := time.Parse(filePrefixFormat, prefix)
	if err != nil {
		return t, "", fmt.Errorf("%w: %w", errParseBlobName, err)
	}
	return t, objectName, nil
}
