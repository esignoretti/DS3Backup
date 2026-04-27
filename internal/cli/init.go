package cli

import (
	"crypto/rand"
	"fmt"
	"log"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/esignoretti/ds3backup/internal/config"
	"github.com/esignoretti/ds3backup/internal/crypto"
	"github.com/esignoretti/ds3backup/internal/recovery"
	"github.com/esignoretti/ds3backup/internal/s3client"
	"golang.org/x/term"
)

var (
	masterPassword string
	rebuild        bool
)

// initCmd represents the init command
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize DS3 Backup with S3 target",
	Long: `Initialize DS3 Backup by configuring S3 connection and master password.

This command will:
  1. Validate S3 credentials and bucket access
  2. Check Object Lock support
  3. Set up master password (optional, for encrypting job configs)
  4. Create initial configuration file

With --rebuild flag:
  Scans S3 bucket and rebuilds local configuration from backup metadata.
  Skips jobs marked as RETIRED.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if rebuild {
			return runRebuild(cmd)
		}
		
		fmt.Println("Initializing DS3 Backup...")

		// Validate parameters
		if endpoint == "" || bucket == "" || accessKey == "" || secretKey == "" {
			return fmt.Errorf("missing required parameters:\n  --endpoint, --bucket, --access-key, --secret-key are required")
		}

		if objectLock != "GOVERNANCE" && objectLock != "COMPLIANCE" && objectLock != "NONE" {
			return fmt.Errorf("invalid object lock mode: %s (must be GOVERNANCE, COMPLIANCE, or NONE)", objectLock)
		}

		// Get master password if not provided via flag
		if masterPassword == "" {
			fmt.Print("Enter master password (leave empty for no encryption): ")
			bytePassword, err := term.ReadPassword(int(syscall.Stdin))
			if err != nil {
				return fmt.Errorf("failed to read password: %w", err)
			}
			masterPassword = string(bytePassword)
			fmt.Println()
			
			if masterPassword != "" {
				fmt.Print("Confirm master password: ")
				byteConfirm, err := term.ReadPassword(int(syscall.Stdin))
				if err != nil {
					return fmt.Errorf("failed to read confirmation: %w", err)
				}
				fmt.Println()
				
				if masterPassword != string(byteConfirm) {
					return fmt.Errorf("passwords do not match")
				}
			}
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

		// Create master password checksum
		var masterPasswordChecksum string
		if masterPassword != "" {
			fmt.Println("Setting up master password encryption...")
			masterPasswordChecksum, err = crypto.CreateMasterPasswordChecksum(masterPassword)
			if err != nil {
				return fmt.Errorf("failed to setup master password: %w", err)
			}
			fmt.Println("✓ Master password configured")
		}

		// Generate encryption salt
		salt := make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			return fmt.Errorf("failed to generate salt: %w", err)
		}

		// Create config
		cfg := config.DefaultConfig()
		cfg.Encryption.Salt = salt
		cfg.S3 = s3Cfg
		cfg.ObjectLock.Mode = objectLock
		cfg.ObjectLock.DefaultRetentionDays = retentionDays
		cfg.MasterPassword = masterPasswordChecksum

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
		if masterPassword != "" {
			fmt.Printf("  Master Password: enabled (encrypts job configurations)\n")
		} else {
			fmt.Printf("  Master Password: disabled (job configs stored unencrypted)\n")
		}
		fmt.Printf("\nNext steps:\n")
		fmt.Printf("  1. Create a backup job: ds3backup job add --name=\"MyBackup\" --path=~/Documents --password=xxx\n")
		fmt.Printf("  2. Run backup: ds3backup backup run <job-id>\n")
		fmt.Printf("  3. Recover from disaster: ds3backup init --rebuild\n")

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
	initCmd.Flags().StringVar(&objectLock, "object-lock-mode", "NONE", "Object lock mode (GOVERNANCE, COMPLIANCE, or NONE)")
	initCmd.Flags().IntVar(&retentionDays, "retention-days", 30, "Default retention period in days")
	initCmd.Flags().StringVar(&masterPassword, "master-password", "", "Master password for encrypting job configs")
	initCmd.Flags().BoolVar(&rebuild, "rebuild", false, "Rebuild configuration from S3 backup metadata")

	initCmd.MarkFlagRequired("endpoint")
	initCmd.MarkFlagRequired("bucket")
	initCmd.MarkFlagRequired("access-key")
	initCmd.MarkFlagRequired("secret-key")
}

// runRebuild executes the rebuild process
func runRebuild(cmd *cobra.Command) error {
	fmt.Println("Rebuilding configuration from S3...")
	fmt.Println()

	// Validate parameters
	if endpoint == "" || bucket == "" || accessKey == "" || secretKey == "" {
		return fmt.Errorf("missing required parameters:\n  --endpoint, --bucket, --access-key, --secret-key are required")
	}

	// Get master password
	if masterPassword == "" {
		fmt.Print("Enter master password (leave empty if not set): ")
		bytePassword, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}
		masterPassword = string(bytePassword)
		fmt.Println()
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

	// Create S3 client
	fmt.Println("Connecting to S3...")
	client, err := s3client.NewClient(s3Cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to S3: %w", err)
	}

	// Run rebuild
	ctx := cmd.Context()
	if err := recovery.RunRebuild(ctx, client, masterPassword); err != nil {
		return fmt.Errorf("rebuild failed: %w", err)
	}

	return nil
}
