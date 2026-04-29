# Codebase Concerns

**Analysis Date:** 2026-04-29

## Tech Debt

### Duplicate BadgerDB v3 and v4 Dependencies
- **Issue:** `go.mod` imports both `github.com/dgraph-io/badger/v3 v3.2103.5` AND `github.com/dgraph-io/badger/v4 v4.5.0`. Only v4 is used in source code (`internal/index/index.go:10`).
- **Files:** `go.mod:6-7`
- **Impact:** Unnecessary dependency bloat, increased build times, potential for version conflicts. The v3 dependency appears unused.
- **Fix approach:** Remove `github.com/dgraph-io/badger/v3` from `go.mod` and run `go mod tidy`.

### Duplicate Disaster Recovery Backup in RunBackup
- **Issue:** In `internal/backup/engine.go:237-243` and again at lines 252-259, `createDisasterRecoveryBackup()` is called twice in the same function. The second call after retention (line 253) is redundant.
- **Files:** `internal/backup/engine.go:237,252`
- **Impact:** Disaster recovery archive is uploaded to S3 twice per backup run, doubling S3 PUT costs for this operation.
- **Fix approach:** Remove the duplicate call at line 252-259.

### Retention Policy is a No-Op
- **Issue:** `applyRetention()` in `internal/backup/engine.go:394-411` logs that backups are "marked for deletion" but never actually deletes anything. The comment says "S3 lifecycle will handle actual deletion" but there is no lifecycle policy enforcement from the application side.
- **Files:** `internal/backup/engine.go:394-411`
- **Impact:** Expired backups accumulate in S3 indefinitely, costing storage. Users must manually configure lifecycle policies.
- **Fix approach:** Implement S3 object deletion for expired GOVERNANCE mode backups, or integrate with S3 lifecycle API properly (the `SetLifecyclePolicy` function at `internal/s3client/client.go:238` is a stub that just prints a message).

### Index Rebuild Command is a Stub
- **Issue:** `indexRebuildCmd` in `internal/cli/index.go:144` contains a `// TODO: Implement actual rebuild logic` comment and prints "⚠️  Index rebuild not yet fully implemented."
- **Files:** `internal/cli/index.go:144-146`
- **Impact:** Users cannot rebuild a corrupted local index from S3 object metadata. The only recovery path is the full disaster recovery archive (`init --rebuild`), which requires the `.ds3backup.tar.gz` to exist on S3.
- **Fix approach:** Implement the index rebuild by scanning S3 backup files and reconstructing the BadgerDB entries from file metadata and batch manifests.

### Lifecycle Policy and Object Lock API are Stubs
- **Issue:** `SetLifecyclePolicy()` at `internal/s3client/client.go:238-241` prints a message telling users to configure manually. `GetLifecyclePolicy()` at `internal/s3client/client.go:245-247` returns a static message.
- **Files:** `internal/s3client/client.go:238-247`
- **Impact:** CLI commands `s3 lifecycle-set` and `s3 lifecycle-get` are non-functional. They report success without actually doing anything.
- **Fix approach:** Implement actual AWS S3 lifecycle API calls via the SDK.

### Obsolete RebuildEngine Code
- **Issue:** The `RebuildEngine` struct (`internal/recovery/rebuild.go:175-184`), `RebuildIndex()` (line 188), `IsRetired()` (line 204), `DiscoverJobs()` (line 209), and `downloadJobMetadata()` (line 214) are kept for "API compatibility" but all return stubs/empty results.
- **Files:** `internal/recovery/rebuild.go:175-216`
- **Impact:** ~40 lines of dead code that could confuse future maintainers.
- **Fix approach:** Remove these unused methods and the `RebuildEngine` struct.

