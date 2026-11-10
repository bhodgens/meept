package ledger

// 02-daystate/00T §C — due_at test gate (NEW file; ledger_test.go's
// existing suite is untouched per D4).
//
// FROZEN extension contract (master.md):
//
//	ALTER TABLE ledger_state ADD COLUMN due_at TEXT NOT NULL DEFAULT ''
//	CREATE INDEX IF NOT EXISTS idx_ledger_state_due ON ledger_state(due_at) WHERE due_at <> ''
//
//	Go: DraftRecord gains `Due time.Time` (zero = none; only meaningful
//	when SetStatus != ""); envelope gains `"due"` (RFC3339 or ""); state
//	UPSERT writes due_at (overwritten on each state-setting append; ''
//	clears); State gains `Due time.Time` (zero when none); QueryOpen
//	returns Due; Overdue predicate: status IN (open,waiting_on_me,
//	waiting_on_them) AND due_at != '' AND due_at < now (RFC3339
//	lexicographic-safe); VerifyState fold compares due as part of Want.
//
// Clock posture: Open has no public time seam (frozen), and every due
// assertion here compares explicit RFC3339 strings against an explicit
// reference time — no wall-clock dependency except that appended rows
// land ~now.

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite" // driver registration for raw handles (house convention)
)

// discardLogger mirrors ledger_test.go's logger posture without
// redeclaring the symbol (that file is frozen/untouched).
func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// draftWithDue builds a commitment-open append for subject with a due.
func draftWithDue(subject string, due time.Time) DraftRecord {
	return DraftRecord{
		EmployeeID: "emp-due",
		Kind:       KindCommitment,
		Subject:    subject,
		Source:     SourceVoice,
		SetStatus:  StatusOpen,
		Summary:    "carry due " + subject,
		Due:        due,
	}
}

