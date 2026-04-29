package tray

import (
	"fmt"
	"os/exec"
	"runtime"
)

// SendNotification sends a desktop notification using platform-specific APIs.
func SendNotification(title, message string) error {
	switch runtime.GOOS {
	case "darwin":
		return sendMacOSNotification(title, message)
	case "linux":
		return sendLinuxNotification(title, message)
	case "windows":
		// Stub: Windows toast notifications not yet implemented
		return nil
	default:
		return nil
	}
}

// sendMacOSNotification sends a notification via osascript on macOS.
func sendMacOSNotification(title, message string) error {
	script := fmt.Sprintf(`display notification "%s" with title "%s"`, message, title)
	cmd := exec.Command("osascript", "-e", script)
	return cmd.Run()
}

// sendLinuxNotification sends a notification via notify-send on Linux.
func sendLinuxNotification(title, message string) error {
	cmd := exec.Command("notify-send", title, message)
	return cmd.Run()
}

// NotifyBackupComplete sends a desktop notification when a backup completes successfully.
func NotifyBackupComplete(jobName string, filesProcessed int) {
	msg := fmt.Sprintf("Backup complete for %s: %d files", jobName, filesProcessed)
	if err := SendNotification("DS3 Backup", msg); err != nil {
		// Notifications are best-effort; log failures silently
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