### VERSION File is Stale
- **Issue:** `VERSION` file contains `0.0.1` while `internal/cli/version.go:4` has `const Version = "0.0.6"` and `README.md:3` says `0.0.5`.
- **Files:** `VERSION:1`, `internal/cli/version.go:4`, `README.md:3`
- **Impact:** Three different version numbers in the same repository. Confusing for release management.
- **Fix approach:** Consolidate version to a single source of truth (either the Go constant or a single file), update README to reflect actual version.

### Duplicate Formatting Utils
- **Issue:** `formatBytes()` is defined in both `internal/cli/utils.go:19` and `internal/restore/progress.go:95-110` with identical logic. `formatDuration()` is in both `internal/cli/utils.go:9` and `internal/restore/progress.go:141-147`.
- **Files:** `internal/cli/utils.go:9,19`, `internal/restore/progress.go:95,141`
- **Impact:** Code duplication — changes to formatting logic need to be applied in two places.
- **Fix approach:** Move shared formatting functions to a `pkg/format` package or the existing `pkg/models` package.

### Duplicate Worker Pattern in Restore Engine
- **Issue:** `RestoreEngine` has three almost-identical restore methods: `Restore()` (line 42), `RestoreWithProgress()` (line 311), and `RestoreEntries()` (line 421). `ResumeRestore()` (line 525) uses a different downloader (V2) but follows the same pattern. Each has its own worker result collection loop.
- **Files:** `internal/restore/engine.go:42-626`
- **Impact:** ~400 lines of near-duplicate code. Bug fixes to one method may not be applied to others.
- **Fix approach:** Refactor to a single common restore pipeline with options for progress tracking, specific entries, and resume.

### Parallel Optimization Not Implemented
- **Issue:** `RestoreOptions.Concurrency` defaults to 8 in README but is hardcoded to 4 in `restoreRunCmd` (`internal/cli/restore.go:122`). Dynamic scaling (Phase 4.4 Part 2) is explicitly listed as incomplete in README.
- **Files:** `internal/cli/restore.go:122`, `README.md:375`
- **Impact:** Fixed concurrency may not be optimal for all network/storage conditions. No bandwidth control.
- **Fix approach:** Implement dynamic worker scaling based on throughput monitoring.

## Security Considerations

### Plaintext Encryption Password Stored in Config
- **CRITICAL** - The `EncryptionPassword` field in `BackupJob` (`pkg/models/models.go:15`) is stored in plaintext in `~/.ds3backup/config.json`. The `masterPassword` feature can encrypt job configs on S3 (`internal/recovery/rebuild.go:219-228`), but the local config file stores it in plaintext.
- **Files:** `pkg/models/models.go:15`, `internal/config/config.go:18-22`
- **Risk:** Anyone with filesystem access to `~/.ds3backup/config.json` can read all encryption passwords and decrypt all backups. The `MasterPassword` field stores an encrypted checksum (non-reversible), but `EncryptionPassword` is plaintext.
- **Current mitigation:** Config file permissions are set to `0600` (`internal/config/config.go:112`).
- **Recommendations:** Encrypt `EncryptionPassword` at rest in the local config file, decrypting in memory only when needed compliant with Argon2id key derivation.

### S3 Credentials in Plaintext Config
- **MEDIUM** - `AccessKey` and `SecretKey` are stored in plaintext in `config.json` (`internal/config/config.go:28-29`).
- **Files:** `internal/config/config.go:28-29`
- **Risk:** Credentials compromise if config file is exposed.
- **Current mitigation:** File permissions `0600`. README recommends using IAM roles in production.
- **Recommendations:** Consider using OS keychain (macOS Keychain, Windows Credential Manager) or environment variables as an alternative credential source.

### S3 Download Timeout with Large Files
- **LOW** - The `GetObject()` method (`internal/s3client/client.go:131`) uses a 90-second timeout, but the downloader part size is 64MB (`internal/s3client/client.go:71`). For large files over slow connections, 90 seconds may be insufficient to download a 64MB part.
- **Files:** `internal/s3client/client.go:71,131-161`
- **Risk:** Restore operations may time out on large files over slow connections.
- **Recommendations:** Make timeout configurable or adaptive based on observed throughput and part size.

