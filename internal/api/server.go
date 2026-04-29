package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// APIServer is the HTTP REST API server for daemon control.
type APIServer struct {
	port            int
	runner          BackupRunner
	jobManager      JobManager
	historyProvider HistoryProvider
	logPath         string
	server          *http.Server
	mu              sync.RWMutex
	startTime       time.Time
}

// NewAPIServer creates a new APIServer with the given port and dependencies.
func NewAPIServer(port int, runner BackupRunner, jobManager JobManager, historyProvider HistoryProvider, logPath string) *APIServer {
	return &APIServer{
		port:            port,
		runner:          runner,
		jobManager:      jobManager,
		historyProvider: historyProvider,
		logPath:         logPath,
	}
}

// Start sets up the HTTP server and begins listening on localhost only.
// It runs in a goroutine and returns immediately. Callers should check
// IsRunning() to verify the server started successfully.
func (s *APIServer) Start() error {
	s.mu.Lock()
	s.startTime = time.Now()
	mux := s.setupRouter()
	s.server = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", s.port),
		Handler: loggingMiddleware(mux),
	}
	s.mu.Unlock()

	go func() {
		log.Printf("API server listening on %s", s.Addr())
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("API server error: %v", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the HTTP server with a 5-second timeout.
func (s *APIServer) Stop() error {
	s.mu.Lock()
	svr := s.server
	if svr == nil {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Printf("API server shutting down...")
	if err := svr.Shutdown(ctx); err != nil {
		return fmt.Errorf("API server shutdown error: %w", err)
	}

	// Clear the server reference so IsRunning returns false.
	s.mu.Lock()
	s.server = nil
	s.mu.Unlock()

	log.Printf("API server stopped")
	return nil
}

// IsRunning returns whether the server is currently running.
func (s *APIServer) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.server != nil
}

// Addr returns the listen address of the server.
func (s *APIServer) Addr() string {
	return fmt.Sprintf("127.0.0.1:%d", s.port)
}

// writeJSON serializes data as JSON and writes it to the response.
func (s *APIServer) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("error encoding JSON response: %v", err)
		http.Error(w, `{"error":"internal server error","code":500}`, http.StatusInternalServerError)
	}
}

// writeError writes a JSON error response with the given status code and message.
func (s *APIServer) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, ErrorResponse{
		Error: message,
		Code:  status,
	})
}
