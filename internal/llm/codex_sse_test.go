package llm

// Tests for CodexClient Responses-API SSE streaming (codex_sse.go).
//
// Upstream contract under test (openai/codex, codex-rs):
//   - codex-rs/codex-api/src/endpoint/responses.rs — Accept: text/event-stream
//     on every streamed /responses POST; stream:true in the wire body.
//   - codex-rs/codex-api/src/sse/responses.rs (process_responses_event,
//     process_sse) — event grammar, terminal handling, and the
//     "stream closed before response.completed" error.
//   - codex-rs/codex-api/tests/sse_end_to_end.rs — event: <type>\ndata: <json>
//     framing used to build the fixture streams below.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// codexSSEEvent builds one SSE frame: "event: <type>\ndata: <json>\n\n".
func codexSSEEvent(v map[string]any) string {
	typ, _ := v["type"].(string)
	b, _ := json.Marshal(v)
	return fmt.Sprintf("event: %s\ndata: %s\n\n", typ, b)
}

// codexSSEBody concatenates event frames into a full stream body.
func codexSSEBody(events []map[string]any) string {
	var b strings.Builder
	for _, ev := range events {
		b.WriteString(codexSSEEvent(ev))
	}
	return b.String()
}

// codexTestServerWithResponder is newCodexTestServer with a custom responder
// so SSE tests can control Content-Type and stream in chunks.
func codexTestServerWithResponder(t *testing.T, status int, contentType, body string) (*httptest.Server, <-chan capturedRequest) {
	t.Helper()
	ch := make(chan capturedRequest, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqBody, err := readAllLimited(r)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		ch <- capturedRequest{
			Method:      r.Method,
			Path:        r.URL.Path,
			Header:      r.Header.Clone(),
			Body:        reqBody,
			ContentType: r.Header.Get("Content-Type"),
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, ch
}

// readAllLimited reads the request body, bounded to keep the helper
// allocation-safe.
func readAllLimited(r *http.Request) (string, error) {
	defer r.Body.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)
	for {
		n, err := r.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			if err.Error() == "EOF" {
				return string(buf), nil
			}
			return string(buf), err
		}
		if len(buf) > 1<<20 {
			return string(buf), fmt.Errorf("request body too large")
		}
	}
}

// assertStreamingRequestShape asserts the codex-rs streaming wire contract
// on a captured request: stream:true and Accept: text/event-stream.
func assertStreamingRequestShape(t *testing.T, req capturedRequest) {
	t.Helper()
	if got := req.Header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream (codex-rs contract)", got)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		t.Fatalf("body not JSON: %v\nbody: %s", err, req.Body)
	}
	if body["stream"] != true {
		t.Errorf("stream = %v, want true", body["stream"])
	}
}

// codexRealisticStream is the event sequence a real Codex turn produces:
// lifecycle open, reasoning deltas, output text deltas, folded items, and
// the terminal completed carrying usage (codex-rs/codex-api/src/sse/
// responses.rs process_responses_event order).
func codexRealisticStream(completed map[string]any) []map[string]any {
	return []map[string]any{
		{"type": "response.created", "response": map[string]any{"id": "resp_1"}},
		{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{
			"type": "reasoning", "id": "rs_1", "summary": []any{},
		}},
		{"type": "response.reasoning_summary_text.delta", "item_id": "rs_1", "summary_index": 0, "delta": "thinking "},
		{"type": "response.reasoning_summary_text.delta", "item_id": "rs_1", "summary_index": 0, "delta": "hard"},
		{"type": "response.output_item.done", "item": map[string]any{
			"type": "reasoning", "id": "rs_1",
			"summary": []any{map[string]any{"type": "summary_text", "text": "thinking hard"}},
		}},
		{"type": "response.output_item.added", "output_index": 1, "item": map[string]any{
			"type": "message", "role": "assistant", "id": "msg_1", "content": []any{},
		}},
		{"type": "response.output_text.delta", "item_id": "msg_1", "delta": "Hel"},
		{"type": "response.output_text.delta", "item_id": "msg_1", "delta": "lo"},
		{"type": "response.output_text.delta", "item_id": "msg_1", "delta": ", world"},
		{"type": "response.output_item.done", "item": map[string]any{
			"type": "message", "role": "assistant", "id": "msg_1",
			"content": []any{map[string]any{"type": "output_text", "text": "Hello, world"}},
		}},
		{"type": "response.completed", "response": completed},
	}
}

