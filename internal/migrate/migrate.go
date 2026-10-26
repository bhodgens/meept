// Package migrate implements the central SQLite schema-migration registry
// for meept.
//
// Every SQLite-backed store declares its schema as an ordered list of
// forward-only migrations and calls [Apply] at open time. The applied
// version is tracked in the database header itself via
// PRAGMA user_version, so no bookkeeping table is needed and the version
// is transactional with the DDL it records.
//
// Contract (full text in docs/workflows/migrations.md):
//
//   - Migrations are numbered 1..N, strictly sequential, forward-only.
//     There is no down migration. Rollback is restore-from-backup
//     (internal/backup exists for exactly this).
//   - Each migration runs inside one transaction (DDL + user_version
//     bump together), so a crash mid-migration leaves the database at
//     the previous consistent version.
//   - Applied == current (Target): no-op.
//   - Applied < Target: pending steps run in order, stopping at the
//     first error (the store constructor fails; the database stays at
//     the last successful version).
//   - Applied > Target: a down-level binary against an up-level
//     database. [Apply] fails loudly with [ErrDownLevel] and never
//     silently proceeds — silently running an old schema against newer
//     data corrupts it.
//
// Databases that predate the registry may already carry a step's
// schema while user_version is still 0. A migration may declare an
// optional Guard: a read-only predicate run before the step; when it
// returns false the step's DDL is skipped and only the version is
// recorded, keeping the applied-version history contiguous. Fresh
// databases run every step unconditionally (guards return true).
//
// PRAGMA user_version on modernc.org/sqlite: supported for both read
// and write, including inside a transaction (verified empirically;
// see docs/workflows/migrations.md).
package migrate

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// ErrDownLevel is returned by [Apply] when the database records a schema
// version higher than the highest known migration — i.e. this binary is
// older than the database. Callers should surface it; proceeding would
// run stale schema assumptions against newer data.
var ErrDownLevel = errors.New("migrate: database schema is newer than this binary (down-level binary against up-level db)")

// Migration is a single numbered, forward-only schema step.
type Migration struct {
	// Version is the 1-based schema version this step produces.
	// Versions must be unique and form the sequence 1..N with no gaps.
	Version int
	// Name is a short human-readable identifier for logs and tests
	// (e.g. "add llm_calls session_id").
	Name string
	// SQL is the forward-only DDL/DML executed inside one transaction.
	// Multiple statements are allowed: modernc.org/sqlite's Exec runs
	// them all. Because the user_version bump happens in the same
	// transaction, a failed step leaves the database untouched.
	SQL string
	// Guard is an optional per-database predicate run OUTSIDE the step
	// transaction, before the step executes. Return true to run the
	// step, false to record it as already applied (skip the DDL, bump
	// the version). Guards exist for databases predating the registry:
	// they may already carry a step's columns (applied by the old
	// hand-rolled idempotent DDL) while user_version is still 0. Guard
	// must be a pure read of schema/data — it must not execute DDL.
	Guard func(db *sql.DB) (bool, error)
}

// Registry is an ordered set of migrations for one database identity
// (e.g. "task", "metrics"). It validates ordering at declaration time
// so a gap or duplicate versions fails at construction, not at open.
type Registry struct {
	identity string
	steps    []Migration
}

// NewRegistry validates versions (unique, sequential from 1) and returns
// a registry for the named database identity. The identity is advisory
// only — used in error messages — because user_version is per-database.
func NewRegistry(identity string, steps []Migration) (*Registry, error) {
	if len(steps) == 0 {
		return nil, fmt.Errorf("migrate %s: registry declares no migrations", identity)
	}
	sorted := make([]Migration, len(steps))
	copy(sorted, steps)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Version < sorted[j].Version })
	for i, m := range sorted {
		if m.Version != i+1 {
			return nil, fmt.Errorf("migrate %s: migration versions must be sequential 1..N with no gaps; got version %d at position %d", identity, m.Version, i)
		}
		if strings.TrimSpace(m.SQL) == "" {
			return nil, fmt.Errorf("migrate %s: migration %d (%s) has empty SQL", identity, m.Version, m.Name)
		}
	}
	return &Registry{identity: identity, steps: sorted}, nil
}

// Target returns the highest version this binary knows for the registry.
func (r *Registry) Target() int {
	return len(r.steps)
}

// userVersion reads PRAGMA user_version from db.
func userVersion(db *sql.DB) (int, error) {
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return 0, fmt.Errorf("migrate: read user_version: %w", err)
	}
	return v, nil
}

// setUserVersion writes PRAGMA user_version inside tx. PRAGMA assignments
// do not support parameter binding, but the value is a validated int.
func setUserVersion(tx *sql.Tx, v int) error {
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", v)); err != nil {
		return fmt.Errorf("migrate: set user_version %d: %w", v, err)
	}
	return nil
}

// Applied reads the database's current schema version without applying
// anything. Useful for tests and diagnostics.
func Applied(db *sql.DB) (int, error) {
	return userVersion(db)
}

