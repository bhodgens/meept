package ledger

// 01-store/00T — TEST GATE for internal/ledger. Tests ONLY; the
// implementation leaves (01-schema, 02-append-transitions,
// 03-query-verify-tail) must turn this suite GREEN. Every reference here
// uses the frozen API sketch verbatim (master.md / 00T Interface
// Contracts). §-row coverage:
//
//   §1 Open lifecycle      — TestOpenFreshPathMigrates, TestOpenTwiceSamePath,
//                            TestCloseReopenPreservesRows
//   §2 Append validation   — TestAppendAcceptsEveryKind, TestAppendAcceptsEverySource,
//                            TestAppendRejectsInvalidKind, TestAppendRejectsInvalidSource
//   §3 Transitions matrix  — TestTransitionMatrix (incl. reopen + new-subject)
//   §4 Resolves            — TestResolvesOwnRow, TestResolvesUnknownID, TestResolvesForeignEmployee
//   §5 Envelope/state      — TestSetStatusWritesEnvelopeAndState, TestSetStatusNullDataEnvelope,
//                            TestLogOnlyAppendLeavesStateUntouched
//   §6 Queries             — TestQueryLogFilters, TestQueryLogLimits, TestQueryOpenExcludesClosed,
//                            TestGetStateOkAndMissing
//   §7 Verify/Repair       — TestVerifyStateClean, TestVerifyStateDetectsCorruption,
//                            TestRepairStateFixesDivergence
//   §8 Tail                — TestTailCatchUp, TestTailLiveAppend, TestTailOrderNoGaps,
//                            TestTailCtxCancelCloses
//
// House posture (internal/voice/callstore_test.go, internal/effects/*_test.go):
// stdlib testing only — no testify anywhere in store tests.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite" // driver registration for raw handles (house convention)
)

// newTestStore opens a fresh Store under t.TempDir() with a discarding
// logger and registers cleanup. Frozen Open has no time seam (00T task
// 10): tests use real times and assert orderings, never timestamp
// equality.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, _ := newTestStoreAt(t)
	return s
}

