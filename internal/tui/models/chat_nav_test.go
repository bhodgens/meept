package models

// 05-navigation TEST-GATE T2 (leaf 00T-gate.md, matrix F12a; master
// C1/C4/C5/C6 consumers).
//
// Pins the reply->task navigation seam: a completed task reply renders a
// task chip carrying the task id, Enter on the selected chip emits a
// navigation signal to the task detail, and the tasks-view detail modal
// shows FRESH steps fetched through ListTaskSteps (the zero-caller seam
// at connection.go:43) with Escape closing it. Trace expand (C4
// include_tool_calls) and the why-this-route popover (C5 dispatch_trace)
// are pinned at the read-surface seam: the RPC round-trips must happen
// and their rows must render.
//
// RED today: no chip exists (ChatMessage has no TaskID, no chip render,
// no navigation msg), Enter on a viewport selection is a no-op, the
// detail modal never calls ListTaskSteps, and no trace/popover read
// surface is wired. Reflection/behavior assertions keep this file
// compiling at HEAD (spec-first gate rule).

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

// newGateChatModel builds a chat model on an arbitrary RPCClient (the
// recording wrapper) with the same deterministic setup as the leaf-04
// helper: accepted submit, manual timers, fixed size.
func newGateChatModel(t *testing.T, rpc RPCClient) *ChatModel {
	t.Helper()
	style := lipgloss.NewStyle()
	model := NewChatModelWithConfig(rpc, style, style, style, "once",
		InputBehaviorConfig{EnterBehavior: "shift_sends"}, ChatConfig{
			ScrollSpeed:     3,
			LivenessTimeout: 90 * time.Second,
		})
	model.SetSize(100, 30)
	model.sessionID = "sess-1"
	model.turns = newTurnRouter(NewManualTimerSource())
	return model
}

// gateFindTaskNavMsg scans produced messages for a task-navigation
// signal carrying the expected task id. The concrete msg type is the
// implementation leaf's choice; the gate contract is "a tea.Msg exists
// whose exported string field holds the task id and whose type name
// names task navigation" (TaskChipNavMsg / OpenTaskDetailMsg class).
func gateFindTaskNavMsg(msgs []tea.Msg, taskID string) bool {
	for _, m := range msgs {
		if m == nil {
			continue
		}
		name := reflect.TypeOf(m).Name()
		if !strings.Contains(strings.ToLower(name), "task") {
			continue
		}
		rv := reflect.ValueOf(m)
		for rv.Kind() == reflect.Ptr {
			rv = rv.Elem()
		}
		if rv.Kind() != reflect.Struct {
			continue
		}
		for i := 0; i < rv.NumField(); i++ {
			f := rv.Field(i)
			if f.Kind() == reflect.String && f.String() == taskID {
				return true
			}
		}
	}
	return false
}

// gateCompletedTaskReply submits a turn and delivers a C1 terminal with
// task_id set, leaving the reply bubble selected-able in the transcript.
func gateCompletedTaskReply(t *testing.T, model *ChatModel) {
	t.Helper()
	gateSubmitTurn(t, model)
	model.DeliverTurnTerminal("turn-1", gateC1Payload("turn-1", model.conversationID))
	pt := mustGet(t, model, "turn-1")
	pt.mu.Lock()
	res := pt.result
	pt.mu.Unlock()
	model.Update(res)
}

// TestTaskChipNav pins the chip render + Enter navigation contract.
func TestTaskChipNav(t *testing.T) {
	model, _ := newAsyncTestChatModel(t)
	gateCompletedTaskReply(t, model)

	// 1. The reply bubble renders a task chip carrying the task id.
	view := model.View()
	if !strings.Contains(view, "task-42") {
		t.Errorf("completed task reply renders no task chip with the task id (C1 task_id dropped; W14): view has no 'task-42'")
	}

	// 2. Enter on the selected chip emits a navigation signal to the
	// task detail (the App opens the tasks-view detail modal).
	model.SetFocus(FocusViewport)
	model.selectedMsgIdx = len(model.messages) - 1
	cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msgs := drainBatchMsgs(cmd)
	if !gateFindTaskNavMsg(msgs, "task-42") {
		t.Errorf("Enter on the task-chip reply emitted no task-detail navigation msg carrying task-42 (no chip nav wired; W14/W19); got %v", msgs)
	}
}

// TestTaskChipNavDetailFreshSteps pins the detail-modal half: opening a
// task detail fetches FRESH steps through ListTaskSteps (today zero
// callers) and renders them; Escape closes the modal; the steps counter
// is labeled completed/total.
func TestTaskChipNavDetailFreshSteps(t *testing.T) {
	mock := NewMockTasksRPCClient()
	mock.TasksExtendedResponse = &types.TaskExtendedListResponse{
		Tasks: []types.TaskExtended{{
			Task: types.Task{
				ID:            "task-42",
				Name:          "quartz task",
				State:         "executing",
				TotalJobs:     2,
				CompletedJobs: 1,
			},
		}},
	}
	mock.TaskStepsResponse = &types.TaskStepsResponse{
		Steps: []types.TaskStepView{
			{ID: "step-1", TaskID: "task-42", Description: "quartzstepalpha", State: "completed", Sequence: 1},
			{ID: "step-2", TaskID: "task-42", Description: "quartzstepbeta", State: "running", Sequence: 2},
		},
	}
	model := NewTasksModel(mock)
	model.SetSize(100, 30)
	model.Update(TasksUpdateMsg{Tasks: mock.TasksExtendedResponse.Tasks})

	// Open the detail modal on the task row (existing Enter seam).
	model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !model.showingDetail || model.selectedTask == nil {
		t.Fatal("Enter did not open the task detail modal (precondition seam broken)")
	}

	// The detail must fetch fresh steps through the zero-caller seam.
	if mock.TaskStepsCalls == 0 {
		t.Errorf("detail modal opened without fetching fresh steps via ListTaskSteps (connection.go:43 has zero callers; W19 detail staleness)")
	}
	view := model.View()
	if !strings.Contains(view, "quartzstepalpha") || !strings.Contains(view, "quartzstepbeta") {
		t.Errorf("detail modal does not render the fetched steps (quartzstepalpha/quartzstepbeta missing):\n%s", view)
	}
	// Labeled steps counter (completed/total), not a bare number.
	if !strings.Contains(view, "1/2") {
		t.Errorf("detail modal shows no labeled steps counter 1/2 (completed/total):\n%s", view)
	}

	// Escape closes the modal.
	model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if model.showingDetail {
		t.Error("Escape did not close the task detail modal")
	}
}

