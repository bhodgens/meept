package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/task"
)

// ---- stall-based sync-wait ceiling pins (orchestrator.sync_wait_stall /
// sync_wait_max) ----

// TestChatHandler_SyncWaitPlan_Defaults pins the config-default selection
// table: stall mode, 2m inactivity window, 30m hard cap.
func TestChatHandler_SyncWaitPlan_Defaults(t *testing.T) {
	h := &ChatHandler{}
	mode, stall, hardMax := h.syncWaitPlan()
	if mode != syncWaitModeStall {
		t.Fatalf("mode = %v, want stall (zero config = stall-based enabled)", mode)
	}
	if stall != 2*time.Minute {
		t.Errorf("stall = %v, want 2m default", stall)
	}
	if hardMax != 30*time.Minute {
		t.Errorf("hardMax = %v, want 30m default", hardMax)
	}
}

// TestChatHandler_SyncWaitPlan_EscapeHatch pins the byte-identical escape
// hatch: sync_wait_stall < 0 restores the legacy fixed 110s ceiling.
func TestChatHandler_SyncWaitPlan_EscapeHatch(t *testing.T) {
	h := &ChatHandler{}
	h.SetSyncWaitStall(-1, 30*time.Minute)
	mode, stall, hardMax := h.syncWaitPlan()
	if mode != syncWaitModeFixed {
		t.Fatalf("mode = %v, want fixed (sync_wait_stall < 0 = legacy)", mode)
	}
	if hardMax != 110*time.Second {
		t.Errorf("hardMax = %v, want the legacy 110s ceiling", hardMax)
	}
	if stall != 0 {
		t.Errorf("stall = %v, want 0 in fixed mode", stall)
	}
}

// TestChatHandler_SyncWaitPlan_TestCeilingWins pins that a positive
// syncWaitCeiling test seam still selects fixed mode with its own bound.
func TestChatHandler_SyncWaitPlan_TestCeilingWins(t *testing.T) {
	h := &ChatHandler{}
	h.syncWaitCeiling = 250 * time.Millisecond
	mode, _, hardMax := h.syncWaitPlan()
	if mode != syncWaitModeFixed || hardMax != 250*time.Millisecond {
		t.Fatalf("mode=%v hardMax=%v, want fixed/250ms (test seam precedence)", mode, hardMax)
	}
}

// TestChatHandler_WaitForTaskCompletion_StallFires pins the core stall
// semantics: a non-terminal task whose state AND step set never change gets
// the degraded reply after the stall window, well before any fixed cap.
func TestChatHandler_WaitForTaskCompletion_StallFires(t *testing.T) {
	h := newTestChatHandlerWithStores(t)
	h.SetSyncWaitStall(400*time.Millisecond, time.Hour) // high hard cap: only stall can fire

	tk := task.NewTask("stalled task", "stall-fires pin")
	tk.State = task.StateExecuting // non-terminal for the whole test
	if err := h.taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	start := time.Now()
	reply := h.waitForTaskCompletion(context.Background(), tk.ID)
	elapsed := time.Since(start)

	if elapsed >= 5*time.Second {
		t.Fatalf("wait held %.1fs; stall window must bound it", elapsed.Seconds())
	}
	if !strings.Contains(reply, "still running") {
		t.Fatalf("reply = %q, want the still-running stub (no steps exist)", reply)
	}
}

// TestChatHandler_WaitForTaskCompletion_ProgressResetsStall pins that
// visible progress (a step appearing + transitioning state) resets the
// stall timer: the wait must outlive stall + a generous margin because the
// task keeps moving, and must still return the step's real result when the
// task terminalizes.
func TestChatHandler_WaitForTaskCompletion_ProgressResetsStall(t *testing.T) {
	h := newTestChatHandlerWithStores(t)
	h.SetSyncWaitStall(400*time.Millisecond, time.Hour)

	tk := task.NewTask("progressing task", "progress-resets-stall pin")
	tk.State = task.StateExecuting
	if err := h.taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	stop := make(chan struct{})
	go func() {
		// Keep the fingerprint changing every 150ms (well inside the 400ms
		// stall window) by toggling the task state through the
		// single-connection task-store pool (writes serialize with the
		// poller's reads there). After ~3s of churn, finalize the task.
		toggled := false
		for i := 0; i < 20; i++ {
			select {
			case <-stop:
				return
			case <-time.After(150 * time.Millisecond):
			}
			tk.State = task.StatePlanning
			if toggled {
				tk.State = task.StateExecuting
			}
			toggled = !toggled
			_ = h.taskStore.Update(tk)
		}
		tk.State = task.StateCompleted
		_ = h.taskStore.Update(tk)
	}()
	defer close(stop)

	reply := h.waitForTaskCompletion(context.Background(), tk.ID)

	if strings.Contains(reply, "still running") {
		t.Fatalf("reply = %q; continuous progress must defer the stall reply until terminal", reply)
	}
	if !strings.HasSuffix(reply, "completed.") {
		t.Fatalf("reply = %q, want the completed reply after the churn finalizes the task", reply)
	}
}

