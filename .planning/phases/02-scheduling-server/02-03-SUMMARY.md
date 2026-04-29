---
phase: 02-scheduling-server
plan: 03
subsystem: "Daemon Mode & System Tray"
tags: ["daemon", "system-tray", "cli", "macos", "scheduler"]
requires:
  - "02-01: Scheduler & Job Management"
  - "02-02: REST API Server"
provides:
  - "Daemon run/status/stop CLI subcommands"
  - "System tray integration (macOS)"
  - "Desktop notifications"
affects:
  - "internal/cli/daemon.go"
  - "internal/cli/root.go"
  - "internal/tray/tray.go"
  - "internal/tray/menu.go"
  - "internal/tray/notifier.go"
  - "go.mod"
  - "go.sum"
tech-stack:
  added:
    - "github.com/getlantern/systray v1.2.2"
  patterns:
    - "Cobra CLI subcommand tree (daemon → run/status/stop)"
    - "Systray menu lifecycle (onReady/onExit)"
    - "PID-based daemon process detection"
decisions:
  - "D-06: Daemon stores PID at ~/.ds3backup/daemon.pid for PID-based status detection"
  - "D-10: daemon stop command tries API stop first, falls back to SIGTERM via PID"
  - "D-11: Desktop notifications use platform-specific APIs (osascript/notify-send)"
metrics:
  duration: "~15m"
  completed: "2026-04-29"
key-files:
  created:
    - "internal/cli/daemon.go (456 lines)"
    - "internal/tray/tray.go (94 lines)"
    - "internal/tray/menu.go (182 lines)"
    - "internal/tray/notifier.go (59 lines)"
  modified:
    - "go.mod (added getlantern/systray dep)"
    - "go.sum (dependency checksums)"
---

# Phase 2 Plan 3: Daemon Mode CLI + System Tray Summary

Daemon mode CLI commands (run, status, stop) with full scheduler/API/tray lifecycle orchestration and macOS system tray integration.

## What Was Built

### Daemon CLI Commands (`internal/cli/daemon.go`)

- **`ds3backup daemon run`** — Starts the background daemon with:
  - Scheduler initialization and schedule loading
  - API server on localhost (configurable port, default 8099)
  - System tray (macOS only, controllable via `--no-tray`)
  - Headless mode (`--no-api` starts scheduler without API)
  - PID file management at `~/.ds3backup/daemon.pid`
  - Signal-based graceful shutdown (SIGINT/SIGTERM) in reverse order: tray → API → scheduler
  - `runBackupForDaemon` helper that replicates the backup pipeline (S3 client, crypto, index, engine) for scheduled execution

- **`ds3backup daemon status`** — Queries daemon state via:
  - PID file check for process liveness
  - HTTP GET to `/api/v1/status` for detailed state (running, scheduler, jobs, uptime)

- **`ds3backup daemon stop`** — Stops the daemon via:
  - API POST to `/api/v1/stop` (primary)
  - SIGTERM sent directly to PID (fallback)

### Adapter Types

- `daemonRunnerAdapter` — Implements `api.BackupRunner` by wrapping `scheduler.Scheduler`
- `daemonJobManagerAdapter` — Implements `api.JobManager` by wrapping `config.Config`

### System Tray (`internal/tray/`)

- **`tray.go`** — `TrayApp` struct with `Run()`, `Stop()`, `IsRunning()` methods. Uses `getlantern/systray` for cross-platform tray support. Includes minimal blue-circle PNG icon data.
- **`menu.go`** — Dynamic menu with:
  - Title and status info items (updated every 5s via API polling)
  - "Run Backup..." parent item (job sub-items stubbed for future)
  - Scheduler start/stop toggle via API
  - Dashboard placeholder
  - Quit button
- **`notifier.go`** — Desktop notifications using:
  - macOS: `osascript` display notification
  - Linux: `notify-send`
  - Windows: stub (not yet implemented)

### Dependencies

- Added `github.com/getlantern/systray v1.2.2` with its transitive deps

## Deviations from Plan

**None — plan executed exactly as written.**

All three tasks were implemented in a single commit since the daemon lifecycle wiring (Task 3) was written as part of the initial daemon.go creation, consistent with the full orchestration flow specified in the plan.

## Known Stubs

| Stub | File | Line | Reason |
|------|------|------|--------|
| Run Backup sub-items (per-job) | `internal/tray/menu.go` | 81 | Individual job sub-menu items are not yet wired; only the parent menu item exists. Future: show per-job submenu. |
| Dashboard | `internal/tray/menu.go` | 87 | "Open Dashboard" is a placeholder for a future web UI feature. |
| Windows notifications | `internal/tray/notifier.go` | 32 | Windows toast notification is a no-op stub; macOS and Linux are implemented. |

## Verification Results

1. ✅ `go build ./...` — compiles with no errors
2. ✅ `ds3backup daemon --help` — shows all 3 subcommands (run, status, stop)
3. ✅ `ds3backup daemon run --help` — shows --no-tray, --no-api, --port flags
4. ✅ `ds3backup daemon status --help` — shows --port flag
5. ✅ `ds3backup daemon stop --help` — shows --port flag
6. ✅ All file line counts exceed minimums (daemon.go: 456 ≥ 150, tray.go: 94 ≥ 60, menu.go: 182 ≥ 50, notifier.go: 59 ≥ 40)
7. ✅ Code patterns verified: `scheduler.Start/Stop`, `apiServer.Start/Stop`, `localhost`/`127.0.0.1`

## Threat Flags

None — all security-relevant surface (PID file, signal handling, goroutine lifecycle) matches the existing threat model with appropriate mitigations.

## Self-Check

- [x] `internal/cli/daemon.go` exists (456 lines)
- [x] `internal/tray/tray.go` exists (94 lines)
- [x] `internal/tray/menu.go` exists (182 lines)
- [x] `internal/tray/notifier.go` exists (59 lines)
- [x] `go.mod` contains `getlantern/systray`
- [x] `go build ./...` passes
- [x] All plan must_haves and patterns present

**Result: PASSED**
