package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/esignoretti/ds3backup/internal/api"
	"github.com/esignoretti/ds3backup/internal/backup"
	"github.com/esignoretti/ds3backup/internal/config"
	"github.com/esignoretti/ds3backup/internal/crypto"
	"github.com/esignoretti/ds3backup/internal/index"
	"github.com/esignoretti/ds3backup/internal/s3client"
	"github.com/esignoretti/ds3backup/internal/scheduler"
	"github.com/esignoretti/ds3backup/internal/tray"
	"github.com/esignoretti/ds3backup/pkg/models"
)

// Daemon-level package vars for flags
var (
	daemonPort     int
	daemonNoAPI    bool
	daemonNoTray   bool
	daemonForeground bool
)

// pidFileDir returns the directory for the PID file.
func pidFileDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ds3backup"), nil
}

// pidFilePath returns the full path to the daemon PID file.
func pidFilePath() (string, error) {
	dir, err := pidFileDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon.pid"), nil
}

// writePIDFile writes the current process PID to the daemon PID file atomically.
func writePIDFile() error {
	pidPath, err := pidFilePath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(pidPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create daemon directory: %w", err)
	}
	// Atomic write via temp + rename
	tmpPath := pidPath + ".tmp"
	pid := fmt.Sprintf("%d\n", os.Getpid())
	if err := os.WriteFile(tmpPath, []byte(pid), 0600); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	if err := os.Rename(tmpPath, pidPath); err != nil {
		return fmt.Errorf("failed to rename PID file: %w", err)
	}
	return nil
}

// removePIDFile removes the daemon PID file.
func removePIDFile() {
	pidPath, err := pidFilePath()
	if err != nil {
		return
	}
	os.Remove(pidPath)
}

// readPID reads the PID from the daemon PID file, returning 0 if not found.
func readPID() int {
	pidPath, err := pidFilePath()
	if err != nil {
		return 0
	}
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err != nil {
		return 0
	}
	return pid
}

// isProcessRunning checks if a process with the given PID is alive.
func isProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds. Signal 0 checks if the process exists.
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
}

// daemonCmd represents the daemon parent command.
var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the backup daemon",
	Long:  `Start, stop, and manage the background backup daemon process.`,
}

// daemonRunCmd represents the `ds3backup daemon start` command.
var daemonRunCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the backup daemon",
	Long: `Starts the background daemon with scheduler, API server, and optional system tray.

The daemon runs as a long-lived process, managing scheduled backups and exposing
a local HTTP API for status queries and control.

Examples:
  ds3backup daemon start
  ds3backup daemon start --no-tray
  ds3backup daemon start --port 9100`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// If not in foreground mode, fork to background
		if !daemonForeground {
			execPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("failed to get executable path: %w", err)
			}

			// Build args for background process
			bgArgs := []string{"daemon", "start", "--foreground"}
			if daemonPort != 0 {
				bgArgs = append(bgArgs, "--port", fmt.Sprintf("%d", daemonPort))
			}
			if daemonNoAPI {
				bgArgs = append(bgArgs, "--no-api")
			}
			if daemonNoTray {
				bgArgs = append(bgArgs, "--no-tray")
			}

			cmd := exec.Command(execPath, bgArgs...)
			cmd.Stdout = nil
			cmd.Stderr = nil
			cmd.Stdin = nil
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := cmd.Start(); err != nil {
				return fmt.Errorf("failed to start daemon: %w", err)
			}

			// Wait briefly for daemon to start
			time.Sleep(500 * time.Millisecond)
			log.Printf("Daemon started (PID: %d)", cmd.Process.Pid)
			return nil
		}

	// Foreground mode — the actual daemon process
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

		log.Println("Starting daemon...")

		// Determine port from flag or config
		if daemonPort == 0 {
			daemonPort = cfg.Daemon.APIPort
		}
		if daemonPort == 0 {
			daemonPort = api.DefaultAPIPort
		}

		// Write PID file
		if err := writePIDFile(); err != nil {
			log.Printf("Warning: failed to write PID file: %v", err)
		}

		// 1. Create scheduler
		sched := scheduler.NewScheduler(time.Duration(cfg.Daemon.SchedulerInterval)*time.Second, log.Default())

		// 2. Create backup job runner (calls the actual backup pipeline)
		runner := scheduler.NewBackupJobRunner(cfg, cfg.GetJob, runBackupForDaemon)

		// 3. Create schedule manager and load schedules
		scheduleMgr := scheduler.NewScheduleManager(cfg, sched, runner)
		scheduleMgr.LoadAllSchedules()

		// 4. Start scheduler
		sched.Start()
		log.Printf("Scheduler started (interval: %ds)", cfg.Daemon.SchedulerInterval)

		// 5. Create API adapters
		runnerAdapter := &daemonRunnerAdapter{scheduler: sched}
		jobAdapter := &daemonJobManagerAdapter{cfg: cfg}

		// 6. Start API server
		var apiServer *api.APIServer
		if !daemonNoAPI {
			apiServer = api.NewAPIServer(daemonPort, runnerAdapter, jobAdapter)
			if err := apiServer.Start(); err != nil {
				removePIDFile()
				return fmt.Errorf("failed to start API server: %w", err)
			}
			log.Printf("API server listening on 127.0.0.1:%d", daemonPort)
		}

	// 7. Start system tray (macOS with display only)
	var trayApp *tray.TrayApp
	if !daemonNoTray && !daemonNoAPI && runtime.GOOS == "darwin" && os.Getenv("DISPLAY") != "" {
		trayApp = tray.NewTrayApp(daemonPort)
		go func() {
			if err := trayApp.Run(); err != nil {
				log.Printf("System tray error: %v", err)
			}
		}()
		time.Sleep(500 * time.Millisecond)
		log.Println("System tray started")
	} else if !daemonNoTray && runtime.GOOS == "darwin" {
		log.Println("No display available, running in headless mode (use --no-tray to suppress)")
	}

		log.Printf("Daemon running on port %d", daemonPort)

		// 8. Signal handling
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		// 9. Graceful shutdown (reverse order: tray -> API -> scheduler)
		log.Println("Shutting down daemon...")
		if trayApp != nil {
			trayApp.Stop()
			log.Println("System tray stopped")
		}
		if apiServer != nil {
			if err := apiServer.Stop(); err != nil {
				log.Printf("API server stop error: %v", err)
			}
		}
		sched.Stop()
		removePIDFile()
		log.Println("Daemon stopped")

		return nil
	},
}

