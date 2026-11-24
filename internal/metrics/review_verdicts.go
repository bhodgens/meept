package metrics

// Review verdict store (model-rating-metrics-plan contract C2,
// docs/plans/model-rating-metrics-plan/01-foundations/02-verdict-store.md).
//
// model_review_verdicts is append-only: verdicts persist indefinitely and the
// table is NEVER added to the retention purge list (see retentionTables in
// store.go and RetentionTables() below). Rollups are SQL aggregations over
// this table; model_performance is not reused (its key has no category
// dimension).
//
// Blame rule C3 lives in ResolveReviewedModel: the reviewed model for a trace
// is the model_id of the FINAL SUCCESSFUL llm_calls row for
// (trace_id, agent_id), never guessed.

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/modelrating"
	"github.com/jmoiron/sqlx"
)

// validateVerdictVocab enforces the C1 vocabulary rules by delegating to the
// modelrating package's Verdict.Validate (contract C2 consumes C1 vocab for
// scope/category/severity validation on Record): the scope and category must
// be frozen values, "pass" rows carry no severity, and defect rows carry one
// of the frozen severities.
func validateVerdictVocab(scope, category, severity string) error {
	return modelrating.Verdict{
		RubricID: modelrating.RubricID("store-validation"),
		Scope:    modelrating.RubricScope(scope),
		Category: modelrating.Category(category),
		Severity: modelrating.Severity(severity),
	}.Validate()
}

// initReviewVerdictSchema creates model_review_verdicts and its indexes.
// Column names are frozen by the leaf DDL (02-verdict-store.md) and must not
// be renamed. Fresh databases get the table from this script; existing
// databases get it on next open because CREATE TABLE IF NOT EXISTS is
// idempotent — no migration-registry step is needed for a brand-new table.
//
// rubric_version stamps the modelrating.RubricCatalogVersion each row was
// graded under. Existing databases gain the column via ALTER TABLE (ignored
// when it already exists) and their pre-column rows are BACKFILLED to
// version "1": every verdict recorded before version stamping existed was
// graded under catalog version 1, so stamping keeps them inside
// version-filtered aggregates instead of silently dropping them.
func initReviewVerdictSchema(db *sql.DB) error {
	schema := `
-- Review verdicts (model-rating-metrics-plan C2): append-only rubric verdicts
-- per step/cycle. NEVER added to the retention purge list — indefinite
-- retention is the feature (VR-RET-1).
CREATE TABLE IF NOT EXISTS model_review_verdicts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    step_id TEXT NOT NULL DEFAULT '',
    task_id TEXT NOT NULL DEFAULT '',
    cycle INTEGER NOT NULL DEFAULT 0,
    is_final BOOLEAN NOT NULL DEFAULT 0,
    rubric_id TEXT NOT NULL DEFAULT '',
    scope TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL DEFAULT '',
    reviewed_model_id TEXT NOT NULL DEFAULT '',
    judge_agent_id TEXT NOT NULL DEFAULT '',
    judge_model_id TEXT NOT NULL DEFAULT '',
    orchestrator_agent_id TEXT NOT NULL DEFAULT '',
    orchestrator_model_id TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL DEFAULT 0,
    trace_id TEXT NOT NULL DEFAULT '',
    rubric_version TEXT NOT NULL DEFAULT '1'
);
CREATE INDEX IF NOT EXISTS idx_mrv_step ON model_review_verdicts(step_id);
CREATE INDEX IF NOT EXISTS idx_mrv_model_ts ON model_review_verdicts(reviewed_model_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_mrv_trace ON model_review_verdicts(trace_id);
CREATE INDEX IF NOT EXISTS idx_mrv_breakdown ON model_review_verdicts(reviewed_model_id, category);
`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to initialize model_review_verdicts schema: %w", err)
	}
	// Migration for databases created before rubric_version existed: add the
	// column (no-op when present) and stamp the pre-column rows as version 1.
	// "duplicate column name" is the idempotent path, not an error.
	if _, err := db.Exec(`ALTER TABLE model_review_verdicts ADD COLUMN rubric_version TEXT NOT NULL DEFAULT '1'`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("failed to add rubric_version column: %w", err)
		}
	}
	if _, err := db.Exec(`UPDATE model_review_verdicts SET rubric_version = '1' WHERE rubric_version = ''`); err != nil {
		return fmt.Errorf("failed to backfill rubric_version: %w", err)
	}
	return nil
}

