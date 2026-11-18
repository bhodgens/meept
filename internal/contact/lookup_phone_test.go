package contact

// 00T §2 gate tests (caller-authz plan, branch 01-identity): the frozen
// contact-side contracts (docs/plans/20261110-caller-authz/master.md):
//   - Record gains VoiceApproved bool (O2: contacts.db is the system of
//     record for approved callers; default FALSE = unapproved).
//   - store migrate adds the guarded ALTER
//     ALTER TABLE contacts ADD COLUMN voice_approved INTEGER NOT NULL DEFAULT 0
//     (duplicate-column tolerated, callstore.go idiom) plus
//     CREATE INDEX IF NOT EXISTS idx_contacts_phone ON contacts(phone).
//   - new method LookupByPhone(ctx, e164) (*Record, bool, error) -
//     EXACT match (no normalization; GetByPhone keeps the normalizing
//     lookup), parameterized SQL.
//
// RED style: undefined symbols (Record.VoiceApproved / LookupByPhone)
// until leaf 01 lands them; the existing suite stays untouched.

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

// gatePhone builds a valid test E.164 number (+1555 NNNNNNN) from a
// small int. Helper instead of full literals: agent tooling redacts
// long digit runs, which silently corrupts phone fixtures.
func gatePhone(n int) string { return fmt.Sprintf("+1555%07d", n) }

// TestLookupByPhoneFound pins the happy path: exact E.164 hit returns
// (*Record, true, nil) with every field - VoiceApproved included -
// round-tripping through the store.
func TestLookupByPhoneFound(t *testing.T) {
	ctx := context.Background()
	s, err := NewStore(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	in := Record{
		ID:            "c-ada",
		DisplayName:   "Ada",
		Phone:         gatePhone(1),
		Policy:        []Channel{ChannelTelnyxVoice},
		VoiceApproved: true,
	}
	if err := s.Put(ctx, in); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, found, err := s.LookupByPhone(ctx, gatePhone(1))
	if err != nil {
		t.Fatalf("LookupByPhone: %v", err)
	}
	if !found {
		t.Fatalf("LookupByPhone found=false, want true for an exact stored E.164")
	}
	if got == nil {
		t.Fatal("LookupByPhone returned nil *Record with found=true")
	}
	if got.ID != in.ID || got.DisplayName != in.DisplayName || got.Phone != in.Phone {
		t.Errorf("identity fields round-trip wrong: %+v, want %+v", *got, in)
	}
	if !got.VoiceApproved {
		t.Errorf("VoiceApproved = false, want true (approved-caller round trip)")
	}
}

// TestLookupByPhoneNotFound pins the miss posture: (nil, false, nil).
// Unknown is NOT an error - the daemon resolver fail-softs to
// unapproved, it never propagates a lookup error upward.
func TestLookupByPhoneNotFound(t *testing.T) {
	ctx := context.Background()
	s, err := NewStore(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	got, found, err := s.LookupByPhone(ctx, gatePhone(9999))
	if err != nil {
		t.Fatalf("LookupByPhone on a miss returned an error: %v (miss must be found=false, err=nil)", err)
	}
	if found {
		t.Fatalf("LookupByPhone found=true for an absent phone, record %+v", got)
	}
	if got != nil {
		t.Errorf("LookupByPhone record = %+v on a miss, want nil", *got)
	}
}

// TestLookupByPhoneIsExact pins the frozen "exact match" wording: unlike
// GetByPhone (which normalizes the input), LookupByPhone compares the
// E.164 it is handed verbatim. The daemon feeds Telnyx's ev.From, which
// is already canonical - a dial-shape input must MISS, not silently
// normalize.
func TestLookupByPhoneIsExact(t *testing.T) {
	ctx := context.Background()
	s, err := NewStore(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Put(ctx, Record{ID: "c-bo", DisplayName: "Bo", Phone: gatePhone(2)}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// The dial shape below normalizes to gatePhone(2) - GetByPhone hits,
	// LookupByPhone must miss (exact compare, zero normalization).
	dialShape := "555 000 0002"
	if _, found, err := s.LookupByPhone(ctx, dialShape); err != nil || found {
		t.Errorf("LookupByPhone(dial-shape) = found %v err %v, want false nil (exact match, no normalization)", found, err)
	}
	if _, err := s.GetByPhone(ctx, dialShape); err != nil {
		t.Errorf("GetByPhone normalize-on-read regressed: %v", err)
	}
}

// TestNewStoreMigratesVoiceApprovedColumnAndIndex pins the schema
// contract on a FRESH db: voice_approved exists as
// INTEGER NOT NULL DEFAULT 0, and idx_contacts_phone exists. The probe
// runs on an INDEPENDENT handle and fully drains every rows iteration
// before any second query (the store pool is MaxOpenConns(1) - a
// half-iterated cursor starves every later acquisition).
func TestNewStoreMigratesVoiceApprovedColumnAndIndex(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "contacts.db")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	probe, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open probe handle: %v", err)
	}
	defer probe.Close() //nolint:errcheck // read-only probe handle

	rows, err := probe.Query(`PRAGMA table_info(contacts)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()
	var (
		sawVoiceApproved bool
		vaType           string
		vaNotNull        int
		vaDefault        sql.NullString
	)
	for rows.Next() {
		var (
			cid     int
			name    string
			typ     string
			notnull int
			dflt    sql.NullString
			pk      sql.NullInt64
		)
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		if name == "voice_approved" {
			sawVoiceApproved = true
			vaType, vaNotNull, vaDefault = typ, notnull, dflt
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table_info: %v", err)
	}
	if !sawVoiceApproved {
		t.Fatal("fresh store has no voice_approved column (guarded ALTER migration missing)")
	}
	if vaType != "INTEGER" {
		t.Errorf("voice_approved type = %q, want INTEGER (master contract: INTEGER NOT NULL DEFAULT 0)", vaType)
	}
	if vaNotNull != 1 {
		t.Errorf("voice_approved NOT NULL = %d, want 1 (master contract: NOT NULL DEFAULT 0)", vaNotNull)
	}
	if !vaDefault.Valid || vaDefault.String != "0" {
		t.Errorf("voice_approved default = %v, want \"0\" (unknown number = unapproved, O2)", vaDefault)
	}

	var idxCount int
	if err := probe.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_contacts_phone'`,
	).Scan(&idxCount); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if idxCount != 1 {
		t.Errorf("idx_contacts_phone present = %d rows, want 1 (CREATE INDEX IF NOT EXISTS missing)", idxCount)
	}
}

// TestReopenSameStoreToleratesDuplicateAlter pins the guarded-ALTER
// idiom's steady state: opening (and migrating) the SAME database file
// twice must not fail on the duplicate-column error, and existing rows
// survive the re-run unchanged - a migration that wipes is a review
// checklist violation (No migration wipes data).
func TestReopenSameStoreToleratesDuplicateAlter(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "contacts.db")

	s1, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore (first open): %v", err)
	}
	in := Record{ID: "c-cy", DisplayName: "Cy", Phone: gatePhone(3), VoiceApproved: true}
	if err := s1.Put(ctx, in); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close 1: %v", err)
	}

	s2, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore (migrate re-run on an already-migrated db): %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })

	got, found, err := s2.LookupByPhone(ctx, gatePhone(3))
	if err != nil || !found || got == nil {
		t.Fatalf("re-opened store lost the row: found %v err %v", found, err)
	}
	if got.DisplayName != "Cy" || !got.VoiceApproved {
		t.Errorf("row mutated across the migrate re-run: %+v", *got)
	}
}

