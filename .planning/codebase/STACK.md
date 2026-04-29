# Technology Stack

**Analysis Date:** 2026-04-29

## Languages

**Primary:**
- **Go** `go1.25.0` — All application code, CLI, and internal packages.
  - Module path: `github.com/esignoretti/ds3backup`

## Runtime

**Environment:**
- Go compiled binary — cross-platform single executable.

**Package Manager:**
- **Go modules** (built-in with `go1.25.0`)
- Lockfile: `go.sum` present.

## Frameworks

**CLI:**
- **Cobra** (`github.com/spf13/cobra` v1.8.1) — CLI framework for the `ds3backup` binary.
  - Subcommands: `init`, `backup`, `restore`, `job`, `index`, `config`, `s3`, `version`.
  - Flags: `--config`, `--verbose` (global); `--json`, `--full`, `--dry-run`, `--overwrite`, `--include`, `--exclude`, `--password`, `--time`, etc.
  - Access to terminal for password entry via `golang.org/x/term`.

**No web framework or HTTP server** — this is a CLI-only tool.

## Key Direct Dependencies

**Storage / S3:**
- `github.com/minio/minio-go/v7` v7.0.63 — MinIO Go SDK for S3-compatible storage (primary S3 client).
- `github.com/aws/aws-sdk-go-v2` v1.41.6 — AWS SDK v2 (indirect, used internally by minio-go for some operations).
  - Sub-packages: `config`, `credentials`, `service/s3`, `feature/s3/manager`.
- The S3 client in `internal/s3client/` uses AWS SDK v2 directly (`github.com/aws/aws-sdk-go-v2/service/s3`), **not** minio-go.

**Database / Indexing:**
- `github.com/dgraph-io/badger/v4` v4.5.0 — Embedded key-value store used for local backup index (file index, dedup, run history).
- `github.com/dgraph-io/badger/v3` v3.2103.5 — Also listed in go.mod (likely transitional; v4 is the active version used in code).
  - Both use `github.com/dgraph-io/ristretto` (v0.1.1 and v2.0.0) for caching.

**Cryptography:**
- `golang.org/x/crypto` v0.29.0 — Argon2id key derivation (`argon2.IDKey`), BLAKE2b hashing (`blake2b`).
- Standard library `crypto/aes`, `crypto/cipher`, `crypto/rand`, `crypto/sha256`, `crypto/hmac` — AES-256-GCM encryption, SHA-256 checksums, HMAC per-file key derivation.

**Compression:**
- `github.com/klauspost/compress` v1.17.11 — zstd compression library for file-level compression.
- Standard library `compress/gzip` and `archive/tar` — used for disaster recovery archive and archive extraction (in `internal/backup/archive.go`).

**Logging:**
- Standard library `log` — Console + file logging to `~/.ds3backup/ds3backup.log`.
- `github.com/sirupsen/logrus` v1.9.3 — Indirect dependency (from badger/minio).

**Serialization / Data:**
- Standard library `encoding/json` — JSON for config files, batch manifests, encrypted file metadata.
- `google.golang.org/protobuf` v1.33.0 — Indirect (required by badger).
- `github.com/gogo/protobuf` v1.3.2 — Indirect (required by badger v3).

**Other Utilities:**
- `github.com/google/uuid` v1.3.0 — Indirect (from minio-go).
- `github.com/rs/xid` v1.5.0 — Indirect (from minio-go).
- `gopkg.in/ini.v1` v1.67.0 — Indirect (from minio-go).
- `golang.org/x/net` v0.31.0 — Indirect (HTTP/transport).
- `golang.org/x/sync` v0.20.0 — Indirect.
- `golang.org/x/sys` v0.43.0 — Indirect (system calls).
- `golang.org/x/term` v0.42.0 — Indirect (terminal access for password entry).
- `golang.org/x/text` v0.20.0 — Indirect.
- `go.opencensus.io` v0.24.0 — Indirect (from badger: metrics/tracing).
- `github.com/cespare/xxhash` v1.1.0 and v2.3.0 — Indirect (from badger/ristretto).
- `github.com/dustin/go-humanize` v1.0.1 — Indirect (from minio/badger).
- `github.com/json-iterator/go` v1.1.12 — Indirect (from minio).
- `github.com/modern-go/concurrent` and `reflect2` — Indirect (from minio).

**Indirect AWS SDK modules:**
All v2.x, pulled by `aws-sdk-go-v2/service/s3` and `feature/s3/manager`:
- `github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream` v1.7.9
- `github.com/aws/aws-sdk-go-v2/config` v1.32.16
- `github.com/aws/aws-sdk-go-v2/credentials` v1.19.15
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds` v1.18.22
- `github.com/aws/aws-sdk-go-v2/feature/s3/manager` v1.22.16
- `github.com/aws/smithy-go` v1.25.0

## Configuration

**Environment:**
- No standard `.env` file with environment variables for configuration.
- Configuration stored in `~/.ds3backup/config.json` (JSON file).
- The `mimir-key` file at project root appears to be an API key for an OpenCode AI provider (Cubbit Mimir), not for ds3backup itself.
- `.gitignore` excludes `config.json`, `*.key`, `*.pem`.

**Build:**
- No Makefile detected.
- No build config files (`*.cfg`, `*.ini`, `*.toml` for build).
- No CI config detected.
- Single binary build via `go build`.

**Key configs required at init:**
- `--endpoint` — S3-compatible endpoint URL
- `--bucket` — S3 bucket name
- `--access-key` — S3 access key
- `--secret-key` — S3 secret key
- `--master-password` (optional) — for encrypting job passwords
- `--object-lock-mode` — GOVERNANCE, COMPLIANCE, or NONE
- `--retention-days` (default 30)

## Platform Requirements

**Development:**
- Go 1.25.0+
- Git
- No additional development requirements.

**Production:**
- Linux, macOS, or Windows (cross-platform binary).
- Write access to `~/.ds3backup/` directory.
- Network access to S3-compatible endpoint.
- No database server required (BadgerDB is embedded).

---

*Stack analysis: 2026-04-29*
