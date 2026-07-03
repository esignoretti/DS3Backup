# Code Audit Fixes — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix all 5 critical bugs, 5 warnings, and remove ~50 lines of dead code identified in the comprehensive code audit.

**Architecture:** 9 independent tasks — each produces working code and passing tests. Fixes grouped by subsystem (tray, crypto, backup, API). Most tasks are small (1-3 file edits). One task restructures the tray subprocess lifecycle (the most complex fix).

**Tech Stack:** Go 1.26, systray v1.2.2, robfig/cron v3, cobra v1.8

---

### Task 1: Fix TraySubprocess self-deadlock + double cmd.Wait()

**Files:**
- Modify: `internal/tray/subprocess.go`

**Bugs:** CR-01 (`Start()` holds mutex then calls `IsRunning()` which re-acquires it → deadlock) and CR-02 (two goroutines call `cmd.Wait()` on same `*exec.Cmd` → undefined behavior).

- [ ] **Step 1: Replace self-deadlocking `IsRunning()` call with inlined check**

In `Start()`, at line 67, replace `if !ts.IsRunning()` with an inlined unlocked check:

```go
// Inline check: no mutex needed — we just called cmd.Start() and set ts.running=true
if ts.cmd.Process == nil {
    return fmt.Errorf("tray subprocess failed to start")
}
// Signal 0 checks if process is alive
if ts.cmd.Process.Signal(syscall.Signal(0)) != nil {
    return fmt.Errorf("tray subprocess failed to start")
}
```

- [ ] **Step 2: Eliminate double cmd.Wait()**

Remove the monitor goroutine's `cmd.Wait()` in `Start()`. Move `ts.running = false` to `Stop()` only, or use an `exited` channel.

Add `exited chan struct{}` to `TraySubprocess` struct. In `Start()`, have the monitor goroutine close `exited` on process exit instead of calling Wait:

```go
// In NewTraySubprocess:
exited: make(chan struct{}),

// In Start(), replace the monitor goroutine:
go func() {
    cmd.Wait() // sole owner of Wait()
    close(ts.exited)
}()
```

- [ ] **Step 3: Fix Stop() to use `exited` channel instead of calling Wait()**

```go
func (ts *TraySubprocess) Stop() {
    ts.mu.Lock()
    if !ts.running || ts.cmd == nil || ts.cmd.Process == nil {
        ts.mu.Unlock()
        return
    }
    pid := ts.cmd.Process.Pid
    ts.mu.Unlock()

    log.Printf("Stopping tray subprocess (PID: %d)...", pid)
    ts.cmd.Process.Signal(syscall.SIGTERM)

    select {
    case <-ts.exited:
    case <-time.After(3 * time.Second):
        log.Println("Tray subprocess did not exit in time, killing")
        ts.cmd.Process.Kill()
        <-ts.exited
    }

    ts.mu.Lock()
    ts.running = false
    ts.mu.Unlock()
    log.Println("Tray subprocess stopped")
}
```

- [ ] **Step 4: Build and test**

Run: `/opt/homebrew/bin/go build ./... && /opt/homebrew/bin/go test ./...`
Expected: no build errors, tray tests pass

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "fix(tray): subprocess deadlock + double cmd.Wait
- CR-01: Start() held mutex then called IsRunning() which re-acquired it → deadlock
- CR-02: monitor goroutine and Stop() both called cmd.Wait() on same Cmd
- Use exited channel, single cmd.Wait() owner, inlined liveness check"
```

---

### Task 2: Fix osascript command injection

**Files:**
- Modify: `internal/tray/notifier.go`

**Bug:** CR-03 — job name or error message with `"` can inject arbitrary AppleScript.

- [ ] **Step 1: Add escape function and use it in osascript path**

Add `strings` import. Add escape function before `sendMacOSNotification`:

```go
func escapeOsascript(s string) string {
    s = strings.ReplaceAll(s, "\\", "\\\\")
    s = strings.ReplaceAll(s, "\"", "\\\"")
    return s
}
```

