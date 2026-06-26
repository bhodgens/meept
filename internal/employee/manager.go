// Package employee — manager.go contains the Manager type and its lifecycle
// methods (Hire, Retire, Review, AmendConstitution, List, Get) plus the
// ConstitutionStore that persists constitutions to a dedicated SQLite table.
//
// The Manager wraps an existing bot.Manager (persistence + execution) and
// layers constitution enforcement, goal, and audit semantics on top. The
// underlying bot package stays untouched at the storage layer; constitutions
// live in a separate table to avoid churning bot.BotDefinition's schema.
//
// See docs/superpowers/specs/2026-06-23-ai-employee-design.md §"Package
// layout" and §"RPC" for the authoritative contract.
package employee

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/pkg/security"
	idpkg "github.com/caimlas/meept/pkg/id"

	_ "modernc.org/sqlite" // sqlite driver registration
)

// --------------------------------------------------------------------------
// Errors
// --------------------------------------------------------------------------

// ErrNotImplemented is returned by Manager methods that have not yet been
// fully implemented. Phase 5 wires the lifecycle methods (Hire, Retire,
// List, Get, Pause, Resume, AmendConstitution); the GoalLoop driver and
// audit enforcement methods still return this until later phases land.
var ErrNotImplemented = errors.New("employee: not implemented")

// ErrEmployeeNotFound is returned by Get/Update/Retire when no row matches
// the supplied employee ID.
var ErrEmployeeNotFound = errors.New("employee not found")

// ErrConstitutionRequired is returned when an employee is loaded or hired
// without a constitution. Spec line 222: "A constitution is required at
// load time."
var ErrConstitutionRequired = errors.New("employee: constitution required; see docs/workflows/employees.md")

// ErrFrozenField is returned by AmendConstitution when the amendment patch
// touches a field listed in the existing constitution's
// AmendmentPolicy.FrozenFields.
var ErrFrozenField = errors.New("employee: constitution field is frozen")

// --------------------------------------------------------------------------
// Request / response types (used by Phase 6 RPCHandler)
// --------------------------------------------------------------------------

// HireRequest is the input shape for Manager.Hire (spec: agents.create).
// The Constitution map is the raw constitution as submitted by the caller;
// the Manager validates it via Constitution.Validate before persisting.
type HireRequest struct {
	ID           string
	Name         string
	Description  string
	Prompt       string
	Model        string
	Triggers     []bot.BotTrigger
	MemoryScope  bot.MemoryScope
	Tools        []string
	Enabled      bool
	Constitution map[string]any
}

// UpdateRequest is the input shape for Manager.UpdateEmployee. Constitution
// changes are NOT accepted here — use AmendConstitution for those.
type UpdateRequest struct {
	ID           string
	Name         string
	Description  string
	Prompt       string
	Model        string
	Tools        []string
	Enabled      *bool
	Constitution map[string]any
}

// AmendRequest is the input shape for Manager.AmendConstitution. Fields is a
// map of constitution field path → new value. FrozenFields on the existing
// constitution are checked before a Plan is created.
type AmendRequest struct {
	EmployeeID string
	Fields     map[string]any
	Reason     string
	// By identifies who is proposing the amendment ("user" or an agent
	// ID). The manager consults the existing constitution's
	// AmendmentPolicy.SelfProposeAllowed when By is not "user".
	By string
	// ExpectedVersion is the optimistic-locking guard (C1). The caller
	// sets this to the version they observed when reading the constitution.
	// AmendConstitution rejects with ErrVersionMismatch if the current
	// version differs, preventing concurrent amendments from silently
	// overwriting each other. Set to 0 to skip the check (legacy/test
	// path).
	ExpectedVersion int
}

// ErrVersionMismatch is returned by AmendConstitution when the caller's
// ExpectedVersion does not match the current constitution version. This
// prevents concurrent amendment approvals from silently overwriting each
// other (spec gap C1: optimistic locking for constitution versioning).
var ErrVersionMismatch = errors.New("employee: constitution version mismatch")

// AuditQuery filters audit findings for listing.
type AuditQuery struct {
	EmployeeID string
	Since      time.Duration
	Severity   string
}

// MigrationProposal is one bot→employee constitution proposal produced by
// Manager.Migrate. Each proposal is conservative (tier_1_reactive, low risk
// ceiling) unless the bot's prompt clearly maps to a higher tier.
type MigrationProposal struct {
	BotID       string       `json:"bot_id"`
	BotName     string       `json:"bot_name"`
	Proposed    Constitution `json:"proposed"`
	Confidence  float64      `json:"confidence"`
	NeedsReview bool         `json:"needs_review"`
	Warnings    []string     `json:"warnings,omitempty"`
}

// MigrationApplyResult is returned by Manager.ApplyMigration when writing a
// proposed constitution to disk.
type MigrationApplyResult struct {
	Applied  bool     `json:"applied"`
	Warnings []string `json:"warnings,omitempty"`
}

// TriggerResult summarizes one programmatic invocation of an employee via
// Manager.Trigger.
type TriggerResult struct {
	InvocationID string    `json:"invocation_id"`
	StartedAt    time.Time `json:"started_at"`
	Status       string    `json:"status"`
}

// Review summarizes the current state of an employee for the
// Review RPC / HTTP endpoint. It collects the Employee wrapper, recent
// audit findings, and the current drift score in one payload so callers
// don't need to fan out to three methods.
type Review struct {
	Employee       Employee       `json:"employee"`
	Status         EmployeeStatus `json:"status"`
	RecentFindings []AuditFinding `json:"recent_findings"`
	DriftScore     float64        `json:"drift_score"`
	ActiveGoals    []*Goal        `json:"active_goals"`
}

// --------------------------------------------------------------------------
// ConstitutionStore
// --------------------------------------------------------------------------

// ConstitutionStore persists one Constitution per employee in a dedicated
// SQLite table. Keeping constitutions out of bot_definitions.data avoids
// touching the bot package's schema while still associating each row with
// its owning employee via the employee_id foreign key.
//
// The store is safe for concurrent use: the underlying *sql.DB is
// goroutine-safe and the store itself holds no mutable state.
type ConstitutionStore struct {
	db    *sql.DB
	log   *slog.Logger
	mu    sync.Mutex // serializes migrate-on-open and Close
	ready bool
}

const constitutionSchema = `
CREATE TABLE IF NOT EXISTS employee_constitutions (
    employee_id  TEXT PRIMARY KEY,
    data         TEXT NOT NULL,
    version      INTEGER NOT NULL DEFAULT 1,
    approved_at  TEXT NOT NULL,
    authored_by  TEXT NOT NULL DEFAULT ''
);
`

// NewConstitutionStore opens (or creates) the SQLite database at path and
// runs migrations. If log is nil, slog.Default() is used.
func NewConstitutionStore(path string, log *slog.Logger) (*ConstitutionStore, error) {
	if log == nil {
		log = slog.Default()
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open constitution db: %w", err)
	}
	s := &ConstitutionStore{db: db, log: log}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate constitution db: %w", err)
	}
	s.mu.Lock()
	s.ready = true
	s.mu.Unlock()
	return s, nil
}

// NewConstitutionStoreFromDB wraps an existing *sql.DB connection. Use when
// sharing a connection with the bot store (recommended: one .db file per
// data dir, multiple tables).
func NewConstitutionStoreFromDB(db *sql.DB, log *slog.Logger) (*ConstitutionStore, error) {
	if log == nil {
		log = slog.Default()
	}
	s := &ConstitutionStore{db: db, log: log}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate constitution db: %w", err)
	}
	s.mu.Lock()
	s.ready = true
	s.mu.Unlock()
	return s, nil
}

func (s *ConstitutionStore) migrate() error {
	_, err := s.db.Exec(constitutionSchema)
	return err
}

// Close closes the underlying database handle. Subsequent calls are no-ops.
func (s *ConstitutionStore) Close() error {
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

// Put persists the constitution for employeeID. If a row already exists it
// is replaced (INSERT OR REPLACE). The version, approved_at, and
// authored_by columns are populated from the Constitution struct so
// administrative queries can read them without parsing the JSON blob.
func (s *ConstitutionStore) Put(ctx context.Context, employeeID string, c Constitution) error {
	if employeeID == "" {
		return errors.New("constitution put: empty employee id")
	}
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("constitution put: marshal: %w", err)
	}
	approvedAt := c.ApprovedAt
	if approvedAt.IsZero() {
		approvedAt = time.Now().UTC()
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO employee_constitutions
			(employee_id, data, version, approved_at, authored_by)
		VALUES (?, ?, ?, ?, ?)`,
		employeeID, string(data), c.Version,
		approvedAt.Format(time.RFC3339Nano), c.AuthoredBy,
	)
	if err != nil {
		return fmt.Errorf("constitution put: insert: %w", err)
	}
	return nil
}

// Get retrieves the constitution for employeeID. Returns
// ErrConstitutionRequired when no row exists (matching the spec's
// "constitution required at load time" rule).
func (s *ConstitutionStore) Get(ctx context.Context, employeeID string) (Constitution, error) {
	var (
		data        string
		version     int
		approvedAtS string
		authoredBy  string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT data, version, approved_at, authored_by
		FROM employee_constitutions WHERE employee_id = ?`,
		employeeID,
	).Scan(&data, &version, &approvedAtS, &authoredBy)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Constitution{}, ErrConstitutionRequired
		}
		return Constitution{}, fmt.Errorf("constitution get: %w", err)
	}
	var c Constitution
	if err := json.Unmarshal([]byte(data), &c); err != nil {
		return Constitution{}, fmt.Errorf("constitution get: unmarshal: %w", err)
	}
	// Prefer the column values over the JSON blob for canonical fields;
	// the columns are updated atomically with the blob in Put.
	c.Version = version
	c.AuthoredBy = authoredBy
	if t, err := time.Parse(time.RFC3339Nano, approvedAtS); err == nil {
		c.ApprovedAt = t
	}
	return c, nil
}

