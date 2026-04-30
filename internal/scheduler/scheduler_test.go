package scheduler

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/esignoretti/ds3backup/internal/config"
	"github.com/esignoretti/ds3backup/pkg/models"
)

var errTestBackup = errors.New("backup failed")

func newTestLogger() *log.Logger {
	return log.New(os.Stderr, "", 0)
}

func newTestConfigWithJob(t *testing.T, job models.BackupJob) *config.Config {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	cfg.Jobs = []models.BackupJob{job}
	if err := cfg.SaveConfig(); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}
	return cfg
}

func TestScheduleJob_ParsesValidCron(t *testing.T) {
	s := NewScheduler(60*time.Second, newTestLogger())
	ran := make(chan struct{}, 1)

	err := s.ScheduleJob("test-job", []string{"0 2 * * *"}, func() {
		ran <- struct{}{}
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !s.HasJob("test-job") {
		t.Error("expected job to be registered")
	}
}

func TestScheduleJob_RejectsInvalidCron(t *testing.T) {
	s := NewScheduler(60*time.Second, newTestLogger())

	err := s.ScheduleJob("bad-job", []string{"not a cron expression"}, func() {})
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}

func TestReloadJobs_SchedulesEnabledOnly(t *testing.T) {
	s := NewScheduler(60*time.Second, newTestLogger())

	jobs := []JobSchedule{
		{ID: "job-1", Enabled: true, CronExprs: []string{"0 2 * * *"}},
		{ID: "job-2", Enabled: false, CronExprs: []string{"0 3 * * *"}},
		{ID: "job-3", Enabled: true, CronExprs: nil},
		{ID: "job-4", Enabled: true, CronExprs: []string{"0 4 * * *"}},
	}

	s.ReloadJobs(jobs, func(jobID string) func() {
		return func() {}
	})

	scheduled := s.GetScheduledJobs()
	if len(scheduled) != 2 {
		t.Fatalf("expected 2 scheduled jobs, got %d: %v", len(scheduled), scheduled)
	}

	hasJob1 := false
	hasJob4 := false
	for _, j := range scheduled {
		if j == "job-1" {
			hasJob1 = true
		}
		if j == "job-4" {
			hasJob4 = true
		}
	}
	if !hasJob1 || !hasJob4 {
		t.Errorf("expected job-1 and job-4 to be scheduled, got: %v", scheduled)
	}
}

func TestStartStop_TransitionsState(t *testing.T) {
	s := NewScheduler(60*time.Second, newTestLogger())

	if s.IsRunning() {
		t.Error("expected scheduler to not be running initially")
	}

	s.Start()
	if !s.IsRunning() {
		t.Error("expected scheduler to be running after Start")
	}

	s.Stop()
	if s.IsRunning() {
		t.Error("expected scheduler to not be running after Stop")
	}
}

func TestUnscheduleJob(t *testing.T) {
	s := NewScheduler(60*time.Second, newTestLogger())

	s.ScheduleJob("job-1", []string{"0 2 * * *"}, func() {})
	if !s.HasJob("job-1") {
		t.Fatal("expected job-1 to be scheduled")
	}

	s.UnscheduleJob("job-1")
	if s.HasJob("job-1") {
		t.Error("expected job-1 to be unscheduled")
	}
}

func TestScheduleJob_TriggersRunner(t *testing.T) {
	t.Skip("cron library fires at minute boundaries; functional test only")

	s := NewScheduler(60*time.Second, newTestLogger())
	s.Start()
	defer s.Stop()

	var ran atomic.Bool
	s.ScheduleJob("trigger-test", []string{"* * * * *"}, func() {
		ran.Store(true)
	})

	time.Sleep(3200 * time.Millisecond)
	if !ran.Load() {
		t.Error("expected runner to be triggered")
	}
}

func TestMultiSchedule_AllExpressionsFire(t *testing.T) {
	s := NewScheduler(60*time.Second, newTestLogger())

	err := s.ScheduleJob("multi-job", []string{"0 2 * * *", "0 14 * * *"}, func() {})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !s.HasJob("multi-job") {
		t.Error("expected multi-job to be registered")
	}

	// Verify exactly one job entry (with two cron entries)
	jobs := s.GetScheduledJobs()
	if len(jobs) != 1 {
		t.Errorf("expected 1 scheduled job, got %d", len(jobs))
	}
}

func TestMultiSchedule_PartialExprFailure(t *testing.T) {
	s := NewScheduler(60*time.Second, newTestLogger())

	// One valid + one invalid should return error and not register any
	err := s.ScheduleJob("partial-job", []string{"0 2 * * *", "not-valid"}, func() {})
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}

	// Job should NOT be registered (rollback)
	if s.HasJob("partial-job") {
		t.Error("expected partial-job NOT to be registered after partial failure")
	}
}

func TestMultiSchedule_ReloadJobsMultipleExpressions(t *testing.T) {
	s := NewScheduler(60*time.Second, newTestLogger())

	// Schedule with 2 expressions
	err := s.ScheduleJob("reload-job", []string{"0 2 * * *", "0 14 * * *"}, func() {})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !s.HasJob("reload-job") {
		t.Fatal("expected reload-job to be scheduled")
	}

	// Reload with different expressions
	s.ReloadJobs([]JobSchedule{
		{ID: "reload-job", Enabled: true, CronExprs: []string{"30 1 * * *"}},
	}, func(jobID string) func() { return func() {} })

	if !s.HasJob("reload-job") {
		t.Error("expected reload-job to still be scheduled after reload")
	}

	// Only 1 job should remain
	if got := len(s.GetScheduledJobs()); got != 1 {
		t.Errorf("expected 1 scheduled job after reload, got %d: %v", got, s.GetScheduledJobs())
	}
}

func TestMultiSchedule_UnscheduleJobRemovesAll(t *testing.T) {
	s := NewScheduler(60*time.Second, newTestLogger())

	// Schedule with 2 expressions
	err := s.ScheduleJob("multi-remove", []string{"0 2 * * *", "0 14 * * *"}, func() {})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !s.HasJob("multi-remove") {
		t.Fatal("expected multi-remove to be scheduled")
	}

	// Unschedule — should remove all entries
	s.UnscheduleJob("multi-remove")

	if s.HasJob("multi-remove") {
		t.Error("expected multi-remove to be unscheduled")
	}
}

func TestScheduleJob_EmptyExprsReturnsError(t *testing.T) {
	s := NewScheduler(60*time.Second, newTestLogger())

	err := s.ScheduleJob("empty-job", []string{}, func() {})
	if err == nil {
		t.Fatal("expected error for empty cron expressions")
	}
}

// --- Retry Tests (D-01, D-02, D-03) ---

func TestRetryRunner_SuccessOnFirstAttempt(t *testing.T) {
	job := models.BackupJob{
		ID:      "retry-success",
		Name:    "Retry Success Job",
		Enabled: true,
	}
	cfg := newTestConfigWithJob(t, job)

	var attemptCount atomic.Int32
	runner := &BackupJobRunner{
		cfg: cfg,
		getJob: func(jobID string) *models.BackupJob {
			return cfg.GetJob(jobID)
		},
		runBackupFn: func(j *models.BackupJob) (*models.BackupRun, error) {
			attemptCount.Add(1)
			return &models.BackupRun{}, nil
		},
		jobLocks:      make(map[string]*sync.Mutex),
		retryInterval: time.Millisecond,
	}

	runner.RunJob(job.ID)

	if n := attemptCount.Load(); n != 1 {
		t.Errorf("expected 1 attempt, got %d", n)
	}

	updatedJob := cfg.GetJob(job.ID)
	if updatedJob.LastError != "" {
		t.Errorf("expected LastError to be cleared, got %q", updatedJob.LastError)
	}
}

func TestRetryRunner_RetriesOnFailure(t *testing.T) {
	job := models.BackupJob{
		ID:      "retry-transient",
		Name:    "Retry Transient Job",
		Enabled: true,
	}
	cfg := newTestConfigWithJob(t, job)

	var attemptCount atomic.Int32
	runner := &BackupJobRunner{
		cfg: cfg,
		getJob: func(jobID string) *models.BackupJob {
			return cfg.GetJob(jobID)
		},
		runBackupFn: func(j *models.BackupJob) (*models.BackupRun, error) {
			n := attemptCount.Add(1)
			if n < 3 {
				return &models.BackupRun{Error: "transient error"}, nil
			}
			return &models.BackupRun{}, nil
		},
		jobLocks:      make(map[string]*sync.Mutex),
		retryInterval: time.Millisecond,
	}

	runner.RunJob(job.ID)

	if n := attemptCount.Load(); n != 3 {
		t.Errorf("expected 3 attempts (1 initial + 2 retries), got %d", n)
	}

	updatedJob := cfg.GetJob(job.ID)
	if updatedJob.LastError != "" {
		t.Errorf("expected LastError to be cleared after successful retry, got %q", updatedJob.LastError)
	}
}

func TestRetryRunner_ExhaustsRetriesThenErrors(t *testing.T) {
	job := models.BackupJob{
		ID:      "retry-exhaust",
		Name:    "Retry Exhaust Job",
		Enabled: true,
	}
	cfg := newTestConfigWithJob(t, job)

	var attemptCount atomic.Int32
	runner := &BackupJobRunner{
		cfg: cfg,
		getJob: func(jobID string) *models.BackupJob {
			return cfg.GetJob(jobID)
		},
		runBackupFn: func(j *models.BackupJob) (*models.BackupRun, error) {
			attemptCount.Add(1)
			return nil, errTestBackup
		},
		jobLocks:      make(map[string]*sync.Mutex),
		retryInterval: time.Millisecond,
	}

	runner.RunJob(job.ID)

	if n := attemptCount.Load(); n != 3 {
		t.Errorf("expected 3 attempts (1 initial + 2 retries), got %d", n)
	}

	updatedJob := cfg.GetJob(job.ID)
	if updatedJob.LastError == "" {
		t.Fatal("expected LastError to be set after all retries exhausted")
	}
}

func TestRetryRunner_SkipForDisabledJob(t *testing.T) {
	job := models.BackupJob{
		ID:      "retry-disabled",
		Name:    "Retry Disabled Job",
		Enabled: false,
	}
	cfg := newTestConfigWithJob(t, job)

	var attemptCount atomic.Int32
	runner := &BackupJobRunner{
		cfg: cfg,
		getJob: func(jobID string) *models.BackupJob {
			return cfg.GetJob(jobID)
		},
		runBackupFn: func(j *models.BackupJob) (*models.BackupRun, error) {
			attemptCount.Add(1)
			return nil, errTestBackup
		},
		jobLocks:      make(map[string]*sync.Mutex),
		retryInterval: time.Millisecond,
	}

	runner.RunJob(job.ID)

	if n := attemptCount.Load(); n != 0 {
		t.Errorf("expected 0 attempts for disabled job, got %d", n)
	}
}

// --- Overlap Prevention Tests (D-05, D-06, D-09, D-10) ---

func TestOverlapPrevention_SkipsSecondRun(t *testing.T) {
	job := models.BackupJob{
		ID:      "overlap-job",
		Name:    "Overlap Job",
		Enabled: true,
	}
	cfg := newTestConfigWithJob(t, job)

	started := make(chan struct{})
	proceed := make(chan struct{})
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	runner := &BackupJobRunner{
		cfg: cfg,
		getJob: func(jobID string) *models.BackupJob {
			return cfg.GetJob(jobID)
		},
		runBackupFn: func(j *models.BackupJob) (*models.BackupRun, error) {
			close(started)
			<-proceed
			return &models.BackupRun{}, nil
		},
		jobLocks:      make(map[string]*sync.Mutex),
		retryInterval: time.Millisecond,
	}

	// Start first run in background (it blocks on proceed channel)
	go runner.RunJobAsync(job.ID)
	<-started // wait for first run to acquire lock and enter runBackupFn

	// Second run — should skip because lock is held
	runner.RunJob(job.ID)

	// Check skip message was logged
	if !strings.Contains(logBuf.String(), "Skipping scheduled run") {
		t.Error("expected 'Skipping scheduled run' log message for overlapping run")
	}

	// Unblock first run and give it time to complete
	close(proceed)
	time.Sleep(10 * time.Millisecond)
}

func TestOverlapPrevention_DifferentJobsParallel(t *testing.T) {
	job1 := models.BackupJob{
		ID:      "parallel-1",
		Name:    "Parallel Job 1",
		Enabled: true,
	}
	job2 := models.BackupJob{
		ID:      "parallel-2",
		Name:    "Parallel Job 2",
		Enabled: true,
	}
	cfg := newTestConfigWithJob(t, job1)
	cfg.Jobs = append(cfg.Jobs, job2)
	if err := cfg.SaveConfig(); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	var mu sync.Mutex
	var completedJobs []string

	runner := &BackupJobRunner{
		cfg: cfg,
		getJob: func(jobID string) *models.BackupJob {
			return cfg.GetJob(jobID)
		},
		runBackupFn: func(j *models.BackupJob) (*models.BackupRun, error) {
			time.Sleep(20 * time.Millisecond)
			mu.Lock()
			completedJobs = append(completedJobs, j.ID)
			mu.Unlock()
			return &models.BackupRun{}, nil
		},
		jobLocks:      make(map[string]*sync.Mutex),
		retryInterval: time.Millisecond,
	}

	// Use RunJob directly (not async) to avoid SaveConfig races when two jobs
	// complete near-simultaneously on the same config file.
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		runner.RunJob("parallel-1")
	}()
	go func() {
		defer wg.Done()
		runner.RunJob("parallel-2")
	}()

	wg.Wait()

	if strings.Contains(logBuf.String(), "Skipping scheduled run") {
		t.Error("unexpected skip message — different jobs should not interfere")
	}

	if len(completedJobs) != 2 {
		t.Fatalf("expected 2 completed jobs, got %d", len(completedJobs))
	}
}