In the osascript fallback block, escape both title and message:

```go
msg := escapeOsascript(message)
escTitle := escapeOsascript(title)
if dashboardURL != "" {
    msg = escapeOsascript(message + " — " + dashboardURL)
}
script := fmt.Sprintf(`display notification "%s" with title "%s"`, msg, escTitle)
```

- [ ] **Step 2: Build and test**

Run: `/opt/homebrew/bin/go build ./... && /opt/homebrew/bin/go test ./...`
Expected: no build errors, tests pass

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "fix(notifier): osascript command injection (CR-03)
Escape double quotes and backslashes in notification messages
before interpolating into AppleScript. Job names with embedded
quotes could previously inject arbitrary AppleScript."
```

---

### Task 3: Fix batch silent file drop

**Files:**
- Modify: `internal/backup/engine.go`

**Bug:** CR-04 — `batchBuilder.AddFile()` return value ignored. When batch is full, `AddFile` returns `(false, nil)` but the caller still increments `FilesAdded` and continues.

- [ ] **Step 1: Check AddFile return and upload batch when full**

In `engine.go` around line 157, check the return value:

```go
ok, err := batchBuilder.AddFile(entry.Path, entry.Hash, serialized)
if err != nil {
    return nil, fmt.Errorf("batch add file %s: %w", entry.Path, err)
}
if !ok {
    // Batch is full — upload it now, then create new batch
    manifest, uploadErr := batchBuilder.Upload(ctx, e.s3client, job.ObjectLockMode, job.RetentionDays)
    if uploadErr != nil {
        return nil, fmt.Errorf("batch upload: %w", uploadErr)
    }
    run.BatchesUploaded++
    // ... (copy the batch-to-fileRef mapping logic from step 5 in engine.go)
    batchBuilder = s3client.NewBatchBuilder(s3client.DefaultBatchConfig, job.ID)
    // Re-add the current file to the new batch
    ok, err = batchBuilder.AddFile(entry.Path, entry.Hash, serialized)
    if err != nil || !ok {
        return nil, fmt.Errorf("failed to add file to fresh batch: %w", err)
    }
}
```

This requires extracting the batch-to-fileRef mapping into a helper to avoid code duplication with step 5.

- [ ] **Step 2: Extract `flushBatch` helper function**

Add a method or function that handles the batch upload + file ref mapping:

```go
func (e *BackupEngine) flushBatch(ctx context.Context, batchBuilder *s3client.BatchBuilder, job *models.BackupJob, uniqueFiles []*fileState, run *models.BackupRun) (*s3client.BatchBuilder, error) {
    if batchBuilder.FileCount() == 0 {
        return batchBuilder, nil
    }
    manifest, err := batchBuilder.Upload(ctx, e.s3client, job.ObjectLockMode, job.RetentionDays)
    if err != nil {
        return nil, err
    }
    run.BatchesUploaded++
    // Map file refs
    for _, ref := range manifest.Files {
        for _, uf := range uniqueFiles {
            if uf.Hash != nil && len(uf.Hash) > 0 && string(uf.Hash) == string(ref.Hash) && uf.IsInBatch && uf.S3Key == "" {
                uf.BatchID = manifest.BatchID
                uf.S3Key = fmt.Sprintf("backups/%s/batches/%s.enc", job.ID, manifest.BatchID)
                uf.OffsetInBatch = ref.OffsetInBatch
                uf.LengthInBatch = ref.LengthInBatch
                break
            }
        }
    }
    return s3client.NewBatchBuilder(s3client.DefaultBatchConfig, job.ID), nil
}
```

- [ ] **Step 3: Build and test**

Run: `/opt/homebrew/bin/go build ./... && /opt/homebrew/bin/go test ./...`
Expected: no build errors, tests pass

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "fix(backup): batch silent file drop (CR-04)
AddFile() return value was ignored. When batch hit 10MB/500 file
limit, excess files silently vanished while FilesAdded incremented.
Now flush full batch immediately and continue with fresh batch."
```

