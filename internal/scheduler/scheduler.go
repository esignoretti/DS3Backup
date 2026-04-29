package scheduler

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	mu      sync.Mutex
	cron    *cron.Cron
	entries map[string]cron.EntryID
	interval time.Duration
	running bool
	logger  *log.Logger
}

func NewScheduler(interval time.Duration, logger *log.Logger) *Scheduler {
	return &Scheduler{
		cron:    cron.New(cron.WithParser(cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow))),
		entries: make(map[string]cron.EntryID),
		interval: interval,
		logger:  logger,
	}
}

func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return
	}
	s.cron.Start()
	s.running = true
	s.logger.Println("Scheduler started")
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	ctx := s.cron.Stop()
	<-ctx.Done()
	s.running = false
	s.logger.Println("Scheduler stopped")
}

func (s *Scheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Scheduler) ScheduleJob(jobID string, cronExpr string, runner func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.entries[jobID]; exists {
		s.cron.Remove(s.entries[jobID])
	}

	entryID, err := s.cron.AddFunc(cronExpr, runner)
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
	}

	s.entries[jobID] = entryID
	s.logger.Printf("Scheduled job %s with cron %s", jobID, cronExpr)
	return nil
}

func (s *Scheduler) UnscheduleJob(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, exists := s.entries[jobID]; exists {
		s.cron.Remove(entryID)
		delete(s.entries, jobID)
		s.logger.Printf("Unscheduled job %s", jobID)
	}
}

func (s *Scheduler) GetScheduledJobs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	jobs := make([]string, 0, len(s.entries))
	for jobID := range s.entries {
		jobs = append(jobs, jobID)
	}
	return jobs
}

func (s *Scheduler) HasJob(jobID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, exists := s.entries[jobID]
	return exists
}

// JobSchedule represents a scheduled job entry for ReloadJobs
type JobSchedule struct {
	ID       string
	Enabled  bool
	CronExpr string
}

func (s *Scheduler) ReloadJobs(jobs []JobSchedule, runnerFactory func(jobID string) func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove all current entries
	for _, entryID := range s.entries {
		s.cron.Remove(entryID)
	}
	s.entries = make(map[string]cron.EntryID)

	// Schedule enabled jobs with cron expressions
	for _, job := range jobs {
		if job.Enabled && job.CronExpr != "" {
			entryID, err := s.cron.AddFunc(job.CronExpr, runnerFactory(job.ID))
			if err != nil {
				s.logger.Printf("Warning: failed to schedule job %s: %v", job.ID, err)
				continue
			}
			s.entries[job.ID] = entryID
			s.logger.Printf("Loaded schedule for job %s: %s", job.ID, job.CronExpr)
		}
	}
}
