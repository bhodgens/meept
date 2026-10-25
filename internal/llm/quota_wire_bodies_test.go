package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// Wire bodies captured verbatim from the live daemon log (2026-10-24). These
// pin the BUG-2 fix: a generic provider error type on a bare 429 must NOT
// classify as quota, while a true quota body (explicit error.code, a
// quota-shaped type, or a reset horizon) must.

// zaiQuotaWireBody is zai's TRUE quota 429 body, verbatim:
// HTTP 429 with error.code "1308" and a reset timestamp in the message.
const zaiQuotaWireBody = `{"error":{"code":"1308","message":"Usage limit reached for 5 hour. Your limit will reset at 2026-10-24 14:06:18"}}`

// agnesGenericRateLimitWireBody is agnes's generic 429 body, verbatim: no
// error.code (empty string), a generic error.type "AgnesAI_error", and no
// reset horizon. It must stay a rate limit (RateLimitError, retryable).
const agnesGenericRateLimitWireBody = `{"error":{"code":"","message":"Too many requests. Please try again in a moment. (request id: 20261024051643759426837qJmaanwo)","type":"AgnesAI_error"}}`

func TestParseQuotaBody_WireBodyCorpus(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantNil   bool
		wantQuota bool // classifyQuotaDecision(429, body, ParseRateLimitBody(body))
		wantCode  string
		note      string
	}{
		{
			name:      "zai code 1308 is quota",
			body:      zaiQuotaWireBody,
			wantQuota: true,
			wantCode:  "1308",
			note:      "explicit non-empty error.code",
		},
		{
			name:      "agnes AgnesAI_error generic 429 is not quota",
			body:      agnesGenericRateLimitWireBody,
			wantQuota: false,
			wantCode:  "",
			note:      "generic type, empty code, no reset horizon -> Code must stay empty",
		},
		{
			name:      "openai usage_limit_reached is quota",
			body:      `{"error":{"type":"usage_limit_reached","message":"limit reached"}}`,
			wantQuota: true,
			wantCode:  "usage_limit_reached",
		},
		{
			name:      "openai usage_limit_reached with resets_at is quota",
			body:      `{"error":{"type":"usage_limit_reached","message":"limit reached","resets_at":1893456000}}`,
			wantQuota: true,
			wantCode:  "usage_limit_reached",
		},
		{
			name:      "resets_at alone with generic type is quota",
			body:      `{"error":{"type":"AgnesAI_error","message":"limit","resets_at":1893456000}}`,
			wantQuota: true,
			wantCode:  "AgnesAI_error",
			note:      "a reset horizon promotes even a generic type to a quota Code",
		},
		{
			name:      "insufficient_quota is quota",
			body:      `{"error":{"type":"insufficient_quota","message":"billing"}}`,
			wantQuota: true,
			wantCode:  "insufficient_quota",
		},
		{
			name:     "not json returns nil",
			body:     `Rate limit exceeded`,
			wantNil:  true,
			wantCode: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(tt.body)
			qb := parseQuotaBody(body)
			if tt.wantNil {
				if qb != nil {
					t.Fatalf("parseQuotaBody = %+v, want nil", qb)
				}
			} else if qb == nil {
				t.Fatal("parseQuotaBody = nil, want non-nil")
			}

			got := classifyQuotaDecision(http.StatusTooManyRequests, body, ParseRateLimitBody(body))
			if got != tt.wantQuota {
				t.Errorf("classifyQuotaDecision(429) = %v, want %v (%s)", got, tt.wantQuota, tt.note)
			}

			if qb != nil && qb.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", qb.Code, tt.wantCode)
			}
		})
	}
}

// TestQuotaWireBodies_ClientClassification drives the verbatim wire bodies
// through the real OpenAI-compatible client: zai's 1308 body must produce a
// *QuotaResetError with a single server hit (no short-retry), and agnes's
// generic 429 body must produce a retryable *RateLimitError.
func TestQuotaWireBodies_ClientClassification(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantQuota    bool
		wantCode     string
		wantAttempts int32
	}{
		{
			name:         "zai 1308 quota body -> QuotaResetError, no short-retry",
			body:         zaiQuotaWireBody,
			wantQuota:    true,
			wantCode:     "1308",
			wantAttempts: 1,
		},
		{
			name:         "agnes generic 429 -> RateLimitError, retried",
			body:         agnesGenericRateLimitWireBody,
			wantQuota:    false,
			wantAttempts: defaultShortRetries,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			cfg := &ModelConfig{
				ProviderID: "testprov",
				ModelID:    "test-model",
				BaseURL:    server.URL,
				APIKey:     "test-api-key",
				MaxTokens:  16,
			}
			c := NewClient(cfg, WithLogger(discardLogger()))
			c.SetFailurePolicyConfig(fastFailurePolicyCfg)

			_, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
			if err == nil {
				t.Fatal("expected error")
			}

			if got := hits.Load(); got != tt.wantAttempts {
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
			} else {
				if IsQuotaResetError(err) {
					t.Fatalf("generic 429 classified as quota: %v", err)
				}
				if !IsRateLimitError(err) {
					t.Fatalf("expected RateLimitError, got %T: %v", err, err)
				}
			}
		})
	}
}
