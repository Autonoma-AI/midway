package cache

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Downloader handles downloading objects from S3 with automatic region detection.
type S3Downloader struct {
	cfg           aws.Config
	bucketRegions sync.Map // bucket name -> region
}

// NewS3Downloader creates a new S3 downloader that auto-detects bucket regions.
func NewS3Downloader(cfg aws.Config) *S3Downloader {
	return &S3Downloader{
		cfg: cfg,
	}
}

func (d *S3Downloader) getClientForBucket(ctx context.Context, bucket string) (*s3.Client, error) {
	// Check cache first
	if region, ok := d.bucketRegions.Load(bucket); ok {
		cfg := d.cfg.Copy()
		cfg.Region = region.(string)
		return s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.DisableLogOutputChecksumValidationSkipped = true
		}), nil
	}

	// Detect bucket region using us-east-1 (GetBucketLocation works globally from us-east-1)
	cfg := d.cfg.Copy()
	cfg.Region = "us-east-1"
	client := s3.NewFromConfig(cfg)

	location, err := client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get bucket location: %w", err)
	}

	// Empty location means us-east-1
	region := string(location.LocationConstraint)
	if region == "" {
		region = "us-east-1"
	}

	d.bucketRegions.Store(bucket, region)

	cfg.Region = region
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.DisableLogOutputChecksumValidationSkipped = true
	}), nil
}

// Download downloads an object from S3 and returns a reader.
// The key should be in format "bucket/path/to/file.apk".
func (d *S3Downloader) Download(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	bucket, objectKey, err := parseS3Key(key)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse S3 key: %w", err)
	}

	client, err := d.getClientForBucket(ctx, bucket)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get S3 client for bucket %s: %w", bucket, err)
	}

	result, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to download from S3: %w", err)
	}

	size := int64(0)
	if result.ContentLength != nil {
		size = *result.ContentLength
	}

	return result.Body, size, nil
}

// parseS3Key parses a key in format "bucket/path/to/file" into bucket and object key
func parseS3Key(key string) (bucket, objectKey string, err error) {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid key format, expected bucket/path: %s", key)
	}
	return parts[0], parts[1], nil
}
