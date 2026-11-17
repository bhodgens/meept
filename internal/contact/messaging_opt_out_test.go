package contact

// New tests for leaf 02-send-leg/02 (telnyx-texting D5): the contacts
// messaging_opt_out guard column — Put/Get roundtrip, reopen tolerance
// (guarded ALTER is duplicate-tolerant; a pre-migration database reopens
// and gains the column), and the chain posture for a refused attempt.
// These live in a NEW file; existing contact tests are untouched.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func optOutPhone(n int) string { return fmt.Sprintf("+1555%07d", n) }

// TestMessagingOptOutRoundtrip pins the column contract: Put with
// MessagingOptOut true/false round-trips through Get, GetByPhone,
// LookupByPhone, and List; a fresh record defaults to false.
func TestMessagingOptOutRoundtrip(t *testing.T) {
	ctx := context.Background()
	s, err := NewStore(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// Default: a record Put without the flag reads back opted-IN.
	if err := s.Put(ctx, Record{ID: "c-in", DisplayName: "Consented", Phone: optOutPhone(1)}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(ctx, "c-in")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.MessagingOptOut {
		t.Fatal("fresh record reads MessagingOptOut=true, want false (column default 0)")
	}

	// Opted-out write, then read through EVERY shared-column reader.
	rec := got
	rec.MessagingOptOut = true
	if err := s.Put(ctx, rec); err != nil {
		t.Fatalf("Put(opt-out): %v", err)
	}
	if r, err := s.Get(ctx, "c-in"); err != nil || !r.MessagingOptOut {
		t.Fatalf("Get = %+v err %v, want MessagingOptOut=true nil", r, err)
	}
	if r, err := s.GetByPhone(ctx, optOutPhone(1)); err != nil || !r.MessagingOptOut {
		t.Fatalf("GetByPhone = %+v err %v, want MessagingOptOut=true nil", r, err)
	}
	r, found, err := s.LookupByPhone(ctx, optOutPhone(1))
	if err != nil || !found || r == nil || !r.MessagingOptOut {
		t.Fatalf("LookupByPhone = found %v rec %+v err %v, want true + opted-out", found, r, err)
	}
	all, err := s.List(ctx)
	if err != nil || len(all) != 1 || !all[0].MessagingOptOut {
		t.Fatalf("List = %d records err %v, want 1 opted-out record", len(all), err)
	}

	// Re-open (consent flip back to opted-in is a normal Put — the
	// START reply path; second-wins like voice_approved).
	rec.MessagingOptOut = false
	if err := s.Put(ctx, rec); err != nil {
		t.Fatalf("Put(re-open consent): %v", err)
	}
	if r, err := s.Get(ctx, "c-in"); err != nil || r.MessagingOptOut {
		t.Fatalf("after re-put: %+v err %v, want MessagingOptOut=false", r, err)
	}
}

// TestNewStoreMigratesMessagingOptOutColumn pins the schema contract on
// a FRESH db: messaging_opt_out exists as INTEGER NOT NULL DEFAULT 0
// (master §7 column contract), independent of voice_approved.
func TestNewStoreMigrationsMessagingOptOutColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "contacts.db")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	_ = s.Close()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen probe: %v", err)
	}
	defer db.Close() //nolint:errcheck // probe handle close error is not actionable
	rows, err := db.Query(`SELECT name, type, "notnull", dflt_value FROM pragma_table_info('contacts')`)
	if err != nil {
		t.Fatalf("pragma_table_info: %v", err)
	}
	found := false
	for rows.Next() {
		var name, typ string
		var notnull int
		var dflt sql.NullString
		if err := rows.Scan(&name, &typ, &notnull, &dflt); err != nil {
			t.Fatalf("scan pragma row: %v", err)
		}
		if name != "messaging_opt_out" {
			continue
		}
		found = true
		if typ != "INTEGER" {
			t.Errorf("messaging_opt_out type = %q, want INTEGER", typ)
		}
		if notnull != 1 {
			t.Errorf("messaging_opt_out NOT NULL = %d, want 1", notnull)
		}
		if dflt.String != "0" {
			t.Errorf("messaging_opt_out default = %q, want \"0\"", dflt.String)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate pragma rows: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close pragma rows: %v", err)
	}
	if !found {
		t.Fatal("fresh store has no messaging_opt_out column (guarded ALTER migration missing)")
	}
}

