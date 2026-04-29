package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/esignoretti/ds3backup/pkg/models"
)

// Config represents the main configuration
type Config struct {
	Version         int              `json:"version"`
	S3              S3Config         `json:"s3"`
	Encryption      EncryptionConfig `json:"encryption"`
	ObjectLock      ObjectLockConfig `json:"objectLock"`
	MasterPassword  string           `json:"masterPassword,omitempty"`
	Jobs            []models.BackupJob `json:"jobs"`
	Daemon          DaemonConfig     `json:"daemon"`
	ConfigPath      string           `json:"-"`
}

// DaemonConfig holds daemon mode settings
type DaemonConfig struct {
	Enabled           bool  `json:"enabled"`
	SchedulerInterval int   `json:"schedulerInterval"`
	APIPort           int   `json:"apiPort"`
	AutoStart         bool  `json:"autoStart"`
}

// S3Config holds S3 connection details
type S3Config struct {
	Endpoint  string `json:"endpoint"`
	Bucket    string `json:"bucket"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
	Region    string `json:"region"`
	UseSSL    bool   `json:"useSSL"`
}

// EncryptionConfig holds encryption parameters
type EncryptionConfig struct {
	Algorithm   string `json:"algorithm"` // "AES-256-GCM"
	KeyDerivation string `json:"keyDerivation"` // "argon2id"
	Salt        []byte `json:"salt"`
	Iterations  uint32 `json:"iterations"`
	Memory      uint32 `json:"memory"` // in KB
	Parallelism uint8  `json:"parallelism"`
	KeyLength   uint32 `json:"keyLength"`
}

// ObjectLockConfig holds Object Lock settings
type ObjectLockConfig struct {
	Enabled            bool   `json:"enabled"`
	Mode               string `json:"mode"` // "GOVERNANCE", "COMPLIANCE", or "NONE"
	DefaultRetentionDays int   `json:"defaultRetentionDays"`
}

// DefaultConfig creates a new config with defaults
func DefaultConfig() *Config {
	return &Config{
		Version: 1,
		Encryption: EncryptionConfig{
			Algorithm:     "AES-256-GCM",
			KeyDerivation: "argon2id",
			Iterations:    3,
			Memory:        64 * 1024,
			Parallelism:   4,
			KeyLength:     32,
		},
		ObjectLock: ObjectLockConfig{
			Enabled:            true,
			Mode:               "GOVERNANCE",
			DefaultRetentionDays: 30,
		},
		Daemon: DaemonConfig{
			Enabled:           false,
			SchedulerInterval: 60,
			APIPort:           8099,
			AutoStart:         false,
		},
		Jobs: []models.BackupJob{},
	}
}

// LoadConfig loads configuration from file
func LoadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	config.ConfigPath = configPath
	return &config, nil
}

// SaveConfig saves configuration to file
func (c *Config) SaveConfig() error {
	if c.ConfigPath == "" {
		return fmt.Errorf("config path not set")
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(c.ConfigPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Add trailing newline
	data = append(data, byte('\n'))


	// Write to temp file first, then rename (atomic)
	tmpPath := c.ConfigPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	if err := os.Rename(tmpPath, c.ConfigPath); err != nil {
		return fmt.Errorf("failed to rename config file: %w", err)
	}

	return nil
}

// AddJob adds a new backup job
func (c *Config) AddJob(job models.BackupJob) {
	job.ID = generateJobID()
	job.CreatedAt = time.Now()
	job.Enabled = true
	c.Jobs = append(c.Jobs, job)
}

// GetJob returns a job by ID
func (c *Config) GetJob(jobID string) *models.BackupJob {
	for i := range c.Jobs {
		if c.Jobs[i].ID == jobID {
			return &c.Jobs[i]
		}
	}
	return nil
}

// GetJobByName returns a job by name
func (c *Config) GetJobByName(name string) *models.BackupJob {
	for i := range c.Jobs {
		if c.Jobs[i].Name == name {
			return &c.Jobs[i]
		}
	}
	return nil
}

// RemoveJob removes a job by ID
func (c *Config) RemoveJob(jobID string) bool {
	for i := range c.Jobs {
		if c.Jobs[i].ID == jobID {
			c.Jobs = append(c.Jobs[:i], c.Jobs[i+1:]...)
			return true
		}
	}
	return false
}

// ConfigDir returns the default configuration directory
func ConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".ds3backup"), nil
}

// DefaultConfigPath returns the default config file path
func DefaultConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// generateJobID creates a unique job ID
func generateJobID() string {
	return fmt.Sprintf("job_%d", time.Now().UnixNano())
}

// labelName is the launchd label used for the macOS LaunchAgent plist.
const labelName = "com.ds3backup.daemon"

// launchAgentPlistPath returns the path to the LaunchAgent plist file.
func launchAgentPlistPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(homeDir, "Library", "LaunchAgents", labelName+".plist"), nil
}

// systemdServicePath returns the path to the systemd user service unit file.
func systemdServicePath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(homeDir, ".config", "systemd", "user", "ds3backup-daemon.service"), nil
}

// installLaunchAgent writes a macOS LaunchAgent plist and loads it into launchd.
func (d *DaemonConfig) installLaunchAgent(binaryPath string) error {
	plistPath, err := launchAgentPlistPath()
	if err != nil {
		return err
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>daemon</string>
        <string>run</string>
        <string>--no-tray</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>%s/.ds3backup/daemon.stdout.log</string>
    <key>StandardErrorPath</key>
    <string>%s/.ds3backup/daemon.stderr.log</string>
</dict>
</plist>
`, labelName, binaryPath, homeDir, homeDir)

	dir := filepath.Dir(plistPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create LaunchAgents directory: %w", err)
	}

	if err := os.WriteFile(plistPath, []byte(plistContent), 0644); err != nil {
		return fmt.Errorf("failed to write plist: %w", err)
	}

	// Ask launchctl to load the plist. If launchctl is unavailable (container, CI),
	// the plist file is still written and will be loaded at next login.
	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		return fmt.Errorf("failed to load launch agent (plist written at %s): %w", plistPath, err)
	}

	return nil
}