// TestCodexSSEStreamHappyPath drives the full streaming path through
// ChatWithDeltaCallback: request shape, delta invocations, folded content,
// reasoning, and usage from response.completed.
func TestCodexSSEStreamHappyPath(t *testing.T) {
	completed := map[string]any{
		"id": "resp_1",
		"usage": map[string]any{
			"input_tokens": 120, "output_tokens": 45, "total_tokens": 165,
		},
	}
	srv, ch := codexTestServerWithResponder(t, http.StatusOK,
		"text/event-stream", codexSSEBody(codexRealisticStream(completed)))
	client := newCodexClientForTest(t, srv.URL)

	var deltas []string
	var mu sync.Mutex
	resp, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(delta string) error {
			mu.Lock()
			defer mu.Unlock()
			deltas = append(deltas, delta)
			return nil
		})
	if err != nil {
		t.Fatalf("ChatWithDeltaCallback: %v", err)
	}

	// Request shape: codex-rs streaming contract.
	req := <-ch
	assertStreamingRequestShape(t, req)

	// Deltas fired per output_text.delta, in order.
	wantDeltas := []string{"Hel", "lo", ", world"}
	mu.Lock()
	gotDeltas := append([]string(nil), deltas...)
	mu.Unlock()
	if len(gotDeltas) != len(wantDeltas) {
		t.Fatalf("deltas = %q, want %q", gotDeltas, wantDeltas)
	}
	for i := range wantDeltas {
		if gotDeltas[i] != wantDeltas[i] {
			t.Errorf("delta[%d] = %q, want %q", i, gotDeltas[i], wantDeltas[i])
		}
	}

	// Folded response mirrors the non-streaming parse semantics.
	if resp.Content != "Hello, world" {
		t.Errorf("Content = %q", resp.Content)
	}
	if resp.Reasoning != "thinking hard" {
		t.Errorf("Reasoning = %q", resp.Reasoning)
	}
	if resp.Model != "gpt-5.1-codex" {
		t.Errorf("Model = %q", resp.Model)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("FinishReason = %q", resp.FinishReason)
	}
	wantUsage := TokenUsage{PromptTokens: 120, CompletionTokens: 45, TotalTokens: 165}
	if resp.Usage != wantUsage {
		t.Errorf("Usage = %+v, want %+v", resp.Usage, wantUsage)
	}
}

// TestCodexSSEStreamToolCall covers function_call items folded from
// output_item.done and the tool_calls finish reason.
func TestCodexSSEStreamToolCall(t *testing.T) {
	events := []map[string]any{
		{"type": "response.created", "response": map[string]any{"id": "resp_1"}},
		{"type": "response.output_item.done", "item": map[string]any{
			"type": "function_call", "call_id": "call_9", "name": "get_weather",
			"arguments": `{"city":"SF"}`,
		}},
		{"type": "response.completed", "response": map[string]any{
			"id":    "resp_1",
			"usage": map[string]any{"input_tokens": 10, "output_tokens": 5},
		}},
	}
	srv, _ := codexTestServerWithResponder(t, http.StatusOK, "text/event-stream", codexSSEBody(events))
	client := newCodexClientForTest(t, srv.URL)

	resp, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "weather?"}},
		func(string) error { return nil })
	if err != nil {
		t.Fatalf("ChatWithDeltaCallback: %v", err)
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want tool_calls", resp.FinishReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %+v", resp.ToolCalls)
	}
	want := ToolCall{ID: "call_9", Type: "function",
		Function: ToolCallFunction{Name: "get_weather", Arguments: `{"city":"SF"}`}}
	if resp.ToolCalls[0] != want {
		t.Errorf("ToolCalls[0] = %+v, want %+v", resp.ToolCalls[0], want)
	}
}

