package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
	restoreTime       string
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
		engine := restore.NewRestoreEngine(cfg, s3Client, indexDB, cryptoEngine, jobID)

		// Restore options
		opts := &models.RestoreOptions{
			DestinationPath: restoreTo,
			DryRun:          restoreDryRun,
			Overwrite:       restoreOverwrite,
			IncludePatterns: restoreInclude,
			ExcludePatterns: restoreExclude,
			Concurrency:     4, // Reduced from 8 due to MinIO SDK timeout issues
		}

		if restoreVerify {
			return runVerify(engine, jobID)
		}

		// Handle point-in-time restore
		if restoreTime != "" {
			return runRestoreWithTime(engine, jobID, indexDB, opts)
		}

		if restoreDryRun {
			return runDryRun(engine, jobID, opts)
		}

		return runRestore(engine, jobID, indexDB, opts)
	},
}

// runRestoreWithTime handles restore from a specific point in time
func runRestoreWithTime(engine *restore.RestoreEngine, jobID string, indexDB *index.IndexDB, opts *models.RestoreOptions) error {
	// Parse time string
	targetTime, err := parseTime(restoreTime)
	if err != nil {
		return err
	}

	// Find backup run at that time
	targetRun, err := indexDB.GetRunByTime(jobID, targetTime)
	if err != nil {
		return fmt.Errorf("failed to find backup at specified time: %w", err)
	}
	if targetRun == nil {
		return fmt.Errorf("no backup found at or before %s\nRun 'ds3backup backup list %s' to see available backups", restoreTime, jobID)
	}

	// Warn if backup failed
	if targetRun.Status == "failed" {
		fmt.Printf("⚠️  Warning: selected backup run failed (status: %s)\n", targetRun.Status)
		fmt.Println("   Restore may be incomplete.")
		fmt.Println()
	}

	fmt.Printf("📅 Restoring from backup: %s\n", targetRun.RunTime.Format("2006-01-02 15:04:05"))
	fmt.Println()

	// Get entries for that specific run
	entries, err := indexDB.GetEntriesForRun(jobID, targetRun.RunTime)
	if err != nil {
		return fmt.Errorf("failed to get entries for specified backup: %w", err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("no files found in backup at %s", targetRun.RunTime.Format("2006-01-02 15:04:05"))
	}

	if opts.DryRun {
		return runDryRunWithEntries(engine, jobID, opts, entries)
	}

	return runRestoreWithEntries(engine, jobID, opts, entries)
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
	fmt.Printf("Total size: %s\n", formatBytes(result.TotalSize))
	
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
			fmt.Printf("  - %s (%s)\n", f.Path, formatBytes(f.Size))
		}
		if result.MoreFiles > 0 {
			fmt.Printf("  ... (%d more files)\n", result.MoreFiles)
		}
	}

	return nil
}

func runDryRunWithEntries(engine *restore.RestoreEngine, jobID string, opts *models.RestoreOptions, entries []*models.FileEntry) error {
	fmt.Println("📋 Restore Preview (Dry Run)")
	fmt.Println()

	// Calculate statistics
	var filesToRestore int
	var filesToSkip int
	var totalSize int64

	for _, entry := range entries {
		destPath := filepath.Join(opts.DestinationPath, entry.Path)
		if _, err := os.Stat(destPath); err == nil && !opts.Overwrite {
			filesToSkip++
		} else {
			filesToRestore++
			totalSize += entry.Size
		}
	}

	fmt.Printf("Files to restore: %d\n", filesToRestore)
	fmt.Printf("Files to skip (already exist): %d\n", filesToSkip)
	fmt.Printf("Total size: %s\n", formatBytes(totalSize))
	
	// Estimate time
	estimatedSeconds := float64(totalSize) / 10 / 1024 / 1024
	if estimatedSeconds < 60 {
		fmt.Printf("Estimated time: ~%.0f seconds\n", estimatedSeconds)
	} else {
		fmt.Printf("Estimated time: ~%.1f minutes\n", estimatedSeconds/60)
	}
	fmt.Println()

	// Show sample files
	maxSamples := 20
	sampleCount := 0
	for _, entry := range entries {
		if sampleCount >= maxSamples {
			fmt.Printf("  ... (%d more files)\n", len(entries)-sampleCount)
			break
		}
		fmt.Printf("  - %s (%s)\n", entry.Path, formatBytes(entry.Size))
		sampleCount++
	}

	return nil
}

func runRestore(engine *restore.RestoreEngine, jobID string, indexDB *index.IndexDB, opts *models.RestoreOptions) error {
	fmt.Println("🔄 Restoring files...")
	fmt.Println()

	// Get total files for progress
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

	return displayRestoreResult(result)
}

