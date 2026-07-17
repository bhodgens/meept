package eval

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
)

// mockLLMClient is a minimal LLMClient for tests.
type mockLLMClient struct {
	Respond func(messages []ChatMessage) (*ChatResult, error)
}

func (m *mockLLMClient) Chat(messages []ChatMessage) (*ChatResult, error) {
	if m.Respond != nil {
		return m.Respond(messages)
	}
	return &ChatResult{Content: "mock response"}, nil
}

// freshStore creates an empty InMemoryTraceStore for testing.
// (Cannot populate spans from here since spanViewData is unexported.)
func freshStore() *agent.InMemoryTraceStore {
	return agent.NewInMemoryTraceStore()
}

// -----------------------------------------------------------------------
// TestConversationSession_BasicFlow
// -----------------------------------------------------------------------

func TestConversationSession_BasicFlow(t *testing.T) {
	config := &SessionConfig{
		MaxTurns: 3,
		Mode:     ModeDiscovery,
	}
	session := NewConversationSession(config)

	if session.GetState() != StateIdle {
		t.Errorf("expected idle state, got %s", session.GetState())
	}
	if session.GetSessionID() == "" {
		t.Error("expected non-empty session ID")
	}
	if session.GetTurnCount() != 0 {
		t.Errorf("expected 0 turns, got %d", session.GetTurnCount())
	}

	store := freshStore()
session.Start(store)

	if session.GetState() != StateIdle {
		t.Errorf("expected idle after start, got %s", session.GetState())
	}

	resp, done, err := session.ProcessTurn("What traces are available?")
	if err != nil {
		t.Fatalf("ProcessTurn failed: %v", err)
	}
	if done {
		t.Error("expected not done after first turn")
	}
	if resp == "" {
		t.Error("expected non-empty response")
	}
	if session.GetTurnCount() != 1 {
		t.Errorf("expected 1 turn, got %d", session.GetTurnCount())
	}

	resp2, done2, err2 := session.ProcessTurn("Tell me about failures.")
	if err2 != nil {
		t.Fatalf("ProcessTurn failed: %v", err2)
	}
	if done2 {
		t.Error("expected not done after second turn")
	}
	if resp2 == "" {
		t.Error("expected non-empty second response")
	}

	_, done3, err3 := session.ProcessTurn("Summarize.")
	if err3 != nil {
		t.Fatalf("ProcessTurn failed: %v", err3)
	}
	if !done3 {
		t.Error("expected done after third turn with MaxTurns=3")
	}

	_, done4, err4 := session.ProcessTurn("one more")
	if !done4 {
		t.Error("expected done for post-complete turn")
	}
	if err4 == nil {
		t.Error("expected error for post-complete turn")
	}

	history := session.GetHistory()
	if len(history) != 3 {
		t.Errorf("expected 3 turns in history, got %d", len(history))
	}
	for i, turn := range history {
		if turn.Index != i+1 {
			t.Errorf("turn %d has wrong index %d", i, turn.Index)
		}
		if turn.TurnID == "" {
			t.Errorf("turn %d has empty turn ID", i)
		}
	}
}

// -----------------------------------------------------------------------
// TestConversationSession_StateTransitions
// -----------------------------------------------------------------------

func TestConversationSession_StateTransitions(t *testing.T) {
	session := NewConversationSession(DefaultSessionConfig())

	if got := session.GetState(); got != StateIdle {
		t.Errorf("initial state: want idle, got %s", got)
	}

	session.Start(freshStore())
	if got := session.GetState(); got != StateIdle {
		t.Errorf("after start: want idle, got %s", got)
	}

	_, _, err := session.ProcessTurn("hello")
	if err != nil {
		t.Fatalf("ProcessTurn: %v", err)
	}
	if got := session.GetState(); got != StateIdle {
		t.Errorf("after process: want idle, got %s", got)
	}

	session.Close()
	if got := session.GetState(); got != StateComplete {
		t.Errorf("after close: want complete, got %s", got)
	}
}

// -----------------------------------------------------------------------
// TestConversationSession_ModeSwitching
// -----------------------------------------------------------------------

