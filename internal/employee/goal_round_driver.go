// Package employee — goal_round_driver.go implements the autonomous
// continuation driver (dsh-convergence leaf docs/plans/20261102-dsh-
// convergence/02-goals-plans/03-round-driver.md, contract C4c). When a goal
// is active/armed and the agent reaches quiescence, the driver advances round
// N+1 (CAS on revision) with zero human turns and auto-blocks at the round
// cap (code "round-limit").
//
// Round admission is fenced: only an exact revision and a sequential round
// are admitted. Auto-block persists a durable BlockedReason{code:"round-limit"}
// and disarms the goal.
//
// The leaf's full scheduler/queue deferred-turn loop is implemented here
// (leaf T2): OnRoundSettled reserves round N+1, then re-enters
// Manager.Trigger with a source:"goal" payload through the daemon scheduler
// (DeferredScheduler seam, satisfied by *scheduler.Scheduler.ScheduleConfig
// via the daemon adapter). A Start(ctx) poll loop drives the whole cycle
// autonomously: every poll interval it finds employees whose goals are
// drivable (active + armed, under the cap), waits for quiescence (the
// employee's per-employee invoke mutex is free, no parked episode, no queued
// steer), reserves the round, and schedules the deferred turn. The existing
// agents.goals.round.advance RPC/CLI surface stays as the manual override.
//
// Auto-advance is ON by default (the leaf contract: zero human turns); the
// [employees] goal_auto_advance knob (default true) restores the manual-only
// behavior when set false.

package employee

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/tools/builtin"
)

// GoalRoundOutcome is the result of a driver settlement step.
type GoalRoundOutcome string

const (
	// GoalRoundAdvanced means the next round was reserved (RoundsStarted
	// incremented) for an active armed goal under the cap.
	GoalRoundAdvanced GoalRoundOutcome = "advanced"
	// GoalRoundBlocked means the goal reached its round cap and was
	// auto-blocked with code "round-limit".
	GoalRoundBlocked GoalRoundOutcome = "blocked"
	// GoalRoundIdle means no drivable (active armed) goal was found for the
	// employee — nothing to advance.
	GoalRoundIdle GoalRoundOutcome = "idle"
)

// RoundLimitBlockCode is the durable block code the driver writes on
// auto-block (mirrors dsh: code "round-limit").
const RoundLimitBlockCode = "round-limit"

// GoalRoundDriver is the per-employee-serialized continuation driver. It
// reads the live Manager (GoalStore) and, on each settlement, advances or
// blocks the employee's active armed goal, then re-enters Manager.Trigger
// with a source:"goal" payload so the reserved round runs with zero human
// turns (the leaf's scheduler deferred-turn loop).
//
// Arming note: employee.Goal.armed is deliberately process-local and never
// persisted, so a Manager reload (ListGoals/GetGoal) always hands back a
// disarmed instance. The driver therefore treats a GoalActive (non-paused,
// non-retired, non-blocked) goal as armed-for-continuation — the observable
// live interpretation of "active mandate with continuation authority" given
// arming cannot span the store round-trip. The Goal-level primitives
// (Activate/IsArmed/IsBlockedAfterNConsecutive) are still load-bearing and
// are unit-tested directly.
type GoalRoundDriver struct {
	mgr *Manager
	log *slog.Logger

	// setAutonomous is the leaf-02 authority bridge the daemon binds to
	// builtin.SetGoalAutonomousRound (employee cannot import builtin — the
	// import cycle). When nil, an autonomous-round set is a no-op and the
	// goal tools treat the turn as direct-human (fail-safe).
	setAutonomous func(bool)

	// deferred schedules the one-shot deferred turn that re-enters
	// Manager.Trigger after the round is reserved (leaf T2's
	// ScheduleConfig reuse). Nil means reserve-only (the pre-autonomy
	// RPC/CLI-driven behavior).
	deferred DeferredScheduler

	// quiescent reports whether the employee is settled (no in-flight
	// episode, no parked episode, no queued steer). Nil means the check
	// passes (nil-safe seam per the leaf: driver works without the
	// delegation/park wiring).
	quiescent func(employeeID string) bool

	// autoAdvance gates the autonomous loop (leaf contract: ON by default;
	// the [employees] goal_auto_advance=false knob restores manual-only).
	autoAdvance bool

	// pollInterval is the Start loop's settlement-check cadence.
	pollInterval time.Duration

	// mu guards the start/stop state below (pollCancel/pollDone/pollStopping).
	mu           sync.Mutex
	armed        map[string]*bool
	inflight     map[string]bool   // employeeID -> deferred turn not yet settled
	deferredGoal map[string]string // employeeID -> goal id of its last deferred turn (C27 disarm key)
	// goalOwner maps goal id -> employee id (goal.EmployeeID at Arm time) so
	// the tracking GC (A6-13) can re-resolve a tracked goal's live state
	// without scanning every employee's goal list.
	goalOwner map[string]string
	// reserveFails counts CONSECUTIVE transient reserve failures per
	// employee (A6-5). Cleared on a successful reserve; capped so a
	// permanently broken store cannot hot-loop re-reservations (the C27
	// concern) — at driverReserveFailureCap the goal disarms like the old
	// unconditional behavior did on the first error.
	reserveFails map[string]int
	// reserveWriteHook is a TEST-ONLY seam (same pattern as
	// employeeBoundAgentLoopExecutor.cloneHook): when non-nil,
	// OnRoundSettled consults it INSTEAD of relying solely on the store
	// write so pins can deterministically inject transient (CAS/BUSY) and
	// terminal (not-found/transition) reserve write failures. Production
	// never sets it (the setter lives in a _test.go file), so the
	// production path is unchanged.
	reserveWriteHook func(g *Goal) error
	pollCancel       context.CancelFunc
	pollDone         chan struct{} // closed when the poll loop exits
	pollStopping     bool
}

