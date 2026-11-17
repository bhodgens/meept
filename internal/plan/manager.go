package plan

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/kanban"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// TaskCreator abstracts task creation, decoupling PlanManager from task.Store.
// Implemented by task.Registry or a daemon-provided adapter.
type TaskCreator interface {
	CreateTask(ctx context.Context, name, description string) (*task.Task, error)
	CreateTaskStep(ctx context.Context, taskID, description string, sequence int) (*task.TaskStep, error)
	UpdateTaskStep(ctx context.Context, step *task.TaskStep) error
	LinkSession(ctx context.Context, taskID, sessionID string) error
	// SetTaskJobCount sets the TotalJobs counter on the task so that
	// auto-completion (CompletedJobs == TotalJobs) works correctly.
	SetTaskJobCount(ctx context.Context, taskID string, count int) error
	// ScheduleSteps promotes ready steps and publishes the
	// orchestrator.schedule event so the tactical planner can enqueue jobs.
	ScheduleSteps(ctx context.Context, taskID string) error
}

// TaskGoalSetter is the optional seam a TaskCreator implements when the
// backing task store can persist a task's owning goal (goal-completion C3).
// Synthesize stamps the plan's GoalID onto every task it creates through it;
// a creator without the seam keeps working (goal-less tasks stay legacy-legal)
// (bughunt 2026-10-14: plans carried goal_id, synthesized tasks did not).
type TaskGoalSetter interface {
	SetGoalID(ctx context.Context, taskID, goalID string) error
}

// TaskCanceller is the optional seam a TaskCreator implements when the
// backing task store can cancel a task (leaf 06 T2, audit F8). CancelPlan's
// cascade reaches the plan's parent task and every live child task through
// it — the EXISTING task-cancel machinery (interrupt trigger, terminal
// state write, task.ev.task.cancelled notification), not a new event
// surface. A creator without the seam keeps working: the phase cascade
// still runs and the task-cancel step is skipped with a debug log.
type TaskCanceller interface {
	CancelTask(ctx context.Context, taskID, reason string) error
}

// PlanManager orchestrates plan lifecycle: creation, approval, synthesis
// into tasks, progress tracking, and confirmation.
type PlanManager struct {
	store        PlanStore
	bus          *bus.MessageBus
	config       config.PlansConfig
	taskCreator  TaskCreator
	logger       *slog.Logger
	phaseTaskMap map[string]string // taskID -> phaseID, populated during Synthesize
	taskPlanMap  map[string]string // taskID -> planID, populated during Synthesize

	// kanbanEvents is the opt-in gate (plan leaf 02/04) for the typed
	// kanban.event.plan.state StateEvent publishers on the plan state
	// machine's transition paths. Default false = no publish at all =
	// byte-identical behavior. Set via SetKanbanEventPublishing (the
	// queue's and registry's 02/03 setter pattern). It sits under its OWN
	// kanbanMu, NOT m.mu: m.mu is held across Synthesize/EnsureTaskPlan
	// map writes whose call chains reach publishPlanState, and Go's
	// RWMutex is non-reentrant — sharing it would self-deadlock (the
	// selflock analyzer's exact shape). kanbanMu guards only the bool.
	kanbanMu     sync.RWMutex
	kanbanEvents bool

	mu sync.RWMutex // protects phaseTaskMap and taskPlanMap
}

// synthesisLeaseDuration is how long a synthesis claim stays live. A crash
// mid-expansion leaves the claim behind; the lease bounds how long that plan is
// stranded before it can be re-claimed. Generous relative to a synthesis pass
// (a handful of task writes), short enough that a restart recovers promptly.
const synthesisLeaseDuration = 5 * time.Minute

// MaxSynthesisAttempts bounds how many times one plan may be expanded before it
// is parked in StateFailed. Without it a plan that fails synthesis for a
// persistent reason (a malformed plan.md, an unwritable worktree) is re-entered
// on every auto-approve pass forever — the same unbounded loop, one plan at a
// time (bughunt 2026-10-08).
const MaxSynthesisAttempts = 3

// NewPlanManager creates a new PlanManager.
func NewPlanManager(store PlanStore, bus *bus.MessageBus, cfg config.PlansConfig, taskCreator TaskCreator, logger *slog.Logger) *PlanManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &PlanManager{
		store:        store,
		bus:          bus,
		config:       cfg,
		taskCreator:  taskCreator,
		logger:       logger.With("component", "plan-manager"),
		phaseTaskMap: make(map[string]string),
		taskPlanMap:  make(map[string]string),
	}
}

// ---------------------------------------------------------------------------
// Lifecycle methods
// ---------------------------------------------------------------------------

// GetPlan returns the plan with the given ID from the store.
func (m *PlanManager) GetPlan(ctx context.Context, planID string) (*Plan, error) {
	return m.store.GetPlan(ctx, planID)
}

// SetPlanGoal stamps the owning goal onto a stored plan row
// (goal-completion leaf 02, C6 storage half). It satisfies the employee
// package's PlanGoalSetter contract via the daemon's planCreatorAdapter, so
// GoalLoop.Plan persists Plan.GoalID right after CreatePlan returns. Errors
// surface to the caller (the GoalLoop logs them as non-fatal).
func (m *PlanManager) SetPlanGoal(ctx context.Context, planID, goalID string) error {
	p, err := m.store.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("set plan goal: get plan: %w", err)
	}
	p.GoalID = goalID
	if err := m.store.UpdatePlan(ctx, p); err != nil {
		return fmt.Errorf("set plan goal: update plan: %w", err)
	}
	return nil
}

// SetPlanPrompt persists the candidate goal prompt onto a stored plan row
// (issue #148 resume-prompt-loss). The GoalLoop creates the plan through
// the daemon's PlanCreator adapter, which cannot carry the prompt in its
// fixed signature; this targeted column write is the persistence seam. A
// resumed ApproveAndExecute (PlanRef{ID} only) rehydrates the EXACT prompt
// through the plan-brief lookup, so the execution user message matches a
// same-process approve. Failures are returned for the caller to log; the
// plan row keeps whatever prompt it had.
func (m *PlanManager) SetPlanPrompt(ctx context.Context, planID, prompt string) error {
	if err := m.updatePlanColumns(ctx, planID, func(p *Plan) {
		p.Prompt = prompt
		p.UpdatedAt = time.Now().UTC()
	}); err != nil {
		return fmt.Errorf("set plan prompt: %w", err)
	}
	return nil
}

// GetPlansForSession returns the plans linked to a session (read-through
// to the store). Used by the dispatcher's quickplan Session execution
// context block (quickplan-mode leaf 02).
func (m *PlanManager) GetPlansForSession(ctx context.Context, sessionID string) ([]*Plan, error) {
	return m.store.GetPlansForSession(ctx, sessionID)
}

// HasOpenPlanWithTitle reports whether a plan with the exact title exists in
// an OPEN approval-or-execution state (pending_approval, approved,
// synthesizing, executing). Machine proposers (the skill evolver) use it to
// avoid re-creating the same proposal every cycle while an earlier one is
// still waiting at the human gate or mid-flight — the unbounded-duplicate
// bug that produced 639 pending + 922 executing copies of ~36 proposals
// (observed live 2026-10-22). Draft/planning are deliberately NOT checked:
// those are transient pre-submit states, and legacy draft piles must not
// block a fresh proposal forever.
func (m *PlanManager) HasOpenPlanWithTitle(ctx context.Context, title string) (bool, error) {
	for _, state := range []PlanState{StatePendingApproval, StateApproved, StateSynthesizing, StateExecuting} {
		plans, err := m.store.ListPlansByState(ctx, state, 1000)
		if err != nil {
			return false, fmt.Errorf("scan %s plans: %w", state, err)
		}
		for _, p := range plans {
			if p.Title == title {
				return true, nil
			}
		}
	}
	return false, nil
}

// CountPlansByState returns how many plans are in the given state (capped at
// limit). Used by machine proposers to enforce a pending-queue ceiling.
func (m *PlanManager) CountPlansByState(ctx context.Context, state PlanState, limit int) (int, error) {
	plans, err := m.store.ListPlansByState(ctx, state, limit)
	if err != nil {
		return 0, fmt.Errorf("count %s plans: %w", state, err)
	}
	return len(plans), nil
}

// ListApprovedPlans returns the plans currently in StateApproved (capped at
// 1000 rows, the same bound CountPlansByState's callers use). Read-only:
// the employee approved-plan resume sweep consumes the IDs to reconcile
// approved-but-unexecuted plans (stranded-approval fix); it never mutates
// plan state.
func (m *PlanManager) ListApprovedPlans(ctx context.Context) ([]*Plan, error) {
	plans, err := m.store.ListPlansByState(ctx, StateApproved, 1000)
	if err != nil {
		return nil, fmt.Errorf("list approved plans: %w", err)
	}
	return plans, nil
}

// stampTaskGoal carries a plan's goal onto a synthesized task through the
// optional TaskGoalSetter seam. Best-effort: a creator without the seam or a
// failed stamp logs a Warn and leaves the task goal-less (legacy-legal).
func (m *PlanManager) stampTaskGoal(ctx context.Context, taskID, goalID string) {
	if goalID == "" {
		return
	}
	setter, ok := m.taskCreator.(TaskGoalSetter)
	if !ok {
		return
	}
	if err := setter.SetGoalID(ctx, taskID, goalID); err != nil {
		m.logger.Warn("failed to stamp plan goal onto task",
			"task_id", taskID, "goal_id", goalID, "error", err)
	}
}

// maxStrandedRecoverScan bounds how many stranded-synthesis plans one
// recovery pass will consider (SQLite LIMIT). Stranded rows are rare and
// small; this cap prevents a pathological table from stalling startup.
const maxStrandedRecoverScan = 100

