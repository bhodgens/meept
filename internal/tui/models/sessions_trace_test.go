package models

// 09-cli-affordances TEST-GATE (task 6, TUI half): pressing `t` on a
// selected session in the sessions view emits SessionTraceRequestedMsg;
// the fetched rows render in the session detail pane. RED before the
// model grows the affordance.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

type traceCapRPC struct {
	mockSessionsRPC
	calls []string
}

func (t *traceCapRPC) Call(method string, params any) (json.RawMessage, error) {
	t.calls = append(t.calls, method)
	if method == "session.dispatch_trace" {
		if t.callErr != nil {
			return nil, t.callErr
		}
		raw, _ := json.Marshal(map[string]any{
			"entries": []map[string]any{
				{
					"turn_id":           "turn-1",
					"input_summary":     "route me carefully",
					"intent_type":       "plan",
					"agent_id":          "planner",
					"confidence":        0.91,
					"classifier_method": "llm",
					"handler_case":      "plan_route",
				},
			},
			"count": 1,
		})
		return raw, nil
	}
	return t.mockSessionsRPC.Call(method, params)
}

func newSessionsTraceModel(t *testing.T) (*SessionsModel, *traceCapRPC) {
	t.Helper()
	rpc := &traceCapRPC{mockSessionsRPC: mockSessionsRPC{connected: true}}
	m := newSessionsModelWithSessions(rpc, []types.Session{
		{ID: "sess-trace", Name: "trace me", LastActivity: "2026-11-22T10:00:00Z"},
	})
	return m, rpc
}

// TestSessionsTraceKeyEmitsRequest pins key `t`: on a selected session it
// returns a cmd whose msg requests the dispatch trace.
func TestSessionsTraceKeyEmitsRequest(t *testing.T) {
	m, _ := newSessionsTraceModel(t)

	cmd := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	if cmd == nil {
		t.Fatal("09-gate: pressing 't' on a selected session returned nil cmd")
	}
	msg := cmd()
	req, ok := msg.(SessionTraceRequestedMsg)
	if !ok {
		t.Fatalf("09-gate: expected SessionTraceRequestedMsg, got %T", msg)
	}
	if req.SessionID != "sess-trace" {
		t.Errorf("09-gate: SessionTraceRequestedMsg.SessionID = %q, want sess-trace", req.SessionID)
	}
}

// TestSessionsTraceRowsRenderInDetail pins the render half: after the
// trace rows arrive, the session detail pane shows the routing evidence
// (intent / handler case / input summary).
func TestSessionsTraceRowsRenderInDetail(t *testing.T) {
	m, rpc := newSessionsTraceModel(t)

	cmd := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	if cmd == nil {
		t.Fatal("09-gate: 't' returned nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(SessionTraceRequestedMsg); !ok {
		t.Fatalf("09-gate: expected SessionTraceRequestedMsg, got %T", msg)
	}

	// Drive the fetched rows through the model's Update (the same seam
	// SessionPlansUpdateMsg uses).
	traceCmd := m.fetchSessionTrace("sess-trace")
	if traceCmd == nil {
		t.Fatal("09-gate: fetchSessionTrace returned nil")
	}
	upd := traceCmd()
	updateMsg, ok := upd.(SessionTraceUpdateMsg)
	if !ok {
		t.Fatalf("09-gate: expected SessionTraceUpdateMsg, got %T", upd)
	}
	m.Update(updateMsg)

	view := m.View()
	for _, want := range []string{"route me carefully", "plan", "plan_route"} {
		if !strings.Contains(view, want) {
			t.Errorf("09-gate: sessions detail view missing trace evidence %q", want)
		}
	}
	if len(rpc.calls) == 0 || rpc.calls[len(rpc.calls)-1] != "session.dispatch_trace" {
		t.Errorf("09-gate: fetch did not end on session.dispatch_trace (calls: %v)", rpc.calls)
	}
}

// TestSessionsTraceFetchFailsGracefully pins the error lane: an RPC
// transport error surfaces in the detail pane, never a panic or a blank
// pane.
func TestSessionsTraceFetchFailsGracefully(t *testing.T) {
	rpc := &traceCapRPC{mockSessionsRPC: mockSessionsRPC{connected: true, callErr: errors.New("rpc boom")}}
	m := newSessionsModelWithSessions(rpc, []types.Session{
		{ID: "sess-trace-err", Name: "trace err", LastActivity: "2026-11-22T10:00:00Z"},
	})

	traceCmd := m.fetchSessionTrace("sess-trace-err")
	if traceCmd == nil {
		t.Fatal("09-gate: fetchSessionTrace returned nil")
	}
	upd := traceCmd()
	updateMsg, ok := upd.(SessionTraceUpdateMsg)
	if !ok {
		t.Fatalf("09-gate: expected SessionTraceUpdateMsg, got %T", upd)
	}
	if updateMsg.Err == nil {
		t.Fatal("09-gate: an RPC transport error must surface as Err, got nil")
	}
	m.Update(updateMsg)
	// No panic is the assertion; the view must still render and carry
	// the error text.
	view := m.View()
	if !strings.Contains(view, "rpc boom") {
		t.Errorf("09-gate: trace error not rendered in the detail pane:\n%s", view)
	}
}

// TestSessionsTraceKeyNoSelectionNil pins the guard: `t` with no
// selectable session under the cursor must not fire a request.
func TestSessionsTraceKeyNoSelectionNil(t *testing.T) {
	rpc := &traceCapRPC{mockSessionsRPC: mockSessionsRPC{connected: true}}
	m := newSessionsModelWithSessions(rpc, nil)

	if cmd := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"}); cmd != nil {
		t.Fatal("09-gate: 't' on an empty sessions list must return nil")
	}
}
