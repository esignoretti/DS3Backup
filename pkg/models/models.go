package models

import (
	"encoding/json"
	"time"
)

// BackupJob represents a backup job configuration.
// CronExprs holds one or more cron expressions for the job's schedule.
// The custom UnmarshalJSON also handles the legacy single-cronExpr format
// for backward compatibility with existing config files.
type BackupJob struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	SourcePath       string     `json:"sourcePath"`
	RetentionDays    int        `json:"retentionDays"`
	ObjectLockMode   string     `json:"objectLockMode"`
	Enabled          bool       `json:"enabled"`
	EncryptionPassword string   `json:"encryptionPassword"`
	CreatedAt        time.Time  `json:"createdAt"`
	LastRun          *time.Time `json:"lastRun,omitempty"`
	NextRun          time.Time  `json:"nextRun"`
	LastError        string     `json:"lastError,omitempty"`
	ScheduleEnabled  bool       `json:"scheduleEnabled"`
	CronExprs        []string   `json:"cronExprs,omitempty"`
	NextRetryTime      time.Time  `json:"nextRetryTime,omitempty"`
	RetryCount         int        `json:"retryCount,omitempty"`
	RunInProgress      bool       `json:"-"`
	LastCheckpointTime time.Time  `json:"lastCheckpointTime,omitempty"`
}

// UnmarshalJSON implements custom JSON unmarshaling for BackupJob.
// It handles both the new cronExprs format and the legacy single cronExpr format
// to ensure existing config files migrate cleanly.
func (j *BackupJob) UnmarshalJSON(data []byte) error {
	// Alias to avoid infinite recursion
	type jobAlias BackupJob
	raw := struct {
		*jobAlias
		LegacyCronExpr string `json:"cronExpr,omitempty"`
	}{
		jobAlias: (*jobAlias)(j),
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	// If no new-format expressions exist but a legacy cronExpr was set, migrate it
	if len(j.CronExprs) == 0 && raw.LegacyCronExpr != "" {
		j.CronExprs = []string{raw.LegacyCronExpr}
	}
	return nil
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
