package models

// 05-navigation TEST-GATE T1 (leaf 00T-gate.md, matrix F12a; master C1).
//
// Pins that the TUI keeps the C1 terminal correlation fields
// (task_id / intent_type / handler_case / classified_by) on BOTH the
// turnTerminalMsg and the rendered ChatMessage, on the tracked-turn path
// AND the F19 early-buffer (late-terminal) path, and that the stale
// "no task_id" comments are gone (they are FALSE at HEAD: the daemon
// handler sets task_id, internal/agent/handler.go TurnTerminalEvent).
//
// RED today: DeliverTurnTerminal keeps only conv/reply/status/error/
// duration (chat_turn.go), turnTerminalMsg and ChatMessage carry no
// correlation fields. Field assertions use reflection so this file
// compiles at HEAD and turns GREEN with zero edits when the fields land
// (spec-first gate rule: never a compile-error RED, never t.Skip).

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// gateFieldString reads a string field by name through reflection.
// ok=false means the field does not exist yet (the RED reason).
func gateFieldString(v any, name string) (string, bool) {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return "", false
	}
	f := rv.FieldByName(name)
	if !f.IsValid() || f.Kind() != reflect.String {
		return "", false
	}
	return f.String(), true
}

// gateC1Payload is the frozen C1 terminal payload (master.md C1): the
// existing 13 keys, correlation subset asserted here.
func gateC1Payload(turnID, convID string) map[string]any {
	return map[string]any{
		"conversation_id": convID,
		"session_id":      "sess-1",
		"turn_id":         turnID,
		"task_id":         "task-42",
		"intent_type":     "code",
		"agent_id":        "executor",
		"handler_case":    "task_dispatch",
		"status":          "completed",
		"reply":           "quartz reply text",
		"duration_ms":     float64(42),
		"classified_by":   "llm",
		"model":           "fake-model",
		"error":           "",
	}
}

// gateSubmitTurn drives the real submit path (textarea -> doSendMessage ->
// submitTurnResultMsg -> Update) so the turn is tracked exactly as in
// production. The mock ack echoes an empty conversation id, which the
// daemon never does for an accepted submit (it resolves the session's
// conversation); normalize it to the model's conversation so the turn is
// same-session exactly as in production (F21 cross-session path is not
// what this gate pins).
func gateSubmitTurn(t *testing.T, model *ChatModel) {
	t.Helper()
	model.textarea.SetValue("dispatch the quartz work")
	cmd := model.doSendMessage()
	if cmd == nil {
		t.Fatal("doSendMessage returned no cmd")
	}
	msgs := drainBatchMsgs(cmd)
	sub, ok := extractMsg[submitTurnResultMsg](msgs)
	if !ok {
		t.Fatalf("expected submitTurnResultMsg, got %v", msgs)
	}
	if sub.Ack.ConversationID == "" {
		sub.Ack.ConversationID = model.conversationID
	}
	model.Update(sub)
}

// gateAssistantMessage returns the last assistant-role transcript message.
func gateAssistantMessage(t *testing.T, model *ChatModel) ChatMessage {
	t.Helper()
	for i := len(model.messages) - 1; i >= 0; i-- {
		if model.messages[i].Role == RoleAssistant {
			return model.messages[i]
		}
	}
	t.Fatalf("no assistant message rendered; transcript has %d messages", len(model.messages))
	return ChatMessage{}
}

// gateAssertC1Fields asserts the four C1 correlation fields on a value
// through reflection, reporting the missing-field RED reason precisely.
func gateAssertC1Fields(t *testing.T, v any, label string) {
	t.Helper()
	want := map[string]string{
		"TaskID":       "task-42",
		"IntentType":   "code",
		"HandlerCase":  "task_dispatch",
		"ClassifiedBy": "llm",
	}
	for name, wantVal := range want {
		got, ok := gateFieldString(v, name)
		if !ok {
			t.Errorf("%s: C1 field %s missing (dropped by the keep-list); master C1 requires it", label, name)
			continue
		}
		if got != wantVal {
			t.Errorf("%s: C1 field %s = %q, want %q", label, name, got, wantVal)
		}
	}
}

