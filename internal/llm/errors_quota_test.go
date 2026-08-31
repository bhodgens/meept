package llm

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestQuotaResetError(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name         string
		err          *QuotaResetError
		wantNonRetry bool
	}{
		{
			name: "with reset time",
			err: &QuotaResetError{
				ProviderID: "openai",
				ModelID:    "gpt-4",
				Code:       "usage_limit_reached",
				ResetAt:    now.Add(3 * time.Hour),
				MaxWait:    24 * time.Hour,
				StatusCode: 429,
			},
			wantNonRetry: true,
		},
		{
			name: "without reset time",
			err: &QuotaResetError{
				ProviderID: "anthropic",
				ModelID:    "claude-3",
				Code:       "quota_exceeded",
				StatusCode: 429,
			},
			wantNonRetry: true,
		},
		{
			name: "402 payment required",
			err: &QuotaResetError{
				ProviderID: "xai",
				ModelID:    "grok-2",
				Code:       "insufficient_quota",
				StatusCode: 402,
			},
			wantNonRetry: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.err.NonRetryable() {
				t.Errorf("NonRetryable() = false, want true")
			}
			if !IsQuotaResetError(tt.err) {
				t.Errorf("IsQuotaResetError() = false, want true")
			}
			qe, ok := AsQuotaResetError(tt.err)
			if !ok || qe != tt.err {
				t.Errorf("AsQuotaResetError() failed")
			}
			// Wrapped error
			wrapped := &wrappedError{inner: tt.err}
			if !IsQuotaResetError(wrapped) {
				t.Errorf("IsQuotaResetError(wrapped) = false, want true")
			}
			// Nil
			if IsQuotaResetError(nil) {
				t.Errorf("IsQuotaResetError(nil) = true, want false")
			}
			if _, ok := AsQuotaResetError(nil); ok {
				t.Errorf("AsQuotaResetError(nil) = (non-nil, true), want (nil, false)")
			}
		})
	}
}

type wrappedError struct {
	inner error
}

func (e *wrappedError) Error() string { return "wrapped: " + e.inner.Error() }
func (e *wrappedError) Unwrap() error { return e.inner }

func TestParseQuotaResponse(t *testing.T) {
	now := time.Now()
	unknown := QuotaContext{ProviderID: "test", ModelID: "m1", MaxWait: 24 * time.Hour}

	tests := []struct {
		name     string
		status   int
		header   http.Header
		body     []byte
		wantNil  bool
		wantCode string
	}{
		{
			name:    "200 returns nil",
			status:  200,
			body:    []byte(`{"text":"ok"}`),
			wantNil: true,
		},
		{
			name:     "429 usage_limit_reached with resets_at",
			status:   429,
			body:     []byte(`{"error":{"type":"usage_limit_reached","resets_at":` + itoa(now.Add(1*time.Hour).Unix()) + `}}`),
			wantCode: "usage_limit_reached",
		},
		{
			name:     "402 insufficient_quota",
			status:   402,
			body:     []byte(`{"error":{"type":"insufficient_quota","code":"credit_balance_exhausted"}}`),
			wantCode: "insufficient_quota",
		},
		{
			name:     "429 quota_exceeded anthropic",
			status:   429,
			body:     []byte(`{"type":"error","error":{"type":"quota_exceeded","message":"Account has exceeded its quota"}}`),
			wantCode: "quota_exceeded",
		},
		{
			name:     "500 returns nil",
			status:   500,
			body:     []byte(`{"error":"internal"}`),
			wantNil:  true,
			wantCode: "",
		},
		{
			name:     "429 plain text",
			status:   429,
			body:     []byte(`Rate limit exceeded`),
			wantCode: "quota_exceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseQuotaResponse(tt.status, tt.header, tt.body, unknown)
			if tt.wantNil {
				if got != nil {
					t.Errorf("ParseQuotaResponse() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("ParseQuotaResponse() = nil, want non-nil")
			}
			if got.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", got.Code, tt.wantCode)
			}
			if got.StatusCode != tt.status {
				t.Errorf("StatusCode = %d, want %d", got.StatusCode, tt.status)
			}
		})
	}
}

func TestQuotaCredentialKey(t *testing.T) {
	tests := []struct {
		name      string
		provider  string
		cfg       *ModelConfig
		wantStart string
	}{
		{
			name:      "literal api key",
			provider:  "openai",
			cfg:       &ModelConfig{APIKey: "sk-test123"},
			wantStart: "openai:key:",
		},
		{
			name:      "oauth provider",
			provider:  "google",
			cfg:       &ModelConfig{OAuthProvider: "github-models"},
			wantStart: "google:oauth:github-models",
		},
		{
			name:      "empty key",
			provider:  "local",
			cfg:       &ModelConfig{},
			wantStart: "local:default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := QuotaCredentialKey(tt.provider, tt.cfg)
			if !strings.HasPrefix(got, tt.wantStart) {
				t.Errorf("QuotaCredentialKey() = %q, want prefix %q", got, tt.wantStart)
			}
			// Same key -> same fingerprint
			got2 := QuotaCredentialKey(tt.provider, tt.cfg)
			if got != got2 {
				t.Errorf("same config produced different keys: %q vs %q", got, got2)
			}
			// Different key -> different fingerprint
			if tt.cfg.APIKey != "" {
				different := &ModelConfig{APIKey: "sk-different"}
				got3 := QuotaCredentialKey(tt.provider, different)
				if got == got3 {
					t.Errorf("different keys produced same fingerprint")
				}
				// Neither should contain raw key
				if strings.Contains(got, "sk-test123") {
					t.Errorf("fingerprint contains raw key material")
				}
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input time.Duration
		want  string
	}{
		{3*time.Hour + 12*time.Minute, "3h 12m"},
		{2 * time.Hour, "2h"},
		{45 * time.Minute, "45m"},
		{90 * time.Second, "1m"}, // truncated, not rounded
	}
	for _, tt := range tests {
		got := formatDuration(tt.input)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b [24]byte
	n := len(b)
	for v != 0 {
		n--
		b[n] = byte('0' + v%10)
		v /= 10
	}
	return string(b[n:])
}
