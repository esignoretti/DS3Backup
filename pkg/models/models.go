package models

import (
	"encoding/json"
	"time"
)

// ScheduleEntry represents a single cron schedule for a job,
// with an optional flag to force a full backup on each run.
type ScheduleEntry struct {
	Expr      string `json:"expr"`
	FullBackup bool  `json:"fullBackup,omitempty"`
}

// BackupJob represents a backup job configuration.
// Schedules holds the job's cron schedules (each with optional full backup flag).
// CronExprs is kept for backward-compatible deserialization of old config files.
type BackupJob struct {
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	SourcePath         string          `json:"sourcePath"`
	RetentionDays      int             `json:"retentionDays"`
	ObjectLockMode     string          `json:"objectLockMode"`
	Enabled            bool            `json:"enabled"`
	EncryptionPassword string          `json:"encryptionPassword"`
	CreatedAt          time.Time       `json:"createdAt"`
	LastRun            *time.Time      `json:"lastRun,omitempty"`
	NextRun            time.Time       `json:"nextRun"`
	LastError          string          `json:"lastError,omitempty"`
	ScheduleEnabled    bool            `json:"scheduleEnabled"`
	CronExprs          []string        `json:"cronExprs,omitempty"`
	Schedules          []ScheduleEntry `json:"schedules,omitempty"`
	NextRetryTime      time.Time       `json:"nextRetryTime,omitempty"`
	RetryCount         int             `json:"retryCount,omitempty"`
	RunInProgress      bool            `json:"-"`
	LastCheckpointTime time.Time       `json:"lastCheckpointTime,omitempty"`
}

// UnmarshalJSON implements custom JSON unmarshaling for BackupJob.
// It handles both the new schedules format and the legacy single cronExpr/cronExprs format.
func (j *BackupJob) UnmarshalJSON(data []byte) error {
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
	// Migrate legacy cronExprs to schedules if no schedules yet
	if len(j.Schedules) == 0 {
		if len(j.CronExprs) > 0 {
			for _, expr := range j.CronExprs {
				if expr != "" {
					j.Schedules = append(j.Schedules, ScheduleEntry{Expr: expr})
				}
			}
			j.CronExprs = nil
		} else if raw.LegacyCronExpr != "" {
			j.Schedules = append(j.Schedules, ScheduleEntry{Expr: raw.LegacyCronExpr})
		}
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
