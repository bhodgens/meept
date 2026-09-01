package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	pkgsecurity "github.com/caimlas/meept/pkg/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Stubs
// ---------------------------------------------------------------------------

// hookTestChatter is a recording llm.Chatter stub: it captures the message
// slices each Chat call receives and returns a canned final response after
// every call (so one LLM call = one completed turn).
type hookTestChatter struct {
	mu        sync.Mutex
	messages  [][]llm.ChatMessage // per-call captured message slices
	callCount int
}

func newHookTestChatter() *hookTestChatter {
	return &hookTestChatter{}
}

func (m *hookTestChatter) Chat(_ context.Context, messages []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	m.mu.Lock()
	m.messages = append(m.messages, messages)
	m.callCount++
	m.mu.Unlock()
	return &llm.Response{
		Content:      "hook test done",
		FinishReason: "stop",
		Usage:        llm.TokenUsage{TotalTokens: 5},
	}, nil
}

func (m *hookTestChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *hookTestChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "hook-test-model"}
}

// callMessages returns the messages captured on the Nth LLM call (1-based).
func (m *hookTestChatter) callMessages(n int) []llm.ChatMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n < 1 || n > len(m.messages) {
		return nil
	}
	return m.messages[n-1]
}

func (m *hookTestChatter) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// hookStubHook is a scripted PrepareNextTurnHook: each invocation pops the
// next modification from the queue (last entry repeats when exhausted).
type hookStubHook struct {
	mu   sync.Mutex
	mods []TurnModification
	seen []TurnState
}

func newHookStubHook(mods ...TurnModification) *hookStubHook {
	return &hookStubHook{mods: mods}
}

func (h *hookStubHook) PrepareNextTurn(_ context.Context, state TurnState) TurnModification {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seen = append(h.seen, state)
	if len(h.mods) == 0 {
		return TurnModification{}
	}
	mod := h.mods[0]
	if len(h.mods) > 1 {
		h.mods = h.mods[1:]
	}
	return mod
}

func (h *hookStubHook) callCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.seen)
}

func (h *hookStubHook) lastSeen() (TurnState, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.seen) == 0 {
		return TurnState{}, false
	}
	return h.seen[len(h.seen)-1], true
}

// panickingHook panics unconditionally from PrepareNextTurn.
type panickingHook struct{ panicked bool }