// TestChatHandler_WaitForTaskCompletion_HardCapFires pins the hard cap:
// even a constantly-progressing task gets the degraded reply once
// sync_wait_max elapses. (The cap check runs before the progress reset.)
func TestChatHandler_WaitForTaskCompletion_HardCapFires(t *testing.T) {
	h := newTestChatHandlerWithStores(t)
	// Stall window larger than the hard cap's practical horizon: only the
	// cap can end the wait.
	h.SetSyncWaitStall(time.Hour, 600*time.Millisecond)

	tk := task.NewTask("endless progress", "hard-cap pin")
	tk.State = task.StateExecuting
	if err := h.taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	stop := make(chan struct{})
	go func() {
		// Keep the fingerprint changing every 150ms by toggling the task
		// state through the single-connection task-store pool (writes
		// serialize with the poller's reads there) — visibly progressing,
		// never terminal, until the wait gives up at the hard cap.
		toggled := false
		for {
			select {
			case <-stop:
				return
			case <-time.After(150 * time.Millisecond):
			}
			tk.State = task.StatePlanning
			if toggled {
				tk.State = task.StateExecuting
			}
			toggled = !toggled
			_ = h.taskStore.Update(tk)
		}
	}()
	defer close(stop)

	start := time.Now()
	reply := h.waitForTaskCompletion(context.Background(), tk.ID)
	elapsed := time.Since(start)

	if elapsed >= 5*time.Second {
		t.Fatalf("wait held %.1fs; hard cap must bound it", elapsed.Seconds())
	}
	if !strings.Contains(reply, "still running") {
		t.Fatalf("reply = %q, want the degraded stub at the hard cap", reply)
	}
}

// TestChatHandler_WaitForTaskCompletion_StallReturnsBestStepResult pins
// that the stall-fired degraded reply preserves bestStepResult: an
// APPROVED step holding the user's answer beats the generic stub.
func TestChatHandler_WaitForTaskCompletion_StallReturnsBestStepResult(t *testing.T) {
	h := newTestChatHandlerWithStores(t)
	h.SetSyncWaitStall(300*time.Millisecond, time.Hour)

	tk := task.NewTask("slow bookkeeping", "best-step-at-stall pin")
	tk.State = task.StateExecuting
	if err := h.taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	step := task.NewTaskStep(tk.ID, "create hello.txt", 2)
	step.State = task.StepApproved
	step.Result = "the file is at /tmp/hello.txt"
	if err := h.stepStore.Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}

	reply := h.waitForTaskCompletion(context.Background(), tk.ID)
	if reply != "the file is at /tmp/hello.txt" {
		t.Fatalf("reply = %q, want the approved step's result", reply)
	}
}

// TestChatHandler_WaitForTaskCompletion_EscapeHatchFixedCeiling pins the
// behavioral half of the escape hatch: with sync_wait_stall < 0 the wait
// runs in FIXED mode — a constantly-progressing task must NOT extend the
// wait (progress resets are stall-mode-only), and the reply is the
// degraded stub at the bound. The bound's 110s value itself is pinned by
// TestChatHandler_SyncWaitPlan_EscapeHatch; here the syncWaitCeiling seam
// shrinks it so the test stays fast.
func TestChatHandler_WaitForTaskCompletion_EscapeHatchFixedCeiling(t *testing.T) {
	h := newTestChatHandlerWithStores(t)
	h.SetSyncWaitStall(-1, 30*time.Minute) // legacy escape hatch
	h.syncWaitCeiling = 600 * time.Millisecond

	tk := task.NewTask("legacy ceiling", "escape-hatch pin")
	tk.State = task.StateExecuting
	if err := h.taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	stop := make(chan struct{})
	go func() {
		// Keep the task visibly progressing (state churn through the
		// single-connection task-store pool). In fixed mode this must NOT
		// extend the wait past the fixed bound.
		toggled := false
		for {
			select {
			case <-stop:
				return
			case <-time.After(150 * time.Millisecond):
			}
			tk.State = task.StatePlanning
			if toggled {
				tk.State = task.StateExecuting
			}
			toggled = !toggled
			_ = h.taskStore.Update(tk)
		}
	}()
	defer close(stop)

	start := time.Now()
	reply := h.waitForTaskCompletion(context.Background(), tk.ID)
	elapsed := time.Since(start)

	// Fixed mode: the bound fires regardless of the ongoing progress churn.
	if elapsed >= 5*time.Second {
		t.Fatalf("wait held %.1fs; fixed ceiling must bound it regardless of progress", elapsed.Seconds())
	}
	if !strings.Contains(reply, "still running") {
		t.Fatalf("reply = %q, want the degraded stub at the fixed ceiling", reply)
	}
	// Fixed bounds do not reset on progress: the wait must survive at least
	// one bound-length window (an accidental instant-stall would return at
	// the first poll, ~2s tick).
	if elapsed < 550*time.Millisecond {
		t.Fatalf("wait returned after %.1fs; fixed ceiling must not fire early", elapsed.Seconds())
	}
}
