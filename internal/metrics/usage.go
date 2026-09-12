// Package metrics: windowed model/agent usage queries over metrics.db.
//
// These are the data sources behind the model/agent extension of
// GET /api/v1/metrics/live. Model usage aggregates the append-only
// llm_calls ledger (Contract D); agent task outcomes come from
// agent_task_outcomes and live agent state from agent_states.
package metrics

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// ModelUsage is one model's aggregated usage over a time window. ID is
// "<provider>/<model_id>".
type ModelUsage struct {
	ID           string  `json:"id" db:"id"`
	Calls        int64   `json:"calls" db:"calls"`
	TokensIn     int64   `json:"tokens_in" db:"tokens_in"`
	TokensOut    int64   `json:"tokens_out" db:"tokens_out"`
	AvgLatencyMs float64 `json:"avg_latency_ms" db:"avg_latency_ms"`
}

// AgentUsage is one agent's task outcome totals. State is the agent's last
// persisted agent-state value ("current_state"), empty when no state row
// exists. TasksCompleted/TasksFailed are counts of agent_task_outcomes rows.
type AgentUsage struct {
	ID             string `json:"id" db:"id"`
	State          string `json:"state" db:"state"`
	TasksCompleted int64  `json:"tasks_completed" db:"tasks_completed"`
	TasksFailed    int64  `json:"tasks_failed" db:"tasks_failed"`
}

// UsageTotals is the sum of every model row in the window.
type UsageTotals struct {
	Calls     int64 `json:"calls"`
	TokensIn  int64 `json:"tokens_in"`
	TokensOut int64 `json:"tokens_out"`
}

// SumModelUsage totals a model usage list. It never returns nil-dependent
// values; an empty input yields the zero UsageTotals.
func SumModelUsage(models []ModelUsage) UsageTotals {
	var t UsageTotals
	for _, m := range models {
		t.Calls += m.Calls
		t.TokensIn += m.TokensIn
		t.TokensOut += m.TokensOut
	}
	return t
}

// modelUsageQuery aggregates llm_calls per provider+model over [since, now).
// Timestamps are stored as RFC3339 text (RecordLLMCall), so the bound is
// formatted identically and compares byte-wise. ORDER BY calls DESC, id ASC
// keeps the list stable across requests.
const modelUsageQuery = `
SELECT provider || '/' || model_id AS id,
       COUNT(*)                          AS calls,
       COALESCE(SUM(tokens_sent), 0)     AS tokens_in,
       COALESCE(SUM(tokens_received), 0) AS tokens_out,
       COALESCE(AVG(latency_ms), 0)      AS avg_latency_ms
FROM llm_calls
WHERE timestamp >= ?
GROUP BY provider, model_id
ORDER BY calls DESC, id ASC`

// QueryModelUsage aggregates llm_calls from since (inclusive) to now. The
// returned slice is always non-nil.
func QueryModelUsage(ctx context.Context, db *sqlx.DB, since time.Time) ([]ModelUsage, error) {
	rows := []ModelUsage{}
	if db == nil {
		return rows, nil
	}
	if err := db.SelectContext(ctx, &rows, modelUsageQuery, since.UTC().Format(time.RFC3339)); err != nil {
		return nil, fmt.Errorf("query model usage: %w", err)
	}
	if rows == nil {
		rows = []ModelUsage{}
	}
	return rows, nil
}

// ModelUsageSince reports model usage over [since, now).
func (s *Store) ModelUsageSince(ctx context.Context, since time.Time) ([]ModelUsage, error) {
	return QueryModelUsage(ctx, s.db, since)
}

// AgentUsageSince reports every known agent with its task outcome totals and
// last known state. "Known agents" is the union of agents seen in llm_calls
// within the since..now window and agents present in agent_task_outcomes
// (which has no window: it is the outcome ledger). The returned slice is
// always non-nil.
func (s *Store) AgentUsageSince(ctx context.Context, since time.Time) ([]AgentUsage, error) {
	return QueryAgentUsage(ctx, s.db, since)
}

// tableExists reports whether a table is present in the SQLite schema.
func tableExists(ctx context.Context, db *sqlx.DB, name string) bool {
	var n int
	err := db.GetContext(ctx, &n,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name)
	return err == nil && n > 0
}

func isMissingTable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such table")
}

