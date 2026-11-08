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
	"slices"
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
	// GoalBlocked means the goal refused further autonomous rounds — either
	// the round cap was reached or the model self-reported a block. It is a
	// durable phase; an operator must explicitly unblock it.
	GoalBlocked
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
	case GoalBlocked:
		return "blocked"
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
	case "blocked":
		return GoalBlocked, nil
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
	// SourceImplicit is a goal auto-created to anchor chat-path Plans that
	// arrived with no explicit goal. ValidateGoalSource treats it as valid at
	// every tier and it never requires signoff; implicit goals auto-retire
	// when their last active plan completes and no new plan references them
	// for 30 days.
	SourceImplicit GoalSource = "implicit"
)

// --------------------------------------------------------------------------
// Goal
// --------------------------------------------------------------------------

// BlockedReason carries the durable reason a goal entered the blocked phase.
// Code is a short machine key (e.g. "round-limit", "model-reported"); Message
// is the human-readable explanation.
type BlockedReason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

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
	// any. Empty when no plan is active. Kept for backward compatibility;
	// ActivePlanIDs is the authoritative multi-plan field (G2). When
	// ActivePlanIDs is non-empty, ActivePlanID mirrors its first element.
	ActivePlanID string `json:"active_plan_id,omitempty"`
	// ActivePlanIDs holds the IDs of all concurrently-executing plans
	// pursuing this goal (G2: multi-plan concurrency). The first element
	// mirrors ActivePlanID for backward compatibility. Empty when no plans
	// are active.
	ActivePlanIDs []string `json:"active_plan_ids,omitempty"`
	// PlanHistory is the ordered list of completed plan IDs (oldest first).
	// Stored as JSON in SQLite. Capped at MaxPlanHistory entries (G4: ring
	// buffer); the oldest entry is dropped when the cap is exceeded.
	PlanHistory []string `json:"plan_history"`
	// MaxPlanHistory is the maximum number of plan IDs retained in
	// PlanHistory (G4). Zero means use the default (DefaultMaxPlanHistory).
	// Attempts to append beyond this cap evict the oldest entry.
	MaxPlanHistory int `json:"max_plan_history,omitempty"`
	// MaxRounds is the cap on autonomous rounds this goal may run. Zero
	// means use the default (DefaultMaxGoalRounds, resolved via RoundsCap()).
	// When RoundsStarted reaches MaxRounds the goal is expected to block.
	MaxRounds int `json:"max_rounds,omitempty"`
	// RoundsStarted counts how many autonomous rounds this goal has run.
	// Guarded counter; durable across daemon restarts.
	RoundsStarted int `json:"rounds_started"`
	// BlockedReason is the durable reason the goal entered GoalBlocked, if
	// any. Nil when the goal is not blocked.
	BlockedReason *BlockedReason `json:"blocked_reason,omitempty"`
	// Revision is a compare-and-set counter, +1 on each Create/Update. Lets
	// goal_update CAS on optimistic concurrency.
	Revision int64 `json:"revision"`
	// CreatedAt is when the goal was first persisted.
	CreatedAt time.Time `json:"created_at"`
	// RetiredAt is when the goal was retired. Zero for active goals.
	// omitzero: omitempty has no effect on a time.Time (nested struct);
	// omitzero drops the key only for the zero time, preserving the
	// "absent = active goal" wire shape the original tag intended.
	RetiredAt time.Time `json:"retired_at,omitzero"`

	// Gate is the optional completion check for this goal. Nil or empty
	// Command means no per-goal gate (the employee default or none).
	Gate *GateConfig `json:"gate,omitempty"`

	// mu guards the in-memory Goal during concurrent reads/writes of the
	// slice and time fields. Store operations snapshot under this lock and
	// perform SQL I/O outside the critical section (per CLAUDE.md mutex
	// guidance).
	mu sync.RWMutex `json:"-"`

	// armed is the process-local, non-persisted arming flag: Activate()
	// arms automatic continuation, Disarm() clears it. The zero value is
	// disarmed. Deliberately never written to the store — after a daemon
	// restart an armed goal comes back disarmed (dsh session-start disarm
	// edge). Guarded by mu.
	armed bool `json:"-"`

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

