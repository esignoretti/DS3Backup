package scheduler

import (
	"log"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func newTestLogger() *log.Logger {
	return log.New(os.Stderr, "", 0)
}

func TestScheduleJob_ParsesValidCron(t *testing.T) {
	s := NewScheduler(60*time.Second, newTestLogger())
	ran := make(chan struct{}, 1)

	err := s.ScheduleJob("test-job", "0 2 * * *", func() {
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

	err := s.ScheduleJob("bad-job", "not a cron expression", func() {})
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}

func TestReloadJobs_SchedulesEnabledOnly(t *testing.T) {
	s := NewScheduler(60*time.Second, newTestLogger())

	jobs := []JobSchedule{
		{ID: "job-1", Enabled: true, CronExpr: "0 2 * * *"},
		{ID: "job-2", Enabled: false, CronExpr: "0 3 * * *"},
		{ID: "job-3", Enabled: true, CronExpr: ""},
		{ID: "job-4", Enabled: true, CronExpr: "0 4 * * *"},
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

	s.ScheduleJob("job-1", "0 2 * * *", func() {})
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
	s.ScheduleJob("trigger-test", "* * * * *", func() {
		ran.Store(true)
	})

	time.Sleep(3200 * time.Millisecond)
	if !ran.Load() {
		t.Error("expected runner to be triggered")
	}
}