// TestVoiceApprovedRoundTripsThroughAllReaders pins that VoiceApproved
// is a real column, not a Put-time field that readers drop: Get,
// GetByPhone and List all carry it, a default Put reads back false
// (unknown/unapproved = FALSE per O2), and the phone-identity upsert
// overwrites it second-wins like every other mutable column.
func TestVoiceApprovedRoundTripsThroughAllReaders(t *testing.T) {
	ctx := context.Background()
	s, err := NewStore(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	approved := Record{ID: "c-dee", DisplayName: "Dee", Phone: gatePhone(4), VoiceApproved: true}
	if err := s.Put(ctx, approved); err != nil {
		t.Fatalf("Put approved: %v", err)
	}
	plain := Record{ID: "c-ed", DisplayName: "Ed", Phone: gatePhone(5)}
	if err := s.Put(ctx, plain); err != nil {
		t.Fatalf("Put default: %v", err)
	}

	byID, err := s.Get(ctx, approved.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !byID.VoiceApproved {
		t.Errorf("Get lost VoiceApproved: %+v", byID)
	}
	byPhone, err := s.GetByPhone(ctx, plain.Phone)
	if err != nil {
		t.Fatalf("GetByPhone: %v", err)
	}
	if byPhone.VoiceApproved {
		t.Errorf("default Put reads approved=true, want false (O2: unknown = unapproved)")
	}

	all, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List = %d rows, want 2", len(all))
	}
	flags := map[string]bool{}
	for _, r := range all {
		flags[r.ID] = r.VoiceApproved
	}
	if !flags[approved.ID] || flags[plain.ID] {
		t.Errorf("List VoiceApproved wrong: %v", flags)
	}

	// Upsert second-wins on the flag: rewriting with VoiceApproved=false
	// clears approval (owner removes an approver via the next Put).
	revoked := approved
	revoked.VoiceApproved = false
	if err := s.Put(ctx, revoked); err != nil {
		t.Fatalf("Put revoked: %v", err)
	}
	after, err := s.Get(ctx, revoked.ID)
	if err != nil {
		t.Fatalf("Get after revoke: %v", err)
	}
	if after.VoiceApproved {
		t.Error("second Put did not overwrite VoiceApproved (upsert must keep it second-wins)")
	}
}
