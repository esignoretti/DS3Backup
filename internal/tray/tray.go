package tray

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/getlantern/systray"
)

// IsHeadless returns true when no GUI session is available.
// On macOS this detects SSH sessions, CI environments, and other headless
// contexts where AppKit/Cocoa APIs would crash the process.
func IsHeadless() bool {
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_CLIENT") != "" {
		return true
	}
	if os.Getenv("CI") != "" {
		return true
	}
	return false
}

// TrayState represents the tray icon visual state.
type TrayState int

const (
	StateIdle    TrayState = iota // blue
	StateRunning                  // green
	StateError                    // red
)

// iconIdle is a 16x16 solid blue PNG.
var iconIdle = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00, 0x10,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0xF3, 0xFF, 0x61, 0x00, 0x00, 0x00,
	0x19, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0xD0, 0x6C, 0xD8, 0xF9,
	0x9F, 0x12, 0xCC, 0x30, 0x6A, 0xC0, 0xA8, 0x01, 0xA3, 0x06, 0x0C, 0x17,
	0x03, 0x00, 0xF6, 0xED, 0x61, 0x1F, 0x52, 0x1C, 0xCD, 0xE0, 0x00, 0x00,
	0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
}

// iconRunning is a 16x16 solid green PNG.
var iconRunning = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00, 0x10,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0xF3, 0xFF, 0x61, 0x00, 0x00, 0x00,
	0x19, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0xD0, 0x3B, 0x53, 0xF8,
	0x9F, 0x12, 0xCC, 0x30, 0x6A, 0xC0, 0xA8, 0x01, 0xA3, 0x06, 0x0C, 0x17,
	0x03, 0x00, 0x91, 0x8A, 0x6A, 0x1F, 0x02, 0x02, 0x84, 0x7D, 0x00, 0x00,
	0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
}

// iconError is a 16x16 solid red PNG.
var iconError = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00, 0x10,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0xF3, 0xFF, 0x61, 0x00, 0x00, 0x00,
	0x19, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x78, 0xEE, 0x63, 0xF3,
	0x9F, 0x12, 0xCC, 0x30, 0x6A, 0xC0, 0xA8, 0x01, 0xA3, 0x06, 0x0C, 0x17,
	0x03, 0x00, 0xA2, 0x11, 0x6E, 0x1F, 0x5B, 0xDF, 0x91, 0xB2, 0x00, 0x00,
	0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
}

// TrayApp manages the macOS system tray lifecycle, menu, and state.
type TrayApp struct {
	running        bool
	apiBaseURL     string
	mu             sync.Mutex
	menuItems      map[string]*menuItem
	stopChan       chan struct{}
	jobStatusItems map[string]*systray.MenuItem
	runBackupItems map[string]*systray.MenuItem
	state          TrayState
}

// NewTrayApp creates a new TrayApp connected to the daemon API on the given port.
func NewTrayApp(apiPort int) *TrayApp {
	return &TrayApp{
		apiBaseURL:     fmt.Sprintf("http://127.0.0.1:%d", apiPort),
		menuItems:      make(map[string]*menuItem),
		stopChan:       make(chan struct{}),
		jobStatusItems: make(map[string]*systray.MenuItem),
		runBackupItems: make(map[string]*systray.MenuItem),
		state:          StateIdle,
	}
}

// Run starts the system tray application in a goroutine with panic recovery.
// Returns after 5s if the tray starts successfully, or immediately if it crashes.
// NOTE: SIGTRAP/SIGSEGV from C/ObjC code cannot be caught by recover().
// For crash isolation, use RunBlocking() in a subprocess instead.
func (t *TrayApp) Run() error {
	started := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("CRASH: systray.Run panicked: %v", r)
			}
			close(started)
		}()
		systray.Run(t.onReady, t.onExit)
	}()

	select {
	case <-started:
		t.mu.Lock()
		t.running = false
		t.mu.Unlock()
		return fmt.Errorf("systray exited or crashed")
	case <-time.After(5 * time.Second):
		t.mu.Lock()
		t.running = true
		t.mu.Unlock()
		return nil
	}
}

// RunBlocking starts the system tray application and blocks until systray.Quit() is called.
// Intended for use in a subprocess where C/ObjC crashes won't affect the daemon.
func (t *TrayApp) RunBlocking() {
	t.mu.Lock()
	t.running = true
	t.mu.Unlock()

	systray.Run(t.onReady, t.onExit)

	t.mu.Lock()
	t.running = false
	t.mu.Unlock()
}

// Stop terminates the system tray application and cleans up.
func (t *TrayApp) Stop() {
	close(t.stopChan)
	systray.Quit()
	t.mu.Lock()
	t.running = false
	t.mu.Unlock()
}

// IsRunning returns whether the tray application is currently active.
func (t *TrayApp) IsRunning() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.running
}

// SetState changes the tray icon to reflect the given state.
func (t *TrayApp) SetState(state TrayState) {
	t.mu.Lock()
	t.state = state
	t.mu.Unlock()
	switch state {
	case StateRunning:
		systray.SetIcon(iconRunning)
		systray.SetTooltip("DS3 Backup — Backup in progress")
	case StateError:
		systray.SetIcon(iconError)
		systray.SetTooltip("DS3 Backup — Error")
	default:
		systray.SetIcon(iconIdle)
		systray.SetTooltip("DS3 Backup Daemon")
	}
}

// onReady is called by systray when the tray icon is ready.
func (t *TrayApp) onReady() {
	systray.SetIcon(iconIdle)
	systray.SetTitle("DS3 Backup")
	systray.SetTooltip("DS3 Backup Daemon")

	setupMenu(t)

	log.Println("System tray ready")
}

// onExit is called by systray when the application exits.
func (t *TrayApp) onExit() {
	log.Println("System tray exiting")
	t.mu.Lock()
	t.running = false
	t.mu.Unlock()
}
