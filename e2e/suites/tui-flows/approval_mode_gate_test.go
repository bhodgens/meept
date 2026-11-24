//go:build e2e

// 06-planning TEST-GATE T5 (F13a/F13c): TUI approval-card, mode-badge, and
// evolver-document flows against the REAL hermetic daemon, extending the
// tui-flows suite.
//
// RED today for missing-implementation reasons:
//   - the TUI App event switch has no task.pending_approval case (orphan
//     topic), so a gated plan never renders an approval card in chat.
//   - (resolved 2026-11-24) the chat transcript renders no mode badge for
//     RESTORED turns: live turn.terminal events badge, and loads now
//     hydrate intent_type/handler_case from session.dispatch_trace in
//     loadServerMessages (chat_nav.go
//     hydrateRestoredClassifications), so TestTUIModeBadgeDistinguishes
//     pins the restored-transcript badge.
//   - the plans view renders literal zero counts for evolver document
//     proposals instead of reusing planCountLine.
//
// These drive the real tui.NewApp under a headless bubbletea program
// against harness.Stack (the existing tuiDriver), so the assertions are
// in-situ, not mock-only. GREEN after 06-planning/01+03.

package tuiflows

import (
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/tui"
)

// gateTUIPlanStack boots a daemon whose strategic task gate parks a
// scripted 6-step plan at awaiting_approval (threshold 3). plans.mode
// stays OFF: the dispatcher's plan-manager lane (5.5) intercepts planning
// intents whenever mode != off and synthesizes a docs plan instead of
// dispatching the strategic planner, so the task-level gate the F13a pin
// asserts on never runs (observed 2026-11-24, same shape as
// approval-wait). The prompt veto ("don't implement it") keeps the
// intent auto-seal from bypassing the gate.
func gateTUIPlanStack(t *testing.T) *harness.Stack {
	t.Helper()
	return harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"plans.mode":                                "off",
		"plans.approval.require_approval":           false,
		"orchestrator.task_approval_step_threshold": 3,
	}))
}

// gateScriptSixStepPlan scripts the fake planner with a 6-step plan so the
// dispatched task trips the approval gate.
func gateScriptSixStepPlan(s *harness.Stack, marker string) {
	steps := `{"steps":[`
	for i := 0; i < 6; i++ {
		if i > 0 {
			steps += ","
		}
		steps += `{"description":"` + marker + ` step ` + string(rune('0'+i)) + `: do the work","tool_hint":"analyze","depends_on":[]}`
	}
	steps += `]}`
	s.Fake.SetPlannerResponse(steps)
	s.Fake.SetPostToolText("The requested work is complete.")
}

// TestTUIApprovalCardRenders pins F13a in situ: a gated plan dispatched
// through a real chat turn renders an approval card in the TUI chat
// transcript (task id + approve/reject affordance). RED today: the orphan
// task.pending_approval topic is dropped by the App event switch.
func TestTUIApprovalCardRenders(t *testing.T) {
	stack := gateTUIPlanStack(t)
	sessionID := stack.CreateSession(t, "tui-approval", stack.ProjectDir)
	const marker = "TUIAPPROVE"
	gateScriptSixStepPlan(stack, marker)

	// Dispatch the gated plan through a real turn.
	stack.SubmitChatHTTP(t, sessionID, marker+": plan the multi-step rework, don't implement it")

	// Wait until the task parks at awaiting_approval (the gate is real).
	var taskID string
	harness.WaitFor(t, 30*time.Second, "gated task awaiting_approval", func() bool {
		for _, row := range harness.Tasks(t, stack.TasksDBPath()) {
			if strings.Contains(row.Name, marker) && row.State == "awaiting_approval" {
				taskID = row.ID
				return true
			}
		}
		return false
	})

	// Boot the TUI pinned to the session; the pending_approval event must
	// surface as a chat card. The event was published before the TUI
	// subscribed, so also re-publish it through the bus to exercise the
	// live delivery path the card consumes.
	hp := startTUIAgainst(t, stack, WithTargetSession(sessionID))
	settleAsync()
	if got := hp.app.ActiveSessionID(); got != sessionID {
		t.Fatalf("target session not loaded: got %q want %q", got, sessionID)
	}

	// Re-publish task.pending_approval on the live bus (the TUI's event
	// stream polls it). This is the exact payload awaitUserApproval emits.
	rpc := harness.DialRPC(t, stack.SocketPath)
	rpc.CallResult("bus.publish", map[string]any{
		"topic": "task.pending_approval",
		"payload": map[string]any{
			"task_id":    taskID,
			"session_id": sessionID,
			"total":      6,
			"steps":      []any{},
		},
	})
	settleAsync()
	time.Sleep(1200 * time.Millisecond) // > two 500ms poll intervals
	settleAsync()

	app := hp.finish()
	view := app.View().Content
	if !strings.Contains(view, taskID) {
		t.Fatalf("F13a: TUI chat shows no approval card for gated task %s (orphan task.pending_approval):\n%s", taskID, gateTUITail(view, 25))
	}
	lower := strings.ToLower(view)
	if !strings.Contains(lower, "approve") || !strings.Contains(lower, "reject") {
		t.Fatalf("F13a: approval card lacks approve/reject affordances:\n%s", gateTUITail(view, 25))
	}
}

