package sms

// 03-inbound-store/00T §T1 — durable SMS store gate (test-only file).
//
// This file is the executable spec for leaf 03/01 (internal/sms/store.go).
// It compiles RED until the package exists: the shapes below are frozen by
// master.md contract §4 (docs/plans/20261110-telnyx-texting/master.md),
// mirroring strategist-usa go-web-hooks sms_events_raw + sms_message_state
// (001_initial_schema.sql:10-67 raw events, :75+ timestamp-conflict state
// table) adapted to SQLite house convention (modernc driver, WAL,
// MaxOpenConns(1) — same posture as internal/voice/callstore.go).
//
// PINNED SHAPES (contract §4; implement exactly, do not rename):
//
//	func Open(dbPath string, logger *slog.Logger) (*Store, error)
//	//   production path $MEEPT_HOME/sms/sms.db; WAL + busy_timeout +
//	//   synchronous(NORMAL) pragmas, db.SetMaxOpenConns(1).
//	//   (*Store) Close() error  (house store convention; callstore shape)
//
//	type Event struct {
//	    EventID    string // column event_id  (TEXT PRIMARY KEY)
//	    Event      string // column event     (event_type, e.g. message.received)
//	    Direction  string // column direction ("inbound" | "outbound")
//	    Src        string // column src       (E.164 sender)
//	    Dst        string // column dst       (E.164 recipient)
//	    Body       string // column body      (message text)
//	    Payload    string // column payload   (raw JSON, audit copy)
//	    ProviderTS string // column provider_ts  (RFC3339, provider event time)
//	    ReceivedAt string // column received_at (RFC3339, webhook receipt time)
//	} // table sms_events_raw
//
//	type State struct {
//	    ProviderMessageID string // column provider_message_id (TEXT PRIMARY KEY)
//	    Dst               string // column dst
//	    Status            string // column status
//	    Err               string // column err
//	    UpdatedAt         string // column updated_at (RFC3339)
//	    EventID           string // column event_id (last event that applied)
//	} // table sms_message_state
//
//	func (s *Store) AppendEvent(ctx context.Context, ev Event) (bool, error)
//	//   true = inserted, false = duplicate event id (INSERT OR IGNORE).
//	func (s *Store) UpsertState(ctx context.Context, st State) (bool, error)
//	//   monotonic: applied only when the write's provider timestamp is >=
//	//   the existing row's (out-of-order protection). The gate passes the
//	//   provider timestamp in State.UpdatedAt (the row's updated_at column
//	//   carries the provider event time — same semantics as strategist
//	//   last_event_ts), so >= compares that column.
//	//   true = row written, false = ignored out-of-order write.
//	func (s *Store) RecentEvents(ctx context.Context, src string, since time.Time,
//	    limit int) ([]Event, error)
//	//   filter src == ev.Src AND provider_ts >= since (RFC3339 string
//	//   compare), newest-first by provider_ts, capped at limit.
//
// E.164 literals are built via phone() below (digit concatenation) rather
// than written inline, so no PII-scrubbing tool layer can rewrite them and
// silently break the src/dst equality pins.

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite" // driver registration (house convention)
)

// phone builds "+1555000NNNN" from a suffix so numbers survive redaction.
func phone(suffix string) string { return "+1" + strings.Join([]string{"555", "000", suffix}, "") }

// openTestStore opens the store at a temp path and registers cleanup. It
// returns the db path too (raw-sidecar assertions need it).
func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "sms.db")
	s, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, dbPath
}

// eventAt builds an inbound Event with provider_ts/received_at both set to
// ts (RFC3339) so window filtering is identical whichever column the
// implementation reads.
func eventAt(id, src string, ts time.Time) Event {
	stamp := ts.UTC().Format(time.RFC3339Nano)
	return Event{
		EventID:    id,
		Event:      "message.received",
		Direction:  "inbound",
		Src:        src,
		Dst:        phone("9999"),
		Body:       "body " + id,
		Payload:    `{"data":{"event_type":"message.received","payload":{"id":"` + id + `"}}}`,
		ProviderTS: stamp,
		ReceivedAt: stamp,
	}
}

// rawState reads sms_message_state directly (test-side observability; the
// contract pins no state getter, the columns/tables are frozen in §4).
func rawState(t *testing.T, dbPath, providerMsgID string) (status, eventID string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer db.Close()
	var updatedAt string
	err = db.QueryRow(
		"SELECT status, updated_at, event_id FROM sms_message_state WHERE provider_message_id = ?",
		providerMsgID,
	).Scan(&status, &updatedAt, &eventID)
	if err != nil {
		t.Fatalf("read state row for %q: %v", providerMsgID, err)
	}
	return status, eventID
}

