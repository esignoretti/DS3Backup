package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/esignoretti/ds3backup/internal/backup"
	"github.com/esignoretti/ds3backup/internal/crypto"
	"github.com/esignoretti/ds3backup/internal/index"
	"github.com/esignoretti/ds3backup/internal/s3client"
)

var (
	fullBackup   bool
	jsonOutput   bool
)

// backupCmd represents the backup command
var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Run backup operations",
	Long:  `Execute backup jobs and view backup status.`,
}

// backupRunCmd represents the backup run command
var backupRunCmd = &cobra.Command{
	Use:   "run <job-id>",
	Short: "Run a backup job",
	Long: `Execute a backup job.

By default, runs an incremental backup. Use --full for a full backup.

Example:
  ds3backup backup run job_123456
  ds3backup backup run job_123456 --full`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jobID := args[0]

		// Load config
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		// Find job
		job := cfg.GetJob(jobID)
		if job == nil {
			return fmt.Errorf("job not found: %s", jobID)
		}

		if !job.Enabled {
			return fmt.Errorf("job is disabled: %s", jobID)
		}

		fmt.Printf("Starting backup for job: %s\n", job.Name)
		fmt.Printf("Source: %s\n", job.SourcePath)
		if fullBackup {
			fmt.Println("Type: FULL backup")
		} else {
			fmt.Println("Type: Incremental backup")
		}
		fmt.Println()

		// Create S3 client
		s3Client, err := s3client.NewClient(cfg.S3)
		if err != nil {
			return fmt.Errorf("failed to create S3 client: %w", err)
		}

		// Create crypto engine
		cryptoEngine, err := crypto.NewCryptoEngine(password, cfg.Encryption.Salt)
		if err != nil {
			return fmt.Errorf("failed to create crypto engine: %w", err)
		}

		// Create index DB
		indexDir := filepath.Join(os.TempDir(), "ds3backup-index-"+jobID)
		if err := os.MkdirAll(indexDir, 0700); err != nil {
			return err
		}
		defer os.RemoveAll(indexDir)

		indexDB, err := index.OpenIndexDB(indexDir)
		if err != nil {
			return fmt.Errorf("failed to open index: %w", err)
		}
		defer indexDB.Close()

		// Create backup engine
		engine := backup.NewBackupEngine(cfg, s3Client, indexDB, cryptoEngine)

		// Run backup with progress
		run, err := engine.RunBackup(job, fullBackup, func(progress backup.BackupProgress) {
			if jsonOutput {
				// JSON progress output
				status := map[string]interface{}{
					"percent":        progress.Percent,
					"filesProcessed": progress.FilesProcessed,
					"totalFiles":     progress.TotalFiles,
					"bytesUploaded":  progress.BytesUploaded,
					"currentFile":    progress.CurrentFile,
				}
				data, _ := json.Marshal(status)
				fmt.Fprintf(os.Stderr, "%s\n", string(data))
			} else {
				// Text progress output
				fmt.Printf("\r[%3d%%] Files: %d/%d, Uploaded: %s", 
					progress.Percent,
					progress.FilesProcessed,
					progress.TotalFiles,
					formatBytes(progress.BytesUploaded))
			}
		})

		fmt.Println() // New line after progress

		// Update job in config
		now := time.Now()
		job.LastRun = &now
		cfg.SaveConfig()

		// Output results
		if jsonOutput {
			output, _ := json.MarshalIndent(run, "", "  ")
			fmt.Println(string(output))
		} else {
			if err != nil {
				fmt.Printf("❌ Backup failed: %v\n", err)
				if run.Error != "" {
					fmt.Printf("   Error: %s\n", run.Error)
				}
			} else {
				fmt.Printf("✅ Backup completed\n")
				fmt.Printf("   Files added: %d\n", run.FilesAdded)
				fmt.Printf("   Files changed: %d\n", run.FilesChanged)
				fmt.Printf("   Files skipped (duplicates): %d\n", run.FilesSkipped)
				if run.FilesFailed > 0 {
					fmt.Printf("   Files failed: %d\n", run.FilesFailed)
				}
				fmt.Printf("   Batches uploaded: %d\n", run.BatchesUploaded)
				fmt.Printf("   Bytes uploaded: %s\n", formatBytes(run.BytesUploaded))
				fmt.Printf("   Duration: %s\n", run.Duration)

				if run.IndexSyncFailed {
					fmt.Printf("   ⚠️  WARNING: Index sync to S3 failed (local index updated, can rebuild from S3)\n")
				}
			}
		}

		return err
	},
}

// formatBytes formats bytes as human-readable string
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// backupStatusCmd represents the backup status command
var backupStatusCmd = &cobra.Command{
	Use:   "status <job-id>",
	Short: "Show backup job status",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jobID := args[0]

		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		job := cfg.GetJob(jobID)
		if job == nil {
			return fmt.Errorf("job not found: %s", jobID)
		}

		fmt.Printf("Job: %s [%s]\n", job.Name, job.ID)
		fmt.Printf("  Source: %s\n", job.SourcePath)
		fmt.Printf("  Retention: %d days\n", job.RetentionDays)
		fmt.Printf("  Object Lock: %s\n", job.ObjectLockMode)
		fmt.Printf("  Enabled: %v\n", job.Enabled)

		if job.LastRun != nil {
			fmt.Printf("  Last Run: %s\n", job.LastRun.Format(time.RFC3339))
		} else {
			fmt.Printf("  Last Run: Never\n")
		}

		if job.LastError != "" {
			fmt.Printf("  Last Error: %s\n", job.LastError)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(backupCmd)
	backupCmd.AddCommand(backupRunCmd)
	backupCmd.AddCommand(backupStatusCmd)

	backupRunCmd.Flags().BoolVar(&fullBackup, "full", false, "Force full backup")
	backupRunCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
