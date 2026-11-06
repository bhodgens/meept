package llm

// TTFT capture tests (llm-serving-tiers leaf 04, tasks T2/T3):
//   - streaming stubs emitting multiple deltas produce TTFTMS >= 0 on the
//     record (OpenAI-compatible and Anthropic clients);
//   - non-streaming calls and errored calls record TTFTMS -1;
//   - thinking/reasoning deltas never start the clock (first CONTENT
//     delta only, R0 Section 3);
//   - the ttftMS helper pins the cache-hit -1 sentinel directly (a cache
//     hit arms no clock, so any ledgered row carries -1).
//
// Records are observed through the SetLLMCallObserver seam (leaf 01), the
// same channel recordUsageStore feeds the llm_calls store with.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	appmetrics "github.com/caimlas/meept/internal/metrics"
)

// ttftObsChan is the observer channel type (one record per completed call).
type ttftObsChan = chan appmetrics.LLMCallRecord

// awaitRecord drains exactly one record off the observer channel.
func awaitRecord(t *testing.T, ch ttftObsChan) appmetrics.LLMCallRecord {
	t.Helper()
	select {
	case rec := <-ch:
		return rec
	case <-time.After(3 * time.Second):
		t.Fatal("observer never fired")
		return appmetrics.LLMCallRecord{}
	}
}

// ttftStreamServer is an OpenAI-compatible SSE stub emitting three content
// deltas. With reasoningFirst, a reasoning_content delta lands IMMEDIATELY
// and the first content delta after a 60ms quiet gap -- pinning that the
// clock starts at the first CONTENT delta, never the reasoning one.
func ttftStreamServer(reasoningFirst bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		if reasoningFirst {
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"hmm\"}}]}\n\n"))
			f.Flush()
			time.Sleep(60 * time.Millisecond)
		} else {
			time.Sleep(60 * time.Millisecond)
		}
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"one\"}}]}\n\n"))
		f.Flush()
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"two\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"three\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":3,\"total_tokens\":12}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
}

// TestTTFT_OpenAIStreamingCapturesFirstDelta pins T2's happy path: a
// streaming call whose first content delta lands after a measurable delay
// records TTFTMS >= 0 and never above the full-call latency (the stream
// sleeps 60ms BEFORE the first delta; the remaining deltas flow at once).
func TestTTFT_OpenAIStreamingCapturesFirstDelta(t *testing.T) {
	server := ttftStreamServer(false)
	defer server.Close()

	client := NewClient(&ModelConfig{BaseURL: server.URL, ModelID: "test-model", ProviderID: "prov-x"})
	obsCh := make(ttftObsChan, 4)
	client.SetLLMCallObserver(func(rec appmetrics.LLMCallRecord) { obsCh <- rec })

	_, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}},
		func(string) error { return nil })
	if err != nil {
		t.Fatalf("ChatWithDeltaCallback: %v", err)
	}
	rec := awaitRecord(t, obsCh)
	if rec.TTFTMS < 0 {
		t.Errorf("streaming TTFTMS = %d, want >= 0", rec.TTFTMS)
	}
	if rec.TTFTMS > rec.LatencyMs {
		t.Errorf("TTFTMS (%d) > LatencyMs (%d): first delta cannot precede the call end", rec.TTFTMS, rec.LatencyMs)
	}
}

// TestTTFT_OpenAIReasoningDeltaDoesNotStartClock pins the R0 Section 3
// clock definition: a reasoning_content delta arriving FIRST does not
// start TTFT -- the first CONTENT delta does. The reasoning delta is
// written immediately and the content delta after 60ms, so a clock
// started on reasoning would truncate TTFTMS to ~0 while the correct
// capture is >= ~60ms.
func TestTTFT_OpenAIReasoningDeltaDoesNotStartClock(t *testing.T) {
	server := ttftStreamServer(true)
	defer server.Close()

	client := NewClient(&ModelConfig{BaseURL: server.URL, ModelID: "test-model", ProviderID: "prov-x"})
	obsCh := make(ttftObsChan, 4)
	client.SetLLMCallObserver(func(rec appmetrics.LLMCallRecord) { obsCh <- rec })

	_, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}},
		func(string) error { return nil })
	if err != nil {
		t.Fatalf("ChatWithDeltaCallback: %v", err)
	}
	rec := awaitRecord(t, obsCh)
	if rec.TTFTMS < 50 {
		t.Errorf("TTFTMS = %d, want >= 50 (clock must start at first CONTENT delta, not the earlier reasoning delta)", rec.TTFTMS)
	}
}

