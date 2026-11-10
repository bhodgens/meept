package queue

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite" //nolint:revive // blank import for side effects
)

// legacyJobsDDL is the pre-F7 jobs table: NO CHECK on state, and deliberately
// missing the columns later added by the legacy ALTER migrations
// (interactive, claim_token, next_retry_at) — exactly what an on-disk database
// from an older binary looks like. The rebuild must carry those forward via
// the ALTERs plus PRAGMA-captured column list.
const legacyJobsDDL = `
CREATE TABLE jobs (
	id            TEXT PRIMARY KEY,
	task_id       TEXT,
	agent_id      TEXT,
	type          TEXT NOT NULL,
	priority      INTEGER DEFAULT 2,
	state         TEXT DEFAULT 'pending',
	payload       TEXT NOT NULL,
	required_caps TEXT DEFAULT '[]',
	max_retries   INTEGER DEFAULT 3,
	retry_count   INTEGER DEFAULT 0,
	claimed_by    TEXT,
	result        TEXT,
	error         TEXT,
	created_at    TEXT NOT NULL,
	updated_at    TEXT NOT NULL,
	due_at        TEXT
);
CREATE INDEX idx_jobs_state_priority ON jobs(state, priority DESC, created_at);
CREATE INDEX idx_jobs_task_id ON jobs(task_id);
CREATE INDEX idx_jobs_claimed_by ON jobs(claimed_by);
CREATE INDEX idx_jobs_agent_id ON jobs(agent_id);
`

// createLegacyQueueDB writes a pre-CHECK database file with two rows, one of
// which carries extra data that must survive the rebuild byte-for-byte.
func createLegacyQueueDB(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(legacyJobsDDL); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO jobs (id, type, priority, state, payload, required_caps, max_retries, retry_count, created_at, updated_at, result)
		VALUES ('legacy-1', 'one_off', 2, 'completed', '{"prompt":"old"}', '[]', 3, 1, ?, ?, 'the-result')`, now, now); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	// NULL state: CHECK passes NULL (SQL three-valued logic). Insert it
	// explicitly — omitting the column would silently take the DDL DEFAULT
	// 'pending'. The rebuild must preserve the NULL.
	if _, err := db.Exec(`INSERT INTO jobs (id, type, priority, state, payload, required_caps, max_retries, retry_count, created_at, updated_at)
		VALUES ('legacy-2', 'one_off', 2, NULL, '{"prompt":"nullstate"}', '[]', 3, 0, ?, ?)`, now, now); err != nil {
		t.Fatalf("insert null-state legacy row: %v", err)
	}
}

func jobsIndexNames(t *testing.T, store *Store) map[string]bool {
	t.Helper()
	rows, err := store.db.Query(`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='jobs' AND name NOT LIKE 'sqlite_%'`)
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

func jobsTableHasCheck(t *testing.T, store *Store) bool {
	t.Helper()
	var tableSQL string
	if err := store.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='jobs'`).Scan(&tableSQL); err != nil {
		t.Fatalf("read jobs sql: %v", err)
	}
	return strings.Contains(strings.ToUpper(tableSQL), "CHECK")
}

