package migrate

import (
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/test.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustRegistry(t *testing.T, identity string, steps []Migration) *Registry {
	t.Helper()
	r, err := NewRegistry(identity, steps)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r
}

func TestNewRegistryRejectsGapsAndDuplicates(t *testing.T) {
	cases := []struct {
		name  string
		steps []Migration
	}{
		{"gap", []Migration{{Version: 1, Name: "a", SQL: "CREATE TABLE a (x)"}, {Version: 3, Name: "c", SQL: "CREATE TABLE c (x)"}}},
		{"duplicate", []Migration{{Version: 1, Name: "a", SQL: "CREATE TABLE a (x)"}, {Version: 1, Name: "a2", SQL: "CREATE TABLE a2 (x)"}}},
		{"not starting at one", []Migration{{Version: 2, Name: "b", SQL: "CREATE TABLE b (x)"}}},
		{"empty sql", []Migration{{Version: 1, Name: "a", SQL: "   "}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewRegistry("test", tc.steps); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
	if _, err := NewRegistry("test", nil); err == nil {
		t.Fatal("empty registry must be rejected")
	}
}

func TestApplyFreshRunsAllSteps(t *testing.T) {
	r := mustRegistry(t, "test", []Migration{
		{Version: 1, Name: "base", SQL: "CREATE TABLE items (id TEXT PRIMARY KEY, a TEXT)"},
		{Version: 2, Name: "add b", SQL: "ALTER TABLE items ADD COLUMN b INTEGER DEFAULT 0"},
		{Version: 3, Name: "add c", SQL: "ALTER TABLE items ADD COLUMN c TEXT"},
	})
	db := openTestDB(t)
	if err := r.Apply(db, nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, err := Applied(db)
	if err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Fatalf("user_version = %d, want 3", got)
	}
	// Column exists and default applied.
	if _, err := db.Exec(`INSERT INTO items (id, a) VALUES ('x', 'v')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var b int
	var c sql.NullString
	if err := db.QueryRow(`SELECT b, c FROM items WHERE id = 'x'`).Scan(&b, &c); err != nil {
		t.Fatalf("select: %v", err)
	}
	if b != 0 || c.Valid {
		t.Fatalf("defaults not applied: b=%d c=%+v", b, c)
	}
}

func TestApplyAlreadyCurrentIsNoop(t *testing.T) {
	r := mustRegistry(t, "test", []Migration{
		{Version: 1, Name: "base", SQL: "CREATE TABLE items (id TEXT PRIMARY KEY)"},
	})
	db := openTestDB(t)
	if err := r.Apply(db, nil); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO items (id) VALUES ('x')`); err != nil {
		t.Fatal(err)
	}
	// Re-opening the store re-applies the same registry.
	if err := r.Apply(db, nil); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if got, _ := Applied(db); got != 1 {
		t.Fatalf("user_version = %d, want 1", got)
	}
	// No duplicate data from a re-run.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("row count = %d, want 1 (re-apply must not re-run)", n)
	}
}

func TestApplyResumesFromPartialVersion(t *testing.T) {
	r := mustRegistry(t, "test", []Migration{
		{Version: 1, Name: "base", SQL: "CREATE TABLE items (id TEXT PRIMARY KEY)"},
		{Version: 2, Name: "add a", SQL: "ALTER TABLE items ADD COLUMN a TEXT"},
	})
	db := openTestDB(t)
	// Simulate an older binary that stopped at v1.
	r1 := mustRegistry(t, "test", []Migration{
		{Version: 1, Name: "base", SQL: "CREATE TABLE items (id TEXT PRIMARY KEY)"},
	})
	if err := r1.Apply(db, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO items (id) VALUES ('old')`); err != nil {
		t.Fatal(err)
	}
	// Newer binary applies only the pending step; old row must survive.
	if err := r.Apply(db, nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, _ := Applied(db); got != 2 {
		t.Fatalf("user_version = %d, want 2", got)
	}
	var a sql.NullString
	if err := db.QueryRow(`SELECT a FROM items WHERE id = 'old'`).Scan(&a); err != nil {
		t.Fatalf("legacy row unreadable after migration: %v", err)
	}
	if a.Valid {
		t.Fatalf("legacy row mutated: a=%q", a.String)
	}
}

func TestApplyDownLevelBinaryFailsLoud(t *testing.T) {
	steps := []Migration{
		{Version: 1, Name: "base", SQL: "CREATE TABLE items (id TEXT PRIMARY KEY)"},
		{Version: 2, Name: "add a", SQL: "ALTER TABLE items ADD COLUMN a TEXT"},
	}
	db := openTestDB(t)
	upToDate := mustRegistry(t, "test", steps)
	if err := upToDate.Apply(db, nil); err != nil {
		t.Fatal(err)
	}
	// A binary that only knows v1 opens the v2 database: hard error.
	old := mustRegistry(t, "test", steps[:1])
	err := old.Apply(db, nil)
	if err == nil {
		t.Fatal("down-level Apply must fail, got nil")
	}
	if !errors.Is(err, ErrDownLevel) {
		t.Fatalf("error %v does not wrap ErrDownLevel", err)
	}
	// The database must be untouched by the failed attempt.
	if got, _ := Applied(db); got != 2 {
		t.Fatalf("failed down-level Apply mutated version: %d", got)
	}
}

func TestFailedStepRollsBackTransaction(t *testing.T) {
	// Step 2 fails (references a nonexistent table). The database must
	// stay at version 1 with no partial DDL from step 2.
	r := mustRegistry(t, "test", []Migration{
		{Version: 1, Name: "base", SQL: "CREATE TABLE items (id TEXT PRIMARY KEY)"},
		{Version: 2, Name: "broken", SQL: "ALTER TABLE nonexistent_table ADD COLUMN x TEXT"},
	})
	db := openTestDB(t)
	if err := r.Apply(db, nil); err == nil {
		t.Fatal("expected step 2 to fail")
	}
	if got, _ := Applied(db); got != 1 {
		t.Fatalf("user_version = %d after failed step, want 1 (transactional rollback)", got)
	}
	// Version-1 schema still usable.
	if _, err := db.Exec(`INSERT INTO items (id) VALUES ('x')`); err != nil {
		t.Fatalf("v1 schema unusable after failed step: %v", err)
	}
}

func TestFailedStepDoesNotBlockLaterStepsAfterFix(t *testing.T) {
	// The migration-seen-red guard: with a broken step the version check
	// fails; after fixing the step SQL, Apply succeeds and reaches target.
	r := mustRegistry(t, "test", []Migration{
		{Version: 1, Name: "base", SQL: "CREATE TABLE items (id TEXT PRIMARY KEY)"},
		{Version: 2, Name: "add a", SQL: "ALTER TABLE items ADD COLUMN a TEXT"},
		{Version: 3, Name: "add b", SQL: "ALTER TABLE items ADD COLUMN b TEXT"},
	})
	db := openTestDB(t)

	broken := Registry{identity: "test", steps: []Migration{
		r.steps[0],
		r.steps[1],
		{Version: 3, Name: "add b", SQL: "ALTER TABLE items ADD COLUMN a TEXT"}, // duplicate column
	}}
	if err := broken.Apply(db, nil); err == nil {
		t.Fatal("broken step must fail")
	}
	if got, _ := Applied(db); got != 2 {
		t.Fatalf("version after failed step = %d, want 2", got)
	}
	// Fix the step; the same registry now completes.
	if err := r.Apply(db, nil); err != nil {
		t.Fatalf("Apply after fix: %v", err)
	}
	if got, _ := Applied(db); got != 3 {
		t.Fatalf("version after fix = %d, want 3", got)
	}
}

func TestMultiStatementStep(t *testing.T) {
	r := mustRegistry(t, "test", []Migration{
		{Version: 1, Name: "base", SQL: "CREATE TABLE items (id TEXT PRIMARY KEY, v INTEGER DEFAULT 0);\nCREATE INDEX idx_items_v ON items(v);"},
	})
	db := openTestDB(t)
	if err := r.Apply(db, nil); err != nil {
		t.Fatalf("multi-statement step: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO items (id) VALUES ('x')`); err != nil {
		t.Fatal(err)
	}
}

func TestGuardSkipRecordsVersionWithoutDDL(t *testing.T) {
	r := mustRegistry(t, "test", []Migration{
		{
			Version: 1,
			Name:    "base",
			SQL:     "CREATE TABLE items (id TEXT PRIMARY KEY)",
			Guard: func(db *sql.DB) (bool, error) {
				exists, err := tableExists(db, "items")
				return !exists, err
			},
		},
		{
			Version: 2,
			Name:    "add a",
			SQL:     "ALTER TABLE items ADD COLUMN a TEXT",
			Guard: func(db *sql.DB) (bool, error) {
				// Step needed when the column is still missing.
				exists, err := ColumnExists(db, "items", "a")
				return !exists, err
			},
		},
	})
	db := openTestDB(t)
	// Simulate a pre-registry database: schema applied by the old
	// hand-rolled idempotent DDL, user_version still 0.
	if _, err := db.Exec("CREATE TABLE items (id TEXT PRIMARY KEY, a TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := r.Apply(db, nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, _ := Applied(db); got != 2 {
		t.Fatalf("user_version = %d, want 2 (guard skip must still record)", got)
	}
	// The guarded ALTER must NOT have run (it would have failed on the
	// duplicate column anyway, but assert no error was swallowed by
	// checking the column set is exactly the pre-registry one).
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('items') WHERE name = 'a'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("column a count = %d, want 1", n)
	}
}

func TestGuardTrueRunsDDL(t *testing.T) {
	r := mustRegistry(t, "test", []Migration{
		{Version: 1, Name: "base", SQL: "CREATE TABLE items (id TEXT PRIMARY KEY)"},
		{
			Version: 2,
			Name:    "add a",
			SQL:     "ALTER TABLE items ADD COLUMN a TEXT",
			Guard: func(db *sql.DB) (bool, error) {
				// Step needed when the column is still missing.
				exists, err := ColumnExists(db, "items", "a")
				return !exists, err
			},
		},
	})
	db := openTestDB(t)
	if err := r.Apply(db, nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, _ := Applied(db); got != 2 {
		t.Fatalf("user_version = %d, want 2", got)
	}
	if _, err := db.Exec(`INSERT INTO items (id, a) VALUES ('x', 'v')`); err != nil {
		t.Fatalf("column a missing after guarded apply: %v", err)
	}
}

func TestGuardErrorFailsApply(t *testing.T) {
	sentinel := errors.New("guard exploded")
	r := mustRegistry(t, "test", []Migration{
		{Version: 1, Name: "base", SQL: "CREATE TABLE items (id TEXT PRIMARY KEY)"},
		{
			Version: 2,
			Name:    "add a",
			SQL:     "ALTER TABLE items ADD COLUMN a TEXT",
			Guard: func(db *sql.DB) (bool, error) {
				return false, sentinel
			},
		},
	})
	db := openTestDB(t)
	err := r.Apply(db, nil)
	if err == nil {
		t.Fatal("guard error must fail Apply")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("error %v does not wrap guard error", err)
	}
	if got, _ := Applied(db); got != 1 {
		t.Fatalf("version = %d after guard failure, want 1", got)
	}
}

func tableExists(db *sql.DB, table string) (bool, error) {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}