func (p *panickingHook) PrepareNextTurn(context.Context, TurnState) TurnModification {
	p.panicked = true
	panic("hook exploded")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newHookPipelineLoop builds a minimal AgentLoop with the given chatter and
// hook registry, mirroring the construction used by the guards integration
// tests (guards_test.go).
func newHookPipelineLoop(chatter llm.Chatter, reg *HookRegistry) *AgentLoop {
	secChecker := pkgsecurity.NewPermissionChecker(pkgsecurity.Config{})
	return NewAgentLoop("test-session", "/tmp",
		WithLLMChatter(chatter),
		WithToolRegistry(NewPlaceholderToolRegistry()),
		WithSecurityChecker(secChecker),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
		WithHookRegistry(reg),
		WithAgentConfig(AgentConfig{MaxIterations: 5}),
	)
}

func hookNudgeMod(text string) TurnModification {
	return TurnModification{
		Modified:      true,
		ExtraMessages: []llm.ChatMessage{{Role: llm.RoleUser, Content: text}},
		Reason:        "hook test nudge",
	}
}

func containsMessage(msgs []llm.ChatMessage, needle string) bool {
	for _, m := range msgs {
		if strings.Contains(m.Content, needle) {
			return true
		}
	}
	return false
}

func countUserText(msgs []llm.ChatMessage, needle string) int {
	n := 0
	for _, m := range msgs {
		if strings.Contains(m.Content, needle) {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// (a) ExtraMessages: hook returns ExtraMessages -> they are prepended to the
// message slice the stub LLM client receives on the next call.
func TestHookPipeline_ExtraMessagesPrepended(t *testing.T) {
	chatter := newHookTestChatter()
	reg := NewHookRegistry(nil)
	reg.RegisterPrepareNextTurn("stub", HookPriorityNormal, newHookStubHook(
		hookNudgeMod("HOOK-INJECTED-NUDGE"),
	))
	loop := newHookPipelineLoop(chatter, reg)

	_, err := loop.RunOnce(context.Background(), "do a thing", "conv-hooks-a")
	require.NoError(t, err)

	require.GreaterOrEqual(t, chatter.count(), 1, "LLM must be called at least once")
	msgs := chatter.callMessages(1)
	require.NotNil(t, msgs)
	assert.True(t, containsMessage(msgs, "HOOK-INJECTED-NUDGE"),
		"hook ExtraMessages must appear in the messages the LLM client receives")
}

// (b) ModelOverride: hook returns ModelOverride -> the existing one-shot
// override seam is used; the consumption block clears it after the cycle.
// NOTE: the full end-to-end variant is TestHookPipeline_ModelOverrideWithResolver
// below, which uses a real llm.Client — the production consumption block
// (loop.go GetModelOverride site) requires a concrete llmClient, so a mock
// chatter alone cannot exercise the consume+clear half. The helper-level
// unit test TestApplyTurnModification_ModelOverrideOnly pins the routing
// through SetModelOverride.

// (b2) ModelOverride with a resolver + real llm.Client: the hook-returned
// override resolves through the existing GetModelOverride consumption path
// and the client actually SWITCHES — the server observes the overridden
// model ID on the wire.
func TestHookPipeline_ModelOverrideWithResolver(t *testing.T) {
	var mu sync.Mutex
	servedModels := make([]string, 0, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &payload)
		mu.Lock()
		servedModels = append(servedModels, payload.Model)
		mu.Unlock()
		// The loop streams through llm.Client.ChatWithDeltaCallback, so the
		// stub must answer with SSE chunks.
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"hook \"}}]}\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"test done\"},\"finish_reason\":null}]}\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":3,\"total_tokens\":4}}\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		fl.Flush()
	}))
	t.Cleanup(srv.Close)

	cfg := &llm.ProvidersConfig{
		Providers: map[string]llm.ProviderConfig{
			"prov": {
				API:    "http",
				Models: map[string]llm.ModelDef{"alpha": {Name: "alpha"}, "beta": {Name: "beta"}},
				Options: llm.ProviderOptionsConfig{
					BaseURL: srv.URL,
				},
			},
		},
	}
	resolver := llm.NewResolver(cfg, nil)
	reg := NewHookRegistry(nil)
	reg.RegisterPrepareNextTurn("stub", HookPriorityNormal, newHookStubHook(
		TurnModification{Modified: true, ModelOverride: "prov/beta"},
	))

	alphaCfg := resolver.ResolveRef("prov/alpha")
	require.NotNil(t, alphaCfg, "test fixture model must resolve")
	client := llm.NewClient(alphaCfg)

	secChecker := pkgsecurity.NewPermissionChecker(pkgsecurity.Config{})
	loop := NewAgentLoop("test-session", "/tmp",
		WithLLMChatter(client),
		WithToolRegistry(NewPlaceholderToolRegistry()),
		WithSecurityChecker(secChecker),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
		WithHookRegistry(reg),
		WithResolver(resolver),
		WithAgentConfig(AgentConfig{MaxIterations: 5}),
	)

	_, err := loop.RunOnce(context.Background(), "do a thing", "conv-hooks-b2")
	require.NoError(t, err)

	// The one-shot override was applied through SetModelOverride and cleared
	// by the existing consumption block.
	assert.Empty(t, loop.GetModelOverride(),
		"hook-provided override must route through the one-shot seam and be consumed")

	// The LLM must have been reached with the OVERRIDDEN model, not alpha.
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, servedModels, "real LLM client must have reached the test server")
	assert.Equal(t, "beta", servedModels[0],
		"first LLM call must run on the hook-overridden model (observed: %v)", servedModels)
}