// TestCodexSSEStreamNonStreamingFallback pins the split decision: Chat
// (no callback) keeps stream:false + Accept: application/json.
func TestCodexSSEStreamNonStreamingFallback(t *testing.T) {
	srv, ch := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[],"usage":{"input_tokens":1,"output_tokens":2}}`})
	client := newCodexClientForTest(t, srv.URL)
	resp, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "x"}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	req := <-ch
	if got := req.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q, want application/json on non-streaming path", got)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if body["stream"] != false {
		t.Errorf("stream = %v, want false on non-streaming path", body["stream"])
	}
	if resp.Usage.TotalTokens != 3 {
		t.Errorf("Usage.TotalTokens = %d, want 3", resp.Usage.TotalTokens)
	}
}

// TestCodexChatWithProgressStillNonStreaming documents the ChatWithProgress
// decision: it delegates to Chat (stream:false), so its captured request
// must keep the non-streaming shape. Progress callbacks are the UX layer;
// deltas are opt-in via ChatWithDeltaCallback.
func TestCodexChatWithProgressStillNonStreaming(t *testing.T) {
	srv, ch := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[]}`})
	client := newCodexClientForTest(t, srv.URL)
	if _, err := client.ChatWithProgress(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "x"}},
		func(ProgressStage, string) {}); err != nil {
		t.Fatalf("ChatWithProgress: %v", err)
	}
	req := <-ch
	if got := req.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q, want application/json", got)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if body["stream"] != false {
		t.Errorf("stream = %v, want false", body["stream"])
	}
}

// TestCodexSSEStreamAbortedByDeltaCallback pins TTSR parity: a non-nil
// onDelta error aborts the stream and is returned to the caller.
func TestCodexSSEStreamAbortedByDeltaCallback(t *testing.T) {
	srv, _ := codexTestServerWithResponder(t, http.StatusOK,
		"text/event-stream", codexSSEBody(codexRealisticStream(map[string]any{"id": "resp_1"})))
	client := newCodexClientForTest(t, srv.URL)
	aborted := fmt.Errorf("ttsr abort")
	var calls int
	_, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(string) error {
			calls++
			return aborted
		})
	if err == nil {
		t.Fatal("expected abort error")
	}
	if calls != 1 {
		t.Errorf("delta callback fired %d times, want 1 (stream must abort immediately)", calls)
	}
	if !strings.Contains(err.Error(), "ttsr abort") {
		t.Errorf("error %q missing callback cause", err.Error())
	}
}

// TestCodexSSEStreamFailedEvent covers the terminal response.failed event:
// the stream must fail with the server's message.
func TestCodexSSEStreamFailedEvent(t *testing.T) {
	events := []map[string]any{
		{"type": "response.created", "response": map[string]any{"id": "resp_1"}},
		{"type": "response.output_text.delta", "delta": "partial"},
		{"type": "response.failed", "response": map[string]any{
			"id": "resp_1",
			"error": map[string]any{
				"code": "upstream_error", "message": "Upstream provider error",
			},
		}},
	}
	srv, _ := codexTestServerWithResponder(t, http.StatusOK, "text/event-stream", codexSSEBody(events))
	client := newCodexClientForTest(t, srv.URL)
	_, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "x"}},
		func(string) error { return nil })
	if err == nil {
		t.Fatal("expected error for response.failed")
	}
	if !strings.Contains(err.Error(), "Upstream provider error") {
		t.Errorf("error %q missing server message", err.Error())
	}
}

