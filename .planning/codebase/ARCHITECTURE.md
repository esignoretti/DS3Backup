<!-- refreshed: 2026-04-29 -->
# Architecture

**Analysis Date:** 2026-04-29

## System Overview

DS3 Backup is a CLI-based secure backup tool that implements a streaming pipeline: **scan → dedup → compress → encrypt → upload** to S3-compatible storage. The system uses a local BadgerDB index for tracking file metadata (hashes, paths, backup runs) and supports client-side AES-256-GCM encryption with Argon2id key derivation.

```text
┌─────────────────────────────────────────────────────────────────────────┐
│                          CLI Layer (cobra commands)                      │
│  `internal/cli/root.go` registers:                                      │
│    init | config | job add/list/delete | backup run/status/list         │
│    restore run/resume/status | index show/rebuild/clear | s3 lifecycle  │
└────────────────────────────────┬────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                      Application Layer (engines)                        │
│                                                                         │
│  ┌──────────────────┐    ┌──────────────────┐    ┌──────────────────┐   │
│  │  BackupEngine     │    │  RestoreEngine   │    │  RebuildEngine   │   │
│  │  scan→dedup→com-  │    │  concurrent      │    │  DR: download    │   │
│  │  press→encrypt→   │    │  download→       │    │  archive→extract │   │
│  │  upload (batch/   │    │  deserialize→    │    │  →restore config │   │
│  │  individual)      │    │  decrypt→write   │    │                  │   │
│  └────────┬──────────┘    └────────┬─────────┘    └────────┬─────────┘   │
│           │                       │                       │            │
└───────────┼───────────────────────┼───────────────────────┼────────────┘
            │                       │                       │
            ▼                       ▼                       ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                        Service Layer (internal)                         │
│                                                                         │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌──────────────┐   │
│  │  crypto      │ │  s3client    │ │  index       │ │  config      │   │
│  │  AES-256-GCM │ │  S3 API via  │ │  BadgerDB    │ │  JSON config │   │
│  │  zstd compr. │ │  AWS SDK v2  │ │  key-value   │ │  on disk     │   │
│  │  Argon2id KDF│ │  batch mgmt  │ │  store       │ │              │   │
│  └──────────────┘ └──────────────┘ └──────────────┘ └──────────────┘   │
└─────────────────────────────────────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                        External Services                                │
│                                                                         │
│  ┌──────────────────────────────┐    ┌────────────────────────────────┐ │
│  │  S3-Compatible Storage       │    │  Local Filesystem             │ │
│  │  (Cubbit DS3, MinIO, AWS)    │    │  Source files, config dir     │ │
│  │  S3 object layout:           │    │  ~/.ds3backup/               │ │
│  │  backups/<jobID>/            │    │    ├── config.json            │ │
│  │    files/<hash>.enc          │    │    ├── index/<jobID>/         │ │
│  │    batches/<batchID>.enc     │    │    └── state/<jobID>/         │ │
│  │    batches/<batchID>-manifest│    │                                │ │
│  │  .ds3backup/                 │    └────────────────────────────────┘ │
│  │    index/<jobID>/            │                                       │
│  │    jobs/<jobID>/config.json  │                                       │
│  │    encryption-salt.json      │                                       │
│  │    .ds3backup.tar.gz         │                                       │
│  │    .ds3backup.tar.gz.sha256  │                                       │
│  └──────────────────────────────┘                                       │
└─────────────────────────────────────────────────────────────────────────┘
```

## Component Responsibilities

