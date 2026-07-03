package tray

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

var (
	dashboardURL atomic.Value // stores string
	checkTNOnce sync.Once
)

// SetDashboardURL configures the URL that notification "Show" buttons open.
// Should be called before any notification is sent (e.g., from daemon startup).
func SetDashboardURL(url string) {
	dashboardURL.Store(url)
}

func getDashboardURL() string {
	v := dashboardURL.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

// SendNotification sends a desktop notification using platform-specific APIs.
func SendNotification(title, message string) error {
	checkTNOnce.Do(func() {
		if _, err := exec.LookPath("terminal-notifier"); err != nil {
			log.Println("TIP: Install terminal-notifier for actionable notifications: brew install terminal-notifier")
		}
	})
	switch runtime.GOOS {
	case "darwin":
		return sendMacOSNotification(title, message)
	case "linux":
		return sendLinuxNotification(title, message)
	case "windows":
		return nil
	default:
		return nil
	}
}

// sendMacOSNotification tries terminal-notifier first, falls back to osascript.
// With terminal-notifier, -open sets the "Show" button to open the dashboard.
// Without it, osascript's notification always opens Finder — no way to override.
func sendMacOSNotification(title, message string) error {
	url := getDashboardURL()
	args := []string{
		"-title", title,
		"-message", message,
		"-group", "com.ds3backup",
	}
	if url != "" {
		args = append(args, "-open", url)
	}
	cmd := exec.Command("terminal-notifier", args...)
	if err := cmd.Run(); err == nil {
		return nil
	}
	// osascript: embed URL in message since "Show" button can't be customized
	msg := message
	if url != "" {
		msg = message + " — " + url
	}
	msg = escapeOsascript(msg)
	escTitle := escapeOsascript(title)
	script := fmt.Sprintf(`display notification "%s" with title "%s"`, msg, escTitle)
	return exec.Command("osascript", "-e", script).Run()
}

func escapeOsascript(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

// sendLinuxNotification sends a notification via notify-send.
func sendLinuxNotification(title, message string) error {
	return exec.Command("notify-send", title, message).Run()
}

// NotifyBackupComplete sends a desktop notification when a backup completes successfully.
func NotifyBackupComplete(jobName string, filesProcessed int) {
	msg := fmt.Sprintf("Backup complete for %s: %d files", jobName, filesProcessed)
	if err := SendNotification("DS3 Backup", msg); err != nil {
		// Best-effort
	}
}

// NotifyBackupFailed sends a desktop notification when a backup fails.
func NotifyBackupFailed(jobName string, errMsg string) {
	msg := fmt.Sprintf("Backup failed for %s: %s", jobName, errMsg)
	if err := SendNotification("DS3 Backup", msg); err != nil {
		// Best-effort
	}
}

// NotifyBackupStarting sends a desktop notification when a backup begins.
func NotifyBackupStarting(jobName string) {
	msg := fmt.Sprintf("Backup starting for %s", jobName)
	if err := SendNotification("DS3 Backup", msg); err != nil {
		// Best-effort
	}
}
