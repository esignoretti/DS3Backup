package api

import (
	"log"
	"net/http"
	"strings"
)

// setupRouter creates and configures the HTTP serve mux with all API routes.
// Uses Go 1.22+ ServeMux pattern matching for path parameters ({id} syntax).
//
// Registered routes:
//
//	GET    /api/v1/status        - Daemon status and scheduler state
//	POST   /api/v1/start         - Start the scheduler
//	POST   /api/v1/stop          - Stop the scheduler
//	GET    /api/v1/jobs          - List all backup jobs
//	GET    /api/v1/jobs/{id}     - Get details for a specific job
//	POST   /api/v1/backup/run/{id} - Trigger a backup run for a job
func (s *APIServer) setupRouter() http.Handler {
	mux := http.NewServeMux()

	// Daemon endpoints
	mux.HandleFunc("GET /api/v1/status", s.handleStatus)
	mux.HandleFunc("POST /api/v1/start", s.handleStart)
	mux.HandleFunc("POST /api/v1/stop", s.handleStop)

	// Job endpoints
	mux.HandleFunc("GET /api/v1/jobs", s.handleListJobs)
	mux.HandleFunc("GET /api/v1/jobs/{id}", s.handleGetJob)

	// Backup endpoints
	mux.HandleFunc("POST /api/v1/backup/run/{id}", s.handleRunBackup)

	return mux
}

// loggingMiddleware logs each incoming API request with method and path.
// Used as a wrapper around the main mux to provide observability.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("API %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

// extractPathParam extracts a path parameter value from a URL path by
// removing the given prefix string.
//
// Deprecated: Use r.PathValue("param") with Go 1.22+ ServeMux patterns instead.
// Kept for backwards compatibility and manual path parsing if needed.
func extractPathParam(path, prefix string) string {
	return strings.TrimPrefix(path, prefix)
}