### Plaintext Salt Sent to S3
- **LOW** - The encryption salt is uploaded to S3 in plaintext JSON (`internal/cli/init.go:151`:
```go
saltData := fmt.Sprintf(`{"salt": "%s"}`, base64.StdEncoding.EncodeToString(salt))
```
This is documented as intentional ("salt is not secret") but means an attacker with S3 read access knows the salt, reducing the cost of a brute-force attack on the password.
- **Files:** `internal/cli/init.go:151`
- **Risk:** Reduced effective key derivation cost for attackers.
- **Current mitigation:** Salt is not secret by design in standard cryptography. Argon2id's memory-hardness provides the primary defense.
- **Recommendations:** Document this tradeoff explicitly for users.

## Known Bugs

### macOS BadgerDB Lock Issue (CRITICAL, Platform-Specific)
- **Symptoms:** After running a backup, attempting to restore immediately fails with "Cannot acquire directory lock... Another process is using this Badger database." Even after process exit, `Close()`, GC, and filesystem sync.
- **Files:** `internal/index/index.go:20-33`, `internal/cli/backup.go:168-174`
- **Trigger:** Rapid backup → restore sequence on macOS.
- **Workaround:** Wait 60+ seconds between operations, or run an intermediate command like `job list`.
- **Status:** Documented as unfixable without major architecture changes in `KNOWN_ISSUES.md`.

### Object Lock Mode Hardcoded to GOVERNANCE in Upload
- **Bug:** `PutObjectWithLock()` at `internal/s3client/client.go:124` hardcodes `ObjectLockMode: "GOVERNANCE"` regardless of what mode the job is configured with. COMPLIANCE mode from the job config is ignored during uploads.
- **Files:** `internal/s3client/client.go:111-128`
- **Impact:** Jobs configured with COMPLIANCE mode will actually use GOVERNANCE mode for individual file uploads Rotation mode selection. The jobs are not truly protected by COMPLIANCE mode.
- **Fix approach:** Pass and use the `mode` parameter (already provided to the function) instead of the hardcoded `"GOVERNANCE"` string.

### Config Save Not Called If RunBackup Fails
- **Bug:** In `internal/cli/backup.go:137`, `cfg.SaveConfig()` is called to update `job.LastRun` regardless of whether `engine.RunBackup()` at line 110 returned an error. But if `RunBackup` returns an error, `err` is non-nil and the backup didn't actually complete, yet `LastRun` is still updated.
- **Files:** `internal/cli/backup.go:135-137`
- **Impact:** After a failed backup, `job.LastRun` appears to show a recent run even though it failedwing the CLI code.
- **Fix approach:** Only update `job.LastRun` and save when `err == nil`.

## Performance Bottlenecks

### Entire File Loaded into Memory for Processing
- **Problem:** Files are read entirely into memory with `os.ReadFile()` at `internal/backup/engine.go:111`, and the entire file content is held for the duration of compression+encryption+upload.
- **Files:** `internal/backup/engine.go:111`
- **Cause:** Simple implementation, no streaming.
- **Improvement path:** Implement streaming compression and encryption for large files (>100MB). Use `io.Pipe` or similar to process in chunks.

### Batch Data Fully Cached in Memory
- **Problem:** `BatchExtractor.cache` (`internal/restore/extractor.go:15`) caches entire batch files (up to 10MB each) in memory with no eviction policy.
- **Files:** `internal/restore/extractor.go:14-16,30-34`
- **Cause:** Cache grows unboundedly during a restore if many batches are needed.
- **Improvement path:** Implement LRU eviction or at least a maximum cache size limit.

### Sequential Per-File S3 Uploads for Large Files
- **Problem:** Each large file is uploaded individually via `PutObjectWithLock()` in a sequential loop (`internal/backup/engine.go:137`). No parallelism for individual file uploads.
- **Files:** `internal/backup/engine.go:137`
- **Cause:** Simple sequential implementation.
- **Improvement path:** Use a worker pool for large file uploads similar to how restore uses parallel downloads.

### BadgerDB Index Scan for Point-in-Time Restore
- **Problem:** `GetEntriesForRun()` (`internal/index/index.go:323-360`) iterates over ALL file entries for a job, checking each entry's `BackupTime` against the target time with 1-second tolerance.
- **Files:** `internal/index/index.go:323-360`
- **Cause:** No secondary index on `BackupTime`.
- **Improvement path:** Maintain a separate index mapping `run:<jobID>:<timestamp>:<path>` to allow direct lookup without full scan.

## Fragile Areas

### Encryption Serialization Format (Binary, Not Self-Describing)
- **Files:** `internal/crypto/crypto.go:174-238`
- **Why fragile:** The `Serialize()` and `DeserializeEncryptedFile()` methods use a custom binary format with single-byte length prefixes for nonce, hash, and tag. If the format changes (e.g., nonce size varies by cipher), existing stored data becomes unreadable. A single off-by-one error in deserialization silently corrupts all following fields.
- **Test coverage:** No unit tests exist for these functions (no `_test.go` files found).
- **Safe modification:** Add length-prefixed type fields or use a structured format like CBOR or protobuf for forward compatibility.

### BadgerDB File Lock for Concurrent Access
- **Files:** `internal/index/index.go:20-33`
- **Why fragile:** BadgerDB uses file-level locking (`flock()` on macOS). The application is designed for single-process access only. Any concurrent access (e.g., two backup runs, or backup + restore simultaneously) will fail with file lock errors.
- **Test coverage:** No integration tests for concurrent access.
- **Safe modification:** Ensure all CLI commands serialize access to the index via a process-level lock file.

### Duplicate AES-256-GCM Implementation
- **Files:** `internal/crypto/crypto.go:72-121` and `internal/crypto/master_password.go:36-77`
- **Why fragile:** Two separate implementations of AES-256-GCM encryption, one for file-level encryption and one for master password encryption. They use slightly different patterns (one extracts nonce/tag/ciphertext separately, one uses GCM's `Seal` with nonce prefix). Bugs fixed in one may not be applied to the other.
- **Safe modification:** Consolidate into a single `Encrypt(key, plaintext)` and `Decrypt(key, ciphertext)` pair.

## Scaling Limits

### BadgerDB for Large Backup Sets
- **Problem:** BadgerDB is used as the local index for file tracking. For large backups (millions of files), the in-memory memtable size is 64MB (`internal/index/index.go:23`) and LSM tree compaction can become a bottleneck.
- **Current capacity:** The default 64MB memtable handles tens of thousands of files well. Performance degradation is likely beyond ~500K file entries.
- **Limit:** BadgerDB's LSM tree compaction with `WithNumCompactors(2)` (line 24) may fall behind with very high write throughput during a full backup of millions of small files.
- **Scaling path:** Increase memtable size, increase compactor count, or consider an alternative embedded database (SQLite, BoltDB) for larger workloads.

### Single S3 Endpoint
- **Problem:** The application is designed for a single S3-compatible endpoint. No multi-region, multi-bucket, or replication support.
- **Files:** `internal/config/config.go:26-31`
- **Limit:** All data is stored in one bucket at one endpoint. This is a single point of failure.
- **Scaling path:** Add support for multiple storage targets (primary + replica) in future releases.

## Missing Critical Features

### No Tests (Zero Test Files)
- **Problem:** There are zero `_test.go` files in the entire repository.
- **Files:** N/A (no test files exist)
- **Risk:** No regression protection. Every bug fix or feature addition is done without automated verification.
- **Priority:** High

### No Graceful Shutdown / Signal Handling
- **Problem:** The application does not handle `SIGINT`/`SIGTERM` to gracefully stop in-progress backups or restores. If a user presses Ctrl+C during a backup, the BadgerDB index may be left in an inconsistent state.
- **Files:** `internal/cli/root.go:74-89`
- **Risk:** Data corruption risk for BadgerDB index on abrupt termination.

### No Backup Integrity Checksum On-Read
- **Problem:** While files have hash verification after decrypt, there is no end-to-end integrity check of the backup at the S3 level. If an S3 object is silently corrupted (bit rot), the issue is only detected during restore.
- **Files:** N/A (feature not implemented)
- **Risk:** Silent data corruption may go undetected for months.

## Test Coverage Gaps

### Untested Area: Encryption/Decryption Round-Trip
- **What's not tested:** `CompressAndEncrypt()` → `Serialize()` → `DeserializeEncryptedFile()` → `DecryptAndDecompress()` round-trip.
- **Files:** `internal/crypto/crypto.go`
- **Risk:** A bug in serialization could make all backups unrecoverable.
- **Priority:** High

### Untested Area: Backup Engine Pipeline
- **What's not tested:** `RunBackup()` — scanning, dedup, compression, encryption, batching, upload flow.
- **Files:** `internal/backup/engine.go`
- **Risk:** Regression in core backup logic.
- **Priority:** High

### Untested Area: Index Operations
- **What's not tested:** `SaveEntry()`, `GetEntry()`, `GetByHash()`, `GetChangedFiles()`, `GetUniqueFilesToBackup()`.
- **Files:** `internal/index/index.go`, `internal/index/scan.go`
- **Risk:** Deduplication or incremental backup logic bugs.
- **Priority:** Medium

## Dependency Risks

### BadgerDB v4 (Unmaintained?)
- **Risk:** BadgerDB v4 was archived by Dgraph in 2023. The project is no longer actively maintained. The known macOS lock issue will never be fixed upstream.
- **Impact:** If a critical bug is discovered, there will be no upstream fix. The macOS lock issue is permanent.
- **Migration plan:** Consider migrating to SQLite (via `modernc.org/sqlite` — pure Go, no CGo) or BoltDB (via `go.etcd.io/bbolt`).

### High Number of Indirect Dependencies
- **Risk:** The AWS SDK v2 pulls in 20+ indirect dependencies (`go.mod:14-64`), plus BadgerDB's own dependencies (Ristretto, Protobuf, etc.).
- **Impact:** Large binary size (28MB according to test_results.txt). Increased attack surface. Slower builds.
- **Migration:** Consider a lighter S3-compatible client (e.g., `minio-go/v7` which is already listed as a direct dependency at `go.mod:9` — it appears imported but not used in Go source files).

### `cloud.google.com/go` Not a Dependency but Awkward AWS SDK Usage
- **Risk:** The code imports the AWS SDK v2 directly AND the MinIO SDK. Both are full SDKs. One should be sufficient.
- **Files:** `go.mod:7-9`
- **Impact:** Redundant dependencies. If MinIO SDK supports all needed S3 operations (which it does), the AWS SDK v2 could be removed.

## Cross-Platform Concerns

### macOS BadgerDB Lock Issue (Also Listed Under Known Bugs)
- **Impact:** Breaks rapid backup → restore workflows on macOS. This is the most impactful platform-specific issue.

### Windows Permission Handling
- **Files:** `internal/restore/metadata.go:20`
- **Issue:** File permissions are only set on non-Windows platforms (`runtime.GOOS != "windows"`). On Windows, restored files get default permissions. This is probably correct behavior but undocumented.

### Hardcoded Path Separators
- **Files:** `internal/backup/engine.go:137,186,194,324` and throughout
- **Issue:** S3 key construction uses forward slashes throughout, which is correct for S3. But path manipulation in config directory uses `filepath.Join()` which is correct for the OS. The code appears to handle this correctly, but needs awareness that S3 paths are always forward-slash.

---

*Concerns audit: 2026-04-29*
