# Episodic Memory Schema Migration Missing - `last_accessed_at` Column

**Date**: 2026-05-15
**Phase**: 3 (multi-agent orchestration)
**Severity**: medium
**Component**: `internal/memory/ftstore.go`, `internal/memory/episodic.go`

## Description

The episodic memory SQLite table was created in a prior daemon run with an older schema that lacks the `last_accessed_at` column. The current code uses `CREATE TABLE IF NOT EXISTS`, which silently skips table creation when the table already exists. Since there is no migration path, any INSERT that includes `last_accessed_at` fails with:

```
table episodic_memories has no column named last_accessed_at
```

This causes all episodic memory writes to fail, which triggers model retries and wastes LLM tokens.

## Reproduction

1. Have an existing `~/.meept/memory/episodic/episodic_memories.db` from a prior build
2. Start the daemon with a newer build that includes `last_accessed_at` in the schema
3. Send any chat that triggers a `memory_store` tool call with `type: "episodic"`
4. Observe error: `Tool execution failed ... error="failed to store memory: table episodic_memories has no column named last_accessed_at"`

## Evidence

```
level=DEBUG msg="Storing without FTS5 (slower search)" component=memory subsystem=episodic
level=ERROR msg="Tool execution failed" agent=coder tool=memory_store error="failed to store memory: failed to store memory: table episodic_memories has no column named last_accessed_at"
```

The schema in `episodic.go:18-29` defines `last_accessed_at TEXT NOT NULL DEFAULT ''`, and `episodic.go:162` INSERTs into that column, but the existing table on disk has no such column.

## Root Cause

`ftstore.go:initSchema()` (line 107-113) runs `CREATE TABLE IF NOT EXISTS` using the schema SQL from the config. When the table already exists with a different schema, SQLite silently succeeds without modifying the table. There is no ALTER TABLE migration for adding new columns.

## Proposed Fix

1. Add a schema versioning mechanism to `SQLiteFTSStore`:
   - Store a `schema_version` in a `_meta` table
   - On init, compare current schema version with stored version
   - Run ALTER TABLE statements to add missing columns

2. Alternatively, add a lightweight column check after table creation:
   ```go
   // Check if last_accessed_at column exists
   var colName string
   err := db.QueryRowContext(ctx,
       "SELECT name FROM pragma_table_info('episodic_memories') WHERE name = ?",
       "last_accessed_at").Scan(&colName)
   if err == sql.ErrNoRows {
       _, _ = db.ExecContext(ctx, "ALTER TABLE episodic_memories ADD COLUMN last_accessed_at TEXT NOT NULL DEFAULT ''")
   }
   ```

3. Generalize this into a migration framework in `ftstore.go` that runs all pending migrations from a `FTSConfig.Migrations []string` field.

## Model vs Harness
[X] Harness bug  [ ] Model quality issue  [ ] Both