| Component | Responsibility | File |
|-----------|----------------|------|
| CLI Root | Cobra command registration, config loading, logging setup | `internal/cli/root.go` |
| Init Command | First-time setup: S3 validation, master password, config creation | `internal/cli/init.go` |
| Backup Command | Orchestrates backup run, status, and history listing | `internal/cli/backup.go` |
| Restore Command | Orchestrates restore run, resume, status, verify, dry-run | `internal/cli/restore.go` |
| Job Command | CRUD for backup job configurations | `internal/cli/job.go` |
| Index Command | View, rebuild, clear local BadgerDB index | `internal/cli/index.go` |
| Config Command | Show, reset, validate configuration | `internal/cli/config_cmd.go` |
| S3 Command | Lifecycle policy, object lock check, object listing | `internal/cli/s3.go` |
| BackupEngine | Pipeline: scan → dedup → compress+encrypt → upload (batch/individual) | `internal/backup/engine.go` |
| Archive | tar.gz creation/extraction, SHA256 checksum utilities | `internal/backup/archive.go` |
| RestoreEngine | Concurrent download → decrypt+decompress → write → verify hash | `internal/restore/engine.go` |
| Downloader | Worker pool for concurrent restore jobs (v1) | `internal/restore/downloader.go` |
| DownloaderV2 | Worker pool with retry, resume support, atomic writes | `internal/restore/downloader_v2.go` |
| BatchExtractor | Cache-and-extract from batch archives | `internal/restore/extractor.go` |
| RestoreState | Persisted JSON state tracking for resumable restore | `internal/restore/state.go` |
| ProgressTracker | Thread-safe progress tracking with speed calculation | `internal/restore/progress.go` |
| Metadata | File metadata restoration (mod time, permissions) | `internal/restore/metadata.go` |
| CryptoEngine | Argon2id key derivation, AES-256-GCM encrypt/decrypt, zstd compress/decompress | `internal/crypto/crypto.go` |
| Master Password | AES-256-GCM encrypt/decrypt for job config passwords | `internal/crypto/master_password.go` |
| IndexDB | BadgerDB wrapper: file entries, hash dedup, backup runs | `internal/index/index.go` |
| Scan | Directory walking, BLAKE2b hashing, change detection | `internal/index/scan.go` |
| S3Client | AWS SDK v2 wrapper: Put/Get/List/Delete, Object Lock, lifecycle | `internal/s3client/client.go` |
| BatchBuilder | File batching logic: groups small files into single S3 objects | `internal/s3client/batch.go` |
| Config | JSON config file: S3, encryption, object lock, jobs | `internal/config/config.go` |
| Recovery | Disaster recovery: download archive, verify checksum, extract, restore config | `internal/recovery/rebuild.go` |
| Models | Data types: BackupJob, FileEntry, BackupRun, BatchManifest, RestoreOptions | `pkg/models/models.go`, `pkg/models/restore.go` |

## Pattern Overview

**Overall:** Modular CLI with layered service architecture

**Key Characteristics:**
- **Pipeline architecture** for backup: each stage (scan, dedup, compress+encrypt, upload) is sequential per file, but concurrent via S3 batch operations
- **Worker pool** for restore: configurable concurrent download/decrypt/write workers (default 8)
- **Dependency injection**: engines receive config, S3 client, index DB, and crypto engine via constructors
- **State persistence**: BadgerDB for file index, JSON files for config and restore state
- **Atomic file writes**: config and restore state use temp-file-then-rename pattern

## Layers

**CLI Layer:**
- Purpose: Command parsing, flag handling, user interaction (stdout/stderr output)
- Location: `internal/cli/`
- Contains: Cobra commands for every user-facing operation
- Depends on: All internal packages, pkg/models
- Used by: `cmd/ds3backup/main.go`

**Application Layer:**
- Purpose: Core backup/restore/recovery logic
- Location: `internal/backup/`, `internal/restore/`, `internal/recovery/`
- Contains: Pipeline orchestration, worker pools, file processing
- Depends on: Service layer (`internal/crypto/`, `internal/s3client/`, `internal/index/`, `internal/config/`), `pkg/models/`

**Service Layer:**
- Purpose: Low-level infrastructure services
- Location: `internal/crypto/`, `internal/s3client/`, `internal/index/`, `internal/config/`
- Contains: Encryption/compression, S3 API operations, BadgerDB persistence, config file management
- Depends on: `pkg/models/`, external SDKs

**Model Layer:**
- Purpose: Shared data types
- Location: `pkg/models/`
- Contains: Struct definitions for BackupJob, FileEntry, BackupRun, BatchManifest, RestoreOptions, etc.
- Depends on: Standard library only

## Data Flow

### Primary Backup Request Path

