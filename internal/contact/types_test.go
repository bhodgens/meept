package contact

// Gate tests for contract C1 (master.md, quoted verbatim): the Record
// shape and the Store interface. Written BEFORE implementation
// (00T-gate-tests.md, T1). RED until leaf 01 lands types.go + store.go.
//
// Pinned signatures (do not weaken):
//
//	type Channel string // "telnyx_voice" | "signal_voice" | "signal_msg" | "telnyx_sms"
//	type Record struct{ ID, DisplayName, Phone, SignalUUID string;
//	                   Policy []Channel; CreatedAt, UpdatedAt time.Time }
//	type Store interface {
//	    Put(ctx, Record) error
//	    Get(ctx, id) (Record, error)            // ErrUnknownContact when absent
//	    GetByPhone(ctx, e164) (Record, error)
//	    GetBySignalUUID(ctx, uuid) (Record, error)
//	    List(ctx) ([]Record, error)
//	    Delete(ctx, id) error
//	}
//
// Store is SQLite-backed (pattern: internal/voice/callstore.go); schema
// migration table `contacts`. Phone is E.164-normalized AT WRITE time.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// mustRecord builds a Record with a phone that NEEDS normalization, so
// the E.164-at-write rule is exercised on every test that stores it.
func mustRecord(name, rawPhone string) Record {
	return Record{
		ID:          "c-" + name,
		DisplayName: name,
		Phone:       rawPhone, // intentionally NOT normalized
		Policy:      []Channel{ChannelTelnyxVoice, ChannelSignalVoice, ChannelSignalMsg, ChannelTelnyxSMS},
	}
}

// TestChannelConstants pins the C1 channel vocabulary byte-exactly: the
// chain policy and the telnyx/signal adapters all key on these strings.
func TestChannelConstants(t *testing.T) {
	want := map[Channel]string{
		ChannelTelnyxVoice: "telnyx_voice",
		ChannelSignalVoice: "signal_voice",
		ChannelSignalMsg:   "signal_msg",
		ChannelTelnyxSMS:   "telnyx_sms",
	}
	for ch, s := range want {
		if string(ch) != s {
			t.Errorf("Channel constant = %q, want %q", string(ch), s)
		}
	}
}

// TestRecordShape pins the C1 Record fields (json/wire consumers and the
// tool arg resolver read these names).
func TestRecordShape(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	rec := Record{
		ID:          "rec-1",
		DisplayName: "Ada",
		Phone:       "+15550000001",
		SignalUUID:  "11111111-2222-3333-4444-555555555555",
		Policy:      []Channel{ChannelSignalMsg},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if rec.ID != "rec-1" || rec.DisplayName != "Ada" || rec.Phone != "+15550000001" ||
		rec.SignalUUID != "11111111-2222-3333-4444-555555555555" ||
		len(rec.Policy) != 1 || rec.Policy[0] != ChannelSignalMsg ||
		!rec.CreatedAt.Equal(now) || !rec.UpdatedAt.Equal(now) {
		t.Errorf("Record field shape drifted from contract C1: %+v", rec)
	}
}

// TestStorePutGetByPhoneNormalized pins F1: Put stores under the E.164
// normalization of the given phone, and GetByPhone resolves by BOTH the
// raw dial shape and the canonical E.164 form (the leaf's Given/When/Then:
// Put with a padded phone; GetByPhone returns it normalized).
func TestStorePutGetByPhoneNormalized(t *testing.T) {
	ctx := context.Background()
	s, err := NewStore(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	in := mustRecord("ada", " 1 (555) 000-0001 ") // noisy human-shaped input
	if err := s.Put(ctx, in); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Canonical lookup (normalized).
	got, err := s.GetByPhone(ctx, "+15550000001")
	if err != nil {
		t.Fatalf("GetByPhone canonical: %v", err)
	}
	if got.Phone != "+15550000001" {
		t.Errorf("stored Phone = %q, want E.164-normalized %q (normalize AT WRITE, C1)", got.Phone, "+15550000001")
	}
	if got.ID != in.ID || got.DisplayName != "Ada" {
		t.Errorf("GetByPhone record = %+v, want the stored record body", got)
	}
	if len(got.Policy) != 4 || got.Policy[0] != ChannelTelnyxVoice {
		t.Errorf("Policy round-trip = %v, want the stored ordered chain", got.Policy)
	}
}

// TestStorePutEmptyPhoneRejected pins the C1 rule "Phone: E.164, required".
func TestStorePutEmptyPhoneRejected(t *testing.T) {
	ctx := context.Background()
	s, err := NewStore(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	rec := mustRecord("nope", "")
	rec.Phone = ""
	if err := s.Put(ctx, rec); err == nil {
		t.Errorf("Put with empty Phone = nil error, want rejection (C1: phone required)")
	}
}

// TestStoreGetSignalUUID pins F1's second lookup: after SignalUUID is set
// (observable from an envelope or config), GetBySignalUUID resolves the
// same record.
func TestStoreGetSignalUUID(t *testing.T) {
	ctx := context.Background()
	s, err := NewStore(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	in := mustRecord("bob", "+15550000002")
	if err := s.Put(ctx, in); err != nil {
		t.Fatalf("Put: %v", err)
	}

	const uuid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	// Settable later: fetch, set, put back.
	got, err := s.Get(ctx, in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got.SignalUUID = uuid
	if err := s.Put(ctx, got); err != nil {
		t.Fatalf("Put (uuid set): %v", err)
	}

	byUUID, err := s.GetBySignalUUID(ctx, uuid)
	if err != nil {
		t.Fatalf("GetBySignalUUID: %v", err)
	}
	if byUUID.ID != in.ID {
		t.Errorf("GetBySignalUUID = %+v, want record %s", byUUID, in.ID)
	}
}

// TestStoreGetUnknown pins the named sentinel: an absent id is
// ErrUnknownContact (errors.Is-checkable), never a bare error.
func TestStoreGetUnknown(t *testing.T) {
	ctx := context.Background()
	s, err := NewStore(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if _, err := s.Get(ctx, "no-such-id"); !errors.Is(err, ErrUnknownContact) {
		t.Errorf("Get(unknown) = %v, want ErrUnknownContact", err)
	}
}

// TestStoreListDelete pins the remaining Store surface: List returns
// everything, Delete removes, and the deleted id is then unknown.
func TestStoreListDelete(t *testing.T) {
	ctx := context.Background()
	s, err := NewStore(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for _, r := range []Record{mustRecord("a", "+15550000003"), mustRecord("b", "+15550000004")} {
		if err := s.Put(ctx, r); err != nil {
			t.Fatalf("Put %s: %v", r.ID, err)
		}
	}
	all, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List = %d records, want 2", len(all))
	}
	if err := s.Delete(ctx, "c-a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, "c-a"); !errors.Is(err, ErrUnknownContact) {
		t.Errorf("Get after Delete = %v, want ErrUnknownContact", err)
	}
}

// TestStoreDurability pins the SQLite-backed rule (C1 note): a second
// Store over the same file sees the records (callstore.go pattern).
func TestStoreDurability(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "contacts.db")
	s, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Put(ctx, mustRecord("carol", "+15550000005")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	s2, err := NewStore(db)
	if err != nil {
		t.Fatalf("reopen NewStore: %v", err)
	}
	got, err := s2.GetByPhone(ctx, "+15550000005")
	if err != nil {
		t.Fatalf("reopened GetByPhone: %v", err)
	}
	if got.DisplayName != "carol" {
		t.Errorf("reopened record = %+v, want carol", got)
	}
}