// ReviewVerdictRecord is one rubric verdict emitted by a review judge for a
// step (contract C2). Workflow-scope rows blame the orchestrator: their
// ReviewedModelID is empty and OrchestratorAgentID/OrchestratorModelID
// identify the blamed orchestrator.
type ReviewVerdictRecord struct {
	StepID              string  `db:"step_id"`
	TaskID              string  `db:"task_id"`
	Cycle               int     `db:"cycle"`
	IsFinal             bool    `db:"is_final"`
	RubricID            string  `db:"rubric_id"`
	Scope               string  `db:"scope"`
	Category            string  `db:"category"`
	Severity            string  `db:"severity"`
	ReviewedModelID     string  `db:"reviewed_model_id"`
	JudgeAgentID        string  `db:"judge_agent_id"`
	JudgeModelID        string  `db:"judge_model_id"`
	OrchestratorAgentID string  `db:"orchestrator_agent_id"`
	OrchestratorModelID string  `db:"orchestrator_model_id"`
	Confidence          float64 `db:"confidence"`
	TraceID             string  `db:"trace_id"`
	// RubricVersion is the modelrating.RubricCatalogVersion the row was
	// graded under. Empty on input means "stamp the current catalog
	// version" — the only value producers should ever need. A non-empty
	// input pins an explicit version (used by tests and future migrations).
	RubricVersion string    `db:"rubric_version"`
	Timestamp     time.Time `db:"timestamp"`
}

