# Over-Engineering Audit — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use `- [ ]` syntax.

**Goal:** Delete ~660 lines of dead/speculative code, simplify interfaces, remove throwaway binaries. Net reduction across ~8 files.

**Architecture:** 6 independent tasks. Build + test after each. No new dependencies.

---

### Task 1: Delete throwaway test mains

**Files:** `cmd/check_s3/main.go` (63 lines), `cmd/test_archive/main.go` (38 lines)

These are not wired into the main binary. Hardcoded paths, debugging only.

- [ ] Delete `cmd/check_s3/main.go`
- [ ] Delete `cmd/test_archive/main.go`
- [ ] Build + test
- [ ] Commit: `delete: throwaway test mains (101 lines)`

---

### Task 2: Remove dead functions

**Files:**
- Modify: `internal/crypto/master_password.go`
- Modify: `internal/backup/archive.go`
- Modify: `internal/restore/state.go`
- Modify: `pkg/models/restore.go`
- Modify: `internal/recovery/rebuild.go`

**Changes:**

- `master_password.go`: Delete `HashPassword()` (line 168). 0 callers.
- `archive.go`: Delete `VerifySHA256()` (line 193) + `VerifySHA256FromReader()` (line 207). 0 callers.
- `state.go`: Delete `GetIncompleteFiles()`, `GetFailedFiles()`, `GetPendingFiles()`, `GetPartialDirectory()`. 0 callers. Keep `RestoreState` struct and other methods (used by downloader_v2).
- `models/restore.go`: Delete `RestoreProgress` struct — 0 references in codebase.
- `rebuild.go`: Delete `discoverJobsFromConfig()` (line 148). Inline `return cfg.Jobs, nil` at call site.

- [ ] Remove each dead function/struct
- [ ] Build + test
- [ ] Commit: `delete: dead functions (HashPassword, VerifySHA256, etc.)`

---

### Task 3: Delete cli/utils.go — pointless delegation layer

**Files:** `internal/cli/utils.go` (15 lines)

Two functions that just delegate to `util.FormatBytes` / `util.FormatDuration`. All 4 callers in `cli/` can call `util` directly.

- [ ] Replace `formatBytes(x)` with `util.FormatBytes(x)` in callers (3 occurrences in cli/ files)
- [ ] Replace `formatDuration(x)` with `util.FormatDuration(x)` in callers (1 occurrence)
- [ ] Delete `internal/cli/utils.go`
- [ ] Build + test
- [ ] Commit: `delete: cli/utils.go delegation layer`

---

### Task 4: Inline progress display helpers — 0 external callers

**Files:** `internal/restore/progress.go`

`FormatPath()`, `FormatSpeed()`, `ClearLine()`, `ProgressLine()`, `SummaryLine()` are called 0 times from outside `progress.go`. They exist so callers COULD use them but don't. Move into callers or delete.

Check: `ProgressTracker.Update()`, `Status()`, `Final()` are used — keep those (~80 lines). Delete the 5 display helpers (~45 lines).

- [ ] Delete the 5 display-only functions
- [ ] Build + test
- [ ] Commit: `delete: unused progress display helpers (45 lines)`

---

### Task 5: Simplify AddFile signature — error always nil

**Files:** `internal/s3client/batch.go`

`AddFile()` returns `(bool, error)` but `error` is always nil (line 62). Simplifies caller in `engine.go`.

- [ ] Change return to `(bool)`
- [ ] Remove error check in `engine.go` caller
- [ ] Build + test
- [ ] Commit: `shrink: AddFile returns bool not (bool, error)`

---

### Task 6: Mark speculative ResumeRestore as known debt, shrink what's safe now

**Files:** `internal/restore/` (downloader_v2.go 286 lines, state.go 376 lines = ~660 lines)

**Honest assessment:** `ResumeRestore` is wired to a CLI command and has callers. It's not dead code — but it's speculative. The feature adds ~660 lines for an edge case (network drops mid-restore) no user ever asked for. The original `Downloader` already handles retries if you just re-run.

**Can't delete** without removing CLI commands users may depend on. But can shrink:

- `downloader_v2.go`: Delete `processJobWithRetry`'s exponential backoff (lines 134-143 collapse). Simplify `isNonRetryableError` + `containsAny` — replace with `strings.Contains` inline. Delete `DownloadJob`+`DownloadResult` types — reuse `Job`+`Result` from downloader.go (identical fields, just extra `DestPath`/`Retries`).

**~40 lines shrink possible**, rest is structural.

- [ ] Reuse `Job`/`Result` from downloader.go instead of duplicating as `DownloadJob`/`DownloadResult`
- [ ] Collapse exponential backoff to fixed 2s delay
- [ ] Inline `isNonRetryableError`+`containsAny`
- [ ] Build + test
- [ ] Commit: `shrink: deduplicate downloader types, simplify retry logic`

---

### Summary

| Task | Lines removed | Type |
|------|-------------|------|
| 1. Throwaway mains | 101 | delete |
| 2. Dead functions | ~55 | delete |
| 3. cli/utils.go | 15 | delete |
| 4. Progress display helpers | 45 | delete |
| 5. AddFile signature | 1 | shrink |
| 6. DownloaderV2 shrink | ~40 | shrink |
| **Total** | **~257** | |

~660 more lines of speculative resume feature flagged as known debt but kept (wired to CLI commands).

**Not touched:** `github.com/getlantern/systray` cascade of 6 indirect deps — active code, not dead.