// TestTurnTerminalKeepsTaskID pins C1 on the tracked-turn terminal path:
// the resolved turnTerminalMsg AND the rendered ChatMessage keep
// task_id/intent_type/handler_case/classified_by.
func TestTurnTerminalKeepsTaskID(t *testing.T) {
	model, _ := newAsyncTestChatModel(t)
	gateSubmitTurn(t, model)

	model.DeliverTurnTerminal("turn-1", gateC1Payload("turn-1", model.conversationID))

	pt := mustGet(t, model, "turn-1")
	pt.mu.Lock()
	res := pt.result
	pt.mu.Unlock()
	if res.Status != "completed" {
		t.Fatalf("turn not resolved: status %q", res.Status)
	}
	gateAssertC1Fields(t, res, "turnTerminalMsg")

	// Render the terminal through the real Update path and pin the
	// message model the chip/dedupe consumers read.
	model.Update(res)
	msg := gateAssistantMessage(t, model)
	if msg.Content != "quartz reply text" {
		t.Fatalf("reply not rendered: %q", msg.Content)
	}
	gateAssertC1Fields(t, msg, "ChatMessage")
}

// TestTurnTerminalKeepsTaskIDEarlyBuffer is the late-terminal regression
// pin (F19): a terminal that races the submit ack is buffered, consumed
// on registration, and must STILL carry the C1 correlation fields.
func TestTurnTerminalKeepsTaskIDEarlyBuffer(t *testing.T) {
	model, _ := newAsyncTestChatModel(t)

	// Terminal arrives BEFORE the ack registers the turn.
	model.DeliverTurnTerminal("turn-1", gateC1Payload("turn-1", model.conversationID))

	model.textarea.SetValue("dispatch the quartz work")
	cmd := model.doSendMessage()
	msgs := drainBatchMsgs(cmd)
	sub, ok := extractMsg[submitTurnResultMsg](msgs)
	if !ok {
		t.Fatalf("expected submitTurnResultMsg, got %v", msgs)
	}
	if sub.Ack.ConversationID == "" {
		sub.Ack.ConversationID = model.conversationID
	}
	model.Update(sub) // consumes the early buffer

	msg := gateAssistantMessage(t, model)
	if msg.Content != "quartz reply text" {
		t.Fatalf("early-terminal reply not rendered: %q", msg.Content)
	}
	gateAssertC1Fields(t, msg, "ChatMessage (early-buffer path)")
}

// TestTurnTerminalStaleCommentsRemoved pins the leaf T1 requirement that
// the FALSE "no task_id" comments are removed: the daemon terminal DOES
// carry task_id (internal/agent/handler.go sets it), so any comment
// claiming otherwise misleads every future dedupe/navigation change.
func TestTurnTerminalStaleCommentsRemoved(t *testing.T) {
	for _, path := range []string{"chat_turn.go", "../app.go"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if strings.Contains(string(data), "carries no task_id") {
			t.Errorf("%s still claims the terminal/ack 'carries no task_id' - false at HEAD (handler.go sets task_id); stale comment must be removed", path)
		}
	}
}

// TestTurnTerminalCopyWithDiscipline is the copyWith-equivalent state
// pin: resolving one turn must not orphan a concurrent pending turn, and
// the resolved bubble keeps its correlation (no fresh-state rebuild).
func TestTurnTerminalCopyWithDiscipline(t *testing.T) {
	model, _ := newAsyncTestChatModel(t)
	gateSubmitTurn(t, model)

	// Second concurrent turn (direct registration keeps the test focused
	// on per-turn resolution, same pattern as the leaf-04 suite).
	model.turns.register("turn-2", model.conversationID, model.sessionID)
	model.pendingTurns = append(model.pendingTurns, mustGet(t, model, "turn-2"))

	model.DeliverTurnTerminal("turn-1", gateC1Payload("turn-1", model.conversationID))
	pt := mustGet(t, model, "turn-1")
	pt.mu.Lock()
	res := pt.result
	pt.mu.Unlock()
	model.Update(res) // render the terminal (production await-cmd path)

	if !model.HasPendingTurns() {
		t.Error("resolving turn-1 orphaned the concurrent pending turn-2 (fresh-state rebuild regression)")
	}
	if _, still := model.turns.get("turn-2"); !still {
		t.Error("turn-2 dropped from the router when turn-1 resolved")
	}
	msg := gateAssistantMessage(t, model)
	gateAssertC1Fields(t, msg, "ChatMessage (concurrent resolution)")
}
