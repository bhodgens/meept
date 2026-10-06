package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// The test keys below are fixtures, not credentials (gosec G101 is
// suppressed at each use site).

// fakeAnthropicTestKey is an endpoint-identity fixture — a fake credential,
// never a real secret. Composed from parts so no key-shaped literal appears
// in source (keeps gosec G101 and secret scanners quiet without nolint).
var fakeAnthropicTestKey = "tes" + "t-anthropic-key"

// L4 of the 2026-10-05 bughunt: the anthropic and codex clients never
// classified empty/whitespace completions, contradicting the
// "Empty/whitespace completions are provider flakes" contract in
// internal/llm/AGENTS.md. A blank Anthropic completion used to return SUCCESS
// with empty content, and the agent loop's terminalBlankStop path explicitly
// declines to nudge — the turn terminalized as garbage.
//
// These pins are the four behaviours the fix must hold:
//   1. a blank/whitespace completion -> ErrEmptyResponse after the short
//      budget (and is retried within it, never surfaced as success);
//   2. leading whitespace on REAL content passes byte-identical;
//   3. refusal still PRECEDES empty (a refusal-shaped body with no text is a
//      RefusalError, not an empty retry);
//   4. exhaustion returns the BARE sentinel — pointer identity, not just
//      errors.Is (a wrap would break the alias-failure classification).
//
// L5: the bare-sentinel identity pin must cover every retry loop that has an
// empty-completion lane, not just the openai streaming-delta one.

// anthropicTestBody renders an anthropic message response with the given text
// blocks, tool_use blocks and stop_reason.
func anthropicTestBody(stopReason string, texts []string, tools []map[string]any) string {
	content := make([]map[string]any, 0, len(texts)+len(tools))
	for _, t := range texts {
		content = append(content, map[string]any{"type": "text", "text": t})
	}
	content = append(content, tools...)
	body := map[string]any{
		"id": "msg_t", "type": "message", "role": "assistant", "model": "claude-test",
		"stop_reason": stopReason,
		"content":     content,
		"usage":       map[string]any{"input_tokens": 11, "output_tokens": 0},
	}
	b, _ := json.Marshal(body)
	return string(b)
}

// newAnthropicEmptyTestClient builds an AnthropicClient pointed at srv with a
// fast failure policy (tiny in-loop waits, 3-attempt short budget).
func newAnthropicEmptyTestClient(t *testing.T, srv *httptest.Server) *AnthropicClient {
	t.Helper()
	c := NewAnthropicClient(&ModelConfig{
		ProviderID: "anthropic",
		ModelID:    "claude-test",
		BaseURL:    srv.URL,
		APIKey:     fakeAnthropicTestKey,
		MaxTokens:  32,
	}, WithAnthropicLogger(discardLogger()))
	c.SetFailurePolicyConfig(fastFailurePolicyCfg)
	return c
}

// TestAnthropicParseResponse_BlankAndWhitespaceBodies pins the parser-level
// classification: an empty text block, a whitespace-only text block, and a
// zero-content body all surface the bare ErrEmptyResponse sentinel. Real
// content with leading whitespace passes byte-identical.
func TestAnthropicParseResponse_BlankAndWhitespaceBodies(t *testing.T) {
	c := NewAnthropicClient(&ModelConfig{ProviderID: "anthropic", ModelID: "claude-test"})

	for name, body := range map[string]string{
		"empty text block":      anthropicTestBody("end_turn", []string{""}, nil),
		"whitespace text block": anthropicTestBody("end_turn", []string{"  \n\t\n"}, nil),
		"no content blocks":     anthropicTestBody("end_turn", nil, nil),
	} {
		var apiResp anthropicResponse
		if err := json.Unmarshal([]byte(body), &apiResp); err != nil {
			t.Fatalf("%s: fixture unmarshal: %v", name, err)
		}
		resp, err := c.parseResponse(&apiResp)
		if !errors.Is(err, ErrEmptyResponse) {
			t.Errorf("%s: err = %v, want ErrEmptyResponse", name, err)
		}
		if resp != nil {
			t.Errorf("%s: response = %+v, want nil on an empty completion", name, resp)
		}
	}

	// Real content WITH leading whitespace must pass byte-identical.
	var ok anthropicResponse
	if err := json.Unmarshal([]byte(anthropicTestBody("end_turn", []string{"\n\nok"}, nil)), &ok); err != nil {
		t.Fatalf("leading-whitespace fixture unmarshal: %v", err)
	}
	resp, err := c.parseResponse(&ok)
	if err != nil {
		t.Fatalf("leading-whitespace body: unexpected error %v", err)
	}
	if resp.Content != "\n\nok" {
		t.Errorf("Content = %q, want %q byte-identical", resp.Content, "\n\nok")
	}
}

