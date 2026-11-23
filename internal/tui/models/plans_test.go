package models

// Pins for the 2026-11-22 bughunt TUI plans-view findings:
//   - M3: navigation/Enter must account for synthetic task-gate rows
//     prepended ahead of filterPlans() rows (cursor-to-selection offset).
//   - M4: the banner advertises "x: reject (never executes)"; x must route
//     a focused gate row to task.reject, never to an executing action.

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

// plansGateRPC records every RPC call with its params so the pins can
// assert the exact wire method AND that reject never calls task.approve.
type plansGateRPC struct {
	calls []plansGateCall
}

type plansGateCall struct {
	method string
	params map[string]any
}

func (r *plansGateRPC) Call(method string, params any) (json.RawMessage, error) {
	p, _ := params.(map[string]any)
	r.calls = append(r.calls, plansGateCall{method: method, params: p})
	if method == "plan.list_by_session" {
		return json.Marshal(types.PlanListResponse{})
	}
	return json.Marshal(map[string]any{})
}

func (r *plansGateRPC) IsConnected() bool { return true }

func (r *plansGateRPC) methods() []string {
	out := make([]string, 0, len(r.calls))
	for _, c := range r.calls {
		out = append(out, c.method)
	}
	return out
}

// newGateOffsetModel builds the exact M3 shape: one synthetic gate row and
// one real plan. updatePlansTable prepends the gate, so merged-row 0 is the
// gate and merged-row 1 is the plan (while filterPlans() has length 1).
func newGateOffsetModel(rpc *plansGateRPC) *PlansModel {
	m := NewPlansModel(rpc)
	m.SetSize(100, 30)
	m.Update(PlansUpdateMsg{Plans: []types.PlanExtended{
		{ID: "plan-1", Title: "the real plan", State: "draft"},
	}})
	m.SetPendingTaskGates([]TaskGateApprovalMsg{
		{TaskID: "task-gate-1", SessionID: "sess-1", Total: 2},
	})
	return m
}

func TestPlansModel_GateRowOffset_EnterOnPlanRowSelectsPlan(t *testing.T) {
	m := newGateOffsetModel(&plansGateRPC{})

	// Merged table: row 0 = gate, row 1 = the plan. Put the cursor on
	// the plan row and press enter.
	m.table.SetCursor(1)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.selected == nil {
		t.Fatal("enter on the plan row (merged index 1) selected nothing (M3 bounds bug)")
	}
	if m.selected.ID != "plan-1" {
		t.Fatalf("enter on the plan row selected %q, want %q", m.selected.ID, "plan-1")
	}
	if !m.showingDetail {
		t.Fatal("enter on the plan row did not open the detail modal")
	}
}

func TestPlansModel_GateRowOffset_EnterOnGateRowSelectsNoPlan(t *testing.T) {
	m := newGateOffsetModel(&plansGateRPC{})

	// The gate row is merged index 0; the old code mapped cursor 0 into
	// filterPlans()[0] and selected the real plan. It must select nothing.
	m.table.SetCursor(0)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.selected != nil {
		t.Fatalf("enter on the gate row selected plan %q; gate rows must not select a plan (M3)", m.selected.ID)
	}
}

func TestPlansModel_GateRowOffset_NavigationKeepsSelectionInSync(t *testing.T) {
	m := newGateOffsetModel(&plansGateRPC{})

	// Moving down from the gate row onto the plan row must select the
	// plan; moving back up onto the gate row must clear it.
	m.table.SetCursor(0)
	m.Update(tea.KeyPressMsg{Code: 'j'}) // -> cursor 1 (plan row)
	if m.selected == nil || m.selected.ID != "plan-1" {
		t.Fatalf("after navigating to the plan row, selected = %+v, want plan-1", m.selected)
	}
	m.Update(tea.KeyPressMsg{Code: 'k'}) // -> cursor 0 (gate row)
	if m.selected != nil {
		t.Fatalf("after navigating back to the gate row, selected = %+v, want nil", m.selected)
	}
}

func TestPlansModel_GateRowOffset_AApprovesTaskOnGatePlanOnPlan(t *testing.T) {
	rpc := &plansGateRPC{}
	m := newGateOffsetModel(rpc)

	// On the gate row, "a" must approve the TASK (task.approve), never
	// the plan. This is the action the gate banner advertises.
	m.table.SetCursor(0)
	m.Update(tea.KeyPressMsg{Code: 'a'})
	// drain the returned cmd so the RPC call is recorded.
	rpc.calls = nil
	if cmd := m.Update(tea.KeyPressMsg{Code: 'a'}); cmd != nil {
		cmd()
	}
	methods := rpc.methods()
	if len(methods) != 1 || methods[0] != "task.approve" {
		t.Fatalf("a on gate row called %v, want exactly [task.approve]", methods)
	}
	if got := rpc.calls[0].params["task_id"]; got != "task-gate-1" {
		t.Fatalf("task.approve task_id = %v, want task-gate-1", got)
	}

	// On the plan row, "a" must approve the PLAN (plan.approve).
	// Navigate with j so selection syncs the way it does interactively.
	rpc.calls = nil
	m.table.SetCursor(0)
	m.Update(tea.KeyPressMsg{Code: 'j'}) // -> cursor 1, syncs selection
	if m.selected == nil || m.selected.ID != "plan-1" {
		t.Fatalf("navigation did not select plan-1: %+v", m.selected)
	}
	if cmd := m.Update(tea.KeyPressMsg{Code: 'a'}); cmd != nil {
		cmd()
	}
	methods = rpc.methods()
	if len(methods) != 1 || methods[0] != "plan.approve" {
		t.Fatalf("a on plan row called %v, want exactly [plan.approve]", methods)
	}
	if got := rpc.calls[0].params["plan_id"]; got != "plan-1" {
		t.Fatalf("plan.approve plan_id = %v, want plan-1", got)
	}
}

