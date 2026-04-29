# Coding Conventions

**Analysis Date:** 2026-04-29

## Naming Patterns

**Files:**
- Go source files use `snake_case.go` — e.g., `master_password.go`, `download_v2.go`, `config_cmd.go`
- Multiple files in the same package represent logical splits (e.g., `engine.go` + `archive.go` inside `backup/`)
- Test files are absent — no `_test.go` files exist anywhere in the codebase

**Functions:**
- **Exported functions**: PascalCase — `NewCryptoEngine`, `CompressAndEncrypt`, `RunBackup`, `SaveConfig`
- **Unexported functions**: camelCase — `deriveFileKey`, `shouldSkip`, `calculateHash`, `formatBytes`, `mustHomeDir`
- **Exported method receivers**: PascalCase — `func (c *Config) SaveConfig()`, `func (db *IndexDB) SaveEntry()`
- **Constructor pattern**: consistently `New{Type}()` — `NewBackupEngine`, `NewS3Client`, `NewCryptoEngine`, `NewDownloader`, `NewRestoreState`, `NewBatchBuilder`

**Variables:**
- **Local variables**: camelCase — `jobID`, `s3Client`, `configDir`, `hashKey`, `encryptedData`
- **Package-level vars**: camelCase — `cfgFile`, `fullBackup`, `jsonOutput`, `restorePassword`
- **Constants**: PascalCase for exported (`Version` in `internal/cli/version.go`), camelCase for unexported (`skipPatterns` in `scan.go`)
- **No ALL_CAPS or SCREAMING_SNAKE used** anywhere in the codebase

**Types (structs/interfaces):**
- **Exported structs**: PascalCase — `BackupEngine`, `RestoreEngine`, `CryptoEngine`, `FileEntry`, `BackupJob`, `Config`
- **Unexported structs**: camelCase — Notably absent, almost all types are exported
- **JSON field tags**: camelCase — `json:"jobId"`, `json:"sourcePath"`, `json:"retentionDays"`
- **Custom types used for clarity**: e.g., `BatchConfig`, `ScanResult`, `RestoreState`, `FileState`, `BatchFileEntry`

## Code Style

**Formatting:**
- Go standard `gofmt` style with tabs for indentation
- No `.gofmt` or `.editorconfig` found; standard Go formatting assumed

**Linting:**
- No linter config files found (no `.golangci.yml`, `.eslintrc*`, etc.)
- No `golangci-lint` configuration present

**Error handling idiom:**
- Standard Go `if err != nil { return ... }` pattern used throughout
- Error wrapping via `fmt.Errorf("context: %w", err)` — consistently uses `%w` for error wrapping
- Errors returned, not panicked (except `mustHomeDir()` which calls `os.Exit(1)`)

## Import Organization

**Order:**
1. **Standard library** (no blank line separation within)
2. **Third-party packages** (grouped after stdlib)
3. **Internal project packages** (`github.com/esignoretti/ds3backup/...`)

Example from `internal/cli/backup.go`:
```go
import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/esignoretti/ds3backup/internal/backup"
	"github.com/esignoretti/ds3backup/internal/config"
	"github.com/esignoretti/ds3backup/internal/crypto"
	"github.com/esignoretti/ds3backup/internal/index"
	"github.com/esignoretti/ds3backup/internal/s3client"
	"github.com/esignoretti/ds3backup/pkg/models"
)
```

**Path Aliases:**
- No import aliases used; all imports by default package name
- Exception: `awscfg "github.com/aws/aws-sdk-go-v2/config"` in `internal/s3client/client.go` — aliased to avoid conflict
- Internal packages referenced via full module path: `github.com/esignoretti/ds3backup/internal/config`, `github.com/esignoretti/ds3backup/pkg/models`

## Error Handling

**Patterns:**
- **Error wrapping**: `fmt.Errorf("failed to do X: %w", err)` — consistent across the entire codebase
- **Early return**: functions validate inputs/state at the top, return errors immediately
- **Warning pattern for non-fatal errors**: logged rather than returned:
  ```go
  log.Printf("WARNING: Failed to read %s: %v", entry.Path, err)
  ```
- **graceful degradation**: non-critical errors (e.g., index sync failure) wrapped in `run.IndexSyncFailed = true` rather than aborting
- **No sentinel errors** — no `var ErrXxx` definitions found
- **No custom error types** — all errors are built from `fmt.Errorf`

**Error message convention:**
- Messages start with lowercase after "failed to": `fmt.Errorf("failed to create S3 client: %w", err)`
- User-facing errors use `fmt.Errorf("job not found: %s", jobID)` with no wrapping
- CLI commands return errors via `RunE` which cobra prints to stderr

## Logging

**Framework:** Go standard `log` package (`internal/cli/root.go` lines 55-68)

