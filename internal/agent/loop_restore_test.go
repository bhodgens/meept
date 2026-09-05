package agent

// Conversation-restore tests (plan: docs/plans/session-continuity/
// 02-conversation-restore.md). They verify that a cache-miss conversation
// lookup in RunOnce restores prior history from the session store via
// ConversationStore.GetOrRestore, that session.restore_message_limit
// tail-caps the restored history, and that store errors / unknown IDs /
// empty histories degrade to a fresh conversation without breaking the
// turn.

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRestoreStore is a minimal in-memory sessionMessageReader for
// restore-path tests.
type fakeRestoreStore struct {
	mu       sync.Mutex
	messages map[string][]session.Message
	err      error
	getCalls int
}

func newFakeRestoreStore() *fakeRestoreStore {
	return &fakeRestoreStore{messages: make(map[string][]session.Message)}
}

func (f *fakeRestoreStore) GetMessages(sessionID string, offset, limit int) ([]session.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	if f.err != nil {
		return nil, f.err
	}
	msgs := f.messages[sessionID]
	if offset >= len(msgs) {
		return nil, nil
	}
	end := len(msgs)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	result := make([]session.Message, end-offset)
	copy(result, msgs[offset:end])
	return result, nil
}

func (f *fakeRestoreStore) seed(sessionID string, msgs ...session.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages[sessionID] = msgs
}

// captureChatter wraps the terminate-path chatter fake and records the
// message list of every LLM call so tests can assert on model-visible
// conversation contents in order.
type captureChatter struct {
	mockChatter
	mu       sync.Mutex
	captured [][]llm.ChatMessage
}

func newCaptureChatter(responses ...*llm.Response) *captureChatter {
	return &captureChatter{mockChatter: mockChatter{responses: responses}}
}

func (m *captureChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	m.mu.Lock()
	cp := make([]llm.ChatMessage, len(messages))
	copy(cp, messages)
	m.captured = append(m.captured, cp)
	m.mu.Unlock()
	return m.mockChatter.Chat(ctx, messages, opts...)
}

func (m *captureChatter) allCalls() [][]llm.ChatMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]llm.ChatMessage, len(m.captured))
	copy(out, m.captured)
	return out
}

func (m *captureChatter) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.captured)
}

// newRestoreTestLoop builds a loop wired for a single-turn RunOnce test:
// capture chatter + restore store + config with the given restore limit.
func newRestoreTestLoop(t *testing.T, store *fakeRestoreStore, limit int, responses ...*llm.Response) (*AgentLoop, *captureChatter) {
	t.Helper()
	chatter := newCaptureChatter(responses...)
	loop := NewAgentLoop("test-session", "/tmp",
		WithLLMChatter(chatter),
		WithLoopLogger(slogDiscardLogger()),
		WithAgentConfig(AgentConfig{MaxIterations: 10}),
	)
	require.NotNil(t, loop)
	loop.SetSessionStore(store, config.SessionConfig{RestoreMessageLimit: limit})
	return loop, chatter
}

// seedExchange appends one user/assistant exchange to the store.
func seedExchange(store *fakeRestoreStore, convID string, userMsg, assistantMsg string) {
	existing := store.messagesFor(convID)
	existing = append(existing,
		session.Message{SessionID: convID, Role: "user", Content: userMsg, EntryType: "message"},
		session.Message{SessionID: convID, Role: "assistant", Content: assistantMsg, EntryType: "message"},
	)
	store.seed(convID, existing...)
}

// messagesFor is a test-side read helper for seeding; it locks properly.
func (f *fakeRestoreStore) messagesFor(sessionID string) []session.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]session.Message, len(f.messages[sessionID]))
	copy(out, f.messages[sessionID])
	return out
}

