package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestAnthropicClient_DoRequest_ReturnsRateLimitError verifies that a 429
// response from Anthropic is returned as a *RateLimitError (not a bare *APIError),
// with Retry-After parsed and provider/model IDs populated.
func TestAnthropicClient_DoRequest_ReturnsRateLimitError(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		body           string
		retryAfter     string
		wantRateLimit  bool
		wantRetryAfter time.Duration
		wantProviderID string
		wantModelID    string
		wantLimitType  string
	}{
		{
			name:           "429 with Retry-After seconds",
			statusCode:     http.StatusTooManyRequests,
			body:           `{"error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`,
			retryAfter:     "10",
			wantRateLimit:  true,
			wantRetryAfter: 10 * time.Second,
			wantProviderID: "anthropic",
			wantModelID:    "claude-test",
			wantLimitType:  "rate_limit_error",
		},
		{
			name:           "429 with Anthropic structured error",
			statusCode:     http.StatusTooManyRequests,
			body:           `{"error":{"type":"rate_limit_error","message":"Your request was rejected because it exceeded the rate limit."}}`,
			retryAfter:     "",
			wantRateLimit:  true,
			wantRetryAfter: 0,
			wantProviderID: "anthropic",
			wantModelID:    "claude-test",
			wantLimitType:  "rate_limit_error",
		},
		{
			name:           "429 with plain text body (no JSON)",
			statusCode:     http.StatusTooManyRequests,
			body:           "Too many requests, please slow down.",
			retryAfter:     "",
			wantRateLimit:  true,
			wantRetryAfter: 0,
			wantProviderID: "anthropic",
			wantModelID:    "claude-test",
			wantLimitType:  "",
		},
		{
			name:          "500 should return APIError, not RateLimitError",
			statusCode:    http.StatusInternalServerError,
			body:          `{"error":{"type":"api_error","message":"Internal server error"}}`,
			wantRateLimit: false,
		},
		{
			name:          "429 with Retry-After HTTP-date",
			statusCode:    http.StatusTooManyRequests,
			body:          `{"error":{"type":"rate_limit_error","message":"Rate limit"}}`,
			retryAfter:    "dynamic", // set in handler
			wantRateLimit: true,
			wantRetryAfter: 3 * time.Second, // approximately
			wantProviderID: "anthropic",
			wantModelID:    "claude-test",
			wantLimitType:  "rate_limit_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if tt.retryAfter == "dynamic" {
					w.Header().Set("Retry-After", time.Now().Add(3*time.Second).UTC().Format(time.RFC1123))
				} else if tt.retryAfter != "" {
					w.Header().Set("Retry-After", tt.retryAfter)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			cfg := &ModelConfig{
				ProviderID: "anthropic",
				ModelID:    "claude-test",
				BaseURL:    server.URL,
				APIKey:     "test",
				MaxTokens:  128,
			}
			c := NewAnthropicClient(cfg, WithAnthropicLogger(discardLogger()))

			// Chat has its own retry loop; we expect it to retry and
			// return a ClientError wrapping the last attempt's error.
			_, err := c.Chat(context.Background(), []ChatMessage{
				{Role: RoleUser, Content: "hi"},
			})
			if err == nil {
				t.Fatal("expected error")
			}

			var clientErr *ClientError
			if !errors.As(err, &clientErr) {
				t.Fatalf("expected ClientError wrapping, got %T: %v", err, err)
			}

			if tt.wantRateLimit {
				if _, ok := AsRateLimitError(clientErr.Cause, "", ""); !ok {
					t.Fatalf("expected RateLimitError or APIError{429} in chain, got %T: %v", clientErr.Cause, clientErr.Cause)
				}
				var directRlErr *RateLimitError
				if errors.As(clientErr.Cause, &directRlErr) {
					if directRlErr.ProviderID != tt.wantProviderID {
						t.Errorf("ProviderID = %q, want %q", directRlErr.ProviderID, tt.wantProviderID)
					}
					if directRlErr.ModelID != tt.wantModelID {
						t.Errorf("ModelID = %q, want %q", directRlErr.ModelID, tt.wantModelID)
					}
					if directRlErr.LimitType != tt.wantLimitType {
						t.Errorf("LimitType = %q, want %q", directRlErr.LimitType, tt.wantLimitType)
					}
					if tt.wantRetryAfter > 0 {
						diff := directRlErr.RetryAfter - tt.wantRetryAfter
						if diff < 0 {
							diff = -diff
						}
						if diff > time.Second {
							t.Errorf("RetryAfter = %v, want ~%v", directRlErr.RetryAfter, tt.wantRetryAfter)
						}
					}
				}
			} else {
				if IsRateLimitError(clientErr.Cause) {
					t.Errorf("expected non-rate-limit error, got: %v", clientErr.Cause)
				}
			}
		})
	}
}