1. **CLI entry**: `ds3backup backup run <job-id>` → `cli/backup.go:backupRunCmd.RunE`
2. **Load config**: `loadConfig()` reads JSON from `~/.ds3backup/config.json` (`cli/root.go:loadConfig`)
3. **Find job**: `cfg.GetJob(jobID)` looks up the job by ID (`internal/config/config.go:GetJob`)
4. **Create S3 client**: `s3client.NewClient(cfg.S3)` connects to S3 via AWS SDK v2 (`internal/s3client/client.go:NewClient`)
5. **Create crypto engine**: `crypto.NewCryptoEngine(password, salt)` derives Argon2id key (`internal/crypto/crypto.go:NewCryptoEngine`)
6. **Open index DB**: `index.OpenIndexDB(path)` opens BadgerDB at `~/.ds3backup/index/<jobID>/` (`internal/index/index.go:OpenIndexDB`)
7. **Create backup engine**: `backup.NewBackupEngine(cfg, s3, index, crypto)` wires dependencies (`internal/backup/engine.go:NewBackupEngine`)
8. **Run pipeline**: `engine.RunBackup(job, fullBackup, progressCb)`: (`internal/backup/engine.go:RunBackup`)
   - **Step 1 - Scan**: `indexDB.ScanDirectory()` walks source path, computes BLAKE2b-256 hashes (`internal/index/scan.go:ScanDirectory`)
   - **Step 2 - Filter changed**: `indexDB.GetChangedFiles()` compares against index by modTime+size (`internal/index/scan.go:GetChangedFiles`)
   - **Step 3 - Dedup**: `indexDB.GetUniqueFilesToBackup()` groups by hash, skips duplicates (`internal/index/scan.go:GetUniqueFilesToBackup`)
   - **Step 4 - Process**: for each file: read disk → zstd compress → AES-256-GCM encrypt → serialize → batch or upload individually
   - **Step 4a - Small files**: added to `BatchBuilder`, uploaded as batch archives to `backups/<jobID>/batches/`
   - **Step 4b - Large files** (>1MB): uploaded individually to `backups/<jobID>/files/<hash>.enc` with Object Lock
   - **Step 5 - Final batch upload**: remaining batched files uploaded as single S3 object with manifest
   - **Step 6 - Save index**: all file entries saved to BadgerDB (by path and by hash)
   - **Step 7 - DR backup**: creates tar.gz of `~/.ds3backup/`, uploads to `.ds3backup.tar.gz` with SHA256 checksum
   - **Step 8 - Retention**: marks backup runs older than retention days for S3 lifecycle deletion
9. **Update job config**: `job.LastRun` timestamp saved to JSON config

### Restore Request Path

1. **CLI entry**: `ds3backup restore run <job-id> --password=xxx` → `cli/restore.go:restoreRunCmd.RunE`
2. **Open index DB**: at `~/.ds3backup/index/<jobID>/` via `index.OpenIndexDB()`
3. **Create restore engine**: `restore.NewRestoreEngine(cfg, s3, indexDB, crypto, jobID)` (`internal/restore/engine.go:NewRestoreEngine`)
4. **Get entries**: `indexDB.GetAllEntries(jobID)` reads all file entries from BadgerDB
5. **Filter (optional)**: apply include/exclude glob patterns
6. **Concurrent restore**: `NewDownloader(concurrency, s3, crypto, extractor, ctx)` creates worker pool (`internal/restore/downloader.go:NewDownloader`)
7. **Per-file pipeline** (in each worker):
   - If batched: `BatchExtractor.GetBatch()` downloads+caches the batch, `ExtractFile()` slices file data
   - If individual: `s3client.GetObject()` downloads from `backups/<jobID>/files/<hash>.enc`
   - `DeserializeEncryptedFile()` → `DecryptAndDecompress()` → `blake2b.Sum256()` hash verification → write to disk → restore metadata (mod time, permissions)
8. **Results collected**: files restored/skipped/failed, warnings, errors

### Restore Resume Path

1. **CLI entry**: `ds3backup restore resume <job-id> --password=xxx` → `cli/restore.go:restoreResumeCmd.RunE`
2. **Load state**: `restore.FindLatestRestoreState()` reads JSON from `~/.ds3backup/state/<jobID>/<sessionID>/restore-state.json`
3. **Get incomplete/failed files**: `state.GetIncompleteFiles()` or `state.GetFailedFiles()`
4. **Create DownloaderV2**: worker pool with retry logic (exponential backoff: 1s, 2s, 4s), atomic `.partial` writes, state tracking (`internal/restore/downloader_v2.go`)
5. **Per-file pipeline**: same as restore, but state is updated on each file completion/skip/failure

### Disaster Recovery Path

1. **CLI entry**: `ds3backup init --rebuild` → `cli/init.go:runRebuild()`
2. **Connect to S3**: using provided credentials (or existing config)
3. **Run rebuild**: `recovery.RunRebuild(ctx, s3Client, masterPassword)` (`internal/recovery/rebuild.go:RunRebuild`):
   - Remove existing `~/.ds3backup/`
   - Download `.ds3backup.tar.gz` from S3
   - Download `.ds3backup.tar.gz.sha256` and verify checksum
   - Extract tar.gz to `~/.ds3backup/`
   - Load config, validate/update master password
   - Load encryption salt from `.ds3backup/encryption-salt.json`
   - Discover jobs from config

### Point-in-Time Restore

