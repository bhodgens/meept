# internal/migrate

Central SQLite schema-migration registry. Every SQLite-backed store
declares its schema as an ordered list of forward-only migrations and
applies them through `migrate.Apply` at open time; the applied version
is stamped in `PRAGMA user_version` on the store's main db.

- Full contract, guard rationale, and the add-migration-N+1 procedure:
  [docs/workflows/migrations.md](migrations.md)
- Wired stores and their step counts: table in migrations.md
- Down-level binary (db newer than binary): hard error
  (`migrate.ErrDownLevel`), never silent
- Rollback: restore from backup (internal/backup); no down migrations
