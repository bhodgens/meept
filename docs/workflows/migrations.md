# SQLite Schema Migrations (internal/migrate)

Central registry for every SQLite-backed store's schema history. One
contract, one version stamp, one direction: forward.

## The registry contract

Each store declares its migrations as an ordered list of numbered,
forward-only SQL steps and calls `migrate.Apply` at open time:

```go
var storeMigrations = func() *migrate.Registry {
    r, err := migrate.NewRegistry("my-store", []migrate.Migration{
        {Version: 1, Name: "baseline", SQL: baselineDDL},
        {Version: 2, Name: "add column x", SQL: "ALTER TABLE t ADD COLUMN x TEXT"},
    })
    if err != nil {
        panic(fmt.Sprintf("my-store migration registry invalid: %v", err))
    }
    return r
}()
```

- **Identity**: the registry is keyed by a store identity string used in
  logs and errors. The applied version lives in the database itself via
  `PRAGMA user_version` on the store's main db -- no bookkeeping table.
- **One transaction per step**: each migration's SQL and its
  `user_version` bump run inside a single transaction. A crash mid-step
  leaves the database at the previous consistent version. (Verified:
  modernc.org/sqlite supports reading and writing `user_version`,
  including inside a transaction.)
- **No-op when current**: applied == target means Apply does nothing.
- **Resume from below**: applied < target runs the pending steps in
  order, stopping at the first error (the store constructor fails; the
  database stays at the last good version).
- **Down-level binary fails loud**: applied > target is a down-level
  binary against an up-level database. Apply returns an error wrapping
  `migrate.ErrDownLevel` and never silently proceeds -- running stale
  schema assumptions against newer data corrupts the database.

## Guards (pre-registry databases)

Databases created before the registry existed already carry migrated
columns while `user_version` is still 0. A migration may declare a
read-only `Guard` predicate; when it returns false (schema already
present), the step's DDL is skipped and only the version is recorded,
keeping the history contiguous. Fresh databases run every step
(guards return true).

Helpers: `migrate.ColumnExists`, `migrate.IndexExists`,
`migrate.TableExists`, `migrate.ColumnsMissingGuard` (needed when ANY
column is missing), `migrate.ColumnSteps` (one guarded step per column,
for tables whose pre-registry databases could miss an arbitrary subset).

## How to add migration N+1

1. Append one `Migration` to the store's registry declaration. The
   registry constructor validates versions (strictly sequential 1..N,
   no gaps or duplicates) at package init -- an invalid history panics
   at startup, never at open time.
2. Write forward-only SQL. Multiple statements are allowed. Never edit
   an already-shipped step's SQL: databases that already applied it
   recorded the version, and editing the step would make the recorded
   history lie about the schema. A wrong shipped migration is fixed by
   a NEW step that repairs forward.
3. Update any schema-shape query paths (column lists in INSERT/SELECT)
   in the same commit.
4. If the step touches a table whose CREATE script lives in the same
   package, keep the CREATE script at the current shape: fresh
   databases must end byte-identical (query-wise) to migrated-up ones.
   The e2e suite `e2e/suites/migrate-eq` pins this per wired store
   (fresh vs migrated-up schema equality via pragma dumps).

## Why forward-only

Down migrations double the surface that must be tested, cannot undo
data loss (a dropped column's data is gone), and are almost never
exercised in production. Forward-only keeps every database on a linear,
comparable history.

## Rollback = restore from backup

There is no down migration. If a migration breaks a deployment, restore
the database file from backup (`internal/backup` exists for exactly
this) and fix forward. A migration bug is repaired by migration N+2,
never by editing N.

## Wired stores

| Store                | Registry identity | Steps            |
|----------------------|-------------------|------------------|
| internal/task        | task              | 1..30 (shared db between Store and StepStore) |
| internal/metrics     | metrics           | 1..6             |
| internal/llm/metrics | llm-metrics       | 1..2             |
| internal/plan        | plan              | 1..3             |
| internal/shadow      | shadow/training, shadow/examples, shadow/adapters | per-store |
| internal/memory      | memory/<table>    | per-FTS-store    |

The task database file is shared by two constructors (Store and
StepStore); its migrations form ONE contiguous history so `user_version`
stays a single per-file counter.
