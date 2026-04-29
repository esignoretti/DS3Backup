# Codebase Structure

**Analysis Date:** 2026-04-29

## Directory Layout

```
ds3backup/
├── cmd/
│   ├── ds3backup/
│   │   └── main.go                 # Entry point
│   ├── check_s3/                   # S3 connectivity check utility
│   └── test_archive/               # Archive testing utility
├── internal/
│   ├── cli/                        # Cobra CLI commands
│   │   ├── root.go                 # Root command, logging, config loading
│   │   ├── backup.go               # backup run/status/list commands
│   │   ├── restore.go              # restore run/resume/status commands
│   │   ├── init.go                 # init command (setup + --rebuild)
│   │   ├── config_cmd.go           # config show/reset/validate commands
│   │   ├── job.go                  # job add/list/delete commands
│   │   ├── index.go                # index show/rebuild/clear commands
│   │   ├── s3.go                   # s3 lifecycle/ls/check-object-lock commands
│   │   ├── utils.go                # formatBytes, formatDuration helpers
│   │   └── version.go              # Version constant
│   ├── backup/
│   │   ├── engine.go               # BackupEngine — full backup pipeline
│   │   └── archive.go              # tar.gz utilities, SHA256 checksums
│   ├── restore/
│   │   ├── engine.go               # RestoreEngine — full restore pipeline
│   │   ├── downloader.go           # Downloader — worker pool (v1)
│   │   ├── downloader_v2.go        # DownloaderV2 — worker pool with retry (v2)
│   │   ├── extractor.go            # BatchExtractor — batch archive extraction
│   │   ├── state.go                # RestoreState — persisted resumable state
│   │   ├── progress.go            # ProgressTracker — progress tracking UI
│   │   └── metadata.go            # File metadata restoration (mod time, perms)
│   ├── crypto/
│   │   ├── crypto.go              # CryptoEngine: Argon2id → AES-256-GCM + zstd
│   │   └── master_password.go     # Master password encryption for job configs
│   ├── config/
│   │   └── config.go              # Config struct, Load/Save, job CRUD
│   ├── index/
│   │   ├── index.go               # IndexDB (BadgerDB wrapper): entries, runs, history
│   │   └── scan.go                # Directory scanning, BLAKE2b hashing, dedup
│   ├── s3client/
│   │   ├── client.go              # AWS SDK v2 S3 wrapper: Put/Get/List/Delete
│   │   └── batch.go               # BatchBuilder: small file batching logic
│   └── recovery/
│       └── rebuild.go             # Disaster recovery: download archive, verify, extract
├── pkg/
│   └── models/
│       ├── models.go              # Core models: BackupJob, FileEntry, BackupRun, BatchManifest
│       └── restore.go             # Restore models: RestoreOptions, RestoreResult, VerifyResult
├── .planning/
│   └── codebase/
│       ├── STACK.md
│       ├── INTEGRATIONS.md
│       ├── ARCHITECTURE.md
│       └── STRUCTURE.md
├── go.mod
├── go.sum
├── README.md
├── LICENSE
├── VERSION
└── *.md                            # Various documentation files
```

## Directory Purposes

**`cmd/ds3backup/`:**
- Purpose: Binary entry point
- Contains: `main.go` (9 lines — imports and calls `cli.Execute()`)
- Key files: `cmd/ds3backup/main.go`

**`internal/cli/`:**
- Purpose: All user-facing CLI commands
- Contains: 10 files, one per command group plus root setup, utilities, and version
- Key files:
  - `root.go` — global flags (`--config`, `--verbose`), logging setup, `Execute()` and `initConfig()`
  - `backup.go` — `ds3backup backup run <job-id>`, `backup status`, `backup list`
  - `restore.go` — `ds3backup restore run`, `restore resume`, `restore status`
  - `init.go` — `ds3backup init` (first-time setup) and `ds3backup init --rebuild` (DR)
  - `config_cmd.go` — `ds3backup config show/reset/validate`
  - `job.go` — `ds3backup job add/list/delete`
  - `index.go` — `ds3backup index show/rebuild/clear`
  - `s3.go` — `ds3backup s3 lifecycle-set/lifecycle-get/check-object-lock/ls`
  - `utils.go` — `formatBytes()`, `formatDuration()` helpers
  - `version.go` — `const Version = "0.0.6"`

**`internal/backup/`:**
- Purpose: Backup pipeline implementation
- Contains: 2 files — engine and archive utilities
- Key files:
  - `engine.go` — `BackupEngine` struct, `RunBackup()` method implementing the 8-step pipeline
  - `archive.go` — `CreateBackupArchive()`, `ExtractBackupArchive()`, SHA256 checksum functions

**`internal/restore/`:**
- Purpose: Restore pipeline implementation
- Contains: 7 files — engine, two downloader versions, extractor, state, progress, metadata
- Key files:
  - `engine.go` — `RestoreEngine` struct with `Restore()`, `DryRun()`, `Verify()`, `RestoreWithProgress()`, `RestoreEntries()`, `ResumeRestore()`
  - `downloader.go` — `Downloader` (v1) with `Job`/`Result` types, worker pool
  - `downloader_v2.go` — `DownloaderV2` with retry, atomic `.partial` writes, state updates
  - `state.go` — `RestoreState`/`FileState` with JSON persistence, session management
  - `progress.go` — `ProgressTracker` with thread-safe status/speed display
  - `extractor.go` — `BatchExtractor` with in-memory batch caching
  - `metadata.go` — `SetFileMetadata()` for mod time and permissions