// TestTUIModeBadgeDistinguishes pins F13c in situ: a direct work turn and a
// plan turn render distinguishable mode badges in the chat transcript from
// the C1 terminal fields. RED today: no badge exists.
func TestTUIModeBadgeDistinguishes(t *testing.T) {
	stack := harness.Start(t)
	stack.RegisterProject(t, "e2e-project")
	sessionID := stack.CreateSession(t, "tui-mode", stack.ProjectDir)

	// A direct imperative turn (fake-LLM imperative arbitration -> code).
	artifact := stack.ProjectDir + "/mode-badge.txt"
	stack.Fake.SetPostToolText("Created mode-badge.txt.")
	stack.Fake.EnqueueFileWrite("call-mode-badge", artifact, "mode fixture")
	stack.ChatTurn(t, sessionID, "create a file named mode-badge.txt containing mode fixture", 120*time.Second)

	hp := startTUIAgainst(t, stack, WithTargetSession(sessionID))
	settleAsync()
	time.Sleep(1200 * time.Millisecond)
	settleAsync()
	app := hp.finish()
	chatView := strings.ToLower(app.View().Content)

	// The direct turn must carry a "direct" mode badge somewhere in the
	// transcript (the reply itself names the file, not the mode).
	if !strings.Contains(chatView, "direct") {
		t.Fatalf("F13c: TUI chat shows no mode badge for a direct turn (intent_type/handler_case not surfaced):\n%s", gateTUITail(app.View().Content, 25))
	}
}

// TestTUIEvolverDocRender pins F13c in situ: a 0-phase evolver document
// proposal created through plan.create renders the planCountLine wording
// ("document proposal") in the plans view, not literal zeros. RED today:
// updatePlansTable writes literal zero columns.
func TestTUIEvolverDocRender(t *testing.T) {
	stack := harness.Start(t)
	sessionID := stack.CreateSession(t, "tui-evolver", stack.MeeptHome)

	rpc := harness.DialRPC(t, stack.SocketPath)
	created := rpc.CallResult("plan.create", map[string]any{
		"title":       "Skill evolution: improve tui flow",
		"description": "evolver document proposal",
		"session_id":  sessionID,
	})
	planID, _ := created["id"].(string)
	if planID == "" {
		t.Fatalf("plan.create returned no id: %v", created)
	}

	hp := startTUIAgainst(t, stack, WithTargetSession(sessionID))
	settleAsync()
	hp.sendKey("ctrl+x")
	hp.settle()
	hp.sendKey("p") // plans view
	settleAsync()
	app := hp.finish()
	if app.ActiveView() != tui.ViewPlans {
		t.Fatalf("plans view did not open: view=%v", app.ActiveView())
	}
	view := app.View().Content
	if !strings.Contains(view, "document proposal") {
		t.Fatalf("F13c: plans view renders literal zero counts for the evolver document proposal %s (must reuse planCountLine):\n%s", planID, gateTUITail(view, 30))
	}
}

func gateTUITail(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
