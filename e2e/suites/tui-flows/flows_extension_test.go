//go:build e2e

// Extension flows for the tui-flows suite (plan tree
// docs/plans/20261001-frontend-e2e/, leaf 01):
//
//	tui-plans-01   TestPlansViewRendersCreatedPlan  a created plan renders
//	               in the plans view, fetched through the REAL
//	               plan.list_by_session seam the view uses
//	tui-tasks-01   TestTaskSubmitShowsInTasksView  a dispatched task lands
//	               a tasks.db row and renders as a row in the tasks view
//
// tui-steer-01 (TestChatSteerMidTurnInjectsIntoQueue) is NOT implemented:
// the ctrl+s steering seam is dead against a real daemon — see the report
// accompanying this leaf (TUI EventStream topic-list gap; no daemon-side
// capability can reach the TUI through the shipped subscription set).
package tuiflows

import (
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/tui"
)

// TestPlansViewRendersCreatedPlan covers tui-plans-01: a plan created
// through the real RPC surface (plan.create, session-linked) renders in
// the plans view. The view fetches via plan.list_by_session — the same
// session link is asserted on the daemon store BEFORE the TUI boots, so
// the rendered row is provably store-backed, not in-memory TUI state.
func TestPlansViewRendersCreatedPlan(t *testing.T) {
	stack := harness.Start(t)
	sessionID := stack.CreateSession(t, "plans-session", stack.MeeptHome)

	// Create the plan through the real RPC handler (internal/rpc/plan.go
	// handleCreate) the same way the plan-lifecycle suite does — but
	// session-linked, which is what the TUI plans view filters on
	// (internal/tui/models/plans.go fetchPlans -> plan.list_by_session).
	const planTitle = "quartzstone migration plan"
	const titleFragment = "quartzstone"
	rpc := harness.DialRPC(t, stack.SocketPath)
	created := rpc.CallResult("plan.create", map[string]any{
		"title":       planTitle,
		"description": "e2e tui-plans-01 fixture plan",
		"session_id":  sessionID,
	})
	planID, _ := created["id"].(string)
	if planID == "" {
		t.Fatalf("plan.create returned no id: %v", created)
	}

	// Downstream seam: the store row is linked to THIS session — the
	// exact query the TUI plans view issues on Init (ListPlansBySession
	// joins plan_sessions). If this misses, the view cannot show it.
	bySession := rpc.CallResult("plan.list_by_session", map[string]any{
		"session_id": sessionID,
	})
	plans, _ := bySession["plans"].([]any)
	found := false
	for _, p := range plans {
		if m, ok := p.(map[string]any); ok {
			if id, _ := m["id"].(string); id == planID {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("plan %s not linked to session %s; plan.list_by_session = %v",
			planID, sessionID, bySession)
	}

	// Boot the TUI pinned to the session (SetSession wires
	// PlansModel.sessionID on SessionLoadedMsg), then open the plans
	// view through the palette key flow.
	hp := startTUIAgainst(t, stack, WithTargetSession(sessionID))
	settleAsync()
	if got := hp.app.ActiveSessionID(); got != sessionID {
		t.Fatalf("target session not loaded: got %q want %q", got, sessionID)
	}

	hp.sendKey("ctrl+x")
	hp.settle()
	hp.sendKey("p") // palette action: plans view
	settleAsync()   // plans.Init -> fetchPlans RPC round-trip

	app := hp.finish()
	if app.ActiveView() != tui.ViewPlans {
		t.Fatalf("currentView = %v, want plans view", app.ActiveView())
	}
	view := app.View().Content
	if strings.Contains(view, "loading plans...") {
		t.Fatalf("plans view still loading after fetch settled:\n%s", view)
	}
	// The plan's row renders: the title (truncated to the 22-char title
	// column) comes from the daemon store row.
	if !strings.Contains(view, titleFragment) {
		t.Errorf("plans view does not show the created plan title %q:\n%s", planTitle, view)
	}
	// The header count "(filtered/total)" proves the store-backed list
	// (exactly one session-linked plan) reached the model — the
	// zero-width-viewport regression class renders no rows at all.
	if !strings.Contains(view, "(1/1)") {
		t.Errorf("plans view header count missing (1/1); plans table empty or misfetched:\n%s", view)
	}
	// Draft state icon: plan.create advances planning -> draft.
	if !strings.Contains(view, "draft") {
		t.Errorf("plans view does not show the plan's draft state icon:\n%s", view)
	}
}

// TestTaskSubmitShowsInTasksView covers tui-tasks-01: a chat turn that
// classifies to work dispatches a real task (the store row in tasks.db
// is the downstream seam), and the TUI tasks view renders it as a row
// fetched through the real task.list_extended RPC.
func TestTaskSubmitShowsInTasksView(t *testing.T) {
	stack := harness.Start(t)
	stack.RegisterProject(t, "e2e-project")
	sessionID := stack.CreateSession(t, "tasks-session", stack.ProjectDir)

	// Imperative file request classifies intent=code (the fake-LLM
	// heuristic mirrors the dispatcher's imperative arbitration — the
	// task-state-01 pattern), so the dispatch creates a task whose Name
	// is the message summary. Marker leads the message so it survives
	// the 20-rune name column truncation.
	const marker = "quartzmarble"
	artifact := stack.ProjectDir + "/quartz.txt"
	stack.Fake.SetPostToolText("Created quartz.txt at " + artifact + ".")
	stack.Fake.EnqueueFileWrite("call-tui-tasks-01", artifact, "quartz-list")
	stack.ChatTurn(t, sessionID,
		marker+" post: create a file named quartz.txt containing quartz-list",
		120*time.Second)

	// Downstream seam: the dispatched task exists in the daemon's own
	// task store (the same rows the tasks view's task.list_extended
	// fetch reads).
	harness.WaitFor(t, 20*time.Second, "marker task row in tasks.db", func() bool {
		for _, row := range harness.Tasks(t, stack.TasksDBPath()) {
			if strings.Contains(row.Name, marker) {
				return true
			}
		}
		return false
	})

	// Boot the TUI on the same session, then open the tasks view via
	// the palette key flow (initCurrentView -> TasksModel.Init ->
	// fetchTasks RPC).
	hp := startTUIAgainst(t, stack, WithTargetSession(sessionID))
	settleAsync()
	if got := hp.app.ActiveSessionID(); got != sessionID {
		t.Fatalf("target session not loaded: got %q want %q", got, sessionID)
	}

	hp.sendKey("ctrl+x")
	hp.settle()
	hp.sendKey("t") // palette action: tasks view
	settleAsync()   // tasks.Init -> task.list_extended round-trip

	app := hp.finish()
	if app.ActiveView() != tui.ViewTasks {
		t.Fatalf("currentView = %v, want tasks view", app.ActiveView())
	}
	view := app.View().Content
	if strings.Contains(view, "loading jobs...") {
		t.Fatalf("tasks view still loading after fetch settled:\n%s", view)
	}
	// The task's row renders: the marker name fragment (truncated to the
	// name column width) can only come from the store-backed row.
	if !strings.Contains(view, marker) {
		t.Errorf("tasks view does not show a row for the dispatched task %q:\n%s", marker, view)
	}
	// At least one rendered table row separator (zero-width-viewport
	// regression class: a width-0 viewport renders the header over an
	// empty body with no row rules).
	if !strings.Contains(view, "─") {
		t.Errorf("tasks view shows no rendered table separator:\n%s", view)
	}
}
