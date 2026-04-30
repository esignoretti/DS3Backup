package restore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/esignoretti/ds3backup/internal/config"
	"github.com/esignoretti/ds3backup/internal/crypto"
	"github.com/esignoretti/ds3backup/internal/index"
	"github.com/esignoretti/ds3backup/internal/s3client"
	"github.com/esignoretti/ds3backup/pkg/models"
	"golang.org/x/crypto/blake2b"
)

// RestoreEngine handles restore operations
type RestoreEngine struct {
	config    *config.Config
	s3client  *s3client.Client
	indexDB   *index.IndexDB
	crypto    *crypto.CryptoEngine
	extractor *BatchExtractor
}

// NewRestoreEngine creates a new restore engine
func NewRestoreEngine(cfg *config.Config, s3 *s3client.Client, idx *index.IndexDB, cryptoEngine *crypto.CryptoEngine, jobID string) *RestoreEngine {
	return &RestoreEngine{
		config:    cfg,
		s3client:  s3,
		indexDB:   idx,
		crypto:    cryptoEngine,
		extractor: NewBatchExtractor(s3, jobID),
	}
}

// Restore executes a restore operation
func (e *RestoreEngine) Restore(jobID string, opts *models.RestoreOptions) (*models.RestoreResult, error) {
	startTime := time.Now()

	entries, err := e.indexDB.GetAllEntries(jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get file entries: %w", err)
	}

	if len(entries) == 0 {
		return &models.RestoreResult{}, nil
	}

	if len(opts.IncludePatterns) > 0 || len(opts.ExcludePatterns) > 0 {
		entries = e.filterEntries(entries, opts)
	}

	e.extractor = NewBatchExtractor(e.s3client, jobID)
	defer e.extractor.ClearCache()

	result, err := e.runDownloaderPipeline(entries, opts, nil)
	if err != nil {
		return result, err
	}
	result.Duration = int64(time.Since(startTime).Seconds())
	return result, nil
}

// runDownloaderPipeline is the shared restore pipeline used by Restore,
// RestoreWithProgress, and RestoreEntries. It handles filtering by overwrite
// rules, dry-run detection, job submission, and result collection.
//
// When tracker is non-nil, results are collected in a goroutine with mutex
// protection and progress updates. When tracker is nil, results are collected
// synchronously (used by Restore).
func (e *RestoreEngine) runDownloaderPipeline(entries []*models.FileEntry, opts *models.RestoreOptions, tracker *ProgressTracker) (*models.RestoreResult, error) {
	// Filter by overwrite rules
	var filesToRestore []*models.FileEntry
	var filesSkipped int

	for _, entry := range entries {
		destPath := e.getDestinationPath(entry, opts)
		if !opts.Overwrite {
			if _, err := os.Stat(destPath); err == nil {
				filesSkipped++
				continue
			}
		}
		filesToRestore = append(filesToRestore, entry)
	}

	if opts.DryRun {
		return &models.RestoreResult{
			FilesRestored: 0,
			FilesSkipped:  filesSkipped,
			BytesRestored: 0,
		}, nil
	}

	ctx := context.Background()
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 8
	}

	downloader := NewDownloader(concurrency, e.s3client, e.crypto, e.extractor, ctx)
	downloader.Start()

	result := &models.RestoreResult{
		FilesSkipped: filesSkipped,
		Warnings:     []string{},
		Errors:       []string{},
	}

	if tracker != nil {
		// Collect results with mutex + tracker updates
		var mu sync.Mutex
		done := make(chan struct{})
		go func() {
			for res := range downloader.Results() {
				if res.Skipped {
					mu.Lock()
					result.FilesSkipped++
					mu.Unlock()
					tracker.Update(res.Entry.Path, res.Bytes, true)
				} else if res.Success {
					mu.Lock()
					result.FilesRestored++
					result.BytesRestored += res.Bytes
					if len(res.Warnings) > 0 {
						result.Warnings = append(result.Warnings, res.Warnings...)
					}
					mu.Unlock()
					tracker.Update(res.Entry.Path, res.Bytes, false)
				} else {
					mu.Lock()
					result.FilesFailed++
					if res.Error != nil {
						result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", res.Entry.Path, res.Error))
					}
					mu.Unlock()
				}
			}
			close(done)
		}()

		// Submit jobs
		for _, entry := range filesToRestore {
			destPath := e.getDestinationPath(entry, opts)
			downloader.Submit(&Job{
				Entry:    entry,
				DestPath: destPath,
				IsBatch:  entry.IsInBatch,
				BatchID:  entry.BatchID,
			})
		}

		downloader.Stop()
		<-done
		return result, nil
	}

	// No tracker: synchronous result collection
	for _, entry := range filesToRestore {
		destPath := e.getDestinationPath(entry, opts)
		downloader.Submit(&Job{
			Entry:    entry,
			DestPath: destPath,
			IsBatch:  entry.IsInBatch,
			BatchID:  entry.BatchID,
		})
	}

	processed := 0
	total := len(filesToRestore)
	for processed < total {
		select {
		case res := <-downloader.Results():
			processed++
			if res.Skipped {
				result.FilesSkipped++
			} else if res.Success {
				result.FilesRestored++
				result.BytesRestored += res.Bytes
				result.Warnings = append(result.Warnings, res.Warnings...)
			} else {
				result.FilesFailed++
				if res.Error != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", res.Entry.Path, res.Error))
				}
			}
		case <-ctx.Done():
			downloader.Stop()
			return result, ctx.Err()
		}
	}

	downloader.Stop()
	return result, nil
}

