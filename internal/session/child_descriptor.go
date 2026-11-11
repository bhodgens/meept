package session

// child_descriptor.go implements durable child-descriptor persistence for the
// delegation cold-resume path (01-delegation leaf 06). The SQLite store keeps
// one row per child keyed by child_id (upsert on save); the MemoryStore keeps
// the same records in a mutex-guarded map (test / non-daemon fallback). The
// shape is deliberately neutral — it holds the agent's descriptor as a JSON
// blob plus the lifecycle fields Resume needs — so this package never imports
// internal/agent.
//
// Contract: leaf docs/plans/20261102-dsh-convergence/01-delegation/06-descriptor-resume.md.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrChildDescriptorNotFound is returned by GetChildDescriptor for an unknown
// child_id.
var ErrChildDescriptorNotFound = errors.New("session: child descriptor not found")

// childDescriptorSchema is the additive SQLite DDL for durable child records.
// Kept separate so both the migrate() path and (optionally) a standalone
// bootstrap can exec it; CREATE TABLE IF NOT EXISTS makes it safe to run on
// any database, old or new.
const childDescriptorSchema = `
CREATE TABLE IF NOT EXISTS child_descriptors (
	child_id          TEXT PRIMARY KEY,
	parent_session_id TEXT NOT NULL,
	agent_id          TEXT NOT NULL DEFAULT '',
	state             TEXT NOT NULL DEFAULT 'pending',
	descriptor_json   TEXT NOT NULL,
	created_at        TEXT NOT NULL,
	updated_at        TEXT NOT NULL
);
`

// SaveChildDescriptor upserts a child record by child_id. It replaces the
// stored descriptor/lifecycle fields atomically (single statement), so a
// completed child resumption always reads the latest durable snapshot.
func (s *SQLiteStore) SaveChildDescriptor(ctx context.Context, rec ChildDescriptorRecord) error {
	if rec.ChildID == "" {
		return fmt.Errorf("session: child descriptor requires child_id")
	}
	if len(rec.DescriptorJSON) == 0 {
		return fmt.Errorf("session: child descriptor requires descriptor_json")
	}
	now := time.Now()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO child_descriptors (child_id, parent_session_id, agent_id, state, descriptor_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(child_id) DO UPDATE SET
			parent_session_id = excluded.parent_session_id,
			agent_id          = excluded.agent_id,
			state             = excluded.state,
			descriptor_json   = excluded.descriptor_json,
			created_at        = excluded.created_at,
			updated_at        = excluded.updated_at`,
		rec.ChildID, rec.ParentSessionID, rec.AgentID, rec.State, rec.DescriptorJSON,
		rec.CreatedAt.UTC().Format(time.RFC3339Nano), rec.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("session: save child descriptor %s: %w", rec.ChildID, err)
	}
	return nil
}

// GetChildDescriptor returns the durable record for a child, or
// ErrChildDescriptorNotFound when no row exists.
func (s *SQLiteStore) GetChildDescriptor(ctx context.Context, childID string) (ChildDescriptorRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT child_id, parent_session_id, agent_id, state, descriptor_json, created_at, updated_at
		FROM child_descriptors WHERE child_id = ?`, childID)

	var rec ChildDescriptorRecord
	var jsonBlob, createdAt, updatedAt string
	if err := row.Scan(&rec.ChildID, &rec.ParentSessionID, &rec.AgentID, &rec.State, &jsonBlob, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ChildDescriptorRecord{}, ErrChildDescriptorNotFound
		}
		return ChildDescriptorRecord{}, fmt.Errorf("session: get child descriptor %s: %w", childID, err)
	}
	rec.DescriptorJSON = []byte(jsonBlob)
	if ts, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		rec.CreatedAt = ts
	}
	if ts, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
		rec.UpdatedAt = ts
	}
	return rec, nil
}

// RemoveChildDescriptor deletes a child's durable record. It is idempotent —
// removing an already-missing child succeeds.
func (s *SQLiteStore) RemoveChildDescriptor(ctx context.Context, childID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM child_descriptors WHERE child_id = ?`, childID); err != nil {
		return fmt.Errorf("session: remove child descriptor %s: %w", childID, err)
	}
	return nil
}

// ListChildDescriptors returns every durable child record (no ordering
// guarantee). Enumeration capability for the daemon's startup cold-resume
// sweep (bughunt C13): the sweep type-asserts the store onto
// agent.ChildDescriptorLister, so this is an additive concrete method — the
// session.Store interface itself is unchanged and existing implementers and
// test fakes stay untouched.
func (s *SQLiteStore) ListChildDescriptors(ctx context.Context) ([]ChildDescriptorRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT child_id, parent_session_id, agent_id, state, descriptor_json, created_at, updated_at
		FROM child_descriptors`)
	if err != nil {
		return nil, fmt.Errorf("session: list child descriptors: %w", err)
	}
	defer rows.Close()
	var out []ChildDescriptorRecord
	for rows.Next() {
		var rec ChildDescriptorRecord
		var jsonBlob, createdAt, updatedAt string
		if err := rows.Scan(&rec.ChildID, &rec.ParentSessionID, &rec.AgentID, &rec.State, &jsonBlob, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("session: list child descriptors: %w", err)
		}
		rec.DescriptorJSON = []byte(jsonBlob)
		if ts, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
			rec.CreatedAt = ts
		}
		if ts, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
			rec.UpdatedAt = ts
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session: list child descriptors: %w", err)
	}
	return out, nil
}

// SaveChildDescriptor upserts a child record for the in-memory store. It
// mirrors the SQLite semantics (missing child_id/descriptor rejected; timestamps
// defaulted) so the two stores behave identically.
func (s *MemoryStore) SaveChildDescriptor(_ context.Context, rec ChildDescriptorRecord) error {
	if rec.ChildID == "" {
		return fmt.Errorf("session: child descriptor requires child_id")
	}
	if len(rec.DescriptorJSON) == 0 {
		return fmt.Errorf("session: child descriptor requires descriptor_json")
	}
	now := time.Now()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.childDescs == nil {
		s.childDescs = make(map[string]ChildDescriptorRecord)
	}
	s.childDescs[rec.ChildID] = rec
	return nil
}

// GetChildDescriptor returns a child record for the in-memory store, or
// ErrChildDescriptorNotFound when absent.
func (s *MemoryStore) GetChildDescriptor(_ context.Context, childID string) (ChildDescriptorRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.childDescs[childID]
	if !ok {
		return ChildDescriptorRecord{}, ErrChildDescriptorNotFound
	}
	return rec, nil
}

// RemoveChildDescriptor deletes a child record for the in-memory store. It is
// idempotent under concurrent access (guarded reads under the same mutex).
func (s *MemoryStore) RemoveChildDescriptor(_ context.Context, childID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.childDescs, childID)
	return nil
}

// ListChildDescriptors returns every durable child record for the in-memory
// store (snapshot under the store lock; no ordering guarantee). The
// enumeration capability for the startup cold-resume sweep (C13).
func (s *MemoryStore) ListChildDescriptors(_ context.Context) ([]ChildDescriptorRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ChildDescriptorRecord, 0, len(s.childDescs))
	for _, rec := range s.childDescs {
		out = append(out, rec)
	}
	return out, nil
}