// gateRecordingRPC wraps the chat mock and records every generic Call
// (method + params) so read-surface tests can assert the RPC round-trip.
type gateRecordingRPC struct {
	*MockChatRPCClient
	calls []gateRPCCall
}

type gateRPCCall struct {
	method string
	params map[string]any
}

func (g *gateRecordingRPC) Call(method string, params any) (json.RawMessage, error) {
	p, _ := params.(map[string]any)
	g.calls = append(g.calls, gateRPCCall{method: method, params: p})
	switch method {
	case "session.messages.get":
		// C4 shape: include_tool_calls joins persisted tool calls onto
		// each message (03-context/01 producer; consumed here).
		return json.RawMessage(`{"messages":[{"id":7,"role":"assistant","content":"quartz reply text","tool_calls":[{"id":"tc-1","name":"file_write","args_summary":"quartz.txt","result_summary":"wrote 11 bytes","at":"2026-11-19T10:00:00Z"}]}],"total":1}`), nil
	case "session.dispatch_trace":
		// C5 shape: rows carry intent/method/confidence (+ turn_id and
		// populated input_summary per the contract).
		return json.RawMessage(`{"entries":[{"session_id":"sess-1","turn_id":"turn-1","input_summary":"dispatch the quartz work","intent_type":"code","agent_id":"executor","confidence":0.95,"classifier_method":"llm","handler_case":"task_dispatch","task_id":"task-42"}],"count":1}`), nil
	default:
		return g.MockChatRPCClient.Call(method, params)
	}
}

// gateCallWithParam reports whether a recorded call used the method and
// carried the param key with the expected value.
func gateCallWithParam(calls []gateRPCCall, method, key string, want any) bool {
	for _, c := range calls {
		if c.method != method {
			continue
		}
		if got, ok := c.params[key]; ok && got == want {
			return true
		}
	}
	return false
}

// TestTraceExpand pins the C4 read surface: expanding a completed reply
// requests session.messages.get with include_tool_calls=true and renders
// the persisted tool-call rows under the reply.
func TestTraceExpand(t *testing.T) {
	mock := NewMockChatRPCClient()
	mock.SubmitAccepted = true
	mock.SubmitTurnID = "turn-1"
	mock.SubmitSessionID = "sess-1"
	rec := &gateRecordingRPC{MockChatRPCClient: mock}
	model := newGateChatModel(t, rec)
	gateCompletedTaskReply(t, model)

	model.SetFocus(FocusViewport)
	model.selectedMsgIdx = len(model.messages) - 1
	// 'e' is the existing expand key; the trace must ride the expansion.
	cmd := model.Update(tea.KeyPressMsg{Code: 'e'})
	drainBatchMsgs(cmd)

	if !gateCallWithParam(rec.calls, "session.messages.get", "include_tool_calls", true) {
		t.Errorf("trace expand never requested session.messages.get with include_tool_calls=true (C4 read surface unwired; calls=%v)", rec.calls)
	}
	view := model.View()
	if !strings.Contains(view, "file_write") || !strings.Contains(view, "quartz.txt") {
		t.Errorf("expanded reply shows no persisted tool-call trace (file_write / quartz.txt missing; C4):\n%s", view)
	}
}

// TestRoutePopover pins the C5 read surface: the why-this-route popover
// requests session.dispatch_trace for the reply's session and renders
// the intent/method/confidence rows.
func TestRoutePopover(t *testing.T) {
	mock := NewMockChatRPCClient()
	mock.SubmitAccepted = true
	mock.SubmitTurnID = "turn-1"
	mock.SubmitSessionID = "sess-1"
	rec := &gateRecordingRPC{MockChatRPCClient: mock}
	model := newGateChatModel(t, rec)
	gateCompletedTaskReply(t, model)

	model.SetFocus(FocusViewport)
	model.selectedMsgIdx = len(model.messages) - 1
	// 'w' = why-this-route on the selected reply (the leaf's popover key).
	cmd := model.Update(tea.KeyPressMsg{Code: 'w'})
	drainBatchMsgs(cmd)

	if !gateCallWithParam(rec.calls, "session.dispatch_trace", "session_id", "sess-1") {
		t.Errorf("route popover never requested session.dispatch_trace for sess-1 (C5 read surface unwired; calls=%v)", rec.calls)
	}
	view := model.View()
	for _, want := range []string{"task_dispatch", "llm", "0.95"} {
		if !strings.Contains(view, want) {
			t.Errorf("route popover missing evidence row fragment %q (intent/method/confidence from C5):\n%s", want, view)
		}
	}
}