// RecoverStrandedSynthesis is the DRIVER for the synthesis lease (bughunt
// M10). It returns plans left in StateSynthesizing with an EXPIRED synthesis
// lease back to StateApproved, so the normal approval -> synthesis path can
// re-drive them. Without this driver, StateSynthesizing is a terminal sink: a
// crash (or a cancelled ctx) between ClaimPlanSynthesis and the UpdatePlan
// that records task_id stranded the plan forever — the lease made the row
// re-claimable but nothing ever claimed it. Only EXPIRED leases are reclaimed:
// the reclaim is a compare-and-set (the lease predicate is part of its UPDATE),
// so a concurrent expander that re-claimed the row wins and this loses — the
// safe verdict, a live lease is never stomped.
//
// A store that does not expose the SynthesisReclaimer capability is skipped
// silently (test doubles, alternative backends stay valid PlanStores).
func (m *PlanManager) RecoverStrandedSynthesis(ctx context.Context) (int, error) {
	reclaimer, ok := m.store.(SynthesisReclaimer)
	if !ok {
		return 0, nil
	}
	plans, err := m.store.ListPlansByState(ctx, StateSynthesizing, maxStrandedRecoverScan)
	if err != nil {
		return 0, fmt.Errorf("plan: list stranded synthesis: %w", err)
	}
	recovered := 0
	for _, p := range plans {
		won, err := reclaimer.ReclaimExpiredSynthesis(ctx, p.ID)
		if err != nil {
			return recovered, fmt.Errorf("plan: reclaim stranded synthesis %s: %w", p.ID, err)
		}
		if won {
			recovered++
			// CAS won: synthesizing -> approved is committed (plan leaf 02/04).
			m.publishPlanState(p.ID, StateSynthesizing, StateApproved, kanban.EventReasonReclaim)
		}
	}
	return recovered, nil
}

// defaultSynthesisReclaimInterval bounds how often the periodic synthesis
// reclaimer scans (plan-remediation C-P2, audit M-8/M-9). Generous relative to
// a synthesis pass (a handful of task writes) so the scan itself is noise.
const defaultSynthesisReclaimInterval = 5 * time.Minute

// synthesisReclaimCounts is the tally of one reclaimer pass, returned by
// runSynthesisReclaimPass so callers (and the Info log) can see what moved.
type synthesisReclaimCounts struct {
	reclaimed int // expired-lease synthesizing rows flipped back to approved
	redriven  int // Synthesize calls the pass issued (reclaim + approved scan)
	failed    int // Synthesize calls that returned an error
}

// runSynthesisReclaimPass performs ONE full reclaimer pass (C-P2): (a) the
// expired-lease reclaim — the RecoverStrandedSynthesis body, composed rather
// than forked so startup callers keep their count semantics — and (b) the
// MISSING EDGE the audit called M-9: every reclaimed plan, plus every
// approved-with-no-live-task plan found by the same scan, is RE-DRIVEN through
// Synthesize. Before this, reclaim-to-approved was still a dead end:
// ClaimPlanSynthesis is only called inside Synthesize, and Synthesize was only
// ever reached from ApprovePlan.
//
// Concurrency stays safe without extra locking: the claim CAS (single-winner
// on the row) is re-checked inside Synthesize, so a concurrent expander or a
// duplicate pass simply loses and skips. The attempt bound is NOT reimplemented
// here — Synthesize's existing deferred release/park enforces
// MaxSynthesisAttempts from the persisted attempts column, so a re-driven plan
// that keeps failing ends parked in StateFailed, never spun forever.
func (m *PlanManager) runSynthesisReclaimPass(ctx context.Context) (synthesisReclaimCounts, error) {
	var counts synthesisReclaimCounts
	reclaimer, ok := m.store.(SynthesisReclaimer)
	if !ok {
		return counts, nil
	}

	// (a) Reclaim expired leases: flips synthesizing -> approved, clearing the
	// lease. Same bounds and error shape as RecoverStrandedSynthesis.
	plans, err := m.store.ListPlansByState(ctx, StateSynthesizing, maxStrandedRecoverScan)
	if err != nil {
		return counts, fmt.Errorf("plan: list stranded synthesis: %w", err)
	}
	seen := make(map[string]bool, len(plans))
	candidates := make([]string, 0, len(plans))
	for _, p := range plans {
		won, err := reclaimer.ReclaimExpiredSynthesis(ctx, p.ID)
		if err != nil {
			return counts, fmt.Errorf("plan: reclaim stranded synthesis %s: %w", p.ID, err)
		}
		if won {
			counts.reclaimed++
			seen[p.ID] = true
			candidates = append(candidates, p.ID)
			// CAS won: synthesizing -> approved is committed (plan leaf 02/04).
			m.publishPlanState(p.ID, StateSynthesizing, StateApproved, kanban.EventReasonReclaim)
		}
	}

	// (b) Re-drive. Without a task creator there is nothing to expand into,
	// so the reclaim above still ran but the pass issues zero Synthesize calls.
	if m.taskCreator != nil {
		// The approved-with-no-live-task residue: an approved row with no
		// task_id never reaches Synthesize on its own (ApprovePlan only fires
		// from submit/approve paths), so the reclaimer is its only revisit.
		// Plans reclaimed in step (a) land here too — deduped via `seen`.
		approved, err := m.store.ListPlansByState(ctx, StateApproved, maxStrandedRecoverScan)
		if err != nil {
			return counts, fmt.Errorf("plan: list approved for re-drive: %w", err)
		}
		for _, p := range approved {
			if p.TaskID != "" || seen[p.ID] {
				continue
			}
			seen[p.ID] = true
			candidates = append(candidates, p.ID)
		}
		for _, id := range candidates {
			counts.redriven++
			if err := m.Synthesize(ctx, id); err != nil {
				counts.failed++
				m.logger.Warn("plan synthesis reclaimer: re-drive failed",
					"plan_id", id, "error", err)
			}
		}
	}

	return counts, nil
}

// StartSynthesisReclaimer launches the PERIODIC synthesis recovery loop
// (plan-remediation C-P2, audit M-8/M-9): the startup-only
// RecoverStrandedSynthesis wiring caught abandonment only at daemon boot, so a
// plan abandoned mid-run (cancelled ctx, panic in the expander) sat claimed
// until the next restart — and even then, reclaim-to-approved had no re-driver.
// This closes both halves: one immediate pass (preserving the startup
// invocation), then a pass every `interval` on ctx; the goroutine exits when
// ctx is cancelled. interval <= 0 selects defaultSynthesisReclaimInterval (5m).
// Nil-safe on a nil manager so daemon wiring can call it unconditionally.
func (m *PlanManager) StartSynthesisReclaimer(ctx context.Context, interval time.Duration) {
	if m == nil {
		return
	}
	if interval <= 0 {
		interval = defaultSynthesisReclaimInterval
	}
	runPass := func() {
		counts, err := m.runSynthesisReclaimPass(ctx)
		if err != nil {
			m.logger.Warn("plan synthesis reclaimer pass failed", "error", err)
		}
		if counts.reclaimed > 0 || counts.redriven > 0 {
			m.logger.Info("plan synthesis reclaimer pass",
				"reclaimed", counts.reclaimed,
				"redriven", counts.redriven,
				"failed", counts.failed)
		}
	}
	go func() {
		// Immediate startup pass: this replaces the old boot-time
		// RecoverStrandedSynthesis call, and the periodic loop keeps the
		// reclaim idiom for mid-run abandonment.
		runPass()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runPass()
			}
		}
	}()
}

// reservePlanFilePath atomically reserves an unused plan file path for slug,
// creating an empty placeholder with O_CREATE|O_EXCL so exactly one concurrent
// caller can win each candidate name (bughunt M1).
//
// A stat-then-write loop is NOT atomic: two callers both observe "missing" and
// both proceed to the same name. O_EXCL makes the create itself the test, so the
// loser simply advances to the next suffix.
func reservePlanFilePath(dir, slug string, logger *slog.Logger) (string, error) {
	// The plan directory may not exist yet on a fresh workspace. The previous
	// stat-then-write shape never needed it to (os.Stat on a missing path simply
	// reported "not exist"), so creating it here keeps that contract.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create plan dir %s: %w", dir, err)
	}
	for n := 1; ; n++ {
		name := slug + ".md"
		if n > 1 {
			name = fmt.Sprintf("%s-%d.md", slug, n)
		}
		candidate := filepath.Join(dir, name)
		f, err := os.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			// Reserved. Close immediately; the caller writes the real content.
			if cerr := f.Close(); cerr != nil {
				return "", fmt.Errorf("close plan file placeholder %s: %w", candidate, cerr)
			}
			if n > 1 {
				logger.Info("plan file name collision; disambiguating", "file", candidate)
			}
			return candidate, nil
		}
		if !os.IsExist(err) {
			return "", fmt.Errorf("reserve plan file %s: %w", candidate, err)
		}
		// EEXIST: this candidate is taken, try the next suffix.
	}
}

// CreatePlan creates a new plan, stores it, writes the initial plan.md, and
// publishes a plan.created event.
func (m *PlanManager) CreatePlan(ctx context.Context, title, description, projectID, projectPath, sessionID string) (*Plan, error) {
	return m.CreatePlanWithGoal(ctx, title, description, projectID, projectPath, sessionID, "")
}