// reserveWriteFailure consults the test-only hook. nil hook (every
// production path) means "no injected failure" and the real store write
// proceeds.
func (d *GoalRoundDriver) reserveWriteFailure(goal *Goal) error {
	d.mu.Lock()
	hook := d.reserveWriteHook
	d.mu.Unlock()
	if hook == nil {
		return nil
	}
	return hook(goal)
}

// driverReserveFailureCap bounds consecutive transient reserve failures
// before the driver gives up and disarms (A6-5 retry-with-backstop).
const driverReserveFailureCap = 5

// DefaultGoalRoundPollInterval is the driver's settlement-check cadence when
// Start is called without an explicit interval (WithPollInterval).
const DefaultGoalRoundPollInterval = time.Minute

// DeferredScheduler is the minimal scheduler surface the driver needs to
// schedule the one-shot deferred turn (leaf: ScheduleConfig reuse, no new
// scheduler primitives). *scheduler.Scheduler satisfies this via its
// ScheduleConfig method through the daemon adapter.
type DeferredScheduler interface {
	// ScheduleOneShot registers fn to run once, immediately or at the
	// next scheduler tick. name identifies the registration (same name
	// replaces prior registration).
	ScheduleOneShot(name string, fn func())
}

// QuiescenceFunc reports whether an employee is settled: no in-flight or
// parked episode, no queued steering. The daemon binds the parker +
// manager-derived predicate; nil (unwired) is fail-open per the leaf's
// nil-safe seam contract.
type QuiescenceFunc func(employeeID string) bool

// NewGoalRoundDriver constructs the round driver over the live manager. A nil
// manager is allowed: OnRoundSettled degrades to GoalRoundIdle (the goal tools
// and the manager's own paths keep working). A nil logger defaults to
// slog.Default(). Auto-advance defaults ON (the leaf's zero-human-turns
// contract); WithAutoAdvance(false) restores the manual-only RPC/CLI
// behavior.
func NewGoalRoundDriver(mgr *Manager, log *slog.Logger) *GoalRoundDriver {
	if log == nil {
		log = slog.Default()
	}
	return &GoalRoundDriver{
		mgr:          mgr,
		log:          log.With("component", "goal-round-driver"),
		armed:        make(map[string]*bool),
		inflight:     make(map[string]bool),
		deferredGoal: make(map[string]string),
		goalOwner:    make(map[string]string),
		reserveFails: make(map[string]int),
		autoAdvance:  true,
		pollInterval: DefaultGoalRoundPollInterval,
	}
}

// WithDeferredScheduler binds the one-shot deferred-turn scheduler (the
// daemon passes an adapter over *scheduler.Scheduler.ScheduleConfig). nil is
// ignored (typed-nil guard per CLAUDE.md).
func (d *GoalRoundDriver) WithDeferredScheduler(s DeferredScheduler) *GoalRoundDriver {
	if s != nil && d != nil {
		d.deferred = s
	}
	return d
}

