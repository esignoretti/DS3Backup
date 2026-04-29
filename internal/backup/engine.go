package backup

import (
	"context"
	"strings"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/esignoretti/ds3backup/internal/config"
	"github.com/esignoretti/ds3backup/internal/crypto"
	"github.com/esignoretti/ds3backup/internal/index"
	"github.com/esignoretti/ds3backup/internal/s3client"
	"github.com/esignoretti/ds3backup/pkg/models"
)

// BackupEngine handles backup operations
type BackupEngine struct {
	config   *config.Config
	s3client *s3client.Client
	indexDB  *index.IndexDB
	crypto   *crypto.CryptoEngine
}

// BackupProgress holds progress information
type BackupProgress struct {
	Percent         int
	FilesProcessed  int
	TotalFiles      int
	BytesUploaded   int64
	CurrentFile     string
}

// NewBackupEngine creates a new backup engine
func NewBackupEngine(cfg *config.Config, s3 *s3client.Client, idx *index.IndexDB, crypto *crypto.CryptoEngine) *BackupEngine {
	return &BackupEngine{
		config:   cfg,
		s3client: s3,
		indexDB:  idx,
		crypto:   crypto,
	}
}

// RunBackup executes a backup job
func (e *BackupEngine) RunBackup(job *models.BackupJob, fullBackup bool, progressCb func(BackupProgress)) (*models.BackupRun, error) {
	ctx := context.Background()

	run := &models.BackupRun{
		JobID:     job.ID,
		RunTime:   time.Now(),
		Status:    "running",
		StartTime: time.Now(),
	}

	defer func() {
		run.EndTime = time.Now()
		run.Duration = run.EndTime.Sub(run.StartTime)
		e.indexDB.SaveRun(run)
	}()

	log.Printf("Starting backup for job: %s", job.Name)
	log.Printf("Source: %s", job.SourcePath)

	// Step 1: Scan directory
	log.Println("Scanning directory...")
	scanResult, err := e.indexDB.ScanDirectory(job.SourcePath, job.ID)
	if err != nil {
		run.Status = "failed"
		run.Error = err.Error()
		return run, fmt.Errorf("failed to scan directory: %w", err)
	}
	log.Printf("Found %d files (%s)", scanResult.TotalFiles, formatBytes(scanResult.TotalSize))

	// Step 2: Filter changed files (skip if full backup)
	entries := scanResult.Files
	if !fullBackup && job.LastRun != nil {
		log.Println("Filtering changed files...")
		changedEntries, err := e.indexDB.GetChangedFiles(scanResult.Files, job.ID)
		if err != nil {
			return run, err
		}
		entries = changedEntries
		run.FilesChanged = len(entries)
		log.Printf("Changed files: %d", len(entries))
	} else {
		log.Printf("Full backup or first run, processing all %d files", len(entries))
		if job.LastRun == nil {
			run.FilesChanged = len(entries) // First run = all files are "changed"
		}
	}

	// Step 3: Deduplication
	log.Println("Checking for duplicates...")
	uniqueFiles, allEntries := e.indexDB.GetUniqueFilesToBackup(entries)
	duplicates := len(entries) - len(uniqueFiles)
	if duplicates > 0 {
		log.Printf("Skipped %d duplicate files", duplicates)
	}
	run.FilesSkipped = duplicates

	// Step 4: Process files (batch small, upload large individually)
	log.Println("Processing files...")
	batchBuilder := s3client.NewBatchBuilder(s3client.DefaultBatchConfig, job.ID)

	totalFiles := len(uniqueFiles)
	for i, entry := range uniqueFiles {
		// Read file
		content, err := os.ReadFile(entry.Path)
		if err != nil {
			log.Printf("WARNING: Failed to read %s: %v", entry.Path, err)
			run.FilesFailed++
			continue
		}

		// Compress and encrypt
		encrypted, err := e.crypto.CompressAndEncrypt(content)
		if err != nil {
			log.Printf("WARNING: Failed to encrypt %s: %v", entry.Path, err)
			run.FilesFailed++
			continue
		}

		// Serialize
		serialized, err := encrypted.Serialize()
		if err != nil {
			log.Printf("WARNING: Failed to serialize %s: %v", entry.Path, err)
			run.FilesFailed++
			continue
		}

		// Decide: batch or individual upload
		if entry.Size > s3client.DefaultBatchConfig.LargeFileThreshold {
			// Large file: upload individually
			s3Key := fmt.Sprintf("backups/%s/files/%x.enc", job.ID, entry.Hash)
			err = e.s3client.PutObjectWithLock(ctx, s3Key, serialized, job.ObjectLockMode, job.RetentionDays)
			if err != nil {
				log.Printf("WARNING: Failed to upload %s: %v", entry.Path, err)
				run.FilesFailed++
				continue
			}

			// Update the entry in the slice (not the local copy)
			uniqueFiles[i].S3Key = s3Key
			uniqueFiles[i].IsInBatch = false
			uniqueFiles[i].CompressedSize = encrypted.CompressedSize

			run.FilesAdded++
			run.BytesUploaded += entry.Size
		} else {
			// Small file: add to batch
			uniqueFiles[i].IsInBatch = true
			uniqueFiles[i].CompressedSize = encrypted.CompressedSize
			
			batchBuilder.AddFile(entry.Path, entry.Hash, serialized)
			
			// Count batched files as added (will be uploaded with batch)
			run.FilesAdded++
			run.BytesUploaded += entry.Size
		}

		// Progress callback
		if progressCb != nil {
			progressCb(BackupProgress{
				Percent:        (i + 1) * 100 / totalFiles,
				FilesProcessed: i + 1,
				TotalFiles:     totalFiles,
				BytesUploaded:  run.BytesUploaded,
				CurrentFile:    entry.Path,
			})
		}
	}

	// Step 5: Upload remaining batch
	if batchBuilder.FileCount() > 0 {
		manifest, err := batchBuilder.Upload(ctx, e.s3client)
		if err != nil {
			log.Printf("WARNING: Final batch upload failed: %v", err)
			run.IndexSyncFailed = true
		} else {
			run.BatchesUploaded++
			// Update S3 keys and offsets for files in this batch
			batchS3Key := fmt.Sprintf("backups/%s/batches/%s.enc", job.ID, manifest.BatchID)
			
			// Create a map from path to manifest file ref
			fileRefMap := make(map[string]*models.BatchFileRef)
			for i := range manifest.Files {
				fileRefMap[manifest.Files[i].Path] = &manifest.Files[i]
			}
			
			for i := range uniqueFiles {
				if uniqueFiles[i].IsInBatch && uniqueFiles[i].S3Key == "" {
					uniqueFiles[i].S3Key = batchS3Key
					uniqueFiles[i].BatchID = manifest.BatchID
					
					// Get offset and length from manifest
					if ref, ok := fileRefMap[uniqueFiles[i].Path]; ok {
						uniqueFiles[i].OffsetInBatch = ref.OffsetInBatch
						uniqueFiles[i].LengthInBatch = ref.LengthInBatch
					}
				}
			}
		}
	}
	
	// Save ALL entries to index (including duplicates)
	// First, create a map from hash to unique file (for updating duplicates)
	hashMap := make(map[string]*models.FileEntry)
	for i := range uniqueFiles {
		hashKey := string(uniqueFiles[i].Hash)
		hashMap[hashKey] = &uniqueFiles[i]
	}
	
	// Update allEntries with uploaded metadata (match by hash, not path)
	for i := range allEntries {
		hashKey := string(allEntries[i].Hash)
		if unique, exists := hashMap[hashKey]; exists {
			// Copy updated metadata from uniqueFiles
			allEntries[i].S3Key = unique.S3Key
			allEntries[i].IsInBatch = unique.IsInBatch
			allEntries[i].BatchID = unique.BatchID
			allEntries[i].OffsetInBatch = unique.OffsetInBatch
			allEntries[i].LengthInBatch = unique.LengthInBatch
			allEntries[i].CompressedSize = unique.CompressedSize
		}
	}
	
	log.Printf("Saving %d entries to index...", len(allEntries))
	for i := range allEntries {
		if err := e.indexDB.SaveEntry(&allEntries[i]); err != nil {
			log.Printf("WARNING: Failed to save index entry for %s: %v", allEntries[i].Path, err)
		}
	}

	// Step 6: Create disaster recovery backup (single tar.gz file)
	log.Println("Creating disaster recovery backup...")
	if err := e.createDisasterRecoveryBackup(); err != nil {
		log.Printf("WARNING: Disaster recovery backup failed: %v", err)
		run.IndexSyncFailed = true
	} else {
		log.Println("✓ Disaster recovery backup created")
	}

	// Step 7: Apply retention
	log.Println("Applying retention policy...")
	e.applyRetention(job)

	run.Status = "completed"
	log.Printf("Backup completed: %d files added, %s uploaded", run.FilesAdded, formatBytes(run.BytesUploaded))

	// Step 8: Create disaster recovery backup
	log.Println("Creating disaster recovery backup...")
	if err := e.createDisasterRecoveryBackup(); err != nil {
		log.Printf("WARNING: Disaster recovery backup failed: %v", err)
		run.IndexSyncFailed = true
	} else {
		log.Println("✓ Disaster recovery backup created")
	}

	return run, nil
}