// TestJobsStateCheckMigratedDB pins the F7 migration path: a legacy database
// without the CHECK opens, gains the CHECK, keeps every index, and leaves the
// existing rows (including a NULL-state row) untouched.
func TestJobsStateCheckMigratedDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	createLegacyQueueDB(t, dbPath)

	// Baseline: pre-CHECK fixture must have no CHECK and exactly the four
	// legacy indexes (guards the fixture itself).
	probeDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("probe open: %v", err)
	}
	var probeSQL string
	if err := probeDB.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='jobs'`).Scan(&probeSQL); err != nil {
		probeDB.Close()
		t.Fatalf("probe read: %v", err)
	}
	if strings.Contains(strings.ToUpper(probeSQL), "CHECK") {
		probeDB.Close()
		t.Fatal("fixture already has CHECK — fixture is invalid")
	}
	probeDB.Close()

	store := newTestStore(t, dbPath)

	// 1. CHECK now present in sqlite_master.
	if !jobsTableHasCheck(t, store) {
		t.Fatal("jobs table still lacks CHECK after migration")
	}

	// 2. Garbage INSERT now refused on the migrated DB.
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = store.db.Exec(`INSERT INTO jobs (id, type, priority, state, payload, required_caps, max_retries, retry_count, created_at, updated_at)
		VALUES ('garbage-migrated', 'one_off', 2, 'garbage_bogus', '{}', '[]', 3, 0, ?, ?)`, now, now)
	if err == nil {
		t.Fatal("expected constraint error on migrated DB, got none")
	}

	// 3. Existing rows survived, column values intact.
	var result string
	if err := store.db.QueryRow(`SELECT result FROM jobs WHERE id = 'legacy-1'`).Scan(&result); err != nil {
		t.Fatalf("legacy row lost: %v", err)
	}
	if result != "the-result" {
		t.Errorf("legacy row result = %q, want %q", result, "the-result")
	}
	var state sql.NullString
	if err := store.db.QueryRow(`SELECT state FROM jobs WHERE id = 'legacy-2'`).Scan(&state); err != nil {
		t.Fatalf("null-state row lost: %v", err)
	}
	if state.Valid {
		t.Errorf("null-state row became %q; want NULL preserved", state.String)
	}
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("row count after migration = %d, want 2", count)
	}

	// 4. Index parity: every legacy index survived the rebuild, and the
	// post-migration idx_jobs_claim landed too.
	names := jobsIndexNames(t, store)
	for _, want := range []string{"idx_jobs_state_priority", "idx_jobs_task_id", "idx_jobs_claimed_by", "idx_jobs_agent_id", "idx_jobs_claim"} {
		if !names[want] {
			t.Errorf("index %s missing after rebuild (have %v)", want, names)
		}
	}

	// 5. The legacy ALTER-carried columns exist on the rebuilt table.
	for _, col := range []string{"interactive", "claim_token", "next_retry_at"} {
		ok, err := store.columnExists("jobs", col)
		if err != nil {
			t.Fatalf("columnExists(%s): %v", col, err)
		}
		if !ok {
			t.Errorf("rebuilt jobs table lost column %s", col)
		}
	}

	// 6. Idempotence: reopening the migrated DB is a silent no-op.
	store.Close()
	store2 := newTestStore(t, dbPath)
	if !jobsTableHasCheck(t, store2) {
		t.Fatal("CHECK vanished on reopen")
	}
	if got := jobsIndexNames(t, store2); len(got) != len(names) {
		t.Errorf("index count drift on reopen: %v vs %v", got, names)
	}
}

// TestJobsStateCheckMigrationRefusesGarbageRows proves the no-data-loss rule:
// a legacy DB containing a row with a state outside the enum FAILS the
// migration outright (NewStore returns an error) and the offending row is
// still there — repaired or not, the migration never deletes or rewrites it.
func TestJobsStateCheckMigrationRefusesGarbageRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "garbage.db")
	createLegacyQueueDB(t, dbPath)

	probeDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := probeDB.Exec(`INSERT INTO jobs (id, type, priority, state, payload, required_caps, max_retries, retry_count, created_at, updated_at)
		VALUES ('bad-state-row', 'one_off', 2, 'wat_is_this', '{}', '[]', 3, 0, ?, ?)`, now, now); err != nil {
		probeDB.Close()
		t.Fatalf("insert bad row: %v", err)
	}
	probeDB.Close()

	if _, err := NewStore(dbPath, nil); err == nil {
		t.Fatal("expected migration error for out-of-enum row, got none")
	} else if !strings.Contains(err.Error(), "wat_is_this") {
		t.Errorf("migration error %q does not name the offending value", err)
	}

	// Data untouched: the bad row is still on disk (store never opened,
	// migration rolled back).
	recoverDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer recoverDB.Close()
	var state string
	if err := recoverDB.QueryRow(`SELECT state FROM jobs WHERE id = 'bad-state-row'`).Scan(&state); err != nil {
		t.Fatalf("bad row was deleted or rewritten: %v", err)
	}
	if state != "wat_is_this" {
		t.Errorf("bad row state = %q, want original 'wat_is_this'", state)
	}
}

// TestJobsStateCheckFreshSchemaMatchesMigration pins that a fresh DB and a
// migrated DB end up with the same index set (minus nothing), so the rebuild
// cannot silently drop an index that baseSchema owns.
func TestJobsStateCheckFreshSchemaMatchesMigration(t *testing.T) {
	fresh := newTestStore(t, "")
	legacyPath := filepath.Join(t.TempDir(), "parity.db")
	createLegacyQueueDB(t, legacyPath)
	migrated := newTestStore(t, legacyPath)

	freshNames := jobsIndexNames(t, fresh)
	migNames := jobsIndexNames(t, migrated)
	for n := range freshNames {
		// fresh DBs carry idx_jobs_state_priority etc.; the legacy fixture
		// predates idx_jobs_claim (created post-migration) — compare the
		// baseSchema-owned set both ways.
		if !migNames[n] {
			t.Errorf("migrated DB missing index %s present on fresh DB", n)
		}
	}
	for n := range migNames {
		if !freshNames[n] {
			t.Errorf("fresh DB missing index %s present on migrated DB", n)
		}
	}
}
