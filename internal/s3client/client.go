package s3client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/esignoretti/ds3backup/internal/config"
)

// Client wraps MinIO S3 client
type Client struct {
	client     *minio.Client
	bucket     string
	objectLock bool
}

// NewClient creates a new S3 client
func NewClient(cfg config.S3Config) (*Client, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}

	// Check if bucket exists and has object lock
	ctx := context.Background()
	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("failed to check bucket existence: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("bucket %s does not exist", cfg.Bucket)
	}

	// Check object lock support
	objectLockEnabled := false
	_, _, _, _, err = client.GetObjectLockConfig(ctx, cfg.Bucket)
	if err == nil {
		objectLockEnabled = true
	}

	return &Client{
		client:     client,
		bucket:     cfg.Bucket,
		objectLock: objectLockEnabled,
	}, nil
}

// PutObject uploads an object to S3
func (c *Client) PutObject(ctx context.Context, key string, data []byte) error {
	reader := bytes.NewReader(data)
	_, err := c.client.PutObject(ctx, c.bucket, key, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	return err
}

// PutObjectWithLock uploads an object with Object Lock
func (c *Client) PutObjectWithLock(ctx context.Context, key string, data []byte, mode string, retentionDays int) error {
	if !c.objectLock {
		// Fall back to regular upload if object lock not supported
		return c.PutObject(ctx, key, data)
	}

	reader := bytes.NewReader(data)
	retentionUntil := time.Now().AddDate(0, 0, retentionDays+1) // +1 day buffer

	opts := minio.PutObjectOptions{
		ContentType:    "application/octet-stream",
		Mode:           minio.Governance,
		RetainUntilDate: retentionUntil,
	}

	_, err := c.client.PutObject(ctx, c.bucket, key, reader, int64(len(data)), opts)
	return err
}

// GetObject downloads an object from S3
func (c *Client) GetObject(ctx context.Context, key string) ([]byte, error) {
	obj, err := c.client.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// ListObjects lists objects with a given prefix
func (c *Client) ListObjects(ctx context.Context, prefix string) ([]string, error) {
	var objects []string

	opts := minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}

	for obj := range c.client.ListObjects(ctx, c.bucket, opts) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		objects = append(objects, obj.Key)
	}

	return objects, nil
}

// DeleteObject deletes an object from S3
func (c *Client) DeleteObject(ctx context.Context, key string) error {
	return c.client.RemoveObject(ctx, c.bucket, key, minio.RemoveObjectOptions{})
}

// CheckObjectLockSupport checks if bucket supports Object Lock
func (c *Client) CheckObjectLockSupport() (bool, error) {
	_, _, _, _, err := c.client.GetObjectLockConfig(context.Background(), c.bucket)
	if err != nil {
		if minio.ToErrorResponse(err).Code == "ObjectLockConfigurationNotFoundError" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// SetLifecyclePolicy sets lifecycle policy for retention cleanup
func (c *Client) SetLifecyclePolicy(ctx context.Context, retentionDays int) error {
	// This would set up S3 lifecycle rules to automatically delete old objects
	// Implementation depends on S3 provider capabilities
	return nil
}

// BucketExists checks if bucket exists
func (c *Client) BucketExists(ctx context.Context) (bool, error) {
	return c.client.BucketExists(ctx, c.bucket)
}