func TestConversationSession_ModeSwitching(t *testing.T) {
	config := &SessionConfig{MaxTurns: 15}
	session := NewConversationSession(config)
	session.Start(freshStore())

	// Turns 1-3: discovery
	for i := 1; i <= 3; i++ {
		_, _, err := session.ProcessTurn(fmt.Sprintf("query %d", i))
		if err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
		if got := session.GetCurrentMode(); got != ModeDiscovery {
			t.Errorf("after turn %d, mode = %s, want %s", i, got, ModeDiscovery)
		}
	}

	// Turn 4: switches to surgical
	_, _, err := session.ProcessTurn("query 4")
	if err != nil {
		t.Fatalf("turn 4: %v", err)
	}
	if got := session.GetCurrentMode(); got != ModeSurgical {
		t.Errorf("after turn 4, mode = %s, want %s", got, ModeSurgical)
	}

	// Turns 5-10: stay surgical
	for i := 5; i <= 10; i++ {
		_, _, err := session.ProcessTurn(fmt.Sprintf("query %d", i))
		if err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
		if got := session.GetCurrentMode(); got != ModeSurgical {
			t.Errorf("after turn %d, mode = %s, want %s", i, got, ModeSurgical)
		}
	}

	// Turn 11: switches to synthesis
	_, _, err = session.ProcessTurn("query 11")
	if err != nil {
		t.Fatalf("turn 11: %v", err)
	}
	if got := session.GetCurrentMode(); got != ModeSynthesis {
		t.Errorf("after turn 11, mode = %s, want %s", got, ModeSynthesis)
	}

	// Turns 12-14: remain synthesis
	for i := 12; i <= 14; i++ {
		_, _, err := session.ProcessTurn(fmt.Sprintf("query %d", i))
		if err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
		if got := session.GetCurrentMode(); got != ModeSynthesis {
			t.Errorf("after turn %d, mode = %s, want %s", i, got, ModeSynthesis)
		}
	}

	// Turn 15: last with MaxTurns=15, synthesis + done
	_, done, err := session.ProcessTurn("query 15")
	if err != nil {
		t.Fatalf("turn 15: %v", err)
	}
	if !done {
		t.Error("expected done on turn 15")
	}
	if session.GetState() != StateComplete {
		t.Errorf("expected complete state, got %s", session.GetState())
	}
}

// -----------------------------------------------------------------------
// TestConversationSession_ManualModeSwitch
// -----------------------------------------------------------------------

func TestConversationSession_ManualModeSwitch(t *testing.T) {
	config := &SessionConfig{
		MaxTurns: 20,
		Mode:     ModeSurgical, // pinned
	}
	session := NewConversationSession(config)
	session.Start(freshStore())

	for i := 1; i <= 5; i++ {
		_, _, err := session.ProcessTurn(fmt.Sprintf("q %d", i))
		if err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
		if session.GetCurrentMode() != ModeSurgical {
			t.Errorf("after turn %d, mode = %s, want pinned ModeSurgical", i, session.GetCurrentMode())
		}
	}

	session.SwitchTo(ModeSynthesis)
	if got := session.GetCurrentMode(); got != ModeSynthesis {
		t.Errorf("after SwitchTo, mode = %s, want synthesis", got)
	}

	session.Close()
	session.SwitchTo(ModeDiscovery)
	if got := session.GetCurrentMode(); got != ModeSynthesis {
		t.Errorf("after SwitchTo on complete, mode = %s, should stay synthesis", got)
	}
}

// -----------------------------------------------------------------------
// TestConversationSession_MaxTurnsEnforcement
// -----------------------------------------------------------------------

func TestConversationSession_MaxTurnsEnforcement(t *testing.T) {
	tests := []struct {
		name      string
		maxTurns  int
		wantTurns int
	}{
		{"max 1", 1, 1},
		{"max 5", 5, 5},
		{"max 10", 10, 10},
		{"unlimited 0", 0, 3}, // no cap; we only do 3 in the test
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &SessionConfig{MaxTurns: tt.maxTurns}
			session := NewConversationSession(config)
			session.Start(freshStore())

			for i := 0; i < tt.wantTurns; i++ {
				_, done, err := session.ProcessTurn(fmt.Sprintf("q %d", i+1))
				if err != nil {
					t.Fatalf("turn %d: unexpected error: %v", i+1, err)
				}
				if done && i < tt.wantTurns-1 {
					t.Errorf("turn %d: unexpectedly done", i+1)
				}
			}

			// Next turn should be done/error at max turns
			_, done, err := session.ProcessTurn("over limit")
			if tt.maxTurns > 0 && (err == nil) {
				if !done {
					t.Errorf("expected done/error at max turns")
				}
			}

			if tt.maxTurns > 0 && session.GetTurnCount() != tt.wantTurns {
				t.Errorf("turn count = %d, want %d", session.GetTurnCount(), tt.wantTurns)
			}
			if tt.maxTurns > 0 && session.GetState() != StateComplete {
				t.Errorf("state = %s, want complete", session.GetState())
			}
		})
	}
}