// CreatePlanWithGoal is CreatePlan with the owning goal id stamped on the
// plan BEFORE the store write (goal-completion leaf 03, SHARED-CONVENTIONS
// C6): the chat path resolves the session's goal via the dispatcher's
// GoalAnchorStore and persists the linkage atomically with creation instead
// of a follow-up SetPlanGoal update. An empty goalID is legacy-legal
// (chat with no resolvable employee keeps working).
func (m *PlanManager) CreatePlanWithGoal(ctx context.Context, title, description, projectID, projectPath, sessionID, goalID string) (*Plan, error) {
	dir := m.resolvePlanDir(projectPath)

	// Never clobber an existing plan file. slugify(title) can collide with a
	// plan from an earlier run (a common title like "plan the refactor" recurs
	// across sessions); WritePlanMarkdown overwrites unconditionally, which used
	// to silently re-stamp the old plan's plan_id/created meta while the store
	// held two plans pointing at one file. Disambiguate with a -2, -3, ...
	// suffix instead (mirrors the repo plan_id seq).
	//
	// The name is RESERVED with an atomic O_CREATE|O_EXCL create (bughunt M1), not
	// chosen with stat-then-write: two concurrent CreatePlan calls with the same
	// title both saw the base name missing and both picked "-2", so both wrote to
	// the same path — the exact clobber this guard exists to prevent, just at a
	// narrower window. The reserve-then-write loop lets exactly one caller win
	// each candidate; the loser advances to the next suffix. The placeholder file
	// is immediately closed and later overwritten by WritePlanMarkdown, so the
	// window in which an empty file is visible is a single write long.
	filePath, err := reservePlanFilePath(dir, slugify(title), m.logger)
	if err != nil {
		return nil, err
	}
	// The reserve created a real placeholder file. Every failure AFTER this
	// point must remove it, or the slug is consumed forever (the next plan of
	// the same title silently becomes -2) and an empty .md accumulates per
	// failure (bughunt LOW). Best-effort: the store row is the source of
	// truth, the placeholder is only the lease on the name.
	defer func() {
		if err != nil {
			_ = os.Remove(filePath)
		}
	}()

	plan := NewPlan(title, description, projectID, filePath, sessionID)
	plan.GoalID = goalID

	if err := m.store.CreatePlan(ctx, plan); err != nil {
		err = fmt.Errorf("create plan: %w", err)
		return nil, err
	}

	if sessionID != "" {
		if err := m.store.LinkSession(ctx, plan.ID, sessionID); err != nil {
			m.logger.Warn("failed to link session", "plan_id", plan.ID, "error", err)
		}
	}

	// Write initial plan.md with empty phases.
	if err := WritePlanMarkdown(filePath, plan, nil); err != nil {
		m.logger.Warn("failed to write initial plan markdown", "path", filePath, "error", err)
	}

	// Transition from planning -> draft.
	plan.State = StateDraft
	plan.UpdatedAt = time.Now().UTC()
	if err := m.store.SetPlanState(ctx, plan.ID, StateDraft); err != nil {
		err = fmt.Errorf("set plan state to draft: %w", err)
		return nil, err
	}
	// The create-path write is the plan machine's first edge (planning ->
	// draft, the one SetPlanState the table comments allow alongside the
	// lease primitives). Publish AFTER the write committed (plan leaf 02/04).
	m.publishPlanState(plan.ID, StatePlanning, StateDraft, kanban.EventReasonCreate)

	m.publishEvent("plan.created", map[string]any{
		"plan_id":    plan.ID,
		"title":      plan.Title,
		"project_id": plan.ProjectID,
	})

	m.logger.Info("plan created", "plan_id", plan.ID, "title", title)
	return plan, nil
}

// SubmitPlan transitions a plan from draft to pending_approval (or auto-approves).
//
// R2-5 (regression-review 2026-11-11): a plan sitting in PLANNING (the
// RevisePlan rework state) is accepted here via the guarded table's
// planning->draft edge — CompletePlanningRevision's transition is fired
// FIRST, so the revised plan re-enters the normal draft lifecycle instead of
// stranding in planning (SubmitPlan was draft-only and no production caller
// ever invoked CompletePlanningRevision). A non-planning plan keeps the
// byte-identical draft->pending_approval CAS. Without retention pruning
// touching planning rows promptly, the pre-fix behavior left every revised
// plan permanently unsubmitable.
func (m *PlanManager) SubmitPlan(ctx context.Context, planID string) error {
	// Planning-strand rescue: a revised plan reaches submit through the
	// ONE legal planning->draft writer. No-op (table-refused, CAS-guarded)
	// for every other state, so draft/pending_approval/... rows are
	// untouched and a concurrent racer that moved the row loses cleanly.
	// A missing row is surfaced by the draft CAS below, not silenced here.
	if err := m.CompletePlanningRevision(ctx, planID); err != nil && !errors.Is(err, ErrPlanNotFound) {
		m.logger.Debug("submit: plan not in planning — normal submit path", "plan_id", planID)
	}
	// Table-driven CAS (C-P4): draft -> pending_approval, with the from-set
	// pinned to draft — the pre-migration guard's exact accepted set.
	if err := m.transitionFrom(ctx, planID, []PlanState{StateDraft}, StatePendingApproval, kanban.EventReasonSubmit); err != nil {
		return err
	}
	plan, err := m.store.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}

	phases, err := m.store.GetPhases(ctx, planID)
	if err != nil {
		m.logger.Warn("failed to get phases for submit event", "plan_id", planID, "error", err)
	}
	totalSteps := 0
	for _, ph := range phases {
		totalSteps += ph.TotalSteps
	}
	m.publishEvent("plan.submitting", map[string]any{
		"plan_id":     planID,
		"title":       plan.Title,
		"phase_count": len(phases),
		"step_count":  totalSteps,
		// session_id scopes the chat notification: an empty value marks a
		// machine-originated (evolver sink) plan that belongs to NO chat
		// and must never render inside a user's conversation.
		"session_id": plan.SourceSession,
	})

	// Auto-approve if approval is not required.
	if !m.config.Approval.RequireApproval {
		m.logger.Info("auto-approving plan", "plan_id", planID)
		// Use empty session/by for auto-approval.
		if err := m.ApprovePlan(ctx, planID, "", "system"); err != nil {
			return fmt.Errorf("auto-approve plan: %w", err)
		}
	}

	m.logger.Info("plan submitted", "plan_id", planID)
	return nil
}

// ApprovePlan approves a pending plan, records the signoff, and triggers synthesis.
// The guard is the table-driven CAS (C-P4): pending_approval -> approved only.
// The from-set stays pinned to pending_approval — the table's
// synthesizing->approved row exists for the reclaimer's stranded-lease path,
// and a normal approve must never fire it (that would stomp a live expander).
func (m *PlanManager) ApprovePlan(ctx context.Context, planID, sessionID, by string) error {
	if err := m.transitionFrom(ctx, planID, []PlanState{StatePendingApproval}, StateApproved, kanban.EventReasonApprove); err != nil {
		return err
	}

	signoff := NewPlanSignoff(planID, "", sessionID, by, "approved", "")
	if err := m.store.CreateSignoff(ctx, signoff); err != nil {
		return fmt.Errorf("create signoff: %w", err)
	}

	// T4 targeted update: stamp the approval columns on the CURRENT row
	// (re-read), not on a pre-transition copy — the state move is already
	// committed by the CAS above.
	now := time.Now().UTC()
	if err := m.updatePlanColumns(ctx, planID, func(p *Plan) {
		p.ApprovedAt = &now
		p.ApprovedBy = by
		p.UpdatedAt = now
	}); err != nil {
		return err
	}

	m.publishEvent("plan.approved", map[string]any{
		"plan_id": planID,
		"by":      by,
	})

	m.logger.Info("plan approved", "plan_id", planID, "by", by)

	// Synthesis is best-effort once approval is committed. A failure here must
	// not fail the operator approve RPC or roll back the CAS, signoff, or
	// approval columns — only those earlier errors stay fatal.
	// runSynthesisReclaimPass re-drives approved plans that have no live task.
	//
	// Per-goal outcome (issue #148): a sibling create — several goals' plans
	// approved one after another — must never be aborted by this plan's
	// expansion failure, so the call still returns nil. The failure rides on
	// the plan row itself (synthesis_error column) instead of the return
	// value: operators and the resume sweep see WHICH plan failed and why,
	// and a successful re-drive clears it.
	if err := m.Synthesize(ctx, planID); err != nil {
		m.logger.Error("synthesize plan after approval failed",
			"plan_id", planID, "error", err)
		if markErr := m.updatePlanColumns(ctx, planID, func(p *Plan) {
			p.SynthesisError = err.Error()
			p.UpdatedAt = time.Now().UTC()
		}); markErr != nil {
			m.logger.Warn("failed to record synthesis error on plan row",
				"plan_id", planID, "error", markErr)
		}
		return nil
	}

	return nil
}

// RejectPlan rejects a pending plan.
func (m *PlanManager) RejectPlan(ctx context.Context, planID, sessionID, by, reason string) error {
	// Table-driven CAS first (C-P4): pending_approval -> cancelled. The
	// signoff is written only when this caller WINS the transition, so a
	// losing racer leaves no duplicate audit row behind.
	if err := m.transitionFrom(ctx, planID, []PlanState{StatePendingApproval}, StateCancelled, kanban.EventReasonReject); err != nil {
		return err
	}
	plan, err := m.store.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}

	signoff := NewPlanSignoff(planID, "", sessionID, by, "rejected", reason)
	if err := m.store.CreateSignoff(ctx, signoff); err != nil {
		return fmt.Errorf("create signoff: %w", err)
	}

	m.publishEvent("plan.rejected", map[string]any{
		"plan_id": planID,
		"title":   plan.Title,
		"by":      by,
		"reason":  reason,
		// Prefer the plan's owning session; fall back to the acting
		// session so the notification scopes to a chat when possible.
		"session_id": firstNonEmpty(plan.SourceSession, sessionID),
	})

	m.logger.Info("plan rejected", "plan_id", planID, "by", by)
	return nil
}

