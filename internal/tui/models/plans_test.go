package models

// Pins for the 2026-11-22 bughunt TUI plans-view findings:
//   - M3: navigation/Enter must account for synthetic task-gate rows
//     prepended ahead of filterPlans() rows (cursor-to-selection offset).
//   - M4: the banner advertises "x: reject (never executes)"; x must route
//     a focused gate row to task.reject, never to an executing action.

import (
	"encoding/json"
	"errors"
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

// L10 (a): a refresh that empties the merged list must clear m.selected.
// Before the fix the whole cursor block was skipped when len(rows)==0, so
// m.selected kept pointing at a plan that is no longer listed and a/v/c/d
// acted on it from an empty table.
func TestPlansModel_RefreshToEmptyListClearsSelection(t *testing.T) {
	rpc := &plansGateRPC{}
	m := NewPlansModel(rpc)
	m.SetSize(100, 30)
	m.Update(PlansUpdateMsg{Plans: []types.PlanExtended{
		{ID: "plan-1", Title: "first", State: "draft"},
		{ID: "plan-2", Title: "second", State: "draft"},
	}})
	m.table.SetCursor(1)
	m.syncSelectionFromCursor()
	if m.selected == nil || m.selected.ID != "plan-2" {
		t.Fatalf("premise: cursor 1 did not select plan-2 (got %v)", m.selected)
	}

	// Background refresh reports zero plans.
	m.Update(PlansUpdateMsg{Plans: []types.PlanExtended{}})

	if m.selected != nil {
		t.Fatalf("refresh to empty list kept stale selection %q", m.selected.ID)
	}
	// Gate/action keys must be inert on the emptied view: no RPC, no cmd.
	for _, key := range []rune{'a', 'v', 'c'} {
		if cmd := m.Update(tea.KeyPressMsg{Code: key}); cmd != nil {
			t.Fatalf("%q on emptied list produced a cmd (%T); stale selection is still actionable", key, cmd())
		}
	}
	if len(rpc.calls) != 0 {
		t.Fatalf("keys on emptied list issued RPCs: %v", rpc.methods())
	}
}

// L10 (b): a refresh that still lists the selected plan must RESTORE the
// cursor to that plan's merged row instead of resetting to row 0 (the
// tasks.go restoreCursor / sessions.go prev-id discipline). Gate rows
// occupy the first len(gates) merged rows, so the restore math is
// planIdx + gateVisible — pinned here with a live gate row.
func TestPlansModel_RefreshPreservesSelectedPlanRow(t *testing.T) {
	m := newGateOffsetModel(&plansGateRPC{})

	// Merged table: row 0 = gate, row 1 = plan-1. Select the plan.
	m.table.SetCursor(1)
	m.syncSelectionFromCursor()
	if m.selected == nil || m.selected.ID != "plan-1" {
		t.Fatalf("premise: plan row did not select plan-1 (got %v)", m.selected)
	}

	// Background refresh that keeps plan-1 and the gate row.
	m.Update(PlansUpdateMsg{Plans: []types.PlanExtended{
		{ID: "plan-1", Title: "the real plan", State: "draft"},
	}})

	if m.table.Cursor() != 1 {
		t.Fatalf("refresh yanked the cursor off the selected plan: got %d, want 1", m.table.Cursor())
	}
	if m.selected == nil || m.selected.ID != "plan-1" {
		t.Fatalf("refresh lost the still-listed selection: got %v, want plan-1", m.selected)
	}
	// Selection must stay cursor-consistent (the M3 invariant): the cursor
	// is NOT on a gate row, so selectedGate must report false.
	if _, ok := m.selectedGate(); ok {
		t.Fatal("cursor reports a gate row after restoring onto the plan row")
	}
}

// L10 (b), multi-row boundary: the restore must land exactly on the last
// merged row (planIdx = len-1) and stay inside the viewport clamp.
func TestPlansModel_RefreshPreservesSelectedPlanLastRow(t *testing.T) {
	m := NewPlansModel(&plansGateRPC{})
	m.SetSize(100, 30)
	plans := []types.PlanExtended{
		{ID: "plan-1", Title: "one", State: "draft"},
		{ID: "plan-2", Title: "two", State: "draft"},
		{ID: "plan-3", Title: "three", State: "draft"},
	}
	m.Update(PlansUpdateMsg{Plans: plans})
	m.table.SetCursor(2) // no gates: merged row 2 == filterPlans()[2]
	m.syncSelectionFromCursor()
	if m.selected == nil || m.selected.ID != "plan-3" {
		t.Fatalf("premise: last row did not select plan-3 (got %v)", m.selected)
	}

	m.Update(PlansUpdateMsg{Plans: plans})

	if m.table.Cursor() != 2 {
		t.Fatalf("refresh reset a mid-scroll cursor: got %d, want 2", m.table.Cursor())
	}
	if m.selected == nil || m.selected.ID != "plan-3" {
		t.Fatalf("refresh lost the last-row selection: got %v", m.selected)
	}
}

// L10 (iii): when the previously selected plan disappears from the refresh
// (deleted or filtered out), the view falls back to the top row and the
// selection follows the cursor (syncSelectionFromCursor) — it must NEVER
// keep pointing at the disappeared plan.
func TestPlansModel_RefreshSelectedPlanGoneFallsBackToTop(t *testing.T) {
	rpc := &plansGateRPC{}
	m := NewPlansModel(rpc)
	m.SetSize(100, 30)
	m.Update(PlansUpdateMsg{Plans: []types.PlanExtended{
		{ID: "plan-1", Title: "one", State: "draft"},
		{ID: "plan-2", Title: "two", State: "pending_approval"},
	}})
	m.table.SetCursor(1)
	m.syncSelectionFromCursor()
	if m.selected == nil || m.selected.ID != "plan-2" {
		t.Fatalf("premise: cursor 1 did not select plan-2 (got %v)", m.selected)
	}

	// Refresh drops plan-2; only plan-1 remains.
	m.Update(PlansUpdateMsg{Plans: []types.PlanExtended{
		{ID: "plan-1", Title: "one", State: "draft"},
	}})

	if m.selected != nil && m.selected.ID == "plan-2" {
		t.Fatal("refresh kept selection of the disappeared plan-2")
	}
	if m.table.Cursor() != 0 {
		t.Fatalf("fallback did not reset cursor to top: got %d", m.table.Cursor())
	}
	// The selection follows the cursor: row 0 lists plan-1.
	if m.selected == nil || m.selected.ID != "plan-1" {
		t.Fatalf("fallback selection = %v, want plan-1 (cursor-consistent top row)", m.selected)
	}
	// 'd' keys off m.selected.State — it must not arm the reject prompt for
	// the disappeared pending_approval plan (plan-1 is a draft).
	if cmd := m.Update(tea.KeyPressMsg{Code: 'd'}); cmd != nil {
		t.Fatal("d armed the reject prompt after the pending_approval plan disappeared")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if len(rpc.calls) != 0 {
		t.Fatalf("keys issued RPCs after the selection disappeared: %v", rpc.methods())
	}
}

// No prior selection must stay nil across a refresh (initial-load
// discipline: a background poll must not auto-select a plan).
func TestPlansModel_RefreshWithoutPriorSelectionDoesNotAutoSelect(t *testing.T) {
	m := newGateOffsetModel(&plansGateRPC{})

	if m.selected != nil {
		t.Fatalf("premise: fresh model pre-selected %+v", m.selected)
	}
	m.Update(PlansUpdateMsg{Plans: []types.PlanExtended{
		{ID: "plan-1", Title: "the real plan", State: "draft"},
	}})

	if m.selected != nil {
		t.Fatalf("refresh auto-selected %q with no prior selection", m.selected.ID)
	}
	if m.table.Cursor() != 0 {
		t.Fatalf("refresh without prior selection moved the cursor: got %d, want 0", m.table.Cursor())
	}
}

// TestPlansModel_RemovePendingTaskGate pins the bughunt-wave2 finding-2
// fix: SetPendingTaskGates only upserts, so a decided gate row survived
// until restart. RemovePendingTaskGate must drop the overlay row, and the
// PlanActionMsg success path (approve_task/reject_task) must call it.
func TestPlansModel_RemovePendingTaskGate(t *testing.T) {
	m := newGateOffsetModel(&plansGateRPC{})

	// Precondition: the gate row is in the overlay.
	if len(m.pendingTaskGatesForFilterLocked()) != 1 {
		t.Fatal("precondition: expected 1 overlay gate")
	}

	m.RemovePendingTaskGate("task-gate-1")

	if gates := m.pendingTaskGatesForFilterLocked(); len(gates) != 0 {
		t.Fatalf("overlay still holds %d gate(s) after RemovePendingTaskGate", len(gates))
	}
	// The merged table must shrink back to just the real plan.
	if rows := m.table.Rows(); len(rows) != 1 {
		t.Fatalf("table has %d rows after gate removal, want 1 (the real plan)", len(rows))
	}

	// Unknown/empty ids are inert.
	m.RemovePendingTaskGate("nope")
	m.RemovePendingTaskGate("")
}

// TestPlansModel_PlanActionMsgRemovesGateOnSuccess pins the PlanActionMsg
// success path: approve_task / reject_task results drop the overlay row;
// an errored action keeps it (the gate is still pending server-side).
func TestPlansModel_PlanActionMsgRemovesGateOnSuccess(t *testing.T) {
	cases := []struct {
		name   string
		action string
		err    error
		want   int
	}{
		{"approve success removes", "approve_task", nil, 0},
		{"reject success removes", "reject_task", nil, 0},
		{"approve failure keeps", "approve_task", errStub, 1},
		{"plain plan approve keeps gate", "approve", nil, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newGateOffsetModel(&plansGateRPC{})
			m.Update(PlanActionMsg{PlanID: "task-gate-1", Action: tc.action, Err: tc.err})
			if got := len(m.pendingTaskGatesForFilterLocked()); got != tc.want {
				t.Fatalf("overlay gates = %d, want %d", got, tc.want)
			}
		})
	}
}

var errStub = errors.New("rpc failed")

// pendingTaskGatesForFilterLocked is a test helper: the production
// accessor requires the caller to hold sessionMu.
func (m *PlansModel) pendingTaskGatesForFilterLocked() []TaskGateApprovalMsg {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	return m.pendingTaskGatesForFilter()
}
