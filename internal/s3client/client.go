package s3client

import (
	"bytes"
	"strings"
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/esignoretti/ds3backup/internal/config"
)

// Client wraps AWS SDK v2 S3 client
type Client struct {
	client     *s3.Client
	downloader *manager.Downloader
	bucket     string
	objectLock bool
}

// NewClient creates a new S3 client with AWS SDK v2
func NewClient(cfg config.S3Config) (*Client, error) {
	// Configure HTTP client with proper timeouts
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ResponseHeaderTimeout: 2 * time.Minute,
			ExpectContinueTimeout: 10 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
		},
	}

	// Load AWS config with static credentials
	awsConfig, err := awscfg.LoadDefaultConfig(context.TODO(),
		awscfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKey, cfg.SecretKey, "",
		)),
		awscfg.WithRegion(cfg.Region),
		awscfg.WithHTTPClient(httpClient),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client with custom endpoint for Cubbit DS3
	client := s3.NewFromConfig(awsConfig, func(o *s3.Options) {
		endpoint := cfg.Endpoint
		if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
			endpoint = "https://" + endpoint
		}
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true // Required for S3-compatible endpoints
	})

	// Create downloader with 64MB parts
	downloader := manager.NewDownloader(client, func(d *manager.Downloader) {
		d.PartSize = 64 * 1024 * 1024
	})

	// Check if bucket exists
	ctx := context.Background()
	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(cfg.Bucket),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to verify bucket existence: %w", err)
	}

	// Check object lock support
	objectLockEnabled := false
	_, err = client.GetObjectLockConfiguration(ctx, &s3.GetObjectLockConfigurationInput{
		Bucket: aws.String(cfg.Bucket),
	})
	if err == nil {
		objectLockEnabled = true
	}

	return &Client{
		client:     client,
		downloader: downloader,
		bucket:     cfg.Bucket,
		objectLock: objectLockEnabled,
	}, nil
}

// PutObject uploads an object to S3
func (c *Client) PutObject(ctx context.Context, key string, data []byte) error {
	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/octet-stream"),
	})
	return err
}

// PutObjectWithLock uploads an object with Object Lock
func (c *Client) PutObjectWithLock(ctx context.Context, key string, data []byte, mode string, retentionDays int) error {
	// If mode is NONE or object lock not supported, upload without lock
	if mode == "NONE" || !c.objectLock {
		return c.PutObject(ctx, key, data)
	}

	retentionUntil := time.Now().AddDate(0, 0, retentionDays+1) // +1 day buffer

	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:                  aws.String(c.bucket),
		Key:                     aws.String(key),
		Body:                    bytes.NewReader(data),
		ContentType:             aws.String("application/octet-stream"),
		ObjectLockMode:          "GOVERNANCE",
		ObjectLockRetainUntilDate: &retentionUntil,
	})
	return err
}

// GetObject downloads an object from S3 with proper timeout handling
func (c *Client) GetObject(ctx context.Context, key string) ([]byte, error) {
	// Create timeout context for this operation
	opCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	// Use channel to handle timeout properly
	type result struct {
		data []byte
		err  error
	}
	done := make(chan result, 1)

	go func() {
		buf := manager.NewWriteAtBuffer([]byte{})
		_, err := c.downloader.Download(opCtx, buf, &s3.GetObjectInput{
			Bucket: aws.String(c.bucket),
			Key:    aws.String(key),
		})
		done <- result{buf.Bytes(), err}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			return nil, res.err
		}
		return res.data, nil
	case <-opCtx.Done():
		return nil, fmt.Errorf("download timeout after 90s: %w", opCtx.Err())
	}
}

// GetObjectWithProgress downloads an object with progress callback
func (c *Client) GetObjectWithProgress(ctx context.Context, key string, progressCb func(int64, int64)) ([]byte, error) {
	opCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	buf := manager.NewWriteAtBuffer([]byte{})

	_, err := c.downloader.Download(opCtx, buf, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// ListObjects lists objects with a given prefix
func (c *Client) ListObjects(ctx context.Context, prefix string) ([]string, error) {
	var objects []string

	paginator := s3.NewListObjectsV2Paginator(c.client, &s3.ListObjectsV2Input{
		Bucket:  aws.String(c.bucket),
		Prefix:  aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			objects = append(objects, *obj.Key)
		}
	}

	return objects, nil
}

// DeleteObject deletes an object from S3
func (c *Client) DeleteObject(ctx context.Context, key string) error {
	_, err := c.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	return err
}

// CheckObjectLockSupport checks if bucket supports Object Lock
func (c *Client) CheckObjectLockSupport() (bool, error) {
	_, err := c.client.GetObjectLockConfiguration(context.Background(), &s3.GetObjectLockConfigurationInput{
		Bucket: aws.String(c.bucket),
	})
	if err != nil {
		// Check if it's the "not found" error (object lock not enabled)
		errStr := err.Error()
		if strings.Contains(errStr, "ObjectLockConfigurationNotFound") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// SetLifecyclePolicy sets lifecycle policy for retention cleanup
func (c *Client) SetLifecyclePolicy(ctx context.Context, retentionDays int) error {
	fmt.Printf("Note: Lifecycle policy must be set manually in your S3 provider's console.\n")
	fmt.Printf("Recommended policy: Delete objects in 'backups/' prefix older than %d days.\n", retentionDays+1)
	return nil
}

// GetLifecyclePolicy gets the current lifecycle policy
func (c *Client) GetLifecyclePolicy(ctx context.Context) (string, error) {
	return "Lifecycle policy check not available - please check your S3 provider's console", nil
}

// BucketExists checks if bucket exists
func (c *Client) BucketExists(ctx context.Context) (bool, error) {
	_, err := c.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(c.bucket),
	})
	if err != nil {
		return false, err
	}
	return true, nil
}
