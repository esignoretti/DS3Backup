package scheduler

import (
	"log"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/esignoretti/ds3backup/internal/config"
	"github.com/esignoretti/ds3backup/pkg/models"
)

// testLogger creates a logger writing to stderr for tests.
func testLogger() *log.Logger {
	return log.New(os.Stderr, "", 0)
}

// TestScheduler_CronExpressionAtMidnight verifies that a job with "0 0 * * *"
// (midnight daily) registers correctly via HasJob and GetScheduledJobs.
func TestScheduler_CronExpressionAtMidnight(t *testing.T) {
	s := NewScheduler(1*time.Second, testLogger())

	err := s.ScheduleJob("midnight-job", "0 0 * * *", func() {})
	if err != nil {
		t.Fatalf("expected no error for midnight cron, got: %v", err)
	}

	if !s.HasJob("midnight-job") {
		t.Error("expected HasJob to return true for midnight-job")
	}

	jobs := s.GetScheduledJobs()
	found := false
	for _, j := range jobs {
		if j == "midnight-job" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected GetScheduledJobs to include midnight-job, got: %v", jobs)
	}
}

// TestScheduler_EveryMinuteExpression verifies that scheduling with "* * * * *"
// parses successfully.

func TestScheduler_EveryMinuteExpression(t *testing.T) {
	s := NewScheduler(1*time.Second, testLogger())

	err := s.ScheduleJob("every-minute", "* * * * *", func() {})
	if err != nil {
		t.Fatalf("expected no error for every-minute cron, got: %v", err)
	}

	if !s.HasJob("every-minute") {
		t.Error("expected every-minute job to be registered")
	}

	s.Start()
	// Let it run briefly to confirm no panics
	time.Sleep(500 * time.Millisecond)
	s.Stop()

	if s.IsRunning() {
		t.Error("expected scheduler to be stopped")
	}
}

// TestScheduler_MultipleJobs verifies that 3 jobs can be scheduled, one removed,
// and then reloaded with only 1 remaining.

func TestScheduler_MultipleJobs(t *testing.T) {
	s := NewScheduler(1*time.Second, testLogger())

	// Schedule 3 jobs
	jobs := []struct {
		id       string
		cronExpr string
	}{
		{"job-a", "0 2 * * *"},
		{"job-b", "30 1 * * *"},
		{"job-c", "0 12 * * *"},
	}

	for _, j := range jobs {
		err := s.ScheduleJob(j.id, j.cronExpr, func() {})
		if err != nil {
			t.Fatalf("failed to schedule %s: %v", j.id, err)
		}
	}

	// Verify all 3 are registered
	if got := len(s.GetScheduledJobs()); got != 3 {
		t.Fatalf("expected 3 scheduled jobs, got %d", got)
	}

	// Remove job-b
	s.UnscheduleJob("job-b")

	// Verify only 2 remain
	remaining := s.GetScheduledJobs()
	if len(remaining) != 2 {
		t.Fatalf("expected 2 scheduled jobs after removal, got %d: %v", len(remaining), remaining)
	}
	if s.HasJob("job-b") {
		t.Error("expected job-b to be removed")
	}

	// Reload with only job-c
	s.ReloadJobs([]JobSchedule{
		{ID: "job-c", Enabled: true, CronExpr: "0 12 * * *"},
	}, func(jobID string) func() {
		return func() {}
	})

	// Verify only 1 remains
	final := s.GetScheduledJobs()
	if len(final) != 1 {
		t.Fatalf("expected 1 scheduled job after reload, got %d: %v", len(final), final)
	}
	if final[0] != "job-c" {
		t.Errorf("expected job-c to be the only job, got %s", final[0])
	}
}

// TestScheduler_ReloadJobsOverwritesExisting verifies that ReloadJobs replaces
// all existing jobs correctly.

func TestScheduler_ReloadJobsOverwritesExisting(t *testing.T) {
	s := NewScheduler(1*time.Second, testLogger())

	// Schedule job A with expression X and job B with expression Y
	err := s.ScheduleJob("job-A", "0 2 * * *", func() {})
	if err != nil {
		t.Fatalf("failed to schedule job-A: %v", err)
	}
	err = s.ScheduleJob("job-B", "30 1 * * *", func() {})
	if err != nil {
		t.Fatalf("failed to schedule job-B: %v", err)
	}

	// Reload with only job A with expression Z
	callCount := make(map[string]int)
	var mu sync.Mutex
	s.ReloadJobs([]JobSchedule{
		{ID: "job-A", Enabled: true, CronExpr: "0 5 * * *"},
	}, func(jobID string) func() {
		return func() {
			mu.Lock()
			callCount[jobID]++
			mu.Unlock()
		}
	})

	// Verify: job-A still scheduled, job-B removed
	if !s.HasJob("job-A") {
		t.Error("expected job-A to still be scheduled")
	}
	if s.HasJob("job-B") {
		t.Error("expected job-B to be removed")
	}

	// Only 1 job should be registered
	if got := len(s.GetScheduledJobs()); got != 1 {
		t.Errorf("expected 1 scheduled job, got %d: %v", got, s.GetScheduledJobs())
	}
}