// TestCodexSSEStreamIncompleteEvent covers the terminal response.incomplete
// event and its incomplete_details.reason.
func TestCodexSSEStreamIncompleteEvent(t *testing.T) {
	events := []map[string]any{
		{"type": "response.created", "response": map[string]any{"id": "resp_1"}},
		{"type": "response.incomplete", "response": map[string]any{
			"id":                 "resp_1",
			"incomplete_details": map[string]any{"reason": "max_output_tokens"},
		}},
	}
	srv, _ := codexTestServerWithResponder(t, http.StatusOK, "text/event-stream", codexSSEBody(events))
	client := newCodexClientForTest(t, srv.URL)
	_, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "x"}},
		func(string) error { return nil })
	if err == nil {
		t.Fatal("expected error for response.incomplete")
	}
	if !strings.Contains(err.Error(), "max_output_tokens") {
		t.Errorf("error %q missing incomplete reason", err.Error())
	}
}

// TestCodexSSEStreamClosedBeforeCompleted pins upstream process_sse
// behavior: a stream that ends without response.completed is an error.
func TestCodexSSEStreamClosedBeforeCompleted(t *testing.T) {
	events := []map[string]any{
		{"type": "response.created", "response": map[string]any{"id": "resp_1"}},
		{"type": "response.output_text.delta", "delta": "partial"},
	}
	srv, _ := codexTestServerWithResponder(t, http.StatusOK, "text/event-stream", codexSSEBody(events))
	client := newCodexClientForTest(t, srv.URL)
	_, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "x"}},
		func(string) error { return nil })
	if err == nil {
		t.Fatal("expected error for premature stream close")
	}
	if !strings.Contains(err.Error(), "closed before response.completed") {
		t.Errorf("error %q missing premature-close marker", err.Error())
	}
}

// TestCodexSSEStreamGarbageEventsAreSkipped pins the skip-don't-fail policy
// for unparseable event payloads (same as the OpenAI chunk parser) and that
// events without data are ignored.
func TestCodexSSEStreamGarbageEventsAreSkipped(t *testing.T) {
	var b strings.Builder
	b.WriteString("event: response.output_text.delta\nnot json at all\n\n") // malformed data payload
	b.WriteString("event: ping\n\n")                                        // comment-only frame, no data
	b.WriteString(codexSSEEvent(map[string]any{"type": "response.output_text.delta", "delta": "ok"}))
	b.WriteString(codexSSEEvent(map[string]any{"type": "response.completed", "response": map[string]any{
		"id": "r", "usage": map[string]any{"input_tokens": 1, "output_tokens": 2},
	}}))
	srv, _ := codexTestServerWithResponder(t, http.StatusOK, "text/event-stream", b.String())
	client := newCodexClientForTest(t, srv.URL)

	resp, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "x"}},
		func(string) error { return nil })
	if err != nil {
		t.Fatalf("ChatWithDeltaCallback: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q, want ok", resp.Content)
	}
	if resp.Usage.TotalTokens != 3 {
		t.Errorf("Usage.TotalTokens = %d, want 3", resp.Usage.TotalTokens)
	}
}

// TestCodexSSEStreamTerminalStopsScan pins openai/codex#9170 behavior:
// events after response.completed must not be consumed (the server may keep
// the connection open; anything sent afterwards must be irrelevant).
func TestCodexSSEStreamTerminalStopsScan(t *testing.T) {
	var b strings.Builder
	b.WriteString(codexSSEEvent(map[string]any{"type": "response.output_text.delta", "delta": "good"}))
	b.WriteString(codexSSEEvent(map[string]any{"type": "response.completed", "response": map[string]any{"id": "r"}}))
	b.WriteString(codexSSEEvent(map[string]any{"type": "response.failed", "response": map[string]any{
		"id": "r", "error": map[string]any{"message": "must never be seen"},
	}}))
	srv, _ := codexTestServerWithResponder(t, http.StatusOK, "text/event-stream", b.String())
	client := newCodexClientForTest(t, srv.URL)

	resp, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "x"}},
		func(string) error { return nil })
	if err != nil {
		t.Fatalf("ChatWithDeltaCallback: %v", err)
	}
	if resp.Content != "good" {
		t.Errorf("Content = %q", resp.Content)
	}
}