---

### Task 4: Fix DeserializeEncryptedFile OOB panic

**Files:**
- Modify: `internal/crypto/crypto.go`

**Bug:** CR-05 — no bounds checks on length-prefixed fields. Crafted S3 payload → `data[idx : idx+fieldLen]` panics when `idx+fieldLen > len(data)`.

- [ ] **Step 1: Add bounds checks before every slice operation**

```go
func DeserializeEncryptedFile(data []byte) (*EncryptedFile, error) {
    if len(data) < 20 {
        return nil, errors.New("data too short")
    }

    idx := 0

    if idx+1 > len(data) {
        return nil, errors.New("truncated: nonce length")
    }
    nonceLen := int(data[idx])
    idx++
    if idx+nonceLen > len(data) {
        return nil, errors.New("truncated: nonce data")
    }
    nonce := data[idx : idx+nonceLen]
    idx += nonceLen

    if idx+1 > len(data) {
        return nil, errors.New("truncated: hash length")
    }
    hashLen := int(data[idx])
    idx++
    if idx+hashLen > len(data) {
        return nil, errors.New("truncated: hash data")
    }
    hash := data[idx : idx+hashLen]
    idx += hashLen

    if idx+8 > len(data) {
        return nil, errors.New("truncated: original size")
    }
    // ... keep existing big-endian decode
    origSize := int64(binary.BigEndian.Uint64(data[idx : idx+8]))
    idx += 8

    if idx+8 > len(data) {
        return nil, errors.New("truncated: compressed size")
    }
    compSize := int64(binary.BigEndian.Uint64(data[idx : idx+8]))
    idx += 8

    if idx+1 > len(data) {
        return nil, errors.New("truncated: tag length")
    }
    tagLen := int(data[idx])
    idx++
    if idx+tagLen > len(data) {
        return nil, errors.New("truncated: tag data")
    }
    tag := data[idx : idx+tagLen]
    idx += tagLen

    ciphertext := data[idx:]

    return &EncryptedFile{
        Nonce:          nonce,
        Ciphertext:     ciphertext,
        Tag:            tag,
        OriginalHash:   hash,
        OriginalSize:   origSize,
        CompressedSize: compSize,
    }, nil
}
```

Add `encoding/binary` to imports (replace manual byte-shifting with `binary.BigEndian.Uint64`).

- [ ] **Step 2: Build and test**

Run: `/opt/homebrew/bin/go build ./... && /opt/homebrew/bin/go test ./...`
Expected: no build errors, crypto tests pass

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "fix(crypto): DeserializeEncryptedFile OOB panic (CR-05)
Added bounds checks before every slice operation. Crafted S3
payloads with truncated length-prefixed fields could previously
cause out-of-bounds slice panic → daemon crash.
Used encoding/binary for cleaner big-endian uint64 reads."
```

---

### Task 5: Fix AppKit thread affinity for SetIcon and SetTooltip

**Files:**
- Modify: `internal/tray/tray.go`

**Bug:** WR-01 — `SetState()` calls `systray.SetIcon()` and `systray.SetTooltip()` from the `refreshStatusLoop` goroutine. On macOS, AppKit calls must happen from the main thread (where `systray.Run` blocks).

- [ ] **Step 1: Add state change channel and handle in onReady**

Add `stateChan chan TrayState` to `TrayApp` struct and `NewTrayApp`:

```go
type TrayApp struct {
    // ... existing fields ...
    stateChan chan TrayState
}

