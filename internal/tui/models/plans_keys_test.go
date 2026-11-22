package models

// Leaf 08 (20261121-interactive-usability) plans-tab fixes:
//   - the "d" reject key opens the reason prompt for a pending_approval
//     plan and submits plan.reject with the typed reason (GUI parity:
//     plans_tab.dart reject dialog);
//   - refresh requests (manual `r` and lifecycle events) are debounced:
//     a burst coalesces into one fetch.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

// countingPlansRPC records plan.* calls for assertion.
type countingPlansRPC struct {
	calls []string
	fail  bool
}

func (c *countingPlansRPC) Call(method string, params any) (json.RawMessage, error) {
	c.calls = append(c.calls, method)
	if c.fail {
		return nil, errors.New("rpc down")
	}
	if method == "plan.list_by_session" {
		return json.Marshal(types.PlanListResponse{})
	}
	return json.Marshal(map[string]any{})
}

func (c *countingPlansRPC) IsConnected() bool { return true }

func TestPlansModel_RejectKeyOpensPromptAndSubmitsReason(t *testing.T) {
	rpc := &countingPlansRPC{}
	m := NewPlansModel(rpc)
	m.SetSize(100, 30)
	m.Update(PlansUpdateMsg{Plans: newTestPlansForReject()})

	// Select the pending_approval plan and press d.
	m.table.Cursor()
	m.selected = &newTestPlansForReject()[0]
	cmd := m.Update(tea.KeyPressMsg{Code: 'd'})
	if cmd == nil {
		t.Fatal("d on a pending_approval plan produced no cmd (reject prompt never armed)")
	}

	// Feed the armed prompt message back (bubbletea loop).
	msg := cmd()
	if _, ok := msg.(planRejectInputMsg); !ok {
		t.Fatalf("d produced %T, want planRejectInputMsg", msg)
	}
	m.Update(msg)

	// The prompt view renders.
	view := m.View()
	if !strings.Contains(view, "reject plan") {
		t.Fatalf("reject prompt view missing after d key:\n%s", view)
	}

	// Activate the prompt, type a reason, submit with enter.
	m.Update(planRejectInputMsg{active: true})
	for _, r := range "bad plan" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	submitCmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if submitCmd == nil {
		t.Fatal("enter in the reject prompt produced no cmd")
	}
	msg = submitCmd()
	submitted, ok := msg.(planRejectSubmittedMsg)
	if !ok {
		t.Fatalf("enter produced %T, want planRejectSubmittedMsg", msg)
	}
	if submitted.reason != "bad plan" {
		t.Fatalf("submitted reason = %q, want %q", submitted.reason, "bad plan")
	}

	// The submit resolves into a plan.reject RPC call with the reason.
	finalCmd := m.Update(submitted)
	if finalCmd == nil {
		t.Fatal("planRejectSubmittedMsg produced no cmd")
	}
	resultMsg := finalCmd()
	action, ok2 := resultMsg.(PlanActionMsg)
	if !ok2 {
		t.Fatalf("reject cmd produced %T, want PlanActionMsg", resultMsg)
	}
	if action.Action != "reject" || action.Err != nil {
		t.Fatalf("PlanActionMsg = %+v, want action=reject err=nil", action)
	}
	found := false
	for _, c := range rpc.calls {
		if c == "plan.reject" {
			found = true
		}
	}
	if !found {
		t.Fatalf("plan.reject RPC never called (calls: %v)", rpc.calls)
	}
}

func TestPlansModel_EscCancelsRejectPrompt(t *testing.T) {
	rpc := &countingPlansRPC{}
	m := NewPlansModel(rpc)
	m.SetSize(100, 30)
	m.Update(PlansUpdateMsg{Plans: newTestPlansForReject()})
	m.selected = &newTestPlansForReject()[0]

	m.Update(tea.KeyPressMsg{Code: 'd'})
	m.Update(planRejectInputMsg{active: true})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})

	if m.rejectInputActive {
		t.Fatal("esc did not cancel the reject prompt")
	}
	for _, c := range rpc.calls {
		if c == "plan.reject" {
			t.Fatal("esc path still issued plan.reject")
		}
	}
}