// syncIndexToS3 uploads the local index to S3
func (e *BackupEngine) syncIndexToS3(jobID string) error {
	// Get local index directory
	configDir, err := config.ConfigDir()
	if err != nil {
		return err
	}
	indexDir := filepath.Join(configDir, "index", jobID)
	
	// Upload live BadgerDB files (MANIFEST, *.vlog, *.mem, etc.)
	return filepath.Walk(indexDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		// Skip temp directories
		if strings.Contains(path, "/temp/") {
			return nil
		}

		relPath, _ := filepath.Rel(indexDir, path)
		s3Key := fmt.Sprintf(".ds3backup/index/%s/%s", jobID, relPath)

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		ctx := context.Background()
		if err := e.s3client.PutObject(ctx, s3Key, data); err != nil {
			return fmt.Errorf("failed to upload %s: %w", relPath, err)
		}
		return nil
	})
}

// saveIndexCopyWithBackup saves a copy of the index alongside backup files
// This allows restoring from a specific backup snapshot without relying on central index
func (e *BackupEngine) saveIndexCopyWithBackup(jobID string, backupTime time.Time) error {
	// Export BadgerDB to temp directory
	backupDir := "/tmp/ds3backup-index-copy-" + jobID
	if err := e.indexDB.Backup(backupDir); err != nil {
		return fmt.Errorf("failed to backup index: %w", err)
	}
	defer os.RemoveAll(backupDir)

	// Upload all index files to backup location
	timestamp := backupTime.Format("20060102_150405")
	return filepath.Walk(backupDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, _ := filepath.Rel(backupDir, path)
		// Save in backups/<job-id>/index_<timestamp>/ to keep with backup files
		s3Key := fmt.Sprintf("backups/%s/index_%s/%s", jobID, timestamp, relPath)

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		ctx := context.Background()
		return e.s3client.PutObject(ctx, s3Key, data)
	})
}

