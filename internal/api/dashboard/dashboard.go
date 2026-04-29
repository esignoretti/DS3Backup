package dashboard

import (
	"embed"
	"io/fs"
)

//go:embed index.html
var dashboardFiles embed.FS

// GetDashboardFS returns the embedded filesystem with the dashboard HTML.
func GetDashboardFS() fs.FS {
	f, err := fs.Sub(dashboardFiles, ".")
	if err != nil {
		return nil
	}
	return f
}
