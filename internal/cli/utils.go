package cli

import (
	"time"

	"github.com/esignoretti/ds3backup/internal/util"
)

func formatBytes(bytes int64) string {
	return util.FormatBytes(bytes)
}

func formatDuration(d time.Duration) string {
	return util.FormatDuration(d)
}