// createDisasterRecoveryBackup creates a complete backup of .ds3backup directory
func (e *BackupEngine) createDisasterRecoveryBackup() error {
	configDir, err := config.ConfigDir()
	if err != nil {
		return err
	}

	// Create archive file
	archivePath := filepath.Join(os.TempDir(), ".ds3backup.tar.gz")
	if err := CreateBackupArchive(configDir, archivePath); err != nil {
		return fmt.Errorf("failed to create archive: %w", err)
	}
	defer os.Remove(archivePath)

	// Calculate checksum
	checksum, err := CalculateSHA256(archivePath)
	if err != nil {
		return fmt.Errorf("failed to calculate checksum: %w", err)
	}

	// Read archive data
	data, err := os.ReadFile(archivePath)
	if err != nil {
		return err
	}

	// Upload archive to S3
	ctx := context.Background()
	if err := e.s3client.PutObject(ctx, ".ds3backup.tar.gz", data); err != nil {
		return fmt.Errorf("failed to upload archive: %w", err)
	}

	// Upload checksum
	checksumData := []byte(checksum)
	if err := e.s3client.PutObject(ctx, ".ds3backup.tar.gz.sha256", checksumData); err != nil {
		return fmt.Errorf("failed to upload checksum: %w", err)
	}

	// Verify upload by downloading and checking checksum
	uploadedData, err := e.s3client.GetObject(ctx, ".ds3backup.tar.gz")
	if err != nil {
		return fmt.Errorf("failed to verify upload: %w", err)
	}

	uploadedChecksum, err := CalculateSHA256FromReader(strings.NewReader(string(uploadedData)))
	if err != nil {
		return fmt.Errorf("failed to calculate uploaded checksum: %w", err)
	}

	if uploadedChecksum != checksum {
		return fmt.Errorf("upload verification failed: expected %s, got %s", checksum, uploadedChecksum)
	}

	log.Printf("Disaster recovery backup uploaded (%d bytes, SHA256: %s...)", len(data), checksum[:16])
	return nil
}

// applyRetention marks old backups for deletion
func (e *BackupEngine) applyRetention(job *models.BackupJob) {
	cutoff := time.Now().AddDate(0, 0, -job.RetentionDays)

	// Get old backup runs
	runs, err := e.indexDB.GetBackupHistory(job.ID, 1000)
	if err != nil {
		log.Printf("WARNING: Failed to get backup history: %v", err)
		return
	}

	// Mark expired runs (S3 lifecycle will handle actual deletion)
	for _, run := range runs {
		if run.RunTime.Before(cutoff) {
			log.Printf("Marking backup from %s for deletion (expired)", run.RunTime.Format(time.RFC3339))
			// In production: mark objects for deletion in S3
		}
	}
}

// formatBytes formats bytes as human-readable string
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