func NewTrayApp(apiPort int) *TrayApp {
    return &TrayApp{
        // ... existing ...
        stateChan: make(chan TrayState, 4),
    }
}
```

- [ ] **Step 2: Update SetState to send on channel**

```go
func (t *TrayApp) SetState(state TrayState) {
    select {
    case t.stateChan <- state:
    default:
        // Drop if channel full (consumer is behind)
    }
}
```

- [ ] **Step 3: Add state consumer goroutine in onReady**

```go
func (t *TrayApp) onReady() {
    systray.SetIcon(iconIdle)
    systray.SetTitle("DS3 Backup")
    systray.SetTooltip("DS3 Backup Daemon")

    setupMenu(t)

    // Handle state changes on the main systray goroutine
    go func() {
        for state := range t.stateChan {
            switch state {
            case StateRunning:
                systray.SetIcon(iconRunning)
                systray.SetTooltip("DS3 Backup — Backup in progress")
            case StateError:
                systray.SetIcon(iconError)
                systray.SetTooltip("DS3 Backup — Error")
            default:
                systray.SetIcon(iconIdle)
                systray.SetTooltip("DS3 Backup Daemon")
            }
        }
    }()

    log.Println("System tray ready")
}
```

Remove the old `SetState` body that directly called `systray.SetIcon`.

- [ ] **Step 4: Build and test**

Run: `/opt/homebrew/bin/go build ./... && /opt/homebrew/bin/go test ./...`
Expected: no build errors, tray tests pass

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "fix(tray): AppKit thread affinity for SetIcon (WR-01)
systray.SetIcon/SetTooltip called from background goroutine violates
macOS AppKit thread affinity. Now send state changes via channel
consumed in onReady (main systray goroutine)."
```

---

### Task 6: Fix zombie processes + dashboardURL data race + ignored errors

**Files:**
- Modify: `internal/tray/menu.go`
- Modify: `internal/tray/notifier.go`
- Modify: `internal/cli/daemon.go`

**Bugs:** WR-03 (zombie browser processes), WR-04 (dashboardURL data race), WR-05 (ignored io.ReadAll error), IN-01 (init() logs on import)

- [ ] **Step 1: Fix zombie processes in openDashboard**

In `menu.go`, wrap `cmd.Start()` with a goroutine that calls `cmd.Wait()`:

```go
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
        return
    }
    go func() {
        cmd.Wait()
    }()
}
```

- [ ] **Step 2: Fix dashboardURL data race with atomic.Value**

In `notifier.go`, replace `var dashboardURL string` + `SetDashboardURL` with atomic:

```go
var (
    dashboardURL atomic.Value // stores string
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

Add `"sync/atomic"` import. Replace reads of `dashboardURL` with `getDashboardURL()`.

- [ ] **Step 3: Fix init() noise — make check lazy**

Replace the `init()` function with a `sync.Once` in `sendMacOSNotification`:

```go
var checkTerminalNotifierOnce sync.Once

func sendMacOSNotification(title, message string) error {
    checkTerminalNotifierOnce.Do(func() {
        if _, err := exec.LookPath("terminal-notifier"); err != nil {
            log.Println("TIP: Install terminal-notifier for actionable notifications: brew install terminal-notifier")
        }
    })
    // ... rest unchanged
}
```

Add `"sync"` import.

- [ ] **Step 4: Fix ignored io.ReadAll error in daemonStopCmd**

In `daemon.go`:

```go
body, err := io.ReadAll(resp.Body)
if err != nil {
    fmt.Printf("Warning: partial API response: %v\n", err)
}
```

- [ ] **Step 5: Build and test**

Run: `/opt/homebrew/bin/go build ./... && /opt/homebrew/bin/go test ./...`
Expected: no build errors, all tests pass

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "fix: zombie processes, data race, ignored errors (WR-03/04/05)
- openDashboard: cmd.Wait() in goroutine to reap browser process
- dashboardURL: use atomic.Value for data-race-free access
- init: replace with sync.Once for lazy terminal-notifier check
- daemonStopCmd: check io.ReadAll error instead of discarding"
```

---

### Task 7: Remove dead code

**Files:**
- Modify: `internal/tray/tray.go` — remove `Run()` method
- Modify: `internal/tray/subprocess.go` — remove `WaitForShutdown()`
- Modify: `internal/cli/root.go` — remove `GetLogFile()`

- [ ] **Step 1: Remove TrayApp.Run()**

