package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// AgentStatePersister persists agent state snapshots to durable storage for
// crash recovery and cross-restart observability.
type AgentStatePersister interface {
	// Save persists the agent state snapshot. Must be idempotent on agentID.
	Save(ctx context.Context, agentID string, snapshot AgentStateSnapshot) error
	// Load retrieves the most recent persisted snapshot, or sql.ErrNoRows if none.
	Load(ctx context.Context, agentID string) (*AgentStateSnapshot, error)
	// Delete removes a persisted snapshot (e.g., on clean session close).
	Delete(ctx context.Context, agentID string) error
}

// SQLiteStatePersister persists AgentStateSnapshot records to a SQLite table
// via an UPSERT on agent_id. It uses a sync.Once to ensure the DDL runs at
// most once per instance.
type SQLiteStatePersister struct {
	db        *sql.DB
	once      sync.Once
	initErr   error
	tableName string
}

// NewSQLiteStatePersister creates a new SQLiteStatePersister and runs the
// schema-creation DDL immediately. Returns an error if db is nil or the DDL
// fails.
func NewSQLiteStatePersister(db *sql.DB) (*SQLiteStatePersister, error) {
	if db == nil {
		return nil, errors.New("SQLiteStatePersister: db is nil")
	}
	p := &SQLiteStatePersister{
		db:        db,
		tableName: "agent_states",
	}
	p.once.Do(func() {
		p.initErr = p.ensureSchema(context.Background())
	})
	if p.initErr != nil {
		return nil, fmt.Errorf("SQLiteStatePersister: schema init failed: %w", p.initErr)
	}
	return p, nil
}

// ensureSchema creates the agent_states table if it does not yet exist.
func (p *SQLiteStatePersister) ensureSchema(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		agent_id   TEXT PRIMARY KEY,
		state_data TEXT NOT NULL,
		updated_at DATETIME NOT NULL
	)`, p.tableName)
	_, err := p.db.ExecContext(ctx, ddl)
	return err
}

// Save marshals the snapshot to JSON and UPSERTs it keyed on agentID.
func (p *SQLiteStatePersister) Save(ctx context.Context, agentID string, snapshot AgentStateSnapshot) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("SQLiteStatePersister.Save: marshal failed: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	q := fmt.Sprintf(`INSERT INTO %s (agent_id, state_data, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			state_data = excluded.state_data,
			updated_at = excluded.updated_at`, p.tableName)
	_, err = p.db.ExecContext(ctx, q, agentID, string(data), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("SQLiteStatePersister.Save: upsert failed: %w", err)
	}
	return nil
}

// Load retrieves the most recent persisted snapshot for the given agentID.
// Returns sql.ErrNoRows (not wrapped) if no record exists.
func (p *SQLiteStatePersister) Load(ctx context.Context, agentID string) (*AgentStateSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	q := fmt.Sprintf(`SELECT state_data FROM %s WHERE agent_id = ?`, p.tableName)
	var rawData string
	err := p.db.QueryRowContext(ctx, q, agentID).Scan(&rawData)
	if err != nil {
		return nil, err
	}
	var snap AgentStateSnapshot
	if err := json.Unmarshal([]byte(rawData), &snap); err != nil {
		return nil, fmt.Errorf("SQLiteStatePersister.Load: unmarshal failed: %w", err)
	}
	return &snap, nil
}

// Delete removes a persisted snapshot for the given agentID.
func (p *SQLiteStatePersister) Delete(ctx context.Context, agentID string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	q := fmt.Sprintf(`DELETE FROM %s WHERE agent_id = ?`, p.tableName)
	_, err := p.db.ExecContext(ctx, q, agentID)
	if err != nil {
		return fmt.Errorf("SQLiteStatePersister.Delete: %w", err)
	}
	return nil
}
