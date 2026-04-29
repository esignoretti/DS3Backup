package api

import (
	"fmt"
	"net/http"
	"time"
)

// handleStatus handles GET /api/v1/status.
func (s *APIServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	uptime := time.Since(s.startTime).Round(time.Second).String()
	s.mu.RUnlock()

	scheduledJobs := s.runner.GetScheduledJobs()

	s.writeJSON(w, http.StatusOK, StatusResponse{
		Running:          s.runner.IsRunning(),
		SchedulerRunning: s.runner.IsRunning(),
		ScheduledJobs:    len(scheduledJobs),
		APIPort:          s.port,
		Uptime:           uptime,
	})
}

// handleStart handles POST /api/v1/start.
func (s *APIServer) handleStart(w http.ResponseWriter, r *http.Request) {
	s.runner.Start()
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

// handleStop handles POST /api/v1/stop.
func (s *APIServer) handleStop(w http.ResponseWriter, r *http.Request) {
	s.runner.Stop()
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

// handleListJobs handles GET /api/v1/jobs.
func (s *APIServer) handleListJobs(w http.ResponseWriter, r *http.Request) {
	jobs := s.jobManager.GetAllJobs()
	sanitized := make([]BackupJobWithStatus, 0, len(jobs))
	for _, job := range jobs {
		sanitized = append(sanitized, sanitizeJob(&job))
	}
	s.writeJSON(w, http.StatusOK, JobListResponse{Jobs: sanitized})
}

// handleGetJob handles GET /api/v1/jobs/{id}.
func (s *APIServer) handleGetJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	job := s.jobManager.GetJob(jobID)
	if job == nil {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("job not found: %s", jobID))
		return
	}

	sanitized := sanitizeJob(job)
	s.writeJSON(w, http.StatusOK, JobDetailResponse{
		Job:         sanitized,
		IsScheduled: job.ScheduleEnabled,
		CronExpr:    job.CronExpr,
	})
}

// handleRunBackup handles POST /api/v1/backup/run/{id}.
func (s *APIServer) handleRunBackup(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")

	// Validate the job exists
	job := s.jobManager.GetJob(jobID)
	if job == nil {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("job not found: %s", jobID))
		return
	}

	// Trigger async backup run
	s.runner.RunJob(jobID)

	s.writeJSON(w, http.StatusAccepted, BackupTriggerResponse{
		JobID:     jobID,
		Triggered: true,
		Message:   fmt.Sprintf("backup job %s started", jobID),
	})
}
