package scheduler

import (
	"fmt"
	"log"
	"time"

	"github.com/esignoretti/ds3backup/internal/config"
)

type ScheduleManager struct {
	cfg       *config.Config
	scheduler *Scheduler
	runner    *BackupJobRunner
}

func NewScheduleManager(cfg *config.Config, scheduler *Scheduler, runner *BackupJobRunner) *ScheduleManager {
	return &ScheduleManager{
		cfg:       cfg,
		scheduler: scheduler,
		runner:    runner,
	}
}

func (m *ScheduleManager) EnableJobSchedule(jobID string, cronExpr string) error {
	job := m.cfg.GetJob(jobID)
	if job == nil {
		return fmt.Errorf("job not found: %s", jobID)
	}

	// Validate cron expression by trying to schedule
	if err := m.scheduler.ScheduleJob(jobID, cronExpr, m.runner.RunnerFactory()(jobID)); err != nil {
		return err
	}

	job.ScheduleEnabled = true
	job.CronExpr = cronExpr

	if err := m.cfg.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	log.Printf("Schedule enabled for job %s: %s", jobID, cronExpr)
	return nil
}

func (m *ScheduleManager) DisableJobSchedule(jobID string) {
	job := m.cfg.GetJob(jobID)
	if job == nil {
		return
	}

	m.scheduler.UnscheduleJob(jobID)
	job.ScheduleEnabled = false

	if err := m.cfg.SaveConfig(); err != nil {
		log.Printf("Warning: failed to save config after disabling schedule for %s: %v", jobID, err)
	}

	log.Printf("Schedule disabled for job %s", jobID)
}

func (m *ScheduleManager) RescheduleJob(jobID string, cronExpr string) error {
	job := m.cfg.GetJob(jobID)
	if job == nil {
		return fmt.Errorf("job not found: %s", jobID)
	}

	m.scheduler.UnscheduleJob(jobID)

	if err := m.scheduler.ScheduleJob(jobID, cronExpr, m.runner.RunnerFactory()(jobID)); err != nil {
		return err
	}

	job.CronExpr = cronExpr
	job.ScheduleEnabled = true

	if err := m.cfg.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	log.Printf("Schedule updated for job %s: %s", jobID, cronExpr)
	return nil
}

func (m *ScheduleManager) LoadAllSchedules() {
	schedules := make([]JobSchedule, 0, len(m.cfg.Jobs))
	for _, job := range m.cfg.Jobs {
		schedules = append(schedules, JobSchedule{
			ID:       job.ID,
			Enabled:  job.ScheduleEnabled,
			CronExpr: job.CronExpr,
		})
	}

	m.scheduler.ReloadJobs(schedules, m.runner.RunnerFactory())
	log.Printf("Loaded %d job schedules", len(schedules))
}

// UpdateRunTime updates the last run time for a job after a backup completes
func (m *ScheduleManager) UpdateRunTime(jobID string) {
	job := m.cfg.GetJob(jobID)
	if job == nil {
		return
	}

	now := time.Now()
	job.LastRun = &now

	if err := m.cfg.SaveConfig(); err != nil {
		log.Printf("Warning: failed to save config after updating run time for %s: %v", jobID, err)
	}
}
