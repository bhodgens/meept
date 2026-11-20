package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientChat(t *testing.T) {
	// Create a mock server
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}

		if !strings.HasSuffix(r.URL.Path, "/v1/chat/completions") {
			t.Errorf("Expected /v1/chat/completions, got %s", r.URL.Path)
		}

		// Return a mock response
		resp := ChatResponse{
			ID:    "chatcmpl-123",
			Model: "test-model",
			Choices: []Choice{
				{
					Index: 0,
					Message: ResponseMessage{
						Role:    "assistant",
						Content: json.RawMessage(`"Hello, world!"`),
					},
					FinishReason: "stop",
				},
			},
			Usage: struct {
				PromptTokens        int `json:"prompt_tokens"`
				CompletionTokens    int `json:"completion_tokens"`
				TotalTokens         int `json:"total_tokens"`
				PromptTokensDetails struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
				CompletionTokensDetails struct {
					ReasoningTokens int `json:"reasoning_tokens"`
				} `json:"completion_tokens_details"`
			}{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client := NewClient(&ModelConfig{
		BaseURL:     server.URL + "/v1",
		ModelID:     "test-model",
		Temperature: 0.7,
		MaxTokens:   100,
	})

	messages := []ChatMessage{
		{Role: RoleUser, Content: "Hello"},
	}

	resp, err := client.Chat(context.Background(), messages)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if resp.Content != "Hello, world!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello, world!")
	}

	if resp.Model != "test-model" {
		t.Errorf("Model = %q, want %q", resp.Model, "test-model")
	}

	if resp.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", resp.Usage.TotalTokens)
	}
}

