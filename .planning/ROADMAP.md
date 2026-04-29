# Roadmap: DS3 Backup

## Overview

From a CLI-only secure backup tool to a full-featured backup solution with scheduling, API access, desktop UI, and enterprise management features.

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [x] **Phase 1: Foundation & Restore** — Core backup/recovery CLI, encryption, S3 integration, restore pipeline, disaster recovery
- [x] **Phase 2: Scheduling & Server** — Background scheduler, HTTP REST API, system tray, auto-backup daemon
- [ ] **Phase 3: Desktop UI** — Cross-platform tray app with notifications, history visualization, one-click restore
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

### Phase 2: Scheduling & Server
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
- [x] 02-01-PLAN.md — Scheduler engine + cron-based backup execution (will-execute)
- [x] 02-02-PLAN.md — REST API server for daemon control ✅ DONE
- [x] 02-03-PLAN.md — Daemon mode + system tray integration (will-execute)
- [x] 02-04-PLAN.md — Tests, auto-start, and dependency cleanup ✅ DONE

### Phase 3: Desktop UI (Planned)
**Goal**: Cross-platform desktop application with progress notifications, backup history visualization, and one-click restore
**Depends on**: Phase 2 (API for backend)
**Requirements**: UI-01, UI-02, UI-03
**Success Criteria** (what must be TRUE):
  1. User can view backup status and history in a graphical window
  2. User can configure jobs and trigger backups from the UI
  3. Desktop notifications on backup completion/failure
**Plans**: TBD

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
Phases execute in numeric order: 1 → 2 → 3 → 4

| Phase | Plans | Status | Completed |
|-------|-------|--------|-----------|
| 1. Foundation & Restore | Multiple | ✅ Complete | 2026-04-29 |
| 2. Scheduling & Server | 4/4 executed | ✅ Complete | 2026-04-29 |
| 3. Desktop UI | TBD | 📋 Planned | - |
| 4. Enterprise & Polish | TBD | 📋 Planned | - |
