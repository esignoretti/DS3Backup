package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/esignoretti/ds3backup/pkg/models"
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

// handleCreateJob handles POST /api/v1/jobs.
func (s *APIServer) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.SourcePath == "" || req.Password == "" {
		s.writeError(w, http.StatusBadRequest, "name, sourcePath, and password are required")
		return
	}
	job, err := s.jobManager.CreateJob(req.Name, req.SourcePath, req.Password, req.CronExpr)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusCreated, sanitizeJob(job))
}

// handleGetJobHistory handles GET /api/v1/jobs/{id}/history.
func (s *APIServer) handleGetJobHistory(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")

	job := s.jobManager.GetJob(jobID)
	if job == nil {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("job not found: %s", jobID))
		return
	}

	limit := 20
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	if s.historyProvider == nil {
		s.writeJSON(w, http.StatusOK, HistoryResponse{
			JobID: jobID,
			Runs:  []*models.BackupRun{},
		})
		return
	}

	runs, err := s.historyProvider.GetJobHistory(jobID, limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get history: %s", err.Error()))
		return
	}

	if runs == nil {
		runs = []*models.BackupRun{}
	}

	s.writeJSON(w, http.StatusOK, HistoryResponse{
		JobID: jobID,
		Runs:  runs,
	})
}

// handleGetLogs handles GET /api/v1/logs.
func (s *APIServer) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	logPath := s.logPath
	if logPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "cannot determine log path")
			return
		}
		logPath = filepath.Join(home, ".ds3backup", "ds3backup.log")
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("log file not found: %s", logPath))
		return
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) > 200 {
		lines = lines[len(lines)-200:]
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(strings.Join(lines, "\n")))
}