// TestReopenPreMigrationStoreTolerant pins reopen tolerance BOTH ways:
//  1. a database created by the BASE schema (no guard columns) reopens
//     through NewStore, which ALTERs both columns in;
//  2. reopening an already-migrated database is duplicate-tolerant (the
//     guarded ALTER swallows "duplicate column name") and preserves the
//     stored opt-out value.
func TestReopenPreMigrationStoreTolerant(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy.db")

	// (1) Create the legacy shape by hand: base contacts table WITHOUT
	// voice_approved or messaging_opt_out, one pre-existing row.
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec(`CREATE TABLE contacts (
		id TEXT PRIMARY KEY,
		display_name TEXT NOT NULL DEFAULT '',
		phone TEXT NOT NULL UNIQUE,
		signal_uuid TEXT NOT NULL DEFAULT '',
		policy TEXT NOT NULL DEFAULT '[]',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("legacy schema: %v", err)
	}
	now := "2026-11-10T00:00:00Z"
	if _, err := raw.Exec(`INSERT INTO contacts (id, phone, created_at, updated_at) VALUES ('legacy-1', ?, ?, ?)`, optOutPhone(3), now, now); err != nil {
		t.Fatalf("legacy insert: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("raw close: %v", err)
	}

	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore over legacy db: %v", err)
	}
	rec, err := s.Get(ctx, "legacy-1")
	if err != nil {
		t.Fatalf("Get legacy row after migration: %v", err)
	}
	if rec.MessagingOptOut || rec.VoiceApproved {
		t.Fatalf("legacy row defaults = opt-out %v approved %v, want false false (ALTER default 0)", rec.MessagingOptOut, rec.VoiceApproved)
	}
	// (2) Set the flag, close, reopen: duplicate-tolerant ALTER + value.
	rec.MessagingOptOut = true
	if err := s.Put(ctx, rec); err != nil {
		t.Fatalf("Put opt-out: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s2, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("reopen migrated db (duplicate ALTER must be tolerated): %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	reread, err := s2.GetByPhone(ctx, optOutPhone(3))
	if err != nil || !reread.MessagingOptOut {
		t.Fatalf("reread after reopen = %+v err %v, want MessagingOptOut=true", reread, err)
	}
}

// TestErrOptedOutIsCheckable pins the sentinel surface: errors.Is
// resolves through %w wrapping at channel call sites.
func TestErrOptedOutIsCheckable(t *testing.T) {
	wrapped := fmt.Errorf("some channel refused: %w", ErrOptedOut)
	if !errors.Is(wrapped, ErrOptedOut) {
		t.Fatalf("errors.Is(%v, ErrOptedOut) = false, want true", wrapped)
	}
	if errors.Is(wrapped, ErrUnknownContact) {
		t.Fatal("ErrOptedOut must not alias ErrUnknownContact")
	}
}

// optedOutAttempt is a stub adapter that refuses like the migrated
// telnyx_sms channel: (OutcomeUnavailable, ErrOptedOut-wrapped) with
// zero sends.
type optedOutAttempt struct{ channel Channel }

func (a optedOutAttempt) Channel() Channel { return a.channel }
func (a optedOutAttempt) Attempt(context.Context, Record, string) (string, error) {
	return OutcomeUnavailable, fmt.Errorf("test channel: opted out: %w", ErrOptedOut)
}

// countingAttempt records the next channel so the test can prove the
// chain ADVANCED past the opted-out messaging leg.
type countingAttempt struct {
	channel Channel
	calls   int
}

func (a *countingAttempt) Channel() Channel { return a.channel }
func (a *countingAttempt) Attempt(context.Context, Record, string) (string, error) {
	a.calls++
	return OutcomeDelivered, nil
}

// TestChainFallsThroughOptedOutMessenger pins how the chain consumes the
// refusal: the opted-out messaging leg records an unavailable attempt
// (terminal for THAT channel, never retried — the refusal is
// deterministic consent state) and the chain advances to the next
// policy channel, which is allowed to succeed.
func TestChainFallsThroughOptedOutMessenger(t *testing.T) {
	ctx := context.Background()
	sms := optedOutAttempt{channel: ChannelTelnyxSMS}
	sig := &countingAttempt{channel: ChannelSignalMsg}
	runner := NewChainRunner(
		map[Channel]ChannelAttempt{ChannelTelnyxSMS: sms, ChannelSignalMsg: sig},
		nil,
	)
	rec := Record{ID: "c-1", Phone: optOutPhone(9), MessagingOptOut: true,
		Policy: []Channel{ChannelTelnyxSMS, ChannelSignalMsg}}
	rep := runner.Run(ctx, rec, "ping", nil)
	if len(rep.Attempts) != 2 {
		t.Fatalf("attempts = %d, want 2 (messaging refused, signal delivered)", len(rep.Attempts))
	}
	first := rep.Attempts[0]
	if first.Channel != ChannelTelnyxSMS || first.Outcome != OutcomeUnavailable {
		t.Fatalf("messaging row = %+v, want telnyx_sms/unavailable", first)
	}
	if !strings.Contains(first.Err, "opt") {
		t.Fatalf("messaging err = %q, want it to carry the opt-out reason", first.Err)
	}
	if sig.calls != 1 {
		t.Fatalf("next-channel attempts = %d, want 1 (chain advanced past the refusal)", sig.calls)
	}
	if rep.Attempts[1].Outcome != OutcomeDelivered {
		t.Fatalf("signal row = %+v, want delivered", rep.Attempts[1])
	}
}