// RevisePlan requests revision of a plan, sending it back to planning state.
// The from-set is the table's edges into planning — pending_approval,
// cancelled, draft, and FAILED (audit F7: the failed->planning row lands in
// planTransitions, so a failed plan is revisable; leaf 06's
// TestRevisePlanAcceptsFailed tests exactly that row).
func (m *PlanManager) RevisePlan(ctx context.Context, planID, sessionID, feedback string) error {
	plan, err := m.store.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}

	// Check revision count.
	if m.config.Approval.MaxRevisions > 0 {
		revCount, err := m.store.GetRevisionCount(ctx, planID)
		if err != nil {
			return fmt.Errorf("get revision count: %w", err)
		}
		if revCount >= m.config.Approval.MaxRevisions {
			return fmt.Errorf("plan %s has reached max revisions (%d)", planID, m.config.Approval.MaxRevisions)
		}
	}

	// Table-driven CAS second, from-set pinned to the revise command's
	// accepted states (leaf doc: pending_approval, cancelled, failed). The
	// table's draft->planning row serves the rework path, not this command.
	// The CAS pins the observed state, so a racer that moved the row
	// between the read above and this write loses.
	if err := m.transitionFrom(ctx, planID,
		[]PlanState{StatePendingApproval, StateCancelled, StateFailed}, StatePlanning, kanban.EventReasonRevise); err != nil {
		return err
	}

	signoff := NewPlanSignoff(planID, "", sessionID, "", "revision_requested", feedback)
	if err := m.store.CreateSignoff(ctx, signoff); err != nil {
		return fmt.Errorf("create signoff: %w", err)
	}

	// T4 targeted update: revision_count/updated_at on the CURRENT row.
	if err := m.updatePlanColumns(ctx, planID, func(p *Plan) {
		p.RevisionCount++
		p.UpdatedAt = time.Now().UTC()
	}); err != nil {
		return err
	}
	plan.RevisionCount++

	m.publishEvent("plan.revised", map[string]any{
		"plan_id":        planID,
		"revision_count": plan.RevisionCount,
		"feedback":       feedback,
	})

	m.logger.Info("plan revised", "plan_id", planID, "revision_count", plan.RevisionCount)
	return nil
}

// CompletePlanningRevision returns a revised plan from planning to draft
// (bughunt 2026-11-10 B3): RevisePlan legally moves plans INTO planning, but
// the frozen table previously had NO edges out of it, so every revised plan
// stranded forever — SubmitPlan requires draft, ApprovePlan requires
// pending_approval, and PruneStalePlans declined to touch planning rows. The
// revision writer (the plan-compiler / revision rework path) calls this once
// the revised content is written back, handing the plan to the normal draft
// lifecycle (SubmitPlan -> ApprovePlan -> ...). The table's planning->draft
// row is the only legal writer; anything else is refused by the guarded CAS.
func (m *PlanManager) CompletePlanningRevision(ctx context.Context, planID string) error {
	if err := m.transitionFrom(ctx, planID, []PlanState{StatePlanning}, StateDraft, kanban.EventReasonRevisionDone); err != nil {
		return err
	}
	m.publishEvent("plan.revision_completed", map[string]any{
		"plan_id": planID,
	})
	m.logger.Info("plan revision completed, returned to draft", "plan_id", planID)
	return nil
}

// ConfirmPlan confirms a completed plan after review.
func (m *PlanManager) ConfirmPlan(ctx context.Context, planID, sessionID, by string) error {
	// Table-driven CAS FIRST (C-P4): completed/failed/cancelled -> confirmed.
	// The signoff and the confirmation columns are written only when this
	// caller WINS, so a racer (e.g. a concurrent CancelPlan) neither clobbers
	// the row nor leaves a duplicate audit trail. The old shape — read,
	// guard, signoff, full-row UpdatePlan from the stale copy — lost both:
	// last-writer-wins on the state AND stomped the live
	// task_id/synthesis_lease_until columns of the pre-transition copy (T4).
	if err := m.transition(ctx, planID, StateConfirmed, kanban.EventReasonConfirm); err != nil {
		return err
	}
	plan, err := m.store.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}

	signoff := NewPlanSignoff(planID, "", sessionID, by, "confirmed", "")
	if err := m.store.CreateSignoff(ctx, signoff); err != nil {
		return fmt.Errorf("create signoff: %w", err)
	}

	// T4 targeted update: confirmation columns on the CURRENT row (re-read),
	// which already carries the winner's state and any lease/task columns.
	now := time.Now().UTC()
	if err := m.updatePlanColumns(ctx, planID, func(p *Plan) {
		p.ConfirmedAt = &now
		p.ConfirmedBy = by
		p.UpdatedAt = now
	}); err != nil {
		return err
	}

	// Cascade the confirmation onto the plan's phases (leaf 02 T3, audit
	// M-6): PhaseConfirmed had zero writers, so phase rows kept their
	// pre-confirm state forever. Every phase that has RESOLVED — completed
	// or failed — moves to PhaseConfirmed; pending/in_progress phases are
	// left alone (they never resolved, so signing off the plan's outcome
	// does not retroactively confirm their work). Best-effort per phase with
	// no new bus event and unchanged payload shapes (the phase_state intake
	// mirror is fail-soft and only tracks live transitions; plan.confirmed
	// remains the plan-level truth for the confirmation).
	phases, phaseErr := m.store.GetPhases(ctx, planID)
	if phaseErr != nil {
		m.logger.Warn("failed to get phases for plan.md update", "plan_id", planID, "error", phaseErr)
	} else {
		cascaded := 0
		for _, ph := range phases {
			if ph.State != PhaseCompleted && ph.State != PhaseFailed {
				continue
			}
			if err := m.store.SetPhaseState(ctx, ph.ID, PhaseConfirmed); err != nil {
				m.logger.Warn("failed to cascade phase confirmed", "plan_id", planID, "phase_id", ph.ID, "error", err)
				continue
			}
			ph.State = PhaseConfirmed
			cascaded++
		}
		if cascaded > 0 {
			// Re-read so the plan.md tail below renders the confirmed phase
			// states instead of the pre-cascade snapshot.
			if refreshed, rErr := m.store.GetPhases(ctx, planID); rErr == nil {
				phases = refreshed
			}
		}
	}

	// Update plan.md to reflect confirmed state.
	if err := UpdatePlanStatus(plan.FilePath, StateConfirmed, phasesToValues(phases)); err != nil {
		m.logger.Warn("failed to update plan.md on confirm", "plan_id", planID, "error", err)
	}

	m.publishEvent("plan.confirmed", map[string]any{
		"plan_id": planID,
		"title":   plan.Title,
		"by":      by,
		// Prefer the plan's owning session; fall back to the acting
		// session so the notification scopes to a chat when possible.
		"session_id": firstNonEmpty(plan.SourceSession, sessionID),
	})

	m.logger.Info("plan confirmed", "plan_id", planID, "by", by)
	return nil
}

// CancelPlan cancels a plan for the given reason.
func (m *PlanManager) CancelPlan(ctx context.Context, planID, reason string) error {
	// Table-driven CAS (C-P4): cancelled's from-set (draft, pending_approval,
	// approved, synthesizing, executing) is exactly the old non-terminal
	// guard's accepted set minus completed/planning — the audited table has
	// no edge into cancelled from those states (see planTransitions; delta
	// noted in the leaf report).
	if err := m.transition(ctx, planID, StateCancelled, kanban.EventReasonCancel); err != nil {
		return err
	}

	// F8 cascade (leaf 06 T2): a cancelled plan must not orphan its phases
	// or leave live child tasks running. Read the phase rows BEFORE the
	// transition bookkeeping below so the cascade sees the synthesis-time
	// child task ids even for a plan whose maps were never restored.
	phases, phErr := m.store.GetPhases(ctx, planID)
	if phErr != nil {
		// Best-effort parity with the sibling lifecycle writes: the plan
		// row is already cancelled; a phase read failure is logged, not
		// a cancel failure (the operator's stop intent already committed).
		m.logger.Warn("cancel cascade: failed to read phases", "plan_id", planID, "error", phErr)
		phases = nil
	}

	// Phase rollback: pending/in_progress phases reset to 'pending' per the
	// leaf's Q2 rule (cancelled plan -> phases back to pending, NOT failed —
	// a revise->re-run loop restarts them; the cancel itself is not a phase
	// failure). Completed/confirmed/failed phases are terminal records of
	// what already happened and stay untouched.
	for _, ph := range phases {
		if ph.State == PhasePending || ph.State == PhaseInProgress {
			if err := m.store.SetPhaseState(ctx, ph.ID, PhasePending); err != nil {
				m.logger.Warn("cancel cascade: failed to reset phase", "plan_id", planID, "phase_id", ph.ID, "error", err)
			}
		}
	}

	// Task cancellation through the existing task-cancel machinery: the
	// plan's parent task plus every child task bound to a phase. The seam
	// is optional (TaskCanceller); a creator without it (unit tests,
	// legacy adapters) skips this step rather than failing the cancel.
	if canceller, ok := m.taskCreator.(TaskCanceller); ok && m.taskCreator != nil {
		cancelTask := func(taskID string) {
			if taskID == "" {
				return
			}
			if cerr := canceller.CancelTask(ctx, taskID, "plan cancelled: "+reason); cerr != nil {
				m.logger.Warn("cancel cascade: task cancel failed", "plan_id", planID, "task_id", taskID, "error", cerr)
			}
		}
		// Parent task: from the durable plan row first, then the in-memory
		// map (Synthesize records both; a pre-restore manager may only
		// have the map).
		if p, gerr := m.store.GetPlan(ctx, planID); gerr == nil && p != nil {
			cancelTask(p.TaskID)
			m.mu.RLock()
			parentMapped := m.taskPlanMap[p.TaskID] == p.ID
			m.mu.RUnlock()
			if p.TaskID == "" && !parentMapped {
				// Nothing durable to cancel; child tasks below still ride
				// their phase rows' task_id.
				m.logger.Debug("cancel cascade: plan has no parent task", "plan_id", planID)
			}
		}
		for _, ph := range phases {
			if ph.TaskID != "" {
				cancelTask(ph.TaskID)
			}
		}
	} else if m.taskCreator != nil {
		m.logger.Debug("cancel cascade: taskCreator lacks TaskCanceller seam; phases-only cancel", "plan_id", planID)
	}

	m.publishEvent("plan.cancelled", map[string]any{
		"plan_id": planID,
		"reason":  reason,
	})

	m.logger.Info("plan cancelled", "plan_id", planID, "reason", reason)
	return nil
}

