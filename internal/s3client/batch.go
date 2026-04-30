package s3client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/esignoretti/ds3backup/pkg/models"
)

// BatchConfig holds batching configuration
type BatchConfig struct {
	Enabled            bool
	MaxBatchSize       int64         // 10 MB
	MaxBatchFiles      int           // 500 files
	MinFilesForBatch   int           // 5 files minimum
	LargeFileThreshold int64         // 1 MB
}

// DefaultBatchConfig returns default batch configuration
var DefaultBatchConfig = BatchConfig{
	Enabled:            true,
	MaxBatchSize:       10 * 1024 * 1024,   // 10 MB
	MaxBatchFiles:      500,
	MinFilesForBatch:   5,
	LargeFileThreshold: 1 * 1024 * 1024,    // 1 MB
}

// BatchBuilder builds batch archives
type BatchBuilder struct {
	config      BatchConfig
	files       []BatchFileEntry
	currentSize int64
	buffer      bytes.Buffer
	jobID       string
}

// BatchFileEntry represents a file in a batch
type BatchFileEntry struct {
	Path     string
	Hash     []byte
	Data     []byte
	Offset   int64
	Length   int64
}

// NewBatchBuilder creates a new batch builder
func NewBatchBuilder(config BatchConfig, jobID string) *BatchBuilder {
	return &BatchBuilder{
		config: config,
		jobID:  jobID,
	}
}

// AddFile adds a file to the batch
func (b *BatchBuilder) AddFile(path string, hash []byte, data []byte) (bool, error) {
	// Check if batch would exceed limits
	if len(b.files) >= b.config.MaxBatchFiles ||
		b.currentSize+int64(len(data)) > b.config.MaxBatchSize {
		return false, nil // Batch is full
	}

	entry := BatchFileEntry{
		Path:   path,
		Hash:   hash,
		Data:   data,
		Offset: b.currentSize,
		Length: int64(len(data)),
	}

	b.buffer.Write(data)
	b.files = append(b.files, entry)
	b.currentSize += entry.Length

	return true, nil
}

// IsReady checks if batch should be uploaded
func (b *BatchBuilder) IsReady() bool {
	return len(b.files) >= b.config.MinFilesForBatch ||
		b.currentSize >= b.config.MaxBatchSize
}

// FileCount returns number of files in batch
func (b *BatchBuilder) FileCount() int {
	return len(b.files)
}

// Size returns current batch size
func (b *BatchBuilder) Size() int64 {
	return b.currentSize
}

// Upload uploads batch to S3 with optional Object Lock parameters
func (b *BatchBuilder) Upload(ctx context.Context, client *Client, lockMode string, retentionDays int) (*models.BatchManifest, error) {
	if len(b.files) == 0 {
		return nil, nil
	}

	batchID := fmt.Sprintf("batch_%d", time.Now().UnixNano())

	// Upload batch archive with Object Lock
	batchKey := fmt.Sprintf("backups/%s/batches/%s.enc", b.jobID, batchID)
	if err := client.PutObjectWithLock(ctx, batchKey, b.buffer.Bytes(), lockMode, retentionDays); err != nil {
		return nil, fmt.Errorf("failed to upload batch: %w", err)
	}

	// Create manifest
	manifest := &models.BatchManifest{
		BatchID:   batchID,
		JobID:     b.jobID,
		Files:     make([]models.BatchFileRef, len(b.files)),
		TotalSize: b.currentSize,
		FileCount: len(b.files),
		CreatedAt: time.Now(),
		Compression: "zstd",
		Encryption:  "AES-256-GCM",
	}

	for i, f := range b.files {
		manifest.Files[i] = models.BatchFileRef{
			Path:          f.Path,
			Hash:          f.Hash,
			Size:          f.Length,
			OffsetInBatch: f.Offset,
			LengthInBatch: f.Length,
		}
	}

	// Upload manifest with Object Lock
	manifestData, _ := json.Marshal(manifest)
	manifestKey := fmt.Sprintf("backups/%s/batches/%s-manifest.json.enc", b.jobID, batchID)
	if err := client.PutObjectWithLock(ctx, manifestKey, manifestData, lockMode, retentionDays); err != nil {
		return nil, fmt.Errorf("failed to upload manifest: %w", err)
	}

	return manifest, nil
}

// ExtractFile extracts a single file from a batch
func ExtractFile(ctx context.Context, client *Client, jobID, batchID string, fileRef models.BatchFileRef) ([]byte, error) {
	// Download entire batch
	batchKey := fmt.Sprintf("backups/%s/batches/%s.enc", jobID, batchID)
	batchData, err := client.GetObject(ctx, batchKey)
	if err != nil {
		return nil, fmt.Errorf("failed to download batch: %w", err)
	}

	// Extract file data from batch
	if int(fileRef.OffsetInBatch+fileRef.LengthInBatch) > len(batchData) {
		return nil, fmt.Errorf("invalid file offset in batch")
	}

	fileData := batchData[fileRef.OffsetInBatch : fileRef.OffsetInBatch+fileRef.LengthInBatch]
	return fileData, nil
}