// WithQuiescenceCheck binds the settled/quiet predicate. nil is ignored
// (typed-nil guard); when unset the driver treats every employee as quiet
// (nil-safe seam per the leaf).
func (d *GoalRoundDriver) WithQuiescenceCheck(fn QuiescenceFunc) *GoalRoundDriver {
	if fn != nil && d != nil {
		d.quiescent = fn
	}
	return d
}

// WithAutoAdvance sets the autonomous loop gate. Default true (the leaf
// contract); false restores manual-only advance (RPC/CLI drives settlement).
func (d *GoalRoundDriver) WithAutoAdvance(on bool) *GoalRoundDriver {
	if d != nil {
		d.autoAdvance = on
	}
	return d
}

// WithPollInterval sets the Start loop's cadence. Non-positive values are
// ignored (the default stands).
func (d *GoalRoundDriver) WithPollInterval(interval time.Duration) *GoalRoundDriver {
	if d != nil && interval > 0 {
		d.pollInterval = interval
	}
	return d
}

// WithAutonomousRoundSetter binds the leaf-02 authority bridge.
//
// OBSERVER-ONLY since H8/A6-1: the goal tools' authority verdict now
// rides the ROUND CONTEXT (builtin.WithGoalAutonomous, stamped by
// scheduleDeferredTurn), never a process-global. The setter is kept so the
// daemon wiring and existing pins compile; the value it records has NO
// effect on tool authority. nil is ignored (typed-nil guard per CLAUDE.md).
func (d *GoalRoundDriver) WithAutonomousRoundSetter(fn func(bool)) *GoalRoundDriver {
	if fn != nil && d != nil {
		d.setAutonomous = fn
	}
	return d
}

// MarkAutonomousRound forwards to the bound observer seam (see
// WithAutonomousRoundSetter). It is idempotent and safe for concurrent
// use, but — post H8/A6-1 — it is pure observability: the authority that
// the goal tools enforce is the ctx stamp applied in scheduleDeferredTurn.
func (d *GoalRoundDriver) MarkAutonomousRound(on bool) {
	if d == nil || d.setAutonomous == nil {
		return
	}
	d.setAutonomous(on)
}

// Start launches the autonomous continuation loop (leaf T2). Every poll
// interval it scans the roster for employees with a settled (quiet),
// drivable (active + armed, under-cap) goal and drives one settlement step:
// reserve round N+1, schedule the deferred turn that re-enters
// Manager.Trigger with the goal round identity, and let the cap auto-block
// machinery do its work. A second Start on a running driver is a no-op;
// Stop ends the loop (idempotent).
func (d *GoalRoundDriver) Start(ctx context.Context) {
	if d == nil || d.mgr == nil || !d.autoAdvance {
		return
	}
	d.mu.Lock()
	if d.pollCancel != nil {
		// Already running.
		d.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	d.pollCancel = cancel
	d.pollDone = done
	d.mu.Unlock()

	interval := d.pollInterval
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-ticker.C:
				d.settleQuietEmployees(loopCtx)
			}
		}
	}()
	d.log.Info("goal round driver: autonomous continuation loop started",
		"poll_interval", interval)
}

// Stop ends the Start loop and waits for the poll goroutine to exit.
// Idempotent; safe on a nil receiver.
func (d *GoalRoundDriver) Stop() {
	if d == nil {
		return
	}
	d.mu.Lock()
	cancel := d.pollCancel
	done := d.pollDone
	stopping := d.pollStopping
	if cancel != nil && !stopping {
		d.pollStopping = true
		cancel()
	}
	d.mu.Unlock()
	if done != nil && !stopping {
		<-done
	}
}

// settleQuietEmployees drives one settlement pass over the roster: for every
// employee with a drivable goal that is quiet (no in-flight episode, no
// parked episode, no queued steer), reserve the next round and schedule the
// deferred turn. Errors are per-employee: one failure never blocks the rest.
func (d *GoalRoundDriver) settleQuietEmployees(ctx context.Context) {
	emps, err := d.mgr.ListEmployees(ctx, "")
	if err != nil {
		// The roster may be unavailable (bots not configured): quiet debug,
		// never fatal — the loop retries on the next tick.
		d.log.Debug("goal round driver: roster unavailable", "error", err)
		return
	}
	for _, emp := range emps {
		if ctx.Err() != nil {
			return
		}
		d.settleEmployeeIfQuiet(ctx, emp.ID)
	}
	// A6-13: after every roster pass, sweep the per-goal tracking maps so
	// entries for goals that retired/blocked/paused through any surface
	// (tool, RPC, human) do not accumulate for the process lifetime.
	d.gcGoalTracking(ctx)
}

