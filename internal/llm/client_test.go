package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
