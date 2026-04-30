package api

import (
	"time"

	"github.com/robfig/cron/v3"
	"github.com/esignoretti/ds3backup/pkg/models"
)

// DefaultAPIPort is the default port for the API server.
const DefaultAPIPort = 8099

// BackupRunner abstracts the scheduler/backup execution for API consumption.
type BackupRunner interface {
	RunJob(jobID string)
	GetScheduledJobs() []string
	IsRunning() bool
	Start()
	Stop()
}

// JobManager abstracts job config CRUD.
type JobManager interface {
	GetJob(jobID string) *models.BackupJob
	GetAllJobs() []models.BackupJob
	CreateJob(name, source, password, cronExpr string, retentionDays int, objectLockMode string) (*models.BackupJob, error)
	RemoveJob(jobID string) bool
	DeleteJob(jobID, password string, purge bool) error
}

// BackupJobWithStatus is a sanitized version of BackupJob with
// sensitive fields (EncryptionPassword) omitted from JSON output.
type BackupJobWithStatus struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	SourcePath      string     `json:"sourcePath"`
	RetentionDays   int        `json:"retentionDays"`
	ObjectLockMode  string     `json:"objectLockMode"`
	Enabled         bool       `json:"enabled"`
	CreatedAt       time.Time  `json:"createdAt"`
	LastRun         *time.Time `json:"lastRun,omitempty"`
	NextRun         time.Time  `json:"nextRun,omitempty"`
	LastError       string     `json:"lastError,omitempty"`
	ScheduleEnabled bool       `json:"scheduleEnabled"`
	CronExpr        string     `json:"cronExpr,omitempty"`
}

// CreateJobRequest is the JSON body for creating a new job.
type CreateJobRequest struct {
	Name           string `json:"name"`
	SourcePath     string `json:"sourcePath"`
	Password       string `json:"password"`
	CronExpr       string `json:"cronExpr,omitempty"`
	RetentionDays  int    `json:"retentionDays,omitempty"`
	ObjectLockMode string `json:"objectLockMode,omitempty"`
}

// sanitizeJob converts a BackupJob to a BackupJobWithStatus, omitting
// the EncryptionPassword field for safe API responses.
func sanitizeJob(job *models.BackupJob) BackupJobWithStatus {
	nextRun := job.NextRun
	if nextRun.IsZero() && job.ScheduleEnabled && job.CronExpr != "" {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if sched, err := parser.Parse(job.CronExpr); err == nil {
			nextRun = sched.Next(time.Now())
		}
	}
	return BackupJobWithStatus{
		ID:              job.ID,
		Name:            job.Name,
		SourcePath:      job.SourcePath,
		RetentionDays:   job.RetentionDays,
		ObjectLockMode:  job.ObjectLockMode,
		Enabled:         job.Enabled,
		CreatedAt:       job.CreatedAt,
		LastRun:         job.LastRun,
		NextRun:         nextRun,
		LastError:       job.LastError,
		ScheduleEnabled: job.ScheduleEnabled,
		CronExpr:        job.CronExpr,
	}
}

// StatusResponse is the response for the GET /api/v1/status endpoint.
type StatusResponse struct {
	Running           bool   `json:"running"`
	SchedulerRunning  bool   `json:"schedulerRunning"`
	ScheduledJobs     int    `json:"scheduledJobs"`
	APIPort           int    `json:"apiPort"`
	Uptime            string `json:"uptime"`
}

// JobListResponse is the response for the GET /api/v1/jobs endpoint.
type JobListResponse struct {
	Jobs []BackupJobWithStatus `json:"jobs"`
}

// JobDetailResponse is the response for the GET /api/v1/jobs/{id} endpoint.
type JobDetailResponse struct {
	Job      BackupJobWithStatus `json:"job"`
	IsScheduled bool             `json:"scheduled"`
	CronExpr string              `json:"cronExpr"`
}

// BackupTriggerResponse is the response for the POST /api/v1/backup/run/{id} endpoint.
type BackupTriggerResponse struct {
	JobID     string `json:"jobId"`
	Triggered bool   `json:"triggered"`
	Message   string `json:"message"`
}

// HistoryProvider abstracts backup run history retrieval.
type HistoryProvider interface {
	GetJobHistory(jobID string, limit int) ([]*models.BackupRun, error)
}

// HistoryResponse is the response for the GET /api/v1/jobs/{id}/history endpoint.
type HistoryResponse struct {
	JobID string             `json:"jobId"`
	Runs  []*models.BackupRun `json:"runs"`
}

// ErrorResponse is a generic error payload for the API.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  int    `json:"code"`
}

// DeleteJobRequest is the JSON body for deleting a job.
type DeleteJobRequest struct {
	Password string `json:"password"`
	Purge    bool   `json:"purge"`
}
