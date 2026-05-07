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

func (s *APIServer) handleStart(w http.ResponseWriter, r *http.Request) {
	s.runner.Start()
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

func (s *APIServer) handleStop(w http.ResponseWriter, r *http.Request) {
	s.runner.Stop()
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (s *APIServer) handleListJobs(w http.ResponseWriter, r *http.Request) {
	jobs := s.jobManager.GetAllJobs()
	sanitized := make([]BackupJobWithStatus, 0, len(jobs))
	for _, job := range jobs {
		sanitized = append(sanitized, sanitizeJob(&job))
	}
	s.writeJSON(w, http.StatusOK, JobListResponse{Jobs: sanitized})
}

func (s *APIServer) handleGetJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	job := s.jobManager.GetJob(jobID)
	if job == nil {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("job not found: %s", jobID))
		return
	}

	sanitized := sanitizeJob(job)

	// Backward-compat: build cronExprs from Schedules, fallback to CronExprs
	cronExprs := job.CronExprs
	if len(job.Schedules) > 0 {
		cronExprs = make([]string, len(job.Schedules))
		for i, s := range job.Schedules {
			cronExprs[i] = s.Expr
		}
	}
	s.writeJSON(w, http.StatusOK, JobDetailResponse{
		Job:         sanitized,
		IsScheduled: job.ScheduleEnabled,
		CronExprs:   cronExprs,
	})
}

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

	// Backward compat: if cronExprs is empty but old cronExpr is set, convert
	cronExprs := req.CronExprs
	if len(cronExprs) == 0 && req.CronExpr != "" {
		cronExprs = []string{req.CronExpr}
	}

	retentionDays := req.RetentionDays
	if retentionDays <= 0 {
		retentionDays = 30
	}
	objectLockMode := req.ObjectLockMode
	if objectLockMode == "" {
		objectLockMode = "NONE"
	}
	job, err := s.jobManager.CreateJob(req.Name, req.SourcePath, req.Password, cronExprs, retentionDays, objectLockMode)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusCreated, sanitizeJob(job))
}

func (s *APIServer) handleRunBackup(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	job := s.jobManager.GetJob(jobID)
	if job == nil {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("job not found: %s", jobID))
		return
	}
	go s.runner.RunJob(jobID)
	s.writeJSON(w, http.StatusAccepted, BackupTriggerResponse{
		JobID:     jobID,
		Triggered: true,
		Message:   fmt.Sprintf("backup job %s started", jobID),
	})
}

func (s *APIServer) handlePatchJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	job := s.jobManager.GetJob(jobID)
	if job == nil {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("job not found: %s", jobID))
		return
	}

	var req struct {
		CronExprs       *[]string              `json:"cronExprs"`
		CronExpr        *string                `json:"cronExpr"` // Deprecated: use cronExprs
		ScheduleEnabled *bool                  `json:"scheduleEnabled"`
		Schedules       *[]models.ScheduleEntry `json:"schedules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Schedules != nil {
		if err := s.jobManager.RescheduleJob(jobID, *req.Schedules); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else if req.CronExprs != nil || req.CronExpr != nil {
		var newExprs []string
		if req.CronExprs != nil {
			newExprs = *req.CronExprs
		} else if req.CronExpr != nil && *req.CronExpr != "" {
			newExprs = []string{*req.CronExpr}
		}
		schedules := make([]models.ScheduleEntry, len(newExprs))
		for i, e := range newExprs {
			schedules[i] = models.ScheduleEntry{Expr: e}
		}
		if err := s.jobManager.RescheduleJob(jobID, schedules); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if req.ScheduleEnabled != nil {
		job.ScheduleEnabled = *req.ScheduleEnabled
		if err := s.jobManager.UpdateJob(job); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *APIServer) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")

	job := s.jobManager.GetJob(jobID)
	if job == nil {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("job not found: %s", jobID))
		return
	}

	var req DeleteJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Password == "" {
		s.writeError(w, http.StatusBadRequest, "password is required")
		return
	}

	if err := s.jobManager.DeleteJob(jobID, req.Password, req.Purge); err != nil {
		s.writeError(w, http.StatusForbidden, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *APIServer) handleRestore(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")

	var req struct {
		Password        string   `json:"password"`
		Path            string   `json:"path,omitempty"`
		Time            string   `json:"time,omitempty"`
		Overwrite       bool     `json:"overwrite,omitempty"`
		IncludePatterns []string `json:"includePatterns,omitempty"`
		ExcludePatterns []string `json:"excludePatterns,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Password == "" {
		s.writeError(w, http.StatusBadRequest, "password is required")
		return
	}

	job := s.jobManager.GetJob(jobID)
	if job == nil {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("job not found: %s", jobID))
		return
	}
	if req.Password != job.EncryptionPassword {
		s.writeError(w, http.StatusForbidden, "incorrect password")
		return
	}

	opts := &models.RestoreOptions{
		DestinationPath: req.Path,
		Overwrite:       req.Overwrite,
		IncludePatterns: req.IncludePatterns,
		ExcludePatterns: req.ExcludePatterns,
		Concurrency:     4,
	}

	if req.Time != "" {
		t, err := time.Parse(time.RFC3339, req.Time)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid time format (use RFC3339)")
			return
		}
		opts.TargetTime = t
	}

	result, err := s.restoreProvider.Restore(jobID, opts)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

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

func (s *APIServer) handleBrowse(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		home, _ := os.UserHomeDir()
		path = home
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("cannot read directory: %s", err.Error()))
		return
	}

	dirs := make([]string, 0)
	files := make([]string, 0)
	for _, e := range entries {
		name := e.Name()
		if name[0] == '.' {
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, name)
		} else {
			files = append(files, name)
		}
	}

	parent := filepath.Dir(path)
	if path == "/" {
		parent = ""
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":   path,
		"parent": parent,
		"dirs":   dirs,
		"files":  files,
	})
}

func (s *APIServer) handleMkdir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Path == "" || req.Name == "" {
		s.writeError(w, http.StatusBadRequest, "path and name are required")
		return
	}
	dirPath := filepath.Join(req.Path, req.Name)
	if err := os.Mkdir(dirPath, 0755); err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("cannot create directory: %s", err.Error()))
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"path": dirPath})
}
