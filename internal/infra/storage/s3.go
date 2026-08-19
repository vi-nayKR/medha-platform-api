package storage

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Buckets are split one-per-content-type across both dev and prod object
// stores — each environment is a physically separate server, so these names
// need no env suffix.
const (
	BucketFeeds         = "feeds"
	BucketProfiles      = "profiles"
	BucketFestivalLogos = "festival-logos"
	BucketAdminMedia    = "admin-media"
)

var managedBuckets = []string{BucketFeeds, BucketProfiles, BucketFestivalLogos, BucketAdminMedia}

// S3Client wraps an S3-compatible object storage client (SeaweedFS in prod/dev;
// any S3-API-compatible server works) with Medha-specific helpers. The
// underlying SDK (github.com/minio/minio-go) is a generic S3 client despite
// its name — it is not coupled to MinIO as a server.
type S3Client struct {
	client         *minio.Client // internal client (localhost:8333)
	publicClient   *minio.Client // public client for signing (Cloudflare/ngrok URL)
	publicEndpoint string
	publicBaseURL  string
	logger         *slog.Logger
}

// ManagedBuckets returns the fixed list of buckets this service owns.
func ManagedBuckets() []string {
	return managedBuckets
}

// ensureBucketPublic creates the bucket if missing and (re-)applies a public-read
// GetObject policy. Runs on every startup so a bucket/policy reset (e.g. a data
// migration) self-heals without a manual `mc` step.
func ensureBucketPublic(ctx context.Context, client *minio.Client, bucket string, logger *slog.Logger) error {
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("check bucket %s: %w", bucket, err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("create bucket %s: %w", bucket, err)
		}
		logger.Info("storage bucket created", "bucket", bucket)
	}

	policy := fmt.Sprintf(`{
		"Version": "2012-10-17",
		"Statement": [
			{
				"Effect": "Allow",
				"Principal": {"AWS": ["*"]},
				"Action": ["s3:GetObject"],
				"Resource": ["arn:aws:s3:::%s/*"]
			}
		]
	}`, bucket)
	if err := client.SetBucketPolicy(ctx, bucket, policy); err != nil {
		logger.Warn("failed to set public policy on bucket", "bucket", bucket, "error", err)
	}
	return nil
}

// NewS3Client creates a new S3-compatible client and ensures all managed buckets exist.
func NewS3Client(ctx context.Context, endpoint, publicEndpoint, accessKey, secretKey string, useSSL bool, logger *slog.Logger) (*S3Client, error) {
	// 1. Internal Client (for PutObject, DeleteObject, BucketExists)
	// Strip scheme if present (the SDK expects hostname:port)
	cleanedEndpoint := s3EndpointHost(endpoint)
	internalSecure := s3EndpointSecure(endpoint, useSSL)

	client, err := minio.New(cleanedEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: internalSecure,
		Region: "us-east-1",
	})
	if err != nil {
		return nil, fmt.Errorf("create internal s3 client: %w", err)
	}

	// 2. Public Client (for signing pre-signed URLs)
	// We only use this one if publicEndpoint is different from endpoint
	var publicClient *minio.Client
	if publicEndpoint == "" {
		publicEndpoint = endpoint
	}
	cleanedPublicEndpoint := s3EndpointHost(publicEndpoint)
	publicSecure := s3EndpointSecure(publicEndpoint, true)
	if cleanedPublicEndpoint != cleanedEndpoint || publicSecure != internalSecure {
		// Strip scheme if present (the SDK expects hostname:port)
		publicClient, err = minio.New(cleanedPublicEndpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
			Secure: publicSecure,
			Region: "us-east-1",
		})
		if err != nil {
			return nil, fmt.Errorf("create public s3 client: %w", err)
		}
	} else {
		publicClient = client
	}

	// Ensure every managed bucket exists with a public-read policy.
	for _, bucket := range managedBuckets {
		if err := ensureBucketPublic(ctx, client, bucket, logger); err != nil {
			return nil, err
		}
	}

	logger.Info("s3 client initialized",
		"internal_endpoint", endpoint,
		"public_endpoint", publicEndpoint,
		"buckets", managedBuckets,
	)

	return &S3Client{
		client:         client,
		publicClient:   publicClient,
		publicEndpoint: publicEndpoint,
		publicBaseURL:  s3EndpointBaseURL(publicEndpoint, publicSecure),
		logger:         logger,
	}, nil
}

func s3EndpointHost(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		return u.Host
	}
	return strings.TrimRight(endpoint, "/")
}

func s3EndpointSecure(endpoint string, fallback bool) bool {
	if u, err := url.Parse(endpoint); err == nil {
		switch u.Scheme {
		case "http":
			return false
		case "https":
			return true
		}
	}
	return fallback
}