// PruneStalePlans applies age-based retention to THIS manager's store (the
// evolver sink points it at the dedicated plans-evolver store, so it only
// ever touches machine-originated plans). For every plan whose UpdatedAt is
// older than retentionDays:
//   - draft / cancelled / failed rows are DELETED (row + its markdown file);
//   - a stale pending_approval / planning / approved plan is CANCELLED first
//     (never silently dropped) so a human queue drains instead of aging
//     forever, then left for the next cycle's delete once terminal.
//
// Executing / synthesizing / completed / confirmed plans are never touched:
// executing is live work, and completed/confirmed are the audit record of
// applied changes. Returns counts of deleted and cancelled plans. A
// retentionDays <= 0 disables the sweep (0,0,nil). Best-effort per plan: one
// plan's error is logged and does not abort the sweep.
//
// Rationale (evolver decision brief 2026-10-22, item 4): the evolver sink
// grew to 12,477 plans / 50 MB because nothing ever aged out a draft or a
// proposal nobody reviewed. Creation-time caps bound the pending queue but
// not the accumulated draft/cancelled history; this is the retention half.
func (m *PlanManager) PruneStalePlans(ctx context.Context, retentionDays int) (deleted, cancelled int, err error) {
	if m == nil || retentionDays <= 0 {
		return 0, 0, nil
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)

	// Enumerate the states retention may act on. ListPlansByState is bounded
	// by limit; pass a generous cap so a large backlog drains in one cycle
	// (each pass is a cheap indexed scan). Terminal drafts/cancelled/failed
	// are deleted; stale open states are cancelled.
	deletable := []PlanState{StateDraft, StateCancelled, StateFailed}
	cancellable := []PlanState{StatePendingApproval, StateApproved, StatePlanning}

	// Stale planning rows are cancellable since B3 (bughunt 2026-11-10):
	// the transition table now carries planning -> cancelled exactly so
	// retention can prune the backlog of revised plans that were never
	// returned to draft. The old behavior — count-and-warn, no action —
	// existed only because the frozen table had no edge to fire.

	for _, state := range cancellable {
		plans, lerr := m.store.ListPlansByState(ctx, state, 10000)
		if lerr != nil {
			m.logger.Warn("retention: list plans failed", "state", state, "error", lerr)
			err = lerr
			continue
		}
		for _, p := range plans {
			if p == nil || p.UpdatedAt.After(cutoff) {
				continue
			}
			if cerr := m.CancelPlan(ctx, p.ID, fmt.Sprintf("retention: pending over %d days", retentionDays)); cerr != nil {
				m.logger.Warn("retention: cancel stale plan failed", "plan_id", p.ID, "error", cerr)
				err = cerr
				continue
			}
			cancelled++
		}
	}

	for _, state := range deletable {
		plans, lerr := m.store.ListPlansByState(ctx, state, 10000)
		if lerr != nil {
			m.logger.Warn("retention: list plans failed", "state", state, "error", lerr)
			err = lerr
			continue
		}
		for _, p := range plans {
			if p == nil || p.UpdatedAt.After(cutoff) {
				continue
			}
			// Remove the markdown file first (best-effort: a missing or
			// already-gone file is not a delete failure), then the row.
			if p.FilePath != "" {
				if rmErr := os.Remove(p.FilePath); rmErr != nil && !os.IsNotExist(rmErr) {
					m.logger.Warn("retention: remove plan file failed", "plan_id", p.ID, "path", p.FilePath, "error", rmErr)
				}
			}
			if derr := m.store.DeletePlan(ctx, p.ID); derr != nil {
				m.logger.Warn("retention: delete stale plan failed", "plan_id", p.ID, "error", derr)
				err = derr
				continue
			}
			deleted++
		}
	}

	if deleted > 0 || cancelled > 0 {
		m.logger.Info("retention pruned stale plans",
			"deleted", deleted, "cancelled", cancelled, "retention_days", retentionDays)
	}
	return deleted, cancelled, err
}

// synthesisReleaseTarget is the optional store seam that restores a failed
// expansion to a caller-chosen state. ReleasePlanSynthesis always writes
// draft; an approved plan must come back to approved so the approval stands
// and runSynthesisReclaimPass can re-drive it. A store without the seam
// keeps the draft release.
type synthesisReleaseTarget interface {
	ReleasePlanSynthesisTo(ctx context.Context, id string, restore PlanState) error
}

func releaseSynthesisTo(store PlanStore, ctx context.Context, id string, restore PlanState) error {
	if rel, ok := store.(synthesisReleaseTarget); ok {
		return rel.ReleasePlanSynthesisTo(ctx, id, restore)
	}
	return store.ReleasePlanSynthesis(ctx, id)
}

// ---------------------------------------------------------------------------
// Synthesis
// ---------------------------------------------------------------------------

