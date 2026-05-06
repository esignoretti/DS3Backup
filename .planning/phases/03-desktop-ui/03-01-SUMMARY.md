---
phase: 03-desktop-ui
plan: 01
subsystem: api
tags: [api, history, dashboard, embed]
requires: []
provides: [api-history-endpoint, dashboard-embed, HistoryProvider-interface]
affects: [internal/api, internal/cli]
tech-stack:
  added: [go:embed]
  patterns: [HistoryProvider interface pattern, dashboard embed via embed.FS]
key-files:
  created:
    - internal/api/dashboard/dashboard.go
    - internal/api/dashboard/index.html (placeholder)
    - internal/api/dashboard/.gitkeep
  modified:
    - internal/api/types.go
    - internal/api/server.go
    - internal/api/handlers.go
    - internal/api/router.go
    - internal/api/api_test.go
    - internal/cli/daemon.go
decisions:
  - HistoryProvider is passed as non-pointer interface (nil means no provider)
  - Dashboard is embedded via go:embed in a separate `dashboard` package
  - Dashboard placeholder (not built) returns minimal HTML fallback
  - Limit query param on history endpoint capped at 100
metrics:
  duration: ~4m
  completed: 2026-04-29
  tasks: 3
  commits: 3
  tests: 15 (11 existing + 3 new history + 1 lifecycle)
---

# Phase 03 Plan 01: API Layer Summary

API layer for desktop UI: backup history endpoint and dashboard embed infrastructure.

## Result

Added `HistoryProvider` interface, `HistoryResponse` type, history endpoint at `GET /api/v1/jobs/{id}/history`, and dashboard static file serving via `go:embed`.

## Tasks Executed

### Task 1: Add HistoryProvider interface, HistoryResponse type, and extend APIServer
- **Commit:** `5381710`
- Added `HistoryProvider` interface and `HistoryResponse` type to `types.go`
- Updated `APIServer` struct with `historyProvider` field
- Updated `NewAPIServer` signature to accept `HistoryProvider`
- Created `internal/api/dashboard/package` with `go:embed` support

### Task 2: Create dashboard embed infrastructure and history handler
- **Commit:** `6bca4a7`
- Added `handleGetJobHistory` handler with job validation, limit param, nil-provider fallback
- Registerd `GET /api/v1/jobs/{id}/history` and `GET /` dashboard route on mux

### Task 3: Update tests and daemon integration
- **Commit:** `e82576d`
- Updated `newTestServer` signature, all 11 existing test calls, and `NewAPIServer(0,...)` call
- Added `mockHistoryProvider` and 3 new tests: Found (2 runs), NoProvider (empty array), JobNotFound (404)
- Updated `daemon.go` to pass `nil` for history provider

## Verification

- `go build ./...` — compiles with no errors
- `go test ./internal/api/ -v -count=1` — all 15 tests pass
- All 8 existing tests pass unchanged
- 3 new history tests pass
- 1 lifecycle test passes

## Threat Surface

No new threats introduced. T-03-01 through T-03-03 all accept or mitigate as planned.

## Deviations from Plan

None — plan executed exactly as written.
