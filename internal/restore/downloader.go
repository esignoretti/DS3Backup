package restore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/esignoretti/ds3backup/internal/crypto"
	"github.com/esignoretti/ds3backup/internal/s3client"
	"github.com/esignoretti/ds3backup/pkg/models"
	"golang.org/x/crypto/blake2b"
)

// Job represents a single file restore task
type Job struct {
	Entry    *models.FileEntry
	DestPath string
	IsBatch  bool
	BatchID  string
}

// Result represents completion status of a restore job
type Result struct {
	Entry    *models.FileEntry
	Success  bool
	Error    error
	Warnings []string // Metadata failures
	Bytes    int64
	Skipped  bool // File already existed
}

// Downloader manages parallel file downloads and restoration
type Downloader struct {
	workers    int
	jobQueue   chan *Job
	resultChan chan *Result
	wg         sync.WaitGroup
	crypto     *crypto.CryptoEngine
	s3client   *s3client.Client
	extractor  *BatchExtractor
	ctx        context.Context
	mu         sync.Mutex
}

// NewDownloader creates a new downloader with specified concurrency
func NewDownloader(workers int, s3client *s3client.Client, cryptoEngine *crypto.CryptoEngine, extractor *BatchExtractor, ctx context.Context) *Downloader {
	return &Downloader{
		workers:    workers,
		jobQueue:   make(chan *Job, workers*2),
		resultChan: make(chan *Result, workers*2),
		crypto:     cryptoEngine,
		s3client:   s3client,
		extractor:  extractor,
		ctx:        ctx,
	}
}

// Start starts the worker pool
func (d *Downloader) Start() {
	for i := 0; i < d.workers; i++ {
		d.wg.Add(1)
		go d.worker()
	}
}

// Stop signals workers to stop and waits for completion
func (d *Downloader) Stop() {
	close(d.jobQueue)
	d.wg.Wait()
	close(d.resultChan)
}

// Submit submits a job to the queue
func (d *Downloader) Submit(job *Job) {
	d.jobQueue <- job
}

// Results returns the result channel
func (d *Downloader) Results() <-chan *Result {
	return d.resultChan
}

// worker processes jobs from the queue
func (d *Downloader) worker() {
	defer d.wg.Done()

	for job := range d.jobQueue {
		result := d.processJob(job)
		d.resultChan <- result
	}
}

// processJob handles a single restore job
func (d *Downloader) processJob(job *Job) *Result {
	result := &Result{Entry: job.Entry}

	// Check if file already exists
	if _, err := os.Stat(job.DestPath); err == nil {
		result.Skipped = true
		result.Success = true
		return result
	}

	// 1. Get encrypted data
	var encryptedData []byte
	var err error

	if job.IsBatch {
		batchData, err := d.extractor.GetBatch(d.ctx, job.BatchID)
		if err != nil {
			result.Error = err
			return result
		}
		encryptedData, err = d.extractor.ExtractFile(batchData, job.Entry)
		if err != nil {
			result.Error = err
			return result
		}
	} else {
		encryptedData, err = d.s3client.GetObject(d.ctx, job.Entry.S3Key)
		if err != nil {
			result.Error = fmt.Errorf("download failed: %w", err)
			return result
		}
	}

	// 2. Deserialize encrypted file
	encFile, err := crypto.DeserializeEncryptedFile(encryptedData)
	if err != nil {
		result.Error = fmt.Errorf("deserialization failed: %w", err)
		return result
	}

	// 3. Decrypt & decompress
	decrypted, err := d.crypto.DecryptAndDecompress(encFile)
	if err != nil {
		result.Error = fmt.Errorf("decryption failed: %w", err)
		return result
	}

	// 4. Verify hash
	hash := blake2b.Sum256(decrypted)
	if string(hash[:]) != string(job.Entry.Hash) {
		result.Error = fmt.Errorf("hash verification failed")
		return result
	}

	// 5. Create directory structure
	err = os.MkdirAll(filepath.Dir(job.DestPath), 0755)
	if err != nil {
		result.Error = fmt.Errorf("failed to create directory: %w", err)
		return result
	}

	// 6. Write to disk
	err = os.WriteFile(job.DestPath, decrypted, 0644)
	if err != nil {
		result.Error = fmt.Errorf("failed to write file: %w", err)
		return result
	}

	// 7. Set metadata
	warnings, err := SetFileMetadata(job.DestPath, job.Entry.ModTime)
	result.Warnings = warnings
	result.Bytes = int64(len(decrypted))
	result.Success = true

	return result
}