func TestClientChatWithTools(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify tools were sent
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)

		if _, ok := req["tools"]; !ok {
			t.Error("Expected tools in request")
		}

		// Return response with tool call
		resp := ChatResponse{
			ID:    "chatcmpl-123",
			Model: "test-model",
			Choices: []Choice{
				{
					Message: ResponseMessage{
						Role: "assistant",
						ToolCalls: []RawToolCall{
							{
								ID:   "call_123",
								Type: "function",
								Function: struct {
									Name      string `json:"name"`
									Arguments string `json:"arguments"`
								}{
									Name:      "get_weather",
									Arguments: `{"location": "London"}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client := NewClient(&ModelConfig{
		BaseURL: server.URL,
		ModelID: "test-model",
	})

	tools := []ToolDefinition{
		NewToolDefinition("get_weather", "Get weather for a location", FunctionParameters{
			Type: "object",
			Properties: map[string]ParameterProperty{
				"location": {Type: "string", Description: "City name"},
			},
			Required: []string{"location"},
		}),
	}

	resp, err := client.Chat(context.Background(), []ChatMessage{
		{Role: RoleUser, Content: "What's the weather in London?"},
	}, WithTools(tools))

	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if !resp.HasToolCalls() {
		t.Error("Expected tool calls")
	}

	if len(resp.ToolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(resp.ToolCalls))
	}

	tc := resp.ToolCalls[0]
	if tc.Function.Name != "get_weather" {
		t.Errorf("Tool name = %q, want get_weather", tc.Function.Name)
	}

	args, err := tc.ParsedArguments()
	if err != nil {
		t.Fatalf("Failed to parse arguments: %v", err)
	}

	if args["location"] != "London" {
		t.Errorf("location = %v, want London", args["location"])
	}
}

func TestClientChatRetry(t *testing.T) {
	attempts := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			// Return 503 to trigger retry
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("Service unavailable"))
			return
		}

		// Success on third attempt
		resp := ChatResponse{
			ID:    "chatcmpl-123",
			Model: "test-model",
			Choices: []Choice{
				{
					Message: ResponseMessage{
						Role:    "assistant",
						Content: json.RawMessage(`"Success after retry"`),
					},
					FinishReason: "stop",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client := NewClient(&ModelConfig{
		BaseURL: server.URL,
		ModelID: "test-model",
	})
	// Fast policy: the test still exercises 2 failed 503 attempts before
	// success (3 server hits), but the in-loop backoff is ~1ms instead of
	// the production 30s/60s default steps (which cost 90 real seconds).
	client.SetFailurePolicyConfig(fastFailurePolicyCfg)

	resp, err := client.Chat(context.Background(), []ChatMessage{
		{Role: RoleUser, Content: "Hello"},
	})

	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if resp.Content != "Success after retry" {
		t.Errorf("Content = %q, want %q", resp.Content, "Success after retry")
	}

	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

func TestClientChatAPIError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Invalid request"))
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client := NewClient(&ModelConfig{
		BaseURL: server.URL,
		ModelID: "test-model",
	})

	_, err := client.Chat(context.Background(), []ChatMessage{
		{Role: RoleUser, Content: "Hello"},
	})

	if err == nil {
		t.Fatal("Expected error")
	}

	apiErr := &APIError{}
	ok := errors.As(err, &apiErr)
	if !ok {
		t.Fatalf("Expected APIError, got %T", err)
	}

	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusBadRequest)
	}
}

func TestClientWithBudget(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ChatResponse{
			ID:    "chatcmpl-123",
			Model: "test-model",
			Choices: []Choice{
				{
					Message: ResponseMessage{
						Role:    "assistant",
						Content: json.RawMessage(`"Hello"`),
					},
					FinishReason: "stop",
				},
			},
			Usage: struct {
				PromptTokens        int `json:"prompt_tokens"`
				CompletionTokens    int `json:"completion_tokens"`
				TotalTokens         int `json:"total_tokens"`
				PromptTokensDetails struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
				CompletionTokensDetails struct {
					ReasoningTokens int `json:"reasoning_tokens"`
				} `json:"completion_tokens_details"`
			}{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	budget := NewBudget(BudgetConfig{
		HourlyLimit:    100,
		DailyLimit:     500,
		Aggressiveness: 1.0,
	}, nil)

	client := NewClient(&ModelConfig{
		BaseURL: server.URL,
		ModelID: "test-model",
	}, WithBudget(budget))

	// First request should succeed
	_, err := client.Chat(context.Background(), []ChatMessage{
		{Role: RoleUser, Content: "Hello"},
	})
	if err != nil {
		t.Fatalf("First chat failed: %v", err)
	}

	// Verify budget was updated
	status := budget.GetStatus()
	if status.HourlyUsed != 15 {
		t.Errorf("HourlyUsed = %d, want 15", status.HourlyUsed)
	}
}

func TestClientBudgetExceeded(t *testing.T) {
	// Don't need a server since request should be blocked
	budget := NewBudget(BudgetConfig{
		HourlyLimit:    100,
		DailyLimit:     500,
		Aggressiveness: 1.0,
	}, nil)

	// Exhaust budget
	budget.RecordUsage(TokenUsage{TotalTokens: 150})

	client := NewClient(&ModelConfig{
		BaseURL: "http://localhost:9999", // Won't be called
		ModelID: "test-model",
	}, WithBudget(budget))

	_, err := client.Chat(context.Background(), []ChatMessage{
		{Role: RoleUser, Content: "Hello"},
	})

	if err == nil {
		t.Fatal("Expected error")
	}

	budgetExceededError := &BudgetExceededError{}
	ok := errors.As(err, &budgetExceededError)
	if !ok {
		t.Errorf("Expected BudgetExceededError, got %T: %v", err, err)
	}
}

func TestContentString_PlainString(t *testing.T) {
	msg := ResponseMessage{Content: json.RawMessage(`"Hello, world!"`)}
	got := msg.ContentString()
	if got != "Hello, world!" {
		t.Errorf("ContentString() = %q, want %q", got, "Hello, world!")
	}
}

func TestContentString_Nil(t *testing.T) {
	msg := ResponseMessage{Content: nil}
	got := msg.ContentString()
	if got != "" {
		t.Errorf("ContentString() = %q, want empty string", got)
	}
}

func TestContentString_ArrayBlocks(t *testing.T) {
	msg := ResponseMessage{
		Content: json.RawMessage(`[{"type":"text","text":"Hello"},{"type":"text","text":"World"}]`),
	}
	got := msg.ContentString()
	if got != "Hello\nWorld" {
		t.Errorf("ContentString() = %q, want %q", got, "Hello\nWorld")
	}
}

func TestParseResponse_CachedTokens(t *testing.T) {
	raw := `{
		"id": "chatcmpl-1",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "gpt-4",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "hi"}, "finish_reason": "stop"}],
		"usage": {
			"prompt_tokens": 1000,
			"completion_tokens": 10,
			"total_tokens": 1010,
			"prompt_tokens_details": {"cached_tokens": 800}
		}
	}`
	var chatResp ChatResponse
	if err := json.Unmarshal([]byte(raw), &chatResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if chatResp.Usage.PromptTokensDetails.CachedTokens != 800 {
		t.Errorf("CachedTokens = %d, want 800", chatResp.Usage.PromptTokensDetails.CachedTokens)
	}

	client := NewClient(&ModelConfig{
		BaseURL: "http://localhost",
		ModelID: "gpt-4",
	})
	resp, err := client.parseResponse(&chatResp)
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if resp.Usage.CachedTokens != 800 {
		t.Errorf("resp.Usage.CachedTokens = %d, want 800", resp.Usage.CachedTokens)
	}
	if resp.Usage.PromptTokens != 1000 {
		t.Errorf("resp.Usage.PromptTokens = %d, want 1000", resp.Usage.PromptTokens)
	}
}

func TestContentString_ArrayBlocksWithNonText(t *testing.T) {
	msg := ResponseMessage{
		Content: json.RawMessage(`[{"type":"image","url":"..."},{"type":"text","text":"desc"}]`),
	}
	got := msg.ContentString()
	if got != "desc" {
		t.Errorf("ContentString() = %q, want %q", got, "desc")
	}
}

func TestClientConcurrencyLimit(t *testing.T) {
	// Track concurrent requests
	var maxConcurrent int
	var currentConcurrent int
	var mu sync.Mutex

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		currentConcurrent++
		if currentConcurrent > maxConcurrent {
			maxConcurrent = currentConcurrent
		}
		mu.Unlock()

		defer func() {
			mu.Lock()
			currentConcurrent--
			mu.Unlock()
		}()

		// Simulate some latency to allow concurrent requests to overlap
		time.Sleep(50 * time.Millisecond)

		resp := ChatResponse{
			ID:    "chatcmpl-123",
			Model: "test-model",
			Choices: []Choice{
				{
					Message: ResponseMessage{
						Role:    "assistant",
						Content: json.RawMessage(`"Hello"`),
					},
					FinishReason: "stop",
				},
			},
			Usage: struct {
				PromptTokens        int `json:"prompt_tokens"`
				CompletionTokens    int `json:"completion_tokens"`
				TotalTokens         int `json:"total_tokens"`
				PromptTokensDetails struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
				CompletionTokensDetails struct {
					ReasoningTokens int `json:"reasoning_tokens"`
				} `json:"completion_tokens_details"`
			}{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	// Create client with concurrency limit of 2
	client := NewClient(&ModelConfig{
		BaseURL:        server.URL,
		ModelID:        "test-model",
		MaxConcurrency: 2,
	})

	// Launch 5 concurrent requests
	const numRequests = 5
	errChan := make(chan error, numRequests)
	var wg sync.WaitGroup

	for range numRequests {
		wg.Go(func() {
			_, err := client.Chat(context.Background(), []ChatMessage{
				{Role: RoleUser, Content: "Hello"},
			})
			errChan <- err
		})
	}

	wg.Wait()
	close(errChan)

	// Check all requests succeeded
	for err := range errChan {
		if err != nil {
			t.Fatalf("Chat failed: %v", err)
		}
	}

	// Verify concurrency was limited to 2
	if maxConcurrent > 2 {
		t.Errorf("maxConcurrent = %d, want <= 2", maxConcurrent)
	}
	if maxConcurrent < 2 {
		t.Errorf("maxConcurrent = %d, want >= 2 (limit was utilized)", maxConcurrent)
	}
}

// --- R2 admission gate pins (bughunt 2026-11-19 wave 3) -----------------
//
// The bare-client streaming variants (ChatWithProgress,
// ChatWithDeltaCallback) must carry the SAME pre-dispatch admission gate
// as Chat: a tiny-context model refuses the request BEFORE any upstream
// request is made, with the typed NonRetryable *ContextTooSmallError
// (never parked, never an alias failure). ContextLimit <= 0 (unknown)
// allows - byte-identical to pre-gate behavior.

// admissionTinyContextClient builds a client whose declared context
// limit (512) cannot fit admissionOversizedPrompt; contextLimit 0 means
// "unknown" (the gate must allow).
func admissionTinyContextClient(t *testing.T, srv *httptest.Server, contextLimit int) *Client {
	t.Helper()
	c := NewClient(&ModelConfig{
		ProviderID:   "openai",
		ModelID:      "gpt-tiny",
		BaseURL:      srv.URL,
		APIKey:       "test",
		MaxTokens:    16,
		ContextLimit: contextLimit,
	}, WithLogger(discardLogger()))
	c.SetFailurePolicyConfig(fastFailurePolicyCfg)
	return c
}

// admissionOversizedPrompt is ~1024 estimated tokens (rune/4), far past
// a 512-token context limit even before the output reserve.
func admissionOversizedPrompt() []ChatMessage {
	return []ChatMessage{{Role: RoleUser, Content: strings.Repeat("x", 4096)}}
}

// assertAdmissionVerdict checks the typed pre-dispatch verdict shape.
func assertAdmissionVerdict(t *testing.T, err error, hits int32) {
	t.Helper()
	verdict, ok := AsContextTooSmall(err)
	if !ok {
		t.Fatalf("err = %v (%T), want *ContextTooSmallError", err, err)
	}
	if !errors.Is(err, ErrContextTooSmall) {
		t.Errorf("errors.Is(err, ErrContextTooSmall) = false")
	}
	if !verdict.NonRetryable() {
		t.Error("admission verdict must be NonRetryable (never retried, never parked)")
	}
	if verdict.ProviderID != "openai" || verdict.ModelID != "gpt-tiny" {
		t.Errorf("verdict attribution = %s/%s, want openai/gpt-tiny", verdict.ProviderID, verdict.ModelID)
	}
	if verdict.ContextLimit != 512 || verdict.Reserve != 16 {
		t.Errorf("verdict limit/reserve = %d/%d, want 512/16 (min(MaxTokens,64))",
			verdict.ContextLimit, verdict.Reserve)
	}
	if verdict.EstInput <= 0 {
		t.Errorf("verdict EstInput = %d, want a positive estimate", verdict.EstInput)
	}
	if hits != 0 {
		t.Errorf("upstream hits = %d, want 0 (gate fires PRE-dispatch)", hits)
	}
}

// TestClientAdmissionGate_TinyContextRefusesAllVariantsPreDispatch: the
// tiny-context model refuses Chat, ChatWithProgress, and
// ChatWithDeltaCallback before any upstream request (hits stay 0).
func TestClientAdmissionGate_TinyContextRefusesAllVariantsPreDispatch(t *testing.T) {
	var hits int32
	srv := newScriptedServer(t, []scriptedResponse{
		{status: http.StatusOK, body: okChatBody()},
	}, &hits)
	defer srv.Close()

	msgs := admissionOversizedPrompt()

	c := admissionTinyContextClient(t, srv, 512)
	_, err := c.Chat(context.Background(), msgs)
	assertAdmissionVerdict(t, err, atomic.LoadInt32(&hits))

	c = admissionTinyContextClient(t, srv, 512)
	_, err = c.ChatWithProgress(context.Background(), msgs, func(ProgressStage, string) {})
	assertAdmissionVerdict(t, err, atomic.LoadInt32(&hits))

	c = admissionTinyContextClient(t, srv, 512)
	_, err = c.ChatWithDeltaCallback(context.Background(), msgs, func(string) error { return nil })
	assertAdmissionVerdict(t, err, atomic.LoadInt32(&hits))
}

// TestClientAdmissionGate_ProgressReportsErrorStage: the progress path
// surfaces the refusal through the Done stage like its other terminal
// errors, without ever reaching the thinking/dispatch stages.
func TestClientAdmissionGate_ProgressReportsErrorStage(t *testing.T) {
	var hits int32
	srv := newScriptedServer(t, []scriptedResponse{
		{status: http.StatusOK, body: okChatBody()},
	}, &hits)
	defer srv.Close()

	var stages []ProgressStage
	c := admissionTinyContextClient(t, srv, 512)
	_, err := c.ChatWithProgress(context.Background(), admissionOversizedPrompt(),
		func(stage ProgressStage, _ string) { stages = append(stages, stage) })
	if _, ok := AsContextTooSmall(err); !ok {
		t.Fatalf("err = %v, want *ContextTooSmallError", err)
	}
	if len(stages) == 0 || stages[len(stages)-1] != ProgressStageDone {
		t.Errorf("progress stages = %v, want terminal Done", stages)
	}
	for _, s := range stages {
		if s == ProgressStageThinking {
			t.Error("progress reached the thinking stage despite the pre-dispatch refusal")
		}
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Errorf("upstream hits = %d, want 0", got)
	}
}

// TestClientAdmissionGate_UnknownLimitAllowsAllVariants: ContextLimit 0
// (no declared bound) allows every variant - the request dispatches and
// succeeds byte-identically to the pre-gate client.
func TestClientAdmissionGate_UnknownLimitAllowsAllVariants(t *testing.T) {
	var hits int32
	srv := newScriptedServer(t, []scriptedResponse{
		{status: http.StatusOK, body: okChatBody()},
	}, &hits)
	defer srv.Close()

	msgs := admissionOversizedPrompt()

	c := admissionTinyContextClient(t, srv, 0)
	resp, err := c.Chat(context.Background(), msgs)
	if err != nil || resp == nil || resp.Content != "ok" {
		t.Fatalf("Chat with unknown limit = %v / %+v, want success", err, resp)
	}

	c = admissionTinyContextClient(t, srv, 0)
	resp, err = c.ChatWithProgress(context.Background(), msgs, nil)
	if err != nil || resp == nil || resp.Content != "ok" {
		t.Fatalf("ChatWithProgress with unknown limit = %v / %+v, want success", err, resp)
	}

	// Streaming variant needs an SSE body, not the JSON completion.
	streamHits := int32(0)
	streamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&streamHits, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-tiny\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-tiny\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer streamSrv.Close()

	c = admissionTinyContextClient(t, streamSrv, 0)
	resp, err = c.ChatWithDeltaCallback(context.Background(), msgs, func(string) error { return nil })
	if err != nil || resp == nil || resp.Content != "ok" {
		t.Fatalf("ChatWithDeltaCallback with unknown limit = %v / %+v, want success", err, resp)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("non-streaming hits = %d, want 2 (both variants dispatched)", got)
	}
	if got := atomic.LoadInt32(&streamHits); got != 1 {
		t.Errorf("streaming hits = %d, want 1 (dispatched)", got)
	}
}

// TestClientAdmissionGate_ChatBehaviorUnchanged: the Chat gate itself is
// untouched by the streaming fix - a request that FITS the declared
// limit dispatches normally (1 hit, success), and the refusal shape on
// an oversized prompt stays the typed verdict with zero hits.
func TestClientAdmissionGate_ChatBehaviorUnchanged(t *testing.T) {
	var hits int32
	srv := newScriptedServer(t, []scriptedResponse{
		{status: http.StatusOK, body: okChatBody()},
	}, &hits)
	defer srv.Close()

	// Fits: "hi" estimates 1 token + reserve 16 <= 512.
	c := admissionTinyContextClient(t, srv, 512)
	resp, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err != nil || resp == nil || resp.Content != "ok" {
		t.Fatalf("Chat fitting prompt = %v / %+v, want success", err, resp)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("hits = %d, want 1 (fitting prompt dispatched)", got)
	}

	// Oversized: same typed refusal as before the streaming fix.
	c = admissionTinyContextClient(t, srv, 512)
	_, err = c.Chat(context.Background(), admissionOversizedPrompt())
	assertAdmissionVerdict(t, err, atomic.LoadInt32(&hits)-1)
}