func runRestoreWithEntries(engine *restore.RestoreEngine, jobID string, opts *models.RestoreOptions, entries []*models.FileEntry) error {
	fmt.Println("🔄 Restoring files...")
	fmt.Println()

	totalFiles := len(entries)
	
	// Create progress tracker
	tracker := restore.NewProgressTracker(totalFiles)

	result, err := engine.RestoreEntries(jobID, opts, tracker, entries)
	if err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	return displayRestoreResult(result)
}

func displayRestoreResult(result *models.RestoreResult) error {
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
	fmt.Printf("   Bytes restored: %s\n", formatBytes(result.BytesRestored))
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

// parseTime parses absolute timestamps and relative time expressions
func parseTime(timeStr string) (time.Time, error) {
	// Try relative time first (e.g., 1h, 2d, 1w)
	if relativePattern.MatchString(timeStr) {
		return parseRelativeTime(timeStr)
	}

	// Try absolute time formats
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, timeStr); err == nil {
			// If no timezone specified, treat as UTC
			if format == "2006-01-02 15:04:05" || format == "2006-01-02" {
				return t.UTC(), nil
			}
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("invalid time format '%s'\nSupported formats: 2024-04-26T10:30:00Z, 2024-04-26 10:30:00, 1h, 2d, 1w", timeStr)
}

// parseRelativeTime parses relative time expressions like 1h, 2d, 1w
func parseRelativeTime(timeStr string) (time.Time, error) {
	matches := relativePattern.FindStringSubmatch(timeStr)
	if len(matches) != 3 {
		return time.Time{}, fmt.Errorf("invalid relative time format")
	}

	n, err := strconv.Atoi(matches[1])
	if err != nil {
		return time.Time{}, err
	}

	unit := matches[2]
	now := time.Now()

	switch unit {
	case "h":
		return now.Add(-time.Duration(n) * time.Hour), nil
	case "d":
		return now.Add(-time.Duration(n) * 24 * time.Hour), nil
	case "w":
		return now.Add(-time.Duration(n) * 7 * 24 * time.Hour), nil
	default:
		return time.Time{}, fmt.Errorf("unknown time unit: %s", unit)
	}
}

var relativePattern = regexp.MustCompile(`^(\d+)([hdw])$`)

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
	restoreRunCmd.Flags().StringVar(&restoreTime, "time", "", "Restore from specific time (e.g., '2024-04-26T10:30:00Z', '1d', '2h')")
	restoreRunCmd.Flags().BoolVar(&restoreJSON, "json", false, "JSON output")
}

var (
	restoreResumeRetryFailed bool
	restoreResumeSession     string
	restoreResumeAuto        bool
)

// restoreResumeCmd represents the restore resume command
var restoreResumeCmd = &cobra.Command{
	Use:   "resume <job-id>",
	Short: "Resume interrupted restore",
	Long:  `Resume an interrupted restore operation from the last saved state.`,
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

		if restorePassword == "" {
			return fmt.Errorf("password required for restore (use --password flag)")
		}

		// Load config directory
		configDir, err := config.ConfigDir()
		if err != nil {
			return fmt.Errorf("failed to get config directory: %w", err)
		}

		// Find restore state
		var state *restore.RestoreState
		var stateDir string

		if restoreResumeAuto || restoreResumeSession == "" {
			// Find latest state
			state, stateDir, err = restore.FindLatestRestoreState(jobID, configDir)
			if err != nil {
				return err
			}
			fmt.Printf("📋 Found interrupted restore from %s\n", state.SessionID)
		} else {
			// Load specific session
			state, stateDir, err = restore.FindRestoreStateBySession(jobID, restoreResumeSession, configDir)
			if err != nil {
				return err
			}
			fmt.Printf("📋 Restoring from session %s\n", restoreResumeSession)
		}

		fmt.Printf("   Status: %s\n", state.Status)
		fmt.Printf("   Progress: %d/%d files, %s restored\n", 
			state.ProcessedFiles, state.TotalFiles, formatBytes(state.BytesRestored))
		fmt.Println()

		// Determine which files to restore
		var filesToRestore []*restore.FileState
		if restoreResumeRetryFailed {
			filesToRestore = state.GetFailedFiles()
			if len(filesToRestore) == 0 {
				fmt.Println("✅ No failed files to retry")
				return nil
			}
			fmt.Printf("🔄 Retrying %d failed files...\n", len(filesToRestore))
		} else {
			filesToRestore = state.GetIncompleteFiles()
			if len(filesToRestore) == 0 {
				fmt.Println("✅ Restore already complete")
				return nil
			}
			fmt.Printf("🔄 Resuming restore: %d files remaining...\n", len(filesToRestore))
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

		// Open index DB
		indexDir := filepath.Join(configDir, "index", jobID)
		indexDB, err := index.OpenIndexDB(indexDir)
		if err != nil {
			return fmt.Errorf("failed to open index: %w", err)
		}
		defer indexDB.Close()

		// Create restore engine
		engine := restore.NewRestoreEngine(cfg, s3Client, indexDB, cryptoEngine, jobID)

		// Restore options
		opts := &models.RestoreOptions{
			DestinationPath: state.DestinationPath,
			DryRun:          false,
			Overwrite:       false,
			Concurrency:     8,
		}

		// Create progress tracker
		tracker := restore.NewProgressTracker(state.TotalFiles)

		// Resume restore
		fmt.Println("🔄 Resuming restore...")
		fmt.Println()

		result, err := engine.ResumeRestore(state, stateDir, opts, tracker, filesToRestore)
		if err != nil {
			return fmt.Errorf("resume failed: %w", err)
		}

		// Display results
		fmt.Println()
		fmt.Println("✅ Restore completed")
		fmt.Printf("   Files restored: %d\n", result.FilesRestored)
		fmt.Printf("   Files skipped: %d\n", result.FilesSkipped)
		if result.FilesFailed > 0 {
			fmt.Printf("   Files failed: %d\n", result.FilesFailed)
		}
		fmt.Printf("   Bytes restored: %s\n", formatBytes(result.BytesRestored))
		fmt.Printf("   Duration: %s\n", formatDuration(time.Duration(result.Duration)*time.Second))

		return nil
	},
}

