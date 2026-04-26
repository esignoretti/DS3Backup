package restore

import (
	"context"
	"strings"
	"time"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/esignoretti/ds3backup/internal/crypto"
	"github.com/esignoretti/ds3backup/internal/s3client"
	"github.com/esignoretti/ds3backup/pkg/models"
	"golang.org/x/crypto/blake2b"
)

// DownloadJob represents a single file download task
type DownloadJob struct {
	Entry    *models.FileEntry
	DestPath string
	IsBatch  bool
	BatchID  string
}

// DownloadResult represents the result of a download job
type DownloadResult struct {
	Entry    *models.FileEntry
	DestPath string
	Success  bool
	Error    error
	Warnings []string
	Bytes    int64
	Skipped  bool
	Retries  int
}

// DownloaderV2 manages parallel file downloads with resume support
type DownloaderV2 struct {
	workers       int
	jobQueue      chan *DownloadJob
	resultChan    chan *DownloadResult
	wg            sync.WaitGroup
	crypto        *crypto.CryptoEngine
	s3client      *s3client.Client
	extractor     *BatchExtractor
	ctx           context.Context
	state         *RestoreState
	stateDir      string
	maxRetries    int
	retryDelay    int // seconds
	mu            sync.Mutex
}

// NewDownloaderV2 creates a new downloader with resume support
func NewDownloaderV2(
	workers int,
	s3client *s3client.Client,
	cryptoEngine *crypto.CryptoEngine,
	extractor *BatchExtractor,
	ctx context.Context,
	state *RestoreState,
	stateDir string,
	maxRetries int,
	retryDelay int,
) *DownloaderV2 {
	return &DownloaderV2{
		workers:    workers,
		jobQueue:   make(chan *DownloadJob, workers*2),
		resultChan: make(chan *DownloadResult, workers*2),
		crypto:     cryptoEngine,
		s3client:   s3client,
		extractor:  extractor,
		ctx:        ctx,
		state:      state,
		stateDir:   stateDir,
		maxRetries: maxRetries,
		retryDelay: retryDelay,
	}
}

// Start starts the worker pool
func (d *DownloaderV2) Start() {
	for i := 0; i < d.workers; i++ {
		d.wg.Add(1)
		go d.worker()
	}
}

// Stop signals workers to stop and waits for completion
func (d *DownloaderV2) Stop() {
	close(d.jobQueue)
	d.wg.Wait()
	close(d.resultChan)
}

// Submit submits a job to the queue
func (d *DownloaderV2) Submit(job *DownloadJob) {
	d.jobQueue <- job
}

// Results returns the result channel
func (d *DownloaderV2) Results() <-chan *DownloadResult {
	return d.resultChan
}

// worker processes jobs from the queue
func (d *DownloaderV2) worker() {
	defer d.wg.Done()

	for job := range d.jobQueue {
		result := d.processJobWithRetry(job)
		d.resultChan <- result
	}
}

// processJobWithRetry processes a job with retry logic
func (d *DownloaderV2) processJobWithRetry(job *DownloadJob) *DownloadResult {
	var lastResult *DownloadResult
	
	for attempt := 0; attempt <= d.maxRetries; attempt++ {
		result := d.processJob(job)
		lastResult = result
		
		if result.Success || result.Skipped {
			return result
		}
		
		// Don't retry certain errors
		if d.isNonRetryableError(result.Error) {
			return result
		}
		
		// Wait before retry (exponential backoff)
		if attempt < d.maxRetries {
			delay := d.retryDelay * (1 << uint(attempt)) // 1s, 2s, 4s, ...
			select {
			case <-time.After(time.Duration(delay) * time.Second):
				// Continue to next attempt
			case <-d.ctx.Done():
				result.Error = fmt.Errorf("cancelled: %w", d.ctx.Err())
				return result
			}
		}
	}
	
	return lastResult
}

// isNonRetryableError checks if an error should not be retried
func (d *DownloaderV2) isNonRetryableError(err error) bool {
	if err == nil {
		return false
	}
	
	errStr := err.Error()
	// Don't retry: hash mismatches, decryption failures, permission errors
	return containsAny(errStr, []string{
		"hash verification failed",
		"decryption failed",
		"permission denied",
		"deserialization failed",
	})
}

