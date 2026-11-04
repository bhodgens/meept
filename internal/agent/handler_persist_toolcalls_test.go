package agent

import (
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
)

// Tests for the tool-call persistence writer (wiring-gap-remediation leaf
// 05): persistExchange threads the loop's current-turn tool calls into the
// session store keyed to the assistant row. The read side (restore) is a
// documented follow-up, NOT covered here.

// toolCallSpySaver is a narrow SessionMessageSaver double that also
// satisfies sessionToolCallSaver and records what the persist path wrote.
type toolCallSpySaver struct {
	saved           [][]session.Message
	toolCallBatches map[int][]session.ToolCall
	// failWithToolCalls, when non-empty, makes SaveMessagesWithToolCalls
	// return an error (best-effort semantics pin).
	failWithToolCalls bool
	fallbackSaved     bool
}

func (s *toolCallSpySaver) SaveMessages(sessionID string, messages []session.Message) error {
	s.saved = append(s.saved, messages)
	s.fallbackSaved = true
	return nil
}

func (s *toolCallSpySaver) SaveMessagesWithToolCalls(sessionID string, messages []session.Message, toolCalls map[int][]session.ToolCall) error {
	if s.failWithToolCalls {
		s.saved = append(s.saved, messages) // record the attempt, then fail
		return errPersistInjected
	}
	s.saved = append(s.saved, messages)
	s.toolCallBatches = toolCalls
	return nil
}

type constErr string

func (e constErr) Error() string { return string(e) }

const errPersistInjected = constErr("injected persistence failure")

// newPersistTestHandler builds a ChatHandler with just the pieces
// persistExchange touches: logger, message saver, and a loop whose
// conversation carries a scripted turn (user msg -> assistant tool calls ->
// tool results -> final assistant reply).
func newPersistTestHandler(t *testing.T, saver SessionMessageSaver, conv *Conversation) *ChatHandler {
	t.Helper()
	h := &ChatHandler{}
	h.logger = testLogger()
	h.messageSaver = saver
	if conv != nil {
		h.loop = &AgentLoop{}
		// turnToolCalls reads through GetConversation ->
		// conversations.GetIfExists; hand the loop a store pre-seeded with
		// the scripted conversation under the session id the tests use.
		store := NewConversationStore(0)
		store.conversations["sess-tc"] = conv
		store.conversations["sess-plain"] = conv
		store.conversations["sess-fail"] = conv
		store.conversations["sess-nocap"] = conv
		store.conversations["sess-boundary"] = conv
		h.loop.conversations = store
	}
	return h
}

// seedTurnConversation scripts one tool-call turn: user question, assistant
// message carrying two tool calls, tool results, final assistant text.
func seedTurnConversation() *Conversation {
	conv := NewConversation()
	conv.AddUserMessage("clean the tmp dir")
	conv.AddAssistantMessageWithToolCalls("", []llm.ToolCall{
		{ID: "call-1", Type: "function", Function: llm.ToolCallFunction{Name: "shell", Arguments: `{"command":"ls /tmp"}`}},
		{ID: "call-2", Type: "function", Function: llm.ToolCallFunction{Name: "shell", Arguments: `{"command":"rm -rf /tmp/x"}`}},
	})
	conv.AddMessage(llm.ChatMessage{Role: llm.RoleTool, ToolCallID: "call-1", Content: "x"})
	conv.AddMessage(llm.ChatMessage{Role: llm.RoleTool, ToolCallID: "call-2", Content: ""})
	conv.AddAssistantMessage("done, cleaned /tmp/x")
	return conv
}

// TestPersistExchangeWritesToolCalls covers the GREEN path: a scripted turn
// with two tool calls persists the exchange AND lands both calls keyed to
// the assistant row (one store call, assistant index, seq order preserved).
func TestPersistExchangeWritesToolCalls(t *testing.T) {
	saver := &toolCallSpySaver{}
	h := newPersistTestHandler(t, saver, seedTurnConversation())

	h.persistExchange("sess-tc", "clean the tmp dir", nil, "done, cleaned /tmp/x", "")

	if len(saver.saved) != 1 {
		t.Fatalf("expected exactly one store write, got %d", len(saver.saved))
	}
	if saver.fallbackSaved {
		t.Fatal("plain SaveMessages ran even though the tool-call path should have served the write")
	}
	if len(saver.toolCallBatches) != 1 {
		t.Fatalf("expected one tool-call batch, got %d", len(saver.toolCallBatches))
	}
	msgs := saver.saved[0]
	if len(msgs) != 2 || msgs[0].Role != "user" || msgs[1].Role != "assistant" {
		t.Fatalf("unexpected exchange shape: %+v", msgs)
	}
	tcs, ok := saver.toolCallBatches[1] // assistant is index 1
	if !ok {
		t.Fatal("tool calls not keyed to the assistant message index")
	}
	if len(tcs) != 2 {
		t.Fatalf("want 2 persisted tool calls, got %d", len(tcs))
	}
	if tcs[0].ToolCallID != "call-1" || tcs[1].ToolCallID != "call-2" {
		t.Fatalf("tool call ids wrong: %q, %q", tcs[0].ToolCallID, tcs[1].ToolCallID)
	}
	if tcs[0].Seq != 0 || tcs[1].Seq != 1 {
		t.Fatalf("seq wrong: %d, %d", tcs[0].Seq, tcs[1].Seq)
	}
}

