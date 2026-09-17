package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newTestChatHandlerWithBus builds a ChatHandler wired to a real message bus
// (mirrors the wiring pattern in TestHandleRequestBroadcastsMessageReceived).
func newTestChatHandlerWithBus(t *testing.T) *ChatHandler {
	t.Helper()
	return NewChatHandler(nil, nil, bus.New(nil, slogDiscardLogger()), slogDiscardLogger())
}

// waitTurnTerminal blocks until one turn.terminal event arrives on sub.
func waitTurnTerminal(t *testing.T, sub *bus.Subscriber) TurnTerminalEvent {
	t.Helper()
	select {
	case msg := <-sub.Channel:
		var ev TurnTerminalEvent
		if err := json.Unmarshal(msg.Payload, &ev); err != nil {
			t.Fatalf("unmarshal turn.terminal payload: %v", err)
		}
		return ev
	case <-time.After(10 * time.Second):
		t.Fatal("no turn.terminal event received")
		return TurnTerminalEvent{}
	}
}

// assertNoSecondTurnTerminal proves the exactly-once emission contract.
func assertNoSecondTurnTerminal(t *testing.T, sub *bus.Subscriber) {
	t.Helper()
	select {
	case msg := <-sub.Channel:
		t.Fatalf("unexpected second turn.terminal event: %s", msg.Payload)
	case <-time.After(300 * time.Millisecond):
	}
}

