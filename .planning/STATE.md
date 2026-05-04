# Project State

## Project Reference

See: .planning/PROJECT.md (not yet created)

**Core value:** Provide a secure, S3-only backup solution with client-side encryption, scheduling, and an intuitive interface
**Current focus:** Phase 2.5 — Advanced Scheduler

## Current Position

Phase: 3.1 of 5 (UI Scheduling Update) — INSERTED
Plan: 2 of 2 in current phase
Status: Complete
Last activity: 2026-05-04 — All 2 Phase 3.1 plans executed

Progress: [██████████] 60% (overall project)
Note: Phase 1.5 complete. Phase 2 complete. Phase 2.5 complete. Phase 3 complete. Phase 3.1 complete. Next: Phase 4.

## Performance Metrics

**Velocity:**
- Total plans completed: 10
- Average duration: ~5m
- Total execution time: ~35m

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 1. Foundation & Restore | Multiple | ✅ Complete | N/A |
| 1.5. Refactor Backup & Restore | 3/3 | ✅ Complete | ~15m |
| 2. Scheduling & Server | 4/4 | ✅ Complete | ~7m 15s |
| 2.5. Advanced Scheduler | 2/2 | ✅ Complete | ~10m |
| 3. Desktop UI | 3/3 | ✅ Complete | ~4m |
| 3.1. UI Scheduling Update | 2/2 | ✅ Complete | ~5m |

## Accumulated Context

### Decisions

Key design decisions for Phase 2 (previous):
- D-01 through D-11: (See Phase 2 context — completed)

Key design decisions for Phase 1.5 (Refactor Backup & Restore):
- D-1.5-01: Remove duplicate DR backup call (engine.go line 252-259)
- D-1.5-02: Use `mode` parameter instead of hardcoded "GOVERNANCE" in PutObjectWithLock
- D-1.5-03: Implement direct S3 deletion in applyRetention for GOVERNANCE mode
- D-1.5-04: Remove badger/v3 dependency from go.mod
- D-1.5-05: Extract shared runRestorePipeline for restore worker pattern consolidation
- D-1.5-06: Centralize formatBytes/formatDuration in internal/util/format.go
- D-1.5-07: Remove obsolete RebuildEngine stubs
- D-1.5-08: Only save config with updated LastRun when err == nil
- D-1.5-09: Use version.go constant as source of truth (0.0.7)
- D-1.5-10: Implement real PutBucketLifecycleConfiguration/GetBucketLifecycleConfiguration
- D-1.5-11: Implement index rebuild from S3 batch manifests
- D-1.5-12: Use PutObjectWithLock in BatchBuilder.Upload
- D-1.5-13: Accept macOS BadgerDB lock issue (documented)

Key design decisions for Phase 3:
- D-12: HistoryProvider is passed as non-pointer interface (nil means no provider)
- D-13: Dashboard is embedded via go:embed in a separate `dashboard` package
- D-14: Dashboard is a single HTML file with all CSS and JS inlined — no build step, no external deps
- D-15: Dashboard auto-refreshes via setInterval at 5s, not WebSocket
- D-16: Browser opening uses exec.Command with platform-specific commands (open/xdg-open/rundll32)
- D-17: Per-job tray menu items created once on first status poll
- D-18: Notifications are best-effort (log failures silently)

### Pending Todos

| Priority | Item | Phase | Status |
|----------|------|-------|--------|
| High | Execute Phase 1.5 Plan 01 — Bug fixes, deps, formatting | 1.5 | ✅ Done |
| High | Execute Phase 1.5 Plan 02 — Lifecycle, retention, batch Object Lock | 1.5 | ✅ Done |
| Medium | Execute Phase 1.5 Plan 03 — Restore refactor, stubs, index rebuild | 1.5 | ✅ Done |
| High | Execute Phase 2.5 Plan 01 — Multi-schedule per job | 2.5 | ✅ Done |
| High | Execute Phase 2.5 Plan 02 — Retry & resilience | 2.5 | ✅ Done |

### Phase 3.1 Execution Summary

All 2 plans executed successfully:
- **Plan 01**: API layer — RetryCount/NextRetryTime fields on BackupJobWithStatus, dual cron parser (standard + enhanced with Descriptor) for @every/@daily/@hourly support, 3 new tests
- **Plan 02**: Dashboard SPA — formatCronExprs replaces formatCron, multi-schedule display, @every interval labels, retry info row, comma-separated cronExprs editing in add-job and reschedule

### Blockers/Concerns

None.

Key design decisions for Phase 2.5 (Advanced Scheduler):
- D-01: Failed scheduled backups retry 3 total attempts
- D-02: Retry interval fixed at 1 minute between attempts
- D-03: Final failure sets job.LastError, no further retries
- D-04: Panic recovered gracefully, treated as failure
- D-05: Overlapping occurrence SKIPPED (next run at next cron tick)
- D-06: Per-job mutex tracks run-in-progress before firing schedule
- D-07: On restart, missed schedules logged but NOT executed
- D-08: Scheduler persists last-checkpoint time for missed-schedule logging
- D-09: Different jobs run fully in parallel
- D-10: Single job cannot overlap with itself
- D-11: Jobs support list of cron expressions (CronExprs []string)
- D-12: Each expression fires same backup pipeline (no per-expr options)
- D-13: CronExpr becomes CronExprs []string (breaking model change)

### Phase 1.5 Execution Summary

All 3 plans executed successfully across 3 waves:
- **Plan 01**: Bug fixes (DR call, ObjectLock mode, config save), badger/v3 confirmation, VERSION sync, formatting consolidation
- **Plan 02**: Real S3 lifecycle API, retention enforcement, batch Object Lock
- **Plan 03**: Restore pipeline refactor (~118 lines saved), RebuildEngine stub removal, index rebuild from S3

## Deferred Items

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| *(none)* | | | |

## Session Continuity

Last session: 2026-04-30 (execution session)
Stopped at: Phase 1.5 fully executed (3/3 plans complete). Phase 2 and Phase 3 also complete.
Resume file: `/gsd-next` to advance to next phase