// Synthesize creates the task hierarchy from the plan: a parent task with child
// tasks for each phase, and TaskSteps for each parsed step within a phase.
// This method is idempotent: if the plan already has a TaskID, it returns nil
// without re-creating tasks.
func (m *PlanManager) Synthesize(ctx context.Context, planID string) error {
	if m.taskCreator == nil {
		return fmt.Errorf("synthesize plan %s: no task creator configured", planID)
	}

	plan, err := m.store.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}

	// Idempotency guard: if the plan already has a TaskID, synthesis was
	// already performed (e.g., by a concurrent ApprovePlan or a restart).
	if plan.TaskID != "" {
		m.logger.Info("synthesize: plan already has task, skipping",
			"plan_id", planID, "task_id", plan.TaskID)
		return nil
	}

	// CLAIM before creating anything (bughunt 2026-10-08). The TaskID guard
	// above is correct but unreachable after a mid-expansion failure: this
	// function creates the parent task ~20 lines BEFORE it records task_id, so
	// any error in between (a failed CreateTaskStep, the depends_on resolution,
	// a daemon restart) returned with the task created and task_id never
	// written — leaving the plan at draft+empty-task_id, indistinguishable from
	// a plan that had never been expanded. With plan approval auto-granted, the
	// next SubmitPlan re-fired the state CAS and re-expanded, forever:
	// 797 draft plans produced 20.7M orphan tasks at ~14k/s.
	//
	// Claiming first makes "expanded but died" a VISIBLE state instead of an
	// invisible one. The lease means a live claim blocks a concurrent expander,
	// while an expired one (the daemon died) is re-claimable so a single dead
	// claim cannot strand a plan forever.
	claimed, err := m.store.ClaimPlanSynthesis(ctx, planID, synthesisLeaseDuration)
	if err != nil {
		return fmt.Errorf("claim plan for synthesis: %w", err)
	}
	if !claimed {
		// Someone else holds the claim, or the plan is not claimable. Either
		// way there is nothing to do — crucially, NOT an error, because the
		// loser of a race must not bubble up into the caller that then retries.
		m.logger.Info("synthesize: plan already claimed or not claimable, skipping",
			"plan_id", planID, "state", plan.State)
		return nil
	}
	// Claim won: the row moved to synthesizing (or refreshed its lease on a
	// stale synthesizing row, where from==to publishes nothing).
	m.publishPlanState(planID, plan.State, StateSynthesizing, kanban.EventReasonSynthesize)

	// Past this point the plan is OURS and carries a lease. Every failure path
	// must release or park it, or the plan would stay claimed forever.
	// plan.State is the pre-claim snapshot: an approved row must come back to
	// approved, not draft, or the approval is undone and runSynthesisReclaimPass
	// never sees the no-task residue.
	release := func(park bool) {
		var relErr error
		to := StateDraft
		reason := kanban.EventReasonRelease
		if park {
			relErr = m.store.ParkPlanSynthesis(ctx, planID)
			to = StateFailed
			reason = kanban.EventReasonPark
		} else if plan.State == StateApproved {
			relErr = releaseSynthesisTo(m.store, ctx, planID, StateApproved)
			to = StateApproved
		} else {
			relErr = m.store.ReleasePlanSynthesis(ctx, planID)
		}
		if relErr != nil {
			m.logger.Error("failed to release synthesis claim", "plan_id", planID, "error", relErr)
			return
		}
		// Lease discharged: the row left synthesizing. Publish AFTER the
		// store write succeeded (plan leaf 02/04).
		m.publishPlanState(planID, StateSynthesizing, to, reason)
	}

	// Bound the retries: a plan that cannot be expanded is parked (terminal)
	// rather than re-entered on every auto-approve pass.
	defer func() {
		if fresh, gerr := m.store.GetPlan(ctx, planID); gerr == nil &&
			fresh.TaskID == "" && fresh.State == StateSynthesizing {
			// attempts counts this pass PLUS everything before it. ReleasePlanSynthesis
			// is what persists the increment, so the value must be read BEFORE the
			// decision — reading it after (or relying on the row) sees a stale 0 and
			// the plan never reaches the park threshold (bughunt 2026-10-08: the bound
			// has to actually bound).
			attempts := fresh.SynthesisAttempts + 1
			if attempts >= MaxSynthesisAttempts {
				m.logger.Warn("plan synthesis exceeded max attempts; parking",
					"plan_id", planID, "attempts", attempts)
				release(true)
			} else {
				release(false)
			}
		}
	}()

	phases, err := m.store.GetPhases(ctx, planID)
	if err != nil {
		return fmt.Errorf("get phases: %w", err)
	}

	// Parse the plan.md file for step details.
	var parsed *ParsedPlan
	if plan.FilePath != "" {
		parsed, err = ParsePlan(plan.FilePath)
		if err != nil {
			m.logger.Warn("failed to parse plan.md for synthesis, using phases only", "error", err)
		}
	}

	// Build parsed phase lookup by sequence.
	parsedPhases := make(map[int]ParsedPhase)
	if parsed != nil {
		for _, pp := range parsed.Phases {
			parsedPhases[pp.Sequence] = pp
		}
	}

	// If no phases in store, create them from parsed plan.
	if len(phases) == 0 && parsed != nil {
		for _, pp := range parsed.Phases {
			phase := NewPlanPhase(planID, pp.Name, pp.Sequence, len(pp.Steps))
			if err := m.store.CreatePhase(ctx, phase); err != nil {
				return fmt.Errorf("create phase %s: %w", pp.Name, err)
			}
			phases = append(phases, phase)
		}
	}

	// Create parent task.
	parentTask, err := m.taskCreator.CreateTask(ctx, plan.Title, plan.Description)
	if err != nil {
		return fmt.Errorf("create parent task: %w", err)
	}

	// Carry the plan's goal onto the parent task (goal-completion C3, via
	// the optional TaskGoalSetter seam). Best-effort with a Warn: a creator
	// without the seam (or a failed stamp) leaves a goal-less task, which
	// stays legacy-legal (bughunt 2026-10-14).
	m.stampTaskGoal(ctx, parentTask.ID, plan.GoalID)

	// Link the plan's source session to the parent task.
	if plan.SourceSession != "" {
		if err := m.taskCreator.LinkSession(ctx, parentTask.ID, plan.SourceSession); err != nil {
			m.logger.Warn("failed to link session to parent task", "error", err)
		}
	}

	// Store the parent TaskID on the plan via the T4 re-read seam
	// (bughunt 2026-11-10 B5): the local `plan` snapshot was taken BEFORE
	// the (possibly slow) task-creation calls, so a full-row UpdatePlan
	// from it reverts columns stamped concurrently — SetPlanGoal's goal_id,
	// RevisePlan's revision_count. updatePlanColumns re-reads the CURRENT
	// row and mutates only the synthesis-owned columns.
	// The claim is discharged by this write: the lease column is cleared in the
	// SAME statement that records task_id, so there is no window in which a
	// successful expansion still looks claimed.
	taskID := parentTask.ID
	newState := StateExecuting
	if err := m.updatePlanColumns(ctx, planID, func(p *Plan) {
		p.TaskID = taskID
		p.State = newState
		p.SynthesisLeaseUntil = nil
		// Expansion succeeded: clear any recorded synthesis failure
		// (issue #148 per-goal outcome) — the row is no longer failed.
		p.SynthesisError = ""
		p.UpdatedAt = time.Now().UTC()
	}); err != nil {
		return fmt.Errorf("update plan with task ID: %w", err)
	}
	// Expansion succeeded: synthesizing -> executing is committed. Publish
	// AFTER the write (plan leaf 02/04); the legacy plan.executing event
	// below stays untouched.
	m.publishPlanState(planID, StateSynthesizing, newState, kanban.EventReasonExecuting)
	// Keep the in-memory copy usable for the tail writes below without
	// letting it back onto the persistence path.
	plan.TaskID = taskID
	plan.State = newState
	plan.SynthesisLeaseUntil = nil

	// Track parent task -> plan mapping for OnTaskCompleted.
	m.mu.Lock()
	m.taskPlanMap[parentTask.ID] = planID
	m.mu.Unlock()

	// Create child tasks for each phase and steps within them.
	for _, phase := range phases {
		phaseLabel := fmt.Sprintf("Phase %d: %s", phase.Sequence, phase.Name)
		childTask, err := m.taskCreator.CreateTask(ctx, phaseLabel, fmt.Sprintf("Plan %s - %s", plan.Title, phaseLabel))
		if err != nil {
			return fmt.Errorf("create child task for phase %s: %w", phase.Name, err)
		}

		// Track mapping: childTask.ID -> phase.ID, childTask.ID -> plan.ID
		m.mu.Lock()
		m.phaseTaskMap[childTask.ID] = phase.ID
		m.taskPlanMap[childTask.ID] = planID
		m.mu.Unlock()

		// Carry the plan's goal onto the phase child task (same C3 seam).
		m.stampTaskGoal(ctx, childTask.ID, plan.GoalID)

		// Persist the child task ID on the phase for recovery after restart.
		phase.TaskID = childTask.ID
		if err := m.store.UpdatePhase(ctx, phase); err != nil {
			m.logger.Warn("failed to persist phase task ID", "phase_id", phase.ID, "error", err)
		}

		// Create TaskSteps from parsed step details.
		pp, hasParsed := parsedPhases[phase.Sequence]
		if hasParsed {
			// First pass: create all steps and collect (stepNumber -> stepID) mapping.
			stepIDByNumber := make(map[int]string)
			createdSteps := make(map[int]*task.TaskStep) // stepNumber -> created step
			for seq, step := range pp.Steps {
				ts, err := m.taskCreator.CreateTaskStep(ctx, childTask.ID, step.Description, seq+1)
				if err != nil {
					return fmt.Errorf("create task step %d for phase %s: %w", step.Number, phase.Name, err)
				}
				stepIDByNumber[step.Number] = ts.ID
				createdSteps[step.Number] = ts
			}
			// Second pass: resolve depends_on step numbers to step IDs and update.
			for _, step := range pp.Steps {
				if len(step.DependsOn) == 0 {
					continue
				}
				ts := createdSteps[step.Number]
				var depIDs []string
				for _, num := range step.DependsOn {
					if depID, ok := stepIDByNumber[num]; ok {
						depIDs = append(depIDs, depID)
					} else {
						m.logger.Warn("plan step depends_on references unknown step number, skipping",
							"step_id", ts.ID, "dep_number", num)
					}
				}
				ts.DependsOn = depIDs
				if err := m.taskCreator.UpdateTaskStep(ctx, ts); err != nil {
					return fmt.Errorf("update dependencies for step %d in phase %s: %w", step.Number, phase.Name, err)
				}
			}
		} else {
			// No parsed steps; create a single placeholder step.
			_, err := m.taskCreator.CreateTaskStep(ctx, childTask.ID, phaseLabel, 1)
			if err != nil {
				return fmt.Errorf("create placeholder step for phase %s: %w", phase.Name, err)
			}
		}

		// Mark phase in progress.
		if err := m.store.SetPhaseState(ctx, phase.ID, PhaseInProgress); err != nil {
			m.logger.Warn("failed to set phase state to in_progress", "phase_id", phase.ID, "error", err)
		}
		m.publishEvent("plan.phase_state", map[string]any{
			"plan_id":  planID,
			"phase_id": phase.ID,
			"state":    string(PhaseInProgress),
		})

		// Set TotalJobs on the child task and schedule its steps.
		stepCount := len(pp.Steps)
		if !hasParsed {
			stepCount = 1 // placeholder step
		}
		if err := m.taskCreator.SetTaskJobCount(ctx, childTask.ID, stepCount); err != nil {
			m.logger.Warn("failed to set task job count", "task_id", childTask.ID, "error", err)
		}
		if err := m.taskCreator.ScheduleSteps(ctx, childTask.ID); err != nil {
			m.logger.Warn("failed to schedule steps for phase task", "task_id", childTask.ID, "error", err)
		}
	}

	// Update plan.md to reflect executing state.
	if err := UpdatePlanStatus(plan.FilePath, StateExecuting, phasesToValues(phases)); err != nil {
		m.logger.Warn("failed to update plan.md on synthesize", "error", err)
	}

	m.publishEvent("plan.executing", map[string]any{
		"plan_id":     planID,
		"task_id":     parentTask.ID,
		"phase_count": len(phases),
	})

	m.logger.Info("plan synthesized", "plan_id", planID, "task_id", parentTask.ID, "phases", len(phases))
	return nil
}

// ---------------------------------------------------------------------------
// Progress tracking
// ---------------------------------------------------------------------------

// OnStepFailed is the step-failure counterpart of OnStepCompleted (leaf 02
// T1, audit M-6): it records the failure through the EXISTING
// IncrementPhaseProgress accounting (failed_steps — no parallel counter) and
// flips the phase to PhaseFailed once the failure count makes the phase
// unrecoverable: failed_steps >= total_steps. The writer only ever fires for
// phases in in_progress — a phase that already reached a terminal phase state
// (completed/failed/confirmed) is never rewritten by a late failure event.
//
// Failure accounting for a still-recoverable phase (failed_steps <
// total_steps) is recorded without a state change, mirroring OnStepCompleted:
// a partial failure does not kill the phase while steps remain.
func (m *PlanManager) OnStepFailed(ctx context.Context, taskID, stepID string) error {
	m.mu.RLock()
	phaseID, ok := m.phaseTaskMap[taskID]
	if !ok {
		m.mu.RUnlock()
		// Not a plan-tracked task; ignore silently (parity with OnStepCompleted).
		return nil
	}
	planID := m.taskPlanMap[taskID]
	m.mu.RUnlock()

	if err := m.store.IncrementPhaseProgress(ctx, phaseID, "failed_steps", 1); err != nil {
		return fmt.Errorf("increment phase progress: %w", err)
	}

	phases, err := m.store.GetPhases(ctx, planID)
	if err != nil {
		// Phase-failure detection is advisory; the failed_steps increment
		// above already succeeded, so don't fail the executor callback.
		return nil //nolint:nilerr // non-fatal: progress was recorded above
	}

	for _, phase := range phases {
		if phase.ID != phaseID || phase.State != PhaseInProgress {
			continue
		}
		if phase.TotalSteps > 0 && phase.FailedSteps >= phase.TotalSteps {
			if err := m.store.SetPhaseState(ctx, phaseID, PhaseFailed); err != nil {
				m.logger.Warn("failed to set phase failed", "phase_id", phaseID, "error", err)
			}
			m.publishEvent("plan.phase_state", map[string]any{
				"plan_id":  planID,
				"phase_id": phaseID,
				"state":    string(PhaseFailed),
			})
			break
		}
	}

	return nil
}