1. Supports expressions like `2024-04-26T10:30:00Z`, `2024-04-26`, `1d`, `2h`, `1w`
2. `indexDB.GetRunByTime(jobID, targetTime)` finds the latest backup run at or before target time
3. `indexDB.GetEntriesForRun(jobID, targetRunTime)` gets file entries within 1-second tolerance
4. Only entries from that specific run are restored

**State Management:**
- Index state: BadgerDB key-value store at `~/.ds3backup/index/<jobID>/` (files saved by path and by hash for O(1) dedup)
- Config state: JSON file at `~/.ds3backup/config.json`
- Restore state: JSON file at `~/.ds3backup/state/<jobID>/<sessionID>/restore-state.json` (per-file status tracking)
- No global mutable state in application layer (thread-safe via mutexes in state/progress trackers)

## Key Abstractions

**BackupEngine (`internal/backup/engine.go`):**
- Purpose: Orchestrates the backup pipeline end-to-end
- Examples: `internal/backup/engine.go` (single file)
- Pattern: Constructor injection of config, S3 client, index DB, crypto engine

**RestoreEngine (`internal/restore/engine.go`):**
- Purpose: Orchestrates restore operations with multiple entry points (full restore, entries-only, dry run, verify, resume)
- Examples: `internal/restore/engine.go` (single file)
- Pattern: Factory method `NewRestoreEngine()`, delegates concurrent work to Downloader/DownloaderV2

**Downloader/DownloaderV2 (`internal/restore/downloader.go`, `internal/restore/downloader_v2.go`):**
- Purpose: Worker pool for parallel S3 download, decrypt, decompress, verify, write
- Examples: Both files
- Pattern: Channel-based job queue with goroutine workers, configurable concurrency

**IndexDB (`internal/index/index.go`):**
- Purpose: BadgerDB wrapper providing file entry CRUD, hash-based dedup, backup run history, point-in-time queries
- Examples: `internal/index/index.go`, `internal/index/scan.go`
- Pattern: Key-value store with structured keys (`file:<jobID>:<path>`, `hash:<jobID>:<hash>`, `run:<jobID>:<timestamp>`)

**CryptoEngine + EncryptedFile (`internal/crypto/crypto.go`):**
- Purpose: Per-file encryption with per-file derived keys (HMAC-based), Argon2id master key derivation, zstd compression
- Examples: `internal/crypto/crypto.go` (single file)
- Pattern: `CompressAndEncrypt` / `DecryptAndDecompress` symmetric pipeline, `Serialize` / `DeserializeEncryptedFile` for wire format

**S3Client (`internal/s3client/client.go`):**
- Purpose: AWS SDK v2 wrapper with path-style addressing (required for S3-compatible endpoints), Object Lock support, timeout handling
- Examples: `internal/s3client/client.go`, `internal/s3client/batch.go`
- Pattern: Constructed with `config.S3Config`, exposes S3 operations as simple Go methods

**RestoreState (`internal/restore/state.go`):**
- Purpose: Thread-safe persisted state for resumable restore, per-file status (pending/downloading/completed/failed/skipped)
- Examples: `internal/restore/state.go` (single file)
- Pattern: JSON serialization with mutex protection, atomic file writes

**Config.Config (`internal/config/config.go`):**
- Purpose: JSON configuration management with atomic writes
- Examples: `internal/config/config.go` (single file)
- Pattern: Single struct with S3, encryption, object lock, job fields; Load/Save methods with temp-file-rename

## Entry Points

**CLI main:**
- Location: `cmd/ds3backup/main.go`
- Triggers: User executes `ds3backup <command>`
- Responsibilities: Calls `cli.Execute()` which sets up logging, parses flags, dispatches to Cobra commands

**Backup Run:**
- Location: `internal/cli/backup.go` line 36 (`backupRunCmd`)
- Triggers: `ds3backup backup run <job-id> [--full] [--json]`
- Responsibilities: Full pipeline orchestration, progress reporting, result display

**Restore Run:**
- Location: `internal/cli/restore.go` line 42 (`restoreRunCmd`)
- Triggers: `ds3backup restore run <job-id> --password=xxx [--to=<path>] [--dry-run] [--verify] [--overwrite] [--include=...] [--exclude=...] [--time=...]`
- Responsibilities: Full restore pipeline, supports point-in-time, pattern filtering, verify-only, dry-run

**Init:**
- Location: `internal/cli/init.go` line 25 (`initCmd`)
- Triggers: `ds3backup init --endpoint=... --bucket=... --access-key=... --secret-key=... [--master-password=...]` or `ds3backup init --rebuild`
- Responsibilities: First-time setup or disaster recovery rebuild

