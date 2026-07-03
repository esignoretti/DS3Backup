---
phase: 02-code-review-command
reviewed: 2026-07-03T14:30:00Z
depth: deep
files_reviewed: 8
files_reviewed_list:
  - internal/cli/root.go
  - internal/cli/daemon.go
  - internal/cli/tray_cmd.go
  - internal/cli/version.go
  - internal/tray/tray.go
  - internal/tray/menu.go
  - internal/tray/notifier.go
  - internal/tray/subprocess.go
findings:
  critical: 3
  warning: 8
  info: 5
  total: 16
status: issues_found
---

# Phase 02: Code Review Report

**Reviewed:** 2026-07-03T14:30:00Z
**Depth:** deep (cross-file call chain analysis)
**Files Reviewed:** 8
**Status:** issues_found

## Summary

Reviewed 8 Go source files in `internal/cli/` and `internal/tray/`. Found 3 critical issues (mutex deadlock, double cmd.Wait() violating Go contract, osascript command injection), 8 warnings (thread-safety violations, resource leaks, dead code), and 5 info items.

Key risk areas: tray subprocess lifecycle has a self-deadlock that prevents tray from starting; the quit flow races between os.Exit and trayProcess.Stop(); notification messages unsanitized for osascript shell injection.

Full quit flow traced: user clicks Quit → tray goroutine sends SIGTERM to daemon parent via syscall.Kill → calls os.Exit(0) → daemon receives SIGTERM → calls trayProcess.Stop() (tray already dead, no-op) → apiServer.Stop() → sched.Stop() → removePIDFile(). Race window where daemon shutdown sequence may execute after tray already exited by os.Exit, but current guards (ts.running check) prevent double-wait. However, the goroutine in Start() and the goroutine in Stop() both call cmd.Wait() on the same Cmd — this is the real blocker.

---

## Critical Issues

### CR-01: Mutex deadlock in TraySubprocess.Start()

**File:** `internal/tray/subprocess.go:30,67`
**Issue:** `Start()` acquires `ts.mu.Lock()` at line 30 with `defer ts.mu.Unlock()`. At line 67 it calls `ts.IsRunning()` which also acquires `ts.mu.Lock()` (line 109). This is an immediate self-deadlock — the tray subprocess can never start successfully.

```go
// subprocess.go:30-31
ts.mu.Lock()
defer ts.mu.Unlock()

// ... line 53: ts.running = true

// line 67:
if !ts.IsRunning() {  // calls ts.mu.Lock() -> DEADLOCK
```

**Fix:** Replace the call to `ts.IsRunning()` with an inlined unlocked check, or split the mutex into a separate RWMutex for the `running` field. Simplest fix:

```go
// Before line 67:
if !ts.running || ts.cmd == nil || ts.cmd.Process == nil {
    return fmt.Errorf("tray subprocess failed to start")
}
// Signal(0) is ok without lock if we know process was just started
if ts.cmd.Process.Signal(syscall.Signal(0)) != nil {
    return fmt.Errorf("tray subprocess failed to start")
}
```

### CR-02: Double cmd.Wait() — undefined behavior per Go contract

**File:** `internal/tray/subprocess.go:57,93`
**Issue:** Two goroutines call `cmd.Wait()` on the same `*exec.Cmd`:
1. The monitoring goroutine started at line 56-63
2. The `Stop()` method's goroutine at line 92-94

Go docs state: "Wait releases any resources associated with the Cmd. Wait cannot be called more than once." Calling Wait twice causes undefined behavior — panic, hang, or double-release of process resources.

```go
// Line 56-63 (in Start):
go func() {
    if err := cmd.Wait(); err != nil {  // FIRST Wait
        log.Printf("Tray subprocess exited: %v", err)
    }
    ts.mu.Lock()
    ts.running = false
    ts.mu.Unlock()
}()

// Line 92-94 (in Stop):
go func() {
    ts.cmd.Wait()  // SECOND Wait on same Cmd
    close(done)
}()
```

