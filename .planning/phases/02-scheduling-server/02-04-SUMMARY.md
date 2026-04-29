---
phase: 02-scheduling-server
plan: 04
subsystem: "Tests, Auto-Start & Dependency Cleanup"
tags: ["tests", "auto-start", "launchagent", "systemd", "macos", "linux", "api", "tray", "daemon"]
requires:
  - "02-01: Scheduler & Job Management"
  - "02-02: REST API Server"
  - "02-03: Daemon Mode & System Tray"
provides:
  - "Scheduler integration tests (6 new tests)"
  - "API response format tests (4 new tests)"
  - "Tray structural tests (4 tests)"
  - "Daemon CLI/interface tests (6 tests)"
  - "Auto-start: DaemonConfig.InstallAutoStart/RemoveAutoStart/IsAutoStartInstalled"
  - "macOS LaunchAgent plist generation and launchctl load/unload"
  - "Linux systemd user service unit file generation"
affects:
  - "internal/scheduler/scan_test.go"
  - "internal/config/config.go"
  - "internal/api/api_test.go"
  - "internal/api/server.go"
  - "internal/tray/tray_test.go"
  - "internal/cli/daemon_test.go"
  - "go.mod"
  - "go.sum"
tech-stack:
  added:
    - "os/exec (for launchctl/systemctl subprocess calls)"
  patterns:
    - "macOS LaunchAgent plist XML generation via fmt.Sprintf"
    - "LaunchAgent paths in ~/Library/LaunchAgents/"
    - "launchctl load/unload for plist activation"
    - "systemd user service unit files in ~/.config/systemd/user/"
    - "Interface compliance checks via compile-time type assertions"
    - "Response format verification via map[string]interface{} JSON decode"
    - "Test temp directories via t.TempDir()"
decisions:
  - "Auto-start plist paths use os/user home directory for portability"
  - "launchctl load/unload errors are returned (not silently swallowed) so callers can handle"
  - "Systemd daemon-reload is best-effort (error ignored) since systemctl may not be available"
metrics:
  duration: "~10m"
  completed: "2026-04-29"
key-files:
  created:
    - "internal/scheduler/scan_test.go (298 lines)"
    - "internal/tray/tray_test.go (105 lines)"
    - "internal/cli/daemon_test.go (150 lines)"
  modified:
    - "internal/config/config.go (added auto-start methods)"
    - "internal/api/api_test.go (added 4 response format tests)"
    - "internal/api/server.go (fixed Stop clearing server reference)"
---

# Phase 2 Plan 4: Tests, Auto-Start & Dependency Cleanup Summary

Six new scheduler integration tests, platform auto-start (macOS LaunchAgent + Linux systemd), structural tray and daemon CLI tests, and dependency graph cleanup.

## What Was Built

### Scheduler Integration Tests (`internal/scheduler/scan_test.go`)

6 integration-level tests exercising the scheduler with real cron evaluation but without real S3/backup dependencies:

1. **TestScheduler_CronExpressionAtMidnight** — Schedules "0 0 * * *", verifies HasJob and GetScheduledJobs
2. **TestScheduler_EveryMinuteExpression** — Schedules "* * * * *", starts scheduler, waits 500ms, stops — no panics
3. **TestScheduler_MultipleJobs** — Schedules 3 jobs, removes 1, reloads with only 1 — confirms full lifecycle
4. **TestScheduler_ReloadJobsOverwritesExisting** — Schedules job A + B, ReloadJobs with only A (different expression), confirms B removed
5. **TestBackupJobRunner_RunJobCallsBackupFn** — Creates BackupJobRunner with mock runBackupFn, verifies invocation and config save
6. **TestScheduleManager_EnableDisableCycle** — Creates ScheduleManager, enables/disables a job, verifies scheduler state and config persistence

### Auto-Start Support (`internal/config/config.go`)

**DaemonConfig** received 3 new methods:

- **`InstallAutoStart(binaryPath string) error`** — Platform dispatch:
  - **macOS:** Generates `~/Library/LaunchAgents/com.ds3backup.daemon.plist` with RunAtLoad, KeepAlive, stdout/stderr log paths. Calls `launchctl load` to activate immediately.
  - **Linux:** Generates `~/.config/systemd/user/ds3backup-daemon.service` with Restart=on-failure and log paths. Runs `systemctl --user daemon-reload` (best-effort).

- **`RemoveAutoStart() error`** — Unloads (macOS) or removes (Linux) the auto-start configuration.

- **`IsAutoStartInstalled() bool`** — Returns true if the plist or service file exists on disk.

### API Response Format Tests (4 new tests to make 12 total)

- **TestAPIResponseFormat_Status** — Verifies all 5 required fields (running, schedulerRunning, scheduledJobs, apiPort, uptime) have correct types
- **TestAPIResponseFormat_JobList** — Verifies jobs array structure, required per-job fields, and absence of sensitive fields (encryptionPassword)
- **TestAPIResponseFormat_Error** — Verifies error object has "error" (string) and "code" (numeric, 404)
- **TestAPIServer_StartStopLifecycle** — Start server → IsRunning → Stop → IsRunning lifecycle

