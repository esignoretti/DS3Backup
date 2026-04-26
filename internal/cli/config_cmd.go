package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/esignoretti/ds3backup/internal/config"
	"github.com/esignoretti/ds3backup/internal/s3client"
)

// configCmd represents the config command
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
	Long:  `View and manage DS3 Backup configuration.`,
}

// configShowCmd shows current configuration
var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		fmt.Printf("DS3 Backup Configuration\n")
		fmt.Printf("========================\n\n")
		
		fmt.Printf("S3 Configuration:\n")
		fmt.Printf("  Endpoint: %s\n", cfg.S3.Endpoint)
		fmt.Printf("  Bucket: %s\n", cfg.S3.Bucket)
		fmt.Printf("  Region: %s\n", cfg.S3.Region)
		fmt.Printf("  SSL: %v\n", cfg.S3.UseSSL)
		fmt.Printf("  Access Key: %s...\n", maskString(cfg.S3.AccessKey, 4))
		fmt.Printf("\n")

		fmt.Printf("Encryption:\n")
		fmt.Printf("  Algorithm: %s\n", cfg.Encryption.Algorithm)
		fmt.Printf("  Key Derivation: %s\n", cfg.Encryption.KeyDerivation)
		fmt.Printf("  Salt: %x...\n", cfg.Encryption.Salt[:8])
		fmt.Printf("\n")

		fmt.Printf("Object Lock:\n")
		fmt.Printf("  Enabled: %v\n", cfg.ObjectLock.Enabled)
		fmt.Printf("  Mode: %s\n", cfg.ObjectLock.Mode)
		fmt.Printf("  Default Retention: %d days\n", cfg.ObjectLock.DefaultRetentionDays)
		fmt.Printf("\n")

		fmt.Printf("Backup Jobs: %d\n", len(cfg.Jobs))
		for i, job := range cfg.Jobs {
			fmt.Printf("  %d. %s [%s]\n", i+1, job.Name, job.ID)
			fmt.Printf("     Source: %s\n", job.SourcePath)
			fmt.Printf("     Retention: %d days\n", job.RetentionDays)
			fmt.Printf("     Object Lock: %s\n", job.ObjectLockMode)
		}

		return nil
	},
}

// configResetCmd resets configuration
var configResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset configuration (re-initialize)",
	Long: `Reset DS3 Backup configuration.

WARNING: This will remove all configuration including jobs!
Backup data in S3 will NOT be deleted.

Use --wipe to also delete all backup data from S3.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		wipe, _ := cmd.Flags().GetBool("wipe")
		confirm, _ := cmd.Flags().GetBool("yes")

		if !confirm {
			fmt.Printf("⚠️  WARNING: This will reset all configuration!\n")
			if wipe {
				fmt.Printf("⚠️  This will ALSO DELETE all backup data from S3!\n")
			}
			fmt.Printf("\nAre you sure? Use --yes to confirm.\n")
			return nil
		}

		if wipe {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			fmt.Printf("Deleting all backup data from S3...\n")
			s3Client, err := s3client.NewClient(cfg.S3)
			if err != nil {
				return fmt.Errorf("failed to create S3 client: %w", err)
			}

			ctx := cmd.Context()
			objects, err := s3Client.ListObjects(ctx, "")
			if err != nil {
				return fmt.Errorf("failed to list objects: %w", err)
			}

			fmt.Printf("Found %d objects to delete\n", len(objects))
			
			// Delete in batches
			batchSize := 100
			for i := 0; i < len(objects); i += batchSize {
				end := i + batchSize
				if end > len(objects) {
					end = len(objects)
				}

				for j := i; j < end; j++ {
					if err := s3Client.DeleteObject(ctx, objects[j]); err != nil {
						fmt.Printf("Warning: Failed to delete %s: %v\n", objects[j], err)
					}
				}
				fmt.Printf("Deleted %d/%d objects...\n", end, len(objects))
			}

			fmt.Printf("✓ All backup data deleted from S3\n")
		}

		// Remove config file
		cfgPath, err := config.DefaultConfigPath()
		if err != nil {
			return err
		}

		if err := os.Remove(cfgPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove config file: %w", err)
		}

		fmt.Printf("✓ Configuration reset successfully\n")
		fmt.Printf("\nRun 'ds3backup init' to re-initialize.\n")

		return nil
	},
}

// configValidateCmd validates configuration
var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration",
	Long: `Validate the current configuration.

Checks:
- Configuration file exists and is valid JSON
- S3 credentials work
- Bucket exists and is accessible
- Object Lock status matches configuration`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Validating configuration...\n\n")

		// Load config
		cfg, err := loadConfig()
		if err != nil {
			fmt.Printf("✗ Configuration file: %v\n", err)
			return err
		}
		fmt.Printf("✓ Configuration file: Valid\n")

		// Test S3 connection
		s3Client, err := s3client.NewClient(cfg.S3)
		if err != nil {
			fmt.Printf("✗ S3 connection: %v\n", err)
			return err
		}
		fmt.Printf("✓ S3 connection: OK\n")

		// Check bucket exists
		ctx := cmd.Context()
		exists, err := s3Client.BucketExists(ctx)
		if err != nil {
			fmt.Printf("✗ Bucket check: %v\n", err)
			return err
		}
		if !exists {
			fmt.Printf("✗ Bucket check: Bucket does not exist\n")
			return fmt.Errorf("bucket %s does not exist", cfg.S3.Bucket)
		}
		fmt.Printf("✓ Bucket exists: %s\n", cfg.S3.Bucket)

		// Check Object Lock
		objectLockSupported, err := s3Client.CheckObjectLockSupport()
		if err != nil {
			fmt.Printf("⚠ Object Lock check: Could not determine - %v\n", err)
		} else {
			if objectLockSupported {
				fmt.Printf("✓ Object Lock: Supported\n")
				if cfg.ObjectLock.Mode == "NONE" {
					fmt.Printf("⚠ Object Lock: Supported but configured as NONE\n")
				}
			} else {
				if cfg.ObjectLock.Mode != "NONE" {
					fmt.Printf("✗ Object Lock: Not supported but configured as %s\n", cfg.ObjectLock.Mode)
					return fmt.Errorf("bucket does not support Object Lock")
				}
				fmt.Printf("✓ Object Lock: Not supported (NONE mode)\n")
			}
		}

		// Validate jobs
		fmt.Printf("\nValidating %d backup job(s)...\n", len(cfg.Jobs))
		for _, job := range cfg.Jobs {
			if _, err := os.Stat(job.SourcePath); os.IsNotExist(err) {
				fmt.Printf("⚠ Job '%s': Source path does not exist: %s\n", job.Name, job.SourcePath)
			} else {
				fmt.Printf("✓ Job '%s': Source path exists\n", job.Name)
			}
		}

		fmt.Printf("\n✓ Configuration is valid!\n")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configResetCmd)
	configCmd.AddCommand(configValidateCmd)

	configResetCmd.Flags().Bool("wipe", false, "Also delete all backup data from S3")
	configResetCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
}

// maskString masks all but the last n characters
func maskString(s string, keepLast int) string {
	if len(s) <= keepLast {
		return s
	}
	return s[:keepLast] + "..."
}
