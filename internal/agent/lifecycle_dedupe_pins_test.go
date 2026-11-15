package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// ---------------------------------------------------------------------------
// Fix 1 pin: duplicate chat.request with the same turn_id must NOT re-execute
// the turn (F16 dedupe honored at the handler seam, mirroring
// internal/rpc/chat_submit.go's suppression shape).
// ---------------------------------------------------------------------------

// TestHandleRequest_DuplicateTurnIDSuppressed drives handleRequest twice
// with the same explicit turn id: the second request must return the
// re-ack ("turn already submitted; result arrives via turn.terminal"),
// emit the duplicate-suppression terminal (never "completed"), and leave
// the original registry record (conversation id + trace) untouched.
func TestHandleRequest_DuplicateTurnIDSuppressed(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	loop := NewAgentLoop("test-session", "/tmp") // no LLM client: a real run would error
	h := NewChatHandler(loop, nil, msgBus, slogDiscardLogger())
	h.SetTurnRegistry(NewTurnRegistry())

	turnID := "turn-dedupe-pin-1"
	convID := "conv-dedupe-pin"

	// First request: fresh registration, runs for real. The legacy path
	// (loop without LLM client) errors, but the turn still registers and
	// completes its terminal event.
	payload, _ := json.Marshal(ChatRequest{
		Message:        "first",
		ConversationID: convID,
		TurnID:         turnID,
	})
	first := &models.BusMessage{
		ID:        "m-first",
		Type:      models.MessageTypeRequest,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	respSub := msgBus.Subscribe("test-dedupe-resp", "chat.response")
	defer msgBus.Unsubscribe(respSub)
	termSub := msgBus.Subscribe("test-dedupe-term", "turn.terminal")
	defer msgBus.Unsubscribe(termSub)

	h.handleRequest(context.Background(), first)

	// The first turn must have produced its own terminal event. Its
	// registry row stays alive only when a task is pending on it; on the
	// inline/legacy path the row leaves and the id tombstones.
	firstTerm := waitTurnTerminal(t, termSub)
	if firstTerm.TurnID != turnID {
		t.Fatalf("first turn terminal turn_id = %q, want %q", firstTerm.TurnID, turnID)
	}
	assertNoSecondTurnTerminal(t, termSub)

	// Second request with the SAME turn id: must be suppressed without
	// executing. The legacy path would call the loop and error; the
	// suppression must short-circuit BEFORE that, so the chat.response
	// carries the re-ack note and NOT an error.
	second := &models.BusMessage{
		ID:        "m-second",
		Type:      models.MessageTypeRequest,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload, // same payload, same turn id
	}
	h.handleRequest(context.Background(), second)

	// Exactly ONE additional terminal event, and it must be the
	// duplicate-suppression ack — never "completed" (which awaiting
	// clients would take as the result), never "failed".
	dupeTerm := waitTurnTerminal(t, termSub)
	if dupeTerm.TurnID != turnID {
		t.Fatalf("duplicate terminal turn_id = %q, want %q", dupeTerm.TurnID, turnID)
	}
	if dupeTerm.HandlerCase != "duplicate_suppressed" {
		t.Fatalf("duplicate handler_case = %q, want duplicate_suppressed", dupeTerm.HandlerCase)
	}
	if dupeTerm.Status != "parked" {
		t.Fatalf("duplicate status = %q, want parked (accepted-not-finished)", dupeTerm.Status)
	}
	assertNoSecondTurnTerminal(t, termSub)

	// The re-ack response must carry the established suppression note and
	// NO error — the duplicate did not "fail", it was absorbed.
	resp := waitChatResponse(t, respSub, "m-second")
	if resp.Error != "" {
		t.Fatalf("duplicate re-ack carried an error: %q", resp.Error)
	}
	if resp.Reply != "turn already submitted; result arrives via turn.terminal" {
		t.Fatalf("duplicate re-ack reply = %q, want the suppression note", resp.Reply)
	}
	if resp.ConversationID != convID {
		t.Fatalf("duplicate re-ack conversation_id = %q, want %q", resp.ConversationID, convID)
	}
}

// TestHandleRequest_DuplicateTurnIDInFlight pins the STILL-TRACKED half of
// the F16 contract: a turn registered but not yet terminal (the in-flight
// submission shape — registered by chat.submit / an executing handler)
// suppresses a duplicate handleRequest WITHOUT tombstoning or touching the
// live record: the trace attachment stays the ORIGINAL one (AttachTrace
// fires only on true-first registration) and no loop execution happens.
func TestHandleRequest_DuplicateTurnIDInFlight(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	loop := NewAgentLoop("test-session", "/tmp")
	h := NewChatHandler(loop, nil, msgBus, slogDiscardLogger())
	h.SetTurnRegistry(NewTurnRegistry())

	turnID := "turn-dedupe-pin-3"
	convID := "conv-dedupe-pin-3"
	// Simulate the in-flight first submission: the ORIGINAL chat.request
	// already CLAIMED the dispatch (the handler's own gate — task-state
	// regression 2026-11-14: chat.submit's Register is a pre-registration
	// for client-retry idempotency and is NOT a dispatch claim; a row
	// with dispatchClaimed=true is what a true in-flight duplicate hits).
	// Registered with its original trace correlation (the mint seam's
	// AttachTrace).
	origTrace, origSpan := id.TraceID(), id.SpanID()
	h.turnRegistry.ClaimDispatch(turnID, convID)
	h.turnRegistry.AttachTrace(turnID, origTrace, origSpan)

	payload, _ := json.Marshal(ChatRequest{
		Message:        "in-flight duplicate",
		ConversationID: convID,
		TurnID:         turnID,
	})

	respSub := msgBus.Subscribe("test-inflight-resp", "chat.response")
	defer msgBus.Unsubscribe(respSub)
	termSub := msgBus.Subscribe("test-inflight-term", "turn.terminal")
	defer msgBus.Unsubscribe(termSub)

	h.handleRequest(context.Background(), &models.BusMessage{
		ID:        "m-inflight",
		Type:      models.MessageTypeRequest,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	})

	// Suppression terminal, never an execution.
	dupe := waitTurnTerminal(t, termSub)
	if dupe.HandlerCase != "duplicate_suppressed" || dupe.Status != "parked" {
		t.Fatalf("in-flight duplicate terminal = %+v, want duplicate_suppressed/parked", dupe)
	}
	assertNoSecondTurnTerminal(t, termSub)

	resp := waitChatResponse(t, respSub, "m-inflight")
	if resp.Error != "" {
		t.Fatalf("in-flight duplicate re-ack carried an error: %q", resp.Error)
	}

	// The live record survived byte-identically: same conversation
	// binding AND the ORIGINAL trace — the duplicate's freshly-minted
	// trace never overwrote it.
	traceID, span := h.turnRegistry.TraceForTurn(turnID)
	if traceID != origTrace || span != origSpan {
		t.Fatalf("registry trace after duplicate = (%q, %q), want the original (%q, %q)",
			traceID, span, origTrace, origSpan)
	}
	if rec := turnRecordForTest(t, h.turnRegistry, turnID); rec == nil || rec.ConversationID != convID {
		t.Fatalf("registry conversation binding changed by duplicate: %+v", rec)
	}
}

// TestHandleRequest_DuplicateTurnIDTombstoned pins the completed-tombstone
// half of the F16 contract: after the turn completed (Complete tombstones
// the id), a same-turn-id retry is still suppressed — it must not
// re-execute the finished work.
func TestHandleRequest_DuplicateTurnIDTombstoned(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	loop := NewAgentLoop("test-session", "/tmp")
	h := NewChatHandler(loop, nil, msgBus, slogDiscardLogger())
	h.SetTurnRegistry(NewTurnRegistry())

	turnID := "turn-dedupe-pin-2"
	payload, _ := json.Marshal(ChatRequest{
		Message:        "only once",
		ConversationID: "conv-dedupe-pin-2",
		TurnID:         turnID,
	})

	termSub := msgBus.Subscribe("test-tomb-term", "turn.terminal")
	defer msgBus.Unsubscribe(termSub)

	first := &models.BusMessage{
		ID:        "m-t1",
		Type:      models.MessageTypeRequest,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	h.handleRequest(context.Background(), first)
	ev := waitTurnTerminal(t, termSub)
	if ev.Status == "" {
		t.Fatal("first turn produced no terminal event")
	}
	assertNoSecondTurnTerminal(t, termSub)

	// Retry after completion: suppressed with the same parked re-ack.
	h.handleRequest(context.Background(), &models.BusMessage{
		ID:        "m-t2",
		Type:      models.MessageTypeRequest,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	})
	dupe := waitTurnTerminal(t, termSub)
	if dupe.HandlerCase != "duplicate_suppressed" || dupe.Status != "parked" {
		t.Fatalf("tombstone retry terminal = %+v, want duplicate_suppressed/parked", dupe)
	}
	assertNoSecondTurnTerminal(t, termSub)
}

// turnRecordForTest reads a SubmittedTurnRecord directly (white-box pin).
func turnRecordForTest(t *testing.T, reg *TurnRegistry, turnID string) *SubmittedTurnRecord {
	t.Helper()
	if reg == nil {
		t.Fatal("nil registry")
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	rec, ok := reg.turns[turnID]
	if !ok {
		return nil
	}
	cp := *rec
	return &cp
}

// waitChatResponse blocks until a chat.response replying to replyTo arrives.
func waitChatResponse(t *testing.T, sub *bus.Subscriber, replyTo string) ChatResponse {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case msg := <-sub.Channel:
			if msg.ReplyTo != replyTo {
				continue // a response for another request; keep waiting
			}
			var resp ChatResponse
			if err := json.Unmarshal(msg.Payload, &resp); err != nil {
				t.Fatalf("unmarshal chat.response: %v", err)
			}
			return resp
		case <-time.After(500 * time.Millisecond):
		}
	}
	t.Fatalf("no chat.response replying to %s within deadline", replyTo)
	return ChatResponse{}
}

// ---------------------------------------------------------------------------
// Fix 2 pin: the budget pre-check denial must cancel (not fail) the
// fresh-created pending task — pending->failed is not a legal edge in
// taskTransitions; pending->cancelled is.
// ---------------------------------------------------------------------------

// TestHandleRequest_BudgetBlockedCancelsPendingTask drives the real
// async-dispatch path (quickplan verdict -> created pending task) with an
// exceeded budget and asserts the never-started task lands in
// StateCancelled — never StateFailed.
func TestHandleRequest_BudgetBlockedCancelsPendingTask(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	d, store := asyncTurnTestDispatcher(t)
	h := NewChatHandler(nil, d, msgBus, slogDiscardLogger())
	h.SetTaskStore(store)
	// Tiny hourly limit + recorded usage: CheckBudget exceeds immediately.
	h.SetBudget(llm.NewBudget(llm.BudgetConfig{
		HourlyLimit:    1,
		Aggressiveness: 1.0,
	}, nil))
	h.budget.RecordUsage(llm.TokenUsage{TotalTokens: 100})

	termSub := msgBus.Subscribe("test-budget-term", "turn.terminal")
	defer msgBus.Unsubscribe(termSub)

	payload, _ := json.Marshal(ChatRequest{
		Message:        asyncTurnTestInput,
		ConversationID: "conv-budget-pin",
	})
	h.handleRequest(context.Background(), &models.BusMessage{
		ID:        "m-budget",
		Type:      models.MessageTypeRequest,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	})

	// The turn ends in the "parked" state (the default-config budget
	// windows are time-windowed, so the handler queues the turn for
	// automatic retry) — the budget failure is the REPLY, not an error.
	ev := waitTurnTerminal(t, termSub)
	if ev.Status != "parked" {
		t.Fatalf("budget-blocked turn status = %q, want parked", ev.Status)
	}
	if ev.HandlerCase != "budget_blocked" {
		t.Fatalf("handler_case = %q, want budget_blocked", ev.HandlerCase)
	}

	// The task ClassifyAndRoute just created was persisted and CANCELLED
	// (legal pending edge), not failed.
	tasks, listErr := store.List(nil, 100)
	if listErr != nil {
		t.Fatalf("list tasks: %v", listErr)
	}
	if len(tasks) == 0 {
		t.Fatal("ClassifyAndRoute created no task — fixture drift")
	}
	tk, getErr := store.GetByID(tasks[0].ID)
	if getErr != nil || tk == nil {
		t.Fatalf("task not in store: %v", getErr)
	}
	if tk.State != task.StateCancelled {
		t.Fatalf("budget-blocked task state = %q, want cancelled (pending->failed is an illegal edge)", string(tk.State))
	}
}

// ---------------------------------------------------------------------------
// Fix 3 pins: a late finalize must never flip a terminal task's state.
// ---------------------------------------------------------------------------

// TestTacticalScheduler_OnJobFailed_CancelledTaskNotFlippedToFailed pins
// the tactical.go guard: a task a user cancelled (or startup recovery
// terminalized) while its last step job was in flight must survive a late
// OnJobFailed finalization with its terminal state AND stay silent on
// task.failed.
func TestTacticalScheduler_OnJobFailed_CancelledTaskNotFlippedToFailed(t *testing.T) {
	ts, msgBus, cleanup := newTacticalTestSetup(t)
	defer cleanup()

	q, qerr := queue.NewPersistentQueue(t.TempDir()+"/q.db", msgBus, slogDiscardLogger())
	if qerr != nil {
		t.Fatalf("queue: %v", qerr)
	}
	defer q.Close()
	ts.queue = q

	parentTask := task.NewTask("late-fail-pin", "cancelled task survives late finalize")
	parentTask.SetState(task.StateExecuting)
	if err := ts.taskStore.Create(parentTask); err != nil {
		t.Fatalf("create task: %v", err)
	}

	step := task.NewTaskStep(parentTask.ID, "in-flight step", 0)
	if err := ts.stepStore.Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}
	job, jerr := queue.NewJob(queue.JobTypeProjectTask, map[string]string{"prompt": "b2 step"})
	if jerr != nil {
		t.Fatalf("NewJob: %v", jerr)
	}
	job.WithTaskID(step.TaskID).WithAgentID(step.AgentID)
	if err := q.Enqueue(context.Background(), job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	claimed, cerr := q.Claim(context.Background(), "worker-late-fail", nil, "")
	if cerr != nil {
		t.Fatalf("Claim: %v", cerr)
	}
	if err := ts.stepStore.SetJobID(step.ID, claimed.ID); err != nil {
		t.Fatalf("SetJobID: %v", err)
	}
	if err := ts.stepStore.SetState(step.ID, task.StepRunning); err != nil {
		t.Fatalf("set step running: %v", err)
	}

	// The task is terminalized OUT OF BAND while the step is in flight
	// (the user-cancel shape).
	cancelled, gerr := ts.taskStore.GetByID(parentTask.ID)
	if gerr != nil || cancelled == nil {
		t.Fatalf("reload task: %v", gerr)
	}
	cancelled.SetState(task.StateCancelled)
	if err := ts.taskStore.Update(cancelled); err != nil {
		t.Fatalf("cancel task: %v", err)
	}

	failedSub := msgBus.Subscribe("test-late-fail", "task.failed")
	defer msgBus.Unsubscribe(failedSub)

	if err := ts.OnJobFailed(context.Background(), claimed.ID, "boom"); err != nil {
		t.Fatalf("OnJobFailed: %v", err)
	}

	// The cancelled state survived the late finalize.
	tk, gerr := ts.taskStore.GetByID(parentTask.ID)
	if gerr != nil || tk == nil {
		t.Fatalf("reload task after finalize: %v", gerr)
	}
	if tk.State != task.StateCancelled {
		t.Fatalf("late finalize flipped cancelled task to %q — terminal state must never be overwritten", string(tk.State))
	}
	// No task.failed published for the terminal task.
	select {
	case msg := <-failedSub.Channel:
		t.Fatalf("task.failed published for an already-terminal task: %s", msg.Payload)
	case <-time.After(150 * time.Millisecond):
	}
}

// TestPairManager_finalizeTask_CancelledTaskNotFlipped pins the
// pair_manager.go guard: a late pair-session finalization must not
// overwrite a user-cancelled task — in either outcome direction.
func TestPairManager_finalizeTask_CancelledTaskNotFlipped(t *testing.T) {
	store, err := task.NewStore(t.TempDir()+"/tasks.db", nil)
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	defer store.Close()

	pm := NewPairManager(PairManagerConfig{
		TaskStore: store,
		Logger:    slogDiscardLogger(),
	})

	parentTask := task.NewTask("pair-late-pin", "cancelled task survives pair finalize")
	parentTask.SetState(task.StateExecuting)
	if err := store.Create(parentTask); err != nil {
		t.Fatalf("create task: %v", err)
	}
	cancelled, gerr := store.GetByID(parentTask.ID)
	if gerr != nil || cancelled == nil {
		t.Fatalf("reload task: %v", gerr)
	}
	cancelled.SetState(task.StateCancelled)
	if err := store.Update(cancelled); err != nil {
		t.Fatalf("cancel task: %v", err)
	}

	session := pm.CreateSession(parentTask.ID, "spec", "coder", "planner", 3)

	// Both outcome directions must refuse to touch the terminal task.
	pm.finalizeTask(context.Background(), session, true)
	pm.finalizeTask(context.Background(), session, false)

	tk, gerr := store.GetByID(parentTask.ID)
	if gerr != nil || tk == nil {
		t.Fatalf("reload task after pair finalize: %v", gerr)
	}
	if tk.State != task.StateCancelled {
		t.Fatalf("pair finalize flipped cancelled task to %q — terminal state must never be overwritten", string(tk.State))
	}
}
