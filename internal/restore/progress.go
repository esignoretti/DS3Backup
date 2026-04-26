package restore

import (
	"fmt"
	"strings"
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

	// Calculate speed every 100ms
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

// FormatPath truncates long paths for display
func FormatPath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	// Show beginning and end of path
	keep := (maxLen - 5) / 2
	return path[:keep] + "..." + path[len(path)-keep:]
}

// FormatSpeed formats speed in MB/s
func FormatSpeed(mbps float64) string {
	if mbps < 1 {
		return fmt.Sprintf("%.1f KB/s", mbps*1024)
	}
	return fmt.Sprintf("%.1f MB/s", mbps)
}

// FormatBytes formats bytes as human-readable string
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := "KMGTPE"
	if exp >= len(units) {
		exp = len(units) - 1
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), units[exp])
}

// ClearLine clears the current line for progress updates
func ClearLine() string {
	return "\r\033[K"
}

// ProgressLine formats a progress line
func ProgressLine(percent, processed, total int, bytes int64, speed float64, file string) string {
	speedStr := FormatSpeed(speed)
	bytesStr := FormatBytes(bytes)
	fileStr := FormatPath(file, 60)

	return fmt.Sprintf("\r[%3d%%] Files: %d/%d, Restored: %s, Speed: %s, Current: %s",
		percent, processed, total, bytesStr, speedStr, fileStr)
}

// SummaryLine formats a summary line
func SummaryLine(restored, skipped, failed int, duration time.Duration) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("Files restored: %d", restored))
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("Files skipped: %d", skipped))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("Files failed: %d", failed))
	}
	parts = append(parts, fmt.Sprintf("Duration: %s", formatDuration(duration)))
	return strings.Join(parts, ", ")
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm %ds", minutes, seconds)
}