// Apply brings the database up to the registry's target version.
//
//   - applied == Target: no-op.
//   - applied < Target: each pending step runs in its own transaction
//     (DDL + user_version bump), in order.
//   - applied > Target: returns an error wrapping [ErrDownLevel].
func (r *Registry) Apply(db *sql.DB, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	current, err := userVersion(db)
	if err != nil {
		return err
	}
	if current > len(r.steps) {
		return fmt.Errorf("migrate %s: db user_version=%d but this binary knows migrations only up to %d: %w",
			r.identity, current, len(r.steps), ErrDownLevel)
	}
	for _, m := range r.steps {
		if m.Version <= current {
			continue
		}
		if m.Guard != nil {
			needed, err := m.Guard(db)
			if err != nil {
				return fmt.Errorf("migrate %s: guard for migration %d (%s): %w", r.identity, m.Version, m.Name, err)
			}
			if !needed {
				// The pre-registry database already carries this
				// step's schema. Record the version without DDL so
				// the version history stays contiguous.
				if err := recordVersion(db, m.Version); err != nil {
					return fmt.Errorf("migrate %s: migration %d (%s): %w", r.identity, m.Version, m.Name, err)
				}
				logger.Info("schema migration already present", "db", r.identity, "version", m.Version, "name", m.Name)
				continue
			}
		}
		if err := applyStep(db, m); err != nil {
			return fmt.Errorf("migrate %s: migration %d (%s): %w", r.identity, m.Version, m.Name, err)
		}
		logger.Info("schema migration applied", "db", r.identity, "version", m.Version, "name", m.Name)
	}
	return nil
}

// recordVersion bumps user_version to v inside its own transaction,
// without running any DDL (guard-skip path).
func recordVersion(db *sql.DB, v int) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := setUserVersion(tx, v); err != nil {
		tx.Rollback() //nolint:errcheck // best-effort rollback; the pragma error is what surfaces
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// applyStep runs one migration (its SQL plus the version bump) inside a
// single transaction. On any error the transaction rolls back and the
// database remains at the previous version.
func applyStep(db *sql.DB, m Migration) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if _, err := tx.Exec(m.SQL); err != nil {
		tx.Rollback() //nolint:errcheck // best-effort rollback; the exec error is what surfaces
		return fmt.Errorf("exec: %w", err)
	}
	if err := setUserVersion(tx, m.Version); err != nil {
		tx.Rollback() //nolint:errcheck // best-effort rollback; the pragma error is what surfaces
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// ColumnExists reports whether table has a column named col. It is the
// guard predicate wired stores use for legacy databases that predate the
// registry (schema present, user_version 0).
func ColumnExists(db *sql.DB, table, col string) (bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, fmt.Errorf("migrate: pragma table_info(%s): %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, fmt.Errorf("migrate: scan table_info(%s): %w", table, err)
		}
		if name == col {
			return true, nil
		}
	}
	return false, rows.Err()
}

// IndexExists reports whether an index named name exists. Like
// ColumnExists, it is a guard helper: indexes created after a historical
// ALTER (see internal/metrics) must exist even on databases whose column
// already predates the registry.
func IndexExists(db *sql.DB, name string) (bool, error) {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&n); err != nil {
		return false, fmt.Errorf("migrate: index lookup %s: %w", name, err)
	}
	return n > 0, nil
}

// TableExists reports whether a table named name exists. Guard helper
// for baseline-DDL migrations and for migrations whose target table may
// not exist yet when Apply runs (see internal/task, whose tasks and
// task_steps tables are created by different constructors on a fresh
// database).
func TableExists(db *sql.DB, name string) (bool, error) {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n); err != nil {
		return false, fmt.Errorf("migrate: table lookup %s: %w", name, err)
	}
	return n > 0, nil
}

// ColumnsMissingGuard returns a Guard that reports the step as needed when
// ANY of the named columns is missing from table. Multi-column steps use
// this so a legacy database missing ANY of the step's columns is repaired
// (the pre-registry hand-rolled ALTERs re-ran on every open and repaired
// whatever was absent; this preserves that semantics).
func ColumnsMissingGuard(table string, columns ...string) func(*sql.DB) (bool, error) {
	return func(db *sql.DB) (bool, error) {
		for _, col := range columns {
			exists, err := ColumnExists(db, table, col)
			if err != nil {
				return false, err
			}
			if !exists {
				return true, nil
			}
		}
		return false, nil
	}
}

// ColumnSteps builds one guarded migration per column, numbered from
// startVersion. Each step adds a single column of the form
// "ADD COLUMN <col> <colType>" and is needed when the table exists and
// lacks that column. Use this for tables whose pre-registry databases
// could be missing an arbitrary SUBSET of columns (no per-batch version
// stamps existed); a batch step guarded on "any column missing" would
// re-run ALTERs for columns already present and die on the duplicate.
func ColumnSteps(startVersion int, table string, cols [][2]string) []Migration {
	steps := make([]Migration, 0, len(cols))
	for i, c := range cols {
		col, colType := c[0], c[1]
		steps = append(steps, Migration{
			Version: startVersion + i,
			Name:    table + "." + col,
			SQL:     "ALTER TABLE " + table + " ADD COLUMN " + col + " " + colType,
			Guard: func(db *sql.DB) (bool, error) {
				exists, err := TableExists(db, table)
				if err != nil {
					return false, err
				}
				if !exists {
					// Table not created yet on this open path;
					// record the version — the table's CREATE
					// script brings the current shape.
					return false, nil
				}
				has, err := ColumnExists(db, table, col)
				return !has, err
			},
		})
	}
	return steps
}