// QueryAgentUsage aggregates agent usage. since bounds the llm_calls-derived
// portion of "known agents"; agent_task_outcomes is scanned in full (no
// window) because it is the outcome ledger. Tables that do not yet exist are
// treated as empty rather than errors.
func QueryAgentUsage(ctx context.Context, db *sqlx.DB, since time.Time) ([]AgentUsage, error) {
	out := []AgentUsage{}
	if db == nil {
		return out, nil
	}

	hasCalls := tableExists(ctx, db, "llm_calls")
	hasOutcomes := tableExists(ctx, db, "agent_task_outcomes")

	var idParts []string
	var args []any
	if hasCalls {
		idParts = append(idParts,
			`SELECT DISTINCT agent_id AS id FROM llm_calls WHERE agent_id != '' AND timestamp >= ?`)
		args = append(args, since.UTC().Format(time.RFC3339))
	}
	if hasOutcomes {
		idParts = append(idParts,
			`SELECT DISTINCT agent_id AS id FROM agent_task_outcomes WHERE agent_id != ''`)
	}
	if len(idParts) == 0 {
		return out, nil
	}

	// idParts is built from fixed string constants above, never user input.
	completedExpr, failedExpr := "0", "0"
	if hasOutcomes {
		completedExpr = "COALESCE(agg.tasks_completed, 0)"
		failedExpr = "COALESCE(agg.tasks_failed, 0)"
	}
	query := fmt.Sprintf(`SELECT ids.id AS id, %s AS tasks_completed, %s AS tasks_failed`,
		completedExpr, failedExpr) +
		` FROM (` + strings.Join(idParts, "\nUNION\n") + `) AS ids`
	if hasOutcomes {
		query += ` LEFT JOIN (SELECT agent_id,` +
			` SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) AS tasks_completed,` +
			` SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END) AS tasks_failed` +
			` FROM agent_task_outcomes WHERE agent_id != '' GROUP BY agent_id) AS agg` +
			` ON agg.agent_id = ids.id`
	}
	query += ` ORDER BY ids.id ASC`

	rows := []AgentUsage{}
	if err := db.SelectContext(ctx, &rows, query, args...); err != nil {
		if isMissingTable(err) {
			return out, nil
		}
		return nil, fmt.Errorf("query agent usage: %w", err)
	}
	if rows == nil {
		rows = []AgentUsage{}
	}

	// Best-effort last known state per agent (agent_states is only populated
	// when agent.state_tracking.persist is enabled).
	if states, err := queryAgentStates(ctx, db); err == nil {
		for i := range rows {
			if st, ok := states[rows[i].ID]; ok {
				rows[i].State = st
			}
		}
	}
	return rows, nil
}

// queryAgentStates reads the agent_states table, returning agent_id ->
// current_state. A missing table yields an empty (non-nil) map.
func queryAgentStates(ctx context.Context, db *sqlx.DB) (map[string]string, error) {
	states := map[string]string{}
	if !tableExists(ctx, db, "agent_states") {
		return states, nil
	}
	type row struct {
		AgentID   string `db:"agent_id"`
		StateData string `db:"state_data"`
	}
	var rows []row
	if err := db.SelectContext(ctx, &rows, `SELECT agent_id, state_data FROM agent_states`); err != nil {
		if isMissingTable(err) {
			return states, nil
		}
		return states, fmt.Errorf("query agent states: %w", err)
	}
	for _, r := range rows {
		var snap struct {
			CurrentState string `json:"current_state"`
		}
		if err := json.Unmarshal([]byte(r.StateData), &snap); err != nil {
			continue
		}
		if snap.CurrentState != "" {
			states[r.AgentID] = snap.CurrentState
		}
	}
	return states, nil
}

// ReadOnlyStore is a read-only handle on an existing metrics.db, used as the
// usage source when no *Store is reachable from the HTTP server. It enables
// PRAGMA query_only so an accidental write cannot mutate the ledger.
type ReadOnlyStore struct {
	db *sqlx.DB
}

// OpenReadOnly opens path read-only. The file must already exist.
func OpenReadOnly(path string) (*ReadOnlyStore, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("metrics database not available at %s: %w", path, err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open metrics database %s: %w", path, err)
	}
	raw.SetMaxOpenConns(1)
	raw.SetMaxIdleConns(1)
	db := sqlx.NewDb(raw, "sqlite")
	if _, err := db.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure metrics database: %w", err)
	}
	if _, err := db.Exec("PRAGMA query_only=ON;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("mark metrics database read-only: %w", err)
	}
	return &ReadOnlyStore{db: db}, nil
}

// ModelUsageSince reports model usage over [since, now).
func (r *ReadOnlyStore) ModelUsageSince(ctx context.Context, since time.Time) ([]ModelUsage, error) {
	if r == nil || r.db == nil {
		return []ModelUsage{}, nil
	}
	return QueryModelUsage(ctx, r.db, since)
}

// AgentUsageSince reports agent usage. since bounds the llm_calls-derived
// portion of the known-agent union.
func (r *ReadOnlyStore) AgentUsageSince(ctx context.Context, since time.Time) ([]AgentUsage, error) {
	if r == nil || r.db == nil {
		return []AgentUsage{}, nil
	}
	return QueryAgentUsage(ctx, r.db, since)
}

// Close releases the underlying connection. Safe on a nil receiver.
func (r *ReadOnlyStore) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}