// newTestStoreAt is newTestStore plus the db path, for tests that must
// read the physical rows with a raw handle (§5, §7).
func newTestStoreAt(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ledger.db")
	s, err := Open(path, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("ledger.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

// readRawPayload reads the stored ledger.payload for a row id with a raw
// sqlite handle — ground truth for §5 envelope assertions (the returned
// Record's payload shape is not frozen; the ROW is).
func readRawPayload(t *testing.T, dbPath string, id int64) []byte {
	t.Helper()
	db := openDB(t, dbPath)
	var payload []byte
	err := db.QueryRowContext(context.Background(), `SELECT payload FROM ledger WHERE id = ?`, id).Scan(&payload)
	if err != nil {
		t.Fatalf("raw payload read id=%d: %v", id, err)
	}
	return payload
}

func openDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("sql.Open raw handle: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// mustAppend fails the test on Append error and returns the stored record.
func mustAppend(t *testing.T, s *Store, d DraftRecord) *Record {
	t.Helper()
	rec, err := s.Append(context.Background(), d)
	if err != nil {
		t.Fatalf("Append(%s/%s): %v", d.Kind, d.Subject, err)
	}
	return rec
}

// =========================================================================
// §1 Open lifecycle
// =========================================================================

func TestOpenFreshPathMigrates(t *testing.T) {
	// Fresh path must succeed (migration creates the tables) and an
	// immediate Append/Query roundtrip must work.
	s := newTestStore(t)
	rec := mustAppend(t, s, DraftRecord{
		EmployeeID: "emp-a",
		Kind:       KindObservation,
		Subject:    "acme-roof",
		Payload:    []byte(`{"note":"called"}`),
		Source:     SourceVoice,
		SourceRef:  "cc-1",
	})
	if rec.ID <= 0 {
		t.Fatalf("append on fresh store: ID = %d, want > 0", rec.ID)
	}
	rows, err := s.QueryLog(context.Background(), LogQuery{EmployeeID: "emp-a"})
	if err != nil {
		t.Fatalf("QueryLog: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != rec.ID {
		t.Fatalf("QueryLog after fresh migrate: %d rows, want the single appended record", len(rows))
	}
}

func TestOpenTwiceSamePath(t *testing.T) {
	// Open is idempotent: two Stores on one path must both open; the
	// migration is CREATE IF NOT EXISTS, not a re-apply failure.
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.db")
	s1, err := Open(path, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open #1: %v", err)
	}
	defer s1.Close()
	s2, err := Open(path, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open #2 (same path): %v", err)
	}
	defer s2.Close()

	mustAppend(t, s1, DraftRecord{EmployeeID: "emp-a", Kind: KindAction, Subject: "s", Source: SourceCLI})
	// Second handle sees the first handle's write (same physical store).
	rows, err := s2.QueryLog(context.Background(), LogQuery{EmployeeID: "emp-a"})
	if err != nil {
		t.Fatalf("QueryLog via second handle: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("second handle sees %d rows, want 1", len(rows))
	}
}

func TestCloseReopenPreservesRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.db")

	s1, err := Open(path, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	rec := mustAppend(t, s1, DraftRecord{
		EmployeeID: "emp-a", Kind: KindCommitment, Subject: "roof-quote",
		Payload: []byte(`{"due":"friday"}`), Source: SourceVoice, SourceRef: "cc-9",
	})
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := Open(path, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	defer s2.Close()

	rows, err := s2.QueryLog(context.Background(), LogQuery{EmployeeID: "emp-a"})
	if err != nil {
		t.Fatalf("QueryLog after reopen: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows after reopen = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.ID != rec.ID || got.Kind != KindCommitment || got.Subject != "roof-quote" ||
		got.Source != SourceVoice || got.SourceRef != "cc-9" ||
		string(got.Payload) != `{"due":"friday"}` {
		t.Fatalf("persisted record drifted: %+v", got)
	}
	if got.EmployeeID != "emp-a" {
		t.Fatalf("persisted EmployeeID = %q, want emp-a", got.EmployeeID)
	}
}

// =========================================================================
// §2 Append validation (kinds / sources)
// =========================================================================

func TestAppendAcceptsEveryKind(t *testing.T) {
	kinds := []Kind{KindCommittal, KindAction, KindObservation, KindCommitment, KindResolution}
	s := newTestStore(t)
	for i, k := range kinds {
		rec := mustAppend(t, s, DraftRecord{
			EmployeeID: "emp-kind", Kind: k, Subject: fmt.Sprintf("s-%d", i),
			Source: SourceTask,
		})
		if rec.Kind != k {
			t.Fatalf("round-tripped Kind = %q, want %q", rec.Kind, k)
		}
	}
	rows, err := s.QueryLog(context.Background(), LogQuery{EmployeeID: "emp-kind"})
	if err != nil {
		t.Fatalf("QueryLog: %v", err)
	}
	if len(rows) != len(kinds) {
		t.Fatalf("stored %d rows, want %d", len(rows), len(kinds))
	}
}

func TestAppendAcceptsEverySource(t *testing.T) {
	sources := []Source{SourceVoice, SourceDesk, SourceTool, SourceCLI, SourceTask}
	s := newTestStore(t)
	for i, src := range sources {
		rec := mustAppend(t, s, DraftRecord{
			EmployeeID: "emp-src", Kind: KindObservation, Subject: fmt.Sprintf("s-%d", i),
			Source: src,
		})
		if rec.Source != src {
			t.Fatalf("round-tripped Source = %q, want %q", rec.Source, src)
		}
	}
}

func TestAppendRejectsInvalidKind(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Append(context.Background(), DraftRecord{
		EmployeeID: "emp-a", Kind: Kind("nonsense"), Subject: "x", Source: SourceCLI,
	})
	if err == nil {
		t.Fatal("Append with invalid kind: want rejection, got nil error")
	}
	if !strings.HasPrefix(err.Error(), "ledger:") {
		t.Fatalf("rejection error %q, want 'ledger:'-prefixed", err)
	}
	rows, qerr := s.QueryLog(context.Background(), LogQuery{EmployeeID: "emp-a"})
	if qerr != nil {
		t.Fatalf("QueryLog: %v", qerr)
	}
	if len(rows) != 0 {
		t.Fatalf("rejected append wrote %d rows, want none", len(rows))
	}
}

func TestAppendRejectsInvalidSource(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Append(context.Background(), DraftRecord{
		EmployeeID: "emp-a", Kind: KindAction, Subject: "x", Source: Source("carrier-pigeon"),
	})
	if err == nil {
		t.Fatal("Append with invalid source: want rejection, got nil error")
	}
	if !strings.HasPrefix(err.Error(), "ledger:") {
		t.Fatalf("rejection error %q, want 'ledger:'-prefixed", err)
	}
	rows, qerr := s.QueryLog(context.Background(), LogQuery{EmployeeID: "emp-a"})
	if qerr != nil {
		t.Fatalf("QueryLog: %v", qerr)
	}
	if len(rows) != 0 {
		t.Fatalf("rejected append wrote %d rows, want none", len(rows))
	}
}

// =========================================================================
// §3 Transition matrix (AllowedTransitions)
// =========================================================================

// allStatuses is the closed set in contract order; "" means new subject.
var allStatuses = []Status{StatusOpen, StatusDone, StatusCancelled, StatusWaitingMe, StatusWaitingThem}

// legalTransition mirrors the frozen map, but the SUBJECT of the test is
// the exported AllowedTransitions — the table is built FROM it so an
// implementation that widens/narrows the map is caught by the
// accept/reject expectation derived per row.
func expectedFromAllowedTransitions(t *testing.T, from, to Status) bool {
	t.Helper()
	if from == "" {
		return true // "" (new subject) -> any
	}
	targets, ok := AllowedTransitions[from]
	if !ok {
		t.Fatalf("AllowedTransitions has no entry for current status %q", from)
	}
	return slices.Contains(targets, to)
}

func TestTransitionMatrix(t *testing.T) {
	// Frozen contract rows (also asserted independently of the map so a
	// wrong AllowedTransitions cannot make this test vacuously pass):
	type row struct {
		from Status // subject's current status ("" = new subject)
		to   Status // DraftRecord.SetStatus
		want bool   // accept?
	}
	cases := []row{
		{from: "", to: StatusOpen, want: true},
		{from: "", to: StatusDone, want: true},
		{from: "", to: StatusCancelled, want: true},
		{from: "", to: StatusWaitingMe, want: true},
		{from: "", to: StatusWaitingThem, want: true},
		{from: StatusOpen, to: StatusDone, want: true},
		{from: StatusOpen, to: StatusCancelled, want: true},
		{from: StatusOpen, to: StatusWaitingMe, want: true},
		{from: StatusOpen, to: StatusWaitingThem, want: true},
		{from: StatusOpen, to: StatusOpen, want: false},
		{from: StatusWaitingMe, to: StatusOpen, want: true},
		{from: StatusWaitingMe, to: StatusDone, want: true},
		{from: StatusWaitingMe, to: StatusCancelled, want: true},
		{from: StatusWaitingMe, to: StatusWaitingMe, want: false},
		{from: StatusWaitingMe, to: StatusWaitingThem, want: false},
		{from: StatusWaitingThem, to: StatusOpen, want: true},
		{from: StatusWaitingThem, to: StatusDone, want: true},
		{from: StatusWaitingThem, to: StatusCancelled, want: true},
		{from: StatusWaitingThem, to: StatusWaitingThem, want: false},
		{from: StatusWaitingThem, to: StatusWaitingMe, want: false},
		{from: StatusDone, to: StatusOpen, want: true}, // reopen
		{from: StatusDone, to: StatusDone, want: false},
		{from: StatusDone, to: StatusCancelled, want: false},
		{from: StatusDone, to: StatusWaitingMe, want: false},
		{from: StatusDone, to: StatusWaitingThem, want: false},
		{from: StatusCancelled, to: StatusOpen, want: true}, // reopen
		{from: StatusCancelled, to: StatusDone, want: false},
		{from: StatusCancelled, to: StatusCancelled, want: false},
		{from: StatusCancelled, to: StatusWaitingMe, want: false},
		{from: StatusCancelled, to: StatusWaitingThem, want: false},
	}

	s := newTestStore(t)
	for i, c := range cases {
		subject := fmt.Sprintf("subj-%d", i)
		ctx := context.Background()

		// Seed the subject's current status (skipped for new subject).
		if c.from != "" {
			mustAppend(t, s, DraftRecord{
				EmployeeID: "emp-t", Kind: KindCommitment, Subject: subject,
				Source: SourceCLI, SetStatus: c.from, Summary: "seed " + string(c.from),
			})
		}

		_, err := s.Append(ctx, DraftRecord{
			EmployeeID: "emp-t", Kind: KindAction, Subject: subject,
			Source: SourceCLI, SetStatus: c.to, Summary: "move " + string(c.to),
		})

		if c.want {
			if err != nil {
				t.Fatalf("case %d: %q -> %q: want accept, got %v", i, c.from, c.to, err)
			}
		} else {
			if !errors.Is(err, ErrIllegalTransition) {
				t.Fatalf("case %d: %q -> %q: want ErrIllegalTransition, got %v", i, c.from, c.to, err)
			}
		}

		// Cross-check against the exported map the implementation freezes.
		if mapWant := expectedFromAllowedTransitions(t, c.from, c.to); mapWant != c.want {
			t.Fatalf("case %d: AllowedTransitions implies %v, frozen contract says %v", i, mapWant, c.want)
		}

		// Rejected transitions must leave state untouched AND write no
		// log row (02-leaf checklist: zero rows in both tables).
		if !c.want {
			rows, rerr := s.QueryLog(ctx, LogQuery{EmployeeID: "emp-t", Subject: subject})
			if rerr != nil {
				t.Fatalf("case %d: QueryLog after rejection: %v", i, rerr)
			}
			wantRows := 0
			if c.from != "" {
				wantRows = 1 // the seed append's row; no row from the rejected append
			}
			if len(rows) != wantRows {
				t.Fatalf("case %d: rejected transition wrote %d ledger rows, want %d", i, len(rows), wantRows)
			}
			if c.from != "" {
				st, ok, gerr := s.GetState(ctx, "emp-t", subject)
				if gerr != nil || !ok {
					t.Fatalf("case %d: GetState after rejection: ok=%v err=%v", i, ok, gerr)
				}
				if st.Status != c.from {
					t.Fatalf("case %d: rejected transition mutated state to %q, want %q", i, st.Status, c.from)
				}
			} else if _, ok, gerr := s.GetState(ctx, "emp-t", subject); gerr != nil || ok {
				t.Fatalf("case %d: rejected new-subject transition created state row (ok=%v err=%v)", i, ok, gerr)
			}
		}
	}
}

// =========================================================================
// §4 Resolves
// =========================================================================

func TestResolvesOwnRow(t *testing.T) {
	s := newTestStore(t)
	opened := mustAppend(t, s, DraftRecord{
		EmployeeID: "emp-a", Kind: KindCommitment, Subject: "roof",
		Source: SourceVoice, SetStatus: StatusOpen, Summary: "owed quote",
	})
	closed := mustAppend(t, s, DraftRecord{
		EmployeeID: "emp-a", Kind: KindResolution, Subject: "roof",
		Source: SourceVoice, Resolves: opened.ID, SetStatus: StatusDone, Summary: "quote sent",
	})
	if closed.Resolves != opened.ID {
		t.Fatalf("stored Resolves = %d, want %d", closed.Resolves, opened.ID)
	}
}

func TestResolvesUnknownID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	prior := mustAppend(t, s, DraftRecord{
		EmployeeID: "emp-a", Kind: KindCommitment, Subject: "roof", Source: SourceVoice,
	})

	_, err := s.Append(ctx, DraftRecord{
		EmployeeID: "emp-a", Kind: KindResolution, Subject: "roof",
		Source: SourceVoice, Resolves: 999_999,
	})
	if !errors.Is(err, ErrResolveNotFound) {
		t.Fatalf("resolves=unknown id: want ErrResolveNotFound, got %v", err)
	}

	// Transaction rolled back: the failed append wrote nothing, and only
	// the prior row exists for the employee.
	rows, qerr := s.QueryLog(ctx, LogQuery{EmployeeID: "emp-a"})
	if qerr != nil {
		t.Fatalf("QueryLog: %v", qerr)
	}
	if len(rows) != 1 || rows[0].ID != prior.ID {
		t.Fatalf("after rollback: %d rows %v, want exactly the prior row", len(rows), ids(rows))
	}
}

func TestResolvesForeignEmployee(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// Row belongs to emp-b.
	foreign := mustAppend(t, s, DraftRecord{
		EmployeeID: "emp-b", Kind: KindCommitment, Subject: "other", Source: SourceTask,
	})
	// emp-a resolving emp-b's id is a foreign reference -> ErrResolveNotFound.
	_, err := s.Append(ctx, DraftRecord{
		EmployeeID: "emp-a", Kind: KindResolution, Subject: "other",
		Source: SourceTask, Resolves: foreign.ID,
	})
	if !errors.Is(err, ErrResolveNotFound) {
		t.Fatalf("resolves=foreign-employee id: want ErrResolveNotFound, got %v", err)
	}
	rows, qerr := s.QueryLog(ctx, LogQuery{EmployeeID: "emp-a"})
	if qerr != nil {
		t.Fatalf("QueryLog: %v", qerr)
	}
	if len(rows) != 0 {
		t.Fatalf("failed cross-employee append wrote %d rows, want rollback to 0", len(rows))
	}
}

func ids(rows []Record) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

// =========================================================================
// §5 PayloadEnvelope + state upsert
// =========================================================================

// envelope mirrors the frozen {"v":1,"status","summary","data"} wrap.
type envelope struct {
	V       int             `json:"v"`
	Status  string          `json:"status"`
	Summary string          `json:"summary"`
	Data    json.RawMessage `json:"data"`
}

func TestSetStatusWritesEnvelopeAndState(t *testing.T) {
	s, dbPath := newTestStoreAt(t)
	ctx := context.Background()
	userPayload := []byte(`{"note":"quote promised"}`)

	rec := mustAppend(t, s, DraftRecord{
		EmployeeID: "emp-a", Kind: KindCommitment, Subject: "roof",
		Payload: userPayload, Source: SourceVoice, SourceRef: "cc-1",
		SetStatus: StatusWaitingMe, Summary: "owe quote by friday",
	})

	// The LEDGER ROW payload must decode to the frozen envelope with the
	// user payload preserved under "data" (raw read = ground truth).
	var env envelope
	if err := json.Unmarshal(readRawPayload(t, dbPath, rec.ID), &env); err != nil {
		t.Fatalf("stored row payload is not the envelope JSON: %v", err)
	}
	if env.V != 1 {
		t.Fatalf("envelope v = %d, want 1", env.V)
	}
	if env.Status != string(StatusWaitingMe) {
		t.Fatalf("envelope status = %q, want %q", env.Status, StatusWaitingMe)
	}
	if env.Summary != "owe quote by friday" {
		t.Fatalf("envelope summary = %q", env.Summary)
	}
	if string(env.Data) != string(userPayload) {
		t.Fatalf("envelope data = %s, want user payload %s", env.Data, userPayload)
	}

	// ledger_state must be upserted with as_of = this row's id.
	st, ok, err := s.GetState(ctx, "emp-a", "roof")
	if err != nil || !ok {
		t.Fatalf("GetState after SetStatus append: ok=%v err=%v", ok, err)
	}
	if st.Status != StatusWaitingMe {
		t.Fatalf("state status = %q, want %q", st.Status, StatusWaitingMe)
	}
	if st.Summary != "owe quote by friday" {
		t.Fatalf("state summary = %q", st.Summary)
	}
	if st.AsOf != rec.ID {
		t.Fatalf("state as_of = %d, want appended row id %d", st.AsOf, rec.ID)
	}
	if st.EmployeeID != "emp-a" || st.Subject != "roof" {
		t.Fatalf("state keyed by %q/%q", st.EmployeeID, st.Subject)
	}
}

func TestSetStatusNullDataEnvelope(t *testing.T) {
	// Nil user payload + SetStatus -> stored row envelope with "data": null.
	s, dbPath := newTestStoreAt(t)
	rec := mustAppend(t, s, DraftRecord{
		EmployeeID: "emp-a", Kind: KindAction, Subject: "no-data",
		Source: SourceDesk, SetStatus: StatusOpen, Summary: "just opened",
	})
	var env envelope
	if err := json.Unmarshal(readRawPayload(t, dbPath, rec.ID), &env); err != nil {
		t.Fatalf("stored row payload not envelope JSON: %v", err)
	}
	if env.V != 1 || env.Status != string(StatusOpen) || env.Summary != "just opened" {
		t.Fatalf("envelope drifted: %+v", env)
	}
	if string(env.Data) != "null" {
		t.Fatalf("envelope data = %s, want JSON null for nil user payload", env.Data)
	}
}

func TestLogOnlyAppendLeavesStateUntouched(t *testing.T) {
	s, dbPath := newTestStoreAt(t)
	ctx := context.Background()

	// No state exists yet for a new employee/subject.
	if _, ok, err := s.GetState(ctx, "emp-a", "ghost"); err != nil || ok {
		t.Fatalf("GetState before any SetStatus: ok=%v err=%v, want false/nil", ok, err)
	}

	// Log-only append (SetStatus == "") must not create or alter state.
	logOnly := mustAppend(t, s, DraftRecord{
		EmployeeID: "emp-a", Kind: KindObservation, Subject: "ghost",
		Payload: []byte(`{"mood":"sunny"}`), Source: SourceTool,
	})
	if _, ok, err := s.GetState(ctx, "emp-a", "ghost"); err != nil || ok {
		t.Fatalf("GetState after log-only append: ok=%v err=%v, want state untouched (false/nil)", ok, err)
	}
	// Log-only row is stored WITHOUT an envelope (raw read: the user
	// payload verbatim is the strictest reading of "opaque JSON"; at
	// minimum it must not decode as a status-carrying envelope).
	rawLogOnly := readRawPayload(t, dbPath, logOnly.ID)
	var env envelope
	if err := json.Unmarshal(rawLogOnly, &env); err == nil && env.V == 1 && env.Status != "" {
		t.Fatalf("log-only row was wrapped in a status envelope: %s", rawLogOnly)
	}
	if string(rawLogOnly) != `{"mood":"sunny"}` {
		t.Fatalf("log-only row payload = %s, want user payload stored verbatim", rawLogOnly)
	}

	// Pre-existing state must survive a later log-only append unchanged.
	tracked := mustAppend(t, s, DraftRecord{
		EmployeeID: "emp-a", Kind: KindCommitment, Subject: "roof",
		Source: SourceVoice, SetStatus: StatusOpen, Summary: "open thing",
	})
	before, ok, err := s.GetState(ctx, "emp-a", "roof")
	if err != nil || !ok {
		t.Fatalf("GetState: ok=%v err=%v", ok, err)
	}
	mustAppend(t, s, DraftRecord{
		EmployeeID: "emp-a", Kind: KindCommittal, Subject: "roof",
		Payload: []byte(`{"said":"by june"}`), Source: SourceVoice,
	})
	after, ok, err := s.GetState(ctx, "emp-a", "roof")
	if err != nil || !ok {
		t.Fatalf("GetState after log-only: ok=%v err=%v", ok, err)
	}
	if after.Status != before.Status || after.Summary != before.Summary || after.AsOf != tracked.ID {
		t.Fatalf("log-only append mutated state: before %+v after %+v", before, after)
	}
}

// =========================================================================
// §5b WithAfterAppend hook seam (exported Option; wired by 02-03 publish)
// =========================================================================

func TestWithAfterAppendHookFiresPerSuccessfulAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.db")

	type call struct {
		rec Record
		st  *State
	}
	var calls []call

	// NOTE: the plan says both "exported Option WithAfterAppend" and
	// "Store.WithAfterAppend"; the frozen Open(dbPath, logger) signature
	// carries no options, so the gate pins the Store-method reading,
	// preserving the 2-arg Open. If leaf 02 lands an Open(...opts) form
	// instead, this call site is the one to adjust.
	s, err := Open(path, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.WithAfterAppend(func(rec Record, st *State) {
		calls = append(calls, call{rec, st})
	})
	defer s.Close()
	ctx := context.Background()

	// Status-carrying append -> hook fires with the record + state.
	carrying := mustAppend(t, s, DraftRecord{EmployeeID: "emp-h", Kind: KindCommitment,
		Subject: "hooked", Source: SourceCLI, SetStatus: StatusOpen, Summary: "open"})
	// Log-only append -> hook fires with a nil State (frozen seam shape).
	logOnly := mustAppend(t, s, DraftRecord{EmployeeID: "emp-h", Kind: KindObservation,
		Subject: "hooked", Source: SourceCLI})
	// Rejected append (open->open illegal) -> ZERO additional calls.
	if _, err := s.Append(ctx, DraftRecord{EmployeeID: "emp-h", Kind: KindAction,
		Subject: "hooked", Source: SourceCLI, SetStatus: StatusOpen}); !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("expected open->open rejection, got %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("hook fired %d times, want exactly 2 (once per successful append): %+v", len(calls), calls)
	}
	if calls[0].rec.ID != carrying.ID || calls[0].st == nil || calls[0].st.Status != StatusOpen {
		t.Fatalf("status-carrying hook args drifted: rec=%+v st=%+v", calls[0].rec, calls[0].st)
	}
	if calls[1].rec.ID != logOnly.ID || calls[1].st != nil {
		t.Fatalf("log-only hook args: rec=%+v st=%v, want nil *State", calls[1].rec, calls[1].st)
	}
}

// =========================================================================
// §6 Queries
// =========================================================================

func TestQueryLogFilters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a1 := mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindObservation, Subject: "roof", Source: SourceVoice})
	a2 := mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindCommitment, Subject: "roof", Source: SourceVoice})
	a3 := mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindAction, Subject: "fence", Source: SourceTask})
	mustAppend(t, s, DraftRecord{EmployeeID: "emp-b", Kind: KindAction, Subject: "other", Source: SourceTask})

	// subject filter
	rows, err := s.QueryLog(ctx, LogQuery{EmployeeID: "emp-a", Subject: "roof"})
	if err != nil {
		t.Fatalf("QueryLog subject: %v", err)
	}
	if !slices.Equal(ids(rows), []int64{a1.ID, a2.ID}) {
		t.Fatalf("subject filter ids = %v, want [%d %d] in id order", ids(rows), a1.ID, a2.ID)
	}

	// kinds filter
	rows, err = s.QueryLog(ctx, LogQuery{EmployeeID: "emp-a", Kinds: []Kind{KindAction}})
	if err != nil {
		t.Fatalf("QueryLog kinds: %v", err)
	}
	if !slices.Equal(ids(rows), []int64{a3.ID}) {
		t.Fatalf("kinds filter ids = %v, want [%d]", ids(rows), a3.ID)
	}
	rows, err = s.QueryLog(ctx, LogQuery{EmployeeID: "emp-a", Kinds: []Kind{KindObservation, KindCommitment}})
	if err != nil {
		t.Fatalf("QueryLog multi-kind: %v", err)
	}
	if !slices.Equal(ids(rows), []int64{a1.ID, a2.ID}) {
		t.Fatalf("multi-kind filter ids = %v, want [%d %d]", ids(rows), a1.ID, a2.ID)
	}

	// since_id (exclusive: ID > sinceID)
	rows, err = s.QueryLog(ctx, LogQuery{EmployeeID: "emp-a", SinceID: a2.ID})
	if err != nil {
		t.Fatalf("QueryLog since_id: %v", err)
	}
	if !slices.Equal(ids(rows), []int64{a3.ID}) {
		t.Fatalf("since_id=%d ids = %v, want [%d]", a2.ID, ids(rows), a3.ID)
	}

	// limit
	rows, err = s.QueryLog(ctx, LogQuery{EmployeeID: "emp-a", Limit: 2})
	if err != nil {
		t.Fatalf("QueryLog limit: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("limit=2 returned %d rows, want 2", len(rows))
	}

	// other employee is invisible
	rows, err = s.QueryLog(ctx, LogQuery{EmployeeID: "emp-b"})
	if err != nil {
		t.Fatalf("QueryLog emp-b: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("emp-b rows = %d, want 1", len(rows))
	}
}

func TestQueryLogLimitsDefault200Max1000(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const n = 250
	for i := 0; i < n; i++ {
		mustAppend(t, s, DraftRecord{
			EmployeeID: "emp-many", Kind: KindObservation,
			Subject: fmt.Sprintf("s%03d", i), Source: SourceTool,
		})
	}

	// Limit 0 -> default 200.
	rows, err := s.QueryLog(ctx, LogQuery{EmployeeID: "emp-many"})
	if err != nil {
		t.Fatalf("QueryLog default limit: %v", err)
	}
	if len(rows) != 200 {
		t.Fatalf("default limit returned %d rows, want 200", len(rows))
	}

	// Limit > 1000 is clamped to 1000 (here: the 250 available rows are
	// all returned; the clamp is that no error is raised and we never get
	// more than 1000).
	rows, err = s.QueryLog(ctx, LogQuery{EmployeeID: "emp-many", Limit: 5000})
	if err != nil {
		t.Fatalf("QueryLog limit=5000: %v", err)
	}
	if len(rows) != n {
		t.Fatalf("limit clamped above max: got %d rows, want %d (all, capped at 1000)", len(rows), n)
	}

	// Explicit in-range limit honoured.
	rows, err = s.QueryLog(ctx, LogQuery{EmployeeID: "emp-many", Limit: 1000})
	if err != nil {
		t.Fatalf("QueryLog limit=1000: %v", err)
	}
	if len(rows) != n {
		t.Fatalf("limit=1000 returned %d rows, want %d", len(rows), n)
	}
}

func TestQueryOpenExcludesClosed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindCommitment, Subject: "open-one",
		Source: SourceVoice, SetStatus: StatusOpen, Summary: "open"})
	mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindCommitment, Subject: "waiting-one",
		Source: SourceVoice, SetStatus: StatusWaitingThem, Summary: "waiting"})
	mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindCommitment, Subject: "done-one",
		Source: SourceVoice, SetStatus: StatusDone, Summary: "done"})
	mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindCommitment, Subject: "cancelled-one",
		Source: SourceVoice, SetStatus: StatusCancelled, Summary: "cancelled"})
	// Log-only on a fresh subject creates no state row -> excluded.
	mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindObservation,
		Subject: "log-only", Source: SourceTool})
	// Other employee's open state must not leak.
	mustAppend(t, s, DraftRecord{EmployeeID: "emp-b", Kind: KindCommitment, Subject: "theirs",
		Source: SourceVoice, SetStatus: StatusOpen, Summary: "not yours"})

	open, err := s.QueryOpen(ctx, "emp-a")
	if err != nil {
		t.Fatalf("QueryOpen: %v", err)
	}
	var subjects []string
	for _, st := range open {
		if st.Status == StatusDone || st.Status == StatusCancelled {
			t.Fatalf("QueryOpen returned closed status %q for %q", st.Status, st.Subject)
		}
		if st.EmployeeID != "emp-a" {
			t.Fatalf("QueryOpen leaked employee %q", st.EmployeeID)
		}
		subjects = append(subjects, st.Subject)
	}
	if !slices.Equal(subjects, []string{"waiting-one", "open-one"}) {
		t.Fatalf("QueryOpen subjects = %v, want [waiting-one open-one] (newest-updated first)", subjects)
	}
}

