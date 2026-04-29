package cli

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/esignoretti/ds3backup/internal/config"
)

var (
	cfgFile       string
	endpoint      string
	bucket        string
	accessKey     string
	secretKey     string
	password      string
	region        string
	objectLock    string
	retentionDays int
	verbose       bool
	logFile       *os.File
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
// preRunSetup checks flags and sets up logging before command execution
func preRunSetup() {
	// Check if verbose flag is set in args
	for _, arg := range os.Args[1:] {
		if arg == "-v" || arg == "--verbose" {
			verbose = true
			break
		}
	}
	
	// Setup logging to file
	logDir := filepath.Join(mustHomeDir(), ".ds3backup")
	if err := os.MkdirAll(logDir, 0755); err == nil {
		var err error
		logPath := filepath.Join(logDir, "ds3backup.log")
		logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			var output io.Writer = logFile
			if verbose {
				output = io.MultiWriter(logFile, os.Stderr)
			}
			log.SetOutput(output)
			log.SetFlags(log.Ldate | log.Ltime)
			log.Printf("=== DS3Backup Started (verbose=%v) ===", verbose)
		}
	}
}


func Execute() {
	preRunSetup()
	
	defer func() {
		if logFile != nil {
			logFile.Close()
		}
	}()
	
	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		if verbose {
			log.Printf("ERROR: %v", err)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.ds3backup/config.json)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	
	// Log every command execution
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		// Build command string
		cmdPath := cmd.CommandPath()
		if len(args) > 0 {
			cmdPath += " " + fmt.Sprint(args)
		}
		log.Printf("Command: %s", cmdPath)
	}
}

// mustHomeDir returns the home directory or exits
func mustHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error finding home directory:", err)
		os.Exit(1)
	}
	return home
}

// initConfig reads in config file
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag
		return
	}

	// Search config in home directory
	cfgFile = filepath.Join(mustHomeDir(), ".ds3backup", "config.json")
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

// IsVerbose returns true if verbose mode is enabled
func IsVerbose() bool {
	return verbose
}

// GetLogFile returns the log file
func GetLogFile() *os.File {
	return logFile
}
