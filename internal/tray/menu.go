package tray

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/getlantern/systray"
	"github.com/esignoretti/ds3backup/internal/api"
)

// menuItem holds a reference to a systray menu item for dynamic updates.
type menuItem struct {
	item *systray.MenuItem
}

// setupMenu creates the system tray menu structure for the tray app.
// Called by onReady() after the tray icon is set.
func setupMenu(t *TrayApp) {
	// Title item
	t.menuItems["title"] = &menuItem{
		item: systray.AddMenuItem("● DS3 Backup", "DS3 Backup Daemon"),
	}
	t.menuItems["title"].item.Disable()

	systray.AddSeparator()

	// Status items
	statusItem := systray.AddMenuItem("Status: Unknown", "Daemon status")
	t.menuItems["status"] = &menuItem{item: statusItem}
	statusItem.Disable()

	scheduledItem := systray.AddMenuItem("Scheduled: -- jobs", "Scheduled jobs count")
	t.menuItems["scheduled"] = &menuItem{item: scheduledItem}
	scheduledItem.Disable()

	systray.AddSeparator()

	// Run Backup submenu (parent item with sub-items for each job)
	runBackupItem := systray.AddMenuItem("▶ Run Backup...", "Trigger a backup for a specific job")
	t.menuItems["runBackup"] = &menuItem{item: runBackupItem}

	systray.AddSeparator()

	// Stop/Start Scheduler toggle
	stopSchedulerItem := systray.AddMenuItem("⏹ Stop Scheduler", "Stop the scheduler")
	t.menuItems["stopScheduler"] = &menuItem{item: stopSchedulerItem}

	systray.AddSeparator()

	// Open Dashboard (stub for future)
	dashboardItem := systray.AddMenuItem("📊 Open Dashboard", "Open the web dashboard")
	t.menuItems["dashboard"] = &menuItem{item: dashboardItem}

	systray.AddSeparator()

	// Quit
	quitItem := systray.AddMenuItem("❌ Quit", "Quit DS3 Backup daemon")
	t.menuItems["quit"] = &menuItem{item: quitItem}

	// Start periodic status refresh
	go t.refreshStatusLoop()

	// Handle menu item clicks
	go t.handleMenuClicks()
}

// refreshStatusLoop periodically polls the API for status updates.
func (t *TrayApp) refreshStatusLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			t.refreshStatus()
		case <-t.stopChan:
			return
		}
	}
}

// refreshStatus polls the API and updates menu item titles.
func (t *TrayApp) refreshStatus() {
	status, err := t.queryStatus()
	if err != nil {
		if statusItem, ok := t.menuItems["status"]; ok {
			statusItem.item.SetTitle("Status: Offline")
		}
		return
	}

	if statusItem, ok := t.menuItems["status"]; ok {
		if status.Running {
			statusItem.item.SetTitle(fmt.Sprintf("Status: Running (up %s)", status.Uptime))
		} else {
			statusItem.item.SetTitle("Status: Stopped")
		}
	}

	if scheduledItem, ok := t.menuItems["scheduled"]; ok {
		scheduledItem.item.SetTitle(fmt.Sprintf("Scheduled: %d jobs", status.ScheduledJobs))
	}

	// Update scheduler toggle title
	if stopItem, ok := t.menuItems["stopScheduler"]; ok {
		if status.SchedulerRunning {
			stopItem.item.SetTitle("⏹ Stop Scheduler")
		} else {
			stopItem.item.SetTitle("▶ Start Scheduler")
		}
	}

	// Build per-job backup menu items on first successful status poll
	t.ensureJobBackupItems()
}

// queryStatus calls the daemon API for current status.
func (t *TrayApp) queryStatus() (*api.StatusResponse, error) {
	resp, err := http.Get(t.apiBaseURL + "/api/v1/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var status api.StatusResponse
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// handleMenuClicks processes clicks on menu items in a loop.
func (t *TrayApp) handleMenuClicks() {
	for {
		select {
		case <-t.stopChan:
			return
		case <-t.menuItems["stopScheduler"].item.ClickedCh:
			t.toggleScheduler()
		case <-t.menuItems["quit"].item.ClickedCh:
			log.Println("Quit requested from tray menu")
			systray.Quit()
	case <-t.menuItems["dashboard"].item.ClickedCh:
		t.openDashboard()
	case <-t.menuItems["runBackup"].item.ClickedCh:
		log.Println("Run Backup main item clicked — use per-job sub-items")
		}
	}
}

// toggleScheduler starts or stops the scheduler via the API.
func (t *TrayApp) toggleScheduler() {
	status, err := t.queryStatus()
	if err != nil {
		log.Printf("Failed to get status for scheduler toggle: %v", err)
		return
	}

	var resp *http.Response
	if status.SchedulerRunning {
		resp, err = http.Post(t.apiBaseURL+"/api/v1/stop", "application/json", nil)
	} else {
		resp, err = http.Post(t.apiBaseURL+"/api/v1/start", "application/json", nil)
	}

	if err != nil {
		log.Printf("Failed to toggle scheduler: %v", err)
		return
	}
	resp.Body.Close()

	// Refresh status immediately
	t.refreshStatus()
}

// openDashboard opens the dashboard in the default browser.
func (t *TrayApp) openDashboard() {
	dashboardURL := t.apiBaseURL + "/"
	log.Printf("Opening dashboard: %s", dashboardURL)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dashboardURL)
	case "linux":
		cmd = exec.Command("xdg-open", dashboardURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", dashboardURL)
	default:
		log.Printf("Unsupported platform for opening browser: %s", runtime.GOOS)
		return
	}

	if err := cmd.Start(); err != nil {
		log.Printf("Failed to open dashboard: %v", err)
	}
}

// ensureJobBackupItems creates per-job backup trigger menu items.
// Called once on first successful status poll.
func (t *TrayApp) ensureJobBackupItems() {
	// Skip if already created
	if len(t.jobRunItems) > 0 {
		return
	}

	jobs, err := t.queryJobs()
	if err != nil || len(jobs) == 0 {
		return
	}

	systray.AddSeparator()

	// Create individual backup trigger items
	for _, job := range jobs {
		item := systray.AddMenuItem(
			fmt.Sprintf("▶ Backup: %s", job.Name),
			fmt.Sprintf("Trigger backup for %s", job.Name),
		)
		t.jobRunItems[job.ID] = item

		// Start a goroutine to handle clicks for this item
		go func(jobID string, menuItem *systray.MenuItem) {
			for range menuItem.ClickedCh {
				t.triggerBackup(jobID)
			}
		}(job.ID, item)
	}
}

// queryJobs fetches the list of jobs from the API.
func (t *TrayApp) queryJobs() ([]api.BackupJobWithStatus, error) {
	resp, err := http.Get(t.apiBaseURL + "/api/v1/jobs")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var jobList api.JobListResponse
	if err := json.Unmarshal(body, &jobList); err != nil {
		return nil, err
	}

	return jobList.Jobs, nil
}

// triggerBackup sends a POST to trigger a backup for the given job.
func (t *TrayApp) triggerBackup(jobID string) {
	log.Printf("Triggering backup for job: %s", jobID)
	resp, err := http.Post(
		t.apiBaseURL+"/api/v1/backup/run/"+jobID,
		"application/json", nil,
	)
	if err != nil {
		log.Printf("Failed to trigger backup for %s: %v", jobID, err)
		return
	}
	resp.Body.Close()
	log.Printf("Backup triggered for job %s", jobID)
}