// restoreStatusCmd represents the restore status command
var restoreStatusCmd = &cobra.Command{
	Use:   "status <job-id>",
	Short: "Show restore state",
	Long:  `Show the status of restore operations for a job.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jobID := args[0]

		// Load config directory
		configDir, err := config.ConfigDir()
		if err != nil {
			return err
		}

		// Find latest state
		state, stateDir, err := restore.FindLatestRestoreState(jobID, configDir)
		if err != nil {
			return err
		}

		fmt.Printf("Restore State for job: %s\n", jobID)
		fmt.Printf("Session: %s\n", state.SessionID)
		fmt.Printf("Started: %s\n", state.StartTime.Format("2006-01-02 15:04:05"))
		fmt.Printf("Last Update: %s\n", state.LastUpdateTime.Format("2006-01-02 15:04:05"))
		fmt.Printf("Status: %s\n", state.Status)
		fmt.Println()
		fmt.Printf("Progress: %d/%d files (%d%%)\n", 
			state.ProcessedFiles, state.TotalFiles,
			state.ProcessedFiles*100/state.TotalFiles)
		fmt.Printf("Bytes Restored: %s / %s\n", 
			formatBytes(state.BytesRestored), formatBytes(state.BytesTotal))
		fmt.Printf("Speed: %.2f MB/s\n", state.SpeedMBps)
		fmt.Println()

		// Show file status summary
		completed := 0
		failed := 0
		pending := 0
		skipped := 0
		for _, file := range state.Files {
			switch file.Status {
			case "completed":
				completed++
			case "failed":
				failed++
			case "pending", "downloading":
				pending++
			case "skipped":
				skipped++
			}
		}

		fmt.Println("File Status:")
		fmt.Printf("  ✅ Completed: %d\n", completed)
		fmt.Printf("  ⏭️  Skipped: %d\n", skipped)
		fmt.Printf("  ⏳ Pending: %d\n", pending)
		fmt.Printf("  ❌ Failed: %d\n", failed)
		fmt.Println()

		// Show failed files if any
		if failed > 0 {
			fmt.Println("Failed Files:")
			count := 0
			for path, file := range state.Files {
				if file.Status == "failed" {
					fmt.Printf("  - %s: %s\n", path, file.LastError)
					count++
					if count >= 10 {
						fmt.Printf("  ... and %d more\n", failed-10)
						break
					}
				}
			}
			fmt.Println()
			fmt.Println("Resume with: ds3backup restore resume", jobID, "--retry-failed --password=xxx")
		}

		fmt.Printf("State Directory: %s\n", stateDir)

		return nil
	},
}

func init() {
	restoreCmd.AddCommand(restoreResumeCmd)
	restoreCmd.AddCommand(restoreStatusCmd)

	restoreResumeCmd.Flags().BoolVar(&restoreResumeRetryFailed, "retry-failed", false, "Retry only failed files")
	restoreResumeCmd.Flags().StringVar(&restoreResumeSession, "session", "", "Specific restore session (timestamp)")
	restoreResumeCmd.Flags().BoolVar(&restoreResumeAuto, "auto", true, "Auto-resume latest interrupted restore")
	restoreResumeCmd.Flags().StringVar(&restorePassword, "password", "", "Encryption password (required for resume)")
}
