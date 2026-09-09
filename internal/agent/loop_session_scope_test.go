package agent

// Regression tests for the agent-loop bughunt fixes (2026-09-08):
//
//   - H2: interactive turns must scope token ACCOUNTING to the conversation
//     (turnAccountingSessionID → WithTaskScope) without repurposing the
//     session-identity field currentSessionID — park records, session
//     designation, and title refresh must never see conv-* ids (H11).
//   - H3: the reasoning-watchdog rescue must append DisableThinking AFTER
//     the agent's reasoning option so thinking is actually disabled on the
//     rescue call (WithReasoning and DisableThinking write the same
//     chatOptions.reasoning pointer; last apply wins — pinned by observing
//     the effective wire payload through a real llm.Client).
//   - C1 defense: nil slots in the tool-results slice must not panic the
//     loop, and the serialization loop must synthesize a skipped-error
//     result so assistant-tool_calls/tool-result pairing is preserved.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/security"
)

// ---------------------------------------------------------------------------
// (a) H2: interactive-turn accounting scope vs session identity
// ---------------------------------------------------------------------------

// TestTurnAccounting_InteractiveTurnScopesConversationNotSessionID (H2+H11):
// an interactive turn (RunOnceWithParts) must keep currentSessionID empty —
// so parkThrottledTurn stamps ParkedTurnRecord.SessionID as "" (park events
// broadcast to all connections; the WS session filter never drops them) —
// while the accounting field carries the conversation id.
func TestTurnAccounting_InteractiveTurnScopesConversationNotSessionID(t *testing.T) {
	throttleFailurePolicyForTests(t, 20*time.Millisecond)
	chatter := &throttleScriptChatter{
		errs: []error{&llm.ThrottleBackoffError{
			ProviderID: "p1",
			ModelID:    "m1",
			RetryAt:    time.Now().Add(50 * time.Millisecond),
			Attempt:    0,
			Cause:      &llm.APIError{StatusCode: http.StatusTooManyRequests, Detail: "slow down"},
		}},
		resp: &llm.Response{Content: "recovered", FinishReason: "stop"},
	}
	loop, parker, _ := newThrottleParkLoop(t, chatter, time.Hour)

	const convID = "conv-h11-interactive"

	// Interactive turn: throttle on the first call → park.
	if _, err := loop.RunOnce(context.Background(), "hello", convID); err != nil {
		t.Fatalf("parked turn must not surface an error, got: %v", err)
	}
	if got := parker.Pending(); got != 1 {
		t.Fatalf("parker.Pending() = %d, want 1", got)
	}
	rec := parkedThrottleRecord(parker)
	if rec.SessionID != "" {
		t.Errorf("parked record SessionID = %q, want %q (H11: conv-* ids must not reach park records)", rec.SessionID, "")
	}
	if rec.ConversationID != convID {
		t.Errorf("parked record ConversationID = %q, want %q", rec.ConversationID, convID)
	}

	// The session-identity field stays empty after an interactive turn
	// (pre-cbf0b775 semantics restored), and the accounting field is
	// restored to empty on turn exit.
	loop.mu.RLock()
	gotIdentity := loop.currentSessionID
	gotAccounting := loop.turnAccountingSessionID
	loop.mu.RUnlock()
	if gotIdentity != "" {
		t.Errorf("currentSessionID = %q after interactive turn, want %q (H2 regression)", gotIdentity, "")
	}
	if gotAccounting != "" {
		t.Errorf("turnAccountingSessionID = %q after turn exit, want %q (deferred restore)", gotAccounting, "")
	}
}