**`internal/crypto/`:**
- Purpose: Encryption, compression, key derivation
- Contains: 2 files
- Key files:
  - `crypto.go` — `CryptoEngine` with `CompressAndEncrypt()`/`DecryptAndDecompress()`, binary `Serialize()`/`DeserializeEncryptedFile()` format
  - `master_password.go` — `EncryptWithMasterPassword()`/`DecryptWithMasterPassword()`, password checksum creation/verification

**`internal/config/`:**
- Purpose: Configuration management
- Contains: 1 file
- Key files:
  - `config.go` — `Config` struct (S3, Encryption, ObjectLock, Jobs), `LoadConfig()`, `SaveConfig()`, job CRUD, `ConfigDir()`, `DefaultConfigPath()`

**`internal/index/`:**
- Purpose: BadgerDB-based local index
- Contains: 2 files
- Key files:
  - `index.go` — `IndexDB` with `SaveEntry()`, `GetEntry()`, `GetByHash()`, `SaveRun()`, `GetBackupHistory()`, `GetAllEntries()`, `GetRunByTime()`, `GetEntriesForRun()`, `Backup()`, `Restore()`
  - `scan.go` — `ScanDirectory()`, `GetChangedFiles()`, `GetUniqueFilesToBackup()`, `calculateHash()` (BLAKE2b-256), `QuickHash()`, `shouldSkip()` filter

**`internal/s3client/`:**
- Purpose: S3-compatible storage client
- Contains: 2 files
- Key files:
  - `client.go` — `Client` wrapping AWS SDK v2 with `PutObject()`, `PutObjectWithLock()`, `GetObject()`, `ListObjects()`, `DeleteObject()`, `CheckObjectLockSupport()`, `SetLifecyclePolicy()`, `BucketExists()`
  - `batch.go` — `BatchBuilder` with configurable size/files limits, `Upload()` with manifest

**`internal/recovery/`:**
- Purpose: Disaster recovery
- Contains: 1 file
- Key files:
  - `rebuild.go` — `RunRebuild()` (8-step DR process), `LoadEncryptionSalt()`, `SaveJobMetadata()`, `RebuildEngine` (stub for backward compatibility)

**`pkg/models/`:**
- Purpose: Shared data types
- Contains: 2 files
- Key files:
  - `models.go` — `BackupJob`, `FileEntry`, `BackupRun`, `Schedule`, `BatchManifest`, `BatchFileRef`
  - `restore.go` — `RestoreOptions`, `RestoreProgress`, `RestoreResult`, `DryRunResult`, `VerifyResult`

**`.planning/codebase/`:**
- Purpose: GSD codebase analysis documents
- Contains: Generated analysis files (STACK.md, INTEGRATIONS.md, ARCHITECTURE.md, STRUCTURE.md)

## Key File Locations

**Entry Points:**
- `cmd/ds3backup/main.go`: Program entry — calls `cli.Execute()`
- `internal/cli/root.go`: `Execute()` function — logging setup, Cobra root command initialization
- `internal/config/config.go`: `ConfigDir()`, `DefaultConfigPath()` — configuration base path

**Configuration:**
- `~/.ds3backup/config.json`: Main configuration file (S3, encryption, object lock, jobs)
- `~/.ds3backup/ds3backup.log`: Log file (appended with timestamp on each run)
- `internal/config/config.go`: Config loading/saving logic

**Core Logic:**
- `internal/backup/engine.go`: Backup pipeline (lines 47-261)
- `internal/restore/engine.go`: Restore pipeline (lines 42-134)
- `internal/recovery/rebuild.go`: Disaster recovery (lines 21-145)
- `internal/crypto/crypto.go`: Encryption/compression (lines 72-171)
- `internal/index/scan.go`: File scanning and dedup (lines 25-124)

**Testing:**
- No test files found (`*_test.go` files are absent from the entire codebase)

**Database/Index:**
- `~/.ds3backup/index/<jobID>/`: Per-job BadgerDB index directory
- `internal/index/index.go`: Index DB wrapper
- `internal/index/scan.go`: Directory scanning and hashing

**State (Restore):**
- `~/.ds3backup/state/<jobID>/<sessionID>/restore-state.json`: Per-session restore state
- `internal/restore/state.go`: Restore state management

## Naming Conventions

**Files:**
- `snake_case.go`: All Go source files (e.g., `config.go`, `master_password.go`, `downloader_v2.go`)
- `UPPERCASE.md`: Planning documents (e.g., `STACK.md`, `ARCHITECTURE.md`)

**Directories:**
- Single-word lowercase: All packages (`cli`, `backup`, `restore`, `crypto`, `config`, `index`, `s3client`, `recovery`)

