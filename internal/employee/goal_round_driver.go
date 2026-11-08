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
// The leaf's full scheduler/queue deferred-turn loop (re-entering
// Manager.Trigger with a source:"goal" payload) is the future seam; this file
// ships the driver logic (admission, advance, auto-block, autonomous-flag
// bridge) that the daemon wiring exposes through the round-advance RPC/CLI so
// the cap behavior is operator-observable and driven on the live manager.

package employee

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
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
// blocks the employee's active armed goal.
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

	// armed overrides the active-default arming per goal id. A nil (absent)
	// entry means "default": a GoalActive goal is armed for continuation.
	// true/false explicitly arm/disarm (disarm survives the active default,
	// e.g. after an error or a block). Guarded by mu.
	mu    sync.Mutex
	armed map[string]*bool
}

// NewGoalRoundDriver constructs the round driver over the live manager. A nil
// manager is allowed: OnRoundSettled degrades to GoalRoundIdle (the goal tools
// and the manager's own paths keep working). A nil logger defaults to
// slog.Default().
func NewGoalRoundDriver(mgr *Manager, log *slog.Logger) *GoalRoundDriver {
	if log == nil {
		log = slog.Default()
	}
	return &GoalRoundDriver{
		mgr:   mgr,
		log:   log.With("component", "goal-round-driver"),
		armed: make(map[string]*bool),
	}
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
	// and mark the round autonomous for the goal tools. CAS is enforced
	// structurally — we load once, mutate, and Update (the store bumps the
	// revision); ValidGoalRound fences a stale reservation before it enters
	// the loop.
	goal.StartRound()
	d.MarkAutonomousRound(true)
	defer d.MarkAutonomousRound(false)
	d.Arm(goal)
	if err := d.mgr.UpdateGoal(ctx, goal); err != nil {
		d.disarm(goal.ID)
		return GoalRoundIdle, fmt.Errorf("reserve goal round: %w", err)
	}
	d.log.Info("goal round reserved", "goal_id", goal.ID, "employee_id", employeeID, "rounds_started", goal.RoundsStarted, "max", cap)
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
	return nil, nil
}