// settleEmployeeIfQuiet runs one settlement step for a quiet employee. The
// quiescence check, the in-flight dedup, and Manager.Trigger's own
// per-employee invoke mutex together guarantee a round never double-starts.
func (d *GoalRoundDriver) settleEmployeeIfQuiet(ctx context.Context, employeeID string) {
	if _, err := d.settleEmployeeIfQuietOutcome(ctx, employeeID); err != nil {
		d.log.Warn("goal round driver: settlement failed",
			"employee_id", employeeID, "error", err)
	}
}

// settleEmployeeIfQuietOutcome is settleEmployeeIfQuiet with the outcome
// returned (test seam; same guards). A not-quiet or in-flight employee
// reports GoalRoundIdle without touching the goal.
func (d *GoalRoundDriver) settleEmployeeIfQuietOutcome(ctx context.Context, employeeID string) (GoalRoundOutcome, error) {
	if d.isInflight(employeeID) {
		return GoalRoundIdle, nil
	}
	if !d.isQuiet(employeeID) {
		return GoalRoundIdle, nil
	}
	outcome, err := d.OnRoundSettled(ctx, employeeID)
	if err != nil {
		return GoalRoundIdle, err
	}
	// OnRoundSettled schedules the deferred turn on the advance edge; the
	// only other settlement outcome is the cap auto-block (nothing to run).
	return outcome, nil
}

// goalReserveErrorTerminal classifies a Manager.UpdateGoal reserve failure
// for the driver's disarm decision (A6-5). TERMINAL errors — the row is
// gone (ErrGoalNotFound) or the state-transition fence refuses the write
// (ErrGoalTransition, which no retry changes while the row stays in the
// refusing state) — disarm the goal exactly like the old unconditional
// behavior. Everything else is TRANSIENT (C29's ErrGoalRevisionConflict
// CAS race with a concurrent human/tool write, SQLite BUSY/locked, I/O or
// deadline blips) and retries on the next poll tick up to
// driverReserveFailureCap. ErrGoalRevisionConflict is deliberately NOT
// terminal: it is the routine concurrent-update shape.
func goalReserveErrorTerminal(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrGoalNotFound) || errors.Is(err, ErrGoalTransition)
}

// isQuiet applies the bound quiescence predicate; nil (unwired) is fail-open.
func (d *GoalRoundDriver) isQuiet(employeeID string) bool {
	if d.quiescent == nil {
		return true
	}
	return d.quiescent(employeeID)
}

// isInflight reports whether a deferred turn for this employee has been
// scheduled and not yet observed settled.
func (d *GoalRoundDriver) isInflight(employeeID string) bool {
	if d == nil || employeeID == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.inflight[employeeID]
}

// markInflight sets/clears the per-employee in-flight dedup flag.
func (d *GoalRoundDriver) markInflight(employeeID string, on bool) {
	if d == nil || employeeID == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if on {
		d.inflight[employeeID] = true
		return
	}
	delete(d.inflight, employeeID)
}

// goalRoundDeferredJobPrefix namespaces the driver's one-shot scheduler
// registrations so they never collide with user jobs.
const goalRoundDeferredJobPrefix = "employee.goal-round."

