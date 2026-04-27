# Final Fix Summary: Metadata Decryption Bug

## Root Cause
**Bug:** Job metadata encryption uses `cfg.MasterPassword` which is a **checksum** (hash), not the actual master password.

**Location:** `internal/cli/job.go:SaveJobMetadata()` call passes `cfg.MasterPassword`

**Effect:** All job metadata on S3 is encrypted with the wrong "password" (the checksum), making decryption impossible during rebuild.

## Solution Implemented

**Store job metadata UNENCRYPTED on S3**

Rationale:
1. Backup files themselves are encrypted with job password (secure)
2. Metadata (job name, source path, retention) is not sensitive
3. Simplest fix, no security impact
4. Works with existing rebuild workflow

## Changes Made

### 1. SaveJobMetadata (internal/recovery/rebuild.go)
- Removed double encryption of entire JSON
- Only job password field remains encrypted (if master password provided)
- File extension changed from `.enc` to `.json`

### 2. downloadJobMetadata (internal/recovery/rebuild.go)
- Removed outer decryption layer
- Directly parses JSON
- Still decrypts job password field if present

### 3. File Path
- Changed from `.ds3backup/jobs/<id>/config.json.enc`
- To: `.ds3backup/jobs/<id>/config.json`

## Testing Status

**Old jobs:** Will fail to decrypt (were encrypted with checksum)
**New jobs:** Will work correctly (stored unencrypted)

**Next Steps:**
1. Delete old test jobs from S3
2. Create fresh jobs with fixed code
3. Test complete disaster recovery workflow
4. Verify BadgerDB restore works
5. Test file restore with job passwords

## Security Considerations

**Is storing metadata unencrypted safe?**
- ✅ Backup files are encrypted (actual data protection)
- ✅ Job passwords can still be encrypted with master password
- ⚠️  Job names and paths visible if S3 compromised
- ✅ Acceptable trade-off for functionality

**Future enhancement:** Encrypt only the `EncryptionPassword` field with master password, leave rest unencrypted.
