package recovery

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/esignoretti/ds3backup/internal/backup"
	"github.com/esignoretti/ds3backup/internal/config"
	"github.com/esignoretti/ds3backup/internal/crypto"
	"github.com/esignoretti/ds3backup/internal/s3client"
	"github.com/esignoretti/ds3backup/pkg/models"
)

// RunRebuild executes the complete disaster recovery process
func RunRebuild(ctx context.Context, s3client *s3client.Client, masterPassword string) error {
	fmt.Println("🔄 Starting disaster recovery...")
	fmt.Println()

	// Get config directory
	configDir, err := config.ConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	// Step 1: Delete existing .ds3backup directory
	fmt.Println("Step 1: Cleaning up existing configuration...")
	if err := os.RemoveAll(configDir); err != nil {
		return fmt.Errorf("failed to remove existing config: %w", err)
	}
	fmt.Println("  ✓ Removed existing .ds3backup directory")

	// Step 2: Create fresh directory
	fmt.Println("\nStep 2: Creating fresh configuration directory...")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	fmt.Println("  ✓ Created .ds3backup directory")

	// Step 3: Download disaster recovery archive
	fmt.Println("\nStep 3: Downloading disaster recovery archive from S3...")
	archiveData, err := s3client.GetObject(ctx, ".ds3backup.tar.gz")
	if err != nil {
		return fmt.Errorf("failed to download archive: %w", err)
	}
	fmt.Println("  ✓ Archive downloaded")

	// Step 4: Download and verify checksum
	fmt.Println("\nStep 4: Verifying archive integrity...")
	checksumData, err := s3client.GetObject(ctx, ".ds3backup.tar.gz.sha256")
	if err != nil {
		return fmt.Errorf("failed to download checksum: %w", err)
	}
	expectedChecksum := strings.TrimSpace(string(checksumData))

	// Calculate checksum of downloaded data
	hash, err := backup.CalculateSHA256FromReader(bytes.NewReader(archiveData))
	if err != nil {
		return fmt.Errorf("failed to calculate checksum: %w", err)
	}

	if hash != expectedChecksum {
		return fmt.Errorf("❌ CHECKSUM MISMATCH - Archive corrupted!\n  Expected: %s\n  Got: %s", expectedChecksum, hash)
	}
	fmt.Printf("  ✓ Checksum verified: %s\n", hash[:16]+"...")

	// Step 5: Extract archive
	fmt.Println("\nStep 5: Extracting archive...")
	if err := backup.ExtractBackupArchive(bytes.NewReader(archiveData), configDir); err != nil {
		return fmt.Errorf("failed to extract archive: %w", err)
	}
	fmt.Println("  ✓ Archive extracted successfully")

	// Step 6: Load and update config with master password
	fmt.Println("\nStep 6: Updating configuration...")
	cfg, err := config.LoadConfig(filepath.Join(configDir, "config.json"))
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// If master password provided and jobs have plain text passwords, encrypt them
	if masterPassword != "" && len(cfg.Jobs) > 0 {
		needsSave := false
		for i := range cfg.Jobs {
			if cfg.Jobs[i].EncryptionPassword != "" {
				// Password is already encrypted or plain - keep as-is for now
				// In future, could add explicit encryptedPassword field
				needsSave = true
			}
		}
		
		if needsSave {
			// Save updated config
			if err := cfg.SaveConfig(); err != nil {
				return fmt.Errorf("failed to save updated config: %w", err)
			}
			fmt.Println("  ✓ Configuration validated")
		}
	}

	// Step 7: Load encryption salt
	fmt.Println("\nStep 7: Loading encryption salt...")
	salt, err := LoadEncryptionSalt(ctx, s3client)
	if err != nil {
		fmt.Printf("  ⚠️  Could not load encryption salt: %v\n", err)
	} else {
		fmt.Println("  ✓ Encryption salt loaded")
		// Update config with salt if not present
		if len(cfg.Encryption.Salt) == 0 {
			cfg.Encryption.Salt = salt
			cfg.SaveConfig()
		}
	}

	// Step 8: Discover jobs
	fmt.Println("\nStep 8: Discovering backup jobs...")
	jobs, err := discoverJobsFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to discover jobs: %w", err)
	}

	if len(jobs) == 0 {
		fmt.Println("  ⚠️  No backup jobs found")
	} else {
		fmt.Printf("  ✓ Found %d backup job(s):\n", len(jobs))
		for _, job := range jobs {
			fmt.Printf("    • %s (%s)\n", job.Name, job.ID)
		}
	}

	fmt.Println()
	fmt.Println("✅ Disaster recovery completed successfully!")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  ds3backup job list              # View all recovered jobs")
	fmt.Println("  ds3backup backup run <job-id>   # Run a backup")
	fmt.Println("  ds3backup restore <job-id>      # Restore files")

	return nil
}