**Disaster Recovery (rebuild):**
- Location: `internal/cli/init.go` line 195 (`runRebuild`)
- Triggers: `ds3backup init --rebuild`
- Responsibilities: Full DR from S3, restores all config and jobs

## Architectural Constraints

- **Threading:** Restore uses goroutine worker pools (channel-based job queues). Backup is single-threaded per file but batches files for S3 upload. No worker threads for background operations.
- **Global state:** CLI package uses package-level flag variables (`cli/root.go` lines 15-27). These are initialized once during Cobra command setup. No other global mutable state.
- **Circular imports:** None detected. Dependency graph is strictly layered: `cmd` → `cli` → `internal/{backup,restore,recovery,config,crypto,index,s3client}` → `pkg/models`
- **Index lock:** BadgerDB uses file-based locking; only one process can open an index at a time. The `backup run` command explicitly closes the index before returning.
- **S3 key layout:** Structured as `backups/<jobID>/files/<hash>.enc`, `backups/<jobID>/batches/<batchID>.enc`, `.ds3backup/` for metadata.
- **Concurrent restore limit:** Default concurrency is 4 for normal restore (reduced from 8 due to MinIO SDK timeout issues) and 8 for resume.

## Anti-Patterns

### Duplicated Progress Code in RestoreEngine

**What happens:** `RestoreWithProgress`, `RestoreEntries`, and the old `Restore` method all contain nearly identical worker-pool setup, result collection, and file-iteration logic. `ResumeRestore` also has its own similar loop (`internal/restore/engine.go` lines 311-521).
**Why it's wrong:** Code duplication increases maintenance burden; a bug fix in one method may not be applied to others.
**Do this instead:** Extract a shared `runRestorePipeline(entries, opts, tracker)` private method that all public restore entry points delegate to.

### BackupEngine.RunBackup Does Step 7 Twice

**What happens:** The disaster recovery backup creation (`createDisasterRecoveryBackup`) runs at line 237 AND again at line 253 in `engine.go`.
**Why it's wrong:** Duplicate execution wastes time and bandwidth; the first call's result is overwritten by the second.
**Do this instead:** Remove the second call (lines 252-259) — the first at line 237 is sufficient.

### BatchBuilder Upload Skips Object Lock

**What happens:** `BatchBuilder.Upload()` at `internal/s3client/batch.go` line 106 calls `client.PutObject()` (no Object Lock) instead of `PutObjectWithLock()`.
**Why it's wrong:** Batch uploads will not have Object Lock protection, creating an inconsistency where individual large files are protected but batches of small files are not.
**Do this instead:** Accept Object Lock mode/retention in `Upload()` and call `PutObjectWithLock()`.

### Config Stores Encryption Password in Plaintext in Memory

**What happens:** `BackupJob.EncryptionPassword` (`pkg/models/models.go` line 15) is stored as a string in the JSON config and loaded/saved in plaintext.
**Why it's wrong:** If the config file is read by another process or the file permissions are misconfigured, passwords are exposed.
**Do this instead:** Use the master password to encrypt job passwords in the config file, or use OS keychain integration.

### Duplicate formatBytes Functions

**What happens:** `formatBytes` exists in both `internal/backup/engine.go` (line 414) and `internal/restore/progress.go` (line 95), and in `internal/cli/utils.go` (line 19).
**Why it's wrong:** Three copies of the same byte-formatting utility function.
**Do this instead:** Place a single `formatBytes` in a shared location (e.g., `pkg/models/` or `internal/util/`).

## Error Handling

**Strategy:** Go-style explicit error returns. CLI commands return errors to Cobra for stderr display.

**Patterns:**
- Engines return `(*Result, error)` tuples; non-fatal errors are collected in result fields (e.g., `BackupRun.FilesFailed`, `RestoreResult.Warnings`)
- Critical errors (bad config, S3 connection failure, decryption failure) abort immediately
- Non-critical errors (individual file read/upload failures) are logged and counted, pipeline continues
- Context cancellation propagated for clean shutdown

## Cross-Cutting Concerns

**Logging:** Standard library `log` package; output to file at `~/.ds3backup/ds3backup.log`; verbose mode additionally writes to stderr
**Validation:** Config validation includes S3 connectivity test, bucket existence, Object Lock support, source path existence
**Authentication:** S3 credentials (access key + secret key) stored in config; encryption uses per-job password + optional master password

---

*Architecture analysis: 2026-04-29*
