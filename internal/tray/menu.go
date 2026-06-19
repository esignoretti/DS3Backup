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
func setupMenu(t *TrayApp) {
	t.menuItems["title"] = &menuItem{
		item: systray.AddMenuItem("● DS3 Backup", "DS3 Backup Daemon"),
	}
	t.menuItems["title"].item.Disable()

	systray.AddSeparator()

	statusItem := systray.AddMenuItem("Status: Unknown", "Daemon status")
	t.menuItems["status"] = &menuItem{item: statusItem}
	statusItem.Disable()

	scheduledItem := systray.AddMenuItem("Scheduled: -- jobs", "Scheduled jobs count")
	t.menuItems["scheduled"] = &menuItem{item: scheduledItem}
	scheduledItem.Disable()

	systray.AddSeparator()

	// "Run Backup" as a submenu with per-job clickable items
	runBackupItem := systray.AddMenuItem("Run Backup ▸", "Select a job to back up")
	t.menuItems["runBackup"] = &menuItem{item: runBackupItem}

	systray.AddSeparator()

	// Per-job status section label
	jobsHeader := systray.AddMenuItem("Jobs", "Backup job status overview")
	t.menuItems["jobsHeader"] = &menuItem{item: jobsHeader}
	jobsHeader.Disable()

	systray.AddSeparator()

	stopSchedulerItem := systray.AddMenuItem("Stop Scheduler", "Stop the scheduler")
	t.menuItems["stopScheduler"] = &menuItem{item: stopSchedulerItem}

	systray.AddSeparator()

	dashboardItem := systray.AddMenuItem("Open Dashboard", "Open the web dashboard")
	t.menuItems["dashboard"] = &menuItem{item: dashboardItem}

	systray.AddSeparator()

	quitItem := systray.AddMenuItem("Quit", "Quit DS3 Backup daemon")
	t.menuItems["quit"] = &menuItem{item: quitItem}

	go t.refreshStatusLoop()
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

// refreshStatus polls the API and updates menu item titles and tray icon state.
func (t *TrayApp) refreshStatus() {
	status, err := t.queryStatus()
	if err != nil {
		if statusItem, ok := t.menuItems["status"]; ok {
			statusItem.item.SetTitle("Status: Offline")
		}
		t.SetState(StateError)
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

	if stopItem, ok := t.menuItems["stopScheduler"]; ok {
		if status.SchedulerRunning {
			stopItem.item.SetTitle("Stop Scheduler")
		} else {
			stopItem.item.SetTitle("Start Scheduler")
		}
	}

	jobs, err := t.queryJobs()
	if err == nil {
		t.refreshJobMenuItems(jobs)

		hasRunning := false
		hasError := false
		for _, job := range jobs {
			if job.RunInProgress {
				hasRunning = true
			}
			if job.LastError != "" && job.LastRun != nil {
				hasError = true
			}
		}
		switch {
		case hasRunning:
			t.SetState(StateRunning)
		case hasError:
			t.SetState(StateError)
		default:
			t.SetState(StateIdle)
		}
	}
}

// refreshJobMenuItems updates per-job menu items on every poll cycle.
// Creates two groups per job:
//   1. A disabled status item (read-only, showing ✅/❌/⏳ status)
//   2. A clickable sub-item under "▶ Run Backup..." to trigger that job
// systray does not support removing items, so removed jobs are disabled.
func (t *TrayApp) refreshJobMenuItems(jobs []api.BackupJobWithStatus) {
	// "Run Backup..." submenu items
	runBackupParent, hasRunBackup := t.menuItems["runBackup"]

	currentIDs := make(map[string]bool)
	for _, job := range jobs {
		currentIDs[job.ID] = true
	}

	// Garbage-collect stale tracking entries
	for id := range t.jobStatusItems {
		if !currentIDs[id] {
			delete(t.jobStatusItems, id)
		}
	}
	for id := range t.runBackupItems {
		if !currentIDs[id] {
			delete(t.runBackupItems, id)
		}
	}

	for _, job := range jobs {
		statusPrefix := "▶"
		if job.RunInProgress {
			statusPrefix = "⏳"
		} else if job.LastError != "" && job.LastRun != nil {
			statusPrefix = "❌"
		} else if job.LastRun != nil {
			statusPrefix = "✅"
		}

		statusTitle := fmt.Sprintf("%s  %s", statusPrefix, job.Name)
		statusTooltip := fmt.Sprintf("Job: %s", job.ID)
		if job.LastRun != nil {
			statusTooltip += fmt.Sprintf(" — Last: %s", job.LastRun.Format("Jan 2 15:04"))
		}
		if job.LastError != "" {
			statusTooltip += fmt.Sprintf(" — Error: %s", job.LastError)
		}

		// Disabled status item (read-only)
		if existing, ok := t.jobStatusItems[job.ID]; ok {
			existing.SetTitle(statusTitle)
			existing.SetTooltip(statusTooltip)
		} else {
			item := systray.AddMenuItem(statusTitle, statusTooltip)
			item.Disable()
			t.jobStatusItems[job.ID] = item
		}

		// Clickable run-backup submenu item
		if hasRunBackup {
			rbTitle := fmt.Sprintf("Backup “%s”", job.Name)
			rbTooltip := fmt.Sprintf("Trigger backup for job %s", job.ID)
			if existing, ok := t.runBackupItems[job.ID]; ok {
				existing.SetTitle(rbTitle)
				existing.SetTooltip(rbTooltip)
			} else {
				item := runBackupParent.item.AddSubMenuItem(rbTitle, rbTooltip)
				t.runBackupItems[job.ID] = item
				go func(jobID string, menuItem *systray.MenuItem) {
					for range menuItem.ClickedCh {
						t.triggerBackup(jobID)
					}
				}(job.ID, item)
			}
		}
	}

	// Disable items for removed jobs
	for id, item := range t.jobStatusItems {
		if !currentIDs[id] {
			item.SetTitle(fmt.Sprintf("⏻ %s (removed)", id))
			item.Disable()
		}
	}
	for id, item := range t.runBackupItems {
		if !currentIDs[id] {
			item.SetTitle(fmt.Sprintf("Removed: %s", id))
			item.Disable()
		}
	}
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
