// Package employee implements the AI Employee design (see
// docs/superpowers/specs/2026-06-23-ai-employee-design.md). It owns the
// Goal model and GoalStore — the long-lived mandate layer that sits above
// per-iteration Plans.
//
// This file implements Phase 2 of the spec: the Goal data model, its SQLite
// store, and associated helpers. The GoalLoop driver (Phase 3) and the
// Constitution engine (Phase 1) live in sibling files; goal.go deliberately
// has no compile-time dependency on either.
package employee

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // sqlite driver registration

	"github.com/caimlas/meept/pkg/id"
)

// GoalIDPrefix is the prefix used by NewGoalID.
const GoalIDPrefix = "goal_"

// --------------------------------------------------------------------------
// Enums
// --------------------------------------------------------------------------

// GoalState is the lifecycle state of a Goal.
type GoalState int

const (
	// GoalActive means the mandate is currently being pursued.
	GoalActive GoalState = iota
	// GoalPaused means an operator or amendment paused pursuit.
	GoalPaused
	// GoalRetired means the goal is no longer relevant. Retired goals are
	// retained for audit (soft-delete via retired_at).
	GoalRetired
)

// String returns the canonical string representation used in storage and logs.
func (s GoalState) String() string {
	switch s {
	case GoalActive:
		return "active"
	case GoalPaused:
		return "paused"
	case GoalRetired:
		return "retired"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// ParseGoalState decodes a goal state string. Unknown values map to
// GoalActive with an error, so callers can decide whether to reject or
// default.
func ParseGoalState(s string) (GoalState, error) {
	switch s {
	case "active":
		return GoalActive, nil
	case "paused":
		return GoalPaused, nil
	case "retired":
		return GoalRetired, nil
	default:
		return GoalActive, fmt.Errorf("unknown goal state %q", s)
	}
}

// GoalHealth reflects how well a mandate is being met at the most recent
// assessment.
type GoalHealth int

const (
	// GoalHealthy means the last assessment found the mandate satisfied.
	GoalHealthy GoalHealth = iota
	// GoalAtRisk means warning signs are present but the mandate is not yet
	// violated.
	GoalAtRisk
	// GoalBroken means the mandate is currently violated.
	GoalBroken
	// GoalUnknown means the goal has not yet been assessed.
	GoalUnknown
)

// String returns the canonical storage representation.
func (h GoalHealth) String() string {
	switch h {
	case GoalHealthy:
		return "healthy"
	case GoalAtRisk:
		return "at_risk"
	case GoalBroken:
		return "broken"
	case GoalUnknown:
		return "unknown"
	default:
		return fmt.Sprintf("unknown(%d)", int(h))
	}
}

// ParseGoalHealth decodes a goal health string.
func ParseGoalHealth(s string) (GoalHealth, error) {
	switch s {
	case "healthy":
		return GoalHealthy, nil
	case "at_risk":
		return GoalAtRisk, nil
	case "broken":
		return GoalBroken, nil
	case "unknown":
		return GoalUnknown, nil
	default:
		return GoalUnknown, fmt.Errorf("unknown goal health %q", s)
	}
}

// GoalSource describes what triggered a goal's existence. Only meaningful for
// tier-2+ goals; tier-1 reactive goals are implicitly SourceTrigger.
type GoalSource string

const (
	// SourceUser is a goal directly assigned by a human operator.
	SourceUser GoalSource = "user"
	// SourceTrigger is a goal spawned by a cron/webhook/bus trigger.
	SourceTrigger GoalSource = "trigger"
	// SourceSelfProposed is a goal the employee proposed for itself and an
	// approver accepted.
	SourceSelfProposed GoalSource = "self_proposed"
	// SourceAuditFinding is a goal created to remediate an audit finding.
	SourceAuditFinding GoalSource = "audit_finding"
)

// --------------------------------------------------------------------------
// Goal
// --------------------------------------------------------------------------

// Goal is the long-lived mandate owned by an employee. Plans are the concrete
// iterations that pursue a Goal; the Goal itself changes far less often.
//
// See docs/superpowers/specs/2026-06-23-ai-employee-design.md §"Goal model
// and GoalLoop" for the full design.
type Goal struct {
	// ID is the stable unique identifier (pkg/id.Generate with GoalIDPrefix).
	ID string `json:"id"`
	// EmployeeID is the owning agent definition ID. References
	// bot_definitions(id).
	EmployeeID string `json:"employee_id"`
	// Title is a short human-readable label, e.g. "keep CI green for main".
	Title string `json:"title"`
	// Mandate is the stable objective in plain prose. The mandate should be
	// durable across many plan iterations.
	Mandate string `json:"mandate"`
	// State is the current lifecycle state.
	State GoalState `json:"state"`
	// Source is what triggered this goal's existence.
	Source GoalSource `json:"source"`
	// TriggerRef is an optional reference to the originating trigger (cron
	// schedule, webhook ID, bus topic, etc.). Empty for SourceUser.
	TriggerRef string `json:"trigger_ref,omitempty"`
	// Health is the most recent assessment verdict.
	Health GoalHealth `json:"health"`
	// LastAssessed is when Health was last computed. Zero value means
	// "never assessed".
	LastAssessed time.Time `json:"last_assessed"`
	// ActivePlanID is the currently-executing Plan pursuing this goal, if
	// any. Empty when no plan is active.
	ActivePlanID string `json:"active_plan_id,omitempty"`
	// PlanHistory is the ordered list of completed plan IDs (oldest first).
	// Stored as JSON in SQLite.
	PlanHistory []string `json:"plan_history"`
	// CreatedAt is when the goal was first persisted.
	CreatedAt time.Time `json:"created_at"`
	// RetiredAt is when the goal was soft-deleted. Zero for active goals.
	RetiredAt time.Time `json:"retired_at,omitempty"`

	// mu guards the in-memory Goal during concurrent reads/writes of the
	// slice and time fields. Store operations snapshot under this lock and
	// perform SQL I/O outside the critical section (per CLAUDE.md mutex
	// guidance).
	mu sync.RWMutex `json:"-"`

	// recentFindingsMax is the cap on the RecentFindings slice. Exported
	// as a const so tests and callers can reference it.
	// RecentFindings holds the finding IDs linked to this goal, newest first
	// (capped at recentFindingsMax entries). Maintained by AttachFinding.
	RecentFindings []string `json:"recent_findings,omitempty"`
}

// recentFindingsMax is the maximum number of finding IDs retained on the
// Goal's RecentFindings list. Older entries are evicted when the cap is
// reached. This keeps the in-memory and persisted goal representation
// bounded.
const recentFindingsMax = 50

// AttachFinding appends a finding ID to the goal's RecentFindings list. If
// the list exceeds recentFindingsMax entries, the oldest entries are
// evicted. This is the explicit goal-side bookkeeping for G7 (spec line
// 382: "attach to owning Goal"). Safe for concurrent use.
func (g *Goal) AttachFinding(findingID string) {
	if findingID == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.RecentFindings = append(g.RecentFindings, findingID)
	// Evict oldest entries beyond the cap.
	if len(g.RecentFindings) > recentFindingsMax {
		g.RecentFindings = g.RecentFindings[len(g.RecentFindings)-recentFindingsMax:]
	}
}

// RecentFindingsList returns a defensive copy of the recent findings list.
func (g *Goal) RecentFindingsList() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if len(g.RecentFindings) == 0 {
		return nil
	}
	return append([]string(nil), g.RecentFindings...)
}

// Lock acquires the goal's write lock. Callers must call Unlock.
//
// In most cases prefer the snapshot helpers (ActivePlan, History, etc.)
// instead of holding the lock across I/O. This is exposed for the GoalLoop
// driver's atomic assess→update step.
func (g *Goal) Lock()   { g.mu.Lock() }
func (g *Goal) Unlock() { g.mu.Unlock() }

// snapshot copies the concurrency-sensitive fields under a read lock and
// returns their values. It must be called without holding any other lock on
// g.
func (g *Goal) snapshot() (activePlanID string, history []string, lastAssessed time.Time, retiredAt time.Time, recentFindings []string) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	activePlanID = g.ActivePlanID
	if len(g.PlanHistory) > 0 {
		history = append([]string(nil), g.PlanHistory...) // defensive copy
	}
	lastAssessed = g.LastAssessed
	retiredAt = g.RetiredAt
	if len(g.RecentFindings) > 0 {
		recentFindings = append([]string(nil), g.RecentFindings...)
	}
	return
}

