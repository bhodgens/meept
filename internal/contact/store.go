package contact

// SQLite contact store (contract C1; mirrors internal/voice/callstore.go:
// modernc driver, WAL + busy_timeout pragmas, single connection,
// RFC3339 TEXT timestamps, CREATE TABLE IF NOT EXISTS migration).
// Phone is E.164-normalized AT WRITE time and UNIQUE (upsert on phone:
// a second Put with the same phone overwrites the first, second wins).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/caimlas/meept/pkg/id"

	_ "modernc.org/sqlite" // sqlite driver registration (house convention)
)

// contactsSchema creates the contacts table. Times are RFC3339 TEXT
// (callstore.go convention). policy is the JSON array of Channel strings
// ([] when empty, so an empty policy round-trips as empty, never nil-
// injected). phone is UNIQUE: the upsert is the identity rule (one row
// per reachable phone number; SignalUUID may arrive later and updates
// the same row).
//
// voice_approved is NOT in the base schema (callstore.go outcome-column
// idiom): the guarded ALTER in migrateContacts adds it to fresh AND
// pre-existing databases alike, exercising the house migration path on
// every open. idx_contacts_phone is IF NOT EXISTS so re-runs on databases
// created before the index existed migrate it in place.
const contactsSchema = `
CREATE TABLE IF NOT EXISTS contacts (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL DEFAULT '',
    phone        TEXT NOT NULL UNIQUE,
    signal_uuid  TEXT NOT NULL DEFAULT '',
    policy       TEXT NOT NULL DEFAULT '[]',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_contacts_signal_uuid
    ON contacts(signal_uuid);

CREATE INDEX IF NOT EXISTS idx_contacts_phone
    ON contacts(phone);
`

// contactsVoiceApprovedAlter adds the inbound voice-action approval column
// (plan 20261110-caller-authz O2: unknown/unapproved = 0, the SQLite
// default for every pre-existing row).
const contactsVoiceApprovedAlter = `ALTER TABLE contacts ADD COLUMN voice_approved INTEGER NOT NULL DEFAULT 0`

// contactsMessagingOptOutAlter adds the messaging consent guard column
// (telnyx-texting plan D5: STOP reply sets it, all outbound sends
// consult it before dialing; default 0 = consented for pre-existing
// rows, the same fail-open-to-history posture as voice_approved).
const contactsMessagingOptOutAlter = `ALTER TABLE contacts ADD COLUMN messaging_opt_out INTEGER NOT NULL DEFAULT 0`

// migrateContacts runs the additive contacts migration: base schema (with
// phone index) + guarded ALTERs. Duplicate-column is the expected steady
// state on already-migrated databases and is tolerated (callstore.go
// migrationAddColumn idiom); anything else fails the open — silently
// missing the column would only surface later as query errors.
func migrateContacts(db *sql.DB) error {
	if _, err := db.Exec(contactsSchema); err != nil {
		return fmt.Errorf("contact store: migrate contacts: %w", err)
	}
	if _, err := db.Exec(contactsVoiceApprovedAlter); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("contact store: migrate voice_approved column: %w", err)
		}
	}
	if _, err := db.Exec(contactsMessagingOptOutAlter); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("contact store: migrate messaging_opt_out column: %w", err)
		}
	}
	return nil
}

// e164Pattern is the strict post-normalization validation (leaf contract:
// ^\+[1-9]\d{7,15}$). A phone that does not normalize into this shape is
// rejected at write with ErrInvalidPhone.
var e164Pattern = regexp.MustCompile(`^\+[1-9]\d{7,15}$`)

// ErrInvalidPhone rejects a Put whose phone does not normalize into strict
// E.164 shape. Empty is rejected the same way (phone is required, C1).
var ErrInvalidPhone = errors.New("contact: phone is not a valid E.164 number")

// Store is the C1 store interface. SQLiteStore implements it.
type Store interface {
	Put(ctx context.Context, rec Record) error
	Get(ctx context.Context, id string) (Record, error)
	GetByPhone(ctx context.Context, e164 string) (Record, error)
	GetBySignalUUID(ctx context.Context, uuid string) (Record, error)
	// LookupByPhone is the exact-match, miss-is-not-an-error reader
	// (caller-authz): the inbound resolver distinguishes known/unapproved
	// from unknown without sentinel-error plumbing.
	LookupByPhone(ctx context.Context, e164 string) (*Record, bool, error)
	List(ctx context.Context) ([]Record, error)
	Delete(ctx context.Context, id string) error
}

// SQLiteStore is the SQLite-backed contact store. A single connection
// (SetMaxOpenConns(1)) serializes writers in-process, the same convention
// as the voice call store / effects ledger. No package mutex is held
// across I/O (mutexio rule): the driver serializes, the upsert is a
// single INSERT statement.
type SQLiteStore struct {
	db     *sql.DB
	logger *slog.Logger
}

// compile-time proof the concrete store satisfies the contract interface.
var _ Store = (*SQLiteStore)(nil)

