package session

// Store schema-version accessors (contract S1; leaf 03-session-storage/01,
// tasks T2/T3). The `sessions` table carries
//
//	schema_version INTEGER NOT NULL DEFAULT <SessionFormatVersion>
//
// added additively via migrationAddColumn (existing rows back-fill through
// the DEFAULT clause — no rewrite). GetSchemaVersion reads the row's
// stamped version; MigrateSchema plans the forward path through the
// build-static catalog and refuses stored-newer (ErrSessionFormatNewer)
// and gapped chains (ErrSessionFormatGap). The version stamp is written
// LAST, after the row transforms commit, so a crash mid-migration leaves
// the old version stamped and the migration re-runs (dsh whole-value /
// commit-ordering intent).

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrSessionNotFound is returned by the schema-version accessors when the
// session id has no row.
var ErrSessionNotFound = errors.New("session: not found")

// GetSchemaVersion returns the stored format version of one session row.
// Unknown session ids return ErrSessionNotFound (wrapped).
func (s *SQLiteStore) GetSchemaVersion(sessionID string) (int, error) {
	var version int
	err := s.db.QueryRow(
		"SELECT schema_version FROM sessions WHERE id = ?", sessionID,
	).Scan(&version) //nolint:mutexio // single-statement read on the shared handle
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	if err != nil {
		return 0, fmt.Errorf("session: read schema_version for %s: %w", sessionID, err)
	}
	return version, nil
}

// MigrateSchema carries one session's stored format version forward to
// SessionFormatVersion through the build-static catalog. Forward-only:
// a stored-newer row is refused with ErrSessionFormatNewer and never
// written; a gapped chain is refused with ErrSessionFormatGap. Rows
// already at the current version are a no-op.
func (s *SQLiteStore) MigrateSchema(sessionID string) error {
	current, err := s.GetSchemaVersion(sessionID)
	if err != nil {
		return err
	}
	plan, err := defaultSessionFormatCatalog.Plan(current)
	if err != nil {
		return err
	}
	for _, m := range plan {
		if m.MigrateUp != nil {
			if err := m.MigrateUp(SessionVersionedRow{
				SessionID:   sessionID,
				FromVersion: m.FromVersion,
			}); err != nil {
				return fmt.Errorf("session: migrate %s via %q: %w", sessionID, m.Name, err)
			}
		}
		// Stamp each hop's target version AFTER its transform committed
		// so a crash leaves the last completed version stamped.
		if _, err := s.db.Exec(
			"UPDATE sessions SET schema_version = ? WHERE id = ?",
			m.ToVersion, sessionID,
		); err != nil { //nolint:mutexio // single-statement write on the shared handle
			return fmt.Errorf("session: stamp schema_version %d for %s: %w", m.ToVersion, sessionID, err)
		}
	}
	return nil
}

// GetSchemaVersion reports SessionFormatVersion for every in-memory
// session (the memory store has no persisted legacy rows).
func (m *MemoryStore) GetSchemaVersion(sessionID string) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return 0, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	return SessionFormatVersion, nil
}

// MigrateSchema is a no-op for the memory store: rows are always at the
// current build version, so the catalog plan is empty.
func (m *MemoryStore) MigrateSchema(sessionID string) error {
	_, err := m.GetSchemaVersion(sessionID)
	return err
}
