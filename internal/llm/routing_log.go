package llm

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // sqlite driver registration

	"github.com/caimlas/meept/pkg/id"
)

// RoutingDecision captures a single model-resolution outcome for later
// mining. The routing log is the training-set foundation for the
// student-learns-routing loop.
type RoutingDecision struct {
	ID               string    `json:"id" db:"id"`
	RequestID        string    `json:"request_id" db:"request_id"`
	Timestamp        time.Time `json:"timestamp" db:"timestamp"`
	ChosenModelID    string    `json:"chosen_model_id" db:"chosen_model_id"`
	ChosenProviderID string    `json:"chosen_provider_id" db:"chosen_provider_id"`
	Alias            string    `json:"alias,omitempty" db:"alias"`
	Reason           string    `json:"reason,omitempty" db:"reason"`
	Skill            string    `json:"skill,omitempty" db:"skill"`
	EmployeeID       string    `json:"employee_id,omitempty" db:"employee_id"`
	CandidatesJSON   string    `json:"candidates_json,omitempty" db:"candidates_json"`
}

// RoutingLogger persists RoutingDecisions to SQLite for later mining.
// It follows the security.Engine / lifecycle.UsageTrackerImpl pattern:
// WAL journal mode, MaxOpenConns(1) to avoid SQLITE_BUSY under concurrent
// writes, and schema initialization on construction.
type RoutingLogger struct {
	db     *sqlx.DB
	logger *slog.Logger
}

// NewRoutingLogger opens (creating if necessary) the routing log at dbPath.
// The dbPath follows the lifecycle.UsageTrackerImpl convention:
// `path?_journal_mode=WAL&_busy_timeout=5000`. A leading `~` is expanded to
// the user's home directory. If logger is nil, slog.Default() is used.
func NewRoutingLogger(dbPath string, logger *slog.Logger) (*RoutingLogger, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// Expand home directory prefix (mirrors lifecycle.UsageTrackerImpl).
	if len(dbPath) > 0 && dbPath[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("routing logger: failed to get home directory: %w", err)
		}
		dbPath = filepath.Join(home, dbPath[1:])
	}

	// Ensure the parent directory exists.
	if dir := filepath.Dir(dbPath); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("routing logger: failed to create database directory: %w", err)
		}
	}

	db, err := sqlx.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("routing logger: failed to open database: %w", err)
	}
	db.SetMaxOpenConns(1)

	rl := &RoutingLogger{
		db:     db,
		logger: logger.With("component", "routing-log"),
	}

	if err := rl.initSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("routing logger: schema initialization failed: %w", err)
	}

	return rl, nil
}

// initSchema creates the routing_decisions table and supporting indexes if
// they do not already exist. The schema is append-only: adding columns
// requires a migration.
func (rl *RoutingLogger) initSchema(ctx context.Context) error {
	_, err := rl.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS routing_decisions (
			id TEXT PRIMARY KEY,
			request_id TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			chosen_model_id TEXT NOT NULL,
			chosen_provider_id TEXT NOT NULL,
			alias TEXT,
			reason TEXT,
			skill TEXT,
			employee_id TEXT,
			candidates_json TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_routing_decisions_ts ON routing_decisions(timestamp);
		CREATE INDEX IF NOT EXISTS idx_routing_decisions_chosen ON routing_decisions(chosen_model_id);
	`)
	return err
}

// Record persists a decision. ID and Timestamp are filled in if zero.
// Errors from the underlying INSERT are wrapped and returned; the caller
// decides whether to drop or propagate.
func (rl *RoutingLogger) Record(ctx context.Context, dec RoutingDecision) error {
	if dec.ID == "" {
		dec.ID = id.Generate("routing")
	}
	if dec.Timestamp.IsZero() {
		dec.Timestamp = time.Now().UTC()
	}
	_, err := rl.db.NamedExecContext(ctx,
		`INSERT INTO routing_decisions
		 (id, request_id, timestamp, chosen_model_id, chosen_provider_id, alias, reason, skill, employee_id, candidates_json)
		 VALUES (:id, :request_id, :timestamp, :chosen_model_id, :chosen_provider_id, :alias, :reason, :skill, :employee_id, :candidates_json)`,
		dec)
	if err != nil {
		return fmt.Errorf("routing log record: %w", err)
	}
	return nil
}

// Recent returns the most recent N decisions, newest-first. A non-positive
// limit defaults to 100.
func (rl *RoutingLogger) Recent(ctx context.Context, limit int) ([]RoutingDecision, error) {
	if limit <= 0 {
		limit = 100
	}
	var out []RoutingDecision
	err := rl.db.SelectContext(ctx, &out,
		`SELECT * FROM routing_decisions ORDER BY timestamp DESC LIMIT ?`, limit)
	return out, err
}

// Close releases the database connection. Safe to call multiple times; a nil
// receiver or nil db returns nil.
func (rl *RoutingLogger) Close() error {
	if rl == nil || rl.db == nil {
		return nil
	}
	return rl.db.Close()
}
