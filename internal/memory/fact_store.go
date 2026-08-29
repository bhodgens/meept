// Typed user-memory facts (harness-eval leaf 12, book ch3 "user memory"):
// a compact, typed model of the user that survives sessions. Facts are
// distinct from episodic transcripts and the personality profile: they
// carry a kind (preference/restriction/account/temporal), a conflict
// rule (last-write-wins with history via ValidUntil), and temporal
// validity windows. No new vector engine — SQLite beside the existing
// stores.
package memory

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" //nolint:revive // blank import for side effects

	"github.com/caimlas/meept/pkg/id"
)

// FactKind classifies a MemoryFact.
type FactKind string

// Supported fact kinds.
const (
	// FactPreference is a durable user preference ("prefers window seats").
	FactPreference FactKind = "preference"
	// FactRestriction is a constraint the agent must respect
	// ("vegetarian", "allergic to peanuts").
	FactRestriction FactKind = "restriction"
	// FactAccount is a credential-adjacent identifier fragment
	// ("United MileagePlus 12345678"). Never store raw secrets here.
	FactAccount FactKind = "account"
	// FactTemporal is a date-bound fact ("travels to Tokyo next Friday").
	FactTemporal FactKind = "temporal"
)

// MemoryFact is one typed, time-scoped fact about a user.
type MemoryFact struct {
	ID            string     `json:"id"`
	OwnerID       string     `json:"owner_id"` // empty = daemon owner (multiuser off)
	Kind          FactKind   `json:"kind"`
	Key           string     `json:"key"`
	Value         string     `json:"value"`
	ValidFrom     *time.Time `json:"valid_from,omitempty"`
	ValidUntil    *time.Time `json:"valid_until,omitempty"`
	SourceSession string     `json:"source_session,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// factSchema is the memory_facts table DDL. Parameterized queries only.
const factSchema = `
CREATE TABLE IF NOT EXISTS memory_facts (
	id             TEXT PRIMARY KEY,
	owner_id       TEXT NOT NULL DEFAULT '',
	kind           TEXT NOT NULL,
	key            TEXT NOT NULL,
	value          TEXT NOT NULL,
	valid_from     DATETIME,
	valid_until    DATETIME,
	source_session TEXT NOT NULL DEFAULT '',
	updated_at     DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_memory_facts_owner_kind_key
	ON memory_facts (owner_id, kind, key);
CREATE INDEX IF NOT EXISTS idx_memory_facts_active
	ON memory_facts (owner_id, valid_until);
`

// FactStore persists MemoryFacts in SQLite. Multiuser-off behaviour:
// OwnerID is the empty string and GetActive("") matches only empty-owner
// rows — the disabled path stays byte-identical in behavior. Concurrency:
// SQLite single-writer mode (MaxOpenConns=1) serializes writes; the
// Upsert close+insert pair is atomic via a transaction, so no Go-side
// lock is held across I/O.
type FactStore struct {
	db *sql.DB
}

// NewFactStore opens (creating if needed) the facts database at dbPath.
func NewFactStore(dbPath string) (*FactStore, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("memory: fact store path is empty")
	}
	dir := filepath.Dir(dbPath)
	//nolint:gosec // user config directory/file permissions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("memory: create facts dir: %w", err)
	}
	dsn := "file:" + dbPath + "?_fk=1&_journal_mode=WAL&_busy_timeout=5000&cache=shared"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("memory: open fact store: %w", err)
	}
	// SQLite writes must be serialized.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, pragma := range []string{
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close() //nolint:mutexio // one-time init cleanup path
			return nil, fmt.Errorf("memory: fact store pragma: %w", err)
		}
	}
	if _, err := db.Exec(factSchema); err != nil {
		db.Close() //nolint:mutexio // one-time init cleanup path
		return nil, fmt.Errorf("memory: fact store schema: %w", err)
	}
	return &FactStore{db: db}, nil
}

// Close closes the underlying database.
func (s *FactStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Upsert writes f. When an active fact with the same OwnerID+Kind+Key
// exists, the previous row is closed with ValidUntil=now (last-write-wins
// with history) and the new row is inserted.
func (s *FactStore) Upsert(ctx context.Context, f MemoryFact) error {
	if f.Kind == "" || f.Key == "" {
		return fmt.Errorf("memory: fact kind and key are required")
	}
	if f.ID == "" {
		f.ID = id.Generate("fact-")
	}
	if f.UpdatedAt.IsZero() {
		f.UpdatedAt = time.Now().UTC()
	}

	now := f.UpdatedAt

	// Close-then-insert is atomic inside a transaction; SQLite's own
	// single-writer locking serializes concurrent Upserts (MaxOpenConns=1),
	// so no Go-side mutex is held across I/O.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memory: fact tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	// Close any currently-open row for this identity.
	if _, err := tx.ExecContext(ctx,
		`UPDATE memory_facts SET valid_until = ?
		 WHERE owner_id = ? AND kind = ? AND key = ?
		   AND (valid_until IS NULL OR valid_until > ?)`,
		now, f.OwnerID, string(f.Kind), f.Key, now); err != nil {
		return fmt.Errorf("memory: close previous fact: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memory_facts
		 (id, owner_id, kind, key, value, valid_from, valid_until, source_session, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.OwnerID, string(f.Kind), f.Key, f.Value,
		f.ValidFrom, f.ValidUntil, f.SourceSession, f.UpdatedAt); err != nil {
		return fmt.Errorf("memory: insert fact: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memory: fact commit: %w", err)
	}
	return nil
}

// GetActive returns facts visible for ownerID at instant at: rows whose
// ValidFrom <= at (or NULL) and ValidUntil > at (or NULL). Boundary rule:
// at == ValidUntil is EXCLUDED (the fact has ended).
func (s *FactStore) GetActive(ctx context.Context, ownerID string, at time.Time) ([]MemoryFact, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, kind, key, value, valid_from, valid_until, source_session, updated_at
		 FROM memory_facts
		 WHERE owner_id = ?
		   AND (valid_from IS NULL OR valid_from <= ?)
		   AND (valid_until IS NULL OR valid_until > ?)
		 ORDER BY updated_at DESC`,
		ownerID, at, at)
	if err != nil {
		return nil, fmt.Errorf("memory: query active facts: %w", err)
	}
	defer rows.Close() //nolint:errcheck // deferred close of read-only rows

	return scanFacts(rows)
}