// (c) Zero-value modification: nothing changes — no extra messages, no
// override, no duplicated content.
func TestHookPipeline_ZeroValueModificationIsNoOp(t *testing.T) {
	chatter := newHookTestChatter()
	reg := NewHookRegistry(nil)
	reg.RegisterPrepareNextTurn("stub", HookPriorityNormal, newHookStubHook(
		TurnModification{},
	))
	loop := newHookPipelineLoop(chatter, reg)

	_, err := loop.RunOnce(context.Background(), "do a thing", "conv-hooks-c")
	require.NoError(t, err)

	require.GreaterOrEqual(t, chatter.count(), 1)
	msgs := chatter.callMessages(1)
	require.NotNil(t, msgs)
	assert.False(t, containsMessage(msgs, "HOOK-INJECTED-NUDGE"),
		"zero-value modification must not inject messages")
	assert.Empty(t, loop.GetModelOverride(),
		"zero-value modification must not set an override")
	assert.Equal(t, 1, countUserText(msgs, "do a thing"),
		"user message must not be duplicated")
}

// (d) Panicking hook: the turn completes normally, the panic does not escape,
// and a warning is logged (asserted via the captured logger).
func TestHookPipeline_PanickingHookDoesNotKillTurn(t *testing.T) {
	var logBuf strings.Builder
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	chatter := newHookTestChatter()
	reg := NewHookRegistry(nil)
	reg.RegisterPrepareNextTurn("panic-hook", HookPriorityNormal, &panickingHook{})
	secChecker := pkgsecurity.NewPermissionChecker(pkgsecurity.Config{})
	loop := NewAgentLoop("test-session", "/tmp",
		WithLLMChatter(chatter),
		WithToolRegistry(NewPlaceholderToolRegistry()),
		WithSecurityChecker(secChecker),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
		WithHookRegistry(reg),
		WithLoopLogger(logger),
		WithAgentConfig(AgentConfig{MaxIterations: 5}),
	)

	resp, err := loop.RunOnce(context.Background(), "do a thing", "conv-hooks-d")
	require.NoError(t, err, "a panicking hook must not kill the turn")
	assert.NotEmpty(t, resp)

	assert.Contains(t, logBuf.String(), "prepare_next_turn hook panicked",
		"hook panic must be logged via the loop logger")
}

// (e) Regression: NO hooks registered -> behavior unchanged.
func TestHookPipeline_NoHooksRegression(t *testing.T) {
	chatter := newHookTestChatter()
	reg := NewHookRegistry(nil) // empty registry
	loop := newHookPipelineLoop(chatter, reg)

	resp, err := loop.RunOnce(context.Background(), "do a thing", "conv-hooks-e")
	require.NoError(t, err)
	assert.Equal(t, "hook test done", resp)

	require.Equal(t, 1, chatter.count(), "single-turn: exactly one LLM call")
	msgs := chatter.callMessages(1)
	require.NotNil(t, msgs)
	assert.False(t, containsMessage(msgs, "HOOK-INJECTED-NUDGE"))
	assert.Equal(t, 1, countUserText(msgs, "do a thing"),
		"no message duplication without hooks")
	assert.Empty(t, loop.GetModelOverride())
}