// TestTTFT_OpenAINonStreamingRecordsSentinel pins the -1 sentinel on the
// non-streaming path (T2): no streaming clock is armed, so the ledger row
// carries -1.
func TestTTFT_OpenAINonStreamingRecordsSentinel(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id": "chatcmpl-t", "object": "chat.completion", "model": "test-model",
			"choices": []map[string]any{
				{"index": 0,
					"message":       map[string]any{"role": "assistant", "content": "hi"},
					"finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 4, "completion_tokens": 1, "total_tokens": 5},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client := NewClient(&ModelConfig{BaseURL: server.URL, ModelID: "test-model", ProviderID: "prov-x"})
	obsCh := make(ttftObsChan, 4)
	client.SetLLMCallObserver(func(rec appmetrics.LLMCallRecord) { obsCh <- rec })

	if _, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	rec := awaitRecord(t, obsCh)
	if rec.TTFTMS != -1 {
		t.Errorf("non-streaming TTFTMS = %d, want -1", rec.TTFTMS)
	}
}

// TestTTFT_SentinelSemantics pins the ttftMS computation itself, which is
// the mechanism behind the cache-hit -1 sentinel (T2): a cache hit arms
// no clock, so any row it ever ledgered computes -1. Covered shapes:
// nil options, armed-but-no-delta (error/pre-stream failure), armed with
// a real first-delta instant, and a clock that appears to run backwards
// (clamps to 0, never negative).
func TestTTFT_SentinelSemantics(t *testing.T) {
	if got := ttftMS(nil); got != -1 {
		t.Errorf("ttftMS(nil) = %d, want -1", got)
	}
	// Cache hit: nothing armed at all.
	if got := ttftMS(&chatOptions{}); got != -1 {
		t.Errorf("ttftMS(unarmed) = %d, want -1", got)
	}
	// Armed clock but no delta ever arrived (error path): -1, not a
	// full-elapsed number.
	armed := &chatOptions{ttftStart: time.Now().Add(-100 * time.Millisecond)}
	if got := ttftMS(armed); got != -1 {
		t.Errorf("ttftMS(armed, no delta) = %d, want -1", got)
	}
	// Armed and captured: delta at +30ms => 30.
	armed.ttftCaptured = true
	armed.ttftFirstDelta = armed.ttftStart.Add(30 * time.Millisecond)
	if got := ttftMS(armed); got != 30 {
		t.Errorf("ttftMS(armed, delta at +30ms) = %d, want 30", got)
	}
	// Clock skew clamp: a first delta stamped before dispatch clamps to
	// 0 (never negative).
	armed.ttftFirstDelta = armed.ttftStart.Add(-5 * time.Millisecond)
	if got := ttftMS(armed); got != 0 {
		t.Errorf("ttftMS(delta before start) = %d, want 0 (clamped)", got)
	}
	// captureFirstDelta is idempotent: only the FIRST delta wins.
	opts := &chatOptions{ttftStart: time.Now()}
	captureFirstDelta(opts)
	first := opts.ttftFirstDelta
	time.Sleep(5 * time.Millisecond)
	captureFirstDelta(opts)
	if !opts.ttftFirstDelta.Equal(first) {
		t.Error("captureFirstDelta overwrote the first delta instant; must be first-wins")
	}
	captureFirstDelta(nil) // nil-safety
}

// TestTTFT_OpenAIErrorRecordsSentinel pins the errored-call -1 sentinel
// (T2): a call that fails before any content delta (HTTP 500 on the
// stream open) ledgered its row with TTFTMS -1. The STREAMING loop is
// the fast lane: its throttle/server-error short-retry budget is
// exhausted in milliseconds (immediate re-dispatch, no long backoff at
// the default test posture), so the ledger row lands quickly.
func TestTTFT_OpenAIErrorRecordsSentinel(t *testing.T) {
	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"exploded"}}`))
	}))
	defer errServer.Close()

	client := NewClient(&ModelConfig{BaseURL: errServer.URL, ModelID: "test-model", ProviderID: "prov-x"})
	// Fast failure-policy config keeps the in-loop server-error backoffs
	// tiny (leaf rule: injected config, no long real sleeps; the default
	// BaseThrottle is 30s and the loop would burn 3 attempts of it).
	client.SetFailurePolicyConfig(&FailurePolicyConfig{
		Horizon:      time.Hour,
		BaseThrottle: time.Millisecond,
		PollFloor:    2 * time.Millisecond,
		ShortRetries: 3,
	})
	obsCh := make(ttftObsChan, 4)
	client.SetLLMCallObserver(func(rec appmetrics.LLMCallRecord) { obsCh <- rec })

	if _, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}},
		func(string) error { return nil }); err == nil {
		t.Fatal("expected the 500 to surface as an error")
	}
	rec := awaitRecord(t, obsCh)
	if !rec.IsError {
		t.Errorf("errored call recorded IsError=false (error_message=%q)", rec.ErrorMessage)
	}
	if rec.TTFTMS != -1 {
		t.Errorf("errored TTFTMS = %d, want -1", rec.TTFTMS)
	}
}

// TestTTFT_AnthropicStreamingCapturesFirstDelta pins T3's capture half:
// the Anthropic streaming parser stamps the first TEXT delta, thinking
// deltas before it do not start the clock, and the record carries
// TTFTMS >= the pre-text quiet gap.
func TestTTFT_AnthropicStreamingCapturesFirstDelta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\n" +
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"usage\":{\"input_tokens\":10}}}\n\n"))
		// Thinking delta IMMEDIATELY (must not start the clock)...
		_, _ = w.Write([]byte("event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"id\":\"b0\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"let me think\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_stop\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
		// ...then a 60ms quiet gap before the first TEXT delta.
		time.Sleep(60 * time.Millisecond)
		_, _ = w.Write([]byte("event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"id\":\"b1\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"answer text\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_stop\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":1}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":4}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	client := NewAnthropicClient(&ModelConfig{
		BaseURL:    server.URL,
		ProviderID: ProviderIDAnthropic,
		ModelID:    "claude-ttft",
		APIKey:     "test-key",
		MaxTokens:  64,
	}, WithAnthropicLogger(discardLogger()))
	obsCh := make(ttftObsChan, 4)
	client.SetLLMCallObserver(func(rec appmetrics.LLMCallRecord) { obsCh <- rec })

	_, err := client.ChatWithProgress(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(ProgressStage, string) {})
	if err != nil {
		t.Fatalf("ChatWithProgress: %v", err)
	}
	rec := awaitRecord(t, obsCh)
	if rec.TTFTMS < 50 {
		t.Errorf("Anthropic TTFTMS = %d, want >= 50 (clock starts at the first TEXT delta, not the earlier thinking delta)", rec.TTFTMS)
	}
}

// TestTTFT_AnthropicNonStreamingRecordsSentinel pins the Anthropic -1
// sentinel on the non-streaming Chat path (T3 symmetry with T2).
func TestTTFT_AnthropicNonStreamingRecordsSentinel(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id": "msg_x", "type": "message", "role": "assistant", "model": "claude-ttft",
			"content":     []map[string]any{{"type": "text", "text": "plain answer"}},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 8, "output_tokens": 2},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client := NewAnthropicClient(&ModelConfig{
		BaseURL:    server.URL,
		ProviderID: ProviderIDAnthropic,
		ModelID:    "claude-ttft",
		APIKey:     "test-key",
		MaxTokens:  64,
	}, WithAnthropicLogger(discardLogger()))
	obsCh := make(ttftObsChan, 4)
	client.SetLLMCallObserver(func(rec appmetrics.LLMCallRecord) { obsCh <- rec })

	if _, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	rec := awaitRecord(t, obsCh)
	if rec.TTFTMS != -1 {
		t.Errorf("Anthropic non-streaming TTFTMS = %d, want -1", rec.TTFTMS)
	}
}
