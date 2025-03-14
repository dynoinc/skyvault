package storage

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/thanos-io/objstore"
	thanos_s3 "github.com/thanos-io/objstore/providers/s3"
)

func New(ctx context.Context, bucketURL string) (objstore.Bucket, error) {
	u, err := url.Parse(bucketURL)
	if err != nil {
		return nil, fmt.Errorf("parsing bucket URL: %w", err)
	}

	// Determine if TLS should be used based on the bucket URL
	useSSL := u.Scheme == "https"

	bucketName := strings.TrimPrefix(u.Path, "/")
	if idx := strings.Index(bucketName, "/"); idx > 0 {
		bucketName = bucketName[:idx]
	}

	if bucketName == "" {
		return nil, fmt.Errorf("bucket name is empty")
	}

	// Get credentials from environment variables
	// These are set from the common ConfigMap in Kubernetes
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	region := cmp.Or(os.Getenv("AWS_REGION"), "us-east-1")

	// Extract just the host without the scheme for Minio client
	endpoint := u.Host

	// Now create the Thanos S3 bucket client for compatibility with the rest of the codebase
	var bucket objstore.Bucket

	// Note: Thanos still uses go-kit logging, so we create a dummy NopLogger
	bucket, err = thanos_s3.NewBucketWithConfig(
		&nopLogger{},
		thanos_s3.Config{
			Bucket:    bucketName,
			Endpoint:  endpoint,
			Region:    region,
			AccessKey: accessKey,
			SecretKey: secretKey,
			Insecure:  !useSSL,
		},
		"objstore",
		func(tpt http.RoundTripper) http.RoundTripper {
			return tpt
		},
	)
	if err != nil {
		return nil, fmt.Errorf("creating S3 bucket: %w", err)
	}

	if prefix := strings.TrimPrefix(u.Path[len(bucketName)+1:], "/"); prefix != "" {
		bucket = objstore.NewPrefixedBucket(bucket, prefix)
	}

	return bucket, nil
}

// nopLogger implements the go-kit log.Logger interface but does nothing
type nopLogger struct{}

func (l *nopLogger) Log(_ ...interface{}) error {
	return nil
}