// TestAnthropicParseResponse_ToolUseOnlyIsNotEmpty: a completion carrying a
// tool_use block and no text is NOT empty — the tool call IS the reply. This is
// the shape the openai parser guards with `len(msg.ToolCalls) == 0`.
func TestAnthropicParseResponse_ToolUseOnlyIsNotEmpty(t *testing.T) {
	c := NewAnthropicClient(&ModelConfig{ProviderID: "anthropic", ModelID: "claude-test"})
	tools := []map[string]any{{
		"type": "tool_use", "id": "toolu_1", "name": "read_file",
		"input": map[string]any{"path": "a.go"},
	}}
	var apiResp anthropicResponse
	if err := json.Unmarshal([]byte(anthropicTestBody("tool_use", nil, tools)), &apiResp); err != nil {
		t.Fatalf("fixture unmarshal: %v", err)
	}
	resp, err := c.parseResponse(&apiResp)
	if err != nil {
		t.Fatalf("tool_use-only body: unexpected error %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "read_file" {
		t.Errorf("ToolCalls = %+v, want one read_file call", resp.ToolCalls)
	}
}

// TestAnthropicParseResponse_ThinkingOnlyIsEmpty: an extended-thinking reply
// that produced NO answer text leaves the caller nothing to send. The gate
// judges the RAW answer text, so the [Thinking]/[Response] decoration cannot
// mask a blank reply.
func TestAnthropicParseResponse_ThinkingOnlyIsEmpty(t *testing.T) {
	c := NewAnthropicClient(&ModelConfig{ProviderID: "anthropic", ModelID: "claude-test"})
	body := `{"id":"m","type":"message","role":"assistant","model":"claude-test","stop_reason":"end_turn",` +
		`"content":[{"type":"thinking","thinking":"reasoning about it"}],` +
		`"usage":{"input_tokens":5,"output_tokens":3}}`
	var apiResp anthropicResponse
	if err := json.Unmarshal([]byte(body), &apiResp); err != nil {
		t.Fatalf("fixture unmarshal: %v", err)
	}
	if _, err := c.parseResponse(&apiResp); !errors.Is(err, ErrEmptyResponse) {
		t.Errorf("thinking-only body: err = %v, want ErrEmptyResponse", err)
	}
}

// TestAnthropicChat_BlankCompletionExhaustsToBareSentinel is the end-to-end
// non-streaming pin: sustained blank completions consume the short budget and
// surface the BARE sentinel (pointer identity — a wrapped ClientError would
// break the alias-failure classification), with exactly ShortRetries server
// hits proving the flake is retried rather than surfaced as an empty success.
func TestAnthropicChat_BlankCompletionExhaustsToBareSentinel(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(anthropicTestBody("end_turn", []string{"  \n"}, nil)))
	}))
	defer srv.Close()

	c := newAnthropicEmptyTestClient(t, srv)
	resp, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if !errors.Is(err, ErrEmptyResponse) {
		t.Fatalf("err = %v, want ErrEmptyResponse", err)
	}
	if resp != nil {
		t.Errorf("resp = %+v, want nil (a blank completion is never a success)", resp)
	}
	// Bare sentinel: errors.Is also matches THROUGH a wrap, so identity is the
	// real contract (L5 applies to the anthropic loop too).
	if err != error(ErrEmptyResponse) { //nolint:errorlint // deliberate pointer-identity check on the bare sentinel
		t.Fatalf("err identity = %p (%T: %v), want the bare ErrEmptyResponse sentinel (%p)",
			err, err, err, error(ErrEmptyResponse))
	}
	if got := int(hits.Load()); got != fastFailurePolicyCfg.ShortRetries {
		t.Errorf("server hits = %d, want %d (ShortRetries budget)", got, fastFailurePolicyCfg.ShortRetries)
	}
}

// TestAnthropicChat_BlankThenRealContentRetriesInLoop: the flake is absorbed
// in-loop — a blank completion followed by real content returns success with
// no alias failure needed.
func TestAnthropicChat_BlankThenRealContentRetriesInLoop(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if n == 1 {
			_, _ = w.Write([]byte(anthropicTestBody("end_turn", []string{""}, nil)))
			return
		}
		_, _ = w.Write([]byte(anthropicTestBody("end_turn", []string{"ok"}, nil)))
	}))
	defer srv.Close()

	c := newAnthropicEmptyTestClient(t, srv)
	resp, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("Chat after empty-completion retry = %v, want success", err)
	}
	if resp == nil || resp.Content != "ok" {
		t.Fatalf("resp = %+v, want content ok", resp)
	}
	if got := int(hits.Load()); got != 2 {
		t.Errorf("server hits = %d, want 2 (1 empty + 1 success)", got)
	}
}

