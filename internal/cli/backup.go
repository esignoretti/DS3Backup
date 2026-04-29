package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/esignoretti/ds3backup/internal/backup"
	"github.com/esignoretti/ds3backup/internal/config"
	"github.com/esignoretti/ds3backup/internal/crypto"
	"github.com/esignoretti/ds3backup/internal/index"
	"github.com/esignoretti/ds3backup/internal/s3client"
	"github.com/esignoretti/ds3backup/pkg/models"
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

		// Create crypto engine using job's stored password
		if job.EncryptionPassword == "" {
			return fmt.Errorf("no encryption password configured for job %s", jobID)
		}
		cryptoEngine, err := crypto.NewCryptoEngine(job.EncryptionPassword, cfg.Encryption.Salt)
		if err != nil {
			return fmt.Errorf("failed to create crypto engine: %w", err)
		}

		// Create index DB in persistent location
		configDir, err := config.ConfigDir()
		if err != nil {
			return fmt.Errorf("failed to get config directory: %w", err)
		}
		indexDir := filepath.Join(configDir, "index", jobID)
		if err := os.MkdirAll(indexDir, 0700); err != nil {
			return err
		}

		indexDB, err := index.OpenIndexDB(indexDir)
		if err != nil {
			return fmt.Errorf("failed to open index: %w", err)
		}
		// Don't use defer - we'll close explicitly with proper cleanup

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

		// Explicitly close index to release BadgerDB locks
		if indexDB != nil {
			indexDB.Close()
			// Force garbage collection to release file handles
			runtime.GC()
			// Sync filesystem
			_ = exec.Command("sync").Run()
		}

		return err
	},
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

var (
	backupListLimit int
	backupListJSON  bool
)

// backupListCmd represents the backup list command
var backupListCmd = &cobra.Command{
	Use:   "list <job-id>",
	Short: "List backup history for a job",
	Long:  `Show all backup runs for a specific job with timestamps, status, and statistics.`,
	Args:  cobra.ExactArgs(1),
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

		// Open index DB
		configDir, err := config.ConfigDir()
		if err != nil {
			return fmt.Errorf("failed to get config directory: %w", err)
		}
		indexDir := filepath.Join(configDir, "index", jobID)
		if err := os.MkdirAll(indexDir, 0700); err != nil {
			return err
		}

		indexDB, err := index.OpenIndexDB(indexDir)
		if err != nil {
			return fmt.Errorf("failed to open index: %w", err)
		}
		defer indexDB.Close()

		// Get backup history
		runs, err := indexDB.GetBackupHistory(jobID, backupListLimit)
		if err != nil {
			return fmt.Errorf("failed to get backup history: %w", err)
		}

		if len(runs) == 0 {
			// Try to list backups from S3
			fmt.Println("ℹ️  No local backup history found. Checking S3...")
			s3Client, err := s3client.NewClient(cfg.S3)
			if err != nil {
				if backupListJSON {
					fmt.Println("[]")
				} else {
					fmt.Println("No backup runs found for this job.")
				}
				return nil
			}
			
			// List backup directories from S3
			backupPrefix := fmt.Sprintf("backups/%s/", jobID)
			objects, err := s3Client.ListObjects(cmd.Context(), backupPrefix)
			if err != nil || len(objects) == 0 {
				if backupListJSON {
					fmt.Println("[]")
				} else {
					fmt.Println("No backup runs found for this job.")
				}
				return nil
			}
			
			// Extract backup timestamps from S3 paths
			fmt.Printf("\n📦 Found %d backup(s) on S3:\n\n", len(objects))
			for i, obj := range objects {
				// Path format: backups/<job-id>/batch_<timestamp> or backups/<job-id>/index_<timestamp>
				parts := strings.Split(obj, "/")
				if len(parts) > 0 {
					fmt.Printf("%d. %s\n", i+1, parts[len(parts)-1])
				}
			}
			fmt.Println("\n💡 Tip: Run 'backup run' to rebuild local index")
			return nil
		}

		if backupListJSON {
			output, _ := json.MarshalIndent(runs, "", "  ")
			fmt.Println(string(output))
			return nil
		}

		// Display as table
		return displayBackupTable(runs)
	},
}

// displayBackupTable displays backup runs in a formatted table
func displayBackupTable(runs []*models.BackupRun) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	fmt.Fprintln(w, "TIMESTAMP\tSTATUS\tFILES\tSIZE\tDURATION")

	// Sort by time (newest first)
	for i := len(runs) - 1; i >= 0; i-- {
		run := runs[i]
		timestamp := run.RunTime.Format("2006-01-02 15:04:05")
		status := run.Status
		files := formatBackupFiles(run)
		size := formatBytes(run.BytesUploaded)
		duration := formatDuration(run.Duration)

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			timestamp, status, files, size, duration)
	}

	w.Flush()
	return nil
}

// formatBackupFiles formats file statistics
func formatBackupFiles(run *models.BackupRun) string {
	parts := []string{}
	if run.FilesAdded > 0 {
		parts = append(parts, fmt.Sprintf("+%d", run.FilesAdded))
	}
	if run.FilesChanged > 0 {
		parts = append(parts, fmt.Sprintf("~%d", run.FilesChanged))
	}
	if run.FilesSkipped > 0 {
		parts = append(parts, fmt.Sprintf("-%d", run.FilesSkipped))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

func init() {
	backupCmd.AddCommand(backupListCmd)
	backupListCmd.Flags().IntVar(&backupListLimit, "limit", 20, "Maximum number of backups to show")
	backupListCmd.Flags().BoolVar(&backupListJSON, "json", false, "Output in JSON format")
}