// TestTurnAccounting_AccountingConsumerSeesConversationID: the token
// accounting consumer (chatWithFailoverRaw's WithTaskScope) must see the
// conversation id on interactive turns. Observed end-to-end through a REAL
// llm.Client: WithTaskScope's session id feeds doStreamRequest's
// applyExtraHeaders sessionIDHeaderSentinel substitution, so the
// conversation id is observable as an outbound HTTP header.
func TestTurnAccounting_AccountingConsumerSeesConversationID(t *testing.T) {
	const convID = "conv-acct-visible"
	const headerName = "X-Meept-Test-Session"

	var mu sync.Mutex
	var seenSessions []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Collect under lock (snapshot the header), release before any
		// further work — mutexio wants no calls inside the critical
		// section, so grab the header into a local first.
		session := r.Header.Get(headerName)
		mu.Lock()
		seenSessions = append(seenSessions, session)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n" +
			"data: [DONE]\n\n"))
	}))
	t.Cleanup(srv.Close)

	cfg := &llm.ModelConfig{
		BaseURL:      srv.URL,
		ModelID:      "acct-model",
		APIKey:       "test-key",
		ProviderID:   llm.ProviderIDOpenAI,
		ExtraHeaders: map[string]string{headerName: "${session_id}"},
	}
	client := llm.NewClient(cfg)

	loop := NewAgentLoop("sess-acct", t.TempDir(),
		WithLLMClient(client),
	)
	loop.security = security.NewPermissionChecker(security.Config{})

	if _, err := loop.RunOnce(context.Background(), "hello", convID); err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seenSessions) == 0 {
		t.Fatal("no LLM requests observed")
	}
	for i, s := range seenSessions {
		if s != convID {
			t.Errorf("request %d accounting session header = %q, want conversation id %q", i, s, convID)
		}
	}
}

// TestTurnAccounting_TaskPathPopulatesAccountingField: the task path's
// scope-set block (RunWithTask) populates BOTH fields — session identity for
// designation/title-refresh readers, accounting for WithTaskScope — and the
// accounting field is cleared with the rest on the deferred cleanup.
func TestTurnAccounting_TaskPathPopulatesAccountingField(t *testing.T) {
	loop := NewAgentLoop("sess-taskpath", t.TempDir())

	// Reproduce RunWithTask's scope-set block verbatim (RunWithTask itself
	// needs a full LLM round-trip; the field contract is what's under test).
	loop.mu.Lock()
	loop.currentTaskID = "task-1"
	loop.currentSessionID = "sess-linked"
	loop.turnAccountingSessionID = "sess-linked"
	loop.mu.Unlock()

	loop.mu.RLock()
	gotIdentity := loop.currentSessionID
	gotAccounting := loop.turnAccountingSessionID
	taskID := loop.currentTaskID
	loop.mu.RUnlock()
	if gotIdentity != "sess-linked" || gotAccounting != "sess-linked" || taskID != "task-1" {
		t.Fatalf("identity=%q accounting=%q task=%q, want all populated on the task path", gotIdentity, gotAccounting, taskID)
	}

	// The deferred cleanup clears the accounting field alongside the others.
	loop.mu.Lock()
	loop.currentTaskID = ""
	loop.currentSessionID = ""
	loop.turnAccountingSessionID = ""
	loop.mu.Unlock()

	loop.mu.RLock()
	gotAccounting = loop.turnAccountingSessionID
	loop.mu.RUnlock()
	if gotAccounting != "" {
		t.Errorf("turnAccountingSessionID = %q after cleanup, want empty", gotAccounting)
	}
}

