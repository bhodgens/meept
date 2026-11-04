//go:build e2e

// Doctor origin-backfill e2e gate (branch 02, plans/skills-refresh-origin
// master.md contract C4):
//
//	skills-doctor-01  doctor origin-backfill report-only - TestDoctorOriginBackfillReportOnly
//
// RED by design until branch 02's 03-doctor-report lands the
// origin-backfill doctorCheck in cmd/meept/doctor.go: the check name is
// absent from today's output, so the line assertion fails. The no-mutation
// leg holds for any doctor run and pins the report-only contract once the
// check exists.

package skillsrefreshorigin

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// doctorDBContentHash hashes the LOGICAL row content of the fact store
// (every column of every memory_facts row, ordered by id) for the
// no-mutation assert. A report-only doctor run must leave the logical data
// unchanged.
//
// This keys off logical content, not raw file bytes. The store is WAL-mode;
// after Daemon.Stop the -wal/-shm sidecars are still hot, and the first
// read-only open below can run WAL recovery + rebuild the -shm index and
// checkpoint the -wal - rewriting those bytes with ZERO rows changed. A
// raw-byte hash false-positives "mutated" on that nondeterministic window.
// A genuine report-only violation (doctor writing a backfill) WOULD change
// the logical rows, so this catches the real defect and nothing else.
func doctorDBContentHash(t *testing.T, dbPath string) string {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		t.Fatalf("open fact db %s: %v", dbPath, err)
	}
	defer db.Close() //nolint:errcheck // read-only close, best effort
	db.SetMaxOpenConns(1)

	rows, err := db.Query(`SELECT id, owner_id, kind, key, value, origin
		FROM memory_facts ORDER BY id`)
	if err != nil {
		t.Fatalf("read fact rows %s: %v", dbPath, err)
	}
	defer rows.Close() //nolint:errcheck

	h := sha256.New()
	for rows.Next() {
		var id, owner, kind, key, value, origin sql.NullString
		if err := rows.Scan(&id, &owner, &kind, &key, &value, &origin); err != nil {
			t.Fatalf("scan fact row: %v", err)
		}
		// Deterministic serialization: null -> empty, fields joined by \x00.
		_, _ = h.Write([]byte(fmt.Sprintf("%c%s%c%s%c%s%c%s%c%s%c%s\n",
			'\x00', id.String, '\x00', owner.String, '\x00', kind.String,
			'\x00', key.String, '\x00', value.String, '\x00', origin.String)))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate fact rows: %v", err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// doctorOriginBackfillLine isolates the origin-backfill line from one doctor
// run's output. The CLI renders check lines lowercased (doctor.go
// printDoctorChecks), and the check name is lowercase - keep the compare
// symmetric either way.
func doctorOriginBackfillLine(out string) string {
	lower := strings.ToLower(out)
	for _, line := range strings.Split(lower, "\n") {
		if strings.Contains(line, "origin-backfill") {
			return line
		}
	}
	return ""
}

// skills-doctor-01 (F5): with 'unknown' rows present, `meept doctor`
// reports an origin-backfill line with the counts, exits non-blocking
// (success), and writes nothing - the check is report-only, never an
// auto-backfill.
func TestDoctorOriginBackfillReportOnly(t *testing.T) {
	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			// 'unknown' rows: seed the pre-migration fact store before the
			// daemon boots so the in-place upgrade upgrades a legacy row
			// into exactly the backfill gap the check counts.
			originSeedLegacyFact(t, filepath.Join(st.StateDir, "memory_facts.db"))
			return nil
		}),
	)
	s.RegisterProject(t, "e2e-origin-doctor")
	sessionID := s.CreateSession(t, "origin-doctor", s.ProjectDir)

	// One tool-path turn so the store carries a stamped 'agent' fact next
	// to the seeded legacy 'unknown' row - the doctor's counts span both.
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat"}`)
	s.Fake.SetChatText("noted")
	s.ChatTurn(t, sessionID, "I like window seats", 120*time.Second)

	// Stop the daemon before the mutation check: a live daemon keeps its
	// WAL writer open and would legitimately change the file tree between
	// two hashes. Doctor is a client-side surface - it dials the socket
	// for enrichment, and a stale socket only adds a report-only line
	// (the run never blocks on it).
	s.Daemon.Stop()

	before := doctorDBContentHash(t, filepath.Join(s.StateDir, "memory_facts.db"))

	// Doctor surface: the same CLI invocation pattern the harness uses
	// everywhere (scratch CLI, sandbox MEEPT_HOME, -d state dir). No
	// --fix/--install-missing flags: a bare run is the report-only
	// surface.
	out, stderr := s.RunCLI(t, 30*time.Second, false, "doctor")

	// Non-blocking: RunCLI(ignoreError=false) already failed the test if
	// the CLI exited non-zero - with 'unknown' rows present in the store.

	// The check reports its counts: a gap of >= 1 unknown fact(s) - the
	// legacy fixture row guarantees the count is non-zero.
	line := doctorOriginBackfillLine(out)
	if line == "" {
		t.Fatalf("doctor output has no origin-backfill line (check missing or unlanded):\nstdout:\n%s\nstderr:\n%s\n",
			out, stderr)
	}
	if !strings.Contains(line, "unknown") {
		t.Fatalf("origin-backfill line carries no unknown count:\n%s", line)
	}

	// No write: a report-only doctor must not change the store's logical
	// rows - the fact-set signature is identical after the run.
	after := doctorDBContentHash(t, filepath.Join(s.StateDir, "memory_facts.db"))
	if before != after {
		t.Fatalf("doctor origin-backfill mutated the fact store (report-only violated):\nbefore: %s\nafter:  %s",
			before, after)
	}
}
