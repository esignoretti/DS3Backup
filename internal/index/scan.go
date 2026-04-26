package index

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/esignoretti/ds3backup/pkg/models"
	"golang.org/x/crypto/blake2b"
)

// ScanResult holds the result of directory scanning
type ScanResult struct {
	Files       []models.FileEntry
	TotalFiles  int
	TotalSize   int64
	ScanTime    time.Time
}

// ScanDirectory scans a directory and returns file entries
func (db *IndexDB) ScanDirectory(rootPath, jobID string) (*ScanResult, error) {
	result := &ScanResult{
		Files:    make([]models.FileEntry, 0),
		ScanTime: time.Now(),
	}

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Skip hidden files and common exclusions
		if shouldSkip(info.Name()) {
			return nil
		}

		// Calculate hash
		hash, err := calculateHash(path)
		if err != nil {
			return err
		}

		entry := models.FileEntry{
			Path:        path,
			Size:        info.Size(),
			ModTime:     info.ModTime(),
			Hash:        hash[:],
			JobID:       jobID,
			BackupTime:  time.Now(),
			OriginalSize: info.Size(),
		}

		result.Files = append(result.Files, entry)
		result.TotalFiles++
		result.TotalSize += info.Size()

		return nil
	})

	return result, err
}

// GetChangedFiles returns files that have changed since a given time
func (db *IndexDB) GetChangedFiles(jobID string, since time.Time) ([]models.FileEntry, error) {
	var changed []models.FileEntry

	err := db.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte(fmt.Sprintf("file:%s:", jobID))

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				entry := &models.FileEntry{}
				if err := json.Unmarshal(val, entry); err != nil {
					return err
				}

				// Check if file has changed since last backup
				if entry.ModTime.After(since) {
					changed = append(changed, *entry)
				}

				return nil
			})
			if err != nil {
				return err
			}
		}

		return nil
	})

	return changed, err
}

// GetUniqueFilesToBackup filters out duplicate files (by hash)
func (db *IndexDB) GetUniqueFilesToBackup(entries []models.FileEntry) []models.FileEntry {
	seen := make(map[string]*models.FileEntry)
	unique := make([]models.FileEntry, 0, len(entries))

	for i := range entries {
		entry := &entries[i]
		hashKey := string(entry.Hash)

		if existing, exists := seen[hashKey]; exists {
			// Duplicate found - reference existing S3 object
			entry.S3Key = existing.S3Key
			entry.IsInBatch = existing.IsInBatch
			entry.BatchID = existing.BatchID
			entry.IsDuplicate = true
		} else {
			seen[hashKey] = entry
			unique = append(unique, *entry)
		}
	}

	return unique
}

// shouldSkip determines if a file should be skipped during scanning
func shouldSkip(filename string) bool {
	// Skip hidden files
	if filename[0] == '.' {
		return true
	}

	// Skip common temporary/cache files
	skipPatterns := []string{
		".DS_Store",
		"Thumbs.db",
		"desktop.ini",
		".gitignore",
		".gitattributes",
	}

	for _, pattern := range skipPatterns {
		if filename == pattern {
			return true
		}
	}

	return false
}

// calculateHash calculates BLAKE2b-256 hash of a file
func calculateHash(path string) ([32]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return [32]byte{}, err
	}
	defer file.Close()

	hasher, err := blake2b.New256(nil)
	if err != nil {
		return [32]byte{}, err
	}

	if _, err := io.Copy(hasher, file); err != nil {
		return [32]byte{}, err
	}

	var hash [32]byte
	copy(hash[:], hasher.Sum(nil))
	return hash, nil
}

// QuickHash calculates a quick hash for change detection (mtime + size)
func QuickHash(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}

	hash := md5.Sum([]byte(fmt.Sprintf("%s:%d", info.ModTime().String(), info.Size())))
	return fmt.Sprintf("%x", hash), nil
}
