package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/esignoretti/ds3backup/internal/config"
	"github.com/esignoretti/ds3backup/internal/crypto"
	"github.com/esignoretti/ds3backup/internal/index"
	"github.com/esignoretti/ds3backup/internal/restore"
	"github.com/esignoretti/ds3backup/internal/s3client"
	"github.com/esignoretti/ds3backup/pkg/models"
)

var (
	restoreTo         string
	restoreDryRun     bool
	restoreVerify     bool
	restoreOverwrite  bool
	restoreInclude    []string
	restoreExclude    []string
	restoreJSON       bool
	restorePassword   string
)

// restoreCmd represents the restore command
var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore files from backup",
	Long:  `Restore backed-up files from S3.`,
}

// restoreRunCmd represents the restore run command
var restoreRunCmd = &cobra.Command{
	Use:   "run <job-id>",
	Short: "Restore files from latest backup",
	Long:  `Restore all files from the latest backup run.`,
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

		if !restoreDryRun && restorePassword == "" {
			return fmt.Errorf("password required for restore (use --password flag)")
		}

		fmt.Printf("Starting restore for job: %s\n", job.Name)
		if restoreTo != "" {
			fmt.Printf("Destination: %s\n", restoreTo)
		} else {
			fmt.Printf("Destination: Original paths\n")
		}
		if restoreDryRun {
			fmt.Println("Mode: Dry-run (preview only)")
		} else if restoreVerify {
			fmt.Println("Mode: Verification only")
		}
		if len(restoreInclude) > 0 {
			fmt.Printf("Include patterns: %v\n", restoreInclude)
		}
		if len(restoreExclude) > 0 {
			fmt.Printf("Exclude patterns: %v\n", restoreExclude)
		}
		fmt.Println()

		// Create S3 client
		s3Client, err := s3client.NewClient(cfg.S3)
		if err != nil {
			return fmt.Errorf("failed to create S3 client: %w", err)
		}

		// Create crypto engine
		cryptoEngine, err := crypto.NewCryptoEngine(restorePassword, cfg.Encryption.Salt)
		if err != nil {
			return fmt.Errorf("failed to create crypto engine: %w", err)
		}

		// Create index DB
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

		// Create restore engine
		engine := restore.NewRestoreEngine(cfg, s3Client, indexDB, cryptoEngine)

		// Restore options
		opts := &models.RestoreOptions{
			DestinationPath: restoreTo,
			DryRun:          restoreDryRun,
			Overwrite:       restoreOverwrite,
			IncludePatterns: restoreInclude,
			ExcludePatterns: restoreExclude,
			Concurrency:     8,
		}

		if restoreVerify {
			return runVerify(engine, jobID)
		}

		if restoreDryRun {
			return runDryRun(engine, jobID, opts)
		}

		return runRestore(engine, jobID, opts)
	},
}

func runVerify(engine *restore.RestoreEngine, jobID string) error {
	fmt.Println("🔍 Verifying backup integrity...")
	fmt.Println()

	result, err := engine.Verify(jobID)
	if err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}

	if restoreJSON {
		output, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(output))
		return nil
	}

	if result.Failed == 0 {
		fmt.Printf("✅ All %d files verified successfully\n", result.Verified)
		fmt.Println("   All hashes match")
		fmt.Println("   All files decryptable")
	} else {
		fmt.Printf("❌ %d files failed verification\n", result.Failed)
		fmt.Println()
		fmt.Println("Failed files:")
		for i, errMsg := range result.Errors {
			if i >= 10 {
				fmt.Printf("   ... and %d more\n", len(result.Errors)-10)
				break
			}
			fmt.Printf("   - %s\n", errMsg)
		}
		fmt.Printf("\nVerified: %d/%d files (%.1f%%)\n",
			result.Verified, result.Total,
			float64(result.Verified)*100/float64(result.Total))
	}

	return nil
}