// ActivePlan returns the currently executing plan ID (empty if none) in a
// concurrency-safe manner.
func (g *Goal) ActivePlan() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.ActivePlanID
}

// History returns a defensive copy of the plan-history slice.
func (g *Goal) History() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if len(g.PlanHistory) == 0 {
		return nil
	}
	return append([]string(nil), g.PlanHistory...)
}

// AppendHistory appends a plan ID to the history and returns the new length.
// The caller must hold no other lock on g.
func (g *Goal) AppendHistory(planID string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.PlanHistory = append(g.PlanHistory, planID)
	return len(g.PlanHistory)
}

// SetActivePlan records the currently-executing plan ID.
func (g *Goal) SetActivePlan(planID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ActivePlanID = planID
}

// Assess marks the goal as assessed at now with the given health verdict.
// now is a parameter (not time.Now()) so tests are deterministic.
func (g *Goal) Assess(health GoalHealth, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Health = health
	g.LastAssessed = now
}

// IsRetired reports whether the goal has been soft-deleted.
func (g *Goal) IsRetired() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return !g.RetiredAt.IsZero()
}

// NewGoalID returns a fresh random goal ID.
func NewGoalID() string { return id.Generate(GoalIDPrefix) }

// --------------------------------------------------------------------------
// GoalStore
// --------------------------------------------------------------------------

