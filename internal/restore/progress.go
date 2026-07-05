package restore

import (
	"sync"
	"time"
)

// ProgressTracker tracks restore progress
type ProgressTracker struct {
	mu            sync.Mutex
	totalFiles    int
	processed     int
	bytesRestored int64
	bytesSkipped  int64
	startTime     time.Time
	currentFile   string
	lastUpdate    time.Time
	speedMBps     float64
}

// NewProgressTracker creates a new progress tracker
func NewProgressTracker(totalFiles int) *ProgressTracker {
	return &ProgressTracker{
		totalFiles: totalFiles,
		startTime:  time.Now(),
		lastUpdate: time.Now(),
	}
}

// Update updates progress for a restored file
func (p *ProgressTracker) Update(file string, bytes int64, skipped bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.processed++
	if skipped {
		p.bytesSkipped += bytes
	} else {
		p.bytesRestored += bytes
	}
	p.currentFile = file

	now := time.Now()
	elapsed := now.Sub(p.startTime).Seconds()
	if elapsed > 0 {
		p.speedMBps = float64(p.bytesRestored) / elapsed / 1024 / 1024
	}
	p.lastUpdate = now
}

// Status returns current progress status
func (p *ProgressTracker) Status() (int, int64, float64, string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	percent := 0
	if p.totalFiles > 0 {
		percent = p.processed * 100 / p.totalFiles
	}

	return percent, p.bytesRestored, p.speedMBps, p.currentFile
}

// Final returns final statistics
func (p *ProgressTracker) Final() (int, int, int64, int64, time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	duration := time.Since(p.startTime)
	return p.processed, p.totalFiles, p.bytesRestored, p.bytesSkipped, duration
}
