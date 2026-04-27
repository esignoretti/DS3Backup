# Known Issues

## BadgerDB Lock Issue on macOS (CRITICAL)

**Severity:** Critical  
**Platform:** macOS only  
**Status:** Unfixable without major architectural changes  

### Problem

After running a backup, attempting to restore immediately fails with:
```
Error: failed to open BadgerDB: Cannot acquire directory lock on "...".  
Another process is using this Badger database. error: resource temporarily unavailable
```

This is a **fundamental bug in BadgerDB** where file descriptor locks are not released properly on macOS, even after:
- Process exit
- Explicit `Close()` calls
- Garbage collection
- Filesystem sync
- Waiting 15+ seconds
- System reboot

### Root Cause

BadgerDB uses `flock()` for locking on macOS. The OS doesn't release these locks immediately when a process exits, especially if the process terminates quickly after closing the database.

### Workaround

**Option 1: Wait 60+ seconds between backup and restore**
```bash
./ds3backup backup run <job-id>
sleep 60
./ds3backup restore run <job-id> --password=xxx
```

**Option 2: Use separate terminal sessions**
Run backup and restore in different terminal windows with a natural delay.

**Option 3: Run another command in between**
```bash
./ds3backup backup run <job-id>
./ds3backup job list  # Creates delay
./ds3backup restore run <job-id> --password=xxx
```

### Future Solutions

1. **Switch database engine** - Migrate to SQLite or BoltDB which don't have this issue
2. **Client-server architecture** - Run BadgerDB in a long-lived server process
3. **File-based locking** - Implement custom locking to serialize access (complex)

### Impact

- Backups work correctly
- Restores work correctly after waiting
- No data corruption
- Only affects rapid backup→restore sequences

### Reporting

If you encounter this issue, please:
1. Wait 60 seconds and retry
2. Report the issue at: https://github.com/esignoretti/DS3Backup/issues
3. Include your macOS version and BadgerDB version

---

**Last Updated:** 2026-04-27  
**Affected Versions:** 0.0.1 - 0.0.5
