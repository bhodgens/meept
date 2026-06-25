package lifecycle

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // sqlite driver registration

	"github.com/caimlas/meept/pkg/id"
)

// UsageTracker is the interface for recording and querying skill usage data.
// Implementations must be safe for concurrent use.
type UsageTracker interface {
	// RecordInjection records that a skill was injected (surfaced) into a
	// conversation prompt. Increments inject_count and updates
	// last_injected_at. Also inserts a skill_usage_events row with
	// event_type='inject'.
	RecordInjection(skillName string) error

	// RecordOutcome records the outcome of a skill injection. Increments the
	// appropriate counter (positive/negative/neutral), updates last_used_at,
	// recomputes effectiveness, and inserts a skill_usage_events row with
	// event_type='outcome'.
	RecordOutcome(skillName string, outcome Outcome, sessionID string) error

	// GetStats returns aggregate usage statistics for a single skill.
	GetStats(skillName string) (*UsageStats, error)

	// GetAllStats returns usage statistics for all skills.
	GetAllStats() (map[string]*UsageStats, error)

	// GetLowPerformers returns skills whose inject_count >= minInjections and
	// whose effectiveness is below the given threshold. Used by the evolver
	// prune pass.
	GetLowPerformers(threshold float64, minInjections int) ([]*UsageStats, error)

	// Close releases the underlying database connection.
	Close() error
}

// UsageTrackerImpl is the SQLite-backed implementation of UsageTracker.
// It follows the security.Engine pattern: WAL journal mode, single connection
// pool, and schema initialization on construction.
type UsageTrackerImpl struct {
	db     *sqlx.DB
	logger *slog.Logger
}

// NewUsageTracker creates a new SQLite-backed usage tracker at the given path.
// The path follows the security.Engine convention:
// `path?_journal_mode=WAL&_busy_timeout=5000`.
// SetMaxOpenConns(1) prevents SQLITE_BUSY under concurrent writes.
func NewUsageTracker(dbPath string, logger *slog.Logger) (*UsageTrackerImpl, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// Expand home directory prefix.
	if len(dbPath) > 0 && dbPath[0] == '~' {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("usage tracker: failed to get home directory: %w", err)
		}
		dbPath = filepath.Join(homeDir, dbPath[1:])
	}

	// Ensure the parent directory exists.
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("usage tracker: failed to create database directory: %w", err)
	}

	sqlDB, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("usage tracker: failed to open database: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	db := sqlx.NewDb(sqlDB, "sqlite")

	ut := &UsageTrackerImpl{
		db:     db,
		logger: logger,
	}

	if err := ut.initSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("usage tracker: schema initialization failed: %w", err)
	}

	logger.Info("Skill usage tracker initialized", "db", dbPath)
	return ut, nil
}

// usageSchemaSQL creates the skill_usage (aggregate) and skill_usage_events
// (log) tables. The aggregate table holds per-skill counters and is updated
// via upsert. The events table is an append-only log for auditing and
// time-series analysis.
const usageSchemaSQL = `
CREATE TABLE IF NOT EXISTS skill_usage (
    skill_name       TEXT PRIMARY KEY,
    inject_count     INTEGER NOT NULL DEFAULT 0,
    positive_count   INTEGER NOT NULL DEFAULT 0,
    negative_count   INTEGER NOT NULL DEFAULT 0,
    neutral_count    INTEGER NOT NULL DEFAULT 0,
    last_injected_at DATETIME,
    last_used_at     DATETIME,
    effectiveness    REAL NOT NULL DEFAULT 0.0
);

CREATE TABLE IF NOT EXISTS skill_usage_events (
    id          TEXT PRIMARY KEY,
    skill_name  TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    outcome     TEXT NOT NULL DEFAULT '',
    session_id  TEXT NOT NULL DEFAULT '',
    timestamp   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_skill_usage_events_skill
    ON skill_usage_events(skill_name);

CREATE INDEX IF NOT EXISTS idx_skill_usage_events_type
    ON skill_usage_events(event_type);
`

func (ut *UsageTrackerImpl) initSchema() error {
	_, err := ut.db.Exec(usageSchemaSQL)
	return err
}