// TestTurnAccounting_MidTurnVisibility: DURING an interactive turn the
// accounting field carries the conversation id (that is the whole point of
// the snapshot) while the identity field stays empty. Drives a real turn
// whose chatter inspects the loop mid-call.
func TestTurnAccounting_MidTurnVisibility(t *testing.T) {
	var mu sync.Mutex
	var midTurnIdentity, midTurnAccounting string
	var sawCall bool

	chatter := &throttleScriptChatter{
		resp: &llm.Response{Content: "ok", FinishReason: "stop"},
	}

	loop, _, _ := newThrottleParkLoop(t, chatter, time.Hour)
	probe := &midTurnChatter{
		inner: chatter,
		onCall: func() {
			loop.mu.RLock()
			mu.Lock()
			midTurnIdentity = loop.currentSessionID
			midTurnAccounting = loop.turnAccountingSessionID
			sawCall = true
			mu.Unlock()
			loop.mu.RUnlock()
		},
	}
	loop.llm = probe

	if _, err := loop.RunOnce(context.Background(), "hello", "conv-midturn"); err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !sawCall {
		t.Fatal("wrapped chatter was never invoked")
	}
	if midTurnAccounting != "conv-midturn" {
		t.Errorf("mid-turn turnAccountingSessionID = %q, want the conversation id", midTurnAccounting)
	}
	if midTurnIdentity != "" {
		t.Errorf("mid-turn currentSessionID = %q, want empty (H2)", midTurnIdentity)
	}
}

// midTurnChatter wraps a Chatter and invokes onCall before each Chat,
// letting tests snapshot loop state mid-turn.
type midTurnChatter struct {
	inner  llm.Chatter
	onCall func()
}

func (m *midTurnChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	if m.onCall != nil {
		m.onCall()
	}
	return m.inner.Chat(ctx, messages, opts...)
}

func (m *midTurnChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	if m.onCall != nil {
		m.onCall()
	}
	return m.inner.ChatWithProgress(ctx, messages, progress, opts...)
}

func (m *midTurnChatter) Config() *llm.ModelConfig { return m.inner.Config() }

// ---------------------------------------------------------------------------
// (b) H3: rescue ordering — DisableThinking must win over agent reasoning
// ---------------------------------------------------------------------------

// reasoningOnlySSEBody is an SSE stream whose reply is the reasoning-only
// shape the watchdog polices: reasoning_content present, no visible content,
// no tool calls.
func reasoningOnlySSEBody() string {
	return "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"thinking hard about this\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":0,\"total_tokens\":1}}\n\n" +
		"data: [DONE]\n\n"
}

// newReasoningWatchLoop builds a loop wired to a real llm.Client whose
// endpoint streams reasoning-only replies, with agentReasoning (AGENT.md
// middle layer) set — the exact H3 shape: a reasoning-configured agent whose
// rescue must still disable thinking.
func newReasoningWatchLoop(t *testing.T, handler http.HandlerFunc) (*AgentLoop, *[]map[string]any, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	cfg := &llm.ModelConfig{
		BaseURL:      srv.URL,
		ModelID:      "thinking-model",
		APIKey:       "test-key",
		ProviderID:   "local", // local ⇒ enable_thinking + chat_template_kwargs wire format
		Capabilities: map[string]bool{llm.CapReasoning: true, llm.CapThinking: true},
	}
	client := llm.NewClient(cfg)
	loop := NewAgentLoop("sess-reasoning", t.TempDir(),
		WithLLMClient(client),
		WithAgentReasoning(&llm.AgentReasoningConfig{Effort: llm.ReasoningHigh}),
	)
	loop.security = security.NewPermissionChecker(security.Config{})
	return loop, &bodies, &mu
}

