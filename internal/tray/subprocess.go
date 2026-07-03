package tray

import (
	"fmt"
	"log"
	"os"
	"os/exec"
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
	exited  chan struct{}
}

// NewTraySubprocess creates a subprocess manager for the tray.
func NewTraySubprocess(apiPort int) *TraySubprocess {
	return &TraySubprocess{
		apiPort: apiPort,
		exited:  make(chan struct{}),
	}
}

// Start spawns the tray as a separate process. Returns nil if started successfully.
// The subprocess runs "ds3backup tray start --port N".
func (ts *TraySubprocess) Start() error {
	ts.mu.Lock()

	if ts.running {
		ts.mu.Unlock()
		return fmt.Errorf("tray subprocess already running")
	}

	execPath, err := os.Executable()
	if err != nil {
		ts.mu.Unlock()
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	args := []string{"tray", "start", "--port", fmt.Sprintf("%d", ts.apiPort)}
	cmd := exec.Command(execPath, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		ts.mu.Unlock()
		return fmt.Errorf("failed to start tray subprocess: %w", err)
	}

	ts.cmd = cmd
	ts.running = true
	ts.mu.Unlock()

	// Monitor subprocess in background — sole owner of cmd.Wait()
	go func() {
		cmd.Wait()
		close(ts.exited)
	}()

	// Give it a moment to start; check process is alive
	time.Sleep(500 * time.Millisecond)
	if cmd.Process == nil || cmd.Process.Signal(syscall.Signal(0)) != nil {
		ts.mu.Lock()
		ts.running = false
		ts.mu.Unlock()
		return fmt.Errorf("tray subprocess failed to start")
	}

	log.Printf("Tray subprocess started (PID: %d)", cmd.Process.Pid)
	return nil
}

// Stop sends SIGTERM to the tray subprocess and waits for it to exit.
func (ts *TraySubprocess) Stop() {
	ts.mu.Lock()
	if !ts.running || ts.cmd == nil || ts.cmd.Process == nil {
		ts.mu.Unlock()
		return
	}
	pid := ts.cmd.Process.Pid
	ts.mu.Unlock()

	log.Printf("Stopping tray subprocess (PID: %d)...", pid)

	if err := ts.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		log.Printf("Failed to signal tray subprocess: %v", err)
		ts.cmd.Process.Kill()
	}

	// Wait for monitor goroutine to confirm exit
	select {
	case <-ts.exited:
	case <-time.After(3 * time.Second):
		log.Println("Tray subprocess did not exit in time, killing")
		ts.cmd.Process.Kill()
		<-ts.exited
	}

	ts.mu.Lock()
	ts.running = false
	ts.mu.Unlock()
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