func TestGetStateOkAndMissing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	rec := mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindAction, Subject: "known",
		Source: SourceCLI, SetStatus: StatusWaitingMe, Summary: "my turn"})

	st, ok, err := s.GetState(ctx, "emp-a", "known")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if !ok {
		t.Fatal("GetState(existing): ok = false, want true")
	}
	if st.Status != StatusWaitingMe || st.Summary != "my turn" || st.AsOf != rec.ID {
		t.Fatalf("GetState drifted: %+v (want as_of=%d)", st, rec.ID)
	}

	// Missing subject -> zero State, false, nil error.
	st, ok, err = s.GetState(ctx, "emp-a", "nope")
	if err != nil {
		t.Fatalf("GetState(missing) error: %v, want nil", err)
	}
	if ok {
		t.Fatal("GetState(missing): ok = true, want false")
	}
	if st != (State{}) {
		t.Fatalf("GetState(missing): state = %+v, want zero value", st)
	}
}

// =========================================================================
// §7 VerifyState / RepairState
//
// Corruption is done by writing ledger rows directly with a raw sqlite
// handle (bypassing Store.Append) and forcing state divergence via raw
// UPDATEs. The fold's status-carrying rows are identified ONLY by the
// frozen payload envelope, so the injected rows use that envelope.
// =========================================================================