// asyncTurnTestDispatcher wires a dispatcher whose classifier server answers
// a deterministic quickplan verdict, so ClassifyAndRoute produces an intent
// with RequiresPlanning and a created task (the handler's async gate opens).
// Same proven wiring as TestClassifyAndRoute_LLMQuickPlanOpensAsyncGate.
func asyncTurnTestDispatcher(t *testing.T) (*Dispatcher, *task.Store) {
	t.Helper()
	logger := slogDiscardLogger()
	reg, err := task.NewRegistry(filepath.Join(t.TempDir(), "tasks.db"), bus.New(nil, nil), logger)
	if err != nil {
		t.Fatalf("task registry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })

	d := NewDispatcher(DispatcherConfig{
		Registry:         NewAgentRegistry(RegistryConfig{Logger: logger}),
		TaskStore:        reg.Store(),
		TaskRegistry:     reg,
		ClassifierClient: newPromptAwareClassifierServer(t),
		Logger:           logger,
	})
	return d, reg.Store()
}

// asyncTurnTestInput is long enough to bypass the short/simple guard and
// carries no compound-signal phrase, so the canned quickplan verdict is used.
const asyncTurnTestInput = "knock out the whole roadmap of remaining migration work without checking in with me at every step"

// ---------------------------------------------------------------------------
// Task 1: struct + helper
// ---------------------------------------------------------------------------

// TestTurnTerminalEvent_FrozenPayloadContract pins the CLOSED 13-field wire
// schema: with every field populated the marshaled payload carries exactly
// 13 keys under the exact contract names.
func TestTurnTerminalEvent_FrozenPayloadContract(t *testing.T) {
	ev := TurnTerminalEvent{
		ConversationID: "conv-1",
		SessionID:      "sess-1",
		TurnID:         "turn-1",
		TaskID:         "task-1",
		IntentType:     "code",
		AgentID:        "coder",
		HandlerCase:    "sync_dispatch",
		Status:         "failed",
		Reply:          "the task failed",
		DurationMS:     1500,
		ClassifiedBy:   "llm",
		Model:          "local/lfm-8b-q4",
		Error:          "boom",
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	wantKeys := []string{
		"conversation_id", "session_id", "turn_id", "task_id",
		"intent_type", "agent_id", "handler_case", "status", "reply",
		"duration_ms", "classified_by", "model", "error",
	}
	if len(got) != 13 {
		t.Errorf("payload has %d keys, want 13 (frozen contract)", len(got))
	}
	for _, key := range wantKeys {
		if _, ok := got[key]; !ok {
			t.Errorf("payload missing key %q", key)
		}
	}
	if len(got) != len(wantKeys) {
		t.Errorf("key set mismatch: got %d, want %d", len(got), len(wantKeys))
	}
}

func TestPublishTurnTerminal_PublishesFrozenPayload(t *testing.T) {
	h := newTestChatHandlerWithBus(t)

	sub := h.bus.Subscribe("test-turn-terminal", "turn.terminal")
	defer h.bus.Unsubscribe(sub)

	h.publishTurnTerminal(TurnTerminalEvent{
		ConversationID: "conv-1",
		TurnID:         "turn-1",
		TaskID:         "task-1",
		IntentType:     "code",
		AgentID:        "coder",
		HandlerCase:    "sync_dispatch",
		Status:         "completed",
		Reply:          "done",
		DurationMS:     1500,
		ClassifiedBy:   "llm",
		Model:          "local/lfm-8b-q4",
	})

	select {
	case msg := <-sub.Channel:
		if msg.Type != models.MessageTypeEvent {
			t.Errorf("type = %v, want MessageTypeEvent", msg.Type)
		}
		if msg.Source != "chat-handler" {
			t.Errorf("source = %q, want chat-handler", msg.Source)
		}
		var got map[string]any
		if err := json.Unmarshal(msg.Payload, &got); err != nil {
			t.Fatalf("payload unmarshal: %v", err)
		}
		for _, key := range []string{"conversation_id", "turn_id", "task_id",
			"intent_type", "agent_id", "handler_case", "status", "reply",
			"duration_ms", "classified_by", "model"} {
			if _, ok := got[key]; !ok {
				t.Errorf("payload missing key %q", key)
			}
		}
		// Empty omitempty fields must stay absent: the payload carries
		// exactly the populated subset of the frozen 13-field schema.
		if _, ok := got["session_id"]; ok {
			t.Error("empty session_id must be omitted")
		}
		if _, ok := got["error"]; ok {
			t.Error("empty error must be omitted (status is completed)")
		}
		if got["status"] != "completed" {
			t.Errorf("status = %v, want completed", got["status"])
		}
		assertNoSecondTurnTerminal(t, sub)
	case <-time.After(2 * time.Second):
		t.Fatal("no turn.terminal event received")
	}
}

func TestPublishTurnTerminal_NilBusNoOp(t *testing.T) {
	h := &ChatHandler{logger: testLogger()} // bus nil
	h.publishTurnTerminal(TurnTerminalEvent{ConversationID: "c", TurnID: "t", Status: "completed"})
	// must not panic
}

// ---------------------------------------------------------------------------
// Task 2: emission on handleRequest terminal paths
// ---------------------------------------------------------------------------

// TestHandleRequest_EmitsTurnTerminalOnInlineReply drives the classified
// inline path (analyzer ambiguity -> clarification reply) through a real
// dispatcher; the turn must emit exactly one completed terminal event.
func TestHandleRequest_EmitsTurnTerminalOnInlineReply(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	_, _, primaryCfg, secondaryCfg := failoverTestServers(
		t, emptyContentResponse(), ambiguousAnalysisResponse())
	resolver := newFailoverResolver(t, primaryCfg, secondaryCfg)
	dispatcher := NewDispatcher(DispatcherConfig{
		ClassifierClient:      llm.NewClient(primaryCfg),
		ClassifierModel:       "primary",
		ClassifierModelConfig: primaryCfg,
		Resolver:              resolver,
	})
	h := NewChatHandler(nil, dispatcher, msgBus, slogDiscardLogger())

	sub := msgBus.Subscribe("test-turn-terminal", "turn.terminal")
	defer h.bus.Unsubscribe(sub)

	payload, _ := json.Marshal(ChatRequest{
		Message:        "what are your capabilities",
		ConversationID: "conv-it",
	})
	reqMsg := &models.BusMessage{
		ID:        "m1",
		Type:      models.MessageTypeRequest,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	h.handleRequest(context.Background(), reqMsg)

	ev := waitTurnTerminal(t, sub)
	if ev.ConversationID != "conv-it" || ev.Status != "completed" {
		t.Fatalf("event = %+v", ev)
	}
	if ev.TurnID == "" {
		t.Error("turn_id must be set")
	}
	if ev.DurationMS < 0 {
		t.Errorf("duration_ms = %d, want >= 0", ev.DurationMS)
	}
	if ev.Reply == "" {
		t.Error("reply must carry the final user-facing reply text")
	}
	if ev.HandlerCase == "" {
		t.Error("handler_case must be set")
	}
	assertNoSecondTurnTerminal(t, sub)
}

// TestHandleRequest_EmitsTurnTerminalOnError drives the direct-mode error
// path (no LLM client wired -> the loop fails the turn); the event must be
// a single failed terminal event with a user-facing reply.
func TestHandleRequest_EmitsTurnTerminalOnError(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	loop := NewAgentLoop("test-session", "/tmp") // no LLM client: turn errors
	h := NewChatHandler(loop, nil, msgBus, slogDiscardLogger())

	sub := msgBus.Subscribe("test-turn-terminal", "turn.terminal")
	defer h.bus.Unsubscribe(sub)

	payload, _ := json.Marshal(ChatRequest{
		Message:        "hello from claude",
		ConversationID: "conv-err",
	})
	reqMsg := &models.BusMessage{
		ID:        "m2",
		Type:      models.MessageTypeRequest,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	h.handleRequest(context.Background(), reqMsg)

	ev := waitTurnTerminal(t, sub)
	if ev.Status != "failed" {
		t.Errorf("status = %q, want failed", ev.Status)
	}
	if ev.Error == "" {
		t.Error("error must be non-empty iff status is failed")
	}
	if ev.Reply == "" {
		t.Error("reply must document the failure for the user")
	}
	if ev.ConversationID != "conv-err" {
		t.Errorf("conversation_id = %q, want conv-err", ev.ConversationID)
	}
	if ev.TurnID == "" {
		t.Error("turn_id must be set")
	}
	assertNoSecondTurnTerminal(t, sub)
}

// TestHandleRequest_EmitsTurnTerminalOnAsyncAck drives the async-dispatch
// ack path: the ack means "accepted, work continuing elsewhere", so the
// event carries status PARKED with task_id set — clients skip it and keep
// waiting for the task_completed_relay, which re-broadcasts the real
// result under the SAME turn id (relay fix, bench gate 2026-09-16).
func TestHandleRequest_EmitsTurnTerminalOnAsyncAck(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	d, _ := asyncTurnTestDispatcher(t)
	h := NewChatHandler(nil, d, msgBus, slogDiscardLogger())

	sub := msgBus.Subscribe("test-turn-terminal", "turn.terminal")
	defer h.bus.Unsubscribe(sub)

	payload, _ := json.Marshal(ChatRequest{
		Message:        asyncTurnTestInput,
		ConversationID: "conv-async",
	})
	reqMsg := &models.BusMessage{
		ID:        "m3",
		Type:      models.MessageTypeRequest,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	h.handleRequest(context.Background(), reqMsg)

	ev := waitTurnTerminal(t, sub)
	if ev.Status != "parked" {
		t.Errorf("status = %q, want parked (the ack is accepted-not-finished; completed made clients grade the ack as the result)", ev.Status)
	}
	if ev.TaskID == "" {
		t.Error("task_id must be set on the async-ack path")
	}
	if ev.HandlerCase != "async_dispatch" {
		t.Errorf("handler_case = %q, want async_dispatch", ev.HandlerCase)
	}
	if ev.Reply == "" {
		t.Error("reply must carry the ack text")
	}
	if ev.ConversationID != "conv-async" {
		t.Errorf("conversation_id = %q, want conv-async", ev.ConversationID)
	}
	assertNoSecondTurnTerminal(t, sub)
}

// TestHandleRequest_EmitsTurnTerminalOnSyncTimeout drives the sync-dispatch
// path with a tiny wait ceiling: the task stays non-terminal, so the turn's
// terminal event must carry status "timeout" (detected via task STATE, not
// by matching the stub reply text).
func TestHandleRequest_EmitsTurnTerminalOnSyncTimeout(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	d, store := asyncTurnTestDispatcher(t)
	h := NewChatHandler(nil, d, msgBus, slogDiscardLogger())
	h.SetTaskStore(store)
	h.SetSyncMode(true)
	h.syncWaitCeiling = 200 * time.Millisecond

	sub := msgBus.Subscribe("test-turn-terminal", "turn.terminal")
	defer h.bus.Unsubscribe(sub)

	payload, _ := json.Marshal(ChatRequest{
		Message:        asyncTurnTestInput,
		ConversationID: "conv-timeout",
	})
	reqMsg := &models.BusMessage{
		ID:        "m4",
		Type:      models.MessageTypeRequest,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	h.handleRequest(context.Background(), reqMsg)

	ev := waitTurnTerminal(t, sub)
	if ev.Status != "timeout" {
		t.Errorf("status = %q, want timeout (task still non-terminal after the ceiling)", ev.Status)
	}
	if ev.TaskID == "" {
		t.Error("task_id must be set on the sync-dispatch path")
	}
	if ev.HandlerCase != "sync_dispatch" {
		t.Errorf("handler_case = %q, want sync_dispatch", ev.HandlerCase)
	}
	if ev.Reply == "" {
		t.Error("reply must carry what the caller received (the stub)")
	}
	assertNoSecondTurnTerminal(t, sub)
}

// TestHandleRequest_EmitsTurnTerminalOnSyncCompleted drives the sync path to
// a real terminal state: a subscriber on orchestrator.plan flips the task to
// completed in the shared store while the handler waits, so the terminal
// event must carry status "completed".
func TestHandleRequest_EmitsTurnTerminalOnSyncCompleted(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	d, store := asyncTurnTestDispatcher(t)
	h := NewChatHandler(nil, d, msgBus, slogDiscardLogger())
	h.SetTaskStore(store)
	h.SetSyncMode(true)
	h.syncWaitCeiling = 15 * time.Second

	// Flip the task to completed as soon as the plan request lands, so
	// waitForTaskCompletion's poll observes a terminal task state.
	planSub := msgBus.Subscribe("test-plan", "orchestrator.plan")
	defer h.bus.Unsubscribe(planSub)
	go func() {
		for msg := range planSub.Channel {
			var req PlanRequest
			if err := json.Unmarshal(msg.Payload, &req); err != nil {
				continue
			}
			if tk, err := store.GetByID(req.TaskID); err == nil && tk != nil {
				tk.SetState(task.StateCompleted)
				if err := store.Update(tk); err != nil {
					t.Errorf("flip task completed: %v", err)
				}
			}
			return
		}
	}()

	sub := msgBus.Subscribe("test-turn-terminal", "turn.terminal")
	defer h.bus.Unsubscribe(sub)

	payload, _ := json.Marshal(ChatRequest{
		Message:        asyncTurnTestInput,
		ConversationID: "conv-sync-ok",
	})
	reqMsg := &models.BusMessage{
		ID:        "m5",
		Type:      models.MessageTypeRequest,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	h.handleRequest(context.Background(), reqMsg)

	ev := waitTurnTerminal(t, sub)
	if ev.Status != "completed" {
		t.Errorf("status = %q, want completed (task reached terminal state)", ev.Status)
	}
	if ev.HandlerCase != "sync_dispatch" {
		t.Errorf("handler_case = %q, want sync_dispatch", ev.HandlerCase)
	}
	if ev.TaskID == "" {
		t.Error("task_id must be set on the sync-dispatch path")
	}
	assertNoSecondTurnTerminal(t, sub)
}

// ---------------------------------------------------------------------------
// Task 3: task.completed / task.failed relay
// ---------------------------------------------------------------------------

func TestHandleTaskCompleted_EmitsTerminalEventWithResult(t *testing.T) {
	h := newTestChatHandlerWithBus(t)

	// Live worker carries the session -> conversation mapping.
	h.registerWorker(&Worker{
		ID:             "w-1",
		ConversationID: "conv-async",
		SessionID:      "sess-1",
	})

	sub := h.bus.Subscribe("test-turn-terminal", "turn.terminal")
	defer h.bus.Unsubscribe(sub)

	payload, _ := json.Marshal(map[string]any{
		"task_id":         "task-relay-1",
		"name":            "write the report",
		"completed_jobs":  2,
		"total_jobs":      2,
		"linked_sessions": []string{"sess-1"},
		"result":          "the report is at /tmp/report.md",
	})
	msg := &models.BusMessage{
		ID:        "tc-1",
		Type:      models.MessageTypeEvent,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	h.handleTaskCompleted(msg)

	ev := waitTurnTerminal(t, sub)
	if ev.Status != "completed" {
		t.Errorf("status = %q, want completed", ev.Status)
	}
	if ev.TaskID != "task-relay-1" {
		t.Errorf("task_id = %q, want task-relay-1", ev.TaskID)
	}
	if ev.HandlerCase != "task_completed_relay" {
		t.Errorf("handler_case = %q, want task_completed_relay", ev.HandlerCase)
	}
	if ev.ConversationID != "conv-async" {
		t.Errorf("conversation_id = %q, want conv-async (worker-map correlation)", ev.ConversationID)
	}
	if want := "the report is at /tmp/report.md"; !contains(ev.Reply, want) {
		t.Errorf("reply = %q, want it to contain the result summary %q", ev.Reply, want)
	}
	assertNoSecondTurnTerminal(t, sub)
}

// TestHandleTaskCompleted_HeadlessFallsBackToTaskSentinel pins the
// correlation fallback: no linked-session worker mapping -> the event's
// conversation_id becomes "task:<taskID>" so consumers can key on task_id.
func TestHandleTaskCompleted_HeadlessFallsBackToTaskSentinel(t *testing.T) {
	h := newTestChatHandlerWithBus(t)

	sub := h.bus.Subscribe("test-turn-terminal", "turn.terminal")
	defer h.bus.Unsubscribe(sub)

	payload, _ := json.Marshal(map[string]any{
		"task_id":         "task-headless-1",
		"name":            "headless work",
		"completed_jobs":  1,
		"total_jobs":      1,
		"linked_sessions": []string{},
	})
	msg := &models.BusMessage{
		ID:        "tc-2",
		Type:      models.MessageTypeEvent,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	h.handleTaskCompleted(msg)

	ev := waitTurnTerminal(t, sub)
	if ev.ConversationID != "task:task-headless-1" {
		t.Errorf("conversation_id = %q, want task:task-headless-1", ev.ConversationID)
	}
	if ev.Status != "completed" {
		t.Errorf("status = %q, want completed", ev.Status)
	}
}

func TestHandleTaskFailed_EmitsFailedTerminalEvent(t *testing.T) {
	h := newTestChatHandlerWithBus(t)

	h.registerWorker(&Worker{
		ID:             "w-2",
		ConversationID: "conv-failed",
		SessionID:      "sess-2",
	})

	sub := h.bus.Subscribe("test-turn-terminal", "turn.terminal")
	defer h.bus.Unsubscribe(sub)

	payload, _ := json.Marshal(map[string]any{
		"task_id":         "task-relay-2",
		"name":            "broken work",
		"failed_jobs":     1,
		"completed_jobs":  0,
		"total_jobs":      2,
		"linked_sessions": []string{"sess-2"},
		"error":           "the build exploded",
	})
	msg := &models.BusMessage{
		ID:        "tf-1",
		Type:      models.MessageTypeEvent,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	h.handleTaskFailed(msg)

	ev := waitTurnTerminal(t, sub)
	if ev.Status != "failed" {
		t.Errorf("status = %q, want failed", ev.Status)
	}
	if ev.Error == "" {
		t.Error("error must be non-empty iff status is failed")
	}
	if ev.TaskID != "task-relay-2" {
		t.Errorf("task_id = %q, want task-relay-2", ev.TaskID)
	}
	if ev.HandlerCase != "task_failed_relay" {
		t.Errorf("handler_case = %q, want task_failed_relay", ev.HandlerCase)
	}
	if ev.ConversationID != "conv-failed" {
		t.Errorf("conversation_id = %q, want conv-failed (worker-map correlation)", ev.ConversationID)
	}
}

// ---------------------------------------------------------------------------
// Async-turn registry wiring (leaf 03 of async-turn-migration)
// ---------------------------------------------------------------------------

// TestHandleRequest_TurnIDTrackedAndCompleted drives a submitted turn
// (ChatRequest.TurnID set) through the error path: the registry must contain
// the turn while it runs and have it removed once the turn.terminal fires.
func TestHandleRequest_TurnIDTrackedAndCompleted(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	loop := NewAgentLoop("test-session", "/tmp") // no LLM client: turn errors out fast
	h := NewChatHandler(loop, nil, msgBus, slogDiscardLogger())
	reg := NewTurnRegistry()
	h.SetTurnRegistry(reg)

	sub := msgBus.Subscribe("test-turn-terminal", "turn.terminal")
	defer h.bus.Unsubscribe(sub)

	payload, _ := json.Marshal(ChatRequest{
		Message:        "tracked submit",
		ConversationID: "conv-tracked",
		TurnID:         "turn-submit-1",
	})
	reqMsg := &models.BusMessage{
		ID:        "m-tracked-1",
		Type:      models.MessageTypeRequest,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	h.handleRequest(context.Background(), reqMsg)

	ev := waitTurnTerminal(t, sub)
	if ev.TurnID != "turn-submit-1" {
		t.Errorf("terminal event turn_id = %q, want turn-submit-1 (submitted id preserved)", ev.TurnID)
	}
	if ev.ConversationID != "conv-tracked" {
		t.Errorf("conversation_id = %q, want conv-tracked", ev.ConversationID)
	}
	if ev.Status != "failed" {
		t.Errorf("status = %q, want failed (no-LLM error path)", ev.Status)
	}

	// Complete ran adjacent to the terminal emission: the turn is gone.
	if _, ok := reg.turns["turn-submit-1"]; ok {
		t.Error("turn-submit-1 still tracked after terminal event (Complete not called)")
	}
}

// TestPublishWorkerEvent_TouchesTrackedTurn proves the per-worker-event
// Touch: register the turn, fire a worker event carrying the turn id in
// Worker.RequestID, and observe LastProgressAt advance under the fake clock.
func TestPublishWorkerEvent_TouchesTrackedTurn(t *testing.T) {
	h := newTestChatHandlerWithBus(t)
	reg := NewTurnRegistry()
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	fc := newFakeClock(start)
	reg.now = fc.Now
	h.SetTurnRegistry(reg)

	reg.Register("turn-w1", "conv-1")
	h.registerWorker(&Worker{
		ID:             "w-touch",
		ConversationID: "conv-1",
		RequestID:      "m-1",
		TurnID:         "turn-w1", // submit-time turn id rides the worker
	})

	fc.Advance(3 * time.Minute)
	h.publishWorkerEvent("chat.worker.state_changed", &Worker{
		ID:             "w-touch",
		ConversationID: "conv-1",
		RequestID:      "m-1",
		TurnID:         "turn-w1",
		State:          "executing_tool",
	})

	rec, ok := reg.turns["turn-w1"]
	if !ok {
		t.Fatal("tracked turn vanished on worker event")
	}
	if !rec.LastProgressAt.Equal(start.Add(3 * time.Minute)) {
		t.Errorf("last_progress_at = %v, want %v (Touch advanced by the worker event)", rec.LastProgressAt, start.Add(3*time.Minute))
	}
}

// TestHandleRequest_LegacyTurnUntracked pins the legacy-path contract:
// ChatRequest without TurnID (the `chat` RPC) never creates registry state.
func TestHandleRequest_LegacyTurnUntracked(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	loop := NewAgentLoop("test-session", "/tmp")
	h := NewChatHandler(loop, nil, msgBus, slogDiscardLogger())
	reg := NewTurnRegistry()
	h.SetTurnRegistry(reg)

	sub := msgBus.Subscribe("test-turn-terminal", "turn.terminal")
	defer h.bus.Unsubscribe(sub)

	payload, _ := json.Marshal(ChatRequest{
		Message:        "legacy blocking turn",
		ConversationID: "conv-legacy",
		// TurnID deliberately empty
	})
	reqMsg := &models.BusMessage{
		ID:        "m-legacy-1",
		Type:      models.MessageTypeRequest,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	h.handleRequest(context.Background(), reqMsg)

	ev := waitTurnTerminal(t, sub)
	if ev.TurnID == "" {
		t.Error("terminal event still needs a synthetic turn_id for the wire contract")
	}
	if len(reg.turns) != 0 {
		t.Errorf("legacy turn left registry state: %+v", reg.turns)
	}
}

// TestSetTurnRegistry_NilAndTypedNil pins the setter convention: both a real
// nil and a typed nil *TurnRegistry in an interface must be rejected, and a
// nil receiver must not panic.
func TestSetTurnRegistry_NilAndTypedNil(t *testing.T) {
	h := newTestChatHandlerWithBus(t)

	h.SetTurnRegistry(nil) // plain nil: ignored, no panic
	if h.turnRegistry != nil {
		t.Error("SetTurnRegistry(nil) must not install a registry")
	}

	// Typed-nil through an interface-shaped call site (project invariant).
	var typedNil *TurnRegistry
	var regAny any = typedNil
	if reg, ok := regAny.(*TurnRegistry); !ok || reg != nil {
		t.Fatalf("test setup: typed-nil assertion failed")
	} else {
		h.SetTurnRegistry(reg) // typed nil pointer: ignored
	}
	if h.turnRegistry != nil {
		t.Error("SetTurnRegistry(typed-nil) must not install a registry")
	}

	var nilHandler *ChatHandler
	nilHandler.SetTurnRegistry(NewTurnRegistry()) // nil receiver: no panic
}

// TestTouchTurn_NilRegistryNoOp proves touchTurn/completeTurn are safe when
// no registry was wired (the registry is optional infrastructure).
func TestTouchTurn_NilRegistryNoOp(t *testing.T) {
	h := newTestChatHandlerWithBus(t)
	h.touchTurn("turn-x")    // registry nil: no panic
	h.completeTurn("turn-x") // registry nil: no panic

	reg := NewTurnRegistry()
	h.SetTurnRegistry(reg)
	h.touchTurn("")    // empty id: no-op
	h.completeTurn("") // empty id: no-op
	if len(reg.turns) != 0 {
		t.Errorf("empty-id helpers mutated the registry: %+v", reg.turns)
	}
}
