package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/esignoretti/ds3backup/internal/s3client"
)

// s3Cmd represents the S3 command
var s3Cmd = &cobra.Command{
	Use:   "s3",
	Short: "S3 management commands",
	Long:  `Manage S3 bucket settings and lifecycle policies.`,
}

// s3LifecycleSetCmd sets lifecycle policy
var s3LifecycleSetCmd = &cobra.Command{
	Use:   "lifecycle-set",
	Short: "Set S3 lifecycle policy for retention cleanup",
	Long: `Configure S3 lifecycle policy to automatically delete expired backups.

This creates a lifecycle rule that deletes objects older than the retention period.
Note: This only affects objects without Object Lock or with expired Object Lock.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		retentionDays, _ := cmd.Flags().GetInt("retention-days")
		
		fmt.Printf("Setting S3 lifecycle policy...\n")
		fmt.Printf("  Bucket: %s\n", cfg.S3.Bucket)
		fmt.Printf("  Retention: %d days\n", retentionDays)

		// Create S3 client
		s3Client, err := s3client.NewClient(cfg.S3)
		if err != nil {
			return fmt.Errorf("failed to create S3 client: %w", err)
		}

		ctx := cmd.Context()
		err = s3Client.SetLifecyclePolicy(ctx, retentionDays)
		if err != nil {
			return fmt.Errorf("failed to set lifecycle policy: %w", err)
		}

		fmt.Printf("✓ Lifecycle policy set successfully\n")
		fmt.Printf("  Objects older than %d days will be automatically deleted\n", retentionDays)
		
		return nil
	},
}

// s3LifecycleGetCmd gets lifecycle policy
var s3LifecycleGetCmd = &cobra.Command{
	Use:   "lifecycle-get",
	Short: "Get current S3 lifecycle policy",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		fmt.Printf("Fetching S3 lifecycle policy...\n")
		fmt.Printf("  Bucket: %s\n\n", cfg.S3.Bucket)

		// Create S3 client
		s3Client, err := s3client.NewClient(cfg.S3)
		if err != nil {
			return fmt.Errorf("failed to create S3 client: %w", err)
		}

		ctx := cmd.Context()
		policy, err := s3Client.GetLifecyclePolicy(ctx)
		if err != nil {
			return fmt.Errorf("failed to get lifecycle policy: %w", err)
		}

		if policy == "" {
			fmt.Println("No lifecycle policy configured")
		} else {
			fmt.Println("Current lifecycle policy:")
			fmt.Println(policy)
		}

		return nil
	},
}

// s3CheckObjectLockCmd checks Object Lock support
var s3CheckObjectLockCmd = &cobra.Command{
	Use:   "check-object-lock",
	Short: "Check if bucket supports Object Lock",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		fmt.Printf("Checking Object Lock support...\n")
		fmt.Printf("  Bucket: %s\n\n", cfg.S3.Bucket)

		// Create S3 client
		s3Client, err := s3client.NewClient(cfg.S3)
		if err != nil {
			return fmt.Errorf("failed to create S3 client: %w", err)
		}

		supported, err := s3Client.CheckObjectLockSupport()
		if err != nil {
			return fmt.Errorf("failed to check Object Lock: %w", err)
		}

		if supported {
			fmt.Println("✓ Object Lock is ENABLED on this bucket")
			fmt.Println("\nYou can use GOVERNANCE or COMPLIANCE mode for backups.")
		} else {
			fmt.Println("✗ Object Lock is NOT enabled on this bucket")
			fmt.Println("\nNote: Object Lock must be enabled when creating the bucket.")
			fmt.Println("      It cannot be enabled on existing buckets.")
			fmt.Println("\nYou can still use backups with --object-lock-mode=NONE")
		}

		return nil
	},
}

// s3ListCmd lists objects in bucket
var s3ListCmd = &cobra.Command{
	Use:   "ls [prefix]",
	Short: "List objects in S3 bucket",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		prefix := ""
		if len(args) > 0 {
			prefix = args[0]
		}

		humanReadable, _ := cmd.Flags().GetBool("human-readable")

		fmt.Printf("Listing objects in bucket: %s\n", cfg.S3.Bucket)
		if prefix != "" {
			fmt.Printf("Prefix: %s\n", prefix)
		}
		fmt.Println()

		// Create S3 client
		s3Client, err := s3client.NewClient(cfg.S3)
		if err != nil {
			return fmt.Errorf("failed to create S3 client: %w", err)
		}

		ctx := cmd.Context()
		objects, err := s3Client.ListObjects(ctx, prefix)
		if err != nil {
			return fmt.Errorf("failed to list objects: %w", err)
		}

		if len(objects) == 0 {
			fmt.Println("No objects found")
			return nil
		}

		fmt.Printf("Found %d objects:\n\n", len(objects))
		for _, obj := range objects {
			if humanReadable {
				fmt.Printf("  %s\n", obj)
			} else {
				fmt.Printf("  %s\n", obj)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(s3Cmd)
	s3Cmd.AddCommand(s3LifecycleSetCmd)
	s3Cmd.AddCommand(s3LifecycleGetCmd)
	s3Cmd.AddCommand(s3CheckObjectLockCmd)
	s3Cmd.AddCommand(s3ListCmd)

	s3LifecycleSetCmd.Flags().Int("retention-days", 30, "Retention period in days")
	s3ListCmd.Flags().BoolP("human-readable", "H", false, "Show file sizes in human-readable format")
}