// scheduleDeferredTurn registers the one-shot deferred turn that re-enters
// Manager.Trigger with the goal round identity (source:"goal", the reserved
// round's goal_id + revision). The registration is keyed per employee so a
// re-schedule replaces the prior one instead of stacking duplicates. With no
// scheduler bound (reserve-only mode) the reservation stays for the manual
// RPC/CLI advance to pick up — the returned bool reports that shape so the
// caller never leaves the employee stuck inflight (A6-4).
//
// H8/A6-1: the round's autonomy is stamped on the TRIGGER CONTEXT
// (builtin.WithGoalAutonomous) instead of a process-global flag. The ctx
// rides Manager.Trigger → triggerViaGoalLoop → GoalLoop.Decide → Execute →
// the bound executor → the agent loop → tools/builtin Execute, so ONLY this
// round sees autonomous authority: a concurrent cron/webhook/plain turn
// keeps direct-human rules, and one employee's round settling can never
// flip another employee's round back to direct-human mid-flight.
func (d *GoalRoundDriver) scheduleDeferredTurn(employeeID string) bool {
	if d.deferred == nil {
		return false
	}
	name := goalRoundDeferredJobPrefix + employeeID
	d.deferred.ScheduleOneShot(name, func() {
		// C31: the DEFERRED round is the autonomous continuation — the
		// observer seam is raised for this turn's execution window (kept
		// for the daemon's observability wiring) and the authority itself
		// rides the stamped ctx, cleared implicitly when the turn returns.
		d.MarkAutonomousRound(true)
		defer d.MarkAutonomousRound(false)
		defer d.markInflight(employeeID, false)
		payload := d.goalRoundPayload(context.Background(), employeeID)
		if payload == nil {
			return
		}
		// C26: the round is a real invocation — record it against the
		// persisted budget counters so MaxInvocationsPerDay survives
		// restarts (nil checker = unwired, no-op).
		d.mgr.RecordGoalRound(employeeID)
		roundCtx := builtin.WithGoalAutonomous(d.mgr.ShutdownContext())
		if _, err := d.mgr.Trigger(roundCtx, employeeID, payload); err != nil {
			// An errored round disarms the goal (leaf T4): no further
			// autonomous advance until something re-arms it.
			// C27 (bughunt): the disarm keys on the GOAL id (Arm/isArmed
			// key d.armed[goal.ID]) — the employee-id key never matched,
			// so the errored-round disarm never fired and persistently
			// failing employees were re-driven every tick to the cap.
			if goalID := d.lastDeferredGoalID(employeeID); goalID != "" {
				d.disarm(goalID)
			}
			d.log.Warn("goal round driver: deferred round failed; goal disarmed",
				"employee_id", employeeID, "error", err)
			return
		}
		// The round settled. OnRoundSettled is retried on the next poll
		// tick (inflight cleared); the cap auto-block runs there if the
		// budget is exhausted.
		d.log.Debug("goal round driver: deferred round settled",
			"employee_id", employeeID)
	})
	return true
}

// goalRoundPayload builds the source:"goal" trigger payload carrying the
// reserved round's identity (goal_id, round, revision — the dsh
// user-message source shape). It returns nil (no round to run) when the
// employee has no drivable goal.
func (d *GoalRoundDriver) goalRoundPayload(ctx context.Context, employeeID string) map[string]any {
	goal, err := d.drivableGoal(ctx, employeeID)
	if err != nil || goal == nil {
		return nil
	}
	// Remember the goal id this deferred turn carries (C27: the
	// errored-round disarm must key the GOAL id, matching Arm/isArmed).
	d.mu.Lock()
	d.deferredGoal[employeeID] = goal.ID
	d.mu.Unlock()
	return map[string]any{
		"source":   "goal",
		"goal_id":  goal.ID,
		"round":    goal.RoundsStarted,
		"revision": goal.Revision,
	}
}