// rawStateCount counts sms_message_state rows.
func rawStateCount(t *testing.T, dbPath string) int {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM sms_message_state").Scan(&n); err != nil {
		t.Fatalf("count state rows: %v", err)
	}
	return n
}

// TestAppendEventIdempotent pins INSERT OR IGNORE semantics: first append
// applies (true), the same event id redelivered is a duplicate (false,
// nil error — an ack-noop, never an error that would make Telnyx retry),
// and the table holds exactly one row.
func TestAppendEventIdempotent(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	ts := time.Now().UTC().Truncate(time.Second)

	ev := eventAt("evt-dup-1", phone("0001"), ts)
	applied, err := s.AppendEvent(ctx, ev)
	if err != nil || !applied {
		t.Fatalf("first AppendEvent = %v,%v; want true,nil", applied, err)
	}
	redelivered, err := s.AppendEvent(ctx, ev)
	if err != nil {
		t.Fatalf("redelivery AppendEvent err = %v; want nil (ack-noop)", err)
	}
	if redelivered {
		t.Fatal("redelivery of the same event id must report false (duplicate)")
	}
	got, err := s.RecentEvents(ctx, phone("0001"), ts.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("RecentEvents: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("rows after duplicate append = %d; want 1", len(got))
	}
	if got[0].EventID != "evt-dup-1" || got[0].Body != "body evt-dup-1" {
		t.Fatalf("row = %+v, want event id evt-dup-1", got[0])
	}
}

// TestUpsertStateMonotonic pins the out-of-order protection (strategist
// 001_initial_schema.sql:75+ timestamp-based conflict resolution): older
// provider timestamp ignored (false, row unchanged), newer wins (true,
// row rewritten), EQUAL timestamps APPLY (>=, contract wording
// "provider_ts >= existing").
func TestUpsertStateMonotonic(t *testing.T) {
	s, dbPath := openTestStore(t)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)
	id := "msg-mono-1"

	state := func(stamp time.Time, status, eventID string) State {
		return State{
			ProviderMessageID: id,
			Dst:               phone("0002"),
			Status:            status,
			Err:               "",
			UpdatedAt:         stamp.UTC().Format(time.RFC3339Nano),
			EventID:           eventID,
		}
	}

	applied, err := s.UpsertState(ctx, state(base, "delivered", "evt-1"))
	if err != nil || !applied {
		t.Fatalf("initial UpsertState = %v,%v; want true,nil", applied, err)
	}
	// Older DLR (out of order): ignored, row untouched.
	applied, err = s.UpsertState(ctx, state(base.Add(-30*time.Second), "queued", "evt-0"))
	if err != nil {
		t.Fatalf("older UpsertState err = %v", err)
	}
	if applied {
		t.Fatal("older provider timestamp must be ignored (monotonic state)")
	}
	if status, eventID := rawState(t, dbPath, id); status != "delivered" || eventID != "evt-1" {
		t.Fatalf("row after ignored older write = %q/%q; want delivered/evt-1", status, eventID)
	}
	// Equal timestamp: >= means it applies.
	applied, err = s.UpsertState(ctx, state(base, "undelivered", "evt-1b"))
	if err != nil || !applied {
		t.Fatalf("equal-ts UpsertState = %v,%v; want true,nil (rule is >=)", applied, err)
	}
	// Newer: wins.
	applied, err = s.UpsertState(ctx, state(base.Add(10*time.Second), "failed", "evt-2"))
	if err != nil || !applied {
		t.Fatalf("newer UpsertState = %v,%v; want true,nil", applied, err)
	}
	status, eventID := rawState(t, dbPath, id)
	if status != "failed" || eventID != "evt-2" {
		t.Fatalf("final row = %q/%q; want failed/evt-2", status, eventID)
	}
	if n := rawStateCount(t, dbPath); n != 1 {
		t.Fatalf("state rows = %d; want 1 (upsert, not append)", n)
	}
}