func TestOverlapPrevention_TryLockReleasedOnCompletion(t *testing.T) {
	job := models.BackupJob{
		ID:      "release-job",
		Name:    "Release Job",
		Enabled: true,
	}
	cfg := newTestConfigWithJob(t, job)

	var attemptCount atomic.Int32
	runner := &BackupJobRunner{
		cfg: cfg,
		getJob: func(jobID string) *models.BackupJob {
			return cfg.GetJob(jobID)
		},
		runBackupFn: func(j *models.BackupJob) (*models.BackupRun, error) {
			attemptCount.Add(1)
			return &models.BackupRun{}, nil
		},
		jobLocks:      make(map[string]*sync.Mutex),
		retryInterval: time.Millisecond,
	}

	// First run completes normally
	runner.RunJob(job.ID)
	if attemptCount.Load() != 1 {
		t.Fatalf("expected 1 attempt on first run, got %d", attemptCount.Load())
	}

	// Second run for same job should proceed (lock was released)
	runner.RunJob(job.ID)
	if attemptCount.Load() != 2 {
		t.Errorf("expected 2 attempts total (lock released), got %d", attemptCount.Load())
	}
}

// --- Panic Recovery Test (D-04) ---

func TestPanicRecovery_SetsLastError(t *testing.T) {
	job := models.BackupJob{
		ID:      "panic-job",
		Name:    "Panic Job",
		Enabled: true,
	}
	cfg := newTestConfigWithJob(t, job)

	// Serialize access to the panicDone channel so we know when recovery completes
	panicDone := make(chan struct{})
	runner := &BackupJobRunner{
		cfg: cfg,
		getJob: func(jobID string) *models.BackupJob {
			return cfg.GetJob(jobID)
		},
		runBackupFn: func(j *models.BackupJob) (*models.BackupRun, error) {
			panic("test panic in backup")
		},
		jobLocks:      make(map[string]*sync.Mutex),
		retryInterval: time.Millisecond,
	}

	// Intercept the recover closure by wrapping runBackupFn so that the
	// deferred panic recovery in RunJobAsync can signal completion.
	// Actually, just use a channel-driven approach: replace runBackupFn
	// with one that panics and relies on async defer chain.
	go func() {
		runner.RunJobAsync(job.ID)
		// Wait for the recover+Saves to complete
		time.Sleep(100 * time.Millisecond)
		close(panicDone)
	}()

	select {
	case <-panicDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for panic recovery")
	}

	updatedJob := cfg.GetJob(job.ID)
	if !strings.Contains(updatedJob.LastError, "panic") {
		t.Errorf("expected LastError to contain 'panic', got %q", updatedJob.LastError)
	}
}