**Fix:** Remove the monitor goroutine's `cmd.Wait()` or remove the `Stop()` goroutine's `cmd.Wait()`. The monitor goroutine should be the sole owner of `Wait()`. `Stop()` can detect exit by checking the `done` channel from the monitor, or by using a dedicated exit channel:

```go
// In Start(), after setting ts.cmd:
go func() {
    err := cmd.Wait()
    if err != nil {
        log.Printf("Tray subprocess exited: %v", err)
    }
    ts.mu.Lock()
    ts.running = false
    ts.mu.Unlock()
    close(ts.exited)  // add chan struct{} to TraySubprocess
}()

// In Stop():
select {
case <-ts.exited:
    // already exited
case <-time.After(3 * time.Second):
    ts.cmd.Process.Kill()
    <-ts.exited
}
ts.running = false
```

### CR-03: osascript command injection via unsanitized notification messages

**File:** `internal/tray/notifier.go:60`
**Issue:** `sendMacOSNotification` constructs an osascript command via `fmt.Sprintf` where `message` and `title` are interpolated into AppleScript string literals. If a job name or error message contains a double quote (`"`), it breaks out of the string and injects arbitrary AppleScript.

```go
script := fmt.Sprintf(`display notification "%s" with title "%s"`, msg, title)
```

Example: job name `Backup "Robert'; DROP TABLE; --` would produce:
```applescript
display notification "Backup "Robert'; DROP TABLE; --" with title "DS3 Backup"
```

**Fix:** Escape double quotes and backslashes before interpolation:

```go
func escapeOsascript(s string) string {
    s = strings.ReplaceAll(s, "\\", "\\\\")
    s = strings.ReplaceAll(s, "\"", "\\\"")
    return s
}

// Then:
msg := escapeOsascript(message)
title := escapeOsascript(title)
script := fmt.Sprintf(`display notification "%s" with title "%s"`, msg, title)
```

---

## Warnings

### WR-01: systray.SetIcon called from background goroutine (AppKit thread affinity violation)

**File:** `internal/tray/tray.go:153-168`, `internal/tray/menu.go:89-141`
**Issue:** `SetState()` is called from `refreshStatus()`, which runs in the `refreshStatusLoop` goroutine (spawned at `menu.go:69`). `systray.SetIcon()` and `systray.SetTooltip()` are called inside `SetState()`. The code at `menu.go:269-270` already acknowledges that "AppKit CGo has thread affinity" as justification for using `os.Exit(0)` instead of `systray.Quit()`. The same thread-affinity constraint applies to `SetIcon` — calling it from a goroutine not bound to the main thread can cause crashes, hangs, or undefined behavior on macOS.

**Fix:** Use `systray.SetIcon` only within `onReady` or via a channel that the main systray goroutine reads from. Add a state-change channel:

```go
// In TrayApp:
stateChan chan TrayState

// onReady:
go func() {
    for s := range t.stateChan {
        switch s { ... }
    }
}()

// SetState: send to channel instead of calling directly
```

### WR-02: os.Exit(0) skips deferred cleanup — t.onExit never called

**File:** `internal/tray/menu.go:269-272`
**Issue:** The Quit handler calls `os.Exit(0)` to avoid `systray.Quit()` thread-affinity issues (documented in comment). However, `os.Exit` terminates the process immediately without running deferred functions. This means `t.onExit()` (which sets `t.running = false` and logs) is never called. Additionally, the `defer ticker.Stop()` in `refreshStatusLoop()` never runs.

**Fix:** Replace with `systray.Quit()` if the thread-affinity issue is resolved, or restructure using `syscall.Kill(syscall.Getpid(), syscall.SIGKILL)` as last resort. At minimum, the comment should document that onExit cleanup is intentionally bypassed.

