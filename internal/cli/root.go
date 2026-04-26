package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/esignoretti/ds3backup/internal/config"
)

var (
	cfgFile     string
	endpoint    string
	bucket      string
	accessKey   string
	secretKey   string
	password    string
	region      string
	objectLock  string
	retentionDays int
)

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:     "ds3backup",
	Short:   "DS3 Backup - Secure S3 backup tool",
	Version: Version,
	Long: `DS3 Backup is a secure, S3-only backup tool with client-side encryption.

Features:
  - Client-side AES-256-GCM encryption
  - S3 Object Lock support (Governance/Compliance)
  - Incremental backups with deduplication
  - File batching for efficient S3 operations
  - BadgerDB-based local indexing`,
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.ds3backup/config.json)")
}

// initConfig reads in config file
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag
		return
	}

	// Find home directory
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error finding home directory:", err)
		os.Exit(1)
	}

	// Search config in home directory
	cfgFile = filepath.Join(home, ".ds3backup", "config.json")
}

// loadConfig loads the configuration file
func loadConfig() (*config.Config, error) {
	if _, err := os.Stat(cfgFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s\nRun 'ds3backup init' to initialize", cfgFile)
	}

	return config.LoadConfig(cfgFile)
}

// saveConfig saves the configuration file
func saveConfig(cfg *config.Config) error {
	return cfg.SaveConfig()
}