func s3EndpointBaseURL(endpoint string, secure bool) string {
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		u.Path = ""
		u.RawQuery = ""
		u.Fragment = ""
		return strings.TrimRight(u.String(), "/")
	}

	scheme := "http"
	if secure {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, strings.TrimRight(endpoint, "/"))
}

// GeneratePresignedPutURL generates a presigned URL for uploading an object to a specific bucket.
// The caller uploads directly to the object store using this URL.
func (m *S3Client) GeneratePresignedPutURL(ctx context.Context, bucket, objectName string, expiry time.Duration) (string, error) {
	presignedURL, err := m.publicClient.PresignedPutObject(ctx, bucket, objectName, expiry)
	if err != nil {
		return "", fmt.Errorf("generate presigned PUT url: %w", err)
	}
	return presignedURL.String(), nil
}

// GeneratePresignedGetURL generates a presigned URL for downloading an object from a specific bucket.
func (m *S3Client) GeneratePresignedGetURL(ctx context.Context, bucket, objectName string, expiry time.Duration) (string, error) {
	reqParams := make(url.Values)
	presignedURL, err := m.publicClient.PresignedGetObject(ctx, bucket, objectName, expiry, reqParams)
	if err != nil {
		return "", fmt.Errorf("generate presigned GET url: %w", err)
	}
	return presignedURL.String(), nil
}

// GenerateProfilePhotoKey creates a unique object key for a user's profile photo, for use in BucketProfiles.
// Format: {user_id}/{uuid}.{ext}
func GenerateProfilePhotoKey(userID uuid.UUID, extension string) string {
	return fmt.Sprintf("%s/%s.%s", userID.String(), uuid.New().String(), extension)
}

// GenerateFeedPhotoKey creates a unique object key for a community feed post, for use in BucketFeeds.
// Format: {user_id}/{uuid}.{ext}
func GenerateFeedPhotoKey(userID uuid.UUID, extension string) string {
	return fmt.Sprintf("%s/%s.%s", userID.String(), uuid.New().String(), extension)
}

// PutObjectInBucket uploads data to a specific bucket.
func (m *S3Client) PutObjectInBucket(ctx context.Context, bucketName, objectName string, reader interface {
	Read(p []byte) (n int, err error)
}, objectSize int64, contentType string) error {
	_, err := m.client.PutObject(ctx, bucketName, objectName, reader, objectSize, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("put object in bucket %s: %w", bucketName, err)
	}
	return nil
}

// ResolveURLForBucket converts an object key in a specific bucket to a full public URL.
func (m *S3Client) ResolveURLForBucket(bucketName, objectKey string) string {
	if objectKey == "" {
		return ""
	}
	if strings.HasPrefix(objectKey, "http://") || strings.HasPrefix(objectKey, "https://") {
		return objectKey
	}
	return fmt.Sprintf("%s/%s/%s", m.publicBaseURL, bucketName, objectKey)
}

// DeleteObject removes an object from a specific bucket.
func (m *S3Client) DeleteObject(ctx context.Context, bucket, objectName string) error {
	err := m.client.RemoveObject(ctx, bucket, objectName, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

// DeleteUserDirectory deletes all objects prefixed by the userID in profiles and feeds buckets.
func (m *S3Client) DeleteUserDirectory(ctx context.Context, userID string) error {
	prefix := userID + "/"
	buckets := []string{BucketProfiles, BucketFeeds}
	for _, bucket := range buckets {
		objectsCh := m.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
			Prefix:    prefix,
			Recursive: true,
		})
		for object := range objectsCh {
			if object.Err != nil {
				m.logger.Error("failed to list objects for deletion", "bucket", bucket, "prefix", prefix, "error", object.Err)
				return fmt.Errorf("list objects: %w", object.Err)
			}
			err := m.client.RemoveObject(ctx, bucket, object.Key, minio.RemoveObjectOptions{})
			if err != nil {
				m.logger.Error("failed to remove object", "bucket", bucket, "key", object.Key, "error", err)
				return fmt.Errorf("remove object %s: %w", object.Key, err)
			}
			m.logger.Info("deleted user file from storage", "bucket", bucket, "key", object.Key)
		}
	}
	return nil
}

// ObjectExists checks if an object exists in a specific bucket.
func (m *S3Client) ObjectExists(ctx context.Context, bucket, objectName string) (bool, error) {
	_, err := m.client.StatObject(ctx, bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return false, nil
		}
		return false, fmt.Errorf("check object exists: %w", err)
	}
	return true, nil
}

// RawClient returns the underlying minio.Client for advanced usage (e.g. admin tasks across multiple buckets).
func (m *S3Client) RawClient() *minio.Client {
	return m.client
}

// PublicBaseURL returns the configured public base URL for object URL generation.
func (m *S3Client) PublicBaseURL() string {
	return m.publicBaseURL
}
