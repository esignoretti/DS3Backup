# DS3 Backup

**Version:** 0.0.3

**DS3 Backup** is a secure, S3-only backup tool with client-side encryption, designed for simplicity and reliability.

## Features

- ✅ **Client-side AES-256-GCM encryption** - Your data is encrypted before it leaves your machine
- ✅ **S3 Object Lock support** - Ransomware protection with Governance/Compliance modes
- ✅ **Incremental backups** - Only changed files are uploaded after the first backup
- ✅ **File deduplication** - Duplicate files are detected and referenced, not re-uploaded
- ✅ **File batching** - Small files are batched together for efficient S3 operations (10MB max batch size)
- ✅ **BadgerDB indexing** - Fast, embedded key-value database for file tracking
- ✅ **Compression before encryption** - zstd compression reduces storage costs
- ✅ **Cross-platform** - Works on macOS and Windows

## Installation

### Build from Source

```bash
git clone https://github.com/esignoretti/ds3backup
cd ds3backup
go build -o ds3backup ./cmd/ds3backup
```

## Quick Start

### 1. Initialize with S3

```bash
ds3backup init \
  --endpoint=s3.cubbit.eu \
  --bucket=my-backups \
  --access-key=YOUR_ACCESS_KEY \
  --secret-key=YOUR_SECRET_KEY \
  --password=YOUR_ENCRYPTION_PASSWORD \
  --object-lock-mode=GOVERNANCE \
  --retention-days=30
```

This will:
- Validate S3 credentials and bucket access
- Check Object Lock support
- Set up encryption with your password
- Create configuration file at `~/.ds3backup/config.json`

### 2. Create a Backup Job

```bash
ds3backup job add \
  --name="Documents" \
  --path=~/Documents \
  --retention=30 \
  --object-lock-mode=GOVERNANCE
```

### 3. Run Backup

```bash
# Incremental backup (default)
ds3backup backup run <job-id>

# Full backup
ds3backup backup run <job-id> --full

# With JSON output (for automation)
ds3backup backup run <job-id> --json
```

### 4. View Job Status

```bash
ds3backup job list
ds3backup job status <job-id>
```

## CLI Commands

### Initialization

```bash
ds3backup init [flags]

Flags:
  --endpoint string           S3 endpoint (e.g., s3.cubbit.eu)
  --bucket string             S3 bucket name
  --access-key string         S3 access key
  --secret-key string         S3 secret key
  --password string           Encryption password
  --region string             S3 region (default: us-east-1)
  --object-lock-mode string   GOVERNANCE or COMPLIANCE (default: GOVERNANCE)
  --retention-days int        Retention period in days (default: 30)
```

### Backup Jobs

```bash
# Add a job
ds3backup job add --name="MyBackup" --path=~/Documents --retention=30

# List jobs
ds3backup job list

# Delete a job
ds3backup job delete <job-id>
```

### Backup Operations

```bash
# Run backup
ds3backup backup run <job-id> [--full] [--json]

# View status
ds3backup backup status <job-id>
```

### Restore Operations

```bash
# Restore all files from latest backup
ds3backup restore run <job-id> --password=YOUR_PASSWORD

# Restore to alternate location (preserves full path structure)
ds3backup restore run <job-id> --to=/tmp/restore --password=YOUR_PASSWORD

# Preview restore (dry-run)
ds3backup restore run <job-id> --dry-run

# Verify backup integrity (download, decrypt, verify hash)
ds3backup restore run <job-id> --verify --password=YOUR_PASSWORD

# Overwrite existing files during restore
ds3backup restore run <job-id> --overwrite --password=YOUR_PASSWORD
```

**Restore Features:**
- 8 parallel downloads for fast restoration
- Skip existing files by default (safe)
- Preserve file metadata (permissions, timestamps)
- Hash verification (BLAKE2b-256) ensures integrity
- Full path preservation when restoring to alternate location

## Architecture

### Storage Structure

```
bucket/
  .ds3backup/
    config.json.enc         # Encrypted configuration
    index/
      <job-id>/             # BadgerDB index files
        MANIFEST
        *.sst
        value.log
  
  backups/
    <job-id>/
      files/
        <hash>.enc          # Individual large files (>1MB)
      batches/
        <batch-id>.enc      # Batched small files (≤10MB)
        <batch-id>-manifest.json.enc
```

### Backup Flow

1. **Scan** directory for files
2. **Calculate hash** (BLAKE2b-256) for each file
3. **Check deduplication** - skip if hash already exists
4. **Compress** with zstd (skip for already-compressed formats)
5. **Encrypt** with AES-256-GCM (per-file key derived from hash)
6. **Batch small files** (<1MB) into 10MB archives
7. **Upload to S3** with Object Lock
8. **Update local index** (BadgerDB)
9. **Sync index to S3** (rebuildable cache)

### Encryption

```
Password → Argon2id → Master Key
                    ↓
File Hash → HMAC-SHA256 → Per-File Key
                    ↓
Compressed Data → AES-256-GCM → Encrypted File
```

