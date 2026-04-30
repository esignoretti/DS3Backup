# Known Issues

### macOS: BadgerDB File Lock Persists After Process Exit

**Impact:** After running a backup, attempting a restore within ~60 seconds may fail with "Cannot acquire directory lock... Another process is using this Badger database."

**Status:** ACCEPTED — This is an upstream BadgerDB v4 issue on macOS (project archived by Dgraph in 2023). The BadgerDB `flock()` call on macOS does not release immediately after process exit, even with explicit Close(), GC(), and filesystem sync.

**Workaround:** Wait 60+ seconds between operations, or run an intermediate command like `ds3backup job list`.

**Resolution:** Not fixable without migrating to a different embedded database (e.g., SQLite via modernc.org/sqlite or BoltDB via go.etcd.io/bbolt).

---

## BadgerDB Restore Implementation

**Status:** ✅ Implemented in v0.0.7+

### Full Index Recovery

The rebuild command now fully restores the BadgerDB index from S3 backups:

```bash
# Simulate disaster (delete local config)
rm -rf ~/.ds3backup

# Rebuild from S3
ds3backup init --rebuild --endpoint=... --bucket=... --access-key=... --secret-key=... --master-password="..."
```

**What gets restored:**
1. ✅ Job configurations from `.ds3backup/jobs/<job-id>/config.json.enc`
2. ✅ BadgerDB index from `backups/<job-id>/index_<timestamp>/`
3. ✅ Complete file metadata for restore operations

**Note:** macOS users may experience BadgerDB lock issues during testing. This is a platform-specific limitation, not a bug in the restore implementation.
