package plan

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// State enum pinning for the plans database (audit 2026-11-09 F6, leaf 06 T6),
// mirroring the queue jobs.state pattern: the base schema carries the CHECK so
// fresh databases are born enforcing, and migrateStateChecks() carries
// pre-CHECK databases through a guarded table rebuild. The DB refuses garbage
// the code would never write — the manager's transition table is the
// lifecycle authority; the CHECK is the persistence-layer backstop against
// rogue writers (manual sqlite edits, older binaries) landing states the
// machine cannot interpret.
const (
	plansStateEnumSQL = `('planning','draft','pending_approval','approved','synthesizing',` +
		`'executing','completed','confirmed','cancelled','failed')`
	phasesStateEnumSQL = `('pending','in_progress','completed','confirmed','failed')`
)

// rebuiltPlansDDL is the exact post-CHECK schema for plans, hand-written (not
// PRAGMA-captured) because the captured-column rebuild would silently drop the
// column-level REFERENCES/ON DELETE CASCADE that plan_phases/plan_sessions/
// plan_signoffs depend on. It equals the base schema below PLUS the state
// CHECK; every column added by storeMigrations v1-v5 is present because the
// rebuild runs AFTER those ALTERs (probe makes it a no-op on fresh DBs).
const rebuiltPlansDDL = `CREATE TABLE plans_new (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	description TEXT,
	file_path TEXT NOT NULL,
	project_id TEXT,
	state TEXT NOT NULL DEFAULT 'planning' CHECK (state IN ` + plansStateEnumSQL + `),
	task_id TEXT,
	source_session TEXT,
	approved_at TEXT,
	confirmed_at TEXT,
	approved_by TEXT,
	confirmed_by TEXT,
	revision_count INTEGER DEFAULT 0,
	synthesis_attempts INTEGER DEFAULT 0,
	synthesis_lease_until TEXT,
	goal_id TEXT,
	prompt TEXT,
	result_topic TEXT,
	synthesis_error TEXT,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
)`

// rebuiltPhasesDDL: same rule; plan_id keeps its FK to plans with CASCADE
// (DeletePlan's cascade semantics survive the rebuild — pinned in tests).
const rebuiltPhasesDDL = `CREATE TABLE plan_phases_new (
	id TEXT PRIMARY KEY,
	plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	sequence INTEGER NOT NULL,
	total_steps INTEGER NOT NULL DEFAULT 0,
	completed_steps INTEGER NOT NULL DEFAULT 0,
	failed_steps INTEGER NOT NULL DEFAULT 0,
	state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ` + phasesStateEnumSQL + `),
	produces TEXT,
	consumes TEXT,
	task_id TEXT
)`

