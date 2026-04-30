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

func (m *ScheduleManager) EnableJobSchedule(jobID string, cronExprs []string) error {
	if len(cronExprs) == 0 {
		return fmt.Errorf("at least one cron expression is required")
	}

	job := m.cfg.GetJob(jobID)
	if job == nil {
		return fmt.Errorf("job not found: %s", jobID)
	}

	// Validate cron expressions by trying to schedule
	if err := m.scheduler.ScheduleJob(jobID, cronExprs, m.runner.RunnerFactory()(jobID)); err != nil {
		return err
	}

	job.ScheduleEnabled = true
	job.CronExprs = cronExprs

	if err := m.cfg.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	log.Printf("Schedule enabled for job %s: %v", jobID, cronExprs)
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

func (m *ScheduleManager) RescheduleJob(jobID string, cronExprs []string) error {
	if len(cronExprs) == 0 {
		return fmt.Errorf("at least one cron expression is required")
	}

	job := m.cfg.GetJob(jobID)
	if job == nil {
		return fmt.Errorf("job not found: %s", jobID)
	}

	m.scheduler.UnscheduleJob(jobID)

	if err := m.scheduler.ScheduleJob(jobID, cronExprs, m.runner.RunnerFactory()(jobID)); err != nil {
		return err
	}

	job.CronExprs = cronExprs
	job.ScheduleEnabled = true

	if err := m.cfg.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	log.Printf("Schedule updated for job %s: %v", jobID, cronExprs)
	return nil
}

func (m *ScheduleManager) LoadAllSchedules() {
	// Check for missed schedules since last checkpoint (D-07, D-08)
	if !m.cfg.Daemon.LastSchedulerCheckpoint.IsZero() {
		elapsed := time.Since(m.cfg.Daemon.LastSchedulerCheckpoint)
		if elapsed > time.Duration(m.cfg.Daemon.SchedulerInterval)*time.Second*2 {
			log.Printf("Scheduler was offline for %v. Checking for missed schedules...", elapsed)
			for _, job := range m.cfg.Jobs {
				if job.ScheduleEnabled && len(job.CronExprs) > 0 {
					log.Printf("Job %s may have missed %d scheduled runs during offline period (last checkpoint: %s)",
						job.ID, len(job.CronExprs), m.cfg.Daemon.LastSchedulerCheckpoint.Format(time.RFC3339))
				}
			}
		}
	}

	schedules := make([]JobSchedule, 0, len(m.cfg.Jobs))
	for _, job := range m.cfg.Jobs {
		schedules = append(schedules, JobSchedule{
			ID:        job.ID,
			Enabled:   job.ScheduleEnabled,
			CronExprs: job.CronExprs,
		})
	}

	m.scheduler.ReloadJobs(schedules, m.runner.RunnerFactory())

	// Persist scheduler checkpoint (D-08)
	checkpoint := time.Now()
	m.cfg.Daemon.LastSchedulerCheckpoint = checkpoint
	m.scheduler.SetCheckpointTime(checkpoint)
	if err := m.cfg.SaveConfig(); err != nil {
		log.Printf("Warning: failed to save scheduler checkpoint: %v", err)
	}

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
