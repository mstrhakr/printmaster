# Database Rotation and Recovery

## Overview

PrintMaster Agent implements automatic database rotation to recover from schema migration failures. When a database migration fails (e.g., due to corruption, interrupted migration, or schema conflicts), the agent automatically:

1. Renames the problematic database with a timestamp
2. Creates a fresh database with the correct schema
3. Logs the rotation event with paths to both the backup and new database
4. Continues normal operation

This ensures the agent can always start successfully, even if the database is in a broken state.

## How It Works

### Rotation Trigger

Database rotation occurs when:
- Schema initialization fails during startup
- A migration encounters an error (e.g., table already exists)
- The database file is corrupted or incompatible

### Rotation Process

1. **Detection**: `NewSQLiteStoreWithConfig()` catches schema initialization errors
2. **Backup**: Renames `devices.db` → `devices.db.backup.2025-11-06T10-37-28`
3. **WAL Files**: Also rotates `-wal` and `-shm` files if present
4. **Flag Set**: Stores rotation info in `agent.db` config for UI notification
5. **Fresh Start**: Recursively calls `NewSQLiteStoreWithConfig()` to create a new database
6. **Logging**: Records the event with ERROR and WARN level messages
7. **UI Warning**: On next page load, shows a confirmation dialog explaining what happened

### Log Messages

When rotation occurs, you'll see messages like:

```
[ERROR] Database schema initialization failed, attempting to rotate database
  error=failed to initialize schema: failed to run migrations: failed to rename metrics_history to metrics_raw: SQL logic error: there is already another table or index with this name: metrics_raw (1)
  path=C:\Users\YourUser\AppData\Local\PrintMaster\devices.db

[WARN] Database rotated due to migration failure - starting with fresh database
  backupPath=C:\Users\YourUser\AppData\Local\PrintMaster\devices.db.backup.2025-11-06T10-37-28
  newPath=C:\Users\YourUser\AppData\Local\PrintMaster\devices.db
  originalError=failed to initialize schema: failed to run migrations: ...
```

### User Notification

On the first page load after a rotation event, users will see a confirmation popup:

**Title**: "Database Rotation Notice"

**Message**: 
> The database was rotated due to a migration failure on 2025-11-06T10-37-28.
> 
> A fresh database has been created and the old database has been backed up to:
> C:\Users\YourUser\AppData\Local\PrintMaster\devices.db.backup.2025-11-06T10-37-28
> 
> All discovered devices and historical metrics data from the previous database are not available in the current session. If you need to recover data, you can manually restore the backup file.
> 
> Click OK to acknowledge this warning.

After the user clicks OK, the warning flag is cleared and won't show again.

## Backup Management

### Automatic Cleanup

On startup, the agent automatically cleans up old backup files:
- Keeps the **10 most recent backups** by default
- Removes older backups to prevent disk space accumulation
- Based on file modification time (newest kept, oldest removed)
- Non-fatal operation (logs warnings if cleanup fails)

**Example**: If you have 15 backup files, the 10 newest will be kept and the 5 oldest will be deleted on the next agent startup.

### Manual Recovery

If you need to recover data from a backup:

1. **Stop the agent**
2. **Locate the backup file** in the logs or data directory
3. **Rename it back** to `devices.db` (remove the `.backup.TIMESTAMP` suffix)
4. **Start the agent** - it will attempt to migrate the restored database

### Backup Location

Backups are stored in the same directory as the database:

- **Windows**: `C:\Users\<username>\AppData\Local\PrintMaster\`
- **macOS**: `~/Library/Application Support/PrintMaster/`
- **Linux**: `~/.local/share/PrintMaster/`

## Technical Details

### Functions

#### `RotateDatabase(dbPath string, configStore AgentConfigStore) (string, error)`
- Renames the database file with a timestamp suffix
- Also rotates associated WAL and SHM files
- Sets rotation flag in config store if provided (for UI notification)
- Returns the backup file path
- Fails for in-memory databases (`:memory:`)

#### `CleanupOldBackups(dbPath string, keepCount int) error`
- Removes old backup files, keeping only the N most recent
- Pattern: `devices.db.backup.*`
- Sorted by file modification time (newest first)
- keepCount specifies how many to retain (e.g., 10)
- Non-fatal - continues even if some files can't be deleted

#### `NewSQLiteStoreWithConfig(dbPath string, configStore AgentConfigStore) (*SQLiteStore, error)`
- Creates a new device store with optional config store for rotation tracking
- If schema init fails and dbPath is not `:memory:`, automatically rotates database
- Passes config store to `RotateDatabase()` for UI notification
- Recursively retries with fresh database after rotation

### Schema Migration (V7 → V8)

The most common rotation scenario is the V7→V8 migration that renames `metrics_history` to `metrics_raw`. If this migration is interrupted or run multiple times, both tables may exist, causing:

```
SQL logic error: there is already another table or index with this name: metrics_raw (1)
```

The rotation system handles this automatically by:
1. Backing up the broken database
2. Creating a fresh V8 database with the correct schema
3. Continuing normal operation

**Trade-off**: Historical metrics data is lost, but the agent remains operational. Manual recovery is possible if needed (see above).

## Testing

### Unit Tests

- `TestRotateDatabase`: Basic rotation functionality
- `TestCleanupOldBackups`: Backup cleanup behavior

### Integration Tests

- `TestDatabaseRotationOnMigrationFailure`: Simulates exact V7→V8 migration failure
- `TestDatabaseRotationLogging`: Verifies proper error/warning messages

Run tests:
```bash
go test ./agent/storage -run TestRotate
go test ./agent/storage -run TestCleanup
go test ./agent/storage -run TestDatabaseRotation
```

## Configuration

Currently, rotation behavior is automatic with these defaults:

- **Backup retention**: 10 most recent backups
- **Rotation trigger**: Any schema initialization failure
- **Max retry**: 1 (after rotation, fails if still broken)
- **UI notification**: Automatic popup on first page load after rotation

Future enhancements could make these configurable via `agent.db` settings.

## Monitoring

To monitor rotation events:

1. **Check logs** for ERROR/WARN messages containing "rotate"
2. **List backup files** in the data directory
3. **Track backup file count** (increasing count = recurring issues)

If rotation occurs frequently, investigate:
- Disk space issues causing partial writes
- Concurrent access from multiple agent instances
- File system corruption
- Insufficient permissions

## Best Practices

1. **Don't run multiple agents** with the same database path
2. **Monitor disk space** to prevent write failures
3. **Check logs after rotation** to understand what failed
4. **Keep backups** if historical data is important
5. **Test migrations** in development before deploying new versions

## Limitations

- **Data loss**: Rotation creates a fresh database, losing all historical data
- **No automatic merge**: Backup data must be manually recovered
- **Single retry**: If the second attempt fails, the agent exits
- **User acknowledgment required**: Warning popup blocks UI until acknowledged

Future improvements could include:
- Export backup data to JSON before rotation
- Attempt to recover non-corrupted tables
- Send email/webhook notifications on rotation events
- Configurable retention policies and backup count limits
- Automatic data migration from backup to new database
