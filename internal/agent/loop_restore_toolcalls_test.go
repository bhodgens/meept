package agent

// Leaf 02 (wiring-followups-final): restore-with-tool-calls.
// buildConversationRestoreFn must re-attach persisted tool calls
// (GetToolCallsForMessages joined by message_id) to assistant ChatMessages
// during restore. These tests FAIL against the pre-leaf restore fn, which
// rebuilds role+content only (RED witnessed).

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// toolCallRestoreStore extends fakeRestoreStore with the narrow
// GetToolCallsForMessages capability the restore fn consults (satisfied
// structurally by session.SQLiteStore; the fake mirrors the shape).
type toolCallRestoreStore struct {
	fakeRestoreStore
	callsByID   map[int64][]session.ToolCall
	fetchErr    error
	fetchCalled int
}

func (f *toolCallRestoreStore) GetToolCallsForMessages(messageIDs []int64) (map[int64][]session.ToolCall, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetchCalled++
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	out := make(map[int64][]session.ToolCall, len(messageIDs))
	for _, id := range messageIDs {
		if tcs, ok := f.callsByID[id]; ok {
			out[id] = tcs
		}
	}
	return out, nil
}

// newToolCallRestoreLoop wires the loop with the richer store; the restore
// fn must only use the tool-call join when the reader carries it.
func newToolCallRestoreLoop(t *testing.T, store *toolCallRestoreStore, limit int, responses ...*llm.Response) (*AgentLoop, *captureChatter) {
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

// TestConversationRestore_AttachesToolCalls pins T1/T2's core: a persisted
// exchange whose assistant row carries two tool calls restores BOTH on the
// assistant ChatMessage (message_id join, seq order, llm.ToolCalls shape).
func TestConversationRestore_AttachesToolCalls(t *testing.T) {
	store := &toolCallRestoreStore{fakeRestoreStore: *newFakeRestoreStore(), callsByID: map[int64][]session.ToolCall{}}
	store.seed("conv-tc-restore",
		session.Message{ID: 101, SessionID: "conv-tc-restore", Role: "user", Content: "clean the tmp dir", EntryType: "message"},
		session.Message{ID: 102, SessionID: "conv-tc-restore", Role: "assistant", Content: "", EntryType: "message"},
	)
	store.callsByID[102] = []session.ToolCall{
		{MessageID: 102, ToolCallID: "call-a", ToolName: "shell", Arguments: `{"command":"ls /tmp"}`, Seq: 0},
		{MessageID: 102, ToolCallID: "call-b", ToolName: "shell", Arguments: `{"command":"rm -rf /tmp/x"}`, Seq: 1},
	}

	loop, _ := newToolCallRestoreLoop(t, store, 0, &llm.Response{Content: "ok", Model: "mock"})
	require.NotNil(t, loop.conversationRestoreFn)

	msgs, err := loop.conversationRestoreFn("conv-tc-restore")
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assistant := msgs[1]
	assert.Equal(t, llm.RoleAssistant, assistant.Role)
	require.Len(t, assistant.ToolCalls, 2, "restored assistant message lost its tool calls")
	assert.Equal(t, "call-a", assistant.ToolCalls[0].ID)
	assert.Equal(t, "shell", assistant.ToolCalls[0].Function.Name)
	assert.Equal(t, `{"command":"ls /tmp"}`, assistant.ToolCalls[0].Function.Arguments)
	assert.Equal(t, "call-b", assistant.ToolCalls[1].ID)
	assert.Equal(t, 1, store.fetchCalled, "restore must consult the tool-call join")
}

// TestConversationRestore_ToolCallsModelVisible pins the end-to-end half of
// T2: after restore, the fake-LLM request's rebuilt context carries the
// assistant tool-call record (the model sees what it once called).
func TestConversationRestore_ToolCallsModelVisible(t *testing.T) {
	store := &toolCallRestoreStore{fakeRestoreStore: *newFakeRestoreStore(), callsByID: map[int64][]session.ToolCall{}}
	store.seed("conv-tc-e2e",
		session.Message{ID: 201, SessionID: "conv-tc-e2e", Role: "user", Content: "run the deploy", EntryType: "message"},
		session.Message{ID: 202, SessionID: "conv-tc-e2e", Role: "assistant", Content: "", EntryType: "message"},
	)
	store.callsByID[202] = []session.ToolCall{
		{MessageID: 202, ToolCallID: "call-e2e", ToolName: "deploy", Arguments: `{"target":"staging"}`, Seq: 0},
	}

	loop, chatter := newToolCallRestoreLoop(t, store, 0, &llm.Response{Content: "done", Model: "mock"})
	_, err := loop.RunOnce(context.Background(), "did it finish?", "conv-tc-e2e")
	require.NoError(t, err)

	calls := chatter.allCalls()
	require.Len(t, calls, 1)
	var found bool
	for _, m := range calls[0] {
		if m.Role == llm.RoleAssistant {
			for _, tc := range m.ToolCalls {
				if tc.ID == "call-e2e" && tc.Function.Name == "deploy" {
					found = true
				}
			}
		}
	}
	assert.True(t, found, "model-visible context lost the restored tool-call record")
}

// TestConversationRestore_ToolCallFetchFailureBestEffort pins the
// best-effort rule: a join fetch error NEVER fails restore - messages come
// back without tool calls and the turn proceeds.
func TestConversationRestore_ToolCallFetchFailureBestEffort(t *testing.T) {
	store := &toolCallRestoreStore{
		fakeRestoreStore: *newFakeRestoreStore(),
		fetchErr:         errPersistInjected,
	}
	store.seed("conv-tc-err",
		session.Message{ID: 301, SessionID: "conv-tc-err", Role: "user", Content: "q", EntryType: "message"},
		session.Message{ID: 302, SessionID: "conv-tc-err", Role: "assistant", Content: "a", EntryType: "message"},
	)

	loop, chatter := newToolCallRestoreLoop(t, store, 0, &llm.Response{Content: "fine", Model: "mock"})
	resp, err := loop.RunOnce(context.Background(), "still there?", "conv-tc-err")
	require.NoError(t, err, "tool-call fetch failure must not break the turn")
	assert.Equal(t, "fine", resp)

	calls := chatter.allCalls()
	require.Len(t, calls, 1)
	var contents []string
	for _, m := range calls[0] {
		contents = append(contents, m.Content)
	}
	assert.Contains(t, contents, "q", "restored history survives the join failure")
	assert.Contains(t, contents, "still there?")
}

// TestConversationRestore_NoToolCallCapabilitySkips pins the narrow-reader
// shape: a reader WITHOUT GetToolCallsForMessages restores exactly as
// before (no panic, no calls, no tool calls).
func TestConversationRestore_NoToolCallCapabilitySkips(t *testing.T) {
	store := newFakeRestoreStore()
	store.seed("conv-no-cap",
		session.Message{ID: 401, SessionID: "conv-no-cap", Role: "user", Content: "q1", EntryType: "message"},
		session.Message{ID: 402, SessionID: "conv-no-cap", Role: "assistant", Content: "a1", EntryType: "message"},
	)

	loop := NewAgentLoop("test-session", "/tmp")
	loop.SetSessionStore(store, config.SessionConfig{RestoreMessageLimit: 0})
	require.NotNil(t, loop.conversationRestoreFn)

	msgs, err := loop.conversationRestoreFn("conv-no-cap")
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Empty(t, msgs[1].ToolCalls)
}
