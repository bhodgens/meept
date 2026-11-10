package plan

import (
	"database/sql"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite" //nolint:revive // blank import for side effects
)

// leaf 06 T6 (audit F6): plans.state + plan_phases.state CHECK constraints.
// Mirrors the queue jobs F7 test trio: fresh DB refuses garbage, a legacy
// pre-CHECK DB is carried through the rebuild with rows + indexes + FK
// cascade intact, and out-of-enum rows REFUSE the migration without data
// loss. employee_goals is deliberately NOT CHECK-pinned: its state is an INT
// enum whose ParseGoalState fence already rejects unknown values at load
// (leaf 05), and a CHECK on INTEGER would duplicate that guard at the DB
// layer for no new coverage.

// legacyPlanSchemaDDL is the pre-CHECK plans database: no CHECKs, and the
// plan_phases table WITHOUT the produces/consumes/task_id columns that
// storeMigrations v1 adds — exactly what an on-disk database from an old
// binary looks like. The rebuild must see the post-migration column set.
const legacyPlanSchemaDDL = `
CREATE TABLE plans (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	description TEXT,
	file_path TEXT NOT NULL,
	project_id TEXT,
	state TEXT NOT NULL DEFAULT 'planning',
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
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX idx_plans_state ON plans(state);
CREATE INDEX idx_plans_project ON plans(project_id);
CREATE INDEX idx_plans_updated_at ON plans(updated_at DESC);

CREATE TABLE plan_phases (
	id TEXT PRIMARY KEY,
	plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	sequence INTEGER NOT NULL,
	total_steps INTEGER NOT NULL DEFAULT 0,
	completed_steps INTEGER NOT NULL DEFAULT 0,
	failed_steps INTEGER NOT NULL DEFAULT 0,
	state TEXT NOT NULL DEFAULT 'pending'
);
CREATE INDEX idx_plan_phases_plan ON plan_phases(plan_id);

CREATE TABLE plan_sessions (
	plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
	session_id TEXT NOT NULL,
	linked_at TEXT NOT NULL,
	PRIMARY KEY (plan_id, session_id)
);
CREATE INDEX idx_plan_sessions_session ON plan_sessions(session_id);

CREATE TABLE plan_signoffs (
	id TEXT PRIMARY KEY,
	plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
	phase_id TEXT REFERENCES plan_phases(id),
	session_id TEXT NOT NULL,
	by TEXT NOT NULL,
	action TEXT NOT NULL,
	comment TEXT,
	created_at TEXT NOT NULL
);
CREATE INDEX idx_plan_signoffs_plan ON plan_signoffs(plan_id);
`

// createLegacyPlanDB writes a pre-CHECK database with one intact plan +
// phase + session + signoff row (the phase carries a distinctive name that
// must survive the rebuild byte-for-byte).
func createLegacyPlanDB(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(legacyPlanSchemaDDL); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	// Drop the migration-carried columns the old binary had already applied
	// (user_version stays 0 — the migrate registry probes and skips). This
	// fixture simulates the TRUE pre-migration shape: the store's registry
	// ALTERs add them back before the CHECK rebuild runs.
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO plans (id, title, file_path, project_id, state, created_at, updated_at)
		VALUES ('plan-legacy', 'Legacy Plan', '/tmp/legacy.md', '', 'executing', ?, ?)`, now, now); err != nil {
		t.Fatalf("insert legacy plan: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO plan_phases (id, plan_id, name, sequence, state)
		VALUES ('phase-legacy', 'plan-legacy', 'Survive-Me', 1, 'in_progress')`); err != nil {
		t.Fatalf("insert legacy phase: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO plan_sessions (plan_id, session_id, linked_at)
		VALUES ('plan-legacy', 'sess-legacy', ?)`, now); err != nil {
		t.Fatalf("insert legacy session link: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO plan_signoffs (id, plan_id, session_id, by, action, created_at)
		VALUES ('sign-legacy', 'plan-legacy', 'sess-legacy', 'op', 'approve', ?)`, now); err != nil {
		t.Fatalf("insert legacy signoff: %v", err)
	}
}

