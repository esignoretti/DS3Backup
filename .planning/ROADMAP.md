# Roadmap: DS3 Backup

## Overview

From a CLI-only secure backup tool to a full-featured backup solution with scheduling, API access, desktop UI, and enterprise management features.

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (1.5, 2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [x] **Phase 1: Foundation & Restore** — Core backup/recovery CLI, encryption, S3 integration, restore pipeline, disaster recovery
- [x] **Phase 1.5: Refactor Backup & Restore** (INSERTED) — Tech debt, bugs, performance, missing features in backup/restore pipeline ✅ Complete
- [x] **Phase 2: Scheduling & Server** — Background scheduler, HTTP REST API, system tray, auto-backup daemon ✅ Complete
- [x] **Phase 2.5: Advanced Scheduler** (INSERTED) — Retry logic, overlap prevention, missed-schedule catch-up, multi-schedule per job ✅ Complete
- [x] **Phase 3: Desktop UI** — Cross-platform tray app with notifications, history visualization, one-click restore ✅ Complete
- [x] **Phase 3.1: UI Scheduling Update** (INSERTED) — Dashboard now shows multi-schedule, retry info, interval expressions; API exposes retry fields ✅ Complete
- [x] **Phase 3.2: UI Bug Fixes & Polish** — Browse null error, restore deadlock, PIT restore via API, mkdir support, card sizing ✅ Complete
- [x] **Phase 3.3: Tray Icon & Menu Improvements** — DISPLAY gate fix, status-aware icons, dynamic per-job menu, reliable notifications, RunInProgress ✅ Complete
- [ ] **Phase 4: Enterprise & Polish** — Multi-target storage, audit logging, advanced monitoring

## Phase Details

### Phase 1: Foundation & Restore (SHIPPED)
**Goal**: Working CLI backup/restore tool with encryption, dedup, S3 Object Lock, and disaster recovery
**Depends on**: Nothing (first phase)
**Plans**: Multiple plans (see changelog in README.md)

Progress tracked in README.md changelog (v0.0.1 - v0.0.7).
Implementation spans: init, config, job CRUD, backup pipeline, restore pipeline (MVP → selective → PIT → resume), disaster recovery rebuild.

<details>
<summary>✅ Phase 1 — SHIPPED (v0.0.1 - v0.0.7)</summary>

Delivered:
- Core backup with incremental/full modes
- AES-256-GCM encryption + Argon2id KDF + zstd compression
- S3 Object Lock (GOVERNANCE/COMPLIANCE/NONE)
- BadgerDB local index with file dedup
- File batching for S3 cost optimization
- CLI: init, config, job add/list/delete, backup run/status/list
- Restore: MVP (Phase 4.1), selective+progress (4.2), PIT (4.3), resume (4.4 Part 1)
- Disaster recovery: init --rebuild, tar.gz archive + SHA256 verification
- CLI: index show/rebuild/clear, s3 lifecycle/ls/check-object-lock
</details>

### Phase 1.5: Refactor Backup & Restore (INSERTED)
**Goal**: Fix all documented tech debt, bugs, and missing features in the backup and restore pipeline
**Depends on**: Phase 1 (codebase to refactor)
**Requirements**: REFACTOR-01, REFACTOR-02, REFACTOR-03
**Success Criteria** (what must be TRUE):
  1. Duplicate disaster recovery backup call removed
  2. Object Lock mode from job config is respected for uploads (not hardcoded GOVERNANCE)
  3. Retention policy actually deletes expired objects
  4. BadgerDB v3 dependency removed
  5. Restore worker patterns consolidated into shared pipeline (~400 fewer lines)
  6. Formatting utilities centralized in shared package
  7. RebuildEngine stubs removed
  8. Config not saved with updated LastRun on failed backup
  9. VERSION files synchronized to 0.0.7
  10. S3 lifecycle API no longer a stub — real Put/GetBucketLifecycleConfiguration
  11. Index rebuild actually works — scans S3 manifests, reconstructs index
  12. Batch uploads use Object Lock
  13. macOS BadgerDB lock issue documented as accepted

**Plans**: 3 plans

Plans:
- [x] 1.5-01-PLAN.md — Bug fixes, deps, formatting consolidation, VERSION
- [x] 1.5-02-PLAN.md — S3 lifecycle API, retention enforcement, batch Object Lock
- [x] 1.5-03-PLAN.md — Restore pipeline refactor, stub removal, index rebuild

### Phase 2: Scheduling & Server (SHIPPED)
**Goal**: Daemon-mode backup with cron scheduling, HTTP API for programmatic control, and system tray integration
**Depends on**: Phase 1 (Foundation & Restore)
**Requirements**: SCHED-01, SCHED-02, API-01, API-02, TRAY-01, TRAY-02
**Success Criteria** (what must be TRUE):
   1. User can schedule periodic backups with cron expressions
   2. User can control the backup daemon via a local HTTP API
   3. Scheduled backups run automatically without CLI interaction
   4. User can monitor backup status and history via a system tray icon
   5. Daemon starts on system boot (user login)

**Plans**: 4 plans

Plans:
- [x] 02-01-PLAN.md — Scheduler engine + cron-based backup execution ✅ Done
- [x] 02-02-PLAN.md — REST API server for daemon control ✅ Done
- [x] 02-03-PLAN.md — Daemon mode + system tray integration ✅ Done
- [x] 02-04-PLAN.md — Tests, auto-start, and dependency cleanup ✅ Done

### Phase 2.5: Advanced Scheduler (INSERTED)
**Goal**: Extend the existing cron-based scheduler with resilience, observability, and control features — retry logic, overlap prevention, missed-schedule handling, multi-schedule per job, and job concurrency policy.
**Depends on**: Phase 2 (scheduler, API, daemon), Phase 1.5 (refactored pipeline)
**Requirements**: SCHED-02
**Success Criteria** (what must be TRUE):
   1. Jobs support a list of cron expressions instead of a single one
   2. Failed scheduled backups retry 3 total attempts at 1-minute intervals
   3. Overlapping scheduled occurrences are skipped (per-job mutex)
   4. Different jobs run in fully parallel goroutines
   5. On daemon restart, missed schedules are logged but not executed
   6. Scheduler persists its last-checkpoint time
   7. Panic in scheduled job is recovered gracefully

**Plans**: 2 plans

Plans:
- [x] 2.5-01-PLAN.md — Multi-schedule per job: CronExprs []string, API backward compat, scheduler engine updates ✅ Done
- [x] 2.5-02-PLAN.md — Retry & resilience: retry runner, overlap prevention, missed-log, checkpoint persistence, tests ✅ Done

### Phase 3: Desktop UI (Planned)
**Goal**: Cross-platform desktop application with progress notifications, backup history visualization, and one-click restore
**Depends on**: Phase 2 (API for backend)
**Requirements**: UI-01, UI-02, UI-03
**Success Criteria** (what must be TRUE):
  1. User can view backup status and history in a graphical window
  2. User can configure jobs and trigger backups from the UI
  3. Desktop notifications on backup completion/failure
**Plans**: 3 plans

Plans:
- [x] 03-01-PLAN.md — API enhancements: history endpoint + dashboard static serving + embed infrastructure
- [x] 03-02-PLAN.md — Web dashboard SPA: single-file HTML+CSS+JS dashboard served at GET /
- [x] 03-03-PLAN.md — Tray wiring + notifications: open dashboard in browser, per-job backup items, notification integration

### Phase 3.1: UI Scheduling Update (SHIPPED)
**Goal**: Update the desktop dashboard UI to expose advanced scheduling features — multi-schedule display, retry state visibility, interval expression support (@every), and proper multi-schedule editing in the add-job and reschedule flows.
**Depends on**: Phase 3 (dashboard SPA, API), Phase 2.5 (retry, multi-schedule)
**Requirements**: UI-01, UI-02
**Success Criteria** (what must be TRUE):
    1. Dashboard displays all cron expressions per job (not just the first)
    2. Dashboard shows retry count and next retry time for jobs with failed scheduled backups
    3. @every interval expressions display as human-readable "Every Xh" labels
    4. Add-job modal accepts multiple comma-separated cron expressions
    5. Reschedule button submits cronExprs array (not deprecated single cronExpr)
    6. API exposes RetryCount and NextRetryTime in BackupJobWithStatus
    7. go build and go test all pass

**Plans**: 2 plans

Plans:
- [x] 3.1-01-PLAN.md — API layer: expose retry/multi-schedule fields, @every handling in sanitizeJob ✅ Done
- [x] 3.1-02-PLAN.md — Dashboard SPA: multi-schedule display, retry info, @every support, cronExprs editing ✅ Done

### Phase 3.2: UI Bug Fixes & Polish (SHIPPED)
**Goal**: Fix browse null error, restore deadlock, add point-in-time restore via API, mkdir support, card sizing, restore filtering options.
**Depends on**: Phase 3.1
**Requirements**: — (ad-hoc fixes)
**Success Criteria** (what must be TRUE):
    1. Browse modal shows directories without null error
    2. Restore from API works for jobs with >8 files (no deadlock)
    3. Point-in-time restore works via API (backup run selector)
    4. New folder can be created in browse modal
    5. Cards have equal height with actions aligned at bottom
    6. Restore modal includes overwrite, include/exclude filters

### Phase 3.3: Tray Icon & Menu Improvements (SHIPPED)
**Goal**: Fix macOS tray startup bug (DISPLAY gate), add status-aware icon colors, dynamic per-job menu, reliable notifications, running-job indicator.
**Depends on**: Phase 3 (desktop UI, tray app)
**Requirements**: — (ad-hoc fixes)
**Success Criteria** (what must be TRUE):
    1. Tray icon appears on macOS (DISPLAY env variable bug fixed)
    2. Tray icon color changes: idle (blue), running (green), error (red)
    3. Per-job menu items show ✅/❌/⏳ status and update dynamically
    4. Jobs added/removed at runtime reflected in menu
    5. Notifications try terminal-notifier before osascript fallback
    6. RunInProgress exposed via API for tray consumption
    7. go build and go test all pass

**Executed**: 1 plan (ad-hoc — implemented directly)

### Phase 4: Enterprise & Polish (Planned)
**Goal**: Multi-target storage, advanced monitoring, performance optimization, and security hardening
**Depends on**: Phase 3
**Requirements**: ENTERPRISE-01, ENTERPRISE-02, SEC-01, PERF-01
**Success Criteria** (what must be TRUE):
   1. User can back up to multiple S3 targets
   2. Audit log of all operations is available
   3. Fix outstanding tech debt items from codebase analysis
**Plans**: TBD

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 1.5 → 2 → 2.5 → 3 → 3.1 → 4

| Phase | Plans | Status | Completed |
|-------|-------|--------|-----------|
| 1. Foundation & Restore | Multiple | ✅ Complete | 2026-04-29 |
| 1.5. Refactor Backup & Restore | 3/3 executed | ✅ Complete | 2026-04-30 |
| 2. Scheduling & Server | 4/4 executed | ✅ Complete | 2026-04-29 |
| 2.5. Advanced Scheduler | 2/2 executed | ✅ Complete | 2026-04-30 |
| 3. Desktop UI | 3/3 executed | ✅ Complete | 2026-04-29 |
| 3.1. UI Scheduling Update | 2/2 executed | ✅ Complete | 2026-05-04 |
| 3.2. UI Bug Fixes & Polish | — | ✅ Complete | 2026-05-07 |
| 3.3. Tray Icon & Menu Improvements | 1/1 executed | ✅ Complete | 2026-05-07 |
| 4. Enterprise & Polish | TBD | 📋 Planned | - |