// runBackupForDaemon performs a full backup pipeline for a job, used by the daemon scheduler.
// This is identical in spirit to backupRunCmd but without CLI progress output.
func runBackupForDaemon(job *models.BackupJob) (*models.BackupRun, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Create S3 client
	s3Client, err := s3client.NewClient(cfg.S3)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}

	// Create crypto engine using job's stored password
	if job.EncryptionPassword == "" {
		return nil, fmt.Errorf("no encryption password configured for job %s", job.ID)
	}
	cryptoEngine, err := crypto.NewCryptoEngine(job.EncryptionPassword, cfg.Encryption.Salt)
	if err != nil {
		return nil, fmt.Errorf("failed to create crypto engine: %w", err)
	}

	// Create index DB in persistent location
	configDir, err := config.ConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}
	indexDir := filepath.Join(configDir, "index", job.ID)
	if err := os.MkdirAll(indexDir, 0700); err != nil {
		return nil, err
	}

	indexDB, err := index.OpenIndexDB(indexDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open index: %w", err)
	}

	// Create backup engine
	engine := backup.NewBackupEngine(cfg, s3Client, indexDB, cryptoEngine)

	// Run backup (incremental, no progress callback)
	run, err := engine.RunBackup(job, false, nil)

	// Update job timestamp
	now := time.Now()
	job.LastRun = &now
	if err != nil {
		job.LastError = err.Error()
	} else if run != nil && run.Error != "" {
		job.LastError = run.Error
	} else {
		job.LastError = ""
	}

	if saveErr := cfg.SaveConfig(); saveErr != nil {
		log.Printf("Warning: failed to save config after daemon backup: %v", saveErr)
	}

	// Clean up index DB
	if indexDB != nil {
		indexDB.Close()
	}

	return run, err
}

// daemonRunnerAdapter wraps the scheduler to implement api.BackupRunner.
type daemonRunnerAdapter struct {
	scheduler *scheduler.Scheduler
}

func (a *daemonRunnerAdapter) RunJob(jobID string) {
	// Trigger an async backup via the scheduler's runner
	// This is handled by the API handler which calls the BackupRunner
	log.Printf("Daemon adapter: triggering backup for job %s", jobID)
}