// DefaultMaxPlanHistory is the default cap on PlanHistory entries (G4).
// When Goal.MaxPlanHistory is zero, this value is used.
const DefaultMaxPlanHistory = 100

// DefaultMaxGoalRounds is the default cap on autonomous rounds a goal may run
// (dsh default 256). When Goal.MaxRounds is zero, RoundsCap() uses this value.
const DefaultMaxGoalRounds = 256

// DefaultBlockedAfterConsecutiveRounds is the consecutive-round threshold at
// which IsBlockedAfterNConsecutive reports the goal should self-block (dsh
// default 3).
const DefaultBlockedAfterConsecutiveRounds = 3

// DefaultMaxActivePlans is the default cap on concurrently active plans
// (G2). When Constitution.MaxActivePlans is zero, this value is used.
const DefaultMaxActivePlans = 1

// DefaultConsecutiveSuccessesForRecovery is the default number of
// consecutive successful assessments required for a goal to transition
// from broken/at_risk back to healthy (G1).
const DefaultConsecutiveSuccessesForRecovery = 3

// ---------------------------------------------------------------------------
// G1: Health state machine with recovery transitions
// ---------------------------------------------------------------------------

// HealthDecayFunc maps a count of consecutive failures to the appropriate
// GoalHealth. At 0 failures → GoalHealthy; 1..N-1 → GoalAtRisk; >=N →
// GoalBroken. N is the provided threshold.
func HealthDecayFunc(failures, threshold int) GoalHealth {
	if failures <= 0 {
		return GoalHealthy
	}
	if threshold > 0 && failures >= threshold {
		return GoalBroken
	}
	return GoalAtRisk
}

// HealthRecoveryFunc maps a count of consecutive successes to the
// appropriate GoalHealth, given the goal's current health. The recovery
// state machine is:
//
//	broken → at_risk after M consecutive successes
//	at_risk → healthy after M consecutive successes
//	healthy stays healthy
//
// M is the recovery threshold (default
// DefaultConsecutiveSuccessesForRecovery). A goal that is already healthy
// stays healthy regardless of the success count.
func HealthRecoveryFunc(successes, recoveryThreshold int, current GoalHealth) GoalHealth {
	if current == GoalHealthy || current == GoalUnknown {
		if current == GoalUnknown && successes >= recoveryThreshold {
			return GoalHealthy
		}
		return current
	}
	if successes >= recoveryThreshold {
		// broken → at_risk; at_risk → healthy.
		if current == GoalBroken {
			return GoalAtRisk
		}
		return GoalHealthy
	}
	return current
}

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
func (g *Goal) snapshot() (activePlanID string, activePlanIDs []string, history []string, lastAssessed time.Time, retiredAt time.Time, recentFindings []string) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	activePlanID = g.ActivePlanID
	if len(g.ActivePlanIDs) > 0 {
		activePlanIDs = append([]string(nil), g.ActivePlanIDs...)
	}
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

// autonomySnapshot returns the C4a autonomy fields under a read lock. It must
// be called without holding any other lock on g.
func (g *Goal) autonomySnapshot() (maxRounds, roundsStarted int, blockedReason *BlockedReason) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.MaxRounds, g.RoundsStarted, g.BlockedReason
}

// bumpRevision increments the CAS revision counter under the write lock and
// returns the new value. Each Update calls this exactly once so revision
// advances monotonically per persisted write.
func (g *Goal) bumpRevision() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Revision++
	return g.Revision
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
// G4: When the history exceeds MaxPlanHistory (or DefaultMaxPlanHistory when
// unset), the oldest entry is dropped (ring-buffer semantics). The caller
// must hold no other lock on g.
func (g *Goal) AppendHistory(planID string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.PlanHistory = append(g.PlanHistory, planID)
	cap := g.MaxPlanHistory
	if cap <= 0 {
		cap = DefaultMaxPlanHistory
	}
	if len(g.PlanHistory) > cap {
		g.PlanHistory = g.PlanHistory[len(g.PlanHistory)-cap:]
	}
	return len(g.PlanHistory)
}