Delete lines 96-120 from `tray.go` (the `Run()` method). Also remove the `"time"` import if it becomes unused after removing `Run()`.

- [ ] **Step 2: Remove WaitForShutdown()**

Delete lines 119-125 from `subprocess.go`. Also remove `"os/signal"` and `"os"` imports if they become unused.

- [ ] **Step 3: Remove GetLogFile() and IsVerbose()**

Delete lines 145-153 from `root.go` (both `IsVerbose()` and `GetLogFile()`). Also check if `"log"` import is still needed.

- [ ] **Step 4: Build and test**

Run: `/opt/homebrew/bin/go build ./... && /opt/homebrew/bin/go test ./...`
Expected: no build errors, all tests pass (dead code removal shouldn't affect tests)

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor: remove dead code (~50 lines)
- TrayApp.Run() — unused (RunBlocking used instead)
- WaitForShutdown() — unused (tray_cmd.go has own signal handling)
- GetLogFile() — unused (log file is internal to root.go)
- IsVerbose() — unused externally"
```

---

### Task 8: Hide --foreground flag

**Files:**
- Modify: `internal/cli/daemon.go`

- [ ] **Step 1: Mark --foreground as hidden**

In `init()`, after registering the flag:

```go
daemonRunCmd.Flags().BoolVar(&daemonForeground, "foreground", false, "Run in foreground (used internally)")
daemonRunCmd.Flags().MarkHidden("foreground")
```

- [ ] **Step 2: Build and test**

Run: `/opt/homebrew/bin/go build ./... && /opt/homebrew/bin/go test ./...`
Expected: no build errors, tests pass

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "chore: hide internal --foreground flag (IN-04)
Flag is used internally for daemon background fork re-exec. Not
useful for end users — mark as hidden."
```

---

### Task 9: Fix font loading for airgapped environments

**Files:**
- Modify: `internal/api/dashboard/index.html`

- [ ] **Step 1: Add fallback when Google Fonts unavailable**

Replace the `@import` Google Fonts link with a dual strategy: try loading from Google Fonts with a `link rel="stylesheet"`, and if that fails (or offline), fall back to system fonts. The cleanest approach: keep the `@import` but add better system font fallbacks in the CSS variables:

```css
--font-display: 'Titillium Web', -apple-system, 'Helvetica Neue', 'Segoe UI', Roboto, sans-serif;
--font-body: 'Source Sans 3', -apple-system, 'Helvetica Neue', 'Segoe UI', Roboto, Arial, sans-serif;
```

(This is already done — verify it's correct.)

- [ ] **Step 2: Build and test**

Run: `/opt/homebrew/bin/go build ./... && /opt/homebrew/bin/go test ./...`
Expected: no build errors, tests pass

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "chore: improve dashboard font fallbacks
Better system font stack when Google Fonts unavailable
(airgapped/offline environments)."
```

---

## Self-Review Checklist

- [x] **CR-01** (subprocess deadlock) → Task 1
- [x] **CR-02** (double cmd.Wait) → Task 1
- [x] **CR-03** (osascript injection) → Task 2
- [x] **CR-04** (batch silent drop) → Task 3
- [x] **CR-05** (OOB panic in Deserialize) → Task 4
- [x] **WR-01** (AppKit thread affinity) → Task 5
- [x] **WR-03** (zombie processes) → Task 6
- [x] **WR-04** (dashboardURL data race) → Task 6
- [x] **WR-05** (ignored error) → Task 6
- [x] **WR-06/07/08** (dead code) → Task 7
- [x] **IN-04** (hidden flag) → Task 8
- [x] **IN-01** (init noise) → Task 6
- [ ] **WR-02** (os.Exit skips deferred cleanup) — accepted: tray subprocess is disposable, os.Exit(0) is intentional
- [ ] Test flakes (time.Sleep in tests) — deferred: lower priority, no functional impact
- [ ] Coverage gaps — deferred: Phase 4 concern

**Total:** ~200 lines changed across 7 files
