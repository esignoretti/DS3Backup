# External Integrations

**Analysis Date:** 2026-04-29

## APIs & External Services

**S3-Compatible Object Storage:**
- **Target Provider:** Cubbit DS3 (designed for Cubbit's S3-compatible cell network).
  - SDK/Client: AWS SDK v2 (`github.com/aws/aws-sdk-go-v2/service/s3` v1.100.0) via `internal/s3client/client.go`.
  - Multipart download support via `github.com/aws/aws-sdk-go-v2/feature/s3/manager` v1.22.16.
  - Connection: Static credentials (access key + secret key) passed directly to `credentials.NewStaticCredentialsProvider`.
  - Endpoint: Customizable via `--endpoint` flag (e.g., `s3.cubbit.eu`); defaults to HTTPS with `UsePathStyle: true` required for S3-compatible endpoints.

**Provider-Specific Behavior:**
- **Path-style addressing** is forced (`o.UsePathStyle = true`) — required for S3-compatible non-AWS endpoints. AWS S3 uses virtual-hosted-style by default.
- **Object Lock** detection at init time via `GetObjectLockConfiguration` API call. Supports GOVERNANCE and COMPLIANCE modes.
- **Lifecycle policies** are noted as needing manual configuration in the S3 provider's console (`internal/s3client/client.go:239-247`). Not automated.
- **Endpoint URL normalization:** If endpoint does not start with `http://` or `https://`, `https://` is prepended (`internal/s3client/client.go:61-63`).
- **BypassGovernanceRetention** supported via the `DeleteObject` function for GOVERNANCE mode objects (`internal/s3client/client.go:213`). COMPLIANCE mode objects cannot be deleted programmatically.

## Data Storage

**Primary Storage (Remote):**
- **S3-compatible object storage** at a user-configured endpoint.
  - S3 key naming scheme:
    - Individual files: `backups/<job-id>/files/<hash>.enc`
    - Batched files: `backups/<job-id>/batches/<batch-id>.enc`
    - Batch manifests: `backups/<job-id>/batches/<batch-id>-manifest.json.enc`
    - Index snapshots: `backups/<job-id>/index_<timestamp>/<rel-path>`
    - Central index: `.ds3backup/index/<job-id>/<rel-path>`
    - Job metadata: `.ds3backup/jobs/<job-id>/config.json`
    - Encryption salt: `.ds3backup/encryption-salt.json`
    - Disaster recovery archive: `.ds3backup.tar.gz`
    - Disaster recovery checksum: `.ds3backup.tar.gz.sha256`

**Local Storage (Index/Config):**
- **BadgerDB v4** embedded key-value store at `~/.ds3backup/index/<job-id>/`.
  - Three key prefixes: `file:<jobID>:<path>`, `hash:<jobID>:<hash>`, `run:<jobID>:<timestamp>`.
- **Configuration file** at `~/.ds3backup/config.json` (JSON format).
- **Log file** at `~/.ds3backup/ds3backup.log` (standard library `log`).
- **Restore state files** at `~/.ds3backup/state/<job-id>/<session>/restore-state.json`.
- Files are written atomically (write to `.tmp`, then `os.Rename`).

**File Storage:**
- Local filesystem only (for restore destinations). No external file storage services.

**Caching:**
- **Batch extractor** (`internal/restore/extractor.go`) caches downloaded batch data in memory: `map[string][]byte` keyed by batch ID.
- **BadgerDB** uses `ristretto` (in-memory cache) internally.
- No external caching service (Redis, Memcached, etc.).

## Authentication & Identity

**S3 Authentication:**
- **Static credentials** — Access key and secret key stored in `~/.ds3backup/config.json`.
  - Implementation: `credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")`.
  - Region configurable, defaults to `us-east-1`.
- **No IAM roles, STS, or session tokens** support.
- **No AWS profile/credential chain** support — credentials must be explicitly provided.

**Master Password (for config encryption):**
- **Optional** — used to encrypt job passwords stored in the config.
- Stored as an `argon2id`-derived checksum in config (`EncryptWithMasterPassword` of a known string).
- Not an external identity provider.

## Monitoring & Observability

**Error Tracking:**
- None (no Sentry, Rollbar, etc.).
- All errors returned as Go `error` values; printed to stderr or logged to `~/.ds3backup/ds3backup.log`.

**Logs:**
- Standard library `log` package.
- Written to `~/.ds3backup/ds3backup.log` with `Ldate | Ltime` flags.
- Verbose mode (`--verbose`/`-v`) mirrors log output to stderr via `io.MultiWriter`.
- No structured logging, no log levels beyond presence/absence of verbose mode.

## CI/CD & Deployment

**Hosting:**
- Not applicable (CLI binary distributed as compiled Go executable).

**CI Pipeline:**
- None detected in the repository.

## Environment Configuration

**Required configuration values** (stored in JSON config, not env vars):
| Field | Config Path | CLI Flag | Required |
|-------|-------------|----------|----------|
| S3 Endpoint | `config.s3.endpoint` | `--endpoint` | Yes |
| S3 Bucket | `config.s3.bucket` | `--bucket` | Yes |
| S3 Access Key | `config.s3.accessKey` | `--access-key` | Yes |
| S3 Secret Key | `config.s3.secretKey` | `--secret-key` | Yes |
| S3 Region | `config.s3.region` | `--region` | No (default `us-east-1`) |
| Encryption Salt | `config.encryption.salt` | (generated) | Auto |
| Master Password Checksum | `config.masterPassword` | `--master-password` | No |
| Object Lock Mode | `config.objectLock.mode` | `--object-lock-mode` | No (default `NONE`) |
| Object Lock Retention Days | `config.objectLock.defaultRetentionDays` | `--retention-days` | No (default 30) |

**Secrets location:**
- S3 access key and secret key stored unencrypted in `~/.ds3backup/config.json` (file permissions: 0600).
- Job encryption passwords stored in `~/.ds3backup/config.json` (potentially encrypted via master password).
- `.gitignore` excludes `config.json` and `*.key` files.
- A file `mimir-key` at project root exists but is unrelated to ds3backup (used by OpenCode AI config in `opencode.json`).

## Webhooks & Callbacks

**Incoming:**
- None. The tool has no HTTP server.

**Outgoing:**
- None. No outbound webhooks or callbacks.

## Build & Distribution

**Build command:**
- `go build` — produces single binary `ds3backup`.
- No Makefile, no containerization, no release automation detected.

**Version:**
- Source of truth: `internal/cli/version.go` defines `const Version = "0.0.6"`.
- `VERSION` file has `0.0.1` (outdated — source code is authoritative).

---

*Integration audit: 2026-04-29*