// SetActivePlan records the currently-executing plan ID and mirrors it
// into ActivePlanIDs[0] (G2 backward-compat).
func (g *Goal) SetActivePlan(planID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ActivePlanID = planID
	if planID == "" {
		g.ActivePlanIDs = nil
	} else if len(g.ActivePlanIDs) == 0 {
		g.ActivePlanIDs = []string{planID}
	} else {
		g.ActivePlanIDs[0] = planID
	}
}

// AddActivePlan adds a plan ID to the active plans list (G2). Returns the
// new count. Does NOT enforce MaxActivePlans; the caller (GoalLoop) is
// responsible for checking CanAddActivePlan before calling this.
func (g *Goal) AddActivePlan(planID string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if planID == "" {
		return len(g.ActivePlanIDs)
	}
	// Check for duplicates.
	if slices.Contains(g.ActivePlanIDs, planID) {
		return len(g.ActivePlanIDs)
	}
	g.ActivePlanIDs = append(g.ActivePlanIDs, planID)
	if len(g.ActivePlanIDs) > 0 {
		g.ActivePlanID = g.ActivePlanIDs[0]
	}
	return len(g.ActivePlanIDs)
}

// RemoveActivePlan removes a plan ID from the active plans list (G2).
// Returns the new count. Updates ActivePlanID to mirror the first
// remaining element (or empty when no plans remain).
func (g *Goal) RemoveActivePlan(planID string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	filtered := g.ActivePlanIDs[:0]
	for _, id := range g.ActivePlanIDs {
		if id != planID {
			filtered = append(filtered, id)
		}
	}
	g.ActivePlanIDs = filtered
	if len(g.ActivePlanIDs) > 0 {
		g.ActivePlanID = g.ActivePlanIDs[0]
	} else {
		g.ActivePlanID = ""
	}
	return len(g.ActivePlanIDs)
}

// ActivePlans returns a defensive copy of the active plan IDs (G2).
func (g *Goal) ActivePlans() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if len(g.ActivePlanIDs) == 0 {
		if g.ActivePlanID != "" {
			return []string{g.ActivePlanID}
		}
		return nil
	}
	return append([]string(nil), g.ActivePlanIDs...)
}

// CanAddActivePlan reports whether the goal can accept another active plan,
// given the maxActivePlans limit (G2). When maxActivePlans is zero,
// DefaultMaxActivePlans is used.
func (g *Goal) CanAddActivePlan(maxActivePlans int) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	cap := maxActivePlans
	if cap <= 0 {
		cap = DefaultMaxActivePlans
	}
	// Count: use ActivePlanIDs if non-empty, else fall back to ActivePlanID.
	count := len(g.ActivePlanIDs)
	if count == 0 && g.ActivePlanID != "" {
		count = 1
	}
	return count < cap
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

// RoundsCap returns the resolved maximum number of autonomous rounds:
// MaxRounds when positive, else the DefaultMaxGoalRounds default.
func (g *Goal) RoundsCap() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.MaxRounds > 0 {
		return g.MaxRounds
	}
	return DefaultMaxGoalRounds
}

// Activate arms the goal for automatic continuation. It is idempotent and
// process-local only (never persisted).
func (g *Goal) Activate() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.armed = true
}

// Disarm clears the live arming flag. It is idempotent and process-local.
func (g *Goal) Disarm() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.armed = false
}

// IsArmed reports whether the goal is armed for automatic continuation in
// this process.
func (g *Goal) IsArmed() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.armed
}

// StartRound increments RoundsStarted under the goal lock and returns the
// new count. Callers compare the result against RoundsCap() to enforce the
// round budget.
func (g *Goal) StartRound() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.RoundsStarted++
	return g.RoundsStarted
}

