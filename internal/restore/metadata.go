package restore

import (
	"os"
	"runtime"
	"time"
)

// SetFileMetadata sets file metadata (modification time and permissions)
// Returns warnings for any failures (non-fatal)
func SetFileMetadata(path string, modTime time.Time) ([]string, error) {
	var warnings []string

	// Set modification time
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		warnings = append(warnings, "Failed to set modification time for "+path+": "+err.Error())
	}

	// Set permissions (Unix only)
	if runtime.GOOS != "windows" {
		// Default permissions: 0644 for files
		mode := os.FileMode(0644)
		if err := os.Chmod(path, mode); err != nil {
			warnings = append(warnings, "Failed to set permissions for "+path+": "+err.Error())
		}
	}

	return warnings, nil
}
