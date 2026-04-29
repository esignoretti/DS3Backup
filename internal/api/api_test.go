package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/esignoretti/ds3backup/pkg/models"
)

// mockRunner implements BackupRunner for testing.
type mockRunner struct {
	running   bool
	jobs      []string
	runCalled chan string
}

func (m *mockRunner) RunJob(jobID string) {
	if m.runCalled != nil {
		m.runCalled <- jobID
	}
}
func (m *mockRunner) GetScheduledJobs() []string { return m.jobs }
func (m *mockRunner) IsRunning() bool            { return m.running }
func (m *mockRunner) Start()                     { m.running = true }
func (m *mockRunner) Stop()                      { m.running = false }

// mockJobManager implements JobManager for testing.
type mockJobManager struct {
	jobs map[string]*models.BackupJob
}

func (m *mockJobManager) GetJob(jobID string) *models.BackupJob {
	return m.jobs[jobID]
}

func (m *mockJobManager) CreateJob(name, source, password, cronExpr string) (*models.BackupJob, error) {
	return &models.BackupJob{ID: "new-job", Name: name}, nil
}

func (m *mockJobManager) GetAllJobs() []models.BackupJob {
	result := make([]models.BackupJob, 0, len(m.jobs))
	for _, j := range m.jobs {
		result = append(result, *j)
	}
	return result
}

// newTestServer creates an APIServer with mock dependencies for testing.
func newTestServer(runner *mockRunner, jobManager *mockJobManager, historyProvider HistoryProvider) *APIServer {
	runner.runCalled = make(chan string, 1)
	return NewAPIServer(8099, runner, jobManager, historyProvider, "")
}

// executeRequest performs an HTTP request against the server's router.
func executeRequest(s *APIServer, method, path string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	handler := s.setupRouter()
	handler.ServeHTTP(w, req)
	return w
}

func TestGetStatus(t *testing.T) {
	runner := &mockRunner{running: true, jobs: []string{"job1", "job2"}}
	jm := &mockJobManager{jobs: map[string]*models.BackupJob{}}
	s := newTestServer(runner, jm, nil)

	// Set startTime so uptime is non-zero
	s.mu.Lock()
	s.startTime = time.Now().Add(-1 * time.Hour)
	s.mu.Unlock()

	w := executeRequest(s, http.MethodGet, "/api/v1/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp StatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Running {
		t.Errorf("expected running=true, got false")
	}
	if resp.ScheduledJobs != 2 {
		t.Errorf("expected scheduledJobs=2, got %d", resp.ScheduledJobs)
	}
	if resp.APIPort != 8099 {
		t.Errorf("expected apiPort=8099, got %d", resp.APIPort)
	}
	if resp.Uptime == "" {
		t.Errorf("expected non-empty uptime")
	}
}

func TestStartStop(t *testing.T) {
	runner := &mockRunner{}
	jm := &mockJobManager{jobs: map[string]*models.BackupJob{}}
	s := newTestServer(runner, jm, nil)

	// Start
	w := executeRequest(s, http.MethodPost, "/api/v1/start", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 on start, got %d", w.Code)
	}
	if !runner.IsRunning() {
		t.Errorf("expected runner to be running after start")
	}

	var startResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &startResp); err != nil {
		t.Fatalf("failed to decode start response: %v", err)
	}
	if startResp["status"] != "started" {
		t.Errorf("expected status=started, got %s", startResp["status"])
	}

	// Stop
	w = executeRequest(s, http.MethodPost, "/api/v1/stop", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 on stop, got %d", w.Code)
	}
	if runner.IsRunning() {
		t.Errorf("expected runner to be stopped after stop")
	}

	var stopResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &stopResp); err != nil {
		t.Fatalf("failed to decode stop response: %v", err)
	}
	if stopResp["status"] != "stopped" {
		t.Errorf("expected status=stopped, got %s", stopResp["status"])
	}
}