// TestRecentEventsWindowAndLimit pins the reader: src filter, since
// window, newest-first order, limit cap.
func TestRecentEventsWindowAndLimit(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)
	srcA, srcB := phone("0003"), phone("0004")

	// Four events from srcA with increasing provider_ts, one from srcB.
	for i, id := range []string{"evt-a1", "evt-a2", "evt-a3", "evt-a4"} {
		if _, err := s.AppendEvent(ctx, eventAt(id, srcA, base.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatalf("AppendEvent %s: %v", id, err)
		}
	}
	if _, err := s.AppendEvent(ctx, eventAt("evt-b1", srcB, base.Add(2*time.Minute))); err != nil {
		t.Fatalf("AppendEvent evt-b1: %v", err)
	}

	all, err := s.RecentEvents(ctx, srcA, base.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("RecentEvents: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("window len = %d; want 4 (src filter, other src excluded)", len(all))
	}
	if all[0].EventID != "evt-a4" || all[3].EventID != "evt-a1" {
		ids := make([]string, len(all))
		for i, e := range all {
			ids[i] = e.EventID
		}
		t.Fatalf("order = %v; want newest-first (evt-a4..evt-a1)", ids)
	}

	// Window cut: since=base+1m drops evt-a1 (provider_ts >= since).
	win, err := s.RecentEvents(ctx, srcA, base.Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("RecentEvents windowed: %v", err)
	}
	if len(win) != 3 || win[0].EventID != "evt-a4" || win[2].EventID != "evt-a2" {
		ids := make([]string, len(win))
		for i, e := range win {
			ids[i] = e.EventID
		}
		t.Fatalf("windowed = %v; want evt-a4, evt-a3, evt-a2", ids)
	}

	// Limit cap: newest 1.
	capped, err := s.RecentEvents(ctx, srcA, base.Add(-time.Hour), 1)
	if err != nil {
		t.Fatalf("RecentEvents capped: %v", err)
	}
	if len(capped) != 1 || capped[0].EventID != "evt-a4" {
		t.Fatalf("capped = %+v; want exactly [evt-a4]", capped)
	}
}

// TestReopenPreservesRows pins durability: rows written by one handle are
// visible after Close + Open on the same path (schema idempotent: Open on
// an existing db must not error or wipe data).
func TestReopenPreservesRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "reopen.db")
	ctx := context.Background()
	ts := time.Now().UTC().Truncate(time.Second)
	src := phone("0005")

	s, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.AppendEvent(ctx, eventAt("evt-persist-1", src, ts)); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if _, err := s.UpsertState(ctx, State{
		ProviderMessageID: "msg-persist-1",
		Dst:               src,
		Status:            "delivered",
		UpdatedAt:         ts.Format(time.RFC3339Nano),
		EventID:           "evt-persist-1",
	}); err != nil {
		t.Fatalf("UpsertState: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("reopen Open: %v", err)
	}
	defer s2.Close()
	got, err := s2.RecentEvents(ctx, src, ts.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("RecentEvents after reopen: %v", err)
	}
	if len(got) != 1 || got[0].EventID != "evt-persist-1" {
		t.Fatalf("rows after reopen = %+v; want the persisted event", got)
	}
	// Duplicate append across reopen still idempotent (PRIMARY KEY kept).
	if applied, err := s2.AppendEvent(ctx, eventAt("evt-persist-1", src, ts)); err != nil || applied {
		t.Fatalf("cross-reopen duplicate append = %v,%v; want false,nil", applied, err)
	}
	// State row survived.
	if status, _ := rawState(t, dbPath, "msg-persist-1"); status != "delivered" {
		t.Fatalf("state status after reopen = %q; want delivered", status)
	}
}

// TestStoreWALPosture pins the house SQLite posture (§4): the database
// file is in WAL journal mode and the store serializes writers
// (MaxOpenConns(1) + busy_timeout — observable as concurrent appends
// never erroring).
func TestStoreWALPosture(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wal.db")
	s, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	ts := time.Now().UTC().Truncate(time.Second)
	src := phone("0006")
	// 20 concurrent appends: with the 1-conn WAL convention all succeed
	// (busy_timeout set); any error here is posture drift.
	errCh := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func(i int) {
			_, err := s.AppendEvent(ctx, eventAt("wal-"+strconv.Itoa(i), src, ts.Add(time.Duration(i)*time.Millisecond)))
			errCh <- err
		}(i)
	}
	for i := 0; i < 20; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("concurrent AppendEvent: %v (WAL/busy_timeout posture broken)", err)
		}
	}
	got, err := s.RecentEvents(ctx, src, ts.Add(-time.Hour), 100)
	if err != nil {
		t.Fatalf("RecentEvents: %v", err)
	}
	if len(got) != 20 {
		t.Fatalf("rows = %d; want 20", len(got))
	}

	// journal_mode is a persistent file property: a fresh connection reads
	// "wal".
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer db.Close()
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q; want \"wal\" (house convention, callstore.go posture)", mode)
	}
}