// -----------------------------------------------------------------------
// TestConversationSession_TraceStoreIntegration
// -----------------------------------------------------------------------

func TestConversationSession_TraceStoreIntegration(t *testing.T) {
	store := freshStore()

	// Populate the store with 3 trace spans via JSONL so QueryTraces finds them.
	tmp, err := os.CreateTemp("", "traces-*.jsonl")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	for i := 1; i <= 3; i++ {
		hasErr := ""
		if i == 3 {
			hasErr = "true"
		} else {
			hasErr = "false"
		}
		_, _ = fmt.Fprintf(tmp, `{"span_id":"span-%d","trace_id":"trace-%d","span_name":"test-operation","has_error":%s}`+"\n",
			i, i, hasErr)
	}
	tmp.Close()
	if err := agent.LoadJSONLTraces(store, tmp.Name()); err != nil {
		t.Fatalf("LoadJSONLTraces: %v", err)
	}
	os.Remove(tmp.Name())
	defer os.Remove(tmp.Name())

	config := DefaultSessionConfig()
	session := NewConversationSession(config)
	session.Start(store)

	ctx := context.Background()
	traceIDs, failureModes, err := session.QueryTraces(ctx)
	if err != nil {
		t.Fatalf("QueryTraces failed: %v", err)
	}
	if len(traceIDs) != 3 {
		t.Errorf("expected 3 trace IDs, got %d", len(traceIDs))
	}
	_ = failureModes

	resp, _, err := session.ProcessTurn("List all traces.")
	if err != nil {
		t.Fatalf("ProcessTurn failed: %v", err)
	}
	if resp == "" {
		t.Error("expected non-empty response")
	}

	h := session.GetHistory()
	if len(h) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(h))
	}
	if h[0].Mode != "discovery" {
		t.Errorf("expected discovery mode, got %s", h[0].Mode)
	}
}

// -----------------------------------------------------------------------
// TestConversationSession_NoTraceStore
// -----------------------------------------------------------------------

func TestConversationSession_NoTraceStore(t *testing.T) {
	session := NewConversationSession(DefaultSessionConfig())

	_, _, err := session.ProcessTurn("hello")
	if err != nil {
		t.Fatalf("ProcessTurn without store failed: %v", err)
	}

	_, _, err = session.QueryTraces(context.Background())
	if err == nil {
		t.Error("expected error from QueryTraces without store")
	}
}

// -----------------------------------------------------------------------
// TestConversationSession_LLMClient
// -----------------------------------------------------------------------

func TestConversationSession_LLMClient(t *testing.T) {
	expectedResp := "This is a generated analysis response."
	mockLLM := &mockLLMClient{
		Respond: func(messages []ChatMessage) (*ChatResult, error) {
			return &ChatResult{Content: expectedResp}, nil
		},
	}

	session := NewConversationSession(DefaultSessionConfig())
	session.StartWithLLM(freshStore(), mockLLM)

	resp, _, err := session.ProcessTurn("Analyze.")
	if err != nil {
		t.Fatalf("ProcessTurn failed: %v", err)
	}
	if resp != expectedResp {
		t.Errorf("response = %q, want %q", resp, expectedResp)
	}
}

// -----------------------------------------------------------------------
// TestConversationSession_LLMClientError
// -----------------------------------------------------------------------

func TestConversationSession_LLMClientError(t *testing.T) {
	mockLLM := &mockLLMClient{
		Respond: func(messages []ChatMessage) (*ChatResult, error) {
			return nil, fmt.Errorf("llm: service unavailable")
		},
	}

	session := NewConversationSession(DefaultSessionConfig())
	session.StartWithLLM(freshStore(), mockLLM)

	_, _, err := session.ProcessTurn("analyze")
	if err == nil {
		t.Fatal("expected error from failed LLM call")
	}
	if session.GetState() != StateFailed {
		t.Errorf("expected failed state, got %s", session.GetState())
	}
	if session.GetError() == nil {
		t.Error("expected non-nil error from GetError")
	}
}

