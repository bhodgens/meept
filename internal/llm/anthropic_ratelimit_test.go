package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestAnthropicClient_DoRequest_ReturnsRateLimitError verifies 429 handling:
// quota-window shapes (rate_limit_error / quota_exceeded) return
// QuotaResetError immediately (quota-reset-resilience leaf 01 Task 4d),
// while plain-text 429 bodies keep the legacy RateLimitError path.
func TestAnthropicClient_DoRequest_ReturnsRateLimitError(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		body           string
		retryAfter     string
		wantQuota      bool
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
			wantQuota:      true,
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
			wantQuota:      true,
			wantRetryAfter: 0,
			wantProviderID: "anthropic",
			wantModelID:    "claude-test",
			wantLimitType:  "rate_limit_error",
		},
		{
			name:           "429 with quota_exceeded type",
			statusCode:     http.StatusTooManyRequests,
			body:           `{"type":"error","error":{"type":"quota_exceeded","message":"Spend cap reached"}}`,
			retryAfter:     "",
			wantQuota:      true,
			wantProviderID: "anthropic",
			wantModelID:    "claude-test",
			wantLimitType:  "quota_exceeded",
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
			name:           "429 with Retry-After HTTP-date",
			statusCode:     http.StatusTooManyRequests,
			body:           `{"error":{"type":"rate_limit_error","message":"Rate limit"}}`,
			retryAfter:     "dynamic", // set in handler
			wantQuota:      true,
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
				APIKey:     "test-key",
				MaxTokens:  128,
			}
			c := NewAnthropicClient(cfg, WithAnthropicLogger(discardLogger()))

			// Chat has its own retry loop. Quota errors exit immediately;
			// rate-limit errors retry and return a ClientError wrapping
			// the last attempt's error.
			_, err := c.Chat(context.Background(), []ChatMessage{
				{Role: RoleUser, Content: "hi"},
			})
			if err == nil {
				t.Fatal("expected error")
			}

			if tt.wantQuota {
				var quotaErr *QuotaResetError
				if !errors.As(err, &quotaErr) {
					t.Fatalf("expected QuotaResetError, got %T: %v", err, err)
				}
				if quotaErr.ProviderID != tt.wantProviderID {
					t.Errorf("ProviderID = %q, want %q", quotaErr.ProviderID, tt.wantProviderID)
				}
				if quotaErr.ModelID != tt.wantModelID {
					t.Errorf("ModelID = %q, want %q", quotaErr.ModelID, tt.wantModelID)
				}
				if tt.wantLimitType != "" && quotaErr.Code != tt.wantLimitType {
					t.Errorf("Code = %q, want %q", quotaErr.Code, tt.wantLimitType)
				}
				if tt.wantRetryAfter > 0 {
					// Allow tolerance for the RFC1123 date round-trip.
					diff := quotaErr.RetryAfter - tt.wantRetryAfter
					if diff < -2*time.Second || diff > 2*time.Second {
						t.Errorf("RetryAfter = %v, want ~%v", quotaErr.RetryAfter, tt.wantRetryAfter)
					}
				}
				return
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
				}
			}
		})
	}
}
