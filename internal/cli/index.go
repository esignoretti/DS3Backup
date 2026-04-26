package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/esignoretti/ds3backup/internal/config"
	"github.com/esignoretti/ds3backup/internal/index"
	"github.com/esignoretti/ds3backup/internal/s3client"
)

// indexCmd represents the index command
var indexCmd = &cobra.Command{
	Use:   "index",
	Short: "Manage backup index",
	Long:  `View and manage backup index.`,
}

// indexShowCmd represents the index show command
var indexShowCmd = &cobra.Command{
	Use:   "show <job-id>",
	Short: "Show index statistics for a job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jobID := args[0]

		// Get config directory
		configDir, err := config.ConfigDir()
		if err != nil {
			return fmt.Errorf("failed to get config directory: %w", err)
		}

		indexDir := filepath.Join(configDir, "index", jobID)
		
		// Check if index exists
		if _, err := os.Stat(indexDir); os.IsNotExist(err) {
			return fmt.Errorf("index not found for job %s (no backups have been run yet)", jobID)
		}

		// Open index
		idxDB, err := index.OpenIndexDB(indexDir)
		if err != nil {
			return fmt.Errorf("failed to open index: %w", err)
		}
		defer idxDB.Close()

		// Get backup history
		runs, err := idxDB.GetBackupHistory(jobID, 100)
		if err != nil {
			return fmt.Errorf("failed to get backup history: %w", err)
		}

		fmt.Printf("Index Statistics for Job: %s\n", jobID)
		fmt.Printf("================================\n\n")
		
		fmt.Printf("Total backup runs: %d\n", len(runs))
		
		if len(runs) > 0 {
			fmt.Printf("\nLast backup run:\n")
			lastRun := runs[0]
			fmt.Printf("  Time: %s\n", lastRun.RunTime.Format(time.RFC3339))
			fmt.Printf("  Status: %s\n", lastRun.Status)
			fmt.Printf("  Files added: %d\n", lastRun.FilesAdded)
			fmt.Printf("  Files changed: %d\n", lastRun.FilesChanged)
			fmt.Printf("  Files skipped: %d\n", lastRun.FilesSkipped)
			fmt.Printf("  Bytes uploaded: %s\n", formatBytes(lastRun.BytesUploaded))
			fmt.Printf("  Duration: %s\n", lastRun.Duration)
			
			if lastRun.IndexSyncFailed {
				fmt.Printf("  ⚠️  Index sync to S3: FAILED\n")
			} else {
				fmt.Printf("  ✓ Index sync to S3: OK\n")
			}
		}

		return nil
	},
}

// indexRebuildCmd represents the index rebuild command
var indexRebuildCmd = &cobra.Command{
	Use:   "rebuild <job-id>",
	Short: "Rebuild local index from S3",
	Long: `Rebuild the local index by downloading metadata from S3.

This is useful if:
- Local index is corrupted or lost
- You want to sync index from another machine
- Index sync failed during backup

Note: This can take a while for large backups.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jobID := args[0]

		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		fmt.Printf("Rebuilding index for job: %s\n", jobID)
		fmt.Println("This may take a while depending on the number of backups...")

		// Create S3 client
		s3Client, err := s3client.NewClient(cfg.S3)
		if err != nil {
			return fmt.Errorf("failed to create S3 client: %w", err)
		}

		// Get config directory
		configDir, err := config.ConfigDir()
		if err != nil {
			return fmt.Errorf("failed to get config directory: %w", err)
		}

		indexDir := filepath.Join(configDir, "index", jobID)
		
		// Create/overwrite index directory
		if err := os.MkdirAll(indexDir, 0700); err != nil {
			return fmt.Errorf("failed to create index directory: %w", err)
		}

		// Open index
		idxDB, err := index.OpenIndexDB(indexDir)
		if err != nil {
			return fmt.Errorf("failed to open index: %w", err)
		}
		defer idxDB.Close()

		// List all backup objects
		fmt.Println("\nScanning S3 for backup metadata...")
		
		backupPrefix := fmt.Sprintf("backups/%s/", jobID)
		objects, err := s3Client.ListObjects(cmd.Context(), backupPrefix)
		if err != nil {
			return fmt.Errorf("failed to list backup objects: %w", err)
		}

		fmt.Printf("Found %d objects in S3\n", len(objects))

		// TODO: Implement actual rebuild logic
		// For now, just show what we found
		fmt.Println("\n⚠️  Index rebuild not yet fully implemented.")
		fmt.Println("    Local index will be rebuilt on next backup run.")
		
		return nil
	},
}

// indexClearCmd represents the index clear command
var indexClearCmd = &cobra.Command{
	Use:   "clear <job-id>",
	Short: "Clear local index for a job",
	Long: `Clear the local index for a job.

The index will be rebuilt on the next backup run.
Use this if the index is corrupted or taking too much space.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jobID := args[0]
		
		confirm, _ := cmd.Flags().GetBool("yes")
		
		if !confirm {
			fmt.Printf("⚠️  WARNING: This will delete the local index for job %s\n", jobID)
			fmt.Printf("    The index will be rebuilt on the next backup run.\n")
			fmt.Printf("    This may cause the next backup to take longer.\n\n")
			fmt.Printf("Are you sure? Use --yes to confirm.\n")
			return nil
		}

		// Get config directory
		configDir, err := config.ConfigDir()
		if err != nil {
			return fmt.Errorf("failed to get config directory: %w", err)
		}

		indexDir := filepath.Join(configDir, "index", jobID)
		
		// Remove index directory
		if err := os.RemoveAll(indexDir); err != nil {
			return fmt.Errorf("failed to clear index: %w", err)
		}

		fmt.Printf("✓ Index cleared for job %s\n", jobID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(indexCmd)
	indexCmd.AddCommand(indexShowCmd)
	indexCmd.AddCommand(indexRebuildCmd)
	indexCmd.AddCommand(indexClearCmd)

	indexClearCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
}
