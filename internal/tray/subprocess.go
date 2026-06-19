package tray

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// TraySubprocess manages the tray as an external subprocess.
type TraySubprocess struct {
	apiPort int
	cmd     *exec.Cmd
	mu      sync.Mutex
	running bool
}

// NewTraySubprocess creates a subprocess manager for the tray.
func NewTraySubprocess(apiPort int) *TraySubprocess {
	return &TraySubprocess{apiPort: apiPort}
}

// Start spawns the tray as a separate process. Returns nil if started successfully.
// The subprocess runs "ds3backup tray start --port N".
func (ts *TraySubprocess) Start() error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.running {
		return fmt.Errorf("tray subprocess already running")
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	args := []string{"tray", "start", "--port", fmt.Sprintf("%d", ts.apiPort)}
	cmd := exec.Command(execPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start tray subprocess: %w", err)
	}

	ts.cmd = cmd
	ts.running = true

	// Monitor subprocess in background
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("Tray subprocess exited: %v", err)
		}
		ts.mu.Lock()
		ts.running = false
		ts.mu.Unlock()
	}()

	// Give it a moment to start; check it's alive
	time.Sleep(500 * time.Millisecond)
	if !ts.IsRunning() {
		return fmt.Errorf("tray subprocess failed to start")
	}

	log.Printf("Tray subprocess started (PID: %d)", cmd.Process.Pid)
	return nil
}

// Stop sends SIGTERM to the tray subprocess and waits for it to exit.
func (ts *TraySubprocess) Stop() {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if !ts.running || ts.cmd == nil || ts.cmd.Process == nil {
		return
	}

	log.Printf("Stopping tray subprocess (PID: %d)...", ts.cmd.Process.Pid)
	if err := ts.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		log.Printf("Failed to signal tray subprocess: %v", err)
		ts.cmd.Process.Kill()
	}

	// Wait up to 3 seconds for clean exit
	done := make(chan struct{})
	go func() {
		ts.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		log.Println("Tray subprocess did not exit in time, killing")
		ts.cmd.Process.Kill()
	}

	ts.running = false
	log.Println("Tray subprocess stopped")
}

// IsRunning returns whether the tray subprocess is believed to be alive.
func (ts *TraySubprocess) IsRunning() bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if !ts.running || ts.cmd == nil || ts.cmd.Process == nil {
		return false
	}
	// Signal 0 checks if process exists
	return ts.cmd.Process.Signal(syscall.Signal(0)) == nil
}

// WaitForShutdown blocks until SIGINT/SIGTERM is received.
// Intended for use by the tray subprocess itself.
func WaitForShutdown() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
}
