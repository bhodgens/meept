package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestQuotaClient_ClassifiesQuotaErrors pins leaf 01 Task 4: quota-window
// 429/402 responses from the OpenAI-compat client return QuotaResetError
// WITHOUT re-entering the 3-attempt short-retry loop (server hit count == 1),
// while short-cycle retriable OpenRouter shapes keep RateLimitError behavior.
func TestQuotaClient_ClassifiesQuotaErrors(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		body         string
		wantQuota    bool
		wantCode     string
		wantAttempts int32
	}{
		{
			name:         "openai usage_limit_reached with resets_at",
			statusCode:   http.StatusTooManyRequests,
			body:         `{"error":{"type":"usage_limit_reached","message":"limit reached","resets_at":1893456000}}`,
			wantQuota:    true,
			wantCode:     "usage_limit_reached",
			wantAttempts: 1,
		},
		{
			name:         "402 insufficient_quota",
			statusCode:   http.StatusPaymentRequired,
			body:         `{"error":{"type":"insufficient_quota","message":"billing"}}`,
			wantQuota:    true,
			wantCode:     "insufficient_quota",
			wantAttempts: 1,
		},
		{
			name:         "openrouter short-cycle retriable stays RateLimitError",
			statusCode:   http.StatusTooManyRequests,
			body:         `{"error":{"message":"Provider returned 429): {\"error\":{\"type\":\"tpm_uncached_exceeded\",\"code\":\"tpm_uncached_exceeded\",\"retry_after\":2.0,\"retriable\":true}}","code":429}}`,
			wantQuota:    false,
			wantAttempts: defaultShortRetries,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var hits int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&hits, 1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			cfg := &ModelConfig{
				ProviderID: "openai",
				ModelID:    "gpt-test",
				BaseURL:    server.URL,
				APIKey:     "test-key",
				MaxTokens:  16,
			}
			c := NewClient(cfg, WithLogger(discardLogger()))
			// Fast policy: the rate-limit case still retries exactly
			// defaultShortRetries times (the assertion), but the in-loop
			// throttle waits are ~1ms instead of the 30s default.
			c.SetFailurePolicyConfig(fastFailurePolicyCfg)

			_, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
			if err == nil {
				t.Fatal("expected error")
			}

			if got := atomic.LoadInt32(&hits); got != tt.wantAttempts {
				t.Errorf("server hits = %d, want %d (quota must not short-retry)", got, tt.wantAttempts)
			}

			if tt.wantQuota {
				var quotaErr *QuotaResetError
				if !errors.As(err, &quotaErr) {
					t.Fatalf("expected QuotaResetError, got %T: %v", err, err)
				}
				if quotaErr.Code != tt.wantCode {
					t.Errorf("Code = %q, want %q", quotaErr.Code, tt.wantCode)
				}
				if quotaErr.ProviderID != "openai" || quotaErr.ModelID != "gpt-test" {
					t.Errorf("context = %s/%s, want openai/gpt-test", quotaErr.ProviderID, quotaErr.ModelID)
				}
				if quotaErr.MaxWait != DefaultQuotaMaxWait {
					t.Errorf("MaxWait = %v, want default %v", quotaErr.MaxWait, DefaultQuotaMaxWait)
				}
			} else if !IsRateLimitError(err) {
				t.Fatalf("expected RateLimitError, got %T: %v", err, err)
			}
		})
	}
}