// GoalStore persists Goal records to SQLite. It follows the pattern of
// internal/bot/store.go: one store per table, migrate-on-open, atomic writes,
// soft-delete for retired goals.
//
// All public methods accept a context and are safe for concurrent use. The
// underlying *sql.DB is goroutine-safe; the store itself holds no mutable
// state.
type GoalStore struct {
	db   *sql.DB
	log  *slog.Logger
	mu   sync.Mutex // serializes migrate-on-open and Close
	ready bool
}

// NewGoalStore opens (or creates) the SQLite database at path and runs
// migrations. If log is nil, slog.Default() is used.
func NewGoalStore(path string, log *slog.Logger) (*GoalStore, error) {
	if log == nil {
		log = slog.Default()
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	s := &GoalStore{db: db, log: log}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	s.mu.Lock()
	s.ready = true
	s.mu.Unlock()
	return s, nil
}

func (s *GoalStore) migrate() error {
	// WAL + busy_timeout allow concurrent readers and serialized writers
	// without the default SQLITE_BUSY-fail-fast behaviour that breaks
	// workloads with multiple goroutines sharing one connection pool.
	for _, pragma := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA foreign_keys=ON`,
	} {
		if _, err := s.db.Exec(pragma); err != nil {
			return fmt.Errorf("pragma %q: %w", pragma, err)
		}
	}
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	// Migration: add recent_findings column if it doesn't exist (added for
	// G7 goal-finding attachment). SQLite's ALTER TABLE ADD COLUMN is
	// idempotent-safe when guarded by a pragma_table_info check.
	if _, err := s.db.Exec(`ALTER TABLE employee_goals ADD COLUMN recent_findings TEXT`); err != nil {
		// "duplicate column name" means the column already exists — not an error.
		if !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("migrate recent_findings: %w", err)
		}
	}
	return nil
}

const schema = `
CREATE TABLE IF NOT EXISTS employee_goals (
    id            TEXT PRIMARY KEY,
    employee_id   TEXT NOT NULL,
    title         TEXT NOT NULL,
    mandate       TEXT NOT NULL,
    state         TEXT NOT NULL,
    source        TEXT NOT NULL,
    trigger_ref   TEXT,
    health        TEXT NOT NULL,
    last_assessed TEXT,
    active_plan_id TEXT,
    plan_history  TEXT,
    created_at    TEXT NOT NULL,
    retired_at    TEXT,
    recent_findings TEXT,
    FOREIGN KEY (employee_id) REFERENCES bot_definitions(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_goals_employee ON employee_goals(employee_id);
`

// Close closes the underlying database handle. Subsequent calls are no-ops.
// It is safe to call Close concurrently with outstanding queries: database/sql
// will wait for them to drain.
func (s *GoalStore) Close() error {
	s.mu.Lock()
	if !s.ready {
		s.mu.Unlock()
		return nil
	}
	s.ready = false
	db := s.db
	s.mu.Unlock()
	return db.Close()
}

// Create persists a new goal. g.ID, g.EmployeeID, g.Title, g.Mandate, g.Source
// and g.CreatedAt must be set; sensible defaults are applied to other fields
// if zero.
func (s *GoalStore) Create(ctx context.Context, g *Goal) error {
	if g == nil {
		return errors.New("create: nil goal")
	}
	if g.ID == "" {
		g.ID = NewGoalID()
	}
	if g.CreatedAt.IsZero() {
		g.CreatedAt = time.Now().UTC()
	}
	// Defensive: ensure state is one of the defined constants. The zero
	// value (GoalActive) is valid and the most common case.
	switch g.State {
	case GoalActive, GoalPaused, GoalRetired:
	default:
		return fmt.Errorf("create: invalid goal state %d", g.State)
	}

	history, err := marshalHistory(g.History())
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	recentFindings := marshalFindings(g.RecentFindingsList())

	lastAssessed := goalNullableTime(g.LastAssessed)
	retiredAt := goalNullableTime(g.RetiredAt)
	triggerRef := goalNullableString(g.TriggerRef)
	activePlan := goalNullableString(g.ActivePlanID)

	_, err = s.db.ExecContext(ctx, `
INSERT INTO employee_goals
    (id, employee_id, title, mandate, state, source, trigger_ref,
     health, last_assessed, active_plan_id, plan_history,
     created_at, retired_at, recent_findings)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		g.ID, g.EmployeeID, g.Title, g.Mandate,
		g.State.String(), string(g.Source), triggerRef,
		g.Health.String(), lastAssessed, activePlan, history,
		g.CreatedAt.Format(time.RFC3339), retiredAt, recentFindings,
	)
	if err != nil {
		return fmt.Errorf("insert: %w", err)
	}
	s.log.DebugContext(ctx, "employee goal created",
		"goal_id", g.ID, "employee_id", g.EmployeeID, "title", g.Title)
	return nil
}

// Get fetches a single goal by ID. Returns (nil, ErrGoalNotFound) if no row
// matches.
func (s *GoalStore) Get(ctx context.Context, id string) (*Goal, error) {
	row := s.db.QueryRowContext(ctx, selectByID, id)
	g, err := scanGoal(row)
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}
	return g, nil
}

// ListByEmployee returns all goals (including retired) for the given
// employee, ordered oldest-first.
func (s *GoalStore) ListByEmployee(ctx context.Context, employeeID string) ([]*Goal, error) {
	rows, err := s.db.QueryContext(ctx, selectByEmployee, employeeID)
	if err != nil {
		return nil, fmt.Errorf("list employee: %w", err)
	}
	return collectRows(ctx, rows)
}

// ListActive returns all goals whose state is not retired. If employeeID is
// non-empty, results are further filtered to that employee.
func (s *GoalStore) ListActive(ctx context.Context, employeeID string) ([]*Goal, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if employeeID == "" {
		rows, err = s.db.QueryContext(ctx, selectActiveAll)
	} else {
		rows, err = s.db.QueryContext(ctx, selectActiveByEmployee, employeeID)
	}
	if err != nil {
		return nil, fmt.Errorf("list active: %w", err)
	}
	return collectRows(ctx, rows)
}

// Update writes all mutable fields of g. ID is used as the key; if no row is
// updated, Update returns ErrGoalNotFound. g.mu is acquired for a read
// snapshot before the SQL write so concurrent in-memory mutations do not
// produce a torn row.
func (s *GoalStore) Update(ctx context.Context, g *Goal) error {
	if g == nil {
		return errors.New("update: nil goal")
	}
	activePlanID, history, lastAssessed, retiredAt, recentFindings := g.snapshot()

	historyJSON, err := marshalHistory(history)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	findingsJSON := marshalFindings(recentFindings)

	res, err := s.db.ExecContext(ctx, `
UPDATE employee_goals SET
    title = ?,
    mandate = ?,
    state = ?,
    source = ?,
    trigger_ref = ?,
    health = ?,
    last_assessed = ?,
    active_plan_id = ?,
    plan_history = ?,
    retired_at = ?,
    recent_findings = ?
WHERE id = ?`,
		g.Title, g.Mandate, g.State.String(), string(g.Source),
		goalNullableString(g.TriggerRef), g.Health.String(),
		goalNullableTime(lastAssessed), goalNullableString(activePlanID),
		historyJSON, goalNullableTime(retiredAt), findingsJSON, g.ID,
	)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrGoalNotFound
	}
	return nil
}