// NewSQLiteStore opens (and migrates) the contact store over an opened
// database handle (the contract C1 signature; the daemon wiring owns the
// sql.Open). Creates the contacts table when absent; additive only.
func NewSQLiteStore(db *sql.DB, log *slog.Logger) (*SQLiteStore, error) {
	if db == nil {
		return nil, errors.New("contact store: nil database handle")
	}
	if log == nil {
		log = slog.Default()
	}
	if err := migrateContacts(db); err != nil {
		return nil, err
	}
	return &SQLiteStore{db: db, logger: log}, nil
}

// NewStore opens the store at dbPath (the gate-test constructor: same
// open shape as voice.NewCallStore, pragmas included).
func NewStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("contact store: failed to open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	s, err := NewSQLiteStore(db, nil)
	if err != nil {
		if cerr := db.Close(); cerr != nil {
			slog.Default().Debug("contact store: close after migration failure", "err", cerr)
		}
		return nil, err
	}
	return s, nil
}

// NormalizeE164 normalizes a human-shaped phone input to E.164 (mirrors
// internal/voice NormalizeE164 leg-conditional rules, dial.go:693-732):
// strip whitespace and the punctuation separators; a leading + passes
// through; bare 10-digit is a US number (+1 prefix); bare 11-digit
// starting with the trunk digit keeps its +. Anything else returns the
// trimmed input (validation happens against the strict pattern at write).
func NormalizeE164(number string) string {
	trimmed := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '(', ')', '.':
			return -1
		}
		return r
	}, strings.TrimSpace(number))
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "+") {
		return trimmed
	}
	digitsOnly := true
	for _, r := range trimmed {
		if r < '0' || r > '9' {
			digitsOnly = false
			break
		}
	}
	if !digitsOnly {
		return trimmed
	}
	switch len(trimmed) {
	case 10: // US default: bare 10-digit is a US number
		return "+1" + trimmed
	case 11: // 1NPANXXXXX with the trunk digit
		return "+" + trimmed
	default:
		return trimmed
	}
}