// DryRun performs a dry-run restore (preview only)
func (e *RestoreEngine) DryRun(jobID string, opts *models.RestoreOptions) (*models.DryRunResult, error) {
	// Get all entries
	entries, err := e.indexDB.GetAllEntries(jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get file entries: %w", err)
	}

	// Filter by patterns
	if len(opts.IncludePatterns) > 0 || len(opts.ExcludePatterns) > 0 {
		entries = e.filterEntries(entries, opts)
	}

	// Count existing files and calculate size
	existingCount := 0
	var totalSize int64
	sampleFiles := make([]*models.FileEntry, 0, 20)

	for i, entry := range entries {
		destPath := e.getDestinationPath(entry, opts)

		if _, err := os.Stat(destPath); err == nil {
			existingCount++
		}

		totalSize += entry.Size

		if i < 20 {
			sampleFiles = append(sampleFiles, entry)
		}
	}

	moreFiles := len(entries) - 20
	if moreFiles < 0 {
		moreFiles = 0
	}

	return &models.DryRunResult{
		FilesToRestore: len(entries) - existingCount,
		FilesSkipped:   existingCount,
		TotalSize:      totalSize,
		SampleFiles:    sampleFiles,
		MoreFiles:      moreFiles,
	}, nil
}

// Verify verifies backup integrity without restoring
func (e *RestoreEngine) Verify(jobID string) (*models.VerifyResult, error) {
	ctx := context.Background()

	entries, err := e.indexDB.GetAllEntries(jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get file entries: %w", err)
	}

	result := &models.VerifyResult{
		Total: len(entries),
	}

	for _, entry := range entries {
		// Download
		data, err := e.s3client.GetObject(ctx, entry.S3Key)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", entry.Path, err))
			continue
		}

		// Deserialize
		encFile, err := crypto.DeserializeEncryptedFile(data)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", entry.Path, err))
			continue
		}

		// Decrypt & decompress
		decrypted, err := e.crypto.DecryptAndDecompress(encFile)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", entry.Path, err))
			continue
		}

		// Verify hash
		hash := blake2b.Sum256(decrypted)
		if string(hash[:]) != string(entry.Hash) {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: hash mismatch", entry.Path))
			continue
		}

		result.Verified++
	}

	return result, nil
}

// filterEntries filters entries by include/exclude patterns
func (e *RestoreEngine) filterEntries(entries []*models.FileEntry, opts *models.RestoreOptions) []*models.FileEntry {
	filtered := make([]*models.FileEntry, 0, len(entries))

	for _, entry := range entries {
		include := true

		// Check include patterns
		if len(opts.IncludePatterns) > 0 {
			include = false
			for _, pattern := range opts.IncludePatterns {
				if matchPattern(entry.Path, pattern) {
					include = true
					break
				}
			}
		}

		// Check exclude patterns
		if len(opts.ExcludePatterns) > 0 {
			for _, pattern := range opts.ExcludePatterns {
				if matchPattern(entry.Path, pattern) {
					include = false
					break
				}
			}
		}

		if include {
			filtered = append(filtered, entry)
		}
	}

	return filtered
}

// getDestinationPath determines the destination path for a file
func (e *RestoreEngine) getDestinationPath(entry *models.FileEntry, opts *models.RestoreOptions) string {
	if opts.DestinationPath == "" {
		// Restore to original path
		return entry.Path
	}

	// Restore to alternate location, preserving full path structure
	// /Users/docs/file.pdf → /tmp/restore/Users/docs/file.pdf
	return filepath.Join(opts.DestinationPath, entry.Path)
}