**Security Properties:**
- Password never stored (only salt + derivation params)
- Per-file keys (compromise of one file doesn't help with others)
- Authenticated encryption (tamper detection)
- Object Lock prevents deletion during retention period

## Configuration

Config file location: `~/.ds3backup/config.json`

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
  "jobs": [...]
}
```

## Batching Configuration

Default batching settings (optimized for S3 cost/performance):

- **Max batch size**: 10 MB
- **Max files per batch**: 500
- **Min files for batch**: 5
- **Large file threshold**: 1 MB (files above this are uploaded individually)

**Benefits:**
- 10,000 × 100KB files → ~10 S3 PUT operations (instead of 10,000)
- Cost reduction: ~$0.05 → ~$0.00005 per backup
- Faster backup completion

## Deduplication

**Scope**: Per-job (files are deduplicated within the same backup job)

**How it works:**
1. Calculate BLAKE2b-256 hash of file content
2. Check if hash exists in local index
3. If exists: reference existing S3 object (skip upload)
4. If new: compress, encrypt, upload

**Example:**
- Backing up same file in multiple directories? → Only uploaded once
- File moved/renamed? → Hash stays the same, deduplicated

## Compression

**Algorithm**: zstd (level 3 - balanced speed/ratio)

**Skipped for** (already compressed):
- Images: `.jpg`, `.jpeg`, `.png`, `.gif`, `.webp`, `.heic`
- Video: `.mp4`, `.mov`, `.avi`, `.mkv`
- Audio: `.mp3`, `.aac`, `.flac`
- Archives: `.zip`, `.rar`, `.7z`, `.gz`

## Object Lock

**Modes:**
- **GOVERNANCE** (default): Users with `s3:BypassGovernanceRetention` can delete/overwrite
- **COMPLIANCE**: Immutable until retention expires (no bypass possible)

**Retention period**: `retentionDays + 1` (1-day buffer)

**Cleanup**: S3 lifecycle policies handle automatic deletion after retention expires

## Index Management

**Local index** (BadgerDB):
- Fast queries for deduplication
- Tracks file paths, hashes, S3 locations
- Stored in temp directory during backup

**Index sync**:
- Uploaded to S3 after each backup
- Can be rebuilt from S3 if lost:
  ```bash
  ds3backup index rebuild <job-id> --from-s3
  ```

**If sync fails**: Backup data is safe, index can rebuild from S3 metadata

## Troubleshooting

### "Config file not found"

Run `ds3backup init` to create configuration.

### "Job not found"

List jobs with `ds3backup job list` to find the correct job ID.

### "Path does not exist"

Ensure the backup source path exists and is accessible.

### "S3 connection failed"

Verify:
- S3 credentials are correct
- Bucket exists
- Network connectivity to S3 endpoint
- SSL/TLS is enabled (default)

### "Index sync failed" (warning)

Backup data is safe in S3. The local index can be rebuilt:
```bash
ds3backup index rebuild <job-id> --from-s3
```

## Roadmap

### Phase 2: Scheduling & Server (Next)
- Background scheduler with cron expressions
- HTTP REST API
- System tray integration
- Automatic backup execution

### Phase 1.2: Management Commands ✅ (Current)
- `config show` - Display current configuration
- `config validate` - Validate configuration and S3 connection
- `config reset` - Reset configuration with optional S3 wipe
- `index show` - Display index statistics for a job
- `index rebuild` - Rebuild local index from S3
- `index clear` - Clear local index
- `s3 check-object-lock` - Check if bucket supports Object Lock
- `s3 lifecycle-set` - Set S3 lifecycle policy
- `s3 lifecycle-get` - Get current lifecycle policy
- `s3 ls` - List objects in S3 bucket

### Phase 3: Desktop UI
- Cross-platform system tray app
- Progress notifications
- Backup history visualization
- One-click restore

### Phase 4: Restore
- ✅ **Phase 4.1: MVP Restore** - Core restore functionality (latest backup, all files)
- ⏳ Phase 4.2: Selective restore (pattern filtering)
- ⏳ Phase 4.3: Point-in-time recovery
- ⏳ Phase 4.4: Advanced features (parallel downloads optimization, resume)

## Changelog

### v0.0.3 (2026-04-26)
**New Features:**
- **Restore functionality** (Phase 4.1 MVP)
  - `restore run` - Restore all files from latest backup
  - `--to` flag - Restore to alternate location
  - `--dry-run` - Preview restore without downloading
  - `--verify` - Verify backup integrity
  - `--overwrite` - Overwrite existing files
  - 8 parallel download workers
  - Batch caching (download once, extract multiple files)
  - Metadata preservation (permissions, timestamps)
  - Hash verification (BLAKE2b-256)

**Bug Fixes:**
- Fixed backup engine not saving S3Key for files (entries were saved before S3 keys were set)
- Fixed index entries not including S3 location information

### v0.0.2 (2026-04-26)
**Bug Fixes:**
- Fixed `index show` command not displaying backup runs (BadgerDB v4 Reverse+Prefix iterator bug)
- Fixed prefix matching in GetBackupHistory and GetLastRun functions

**Improvements:**
- Better error handling in index operations
- More descriptive error messages

### v0.0.1 (2026-04-26)
**Initial Release:**
- Core backup functionality with incremental backups
- Client-side AES-256-GCM encryption with Argon2id key derivation
- S3 Object Lock support (GOVERNANCE, COMPLIANCE, NONE modes)
- BadgerDB local index with S3 sync
- File batching for S3 cost optimization
- Deduplication and compression (zstd)
- CLI commands: init, job add/list/delete, backup run/status

## Security Considerations

1. **Password management**: Store encryption password securely (not in config)
2. **S3 credentials**: Consider using IAM roles or environment variables in production
3. **Object Lock**: Use COMPLIANCE mode for maximum protection
4. **Access control**: Limit S3 bucket access to necessary operations only

## License

MIT License - see LICENSE file for details

## Contributing

Contributions welcome! Please open an issue or submit a PR.

## Support

For issues and feature requests, please use GitHub Issues.