// removeLaunchAgent unloads the LaunchAgent from launchd and deletes the plist file.
func (d *DaemonConfig) removeLaunchAgent() error {
	plistPath, err := launchAgentPlistPath()
	if err != nil {
		return err
	}

	// Unload from launchctl (best-effort)
	_ = exec.Command("launchctl", "unload", plistPath).Run()

	// Remove the plist file
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove plist: %w", err)
	}

	return nil
}

// installSystemdService writes a systemd user service unit file.
// This is a best-effort stub implementation — systemctl --user daemon-reload
// must be run by the user.
func (d *DaemonConfig) installSystemdService(binaryPath string) error {
	svcPath, err := systemdServicePath()
	if err != nil {
		return err
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	unitContent := fmt.Sprintf(`[Unit]
Description=DS3 Backup Daemon
After=network.target

[Service]
Type=simple
ExecStart=%s daemon run --no-tray
Restart=on-failure
StandardOutput=append:%s/.ds3backup/daemon.stdout.log
StandardError=append:%s/.ds3backup/daemon.stderr.log

[Install]
WantedBy=default.target
`, binaryPath, homeDir, homeDir)

	dir := filepath.Dir(svcPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create systemd user directory: %w", err)
	}

	if err := os.WriteFile(svcPath, []byte(unitContent), 0644); err != nil {
		return fmt.Errorf("failed to write systemd unit file: %w", err)
	}

	// Best-effort: run daemon-reload so systemd picks up the new unit
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()

	return nil
}

// removeSystemdService deletes the systemd user service unit file.
func (d *DaemonConfig) removeSystemdService() error {
	svcPath, err := systemdServicePath()
	if err != nil {
		return err
	}

	if err := os.Remove(svcPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove systemd unit file: %w", err)
	}

	return nil
}

// InstallAutoStart creates a LaunchAgent plist on macOS or a systemd service file
// on Linux, enabling the daemon to start automatically on user login.
func (d *DaemonConfig) InstallAutoStart(binaryPath string) error {
	switch runtime.GOOS {
	case "darwin":
		return d.installLaunchAgent(binaryPath)
	case "linux":
		return d.installSystemdService(binaryPath)
	default:
		return fmt.Errorf("auto-start not supported on %s", runtime.GOOS)
	}
}

// RemoveAutoStart removes the auto-start configuration (plist or systemd service).
func (d *DaemonConfig) RemoveAutoStart() error {
	switch runtime.GOOS {
	case "darwin":
		return d.removeLaunchAgent()
	case "linux":
		return d.removeSystemdService()
	default:
		return nil
	}
}

// IsAutoStartInstalled returns true if the auto-start configuration file exists.
func (d *DaemonConfig) IsAutoStartInstalled() bool {
	switch runtime.GOOS {
	case "darwin":
		path, err := launchAgentPlistPath()
		if err != nil {
			return false
		}
		_, err = os.Stat(path)
		return err == nil
	case "linux":
		path, err := systemdServicePath()
		if err != nil {
			return false
		}
		_, err = os.Stat(path)
		return err == nil
	default:
		return false
	}
}