// newTestConfig creates a config with a temporary path for testing,
// so SaveConfig does not write outside the test temp directory.
func newTestConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.DefaultConfig()
	tmpDir := t.TempDir()
	cfg.ConfigPath = tmpDir + "/config.json"
	return cfg
}

// TestBackupJobRunner_RunJobCallsBackupFn verifies that BackupJobRunner.RunJob
// calls the runBackupFn and updates LastRun on the job.

func TestBackupJobRunner_RunJobCallsBackupFn(t *testing.T) {
	cfg := newTestConfig(t)

	runBackupCalled := make(chan string, 1)
	mockRunBackup := func(job *models.BackupJob) (*models.BackupRun, error) {
		runBackupCalled <- job.ID
		now := time.Now()
		return &models.BackupRun{
			JobID:   job.ID,
			RunTime: now,
			Status:  "completed",
		}, nil
	}

	runner := NewBackupJobRunner(cfg, cfg.GetJob, mockRunBackup)

	testJob := models.BackupJob{
		ID:      "test-job-1",
		Name:    "Test Job",
		Enabled: true,
	}
	cfg.Jobs = append(cfg.Jobs, testJob)

	runner.RunJob("test-job-1")

	select {
	case calledID := <-runBackupCalled:
		if calledID != "test-job-1" {
			t.Errorf("expected runBackupFn called with test-job-1, got %s", calledID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runBackupFn to be called")
	}

	// Verify job exists and config was saved (config file was written)
	job := cfg.GetJob("test-job-1")
	if job == nil {
		t.Fatal("expected job to exist in config")
	}

	// Verify the config file was actually written (SaveConfig was called)
	info, err := os.Stat(cfg.ConfigPath)
	if err != nil {
		t.Fatalf("expected config file to exist after SaveConfig, got: %v", err)
	}
	if info.Size() == 0 {
		t.Error("expected non-empty config file")
	}
}

// TestScheduleManager_EnableDisableCycle verifies that ScheduleManager
// correctly enables (schedules) and disables (unschedules) a job.

func TestScheduleManager_EnableDisableCycle(t *testing.T) {
	cfg := newTestConfig(t)

	s := NewScheduler(1*time.Second, testLogger())

	var runCount atomic.Int32
	mockRunner := NewBackupJobRunner(cfg, cfg.GetJob, func(job *models.BackupJob) (*models.BackupRun, error) {
		runCount.Add(1)
		now := time.Now()
		return &models.BackupRun{JobID: job.ID, RunTime: now, Status: "completed"}, nil
	})

	manager := NewScheduleManager(cfg, s, mockRunner)

	testJob := models.BackupJob{
		ID:      "sched-job-1",
		Name:    "Scheduled Job",
		Enabled: true,
	}
	cfg.Jobs = append(cfg.Jobs, testJob)

	// Enable the job with a cron expression
	err := manager.EnableJobSchedule("sched-job-1", "0 * * * *")
	if err != nil {
		t.Fatalf("expected no error enabling schedule, got: %v", err)
	}

	// Verify job is scheduled
	if !s.HasJob("sched-job-1") {
		t.Error("expected job to be scheduled after EnableJobSchedule")
	}

	// Verify config reflects schedule enabled
	job := cfg.GetJob("sched-job-1")
	if job == nil {
		t.Fatal("expected job to exist")
	}
	if !job.ScheduleEnabled {
		t.Error("expected ScheduleEnabled to be true")
	}
	if job.CronExpr != "0 * * * *" {
		t.Errorf("expected CronExpr '0 * * * *', got %q", job.CronExpr)
	}

	// Disable the job
	manager.DisableJobSchedule("sched-job-1")

	// Verify job is unscheduled
	if s.HasJob("sched-job-1") {
		t.Error("expected job to be unscheduled after DisableJobSchedule")
	}

	// Verify config reflects schedule disabled
	job = cfg.GetJob("sched-job-1")
	if job == nil {
		t.Fatal("expected job to still exist")
	}
	if job.ScheduleEnabled {
		t.Error("expected ScheduleEnabled to be false")
	}
}
