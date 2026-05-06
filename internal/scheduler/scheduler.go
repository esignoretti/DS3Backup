package scheduler

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/esignoretti/ds3backup/pkg/models"
)

type Scheduler struct {
	mu             sync.Mutex
	cron           *cron.Cron
	entries        map[string][]cron.EntryID
	interval       time.Duration
	running        bool
	logger         *log.Logger
	checkpointTime time.Time
	startTime      time.Time
}

func NewScheduler(interval time.Duration, logger *log.Logger) *Scheduler {
	return &Scheduler{
		cron:           cron.New(cron.WithParser(cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor))),
		entries:        make(map[string][]cron.EntryID),
		interval:       interval,
		logger:         logger,
		checkpointTime: time.Time{},
	}
}

func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return
	}
	s.startTime = time.Now()
	s.cron.Start()
	s.running = true
	if !s.checkpointTime.IsZero() {
		elapsed := time.Since(s.checkpointTime)
		if elapsed > s.interval*2 {
			s.logger.Printf("Scheduler offline for %v — missed schedule window detected", elapsed)
		}
	}
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

func (s *Scheduler) SetCheckpointTime(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpointTime = t
}

func (s *Scheduler) GetCheckpointTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checkpointTime
}

func (s *Scheduler) ScheduleJob(jobID string, schedules []models.ScheduleEntry, runnerFuncFor func(jobID string, fullBackup bool) func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, exists := s.entries[jobID]; exists {
		for _, entryID := range existing {
			s.cron.Remove(entryID)
		}
		delete(s.entries, jobID)
	}

	if len(schedules) == 0 {
		return fmt.Errorf("at least one schedule entry is required")
	}

	var entryIDs []cron.EntryID
	for _, sch := range schedules {
		entryID, err := s.cron.AddFunc(sch.Expr, runnerFuncFor(jobID, sch.FullBackup))
		if err != nil {
			for _, eid := range entryIDs {
				s.cron.Remove(eid)
			}
			return fmt.Errorf("invalid cron expression %q: %w", sch.Expr, err)
		}
		entryIDs = append(entryIDs, entryID)
	}

	s.entries[jobID] = entryIDs
	s.logger.Printf("Scheduled job %s with %d schedules", jobID, len(schedules))
	return nil
}

func (s *Scheduler) UnscheduleJob(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryIDs, exists := s.entries[jobID]; exists {
		for _, entryID := range entryIDs {
			s.cron.Remove(entryID)
		}
		delete(s.entries, jobID)
		s.logger.Printf("Unscheduled job %s (%d entries removed)", jobID, len(entryIDs))
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
	entryIDs, exists := s.entries[jobID]
	return exists && len(entryIDs) > 0
}

// JobSchedule represents a scheduled job entry for ReloadJobs
type JobSchedule struct {
	ID        string
	Enabled   bool
	Schedules []models.ScheduleEntry
}

func (s *Scheduler) ReloadJobs(jobs []JobSchedule, runnerFuncFor func(jobID string, fullBackup bool) func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove all current entries
	for _, entryIDs := range s.entries {
		for _, entryID := range entryIDs {
			s.cron.Remove(entryID)
		}
	}
	s.entries = make(map[string][]cron.EntryID)

	// Schedule enabled jobs with schedules
	for _, job := range jobs {
		if job.Enabled && len(job.Schedules) > 0 {
			var entryIDs []cron.EntryID
			for _, sch := range job.Schedules {
				entryID, err := s.cron.AddFunc(sch.Expr, runnerFuncFor(job.ID, sch.FullBackup))
				if err != nil {
					s.logger.Printf("Warning: failed to schedule job %s expr %q: %v", job.ID, sch.Expr, err)
					continue
				}
				entryIDs = append(entryIDs, entryID)
			}
			if len(entryIDs) > 0 {
				s.entries[job.ID] = entryIDs
				s.logger.Printf("Loaded schedule for job %s: %d schedules", job.ID, len(entryIDs))
			}
		}
	}
}
