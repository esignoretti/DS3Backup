package restore

import (
	"context"
	"fmt"
	"sync"

	"github.com/esignoretti/ds3backup/internal/s3client"
	"github.com/esignoretti/ds3backup/pkg/models"
)

// BatchExtractor handles batch file extraction with caching
type BatchExtractor struct {
	mu      sync.RWMutex
	cache   map[string][]byte // batchID -> downloaded encrypted data
	s3client *s3client.Client
	jobID   string
}

// NewBatchExtractor creates a new batch extractor
func NewBatchExtractor(s3client *s3client.Client, jobID string) *BatchExtractor {
	return &BatchExtractor{
		cache:    make(map[string][]byte),
		s3client: s3client,
		jobID:    jobID,
	}
}

// GetBatch downloads and caches a batch from S3
func (e *BatchExtractor) GetBatch(ctx context.Context, batchID string) ([]byte, error) {
	e.mu.RLock()
	if data, ok := e.cache[batchID]; ok {
		e.mu.RUnlock()
		return data, nil
	}
	e.mu.RUnlock()

	// Download from S3
	s3Key := fmt.Sprintf("backups/%s/batches/%s.enc", e.jobID, batchID)
	data, err := e.s3client.GetObject(ctx, s3Key)
	if err != nil {
		return nil, fmt.Errorf("failed to download batch %s: %w", batchID, err)
	}

	// Cache for other files in same batch
	e.mu.Lock()
	e.cache[batchID] = data
	e.mu.Unlock()

	return data, nil
}

// ExtractFile extracts a single file from batch data
func (e *BatchExtractor) ExtractFile(batchData []byte, entry *models.FileEntry) ([]byte, error) {
	if entry.OffsetInBatch < 0 || entry.LengthInBatch <= 0 {
		return nil, fmt.Errorf("invalid batch offsets for %s", entry.Path)
	}

	start := entry.OffsetInBatch
	end := start + entry.LengthInBatch

	if end > int64(len(batchData)) {
		return nil, fmt.Errorf("batch data too short for %s", entry.Path)
	}

	// Extract bytes from batch
	extracted := make([]byte, entry.LengthInBatch)
	copy(extracted, batchData[start:end])

	return extracted, nil
}

// ClearCache clears the batch cache (free memory)
func (e *BatchExtractor) ClearCache() {
	e.mu.Lock()
	e.cache = make(map[string][]byte)
	e.mu.Unlock()
}