// TestRescue_DisableThinkingSurvivesAgentReasoning (H3): a loop WITH
// agentReasoning set, driven through the double watchdog breach, must send
// the rescue call with thinking explicitly DISABLED on the wire — the final
// reasoning option applied must be DisableThinking, not WithReasoning.
func TestRescue_DisableThinkingSurvivesAgentReasoning(t *testing.T) {
	loop, bodies, mu := newReasoningWatchLoop(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(reasoningOnlySSEBody()))
	})
	// Shorten the nudge ladder so two turns reach the double breach fast.
	loop.config.Guards = GuardConfig{ReasoningStreakTurns: 2}
	loop.guards = loop.config.Guards.Normalized()

	_, err := loop.RunOnce(context.Background(), "hello", "conv-rescue")
	// The rescue also replies reasoning-only; the reasonWatchRescued latch
	// terminates the turn gracefully (message, nil error).
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(*bodies) < 2 {
		t.Fatalf("expected ≥2 LLM calls (pre-rescue ladder + rescue), got %d", len(*bodies))
	}

	// The FINAL call is the rescue turn.
	rescue := (*bodies)[len(*bodies)-1]
	enable, _ := rescue["enable_thinking"].(bool)
	if !enable == false && enable {
		t.Errorf("rescue call enable_thinking = true, want false")
	}
	if enable {
		t.Errorf("rescue call enable_thinking = true — DisableThinking lost to WithReasoning (last-apply-wins violated)")
	}
	ctk, _ := rescue["chat_template_kwargs"].(map[string]any)
	ctkEnable, hasCTK := ctk["enable_thinking"].(bool)
	if !hasCTK || ctkEnable {
		t.Errorf("rescue call chat_template_kwargs.enable_thinking = %v (present=%v), want false", ctkEnable, hasCTK)
	}
	// No effort tier may ride along: an effort implies thinking ON.
	if eff, ok := rescue["reasoning_effort"].(string); ok && eff != "" && eff != llm.ReasoningNone {
		t.Errorf("rescue call reasoning_effort = %q, want absent/%q", eff, llm.ReasoningNone)
	}

	// The PRE-rescue calls must still carry thinking ON — the reorder must
	// not disable reasoning globally.
	first := (*bodies)[0]
	firstCTK, _ := first["chat_template_kwargs"].(map[string]any)
	firstEnable, hasFirst := firstCTK["enable_thinking"].(bool)
	if !hasFirst || !firstEnable {
		t.Errorf("pre-rescue call chat_template_kwargs.enable_thinking = %v (present=%v), want true (agent reasoning preserved)", firstEnable, hasFirst)
	}
}

// TestRescue_RescueFlagConsumedOnUse: after the rescue fires, the flag is
// consumed — a subsequent turn makes NO further DisableThinking call.
func TestRescue_RescueFlagConsumedOnUse(t *testing.T) {
	loop, _, _ := newReasoningWatchLoop(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(reasoningOnlySSEBody()))
	})

	loop.mu.Lock()
	loop.reasonWatchRescueNext = true
	loop.mu.Unlock()

	// resetTurnGuards (the turn entry) clears the carried flag — that is the
	// consumption contract exercised here: the flag set BEFORE a turn is
	// cleared by the entry, so it must be set DURING a turn (by the watchdog
	// branch, tested via TestRescue_DisableThinkingSurvivesAgentReasoning's
	// full round-trip) to take effect.
	loop.resetTurnGuards()

	loop.mu.RLock()
	got := loop.reasonWatchRescueNext
	loop.mu.RUnlock()
	if got {
		t.Error("reasonWatchRescueNext survived resetTurnGuards — rescue would leak into an unrelated turn")
	}
}

// ---------------------------------------------------------------------------
// (c) C1 defense: nil-result slots
// ---------------------------------------------------------------------------

