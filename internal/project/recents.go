package project

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// Scheduler is the minimal scheduler surface the project package needs.
// The daemon adapts *scheduler.Scheduler to this interface using the same
// simpleIntervalJob pattern as employeeSchedulerAdapter.
type Scheduler interface {
	// RunAtInterval registers fn to fire every interval. name is used
	// for logging and deduplication (same name replaces prior registration).
	RunAtInterval(name string, interval time.Duration, fn func())
}

// RecentsStore provides recents tracking for projects.
type RecentsStore struct {
	db *sql.DB
}

// NewRecentsStore creates a new recents store backed by the given db connection.
func NewRecentsStore(db *sql.DB) *RecentsStore {
	return &RecentsStore{db: db}
}

// rfc3339NanoFixed is RFC3339 with the fractional part forced to exactly 9
// digits. time.RFC3339Nano TRIMS trailing zeros, so a stored value's digit
// count varies; last_used_at is TEXT, so any leftover string comparison of it
// would be wrong (see the julianday() note on ListRecents). Fixed width keeps
// the stored form canonical.
const rfc3339NanoFixed = "2006-01-02T15:04:05.000000000Z07:00"

// TouchRecent updates or inserts a project path in recents.
func (s *RecentsStore) TouchRecent(ctx context.Context, path string) error {
	ts := time.Now().UTC().Format(rfc3339NanoFixed)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO project_recents (project_path, last_used_at)
		VALUES (?, ?)
		ON CONFLICT(project_path) DO UPDATE SET last_used_at = ?
	`, path, ts, ts)
	return err
}

// ListRecents returns the top N most recent project paths.
//
// The sort uses julianday(), not the raw TEXT column. last_used_at holds more
// than one textual shape: TouchRecent writes RFC3339 with a fractional part,
// while a row created by the table DEFAULT CURRENT_TIMESTAMP comes back with
// no fractional part at all ("2026-11-02T05:23:07Z"). A TEXT ORDER BY compares
// bytes and gets both cases wrong. Verified on this driver: for
// "...:37.123456789Z" and "...:37.1Z", TEXT DESC returns the .1 row first
// (wrong — .1 is earlier), while julianday() returns the .123456789 row first
// (right). The same trap applies to a no-fraction row, since 'Z' (0x5A) sorts
// after '.' (0x2E). julianday() parses each shape into a real instant, which
// is what "most recent" means. The rowid tiebreaker keeps same-instant touches
// deterministic; rowid is used rather than the id column because callers create
// this table with more than one shape (the daemon's store.go has an
// AUTOINCREMENT id, the project-binding e2e suite declares only
// project_path + last_used_at) and every SQLite table here has a rowid.
func (s *RecentsStore) ListRecents(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT project_path FROM project_recents
		ORDER BY julianday(last_used_at) DESC, rowid DESC
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

// PruneOlderThan removes entries older than the specified duration. The
// comparison uses julianday() for the same reason ListRecents does: rows carry
// different textual shapes, so a raw TEXT "<" comparison against a formatted
// cutoff mis-classifies which rows are old.
func (s *RecentsStore) PruneOlderThan(ctx context.Context, ttl time.Duration) (int64, error) {
	cutoff := time.Now().Add(-ttl)
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM project_recents
		WHERE julianday(last_used_at) < julianday(?)
	`, cutoff.UTC().Format(rfc3339NanoFixed))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// CapToN removes oldest entries to keep only the most recent N. rowid is the
// identity here rather than the id column, so the query works against every
// shape callers create this table with (see ListRecents).
func (s *RecentsStore) CapToN(ctx context.Context, max int) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM project_recents
		WHERE rowid NOT IN (
			SELECT rowid FROM (
				SELECT rowid FROM project_recents
				ORDER BY julianday(last_used_at) DESC, rowid DESC
				LIMIT ?
			)
		)
	`, max)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// SchedulePruneJob registers a daily job that prunes stale project recents
// and caps the total entry count. When cfg.TTLDays <= 0, TTL pruning is
// skipped. When cfg.MaxEntries <= 0, capping is skipped. No-op when sched
// or recents is nil.
func SchedulePruneJob(sched Scheduler, recents *RecentsStore, cfg config.ProjectRecentConfig) {
	if sched == nil || recents == nil {
		return
	}

	maxEntries := cfg.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 50 // default cap
	}
	ttlDays := cfg.TTLDays
	if ttlDays <= 0 {
		ttlDays = 30 // default TTL
	}
	ttl := time.Duration(ttlDays) * 24 * time.Hour

	sched.RunAtInterval("project.recents_prune", 24*time.Hour, func() {
		pruneCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		var deleted int64
		if cfg.TTLDays > 0 {
			d, err := recents.PruneOlderThan(pruneCtx, ttl)
			if err != nil {
				slog.Default().Warn("recents prune: TTL prune failed", "error", err)
			} else {
				deleted = d
			}
		}

		c, err := recents.CapToN(pruneCtx, maxEntries)
		if err != nil {
			slog.Default().Warn("recents prune: cap failed", "error", err)
			return
		}

		slog.Default().Info("recents prune completed",
			"ttl_truncated", deleted, "capped", c,
			"max_entries", maxEntries, "ttl_days", ttlDays)
	})
}