// matchPattern checks if a path matches a glob pattern with ** support
func matchPattern(path, pattern string) bool {
	// Handle ** (globstar) pattern for recursive matching
	if strings.Contains(pattern, "**") {
		// First escape all regex special chars except * and ?
		regexPattern := regexp.QuoteMeta(pattern)
		// Then convert escaped ** to match any path
		regexPattern = strings.ReplaceAll(regexPattern, `\*\*/`, `(?:.*/)?`)
		regexPattern = strings.ReplaceAll(regexPattern, `\*\*`, `.*`)
		// Convert escaped * to match single segment
		regexPattern = strings.ReplaceAll(regexPattern, `\*`, `[^/]*`)
		regexPattern = strings.ReplaceAll(regexPattern, `\?`, `.`)
		regexPattern = "^" + regexPattern + "$"
		
		matched, err := regexp.MatchString(regexPattern, path)
		if err != nil {
			return false
		}
		return matched
	}
	
	// Simple glob pattern
	matched, err := filepath.Match(pattern, path)
	if err != nil {
		return false
	}
	return matched
}
// RestoreWithProgress restores files with progress tracking
func (e *RestoreEngine) RestoreWithProgress(jobID string, opts *models.RestoreOptions, tracker *ProgressTracker) (*models.RestoreResult, error) {
	entries, err := e.indexDB.GetAllEntries(jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get index entries: %w", err)
	}

	filtered := e.filterEntries(entries, opts)
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no files match the specified patterns")
	}

	e.extractor = NewBatchExtractor(e.s3client, jobID)
	defer e.extractor.ClearCache()

	return e.runDownloaderPipeline(filtered, opts, tracker)
}

// RestoreEntries restores specific file entries with progress tracking
func (e *RestoreEngine) RestoreEntries(jobID string, opts *models.RestoreOptions, tracker *ProgressTracker, entries []*models.FileEntry) (*models.RestoreResult, error) {
	if len(entries) == 0 {
		return &models.RestoreResult{}, nil
	}

	e.extractor = NewBatchExtractor(e.s3client, jobID)
	defer e.extractor.ClearCache()

	return e.runDownloaderPipeline(entries, opts, tracker)
}

// ResumeRestore resumes an interrupted restore from saved state
func (e *RestoreEngine) ResumeRestore(
	state *RestoreState,
	stateDir string,
	opts *models.RestoreOptions,
	tracker *ProgressTracker,
	filesToRestore []*FileState,
) (*models.RestoreResult, error) {
	if len(filesToRestore) == 0 {
		return &models.RestoreResult{}, nil
	}

	// Convert FileState back to FileEntry for processing
	// Note: We need to fetch entries from index to get full details
	// For now, we'll work with what we have in state
	
	// Create download channel
	downloadChan := make(chan *FileState, len(filesToRestore))
	for _, fileState := range filesToRestore {
		downloadChan <- fileState
	}
	close(downloadChan)

	// Result tracking
	result := &models.RestoreResult{
		FilesRestored: 0,
		FilesSkipped:  0,
		BytesRestored: 0,
		Warnings:      []string{},
		Errors:        []string{},
	}

	var mu sync.Mutex
	numWorkers := opts.Concurrency
	if numWorkers <= 0 {
		numWorkers = 8
	}

	// Create downloader V2
	ctx := context.Background()
	downloader := NewDownloaderV2(
		numWorkers,
		e.s3client,
		e.crypto,
		e.extractor,
		ctx,
		state,
		stateDir,
		3,  // maxRetries
		1,  // retryDelay (seconds)
	)
	downloader.Start()

	// Submit jobs
	for fileState := range downloadChan {
		// We need to get the full entry from index
		// For MVP, we'll create a minimal entry
		entry := &models.FileEntry{
			Path:      fileState.Path,
			Size:      fileState.Size,
			Hash:      fileState.Hash,
			S3Key:     fileState.S3Key,
			IsInBatch: fileState.BatchID != "",
			BatchID:   fileState.BatchID,
		}
		
		job := &DownloadJob{
			Entry:    entry,
			DestPath: fileState.Path,
			IsBatch:  fileState.BatchID != "",
			BatchID:  fileState.BatchID,
		}
		downloader.Submit(job)
	}

	downloader.Stop()

	// Collect results
	for res := range downloader.Results() {
		if res.Skipped {
			mu.Lock()
			result.FilesSkipped++
			mu.Unlock()
		} else if res.Success {
			mu.Lock()
			result.FilesRestored++
			result.BytesRestored += res.Bytes
			if len(res.Warnings) > 0 {
				result.Warnings = append(result.Warnings, res.Warnings...)
			}
			mu.Unlock()
		} else {
			mu.Lock()
			result.FilesFailed++
			if res.Error != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", res.DestPath, res.Error))
			}
			mu.Unlock()
		}
	}

	return result, nil
}