// RecordReviewVerdict validates the scope/category/severity combination
// against the frozen C1 vocabulary and appends the verdict row. The stored
// timestamp is RFC3339 UTC text (byte-wise comparable, like llm_calls).
func (s *Store) RecordReviewVerdict(rec ReviewVerdictRecord) error {
	if err := validateVerdictVocab(rec.Scope, rec.Category, rec.Severity); err != nil {
		return err
	}
	// Rubric-ID catalog membership is NOT enforced on Record: judge output may
	// carry synthetic/unknown ids and the row must keep them (the emission
	// contract stores raw text rather than dropping rows).
	ts := rec.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	isFinal := 0
	if rec.IsFinal {
		isFinal = 1
	}
	// Stamp the catalog version the row was graded under. Empty input means
	// "current catalog version" — the normal producer path. This is captured
	// per row so a future catalog bump (modelrating.RubricCatalogVersion)
	// leaves stored history interpretable.
	version := rec.RubricVersion
	if version == "" {
		version = modelrating.RubricCatalogVersion
	}
	_, err := s.db.Exec(
		`INSERT INTO model_review_verdicts
			(timestamp, step_id, task_id, cycle, is_final, rubric_id, scope,
			 category, severity, reviewed_model_id, judge_agent_id,
			 judge_model_id, orchestrator_agent_id, orchestrator_model_id,
			 confidence, trace_id, rubric_version)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ts.UTC().Format(time.RFC3339), rec.StepID, rec.TaskID, rec.Cycle,
		isFinal, rec.RubricID, rec.Scope, rec.Category, rec.Severity,
		rec.ReviewedModelID, rec.JudgeAgentID, rec.JudgeModelID,
		rec.OrchestratorAgentID, rec.OrchestratorModelID,
		rec.Confidence, rec.TraceID, version,
	)
	if err != nil {
		return fmt.Errorf("failed to record review verdict: %w", err)
	}
	return nil
}

// ReviewVerdictQuery selects and groups verdict rollups (contract C2).
// GroupBy is one of "model", "judge", "category", "rubric". By default
// workflow-scope rows are excluded from aggregates; IncludeWorkflow opts in.
type ReviewVerdictQuery struct {
	Since           time.Time
	GroupBy         string
	IncludeWorkflow bool
}

// ReviewVerdictRow is one aggregate group: total verdicts, defect verdicts
// (category != "pass"), and the pass rate over the group.
type ReviewVerdictRow struct {
	Group    string
	Verdicts int64
	Defects  int64
	PassRate float64
	// Label carries the catalog rubric statement for rubric-grouped rows
	// ("" for other group-bys and for ids not in the catalog) so surfaces can
	// render "MR-LC1 — line-number corruption" without a second lookup.
	Label string
}

// verdictGroupColumn maps a GroupBy value to its column. The mapping is a
// fixed whitelist — never user-supplied SQL.
var verdictGroupColumn = map[string]string{
	"model":    "reviewed_model_id",
	"judge":    "judge_agent_id",
	"category": "category",
	"rubric":   "rubric_id",
}

// rubricStatements maps catalog rubric ids to their statements (modelrating.
// Rubrics()) for rubric-grouped rollups; unknown ids group under their raw id
// with an empty statement.
func rubricStatements() map[string]string {
	out := make(map[string]string)
	for _, r := range modelrating.Rubrics() {
		out[string(r.ID)] = r.Statement
	}
	return out
}

// QueryReviewVerdicts aggregates model_review_verdicts into per-group counts.
// PassRate = pass-category rows / total rows in the group (0 when empty).
// Workflow-scope rows are excluded unless q.IncludeWorkflow is set.
func (s *Store) QueryReviewVerdicts(q ReviewVerdictQuery) ([]ReviewVerdictRow, error) {
	return queryReviewVerdicts(s.db, q)
}

// queryReviewVerdicts is the sqlx-level implementation shared by *Store and
// *ReadOnlyStore (the CLI analytics path must never open the daemon's live
// metrics.db for writing — bughunt 2026-11-23).
func queryReviewVerdicts(db *sqlx.DB, q ReviewVerdictQuery) ([]ReviewVerdictRow, error) {
	col, ok := verdictGroupColumn[q.GroupBy]
	if !ok {
		return nil, fmt.Errorf("invalid GroupBy %q (want model, judge, category, or rubric)", q.GroupBy)
	}

	query := fmt.Sprintf(`SELECT %[1]s AS grp,
			COUNT(*) AS verdicts,
			SUM(CASE WHEN category != 'pass' THEN 1 ELSE 0 END) AS defects,
			SUM(CASE WHEN category = 'pass' THEN 1 ELSE 0 END) AS passes
		FROM model_review_verdicts
		WHERE scope != 'workflow'`, col) //nolint:gosec // col is from a fixed whitelist
	if q.IncludeWorkflow {
		query = fmt.Sprintf(`SELECT %[1]s AS grp,
			COUNT(*) AS verdicts,
			SUM(CASE WHEN category != 'pass' THEN 1 ELSE 0 END) AS defects,
			SUM(CASE WHEN category = 'pass' THEN 1 ELSE 0 END) AS passes
		FROM model_review_verdicts`, col) //nolint:gosec // col is from a fixed whitelist
	}
	args := []any{}
	if !q.Since.IsZero() {
		// The non-workflow base query already carries a WHERE clause
		// (scope != 'workflow'); the time filter must AND into it rather
		// than open a second WHERE.
		if q.IncludeWorkflow {
			query += " WHERE timestamp >= ?"
		} else {
			query += " AND timestamp >= ?"
		}
		args = append(args, q.Since.UTC().Format(time.RFC3339))
	}
	query += " GROUP BY grp ORDER BY grp"

	type aggRow struct {
		Group    string `db:"grp"`
		Verdicts int64  `db:"verdicts"`
		Defects  int64  `db:"defects"`
		Passes   int64  `db:"passes"`
	}
	var aggs []aggRow
	if err := db.Select(&aggs, query, args...); err != nil {
		return nil, fmt.Errorf("failed to query review verdict rollups: %w", err)
	}
	labels := map[string]string{}
	if q.GroupBy == "rubric" {
		labels = rubricStatements()
	}
	rows := make([]ReviewVerdictRow, 0, len(aggs))
	for _, a := range aggs {
		r := ReviewVerdictRow{
			Group:    a.Group,
			Verdicts: a.Verdicts,
			Defects:  a.Defects,
			Label:    labels[a.Group],
		}
		if a.Verdicts > 0 {
			r.PassRate = float64(a.Passes) / float64(a.Verdicts)
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// CountReviewVerdicts returns the total verdict rows matching q (ignoring
// groupings) — the COUNT half of contract C5: the HTTP endpoint derives
// workflow_excluded as CountReviewVerdicts(IncludeWorkflow=true) minus the
// default (workflow-excluding) count. Unlike QueryReviewVerdicts this is a
// plain COUNT, not a GROUP BY rollup.
func (s *Store) CountReviewVerdicts(q ReviewVerdictQuery) (int64, error) {
	query := `SELECT COUNT(*) FROM model_review_verdicts WHERE scope != 'workflow'`
	if q.IncludeWorkflow {
		query = `SELECT COUNT(*) FROM model_review_verdicts WHERE 1=1`
	}
	args := []any{}
	if !q.Since.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, q.Since.UTC().Format(time.RFC3339))
	}
	var count int64
	if err := s.db.Get(&count, query, args...); err != nil {
		return 0, fmt.Errorf("failed to count review verdicts: %w", err)
	}
	return count, nil
}

// ReviewVerdictBreakdownRow is one (reviewed model, category) pair with its
// verdict count — the "what kind of bad work did THIS model do" aggregate the
// issue's question 2 asks for, answered in one query instead of one
// group-by per category. Workflow-scope rows are always excluded (their
// reviewed_model_id is empty by contract C2). Pass rows appear as
// Category="pass" so a model's denominator rides in the same result.
type ReviewVerdictBreakdownRow struct {
	// Model is the reviewed model id.
	Model string `json:"model" db:"model"`
	// Category is the frozen defect category or "pass".
	Category string `json:"category" db:"category"`
	// Count is the number of in-window verdict rows for the pair.
	Count int64 `json:"count" db:"count"`
}

// QueryReviewVerdictBreakdown aggregates verdicts per (reviewed model,
// category). Workflow-scope rows are structurally excluded. Zero Since means
// unbounded. Ordering: model ASC, then count DESC, then category ASC —
// stable for tables and tests.
func (s *Store) QueryReviewVerdictBreakdown(since time.Time) ([]ReviewVerdictBreakdownRow, error) {
	return queryReviewVerdictBreakdown(s.db, since)
}

// queryReviewVerdictBreakdown is the sqlx-level implementation shared by
// *Store and *ReadOnlyStore.
func queryReviewVerdictBreakdown(db *sqlx.DB, since time.Time) ([]ReviewVerdictBreakdownRow, error) {
	query := `SELECT reviewed_model_id AS model, category, COUNT(*) AS count
		FROM model_review_verdicts
		WHERE scope != 'workflow' AND reviewed_model_id != ''`
	args := []any{}
	if !since.IsZero() {
		query += ` AND timestamp >= ?`
		args = append(args, since.UTC().Format(time.RFC3339))
	}
	query += ` GROUP BY reviewed_model_id, category
		ORDER BY model ASC, count DESC, category ASC`
	var rows []ReviewVerdictBreakdownRow
	if err := db.Select(&rows, query, args...); err != nil {
		return nil, fmt.Errorf("failed to query review verdict breakdown: %w", err)
	}
	if rows == nil {
		rows = []ReviewVerdictBreakdownRow{}
	}
	return rows, nil
}

// QueryReviewVerdicts is the read-only twin of (*Store).QueryReviewVerdicts,
// served from the query_only handle. The CLI analytics path uses it so a
// verdict rollup can never take SQLite write locks against the daemon's
// live metrics.db (bughunt 2026-11-23).
func (r *ReadOnlyStore) QueryReviewVerdicts(q ReviewVerdictQuery) ([]ReviewVerdictRow, error) {
	if r == nil || r.db == nil {
		return []ReviewVerdictRow{}, nil
	}
	return queryReviewVerdicts(r.db, q)
}

// QueryReviewVerdictBreakdown is the read-only twin of
// (*Store).QueryReviewVerdictBreakdown.
func (r *ReadOnlyStore) QueryReviewVerdictBreakdown(since time.Time) ([]ReviewVerdictBreakdownRow, error) {
	if r == nil || r.db == nil {
		return []ReviewVerdictBreakdownRow{}, nil
	}
	return queryReviewVerdictBreakdown(r.db, since)
}

// FinalVerdictsForStep returns the verdict rows for a step's LAST cycle
// cycle only (earlier cycles are superseded review rounds).
func (s *Store) FinalVerdictsForStep(stepID string) ([]ReviewVerdictRecord, error) {
	type storedRow struct {
		StepID              string  `db:"step_id"`
		TaskID              string  `db:"task_id"`
		Cycle               int     `db:"cycle"`
		IsFinal             bool    `db:"is_final"`
		RubricID            string  `db:"rubric_id"`
		Scope               string  `db:"scope"`
		Category            string  `db:"category"`
		Severity            string  `db:"severity"`
		ReviewedModelID     string  `db:"reviewed_model_id"`
		JudgeAgentID        string  `db:"judge_agent_id"`
		JudgeModelID        string  `db:"judge_model_id"`
		OrchestratorAgentID string  `db:"orchestrator_agent_id"`
		OrchestratorModelID string  `db:"orchestrator_model_id"`
		Confidence          float64 `db:"confidence"`
		TraceID             string  `db:"trace_id"`
		Timestamp           string  `db:"timestamp"`
	}
	var stored []storedRow
	err := s.db.Select(&stored, `
		SELECT step_id, task_id, cycle, is_final, rubric_id, scope, category,
		       severity, reviewed_model_id, judge_agent_id, judge_model_id,
		       orchestrator_agent_id, orchestrator_model_id, confidence,
		       trace_id, timestamp
		FROM model_review_verdicts
		WHERE step_id = ?
		  AND cycle = (SELECT MAX(cycle) FROM model_review_verdicts WHERE step_id = ?)
		ORDER BY id`, stepID, stepID)
	if err != nil {
		return nil, fmt.Errorf("failed to query final verdicts for step %q: %w", stepID, err)
	}
	rows := make([]ReviewVerdictRecord, 0, len(stored))
	for _, r := range stored {
		ts, err := time.Parse(time.RFC3339, r.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("stored verdict timestamp %q for step %q: %w", r.Timestamp, stepID, err)
		}
		rows = append(rows, ReviewVerdictRecord{
			StepID:              r.StepID,
			TaskID:              r.TaskID,
			Cycle:               r.Cycle,
			IsFinal:             r.IsFinal,
			RubricID:            r.RubricID,
			Scope:               r.Scope,
			Category:            r.Category,
			Severity:            r.Severity,
			ReviewedModelID:     r.ReviewedModelID,
			JudgeAgentID:        r.JudgeAgentID,
			JudgeModelID:        r.JudgeModelID,
			OrchestratorAgentID: r.OrchestratorAgentID,
			OrchestratorModelID: r.OrchestratorModelID,
			Confidence:          r.Confidence,
			TraceID:             r.TraceID,
			Timestamp:           ts,
		})
	}
	return rows, nil
}

// ResolveReviewedModel implements the C3 blame rule: the model_id of the
// FINAL SUCCESSFUL llm_calls row (error=0) for (traceID, agentID), ordered by
// timestamp DESC then id DESC, limit 1. It returns "" with a nil error when
// the trace has no matching rows — the reviewed model is never guessed.
func (s *Store) ResolveReviewedModel(traceID, agentID string) (string, error) {
	var modelID string
	err := s.db.QueryRow(`
		SELECT model_id FROM llm_calls
		WHERE trace_id = ? AND agent_id = ? AND error = 0
		ORDER BY timestamp DESC, id DESC
		LIMIT 1`, traceID, agentID).Scan(&modelID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to resolve reviewed model for trace %q: %w", traceID, err)
	}
	return modelID, nil
}

// RetentionTables returns a copy of the retention purge list. It exists so
// tests (and callers) can assert the append-only invariant — that
// model_review_verdicts (and llm_calls) are never added to the purge list —
// without reaching into store internals. retentionTables in store.go stays
// the source of truth; the purge loop is untouched.
func RetentionTables() []string {
	out := make([]string, len(retentionTables))
	copy(out, retentionTables)
	return out
}
