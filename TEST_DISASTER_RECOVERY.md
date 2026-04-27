# Disaster Recovery Feature - Testing Guide

## Prerequisites

1. S3 bucket configured (Cubbit or AWS S3)
2. S3 credentials (access key, secret key, endpoint, bucket name)
3. Test directory with some files

## Test Sequence

### Test 1: Initialize with Master Password

```bash
# Interactive mode (prompts for password)
./ds3backup init \
  --endpoint=s3.cubbit.eu \
  --bucket=YOUR_BUCKET \
  --access-key=YOUR_ACCESS_KEY \
  --secret-key=YOUR_SECRET_KEY

# Enter master password when prompted: "TestMaster123"
# Confirm password: "TestMaster123"
```

**Expected Output:**
```
✓ S3 connection successful
✓ Master password configured
✓ DS3 Backup initialized successfully!
  Master Password: enabled (encrypts job configurations)
```

**Verify:**
```bash
cat ~/.ds3backup/config.json | grep -i master
# Should show masterPassword field with checksum
```

---

### Test 2: Create Job (Metadata Saved to S3)

```bash
./ds3backup job add \
  --name="TestBackup" \
  --path=/path/to/test/files \
  --password="JobPassword123"
```

**Expected Output:**
```
✓ Backup job created successfully!
  Job ID: job_xxxxxxxxxxxxxxxxx
Saving job metadata to S3...
  ✓ Job metadata saved to S3
```

**Verify on S3:**
```bash
# Check S3 bucket for .ds3backup/jobs/<job-id>/config.json.enc
# File should exist and be encrypted
```

---

### Test 3: Run Backup

```bash
./ds3backup backup run <job-id-from-previous-step>
```

**Expected Output:**
```
✓ Backup completed
   Files added: X
   Index copy saved
```

**Verify on S3:**
```bash
# Check for:
# 1. Backup files: backups/<job-id>/files/*.enc
# 2. Batch files: backups/<job-id>/batches/*.enc
# 3. Index copy: backups/<job-id>/index_<timestamp>/backup
```

---

### Test 4: Test Retirement Marker (Requires COMPLIANCE Mode)

⚠️ **Warning**: This test requires a bucket with COMPLIANCE Object Lock enabled.
Skip this test if your bucket doesn't support COMPLIANCE mode.

```bash
# Create job with COMPLIANCE mode
./ds3backup job add \
  --name="ComplianceTest" \
  --path=/path/to/test/files \
  --password="JobPassword123" \
  --object-lock-mode=COMPLIANCE

# Run backup
./ds3backup backup run <compliance-job-id>

# Attempt to delete with --clean (should fail and create retirement marker)
./ds3backup job delete <compliance-job-id> --clean
```

**Expected Output:**
```
🔒 Locked: X objects (protected by COMPLIANCE Object Lock)
⚠️  Creating retirement marker...
✓ Retirement marker created
Error: cannot delete X objects: protected by COMPLIANCE Object Lock
```

**Verify on S3:**
```bash
# Check for: .ds3backup/index/<job-id>/RETIRED-DO-NOT-REBUILD
# File should exist (empty file)
```

---

### Test 5: Test Rebuild Command

⚠️ **Prerequisite**: Backup your current config first!

```bash
# Backup current config
cp ~/.ds3backup/config.json ~/.ds3backup/config.json.backup

# Delete local config to simulate disaster
rm ~/.ds3backup/config.json

# Run rebuild
./ds3backup init --rebuild \
  --endpoint=s3.cubbit.eu \
  --bucket=YOUR_BUCKET \
  --access-key=YOUR_ACCESS_KEY \
  --secret-key=YOUR_SECRET_KEY

# Enter master password when prompted: "TestMaster123"
```

**Expected Output:**
```
Scanning S3 bucket for backup jobs...
Found X backup job(s):
  1. TestBackup (job_xxxxxxxxxxxxxxxxx)
  ⚠️  Skipping retired job: job_yyyyyyyyyyyyyyyyy (if COMPLIANCE test was done)

  ✓ Decrypted password for TestBackup
  (or) Enter password for TestBackup: *****

Rebuilding indexes...
  Rebuilding TestBackup... ✓ Success

Updating configuration...

✓ Recovered X job(s)
```

**Verify:**
```bash
# Check config was restored
cat ~/.ds3backup/config.json | grep -A 5 "jobs"

# List jobs
./ds3backup job list

# Should show recovered job(s)
```

---

### Test 6: Verify Recovered Job Works

```bash
# Run backup with recovered job
./ds3backup backup run <recovered-job-id>
```

**Expected Output:**
```
✓ Backup completed
```

---

## Test Results Template

### Test 1: Master Password Initialization
- [ ] Interactive password prompt works
- [ ] Password confirmation works
- [ ] Checksum stored in config
- [ ] Non-interactive mode with --master-password works

### Test 2: Job Metadata Storage
- [ ] Job created successfully
- [ ] Metadata file exists on S3
- [ ] Metadata is encrypted (if master password set)
- [ ] Metadata is unencrypted (if no master password)

### Test 3: Backup with Index Copy
- [ ] Backup completes successfully
- [ ] Index copy saved to S3
- [ ] Index copy location: backups/<job-id>/index_<timestamp>/

### Test 4: Retirement Marker (COMPLIANCE only)
- [ ] Delete fails with COMPLIANCE error
- [ ] Retirement marker created on S3
- [ ] Marker path: .ds3backup/index/<job-id>/RETIRED-DO-NOT-REBUILD

### Test 5: Rebuild Command
- [ ] Jobs discovered from S3
- [ ] Retired jobs skipped
- [ ] Passwords decrypted (or prompted)
- [ ] Indexes downloaded
- [ ] Config restored
- [ ] Jobs listed correctly

### Test 6: Recovered Job Functionality
- [ ] Backup runs successfully
- [ ] Restore works with recovered job

---

## Troubleshooting

### Issue: "failed to decrypt job config"
**Cause**: Wrong master password
**Solution**: Ensure you're using the same master password set during init

### Issue: "no metadata found for job"
**Cause**: Job created before disaster recovery feature
**Solution**: Job will be recovered with default name, manually update config

### Issue: "rebuild failed: no index copies found"
**Cause**: Index copies not uploaded (old backup)
**Solution**: Run new backup to create index copy, then rebuild

### Issue: "failed to connect to S3"
**Cause**: Wrong credentials or network issue
**Solution**: Verify S3 credentials and network connectivity

---

## Cleanup

After testing, clean up test resources:

```bash
# Remove test jobs from S3 (if not COMPLIANCE-protected)
./ds3backup job delete <job-id> --clean

# Remove test config
rm ~/.ds3backup/config.json

# Restore original config (if backed up)
mv ~/.ds3backup/config.json.backup ~/.ds3backup/config.json
```

---

## Success Criteria

All tests pass if:
1. ✅ Master password can be set and verified
2. ✅ Job metadata is saved to S3 (encrypted/unencrypted based on master password)
3. ✅ Backup creates index copies on S3
4. ✅ Retirement marker created for COMPLIANCE-protected jobs
5. ✅ Rebuild discovers jobs from S3
6. ✅ Rebuild skips retired jobs
7. ✅ Rebuild restores job configuration
8. ✅ Recovered jobs function normally

---

**Test Date**: _______________
**Tester**: _______________
**S3 Provider**: _______________
**Result**: [ ] PASS  [ ] FAIL  [ ] PARTIAL

**Notes**:
_______________________________________
_______________________________________
_______________________________________