// RecordInjection increments the inject_count for the named skill, updates
// last_injected_at, and inserts an event row. This is an upsert: if the skill
// has no existing row, one is created with inject_count=1.
func (ut *UsageTrackerImpl) RecordInjection(skillName string) error {
	now := time.Now().UTC()

	tx, err := ut.db.Begin()
	if err != nil {
		return fmt.Errorf("usage tracker: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Upsert aggregate row.
	_, err = tx.Exec(`
		INSERT INTO skill_usage (skill_name, inject_count, last_injected_at, effectiveness)
		VALUES (?, 1, ?, 0.0)
		ON CONFLICT(skill_name) DO UPDATE SET
			inject_count = inject_count + 1,
			last_injected_at = ?
	`, skillName, now, now)
	if err != nil {
		return fmt.Errorf("usage tracker: upsert inject: %w", err)
	}

	// Insert event row.
	_, err = tx.Exec(`
		INSERT INTO skill_usage_events (id, skill_name, event_type, timestamp)
		VALUES (?, ?, 'inject', ?)
	`, id.Generate("skEvt-"), skillName, now)
	if err != nil {
		return fmt.Errorf("usage tracker: insert inject event: %w", err)
	}

	return tx.Commit()
}

// RecordOutcome increments the outcome-specific counter, updates last_used_at,
// recomputes effectiveness = positive_count / inject_count (guarding divide-
// by-zero), and inserts an event row.
func (ut *UsageTrackerImpl) RecordOutcome(skillName string, outcome Outcome, sessionID string) error {
	now := time.Now().UTC()
	outcomeStr := outcome.String()

	// Compute initial counter values for the INSERT path (new skill).
	// When the skill has no existing row, we INSERT with the correct initial
	// counts directly. When the row exists, ON CONFLICT DO UPDATE increments.
	posInit := 0
	negInit := 0
	neuInit := 0
	switch outcome {
	case OutcomePositive:
		posInit = 1
	case OutcomeNegative:
		negInit = 1
	case OutcomeNeutral:
		neuInit = 1
	}

	tx, err := ut.db.Begin()
	if err != nil {
		return fmt.Errorf("usage tracker: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Upsert aggregate row. The INSERT path handles the "new skill" case with
	// correct initial counter values. The ON CONFLICT path increments the
	// appropriate counter and recomputes effectiveness.
	_, err = tx.Exec(`
		INSERT INTO skill_usage (skill_name, inject_count, positive_count, negative_count, neutral_count, last_used_at, effectiveness)
		VALUES (?, 0, ?, ?, ?, ?, 0.0)
		ON CONFLICT(skill_name) DO UPDATE SET
			positive_count = positive_count + CASE WHEN ? = 'positive' THEN 1 ELSE 0 END,
			negative_count = negative_count + CASE WHEN ? = 'negative' THEN 1 ELSE 0 END,
			neutral_count  = neutral_count  + CASE WHEN ? = 'neutral'  THEN 1 ELSE 0 END,
			last_used_at   = ?,
			effectiveness  = CASE
				WHEN inject_count > 0
				THEN CAST(positive_count + CASE WHEN ? = 'positive' THEN 1 ELSE 0 END AS REAL) / inject_count
				ELSE 0.0
			END
	`, skillName, posInit, negInit, neuInit, now, outcomeStr, outcomeStr, outcomeStr, now, outcomeStr)
	if err != nil {
		return fmt.Errorf("usage tracker: upsert outcome: %w", err)
	}

	// Insert event row.
	_, err = tx.Exec(`
		INSERT INTO skill_usage_events (id, skill_name, event_type, outcome, session_id, timestamp)
		VALUES (?, ?, 'outcome', ?, ?, ?)
	`, id.Generate("skEvt-"), skillName, outcomeStr, sessionID, now)
	if err != nil {
		return fmt.Errorf("usage tracker: insert outcome event: %w", err)
	}

	return tx.Commit()
}

// GetStats returns the aggregate usage statistics for a single skill.
// Returns a zero-valued UsageStats (with SkillName set) if the skill has no
// recorded usage — this is NOT an error.
func (ut *UsageTrackerImpl) GetStats(skillName string) (*UsageStats, error) {
	var row struct {
		SkillName      string    `db:"skill_name"`
		InjectCount    int       `db:"inject_count"`
		PositiveCount  int       `db:"positive_count"`
		NegativeCount  int       `db:"negative_count"`
		NeutralCount   int       `db:"neutral_count"`
		LastInjectedAt *time.Time `db:"last_injected_at"`
		LastUsedAt     *time.Time `db:"last_used_at"`
		Effectiveness  float64   `db:"effectiveness"`
	}

	err := ut.db.Get(&row, `SELECT * FROM skill_usage WHERE skill_name = ?`, skillName)
	if err != nil {
		if err == sql.ErrNoRows {
			return &UsageStats{SkillName: skillName}, nil
		}
		return nil, fmt.Errorf("usage tracker: get stats: %w", err)
	}

	stats := &UsageStats{
		SkillName:     row.SkillName,
		InjectCount:   row.InjectCount,
		PositiveCount: row.PositiveCount,
		NegativeCount: row.NegativeCount,
		NeutralCount:  row.NeutralCount,
		Effectiveness: row.Effectiveness,
	}
	if row.LastInjectedAt != nil {
		stats.LastInjectedAt = *row.LastInjectedAt
	}
	if row.LastUsedAt != nil {
		stats.LastUsedAt = *row.LastUsedAt
	}
	return stats, nil
}

// GetAllStats returns usage statistics for all skills, keyed by skill name.
func (ut *UsageTrackerImpl) GetAllStats() (map[string]*UsageStats, error) {
	var rows []struct {
		SkillName      string    `db:"skill_name"`
		InjectCount    int       `db:"inject_count"`
		PositiveCount  int       `db:"positive_count"`
		NegativeCount  int       `db:"negative_count"`
		NeutralCount   int       `db:"neutral_count"`
		LastInjectedAt *time.Time `db:"last_injected_at"`
		LastUsedAt     *time.Time `db:"last_used_at"`
		Effectiveness  float64   `db:"effectiveness"`
	}

	if err := ut.db.Select(&rows, `SELECT * FROM skill_usage`); err != nil {
		return nil, fmt.Errorf("usage tracker: get all stats: %w", err)
	}

	result := make(map[string]*UsageStats, len(rows))
	for _, row := range rows {
		stats := &UsageStats{
			SkillName:     row.SkillName,
			InjectCount:   row.InjectCount,
			PositiveCount: row.PositiveCount,
			NegativeCount: row.NegativeCount,
			NeutralCount:  row.NeutralCount,
			Effectiveness: row.Effectiveness,
		}
		if row.LastInjectedAt != nil {
			stats.LastInjectedAt = *row.LastInjectedAt
		}
		if row.LastUsedAt != nil {
			stats.LastUsedAt = *row.LastUsedAt
		}
		result[row.SkillName] = stats
	}
	return result, nil
}

// GetLowPerformers returns skills whose inject_count >= minInjections AND
// effectiveness < threshold. Results are sorted by effectiveness ascending
// (worst performers first) so the evolver prune pass sees the worst skills
// first.
func (ut *UsageTrackerImpl) GetLowPerformers(threshold float64, minInjections int) ([]*UsageStats, error) {
	var rows []struct {
		SkillName      string    `db:"skill_name"`
		InjectCount    int       `db:"inject_count"`
		PositiveCount  int       `db:"positive_count"`
		NegativeCount  int       `db:"negative_count"`
		NeutralCount   int       `db:"neutral_count"`
		LastInjectedAt *time.Time `db:"last_injected_at"`
		LastUsedAt     *time.Time `db:"last_used_at"`
		Effectiveness  float64   `db:"effectiveness"`
	}

	if err := ut.db.Select(&rows, `
		SELECT * FROM skill_usage
		WHERE inject_count >= ? AND effectiveness < ?
		ORDER BY effectiveness ASC
	`, minInjections, threshold); err != nil {
		return nil, fmt.Errorf("usage tracker: get low performers: %w", err)
	}

	result := make([]*UsageStats, 0, len(rows))
	for _, row := range rows {
		stats := &UsageStats{
			SkillName:     row.SkillName,
			InjectCount:   row.InjectCount,
			PositiveCount: row.PositiveCount,
			NegativeCount: row.NegativeCount,
			NeutralCount:  row.NeutralCount,
			Effectiveness: row.Effectiveness,
		}
		if row.LastInjectedAt != nil {
			stats.LastInjectedAt = *row.LastInjectedAt
		}
		if row.LastUsedAt != nil {
			stats.LastUsedAt = *row.LastUsedAt
		}
		result = append(result, stats)
	}
	return result, nil
}

// Close releases the database connection.
func (ut *UsageTrackerImpl) Close() error {
	if ut.db != nil {
		return ut.db.Close()
	}
	return nil
}