func planTableHasCheck(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var tableSQL string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&tableSQL); err != nil {
		t.Fatalf("read %s sql: %v", table, err)
	}
	return strings.Contains(strings.ToUpper(tableSQL), "CHECK")
}

func planIndexNames(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name=? AND name NOT LIKE 'sqlite_%'`, table)
	if err != nil {
		t.Fatalf("index query: %v", err)
	}
	defer rows.Close()
	names := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("index scan: %v", err)
		}
		names[n] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("index iteration: %v", err)
	}
	return names
}

// TestPlansStateCheckFreshDBRefusesGarbage pins the base-schema half: a
// database created by the CURRENT code rejects an out-of-enum state at the
// SQL layer for BOTH tables.
func TestPlansStateCheckFreshDBRefusesGarbage(t *testing.T) {
	store := setupTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC().Format(time.RFC3339)

	if !planTableHasCheck(t, store.DB(), "plans") {
		t.Fatal("fresh plans table lacks the state CHECK")
	}
	if !planTableHasCheck(t, store.DB(), "plan_phases") {
		t.Fatal("fresh plan_phases table lacks the state CHECK")
	}

	_, err := store.DB().ExecContext(ctx, `INSERT INTO plans (id, title, file_path, state, created_at, updated_at)
		VALUES ('garbage-plan', 'g', '/tmp/g.md', 'wat_is_this', ?, ?)`, now, now)
	if err == nil {
		t.Fatal("expected constraint error on plans.state garbage insert, got none")
	}
	_, err = store.DB().ExecContext(ctx, `INSERT INTO plan_phases (id, plan_id, name, sequence, state)
		VALUES ('garbage-phase', 'nope', 'g', 1, 'not_a_phase_state')`)
	if err == nil {
		t.Fatal("expected constraint error on plan_phases.state garbage insert, got none")
	}

	// The enum values remain insertable (CHECK is the whitelist, not a ban).
	if err := store.CreatePlan(ctx, newTestPlan("valid-state")); err != nil {
		t.Fatalf("insert of a valid plan failed: %v", err)
	}
}

// TestPlansStateCheckMigratedDB pins the rebuild path: a legacy pre-CHECK
// database opens, gains both CHECKs, keeps every index, preserves rows, and
// keeps ON DELETE CASCADE working through the swap. Reopen is a silent no-op.
func TestPlansStateCheckMigratedDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	createLegacyPlanDB(t, dbPath)

	// Fixture guard: the seeded DB has no CHECKs.
	probeDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("probe open: %v", err)
	}
	if planTableHasCheck(t, probeDB, "plans") || planTableHasCheck(t, probeDB, "plan_phases") {
		probeDB.Close()
		t.Fatal("fixture already has CHECK — fixture is invalid")
	}
	probeDB.Close()

	store, err := NewSQLiteStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("open legacy db through store: %v", err)
	}
	defer store.Close()

	// 1. Both CHECKs landed.
	if !planTableHasCheck(t, store.DB(), "plans") {
		t.Fatal("plans table still lacks CHECK after migration")
	}
	if !planTableHasCheck(t, store.DB(), "plan_phases") {
		t.Fatal("plan_phases table still lacks CHECK after migration")
	}

	// 2. Garbage INSERT refused on the migrated DB.
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := store.DB().Exec(`INSERT INTO plans (id, title, file_path, state, created_at, updated_at)
		VALUES ('garbage-migrated', 'g', '/tmp/g.md', 'wat_is_this', ?, ?)`, now, now); err == nil {
		t.Fatal("expected constraint error on migrated plans DB, got none")
	}
	if _, err := store.DB().Exec(`INSERT INTO plan_phases (id, plan_id, name, sequence, state)
		VALUES ('garbage-migrated-phase', 'plan-legacy', 'g', 1, 'bogus')`); err == nil {
		t.Fatal("expected constraint error on migrated plan_phases DB, got none")
	}

	// 3. Rows survived intact.
	var name string
	if err := store.DB().QueryRow(`SELECT name FROM plan_phases WHERE id = 'phase-legacy'`).Scan(&name); err != nil {
		t.Fatalf("legacy phase row lost: %v", err)
	}
	if name != "Survive-Me" {
		t.Errorf("legacy phase name = %q, want Survive-Me", name)
	}
	var state string
	if err := store.DB().QueryRow(`SELECT state FROM plans WHERE id = 'plan-legacy'`).Scan(&state); err != nil {
		t.Fatalf("legacy plan row lost: %v", err)
	}
	if state != "executing" {
		t.Errorf("legacy plan state = %q, want executing", state)
	}

	// 4. Index parity — every legacy index survived its table's rebuild.
	plansIdx := planIndexNames(t, store.DB(), "plans")
	for _, want := range []string{"idx_plans_state", "idx_plans_project", "idx_plans_updated_at"} {
		if !plansIdx[want] {
			t.Errorf("index %s missing after plans rebuild (have %v)", want, plansIdx)
		}
	}
	phasesIdx := planIndexNames(t, store.DB(), "plan_phases")
	if !phasesIdx["idx_plan_phases_plan"] {
		t.Errorf("idx_plan_phases_plan missing after rebuild (have %v)", phasesIdx)
	}
	sessIdx := planIndexNames(t, store.DB(), "plan_sessions")
	if !sessIdx["idx_plan_sessions_session"] {
		t.Errorf("idx_plan_sessions_session missing (untouched table must keep its index; have %v)", sessIdx)
	}

	// 5. FK cascade through the rebuilt parent: deleting the plan must still
	// take the phase/session/signoff rows (ON DELETE CASCADE clause kept).
	ctx := t.Context()
	if err := store.DeletePlan(ctx, "plan-legacy"); err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}
	var count int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM plan_phases WHERE plan_id = 'plan-legacy'`).Scan(&count); err != nil {
		t.Fatalf("count phases: %v", err)
	}
	if count != 0 {
		t.Errorf("phase rows survived plan delete: cascade FK broken by rebuild (count=%d)", count)
	}
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM plan_sessions WHERE plan_id = 'plan-legacy'`).Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if count != 0 {
		t.Errorf("plan_sessions rows survived plan delete (count=%d)", count)
	}

	// 6. Idempotence: reopening the migrated DB keeps CHECKs + index counts.
	before := len(planIndexNames(t, store.DB(), "plans"))
	store.Close()
	store2, err := NewSQLiteStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("reopen migrated db: %v", err)
	}
	defer store2.Close()
	if !planTableHasCheck(t, store2.DB(), "plans") {
		t.Fatal("plans CHECK vanished on reopen")
	}
	if got := len(planIndexNames(t, store2.DB(), "plans")); got != before {
		t.Errorf("plans index count drift on reopen: %d vs %d", got, before)
	}
}

// TestPlansStateCheckMigrationRefusesGarbageRows pins the no-data-loss rule:
// a legacy DB with an out-of-enum row FAILS store open outright, names the
// offending value, and leaves the row untouched on disk.
func TestPlansStateCheckMigrationRefusesGarbageRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "garbage.db")
	createLegacyPlanDB(t, dbPath)

	probeDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := probeDB.Exec(`INSERT INTO plans (id, title, file_path, state, created_at, updated_at)
		VALUES ('bad-state-row', 'b', '/tmp/b.md', 'wat_is_this', ?, ?)`, now, now); err != nil {
		probeDB.Close()
		t.Fatalf("insert bad row: %v", err)
	}
	probeDB.Close()

	if _, err := NewSQLiteStore(dbPath, slog.Default()); err == nil {
		t.Fatal("expected migration error for out-of-enum plan row, got none")
	} else if !strings.Contains(err.Error(), "wat_is_this") {
		t.Errorf("migration error %q does not name the offending value", err)
	}

	// Data untouched: the bad row is still on disk (migration rolled back).
	recoverDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer recoverDB.Close()
	var state string
	if err := recoverDB.QueryRow(`SELECT state FROM plans WHERE id = 'bad-state-row'`).Scan(&state); err != nil {
		t.Fatalf("bad row was deleted or rewritten: %v", err)
	}
	if state != "wat_is_this" {
		t.Errorf("bad row state = %q, want original 'wat_is_this'", state)
	}
}