### WR-03: cmd.Start() without cmd.Wait() leaks OS process resources

**File:** `internal/tray/menu.go:323`, also `internal/cli/backup.go:175`
**Issue:** `openDashboard()` calls `cmd.Start()` to launch the browser but never calls `cmd.Wait()`. Per `exec.Cmd` docs, resources (including OS process table entries) are not released until `Wait()` is called. While short-lived commands like `open` on macOS exit quickly, repeated dashboard opens accumulate zombies until the parent reaps them.

Same pattern at `backup.go:175`: `_ = exec.Command("sync").Run()` — this one uses `Run()` which calls `Wait()` internally, so it's fine.

**Fix:** Call `cmd.Wait()` in a goroutine after `Start()`:

```go
if err := cmd.Start(); err != nil {
    log.Printf("Failed to open dashboard: %v", err)
    return
}
go func() {
    if err := cmd.Wait(); err != nil {
        log.Printf("Browser command exited with error: %v", err)
    }
}()
```

### WR-04: Global dashboardURL has data race (no mutex)

**File:** `internal/tray/notifier.go:17-22`
**Issue:** `dashboardURL` is a package-level `string` variable. `SetDashboardURL()` writes it (called during daemon startup at `daemon.go:212`). `sendMacOSNotification()` reads it. These run from different goroutines after daemon startup. While the daemon calls `SetDashboardURL` before the tray subprocess starts, the tray subprocess is a separate OS process with its own memory space — so the value is copied at fork time. However, if `SendNotification` is called from a goroutine concurrent with `SetDashboardURL`, there's a data race.

**Fix:** Use `sync/atomic` for the string, or add a mutex:

```go
var (
    dashboardURL atomic.Value
)

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
```

### WR-05: daemonStopCmd ignores io.ReadAll error

**File:** `internal/cli/daemon.go:687`
**Issue:** `body, _ := io.ReadAll(resp.Body)` silently discards the read error. If the response body is truncated or corrupt, the output will be empty or partial without indication.

**Fix:** Check the error:

```go
body, err := io.ReadAll(resp.Body)
if err != nil {
    fmt.Printf("Warning: partial API response: %v\n", err)
}
```

### WR-06: TrayApp.Run() is dead code (unused)

**File:** `internal/tray/tray.go:96-120`
**Issue:** `TrayApp.Run()` implements a 5-second timeout startup with panic recovery, but is never called anywhere in the codebase. All production paths use `RunBlocking()` (called from `tray_cmd.go:44`). The `Run()` method has known issues (the `started` channel is closed when systray exits, which would overwrite `t.running` after `Run()` has already returned). Should be removed or documented as unused.

**Fix:** Remove `Run()` method and its associated test coverage if unused, or mark with a clear doc comment.

### WR-07: TraySubprocess.WaitForShutdown() is dead code (unused)

**File:** `internal/tray/subprocess.go:121-125`
**Issue:** `WaitForShutdown()` is exported but never called anywhere. The tray subprocess uses its own signal handling in `tray_cmd.go:37-42`.

**Fix:** Remove or document as deprecated.

### WR-08: root.GetLogFile() is dead code (unused)

**File:** `internal/cli/root.go:150-153`
**Issue:** `GetLogFile()` is exported but has zero callers outside its own definition. The log file reference is only used internally within `root.go`.

**Fix:** Remove.

---

## Info

### IN-01: init() in notifier.go logs on every import

**File:** `internal/tray/notifier.go:10-14`
**Issue:** The `init()` function runs whenever the `tray` package is imported, even in headless/CI environments. It logs "TIP: Install terminal-notifier..." which is noisy but harmless.

**Suggestion:** Move to a lazy check on first notification, or gate on `!IsHeadless()`.

### IN-02: Windows and Linux default notifications silently dropped

**File:** `internal/tray/notifier.go:31-35`
**Issue:** `SendNotification` for `"windows"` and the `default` case return `nil` without sending anything. Windows users get silent failures on backup notifications.