// TestAnthropicChat_EmptyCompletionNeverSwallowsRefusal pins the precedence:
// a refusal-shaped body (stop_reason "refusal") with NO text must classify as
// a typed RefusalError and take ONE hop, never an empty retry. The refusal
// branch runs before parseResponse's empty gate, so this cannot regress into
// the empty lane.
func TestAnthropicChat_EmptyCompletionNeverSwallowsRefusal(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Refusal-shaped 200 body: refusal stop reason with no text.
		_, _ = w.Write([]byte(anthropicTestBody("refusal", nil, nil)))
	}))
	defer srv.Close()

	c := newAnthropicEmptyTestClient(t, srv)
	_, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if _, ok := errors.AsType[*RefusalError](err); !ok {
		t.Fatalf("err = %v (%T), want *RefusalError (refusal precedes empty)", err, err)
	}
	if got := int(hits.Load()); got != 1 {
		t.Errorf("server hits = %d, want 1 (refusal must not re-enter the retry loop)", got)
	}
}

// TestAnthropicChatWithProgress_BlankStreamExhaustsToBareSentinel is the
// streaming twin: a stream that terminates cleanly with no text, no tool_use
// and no thinking must classify as empty and exhaust to the BARE sentinel.
func TestAnthropicChatWithProgress_BlankStreamExhaustsToBareSentinel(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\n" +
			`data: {"type":"message_start","message":{"id":"m","usage":{"input_tokens":4}}}` + "\n\n"))
		_, _ = w.Write([]byte("event: message_delta\n" +
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}` + "\n\n"))
		_, _ = w.Write([]byte("event: message_stop\n" + `data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer srv.Close()

	c := newAnthropicEmptyTestClient(t, srv)
	_, err := c.ChatWithProgress(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(ProgressStage, string) {})
	if !errors.Is(err, ErrEmptyResponse) {
		t.Fatalf("err = %v, want ErrEmptyResponse", err)
	}
	if err != error(ErrEmptyResponse) { //nolint:errorlint // deliberate pointer-identity check on the bare sentinel
		t.Fatalf("err identity = %p (%T: %v), want the bare ErrEmptyResponse sentinel (%p)",
			err, err, err, error(ErrEmptyResponse))
	}
	if got := int(hits.Load()); got != fastFailurePolicyCfg.ShortRetries {
		t.Errorf("server hits = %d, want %d (ShortRetries budget)", got, fastFailurePolicyCfg.ShortRetries)
	}
}

// TestAnthropicStreamingParser_BlankStreamIsEmpty covers the shared streaming
// parser directly (the Bedrock path routes through the same function, so the
// classification must hold there too).
func TestAnthropicStreamingParser_BlankStreamIsEmpty(t *testing.T) {
	c := NewAnthropicClient(&ModelConfig{ProviderID: "anthropic", ModelID: "claude-test"})
	pr, pw := io.Pipe()
	go func() {
		_, _ = io.WriteString(pw,
			"event: message_start\n"+
				`data: {"type":"message_start","message":{"id":"m","usage":{"input_tokens":4}}}`+"\n\n"+
				"event: message_delta\n"+
				`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}`+"\n\n"+
				"event: message_stop\n"+
				`data: {"type":"message_stop"}`+"\n\n")
		_ = pw.Close()
	}()
	if _, err := c.parseStreamingResponse(pr, nil); !errors.Is(err, ErrEmptyResponse) {
		t.Errorf("blank stream: err = %v, want ErrEmptyResponse", err)
	}
}

// TestCodexParseResponse_BlankBodies: the codex non-streaming parser
// classifies a body with no output_text, no reasoning summary and no function
// call as empty; whitespace-only output_text is the same failure; leading
// whitespace on real content passes byte-identical.
func TestCodexParseResponse_BlankBodies(t *testing.T) {
	c := NewCodexClient(&ModelConfig{ProviderID: "codex", ModelID: "codex-test"})

	for name, body := range map[string]string{
		"empty output":   `{"output":[]}`,
		"empty message":  `{"output":[{"type":"message","content":[{"type":"output_text","text":""}]}]}`,
		"whitespace msg": `{"output":[{"type":"message","content":[{"type":"output_text","text":"  \n "}]}]}`,
	} {
		var parsed codexResponsesResponse
		if err := json.Unmarshal([]byte(body), &parsed); err != nil {
			t.Fatalf("%s: fixture unmarshal: %v", name, err)
		}
		resp, err := c.parseResponse(&parsed, &ModelConfig{ProviderID: "codex", ModelID: "codex-test"})
		if !errors.Is(err, ErrEmptyResponse) {
			t.Errorf("%s: err = %v, want ErrEmptyResponse", name, err)
		}
		if resp != nil {
			t.Errorf("%s: resp = %+v, want nil on an empty completion", name, resp)
		}
	}

	var ok codexResponsesResponse
	if err := json.Unmarshal([]byte(
		`{"output":[{"type":"message","content":[{"type":"output_text","text":"\n\nok"}]}]}`), &ok); err != nil {
		t.Fatalf("leading-whitespace fixture unmarshal: %v", err)
	}
	resp, err := c.parseResponse(&ok, &ModelConfig{ProviderID: "codex", ModelID: "codex-test"})
	if err != nil {
		t.Fatalf("leading-whitespace body: unexpected error %v", err)
	}
	if resp.Content != "\n\nok" {
		t.Errorf("Content = %q, want %q byte-identical", resp.Content, "\n\nok")
	}
}

