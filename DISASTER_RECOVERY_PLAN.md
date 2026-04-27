# Disaster Recovery Feature - Implementation Plan

## Overview
This document outlines the implementation of disaster recovery features for DS3 Backup.

## Features

### 1. Master Password System
- Set during `ds3backup init`
- Optional (can be empty for no encryption)
- Used to encrypt job configurations stored on S3
- Verified with checksum during rebuild

### 2. Job Metadata Storage on S3
- Location: `.ds3backup/jobs/<job-id>/config.json.enc`
- Contains: job config + encrypted job password
- Encrypted with master password
- Enables automatic job recovery

### 3. Retirement Marker
- Created when `job delete --clean` fails due to COMPLIANCE Object Lock
- Location: `.ds3backup/index/<job-id>/RETIRED-DO-NOT-REBUILD` (on S3)
- Prevents rebuild of protected jobs

### 4. Rebuild Command
- `ds3backup init --rebuild`
- Scans S3 for jobs
- Skips retired jobs
- Downloads job metadata
- Rebuilds local indexes from backup copies
- Restores job configuration

## Implementation Status

### Phase 1: Master Password & Job Metadata ✅ (In Progress)
- [x] Master password helper functions (`internal/crypto/master_password.go`)
- [x] Config structure updated with `MasterPassword` field
- [ ] Init command updated with master password prompt
- [ ] Job metadata storage on S3
- [ ] Save job metadata during `job add`

### Phase 2: Retirement Marker ⏳
- [ ] Modify `job delete --clean` to create S3 marker
- [ ] Marker path: `.ds3backup/index/<job-id>/RETIRED-DO-NOT-REBUILD`
- [ ] User feedback messages

### Phase 3: Rebuild Implementation ⏳
- [ ] Create `internal/recover/rebuild.go` package
- [ ] Implement `DiscoverJobs()` function
- [ ] Implement `IsRetired()` check
- [ ] Implement `RebuildIndex()` function
- [ ] Add `--rebuild` flag to init command
- [ ] Password prompts for recovered jobs
- [ ] Progress reporting

### Phase 4: Testing & Documentation ⏳
- [ ] Test retirement marker creation
- [ ] Test rebuild with retired jobs
- [ ] Test rebuild with missing metadata
- [ ] Test master password validation
- [ ] Update help text
- [ ] Document disaster recovery workflow

## File Changes Required

### New Files
1. `internal/crypto/master_password.go` - Master password encryption helpers
2. `internal/recover/rebuild.go` - Rebuild engine
3. `DISASTER_RECOVERY_PLAN.md` - This document

### Modified Files
1. `internal/config/config.go` - Add MasterPassword field
2. `internal/cli/init.go` - Add master password prompt and --rebuild flag
3. `internal/cli/job.go` - Save job metadata on add, create retirement marker on delete
4. `internal/backup/engine.go` - Save job metadata after successful backup

## Usage Examples

### Initialize with Master Password
```bash
$ ds3backup init \
  --endpoint=s3.cubbit.eu \
  --bucket=mybucket \
  --access-key=xxx \
  --secret-key=xxx \
  --master-password="my-secret-password"

Enter master password: *****
Confirm master password: *****
✓ Master password configured
```

### Create Job (metadata saved to S3 automatically)
```bash
$ ds3backup job add \
  --name="Documents" \
  --path=~/Documents \
  --password="job-password-123"

✓ Backup job created
✓ Job metadata saved to S3
```

### Delete Job with Object Lock Failure
```bash
$ ds3backup job delete job_123 --clean
Cleaning up S3 files...
🔒 Locked: 10 objects (protected by COMPLIANCE Object Lock)
⚠️  Job marked as RETIRED
Error: cannot delete 10 objects: protected by COMPLIANCE Object Lock
```

### Rebuild from Disaster
```bash
$ ds3backup init --rebuild \
  --endpoint=s3.cubbit.eu \
  --bucket=mybucket \
  --access-key=xxx \
  --secret-key=xxx

Enter master password: *****
Scanning S3 bucket...
Found 3 backup jobs:
  ✓ job_1777297407905167000 (Documents)
  ⚠️  job_1777295716489319000 (SKIPPED - RETIRED)
  ✓ job_1777282093092875000 (Photos)

Enter password for job_1777297407905167000: *****
Enter password for job_1777282093092875000: *****

Rebuilding indexes...
  ✓ job_1777297407905167000: Index restored
  ✓ job_1777282093092875000: Index restored

Recovered 2 jobs (1 retired job skipped)
```

## Security Considerations

1. **Master Password**: Never stored in plain text, only checksum saved
2. **Job Passwords**: Encrypted with master password before storing on S3
3. **Empty Master Password**: Job passwords NOT stored on S3, user must enter during rebuild
4. **File Permissions**: Config file remains 0600 (owner read/write only)

## Migration Path

### Existing Jobs (Pre-Disaster-Recovery)
- Jobs created before this feature won't have metadata on S3
- Rebuild will attempt to recover from index copies only
- User must manually recreate job configuration
- Old backups remain accessible and restorable

### New Jobs
- Automatically save metadata to S3
- Full disaster recovery support
- Can be recovered automatically with `--rebuild`

## Next Steps

1. Complete Phase 1 implementation (master password + job metadata storage)
2. Implement Phase 2 (retirement marker)
3. Implement Phase 3 (rebuild command)
4. Test all scenarios
5. Update documentation
6. Create v0.0.7 release

