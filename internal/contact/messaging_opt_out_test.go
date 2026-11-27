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
	defer rows.Close()
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

// TestSetMessagingOptOutIsColumnScoped pins the #158 fix: the consent
// toggle writes ONLY messaging_opt_out (+ updated_at). Two-writer
// posture:
//  1. SetMessagingOptOut(true) after an owner display_name edit keeps
//     the edit (the toggle never round-trips a whole record), and bumps
//     updated_at.
//  2. SetMessagingOptOut(false) clears the flag the same way.
//  3. A stale full-record Put of a pre-toggle copy DOES revert the flag
//     — documented second-wins upsert behavior (contact/store.go Put),
//     pinned here so the semantics stay explicit; the consent path no
//     longer rides that path itself.
//  4. Unknown phone is ErrUnknownContact (no ghost row), invalid phone
//     is ErrInvalidPhone.
func TestSetMessagingOptOutIsColumnScoped(t *testing.T) {
	ctx := context.Background()
	s, err := NewStore(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Put(ctx, Record{ID: "c-atomic", DisplayName: "Original", Phone: optOutPhone(21)}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Stale pre-toggle copy captured BEFORE any consent write (the
	// second writer in the #158 interleaving).
	stale, err := s.GetByPhone(ctx, optOutPhone(21))
	if err != nil {
		t.Fatalf("GetByPhone: %v", err)
	}
	staleBefore := stale
	stale.UpdatedAt = staleBefore.CreatedAt // a caller-provided stale timestamp

	// Owner edits another column through the full-record path.
	stale.DisplayName = "Owner edit"
	if err := s.Put(ctx, stale); err != nil {
		t.Fatalf("owner Put (display_name edit): %v", err)
	}
	before, err := s.Get(ctx, "c-atomic")
	if err != nil {
		t.Fatalf("Get before toggle: %v", err)
	}
	if before.UpdatedAt.Before(staleBefore.CreatedAt) {
		t.Fatalf("owner edit did not bump updated_at: %+v", before)
	}

	// (1) Toggle SET: the consent flag flips, every other column —
	// including the owner's display_name edit — survives untouched.
	if err := s.SetMessagingOptOut(ctx, optOutPhone(21), true); err != nil {
		t.Fatalf("SetMessagingOptOut(true): %v", err)
	}
	after, err := s.Get(ctx, "c-atomic")
	if err != nil {
		t.Fatalf("Get after toggle: %v", err)
	}
	if !after.MessagingOptOut {
		t.Fatal("SetMessagingOptOut(true) did not set the flag")
	}
	if after.DisplayName != "Owner edit" {
		t.Fatalf("toggle clobbered display_name = %q, want %q (column-scoped write must not round-trip the record)",
			after.DisplayName, "Owner edit")
	}
	if after.ID != before.ID || !after.CreatedAt.Equal(before.CreatedAt) || len(after.Policy) != len(before.Policy) ||
		after.SignalUUID != before.SignalUUID || after.VoiceApproved != before.VoiceApproved {
		t.Fatalf("toggle changed non-consent columns: before %+v after %+v", before, after)
	}
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("updated_at = %v after toggle, want later than %v (toggle must bump it)", after.UpdatedAt, before.UpdatedAt)
	}

	// (2) Toggle CLEAR: same column-scoped behavior on the START leg.
	if err := s.SetMessagingOptOut(ctx, "5550000021", false); err != nil {
		t.Fatalf("SetMessagingOptOut(false) (dial-shape, must normalize): %v", err)
	}
	cleared, err := s.Get(ctx, "c-atomic")
	if err != nil {
		t.Fatalf("Get after clear: %v", err)
	}
	if cleared.MessagingOptOut {
		t.Fatal("SetMessagingOptOut(false) did not clear the flag")
	}
	if cleared.DisplayName != "Owner edit" {
		t.Fatalf("clear toggle clobbered display_name = %q", cleared.DisplayName)
	}

	// (3) Documented second-wins: a stale full-record Put with
	// MessagingOptOut=false still reverts a set flag — the PUT is
	// unchanged by #158; only the consent PATH stopped round-tripping.
	if err := s.SetMessagingOptOut(ctx, optOutPhone(21), true); err != nil {
		t.Fatalf("SetMessagingOptOut(true) again: %v", err)
	}
	staleCopy := staleBefore // MessagingOptOut=false, pre-toggle snapshot
	if err := s.Put(ctx, staleCopy); err != nil {
		t.Fatalf("stale Put: %v", err)
	}
	reverted, err := s.Get(ctx, "c-atomic")
	if err != nil {
		t.Fatalf("Get after stale Put: %v", err)
	}
	if reverted.MessagingOptOut {
		t.Fatal("stale full-record Put did NOT revert the flag: Put's all-column ON CONFLICT write is broken")
	}

	// (4) Posture pins: unknown phone and invalid phone.
	if err := s.SetMessagingOptOut(ctx, optOutPhone(99), true); !errors.Is(err, ErrUnknownContact) {
		t.Fatalf("SetMessagingOptOut unknown phone err = %v, want ErrUnknownContact", err)
	}
	if _, found, _ := s.LookupByPhone(ctx, optOutPhone(99)); found {
		t.Fatal("SetMessagingOptOut created a ghost row for an unknown phone")
	}
	if err := s.SetMessagingOptOut(ctx, "not-a-phone", true); !errors.Is(err, ErrInvalidPhone) {
		t.Fatalf("SetMessagingOptOut invalid phone err = %v, want ErrInvalidPhone", err)
	}
}

// TestStoreInterfaceHasSetMessagingOptOut is a compile-time pin: the
// contact.Store contract carries the #158 toggle, so any alternate
// implementation must provide the column-scoped path too.
var _ interface {
	SetMessagingOptOut(ctx context.Context, phone string, opted bool) error
} = (Store)(nil)

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
