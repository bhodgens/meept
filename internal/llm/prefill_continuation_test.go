package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Prefill-continuation recovery (issue #57). Verified against the llama.cpp
// server source shipped in $MEEPT_HOME/deps/llama.cpp (this build floor is
// enforced by make deps-llama-check):
//   - the final chat response `content` is the SAMPLED CONTINUATION only —
//     the prefill rides as prompt and is never echoed into content
//     (tools/server/server-context.cpp: res->content =
//     slot.generated_text, which accumulates process_token output only);
//   - the LFM2.5 chat parser recognizes ONLY <|tool_call_start|>...
//     <|tool_call_end|> markers (common/parsers/lfm2.cpp), so a bare-JSON
//     prefill continuation completes as plain content with finish_reason
//     "stop" and NO tool_calls.
//
// The response shapes below are therefore mid-object fragments (the tail of
// the prefill's opened object), NOT parseable JSON on their own.

const prefillHintFileWrite = `{"name": "file_write", "arguments": {`

func TestParseLFMPrefillContinuation_FlatArgsTail(t *testing.T) {
	content := `"file_name": "hello.txt", "content": "hi"}`
	calls, ok := parseLFMPrefillContinuation(prefillHintFileWrite, content)
	if !ok {
		t.Fatalf("continuation not recognized as a call (content=%q)", content)
	}
	if len(calls) != 1 {
		t.Fatalf("recovered %d calls, want 1", len(calls))
	}
	if calls[0].Function.Name != "file_write" {
		t.Errorf("name = %q, want file_write", calls[0].Function.Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("arguments not JSON: %v", err)
	}
	if args["file_name"] != "hello.txt" || args["content"] != "hi" {
		t.Errorf("args = %v, want file_name/content payload", args)
	}
}

func TestParseLFMPrefillContinuation_NestedArgsTail(t *testing.T) {
	// The arguments object closes one level deeper: the continuation carries
	// the inner object's closing brace first.
	content := `"file_name": "a.txt", "options": {"overwrite": true}}}`
	calls, ok := parseLFMPrefillContinuation(prefillHintFileWrite, content)
	if !ok {
		t.Fatalf("nested continuation not recognized (content=%q)", content)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("arguments not JSON: %v", err)
	}
	opts, ok := args["options"].(map[string]any)
	if !ok || opts["overwrite"] != true {
		t.Errorf("options arg = %v, want {overwrite: true}", args["options"])
	}
}

func TestParseLFMPrefillContinuation_StringifiedArgs(t *testing.T) {
	// OpenAI wire shape: arguments as a JSON-encoded STRING.
	prefill := `{"name": "file_write", "arguments": "`
	content := `{\"file_name\": \"x.txt\"}"`
	calls, ok := parseLFMPrefillContinuation(prefill, content)
	if !ok {
		t.Fatalf("stringified-args continuation not recognized (content=%q)", content)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("arguments not JSON: %v", err)
	}
	if args["file_name"] != "x.txt" {
		t.Errorf("file_name arg = %v, want x.txt", args["file_name"])
	}
}

func TestParseLFMPrefillContinuation_NotGatedOffWithoutPrefill(t *testing.T) {
	// Without a prefill this helper is never consulted — the "" prefill must
	// return false even for a shape that would otherwise parse.
	if _, ok := parseLFMPrefillContinuation("", `{"name": "file_write", "arguments": {}}`); ok {
		t.Fatal("empty prefill must never recover a call")
	}
}

func TestParseLFMPrefillContinuation_ProseContinuationStaysProse(t *testing.T) {
	// The model declined the call shape and wrote prose instead — the
	// concatenation does not close into one call-shaped object.
	for _, content := range []string{
		`"file_name": "x.txt" — I cannot actually create files.`,
		`sorry, I don't know how to complete this`,
		`"file_name": "truncated but never closed...`,
	} {
		if _, ok := parseLFMPrefillContinuation(prefillHintFileWrite, content); ok {
			t.Errorf("prose continuation %q was mined as a call", content)
		}
	}
}

func TestParseLFMPrefillContinuation_DataTailMintsPrefillCommittedCall(t *testing.T) {
	// The prefill COMMITS to the call (name + arguments opener are already
	// on the wire); any continuation that completes valid JSON is the model
	// filling in that call — even a payload that looks like data. The args
	// ride through; the executor validates them.
	content := `"title": "Attention Is All You Need"}`
	calls, ok := parseLFMPrefillContinuation(prefillHintFileWrite, content)
	if !ok {
		t.Fatalf("prefill-committed continuation not recognized (content=%q)", content)
	}
	if calls[0].Function.Name != "file_write" {
		t.Errorf("name = %q, want file_write (prefill-committed)", calls[0].Function.Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("arguments not JSON: %v", err)
	}
	if args["title"] != "Attention Is All You Need" {
		t.Errorf("title arg = %v, want the continuation payload", args["title"])
	}
}

// chatRespWithContent builds a one-choice ChatResponse whose message content
// is the JSON-encoded string text.
func chatRespWithContent(t *testing.T, text string) *ChatResponse {
	t.Helper()
	raw, err := json.Marshal(text)
	if err != nil {
		t.Fatalf("marshal content: %v", err)
	}
	return &ChatResponse{
		Model: "lfm2.5-8b-a1b",
		Choices: []Choice{{
			FinishReason: "stop",
			Message: ResponseMessage{
				Role:    "assistant",
				Content: raw,
			},
		}},
	}
}

// TestParseResponseWithTools_PrefillContinuationExecutes pins the full
// non-streaming response path: prefill-continued bare-JSON content converts
// to an executable ToolCall.
func TestParseResponseWithTools_PrefillContinuationExecutes(t *testing.T) {
	c := &Client{config: &ModelConfig{ModelID: "lfm2.5-8b-a1b"}, logger: discardLogger()}
	chatResp := chatRespWithContent(t, `"file_name": "hello.txt", "content": "hi"}`)
	chatResp.Usage.PromptTokens = 10
	chatResp.Usage.CompletionTokens = 5
	chatResp.Usage.TotalTokens = 15

	resp, err := c.parseResponseWithTools(chatResp, true, "local", "lfm2.5-8b-a1b", prefillHintFileWrite)
	if err != nil {
		t.Fatalf("parseResponseWithTools: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("recovered %d tool calls, want 1 (content=%q)", len(resp.ToolCalls), resp.Content)
	}
	if resp.ToolCalls[0].Function.Name != "file_write" {
		t.Errorf("name = %q, want file_write", resp.ToolCalls[0].Function.Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(resp.ToolCalls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("arguments not JSON: %v", err)
	}
	if args["file_name"] != "hello.txt" {
		t.Errorf("file_name arg = %v, want hello.txt", args["file_name"])
	}
	// The raw JSON fragment must not leak into Content — downstream guards
	// treat machine-shaped output as non-replies.
	if strings.Contains(resp.Content, "file_name") {
		t.Errorf("call fragment leaked into Content: %q", resp.Content)
	}
}

// TestParseResponseWithTools_NoPrefillStaysProse pins the gating: a mid-object
// fragment with NO prefill on the request keeps the existing semantics —
// content passes through untouched (it is not even valid JSON on its own, so
// the bare-JSON miner leaves it alone).
func TestParseResponseWithTools_NoPrefillStaysProse(t *testing.T) {
	c := &Client{config: &ModelConfig{ModelID: "lfm2.5-8b-a1b"}, logger: discardLogger()}
	frag := `"file_name": "hello.txt", "content": "hi"}`
	chatResp := chatRespWithContent(t, frag)

	resp, err := c.parseResponseWithTools(chatResp, true, "local", "lfm2.5-8b-a1b", "")
	if err != nil {
		t.Fatalf("parseResponseWithTools: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("fragment mined without a prefill: %+v", resp.ToolCalls)
	}
	if resp.Content != frag {
		t.Errorf("fragment content changed: %q", resp.Content)
	}
}

// TestParseResponseWithTools_PrefillProseStaysProse pins the shape gate: a
// prefill whose continuation is NOT a call-shaped object stays prose even
// on a prefilled request.
func TestParseResponseWithTools_PrefillProseStaysProse(t *testing.T) {
	c := &Client{config: &ModelConfig{ModelID: "lfm2.5-8b-a1b"}, logger: discardLogger()}
	prose := `"file_name": "x.txt" — I cannot actually create files.`
	chatResp := chatRespWithContent(t, prose)

	resp, err := c.parseResponseWithTools(chatResp, true, "local", "lfm2.5-8b-a1b", prefillHintFileWrite)
	if err != nil {
		t.Fatalf("parseResponseWithTools: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("prose continuation mined as a call: %+v", resp.ToolCalls)
	}
	if resp.Content != prose {
		t.Errorf("prose content changed: %q", resp.Content)
	}
}

// TestParseResponseWithTools_NativeToolCallsWin pins the precedence: when
// the server DID parse the reply into native tool_calls, the prefill
// recovery never fires and the native calls pass through untouched.
func TestParseResponseWithTools_NativeToolCallsWin(t *testing.T) {
	c := &Client{config: &ModelConfig{ModelID: "lfm2.5-8b-a1b"}, logger: discardLogger()}
	native := RawToolCall{ID: "call_abc", Type: "function"}
	native.Function.Name = "file_write"
	native.Function.Arguments = `{"file_name": "native.txt"}`
	chatResp := &ChatResponse{
		Model: "lfm2.5-8b-a1b",
		Choices: []Choice{{
			FinishReason: "tool_calls",
			Message: ResponseMessage{
				Role:      "assistant",
				ToolCalls: []RawToolCall{native},
			},
		}},
	}
	resp, err := c.parseResponseWithTools(chatResp, true, "local", "lfm2.5-8b-a1b", prefillHintFileWrite)
	if err != nil {
		t.Fatalf("parseResponseWithTools: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "call_abc" {
		t.Fatalf("native tool calls clobbered: %+v", resp.ToolCalls)
	}
}

// TestChatWithDeltaCallback_PrefillContinuationRecovers pins the STREAMING
// path end to end: a llama.cpp SSE stream whose content deltas carry the
// bare-JSON continuation (no tool_calls deltas) yields an executable
// ToolCall when the request carried the prefill, and none without it.
func TestChatWithDeltaCallback_PrefillContinuationRecovers(t *testing.T) {
	chunks := []any{
		[]any{map[string]any{
			"delta": map[string]any{"role": "assistant", "content": `"file_name": "hel`},
		}},
		[]any{map[string]any{
			"delta": map[string]any{"content": `lo.txt", "content": "hi"}`},
		}},
		[]any{map[string]any{
			"delta":         map[string]any{},
			"finish_reason": "stop",
		}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f, _ := w.(http.Flusher)
		for _, ch := range chunks {
			b, _ := json.Marshal(map[string]any{
				"id": "x", "object": "chat.completion.chunk", "model": "m", "choices": ch,
			})
			_, _ = w.Write([]byte("data: " + string(b) + "\n\n"))
			if f != nil {
				f.Flush()
			}
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	newClient := func() *Client {
		return NewClient(&ModelConfig{
			ModelID: "lfm2.5-8b-a1b",
			BaseURL: srv.URL,
		})
	}

	run := func(t *testing.T, withPrefill bool) *Response {
		t.Helper()
		opts := []ChatOption{}
		if withPrefill {
			opts = append(opts, WithAssistantPrefill(prefillHintFileWrite))
		}
		resp, err := newClient().ChatWithDeltaCallback(context.Background(), []ChatMessage{
			{Role: RoleUser, Content: "create hello.txt"},
		}, func(string) error { return nil }, opts...)
		if err != nil {
			t.Fatalf("ChatWithDeltaCallback: %v", err)
		}
		return resp
	}

	resp := run(t, true)
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("streaming prefill continuation recovered %d calls, want 1 (content=%q)",
			len(resp.ToolCalls), resp.Content)
	}
	if resp.ToolCalls[0].Function.Name != "file_write" {
		t.Errorf("name = %q, want file_write", resp.ToolCalls[0].Function.Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(resp.ToolCalls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("arguments not JSON: %v", err)
	}
	if args["file_name"] != "hello.txt" {
		t.Errorf("file_name arg = %v, want hello.txt", args["file_name"])
	}

	respNoPrefill := run(t, false)
	if len(respNoPrefill.ToolCalls) != 0 {
		t.Fatalf("fragment recovered without a prefill: %+v", respNoPrefill.ToolCalls)
	}
}