// IsBlockedAfterNConsecutive reports whether RoundsStarted has reached the
// consecutive-round self-block threshold (DefaultBlockedAfterConsecutiveRounds,
// 3). Used to refuse starting another autonomous round before an operator
// explicitly overrides the cap.
func (g *Goal) IsBlockedAfterNConsecutive() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.RoundsStarted >= DefaultBlockedAfterConsecutiveRounds
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
	db    *sql.DB
	log   *slog.Logger
	mu    sync.Mutex // serializes migrate-on-open and Close
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
	// The employee_goals FK references bot_definitions(id), a table the
	// bot store owns. Every database this store opens must contain the
	// referenced table or every INSERT fails with
	// "no such table: main.bot_definitions". CREATE TABLE IF NOT EXISTS
	// keeps this a no-op on the shared bots.db (where the real, populated
	// bot_definitions already exists) and creates an empty schema-only
	// stub on standalone databases — FK enforcement stays real.
	if _, err := s.db.Exec(botDefinitionsStubSQL); err != nil {
		return fmt.Errorf("ensure bot_definitions: %w", err)
	}
	if _, err := s.db.Exec(schema); err != nil {
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
	// G2: add active_plan_ids column for multi-plan concurrency.
	if _, err := s.db.Exec(`ALTER TABLE employee_goals ADD COLUMN active_plan_ids TEXT`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("migrate active_plan_ids: %w", err)
		}
	}
	// G4: add max_plan_history column for ring-buffer config.
	if _, err := s.db.Exec(`ALTER TABLE employee_goals ADD COLUMN max_plan_history INTEGER DEFAULT 0`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("migrate max_plan_history: %w", err)
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE employee_goals ADD COLUMN gate TEXT`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("migrate gate: %w", err)
		}
	}
	// C4a autonomy surface: additive columns for the round cap, rounds
	// started, the durable blocked reason, and the CAS revision counter.
	// Each ALTER is idempotent-safe when guarded by the duplicate-column
	// check, matching the pattern above.
	for _, add := range []struct {
		name string
		ddl  string
	}{
		{"max_rounds", `ALTER TABLE employee_goals ADD COLUMN max_rounds INTEGER DEFAULT 0`},
		{"rounds_started", `ALTER TABLE employee_goals ADD COLUMN rounds_started INTEGER DEFAULT 0`},
		{"blocked_reason_json", `ALTER TABLE employee_goals ADD COLUMN blocked_reason_json TEXT`},
		{"revision", `ALTER TABLE employee_goals ADD COLUMN revision INTEGER DEFAULT 0`},
	} {
		if _, err := s.db.Exec(add.ddl); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("migrate %s: %w", add.name, err)
			}
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
    max_rounds INTEGER DEFAULT 0,
    rounds_started INTEGER DEFAULT 0,
    blocked_reason_json TEXT,
    revision INTEGER DEFAULT 0,
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
	case GoalActive, GoalPaused, GoalRetired, GoalBlocked:
	default:
		return fmt.Errorf("create: invalid goal state %d", g.State)
	}

	// C4a: revision begins at 1 on first persist so every created goal has a
	// non-zero CAS anchor for later goal_update writes.
	if g.Revision == 0 {
		g.Revision = 1
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
	activePlanIDsJSON := marshalPlanIDs(g.ActivePlans())
	maxPlanHistory := g.MaxPlanHistory

	blockedReasonJSON := marshalBlockedReason(g.BlockedReason)

	_, err = s.db.ExecContext(ctx, `
INSERT INTO employee_goals
    (id, employee_id, title, mandate, state, source, trigger_ref,
     health, last_assessed, active_plan_id, plan_history,
     created_at, retired_at, recent_findings, active_plan_ids, max_plan_history,
     gate, max_rounds, rounds_started, blocked_reason_json, revision)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		g.ID, g.EmployeeID, g.Title, g.Mandate,
		g.State.String(), string(g.Source), triggerRef,
		g.Health.String(), lastAssessed, activePlan, history,
		g.CreatedAt.Format(time.RFC3339), retiredAt, recentFindings,
		activePlanIDsJSON, maxPlanHistory, marshalGate(g.Gate),
		g.MaxRounds, g.RoundsStarted, blockedReasonJSON, g.Revision,
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
	activePlanID, activePlanIDs, history, lastAssessed, retiredAt, recentFindings := g.snapshot()
	maxRounds, roundsStarted, blockedReason := g.autonomySnapshot()
	revision := g.bumpRevision()

	historyJSON, err := marshalHistory(history)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	findingsJSON := marshalFindings(recentFindings)
	activePlanIDsJSON := marshalPlanIDs(activePlanIDs)

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
    recent_findings = ?,
    active_plan_ids = ?,
    max_plan_history = ?,
    gate = ?,
    max_rounds = ?,
    rounds_started = ?,
    blocked_reason_json = ?,
    revision = ?
WHERE id = ?`,
		g.Title, g.Mandate, g.State.String(), string(g.Source),
		goalNullableString(g.TriggerRef), g.Health.String(),
		goalNullableTime(lastAssessed), goalNullableString(activePlanID),
		historyJSON, goalNullableTime(retiredAt), findingsJSON,
		activePlanIDsJSON, g.MaxPlanHistory, marshalGate(g.Gate),
		maxRounds, roundsStarted, marshalBlockedReason(blockedReason), revision, g.ID,
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

// EnsureImplicitGoal returns the employee's active SourceImplicit goal,
// creating one when none exists (goal-completion leaf 02, C2/C6). It anchors
// chat-path Plans that arrived with no explicit goal: the first call creates
// a goal with Source=SourceImplicit, State=GoalActive, Health=GoalUnknown and
// Title/Mandate set to title; subsequent calls return the same goal (filtered
// via ListActive + Source check), so repeated chat turns share one anchor per
// employee. A non-implicit active goal never satisfies the lookup — implicit
// anchoring must not hijack operator-assigned goals.
func (s *GoalStore) EnsureImplicitGoal(ctx context.Context, employeeID, title string) (*Goal, error) {
	if employeeID == "" {
		return nil, errors.New("ensure implicit goal: empty employee id")
	}
	goals, err := s.ListActive(ctx, employeeID)
	if err != nil {
		return nil, fmt.Errorf("ensure implicit goal: list active: %w", err)
	}
	for _, g := range goals {
		if g != nil && g.Source == SourceImplicit {
			return g, nil
		}
	}

	g := &Goal{
		EmployeeID: employeeID,
		Title:      title,
		Mandate:    title,
		State:      GoalActive,
		Source:     SourceImplicit,
		Health:     GoalUnknown,
	}
	if err := s.Create(ctx, g); err != nil {
		return nil, fmt.Errorf("ensure implicit goal: create: %w", err)
	}
	s.log.InfoContext(ctx, "implicit goal created",
		"goal_id", g.ID, "employee_id", employeeID, "title", title)
	return g, nil
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
       created_at, retired_at, recent_findings, active_plan_ids, max_plan_history, gate,
       max_rounds, rounds_started, blocked_reason_json, revision
FROM employee_goals WHERE id = ?`

	selectByEmployee = `
SELECT id, employee_id, title, mandate, state, source, trigger_ref,
       health, last_assessed, active_plan_id, plan_history,
       created_at, retired_at, recent_findings, active_plan_ids, max_plan_history, gate,
       max_rounds, rounds_started, blocked_reason_json, revision
FROM employee_goals WHERE employee_id = ? ORDER BY created_at`

	selectActiveAll = `
SELECT id, employee_id, title, mandate, state, source, trigger_ref,
       health, last_assessed, active_plan_id, plan_history,
       created_at, retired_at, recent_findings, active_plan_ids, max_plan_history, gate,
       max_rounds, rounds_started, blocked_reason_json, revision
FROM employee_goals WHERE state != 'retired' ORDER BY created_at`

	selectActiveByEmployee = `
SELECT id, employee_id, title, mandate, state, source, trigger_ref,
       health, last_assessed, active_plan_id, plan_history,
       created_at, retired_at, recent_findings, active_plan_ids, max_plan_history, gate,
       max_rounds, rounds_started, blocked_reason_json, revision
FROM employee_goals WHERE employee_id = ? AND state != 'retired'
ORDER BY created_at`

	retireStmt = `
UPDATE employee_goals SET state = ?, retired_at = ? WHERE id = ?`
)

