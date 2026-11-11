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
	"fmt"
	"log/slog"
	"sync"
	"time"
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

	// cancel stops the Start loop. Guarded by mu alongside start state.
	mu           sync.Mutex
	armed        map[string]*bool
	cancel       context.CancelFunc
	inflight     map[string]bool   // employeeID -> deferred turn not yet settled
	deferredGoal map[string]string // employeeID -> goal id of its last deferred turn (C27 disarm key)
	pollCancel   context.CancelFunc
	pollDone     chan struct{} // closed when the poll loop exits
	pollStopping bool
}

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

// WithAutonomousRoundSetter binds the leaf-02 authority bridge (nil is
// ignored, typed-nil guard per CLAUDE.md). The daemon passes
// builtin.SetGoalAutonomousRound.
func (d *GoalRoundDriver) WithAutonomousRoundSetter(fn func(bool)) *GoalRoundDriver {
	if fn != nil && d != nil {
		d.setAutonomous = fn
	}
	return d
}

// MarkAutonomousRound records whether the current round is an autonomous
// continuation (no human in the loop). It forwards to the bound
// builtin.SetGoalAutonomousRound seam so complete/blocked are legal during a
// continuation; a nil setter means nothing is set (fail-safe direct-human).
// It is idempotent and safe for concurrent use.
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
// RPC/CLI advance to pick up.
func (d *GoalRoundDriver) scheduleDeferredTurn(employeeID string) {
	if d.deferred == nil {
		return
	}
	name := goalRoundDeferredJobPrefix + employeeID
	d.deferred.ScheduleOneShot(name, func() {
		// C31: the DEFERRED round is the autonomous continuation — raise
		// the goal-tools flag for this turn's execution window and clear
		// it when the turn settles (success or failure), so complete/
		// blocked are legal exactly while the driver-driven round runs.
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
		if _, err := d.mgr.Trigger(context.Background(), employeeID, payload); err != nil {
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
// the Goal.armed flag that cannot span a store reload.
func (d *GoalRoundDriver) Arm(goal *Goal) {
	if d == nil || goal == nil {
		return
	}
	d.mu.Lock()
	v := true
	d.armed[goal.ID] = &v
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
	if err := d.mgr.UpdateGoal(ctx, goal); err != nil {
		d.disarm(goal.ID)
		return GoalRoundIdle, fmt.Errorf("reserve goal round: %w", err)
	}
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
	// now raised by scheduleDeferredTurn at job execution and cleared when
	// the turn settles (see the deferred closure and markInflight(false)).
	d.markInflight(employeeID, true)
	d.scheduleDeferredTurn(employeeID)
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