func TestPlansModel_RejectKeyIgnoredForNonPendingPlan(t *testing.T) {
	rpc := &countingPlansRPC{}
	m := NewPlansModel(rpc)
	m.SetSize(100, 30)
	drafts := newTestPlansForReject()
	m.Update(PlansUpdateMsg{Plans: drafts})
	m.selected = &drafts[1] // the draft row

	if cmd := m.Update(tea.KeyPressMsg{Code: 'd'}); cmd != nil {
		t.Fatal("d on a non-pending_approval plan must be ignored")
	}
}

func TestPlansModel_RefreshRequestsAreDebounced(t *testing.T) {
	rpc := &countingPlansRPC{}
	m := NewPlansModel(rpc)
	m.SetSize(100, 30)

	// First lifecycle event arms the fetch + debounce tick.
	cmd := m.Update(plansRefreshRequestedMsg{})
	if cmd == nil {
		t.Fatal("first refresh request produced no cmd")
	}
	msgs := drainBatchMsgs(cmd)
	var fetchArmed bool
	var tickArmed bool
	for _, msg := range msgs {
		switch msg.(type) {
		case PlansUpdateMsg:
			fetchArmed = true
		case planDebounceMsg:
			tickArmed = true
		}
	}
	if !fetchArmed || !tickArmed {
		t.Fatalf("first refresh: fetchArmed=%v tickArmed=%v", fetchArmed, tickArmed)
	}
	if !m.fetchPending {
		t.Fatal("fetchPending gate not set after the first request")
	}

	// A burst of further events inside the window coalesces: no new fetch.
	for i := 0; i < 5; i++ {
		if cmd := m.Update(plansRefreshRequestedMsg{}); cmd != nil {
			t.Fatalf("burst event %d re-armed a fetch inside the debounce window", i)
		}
	}

	// The debounce tick clears the gate; the next event fetches again.
	m.Update(planDebounceMsg{})
	if m.fetchPending {
		t.Fatal("fetchPending still set after the debounce tick")
	}
	if cmd := m.Update(plansRefreshRequestedMsg{}); cmd == nil {
		t.Fatal("refresh after the debounce window produced no cmd")
	}
}

func TestPlansModel_ManualRefreshIsDebounced(t *testing.T) {
	rpc := &countingPlansRPC{}
	m := NewPlansModel(rpc)
	m.SetSize(100, 30)

	if cmd := m.Update(tea.KeyPressMsg{Code: 'r'}); cmd == nil {
		t.Fatal("r produced no cmd")
	}
	if !m.fetchPending {
		t.Fatal("r did not set the fetchPending gate")
	}
	if cmd := m.Update(tea.KeyPressMsg{Code: 'r'}); cmd != nil {
		t.Fatal("second r inside the debounce window re-armed a fetch")
	}

	// The window elapses, r works again.
	m.Update(planDebounceMsg{})
	if cmd := m.Update(tea.KeyPressMsg{Code: 'r'}); cmd == nil {
		t.Fatal("r after the debounce window produced no cmd")
	}
}

func TestPlansModel_HintMentionsRejectKey(t *testing.T) {
	m := NewPlansModel(&countingPlansRPC{})
	m.SetSize(100, 30)
	view := m.View()
	if !strings.Contains(view, "d: reject") {
		t.Fatalf("plans hint line does not mention the d: reject key:\n%s", view)
	}
}

// --- helpers ---

func newTestPlansForReject() []types.PlanExtended {
	return []types.PlanExtended{
		{ID: "plan-pending", Title: "awaiting approval", State: "pending_approval"},
		{ID: "plan-draft", Title: "a draft", State: "draft"},
	}
}
