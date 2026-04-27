package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/esignoretti/ds3backup/pkg/models"
)

var (
	jobName      string
	jobPath      string
	jobRetention int
	jobLockMode  string
	jobPassword  string
)

// jobCmd represents the job command
var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manage backup jobs",
	Long:  `Create, list, and manage backup jobs.`,
}

// jobAddCmd represents the job add command
var jobAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new backup job",
	Long: `Add a new backup job to backup a directory.

Example:
  ds3backup job add --name="Documents" --path=~/Documents --retention=30`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load config
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		// Validate path
		absPath, err := expandPath(jobPath)
		if err != nil {
			return fmt.Errorf("invalid path: %w", err)
		}

		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			return fmt.Errorf("path does not exist: %s", absPath)
		}

		// Validate lock mode
		if jobLockMode != "GOVERNANCE" && jobLockMode != "COMPLIANCE" && jobLockMode != "NONE" {
			return fmt.Errorf("invalid lock mode: %s (must be GOVERNANCE, COMPLIANCE, or NONE)", jobLockMode)
		}

		// Validate password
		if jobPassword == "" {
			return fmt.Errorf("encryption password is required (--password flag)")
		}

		// Create job
		job := models.BackupJob{
			Name:             jobName,
			SourcePath:       absPath,
			RetentionDays:    jobRetention,
			ObjectLockMode:   jobLockMode,
			EncryptionPassword: jobPassword,
			Enabled:          true,
		}

		cfg.AddJob(job)

		// Save config
		if err := saveConfig(cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		// Get the job with generated ID
		savedJob := cfg.GetJobByName(jobName)
		if savedJob == nil {
			return fmt.Errorf("failed to retrieve created job")
		}

		fmt.Printf("✓ Backup job created successfully!\n")
		fmt.Printf("  Job ID: %s\n", savedJob.ID)
		fmt.Printf("  Name: %s\n", savedJob.Name)
		fmt.Printf("  Source: %s\n", savedJob.SourcePath)
		fmt.Printf("  Retention: %d days\n", savedJob.RetentionDays)
		fmt.Printf("  Object Lock: %s\n", savedJob.ObjectLockMode)
		fmt.Printf("\nRun backup with: ds3backup backup run %s\n", savedJob.ID)
		fmt.Printf("Note: Encryption password is stored with the job configuration.\n")

		return nil
	},
}

// jobListCmd represents the job list command
var jobListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all backup jobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		if len(cfg.Jobs) == 0 {
			fmt.Println("No backup jobs configured.")
			fmt.Println("Create one with: ds3backup job add --name=\"MyBackup\" --path=~/Documents")
			return nil
		}

		fmt.Printf("Backup Jobs (%d):\n\n", len(cfg.Jobs))
		for i, job := range cfg.Jobs {
			fmt.Printf("%d. %s [%s]\n", i+1, job.Name, job.ID)
			fmt.Printf("   Source: %s\n", job.SourcePath)
			fmt.Printf("   Retention: %d days\n", job.RetentionDays)
			fmt.Printf("   Object Lock: %s\n", job.ObjectLockMode)
			fmt.Printf("   Enabled: %v\n", job.Enabled)
			if job.LastRun != nil {
				fmt.Printf("   Last Run: %s\n", job.LastRun.Format(time.RFC3339))
			} else {
				fmt.Printf("   Last Run: Never\n")
			}
			fmt.Println()
		}

		return nil
	},
}

// jobDeleteCmd represents the job delete command
var jobDeleteCmd = &cobra.Command{
	Use:   "delete <job-id>",
	Short: "Delete a backup job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		jobID := args[0]
		if !cfg.RemoveJob(jobID) {
			return fmt.Errorf("job not found: %s", jobID)
		}

		if err := saveConfig(cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Printf("✓ Job %s deleted successfully\n", jobID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(jobCmd)
	jobCmd.AddCommand(jobAddCmd)
	jobCmd.AddCommand(jobListCmd)
	jobCmd.AddCommand(jobDeleteCmd)

	jobAddCmd.Flags().StringVarP(&jobName, "name", "n", "", "Job name")
	jobAddCmd.Flags().StringVarP(&jobPath, "path", "p", "", "Directory path to backup")
	jobAddCmd.Flags().IntVarP(&jobRetention, "retention", "r", 30, "Retention period in days")
	jobAddCmd.Flags().StringVar(&jobLockMode, "object-lock-mode", "GOVERNANCE", "Object Lock mode (GOVERNANCE, COMPLIANCE, or NONE)")
	jobAddCmd.Flags().StringVar(&jobPassword, "password", "", "Encryption password (required)")

	jobAddCmd.MarkFlagRequired("name")
	jobAddCmd.MarkFlagRequired("path")
	jobAddCmd.MarkFlagRequired("password")
}

// expandPath expands ~ to home directory
func expandPath(path string) (string, error) {
	if path[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = home + path[1:]
	}
	return filepath.Abs(path)
}