// migrateStateChecks rebuilds plans and plan_phases onto the CHECK-pinned
// schema when the existing table SQL lacks a CHECK. SQLite cannot ALTER TABLE
// ADD CONSTRAINT, so each swap is: create the new table, copy rows, drop the
// old, rename the new into place, recreate the captured indexes. The child
// tables reference parents BY NAME (plan_phases -> plans, plan_signoffs ->
// plans/plan_phases), so the rebuilt table keeping the same name leaves every
// FK clause intact.
//
// FK-safety: DROP TABLE on a parent with child rows fails while foreign_keys
// is enforced, so the whole swap runs on ONE pooled connection with
// PRAGMA foreign_keys=OFF set before the transaction (the pragma is a no-op
// inside a transaction). A final PRAGMA foreign_key_check proves the swap did
// not break any FK edge in either direction; a violation fails and rolls back.
//
// Pre-flight data rule (same as queue F7): any row with a state outside the
// enum FAILS the migration with the offending values listed — data is never
// deleted or rewritten to make a constraint fit.
func (s *SQLiteStore) migrateStateChecks() error {
	// One dedicated connection: the pragma and the transaction must share it,
	// and the pool must not hand another session's pragma state to the swap.
	conn, err := s.db.Conn(context.Background())
	if err != nil {
		return fmt.Errorf("state CHECK migration: acquire conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(context.Background(), `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("state CHECK migration: pragma foreign_keys off: %w", err)
	}
	defer func() {
		if _, rErr := conn.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`); rErr != nil {
			s.logger.Debug("state CHECK migration: restore foreign_keys failed", "error", rErr)
		}
	}()

	for _, swap := range []struct {
		table   string
		newDDL  string
		enumSQL string
	}{
		{"plans", rebuiltPlansDDL, plansStateEnumSQL},
		{"plan_phases", rebuiltPhasesDDL, phasesStateEnumSQL},
	} {
		hasCheck, err := tableHasCheck(context.Background(), conn, swap.table)
		if err != nil {
			return err
		}
		if hasCheck {
			continue // fresh schema or already migrated — no-op
		}
		if err := rebuildTableWithCheck(context.Background(), conn, s.logger, swap.table, swap.newDDL, swap.enumSQL); err != nil {
			return err
		}
	}
	return nil
}

// tableHasCheck probes sqlite_master for the table's CREATE SQL (uppercase
// contains CHECK — same probe rule as the queue migration).
func tableHasCheck(ctx context.Context, conn *sql.Conn, table string) (bool, error) {
	var tableSQL string
	err := conn.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&tableSQL)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil // table absent (base schema creates it on fresh DBs; defensive)
	}
	if err != nil {
		return false, fmt.Errorf("state CHECK migration probe %s: %w", table, err)
	}
	return strings.Contains(strings.ToUpper(tableSQL), "CHECK"), nil
}

// rebuildTableWithCheck performs one guarded table swap in a transaction.
func rebuildTableWithCheck(ctx context.Context, conn *sql.Conn, logger *slog.Logger, table, newDDL, enumSQL string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin state CHECK migration for %s: %w", table, err)
	}
	committed := false
	defer func() {
		if !committed {
			if rErr := tx.Rollback(); rErr != nil && !errors.Is(rErr, sql.ErrTxDone) {
				logger.Debug("state CHECK migration rollback (no-op after commit)", "table", table, "error", rErr)
			}
		}
	}()

	newName := table + "_new"

	// Pre-flight: refuse rather than destroy.
	var badCount int
	var badStates sql.NullString
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT
		(SELECT COUNT(*) FROM %[1]s WHERE state IS NOT NULL AND state NOT IN %[2]s),
		(SELECT group_concat(s, ', ') FROM
			(SELECT DISTINCT state AS s FROM %[1]s WHERE state IS NOT NULL AND state NOT IN %[2]s))`,
		table, enumSQL)).Scan(&badCount, &badStates); err != nil {
		return fmt.Errorf("state CHECK migration pre-flight %s: %w", table, err)
	}
	if badCount > 0 {
		return fmt.Errorf("state CHECK migration refused for %s: %d row(s) have state outside the enum (values: %s); repair them before upgrading",
			table, badCount, badStates.String)
	}

	// Live column set, in table order (copy list must match exactly; the DDL
	// above is hand-written and must be a superset — a legacy column the DDL
	// does not know about fails loudly at the INSERT below rather than
	// silently dropping data).
	colRows, err := tx.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return fmt.Errorf("state CHECK migration table_info %s: %w", table, err)
	}
	var cols []string
	// Drained inside a closure: the cursor must release the statement
	// before the rebuild Execs below (defer to function exit would hold
	// it across them).
	if err := func() error {
		defer colRows.Close()
		for colRows.Next() {
			var (
				cid     int
				name    string
				ctype   string
				notnull int
				dflt    sql.NullString
				pk      int
			)
			if err := colRows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				return fmt.Errorf("state CHECK migration table_info scan %s: %w", table, err)
			}
			cols = append(cols, name)
		}
		if err := colRows.Err(); err != nil {
			return fmt.Errorf("state CHECK migration table_info iteration %s: %w", table, err)
		}
		return nil
	}(); err != nil {
		return err
	}
	if len(cols) == 0 {
		return fmt.Errorf("state CHECK migration: PRAGMA table_info(%s) returned no columns", table)
	}

	// Captured index SQL (explicit indexes only; the implicit PRIMARY KEY
	// autoindex travels with the new CREATE).
	idxRows, err := tx.QueryContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type='index' AND tbl_name=? AND sql IS NOT NULL`, table)
	if err != nil {
		return fmt.Errorf("state CHECK migration index capture %s: %w", table, err)
	}
	var indexSQL []string
	if err := func() error {
		defer idxRows.Close()
		for idxRows.Next() {
			var sqlText string
			if err := idxRows.Scan(&sqlText); err != nil {
				return fmt.Errorf("state CHECK migration index scan %s: %w", table, err)
			}
			indexSQL = append(indexSQL, sqlText)
		}
		if err := idxRows.Err(); err != nil {
			return fmt.Errorf("state CHECK migration index iteration %s: %w", table, err)
		}
		return nil
	}(); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, newDDL); err != nil {
		return fmt.Errorf("state CHECK migration create %s: %w", newName, err)
	}
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = quoteSQLIdent(c)
	}
	colsJoined := strings.Join(quoted, ", ")
	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s", newName, colsJoined, colsJoined, table)); err != nil {
		return fmt.Errorf("state CHECK migration copy rows %s: %w", table, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DROP TABLE %s`, quoteSQLIdent(table))); err != nil {
		return fmt.Errorf("state CHECK migration drop old %s: %w", table, err)
	}
	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf(`ALTER TABLE %s RENAME TO %s`, quoteSQLIdent(newName), quoteSQLIdent(table))); err != nil {
		return fmt.Errorf("state CHECK migration rename %s: %w", table, err)
	}
	for _, stmt := range indexSQL {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("state CHECK migration recreate index %q: %w", stmt, err)
		}
	}

	// FK proof: the swap kept every child reference edge intact. The pragma
	// returns one row per violation; any row fails the migration and rolls
	// the whole thing back.
	fkRows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("state CHECK migration foreign_key_check after %s: %w", table, err)
	}
	violations := 0
	if err := func() error {
		defer fkRows.Close()
		for fkRows.Next() {
			violations++
		}
		if err := fkRows.Err(); err != nil {
			return fmt.Errorf("state CHECK migration foreign_key_check scan %s: %w", table, err)
		}
		return nil
	}(); err != nil {
		return err
	}
	if violations > 0 {
		return fmt.Errorf("state CHECK migration found %d foreign-key violation(s) after rebuilding %s", violations, table)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit state CHECK migration for %s: %w", table, err)
	}
	committed = true
	logger.Warn("state CHECK migration: rebuilt table", "table", table)
	return nil
}

// quoteSQLIdent double-quotes a SQL identifier (same rule as the queue
// migration helper; names here come from PRAGMA table_info, not user input).
func quoteSQLIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