// TestConversationRestore_RestoreFnDirect unit-tests the restore function
// mapping directly: ascending order, user/assistant filtering, and tail
// capping.
func TestConversationRestore_RestoreFnDirect(t *testing.T) {
	store := newFakeRestoreStore()
	store.seed("conv-1",
		session.Message{SessionID: "conv-1", Role: "system", Content: "anchor noise", EntryType: "anchor"},
		session.Message{SessionID: "conv-1", Role: "user", Content: "q1", EntryType: "message"},
		session.Message{SessionID: "conv-1", Role: "assistant", Content: "a1", EntryType: "message"},
		session.Message{SessionID: "conv-1", Role: "user", Content: "q2", EntryType: "message"},
		session.Message{SessionID: "conv-1", Role: "assistant", Content: "a2", EntryType: "message"},
	)

	loop := NewAgentLoop("test-session", "/tmp")
	loop.SetSessionStore(store, config.SessionConfig{RestoreMessageLimit: 0})
	require.NotNil(t, loop.conversationRestoreFn, "SetSessionStore must wire the restore function")

	// Limit 0 => all user/assistant messages in ascending order.
	msgs, err := loop.conversationRestoreFn("conv-1")
	require.NoError(t, err)
	require.Len(t, msgs, 4)
	assert.Equal(t, llm.RoleUser, msgs[0].Role)
	assert.Equal(t, "q1", msgs[0].Content)
	assert.Equal(t, llm.RoleAssistant, msgs[1].Role)
	assert.Equal(t, "a1", msgs[1].Content)
	assert.Equal(t, llm.RoleUser, msgs[2].Role)
	assert.Equal(t, "q2", msgs[2].Content)
	assert.Equal(t, llm.RoleAssistant, msgs[3].Role)
	assert.Equal(t, "a2", msgs[3].Content)

	// Limit 2 => MOST RECENT two messages (tail cap).
	loop2 := NewAgentLoop("test-session", "/tmp")
	loop2.SetSessionStore(store, config.SessionConfig{RestoreMessageLimit: 2})
	msgs2, err := loop2.conversationRestoreFn("conv-1")
	require.NoError(t, err)
	require.Len(t, msgs2, 2)
	assert.Equal(t, "q2", msgs2[0].Content)
	assert.Equal(t, "a2", msgs2[1].Content)

	// Unknown ID => empty slice, nil error (fresh conversation).
	msgs3, err := loop.conversationRestoreFn("conv-unknown")
	require.NoError(t, err)
	assert.NotNil(t, msgs3)
	assert.Empty(t, msgs3)
}

// TestConversationRestore_PopulatesInOrder drives RunOnce end to end and
// asserts the restored history is model-visible in ascending order before
// the new user message.
func TestConversationRestore_PopulatesInOrder(t *testing.T) {
	store := newFakeRestoreStore()
	seedExchange(store, "conv-restore-1", "earlier question", "earlier answer")
	seedExchange(store, "conv-restore-1", "second question", "second answer")

	loop, chatter := newRestoreTestLoop(t, store, 0,
		&llm.Response{Content: "fresh reply", Model: "mock"},
	)

	resp, err := loop.RunOnce(context.Background(), "follow-up message", "conv-restore-1")
	require.NoError(t, err)
	assert.Equal(t, "fresh reply", resp)

	calls := chatter.allCalls()
	require.Len(t, calls, 1, "expected exactly one LLM call")
	visible := calls[0]

	// The model-visible conversation must contain the restored history in
	// order BEFORE the new user message. Anchors (validation instructions)
	// may precede restored history; find the restored messages and assert
	// relative ordering.
	var contents []string
	for _, m := range visible {
		contents = append(contents, m.Content)
	}
	assert.Contains(t, contents, "earlier question")
	assert.Contains(t, contents, "earlier answer")
	assert.Contains(t, contents, "second question")
	assert.Contains(t, contents, "second answer")
	assert.Contains(t, contents, "follow-up message")

	idx := func(needle string) int {
		for i, c := range contents {
			if c == needle {
				return i
			}
		}
		return -1
	}
	assert.Less(t, idx("earlier question"), idx("earlier answer"), "restored user before its reply")
	assert.Less(t, idx("earlier answer"), idx("second question"), "ascending restored order")
	assert.Less(t, idx("second answer"), idx("follow-up message"), "restored history before the new user message")
}

// TestConversationRestore_LimitTail verifies restore_message_limit=N
// restores only the MOST RECENT N messages.
func TestConversationRestore_LimitTail(t *testing.T) {
	store := newFakeRestoreStore()
	seedExchange(store, "conv-restore-2", "old question", "old answer")
	seedExchange(store, "conv-restore-2", "recent question", "recent answer")

	// Limit 2 => only the most recent exchange is restored.
	loop, chatter := newRestoreTestLoop(t, store, 2,
		&llm.Response{Content: "tail reply", Model: "mock"},
	)

	_, err := loop.RunOnce(context.Background(), "tail follow-up", "conv-restore-2")
	require.NoError(t, err)

	calls := chatter.allCalls()
	require.Len(t, calls, 1)
	var contents []string
	for _, m := range calls[0] {
		contents = append(contents, m.Content)
	}
	assert.NotContains(t, contents, "old question", "limit=2 must drop the oldest exchange")
	assert.NotContains(t, contents, "old answer", "limit=2 must drop the oldest exchange")
	assert.Contains(t, contents, "recent question")
	assert.Contains(t, contents, "recent answer")
	assert.Contains(t, contents, "tail follow-up")
}

