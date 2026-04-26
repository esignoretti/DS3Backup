package models

// RestoreOptions holds restore configuration
type RestoreOptions struct {
	DestinationPath string   // Empty = restore to original paths
	DryRun          bool     // Preview only
	Overwrite       bool     // Overwrite existing (default: false = skip)
	IncludePatterns []string // Glob patterns to include
	ExcludePatterns []string // Glob patterns to exclude
	Concurrency     int      // Number of parallel workers (default: 8)
}

// RestoreProgress holds progress information during restore
type RestoreProgress struct {
	Percent       int
	FilesRestored int
	TotalFiles    int
	BytesRestored int64
	CurrentFile   string
	Status        string // "downloading", "decrypting", "writing", "skipped", "failed"
	SpeedMBps     float64
}

// RestoreResult holds the final restore results
type RestoreResult struct {
	FilesRestored int
	FilesSkipped  int // Already exist or excluded
	FilesFailed   int
	BytesRestored int64
	Duration      int64 // Duration in seconds
	Warnings      []string
	Errors        []string
}

// DryRunResult holds preview information for dry-run mode
type DryRunResult struct {
	FilesToRestore int
	FilesSkipped   int
	TotalSize      int64
	SampleFiles    []*FileEntry // First 20 files
	MoreFiles      int          // Additional files not shown
}

// VerifyResult holds verification results
type VerifyResult struct {
	Total    int
	Verified int
	Failed   int
	Errors   []string
}
