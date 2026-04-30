package scheduler

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/esignoretti/ds3backup/internal/config"
	"github.com/esignoretti/ds3backup/pkg/models"
)

type BackupJobRunner struct {
	cfg           *config.Config
	getJob        func(jobID string) *models.BackupJob
	runBackupFn   func(job *models.BackupJob) (*models.BackupRun, error)
	jobLocks      map[string]*sync.Mutex
	lockMu        sync.Mutex
	retryInterval time.Duration
}

func NewBackupJobRunner(cfg *config.Config, getJob func(string) *models.BackupJob, runBackupFn func(*models.BackupJob) (*models.BackupRun, error)) *BackupJobRunner {
	return &BackupJobRunner{
		cfg:           cfg,
		getJob:        getJob,
		runBackupFn:   runBackupFn,
		jobLocks:      make(map[string]*sync.Mutex),
		retryInterval: 1 * time.Minute,
	}
}

func (r *BackupJobRunner) getJobLock(jobID string) *sync.Mutex {
	r.lockMu.Lock()
	defer r.lockMu.Unlock()
	if _, ok := r.jobLocks[jobID]; !ok {
		r.jobLocks[jobID] = &sync.Mutex{}
	}
	return r.jobLocks[jobID]
}

func (r *BackupJobRunner) RunJob(jobID string) {
	lock := r.getJobLock(jobID)

	// Per-job mutex — TryLock ensures only one run at a time (D-05, D-06)
	if !lock.TryLock() {
		log.Printf("Skipping scheduled run for job %s — previous run still in progress", jobID)
		return
	}
	defer lock.Unlock()

	// 3 total attempts (initial + 2 retries) (D-01)
	maxAttempts := 3
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			log.Printf("Retry attempt %d/%d for job %s...", attempt+1, maxAttempts, jobID)
		}

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

		if err == nil && (run == nil || run.Error == "") {
			// Success! Update config and return
			job.LastError = ""
			job.RetryCount = 0
			job.NextRetryTime = time.Time{}
			if saveErr := r.cfg.SaveConfig(); saveErr != nil {
				log.Printf("ERROR: failed to save config after scheduled backup: %v", saveErr)
			}
			return
		}

		// Capture failure error
		if err != nil {
			lastErr = err
		} else if run != nil {
			lastErr = fmt.Errorf("%s", run.Error)
		}

		if attempt < maxAttempts-1 {
			log.Printf("Scheduled backup for job %s failed (attempt %d/%d): %v", jobID, attempt+1, maxAttempts, lastErr)
			// D-02: Wait before next retry (default 1 minute, short for tests)
			time.Sleep(r.retryInterval)
		}
	}

	// After all attempts exhausted, escalate to LastError (D-03)
	log.Printf("ERROR: scheduled backup for job %s failed after %d attempts: %v", jobID, maxAttempts, lastErr)
	job := r.getJob(jobID)
	if job != nil {
		job.LastError = lastErr.Error()
		job.RetryCount = 0
		job.NextRetryTime = time.Time{}
		if saveErr := r.cfg.SaveConfig(); saveErr != nil {
			log.Printf("ERROR: failed to save config after final failure: %v", saveErr)
		}
	}
}

func (r *BackupJobRunner) RunJobAsync(jobID string) {
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("PANIC in scheduled backup for job %s: %v", jobID, rec)
				// Panic is treated as failure — mark LastError on the job (D-04)
				job := r.getJob(jobID)
				if job != nil {
					job.LastError = fmt.Sprintf("panic: %v", rec)
					if saveErr := r.cfg.SaveConfig(); saveErr != nil {
						log.Printf("ERROR: failed to save config after panic: %v", saveErr)
					}
				}
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
