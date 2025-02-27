package storage

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/go-kit/log"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"
	"github.com/thanos-io/objstore/providers/s3"
)

func New(ctx context.Context, bucketURL string) (objstore.Bucket, error) {
	u, err := url.Parse(bucketURL)
	if err != nil {
		return nil, fmt.Errorf("parsing bucket URL: %w", err)
	}

	switch u.Scheme {
	case "s3":
		bucketName := strings.TrimPrefix(u.Path, "/")
		if idx := strings.Index(bucketName, "/"); idx > 0 {
			bucketName = bucketName[:idx]
		}

		if bucketName == "" {
			return nil, fmt.Errorf("bucket name is empty")
		}

		var bucket objstore.Bucket
		bucket, err = s3.NewBucketWithConfig(
			log.NewNopLogger(),
			s3.Config{
				Bucket:    bucketName,
				Endpoint:  u.Host,
				Region:    "us-east-1",
				AccessKey: os.Getenv("AWS_ACCESS_KEY_ID"),
				SecretKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
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

	case "filesystem":
		return filesystem.NewBucket(u.Path)

	case "inmemory":
		return objstore.NewInMemBucket(), nil

	default:
		return nil, fmt.Errorf("unsupported bucket scheme: %s", u.Scheme)
	}
}
