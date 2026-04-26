package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/esignoretti/ds3backup/pkg/models"
)

// Config represents the main configuration
type Config struct {
	Version      int              `json:"version"`
	S3           S3Config         `json:"s3"`
	Encryption   EncryptionConfig `json:"encryption"`
	ObjectLock   ObjectLockConfig `json:"objectLock"`
	Jobs         []models.BackupJob `json:"jobs"`
	ConfigPath   string           `json:"-"`
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
	Mode               string `json:"mode"` // "GOVERNANCE" or "COMPLIANCE"
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
			Memory:        64 * 1024, // 64 MB
			Parallelism:   4,
			KeyLength:     32,
		},
		ObjectLock: ObjectLockConfig{
			Enabled:            true,
			Mode:               "GOVERNANCE",
			DefaultRetentionDays: 30,
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