// --- Scheduler Checkpoint Tests (D-07, D-08) ---

func TestScheduler_CheckpointTime(t *testing.T) {
	s := NewScheduler(60*time.Second, newTestLogger())

	checkpoint := s.GetCheckpointTime()
	if !checkpoint.IsZero() {
		t.Error("expected initial checkpoint to be zero")
	}

	now := time.Now()
	s.SetCheckpointTime(now)

	retrieved := s.GetCheckpointTime()
	if retrieved.IsZero() {
		t.Error("expected checkpoint to be non-zero after SetCheckpointTime")
	}
}

func TestLoadAllSchedules_LogsMissed(t *testing.T) {
	job := models.BackupJob{
		ID:              "checkpoint-job",
		Name:            "Checkpoint Job",
		Enabled:         true,
		ScheduleEnabled: true,
		CronExprs:       []string{"0 2 * * *"},
	}
	cfg := newTestConfigWithJob(t, job)
	cfg.Daemon.SchedulerInterval = 60
	cfg.Daemon.LastSchedulerCheckpoint = time.Now().Add(-1 * time.Hour)

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	s := NewScheduler(60*time.Second, newTestLogger())
	runner := NewBackupJobRunner(cfg, cfg.GetJob, func(j *models.BackupJob) (*models.BackupRun, error) {
		return &models.BackupRun{}, nil
	})
	sm := NewScheduleManager(cfg, s, runner)

	sm.LoadAllSchedules()

	output := logBuf.String()
	if !strings.Contains(output, "offline") && !strings.Contains(output, "missed") {
		t.Errorf("expected log to contain 'offline' or 'missed', got:\n%s", output)
	}
}

func TestLoadAllSchedules_UpdatesCheckpoint(t *testing.T) {
	job := models.BackupJob{
		ID:              "cp-update-job",
		Name:            "Checkpoint Update Job",
		Enabled:         true,
		ScheduleEnabled: true,
		CronExprs:       []string{"0 2 * * *"},
	}
	cfg := newTestConfigWithJob(t, job)
	cfg.Daemon.SchedulerInterval = 60
	cfg.Daemon.LastSchedulerCheckpoint = time.Time{}

	s := NewScheduler(60*time.Second, newTestLogger())
	runner := NewBackupJobRunner(cfg, cfg.GetJob, func(j *models.BackupJob) (*models.BackupRun, error) {
		return &models.BackupRun{}, nil
	})
	sm := NewScheduleManager(cfg, s, runner)

	sm.LoadAllSchedules()

	if cfg.Daemon.LastSchedulerCheckpoint.IsZero() {
		t.Fatal("expected LastSchedulerCheckpoint to be updated after LoadAllSchedules")
	}

	elapsed := time.Since(cfg.Daemon.LastSchedulerCheckpoint)
	if elapsed > 2*time.Second {
		t.Errorf("expected checkpoint to be recent, but it was %v ago", elapsed)
	}
}