// TestConversationRestore_StoreErrorAndEmpty verifies that a store error,
// an empty history, and an unknown ID all degrade to a fresh conversation
// and the turn still succeeds, with the failure logged exactly once.
func TestConversationRestore_StoreErrorAndEmpty(t *testing.T) {
	store := newFakeRestoreStore()
	store.err = errors.New("db unavailable")

	logBuf := newCapturingLogger()
	loop, chatter := newRestoreTestLoop(t, store, 0,
		&llm.Response{Content: "fresh-start reply", Model: "mock"},
	)
	loop.logger = logBuf.log

	resp, err := loop.RunOnce(context.Background(), "turn on error", "conv-restore-err")
	require.NoError(t, err, "restore failure must not break the turn")
	assert.Equal(t, "fresh-start reply", resp)

	calls := chatter.allCalls()
	require.Len(t, calls, 1)
	var contents []string
	for _, m := range calls[0] {
		contents = append(contents, m.Content)
	}
	assert.Contains(t, contents, "turn on error")

	restoreLogs := logBuf.countContaining("conversation restore failed")
	assert.Equal(t, 1, restoreLogs, "restore failure must be logged exactly once, got %d", restoreLogs)

	// Empty history: store reachable but no rows for the ID => fresh
	// conversation, no restore-failure log.
	emptyStore := newFakeRestoreStore()
	loop2, chatter2 := newRestoreTestLoop(t, emptyStore, 0,
		&llm.Response{Content: "empty-store reply", Model: "mock"},
	)
	resp2, err := loop2.RunOnce(context.Background(), "turn on empty", "conv-restore-empty")
	require.NoError(t, err)
	assert.Equal(t, "empty-store reply", resp2)
	calls2 := chatter2.allCalls()
	require.Len(t, calls2, 1)
	assert.Equal(t, "turn on empty", lastUserContent(calls2[0]))

	// Unknown ID with data elsewhere: same fresh-conversation behavior.
	loop3, chatter3 := newRestoreTestLoop(t, store, 0,
		&llm.Response{Content: "unknown-id reply", Model: "mock"},
	)
	resp3, err := loop3.RunOnce(context.Background(), "turn on unknown", "conv-restore-unknown")
	require.NoError(t, err)
	assert.Equal(t, "unknown-id reply", resp3)
	assert.Equal(t, 1, chatter3.callCount())
}

// lastUserContent returns the content of the last user-role message.
func lastUserContent(messages []llm.ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llm.RoleUser {
			return messages[i].Content
		}
	}
	return ""
}

// TestConversationRestore_CacheMissAcrossLoops implements the Task 2
// scenario: loop 1 persists an exchange into the fake store, loop 2 (fresh
// cache, same store) serves a follow-up and must see restored history
// BEFORE the new user message.
func TestConversationRestore_CacheMissAcrossLoops(t *testing.T) {
	store := newFakeRestoreStore()

	// Turn 1: no prior history; RunOnce adds the exchange to the (fresh)
	// conversation and the fake store independently records it as the
	// durable copy (persistExchange's job in production).
	store.seed("conv-restart", []session.Message{
		{SessionID: "conv-restart", Role: "user", Content: "first question", EntryType: "message"},
		{SessionID: "conv-restart", Role: "assistant", Content: "first answer", EntryType: "message"},
	}...)

	loop1, _ := newRestoreTestLoop(t, store, 0,
		&llm.Response{Content: "first answer", Model: "mock"},
	)
	_, err := loop1.RunOnce(context.Background(), "first question", "conv-restart")
	require.NoError(t, err)

	// Turn 2: a NEW loop (simulates daemon restart / cache miss) with the
	// same store. The follow-up must land AFTER the restored history.
	loop2, chatter2 := newRestoreTestLoop(t, store, 0,
		&llm.Response{Content: "second answer", Model: "mock"},
	)
	_, err = loop2.RunOnce(context.Background(), "second question", "conv-restart")
	require.NoError(t, err)

	calls := chatter2.allCalls()
	require.Len(t, calls, 1)
	var contents []string
	for _, m := range calls[0] {
		contents = append(contents, m.Content)
	}
	assert.Contains(t, contents, "first question", "restored history must be model-visible after cache miss")
	assert.Contains(t, contents, "first answer")
	assert.Contains(t, contents, "second question")
	idx := func(needle string) int {
		for i, c := range contents {
			if c == needle {
				return i
			}
		}
		return -1
	}
	assert.Less(t, idx("first answer"), idx("second question"),
		"new user message must land AFTER the restored history")
}

// capturingLogger collects formatted log output for assertion.
type capturingLogger struct {
	log *slog.Logger
	*syncBuffer
}

type syncBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func newCapturingLogger() *capturingLogger {
	cb := &capturingLogger{syncBuffer: &syncBuffer{}}
	cb.log = slog.New(slog.NewTextHandler(cb, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return cb
}

func (c *capturingLogger) countContaining(needle string) int {
	c.syncBuffer.mu.Lock()
	defer c.syncBuffer.mu.Unlock()
	return strings.Count(string(c.buf), needle)
}
