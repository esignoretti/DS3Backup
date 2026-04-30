package api

import (
	"log"
	"net/http"
)

func (s *APIServer) setupRouter() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /", s.createDashboardHandler())

	// Daemon endpoints
	mux.HandleFunc("GET /api/v1/status", s.handleStatus)
	mux.HandleFunc("POST /api/v1/start", s.handleStart)
	mux.HandleFunc("POST /api/v1/stop", s.handleStop)

	// Job endpoints
	mux.HandleFunc("GET /api/v1/jobs", s.handleListJobs)
	mux.HandleFunc("POST /api/v1/jobs", s.handleCreateJob)
	mux.HandleFunc("GET /api/v1/jobs/{id}", s.handleGetJob)
	mux.HandleFunc("PATCH /api/v1/jobs/{id}", s.handlePatchJob)
	mux.HandleFunc("DELETE /api/v1/jobs/{id}", s.handleDeleteJob)
	mux.HandleFunc("GET /api/v1/jobs/{id}/history", s.handleGetJobHistory)

	// Backup and utility endpoints
	mux.HandleFunc("POST /api/v1/backup/run/{id}", s.handleRunBackup)
	mux.HandleFunc("GET /api/v1/logs", s.handleGetLogs)
	mux.HandleFunc("GET /api/v1/browse", s.handleBrowse)

	return mux
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("API %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
