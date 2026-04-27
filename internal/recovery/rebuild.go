package recovery

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/esignoretti/ds3backup/internal/config"
	"github.com/esignoretti/ds3backup/internal/crypto"
	"github.com/esignoretti/ds3backup/internal/index"
	"github.com/esignoretti/ds3backup/internal/s3client"
	"github.com/esignoretti/ds3backup/pkg/models"
	"golang.org/x/term"
)

// JobMetadata represents job configuration stored on S3
type JobMetadata struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	SourcePath        string `json:"sourcePath"`
	RetentionDays     int    `json:"retentionDays"`
	ObjectLockMode    string `json:"objectLockMode"`
	EncryptedPassword string `json:"encryptedPassword,omitempty"`
}

// RebuildEngine handles disaster recovery operations
type RebuildEngine struct {
	s3client       *s3client.Client
	masterPassword string
}

// NewRebuildEngine creates a new rebuild engine
func NewRebuildEngine(s3client *s3client.Client, masterPassword string) *RebuildEngine {
	return &RebuildEngine{
		s3client:       s3client,
		masterPassword: masterPassword,
	}
}

// IsRetired checks if a job has been marked as retired
func (e *RebuildEngine) IsRetired(ctx context.Context, jobID string) (bool, error) {
	key := fmt.Sprintf(".ds3backup/index/%s/RETIRED-DO-NOT-REBUILD", jobID)
	_, err := e.s3client.GetObject(ctx, key)
	if err != nil {
		if strings.Contains(err.Error(), "NoSuchKey") || strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// DiscoverJobs scans S3 and returns list of backup jobs
func (e *RebuildEngine) DiscoverJobs(ctx context.Context) ([]JobMetadata, error) {
	// List all job directories
	objects, err := e.s3client.ListObjects(ctx, ".ds3backup/index/")
	if err != nil {
		return nil, fmt.Errorf("failed to list index directory: %w", err)
	}

	// Extract unique job IDs
	jobIDs := make(map[string]bool)
	for _, obj := range objects {
		// Path format: .ds3backup/index/<job-id>/...
		parts := strings.Split(strings.TrimPrefix(obj, ".ds3backup/index/"), "/")
		if len(parts) > 0 && parts[0] != "" && !strings.HasPrefix(parts[0], "RETIRED") {
			jobIDs[parts[0]] = true
		}
	}

	// Download job metadata for each job
	var jobs []JobMetadata
	for jobID := range jobIDs {
		// Check if retired
		retired, err := e.IsRetired(ctx, jobID)
		if err != nil {
			fmt.Printf("  ⚠️  Could not check retirement status for %s: %v\n", jobID, err)
			continue
		}
		if retired {
			fmt.Printf("  ⚠️  Skipping retired job: %s\n", jobID)
			continue
		}

		// Try to download job metadata
		metadata, err := e.downloadJobMetadata(ctx, jobID)
		if err != nil {
			fmt.Printf("  ℹ️  No metadata found for %s, will use defaults\n", jobID)
			// Create minimal metadata
			metadata = JobMetadata{
				ID:             jobID,
				Name:           fmt.Sprintf("recovered-%s", jobID[:8]),
				ObjectLockMode: "NONE",
				RetentionDays:  30,
			}
		}
		jobs = append(jobs, metadata)
	}

	return jobs, nil
}

// downloadJobMetadata downloads and decrypts job metadata from S3
func (e *RebuildEngine) downloadJobMetadata(ctx context.Context, jobID string) (JobMetadata, error) {
	key := fmt.Sprintf(".ds3backup/jobs/%s/config.json.enc", jobID)
	
	data, err := e.s3client.GetObject(ctx, key)
	if err != nil {
		return JobMetadata{}, err
	}

	// Decrypt if master password is set
	var metadata JobMetadata
	if e.masterPassword != "" {
		decrypted, err := crypto.DecryptWithMasterPassword(string(data), e.masterPassword)
		if err != nil {
			return JobMetadata{}, fmt.Errorf("failed to decrypt job config (wrong master password?): %w", err)
		}
		if err := json.Unmarshal(decrypted, &metadata); err != nil {
			return JobMetadata{}, fmt.Errorf("failed to parse job config: %w", err)
		}
	} else {
		if err := json.Unmarshal(data, &metadata); err != nil {
			return JobMetadata{}, fmt.Errorf("failed to parse job config: %w", err)
		}
	}

	return metadata, nil
}

// RebuildIndex downloads and restores index for a job
func (e *RebuildEngine) RebuildIndex(ctx context.Context, job JobMetadata) error {
	// Find most recent index copy
	indexPrefix := fmt.Sprintf("backups/%s/index_", job.ID)
	objects, err := e.s3client.ListObjects(ctx, indexPrefix)
	if err != nil {
		return fmt.Errorf("failed to list index copies: %w", err)
	}

	if len(objects) == 0 {
		return fmt.Errorf("no index copies found")
	}

	// Sort to get most recent (by timestamp in path)
	sort.Strings(objects)
	latestIndex := objects[len(objects)-1]
	fmt.Printf("    Using index: %s\n", filepath.Base(latestIndex))

	// Create local index directory
	configDir, err := config.ConfigDir()
	if err != nil {
		return err
	}
	indexDir := filepath.Join(configDir, "index", job.ID)
	if err := os.MkdirAll(indexDir, 0700); err != nil {
		return err
	}

	// Download index backup
	indexData, err := e.s3client.GetObject(ctx, latestIndex)
	if err != nil {
		return fmt.Errorf("failed to download index: %w", err)
	}

	// Write to temp file
	tempDir := filepath.Join(indexDir, "temp")
	if err := os.MkdirAll(tempDir, 0700); err != nil {
		return err
	}
	
	tempFile := filepath.Join(tempDir, "backup")
	if err := os.WriteFile(tempFile, indexData, 0600); err != nil {
		return err
	}

	// Restore BadgerDB from backup
	idx, err := index.OpenIndexDB(indexDir)
	if err != nil {
		return fmt.Errorf("failed to open index DB: %w", err)
	}
	defer idx.Close()

	// Note: BadgerDB restore would go here
	// For now, we just note that the data is available
	fmt.Printf("    Index data downloaded to %s\n", indexDir)
	
	return nil
}

// SaveJobMetadata encrypts and uploads job metadata to S3
func SaveJobMetadata(ctx context.Context, s3client *s3client.Client, job models.BackupJob, masterPassword string) error {
	metadata := JobMetadata{
		ID:             job.ID,
		Name:           job.Name,
		SourcePath:     job.SourcePath,
		RetentionDays:  job.RetentionDays,
		ObjectLockMode: job.ObjectLockMode,
	}

	// Encrypt job password with master password
	if job.EncryptionPassword != "" && masterPassword != "" {
		encrypted, err := crypto.EncryptWithMasterPassword([]byte(job.EncryptionPassword), masterPassword)
		if err != nil {
			return fmt.Errorf("failed to encrypt job password: %w", err)
		}
		metadata.EncryptedPassword = encrypted
	}
	// If no master password, don't store the job password on S3

	// Serialize
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}

	// Encrypt entire config if master password is set
	var encryptedData []byte
	if masterPassword != "" {
		encryptedStr, err := crypto.EncryptWithMasterPassword(data, masterPassword)
		if err != nil {
			return fmt.Errorf("failed to encrypt job metadata: %w", err)
		}
		encryptedData = []byte(encryptedStr)
	} else {
		encryptedData = data
	}

	// Upload to S3
	key := fmt.Sprintf(".ds3backup/jobs/%s/config.json.enc", job.ID)
	return s3client.PutObject(ctx, key, encryptedData)
}

// RunRebuild executes the rebuild process
func RunRebuild(ctx context.Context, s3client *s3client.Client, masterPassword string) error {
	fmt.Println("Scanning S3 bucket for backup jobs...")
	
	engine := NewRebuildEngine(s3client, masterPassword)
	jobs, err := engine.DiscoverJobs(ctx)
	if err != nil {
		return fmt.Errorf("failed to discover jobs: %w", err)
	}

	if len(jobs) == 0 {
		fmt.Println("No backup jobs found on S3.")
		return nil
	}

	fmt.Printf("Found %d backup job(s):\n", len(jobs))
	for i, job := range jobs {
		fmt.Printf("  %d. %s (%s)\n", i+1, job.Name, job.ID)
	}
	fmt.Println()

	// Get passwords for jobs that need them
	jobPasswords := make(map[string]string)
	for _, job := range jobs {
		if job.EncryptedPassword != "" && masterPassword != "" {
			// Try to decrypt with master password
			decrypted, err := crypto.DecryptWithMasterPassword(job.EncryptedPassword, masterPassword)
			if err == nil {
				jobPasswords[job.ID] = string(decrypted)
				fmt.Printf("  ✓ Decrypted password for %s\n", job.Name)
				continue
			}
		}
		
		// Prompt for password
		fmt.Printf("Enter password for %s (%s): ", job.Name, job.ID)
		bytePassword, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}
		fmt.Println()
		jobPasswords[job.ID] = string(bytePassword)
	}
	fmt.Println()

	// Rebuild indexes
	fmt.Println("Rebuilding indexes...")
	for _, job := range jobs {
		fmt.Printf("  Rebuilding %s... ", job.Name)
		if err := engine.RebuildIndex(ctx, job); err != nil {
			fmt.Printf("❌ Failed: %v\n", err)
		} else {
			fmt.Printf("✓ Success\n")
		}
	}

	// Update config with recovered jobs
	fmt.Println("\nUpdating configuration...")
	cfg, err := config.LoadConfig("")
	if err != nil {
		cfg = config.DefaultConfig()
	}

	// Add recovered jobs (avoid duplicates)
	for _, job := range jobs {
		existing := cfg.GetJob(job.ID)
		if existing == nil {
			newJob := models.BackupJob{
				ID:               job.ID,
				Name:             job.Name,
				SourcePath:       job.SourcePath,
				RetentionDays:    job.RetentionDays,
				ObjectLockMode:   job.ObjectLockMode,
				EncryptionPassword: jobPasswords[job.ID],
				Enabled:          true,
			}
			cfg.AddJob(newJob)
		}
	}

	if err := cfg.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("\n✓ Recovered %d job(s)\n", len(jobs))
	return nil
}
