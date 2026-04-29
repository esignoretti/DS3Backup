package api

import (
	"log"
	"net/http"
	"strings"

	"github.com/esignoretti/ds3backup/internal/api/dashboard"
)

// setupRouter creates and configures the HTTP serve mux with all API routes.
func (s *APIServer) setupRouter() http.Handler {
	mux := http.NewServeMux()

	// Daemon endpoints
	mux.HandleFunc("GET /api/v1/status", s.handleStatus)
	mux.HandleFunc("POST /api/v1/start", s.handleStart)
	mux.HandleFunc("POST /api/v1/stop", s.handleStop)

	// Job endpoints
	mux.HandleFunc("GET /api/v1/jobs", s.handleListJobs)
	mux.HandleFunc("POST /api/v1/jobs", s.handleCreateJob)
	mux.HandleFunc("GET /api/v1/jobs/{id}", s.handleGetJob)
	mux.HandleFunc("GET /api/v1/jobs/{id}/history", s.handleGetJobHistory)

	// Backup endpoints
	mux.HandleFunc("POST /api/v1/backup/run/{id}", s.handleRunBackup)

	// Logs endpoint
	mux.HandleFunc("GET /api/v1/logs", s.handleGetLogs)

	// Directory browser endpoint
	mux.HandleFunc("GET /api/v1/browse", s.handleBrowse)

	// Dashboard — serve index.html at root
	dashFS := dashboard.GetDashboardFS()
	if dashFS != nil {
		mux.Handle("GET /", http.FileServer(http.FS(dashFS)))
	}

	return mux
}

// loggingMiddleware logs each incoming API request with method and path.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("API %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

// extractPathParam extracts a path parameter value from a URL path.
func extractPathParam(path, prefix string) string {
	return strings.TrimPrefix(path, prefix)
}