// -----------------------------------------------------------------------
// TestConversationSession_GetError
// -----------------------------------------------------------------------

func TestConversationSession_GetError(t *testing.T) {
	session := NewConversationSession(DefaultSessionConfig())
	if session.GetError() != nil {
		t.Errorf("expected nil error initially, got %v", session.GetError())
	}

	session.Start(freshStore())
	_, _, err := session.ProcessTurn("q")
	if err != nil {
		t.Fatalf("ProcessTurn failed: %v", err)
	}
	if session.GetError() != nil {
		t.Errorf("expected nil after success, got %v", session.GetError())
	}
}

// -----------------------------------------------------------------------
// TestConversationSessionManager
// -----------------------------------------------------------------------

func TestConversationSessionManager_CreateGet(t *testing.T) {
	mgr := NewConversationSessionManager()

	cfg := &SessionConfig{MaxTurns: 10}
	s1 := mgr.CreateSession(cfg)
	if s1 == nil {
		t.Fatal("CreateSession returned nil")
	}

	got := mgr.GetSession(s1.GetSessionID())
	if got == nil {
		t.Fatal("GetSession returned nil")
	}
	if got.GetSessionID() != s1.GetSessionID() {
		t.Error("GetSession returned wrong session")
	}

	if mgr.GetSession("nonexistent") != nil {
		t.Error("expected nil for missing session")
	}
}

func TestConversationSessionManager_ListSessions(t *testing.T) {
	mgr := NewConversationSessionManager()

	_ = mgr.CreateSession(DefaultSessionConfig())
	time.Sleep(time.Millisecond)
	s2 := mgr.CreateSession(DefaultSessionConfig())

	all := mgr.ListSessions()
	if len(all) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(all))
	}
	if all[0] != s2 {
		t.Error("expected s2 (newest) as first")
	}
}

func TestConversationSessionManager_DeleteSession(t *testing.T) {
	mgr := NewConversationSessionManager()
	s := mgr.CreateSession(DefaultSessionConfig())

	err := mgr.DeleteSession(s.GetSessionID())
	if err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}
	if mgr.GetSession(s.GetSessionID()) != nil {
		t.Error("expected nil after delete")
	}
	if err := mgr.DeleteSession(s.GetSessionID()); err == nil {
		t.Error("expected error on double delete")
	}
}

func TestConversationSessionManager_SessionCount(t *testing.T) {
	mgr := NewConversationSessionManager()
	if mgr.SessionCount() != 0 {
		t.Errorf("expected 0, got %d", mgr.SessionCount())
	}
	mgr.CreateSession(DefaultSessionConfig())
	mgr.CreateSession(DefaultSessionConfig())
	if mgr.SessionCount() != 2 {
		t.Errorf("expected 2, got %d", mgr.SessionCount())
	}
}

// -----------------------------------------------------------------------
// TestProcessConversationTurn
// -----------------------------------------------------------------------

func TestProcessConversationTurn(t *testing.T) {
	mgr := NewConversationSessionManager()
	s := mgr.CreateSession(&SessionConfig{MaxTurns: 5})
	s.Start(freshStore())

	resp, done, err := ProcessConversationTurn(mgr, s.GetSessionID(), "hello")
	if err != nil {
		t.Fatalf("ProcessConversationTurn failed: %v", err)
	}
	if done {
		t.Error("expected not done")
	}
	if resp == "" {
		t.Error("expected non-empty response")
	}

	_, _, err = ProcessConversationTurn(mgr, "bad-id", "hello")
	if err == nil {
		t.Error("expected error for missing session")
	}
}

// -----------------------------------------------------------------------
// TestSessionState_String
// -----------------------------------------------------------------------

func TestConversationState_String(t *testing.T) {
	tests := []struct {
		state  ConversationState
		expect string
	}{
		{StateIdle, "idle"},
		{StateProcessing, "processing"},
		{StateComplete, "complete"},
		{StateFailed, "failed"},
		{ConversationState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expect {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.expect)
		}
	}
}

// -----------------------------------------------------------------------
// TestAnalysisMode_String
// -----------------------------------------------------------------------