func scanGoal(sc rowScanner) (*Goal, error) {
	var (
		g                        Goal
		stateStr, sourceStr      string
		healthStr                string
		triggerRef, activePlanID sql.NullString
		lastAssessed, retiredAt  sql.NullString
		planHistoryJSON          sql.NullString
		recentFindingsJSON       sql.NullString
		activePlanIDsJSON        sql.NullString
		maxPlanHistory           sql.NullInt64
		gateJSON                 sql.NullString
		maxRounds                sql.NullInt64
		roundsStarted            sql.NullInt64
		blockedReasonJSON        sql.NullString
		revision                 sql.NullInt64
		createdAt                string
	)
	if err := sc.Scan(
		&g.ID, &g.EmployeeID, &g.Title, &g.Mandate,
		&stateStr, &sourceStr, &triggerRef,
		&healthStr, &lastAssessed, &activePlanID, &planHistoryJSON,
		&createdAt, &retiredAt, &recentFindingsJSON,
		&activePlanIDsJSON, &maxPlanHistory, &gateJSON,
		&maxRounds, &roundsStarted, &blockedReasonJSON, &revision,
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
	if activePlanIDsJSON.Valid && activePlanIDsJSON.String != "" {
		if err := json.Unmarshal([]byte(activePlanIDsJSON.String), &g.ActivePlanIDs); err != nil {
			return nil, fmt.Errorf("unmarshal active_plan_ids: %w", err)
		}
	}
	if maxPlanHistory.Valid {
		g.MaxPlanHistory = int(maxPlanHistory.Int64)
	}
	g.Gate = unmarshalGate(gateJSON)

	if maxRounds.Valid {
		g.MaxRounds = int(maxRounds.Int64)
	}
	if roundsStarted.Valid {
		g.RoundsStarted = int(roundsStarted.Int64)
	}
	if blockedReasonJSON.Valid {
		g.BlockedReason = unmarshalBlockedReason(blockedReasonJSON.String)
	}
	if revision.Valid {
		g.Revision = revision.Int64
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
func marshalGate(g *GateConfig) any {
	if g == nil || g.Command == "" {
		return nil
	}
	b, err := json.Marshal(g)
	if err != nil {
		return nil
	}
	return string(b)
}

func unmarshalGate(s sql.NullString) *GateConfig {
	if !s.Valid || s.String == "" {
		return nil
	}
	var g GateConfig
	if err := json.Unmarshal([]byte(s.String), &g); err != nil {
		return nil
	}
	if g.Command == "" {
		return nil
	}
	return &g
}

// marshalBlockedReason serializes a BlockedReason to its storage form (JSON
// text). Returns nil for a nil reason so the column is NULL (the common case).
func marshalBlockedReason(br *BlockedReason) any {
	if br == nil {
		return nil
	}
	b, err := json.Marshal(br)
	if err != nil {
		return nil
	}
	return string(b)
}

// unmarshalBlockedReason decodes a blocked-reason JSON string. Empty or
// unparseable values decode to nil (defensive for legacy rows).
func unmarshalBlockedReason(s string) *BlockedReason {
	if s == "" {
		return nil
	}
	var br BlockedReason
	if err := json.Unmarshal([]byte(s), &br); err != nil {
		return nil
	}
	return &br
}

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

// marshalPlanIDs serializes an active-plan-IDs slice to the storage form
// (a JSON array). Returns nil for empty/nil slices so the column is NULL
// when no plans are active (the common case).
func marshalPlanIDs(planIDs []string) any {
	if len(planIDs) == 0 {
		return nil
	}
	b, err := json.Marshal(planIDs)
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