// Search returns active facts for ownerID whose Key or Value contains
// query (case-insensitive substring), optionally restricted to one kind.
// Empty query matches everything.
func (s *FactStore) Search(ctx context.Context, ownerID, query, kind string) ([]MemoryFact, error) {
	active, err := s.GetActive(ctx, ownerID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	k := FactKind(kind)
	out := make([]MemoryFact, 0, len(active))
	for _, f := range active {
		if k != "" && f.Kind != k {
			continue
		}
		if q != "" &&
			!strings.Contains(strings.ToLower(f.Key), q) &&
			!strings.Contains(strings.ToLower(f.Value), q) {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

func scanFacts(rows *sql.Rows) ([]MemoryFact, error) {
	out := make([]MemoryFact, 0)
	for rows.Next() {
		var f MemoryFact
		var kind string
		var validFrom, validUntil sql.NullTime
		if err := rows.Scan(&f.ID, &f.OwnerID, &kind, &f.Key, &f.Value,
			&validFrom, &validUntil, &f.SourceSession, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("memory: scan fact: %w", err)
		}
		f.Kind = FactKind(kind)
		if validFrom.Valid {
			t := validFrom.Time
			f.ValidFrom = &t
		}
		if validUntil.Valid {
			t := validUntil.Time
			f.ValidUntil = &t
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