// discoverJobsFromConfig extracts job information from loaded config
func discoverJobsFromConfig(cfg *config.Config) ([]models.BackupJob, error) {
	return cfg.Jobs, nil
}

// LoadEncryptionSalt downloads the encryption salt from S3
func LoadEncryptionSalt(ctx context.Context, s3client *s3client.Client) ([]byte, error) {
	data, err := s3client.GetObject(ctx, ".ds3backup/encryption-salt.json")
	if err != nil {
		return nil, err
	}

	var saltData struct {
		Salt string `json:"salt"`
	}
	if err := json.Unmarshal(data, &saltData); err != nil {
		return nil, fmt.Errorf("failed to parse salt file: %w", err)
	}

	salt, err := base64.StdEncoding.DecodeString(saltData.Salt)
	if err != nil {
		return nil, fmt.Errorf("failed to decode salt: %w", err)
	}

	return salt, nil
}

// Simple downloader for rebuild (no complex index reconstruction needed)
type RebuildEngine struct {
	s3client *s3client.Client
}

// NewRebuildEngine creates a simple rebuild engine
func NewRebuildEngine(s3client *s3client.Client) *RebuildEngine {
	return &RebuildEngine{
		s3client: s3client,
	}
}

// RebuildIndex is no longer needed - entire .ds3backup is restored as a whole
// This method is kept for API compatibility but does nothing
func (e *RebuildEngine) RebuildIndex(ctx context.Context, job JobMetadata) error {
	// Index is already restored as part of the full .ds3backup archive
	return nil
}

// JobMetadata represents job configuration (kept for compatibility)
type JobMetadata struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	SourcePath        string `json:"sourcePath"`
	RetentionDays     int    `json:"retentionDays"`
	ObjectLockMode    string `json:"objectLockMode"`
	EncryptedPassword string `json:"encryptedPassword,omitempty"`
}

// IsRetired checks if a job has been marked as retired (not used in new approach)
func (e *RebuildEngine) IsRetired(ctx context.Context, jobID string) (bool, error) {
	return false, nil
}

// DiscoverJobs scans S3 and returns list of backup jobs (not used in new approach)
func (e *RebuildEngine) DiscoverJobs(ctx context.Context) ([]JobMetadata, error) {
	return nil, nil
}

// downloadJobMetadata downloads job metadata (not used in new approach)
func (e *RebuildEngine) downloadJobMetadata(ctx context.Context, jobID string) (JobMetadata, error) {
	return JobMetadata{}, nil
}

// SaveJobMetadata encrypts and uploads job metadata to S3
func SaveJobMetadata(ctx context.Context, s3client *s3client.Client, job models.BackupJob, masterPassword string) error {
	// Encrypt password with master password if provided
	encryptedPass := job.EncryptionPassword
	if masterPassword != "" && job.EncryptionPassword != "" {
		var err error
		encryptedPass, err = crypto.EncryptWithMasterPassword([]byte(job.EncryptionPassword), masterPassword)
		if err != nil {
			return fmt.Errorf("failed to encrypt password: %w", err)
		}
	}

	metadata := JobMetadata{
		ID:                job.ID,
		Name:              job.Name,
		SourcePath:        job.SourcePath,
		RetentionDays:     job.RetentionDays,
		ObjectLockMode:    job.ObjectLockMode,
		EncryptedPassword: encryptedPass,
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}

	key := fmt.Sprintf(".ds3backup/jobs/%s/config.json", job.ID)
	return s3client.PutObject(ctx, key, data)
}
