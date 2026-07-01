package project

import (
	"context"
	"database/sql"
	"time"
)

// RecentsStore provides recents tracking for projects.
type RecentsStore struct {
	db *sql.DB
}

// NewRecentsStore creates a new recents store backed by the given db connection.
func NewRecentsStore(db *sql.DB) *RecentsStore {
	return &RecentsStore{db: db}
}

// TouchRecent updates or inserts a project path in recents.
func (s *RecentsStore) TouchRecent(ctx context.Context, path string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO project_recents (project_path, last_used_at)
		VALUES (?, datetime('now'))
		ON CONFLICT(project_path) DO UPDATE SET last_used_at = datetime('now')
	`, path)
	return err
}

// ListRecents returns the top N most recent project paths.
func (s *RecentsStore) ListRecents(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT project_path FROM project_recents
		ORDER BY last_used_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, rows.Err()
}

// PruneOlderThan removes entries older than the specified duration.
func (s *RecentsStore) PruneOlderThan(ctx context.Context, ttl time.Duration) (int64, error) {
	cutoff := time.Now().Add(-ttl)
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM project_recents
		WHERE last_used_at < datetime(?)
	`, cutoff.Format("2006-01-02 15:04:05"))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// CapToN removes oldest entries to keep only the most recent N.
func (s *RecentsStore) CapToN(ctx context.Context, max int) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM project_recents
		WHERE id NOT IN (
			SELECT id FROM (
				SELECT id FROM project_recents
				ORDER BY last_used_at DESC
				LIMIT ?
			)
		)
	`, max)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