func TestAnalysisMode_String(t *testing.T) {
	tests := []struct {
		mode   AnalysisMode
		expect string
	}{
		{ModeDiscovery, "discovery"},
		{ModeSurgical, "surgical"},
		{ModeSynthesis, "synthesis"},
		{AnalysisMode(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.expect {
			t.Errorf("Mode(%d).String() = %q, want %q", tt.mode, got, tt.expect)
		}
	}
}

// -----------------------------------------------------------------------
// TestConversationSession_TraceRefs
// -----------------------------------------------------------------------

func TestConversationSession_TurnHasTraceRefs(t *testing.T) {
	store := freshStore()

	session := NewConversationSession(DefaultSessionConfig())
	session.Start(store)

	session.ProcessTurn("Show me trace details.")

	h := session.GetHistory()
	if len(h) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(h))
	}
	if len(h[0].AssistantResp) == 0 {
		t.Error("empty response")
	}
}

// -----------------------------------------------------------------------
// TestConversationSession_CloseMidSession
// -----------------------------------------------------------------------

func TestConversationSession_CloseMidSession(t *testing.T) {
	session := NewConversationSession(&SessionConfig{MaxTurns: 10})
	session.Start(freshStore())

	_, _, err := session.ProcessTurn("q1")
	if err != nil {
		t.Fatalf("process turn 1: %v", err)
	}

	session.Close()

	if session.GetState() != StateComplete {
		t.Errorf("expected complete, got %s", session.GetState())
	}

	_, _, err = session.ProcessTurn("q2")
	if err == nil {
		t.Error("expected error on post-close ProcessTurn")
	}
}

// -----------------------------------------------------------------------
// TestConversationSession_HistoryCopying
// -----------------------------------------------------------------------

func TestConversationSession_HistoryCopying(t *testing.T) {
	session := NewConversationSession(&SessionConfig{MaxTurns: 2})
	session.Start(freshStore())

	session.ProcessTurn("q1")
	session.ProcessTurn("q2")

	h1 := session.GetHistory()
	_ = session.GetHistory()

	h1[0].UserInput = "MODIFIED"
	// Verify h1 was a copy by checking a different call.
	if h := session.GetHistory(); h[0].UserInput == "MODIFIED" {
		t.Error("history should be a copy, not a reference")
	}
}

// -----------------------------------------------------------------------
// TestDefaultSessionConfig
// -----------------------------------------------------------------------

func TestDefaultSessionConfig(t *testing.T) {
	cfg := DefaultSessionConfig()
	if cfg.MaxTurns != 50 {
		t.Errorf("expected MaxTurns=50, got %d", cfg.MaxTurns)
	}
	if cfg.Mode != 0 {
		t.Errorf("expected Mode=0 (auto), got %v", cfg.Mode)
	}
}

// -----------------------------------------------------------------------
// TestSessionConfig_effectiveMaxTurns
// -----------------------------------------------------------------------

func TestSessionConfig_effectiveMaxTurns(t *testing.T) {
	tests := []struct {
		name     string
		maxTurns int
		expected int
	}{
		{"zero", 0, 50},
		{"negative", -1, 50},
		{"one", 1, 1},
		{"ten", 10, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &SessionConfig{MaxTurns: tt.maxTurns}
			got := cfg.effectiveMaxTurns()
			if got != tt.expected {
				t.Errorf("effectiveMaxTurns() = %d, want %d", got, tt.expected)
			}
		})
	}
}

// -----------------------------------------------------------------------
// TestTraceIDsForMode
// -----------------------------------------------------------------------

func TestTraceIDsForMode(t *testing.T) {
	store := freshStore()

	ids, err := TraceIDsForMode(ModeDiscovery, store)
	if err != nil {
		t.Fatalf("TraceIDsForMode failed: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected 0 trace IDs (empty store), got %d", len(ids))
	}

	if _, err := TraceIDsForMode(ModeDiscovery, nil); err != nil {
		t.Error("expected nil, nil for nil store")
	}
}

// -----------------------------------------------------------------------
// Benchmark
// -----------------------------------------------------------------------

func BenchmarkConversationSession_BasicTurn(b *testing.B) {
	store := freshStore()
	session := NewConversationSession(DefaultSessionConfig())
	session.Start(store)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = session.ProcessTurn("benchmark turn")
	}
}