### API Server Bug Fix (`internal/api/server.go`)

**Rule 1 - Bug fix:** `Stop()` was not setting `s.server = nil` after shutdown, causing `IsRunning()` to return `true` even after a successful shutdown. Fixed by clearing the server reference after `Shutdown()` returns, and changing the lock from RLock to Lock for the server reference.

### Tray Structural Tests (`internal/tray/tray_test.go`)

4 structural tests that don't require a graphical environment:
- Port configuration via NewTrayApp
- Different port values
- Notification string formatting
- Stop (without panic) when tray hasn't been started

### Daemon CLI Tests (`internal/cli/daemon_test.go`)

6 tests covering:
- PID file create/read/remove with 0600 permissions (uses t.TempDir)
- Compile-time interface compliance: `var _ api.BackupRunner = (*daemonRunnerAdapter)(nil)`
- Compile-time interface compliance: `var _ api.JobManager = (*daemonJobManagerAdapter)(nil)`
- Flag existence and defaults for --no-api, --no-tray, --port on daemon run/stop/status commands

### Dependency Cleanup

- `go mod tidy` confirmed clean — no unused dependencies to remove
- `go build ./...` compiles successfully
- Full `go test ./...` passes across all packages

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Stop() does not clear server reference in APIServer**
- **Found during:** Task 2 (TestAPIServer_StartStopLifecycle)
- **Issue:** `Stop()` called `s.server.Shutdown()` but never set `s.server = nil`, so `IsRunning()` returned `true` even after shutdown
- **Fix:** Set `s.server = nil` after successful `Shutdown()`, changed lock from RLock to Lock for mutating access
- **Files modified:** `internal/api/server.go`
- **Commit:** `9e5d0f4`

**2. [Rule 3 - Fix] Test used /dev/null for config path which doesn't work with .tmp atomic write**
- **Found during:** Task 1 (TestBackupJobRunner_RunJobCallsBackupFn, TestScheduleManager_EnableDisableCycle)
- **Issue:** `os.DevNull` becomes `/dev/null.tmp` when `SaveConfig` appends `.tmp` before rename, causing "operation not permitted"
- **Fix:** Replaced with `t.TempDir()`-based config path (`newTestConfig` helper)
- **Files modified:** `internal/scheduler/scan_test.go`
- **Commit:** `6c6ba70`

**3. [Deviation - Test expectation] LastRun not updated by BackupJobRunner.RunJob**
- **Found during:** Task 1 (TestBackupJobRunner_RunJobCallsBackupFn)
- **Issue:** The test asserted `LastRun` would be updated by `RunJob`, but that field is only set by the daemon-level `runBackupForDaemon`, not by `BackupJobRunner.RunJob`
- **Fix:** Changed test to verify that `runBackupFn` was called and config was saved (file exists), matching the actual contract
- **Files modified:** `internal/scheduler/scan_test.go`
- **Commit:** `6c6ba70`

## Known Stubs

None.

## Threat Surface Scan

New security-relevant surface in auto-start code:

| Flag | File | Description |
|------|------|-------------|
| threat_flag: exec_cmd | `internal/config/config.go` | `exec.Command("launchctl", "load", plistPath)` at line ~vt — subprocess execution from user-owned path |
| threat_flag: exec_cmd | `internal/config/config.go` | `exec.Command("systemctl", "--user", "daemon-reload")` — best-effort, error ignored per threat model T-02-12 |

Both are within the accepted threat model (T-02-10: plist written to user-owned ~/Library/LaunchAgents, T-02-12: launchctl failure is graceful).

## Verification Results

1. ✅ `go build ./...` — compiles with no errors
2. ✅ `go test ./internal/scheduler/ -v -count=1 -run "TestScheduler_|TestBackupJobRunner_|TestScheduleManager_"` — 6 tests pass
3. ✅ `go test ./internal/api/ -v -count=1` — 12 tests pass
4. ✅ `go test ./internal/tray/ -v -count=1` — 4 structural tests pass
5. ✅ `go test ./internal/cli/ -v -count=1 -run "TestDaemon"` — 6 daemon tests pass
6. ✅ `go mod tidy` — clean, no unused dependencies
7. ✅ `go test ./...` — full suite passes

## Self-Check

- [x] `internal/scheduler/scan_test.go` exists (298 lines ≥ 60 minimum)
- [x] `internal/config/config.go` contains `AutoStart` method
- [x] `internal/config/config.go` uses `LaunchAgents` path pattern
- [x] `internal/api/api_test.go` has 12 total tests
- [x] `internal/tray/tray_test.go` exists and passes
- [x] `internal/cli/daemon_test.go` exists and passes
- [x] `go.mod` contains `robfig/cron`
- [x] `go build ./...` passes
- [x] All plan must_haves and patterns present

**Result: PASSED**
