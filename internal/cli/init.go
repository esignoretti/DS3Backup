package cli

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"github.com/esignoretti/ds3backup/internal/config"
	"github.com/esignoretti/ds3backup/internal/s3client"
)

// initCmd represents the init command
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize DS3 Backup with S3 target",
	Long: `Initialize DS3 Backup by configuring S3 connection.

This command will:
  1. Validate S3 credentials and bucket access
  2. Check Object Lock support
  3. Create initial configuration file

Note: Encryption password is specified during backup/restore operations, not during initialization.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Initializing DS3 Backup...")

		// Validate parameters
		if endpoint == "" || bucket == "" || accessKey == "" || secretKey == "" {
			return fmt.Errorf("missing required parameters:\n  --endpoint, --bucket, --access-key, --secret-key are required")
		}

		if objectLock != "GOVERNANCE" && objectLock != "COMPLIANCE" && objectLock != "NONE" {
			return fmt.Errorf("invalid object lock mode: %s (must be GOVERNANCE, COMPLIANCE, or NONE)", objectLock)
		}

		// Create S3 config
		s3Cfg := config.S3Config{
			Endpoint:  endpoint,
			Bucket:    bucket,
			AccessKey: accessKey,
			SecretKey: secretKey,
			Region:    region,
			UseSSL:    true,
		}

		// Test S3 connection
		fmt.Println("Testing S3 connection...")
		client, err := s3client.NewClient(s3Cfg)
		if err != nil {
			return fmt.Errorf("failed to connect to S3: %w", err)
		}
		fmt.Println("✓ S3 connection successful")

		// Check Object Lock support
		objectLockSupported, err := client.CheckObjectLockSupport()
		if err != nil {
			log.Printf("Warning: Could not check Object Lock support: %v", err)
		} else if !objectLockSupported && objectLock != "NONE" {
			log.Printf("Warning: Bucket does not support Object Lock, but object lock mode is set to %s", objectLock)
		} else if objectLockSupported {
			fmt.Println("✓ Object Lock supported")
		}

		// Create config
		cfg := config.DefaultConfig()
		cfg.S3 = s3Cfg
		cfg.ObjectLock.Mode = objectLock
		cfg.ObjectLock.DefaultRetentionDays = retentionDays

		// Save config
		cfgPath, err := config.DefaultConfigPath()
		if err != nil {
			return fmt.Errorf("failed to get config path: %w", err)
		}
		cfg.ConfigPath = cfgPath

		if err := saveConfig(cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Printf("\n✓ DS3 Backup initialized successfully!\n")
		fmt.Printf("  Config file: %s\n", cfgPath)
		fmt.Printf("  S3 Endpoint: %s\n", endpoint)
		fmt.Printf("  Bucket: %s\n", bucket)
		fmt.Printf("  Object Lock: %s mode\n", objectLock)
		fmt.Printf("  Retention: %d days\n", retentionDays)
		fmt.Printf("\nNext steps:\n")
		fmt.Printf("  1. Create a backup job: ds3backup job add --name=\"MyBackup\" --path=~/Documents\n")
		fmt.Printf("  2. Run backup: ds3backup backup run <job-id>\n")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().StringVar(&endpoint, "endpoint", "", "S3 endpoint (e.g., s3.cubbit.eu)")
	initCmd.Flags().StringVar(&bucket, "bucket", "", "S3 bucket name")
	initCmd.Flags().StringVar(&accessKey, "access-key", "", "S3 access key")
	initCmd.Flags().StringVar(&secretKey, "secret-key", "", "S3 secret key")
	initCmd.Flags().StringVar(&region, "region", "us-east-1", "S3 region")
	initCmd.Flags().StringVar(&objectLock, "object-lock-mode", "GOVERNANCE", "Object lock mode (GOVERNANCE, COMPLIANCE, or NONE)")
	initCmd.Flags().IntVar(&retentionDays, "retention-days", 30, "Default retention period in days")

	initCmd.MarkFlagRequired("endpoint")
	initCmd.MarkFlagRequired("bucket")
	initCmd.MarkFlagRequired("access-key")
	initCmd.MarkFlagRequired("secret-key")
}