func TestPlansModel_GateBannerXRejectsGateTaskNeverExecutes(t *testing.T) {
	rpc := &plansGateRPC{}
	m := newGateOffsetModel(rpc)

	// The banner must advertise the x key we actually handle.
	view := m.View()
	if !strings.Contains(view, "x: reject") || !strings.Contains(view, "never executes") {
		t.Fatalf("gate banner does not advertise x: reject (never executes):\n%s", view)
	}

	// Focus the gate row and press x: it must arm the reject prompt.
	m.table.SetCursor(0)
	cmd := m.Update(tea.KeyPressMsg{Code: 'x'})
	if cmd == nil {
		t.Fatal("x on the gate row produced no cmd")
	}
	if _, ok := cmd().(planRejectInputMsg); !ok {
		t.Fatalf("x on the gate row produced %T, want planRejectInputMsg", cmd())
	}

	// Complete the prompt with a reason and submit.
	m.Update(planRejectInputMsg{active: true})
	for _, r := range "no" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	submitCmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if submitCmd == nil {
		t.Fatal("enter in the gate reject prompt produced no cmd")
	}
	submitted, ok := submitCmd().(planRejectSubmittedMsg)
	if !ok {
		t.Fatalf("gate reject prompt produced %T, want planRejectSubmittedMsg", submitCmd())
	}
	if submitted.gateID != "task-gate-1" || submitted.planID != "" {
		t.Fatalf("submitted gate reject = %+v, want gateID=task-gate-1 planID empty", submitted)
	}
	if submitted.reason != "no" {
		t.Fatalf("submitted reason = %q, want %q", submitted.reason, "no")
	}

	// Submitting must call task.reject with the reason and MUST NOT
	// call task.approve (reject never executes).
	finalCmd := m.Update(submitted)
	if finalCmd == nil {
		t.Fatal("gate reject submit produced no cmd")
	}
	result, ok := finalCmd().(PlanActionMsg)
	if !ok {
		t.Fatalf("gate reject cmd produced %T, want PlanActionMsg", finalCmd())
	}
	if result.Action != "reject_task" || result.Err != nil {
		t.Fatalf("gate reject action = %+v, want reject_task err=nil", result)
	}
	methods := rpc.methods()
	if len(methods) != 1 || methods[0] != "task.reject" {
		t.Fatalf("gate reject called %v, want exactly [task.reject]", methods)
	}
	if got := rpc.calls[0].params["task_id"]; got != "task-gate-1" {
		t.Fatalf("task.reject task_id = %v, want task-gate-1", got)
	}
	if got := rpc.calls[0].params["reason"]; got != "no" {
		t.Fatalf("task.reject reason = %v, want %q", got, "no")
	}
}

func TestPlansModel_XOnPlanRowIsInert(t *testing.T) {
	rpc := &plansGateRPC{}
	m := newGateOffsetModel(rpc)

	// With no gate row focused, x must not open a prompt or issue an RPC.
	m.table.SetCursor(1)
	if cmd := m.Update(tea.KeyPressMsg{Code: 'x'}); cmd != nil {
		t.Fatalf("x on a plan row produced a cmd (%T); x is a gate-row key", cmd())
	}
	if m.rejectInputActive {
		t.Fatal("x on a plan row activated the reject prompt")
	}
	if len(rpc.calls) != 0 {
		t.Fatalf("x on a plan row issued RPCs: %v", rpc.methods())
	}
}

// A refresh rebuilds the table and calls GotoTop(). Selection must follow
// the reset cursor (row 0), never keep pointing at the previously selected
// plan; otherwise a/v/c/d act on a stale plan after a background refresh.
func TestPlansModel_RefreshResetsSelectionToFirstRow(t *testing.T) {
	m := newGateOffsetModel(&plansGateRPC{})

	// Cursor on the plan row selects the real plan.
	m.table.SetCursor(1)
	m.syncSelectionFromCursor()
	if m.selected == nil || m.selected.ID != "plan-1" {
		t.Fatalf("premise: plan row did not select plan-1 (got %v)", m.selected)
	}

	// A background refresh rebuilds rows and resets the cursor to row 0,
	// which is the synthetic gate row. Selection must be cleared.
	m.Update(PlansUpdateMsg{Plans: []types.PlanExtended{
		{ID: "plan-1", Title: "the real plan", State: "draft"},
	}})

	if m.table.Cursor() != 0 {
		t.Fatalf("refresh did not reset cursor to 0: got %d", m.table.Cursor())
	}
	if m.selected != nil {
		t.Fatalf("refresh kept stale selection %q while cursor is on gate row 0", m.selected.ID)
	}
	if _, ok := m.selectedGate(); !ok {
		t.Fatal("premise: row 0 is not a gate row")
	}
}
