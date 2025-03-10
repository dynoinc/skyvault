package storage

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"
	thanos_s3 "github.com/thanos-io/objstore/providers/s3"
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

		// Create a new Minio client for bucket existence check and creation
		accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
		secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
		region := cmp.Or(os.Getenv("AWS_REGION"), "us-east-1")

		// Extract just the host without the scheme for Minio client
		endpoint := u.Host

		// Determine if TLS should be used based on the bucket URL
		useSSL := strings.HasPrefix(bucketURL, "https://")

		// Initialize Minio client
		minioClient, err := minio.New(endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
			Secure: useSSL,
			Region: region,
		})
		if err != nil {
			return nil, fmt.Errorf("creating Minio client: %w", err)
		}

		// Check if the bucket exists
		exists, err := minioClient.BucketExists(ctx, bucketName)
		if err != nil {
			return nil, fmt.Errorf("checking if bucket exists: %w", err)
		}

		// Create the bucket if it doesn't exist
		if !exists {
			slog.Info("bucket does not exist, creating it", "bucket", bucketName)

			err = minioClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{
				Region: region,
			})

			if err != nil {
				// Check if the bucket was created by another process
				exists, errCheck := minioClient.BucketExists(ctx, bucketName)
				if errCheck != nil || !exists {
					return nil, fmt.Errorf("creating bucket: %w", err)
				}
			}
			slog.Info("bucket created successfully", "bucket", bucketName)
		}

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

	case "filesystem":
		path := u.Host
		if u.Path != "" {
			path = filepath.Join(path, strings.TrimPrefix(u.Path, "/"))
		}

		return filesystem.NewBucket(path)

	case "inmemory":
		return objstore.NewInMemBucket(), nil

	default:
		return nil, fmt.Errorf("unsupported bucket scheme: %s", u.Scheme)
	}
}

// nopLogger implements the go-kit log.Logger interface but does nothing
type nopLogger struct{}

func (l *nopLogger) Log(_ ...interface{}) error {
	return nil
}