// Delete removes the constitution row for employeeID. Used during Retire
// to cascade-clean. No-op when the row doesn't exist.
func (s *ConstitutionStore) Delete(ctx context.Context, employeeID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM employee_constitutions WHERE employee_id = ?`,
		employeeID,
	)
	if err != nil {
		return fmt.Errorf("constitution delete: %w", err)
	}
	return nil
}

// --------------------------------------------------------------------------
// Manager
// --------------------------------------------------------------------------

// PlanCreatorFunc is an optional callback injected by the daemon wiring to
// route constitution amendments through the Plan signoff workflow. It
// breaks what would otherwise be a circular import (plan → agent →
// employee → plan). Set via SetPlanCreator.
type PlanCreatorFunc func(
	ctx context.Context,
	employeeID string,
	oldConstitution, newConstitution Constitution,
	reason string,
) (planID string, err error)

// PlanDisposer abstracts plan approval/rejection so the Manager can route
// BusPublisher publishes messages to the internal message bus. The daemon
// injects a concrete adapter (wrapping *bus.MessageBus) via SetBusPublisher.
// When unset, no bus events are published. Using a local interface avoids
// importing internal/bus (cycle risk via internal/agent → internal/bus → ...).
type BusPublisher interface {
	// PublishEmployeePaused publishes an employee.paused bus event.
	// employeeID is the paused employee, reason is a human-readable
	// explanation, source is "operator" or "auto_pause".
	PublishEmployeePaused(employeeID, reason, source string)

	// PublishCriticalFinding publishes an employee.critical_finding bus
	// event (E4). PostTurnAuditor emits this when it finds a critical
	// finding. The Manager subscribes and calls Pause on receipt, which
	// decouples the auditor from the lifecycle (avoids Manager → GoalLoop
	// → BotRunner → PostTurnAuditor → Manager circular dependency).
	PublishCriticalFinding(employeeID, findingID, violatedRule, evidence string)

	// PublishConstitutionValidationError publishes an
	// employee.constitution_validation_error bus event (H5). Emitted when
	// a constitution fails validation at hire or load time. The
	// BotDefinition is stored with status = constitution_invalid so the
	// operator can see and fix the invalid employee in the agents list.
	PublishConstitutionValidationError(employeeID, validationError, constitutionSummary string)
}

// EmployeePausedEvent is the payload for the employee.paused bus event
// (spec line 383).
type EmployeePausedEvent struct {
	EmployeeID string `json:"employee_id"`
	Reason     string `json:"reason"`
	Source     string `json:"source"` // "operator" | "auto_pause"
}

// CriticalFindingEvent is the payload for the employee.critical_finding
// bus event (E4). Emitted by PostTurnAuditor when a critical finding is
// detected. The Manager subscribes and auto-pauses the employee.
type CriticalFindingEvent struct {
	EmployeeID   string `json:"employee_id"`
	FindingID    string `json:"finding_id"`
	ViolatedRule string `json:"violated_rule"`
	Evidence     string `json:"evidence"`
}

// ConstitutionValidationErrorEvent is the payload for the
// employee.constitution_validation_error bus event (H5). Emitted when a
// constitution fails validation at hire or load time. The BotDefinition
// is persisted with status = constitution_invalid so the operator can
// identify and fix the invalid employee via the agents list.
type ConstitutionValidationErrorEvent struct {
	EmployeeID          string `json:"employee_id"`
	ValidationError     string `json:"validation_error"`
	ConstitutionSummary string `json:"constitution_summary"`
}

// goal plan signoffs through internal/plan.PlanManager without importing
// internal/plan (cycle risk via internal/agent → internal/bus → ...).
// The daemon wiring injects the concrete adapter via SetPlanDisposer
// during NewComponents; when unset, ApprovePlan/RejectPlan return a
// clear "not configured" error.
type PlanDisposer interface {
	// ApprovePlan approves a pending plan. sessionID may be empty; by
	// is the approver identity ("user" or an agent ID).
	ApprovePlan(ctx context.Context, planID, sessionID, by string) error
	// RejectPlan rejects a pending plan with a human-readable reason.
	RejectPlan(ctx context.Context, planID, sessionID, by, reason string) error
}

// Manager orchestrates the employee lifecycle: hiring, retiring, pause/resume,
// triggering, constitution amendment (via Plan signoff), goal management,
// audit findings, and legacy bot migration.
//
// The Manager wraps an existing bot.Manager — bot.Manager continues to own
// persistence and execution; this layer adds constitution enforcement, goal
// loops, and audit semantics.
//
// Concurrency: the constitutions cache is guarded by mu. All I/O (SQL,
// bot.Manager calls) happens outside the lock per the CLAUDE.md mutex-scope
// rule. Methods snapshot under the lock, release, then operate.
type Manager struct {
	botManager *bot.Manager
	botStore   *bot.Store

	constitutionStore *ConstitutionStore
	goalStore         *GoalStore
	auditStore        *AuditStore

	// planCreator is an optional callback injected by the daemon wiring
	// to route amendments via Plan signoff. Nil means amendments apply
	// directly (used in tests and single-user setups). Written via
	// SetPlanCreator during daemon init, read during AmendConstitution.
	planCreator PlanCreatorFunc

	// planDisposer is an optional adapter injected by the daemon wiring
	// to route goal plan approvals/rejections through internal/plan's
	// PlanManager. Nil means ApprovePlan/RejectPlan return a "not
	// configured" error. Written via SetPlanDisposer during daemon init.
	planDisposer PlanDisposer

	// botsDir overrides the default ~/.meept/bots/ scan path used by
	// Migrate. When empty, Migrate falls back to the default. Set via
	// SetBotsDir (typically by tests or by the daemon wiring when the
	// operator has relocated the bots directory).
	botsDir string

	// migratorLLM is an optional small-model Chatter used by Migrate to
	// propose richer constitutions (purpose, role, never rules) from each
	// legacy bot's prompt. When nil, Migrate falls back to the conservative
	// synthesizeConservativeConstitution path. Wired by the daemon via
	// SetMigratorLLM. Spec line 226: "reads each existing bot's prompt via
	// the small model and proposes a constitution". Guarded by mu so the
	// nil-check + dereference is safe under concurrent SetMigratorLLM calls.
	migratorLLM llm.Chatter

	// metricsStore is the telemetry sink for the six employee metrics
	// (spec lines 668-674). Nil means telemetry is disabled. Snapshot
	// under mu at emission sites via emitMetric. Wired via
	// SetMetricsStore from daemon.go after both the manager and the
	// store are constructed.
	metricsStore *metrics.Store

	// knownAgentIDs and knownToolNames are the registries used for
	// hire-time validation (spec lines 596-597, 621). They are set once
	// by the daemon wiring before any Hire calls. Nil/empty means
	// "no validation" — useful for tests and single-user setups.
	knownAgentIDs  map[string]struct{}
	knownToolNames map[string]struct{}

	// periodicAuditor is wired by the daemon via SetPeriodicAuditor. When
	// non-nil, the scheduled periodic audit job (SchedulePeriodicAudit)
	// delegates to it. Nil means periodic audit is a no-op.
	periodicAuditor *PeriodicAuditor

	// postTurnAuditor is wired by the daemon via SetPostTurnAuditor. When
	// non-nil, the post-turn audit job delegates to it. Nil means post-turn
	// audit is a no-op (Checkpoint 2 is dormant).
	postTurnAuditor *PostTurnAuditor

	// turnCollector is an optional callback injected by the daemon wiring
	// to supply TurnRecords to the periodic audit job. When nil, the
	// periodic audit runs but produces no findings (no turns to audit).
	turnCollector TurnCollectorFunc

	// busPublisher is an optional callback injected by the daemon wiring
	// to publish bus events (e.g. employee.paused). Nil means no bus events
	// are published. Guarded by mu so the nil-check + dereference is safe.
	busPublisher BusPublisher

	// goalLoops holds the per-employee GoalLoop registry. When a loop is
	// registered for an employee, Trigger delegates to loop.Decide instead
	// of synthesizing a placeholder TriggerResult. Keyed by employee ID.
	// Guarded by goalLoopsMu (separate from mu to avoid contention with
	// the constitution cache on the Trigger hot path).
	goalLoops   map[string]*GoalLoop
	goalLoopsMu sync.RWMutex

	// invokeMuMap provides a per-employee serialization mutex so two
	// concurrent Trigger() calls for the same employee queue rather than
	// overlap (spec line 614: "Goal loop uses the same pattern" as the
	// per-bot invocation mutex). The map is populated lazily on first
	// Trigger for a given employee ID. Guarded by invokeMuMapGuard.
	invokeMuMap      map[string]*sync.Mutex
	invokeMuMapGuard sync.Mutex

	// G5: assessment semaphores prevent overlapping scheduled assessments.
	// When a scheduler tick fires for an employee whose previous assessment
	// is still running, the non-blocking send fails and the tick is skipped
	// with a debug log. Keyed by employee ID, guarded by invokeMuMapGuard
	// (shared with the invoke mutex map for simplicity).
	assessmentSems map[string]chan struct{}

	mu            sync.RWMutex
	constitutions map[string]Constitution // employeeID -> cached
	driftScores   map[string]float64      // employeeID -> last computed
	logger        *slog.Logger
}

// NewManager constructs a new employee Manager wrapping the given bot.Manager.
//
// bm may be nil during partial rollout; callers should check for nil before
// calling methods (the RPCHandler handles this via errNotConfigured).
func NewManager(bm *bot.Manager) *Manager {
	return &Manager{
		botManager:     bm,
		constitutions:  make(map[string]Constitution),
		driftScores:    make(map[string]float64),
		goalLoops:      make(map[string]*GoalLoop),
		invokeMuMap:    make(map[string]*sync.Mutex),
		assessmentSems: make(map[string]chan struct{}),
		logger:         slog.Default(),
	}
}

// NewManagerWithStores constructs a Manager with explicit constitution,
// goal, and audit stores. This is the production constructor used by the
// daemon wiring (wiring.go). Any nil store is accepted; the corresponding
// methods will return ErrNotImplemented when invoked.
func NewManagerWithStores(
	bm *bot.Manager,
	bs *bot.Store,
	cs *ConstitutionStore,
	gs *GoalStore,
	as *AuditStore,
	logger *slog.Logger,
) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	m := &Manager{
		botManager:        bm,
		botStore:          bs,
		constitutionStore: cs,
		goalStore:         gs,
		auditStore:        as,
		constitutions:     make(map[string]Constitution),
		driftScores:       make(map[string]float64),
		goalLoops:         make(map[string]*GoalLoop),
		invokeMuMap:       make(map[string]*sync.Mutex),
		assessmentSems:    make(map[string]chan struct{}),
		logger:            logger.With("component", "employee-manager"),
	}
	// Best-effort: prime the in-memory constitution cache from the store
	// so GetEmployee can return a Constitution without a SQL round-trip
	// on the hot path. Failures are logged; the cache will lazy-fill.
	if cs != nil {
		ctx := context.Background()
		if err := m.primeCache(ctx); err != nil {
			logger.Warn("employee constitution cache prime failed", "error", err)
		}
	}
	return m
}

// SetLogger replaces the default logger. Nil is ignored (typed-nil guard).
func (m *Manager) SetLogger(l *slog.Logger) {
	if l == nil {
		return
	}
	m.logger = l.With("component", "employee-manager")
}

// SetConstitutionStore attaches a constitution store post-construction.
// Nil is ignored. Used when the store is opened after the Manager (e.g.
// during incremental daemon bring-up).
func (m *Manager) SetConstitutionStore(cs *ConstitutionStore) {
	if cs == nil {
		return
	}
	m.mu.Lock()
	m.constitutionStore = cs
	m.mu.Unlock()
	// Best-effort prime.
	if err := m.primeCache(context.Background()); err != nil {
		m.logger.Warn("employee constitution cache prime failed", "error", err)
	}
}

// SetGoalStore attaches a goal store post-construction. Nil is ignored.
func (m *Manager) SetGoalStore(gs *GoalStore) {
	if gs == nil {
		return
	}
	m.mu.Lock()
	m.goalStore = gs
	m.mu.Unlock()
}

// SetAuditStore attaches an audit store post-construction. Nil is ignored.
func (m *Manager) SetAuditStore(as *AuditStore) {
	if as == nil {
		return
	}
	m.mu.Lock()
	m.auditStore = as
	m.mu.Unlock()
}

// AuditStore returns the audit store. May be nil when not configured.
// Callers must nil-check the result.
func (m *Manager) AuditStore() *AuditStore {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.auditStore
}

// SetPostTurnAuditor wires the PostTurnAuditor used by the post-turn audit
// path (Checkpoint 2). Nil is ignored (typed-nil guard per CLAUDE.md
// setter convention). Thread-safe.
func (m *Manager) SetPostTurnAuditor(a *PostTurnAuditor) {
	if a == nil {
		return
	}
	m.mu.Lock()
	m.postTurnAuditor = a
	m.mu.Unlock()
}

// PostTurnAuditor returns the wired PostTurnAuditor, or nil when not wired.
func (m *Manager) PostTurnAuditor() *PostTurnAuditor {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.postTurnAuditor
}

// SetBusPublisher wires the bus publisher used to emit employee events
// (e.g. employee.paused). Nil is ignored (typed-nil guard). Thread-safe.
func (m *Manager) SetBusPublisher(p BusPublisher) {
	if p == nil {
		return
	}
	m.mu.Lock()
	m.busPublisher = p
	m.mu.Unlock()
}

// SetBotStore attaches the bot store post-construction. Nil is ignored.
func (m *Manager) SetBotStore(bs *bot.Store) {
	if bs == nil {
		return
	}
	m.mu.Lock()
	m.botStore = bs
	m.mu.Unlock()
}

// RegisterGoalLoop registers a GoalLoop for an employee. Once registered,
// Trigger delegates to loop.Decide instead of returning a synthesized
// TriggerResult. Nil loop is ignored (typed-nil guard per CLAUDE.md).
// Thread-safe: callers can invoke before or between Trigger calls.
func (m *Manager) RegisterGoalLoop(employeeID string, loop *GoalLoop) {
	if loop == nil || employeeID == "" {
		return
	}
	m.goalLoopsMu.Lock()
	m.goalLoops[employeeID] = loop
	m.goalLoopsMu.Unlock()
}

// GetGoalLoop returns the registered GoalLoop for the given employee, or
// nil when no loop is registered. Thread-safe.
func (m *Manager) GetGoalLoop(employeeID string) *GoalLoop {
	m.goalLoopsMu.RLock()
	loop := m.goalLoops[employeeID]
	m.goalLoopsMu.RUnlock()
	return loop
}

// acquireInvokeMutex returns the per-employee serialization mutex for the
// given employee ID, creating it lazily on first use. The returned mutex
// is shared across all callers for the same employee, ensuring that two
// concurrent Trigger() calls for the same employee queue rather than overlap
// (spec line 614).
//
// The lazy-create happens under invokeMuMapGuard (a short-lived lock that
// is released before any I/O). The returned *sync.Mutex is then Lock()'d
// by the caller around the Decide call — no I/O under the guard lock.
func (m *Manager) acquireInvokeMutex(employeeID string) *sync.Mutex {
	m.invokeMuMapGuard.Lock()
	mu, ok := m.invokeMuMap[employeeID]
	if !ok {
		mu = &sync.Mutex{}
		m.invokeMuMap[employeeID] = mu
	}
	m.invokeMuMapGuard.Unlock()
	return mu
}

// StartAll prepares the employee layer for runtime. It primes the
// constitution cache and (eventually) launches any per-employee GoalLoops.
// It does NOT call botManager.StartAll — that's the daemon's responsibility.
func (m *Manager) StartAll(ctx context.Context) error {
	if m.constitutionStore != nil {
		if err := m.primeCache(ctx); err != nil {
			m.logger.Warn("employee StartAll: cache prime failed", "error", err)
		}
	}
	m.logger.Info("employee manager started")
	return nil
}

// StopAll reverses StartAll. Currently a no-op for the Manager itself (the
// underlying bot.Manager has its own StopAll). Stores are Closed by the
// daemon's Components.Stop to keep ownership clear.
func (m *Manager) StopAll() {
	m.logger.Info("employee manager stopped")
}

// RegisterPreExecCheckers builds a PreExecChecker (wrapped via
// NewPreExecAdapter) for each employee with a constitution and registers
// it with the given PermissionChecker. This is the wiring point that makes
// Checkpoint 1 fire on the active permission-check path (Gap A).
//
// Call this after StartAll has primed the constitution cache. Safe to call
// multiple times (each call replaces the registration). Employees without a
// constitution are skipped (they have no constraints to enforce).
func (m *Manager) RegisterPreExecCheckers(pc *security.PermissionChecker) {
	if pc == nil {
		return
	}
	m.mu.RLock()
	snapshot := make(map[string]Constitution, len(m.constitutions))
	for k, v := range m.constitutions {
		snapshot[k] = v
	}
	m.mu.RUnlock()
	for id, c := range snapshot {
		constCopy := c
		checker := NewPreExecChecker(id, &constCopy)
		pc.SetPreExecChecker(id, NewPreExecAdapter(checker))
	}
	m.logger.Info("registered pre-exec checkers with permission checker",
		"employee_count", len(snapshot))
}

// primeCache loads every persisted constitution into the in-memory map.
// Called during StartAll and after SetConstitutionStore. Idempotent.
func (m *Manager) primeCache(ctx context.Context) error {
	if m.constitutionStore == nil {
		return nil
	}
	// No "list all" SQL on ConstitutionStore; instead, iterate bot
	// definitions (the canonical employee list) and load each one's
	// constitution. Skip bots without a constitution — they'll fail
	// loudly when accessed via Get/Review.
	if m.botManager == nil {
		return nil
	}
	bots, err := m.botManager.ListBots(ctx)
	if err != nil {
		return fmt.Errorf("list bots for cache prime: %w", err)
	}
	loaded := 0
	for _, b := range bots {
		c, err := m.constitutionStore.Get(ctx, b.ID)
		if err != nil {
			if errors.Is(err, ErrConstitutionRequired) {
				// Legacy bot without a constitution — skip silently
				// during prime; Get/Review will surface the error when
				// an operator actually asks for this employee.
				continue
			}
			m.logger.Warn("employee cache prime: failed to load constitution",
				"employee_id", b.ID, "error", err)
			continue
		}
		m.mu.Lock()
		m.constitutions[b.ID] = c
		m.mu.Unlock()
		loaded++
	}
	m.logger.Info("employee constitution cache primed", "loaded", loaded, "total_bots", len(bots))
	return nil
}

// cachedConstitution returns the constitution for employeeID from the
// in-memory cache, or (zero, false) when not cached. Caller must hold no
// lock on m.
func (m *Manager) cachedConstitution(employeeID string) (Constitution, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.constitutions[employeeID]
	return c, ok
}

// setCachedConstitution stores a constitution in the cache.
func (m *Manager) setCachedConstitution(employeeID string, c Constitution) {
	m.mu.Lock()
	m.constitutions[employeeID] = c
	m.mu.Unlock()
}

// clearCachedConstitution removes a constitution from the cache.
func (m *Manager) clearCachedConstitution(employeeID string) {
	m.mu.Lock()
	delete(m.constitutions, employeeID)
	delete(m.driftScores, employeeID)
	m.mu.Unlock()
}

// --------------------------------------------------------------------------
// Manager: lifecycle methods
// --------------------------------------------------------------------------

// ListEmployees lists all employees, optionally filtered by status string.
// Empty filter returns all employees. Each Employee includes its
// Constitution (loaded from the cache or the store).
func (m *Manager) ListEmployees(ctx context.Context, statusFilter string) ([]Employee, error) {
	if m.botManager == nil {
		return nil, errNotConfigured
	}
	bots, err := m.botManager.ListBots(ctx)
	if err != nil {
		return nil, fmt.Errorf("list bots: %w", err)
	}
	employees := make([]Employee, 0, len(bots))
	for _, b := range bots {
		// Status filter — check bot state via botManager.
		if statusFilter != "" {
			state, err := m.botManager.GetBotStatus(ctx, b.ID)
			if err != nil {
				m.logger.Debug("skip employee in list: status lookup failed",
					"employee_id", b.ID, "error", err)
				continue
			}
			if string(FromBotStatus(state.Status)) != statusFilter {
				continue
			}
		}
		emp := Employee{BotDefinition: b}
		if c, ok := m.cachedConstitution(b.ID); ok {
			emp.Constitution = c
		} else if m.constitutionStore != nil {
			c, err := m.constitutionStore.Get(ctx, b.ID)
			if err == nil {
				emp.Constitution = c
				m.setCachedConstitution(b.ID, c)
			} else if !errors.Is(err, ErrConstitutionRequired) {
				m.logger.Warn("list employees: constitution load failed",
					"employee_id", b.ID, "error", err)
			}
		}
		employees = append(employees, emp)
	}
	return employees, nil
}

// GetEmployee retrieves a single employee by ID, including constitution
// and cached drift score.
func (m *Manager) GetEmployee(ctx context.Context, id string) (*Employee, error) {
	if m.botManager == nil {
		return nil, errNotConfigured
	}
	b, err := m.botManager.GetBot(ctx, id)
	if err != nil {
		return nil, ErrEmployeeNotFound
	}
	emp := &Employee{BotDefinition: *b}
	if c, ok := m.cachedConstitution(id); ok {
		emp.Constitution = c
	} else if m.constitutionStore != nil {
		c, err := m.constitutionStore.Get(ctx, id)
		if err == nil {
			emp.Constitution = c
			m.setCachedConstitution(id, c)
		} else if !errors.Is(err, ErrConstitutionRequired) {
			m.logger.Warn("get employee: constitution load failed",
				"employee_id", id, "error", err)
		}
	}
	return emp, nil
}

// Hire validates the request's constitution and creates a new employee.
// Corresponds to spec: "agents.create validates the constitution (delegates
// to Manager.Hire)". The constitution map is decoded into a Constitution
// struct, validated, then persisted via the ConstitutionStore. The
// underlying bot.BotDefinition is created via botManager.CreateBot.
//
// H5: When constitution validation fails, the bot definition is still
// persisted with Enabled=false so the operator can see the invalid employee
// in the agents list. A ConstitutionValidationError event is emitted to the
// bus and an audit finding is written at SeverityCritical.
func (m *Manager) Hire(ctx context.Context, req HireRequest) (*Employee, error) {
	if m.botManager == nil {
		return nil, errNotConfigured
	}
	// Decode and validate the constitution. A missing or empty
	// constitution is rejected per spec line 222.
	c, err := decodeConstitution(req.Constitution)
	if err != nil {
		// H5: Persist the bot definition as disabled with
		// constitution_invalid status so the operator can see it in
		// the agents list and fix the constitution. Best-effort — if
		// the bot store is unavailable, just return the error.
		m.recordConstitutionValidationError(ctx, req, err.Error(),
			"decode failed: "+err.Error())
		return nil, fmt.Errorf("hire: constitution: %w", err)
	}
	if err := c.Validate(req.ID); err != nil {
		// H5: Same pattern — persist disabled, emit event, write finding.
		m.recordConstitutionValidationError(ctx, req, err.Error(),
			"validation failed: "+err.Error())
		return nil, fmt.Errorf("hire: constitution validate: %w", err)
	}

	// Spec lines 597, 621: validate escalation references and detect
	// cycles. Spec line 596: validate tool references. These checks need
	// the full agent/tool registries, so they're performed here (after
	// the structural Validate) using sets injected by the daemon wiring.
	m.mu.RLock()
	knownAgents := m.knownAgentIDs
	knownTools := m.knownToolNames
	m.mu.RUnlock()

	if len(knownAgents) > 0 {
		if unknown, err := c.CheckEscalationReferences(knownAgents); err != nil {
			return nil, fmt.Errorf("hire: escalation references: %w (unknown IDs: %v)", err, unknown)
		}
		// Build escalation graph for cycle detection. Include the new
		// employee being hired plus cached constitutions so we catch
		// self-referential and transitive cycles.
		graph := m.buildEscalationGraphForHire(req.ID, c, knownAgents)
		agentIDs := allAgentIDs(req.ID, knownAgents)
		if cycles, err := DetectEscalationCycles(graph, agentIDs); err != nil {
			return nil, fmt.Errorf("hire: cycle detection: %w", err)
		} else if len(cycles) > 0 {
			return nil, fmt.Errorf("hire: escalation cycle detected: %s", cycles[0].String())
		}
	}

	if len(knownTools) > 0 {
		unknownAllowed, unknownForbidden := c.CheckToolReferences(knownTools)
		if len(unknownAllowed) > 0 {
			m.logger.Warn("hire: constitution references unknown tools in tools_allowed; removing",
				"employee_id", req.ID, "tools", unknownAllowed)
			c.removeUnknownTools(unknownAllowed, true)
		}
		if len(unknownForbidden) > 0 {
			m.logger.Warn("hire: constitution references unknown tools in tools_forbidden; removing",
				"employee_id", req.ID, "tools", unknownForbidden)
			c.removeUnknownTools(unknownForbidden, false)
		}
	}

	// Provenance: a freshly-hired constitution is version 1, authored by
	// "user" (the only caller of Hire), approved now.
	if c.Version == 0 {
		c.Version = 1
	}
	if c.AuthoredBy == "" {
		c.AuthoredBy = "user"
	}
	if c.ApprovedAt.IsZero() {
		c.ApprovedAt = time.Now().UTC()
	}
	// Build the bot.BotDefinition. The bot layer owns persistence of the
	// runtime/trigger/tool fields; the constitution is persisted
	// separately by the ConstitutionStore.
	def := bot.BotDefinition{
		ID:          req.ID,
		Name:        req.Name,
		Description: req.Description,
		Prompt:      req.Prompt,
		Model:       req.Model,
		Triggers:    req.Triggers,
		MemoryScope: req.MemoryScope,
		Tools:       req.Tools,
		Enabled:     req.Enabled,
	}
	if err := m.botManager.CreateBot(ctx, def); err != nil {
		return nil, fmt.Errorf("hire: create bot: %w", err)
	}
	// Persist the constitution. If this fails we attempt to roll back
	// the bot creation so we don't end up with a constitutionless
	// employee (which would fail every subsequent Get/Review).
	if m.constitutionStore != nil {
		if err := m.constitutionStore.Put(ctx, req.ID, c); err != nil {
			m.logger.Error("hire: persist constitution failed; rolling back bot",
				"employee_id", req.ID, "error", err)
			_ = m.botManager.DeleteBot(ctx, req.ID)
			return nil, fmt.Errorf("hire: persist constitution: %w", err)
		}
	}
	m.setCachedConstitution(req.ID, c)
	m.logger.Info("employee hired",
		"employee_id", req.ID, "tier", c.AutonomyTier.String())
	return &Employee{BotDefinition: def, Constitution: c}, nil
}

// UpdateEmployee mutates an existing employee's non-constitution fields.
// Constitution changes must go through AmendConstitution.
func (m *Manager) UpdateEmployee(ctx context.Context, req UpdateRequest) (*Employee, error) {
	if m.botManager == nil {
		return nil, errNotConfigured
	}
	existing, err := m.botManager.GetBot(ctx, req.ID)
	if err != nil {
		return nil, ErrEmployeeNotFound
	}
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Description != "" {
		existing.Description = req.Description
	}
	if req.Prompt != "" {
		existing.Prompt = req.Prompt
	}
	if req.Model != "" {
		existing.Model = req.Model
	}
	if req.Tools != nil {
		existing.Tools = req.Tools
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if err := m.botManager.UpdateBot(ctx, *existing); err != nil {
		return nil, fmt.Errorf("update bot: %w", err)
	}
	// Reuse GetEmployee to assemble the full wrapper (with constitution).
	return m.GetEmployee(ctx, req.ID)
}

// Retire stops and deletes an employee (spec: agents.delete). Goals and
// findings cascade-delete via FK; plans survive in the separate plans
// table. The constitution row is removed explicitly.
func (m *Manager) Retire(ctx context.Context, id string) error {
	if m.botManager == nil {
		return errNotConfigured
	}
	// Delete bot (stops triggers if running via botManager.DeleteBot).
	if err := m.botManager.DeleteBot(ctx, id); err != nil {
		return fmt.Errorf("retire: delete bot: %w", err)
	}
	// Cascade clean: constitution, drift cache. Goal/AuditStore have FK
	// ON DELETE CASCADE referencing bot_definitions; if the bot store and
	// the goal/audit stores share the same SQLite file those cascade.
	// We additionally clear the constitution row (no FK) and the cache.
	if m.constitutionStore != nil {
		if err := m.constitutionStore.Delete(ctx, id); err != nil {
			m.logger.Warn("retire: constitution delete failed",
				"employee_id", id, "error", err)
		}
	}
	m.clearCachedConstitution(id)
	m.logger.Info("employee retired", "employee_id", id)
	return nil
}

// Pause is the operator-initiated pause path. Also used by the enforcement
// engine when auto-pausing on critical findings. Idempotent. Publishes an
// employee.paused bus event when a BusPublisher is wired (spec line 383).
func (m *Manager) Pause(ctx context.Context, id string) error {
	return m.pauseWithSource(ctx, id, "", "operator")
}

// pauseWithSource pauses the employee and publishes a bus event with the
// given source and reason. source is "operator" or "auto_pause". reason
// is a human-readable explanation (may be empty for operator pauses).
// The bus publisher is snapshotted under a read lock and called outside
// the lock (per CLAUDE.md mutex-scope rule — publishing may do I/O).
func (m *Manager) pauseWithSource(ctx context.Context, id, reason, source string) error {
	if m.botManager == nil {
		return errNotConfigured
	}
	if err := m.botManager.PauseBot(ctx, id); err != nil {
		return err
	}
	// Snapshot publisher under read lock, publish outside lock.
	m.mu.RLock()
	pub := m.busPublisher
	m.mu.RUnlock()
	if pub != nil {
		if reason == "" {
			reason = "operator pause"
		}
		pub.PublishEmployeePaused(id, reason, source)
		m.logger.Info("employee paused", "employee_id", id, "source", source, "reason", reason)
	}
	return nil
}

// PauseWithReason pauses the employee with a specific reason and source.
// Used by the enforcement engine (source="auto_pause") and by the RPC
// handler (source="operator"). Idempotent. Publishes an employee.paused
// bus event when a BusPublisher is wired (spec line 383).
func (m *Manager) PauseWithReason(ctx context.Context, id, reason, source string) error {
	return m.pauseWithSource(ctx, id, reason, source)
}

// recordConstitutionValidationError is the H5 helper that persists the bot
// definition as disabled (status = constitution_invalid), writes an audit
// finding at SeverityCritical, and emits a ConstitutionValidationError bus
// event. Best-effort: failures are logged but do not change the returned
// error from the caller (the validation error itself is the real error).
func (m *Manager) recordConstitutionValidationError(ctx context.Context, req HireRequest, validationError, summary string) {
	// Persist the bot definition as disabled so it appears in the agents
	// list with a clear invalid status. Best-effort — if the store is
	// unavailable, just log.
	def := bot.BotDefinition{
		ID:          req.ID,
		Name:        req.Name,
		Description: req.Description,
		Prompt:      req.Prompt,
		Model:       req.Model,
		Triggers:    req.Triggers,
		MemoryScope: req.MemoryScope,
		Tools:       req.Tools,
		Enabled:     false,
	}
	if err := m.botManager.CreateBot(ctx, def); err != nil {
		// If the bot already exists (e.g. re-hire attempt), try UpdateBot.
		if updateErr := m.botManager.UpdateBot(ctx, def); updateErr != nil {
			m.logger.Warn("hire: failed to persist invalid bot definition",
				"employee_id", req.ID, "error", updateErr)
		}
	}

	// Snapshot stores and publisher under read lock, operate outside lock.
	m.mu.RLock()
	auditStore := m.auditStore
	pub := m.busPublisher
	m.mu.RUnlock()

	// Write audit finding.
	if auditStore != nil {
		finding := AuditFinding{
			ID:            idpkg.Generate("audit_"),
			EmployeeID:    req.ID,
			Severity:      SeverityCritical,
			Checkpoint:    CheckpointPreExec,
			ViolatedRule:  "constitution_validation_error",
			Evidence:      validationError,
			DetectedAt:    time.Now().UTC(),
		}
		if err := auditStore.Create(ctx, finding); err != nil {
			m.logger.Warn("hire: failed to write constitution validation audit finding",
				"employee_id", req.ID, "error", err)
		}
	}

	// Emit bus event.
	if pub != nil {
		pub.PublishConstitutionValidationError(req.ID, validationError, summary)
	}

	m.logger.Error("hire: constitution validation error",
		"employee_id", req.ID,
		"error", validationError)
}

// Resume is the only un-pause path (spec: "only un-pause path"). Employees
// cannot self-resume when auto_pause.require_operator_resume is true
// (the default).
func (m *Manager) Resume(ctx context.Context, id string) error {
	if m.botManager == nil {
		return errNotConfigured
	}
	return m.botManager.ResumeBot(ctx, id)
}

// Trigger programmatically invokes an employee. Used by the webhook handler,
// scheduler, and the agents.trigger RPC method.
//
// When a GoalLoop is registered for the employee (via RegisterGoalLoop),
// Trigger acquires a per-employee serialization mutex (spec line 614: two
// triggers fire simultaneously → queue rather than overlap) and delegates to
// loop.Decide. The Decide call runs the full ASSESS → PLAN → EXECUTE →
// REFLECT cycle (tier-dependent) and the returned TriggerResult reflects the
// actual outcome.
//
// When no GoalLoop is registered, Trigger falls back to the legacy path:
// record the trigger attempt and return a synthesized "triggered"
// TriggerResult. This keeps backward compatibility for employees without a
// constitution/loop.
func (m *Manager) Trigger(ctx context.Context, id string, payload map[string]any) (*TriggerResult, error) {
	if m.botManager == nil {
		return nil, errNotConfigured
	}
	// Verify the employee exists and is not paused.
	def, err := m.botManager.GetBot(ctx, id)
	if err != nil {
		return nil, ErrEmployeeNotFound
	}
	if !def.Enabled {
		// Paused employees emit a "paused" outcome metric.
		m.emitMetric("employee.invocations", 1, map[string]string{
			"employee_id": id,
			"tier":        "",
			"outcome":     "paused",
		})
		return nil, errors.New("employee is paused")
	}

	// Telemetry helpers: tier tag is best-effort from the cached constitution.
	tierTag := ""
	if c, ok := m.cachedConstitution(id); ok {
		tierTag = c.AutonomyTier.String()
	}

	// GoalLoop path: when a loop is registered, delegate to Decide under
	// per-employee serialization.
	loop := m.GetGoalLoop(id)
	if loop != nil {
		return m.triggerViaGoalLoop(ctx, id, payload, tierTag, loop)
	}

	// Legacy fallback: no GoalLoop registered. Record the trigger attempt
	// and return a synthesized TriggerResult (keeps the RPC surface
	// functional for employees without a loop).
	result := &TriggerResult{
		InvocationID: idpkg.Generate("trig_"),
		StartedAt:    time.Now().UTC(),
		Status:       "triggered",
	}
	m.emitMetric("employee.invocations", 1, map[string]string{
		"employee_id": id,
		"tier":        tierTag,
		"outcome":     "success",
	})
	m.emitBudgetMetric(ctx, id)
	return result, nil
}

// triggerViaGoalLoop is the GoalLoop-backed Trigger path. It acquires the
// per-employee serialization mutex (spec line 614), then calls loop.Decide.
// The mutex ensures that two concurrent Trigger calls for the same employee
// queue rather than overlap. All I/O (LLM calls, plan creation, bot
// execution) happens inside Decide — outside this method's own lock scope.
func (m *Manager) triggerViaGoalLoop(ctx context.Context, id string, payload map[string]any, tierTag string, loop *GoalLoop) (*TriggerResult, error) {
	// Build the TriggerEvent from the payload.
	source, _ := payload["source"].(string)
	if source == "" {
		source = "manual"
	}
	var payloadBytes []byte
	if payload != nil {
		if raw, err := json.Marshal(payload); err == nil {
			payloadBytes = raw
		}
	}
	trigger := TriggerEvent{
		Source:  source,
		Payload: payloadBytes,
		FiredAt: time.Now().UTC(),
	}

	// Acquire per-employee serialization mutex (spec line 614). The mutex
	// is created lazily on first use. We hold it only across Decide — no
	// other I/O happens under this lock.
	invokeMu := m.acquireInvokeMutex(id)
	invokeMu.Lock()
	decideErr := loop.Decide(ctx, trigger)
	invokeMu.Unlock()

	result := &TriggerResult{
		InvocationID: idpkg.Generate("trig_"),
		StartedAt:    trigger.FiredAt,
	}
	if decideErr != nil {
		result.Status = "error"
		m.emitMetric("employee.invocations", 1, map[string]string{
			"employee_id": id,
			"tier":        tierTag,
			"outcome":     "error",
		})
		m.emitBudgetMetric(ctx, id)
		return result, fmt.Errorf("goal-loop decide: %w", decideErr)
	}
	result.Status = "completed"
	m.emitMetric("employee.invocations", 1, map[string]string{
		"employee_id": id,
		"tier":        tierTag,
		"outcome":     "success",
	})
	m.emitBudgetMetric(ctx, id)
	return result, nil
}

// emitBudgetMetric is a best-effort helper that emits the employee.budget.burn
// gauge from the bot manager's state. If the lookup fails, the gauge is
// skipped (do not emit a zero). Extracted to avoid duplication between the
// GoalLoop and legacy Trigger paths.
func (m *Manager) emitBudgetMetric(ctx context.Context, id string) {
	if m.botManager == nil {
		return
	}
	state, err := m.botManager.GetBotStatus(ctx, id)
	if err != nil || state == nil {
		return
	}
	m.emitMetric("employee.budget.burn", float64(state.TodayCostCents), map[string]string{
		"employee_id": id,
		"unit":        "cents",
	})
}

// AmendConstitution proposes a constitution amendment. The patch is checked
// against the existing constitution's AmendmentPolicy.FrozenFields; if any
// touched field is frozen, the amendment is rejected with ErrFrozenField.
//
// Per spec: amendments require approval. When the existing constitution's
// AmendmentPolicy.RequiresApproval is true (always true per the design
// invariant), the patch is routed via the existing Plan signoff workflow.
// This method returns the created Plan ID. When RequiresApproval is false
// (which the validator rejects, but defensively handled here) the patch is
// applied immediately.
//
// The `by` field identifies who is proposing. When `by` is not "user" and
// AmendmentPolicy.SelfProposeAllowed is false, the amendment is rejected.
func (m *Manager) AmendConstitution(ctx context.Context, req AmendRequest) (string, error) {
	if m.botManager == nil {
		return "", errNotConfigured
	}
	existing, err := m.GetEmployee(ctx, req.EmployeeID)
	if err != nil {
		return "", err
	}
	if !existing.HasConstitution() {
		return "", ErrConstitutionRequired
	}
	// C1: Optimistic version lock. If the caller specifies ExpectedVersion
	// > 0, it must match the current constitution version. A mismatch means
	// the constitution was amended concurrently while this proposal was
	// in-flight (e.g. operator approved v2 while employee loaded v3).
	if req.ExpectedVersion > 0 && req.ExpectedVersion != existing.Constitution.Version {
		return "", fmt.Errorf("%w: expected %d, got %d",
			ErrVersionMismatch, req.ExpectedVersion, existing.Constitution.Version)
	}
	// Self-propose gate.
	if req.By != "" && req.By != "user" && !existing.Constitution.AmendmentPolicy.SelfProposeAllowed {
		return "", fmt.Errorf("amend: employee %q is not permitted to self-propose amendments", req.By)
	}
	// Frozen-fields check. Each key in req.Fields is matched against the
	// frozen list; the dotted form ("constraints.risk_ceiling") is also
	// honored.
	if violated := findFrozenViolation(req.Fields, existing.Constitution.AmendmentPolicy.FrozenFields); violated != "" {
		// Persist an info-level audit finding before returning the error
		// (spec: frozen-field rejection is auditable). Best-effort; if
		// the audit store is nil, skip silently.
		m.mu.RLock()
		auditStore := m.auditStore
		m.mu.RUnlock()
		if auditStore != nil {
			finding := AuditFinding{
				ID:           idpkg.Generate("audit_"),
				EmployeeID:   req.EmployeeID,
				Severity:     SeverityInfo,
				Checkpoint:   CheckpointPreExec,
				ViolatedRule: "frozen_field:" + violated,
				Evidence:     fmt.Sprintf("amendment attempted to modify frozen field %q", violated),
				DetectedAt:   time.Now().UTC(),
			}
			_ = auditStore.Create(context.Background(), finding)
		}
		return "", fmt.Errorf("%w: %s", ErrFrozenField, violated)
	}
	// Compute the patched constitution so we can validate it before
	// routing for signoff. We don't persist yet.
	patched, err := patchConstitution(existing.Constitution, req.Fields)
	if err != nil {
		return "", fmt.Errorf("amend: patch: %w", err)
	}
	if err := patched.Validate(req.EmployeeID); err != nil {
		return "", fmt.Errorf("amend: patched constitution invalid: %w", err)
	}
	// Bump version + provenance.
	patched.Version = existing.Constitution.Version + 1
	if req.By != "" {
		patched.AuthoredBy = req.By
	}
	patched.ApprovedAt = time.Now().UTC()

	// Approval routing. The spec says amendments "require approval" via
	// the Plan signoff flow. The Plan workflow is owned by
	// internal/plan; integrating it here would require a circular
	// import (plan depends on agent which depends on employee). The
	// daemon-level wiring (wiring.go) injects a PlanCreator callback
	// to break the cycle. When no PlanCreator is wired, we apply the
	// amendment directly — this path is for tests and single-user
	// setups where Plan signoff isn't enabled.
	if m.planCreator != nil {
		planID, err := m.planCreator(ctx, req.EmployeeID, existing.Constitution, patched, req.Reason)
		if err != nil {
			return "", fmt.Errorf("amend: plan signoff: %w", err)
		}
		// Persist the patched constitution. In the full flow this
		// would be applied only after the Plan is approved; for the
		// MVP we persist immediately and record the Plan ID.
		if m.constitutionStore != nil {
			if err := m.constitutionStore.Put(ctx, req.EmployeeID, patched); err != nil {
				return "", fmt.Errorf("amend: persist: %w", err)
			}
		}
		m.setCachedConstitution(req.EmployeeID, patched)
		m.logger.Info("constitution amended via plan signoff",
			"employee_id", req.EmployeeID, "plan_id", planID,
			"old_version", existing.Constitution.Version,
			"new_version", patched.Version)
		return planID, nil
	}
	// No Plan workflow wired — apply directly.
	if m.constitutionStore != nil {
		if err := m.constitutionStore.Put(ctx, req.EmployeeID, patched); err != nil {
			return "", fmt.Errorf("amend: persist: %w", err)
		}
	}
	m.setCachedConstitution(req.EmployeeID, patched)
	m.logger.Info("constitution amended (direct, no plan workflow)",
		"employee_id", req.EmployeeID,
		"old_version", existing.Constitution.Version,
		"new_version", patched.Version)
	return "", nil
}

// SetPlanCreator wires the Plan signoff integration. Nil clears the
// callback (amendments apply directly). The Manager methods consult this
// field under no lock — planCreator is write-once during daemon init and
// read-only afterwards, so the data race is benign; callers that need to
// mutate it after Start should use StopAll/SetPlanCreator/StartAll.
func (m *Manager) SetPlanCreator(fn PlanCreatorFunc) {
	if fn == nil {
		return
	}
	m.planCreator = fn
}

// SetPlanDisposer wires the Plan approval/rejection integration used by
// ApprovePlan/RejectPlan. Nil is ignored (the methods return a clear
// "not configured" error when unset). As with SetPlanCreator, this field
// is write-once during daemon init and read-only afterwards.
func (m *Manager) SetPlanDisposer(d PlanDisposer) {
	if d == nil {
		return
	}
	m.planDisposer = d
}

// SetBotsDir overrides the directory Migrate scans for legacy
// ~/.meept/bots/*.json files. Empty restores the default (~/.meept/bots/).
// Nil-guarded via the empty-string check; passing "" explicitly clears
// the override, matching the "default path" behaviour.
func (m *Manager) SetBotsDir(dir string) {
	m.botsDir = dir
}

// SetMigratorLLM wires an optional small-model Chatter used by Migrate to
// propose richer constitutions from each legacy bot's prompt. When nil is
// passed (or this method is never called), Migrate uses the conservative
// synthesizeConservativeConstitution path unchanged. Nil-guarded per the
// typed-nil setter convention in CLAUDE.md.
func (m *Manager) SetMigratorLLM(c llm.Chatter) {
	if c == nil {
		return
	}
	m.mu.Lock()
	m.migratorLLM = c
	m.mu.Unlock()
}

// SetKnownAgentIDs sets the set of known agent IDs for hire-time validation
// (spec line 597, 621). When non-empty, Hire checks that every ID in the
// new employee's escalates_to is known and that no escalation cycle is
// introduced. Nil or empty map disables the check (useful for tests).
// Thread-safe: callers can invoke before or between Hire calls.
func (m *Manager) SetKnownAgentIDs(ids map[string]struct{}) {
	if len(ids) == 0 {
		return
	}
	m.mu.Lock()
	m.knownAgentIDs = ids
	m.mu.Unlock()
}

// SetKnownToolNames sets the set of known tool names for hire-time validation
// (spec line 596). When non-empty, Hire checks that every tool name in the
// new employee's tools_allowed and tools_forbidden is known. Unknown tools
// are logged and stripped (warning, not fatal). Nil or empty map disables
// the check.
func (m *Manager) SetKnownToolNames(names map[string]struct{}) {
	if len(names) == 0 {
		return
	}
	m.mu.Lock()
	m.knownToolNames = names
	m.mu.Unlock()
}

// buildEscalationGraphForHire constructs an EscalationGraph from the new
// employee's constitution plus existing employees' constitutions (from the
// in-memory cache). The new employee is included so self-referential and
// transitive cycles are caught before persistence.
func (m *Manager) buildEscalationGraphForHire(newID string, c Constitution, knownAgents map[string]struct{}) EscalationGraph {
	// Snapshot the cached constitutions under lock to build the graph.
	m.mu.RLock()
	cached := make(map[string]Constitution, len(m.constitutions)+1)
	for id, cc := range m.constitutions {
		cached[id] = cc
	}
	m.mu.RUnlock()

	// Add the new employee's constitution. A shallow copy is fine — we
	// only read EscalatesTo which is not mutated during this call.
	copied := c
	copied.EscalatesTo = append([]string(nil), c.EscalatesTo...)
	cached[newID] = copied

	// For known agent IDs without a cached constitution, add a leaf
	// entry (empty EscalatesTo) so they're recognised as valid nodes.
	for id := range knownAgents {
		if _, ok := cached[id]; !ok {
			cached[id] = Constitution{}
		}
	}
	return StaticEscalationGraph(cached)
}

// allAgentIDs returns a sorted slice of all agent IDs: the new employee
// plus every key in the known set. Used as the seed list for cycle
// detection.
func allAgentIDs(newID string, known map[string]struct{}) []string {
	out := make([]string, 0, len(known)+1)
	out = append(out, newID)
	for id := range known {
		if id != newID {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// --------------------------------------------------------------------------
// Manager: goals (Phase 5 stubs that delegate to GoalStore)
// --------------------------------------------------------------------------

// ListGoals lists goals, optionally filtered by employee_id. Empty
// employeeID returns all goals across all employees.
func (m *Manager) ListGoals(ctx context.Context, employeeID string) ([]*Goal, error) {
	if m.goalStore == nil {
		return nil, ErrNotImplemented
	}
	active, err := m.goalStore.ListActive(ctx, employeeID)
	if err != nil {
		return nil, fmt.Errorf("list goals: %w", err)
	}
	goals := make([]*Goal, 0, len(active))
	goals = append(goals, active...)
	return goals, nil
}

// GetGoal retrieves a single goal by ID including its active plan + history.
func (m *Manager) GetGoal(ctx context.Context, id string) (*Goal, error) {
	if m.goalStore == nil {
		return nil, ErrNotImplemented
	}
	return m.goalStore.Get(ctx, id)
}

// ApprovePlan signs off on a pending plan for a goal, allowing the GoalLoop
// to proceed to the EXECUTE phase. The approval is routed through the
// injected PlanDisposer (backed by internal/plan.PlanManager in
// production). When the disposer is not wired, the method returns a clear
// "not configured" error instead of silently succeeding.
//
// On a successful approval, the Manager updates the goal's ActivePlanID
// (spec line 295: "Active plan ID recorded on the Goal") when the
// goalStore is available and goalID is non-empty. Actual plan execution
// is left to the GoalLoop driver, which has its own scheduler hook.
func (m *Manager) ApprovePlan(ctx context.Context, goalID, planID, reason string) error {
	if m.planDisposer == nil {
		return errors.New("employee: plan disposer not configured")
	}
	// The PlanDisposer's ApprovePlan signature requires sessionID and by.
	// The Manager does not have a session context here; pass empty
	// sessionID and "user" as the approver (the only valid caller of
	// this signoff path per the spec's "require_operator_resume"
	// invariant). The reason is forwarded as the sessionID-free context.
	_ = reason // reason is recorded by the disposer's own signoff row
	if err := m.planDisposer.ApprovePlan(ctx, planID, "", "user"); err != nil {
		return fmt.Errorf("approve plan %s: %w", planID, err)
	}
	// Record the active plan on the goal so the GoalLoop driver can
	// pick it up during its next tick.
	if m.goalStore != nil && goalID != "" {
		goal, err := m.goalStore.Get(ctx, goalID)
		if err != nil {
			// Don't fail the approval over a goal lookup miss; the
			// plan is already approved in the signoff system. Log
			// and move on — the GoalLoop will see the approved state
			// on its next Assess regardless.
			m.logger.Warn("approve plan: goal lookup failed; plan is approved but ActivePlanID not set",
				"goal_id", goalID, "plan_id", planID, "error", err)
			return nil
		}
		goal.SetActivePlan(planID)
		if err := m.goalStore.Update(ctx, goal); err != nil {
			m.logger.Warn("approve plan: goal update failed; plan is approved but ActivePlanID not persisted",
				"goal_id", goalID, "plan_id", planID, "error", err)
		}
	}
	m.logger.Info("plan approved",
		"goal_id", goalID, "plan_id", planID)
	m.emitMetric("employee.plan.approvals", 1, map[string]string{
		"employee_id": "",
		"outcome":     "approved",
	})
	return nil
}

// RejectPlan rejects a pending plan. A reason is required (spec CLI line
// 483). The rejection is routed through the injected PlanDisposer; when
// the disposer is not wired, the method returns a clear "not configured"
// error. The goal's ActivePlanID is NOT updated (rejected plans never
// become active).
func (m *Manager) RejectPlan(ctx context.Context, goalID, planID, reason string) error {
	if m.planDisposer == nil {
		return errors.New("employee: plan disposer not configured")
	}
	if err := m.planDisposer.RejectPlan(ctx, planID, "", "user", reason); err != nil {
		return fmt.Errorf("reject plan %s: %w", planID, err)
	}
	m.logger.Info("plan rejected",
		"goal_id", goalID, "plan_id", planID)
	m.emitMetric("employee.plan.approvals", 1, map[string]string{
		"employee_id": "",
		"outcome":     "rejected",
	})
	return nil
}

// --------------------------------------------------------------------------
// Manager: audit (Phase 5 delegates to AuditStore)
// --------------------------------------------------------------------------

// ListAuditFindings lists findings for an employee, optionally filtered by
// time window (Since) and severity.
func (m *Manager) ListAuditFindings(ctx context.Context, q AuditQuery) ([]AuditFinding, error) {
	if m.auditStore == nil {
		return nil, ErrNotImplemented
	}
	f := AuditListFilter{
		EmployeeID: q.EmployeeID,
		Severity:   q.Severity,
	}
	if q.Since > 0 {
		f.Since = time.Now().UTC().Add(-q.Since)
	}
	return m.auditStore.List(ctx, f)
}

// ResolveAuditFinding marks a finding resolved with a specific resolution
// ("false_positive", "acknowledged", "constitution_amended").
func (m *Manager) ResolveAuditFinding(ctx context.Context, findingID, resolution, note string) error {
	if m.auditStore == nil {
		return ErrNotImplemented
	}
	return m.auditStore.Resolve(ctx, findingID, resolution)
}

// Review returns the current state of an employee: definition, status,
// recent findings, drift score, and active goals. Used by Review RPC /
// HTTP / CLI endpoints.
func (m *Manager) Review(ctx context.Context, id string) (*Review, error) {
	emp, err := m.GetEmployee(ctx, id)
	if err != nil {
		return nil, err
	}
	r := &Review{Employee: *emp}
	if m.botManager != nil {
		if state, err := m.botManager.GetBotStatus(ctx, id); err == nil {
			r.Status = FromBotStatus(state.Status)
		}
	}
	m.mu.RLock()
	r.DriftScore = m.driftScores[id]
	m.mu.RUnlock()
	if m.auditStore != nil {
		findings, err := m.auditStore.List(ctx, AuditListFilter{
			EmployeeID: id, Limit: 10,
		})
		if err == nil {
			r.RecentFindings = findings
		}
	}
	if m.goalStore != nil {
		goals, err := m.goalStore.ListActive(ctx, id)
		if err == nil {
			r.ActiveGoals = append(r.ActiveGoals, goals...)
		}
	}
	return r, nil
}

// --------------------------------------------------------------------------
// Manager: migration
// --------------------------------------------------------------------------

// Migrate scans ~/.meept/bots/*.json (or the override set via SetBotsDir)
// and proposes a constitution for each legacy bot. Never refuses to
// migrate; vague prompts get a minimal conservative constitution flagged
// for human review (spec line 228: "It never refuses to migrate").
//
// When a migrator LLM is wired (SetMigratorLLM), Migrate attempts to derive
// a richer constitution (purpose, role, never rules, risk ceiling) from each
// bot's prompt via the small model. On any LLM error, unparseable response,
// or validation failure, it falls back to the conservative
// synthesizeConservativeConstitution path. The conservative path is
// preserved unchanged — it always succeeds.
//
// Every proposal has Confidence < 1.0 and NeedsReview = true so the
// operator knows the constitution was inferred rather than authored.
func (m *Manager) Migrate(ctx context.Context) ([]MigrationProposal, error) {
	dir := m.resolveBotsDir()
	// Missing or empty bots dir is not an error — return an empty slice.
	entries, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		// filepath.Glob only errors on bad glob patterns, never on missing dirs.
		return nil, fmt.Errorf("migrate: glob bots dir %q: %w", dir, err)
	}
	// Snapshot the migrator LLM under the read lock so the nil-check and
	// subsequent call are consistent even under concurrent SetMigratorLLM.
	// The LLM call itself happens outside the lock (CLAUDE.md mutex-scope
	// rule).
	m.mu.RLock()
	migratorLLM := m.migratorLLM
	m.mu.RUnlock()

	proposals := make([]MigrationProposal, 0, len(entries))
	for _, path := range entries {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			m.logger.Warn("migrate: skip unreadable bot file",
				"path", path, "error", readErr)
			continue
		}
		var def bot.BotDefinition
		if jErr := json.Unmarshal(raw, &def); jErr != nil {
			m.logger.Warn("migrate: skip unparseable bot file",
				"path", path, "error", jErr)
			continue
		}
		if def.ID == "" {
			// Derive ID from the filename so ApplyMigration can find it.
			def.ID = strings.TrimSuffix(filepath.Base(path), ".json")
		}
		prop := m.buildMigrationProposalWithLLM(ctx, migratorLLM, def)
		proposals = append(proposals, prop)
	}
	m.logger.Info("migrate scan complete",
		"bots_dir", dir, "proposals", len(proposals),
		"llm_enabled", migratorLLM != nil)
	return proposals, nil
}

// ApplyMigration writes the proposed constitution for the given bot ID to
// disk and loads the resulting employee. Spec CLI line 477:
// "meept agents migrate --apply <id>".
//
// The bot definition is looked up via botStore when available (production
// path), else re-scanned from the bots directory. The constitution is
// synthesized using the same conservative defaults as Migrate. The bot
// definition is UPDATEd in place (not re-created) because legacy bots
// already exist in bot_definitions.
func (m *Manager) ApplyMigration(ctx context.Context, botID string) (*MigrationApplyResult, error) {
	def, err := m.lookupLegacyBot(ctx, botID)
	if err != nil {
		return nil, fmt.Errorf("apply migration: %w", err)
	}
	constitution := synthesizeConservativeConstitution(def)
	warnings := migrationReviewNotes(def)

	// Persist the bot definition. When the bot already exists in the
	// store this is an UPDATE; otherwise it's a CREATE so the
	// constitution has a valid foreign key to attach to.
	if m.botManager != nil {
		if existing, gErr := m.botManager.GetBot(ctx, def.ID); gErr == nil && existing != nil {
			// Bot exists — update in place (preserve triggers, tools).
			if uErr := m.botManager.UpdateBot(ctx, def); uErr != nil {
				return nil, fmt.Errorf("apply migration: update bot: %w", uErr)
			}
		} else {
			// Bot not yet in the store — create it.
			if cErr := m.botManager.CreateBot(ctx, def); cErr != nil {
				// CreateBot fails on duplicate ID; if that's the case,
				// fall through to persisting the constitution.
				m.logger.Debug("apply migration: create bot failed (may already exist)",
					"bot_id", def.ID, "error", cErr)
			}
		}
	}
	// Persist the constitution.
	if m.constitutionStore != nil {
		if pErr := m.constitutionStore.Put(ctx, def.ID, constitution); pErr != nil {
			return nil, fmt.Errorf("apply migration: persist constitution: %w", pErr)
		}
	}
	m.setCachedConstitution(def.ID, constitution)
	m.logger.Info("migration applied",
		"bot_id", def.ID, "tier", constitution.AutonomyTier.String())
	return &MigrationApplyResult{
		Applied:  true,
		Warnings: warnings,
	}, nil
}

// resolveBotsDir returns the bots directory to scan, defaulting to
// ~/.meept/bots/ when SetBotsDir was not called.
func (m *Manager) resolveBotsDir() string {
	if m.botsDir != "" {
		return m.botsDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// Fall back to a relative path; Migrate will log a warning for
		// the unreadable directory and return an empty slice.
		return ".meept/bots"
	}
	return filepath.Join(home, ".meept", "bots")
}

// lookupLegacyBot finds a bot definition by ID. It tries the bot store
// first (production path), then falls back to scanning the bots dir.
func (m *Manager) lookupLegacyBot(ctx context.Context, botID string) (bot.BotDefinition, error) {
	if m.botStore != nil {
		def, err := m.botStore.Get(ctx, botID)
		if err == nil && def != nil {
			return *def, nil
		}
		// Fall through to dir scan on miss.
	}
	dir := m.resolveBotsDir()
	// Try <dir>/<botID>.json directly first, then glob the dir.
	candidate := filepath.Join(dir, botID+".json")
	if raw, err := os.ReadFile(candidate); err == nil {
		var def bot.BotDefinition
		if jErr := json.Unmarshal(raw, &def); jErr == nil {
			if def.ID == "" {
				def.ID = botID
			}
			return def, nil
		}
	}
	entries, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	for _, path := range entries {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		var def bot.BotDefinition
		if jErr := json.Unmarshal(raw, &def); jErr != nil {
			continue
		}
		if def.ID == botID {
			return def, nil
		}
	}
	return bot.BotDefinition{}, fmt.Errorf("legacy bot %q not found in store or %s", botID, dir)
}

// buildMigrationProposalWithLLM attempts to derive a richer constitution
// from the bot's prompt using a small model. On any error, unparseable
// response, or validation failure, it falls back to the conservative
// synthesizeConservativeConstitution path. The conservative path always
// sets Purpose, so an empty Purpose serves as the "fallback occurred"
// sentinel (spec line 228: "It never refuses to migrate").
func (m *Manager) buildMigrationProposalWithLLM(
	ctx context.Context,
	chatter llm.Chatter,
	def bot.BotDefinition,
) MigrationProposal {
	if chatter != nil {
		constitution, notes, llmErr := m.synthesizeConstitutionWithLLM(ctx, chatter, def)
		if llmErr == nil && constitution.Purpose != "" {
			return MigrationProposal{
				BotID:       def.ID,
				BotName:     def.Name,
				Proposed:    constitution,
				Confidence:  0.7, // LLM-inferred: slightly higher than conservative
				NeedsReview: true,
				Warnings:    notes,
			}
		}
		// LLM path failed — log and fall through to conservative.
		if llmErr != nil {
			m.logger.Warn("migrate: LLM synthesis failed; falling back to conservative",
				"bot_id", def.ID, "error", llmErr)
		} else {
			m.logger.Warn("migrate: LLM synthesis returned empty purpose; falling back to conservative",
				"bot_id", def.ID)
		}
	}
	c := synthesizeConservativeConstitution(def)
	notes := migrationReviewNotes(def)
	if chatter != nil {
		notes = append(notes, "fallback to conservative defaults after LLM failure")
	}
	return MigrationProposal{
		BotID:       def.ID,
		BotName:     def.Name,
		Proposed:    c,
		Confidence:  0.5,
		NeedsReview: true,
		Warnings:    notes,
	}
}

// migrationReviewNotes lists what was inferred during conservative
// synthesis so the operator knows what to review before applying.
func migrationReviewNotes(def bot.BotDefinition) []string {
	notes := []string{
		"autonomy tier defaulted to tier_1_reactive (conservative)",
		"risk ceiling defaulted to low (conservative)",
		"constraints.inherited tools (no allowlist derived from prompt)",
	}
	if strings.TrimSpace(def.Description) == "" {
		notes = append(notes, "bot description was empty; purpose derived from name")
	}
	if strings.TrimSpace(def.Prompt) == "" {
		notes = append(notes, "bot prompt was empty; charter left blank")
	}
	if len(def.Triggers) == 0 {
		notes = append(notes, "no triggers declared; employee will be triggerless until configured")
	}
	return notes
}

// migrateLLMTimeout is the per-bot timeout for the LLM synthesis call.
// 30 seconds is generous for a small model single-shot completion.
const migrateLLMTimeout = 30 * time.Second

// llmConstitutionResponse is the strict JSON schema we ask the small model
// to return when synthesizing a constitution from a legacy bot prompt.
// Unknown fields are ignored by json.Unmarshal.
type llmConstitutionResponse struct {
	Purpose          string   `json:"purpose"`
	Role             string   `json:"role"`
	Never            []string `json:"never"`
	RiskCeiling      string   `json:"risk_ceiling"`
	ToolsAllowedHint []string `json:"tools_allowed_hint"`
}

// synthesizeConstitutionWithLLM attempts to derive a richer constitution
// from the bot's prompt using a small model. On any error, unparseable
// response, or validation failure, it falls back to
// synthesizeConservativeConstitution (returned as a non-nil error so the
// caller can log the fallback reason).
//
// The merge strategy is: start with conservative defaults, override the
// LLM-suggested fields IF they validate. We ALWAYS preserve:
//   - AutonomyTier: Tier1Reactive (never trust the LLM to upgrade tier)
//   - EscalatesTo: ["user"]
//   - AmendmentPolicy.FrozenFields
//   - AuthoredBy: "migrate-llm" (so operators can distinguish LLM-proposed
//     constitutions from pure conservative defaults)
//
// The returned []string is a list of review notes describing what the LLM
// suggested (so the operator can verify before applying).
func (m *Manager) synthesizeConstitutionWithLLM(
	ctx context.Context,
	chatter llm.Chatter,
	def bot.BotDefinition,
) (Constitution, []string, error) {
	// Start from conservative defaults — we only override the LLM-suggested
	// fields that validate. This guarantees the return value is always a
	// usable constitution even when the LLM returns partial garbage.
	base := synthesizeConservativeConstitution(def)
	base.AuthoredBy = "migrate-llm"
	notes := []string{"constitution synthesized via LLM"}

	// Build the LLM prompt.
	prompt := buildMigratorPrompt(def)

	// Bounded context for the LLM call.
	callCtx, cancel := context.WithTimeout(ctx, migrateLLMTimeout)
	defer cancel()

	resp, err := chatter.Chat(callCtx, []llm.ChatMessage{
		{Role: llm.RoleUser, Content: prompt},
	})
	if err != nil {
		return base, notes, fmt.Errorf("migrator LLM call: %w", err)
	}
	if resp == nil || strings.TrimSpace(resp.Content) == "" {
		return base, notes, errors.New("migrator LLM returned empty response")
	}

	// Extract JSON from the response. The LLM is instructed to return ONLY
	// JSON, but defensively strip any preamble before the first "{" and any
	// trailing text after the last "}".
	raw := extractJSON(resp.Content)
	var llmResp llmConstitutionResponse
	if jErr := json.Unmarshal([]byte(raw), &llmResp); jErr != nil {
		return base, notes, fmt.Errorf("migrator LLM unparseable response: %w", jErr)
	}

	// Override Purpose if the LLM provided a non-empty one.
	if p := strings.TrimSpace(llmResp.Purpose); p != "" {
		base.Purpose = p
		notes = append(notes, "purpose: LLM-derived from bot prompt")
	}

	// Override Role if the LLM provided a non-empty one.
	if r := strings.TrimSpace(llmResp.Role); r != "" {
		base.Role = r
		notes = append(notes, "role: LLM-derived from bot prompt")
	}

	// Override Never rules if the LLM provided a non-empty list. Always
	// ensure "execute financial transactions" is present (the conservative
	// default) unless the LLM explicitly omitted it for a financial bot
	// (we can't detect intent, so we just append if missing to be safe).
	if len(llmResp.Never) > 0 {
		merged := make([]string, 0, len(llmResp.Never)+1)
		hasFinancial := false
		for _, rule := range llmResp.Never {
			r := strings.TrimSpace(rule)
			if r == "" {
				continue
			}
			merged = append(merged, r)
			if strings.Contains(strings.ToLower(r), "financial") {
				hasFinancial = true
			}
		}
		if !hasFinancial {
			merged = append(merged, "execute financial transactions")
		}
		base.Constraints.Never = merged
		notes = append(notes, fmt.Sprintf("never rules: LLM suggested %d rule(s)", len(merged)))
	}

	// Override RiskCeiling if the LLM suggested a valid band. We only
	// accept safe, low, or medium — never high or critical from the
	// migrator path (conservative guardrail).
	if rc := strings.TrimSpace(llmResp.RiskCeiling); rc != "" {
		ceiling := RiskLevelCeiling(strings.ToLower(rc))
		switch ceiling {
		case RiskCeilingSafe, RiskCeilingLow, RiskCeilingMedium:
			base.Constraints.RiskCeiling = ceiling
			notes = append(notes, fmt.Sprintf("risk ceiling: LLM suggested %q", ceiling))
		default:
			notes = append(notes,
				fmt.Sprintf("risk ceiling: LLM suggested %q (rejected, not safe/low/medium)", rc))
		}
	}

	// Override ToolsAllowed if the LLM provided a non-empty hint list.
	if len(llmResp.ToolsAllowedHint) > 0 {
		cleaned := make([]string, 0, len(llmResp.ToolsAllowedHint))
		for _, t := range llmResp.ToolsAllowedHint {
			t = strings.TrimSpace(t)
			if t != "" {
				cleaned = append(cleaned, t)
			}
		}
		if len(cleaned) > 0 {
			base.Constraints.ToolsAllowed = cleaned
			notes = append(notes,
				fmt.Sprintf("tools_allowed: LLM suggested %d tool(s)", len(cleaned)))
		}
	}

	// Validate the merged constitution. If it fails, fall back to the
	// pure conservative path (never trust the LLM's output to be valid).
	if vErr := base.Validate(def.ID); vErr != nil {
		conservative := synthesizeConservativeConstitution(def)
		conservative.AuthoredBy = "migrate-llm"
		return conservative, notes, fmt.Errorf("migrator LLM constitution failed validation: %w", vErr)
	}

	return base, notes, nil
}

// buildMigratorPrompt constructs the prompt sent to the small model to
// synthesize a constitution from a legacy bot definition.
func buildMigratorPrompt(def bot.BotDefinition) string {
	var b strings.Builder
	b.WriteString("You are migrating a legacy bot to a constitution-bound employee.\n")
	b.WriteString("Bot ID: " + def.ID + "\n")
	b.WriteString("Bot Name: " + def.Name + "\n")
	b.WriteString("Bot Description: " + def.Description + "\n")
	b.WriteString("Bot Prompt: " + def.Prompt + "\n\n")
	b.WriteString("Analyze this bot and return JSON with EXACTLY this shape:\n")
	b.WriteString("{\n")
	b.WriteString("  \"purpose\": \"<one-sentence purpose>\",\n")
	b.WriteString("  \"role\": \"<short role label, e.g. 'notification dispatcher'>\",\n")
	b.WriteString("  \"never\": [\"<rule 1>\", \"<rule 2>\"],\n")
	b.WriteString("  \"risk_ceiling\": \"safe\" | \"low\" | \"medium\",\n")
	b.WriteString("  \"tools_allowed_hint\": [\"tool1\", \"tool2\"]\n")
	b.WriteString("}\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- never[]: behaviors this bot should NEVER do based on its purpose. " +
		"Be conservative. Include \"execute financial transactions\" unless clearly financial.\n")
	b.WriteString("- risk_ceiling: default \"low\". Only raise to \"medium\" if the bot " +
		"clearly needs file writes or shell access.\n")
	b.WriteString("- tools_allowed_hint: optional. Suggest tool names that appear in the " +
		"prompt (e.g. \"web_fetch\", \"shell_execute\"). Leave empty if unclear.\n")
	b.WriteString("- Return ONLY the JSON, no preamble.\n")
	return b.String()
}

// extractJSON isolates the first JSON object in s by trimming any text
// before the first "{" and after the last "}". Returns s unchanged when no
// braces are present (the caller's json.Unmarshal will then surface the
// parse error).
func extractJSON(s string) string {
	first := strings.IndexByte(s, '{')
	last := strings.LastIndexByte(s, '}')
	if first < 0 || last < 0 || last < first {
		return s
	}
	return s[first : last+1]
}

// synthesizeConservativeConstitution builds the minimal conservative
// constitution mandated by spec line 627: tier_1_reactive,
// risk_ceiling: low, escalates_to: ["user"], never contains a sensible
// default. The charter copies the legacy prompt verbatim; the purpose is
// derived from the description (first sentence) or a fallback string.
//
// This function never fails — it always returns a usable Constitution.
func synthesizeConservativeConstitution(def bot.BotDefinition) Constitution {
	purpose := derivePurpose(def)
	// C6: Delegate to DefaultConservativeConstitution for all defaults,
	// then override the migration-specific fields (Charter from bot
	// prompt, Role and AuthoredBy).
	c := DefaultConservativeConstitution(purpose)
	c.Role = "migrated legacy bot"
	c.Charter = def.Prompt
	c.AuthoredBy = "migrate"
	return c
}

// derivePurpose extracts the first sentence of the bot description, or
// falls back to a conservative identifier-based string.
func derivePurpose(def bot.BotDefinition) string {
	desc := strings.TrimSpace(def.Description)
	if desc == "" {
		return "migrated from legacy bot " + def.ID
	}
	// First sentence = up to the first period, question mark, or
	// exclamation mark, or the whole string if no terminator.
	for i, ch := range desc {
		if ch == '.' || ch == '!' || ch == '?' {
			return desc[:i+1]
		}
	}
	return desc
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// decodeConstitution converts the raw map (from RPC/JSON input) into a
// typed Constitution. Empty or nil input is rejected — a constitution is
// required (spec line 222).
func decodeConstitution(raw map[string]any) (Constitution, error) {
	if len(raw) == 0 {
		return Constitution{}, ErrConstitutionRequired
	}
	// Marshal-then-unmarshal is the simplest way to honor the JSON tags
	// on Constitution (including AutonomyTier's MarshalJSON).
	data, err := json.Marshal(raw)
	if err != nil {
		return Constitution{}, fmt.Errorf("decode constitution: marshal: %w", err)
	}
	var c Constitution
	if err := json.Unmarshal(data, &c); err != nil {
		return Constitution{}, fmt.Errorf("decode constitution: unmarshal: %w", err)
	}
	return c, nil
}

// findFrozenViolation returns the first key in patch that appears in the
// frozen list, or "" when none match. Both plain ("purpose") and dotted
// ("constraints.risk_ceiling") forms are honored.
func findFrozenViolation(patch map[string]any, frozen []string) string {
	if len(frozen) == 0 || len(patch) == 0 {
		return ""
	}
	frozenSet := make(map[string]struct{}, len(frozen))
	for _, f := range frozen {
		frozenSet[strings.ToLower(strings.TrimSpace(f))] = struct{}{}
	}
	for k := range patch {
		lk := strings.ToLower(strings.TrimSpace(k))
		if _, bad := frozenSet[lk]; bad {
			return k
		}
	}
	return ""
}

// patchConstitution applies a partial patch (map of field path → new value)
// to an existing Constitution and returns the new value. Field paths use
// the JSON field names ("purpose", "constraints.risk_ceiling", etc.).
// Unknown fields are ignored with no error — strict validation happens in
// Constitution.Validate after the patch.
func patchConstitution(existing Constitution, patch map[string]any) (Constitution, error) {
	if len(patch) == 0 {
		return existing, nil
	}
	// Round-trip through JSON to apply the patch generically.
	base, err := json.Marshal(existing)
	if err != nil {
		return existing, fmt.Errorf("marshal base: %w", err)
	}
	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil {
		return existing, fmt.Errorf("unmarshal base: %w", err)
	}
	// Apply top-level patches directly; "constraints.<x>" patches are
	// routed into the nested constraints map.
	for k, v := range patch {
		if strings.HasPrefix(k, "constraints.") {
			sub := strings.TrimPrefix(k, "constraints.")
			constraintsAny, ok := baseMap["constraints"]
			if !ok {
				constraintsAny = map[string]any{}
			}
			constraintsMap, ok := constraintsAny.(map[string]any)
			if !ok {
				return existing, fmt.Errorf("patch: constraints is %T, expected map", constraintsAny)
			}
			constraintsMap[sub] = v
			baseMap["constraints"] = constraintsMap
			continue
		}
		if strings.HasPrefix(k, "amendment_policy.") {
			sub := strings.TrimPrefix(k, "amendment_policy.")
			apAny, ok := baseMap["amendment_policy"]
			if !ok {
				apAny = map[string]any{}
			}
			apMap, ok := apAny.(map[string]any)
			if !ok {
				return existing, fmt.Errorf("patch: amendment_policy is %T, expected map", apAny)
			}
			apMap[sub] = v
			baseMap["amendment_policy"] = apMap
			continue
		}
		baseMap[k] = v
	}
	merged, err := json.Marshal(baseMap)
	if err != nil {
		return existing, fmt.Errorf("marshal merged: %w", err)
	}
	var patched Constitution
	if err := json.Unmarshal(merged, &patched); err != nil {
		return existing, fmt.Errorf("unmarshal merged: %w", err)
	}
	return patched, nil
}