// TestPersistExchangeNoToolCallsFallsBack covers the no-tool-call turn:
// plain SaveMessages serves the write and no tool-call batch is attempted.
func TestPersistExchangeNoToolCallsFallsBack(t *testing.T) {
	conv := NewConversation()
	conv.AddUserMessage("hi")
	conv.AddAssistantMessage("hello")
	saver := &toolCallSpySaver{}
	h := newPersistTestHandler(t, saver, conv)

	h.persistExchange("sess-plain", "hi", nil, "hello", "")

	if len(saver.saved) != 1 || !saver.fallbackSaved {
		t.Fatalf("expected the plain SaveMessages fallback, got saved=%d fallback=%v", len(saver.saved), saver.fallbackSaved)
	}
	if saver.toolCallBatches != nil {
		t.Fatalf("unexpected tool-call batch: %+v", saver.toolCallBatches)
	}
}

// TestPersistExchangeWriteFailureNeverBreaksTurn pins the best-effort
// contract: a store write failure (with tool calls in flight) logs and
// returns - persistExchange panics on nothing and returns no error upward
// (the chat path contract). The injected failure is witnessed by the spy.
func TestPersistExchangeWriteFailureNeverBreaksTurn(t *testing.T) {
	saver := &toolCallSpySaver{failWithToolCalls: true}
	h := newPersistTestHandler(t, saver, seedTurnConversation())

	// Must not panic; error is logged and swallowed.
	h.persistExchange("sess-fail", "clean the tmp dir", nil, "done", "")

	if len(saver.saved) != 1 {
		t.Fatalf("expected the attempted write to be witnessed once, got %d", len(saver.saved))
	}
}

// TestPersistExchangeWithoutCapabilityFallsBack pins the narrow-saver
// shape: a saver that only implements SessionMessageSaver still gets the
// exchange through the plain path (wiring a store without the capability
// never drops messages).
func TestPersistExchangeWithoutCapabilityFallsBack(t *testing.T) {
	conv := seedTurnConversation()
	saver := &plainSaverOnly{}
	h := newPersistTestHandler(t, saver, conv)

	h.persistExchange("sess-nocap", "clean the tmp dir", nil, "done", "")

	if !saver.saved {
		t.Fatal("exchange not persisted through the plain path")
	}
}

type plainSaverOnly struct {
	saved bool
}

func (s *plainSaverOnly) SaveMessages(sessionID string, messages []session.Message) error {
	s.saved = true
	return nil
}

// TestTurnToolCallsTurnBoundary pins the turn-scoping rule: a PREVIOUS
// turn's tool calls never leak into the current persist - the collection
// resets at the last user message.
func TestTurnToolCallsTurnBoundary(t *testing.T) {
	conv := NewConversation()
	conv.AddUserMessage("first question")
	conv.AddAssistantMessageWithToolCalls("", []llm.ToolCall{
		{ID: "old-call", Type: "function", Function: llm.ToolCallFunction{Name: "shell", Arguments: `{}`}},
	})
	conv.AddMessage(llm.ChatMessage{Role: llm.RoleTool, ToolCallID: "old-call", Content: "old"})
	conv.AddAssistantMessage("first done")
	conv.AddUserMessage("second question")
	conv.AddAssistantMessage("second done, no tools this turn")

	saver := &toolCallSpySaver{}
	h := newPersistTestHandler(t, saver, conv)
	h.persistExchange("sess-boundary", "second question", nil, "second done, no tools this turn", "")

	if saver.toolCallBatches != nil {
		t.Fatalf("previous turn's tool calls leaked: %+v", saver.toolCallBatches)
	}
}

// TestSessionSQLiteStoreToolCallRoundtrip is the store-side roundtrip pin:
// SaveMessagesWithToolCalls writes rows keyed by the inserted assistant
// message_id and GetToolCallsForMessages reads them back.
func TestSessionSQLiteStoreToolCallRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store, err := session.NewSQLiteStore(filepath.Join(dir, "sessions.db"), testLogger())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, err := store.Create("sess-roundtrip"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	msgs := []session.Message{
		{SessionID: "sess-roundtrip", Role: "user", Content: "run it"},
		{SessionID: "sess-roundtrip", Role: "assistant", Content: "done"},
	}
	toolCalls := map[int][]session.ToolCall{
		1: {
			{ToolName: "shell", ToolCallID: "rt-1", Arguments: `{"command":"ls"}`, Seq: 0},
			{ToolName: "shell", ToolCallID: "rt-2", Arguments: `{"command":"rm -rf x"}`, Seq: 1},
		},
	}
	if err := store.SaveMessagesWithToolCalls("sess-roundtrip", msgs, toolCalls); err != nil {
		t.Fatalf("SaveMessagesWithToolCalls: %v", err)
	}

	// Read back through the message IDs the store assigned.
	saved, err := store.GetMessages("sess-roundtrip", 0, 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(saved) != 2 {
		t.Fatalf("want 2 messages, got %d", len(saved))
	}
	byID, err := store.GetToolCallsForMessages([]int64{saved[1].ID})
	if err != nil {
		t.Fatalf("GetToolCallsForMessages: %v", err)
	}
	got := byID[saved[1].ID]
	if len(got) != 2 {
		t.Fatalf("want 2 tool call rows for assistant message %d, got %d", saved[1].ID, len(got))
	}
	if got[0].ToolCallID != "rt-1" || got[1].ToolCallID != "rt-2" {
		t.Fatalf("roundtrip ids wrong: %q %q", got[0].ToolCallID, got[1].ToolCallID)
	}
	if got[0].MessageID != saved[1].ID {
		t.Fatalf("tool call keyed to message %d, want %d", got[0].MessageID, saved[1].ID)
	}
}
