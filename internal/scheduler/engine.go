package scheduler

import (
	"log"

	"github.com/esignoretti/ds3backup/internal/config"
	"github.com/esignoretti/ds3backup/pkg/models"
)

type BackupJobRunner struct {
	cfg        *config.Config
	getJob     func(jobID string) *models.BackupJob
	runBackupFn func(job *models.BackupJob) (*models.BackupRun, error)
}

func NewBackupJobRunner(cfg *config.Config, getJob func(string) *models.BackupJob, runBackupFn func(*models.BackupJob) (*models.BackupRun, error)) *BackupJobRunner {
	return &BackupJobRunner{
		cfg:        cfg,
		getJob:     getJob,
		runBackupFn: runBackupFn,
	}
}

func (r *BackupJobRunner) RunJob(jobID string) {
	job := r.getJob(jobID)
	if job == nil {
		log.Printf("Warning: job %s not found for scheduled run", jobID)
		return
	}
	if !job.Enabled {
		log.Printf("Warning: job %s is disabled, skipping scheduled run", jobID)
		return
	}

	run, err := r.runBackupFn(job)
	if err != nil {
		log.Printf("ERROR: scheduled backup for job %s failed: %v", jobID, err)
		job.LastError = err.Error()
	} else if run != nil && run.Error != "" {
		log.Printf("Warning: scheduled backup for job %s completed with error: %s", jobID, run.Error)
		job.LastError = run.Error
	} else {
		log.Printf("Scheduled backup for job %s completed successfully", jobID)
		job.LastError = ""
	}

	if err := r.cfg.SaveConfig(); err != nil {
		log.Printf("ERROR: failed to save config after scheduled backup: %v", err)
	}
}

func (r *BackupJobRunner) RunJobAsync(jobID string) {
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("PANIC in scheduled backup for job %s: %v", jobID, rec)
			}
		}()
		r.RunJob(jobID)
	}()
}

func (r *BackupJobRunner) RunnerFactory() func(jobID string) func() {
	return func(jobID string) func() {
		return func() {
			r.RunJobAsync(jobID)
		}
	}
}