// normalizeRecord fills store-managed fields at the write boundary: a
// missing id is generated (pkg/id.Generate, never a clock/rand), the
// phone is normalized, CreatedAt defaults to now, UpdatedAt always
// bumps. The strict E.164 gate runs on the NORMALIZED value.
func normalizeRecord(rec Record, now time.Time) (Record, error) {
	if rec.ID == "" {
		rec.ID = id.Generate("contact-")
	}
	rec.Phone = NormalizeE164(rec.Phone)
	if !e164Pattern.MatchString(rec.Phone) {
		return Record{}, fmt.Errorf("%w: %q", ErrInvalidPhone, rec.Phone)
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now
	return rec, nil
}

// Put upserts one record. Identity is the phone (UNIQUE upsert): a
// second Put with the same phone wins, preserving the FIRST row's
// created_at. An explicit non-empty CreatedAt is honored on first write
// so callers can seed historical records.
func (s *SQLiteStore) Put(ctx context.Context, rec Record) error {
	stored, err := normalizeRecord(rec, time.Now().UTC())
	if err != nil {
		return err
	}
	policy, err := marshalPolicy(stored.Policy)
	if err != nil {
		return err
	}
	// Upsert on phone: the phone UNIQUE constraint is the identity rule;
	// ON CONFLICT updates every mutable column and keeps created_at.
	// VoiceApproved and MessagingOptOut are second-wins like the other
	// mutable columns (the owner revokes approval / the consent glue
	// toggles opt-out by Puts the record with the flag set either way).
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO contacts (id, display_name, phone, signal_uuid, policy, voice_approved, messaging_opt_out, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(phone) DO UPDATE SET
		   id = excluded.id,
		   display_name = excluded.display_name,
		   signal_uuid = excluded.signal_uuid,
		   policy = excluded.policy,
		   voice_approved = excluded.voice_approved,
		   messaging_opt_out = excluded.messaging_opt_out,
		   updated_at = excluded.updated_at`,
		stored.ID, stored.DisplayName, stored.Phone, stored.SignalUUID, policy,
		boolToInt(stored.VoiceApproved),
		boolToInt(stored.MessagingOptOut),
		stored.CreatedAt.UTC().Format(time.RFC3339Nano),
		stored.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("contact store: put %s: %w", stored.ID, err)
	}
	return nil
}

// marshalPolicy encodes the ordered channel policy as JSON ([] when
// empty; never null, so an empty policy round-trips as empty).
func marshalPolicy(policy []Channel) (string, error) {
	if len(policy) == 0 {
		policy = []Channel{}
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		return "", fmt.Errorf("contact store: encode policy: %w", err)
	}
	return string(encoded), nil
}

// boolToInt maps the approval flag onto the SQLite INTEGER column
// (0/1 — the read side scans with an int and compares != 0).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// contactColumns is the shared SELECT list for every reader (scanRecord
// below is the matching scan order). One SSOT keeps Get / GetByPhone /
// GetBySignalUUID / List / LookupByPhone from drifting apart when the
// column set grows.
const contactColumns = `id, display_name, phone, signal_uuid, policy, voice_approved, messaging_opt_out, created_at, updated_at`

// scanRecord decodes one contacts row (column order = contactColumns).
func scanRecord(row interface{ Scan(...any) error }) (Record, error) {
	var (
		rec         Record
		policyRaw   string
		voiceApprov int
		optOut      int
		createdAt   string
		updatedAt   string
	)
	if err := row.Scan(&rec.ID, &rec.DisplayName, &rec.Phone, &rec.SignalUUID, &policyRaw, &voiceApprov, &optOut, &createdAt, &updatedAt); err != nil {
		return Record{}, err
	}
	rec.VoiceApproved = voiceApprov != 0
	rec.MessagingOptOut = optOut != 0
	if policyRaw != "" && policyRaw != "[]" {
		if err := json.Unmarshal([]byte(policyRaw), &rec.Policy); err != nil {
			return Record{}, fmt.Errorf("contact store: decode policy for %s: %w", rec.ID, err)
		}
	}
	var err error
	rec.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Record{}, fmt.Errorf("contact store: decode created_at for %s: %w", rec.ID, err)
	}
	rec.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return Record{}, fmt.Errorf("contact store: decode updated_at for %s: %w", rec.ID, err)
	}
	return rec, nil
}

// Get resolves one record by id. Absent is ErrUnknownContact.
func (s *SQLiteStore) Get(ctx context.Context, id string) (Record, error) {
	rec, err := scanRecord(s.db.QueryRowContext(ctx,
		`SELECT `+contactColumns+`
		 FROM contacts WHERE id = ?`, id,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, fmt.Errorf("%w: id %s", ErrUnknownContact, id)
	}
	if err != nil {
		return Record{}, fmt.Errorf("contact store: get %s: %w", id, err)
	}
	return rec, nil
}

// GetByPhone resolves by E.164 phone. The lookup normalizes the input so
// both the raw dial shape and the canonical form resolve.
func (s *SQLiteStore) GetByPhone(ctx context.Context, e164 string) (Record, error) {
	phone := NormalizeE164(e164)
	rec, err := scanRecord(s.db.QueryRowContext(ctx,
		`SELECT `+contactColumns+`
		 FROM contacts WHERE phone = ?`, phone,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, fmt.Errorf("%w: phone %s", ErrUnknownContact, phone)
	}
	if err != nil {
		return Record{}, fmt.Errorf("contact store: get by phone: %w", err)
	}
	return rec, nil
}

// LookupByPhone resolves by E.164 phone with an EXACT compare — no
// normalization (frozen contract, plan 20261110-caller-authz). This is
// deliberately distinct from GetByPhone: the inbound caller resolver feeds
// Telnyx's ev.From, already canonical, and a dial-shape input must MISS,
// never silently normalize into a different identity. Absent is NOT an
// error: the miss posture is (nil, false, nil) so the resolver fail-softs
// to "unapproved unknown caller" without propagating a lookup error
// upward (O1: the call always proceeds).
func (s *SQLiteStore) LookupByPhone(ctx context.Context, e164 string) (*Record, bool, error) {
	rec, err := scanRecord(s.db.QueryRowContext(ctx,
		`SELECT `+contactColumns+`
		 FROM contacts WHERE phone = ?`, e164,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("contact store: lookup by phone: %w", err)
	}
	return &rec, true, nil
}

// GetBySignalUUID resolves by Signal UUID (empty uuid is unknown: the
// column defaults ” and an empty lookup would match unbound rows).
func (s *SQLiteStore) GetBySignalUUID(ctx context.Context, uuid string) (Record, error) {
	if uuid == "" {
		return Record{}, fmt.Errorf("%w: empty signal uuid", ErrUnknownContact)
	}
	rec, err := scanRecord(s.db.QueryRowContext(ctx,
		`SELECT `+contactColumns+`
		 FROM contacts WHERE signal_uuid = ?`, uuid,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, fmt.Errorf("%w: signal uuid %s", ErrUnknownContact, uuid)
	}
	if err != nil {
		return Record{}, fmt.Errorf("contact store: get by signal uuid: %w", err)
	}
	return rec, nil
}

// List returns every record ordered by display name (stable presentation
// order for the tool/report layers).
func (s *SQLiteStore) List(ctx context.Context) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+contactColumns+`
		 FROM contacts ORDER BY display_name, id`,
	)
	if err != nil {
		return nil, fmt.Errorf("contact store: list: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable in a read-only iteration
	var all []Record
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("contact store: list iterate: %w", err)
	}
	return all, nil
}

// Delete removes one record by id. Absent is ErrUnknownContact (never a
// silent success, C1).
func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM contacts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("contact store: delete %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("contact store: delete %s: rows affected: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: id %s", ErrUnknownContact, id)
	}
	return nil
}

// Close closes the database handle.
func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