**Setup:**
- Logs written to `~/.ds3backup/ds3backup.log` in append mode
- File flags: `os.O_CREATE|os.O_WRONLY|os.O_APPEND`, permissions `0644`
- Log format: `log.Ldate | log.Ltime` → "2009/01/23 01:23:23 message"
- In verbose mode (`-v`/`--verbose`), output goes to both file and stderr via `io.MultiWriter`

**Log levels:**
- **Info**: `log.Printf("Starting backup for job: %s", job.Name)`
- **Warning**: `log.Printf("WARNING: ...")` — always written
- **Error**: `log.Printf("ERROR: %v", err)` in verbose mode only (in `root.go` line 85)
- **Session markers**: `log.Printf("=== DS3Backup Started (verbose=%v) ===", verbose)` at startup

**What gets logged:**
- Every command execution: `log.Printf("Command: %s", cmdPath)` in PersistentPreRun
- Backup phases: scanning, file processing, results
- Warnings during backup: failed reads, encryption failures, upload failures
- No sensitive data (passwords, keys) are logged

## Comments

**When to Comment:**
- Every exported type has a doc comment: `// BackupEngine handles backup operations`
- Every exported function has a doc comment: `// NewBackupEngine creates a new backup engine`
- Key logical sections described inline: `// Step 1: Scan directory`
- Go-style block comments for long explanations (e.g., `storage structure` in README, encryption flow in crypto)

**JSDoc/TSDoc:**
- Standard Go doc comments (`// Name description`) before declarations
- No `/* */` block comments on types/functions — single-line `//` only
- Comments on struct fields present in model types: `// "GOVERNANCE" or "COMPLIANCE"`

**Inline comments:**
- Used for non-obvious logic: `// +1 day buffer`, `// First run = all files are "changed"`
- Security notes: `// Stored in config (ensure file permissions are secure)`
- TODO-style comments absent — no `TODO`, `FIXME`, `HACK`, or `XXX` markers found

## Function Design

**Size:**
- Most functions are moderate (30-80 lines)
- `RunBackup` in `internal/backup/engine.go` is the largest at ~200 lines (10-step process)
- `RunE` closures in CLI files (`backup.go`, `restore.go`) are 60-130 lines

**Parameters:**
- Struct parameters used for complex configs: `RestoreOptions`, `BackupProgress`
- Callback functions for progress: `progressCb func(BackupProgress)`
- Context passed as first parameter for cancellable operations: `ctx context.Context`

**Return Values:**
- Standard Go: `(result, error)` tuple pattern
- Bool returns for existence checks: `RemoveJob(jobID string) bool`
- Some functions return multiple values without error: `Progress() (percent int, processed int, total int, bytes int64, speed float64)`

## Module Design

**Exports:**
- All struct types exported (PascalCase)
- Most constructor functions exported
- Internal helper functions kept unexported: `deriveFileKey`, `shouldSkip`, `calculateHash`
- Package-level variables in CLI (`cli` package) used as shared state for cobra flags

**Barrel Files:**
- Go does not use barrel files; each directory is a package
- `pkg/models/` has `models.go` (backup types) and `restore.go` (restore types) — these are two files in the same package, logically split by domain

## Go Module Path Conventions

**Module path:** `github.com/esignoretti/ds3backup`

**Package structure:**
```
github.com/esignoretti/ds3backup/
├── cmd/              # Entry points (main packages)
│   ├── ds3backup/      # Main binary entrypoint
│   ├── check_s3/       # Utility binary
│   └── test_archive/   # Test utility binary
├── internal/         # Private packages
│   ├── backup/         # Backup engine logic
│   ├── cli/            # Cobra CLI commands
│   ├── config/         # Configuration management
│   ├── crypto/         # Encryption/decryption
│   ├── index/          # BadgerDB index layer
│   ├── recovery/       # Disaster recovery
│   ├── restore/        # Restore engine logic
│   └── s3client/       # S3 API client
└── pkg/              # Public/shared packages
    └── models/         # Data types and structs
```

**No Go workspaces** (`go.work` not present, in `.gitignore`)

## Configuration File Format

**Type:** JSON

**Location:** `~/.ds3backup/config.json`

**Structure** (from `internal/config/config.go`):
```json
{
  "version": 1,
  "s3": {
    "endpoint": "s3.cubbit.eu",
    "bucket": "my-backups",
    "accessKey": "...",
    "secretKey": "...",
    "region": "us-east-1",
    "useSSL": true
  },
  "encryption": {
    "algorithm": "AES-256-GCM",
    "keyDerivation": "argon2id",
    "salt": "...",
    "iterations": 3,
    "memory": 65536,
    "parallelism": 4,
    "keyLength": 32
  },
  "objectLock": {
    "enabled": true,
    "mode": "GOVERNANCE",
    "defaultRetentionDays": 30
  },
  "masterPassword": "...",
  "jobs": [...]
}
```

**Atomic writes:** Config is saved via temp file + rename pattern (`internal/config/config.go` lines 109-118)

---

*Convention analysis: 2026-04-29*