func TestListJobs_Empty(t *testing.T) {
	runner := &mockRunner{}
	jm := &mockJobManager{jobs: map[string]*models.BackupJob{}}
	s := newTestServer(runner, jm, nil)

	w := executeRequest(s, http.MethodGet, "/api/v1/jobs", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp JobListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Jobs == nil {
		t.Fatal("expected non-nil jobs array")
	}
	if len(resp.Jobs) != 0 {
		t.Errorf("expected empty jobs array, got %d items", len(resp.Jobs))
	}
}

func TestListJobs_WithJobs(t *testing.T) {
	now := time.Now()
	runner := &mockRunner{}
	jm := &mockJobManager{
		jobs: map[string]*models.BackupJob{
			"job1": {
				ID:              "job1",
				Name:            "Test Job",
				SourcePath:      "/tmp/test",
				RetentionDays:   30,
				ObjectLockMode:  "GOVERNANCE",
				Enabled:         true,
				EncryptionPassword: "secret",
				CreatedAt:       now,
				NextRun:         now.Add(1 * time.Hour),
				ScheduleEnabled: true,
				CronExpr:        "0 * * * *",
			},
		},
	}
	s := newTestServer(runner, jm, nil)

	w := executeRequest(s, http.MethodGet, "/api/v1/jobs", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp JobListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(resp.Jobs))
	}

	// Verify EncryptionPassword is NOT in the response
	bodyStr := w.Body.String()
	if strings.Contains(bodyStr, "encryptionPassword") {
		t.Error("encryptionPassword field should NOT appear in API response")
	}
	if strings.Contains(bodyStr, "secret") {
		t.Error("secret value should NOT appear in API response")
	}

	// Verify other fields are present
	job := resp.Jobs[0]
	if job.ID != "job1" {
		t.Errorf("expected job ID job1, got %s", job.ID)
	}
	if job.Name != "Test Job" {
		t.Errorf("expected job name Test Job, got %s", job.Name)
	}
}

func TestGetJob_Found(t *testing.T) {
	now := time.Now()
	runner := &mockRunner{}
	jm := &mockJobManager{
		jobs: map[string]*models.BackupJob{
			"job1": {
				ID:              "job1",
				Name:            "Test Job",
				SourcePath:      "/tmp/test",
				RetentionDays:   30,
				ObjectLockMode:  "GOVERNANCE",
				Enabled:         true,
				EncryptionPassword: "secret",
				CreatedAt:       now,
				NextRun:         now.Add(1 * time.Hour),
				ScheduleEnabled: true,
				CronExpr:        "0 * * * *",
			},
		},
	}
	s := newTestServer(runner, jm, nil)

	w := executeRequest(s, http.MethodGet, "/api/v1/jobs/job1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp JobDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Job.ID != "job1" {
		t.Errorf("expected job ID job1, got %s", resp.Job.ID)
	}
	if !resp.IsScheduled {
		t.Errorf("expected scheduled=true")
	}
	if resp.CronExpr != "0 * * * *" {
		t.Errorf("expected cronExpr=0 * * * *, got %s", resp.CronExpr)
	}

	// Verify no password leak
	bodyStr := w.Body.String()
	if strings.Contains(bodyStr, "encryptionPassword") {
		t.Error("encryptionPassword field should NOT appear in API response")
	}
}

func TestGetJob_NotFound(t *testing.T) {
	runner := &mockRunner{}
	jm := &mockJobManager{jobs: map[string]*models.BackupJob{}}
	s := newTestServer(runner, jm, nil)

	w := executeRequest(s, http.MethodGet, "/api/v1/jobs/nonexistent", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", w.Code)
	}

	var resp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Code != 404 {
		t.Errorf("expected code 404, got %d", resp.Code)
	}
	if resp.Error == "" {
		t.Error("expected non-empty error message")
	}
}

func TestRunBackup_TriggersJob(t *testing.T) {
	runner := &mockRunner{}
	jm := &mockJobManager{
		jobs: map[string]*models.BackupJob{
			"job1": {
				ID:   "job1",
				Name: "Test Job",
			},
		},
	}
	s := newTestServer(runner, jm, nil)

	w := executeRequest(s, http.MethodPost, "/api/v1/backup/run/job1", "")

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", w.Code)
	}

	var resp BackupTriggerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.JobID != "job1" {
		t.Errorf("expected jobID job1, got %s", resp.JobID)
	}
	if !resp.Triggered {
		t.Errorf("expected triggered=true")
	}
	if resp.Message == "" {
		t.Error("expected non-empty message")
	}

	// Verify runCalled channel received the job ID
	select {
	case calledID := <-runner.runCalled:
		if calledID != "job1" {
			t.Errorf("expected RunJob called with job1, got %s", calledID)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for RunJob to be called")
	}
}