// TestDraftDueZeroWritesNoDue: a SetStatus append with zero Due stores
// due_at = ” (and the envelope's "due" is the empty string), and the
// state read-back has a zero State.Due.
func TestDraftDueZeroWritesNoDue(t *testing.T) {
	ctx := context.Background()
	s, dbPath := newTestStoreAt(t)

	rec, err := s.Append(ctx, DraftRecord{
		EmployeeID: "emp-due",
		Kind:       KindCommitment,
		Subject:    "s-zero",
		Source:     SourceVoice,
		SetStatus:  StatusOpen,
		Summary:    "no due",
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(readRawPayload(t, dbPath, rec.ID), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	due, ok := env["due"]
	if !ok {
		t.Fatalf(`envelope missing frozen "due" key: %v`, env)
	}
	if due != "" {
		t.Errorf(`envelope "due" = %v; want "" for zero DraftRecord.Due`, due)
	}

	st, found, err := s.GetState(ctx, "emp-due", "s-zero")
	if err != nil || !found {
		t.Fatalf("GetState: found=%v err=%v", found, err)
	}
	if !st.Due.IsZero() {
		t.Errorf("State.Due = %v; want zero when no due was set", st.Due)
	}

	open, err := s.QueryOpen(ctx, "emp-due")
	if err != nil {
		t.Fatalf("QueryOpen: %v", err)
	}
	if len(open) != 1 || !open[0].Due.IsZero() {
		t.Errorf("QueryOpen Due = %+v; want one zero-Due row", open)
	}
}

// TestDraftDueSetWritesStateAndEnvelope: SetStatus append with Due —
// due_at lands on the state row (UTC RFC3339), the envelope "due"
// matches, GetState/QueryOpen return State.Due round-tripping the
// appended instant.
func TestDraftDueSetWritesStateAndEnvelope(t *testing.T) {
	ctx := context.Background()
	s, dbPath := newTestStoreAt(t)

	due := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	rec, err := s.Append(ctx, draftWithDue("s-set", due))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(readRawPayload(t, dbPath, rec.ID), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if got, ok := env["due"]; !ok || got != due.Format(time.RFC3339) {
		t.Errorf(`envelope "due" = %v (present=%v); want %q`, got, ok, due.Format(time.RFC3339))
	}

	st, found, err := s.GetState(ctx, "emp-due", "s-set")
	if err != nil || !found {
		t.Fatalf("GetState: found=%v err=%v", found, err)
	}
	if !st.Due.Equal(due) {
		t.Errorf("GetState Due = %v; want %v", st.Due, due)
	}

	open, err := s.QueryOpen(ctx, "emp-due")
	if err != nil {
		t.Fatalf("QueryOpen: %v", err)
	}
	if len(open) != 1 || !open[0].Due.Equal(due) {
		t.Errorf("QueryOpen Due = %+v; want [%s]", open, due)
	}
}

// TestStateSettingAppendClearsDue — frozen overwrite semantics: every
// state-setting append rewrites due_at; a LATER state-setting append
// WITHOUT a due CLEARS it (”), even for a legal open->waiting edge.
func TestStateSettingAppendClearsDue(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	due := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	if _, err := s.Append(ctx, draftWithDue("s-clear", due)); err != nil {
		t.Fatalf("Append due: %v", err)
	}
	st, _, err := s.GetState(ctx, "emp-due", "s-clear")
	if err != nil || st.Due.IsZero() {
		t.Fatalf("precondition Due not set: %+v err=%v", st, err)
	}

	// Legal transition open -> waiting_on_them, no Due on the draft.
	if _, err := s.Append(ctx, DraftRecord{
		EmployeeID: "emp-due",
		Kind:       KindObservation,
		Subject:    "s-clear",
		Source:     SourceDesk,
		SetStatus:  StatusWaitingThem,
		Summary:    "chasing",
	}); err != nil {
		t.Fatalf("Append no-due: %v", err)
	}

	st, _, err = s.GetState(ctx, "emp-due", "s-clear")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if !st.Due.IsZero() {
		t.Errorf("Due = %v after no-due state append; want zero (overwritten per frozen semantics)", st.Due)
	}
	open, err := s.QueryOpen(ctx, "emp-due")
	if err != nil || len(open) != 1 || !open[0].Due.IsZero() {
		t.Errorf("QueryOpen = %+v err=%v; want one zero-Due row", open, err)
	}
}

// TestOverduePredicateOverStateRows pins the frozen predicate purely
// over QueryOpen results: overdue iff status IN (open, waiting_on_me,
// waiting_on_them) AND Due non-zero AND Due < now. A done subject's past
// due must be invisible; a future due is not overdue.
func TestOverduePredicateOverStateRows(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-2 * time.Hour)
	future := now.Add(2 * time.Hour)

	if _, err := s.Append(ctx, draftWithDue("s-past", past)); err != nil {
		t.Fatalf("Append past: %v", err)
	}
	if _, err := s.Append(ctx, draftWithDue("s-future", future)); err != nil {
		t.Fatalf("Append future: %v", err)
	}
	if _, err := s.Append(ctx, draftWithDue("s-done", past)); err != nil {
		t.Fatalf("Append done-past: %v", err)
	}
	if _, err := s.Append(ctx, DraftRecord{
		EmployeeID: "emp-due", Kind: KindResolution, Subject: "s-done",
		Source: SourceDesk, SetStatus: StatusDone, Summary: "closed",
	}); err != nil {
		t.Fatalf("Append done: %v", err)
	}

	open, err := s.QueryOpen(ctx, "emp-due")
	if err != nil {
		t.Fatalf("QueryOpen: %v", err)
	}
	if len(open) != 2 {
		t.Fatalf("QueryOpen len = %d; want 2 (done excluded): %+v", len(open), open)
	}
	overdue := 0
	for _, st := range open {
		if st.Due.IsZero() {
			t.Errorf("subject %s: QueryOpen lost Due", st.Subject)
		}
		if st.Status != StatusOpen || st.Due.IsZero() {
			continue
		}
		if st.Due.Before(now) {
			overdue++
			if st.Subject != "s-past" {
				t.Errorf("overdue row = %s; want s-past", st.Subject)
			}
		}
	}
	if overdue != 1 {
		t.Errorf("overdue count = %d; want 1 (s-past only)", overdue)
	}
}

// TestVerifyStateDiffOnDue / TestRepairStateRestoresDue: the fold
// compares due as part of Want (same envelope source). Corrupting
// ledger_state.due_at directly is divergence VerifyState must flag
// (Have.Due vs Want.Due) and RepairState must fix from the envelope.
func TestVerifyStateDiffOnDue(t *testing.T) {
	ctx := context.Background()
	s, dbPath := newTestStoreAt(t)

	due := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Second)
	if _, err := s.Append(ctx, draftWithDue("s-verify", due)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	base, err := s.VerifyState(ctx)
	if err != nil || base.Divergent != 0 {
		t.Fatalf("VerifyState baseline divergent=%d err=%v", base.Divergent, err)
	}

	// Corrupt ONLY due_at: fold Want keeps the envelope due.
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec(
		`UPDATE ledger_state SET due_at = '' WHERE employee_id = ? AND subject = ?`,
		"emp-due", "s-verify"); err != nil {
		raw.Close()
		t.Fatalf("corrupt due_at: %v", err)
	}
	raw.Close()

	report, err := s.VerifyState(ctx)
	if err != nil {
		t.Fatalf("VerifyState: %v", err)
	}
	if report.Divergent != 1 || len(report.Diffs) != 1 {
		t.Fatalf("VerifyState after due corruption: %+v; want exactly 1 diff", report)
	}
	d := report.Diffs[0]
	if d.Subject != "s-verify" {
		t.Fatalf("diff subject = %q; want s-verify", d.Subject)
	}
	if !d.Have.Due.IsZero() {
		t.Errorf("Have.Due = %v; want zero (corrupted row)", d.Have.Due)
	}
	if !d.Want.Due.Equal(due) {
		t.Errorf("Want.Due = %v; want %v — VerifyState fold must compare due", d.Want.Due, due)
	}

	n, err := s.RepairState(ctx)
	if err != nil || n != 1 {
		t.Fatalf("RepairState = (%d, %v); want (1, nil)", n, err)
	}
	again, err := s.VerifyState(ctx)
	if err != nil || again.Divergent != 0 {
		t.Fatalf("VerifyState post-repair divergent=%d err=%v; want 0", again.Divergent, err)
	}
	st, _, err := s.GetState(ctx, "emp-due", "s-verify")
	if err != nil || !st.Due.Equal(due) {
		t.Errorf("repaired Due = %v err=%v; want %v", st.Due, err, due)
	}
}

// legacyLedgerSchema is the PRE-extension ledger schema — the frozen
// ledgerSchema constant minus the due_at column and its index. SQLite
// cannot DROP COLUMN portably, so a "old database" is built from this
// schema text, then reopened through ledger.Open, which must run the
// guarded ALTER (duplicate-column tolerated on the next reopen).
const legacyLedgerSchema = `
CREATE TABLE IF NOT EXISTS ledger (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    employee_id TEXT NOT NULL,
    at          TEXT NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('committal','action','observation','commitment','resolution')),
    subject     TEXT NOT NULL DEFAULT '',
    payload     BLOB,
    source      TEXT NOT NULL CHECK (source IN ('voice','desk','tool','cli','task')),
    source_ref  TEXT NOT NULL DEFAULT '',
    resolves    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_ledger_emp_subject ON ledger(employee_id, subject, id);
CREATE INDEX IF NOT EXISTS idx_ledger_resolves    ON ledger(resolves) WHERE resolves <> 0;

CREATE TABLE IF NOT EXISTS ledger_state (
    employee_id TEXT NOT NULL,
    subject     TEXT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('open','done','cancelled','waiting_on_me','waiting_on_them')),
    summary     TEXT NOT NULL DEFAULT '',
    updated_at  TEXT NOT NULL,
    as_of       INTEGER NOT NULL,
    PRIMARY KEY (employee_id, subject)
);

CREATE VIRTUAL TABLE IF NOT EXISTS ledger_fts USING fts5(
    subject, payload_snippet, content=''
);
`

func TestOpenMigratesLegacyDBWithGuardedAlterDue(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ledger.db")

	// Build a legacy db (no due_at column) + one pre-existing state row.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec(legacyLedgerSchema); err != nil {
		raw.Close()
		t.Fatalf("legacy migrate: %v", err)
	}
	if _, err := raw.Exec(
		`INSERT INTO ledger_state (employee_id, subject, status, summary, updated_at, as_of)
         VALUES ('emp-legacy','s-old','open','pre-migration','2026-11-09T10:00:00Z',1)`); err != nil {
		raw.Close()
		t.Fatalf("seed legacy row: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	// Open must add due_at + the partial index without touching rows.
	s, err := Open(path, discardLogger())
	if err != nil {
		t.Fatalf("Open on legacy db: %v", err)
	}
	// Reopen idempotency: the guarded ALTER must tolerate the
	// duplicate-column steady state on an already-migrated db.
	if err := s.Close(); err != nil {
		t.Fatalf("Close after first Open: %v", err)
	}
	s, err = Open(path, discardLogger())
	if err != nil {
		t.Fatalf("re-Open on migrated db: %v", err)
	}
	defer s.Close()

	var cols int
	probe, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("probe open: %v", err)
	}
	defer probe.Close()
	if err := probe.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('ledger_state') WHERE name = 'due_at'`).Scan(&cols); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if cols != 1 {
		t.Fatalf("due_at column count = %d; want 1 after guarded ALTER", cols)
	}
	var idx int
	if err := probe.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_ledger_state_due'`).Scan(&idx); err != nil {
		t.Fatalf("index probe: %v", err)
	}
	if idx != 1 {
		t.Errorf("idx_ledger_state_due present = %d; want 1", idx)
	}

	// Pre-existing legacy row survived the ALTER with an empty due.
	st, found, err := s.GetState(ctx, "emp-legacy", "s-old")
	if err != nil || !found {
		t.Fatalf("legacy row lost: found=%v err=%v", found, err)
	}
	if !st.Due.IsZero() {
		t.Errorf("legacy row Due = %v; want zero (DEFAULT '')", st.Due)
	}

	// And the migrated db is fully due-capable.
	due := time.Now().UTC().Add(6 * time.Hour).Truncate(time.Second)
	if _, err := s.Append(ctx, DraftRecord{
		EmployeeID: "emp-legacy", Kind: KindCommitment, Subject: "s-new",
		Source: SourceVoice, SetStatus: StatusOpen, Summary: "new era", Due: due,
	}); err != nil {
		t.Fatalf("Append with Due on migrated db: %v", err)
	}
	open, err := s.QueryOpen(ctx, "emp-legacy")
	if err != nil || len(open) != 2 {
		t.Fatalf("QueryOpen = %+v err=%v; want 2 rows", open, err)
	}
	var gotDue bool
	for _, row := range open {
		if row.Subject == "s-new" && row.Due.Equal(due) {
			gotDue = true
		}
	}
	if !gotDue {
		t.Errorf("migrated db QueryOpen lost Due: %+v", open)
	}
}