// TestQuotaClient_ClassifyQuotaDecision unit-tests the classification split
// between quota-window errors and short-cycle rate limits.
func TestQuotaClient_ClassifyQuotaDecision(t *testing.T) {
	quotaBody := []byte(`{"error":{"type":"usage_limit_reached","resets_at":1893456000}}`)
	openRouterShort := []byte(`{"error":{"message":"429): {\"error\":{\"type\":\"tpm\",\"retry_after\":2.0,\"retriable\":true}}","code":429}}`)

	tests := []struct {
		name   string
		status int
		body   []byte
		detail *ProviderError
		want   bool
	}{
		{"429 quota body", http.StatusTooManyRequests, quotaBody, nil, true},
		{"402 any body", http.StatusPaymentRequired, []byte(`{}`), nil, true},
		{"429 openrouter short retriable", http.StatusTooManyRequests, openRouterShort, &ProviderError{
			Retriable: true, RetryAfter: 2 * time.Second,
		}, false},
		{"500 never quota", http.StatusInternalServerError, quotaBody, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyQuotaDecision(tt.status, tt.body, tt.detail); got != tt.want {
				t.Errorf("classifyQuotaDecision = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestQuotaClient_SetQuotaMaxWait verifies the config seam.
func TestQuotaClient_SetQuotaMaxWait(t *testing.T) {
	c := NewClient(&ModelConfig{}, WithLogger(discardLogger()))
	c.SetQuotaMaxWait(4 * time.Hour)
	if c.quotaMaxWait != 4*time.Hour {
		t.Errorf("quotaMaxWait = %v, want 4h", c.quotaMaxWait)
	}
	c.SetQuotaMaxWait(0) // zero is rejected by the nil-guard convention
	if c.quotaMaxWait != 4*time.Hour {
		t.Errorf("quotaMaxWait changed on zero set: %v", c.quotaMaxWait)
	}
}

// TestQuotaClient_DeltaCallbackQuotaErrorNoShortRetry pins the leaf-01
// invariant on the ChatWithDeltaCallback retry loop: a quota-window 429
// returns QuotaResetError and hits the server exactly ONCE, while a plain
// 429 keeps its pre-existing retry behavior (regression guard for the
// doStreamRequest quota-classification addition).
func TestQuotaClient_DeltaCallbackQuotaErrorNoShortRetry(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		body         string
		wantQuota    bool
		wantAttempts int32
	}{
		{
			name:         "quota 429 exits loop with one request",
			statusCode:   http.StatusTooManyRequests,
			body:         `{"error":{"type":"usage_limit_reached","message":"limit reached","resets_at":1893456000}}`,
			wantQuota:    true,
			wantAttempts: 1,
		},
		{
			name:         "402 payment required is quota",
			statusCode:   http.StatusPaymentRequired,
			body:         `{"error":{"type":"insufficient_quota","message":"billing"}}`,
			wantQuota:    true,
			wantAttempts: 1,
		},
		{
			name:         "plain 429 keeps retrying (rate-limit path)",
			statusCode:   http.StatusTooManyRequests,
			body:         `{"error":{"message":"slow down"}}`,
			wantQuota:    false,
			wantAttempts: defaultShortRetries,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var hits int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&hits, 1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			cfg := &ModelConfig{
				ProviderID: "openai",
				ModelID:    "gpt-test",
				BaseURL:    server.URL,
				APIKey:     "sk-test",
				MaxTokens:  16,
			}
			c := NewClient(cfg, WithLogger(discardLogger()))
			// Fast policy: the rate-limit case still retries exactly
			// defaultShortRetries times (the assertion), but the in-loop
			// throttle waits are ~1ms instead of the 30s default.
			c.SetFailurePolicyConfig(fastFailurePolicyCfg)

			_, err := c.ChatWithDeltaCallback(context.Background(),
				[]ChatMessage{{Role: RoleUser, Content: "hi"}},
				func(string) error { return nil })
			if err == nil {
				t.Fatal("expected error")
			}

			if got := atomic.LoadInt32(&hits); got != tt.wantAttempts {
				t.Errorf("server hits = %d, want %d", got, tt.wantAttempts)
			}

			if tt.wantQuota {
				if !IsQuotaResetError(err) {
					t.Fatalf("expected QuotaResetError, got %T: %v", err, err)
				}
			} else if IsQuotaResetError(err) {
				t.Fatalf("did not expect QuotaResetError, got: %v", err)
			}
		})
	}
}
