---
name: go-sqlite-test-schema-divergence
description: |
  Fix Go integration test failures caused by test schema divergence from production.
  Use when: (1) Integration tests fail with "table has no column named X" errors,
  (2) Test fixtures define inline schemas that mirror production schemas, (3) Schema
  drift between test and production causes INSERT or SELECT failures. Covers schema
  synchronization patterns for SQLite integration tests.
author: Claude Code
version: 1.0.0
date: 2026-07-14
---

# Go SQLite Test Schema Divergence

## Problem

When writing Go integration tests that create temporary SQLite databases, it's common
to define inline schema constants for convenience. However, when production schemas
evolve (new columns added, types changed), test schemas drift out of sync, causing:

1. **INSERT failures**: Test tries to insert into columns that don't exist in test schema
2. **SELECT failures**: Production code queries columns missing from test schema
3. **Misleading errors**: "table has no column named X" when X exists in production but
   not test fixtures

## Context / Trigger Conditions

- Integration tests using `sql.Open("sqlite", ":memory:")` or temp files
- Test schemas defined as `const schema = "CREATE TABLE..."` constants
- Production code that has evolved since test schema was written
- Error messages like:
  - `SQL logic error: table has no column named last_activity (1)`
  - `SQL logic error: table sessions has no column named updated_at (1)`
  - `INSERT INTO ... last_activity` fails when column missing

## Solution

### Step 1: Identify the divergence

When you see "table has no column named X":

```bash
# Check what column production expects
grep -r "last_activity" internal/*/schema*.sql
grep -r "last_activity" internal/*/*.go | head -20

# Check what test schema defines
grep -A20 "CREATE TABLE.*sessions" tests/integration/*_test.go
```

### Step 2: Synchronize test schema with production

**Pattern A: Copy production schema structure**

```go
// WRONG: Test schema missing columns
const testSchema = `
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    created_at INTEGER,
    source_node TEXT
);
`

// RIGHT: Match production schema exactly
// (see internal/memory/schema_gossip.sql)
const testSchema = `
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    last_activity TEXT NOT NULL,           -- Added to match production
    metadata_json TEXT NOT NULL DEFAULT '{}', -- Named correctly (not "metadata")
    source_node TEXT NOT NULL
);
`
```

**Pattern B: Update INSERT statements to match schema**

```go
// WRONG: Inserting into non-existent columns
_, err = db.Exec(
    `INSERT INTO sessions (id, created_at, updated_at, metadata, source_node) VALUES (?, ?, ?, ?, ?)`,
    sid, time.Now().UnixNano(), time.Now().UnixNano(), []byte(`{}`), peerID,
)

// RIGHT: Match schema column names
_, err = db.Exec(
    `INSERT INTO sessions (id, created_at, last_activity, metadata_json, source_node) VALUES (?, ?, ?, ?, ?)`,
    sid, time.Now().UnixNano(), time.Now().Format(time.RFC3339), `{}`, peerID,
)
```

### Step 3: Verify table/query alignment for tests

When tests verify production code behavior, ensure queries match what production code writes:

```go
// WRONG: Test queries different table than production writes
// Production: StoreRemoteTurn() writes to "turns" table
// Test: Queries "memories" table
var count int
db.QueryRow(`SELECT COUNT(*) FROM memories WHERE id = ?`, expectedID).Scan(&count)

// RIGHT: Query the same table production code writes to
var count int
db.QueryRow(`SELECT COUNT(*) FROM turns WHERE turn_id = ? AND source_node = ?`,
    payload.TurnID, "node-peer").Scan(&count)
```

## Verification

After synchronizing schemas:

```bash
# Run the specific failing test
go test ./tests/integration/... -v -run TestSyncPull_MergePeerIntoGossipAndPersistMetadata

# Verify all integration tests pass
go test ./tests/integration/... -count=1

# Confirm no schema-related errors
# Expected: "PASS" or "ok github.com/caimlas/meept/tests/integration"
```

## Example

Full example from `tests/integration/sync_pull_integration_test.go`:

```go
// BEFORE: Schema divergence causing test failures
const gossipSchemaForSync = `
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    created_at INTEGER,
    updated_at INTEGER,    -- Wrong column name
    metadata BLOB,         -- Wrong type and name
    source_node TEXT
);
`

// Production code (internal/backup/merge.go) expects:
// INSERT OR IGNORE INTO sessions (id, created_at, last_activity, metadata_json, source_node)

// AFTER: Schema matches production
const gossipSchemaForSync = `
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    last_activity TEXT NOT NULL,           -- Matches production
    metadata_json TEXT NOT NULL DEFAULT '{}', -- Matches production
    source_node TEXT NOT NULL
);
`

// And update INSERT statements:
for i := 0; i < sessionRows; i++ {
    sid := id.Generate("sess-")
    _, err = peerDB.Exec(
        `INSERT INTO sessions (id, created_at, last_activity, metadata_json, source_node) VALUES (?, ?, ?, ?, ?)`,
        sid, time.Now().UnixNano(), time.Now().Format(time.RFC3339), `{}`, peerID,
    )
}
```

## Notes

- **Prefer schema imports**: If possible, import production schema files directly in
  tests rather than duplicating. However, inline schemas are sometimes necessary for
  test isolation.

- **Schema evolution discipline**: When adding columns to production schemas, search
  for all test schema constants and update them in the same commit.

- **INSERT OR IGNORE silent failures**: SQLite's `INSERT OR IGNORE` silently skips
  rows when constraints would be violated (e.g., missing NOT NULL columns). Tests
  may pass but no data is actually inserted. Always verify row counts after inserts.

- **Type matching matters**: Production uses `TEXT NOT NULL DEFAULT '{}'` for JSON,
  but test schema had `BLOB` - this can cause subtle type coercion issues.

- **Column naming consistency**: `metadata` vs `metadata_json` - a rename in
  production must be mirrored in test schemas.

## Related Patterns

- `go-sqlite-deadlock-testing` - Silent INSERT OR IGNORE failures, deadlocks
- `go-sqlite-migration-pattern` - Schema evolution and migrations
- `go-table-driven-test-fixtures` - Structured test data patterns
- `translation-layer-field-drop` - Silent data loss when translating between layers

## References

- [SQLite CREATE TABLE](https://www.sqlite.org/lang_createtable.html)
- [modernc.org/sqlite documentation](https://pkg.go.dev/modernc.org/sqlite)
- Meept project: `internal/memory/schema_gossip.sql` - Production schema
- Meept project: `internal/backup/merge.go` - Production merge logic
- Meept project: `tests/integration/sync_pull_integration_test.go` - Fixed test example
