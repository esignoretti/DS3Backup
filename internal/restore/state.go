package restore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/esignoretti/ds3backup/pkg/models"
)

// FileState tracks the state of a single file during restore
type FileState struct {
	Path         string    `json:"path"`
	Size         int64     `json:"size"`
	Hash         []byte    `json:"hash"`
	Status       string    `json:"status"` // "pending", "downloading", "completed", "failed"
	BytesWritten int64     `json:"bytesWritten"`
	S3Key        string    `json:"s3Key"`
	BatchID      string    `json:"batchId"`
	RetryCount   int       `json:"retryCount"`
	LastError    string    `json:"lastError,omitempty"`
	CompletedAt  time.Time `json:"completedAt,omitempty"`
}

// RestoreState tracks the state of a restore operation
type RestoreState struct {
	mu sync.Mutex

	JobID           string            `json:"jobId"`
	SessionID       string            `json:"sessionId"`
	StartTime       time.Time         `json:"startTime"`
	LastUpdateTime  time.Time         `json:"lastUpdateTime"`
	Status          string            `json:"status"` // "running", "paused", "completed", "failed"
	DestinationPath string            `json:"destinationPath"`
	
	// File tracking
	TotalFiles      int               `json:"totalFiles"`
	ProcessedFiles  int               `json:"processedFiles"`
	FailedFiles     int               `json:"failedFiles"`
	BytesTotal      int64             `json:"bytesTotal"`
	BytesRestored   int64             `json:"bytesRestored"`
	
	// Per-file state
	Files           map[string]*FileState `json:"files"`
	
	// Statistics
	SpeedMBps       float64           `json:"speedMbps"`
	LastSpeedUpdate time.Time         `json:"lastSpeedUpdate"`
}

// NewRestoreState creates a new restore state tracker
func NewRestoreState(jobID, sessionID string) *RestoreState {
	return &RestoreState{
		JobID:           jobID,
		SessionID:       sessionID,
		StartTime:       time.Now(),
		LastUpdateTime:  time.Now(),
		Status:          "running",
		Files:           make(map[string]*FileState),
		LastSpeedUpdate: time.Now(),
	}
}

// AddFile adds a file to the restore state
func (s *RestoreState) AddFile(entry *models.FileEntry, destPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.Files[destPath] = &FileState{
		Path:         destPath,
		Size:         entry.Size,
		Hash:         entry.Hash,
		Status:       "pending",
		BytesWritten: 0,
		S3Key:        entry.S3Key,
		BatchID:      entry.BatchID,
		RetryCount:   0,
	}
	
	s.TotalFiles++
	s.BytesTotal += entry.Size
}

// UpdateFileProgress updates the progress of a file being restored
func (s *RestoreState) UpdateFileProgress(path string, bytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if file, ok := s.Files[path]; ok {
		file.Status = "downloading"
		file.BytesWritten = bytes
		s.LastUpdateTime = time.Now()
	}
}

// MarkComplete marks a file as successfully restored
func (s *RestoreState) MarkComplete(path string, bytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if file, ok := s.Files[path]; ok {
		file.Status = "completed"
		file.BytesWritten = bytes
		file.CompletedAt = time.Now()
		s.ProcessedFiles++
		s.BytesRestored += bytes
		s.LastUpdateTime = time.Now()
		
		// Update speed
		elapsed := time.Since(s.LastSpeedUpdate).Seconds()
		if elapsed > 0 {
			s.SpeedMBps = float64(bytes) / elapsed / 1024 / 1024
		}
		s.LastSpeedUpdate = time.Now()
	}
}

// MarkFailed marks a file as failed
func (s *RestoreState) MarkFailed(path string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if file, ok := s.Files[path]; ok {
		file.Status = "failed"
		file.LastError = err.Error()
		file.RetryCount++
		s.FailedFiles++
		s.ProcessedFiles++
		s.LastUpdateTime = time.Now()
	}
}

// MarkSkipped marks a file as skipped (already exists)
func (s *RestoreState) MarkSkipped(path string, bytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if file, ok := s.Files[path]; ok {
		file.Status = "skipped"
		file.BytesWritten = bytes
		file.CompletedAt = time.Now()
		s.ProcessedFiles++
		s.LastUpdateTime = time.Now()
	}
}

// GetFailedFiles returns files that failed to restore
func (s *RestoreState) GetFailedFiles() []*FileState {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	var failed []*FileState
	for _, file := range s.Files {
		if file.Status == "failed" {
			failed = append(failed, file)
		}
	}
	return failed
}

// GetPendingFiles returns files that haven't been restored yet
func (s *RestoreState) GetPendingFiles() []*FileState {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	var pending []*FileState
	for _, file := range s.Files {
		if file.Status == "pending" || file.Status == "downloading" {
			pending = append(pending, file)
		}
	}
	return pending
}

