---
phase: 03-desktop-ui
plan: 03
subsystem: tray
tags: [tray, notifications, browser, menu]
requires: [api-history-endpoint, dashboard-embed, web-dashboard-spa]
provides: [tray-dashboard-integration, tray-per-job-backup, desktop-notifications]
affects: [internal/tray, internal/cli]
tech-stack:
  added: [os/exec for browser opening]
  patterns: [platform-specific browser open, dynamic menu items from API]
key-files:
  modified:
    - internal/tray/menu.go
    - internal/tray/tray.go
    - internal/cli/daemon.go
decisions:
  - Browser opening uses exec.Command with platform-specific commands (open/xdg-open/rundll32)
  - Per-job menu items created once on first status poll, not dynamically rebuilt
  - Notifications are best-effort (log failures silently)
  - Notification calls placed before/after engine.RunBackup in runBackupForDaemon
metrics:
  duration: ~3m
  completed: 2026-04-29
  tasks: 2
  commits: 2
---

# Phase 03 Plan 03: Tray Integration Summary

Wire up tray menu items to open dashboard in browser, trigger per-job backups, and integrate desktop notifications into daemon backup flow.

## Tasks Executed

### Task 1: Wire dashboard opening and per-job backup menu items
- **Commit:** `73549db`
- Replaced stub click handlers for "Open Dashboard" and "Run Backup..."
- Added `openDashboard` method (platform-specific browser open: `open` on macOS, `xdg-open` on Linux, `rundll32` on Windows)
- Added `ensureJobBackupItems` — fetches jobs from API on first poll, creates per-job menu items dynamically
- Added `queryJobs` and `triggerBackup` helper methods
- Added `jobRunItems` field + initialization to TrayApp

### Task 2: Wire desktop notifications into daemon backup flow
- **Commit:** `05f8c1f`
- Added `tray.NotifyBackupStarting(job.Name)` before backup execution
- Added `tray.NotifyBackupFailed` for engine errors or run.Error
- Added `tray.NotifyBackupComplete` on successful completion with file count
- Fulfills requirement UI-03

## Verification

- `go build ./...` — compiles with no errors
- `go test ./... -count=1` — all packages pass
- No more "not yet implemented" logs for dashboard or run backup
- Notification calls present in `runBackupForDaemon`

## Deviations from Plan

None — plan executed exactly as written.