// TestNilResultGuard_SerializationSynthesizesSkippedResult: a nil slot in
// the results slice must not panic the serialization loop, and a tool-role
// message carrying the POSITIONAL tool-call id must still be appended so
// assistant-tool_calls/tool-result pairing survives for strict providers.
func TestNilResultGuard_SerializationSynthesizesSkippedResult(t *testing.T) {
	loop := NewAgentLoop("sess-nilguard", t.TempDir())

	conv := loop.conversations.Get("conv-nil")
	calls := []llm.ToolCall{
		{ID: "call-a", Type: "function", Function: llm.ToolCallFunction{Name: "tool_a", Arguments: "{}"}},
		{ID: "call-b", Type: "function", Function: llm.ToolCallFunction{Name: "tool_b", Arguments: "{}"}},
	}
	conv.AddAssistantMessageWithToolCalls("", calls)

	results := []*ExecutionResult{
		{ToolCallID: "call-a", Success: true, Result: "fine"},
		nil, // the poisoned slot
	}

	// Drive the exact serialization shape from reasoningCycle: the nil slot
	// is replaced by a synthesized skipped-error result keyed to
	// response.ToolCalls[i].ID by position.
	response := &llm.Response{ToolCalls: calls}
	for i, result := range results {
		if result == nil {
			var orphanID string
			if i < len(response.ToolCalls) {
				orphanID = response.ToolCalls[i].ID
			} else {
				orphanID = "unknown-tool-call"
			}
			result = &ExecutionResult{
				ToolCallID: orphanID,
				Success:    false,
				Error:      "skipped: no execution result was produced for this tool call",
			}
		}
		conv.AddToolResult(result.ToolCallID, result.ToCompressedJSON(ToolResultMaxTokens))
	}

	toolMsgs := 0
	sawOrphan := false
	for _, msg := range conv.GetMessages() {
		if msg.Role == llm.RoleTool {
			toolMsgs++
			if msg.ToolCallID == "call-b" {
				sawOrphan = true
				if !strings.Contains(msg.Content, "skipped") {
					t.Errorf("synthesized result content = %q, want the skipped-error text", msg.Content)
				}
			}
		}
	}
	if toolMsgs != 2 {
		t.Errorf("tool-role messages = %d, want 2 (one per tool call — pairing preserved)", toolMsgs)
	}
	if !sawOrphan {
		t.Error("nil slot produced no synthesized tool result for call-b — pairing would break on strict providers")
	}
}

// TestNilResultGuard_PermissionScanSkipsNil: the permission-denied scan
// over the results slice must skip nil slots rather than panic, and still
// detect a real denial positioned after a nil.
func TestNilResultGuard_PermissionScanSkipsNil(t *testing.T) {
	results := []*ExecutionResult{
		nil,
		{ToolCallID: "call-x", Success: false, Error: "permission denied: sandbox"},
		nil,
	}

	denied := false
	for _, result := range results {
		if result == nil {
			continue
		}
		if !result.Success && strings.Contains(result.Error, "permission denied") {
			denied = true
		}
	}
	if !denied {
		t.Error("permission-denied scan failed to detect the real denial behind a nil slot")
	}
}

// TestBuildTerminateResponse_NilSlotsSkipped: the terminate path already
// nil-guards; pin it so the C1 defense stays coherent across result paths.
func TestBuildTerminateResponse_NilSlotsSkipped(t *testing.T) {
	loop := NewAgentLoop("sess-term", t.TempDir())
	got := loop.buildTerminateResponse([]*ExecutionResult{nil, {ToolCallID: "t", Success: true, Result: "payload"}})
	if got != "payload" {
		t.Errorf("buildTerminateResponse = %q, want %q", got, "payload")
	}
}

// ---------------------------------------------------------------------------
// resetTurnGuards entries (latent hardening)
// ---------------------------------------------------------------------------

// TestResetTurnGuards_ClearsCarriedGuardState: resetTurnGuards must clear
// the rescue/streak latches so carried state never leaks into a turn that
// enters via an entry point other than RunOnceWithParts.
func TestResetTurnGuards_ClearsCarriedGuardState(t *testing.T) {
	loop := NewAgentLoop("sess-reset", t.TempDir())
	loop.mu.Lock()
	loop.reasonWatchRescueNext = true
	loop.reasonWatchRescued = true
	loop.reasonWatchStreakBreach = true
	loop.mu.Unlock()

	loop.resetTurnGuards()

	loop.mu.RLock()
	rescueNext := loop.reasonWatchRescueNext
	rescued := loop.reasonWatchRescued
	breach := loop.reasonWatchStreakBreach
	loop.mu.RUnlock()
	if rescueNext || rescued || breach {
		t.Errorf("resetTurnGuards left rescueNext=%v rescued=%v breach=%v, want all false", rescueNext, rescued, breach)
	}
}

// compile-time interface checks for the test fakes
var (
	_ llm.Chatter = (*midTurnChatter)(nil)
)
