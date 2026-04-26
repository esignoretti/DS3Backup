package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/esignoretti/ds3backup/internal/config"
	"github.com/esignoretti/ds3backup/internal/crypto"
	"github.com/esignoretti/ds3backup/internal/index"
	"github.com/esignoretti/ds3backup/internal/restore"
	"github.com/esignoretti/ds3backup/internal/s3client"
	"github.com/esignoretti/ds3backup/pkg/models"
)

var (
	restoreTo     string
	restoreDryRun bool
	restoreVerify bool
	restoreOverwrite bool
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
			Concurrency:     8,
		}

		var result *models.RestoreResult
		var verifyResult *models.VerifyResult
		var dryRunResult *models.DryRunResult

		if restoreVerify {
			// Verification mode
			fmt.Println("🔍 Verifying backup integrity...")
			verifyResult, err = engine.Verify(jobID)
			if err != nil {
				return fmt.Errorf("verification failed: %w", err)
			}

			if jsonOutput {
				output, _ := json.MarshalIndent(verifyResult, "", "  ")
				fmt.Println(string(output))
			} else {
				if verifyResult.Failed == 0 {
					fmt.Printf("✅ All %d files verified successfully\n", verifyResult.Verified)
					fmt.Printf("   Total size: %d bytes\n", verifyResult.Total)
					fmt.Println("   All hashes match")
					fmt.Println("   All files decryptable")
				} else {
					fmt.Printf("❌ %d files failed verification\n", verifyResult.Failed)
					fmt.Println()
					fmt.Println("Failed files:")
					for _, errMsg := range verifyResult.Errors {
						fmt.Printf("   - %s\n", errMsg)
					}
					fmt.Printf("\nVerified: %d/%d files (%.1f%%)\n",
						verifyResult.Verified, verifyResult.Total,
						float64(verifyResult.Verified)*100/float64(verifyResult.Total))
				}
			}
			return nil
		}

		if restoreDryRun {
			// Dry-run mode
			fmt.Println("📋 Restore Preview (Dry Run)")
			fmt.Println()
			dryRunResult, err = engine.DryRun(jobID, opts)
			if err != nil {
				return fmt.Errorf("dry-run failed: %w", err)
			}

			if jsonOutput {
				output, _ := json.MarshalIndent(dryRunResult, "", "  ")
				fmt.Println(string(output))
			} else {
				fmt.Printf("Files to restore: %d\n", dryRunResult.FilesToRestore)
				fmt.Printf("Files to skip (already exist): %d\n", dryRunResult.FilesSkipped)
				fmt.Printf("Total size: %s\n", formatBytes(dryRunResult.TotalSize))
				fmt.Printf("Estimated time: ~%d minutes (at 8.5 MB/s)\n", dryRunResult.TotalSize/1024/1024/8/60+1)
				fmt.Println()

				if len(dryRunResult.SampleFiles) > 0 {
					fmt.Println("Sample files:")
					for _, f := range dryRunResult.SampleFiles {
						fmt.Printf("  - %s (%s)\n", f.Path, formatBytes(f.Size))
					}
					if dryRunResult.MoreFiles > 0 {
						fmt.Printf("  ... (%d more files)\n", dryRunResult.MoreFiles)
					}
				}
			}
			return nil
		}

		// Full restore
		fmt.Println("🔄 Restoring files...")
		fmt.Println()

		result, err = engine.Restore(jobID, opts)
		if err != nil {
			return fmt.Errorf("restore failed: %w", err)
		}

		// Display results
		if jsonOutput {
			output, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(output))
		} else {
			fmt.Println()
			fmt.Println("✅ Restore completed")
			fmt.Printf("   Files restored: %d\n", result.FilesRestored)
			fmt.Printf("   Files skipped: %d\n", result.FilesSkipped)
			if result.FilesFailed > 0 {
				fmt.Printf("   Files failed: %d\n", result.FilesFailed)
			}
			fmt.Printf("   Bytes restored: %s\n", formatBytes(result.BytesRestored))
			fmt.Printf("   Duration: %ds\n", result.Duration)

			if len(result.Warnings) > 0 {
				// Deduplicate and limit warnings
				warningCount := len(result.Warnings)
				fmt.Printf("   ⚠️  Metadata warnings: %d\n", warningCount)
				maxWarnings := 10
				shown := 0
				for i, w := range result.Warnings {
					if shown >= maxWarnings {
						fmt.Printf("      ... and %d more\n", warningCount-maxWarnings)
						break
					}
					fmt.Printf("      - %s\n", w)
					shown++
					if i < warningCount-1 && shown >= maxWarnings {
						fmt.Printf("      ... and %d more\n", warningCount-maxWarnings)
						break
					}
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
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(restoreCmd)
	restoreCmd.AddCommand(restoreRunCmd)

	restoreRunCmd.Flags().StringVar(&restoreTo, "to", "", "Restore to alternate location")
	restoreRunCmd.Flags().BoolVar(&restoreDryRun, "dry-run", false, "Preview restore without downloading")
	restoreRunCmd.Flags().BoolVar(&restoreVerify, "verify", false, "Verify backup integrity without restoring")
	restoreRunCmd.Flags().BoolVar(&restoreOverwrite, "overwrite", false, "Overwrite existing files")
	restoreRunCmd.Flags().BoolVar(&jsonOutput, "json", false, "JSON output")
	restoreRunCmd.Flags().StringVar(&password, "password", "", "Encryption password")
}
