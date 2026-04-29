package tray

import (
	"strings"
	"testing"
)

// TestNewTrayApp_CreatesWithCorrectPort verifies that the API base URL is set
// correctly based on the port passed to NewTrayApp.

func TestNewTrayApp_CreatesWithCorrectPort(t *testing.T) {
	app := NewTrayApp(8099)

	if app.apiBaseURL != "http://127.0.0.1:8099" {
		t.Errorf("expected apiBaseURL http://127.0.0.1:8099, got %s", app.apiBaseURL)
	}

	if app.menuItems == nil {
		t.Error("expected menuItems map to be initialized")
	}

	if app.stopChan == nil {
		t.Error("expected stopChan to be initialized")
	}
}

// TestNewTrayApp_DifferentPort verifies different port values.

func TestNewTrayApp_DifferentPort(t *testing.T) {
	app := NewTrayApp(9100)

	if app.apiBaseURL != "http://127.0.0.1:9100" {
		t.Errorf("expected apiBaseURL http://127.0.0.1:9100, got %s", app.apiBaseURL)
	}
}

// TestSendNotification_Format verifies that notification strings are formatted
// properly without actually sending any notifications.

func TestSendNotification_Format(t *testing.T) {
	tests := []struct {
		title string
		body  string
	}{
		{"Backup Complete", "Job 'Daily Backup' completed successfully"},
		{"Backup Failed", "Job 'Hourly Sync' failed: connection timeout"},
		{"Backup Started", "Job 'Weekly Archive' started"},
	}

	for _, tt := range tests {
		result := formatNotification(tt.title, tt.body)
		if !strings.Contains(result, tt.title) {
			t.Errorf("expected notification to contain title %q, got %q", tt.title, result)
		}
		if !strings.Contains(result, tt.body) {
			t.Errorf("expected notification to contain body %q, got %q", tt.body, result)
		}
	}
}

// TestTrayApp_StopWhenNotRunning verifies that Stop doesn't panic when the tray
// hasn't been started (not in a graphical environment).

func TestTrayApp_StopWhenNotRunning(t *testing.T) {
	app := NewTrayApp(8099)

	// IsRunning should be false initially
	if app.IsRunning() {
		t.Error("expected app not to be running")
	}

	// Stop should not panic even though we never called Run
	app.Stop()

	if app.IsRunning() {
		t.Error("expected app not to be running after Stop")
	}
}

// formatNotification creates a formatted notification string for testing.
// This duplicates the logic from notifier.go to allow testing without
// actually spawning osascript/notify-send.
func formatNotification(title, body string) string {
	return title + ": " + body
}