func rawAppendEnvelope(t *testing.T, dbPath string, employee, subject string, id int64, status, summary string, data string) {
	t.Helper()
	db := openDB(t, dbPath)
	ctx := context.Background()
	var payload string
	if status == "" {
		payload = data // log-only row, no envelope
	} else {
		payload = fmt.Sprintf(`{"v":1,"status":%q,"summary":%q,"data":%s}`, status, summary, data)
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO ledger (id, employee_id, at, kind, subject, payload, source, source_ref, resolves)
         VALUES (?, ?, ?, 'commitment', ?, ?, 'cli', '', 0)`,
		id, employee, time.Now().UTC().Format(time.RFC3339Nano), subject, payload)
	if err != nil {
		t.Fatalf("raw ledger insert id=%d: %v", id, err)
	}
}

func rawUpsertState(t *testing.T, dbPath string, employee, subject string, status, summary string, asOf int64) {
	t.Helper()
	db := openDB(t, dbPath)
	ctx := context.Background()
	_, err := db.ExecContext(ctx,
		`INSERT INTO ledger_state (employee_id, subject, status, summary, updated_at, as_of)
         VALUES (?, ?, ?, ?, ?, ?)
         ON CONFLICT(employee_id, subject) DO UPDATE SET
             status=excluded.status, summary=excluded.summary,
             updated_at=excluded.updated_at, as_of=excluded.as_of`,
		employee, subject, status, summary, time.Now().UTC().Format(time.RFC3339Nano), asOf)
	if err != nil {
		t.Fatalf("raw state upsert %s/%s: %v", employee, subject, err)
	}
}

func TestVerifyStateClean(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Mix of status-carrying and log-only rows through the real API.
	mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindCommitment, Subject: "roof",
		Payload: []byte(`{"x":1}`), Source: SourceVoice, SetStatus: StatusOpen, Summary: "open"})
	mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindObservation, Subject: "roof",
		Payload: []byte(`{"note":"log only"}`), Source: SourceVoice})
	mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindResolution, Subject: "roof",
		Source: SourceVoice, SetStatus: StatusDone, Summary: "done"})

	rep, err := s.VerifyState(ctx)
	if err != nil {
		t.Fatalf("VerifyState: %v", err)
	}
	if rep == nil {
		t.Fatal("VerifyState returned nil report")
	}
	if rep.Divergent != 0 || len(rep.Diffs) != 0 {
		t.Fatalf("clean store reports %d divergent %v, want 0", rep.Divergent, rep.Diffs)
	}
	if rep.Checked < 1 {
		t.Fatalf("Checked = %d, want >= 1 (the one live subject)", rep.Checked)
	}
}

func TestVerifyStateDetectsCorruption(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ledger.db")
	s, err := Open(dbPath, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	// Fold-derived truth for emp-c/ghost: row 9001 sets open, row 9002
	// sets done (raw inserts, ids in the reserved test range).
	rawAppendEnvelope(t, dbPath, "emp-c", "ghost", 9001, "open", "seed open", "null")
	rawAppendEnvelope(t, dbPath, "emp-c", "ghost", 9002, "done", "seed done", "null")
	// Hand-corrupt the state row away from the fold.
	rawUpsertState(t, dbPath, "emp-c", "ghost", "waiting_on_me", "corrupted", 9001)

	rep, err := s.VerifyState(ctx)
	if err != nil {
		t.Fatalf("VerifyState: %v", err)
	}
	if rep.Divergent != 1 || len(rep.Diffs) != 1 {
		t.Fatalf("divergent = %d (diffs %v), want exactly 1", rep.Divergent, rep.Diffs)
	}
	d := rep.Diffs[0]
	if d.EmployeeID != "emp-c" || d.Subject != "ghost" {
		t.Fatalf("diff keyed by %q/%q, want emp-c/ghost", d.EmployeeID, d.Subject)
	}
	if d.Have.Status != StatusWaitingMe {
		t.Fatalf("diff.Have.Status = %q, want corrupted waiting_on_me", d.Have.Status)
	}
	// Want = the recomputed fold: the latest envelope row (9002) says done.
	if d.Want.Status != StatusDone {
		t.Fatalf("diff.Want.Status = %q, want done (fold of latest envelope)", d.Want.Status)
	}
	if d.Want.AsOf != 9002 {
		t.Fatalf("diff.Want.AsOf = %d, want 9002", d.Want.AsOf)
	}
	if d.Want.Summary != "seed done" {
		t.Fatalf("diff.Want.Summary = %q, want fold summary", d.Want.Summary)
	}
}

func TestRepairStateFixesDivergence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ledger.db")
	s, err := Open(dbPath, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	// Healthy subject through the API (must survive repair untouched).
	live := mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindCommitment, Subject: "roof",
		Source: SourceVoice, SetStatus: StatusOpen, Summary: "live open"})
	// Corrupted subject (raw).
	rawAppendEnvelope(t, dbPath, "emp-c", "ghost", 9101, "cancelled", "fold cancelled", "null")
	rawUpsertState(t, dbPath, "emp-c", "ghost", "open", "corrupted", 9101)

	n, err := s.RepairState(ctx)
	if err != nil {
		t.Fatalf("RepairState: %v", err)
	}
	if n != 1 {
		t.Fatalf("RepairState returned %d, want 1", n)
	}

	// Repaired row now equals the fold.
	st, ok, err := s.GetState(ctx, "emp-c", "ghost")
	if err != nil || !ok {
		t.Fatalf("GetState(repaired): ok=%v err=%v", ok, err)
	}
	if st.Status != StatusCancelled || st.AsOf != 9101 {
		t.Fatalf("repaired state = %+v, want cancelled/as_of 9101", st)
	}

	// Untouched row keeps as_of pointing at its live append.
	liveSt, ok, err := s.GetState(ctx, "emp-a", "roof")
	if err != nil || !ok {
		t.Fatalf("GetState(live): ok=%v err=%v", ok, err)
	}
	if liveSt.Status != StatusOpen || liveSt.AsOf != live.ID {
		t.Fatalf("healthy row mutated by repair: %+v (want as_of=%d)", liveSt, live.ID)
	}

	// Second verify is clean.
	rep, err := s.VerifyState(ctx)
	if err != nil {
		t.Fatalf("VerifyState after repair: %v", err)
	}
	if rep.Divergent != 0 || len(rep.Diffs) != 0 {
		t.Fatalf("post-repair divergent = %d %v, want 0", rep.Divergent, rep.Diffs)
	}
	// Repair is idempotent: nothing left to fix.
	n, err = s.RepairState(ctx)
	if err != nil {
		t.Fatalf("second RepairState: %v", err)
	}
	if n != 0 {
		t.Fatalf("second RepairState returned %d, want 0", n)
	}
}

// =========================================================================
// §8 Tail
// =========================================================================

// drainOne waits up to ~2s for one record on ch; fails the test on
// timeout or closed channel.
func drainOne(t *testing.T, ch <-chan Record) Record {
	t.Helper()
	select {
	case rec, ok := <-ch:
		if !ok {
			t.Fatal("Tail channel closed before delivering a record")
		}
		return rec
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Tail record")
		return Record{}
	}
}

func TestTailCatchUp(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r1 := mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindAction, Subject: "one", Source: SourceCLI})
	r2 := mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindAction, Subject: "two", Source: SourceCLI})
	r3 := mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindAction, Subject: "three", Source: SourceCLI})

	// sinceID = r1.ID -> catch-up delivers r2, r3 from SQLite.
	ch, err := s.Tail(ctx, r1.ID)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if got := drainOne(t, ch); got.ID != r2.ID {
		t.Fatalf("catch-up first record ID = %d, want %d", got.ID, r2.ID)
	}
	if got := drainOne(t, ch); got.ID != r3.ID {
		t.Fatalf("catch-up second record ID = %d, want %d", got.ID, r3.ID)
	}

	// sinceID = 0 -> everything from the start.
	ch0, err := s.Tail(ctx, 0)
	if err != nil {
		t.Fatalf("Tail(0): %v", err)
	}
	if got := drainOne(t, ch0); got.ID != r1.ID {
		t.Fatalf("Tail(0) first ID = %d, want %d", got.ID, r1.ID)
	}
}

func TestTailLiveAppend(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	last := mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindAction, Subject: "seed", Source: SourceCLI})

	ch, err := s.Tail(ctx, last.ID)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}

	// Nothing pending yet; then an append AFTER registration must arrive
	// live over the same channel.
	newRec := mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindCommitment, Subject: "live", Source: SourceCLI})
	got := drainOne(t, ch)
	if got.ID != newRec.ID {
		t.Fatalf("live record ID = %d, want %d", got.ID, newRec.ID)
	}
	if got.Subject != "live" || got.Kind != KindCommitment {
		t.Fatalf("live record drifted: %+v", got)
	}
}

func TestTailRegisterThenAppendsNoGaps(t *testing.T) {
	// The frozen guarantee: registration happens BEFORE the catch-up read,
	// so rows appended while Tail is being set up are delivered exactly
	// once, in id order, with no gap.
	s := newTestStore(t)
	ctx := context.Background()
	base := mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindAction, Subject: "base", Source: SourceCLI})

	ch, err := s.Tail(ctx, base.ID)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	a := mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindAction, Subject: "a", Source: SourceCLI})
	b := mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindAction, Subject: "b", Source: SourceCLI})
	c := mustAppend(t, s, DraftRecord{EmployeeID: "emp-a", Kind: KindAction, Subject: "c", Source: SourceCLI})

	want := []int64{a.ID, b.ID, c.ID}
	if !slices.IsSorted(want) {
		t.Fatalf("rowids not monotonic: %v", want)
	}
	var got []int64
	for range want {
		got = append(got, drainOne(t, ch).ID)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Tail delivered %v, want %v in order with no gaps", got, want)
	}
	// Exactly-once: channel must not immediately yield a 4th record.
	select {
	case extra := <-ch:
		t.Fatalf("duplicate/gap-filling delivery: %+v", extra)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestTailCtxCancelCloses(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := s.Tail(ctx, 0)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	cancel()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case _, ok := <-ch:
			if !ok {
				return // closed as required
			}
			// A straggler record (e.g. the seed row) is fine; keep
			// draining until the channel closes.
		case <-time.After(time.Until(deadline)):
			t.Fatal("Tail channel not closed within 2s after ctx cancel")
		}
	}
	t.Fatal("Tail channel not closed within 2s after ctx cancel")
}