// (f) Legacy confirmation: a VerificationAutoTrigger-shaped hook that returns
// the nudge modification -> the nudge lands in the next call's messages.
func TestHookPipeline_VerificationNudgeLands(t *testing.T) {
	chatter := newHookTestChatter()
	reg := NewHookRegistry(nil)

	// Stub shaped like VerificationAutoTrigger.PrepareNextTurn's escalation
	// return (verification_hook.go handleFail).
	verifierShaped := newHookStubHook(TurnModification{
		Modified: true,
		ExtraMessages: []llm.ChatMessage{{
			Role:    llm.RoleUser,
			Content: "Adversarial verification failed after 3 fix attempts. Manual review needed.",
		}},
		Reason: "verification escalation",
	})
	reg.RegisterPrepareNextTurn("verification_auto_trigger_stub", HookPriorityNormal, verifierShaped)

	loop := newHookPipelineLoop(chatter, reg)

	_, err := loop.RunOnce(context.Background(), "do a thing", "conv-hooks-f")
	require.NoError(t, err)

	require.GreaterOrEqual(t, chatter.count(), 1)
	msgs := chatter.callMessages(1)
	require.NotNil(t, msgs)
	assert.True(t, containsMessage(msgs, "Adversarial verification failed"),
		"verification nudge must land in the next LLM call's messages")
}

// ---------------------------------------------------------------------------
// applyTurnModification unit tests (the helper in isolation)
// ---------------------------------------------------------------------------

func TestApplyTurnModification_ZeroValueIsTrueNoOp(t *testing.T) {
	loop := NewAgentLoop("test-session", "/tmp")

	loop.applyTurnModification(TurnModification{})

	assert.Nil(t, loop.pendingHookMessages, "zero-value mod must not allocate pending messages")
	assert.Empty(t, loop.GetModelOverride(), "zero-value mod must not set an override")
}

func TestApplyTurnModification_ExtraMessagesPending(t *testing.T) {
	loop := NewAgentLoop("test-session", "/tmp")
	loop.applyTurnModification(TurnModification{
		Modified:      true,
		ExtraMessages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "extra"}},
	})
	require.Len(t, loop.pendingHookMessages, 1)
	assert.Equal(t, "extra", loop.pendingHookMessages[0].Content)
	assert.Empty(t, loop.GetModelOverride())
}

func TestApplyTurnModification_ModelOverrideOnly(t *testing.T) {
	loop := NewAgentLoop("test-session", "/tmp")
	loop.applyTurnModification(TurnModification{
		Modified:      true,
		ModelOverride: "prov/model-x",
	})
	assert.Equal(t, "prov/model-x", loop.GetModelOverride())
	assert.Nil(t, loop.pendingHookMessages)
}

// ---------------------------------------------------------------------------
// TurnState contract test: the hook fires once per iteration, sees the
// windowed messages, and carries the loop's current model ref.
// ---------------------------------------------------------------------------

func TestHookPipeline_TurnStateCarriesMessagesAndModelRef(t *testing.T) {
	chatter := newHookTestChatter()
	stub := newHookStubHook() // returns zero-value: pure observer
	reg := NewHookRegistry(nil)
	reg.RegisterPrepareNextTurn("observer", HookPriorityNormal, stub)

	secChecker := pkgsecurity.NewPermissionChecker(pkgsecurity.Config{})
	loop := NewAgentLoop("test-session", "/tmp",
		WithLLMChatter(chatter),
		WithToolRegistry(NewPlaceholderToolRegistry()),
		WithSecurityChecker(secChecker),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
		WithHookRegistry(reg),
		WithModelRef("prov/alpha"),
		WithAgentConfig(AgentConfig{MaxIterations: 5}),
	)

	_, err := loop.RunOnce(context.Background(), "turn state probe", "conv-hooks-ts")
	require.NoError(t, err)

	require.Equal(t, 1, stub.callCount(), "hook must fire exactly once per single-iteration turn")
	seen, ok := stub.lastSeen()
	require.True(t, ok)
	assert.Equal(t, "prov/alpha", seen.ModelRef, "TurnState.ModelRef must carry l.modelRef")
	assert.NotEmpty(t, seen.Messages, "TurnState.Messages must carry the windowed messages")
	assert.Equal(t, 1, seen.Iteration)
	assert.True(t, containsMessage(seen.Messages, "turn state probe"),
		"TurnState.Messages must contain the turn's user message")
}