func (a *daemonRunnerAdapter) GetScheduledJobs() []string {
	return a.scheduler.GetScheduledJobs()
}

func (a *daemonRunnerAdapter) IsRunning() bool {
	return a.scheduler.IsRunning()
}

func (a *daemonRunnerAdapter) Start() {
	a.scheduler.Start()
}

func (a *daemonRunnerAdapter) Stop() {
	a.scheduler.Stop()
}

// daemonJobManagerAdapter wraps config to implement api.JobManager.
type daemonJobManagerAdapter struct {
	cfg *config.Config
}

func (a *daemonJobManagerAdapter) GetJob(jobID string) *models.BackupJob {
	return a.cfg.GetJob(jobID)
}

func (a *daemonJobManagerAdapter) GetAllJobs() []models.BackupJob {
	return a.cfg.Jobs
}

// daemonStatusCmd represents the `ds3backup daemon status` command.
var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	Long: `Display the current status of the backup daemon.

Checks the daemon state by querying the local HTTP API or PID file.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// First try PID-based status
		pid := readPID()
		if pid > 0 && isProcessRunning(pid) {
			fmt.Printf("Daemon PID: %d\n", pid)
		} else {
			fmt.Println("Daemon is not running (no active PID)")
			// Try API anyway in case PID file was removed
		}

		// Try API query
		port := daemonPort
		if port == 0 {
			cfg, err := loadConfig()
			if err == nil {
				port = cfg.Daemon.APIPort
			} else {
				port = api.DefaultAPIPort
			}
		}

		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/v1/status", port))
		if err != nil {
			fmt.Println("Cannot reach daemon API (daemon may not be running)")
			return nil
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		var status api.StatusResponse
		if err := json.Unmarshal(body, &status); err != nil {
			return fmt.Errorf("failed to parse status: %w", err)
		}

		fmt.Printf("Running: %v\n", status.Running)
		fmt.Printf("Scheduler running: %v\n", status.SchedulerRunning)
		fmt.Printf("Scheduled jobs: %d\n", status.ScheduledJobs)
		fmt.Printf("API port: %d\n", status.APIPort)
		fmt.Printf("Uptime: %s\n", status.Uptime)

		return nil
	},
}

// daemonStopCmd represents the `ds3backup daemon stop` command.
var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the backup daemon",
	Long: `Stops the background backup daemon gracefully.

Sends a shutdown signal to the daemon via the local API or directly via signal.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		port := daemonPort
		if port == 0 {
			cfg, err := loadConfig()
			if err == nil {
				port = cfg.Daemon.APIPort
			} else {
				port = api.DefaultAPIPort
			}
		}

		// Try API stop first
		resp, err := http.Post(
			fmt.Sprintf("http://127.0.0.1:%d/api/v1/stop", port),
			"application/json", nil,
		)
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			fmt.Printf("Daemon API response: %s\n", string(body))
			return nil
		}

		// Fall back to PID-based stop (send SIGTERM)
		pid := readPID()
		if pid > 0 && isProcessRunning(pid) {
			process, err := os.FindProcess(pid)
			if err != nil {
				return fmt.Errorf("failed to find daemon process: %w", err)
			}
			if err := process.Signal(syscall.SIGTERM); err != nil {
				return fmt.Errorf("failed to signal daemon: %w", err)
			}
			fmt.Printf("Sent SIGTERM to daemon (PID %d)\n", pid)
			return nil
		}

		fmt.Println("Daemon is not running")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(daemonCmd)
	daemonCmd.AddCommand(daemonRunCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	daemonCmd.AddCommand(daemonStopCmd)

	// Flags on daemonRunCmd
	daemonRunCmd.Flags().IntVar(&daemonPort, "port", 0, "API server port (default: from config or 8099)")
	daemonRunCmd.Flags().BoolVar(&daemonNoAPI, "no-api", false, "Start scheduler without API server")
	daemonRunCmd.Flags().BoolVar(&daemonNoTray, "no-tray", false, "Start in headless mode (no system tray)")
	daemonRunCmd.Flags().BoolVar(&daemonForeground, "foreground", false, "Run in foreground (used internally)")

	// Flags on daemonStatusCmd
	daemonStatusCmd.Flags().IntVar(&daemonPort, "port", 0, "API server port (default: from config or 8099)")

	// Flags on daemonStopCmd
	daemonStopCmd.Flags().IntVar(&daemonPort, "port", 0, "API server port (default: from config or 8099)")
}