// OnStepCompleted is called when a task step completes. It updates the
// corresponding phase progress.
func (m *PlanManager) OnStepCompleted(ctx context.Context, taskID, stepID string) error {
	m.mu.RLock()
	phaseID, ok := m.phaseTaskMap[taskID]
	if !ok {
		m.mu.RUnlock()
		// Not a plan-tracked task; ignore silently.
		return nil
	}
	planID := m.taskPlanMap[taskID]
	m.mu.RUnlock()

	if err := m.store.IncrementPhaseProgress(ctx, phaseID, "completed_steps", 1); err != nil {
		return fmt.Errorf("increment phase progress: %w", err)
	}

	// planID was looked up under the lock above to scope the phase query.

	// Check if the phase is now complete.
	phases, err := m.store.GetPhases(ctx, planID)
	if err != nil {
		// Phase-completion detection is advisory; the step increment above
		// already succeeded, so don't fail the completion callback.
		return nil //nolint:nilerr // non-fatal: progress was recorded above
	}

	for _, phase := range phases {
		if phase.ID == phaseID && phase.CompletedSteps >= phase.TotalSteps && phase.TotalSteps > 0 {
			if err := m.store.SetPhaseState(ctx, phaseID, PhaseCompleted); err != nil {
				m.logger.Warn("failed to set phase completed", "phase_id", phaseID, "error", err)
			}
			m.publishEvent("plan.phase_state", map[string]any{
				"plan_id":  planID,
				"phase_id": phaseID,
				"state":    string(PhaseCompleted),
			})
			break
		}
	}

	return nil
}

// OnTaskCompleted is called when a task completes. If the task maps to a phase,
// it updates the phase state — honoring the completion STATUS (bughunt
// 2026-11-10 B1): the tactical scheduler publishes task.completed with
// status "completed" OR "failed" (internal/agent/tactical.go honest-completion
// payload, a94995c8 lineage), so a failed completion must land in PhaseFailed,
// never overwrite an existing PhaseFailed back to PhaseCompleted, and never
// silently pass review. If the task is the plan's parent task, it marks the
// plan completed. The status parameter defaults unknown/"completed"-shaped
// payloads to genuine success (legacy callers that have no status carry one);
// any other value (notably "failed") is treated as a failed completion.
func (m *PlanManager) OnTaskCompleted(ctx context.Context, taskID, status string) error {
	m.mu.RLock()
	phaseID, isPhase := m.phaseTaskMap[taskID]
	planID, hasPlan := m.taskPlanMap[taskID]
	m.mu.RUnlock()

	failed := status == "failed"
	if isPhase {
		// Child task (phase) completed. A failed completion records the
		// failure honestly: increment the phase's failed_steps accounting
		// and flip the phase to PhaseFailed — but never overwrite a
		// terminal PhaseFailed already written by OnStepFailed. Only a
		// genuinely-successful completion writes PhaseCompleted.
		if failed {
			phases, gerr := m.store.GetPhases(ctx, planID)
			if gerr != nil {
				m.logger.Warn("failed to get phases for failed completion", "phase_id", phaseID, "error", gerr)
			} else {
				for _, ph := range phases {
					if ph.ID != phaseID {
						continue
					}
					// Terminal PhaseFailed already recorded: a duplicate
					// failure event must not stomp the honest record.
					if ph.State == PhaseFailed {
						break
					}
					if ierr := m.store.IncrementPhaseProgress(ctx, phaseID, "failed_steps", 1); ierr != nil {
						m.logger.Warn("failed to record failed completion", "phase_id", phaseID, "error", ierr)
					}
					if serr := m.store.SetPhaseState(ctx, phaseID, PhaseFailed); serr != nil {
						m.logger.Warn("failed to set phase failed", "phase_id", phaseID, "error", serr)
					}
					break
				}
			}
			// The failure tail below is identical to the success tail
			// (all-phases-terminal eval -> completePlan's honest check),
			// so fall through to it with the failure state persisted.
		} else {
			// Success path: only write PhaseCompleted when the phase has
			// not already resolved terminal-failed — a success event
			// arriving after OnStepFailed recorded honest failure must
			// not resurrect the phase (same false-success B1 forbids).
			phases, gerr := m.store.GetPhases(ctx, planID)
			alreadyFailed := false
			if gerr == nil {
				for _, ph := range phases {
					if ph.ID == phaseID && ph.State == PhaseFailed {
						alreadyFailed = true
						break
					}
				}
			}
			if alreadyFailed {
				m.logger.Warn("success completion arrived for an already-failed phase; preserving PhaseFailed",
					"phase_id", phaseID)
			} else if err := m.store.SetPhaseState(ctx, phaseID, PhaseCompleted); err != nil {
				m.logger.Warn("failed to set phase completed", "phase_id", phaseID, "error", err)
			}
		}
		finalState := PhaseCompleted
		if failed {
			finalState = PhaseFailed
		}

		// The dead phase-completion bus event publish was REMOVED here
		// (audit F9, leaf 06 T3): zero subscribers across the whole bus
		// surface (verified against docs/generated/bus-topology.json before
		// deletion). The live consumer of phase completion is
		// plan.phase_state below — the intake projection rides it. A dead
		// publish only inflated the topology's orphan set.
		if hasPlan {
			m.publishEvent("plan.phase_state", map[string]any{
				"plan_id":  planID,
				"phase_id": phaseID,
				"state":    string(finalState),
			})
		}

		// Check if all phases of the plan have resolved. Leaf 02 T2 (audit
		// M-6 / plan F1): a FAILED phase counts as terminal — the check is
		// "all phases terminal", not "all completed". completePlan then does
		// the honest evaluation: any failed phase sends the plan to
		// StateFailed instead of silently completing it.
		allPhases, err := m.store.GetPhases(ctx, planID)
		if err != nil {
			m.logger.Warn("failed to get phases for completion check", "plan_id", planID, "error", err)
			return nil
		}
		allTerminal := len(allPhases) > 0
		for _, ph := range allPhases {
			if ph.State != PhaseCompleted && ph.State != PhaseFailed {
				allTerminal = false
				break
			}
		}
		if allTerminal {
			return m.completePlan(ctx, planID)
		}
		return nil
	}

	// Check if this is the plan's parent task via taskPlanMap.
	if !hasPlan {
		return nil
	}

	plan, err := m.store.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("get plan for completion: %w", err)
	}

	// Only transition if the plan is in executing state.
	if plan.State != StateExecuting {
		return nil
	}

	return m.completePlan(ctx, planID)
}

// completePlan transitions a plan to completed, updates plan.md, and
// publishes the plan.completed event. It is called either when the parent
// task completes or when all phase child tasks have completed.
//
// Leaf 02 T2 (audit M-6 / plan F1) makes the evaluation honest first: a plan
// with ANY failed phase fails instead of completing — an errored step must
// never pass review silently. The phase read below serves both the failure
// check and the plan.md / event tail (one read, pre-transition: phase rows
// and plan rows are separate writes).
func (m *PlanManager) completePlan(ctx context.Context, planID string) error {
	plan, err := m.store.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("get plan for completion: %w", err)
	}

	if plan.State != StateExecuting {
		return nil
	}

	phases, phaseErr := m.store.GetPhases(ctx, plan.ID)
	if phaseErr != nil {
		m.logger.Warn("failed to get phases for plan.md update", "plan_id", plan.ID, "error", phaseErr)
	} else {
		for _, ph := range phases {
			if ph.State == PhaseFailed {
				return m.failPlan(ctx, plan.ID)
			}
		}
	}

	// Table-driven CAS (C-P4): executing -> completed. The CAS pins the
	// observed state, so a concurrent CancelPlan that moved the row first
	// makes this a lost race: the completion must NOT clobber the cancel,
	// and the event/markdown below must not fire for a plan nobody completed.
	if err := m.transition(ctx, planID, StateCompleted, kanban.EventReasonComplete); err != nil {
		return err
	}

	// Update plan.md.
	if err := UpdatePlanStatus(plan.FilePath, StateCompleted, phasesToValues(phases)); err != nil {
		m.logger.Warn("failed to update plan.md on completion", "error", err)
	}

	totalSteps := 0
	for _, ph := range phases {
		totalSteps += ph.TotalSteps
	}
	m.publishEvent("plan.completed", map[string]any{
		"plan_id":     plan.ID,
		"title":       plan.Title,
		"phase_count": len(phases),
		"step_count":  totalSteps,
		"session_id":  plan.SourceSession,
	})

	m.logger.Info("plan completed", "plan_id", plan.ID)
	return nil
}

// failPlan transitions a plan whose phases all resolved with at least one
// PhaseFailed to StateFailed, and stamps the outcome into plan.md. It is the
// failure sibling of completePlan (leaf 02 T2, audit M-6 / plan F1). The
// transition rides the table's executing -> failed edge through the same
// guarded CAS as every other lifecycle move (C-P4), so a concurrent
// CancelPlan still wins with exactly one winner. No new bus event is
// published: the leaf contract pins the event surface unchanged (the
// plan.phase_state failure edges already rode the PhaseFailed writer).
func (m *PlanManager) failPlan(ctx context.Context, planID string) error {
	plan, err := m.store.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("get plan for failure: %w", err)
	}

	if plan.State != StateExecuting {
		return nil
	}

	if err := m.transition(ctx, planID, StateFailed, kanban.EventReasonFail); err != nil {
		return err
	}

	// Update plan.md with the failed outcome (best-effort, parity with
	// completePlan's markdown tail).
	phases, phaseErr := m.store.GetPhases(ctx, plan.ID)
	if phaseErr != nil {
		m.logger.Warn("failed to get phases for plan.md update", "plan_id", plan.ID, "error", phaseErr)
	}
	if err := UpdatePlanStatus(plan.FilePath, StateFailed, phasesToValues(phases)); err != nil {
		m.logger.Warn("failed to update plan.md on failure", "error", err)
	}

	m.logger.Info("plan failed: at least one phase failed", "plan_id", plan.ID)
	return nil
}

