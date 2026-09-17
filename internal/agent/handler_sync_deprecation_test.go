package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// ---------------------------------------------------------------------------
// Sync chat deprecation (async-turn-migration leaf 07)
//
// The default contract is async-everywhere: a task-dispatched turn acks
// immediately and its result arrives via turn.terminal. The blocking sync
// wait (waitForTaskCompletion, 110s ceiling, "still running" stub) is a
// legacy opt-in reachable only with syncMode set (daemon wiring:
// SetSyncMode(cfg.Orchestrator.SyncChatEnabled), default false).
//
// waitForTaskCompletion itself is UNCHANGED by this leaf — these tests
// pin its REACHABILITY, not its internals.
// ---------------------------------------------------------------------------

// stubShaped reports whether a reply is the "Task <id> is still running"
// stub (or its completed-task cousin) that the sync wait can synthesize.
// The default path must NEVER produce one.
func stubShaped(reply string) bool {
	return strings.Contains(reply, "is still running") ||
		strings.HasPrefix(reply, "Task ") && strings.Contains(reply, "completed.")
}

// syncDeprecationRequest builds a chat.request bus message with the given
// source client, long enough to bypass the short/simple guard and land in
// the async-dispatch case (same input as asyncTurnTestInput).
func syncDeprecationRequest(t *testing.T, sourceClient, conversationID string) *models.BusMessage {
	t.Helper()
	payload, err := json.Marshal(ChatRequest{
		Message:        asyncTurnTestInput,
		ConversationID: conversationID,
		SourceClient:   sourceClient,
	})
	if err != nil {
		t.Fatalf("marshal chat request: %v", err)
	}
	return &models.BusMessage{
		ID:        "m-sync-dep",
		Type:      models.MessageTypeRequest,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
}

// TestHandleRequest_SyncOff_BenchClientGetsAsyncAck proves the default
// path cannot return the stub: with syncMode unset (the config default,
// sync_chat_enabled=false), a task-dispatched turn from the meept-bench
// source NEVER enters the sync wait — not even the former bench
// special-case may force it. The handler resolves while a real sync wait
// would still be blocking on its 110s ceiling, and the reply is the async
// ack, never stub-shaped.
func TestHandleRequest_SyncOff_BenchClientGetsAsyncAck(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	d, store := asyncTurnTestDispatcher(t)
	h := NewChatHandler(nil, d, msgBus, slogDiscardLogger())
	h.SetTaskStore(store)
	// No SetSyncMode: syncMode is false, exactly what the daemon wires
	// under the default config. A tight syncWaitCeiling would turn any
	// accidental entry into the sync wait into a fast, observable return
	// — but this test deliberately leaves the PRODUCTION ceiling in place
	// and asserts the handler returns long before it could elapse.
	h.syncWaitCeiling = 110 * time.Second

	sub := msgBus.Subscribe("test-turn-terminal", "turn.terminal")
	defer h.bus.Unsubscribe(sub)

	done := make(chan struct{})
	go func() {
		h.handleRequest(context.Background(), syncDeprecationRequest(t, "meept-bench-leaf07", "conv-sync-off-bench"))
		close(done)
	}()

	select {
	case <-done:
		// handleRequest returned without waiting — the async-ack posture.
	case <-time.After(5 * time.Second):
		t.Fatal("handleRequest blocked: with sync_chat_enabled=false the sync wait (110s ceiling) must be unreachable, even for source_client=meept-bench")
	}

	ev := waitTurnTerminal(t, sub)
	if ev.HandlerCase != "async_dispatch" {
		t.Errorf("handler_case = %q, want async_dispatch (sync wait must not run)", ev.HandlerCase)
	}
	if stubShaped(ev.Reply) {
		t.Errorf("reply is stub-shaped under default config: %q", ev.Reply)
	}
	assertNoSecondTurnTerminal(t, sub)
}

// TestHandleRequest_SyncOff_OrdinaryClientGetsAsyncAck drives the same
// default-path proof for a plain (non-bench) source client.
func TestHandleRequest_SyncOff_OrdinaryClientGetsAsyncAck(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	d, store := asyncTurnTestDispatcher(t)
	h := NewChatHandler(nil, d, msgBus, slogDiscardLogger())
	h.SetTaskStore(store)

	sub := msgBus.Subscribe("test-turn-terminal", "turn.terminal")
	defer h.bus.Unsubscribe(sub)

	done := make(chan struct{})
	go func() {
		h.handleRequest(context.Background(), syncDeprecationRequest(t, "external-legacy-consumer", "conv-sync-off-plain"))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handleRequest blocked with sync off: the sync wait is unreachable on the default path")
	}

	ev := waitTurnTerminal(t, sub)
	if ev.HandlerCase != "async_dispatch" {
		t.Errorf("handler_case = %q, want async_dispatch", ev.HandlerCase)
	}
	if stubShaped(ev.Reply) {
		t.Errorf("reply is stub-shaped under default config: %q", ev.Reply)
	}
	assertNoSecondTurnTerminal(t, sub)
}

// TestHandleRequest_SyncOn_LegacyPathStillWorks proves the legacy opt-in
// keeps the old contract: with syncMode set (what the daemon wires when
// orchestrator.sync_chat_enabled=true), a task-dispatched turn from the
// bench source still enters the sync wait and the reply carries task
// completion — not the ack.
func TestHandleRequest_SyncOn_LegacyPathStillWorks(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	d, store := asyncTurnTestDispatcher(t)
	h := NewChatHandler(nil, d, msgBus, slogDiscardLogger())
	h.SetTaskStore(store)
	h.SetSyncMode(true) // daemon wiring for sync_chat_enabled=true
	h.syncWaitCeiling = 200 * time.Millisecond

	sub := msgBus.Subscribe("test-turn-terminal", "turn.terminal")
	defer h.bus.Unsubscribe(sub)

	h.handleRequest(context.Background(), syncDeprecationRequest(t, "meept-bench-leaf07-legacy", "conv-sync-on-bench"))

	ev := waitTurnTerminal(t, sub)
	if ev.HandlerCase != "sync_dispatch" {
		t.Errorf("handler_case = %q, want sync_dispatch (legacy opt-in keeps the blocking wait)", ev.HandlerCase)
	}
	if ev.TaskID == "" {
		t.Error("task_id must be set on the legacy sync path")
	}
	assertNoSecondTurnTerminal(t, sub)
}
