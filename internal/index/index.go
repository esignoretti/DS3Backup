package index

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/esignoretti/ds3backup/pkg/models"
)

// IndexDB wraps BadgerDB for file indexing
type IndexDB struct {
	db *badger.DB
}

// OpenIndexDB opens or creates a BadgerDB index
func OpenIndexDB(path string) (*IndexDB, error) {
	opts := badger.DefaultOptions(path).
		WithCompression(2). // Snappy compression
		WithNumCompactors(2).
		WithMemTableSize(64 << 20). // 64 MB
		WithLogger(nil)             // Disable logging

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open BadgerDB: %w", err)
	}

	return &IndexDB{db: db}, nil
}

// Close closes the database
func (db *IndexDB) Close() error {
	return db.db.Close()
}

// Key formats:
// "file:<jobID>:<path>" -> FileEntry
// "hash:<jobID>:<hash>" -> FileEntry (for dedup)
// "run:<jobID>:<timestamp>" -> BackupRun

func fileKey(jobID, path string) []byte {
	return []byte(fmt.Sprintf("file:%s:%s", jobID, path))
}

func hashKey(jobID string, hash []byte) []byte {
	return []byte(fmt.Sprintf("hash:%s:%x", jobID, hash))
}

func runKey(jobID string, timestamp time.Time) []byte {
	return []byte(fmt.Sprintf("run:%s:%d", jobID, timestamp.UnixNano()))
}

// SaveEntry saves a file entry to the index
func (db *IndexDB) SaveEntry(entry *models.FileEntry) error {
	return db.db.Update(func(txn *badger.Txn) error {
		// Save by path
		fileData, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		if err := txn.Set(fileKey(entry.JobID, entry.Path), fileData); err != nil {
			return err
		}

		// Save by hash (for dedup)
		if err := txn.Set(hashKey(entry.JobID, entry.Hash), fileData); err != nil {
			return err
		}

		return nil
	})
}

// GetEntry retrieves a file entry by path
func (db *IndexDB) GetEntry(jobID, path string) (*models.FileEntry, error) {
	var entry *models.FileEntry

	err := db.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(fileKey(jobID, path))
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			entry = &models.FileEntry{}
			return json.Unmarshal(val, entry)
		})
	})

	if err == badger.ErrKeyNotFound {
		return nil, nil
	}

	return entry, err
}

// GetByHash retrieves a file entry by hash (for deduplication)
func (db *IndexDB) GetByHash(jobID string, hash []byte) (*models.FileEntry, error) {
	var entry *models.FileEntry

	err := db.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(hashKey(jobID, hash))
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			entry = &models.FileEntry{}
			return json.Unmarshal(val, entry)
		})
	})

	if err == badger.ErrKeyNotFound {
		return nil, nil
	}

	return entry, err
}

// SaveRun saves a backup run record
func (db *IndexDB) SaveRun(run *models.BackupRun) error {
	return db.db.Update(func(txn *badger.Txn) error {
		data, err := json.Marshal(run)
		if err != nil {
			return err
		}
		return txn.Set(runKey(run.JobID, run.RunTime), data)
	})
}

// GetLastRun gets the last backup run for a job
func (db *IndexDB) GetLastRun(jobID string) (*models.BackupRun, error) {
	var lastRun *models.BackupRun
	var found bool

	err := db.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte(fmt.Sprintf("run:%s:", jobID))
		opts.Reverse = false

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				run := &models.BackupRun{}
				if err := json.Unmarshal(val, run); err != nil {
					return err
				}
				lastRun = run
				found = true
				return nil
			})
			if err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	if !found {
		return nil, nil
	}

	return lastRun, nil
}

// GetBackupHistory gets backup history for a job
func (db *IndexDB) GetBackupHistory(jobID string, limit int) ([]*models.BackupRun, error) {
	var runs []*models.BackupRun

	err := db.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte(fmt.Sprintf("run:%s:", jobID))
		opts.Reverse = false

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				run := &models.BackupRun{}
				if err := json.Unmarshal(val, run); err != nil {
					return err
				}
				runs = append(runs, run)
				return nil
			})
			if err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	for i, j := 0, len(runs)-1; i < j; i, j = i+1, j-1 {
		runs[i], runs[j] = runs[j], runs[i]
	}

	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}

	return runs, nil
}

// Backup backs up the database to a directory
func (db *IndexDB) Backup(backupDir string) error {
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return err
	}

	// Use Badger's backup utility
	backupPath := filepath.Join(backupDir, "backup")
	backupFile, err := os.Create(backupPath)
	if err != nil {
		return err
	}
	defer backupFile.Close()

	_, err = db.db.Backup(backupFile, 0)
	return err
}

// Restore restores the database from a backup
func (db *IndexDB) Restore(backupDir string) error {
	backupPath := filepath.Join(backupDir, "backup")
	backupFile, err := os.Open(backupPath)
	if err != nil {
		return err
	}
	defer backupFile.Close()

	return db.db.Load(backupFile, 0)
}