// Retire soft-deletes the goal: sets state=retired and retired_at=now (UTC),
// leaving the row in place for audit. now is a parameter so tests are
// deterministic; pass time.Now().UTC() in production.
func (s *GoalStore) Retire(ctx context.Context, id string, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	res, err := s.db.ExecContext(ctx, retireStmt, GoalRetired.String(), now.Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("retire: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrGoalNotFound
	}
	s.log.InfoContext(ctx, "employee goal retired", "goal_id", id, "retired_at", now)
	return nil
}

// --------------------------------------------------------------------------
// Errors
// --------------------------------------------------------------------------

// ErrGoalNotFound is returned by Get/Update/Retire when no row matches the
// supplied ID.
var ErrGoalNotFound = errors.New("goal not found")

// --------------------------------------------------------------------------
// SQL + scan helpers
// --------------------------------------------------------------------------

// rowScanner abstracts *sql.Row and *sql.Rows so the same scan function
// serves both call paths.
type rowScanner interface {
	Scan(dest ...any) error
}

const (
	selectByID = `
SELECT id, employee_id, title, mandate, state, source, trigger_ref,
       health, last_assessed, active_plan_id, plan_history,
       created_at, retired_at, recent_findings
FROM employee_goals WHERE id = ?`

	selectByEmployee = `
SELECT id, employee_id, title, mandate, state, source, trigger_ref,
       health, last_assessed, active_plan_id, plan_history,
       created_at, retired_at, recent_findings
FROM employee_goals WHERE employee_id = ? ORDER BY created_at`

	selectActiveAll = `
SELECT id, employee_id, title, mandate, state, source, trigger_ref,
       health, last_assessed, active_plan_id, plan_history,
       created_at, retired_at, recent_findings
FROM employee_goals WHERE state != 'retired' ORDER BY created_at`

	selectActiveByEmployee = `
SELECT id, employee_id, title, mandate, state, source, trigger_ref,
       health, last_assessed, active_plan_id, plan_history,
       created_at, retired_at, recent_findings
FROM employee_goals WHERE employee_id = ? AND state != 'retired'
ORDER BY created_at`

	retireStmt = `
UPDATE employee_goals SET state = ?, retired_at = ? WHERE id = ?`
)

func scanGoal(sc rowScanner) (*Goal, error) {
	var (
		g                          Goal
		stateStr, sourceStr        string
		healthStr                  string
		triggerRef, activePlanID   sql.NullString
		lastAssessed, retiredAt    sql.NullString
		planHistoryJSON            sql.NullString
		recentFindingsJSON         sql.NullString
		createdAt                  string
	)
	if err := sc.Scan(
		&g.ID, &g.EmployeeID, &g.Title, &g.Mandate,
		&stateStr, &sourceStr, &triggerRef,
		&healthStr, &lastAssessed, &activePlanID, &planHistoryJSON,
		&createdAt, &retiredAt, &recentFindingsJSON,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrGoalNotFound
		}
		return nil, err
	}

	st, err := ParseGoalState(stateStr)
	if err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}
	g.State = st

	h, err := ParseGoalHealth(healthStr)
	if err != nil {
		return nil, fmt.Errorf("unmarshal health: %w", err)
	}
	g.Health = h

	g.Source = GoalSource(sourceStr)
	if g.Source == "" {
		g.Source = SourceUser // safe default
	}

	if triggerRef.Valid {
		g.TriggerRef = triggerRef.String
	}
	if activePlanID.Valid {
		g.ActivePlanID = activePlanID.String
	}
	if lastAssessed.Valid {
		t, err := time.Parse(time.RFC3339, lastAssessed.String)
		if err != nil {
			return nil, fmt.Errorf("unmarshal last_assessed: %w", err)
		}
		g.LastAssessed = t
	}
	if retiredAt.Valid {
		t, err := time.Parse(time.RFC3339, retiredAt.String)
		if err != nil {
			return nil, fmt.Errorf("unmarshal retired_at: %w", err)
		}
		g.RetiredAt = t
	}
	if planHistoryJSON.Valid && planHistoryJSON.String != "" {
		if err := json.Unmarshal([]byte(planHistoryJSON.String), &g.PlanHistory); err != nil {
			return nil, fmt.Errorf("unmarshal plan_history: %w", err)
		}
	}
	if recentFindingsJSON.Valid && recentFindingsJSON.String != "" {
		if err := json.Unmarshal([]byte(recentFindingsJSON.String), &g.RecentFindings); err != nil {
			return nil, fmt.Errorf("unmarshal recent_findings: %w", err)
		}
	}

	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("unmarshal created_at: %w", err)
	}
	g.CreatedAt = t

	return &g, nil
}