func containsAny(s string, substrings []string) bool {
	for _, sub := range substrings {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// processJob handles a single download job
func (d *DownloaderV2) processJob(job *DownloadJob) *DownloadResult {
	result := &DownloadResult{
		Entry:    job.Entry,
		DestPath: job.DestPath,
	}

	// Check if file already exists (skip if not overwriting)
	if _, err := os.Stat(job.DestPath); err == nil {
		result.Skipped = true
		result.Success = true
		d.state.MarkSkipped(job.DestPath, job.Entry.Size)
		return result
	}

	// Check if partial file exists
	partialPath := job.DestPath + ".partial"
	if _, err := os.Stat(partialPath); err == nil {
		// For MVP: remove partial and re-download
		// TODO: Implement byte-range resume
		os.Remove(partialPath)
	}

	// 1. Get encrypted data
	var encryptedData []byte
	var err error

	if job.IsBatch {
		batchData, err := d.extractor.GetBatch(d.ctx, job.BatchID)
		if err != nil {
			result.Error = fmt.Errorf("failed to get batch: %w", err)
			d.state.MarkFailed(job.DestPath, result.Error)
			return result
		}
		encryptedData, err = d.extractor.ExtractFile(batchData, job.Entry)
		if err != nil {
			result.Error = fmt.Errorf("failed to extract file: %w", err)
			d.state.MarkFailed(job.DestPath, result.Error)
			return result
		}
	} else {
		encryptedData, err = d.s3client.GetObject(d.ctx, job.Entry.S3Key)
		if err != nil {
			result.Error = fmt.Errorf("download failed: %w", err)
			d.state.MarkFailed(job.DestPath, result.Error)
			return result
		}
	}

	// Update state: downloading
	d.state.UpdateFileProgress(job.DestPath, int64(len(encryptedData)))

	// 2. Deserialize encrypted file
	encFile, err := crypto.DeserializeEncryptedFile(encryptedData)
	if err != nil {
		result.Error = fmt.Errorf("deserialization failed: %w", err)
		d.state.MarkFailed(job.DestPath, result.Error)
		return result
	}

	// 3. Decrypt & decompress
	decrypted, err := d.crypto.DecryptAndDecompress(encFile)
	if err != nil {
		result.Error = fmt.Errorf("decryption failed: %w", err)
		d.state.MarkFailed(job.DestPath, result.Error)
		return result
	}

	// 4. Verify hash
	hash := blake2b.Sum256(decrypted)
	if string(hash[:]) != string(job.Entry.Hash) {
		result.Error = fmt.Errorf("hash verification failed")
		d.state.MarkFailed(job.DestPath, result.Error)
		return result
	}

	// 5. Create directory structure
	err = os.MkdirAll(filepath.Dir(job.DestPath), 0755)
	if err != nil {
		result.Error = fmt.Errorf("failed to create directory: %w", err)
		d.state.MarkFailed(job.DestPath, result.Error)
		return result
	}

	// 6. Write to partial file first (atomic write)
	partialPath = job.DestPath + ".partial"
	err = os.WriteFile(partialPath, decrypted, 0644)
	if err != nil {
		result.Error = fmt.Errorf("failed to write file: %w", err)
		d.state.MarkFailed(job.DestPath, result.Error)
		return result
	}

	// 7. Rename to final path (atomic)
	err = os.Rename(partialPath, job.DestPath)
	if err != nil {
		result.Error = fmt.Errorf("failed to finalize file: %w", err)
		d.state.MarkFailed(job.DestPath, result.Error)
		return result
	}

	// 8. Set metadata
	warnings, err := SetFileMetadata(job.DestPath, job.Entry.ModTime)
	result.Warnings = warnings
	result.Bytes = int64(len(decrypted))
	result.Success = true

	// Update state: completed
	d.state.MarkComplete(job.DestPath, result.Bytes)

	return result
}