func TestRunBackup_JobNotFound(t *testing.T) {
	runner := &mockRunner{}
	jm := &mockJobManager{jobs: map[string]*models.BackupJob{}}
	s := newTestServer(runner, jm, nil)

	w := executeRequest(s, http.MethodPost, "/api/v1/backup/run/invalid", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", w.Code)
	}

	var resp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestAPIResponseFormat_Status verifies the JSON structure of the status response
// has all required fields.

func TestAPIResponseFormat_Status(t *testing.T) {
	runner := &mockRunner{running: true, jobs: []string{"job1"}}
	jm := &mockJobManager{jobs: map[string]*models.BackupJob{}}
	s := newTestServer(runner, jm, nil)
	s.mu.Lock()
	s.startTime = time.Now().Add(-2 * time.Hour)
	s.mu.Unlock()

	w := executeRequest(s, http.MethodGet, "/api/v1/status", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	// Verify all required fields are present
	requiredFields := []string{"running", "schedulerRunning", "scheduledJobs", "apiPort", "uptime"}
	for _, field := range requiredFields {
		if _, ok := resp[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}

	// Verify boolean types
	if _, ok := resp["running"].(bool); !ok {
		t.Error("expected running to be boolean")
	}
	if _, ok := resp["schedulerRunning"].(bool); !ok {
		t.Error("expected schedulerRunning to be boolean")
	}

	// Verify numeric types
	if _, ok := resp["scheduledJobs"].(float64); !ok {
		t.Error("expected scheduledJobs to be numeric")
	}
	if _, ok := resp["apiPort"].(float64); !ok {
		t.Error("expected apiPort to be numeric")
	}

	// Verify string type
	if _, ok := resp["uptime"].(string); !ok {
		t.Error("expected uptime to be string")
	}
}

// TestAPIResponseFormat_JobList verifies the job list response structure.

func TestAPIResponseFormat_JobList(t *testing.T) {
	now := time.Now()
	runner := &mockRunner{}
	jm := &mockJobManager{
		jobs: map[string]*models.BackupJob{
			"job1": {
				ID:              "job1",
				Name:            "Test Job",
				SourcePath:      "/tmp/test",
				RetentionDays:   30,
				ObjectLockMode:  "GOVERNANCE",
				Enabled:         true,
				CreatedAt:       now,
				NextRun:         now.Add(1 * time.Hour),
				ScheduleEnabled: true,
				CronExpr:        "0 * * * *",
			},
		},
	}
	s := newTestServer(runner, jm, nil)

	w := executeRequest(s, http.MethodGet, "/api/v1/jobs", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	// Verify jobs array exists
	jobs, ok := resp["jobs"].([]interface{})
	if !ok {
		t.Fatal("expected jobs to be an array")
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	job, ok := jobs[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected job to be an object")
	}

	// Verify required fields in each job
	requiredFields := []string{"id", "name", "sourcePath", "retentionDays",
		"objectLockMode", "enabled", "createdAt", "scheduleEnabled"}
	for _, field := range requiredFields {
		if _, ok := job[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}

	// Verify EncryptionPassword is NOT present
	if _, ok := job["encryptionPassword"]; ok {
		t.Error("encryptionPassword field should NOT be in job response")
	}
}

// TestAPIResponseFormat_Error verifies the error response format (404 case).

func TestAPIResponseFormat_Error(t *testing.T) {
	runner := &mockRunner{}
	jm := &mockJobManager{jobs: map[string]*models.BackupJob{}}
	s := newTestServer(runner, jm, nil)

	w := executeRequest(s, http.MethodGet, "/api/v1/jobs/nonexistent", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	// Verify error response has required fields
	if _, ok := resp["error"]; !ok {
		t.Error("missing required field: error")
	}
	if _, ok := resp["code"]; !ok {
		t.Error("missing required field: code")
	}

	// Verify code is numeric and equals 404
	code, ok := resp["code"].(float64)
	if !ok {
		t.Fatal("expected code to be numeric")
	}
	if int(code) != 404 {
		t.Errorf("expected code 404, got %v", code)
	}

	errMsg, ok := resp["error"].(string)
	if !ok {
		t.Fatal("expected error to be string")
	}
	if errMsg == "" {
		t.Error("expected non-empty error message")
	}
}

// mockHistoryProvider implements HistoryProvider for testing.
type mockHistoryProvider struct {
	history map[string][]*models.BackupRun
}

func (m *mockHistoryProvider) GetJobHistory(jobID string, limit int) ([]*models.BackupRun, error) {
	runs := m.history[jobID]
	if runs == nil {
		return []*models.BackupRun{}, nil
	}
	if limit > 0 && len(runs) > limit {
		return runs[:limit], nil
	}
	return runs, nil
}

func TestGetJobHistory_Found(t *testing.T) {
	now := time.Now()
	runner := &mockRunner{}
	jm := &mockJobManager{
		jobs: map[string]*models.BackupJob{
			"job1": {ID: "job1", Name: "Test Job"},
		},
	}
	hp := &mockHistoryProvider{
		history: map[string][]*models.BackupRun{
			"job1": {
				{
					JobID:         "job1",
					RunTime:       now.Add(-1 * time.Hour),
					Status:        "completed",
					FilesAdded:    10,
					FilesChanged:  5,
					BytesUploaded: 1024,
					Duration:      30 * time.Second,
					StartTime:     now.Add(-1 * time.Hour),
					EndTime:       now.Add(-1 * time.Hour).Add(30 * time.Second),
				},
				{
					JobID:         "job1",
					RunTime:       now.Add(-2 * time.Hour),
					Status:        "failed",
					FilesAdded:    0,
					FilesChanged:  0,
					BytesUploaded: 0,
					Error:         "connection timeout",
					Duration:      10 * time.Second,
					StartTime:     now.Add(-2 * time.Hour),
					EndTime:       now.Add(-2 * time.Hour).Add(10 * time.Second),
				},
			},
		},
	}
	s := newTestServer(runner, jm, hp)

	w := executeRequest(s, http.MethodGet, "/api/v1/jobs/job1/history", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp HistoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.JobID != "job1" {
		t.Errorf("expected jobID job1, got %s", resp.JobID)
	}
	if len(resp.Runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(resp.Runs))
	}
	if resp.Runs[0].Status != "completed" {
		t.Errorf("expected first run status completed, got %s", resp.Runs[0].Status)
	}
	if resp.Runs[1].Status != "failed" {
		t.Errorf("expected second run status failed, got %s", resp.Runs[1].Status)
	}
}

func TestGetJobHistory_NoProvider(t *testing.T) {
	runner := &mockRunner{}
	jm := &mockJobManager{
		jobs: map[string]*models.BackupJob{
			"job1": {ID: "job1", Name: "Test Job"},
		},
	}
	s := newTestServer(runner, jm, nil)

	w := executeRequest(s, http.MethodGet, "/api/v1/jobs/job1/history", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp HistoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Runs) != 0 {
		t.Errorf("expected empty runs array, got %d items", len(resp.Runs))
	}
}

func TestGetJobHistory_JobNotFound(t *testing.T) {
	runner := &mockRunner{}
	jm := &mockJobManager{jobs: map[string]*models.BackupJob{}}
	s := newTestServer(runner, jm, nil)

	w := executeRequest(s, http.MethodGet, "/api/v1/jobs/nonexistent/history", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", w.Code)
	}

	var resp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestAPIServer_StartStopLifecycle verifies the server lifecycle:
// start, IsRunning, stop, not running.

func TestAPIServer_StartStopLifecycle(t *testing.T) {
	runner := &mockRunner{}
	jm := &mockJobManager{jobs: map[string]*models.BackupJob{}}
	s := NewAPIServer(0, runner, jm, nil, "")

	if s.IsRunning() {
		t.Error("expected server not to be running before Start")
	}

	// Start (port 0 lets the test bind without conflicts)
	if err := s.Start(); err != nil {
		t.Fatalf("expected no error starting server, got: %v", err)
	}

	if !s.IsRunning() {
		t.Error("expected server to be running after Start")
	}

	// Stop
	if err := s.Stop(); err != nil {
		t.Fatalf("expected no error stopping server, got: %v", err)
	}

	if s.IsRunning() {
		t.Error("expected server not to be running after Stop")
	}
}
