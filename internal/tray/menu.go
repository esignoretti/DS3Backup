package tray

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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
			log.Println("Dashboard requested (not yet implemented)")
			// Future: open browser to API dashboard
		case <-t.menuItems["runBackup"].item.ClickedCh:
			log.Println("Run Backup clicked (individual job selection not yet implemented)")
			// Future: show submenu with job list
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