**Suggestion:** Document this limitation in the function comment, or wire up PowerShell `New-BurntToastNotification` for Windows.

### IN-03: runBackupForDaemon variable shadowing

**File:** `internal/cli/daemon.go:299,341`
**Issue:** `err` is declared at line 299 (`cfg, err := loadConfig()`) and shadowed at line 341 (`run, err := engine.RunBackup(...)`). While the shadowed `err` is correctly used for the RunBackup flow, the variable reuse could lead to confusion during maintenance.

**Suggestion:** Use distinct variable names or explicit scope blocks.

### IN-04: daemonRunCmd foreground flag is internal-only but user-visible

**File:** `internal/cli/daemon.go:721`
**Issue:** The `--foreground` flag is documented as "used internally" but is registered as a user-facing flag on `daemonRunCmd`. Users who discover it and use it directly will get undocumented behavior.

**Suggestion:** Mark as hidden: `daemonRunCmd.Flags().MarkHidden("foreground")`.

### IN-05: TrayApp `started` channel semaphore bug in Run()

**File:** `internal/tray/tray.go:103`
**Issue:** In `Run()`, the `started` channel is closed from the goroutine via `defer close(started)`. Since `close` on a closed channel panics, if `onExit` is somehow called twice, this would panic. The method is dead code (see WR-06), so this is informational only.

**Suggestion:** Remove the dead `Run()` method entirely.

---

## Quit Flow Trace (Deep Analysis)

Full path from user clicking Quit in tray menu to daemon shutdown:

```
menu.go:261  handleMenuClicks receives ClickedCh on quitItem
menu.go:265  ppid = os.Getppid()
menu.go:266  syscall.Kill(ppid, syscall.SIGTERM)
menu.go:272  os.Exit(0)                    ← tray process dies here
                     ↓
daemon.go:267 sigChan receives SIGTERM    ← daemon process
daemon.go:269 falls through select
daemon.go:280 trayProcess.Stop()          ← tray already dead, ts.running=false, no-op
daemon.go:284 apiServer.Stop()
daemon.go:288 sched.Stop()
daemon.go:289 removePIDFile()
daemon.go:290 log.Println("Daemon stopped")
```

**Race window:** Between `os.Exit(0)` (menu.go:272) and `trayProcess.Stop()` (daemon.go:280), the tray subprocess's monitoring goroutine (subprocess.go:56-63) runs `cmd.Wait()`, sets `ts.running = false`. When `Stop()` checks `ts.running`, it's already false → safe no-op.

**However**, the double `cmd.Wait()` (CR-02) means that if `Stop()` is called while the monitor goroutine is still inside `cmd.Wait()`, two concurrent `Wait()` calls execute on the same `*exec.Cmd` — violating Go's contract.

**Notification flow (notifier.go):**

```
notifier.go:70-91  NotifyBackup* called from daemon.go:338-349
notifier.go:26-37  SendNotification → sendMacOSNotification (darwin)
notifier.go:42-62  terminal-notifier first, osascript fallback
notifier.go:60     UNSANITIZED: fmt.Sprintf into osascript string
```

**Subprocess spawn/kill lifecycle:**

```
daemon.go:249  trayProcess.Start()
  subprocess.go:30-73
    - cmd.Start() (fork + exec)
    - mutex deadlock at line 67 (CR-01) — blocks forever
    - monitor goroutine: cmd.Wait() + ts.running=false
    - (never reaches here due to deadlock)

Double-stop: ts.running check prevents re-kill. Safe.
Double-quit: os.Exit(0) kills process, second click never delivered. Safe.
Crash: monitor goroutine sees cmd.Wait() error, sets running=false. No auto-restart.
```

---

_Reviewed: 2026-07-03T14:30:00Z_
_Reviewer: gsd-code-reviewer (deep analysis)_
_Depth: deep_