// collectRows drains rows, calling scanGoal for each. Closes rows.
func collectRows(ctx context.Context, rows *sql.Rows) ([]*Goal, error) {
	defer rows.Close()
	var goals []*Goal
	for rows.Next() {
		g, err := scanGoal(rows)
		if err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		goals = append(goals, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return goals, nil
}

// --------------------------------------------------------------------------
// small helpers
// --------------------------------------------------------------------------

// marshalHistory serializes a plan-history slice to the storage form (a JSON
// array). Empty slices are stored as the literal "[]" so the column is never
// NULL — simplifies queries and keeps the schema NOT NULL-friendly.
func marshalHistory(history []string) (string, error) {
	if history == nil {
		history = []string{}
	}
	b, err := json.Marshal(history)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// marshalFindings serializes a recent-findings slice to the storage form (a
// JSON array). Returns nil for empty/nil slices so the column is NULL when
// no findings are attached (the common case). This differs from
// marshalHistory because findings are only present on goals that have been
// audited.
func marshalFindings(findings []string) any {
	if len(findings) == 0 {
		return nil
	}
	b, err := json.Marshal(findings)
	if err != nil {
		return nil
	}
	return string(b)
}

// goalNullableString and goalNullableTime are goal-local variants of the
// helpers in enforcement.go. They exist as value-receivers (rather than the
// pointer-receiver nullableTime in enforcement.go) so the GoalStore call
// sites stay allocation-free. Names are prefixed with "goal" to avoid a
// package-level redeclaration against enforcement.go.
func goalNullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func goalNullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}
