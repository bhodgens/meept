package agent

// 01-safety TEST GATE T7 (00T-gate.md): watchdog park exemption (C7).
//
// TurnWatchdog.reapOnce reaps EVERY stale tracked turn today, including
// turns whose task sits in awaiting_approval waiting on a human (F-NEW-6:
// the 10-minute false-reap reproduced 3x in the GUI rerun -- the user sees
// parked -> failed(reaped) -> completed, a wrong story).
//
// C7: turns whose task state is planning | awaiting_approval |
// awaiting_seal are EXEMPT from staleness reaping; draft/seal/approval
// activity Touches liveness. Truly stalled EXECUTING turns still reap.
//
// New seams the 04-watchdog-exemption leaf creates:
//   - TurnWatchdog.SetTaskStateFunc(func(taskID string) task.TaskState)
//     (registry-edge state accessor, never a per-tick store query)
//   - StrategicPlanner.SetTurnLivenessTouch(func(taskID string))
//     (draft/seal/approval activity heartbeat into the turn registry)
//
// RED today: undefined seams; the awaiting_approval turn IS reaped.

import (
	"testing"
	"time"

	"github.com/caimlas/meept/internal/task"
)

// TestApprovalWaitNotReaped pins C7: a tracked turn whose task is
// awaiting_approval, stale 700s (> the 600s default), must NOT be reaped;
// an executing task's stale turn still reaps normally.
func TestApprovalWaitNotReaped(t *testing.T) {
	clock := watchdogTestClock()
	reg := watchdogTestRegistry(clock)

	reg.Register("turn-approval", "conv-approval")
	reg.AttachTask("turn-approval", "task-approval")
	reg.Register("turn-executing", "conv-executing")
	reg.AttachTask("turn-executing", "task-executing")

	states := map[string]task.TaskState{
		"task-approval":  task.StateAwaitingApproval,
		"task-executing": task.StateExecuting,
	}

	// 700s of silence: past the 600s stale_after default.
	clock.Advance(700 * time.Second)

	em := &captureEmitter{}
	w := NewTurnWatchdog(reg, em.emit, discardLogger())
	w.SetTaskStateFunc(func(taskID string) task.TaskState {
		return states[taskID]
	})

	reaped := w.RunOnce(600 * time.Second)
	if reaped != 1 {
		t.Errorf("CURRENT DEFECT: reaped %d turns, want exactly 1 (the executing turn); the awaiting_approval turn is exempt (C7)", reaped)
	}
	for _, ev := range em.snapshot() {
		if ev.TurnID == "turn-approval" {
			t.Errorf("CURRENT DEFECT: awaiting_approval turn was reaped as %q/%q -- a human approval wait is not a stalled turn (F-NEW-6)", ev.HandlerCase, ev.Status)
		}
	}
	// The executing turn must still be reaped (no blanket disable).
	sawExecuting := false
	for _, ev := range em.snapshot() {
		if ev.TurnID == "turn-executing" && ev.HandlerCase == "turn_reaped" && ev.Status == "failed" {
			sawExecuting = true
		}
	}
	if !sawExecuting {
		t.Errorf("stalled executing turn was NOT reaped; the exemption must not disable the reaper wholesale")
	}
	// The exempt turn stays tracked (a later approval still terminalizes it).
	if got := reg.TurnIDForTask("task-approval"); got != "turn-approval" {
		t.Errorf("exempt turn lost its task->turn edge (got %q); it must stay tracked for the approval relay", got)
	}
}

// TestApprovalWaitExemptStates pins the full C7 state set: planning and
// awaiting_seal waits are exempt exactly like awaiting_approval.
func TestApprovalWaitExemptStates(t *testing.T) {
	for _, state := range []task.TaskState{task.StatePlanning, task.StateAwaitingApproval, "awaiting_seal"} {
		t.Run(string(state), func(t *testing.T) {
			clock := watchdogTestClock()
			reg := watchdogTestRegistry(clock)
			reg.Register("turn-wait", "conv-wait")
			reg.AttachTask("turn-wait", "task-wait")
			clock.Advance(700 * time.Second)

			em := &captureEmitter{}
			w := NewTurnWatchdog(reg, em.emit, discardLogger())
			w.SetTaskStateFunc(func(taskID string) task.TaskState { return state })

			if reaped := w.RunOnce(600 * time.Second); reaped != 0 {
				t.Errorf("CURRENT DEFECT: turn waiting in state %q reaped (%d); C7 exempts planning|awaiting_approval|awaiting_seal", state, reaped)
			}
		})
	}
}

// TestSealActivityTouch pins the liveness heartbeat: saving/sealing a
// plan draft is ACTIVITY -- it must Touch the tracked turn so a long
// human-in-the-loop drafting session never goes stale between saves.
func TestSealActivityTouch(t *testing.T) {
	sp, taskStore := newDraftTestPlanner(t)
	tsk := seedDraftTask(t, taskStore, "task-seal-touch")

	clock := watchdogTestClock()
	reg := watchdogTestRegistry(clock)
	reg.Register("turn-seal", "conv-seal")
	reg.AttachTask("turn-seal", tsk.ID)

	sp.SetTurnLivenessTouch(func(taskID string) {
		if turnID := reg.TurnIDForTask(taskID); turnID != "" {
			reg.Touch(turnID)
		}
	})

	// 500s pass (under the 600s threshold), then a draft save: liveness
	// must refresh so the NEXT 500s window does not accumulate to a reap.
	clock.Advance(500 * time.Second)
	if err := sp.SaveDraft(tsk.ID, "# draft v1\n\nthe plan body"); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	clock.Advance(500 * time.Second)

	stale := reg.Stale(600 * time.Second)
	for _, rec := range stale {
		if rec.TurnID == "turn-seal" {
			t.Errorf("CURRENT DEFECT: draft save did not Touch turn liveness; the drafting turn went stale across two 500s windows (C7 activity heartbeat)")
		}
	}

	// Seal activity touches too.
	if err := sp.SealDraft(tsk.ID, "sha256:abc"); err != nil {
		t.Fatalf("SealDraft: %v", err)
	}
	clock.Advance(500 * time.Second)
	for _, rec := range reg.Stale(600 * time.Second) {
		if rec.TurnID == "turn-seal" {
			t.Errorf("CURRENT DEFECT: draft seal did not Touch turn liveness (C7 activity heartbeat)")
		}
	}
}
