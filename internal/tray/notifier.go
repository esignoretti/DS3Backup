package tray

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
)

func init() {
	// Warn if terminal-notifier is missing — without it "Show" button opens Finder
	if _, err := exec.LookPath("terminal-notifier"); err != nil {
		log.Println("TIP: Install terminal-notifier for actionable notifications: brew install terminal-notifier")
	}
}

var dashboardURL string

// SetDashboardURL configures the URL that notification "Show" buttons open.
// Should be called before any notification is sent (e.g., from daemon startup).
func SetDashboardURL(url string) {
	dashboardURL = url
}

// SendNotification sends a desktop notification using platform-specific APIs.
func SendNotification(title, message string) error {
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
	args := []string{
		"-title", title,
		"-message", message,
		"-group", "com.ds3backup",
	}
	if dashboardURL != "" {
		args = append(args, "-open", dashboardURL)
	}
	cmd := exec.Command("terminal-notifier", args...)
	if err := cmd.Run(); err == nil {
		return nil
	}
	// osascript: embed URL in message since "Show" button can't be customized
	msg := message
	if dashboardURL != "" {
		msg = message + " — " + dashboardURL
	}
	script := fmt.Sprintf(`display notification "%s" with title "%s"`, msg, title)
	return exec.Command("osascript", "-e", script).Run()
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