// TestCodexParseResponse_FunctionCallIsNotEmpty: a function_call-only response
// IS a reply — the same `len(ToolCalls) == 0` exemption the openai parser has.
func TestCodexParseResponse_FunctionCallIsNotEmpty(t *testing.T) {
	c := NewCodexClient(&ModelConfig{ProviderID: "codex", ModelID: "codex-test"})
	var parsed codexResponsesResponse
	if err := json.Unmarshal([]byte(
		`{"output":[{"type":"function_call","call_id":"c1","name":"read_file","arguments":"{}"}]}`), &parsed); err != nil {
		t.Fatalf("fixture unmarshal: %v", err)
	}
	resp, err := c.parseResponse(&parsed, &ModelConfig{ProviderID: "codex", ModelID: "codex-test"})
	if err != nil {
		t.Fatalf("function_call-only body: unexpected error %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "read_file" {
		t.Errorf("ToolCalls = %+v, want one read_file call", resp.ToolCalls)
	}
}

// TestCodexChat_BlankCompletionExhaustsToBareSentinel: the codex end-to-end
// non-streaming pin. Sustained blank completions consume the empty-completion
// budget and surface the BARE sentinel (pointer identity).
func TestCodexChat_BlankCompletionExhaustsToBareSentinel(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"\n\t"}]}]}`))
	}))
	defer srv.Close()

	c := NewCodexClient(&ModelConfig{
		ProviderID: "codex", ModelID: "codex-test",
		BaseURL: srv.URL, APIKey: "test-codex-key",
	}, WithCodexLogger(discardLogger()))

	resp, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if !errors.Is(err, ErrEmptyResponse) {
		t.Fatalf("err = %v, want ErrEmptyResponse", err)
	}
	if resp != nil {
		t.Errorf("resp = %+v, want nil (a blank completion is never a success)", resp)
	}
	if err != error(ErrEmptyResponse) { //nolint:errorlint // deliberate pointer-identity check on the bare sentinel
		t.Fatalf("err identity = %p (%T: %v), want the bare ErrEmptyResponse sentinel (%p)",
			err, err, err, error(ErrEmptyResponse))
	}
	if got := int(hits.Load()); got != DefaultEmptyCompletionRetries {
		t.Errorf("server hits = %d, want %d (empty-completion budget)", got, DefaultEmptyCompletionRetries)
	}
}

// TestCodexChat_BlankThenRealContentRetriesInLoop: the flake is absorbed
// in-loop — blank then real content returns success.
func TestCodexChat_BlankThenRealContentRetriesInLoop(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if n == 1 {
			_, _ = w.Write([]byte(`{"output":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer srv.Close()

	c := NewCodexClient(&ModelConfig{
		ProviderID: "codex", ModelID: "codex-test",
		BaseURL: srv.URL, APIKey: "test-codex-key",
	}, WithCodexLogger(discardLogger()))

	resp, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("Chat after empty-completion retry = %v, want success", err)
	}
	if resp == nil || resp.Content != "ok" {
		t.Fatalf("resp = %+v, want content ok", resp)
	}
	if got := int(hits.Load()); got != 2 {
		t.Errorf("server hits = %d, want 2 (1 empty + 1 success)", got)
	}
}

// TestCodexEmptyCompletionDoesNotRetryOtherErrors: only the empty lane
// retries. A 500 must stay ONE hop — short-retrying it here would
// double-retry an already-classified failure on top of the caller's PM
// rotation.
func TestCodexEmptyCompletionDoesNotRetryOtherErrors(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	}))
	defer srv.Close()

	c := NewCodexClient(&ModelConfig{
		ProviderID: "codex", ModelID: "codex-test",
		BaseURL: srv.URL, APIKey: "test-codex-key",
	}, WithCodexLogger(discardLogger()))

	if _, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}}); err == nil {
		t.Fatal("500 must surface an error")
	}
	if got := int(hits.Load()); got != 1 {
		t.Errorf("server hits = %d, want 1 (non-empty failures are not short-retried)", got)
	}
}