// lastDeferredGoalID returns the goal id carried by this employee's most
// recent deferred turn, or "" when none was recorded. Guarded by mu.
func (d *GoalRoundDriver) lastDeferredGoalID(employeeID string) string {
	if d == nil {
		return ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.deferredGoal[employeeID]
}

// Arm marks the goal for automatic continuation in this process. It mirrors
// the Goal.armed flag that cannot span a store reload. A6-13: every armed
// entry records its owning employee so the periodic tracking GC can
// re-resolve the goal's live state and delete entries for goals that
// retired, were blocked, or no longer exist — without that, the map grows
// one entry per goal the process ever drove.
func (d *GoalRoundDriver) Arm(goal *Goal) {
	if d == nil || goal == nil {
		return
	}
	d.mu.Lock()
	v := true
	d.armed[goal.ID] = &v
	if d.goalOwner == nil {
		d.goalOwner = make(map[string]string)
	}
	d.goalOwner[goal.ID] = goal.EmployeeID
	d.mu.Unlock()
}

// disarm explicitly removes a goal from continuation (an errored round or a
// block stops further autonomous advance even while the goal stays active).
func (d *GoalRoundDriver) disarm(goalID string) {
	if d == nil || goalID == "" {
		return
	}
	d.mu.Lock()
	v := false
	d.armed[goalID] = &v
	d.mu.Unlock()
}

// recordReserveFailure increments the consecutive transient reserve-failure
// count for an employee and returns the new count (A6-5 backstop).
func (d *GoalRoundDriver) recordReserveFailure(employeeID string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.reserveFails == nil {
		d.reserveFails = make(map[string]int)
	}
	d.reserveFails[employeeID]++
	return d.reserveFails[employeeID]
}

// clearReserveFailures resets an employee's transient reserve-failure streak
// after a successful reservation (A6-5).
func (d *GoalRoundDriver) clearReserveFailures(employeeID string) {
	if d == nil || employeeID == "" {
		return
	}
	d.mu.Lock()
	delete(d.reserveFails, employeeID)
	d.mu.Unlock()
}

// purgeGoalTracking removes the driver's per-goal tracking entries for a
// goal that has SETTLED out of continuation (A6-13): the auto-block edge
// calls it inline, and gcGoalTracking catches goals retired/paused/blocked
// through other surfaces (goal_update tool, human RPC). An entry whose
// override is armed=false on a STILL-ACTIVE goal is deliberately kept — it
// is the C27 errored-round brake, and deleting it would re-arm the goal
// from isArmed's "active default" on the next tick.
func (d *GoalRoundDriver) purgeGoalTracking(goalID, employeeID string) {
	if d == nil || goalID == "" {
		return
	}
	d.mu.Lock()
	delete(d.armed, goalID)
	delete(d.goalOwner, goalID)
	if d.deferredGoal[employeeID] == goalID {
		delete(d.deferredGoal, employeeID)
	}
	d.mu.Unlock()
}

// gcGoalTracking sweeps the armed/goalOwner registries against the live
// store and drops entries for goals that left controllable state (retired,
// blocked, paused) or no longer exist (A6-13: the maps used to grow one
// entry per goal ever driven by the process). Goals still GoalActive keep
// their entry — armed=true is the continuation registration and armed=false
// is the C27 brake. A store read that fails transiently skips the goal for
// this pass; the next tick retries. Runs once per settle pass over the
// roster, so its cost is one GetGoal per tracked goal per poll tick.
func (d *GoalRoundDriver) gcGoalTracking(ctx context.Context) {
	if d == nil || d.mgr == nil {
		return
	}
	d.mu.Lock()
	tracked := maps.Clone(d.goalOwner)
	if tracked == nil {
		tracked = make(map[string]string)
	}
	// Goal ids disarmed (brake entries) but never recorded in goalOwner
	// (disarm without a prior Arm — defensive) also need sweeping.
	for goalID := range d.armed {
		if _, ok := tracked[goalID]; !ok {
			tracked[goalID] = ""
		}
	}
	d.mu.Unlock()

	for goalID, empID := range tracked {
		if ctx.Err() != nil {
			return
		}
		g, err := d.mgr.GetGoal(ctx, goalID)
		if err != nil {
			if errors.Is(err, ErrGoalNotFound) {
				d.purgeGoalTracking(goalID, empID)
			}
			continue
		}
		if g != nil && g.State != GoalActive {
			d.purgeGoalTracking(goalID, empID)
		}
	}
}

// isArmed reports whether the driver considers the goal drivable for
// continuation. Active goals are armed by default (creation through the
// RPC/CLI surface lands active); the per-goal override (Arm/disarm) and the
// Goal instance's live flag both take precedence. Paused/retired/blocked
// goals are never drivable.
func (d *GoalRoundDriver) isArmed(goal *Goal) bool {
	if goal == nil || goal.State != GoalActive {
		return false
	}
	if goal.IsArmed() {
		return true
	}
	if d != nil {
		d.mu.Lock()
		override, ok := d.armed[goal.ID]
		d.mu.Unlock()
		if ok {
			return *override
		}
	}
	return true // active default
}

// ValidGoalRound is the round-admission fence (leaf T1): it accepts a round
// only when it is exactly sequential (round == RoundsStarted+1) AND the goal's
// current revision matches. Anything else (stale revision, skipped/replayed
// round) is rejected so a reserved-but-stale round can never enter a changed
// goal. It does not require a live manager and is safe on any Goal.
//
// STATUS (deadcode wave-3): available-but-unused API surface — no
// production caller exists (tests only). The round driver's reserve path
// relies on load-once/mutate/Update instead. Kept as the documented fence
// for future callers that admit a round against a possibly-changed goal;
// a caller landing here should replace this note, not the function.
func ValidGoalRound(goal *Goal, round, revision int64) bool {
	if goal == nil {
		return false
	}
	if goal.Revision != revision {
		return false
	}
	if round <= 0 || int64(goal.RoundsStarted+1) != round {
		return false
	}
	return true
}

// BlockGoal auto-blocks a goal with the given durable reason. It sets
// State=GoalBlocked, records the BlockedReason, disarms, and persists through
// the manager (which bumps the CAS revision). Safe on a nil goal. A nil
// manager returns an error naming the store.
func BlockGoal(ctx context.Context, mgr *Manager, goal *Goal, code, message string) error {
	if goal == nil {
		return nil
	}
	if mgr == nil {
		return fmt.Errorf("block goal: no goal store")
	}
	goal.State = GoalBlocked
	if code == "" {
		code = RoundLimitBlockCode
	}
	goal.BlockedReason = &BlockedReason{Code: code, Message: message}
	goal.Disarm()
	if err := mgr.UpdateGoal(ctx, goal); err != nil {
		return fmt.Errorf("block goal: %w", err)
	}
	return nil
}

// OnRoundSettled drives one continuation step for the employee. It loads the
// employee's active goals via the live manager, and for the drivable (active
// + armed) goal:
//
//   - if RoundsStarted >= RoundsCap(): auto-blocks with code "round-limit"
//     and returns GoalRoundBlocked; or
//   - else reserves round N+1 (start the next round, CAS on revision) and
//     returns GoalRoundAdvanced.
//
// With no drivable goal it returns GoalRoundIdle (nil, nil). The reserved
// round marks the turn autonomous for the goal tools (complete/blocked become
// legal) via the bound setter before it runs, then clears the flag.
func (d *GoalRoundDriver) OnRoundSettled(ctx context.Context, employeeID string) (GoalRoundOutcome, error) {
	if d == nil || d.mgr == nil {
		return GoalRoundIdle, nil
	}
	if employeeID == "" {
		return GoalRoundIdle, nil
	}
	// A6-4: round.advance phantom-reserve leg — the autonomous loop dedups
	// through settleEmployeeIfQuiet, but the manual RPC/CLI calls
	// OnRoundSettled directly. Without this gate, an advance landing while
	// a deferred round is still in flight reserved a second round nobody
	// runs yet (RoundsStarted inflated past the rounds actually executed).
	if d.isInflight(employeeID) {
		d.log.Debug("goal round: advance refused, employee round already in flight",
			"employee_id", employeeID)
		return GoalRoundIdle, nil
	}
	goal, err := d.drivableGoal(ctx, employeeID)
	if err != nil {
		return GoalRoundIdle, err
	}
	if goal == nil {
		d.log.Debug("goal round: no drivable goal", "employee_id", employeeID)
		return GoalRoundIdle, nil
	}

	cap := goal.RoundsCap()
	if goal.RoundsStarted >= cap {
		msg := fmt.Sprintf("Goal reached its configured limit of %d rounds.", cap)
		if err := BlockGoal(ctx, d.mgr, goal, RoundLimitBlockCode, msg); err != nil {
			return GoalRoundIdle, err
		}
		d.disarm(goal.ID)
		// A6-13: the blocked goal never becomes drivable again from the
		// store's perspective (ListActive excludes nothing by state, but
		// drivableGoal filters on GoalActive) — retire the driver's
		// per-goal tracking entries with it so armed cannot grow one
		// entry per goal forever.
		d.purgeGoalTracking(goal.ID, employeeID)
		d.log.Info("goal auto-blocked at round cap", "goal_id", goal.ID, "employee_id", employeeID, "rounds", goal.RoundsStarted, "max", cap)
		return GoalRoundBlocked, nil
	}

	// Reserve round N+1: increment RoundsStarted under this driver's review
	// and persist. CAS is enforced structurally — we load once, mutate, and
	// Update (the store bumps the revision); a stale-reservation fence
	// (ValidGoalRound) exists for callers that admit a round against a
	// possibly-changed goal, though this driver currently does not call it
	// (available API surface).
	goal.StartRound()
	d.Arm(goal)
	// Test-only fail-injection seam: a non-nil error from the hook replaces
	// the store write (deterministic transient/terminal error pins); the
	// production path never sets a failing hook, so the real UpdateGoal
	// runs identically.
	if hookErr := d.reserveWriteFailure(goal); hookErr != nil {
		err = hookErr
	} else {
		err = d.mgr.UpdateGoal(ctx, goal)
	}
	if err != nil {
		// A6-5: only a TERMINAL write failure (the goal row is gone or the
		// transition fence refuses it permanently) disarms the goal. A
		// transient failure (revision CAS conflict, SQLite BUSY/locked,
		// I/O blip, ctx deadline) must NOT kill continuation — C29's CAS
		// makes exactly the human-interleave case a routine conflict, and
		// disarming on it silently strands live goals. Transient errors
		// retry on the next poll tick; a persistent blip storm is bounded
		// by driverReserveFailureCap so a broken store cannot hot-loop
		// re-reservations forever (the C27 concern).
		if goalReserveErrorTerminal(err) {
			d.disarm(goal.ID)
			d.log.Warn("goal round reserve failed terminally; goal disarmed",
				"goal_id", goal.ID, "employee_id", employeeID, "error", err)
			return GoalRoundIdle, fmt.Errorf("reserve goal round: %w", err)
		}
		strikes := d.recordReserveFailure(employeeID)
		if strikes >= driverReserveFailureCap {
			d.disarm(goal.ID)
			d.log.Warn("goal round reserve kept failing; goal disarmed",
				"goal_id", goal.ID, "employee_id", employeeID, "strikes", strikes, "error", err)
		} else {
			d.log.Debug("goal round reserve hit a transient error; retrying next tick",
				"goal_id", goal.ID, "employee_id", employeeID, "strikes", strikes, "error", err)
		}
		return GoalRoundIdle, fmt.Errorf("reserve goal round: %w", err)
	}
	d.clearReserveFailures(employeeID)
	d.log.Info("goal round reserved", "goal_id", goal.ID, "employee_id", employeeID, "rounds_started", goal.RoundsStarted, "max", cap)
	// Deferred turn (leaf T2): schedule the one-shot re-entry into
	// Manager.Trigger with the reserved round's identity so the round runs
	// with zero human turns. Both the autonomous loop and the manual
	// agents.goals.round.advance RPC land here; the in-flight dedup marks
	// the employee until the scheduled turn settles.
	//
	// C31 (bughunt): the autonomous flag was set/cleared HERE, inside
	// OnRoundSettled — but the marked round executes LATER, in the
	// deferred turn's window, so the flag read false during the actual
	// autonomous continuation (inverting the fail-safe contract:
	// complete/blocked stayed refused on continuation turns). The flag is
	// now carried by the deferred turn's CONTEXT (H8/A6-1) — stamped by
	// scheduleDeferredTurn when the job runs.
	//
	// A6-4: markInflight(true) BEFORE scheduling only makes sense if the
	// schedule can actually land. With no scheduler bound (reserve-only
	// mode) the deferred turn never runs and the clear never fires — the
	// employee would stay inflight forever, wedging both the poll loop and
	// the round.advance dedup. Unmark immediately in that shape.
	d.markInflight(employeeID, true)
	if !d.scheduleDeferredTurn(employeeID) {
		d.markInflight(employeeID, false)
		d.log.Debug("goal round: no deferred scheduler bound; reservation left for the manual advance path",
			"employee_id", employeeID, "goal_id", goal.ID)
	}
	return GoalRoundAdvanced, nil
}

// drivableGoal returns the employee's first active+armed goal, ordering
// oldest-first via the manager's ListGoals. It returns (nil, nil) when none
// is drivable.
func (d *GoalRoundDriver) drivableGoal(ctx context.Context, employeeID string) (*Goal, error) {
	goals, err := d.mgr.ListGoals(ctx, employeeID)
	if err != nil {
		return nil, err
	}
	for _, g := range goals {
		if g == nil {
			continue
		}
		if g.State != GoalActive {
			continue
		}
		if !d.isArmed(g) {
			continue
		}
		return g, nil
	}
	// (nil, nil) when no goal is drivable is the documented contract (see
	// the doc comment above); callers treat it as "nothing to advance".
	return nil, nil //nolint:nilnil // documented sentinel: no drivable goal, not an error
}
