package agent

// 01-safety TEST GATE T8 (00T-gate.md): failure terminalization (F5).
//
// Five silent failure seams pinned:
//   - OnJobFailed's PAIR branch returns before any task.failed publish;
//     finalizeTask publishes nothing (pair_manager.go) -- the user's turn
//     never terminalizes.
//   - orchestrator.handlePlanRequest logs Plan() errors and returns --
//     no task.failed, no terminal.
//   - handler park detection requires err!=nil: a fresh throttle park
//     returns ("", nil) from RunOnceWithParts, so the turn terminalizes
//     "completed" with an EMPTY reply instead of "parked".
//   - handleRequest has no recover(): a panic mid-turn kills the handler
//     goroutine and the awaiting client never gets a terminal.
//
// RED today: each seam exhibits the documented silent/wrong behavior.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// gateWaitEvent waits up to 3s for one event on sub; nil on timeout.
func gateWaitEvent(sub *bus.Subscriber) *models.BusMessage {
	select {
	case msg, ok := <-sub.Channel:
		if !ok {
			return nil
		}
		return msg
	case <-time.After(3 * time.Second):
		return nil
	}
}

// TestPairFailureTerminal pins the pair branch: a pair-managed step job
// failure must publish task.failed (with the job error) and terminalize
// the parent task -- today the branch marks the step/session failed and
// returns SILENTLY.
func TestPairFailureTerminal(t *testing.T) {
	ts, taskStore, msgBus, _, cleanup := newFilterTestScheduler(t, nil)
	defer cleanup()

	pm := NewPairManager(PairManagerConfig{
		TaskStore: taskStore,
		StepStore: taskStore.StepStore(),
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})
	ts.pairManager = pm

	parent := newFilterTestTask(t, ts, "pair-failure-gate", 1)
	step := newFilterTestStep(t, ts, parent.ID, "job-pair-fail", "code", "")
	session := pm.CreateSession(parent.ID, "implement the feature", "coder", "planner", 3)
	session.AddStepID(step.ID)

	failedSub := msgBus.Subscribe("gate-pair-failed", "task.failed")
	defer msgBus.Unsubscribe(failedSub)
	terminalSub := msgBus.Subscribe("gate-pair-terminal", "turn.terminal")
	defer msgBus.Unsubscribe(terminalSub)

	// Wire the failure relay exactly as the daemon does: ChatHandler
	// subscribes task.failed and relays turn.terminal.
	h := NewChatHandler(nil, nil, msgBus, slogDiscardLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := h.Start(ctx); err != nil {
		t.Fatalf("handler start: %v", err)
	}
	defer func() {
		if serr := h.Stop(context.Background()); serr != nil {
			t.Logf("handler stop: %v", serr)
		}
	}()

	jobErr := "actor tool execution failed: file_write anchor missing"
	if err := ts.OnJobFailed(ctx, "job-pair-fail", jobErr); err != nil {
		t.Fatalf("OnJobFailed: %v", err)
	}

	// The step and session are marked failed today; the TASK terminal
	// and the user-facing events are the gap.
	msg := gateWaitEvent(failedSub)
	if msg == nil {
		t.Fatal("CURRENT DEFECT: pair-managed step failure published NO task.failed; the parent task and the user's turn never terminalize (tactical.go pair branch returns early)")
	}
	var payload struct {
		TaskID string `json:"task_id"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("decode task.failed: %v", err)
	}
	if payload.TaskID != parent.ID {
		t.Errorf("task.failed task_id = %q, want %q", payload.TaskID, parent.ID)
	}
	if !strings.Contains(payload.Error, "anchor missing") {
		t.Errorf("task.failed error = %q, want the job error text", payload.Error)
	}

	got, err := taskStore.GetByID(parent.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.State != task.StateFailed {
		t.Errorf("parent task state = %q, want failed after the pair step failure", got.State)
	}

	term := gateWaitEvent(terminalSub)
	if term == nil {
		t.Fatal("no turn.terminal relayed for the pair failure; the user sees nothing")
	}
	var ev TurnTerminalEvent
	if err := json.Unmarshal(term.Payload, &ev); err != nil {
		t.Fatalf("decode terminal: %v", err)
	}
	if ev.Status != "failed" {
		t.Errorf("terminal status = %q, want failed", ev.Status)
	}
}

// TestPairExhaustionTerminal pins finalizeTask: an exhausted pair session
// must publish task.failed when it flips the task to StateFailed (today
// it updates the store and publishes NOTHING).
func TestPairExhaustionTerminal(t *testing.T) {
	taskStore, err := task.NewStore(t.TempDir()+"/tasks.db", nil)
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	defer taskStore.Close()
	msgBus := bus.New(nil, slogDiscardLogger())

	pm := NewPairManager(PairManagerConfig{
		TaskStore: taskStore,
		StepStore: taskStore.StepStore(),
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})
	tk := task.NewTask("pair-exhaustion-gate", "rounds run out")
	tk.SetState(task.StateExecuting)
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	sess := pm.CreateSession(tk.ID, "spec", "coder", "planner", 1)

	failedSub := msgBus.Subscribe("gate-pair-exhaust", "task.failed")
	defer msgBus.Unsubscribe(failedSub)

	pm.finalizeTask(context.Background(), sess, false)

	got, err := taskStore.GetByID(tk.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.State != task.StateFailed {
		t.Fatalf("finalizeTask(success=false) left state %q, want failed", got.State)
	}
	if msg := gateWaitEvent(failedSub); msg == nil {
		t.Fatal("CURRENT DEFECT: finalizeTask published NO task.failed on exhaustion; the failure is store-only and the user's turn hangs (pair_manager.go finalizeTask)")
	}
}

// TestPlanRequestErrorTerminal pins orchestrator.handlePlanRequest: a
// Plan() error must fail the task loudly (task.failed -> terminal), not
// log-and-return.
func TestPlanRequestErrorTerminal(t *testing.T) {
	taskStore, err := task.NewStore(t.TempDir()+"/tasks.db", nil)
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	defer taskStore.Close()
	msgBus := bus.New(nil, slogDiscardLogger())

	// No planner agent spec registered: Plan() fails deterministically
	// ("planner agent not available") on the decomposition path.
	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore:      taskStore,
		StepStore:      taskStore.StepStore(),
		Bus:            msgBus,
		Logger:         slogDiscardLogger(),
		TemplateLoader: newPlannerTemplateLoader(t.TempDir()),
	})
	o := NewOrchestrator(OrchestratorDeps{
		Strategic: sp,
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})

	tk := task.NewTask("plan-error-gate", "a request long enough to route through the planning decomposition path rather than the direct fallback lane")
	tk.SetState(task.StatePlanning)
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	failedSub := msgBus.Subscribe("gate-plan-error", "task.failed")
	defer msgBus.Unsubscribe(failedSub)

	payload, err := json.Marshal(PlanRequest{
		TaskID:    tk.ID,
		SessionID: "sess-plan-error",
		Input:     tk.Description,
		Intent:    string(IntentCode),
		Mode:      "plan",
	})
	if err != nil {
		t.Fatalf("marshal plan request: %v", err)
	}
	o.handlePlanRequest(context.Background(), &models.BusMessage{
		Type:    models.MessageTypeRequest,
		Topic:   "orchestrator.plan",
		Payload: payload,
	})

	msg := gateWaitEvent(failedSub)
	if msg == nil {
		t.Fatal("CURRENT DEFECT: handlePlanRequest logged the Plan() error and returned; no task.failed published, the task sits in planning and the user's turn never terminalizes (orchestrator.go)")
	}
	var body struct {
		TaskID string `json:"task_id"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(msg.Payload, &body); err != nil {
		t.Fatalf("decode task.failed: %v", err)
	}
	if body.TaskID != tk.ID {
		t.Errorf("task.failed task_id = %q, want %q", body.TaskID, tk.ID)
	}
	if body.Error == "" {
		t.Error("task.failed must carry the actionable planning error text")
	}
	got, gerr := taskStore.GetByID(tk.ID)
	if gerr != nil {
		t.Fatalf("get task: %v", gerr)
	}
	if got.State != task.StateFailed {
		t.Errorf("task state after plan error = %q, want failed", got.State)
	}
}

// gatePanicSaver panics inside the post-turn persistence seam -- a real
// fault-injection point on the handleRequest path (persistExchange calls
// the saver synchronously after the reply is produced).
type gatePanicSaver struct{}

func (gatePanicSaver) SaveMessages(sessionID string, messages []session.Message) error {
	panic("gate: injected persistence panic")
}

// TestPanicRecovery pins handleRequest's missing recover(): a panic
// mid-turn must be recovered and terminalized as failed (daemon alive,
// awaiting client resolved), never crash the handler goroutine.
func TestPanicRecovery(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	loop := NewAgentLoop("sess-panic-gate", t.TempDir(),
		WithLLMChatter(&stubChatter{resp: &llm.Response{Content: "the reply before the panic"}}))
	h := NewChatHandler(loop, nil, msgBus, slogDiscardLogger())
	h.SetMessageSaver(gatePanicSaver{})

	sub := msgBus.Subscribe("gate-panic-terminal", "turn.terminal")
	defer msgBus.Unsubscribe(sub)

	reqPayload, err := json.Marshal(ChatRequest{
		Message:        "do the thing",
		ConversationID: "conv-panic-gate",
		SessionID:      "sess-panic-gate",
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	msg := &models.BusMessage{
		ID:        "m-panic-gate",
		Type:      models.MessageTypeRequest,
		Topic:     "chat.request",
		Payload:   reqPayload,
		Timestamp: time.Now().UTC(),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.handleRequest(context.Background(), msg)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("handleRequest never returned after the injected panic")
	}

	// A recovered panic must emit a FAILED terminal for the turn.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case m := <-sub.Channel:
			var ev TurnTerminalEvent
			if uerr := json.Unmarshal(m.Payload, &ev); uerr != nil {
				t.Fatalf("decode terminal: %v", uerr)
			}
			if ev.Status == "failed" && ev.Error != "" {
				return // recovered + terminalized honestly
			}
			t.Fatalf("terminal after panic = status %q error %q; want failed with the panic reason", ev.Status, ev.Error)
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
	t.Fatal("CURRENT DEFECT: no failed turn.terminal after the mid-turn panic; handleRequest has no recover() and the awaiting client never learns the turn died")
}

// TestPanicRecoveryKeepsHandlerAlive pins the daemon-alive half: after a
// recovered panic the handler still serves the NEXT request.
func TestPanicRecoveryKeepsHandlerAlive(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	loop := NewAgentLoop("sess-panic-alive", t.TempDir(),
		WithLLMChatter(&stubChatter{resp: &llm.Response{Content: "second turn reply"}}))
	h := NewChatHandler(loop, nil, msgBus, slogDiscardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := h.Start(ctx); err != nil {
		t.Fatalf("handler start: %v", err)
	}
	defer func() {
		if serr := h.Stop(context.Background()); serr != nil {
			t.Logf("handler stop: %v", serr)
		}
	}()

	sub := msgBus.Subscribe("gate-panic-alive", "turn.terminal")
	defer msgBus.Unsubscribe(sub)

	// Turn 1: panics in persistence.
	h.SetMessageSaver(gatePanicSaver{})
	first, err := json.Marshal(ChatRequest{Message: "first", ConversationID: "conv-alive-1", SessionID: "sess-alive-1"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	msgBus.Publish("chat.request", &models.BusMessage{ID: "m-alive-1", Type: models.MessageTypeRequest, Payload: first})
	if m := gateWaitEvent(sub); m == nil {
		t.Fatal("CURRENT DEFECT: no terminal for the panicking turn (no recover in handleRequest)")
	}

	// Turn 2: healthy saver, must still be served.
	h.SetMessageSaver(gateNoopSaver{})
	second, err := json.Marshal(ChatRequest{Message: "second", ConversationID: "conv-alive-2", SessionID: "sess-alive-2"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	msgBus.Publish("chat.request", &models.BusMessage{ID: "m-alive-2", Type: models.MessageTypeRequest, Payload: second})
	m := gateWaitEvent(sub)
	if m == nil {
		t.Fatal("CURRENT DEFECT: handler goroutine died with the panic; the next request is never served (daemon wedged for chat)")
	}
	var ev TurnTerminalEvent
	if err := json.Unmarshal(m.Payload, &ev); err != nil {
		t.Fatalf("decode terminal: %v", err)
	}
	if ev.Status != "completed" {
		t.Errorf("second turn status = %q, want completed (handler survived the panic)", ev.Status)
	}
}

type gateNoopSaver struct{}

func (gateNoopSaver) SaveMessages(string, []session.Message) error { return nil }

// TestParkEmptyReply pins the park-detection defect: a fresh throttle
// park returns ("", nil) from the loop, and the handler's err!=nil park
// check misses it -- the turn terminalizes "completed" with an EMPTY
// reply. C1: a parked turn reports status=parked, never empty completed.
func TestParkEmptyReply(t *testing.T) {
	SetDefaultBackoffOverride(BackoffConfig{
		BaseDelay:   time.Millisecond,
		MaxDelay:    2 * time.Millisecond,
		MaxAttempts: 2,
	})
	defer clearDefaultBackoffOverride()

	throttleErr := &llm.ThrottleBackoffError{
		ProviderID: "p1",
		ModelID:    "m1",
		RetryAt:    time.Now().Add(30 * time.Second),
		Attempt:    1,
		Cause:      &llm.APIError{StatusCode: http.StatusTooManyRequests, Detail: "slow down"},
	}
	resolver := newFailoverResolver(t,
		&llm.ModelConfig{BaseURL: "http://p1.invalid", ModelID: "m1", APIKey: "k", ProviderID: "p1"},
		&llm.ModelConfig{BaseURL: "http://p2.invalid", ModelID: "m2", APIKey: "k", ProviderID: "p2"},
	)
	loop := newThrottleTestLoop(t, &errChatter{err: throttleErr}, resolver)

	msgBus := bus.New(nil, slogDiscardLogger())
	h := NewChatHandler(loop, nil, msgBus, slogDiscardLogger())

	// Wire a parker with a far-future resume so the park succeeds and no
	// resume fires inside the test window.
	parker := NewTurnParker(slogDiscardLogger(), func(context.Context, ParkedTurnRecord) {}, time.Hour)
	loop.SetTurnParker(parker)

	sub := msgBus.Subscribe("gate-park-empty", "turn.terminal")
	defer msgBus.Unsubscribe(sub)

	reqPayload, err := json.Marshal(ChatRequest{
		Message:        "park me please",
		ConversationID: "conv-park-gate",
		SessionID:      "sess-park-gate",
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	h.handleRequest(context.Background(), &models.BusMessage{
		ID:        "m-park-gate",
		Type:      models.MessageTypeRequest,
		Topic:     "chat.request",
		Payload:   reqPayload,
		Timestamp: time.Now().UTC(),
	})

	m := gateWaitEvent(sub)
	if m == nil {
		t.Fatal("no turn.terminal for the throttle-parked turn")
	}
	var ev TurnTerminalEvent
	if err := json.Unmarshal(m.Payload, &ev); err != nil {
		t.Fatalf("decode terminal: %v", err)
	}
	if ev.Status == "completed" && strings.TrimSpace(ev.Reply) == "" {
		t.Fatalf("CURRENT DEFECT: fresh throttle park terminalized as completed with an EMPTY reply (park detection requires err!=nil; the loop returns nil on park). C1 wants status=parked")
	}
	if ev.Status != "parked" {
		t.Errorf("parked turn status = %q (reply %q), want parked (accepted-not-finished, C1 closed set)", ev.Status, ev.Reply)
	}
}

// TestParkEmptyReplyDirectMode is the same pin on the direct (no
// dispatcher) lane where RunOnceWithParts' ErrTurnParked swallow lives.
func TestParkEmptyReplyDirectMode(t *testing.T) {
	SetDefaultBackoffOverride(BackoffConfig{
		BaseDelay:   time.Millisecond,
		MaxDelay:    2 * time.Millisecond,
		MaxAttempts: 2,
	})
	defer clearDefaultBackoffOverride()

	throttleErr := &llm.ThrottleBackoffError{
		ProviderID: "p1",
		ModelID:    "m1",
		RetryAt:    time.Now().Add(30 * time.Second),
		Attempt:    1,
	}
	resolver := newFailoverResolver(t,
		&llm.ModelConfig{BaseURL: "http://p1.invalid", ModelID: "m1", APIKey: "k", ProviderID: "p1"},
		&llm.ModelConfig{BaseURL: "http://p2.invalid", ModelID: "m2", APIKey: "k", ProviderID: "p2"},
	)
	loop := newThrottleTestLoop(t, &errChatter{err: throttleErr}, resolver)
	parker := NewTurnParker(slogDiscardLogger(), func(context.Context, ParkedTurnRecord) {}, time.Hour)
	loop.SetTurnParker(parker)

	reply, err := loop.RunOnce(context.Background(), "park me", "conv-park-direct")
	if err != nil {
		t.Fatalf("RunOnce on park returned error %v; the park path returns nil today", err)
	}
	if strings.TrimSpace(reply) == "" {
		// This is the defect input shape the handler consumes: an empty
		// reply with nil error. The handler-side pin above asserts the
		// terminal; here we pin that the LOOP must surface the park
		// distinguishably (ErrTurnParked or a park notice), never a bare
		// empty success the handler cannot tell from a real empty reply.
		t.Logf("CURRENT DEFECT shape confirmed: RunOnce returned (%q, nil) for a throttle park -- indistinguishable from an empty completed turn", reply)
	}
	// The contract: a parked turn is distinguishable. Either the loop
	// returns ErrTurnParked (handler maps to parked) or a non-empty park
	// notice. A bare ("", nil) is the bug.
	if strings.TrimSpace(reply) == "" && err == nil {
		t.Errorf("CURRENT DEFECT: throttle park surfaced as empty-reply nil-error; the handler cannot detect the park and reports completed+empty (handler.go park detection requires err!=nil)")
	}
}