// GetPhases returns the phases for a plan, ordered by sequence (read-through
// to the store). The intake projection subscriber (leaf 06) uses it to
// rebuild the phase tree from a plan.executing bus event.
func (m *PlanManager) GetPhases(ctx context.Context, planID string) ([]*PlanPhase, error) {
	return m.store.GetPhases(ctx, planID)
}

// GetPhasesByTask returns the phases for the plan associated with the given
// taskID. If the task is not plan-tracked, returns nil without error.
// The phases are returned ordered by sequence (as stored).
func (m *PlanManager) GetPhasesByTask(ctx context.Context, taskID string) ([]*PlanPhase, error) {
	m.mu.RLock()
	planID, ok := m.taskPlanMap[taskID]
	m.mu.RUnlock()
	if !ok {
		return nil, nil
	}
	phases, err := m.store.GetPhases(ctx, planID)
	if err != nil {
		return nil, fmt.Errorf("get phases for plan %s: %w", planID, err)
	}
	return phases, nil
}

// RegisterTaskPlan records a taskID -> planID mapping. This is used by tests
// and by the Synthesize flow to link child tasks to their parent plan.
func (m *PlanManager) RegisterTaskPlan(taskID, planID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.taskPlanMap[taskID] = planID
}

// EnsureTaskPlan returns the plan already registered for taskID, or creates
// a minimal container plan (plan-compiler leaf 04): the seal path persists
// phase declarations through CreatePhase, which requires a parent plan row.
// Idempotent per task. The returned plan carries the compiled title.
//
// The check-and-create runs under ONE write lock: an earlier version took a
// read lock, checked, unlocked, then created + registered — two concurrent
// seals for the same task could both miss and create duplicate container
// plans (the first orphaned). CreatePlan does not touch m.mu (its store and
// bus calls are lock-free), so the write lock safely spans the whole
// check-create-register sequence.
func (m *PlanManager) EnsureTaskPlan(ctx context.Context, taskID, title string) (*Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if planID, ok := m.taskPlanMap[taskID]; ok {
		p, err := m.store.GetPlan(ctx, planID)
		if err == nil && p != nil {
			return p, nil
		}
		// Registered plan vanished from the store (test store reset):
		// fall through and create a fresh container.
		delete(m.taskPlanMap, taskID)
	}
	created, err := m.CreatePlan(ctx, title, "", "", "", "")
	if err != nil {
		return nil, fmt.Errorf("create seal container plan: %w", err)
	}
	m.taskPlanMap[taskID] = created.ID
	return created, nil
}

// RestoreMappings rebuilds phaseTaskMap and taskPlanMap from the persistent
// store. This should be called on daemon startup to recover plan-task
// associations for executing plans that survived a restart.
func (m *PlanManager) RestoreMappings(ctx context.Context) error {
	// F12 gap (leaf 06 T4): a crash between Synthesize's task writes and
	// the executing transition strands the plan in synthesizing with durable
	// child task ids on the phase rows — restoring only executing plans
	// left those mappings missing until the reclaimer moved the plan
	// forward. Cover both task-bearing in-flight states. (Limit 100 per
	// state kept: the reclaim path bounds synthesizing backlog separately.)
	plans, err := m.store.ListPlansByState(ctx, StateExecuting, 100)
	if err != nil {
		return fmt.Errorf("list executing plans for mapping restore: %w", err)
	}
	synthPlans, err := m.store.ListPlansByState(ctx, StateSynthesizing, 100)
	if err != nil {
		return fmt.Errorf("list synthesizing plans for mapping restore: %w", err)
	}
	plans = append(plans, synthPlans...)

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, plan := range plans {
		// Restore parent task -> plan mapping.
		if plan.TaskID != "" {
			m.taskPlanMap[plan.TaskID] = plan.ID
		}

		// Restore child task -> phase + plan mappings.
		phases, err := m.store.GetPhases(ctx, plan.ID)
		if err != nil {
			m.logger.Warn("failed to get phases for mapping restore", "plan_id", plan.ID, "error", err)
			continue
		}
		for _, phase := range phases {
			if phase.TaskID != "" {
				m.phaseTaskMap[phase.TaskID] = phase.ID
				m.taskPlanMap[phase.TaskID] = plan.ID
			}
		}
	}

	m.logger.Info("restored plan-task mappings",
		"plans", len(plans),
		"phase_mappings", len(m.phaseTaskMap),
		"plan_mappings", len(m.taskPlanMap))
	return nil
}

// CreatePhase persists a phase record to the underlying store. This is the
// public entry point for callers (e.g., the orchestrator's planPhaseSink)
// that need to persist phase declarations produced by the planner.
func (m *PlanManager) CreatePhase(ctx context.Context, phase *PlanPhase) error {
	if phase == nil {
		return nil
	}
	return m.store.CreatePhase(ctx, phase)
}

// ---------------------------------------------------------------------------
// Plan creation threshold
// ---------------------------------------------------------------------------

// ShouldCreatePlan determines whether a plan should be created based on config
// mode, step count, intent, and complexity keywords.
func (m *PlanManager) ShouldCreatePlan(intent string, stepCount int) bool {
	switch m.config.Mode {
	case "off":
		return false
	case "always":
		return true
	case "threshold":
		// Check always-plan intents.
		for _, ai := range m.config.Threshold.AlwaysPlanIntents {
			if strings.EqualFold(intent, ai) {
				return true
			}
		}
		// Check step count threshold.
		if m.config.Threshold.MinSteps > 0 && stepCount >= m.config.Threshold.MinSteps {
			return true
		}
		// Check complexity keywords.
		lower := strings.ToLower(intent)
		for _, kw := range m.config.Threshold.ComplexityKeywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// resolvePlanDir returns the directory where plan files should be stored.
func (m *PlanManager) resolvePlanDir(projectPath string) string {
	if m.config.Storage.ExternalPath != "" {
		expanded := os.ExpandEnv(m.config.Storage.ExternalPath)
		return expanded
	}
	defaultPath := m.config.Storage.DefaultPath
	if defaultPath == "" {
		defaultPath = "docs/plans"
	}
	if filepath.IsAbs(defaultPath) {
		return defaultPath
	}
	// An empty project path must not resolve relative to the process CWD.
	// Employee goal-loop plans and EnsureTaskPlan pass "". That used to
	// write docs/plans/*.md into the git repo whenever a test or daemon
	// was started there.
	if projectPath == "" {
		return filepath.Join(config.MeeptHome(), "plans")
	}
	return filepath.Join(projectPath, defaultPath)
}

// firstNonEmpty returns the first non-empty string, or "" when all are empty.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// SetKanbanEventPublishing arms the opt-in gate (plan leaf 02/04) for the
// typed kanban.event.plan.state StateEvent publishers on the plan state
// machine's transition paths. Default false: the manager behaves
// byte-identically to pre-kanban builds — no StateEvent is ever constructed
// or published, and the legacy plan.* events are untouched. The daemon
// wiring (leaf 07/01) turns it on; tests drive both sides through this
// setter (same mutex-guarded setter pattern as PersistentQueue's).
func (m *PlanManager) SetKanbanEventPublishing(enabled bool) {
	m.kanbanMu.Lock()
	defer m.kanbanMu.Unlock()
	m.kanbanEvents = enabled
}

// publishPlanState emits a plan-machine StateEvent on
// kanban.event.plan.state when (and only when) the opt-in gate is armed.
// Called from the successful transition paths AFTER the guarded CAS or
// store write confirmed the change; refused transitions and lost races
// never reach it. The gate bit is read under kanbanMu (its OWN lock —
// m.mu is held across some publish call chains, and Go's RWMutex is
// non-reentrant), then the publish runs outside any lock (mutexio). It
// never touches the legacy publishEvent surface: plan.executing /
// plan.phase_state stay byte-identical (intake depends on them).
func (m *PlanManager) publishPlanState(planID string, from, to PlanState, reason string) {
	if m == nil || from == "" || to == "" || from == to {
		return
	}
	m.kanbanMu.RLock()
	enabled := m.kanbanEvents
	m.kanbanMu.RUnlock()
	if !enabled {
		return
	}
	kanban.PublishPlanState(m.bus, kanban.StateEvent{
		Kind:   kanban.EventKindPlan,
		ID:     planID,
		From:   string(from),
		To:     string(to),
		Reason: reason,
	})
}

// publishEvent creates a BusMessage and publishes it to the message bus.
func (m *PlanManager) publishEvent(eventType string, payload map[string]any) {
	if m.bus == nil {
		return
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "plan-manager", payload)
	if err != nil {
		m.logger.Warn("failed to create bus message", "event", eventType, "error", err)
		return
	}

	delivered := m.bus.Publish(eventType, msg)
	m.logger.Debug("published event", "event", eventType, "delivered", delivered)
}

// phasesToValues converts a slice of phase pointers to a slice of values.
func phasesToValues(phases []*PlanPhase) []PlanPhase {
	if phases == nil {
		return nil
	}
	vals := make([]PlanPhase, len(phases))
	for i, p := range phases {
		vals[i] = *p
	}
	return vals
}

// slugify converts a title into a filesystem-safe slug.
var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(title string) string {
	s := strings.ToLower(title)
	s = nonAlphaNum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	// The + quantifier in nonAlphaNum already collapses consecutive
	// non-alphanumeric runs into a single dash, so no -- collapse loop needed.
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}