// GetIncompleteFiles returns files that are not completed or skipped
func (s *RestoreState) GetIncompleteFiles() []*FileState {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	var incomplete []*FileState
	for _, file := range s.Files {
		if file.Status != "completed" && file.Status != "skipped" {
			incomplete = append(incomplete, file)
		}
	}
	return incomplete
}

// Progress returns current progress statistics
func (s *RestoreState) Progress() (percent int, processed int, total int, bytes int64, speed float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.TotalFiles > 0 {
		percent = s.ProcessedFiles * 100 / s.TotalFiles
	}
	return percent, s.ProcessedFiles, s.TotalFiles, s.BytesRestored, s.SpeedMBps
}

// Save saves the restore state to disk
func (s *RestoreState) Save(stateDir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// Create directory
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}
	
	// Marshal to JSON
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}
	
	// Atomic write (temp file + rename)
	tempFile := filepath.Join(stateDir, "restore-state.json.tmp")
	if err := os.WriteFile(tempFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}
	
	stateFile := filepath.Join(stateDir, "restore-state.json")
	if err := os.Rename(tempFile, stateFile); err != nil {
		return fmt.Errorf("failed to rename state file: %w", err)
	}
	
	return nil
}

// LoadRestoreState loads a restore state from disk
func LoadRestoreState(stateDir string) (*RestoreState, error) {
	stateFile := filepath.Join(stateDir, "restore-state.json")
	
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil, err
	}
	
	var state RestoreState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}
	
	return &state, nil
}

// FindLatestRestoreState finds the most recent restore state for a job
func FindLatestRestoreState(jobID string, configDir string) (*RestoreState, string, error) {
	stateDir := filepath.Join(configDir, "state", jobID)
	
	// Check if state directory exists
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		return nil, "", fmt.Errorf("no restore states found for job %s", jobID)
	}
	
	// List all session directories
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read state directory: %w", err)
	}
	
	// Find session directories (named by timestamp)
	var sessions []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			sessions = append(sessions, entry.Name())
		}
	}
	
	if len(sessions) == 0 {
		return nil, "", fmt.Errorf("no restore states found for job %s", jobID)
	}
	
	// Sort by name (timestamp format ensures chronological order)
	sort.Strings(sessions)
	
	// Get latest session
	latestSession := sessions[len(sessions)-1]
	latestDir := filepath.Join(stateDir, latestSession)
	
	// Load state
	state, err := LoadRestoreState(latestDir)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load latest state: %w", err)
	}
	
	return state, latestDir, nil
}

// FindRestoreStateBySession finds a specific restore state by session ID
func FindRestoreStateBySession(jobID, sessionID, configDir string) (*RestoreState, string, error) {
	stateDir := filepath.Join(configDir, "state", jobID, sessionID)
	
	state, err := LoadRestoreState(stateDir)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load state for session %s: %w", sessionID, err)
	}
	
	return state, stateDir, nil
}

// CleanupOldStates removes restore states older than the specified duration
// Keeps at least minStates most recent states
func CleanupOldStates(jobID, configDir string, olderThan time.Duration, minStates int) (int, error) {
	stateDir := filepath.Join(configDir, "state", jobID)
	
	// Check if state directory exists
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		return 0, nil
	}
	
	// List all session directories
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return 0, fmt.Errorf("failed to read state directory: %w", err)
	}
	
	// Find session directories with their timestamps
	type sessionInfo struct {
		name string
		time time.Time
	}
	
	var sessions []sessionInfo
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			// Parse session name as timestamp (format: 2024-04-26T10-30-00)
			sessionName := entry.Name()
			sessionTime, err := time.Parse("2006-01-02T15-04-05", sessionName)
			if err != nil {
				continue // Skip invalid session names
			}
			sessions = append(sessions, sessionInfo{name: sessionName, time: sessionTime})
		}
	}
	
	if len(sessions) <= minStates {
		return 0, nil // Nothing to clean up
	}
	
	// Sort by time (newest first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].time.After(sessions[j].time)
	})
	
	// Remove old sessions
	removed := 0
	cutoff := time.Now().Add(-olderThan)
	
	for i := minStates; i < len(sessions); i++ {
		if sessions[i].time.Before(cutoff) {
			sessionPath := filepath.Join(stateDir, sessions[i].name)
			if err := os.RemoveAll(sessionPath); err != nil {
				return removed, fmt.Errorf("failed to remove old state %s: %w", sessions[i].name, err)
			}
			removed++
		}
	}
	
	return removed, nil
}

// GetStateDirectory returns the state directory for a job and session
func GetStateDirectory(jobID, sessionID, configDir string) string {
	return filepath.Join(configDir, "state", jobID, sessionID)
}

// GetPartialDirectory returns the partial files directory
func GetPartialDirectory(stateDir string) string {
	return filepath.Join(stateDir, "partial")
}