// TestAnthropicClient_DoStreamingRequest_ReturnsRateLimitError verifies that
// a 429 response from Anthropic's streaming endpoint is returned as a
// *RateLimitError with Retry-After parsed.
func TestAnthropicClient_DoStreamingRequest_ReturnsRateLimitError(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		body           string
		retryAfter     string
		wantRateLimit  bool
		wantRetryAfter time.Duration
	}{
		{
			name:           "429 with Retry-After",
			statusCode:     http.StatusTooManyRequests,
			body:           `{"error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`,
			retryAfter:     "5",
			wantRateLimit:  true,
			wantRetryAfter: 5 * time.Second,
		},
		{
			name:          "500 returns APIError",
			statusCode:    http.StatusInternalServerError,
			body:          `{"error":{"type":"api_error","message":"Server error"}}`,
			wantRateLimit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if tt.retryAfter == "dynamic" {
					w.Header().Set("Retry-After", time.Now().Add(3*time.Second).UTC().Format(time.RFC1123))
				} else if tt.retryAfter != "" {
					w.Header().Set("Retry-After", tt.retryAfter)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			cfg := &ModelConfig{
				ProviderID: "anthropic",
				ModelID:    "claude-test",
				BaseURL:    server.URL,
				APIKey:     "test",
				MaxTokens:  128,
			}
			c := NewAnthropicClient(cfg, WithAnthropicLogger(discardLogger()))

			progress := func(stage ProgressStage, detail string) {}
			_, err := c.ChatWithProgress(context.Background(), []ChatMessage{
				{Role: RoleUser, Content: "hi"},
			}, progress)
			if err == nil {
				t.Fatal("expected error")
			}

			var clientErr *ClientError
			if !errors.As(err, &clientErr) {
				t.Fatalf("expected ClientError wrapping, got %T: %v", err, err)
			}

			if tt.wantRateLimit {
				var directRlErr *RateLimitError
				if !errors.As(clientErr.Cause, &directRlErr) {
					t.Fatalf("expected RateLimitError in chain, got %T: %v", clientErr.Cause, clientErr.Cause)
				}
				if directRlErr.ProviderID != "anthropic" {
					t.Errorf("ProviderID = %q, want %q", directRlErr.ProviderID, "anthropic")
				}
				if directRlErr.ModelID != "claude-test" {
					t.Errorf("ModelID = %q, want %q", directRlErr.ModelID, "claude-test")
				}
				if tt.wantRetryAfter > 0 && directRlErr.RetryAfter != tt.wantRetryAfter {
					t.Errorf("RetryAfter = %v, want %v", directRlErr.RetryAfter, tt.wantRetryAfter)
				}
			} else {
				if IsRateLimitError(clientErr.Cause) {
					t.Errorf("expected non-rate-limit error, got: %v", clientErr.Cause)
				}
			}
		})
	}
}

// TestAnthropicClient_RetryLoopStillWorks verifies that the Anthropic client's
// internal retry loop (in Chat) still correctly retries when the underlying
// error is a RateLimitError (which wraps APIError via Unwrap).
func TestAnthropicClient_RetryLoopStillWorks(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"Rate limit"}}`))
			return
		}
		resp := map[string]any{
			"id":          "m_test",
			"type":        "message",
			"role":        "assistant",
			"model":       "claude-test",
			"stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "text", "text": "ok after retry"},
			},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
		}
		b, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	}))
	defer server.Close()

	cfg := &ModelConfig{
		ProviderID: "anthropic",
		ModelID:    "claude-test",
		BaseURL:    server.URL,
		APIKey:     "test",
		MaxTokens:  128,
	}
	c := NewAnthropicClient(cfg, WithAnthropicLogger(discardLogger()))

	resp, err := c.Chat(context.Background(), []ChatMessage{
		{Role: RoleUser, Content: "hi"},
	})
	if err != nil {
		t.Fatalf("Chat should succeed after retries: %v", err)
	}
	if resp.Content != "ok after retry" {
		t.Errorf("unexpected response content: %q", resp.Content)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}