func runDryRun(engine *restore.RestoreEngine, jobID string, opts *models.RestoreOptions) error {
	fmt.Println("📋 Restore Preview (Dry Run)")
	fmt.Println()

	result, err := engine.DryRun(jobID, opts)
	if err != nil {
		return fmt.Errorf("dry-run failed: %w", err)
	}

	if restoreJSON {
		output, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(output))
		return nil
	}

	fmt.Printf("Files to restore: %d\n", result.FilesToRestore)
	fmt.Printf("Files to skip (already exist): %d\n", result.FilesSkipped)
	fmt.Printf("Total size: %s\n", restore.FormatBytes(result.TotalSize))
	
	// Estimate time (rough estimate at 10 MB/s)
	estimatedSeconds := float64(result.TotalSize) / 10 / 1024 / 1024
	if estimatedSeconds < 60 {
		fmt.Printf("Estimated time: ~%.0f seconds\n", estimatedSeconds)
	} else {
		fmt.Printf("Estimated time: ~%.1f minutes\n", estimatedSeconds/60)
	}
	fmt.Println()

	if len(result.SampleFiles) > 0 {
		fmt.Println("Sample files:")
		for _, f := range result.SampleFiles {
			fmt.Printf("  - %s (%s)\n", f.Path, restore.FormatBytes(f.Size))
		}
		if result.MoreFiles > 0 {
			fmt.Printf("  ... (%d more files)\n", result.MoreFiles)
		}
	}

	return nil
}

func runRestore(engine *restore.RestoreEngine, jobID string, opts *models.RestoreOptions) error {
	fmt.Println("🔄 Restoring files...")
	fmt.Println()

	// Get total files for progress
	configDir, _ := config.ConfigDir()
	indexDir := filepath.Join(configDir, "index", jobID)
	indexDB, err := index.OpenIndexDB(indexDir)
	if err != nil {
		return err
	}
	defer indexDB.Close()

	entries, err := indexDB.GetAllEntries(jobID)
	if err != nil {
		return err
	}

	totalFiles := len(entries)
	
	// Create progress tracker
	tracker := restore.NewProgressTracker(totalFiles)

	result, err := engine.RestoreWithProgress(jobID, opts, tracker)
	if err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	if restoreJSON {
		output, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(output))
		return nil
	}

	fmt.Println()
	fmt.Println("✅ Restore completed")
	fmt.Printf("   Files restored: %d\n", result.FilesRestored)
	fmt.Printf("   Files skipped: %d\n", result.FilesSkipped)
	if result.FilesFailed > 0 {
		fmt.Printf("   Files failed: %d\n", result.FilesFailed)
	}
	fmt.Printf("   Bytes restored: %s\n", restore.FormatBytes(result.BytesRestored))
	fmt.Printf("   Duration: %s\n", formatDuration(time.Duration(result.Duration)*time.Second))

	if len(result.Warnings) > 0 {
		fmt.Printf("   ⚠️  Metadata warnings: %d\n", len(result.Warnings))
		maxWarnings := 5
		for i, w := range result.Warnings {
			if i >= maxWarnings {
				fmt.Printf("      ... and %d more\n", len(result.Warnings)-maxWarnings)
				break
			}
			fmt.Printf("      - %s\n", w)
		}
	}

	if result.FilesFailed > 0 && len(result.Errors) > 0 {
		fmt.Println()
		fmt.Println("Failed files:")
		maxErrors := 10
		for i, errMsg := range result.Errors {
			if i >= maxErrors {
				fmt.Printf("   ... and %d more\n", len(result.Errors)-maxErrors)
				break
			}
			fmt.Printf("   - %s\n", errMsg)
		}
	}

	return nil
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm %ds", minutes, seconds)
}

func init() {
	rootCmd.AddCommand(restoreCmd)
	restoreCmd.AddCommand(restoreRunCmd)

	restoreRunCmd.Flags().StringVar(&restoreTo, "to", "", "Restore to alternate location")
	restoreRunCmd.Flags().BoolVar(&restoreDryRun, "dry-run", false, "Preview restore without downloading")
	restoreRunCmd.Flags().BoolVar(&restoreVerify, "verify", false, "Verify backup integrity without restoring")
	restoreRunCmd.Flags().BoolVar(&restoreOverwrite, "overwrite", false, "Overwrite existing files")
	restoreRunCmd.Flags().StringSliceVar(&restoreInclude, "include", nil, "Include files matching pattern (e.g., \"**/*.pdf\")")
	restoreRunCmd.Flags().StringSliceVar(&restoreExclude, "exclude", nil, "Exclude files matching pattern")
	restoreRunCmd.Flags().StringVar(&restorePassword, "password", "", "Encryption password (required for restore)")
	restoreRunCmd.Flags().BoolVar(&restoreJSON, "json", false, "JSON output")
}
