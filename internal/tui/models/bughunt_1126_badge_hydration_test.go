package models

import (
	"encoding/json"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

// bughunt 2026-11-24 M12 (issue #187), two halves pinned here:
//
//  1. JOIN: restored-transcript badge hydration joined dispatch rows to
//     assistant bubbles by INPUT TEXT PREFIX (dispatchInputMatches). Two
//     user turns with identical text ("yes") misjoined; a legacy
//     S4-privacy row with an empty input summary matched NOTHING and
//     rendered zero badges. The join is now deterministic by ARRIVAL
//     ORDER: nth stampable assistant bubble ↔ nth stampable dispatch
//     row, scanned strictly forward; text is kept only as a
//     DISQUALIFIER when both sides carry it and it disagrees.
//
//  2. ASYNC: the dispatch_trace RPC used to run synchronously INSIDE
//     Update (SessionMessagesLoadedMsg -> loadServerMessages ->
//     hydrateRestoredClassifications -> rpc.Call), freezing every
//     keypress if the daemon stalled. It now rides a tea.Cmd exactly
//     like the session load itself; the stamps land from a
//     DispatchTraceLoadedMsg.

// m12CountingRPC records session.dispatch_trace round-trips so the
// async half can prove Update itself never blocks on the RPC.
type m12CountingRPC struct {
	*MockChatRPCClient
	traceCalls int
}

func (r *m12CountingRPC) Call(method string, params any) (json.RawMessage, error) {
	if method == "session.dispatch_trace" {
		r.traceCalls++
	}
	return r.MockChatRPCClient.Call(method, params)
}

func m12PrivacyRows() []dispatchTraceRow {
	// Two rows, valid badges, EMPTY input summaries (S4 privacy posture),
	// oldest-first — the shape fetchDispatchTraceCmd hands the stamper.
	return []dispatchTraceRow{
		{TurnID: "sess-1:1", InputSummary: "", IntentType: "plan", HandlerCase: "async_dispatch"},
		{TurnID: "sess-1:2", InputSummary: "", IntentType: "direct", HandlerCase: "direct_mode"},
	}
}

func m12TwoYesTurns() []ChatMessage {
	return []ChatMessage{
		{Role: RoleUser, Content: "yes"},
		{Role: RoleAssistant, Content: "planning started"},
		{Role: RoleUser, Content: "yes"},
		{Role: RoleAssistant, Content: "done"},
	}
}

func TestM12_PositionalJoinBadgesIdenticalTextTurns(t *testing.T) {
	m := &ChatModel{}
	msgs := m12TwoYesTurns()
	m.applyRestoredClassifications(msgs, m12PrivacyRows())
	if msgs[1].IntentType != "plan" || msgs[1].HandlerCase != "async_dispatch" {
		t.Errorf("first bubble badges = %q/%q, want plan/async_dispatch", msgs[1].IntentType, msgs[1].HandlerCase)
	}
	if msgs[3].IntentType != "direct" || msgs[3].HandlerCase != "direct_mode" {
		t.Errorf("second bubble badges = %q/%q, want direct/direct_mode", msgs[3].IntentType, msgs[3].HandlerCase)
	}
}

func TestM12_JoinNeverAssignsSameRowTwice(t *testing.T) {
	m := &ChatModel{}
	msgs := m12TwoYesTurns()
	// Only ONE stampable row: the second bubble must stay unbadged, and
	// the row must not be stamped onto both bubbles.
	rows := []dispatchTraceRow{
		{TurnID: "sess-1:1", InputSummary: "", IntentType: "plan", HandlerCase: "async_dispatch"},
	}
	m.applyRestoredClassifications(msgs, rows)
	if msgs[1].IntentType != "plan" {
		t.Fatalf("first bubble badges = %q, want plan", msgs[1].IntentType)
	}
	if msgs[3].IntentType != "" || msgs[3].HandlerCase != "" {
		t.Errorf("second bubble got badges %q/%q, want unbadged (row already consumed)", msgs[3].IntentType, msgs[3].HandlerCase)
	}
}

func TestM12_TextDisagreementBlocksMisjoin(t *testing.T) {
	// The row carries text that DISAGREES with the user turn; a prefix
	// join could badge the wrong bubble from a later/earlier row. Both
	// sides non-empty and mismatched => the join refuses.
	m := &ChatModel{}
	msgs := []ChatMessage{
		{Role: RoleUser, Content: "compile the wasm target"},
		{Role: RoleAssistant, Content: "on it"},
	}
	rows := []dispatchTraceRow{
		{TurnID: "sess-1:9", InputSummary: "yes, deploy it", IntentType: "deploy", HandlerCase: "deploy_mode"},
	}
	m.applyRestoredClassifications(msgs, rows)
	if msgs[1].IntentType != "" || msgs[1].HandlerCase != "" {
		t.Errorf("bubble badges = %q/%q, want unbadged (text mismatch must block the join)", msgs[1].IntentType, msgs[1].HandlerCase)
	}
}

func TestM12_HydrationNoLongerBlocksUpdate(t *testing.T) {
	// RED at HEAD: the SessionMessagesLoadedMsg case performed the
	// session.dispatch_trace RPC synchronously inside Update (traceCalls
	// incremented before Update returned). GREEN: Update returns a
	// tea.Cmd instead and performs no RPC until the cmd runs.
	mock := &m12CountingRPC{MockChatRPCClient: NewMockChatRPCClient()}
	style := lipgloss.NewStyle()
	m := NewChatModelWithConfig(mock, style, style, style, "once",
		InputBehaviorConfig{EnterBehavior: "shift_sends"}, ChatConfig{})
	m.sessionID = "sess-1"
	m.sessionMessages["sess-1"] = []ChatMessage{}

	cmd := m.Update(SessionMessagesLoadedMsg{
		SessionID: "sess-1",
		Messages:  []types.SessionMessage{{ID: 1, Role: RoleAssistant, Content: "planning started"}},
	})
	if mock.traceCalls != 0 {
		t.Fatalf("Update performed %d session.dispatch_trace call(s) synchronously; hydration must ride a tea.Cmd (M12 half B)", mock.traceCalls)
	}
	if cmd == nil {
		t.Fatal("Update returned no tea.Cmd for the hydration fetch; the badges would never land")
	}
	msg := cmd()
	delivered, ok := msg.(DispatchTraceLoadedMsg)
	if !ok {
		t.Fatalf("cmd delivered %T, want DispatchTraceLoadedMsg", msg)
	}
	if delivered.SessionID != "sess-1" {
		t.Errorf("msg.SessionID = %q, want sess-1", delivered.SessionID)
	}
	if mock.traceCalls != 1 {
		t.Errorf("traceCalls after running the cmd = %d, want 1", mock.traceCalls)
	}
}
