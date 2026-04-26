package models

import (
	"time"
)

// BackupJob represents a backup job configuration
type BackupJob struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	SourcePath     string    `json:"sourcePath"`
	RetentionDays  int       `json:"retentionDays"`
	ObjectLockMode string    `json:"objectLockMode"` // "GOVERNANCE" or "COMPLIANCE"
	Enabled        bool      `json:"enabled"`
	CreatedAt      time.Time `json:"createdAt"`
	LastRun        *time.Time `json:"lastRun,omitempty"`
	NextRun        time.Time `json:"nextRun"`
	LastError      string    `json:"lastError,omitempty"`
}

// FileEntry represents a file in the backup index
type FileEntry struct {
	Path          string    `json:"path"`
	Size          int64     `json:"size"`
	ModTime       time.Time `json:"modTime"`
	Hash          []byte    `json:"hash"`
	BackupTime    time.Time `json:"backupTime"`
	JobID         string    `json:"jobId"`
	S3Key         string    `json:"s3Key"`
	IsInBatch     bool      `json:"isInBatch"`
	BatchID       string    `json:"batchId,omitempty"`
	OffsetInBatch int64     `json:"offsetInBatch,omitempty"`
	LengthInBatch int64     `json:"lengthInBatch,omitempty"`
	IsDuplicate   bool       `json:"isDuplicate"`
	OriginalSize  int64     `json:"originalSize"`
	CompressedSize int64    `json:"compressedSize"`
}

// BackupRun represents a single backup execution
type BackupRun struct {
	JobID           string        `json:"jobId"`
	RunTime         time.Time     `json:"runTime"`
	Status          string        `json:"status"` // "running", "completed", "failed"
	FilesAdded      int           `json:"filesAdded"`
	FilesChanged    int           `json:"filesChanged"`
	FilesSkipped    int           `json:"filesSkipped"`
	FilesFailed     int           `json:"filesFailed"`
	BytesUploaded   int64         `json:"bytesUploaded"`
	BatchesUploaded int           `json:"batchesUploaded"`
	Duration        time.Duration `json:"duration"`
	Error           string        `json:"error,omitempty"`
	IndexSyncFailed bool          `json:"indexSyncFailed"`
	StartTime       time.Time     `json:"startTime"`
	EndTime         time.Time     `json:"endTime"`
}

// Schedule represents a backup schedule (for future use)
type Schedule struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"` // "15min", "hourly", "daily", "weekly", "custom"
	CronExpr  string    `json:"cronExpr"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
}

// BatchManifest represents metadata for a batch archive
type BatchManifest struct {
	BatchID      string        `json:"batchId"`
	JobID        string        `json:"jobId"`
	Files        []BatchFileRef `json:"files"`
	TotalSize    int64         `json:"totalSize"`
	FileCount    int           `json:"fileCount"`
	CreatedAt    time.Time     `json:"createdAt"`
	Compression  string        `json:"compression"`
	Encryption   string        `json:"encryption"`
}

// BatchFileRef represents a file reference within a batch
type BatchFileRef struct {
	Path          string `json:"path"`
	Hash          []byte `json:"hash"`
	Size          int64  `json:"size"`
	OffsetInBatch int64  `json:"offsetInBatch"`
	LengthInBatch int64  `json:"lengthInBatch"`
	OriginalSize  int64  `json:"originalSize"`
}