// TestCodexSSEStreamIdleAbortsOnContextCancel covers the transport-level
// hang: a server that never completes the stream must not wedge the client
// when the caller cancels.
func TestCodexSSEStreamIdleAbortsOnContextCancel(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: response.created\ndata: {\"type\":\"response.created\"}\n\n"))
		w.(http.Flusher).Flush()
		<-block // never complete the stream
	}))
	t.Cleanup(srv.Close)
	client := newCodexClientForTest(t, srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.ChatWithDeltaCallback(ctx,
		[]ChatMessage{{Role: RoleUser, Content: "x"}},
		func(string) error { return nil })
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// TestCodexSSEStreamUnitTable is a direct-table pass over the event handler
// covering the event grammar without the HTTP layer.
func TestCodexSSEStreamUnitTable(t *testing.T) {
	tests := []struct {
		name     string
		event    string
		json     string
		wantTerm bool
		wantErr  bool
		wantAcc  codexStreamAccumulatorCheck
	}{
		{
			name:  "output_text_delta accumulates",
			event: "response.output_text.delta",
			json:  `{"type":"response.output_text.delta","delta":"hi"}`,
			wantAcc: func(t *testing.T, acc *codexStreamAccumulator) {
				if acc.deltaContent.String() != "hi" {
					t.Errorf("deltaContent = %q", acc.deltaContent.String())
				}
			},
		},
		{
			name:  "reasoning_summary_delta accumulates",
			event: "response.reasoning_summary_text.delta",
			json:  `{"type":"response.reasoning_summary_text.delta","delta":"why"}`,
			wantAcc: func(t *testing.T, acc *codexStreamAccumulator) {
				if acc.deltaReasoning.String() != "why" {
					t.Errorf("deltaReasoning = %q", acc.deltaReasoning.String())
				}
			},
		},
		{
			name:  "function_call_arguments_delta ignored",
			event: "response.function_call_arguments.delta",
			json:  `{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"x"}`,
			wantAcc: func(t *testing.T, acc *codexStreamAccumulator) {
				if len(acc.toolCalls) != 0 {
					t.Errorf("toolCalls = %+v, want empty", acc.toolCalls)
				}
			},
		},
		{
			name:  "output_item_done message",
			event: "response.output_item.done",
			json:  `{"type":"response.output_item.done","item":{"type":"message","content":[{"type":"output_text","text":"done"}]}}`,
			wantAcc: func(t *testing.T, acc *codexStreamAccumulator) {
				if acc.itemContent.String() != "done" {
					t.Errorf("itemContent = %q", acc.itemContent.String())
				}
			},
		},
		{
			name:     "completed terminal with usage",
			event:    "response.completed",
			json:     `{"type":"response.completed","response":{"id":"r","usage":{"input_tokens":3,"output_tokens":4}}}`,
			wantTerm: true,
			wantAcc: func(t *testing.T, acc *codexStreamAccumulator) {
				want := TokenUsage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7}
				if acc.usage != want {
					t.Errorf("usage = %+v, want %+v", acc.usage, want)
				}
			},
		},
		{
			name:     "completed without usage yields zero usage",
			event:    "response.completed",
			json:     `{"type":"response.completed","response":{"id":"r"}}`,
			wantTerm: true,
		},
		{
			name:     "failed terminal error",
			event:    "response.failed",
			json:     `{"type":"response.failed","response":{"id":"r","error":{"code":"server_error","message":"boom"}}}`,
			wantTerm: true,
			wantErr:  true,
		},
		{
			name:  "unknown event ignored",
			event: "response.something_new",
			json:  `{"type":"response.something_new"}`,
		},
		{
			name:  "unhandled output_text.done ignored",
			event: "response.output_text.done",
			json:  `{"type":"response.output_text.done","text":"final"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ev codexResponsesSSEEvent
			if err := json.Unmarshal([]byte(tt.json), &ev); err != nil {
				t.Fatalf("fixture unmarshal: %v", err)
			}
			if ev.Type != tt.event {
				t.Fatalf("fixture type %q != %q", ev.Type, tt.event)
			}
			acc := &codexStreamAccumulator{}
			terminal, err := handleResponsesSSEEvent(ev, acc)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if terminal != tt.wantTerm {
				t.Fatalf("terminal = %v, want %v", terminal, tt.wantTerm)
			}
			if tt.wantAcc != nil {
				tt.wantAcc(t, acc)
			}
		})
	}
}

// codexStreamAccumulatorCheck asserts folded state after one event.
type codexStreamAccumulatorCheck func(t *testing.T, acc *codexStreamAccumulator)

// TestCodexStreamRequestMarshal pins the stream field's wire shape: bool.
func TestCodexStreamRequestMarshal(t *testing.T) {
	b, err := json.Marshal(struct {
		Stream codexResponsesStreamRequest `json:"stream"`
	}{Stream: codexResponsesStreamRequest{Enabled: true}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `{"stream":true}` {
		t.Errorf("marshal = %s, want {\"stream\":true}", b)
	}
	b, err = json.Marshal(struct {
		Stream codexResponsesStreamRequest `json:"stream"`
	}{})
	if err != nil {
		t.Fatalf("marshal false: %v", err)
	}
	if string(b) != `{"stream":false}` {
		t.Errorf("marshal = %s, want {\"stream\":false}", b)
	}

	// Unmarshal round-trip: bool form.
	var s codexResponsesStreamRequest
	if err := json.Unmarshal([]byte("true"), &s); err != nil || !s.Enabled {
		t.Errorf("unmarshal true: s=%+v err=%v", s, err)
	}
	if err := json.Unmarshal([]byte("false"), &s); err != nil || s.Enabled {
		t.Errorf("unmarshal false: s=%+v err=%v", s, err)
	}
	// Object form accepted-and-treated-enabled.
	if err := json.Unmarshal([]byte(`{"foo":1}`), &s); err != nil || !s.Enabled {
		t.Errorf("unmarshal object: s=%+v err=%v", s, err)
	}
	// Garbage refused.
	if err := json.Unmarshal([]byte(`"nope"`), &s); err == nil {
		t.Error("unmarshal garbage should fail")
	}
}

// TestCodexNilDeltaFallsBackToChat pins the StreamingChatter nil-callback
// contract: no callback means the non-streaming exchange.
func TestCodexNilDeltaFallsBackToChat(t *testing.T) {
	srv, ch := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[]}`})
	client := newCodexClientForTest(t, srv.URL)
	if _, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "x"}}, nil); err != nil {
		t.Fatalf("ChatWithDeltaCallback(nil): %v", err)
	}
	req := <-ch
	if got := req.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q, want application/json (fallback path)", got)
	}
}

// TestCodexSSEBudgetRecordingOnStream proves usage extracted from
// response.completed is recorded against the budget on the streaming path.
func TestCodexSSEBudgetRecordingOnStream(t *testing.T) {
	events := codexRealisticStream(map[string]any{
		"id":    "resp_1",
		"usage": map[string]any{"input_tokens": 100, "output_tokens": 50},
	})
	srv, _ := codexTestServerWithResponder(t, http.StatusOK, "text/event-stream", codexSSEBody(events))
	budget := NewBudgetFromDefaults(slog.New(slog.DiscardHandler))
	client := newCodexClientForTest(t, srv.URL, WithCodexBudget(budget))
	if _, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "x"}},
		func(string) error { return nil }); err != nil {
		t.Fatalf("ChatWithDeltaCallback: %v", err)
	}
	status := budget.GetStatus()
	if status.HourlyUsed != 150 {
		t.Errorf("HourlyUsed = %d, want 150", status.HourlyUsed)
	}
}