**Functions:**
- `camelCase`: Private/exported functions (e.g., `formatBytes`, `calculateHash`, `shouldSkip`, `runRebuild`)
- `PascalCase`: Exported functions/types (e.g., `RunBackup`, `CompressAndEncrypt`, `NewCryptoEngine`, `OpenIndexDB`)

**Types:**
- `PascalCase`: All struct types (e.g., `BackupEngine`, `RestoreState`, `CryptoEngine`, `FileEntry`)

**Variables:**
- `camelCase`: Go standard (e.g., `s3Client`, `cryptoEngine`, `indexDB`, `configDir`)

## Where to Add New Code

**New Feature (e.g., scheduling, notifications):**
- Primary code: `internal/<feature-name>/` (new package under `internal/`)
- CLI command: `internal/cli/<command>.go` (register in root command)

**New CLI Command:**
- Implementation: `internal/cli/<command>.go`
- Create Cobra command struct, add `init()` to register with parent command(s)
- Wire dependencies via `loadConfig()`, `s3client.NewClient()`, etc.

**New Engine/Service:**
- Implementation: `internal/<service>/` (e.g., `internal/scheduler/`)
- Interface types in `pkg/models/` if shared across layers

**New Model/Data Type:**
- Location: `pkg/models/models.go` or `pkg/models/<domain>.go` for related types

**New S3 Operation:**
- Method on `s3client.Client` in `internal/s3client/client.go`

**New Crypto Operation:**
- Method on `CryptoEngine` in `internal/crypto/crypto.go`

**New Disaster Recovery Feature:**
- Logic in `internal/recovery/rebuild.go`

**Utility Functions:**
- Shared helpers should be placed in a dedicated utility package — currently duplicated across `internal/cli/utils.go`, `internal/backup/engine.go`, and `internal/restore/progress.go`

## Command-to-File Mapping

| Command | File | Implementation |
|---------|------|----------------|
| `ds3backup` | `internal/cli/root.go` | Root command, version display, logging setup |
| `ds3backup init` | `internal/cli/init.go` | S3 validation, password setup, config creation |
| `ds3backup init --rebuild` | `internal/cli/init.go` | Calls `recovery.RunRebuild()` |
| `ds3backup config show` | `internal/cli/config_cmd.go` | Displays current config |
| `ds3backup config reset` | `internal/cli/config_cmd.go` | Removes config file, optional S3 wipe |
| `ds3backup config validate` | `internal/cli/config_cmd.go` | Tests S3 connection, bucket, Object Lock |
| `ds3backup job add` | `internal/cli/job.go` | Creates job, saves to config + S3 metadata |
| `ds3backup job list` | `internal/cli/job.go` | Lists all configured jobs |
| `ds3backup job delete` | `internal/cli/job.go` | Deletes job, optional S3 cleanup |
| `ds3backup backup run <id>` | `internal/cli/backup.go` | Calls `backup.NewBackupEngine().RunBackup()` |
| `ds3backup backup status <id>` | `internal/cli/backup.go` | Displays job info from config |
| `ds3backup backup list <id>` | `internal/cli/backup.go` | Lists backup history from index/S3 |
| `ds3backup restore run <id>` | `internal/cli/restore.go` | Calls `restore.NewRestoreEngine().Restore*()` |
| `ds3backup restore resume <id>` | `internal/cli/restore.go` | Calls `restore.RestoreEngine.ResumeRestore()` |
| `ds3backup restore status <id>` | `internal/cli/restore.go` | Loads restore state from disk |
| `ds3backup index show <id>` | `internal/cli/index.go` | Displays index statistics |
| `ds3backup index rebuild <id>` | `internal/cli/index.go` | Stub — not fully implemented |
| `ds3backup index clear <id>` | `internal/cli/index.go` | Removes local index directory |
| `ds3backup s3 lifecycle-set` | `internal/cli/s3.go` | Stub — prints manual instructions |
| `ds3backup s3 lifecycle-get` | `internal/cli/s3.go` | Stub — prints manual instructions |
| `ds3backup s3 check-object-lock` | `internal/cli/s3.go` | Checks Object Lock on bucket |
| `ds3backup s3 ls` | `internal/cli/s3.go` | Lists objects in S3 bucket |
| `ds3backup -v` / `--verbose` | `internal/cli/root.go` | Enables stderr logging |
| `ds3backup --config` | `internal/cli/root.go` | Custom config file path |

## Special Directories

**`.ds3backup/` (runtime, not in repo):**
- Purpose: User-level configuration directory at `$HOME/.ds3backup/`
- Contains: Config file, per-job BadgerDB index directories, restore state directories, log file
- Generated: Yes (by `ds3backup init` and backup/restore operations)
- Committed: No

**`.planning/`:**
- Purpose: GSD planning artifacts
- Contains: Codebase analysis documents
- Generated: Yes (by `/gsd-map-codebase`)
- Committed: Yes (intended to be committed)

**`cmd/check_s3/`, `cmd/test_archive/`:**
- Purpose: Development utilities for S3 connectivity testing and archive integrity testing
- Not part of the main binary

---

*Structure analysis: 2026-04-29*
