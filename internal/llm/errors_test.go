package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRateLimitError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *RateLimitError
		want string
	}{
		{
			name: "with retry-after and cause",
			err: &RateLimitError{
				ProviderID: "anthropic",
				ModelID:    "claude-opus-4-6",
				RetryAfter: 30 * time.Second,
				Cause:      errors.New("boom"),
			},
			want: "rate limit exceeded: provider=anthropic model=claude-opus-4-6, retry-after=30s: boom",
		},
		{
			name: "no retry-after",
			err: &RateLimitError{
				ProviderID: "openai",
				ModelID:    "gpt-4",
			},
			want: "rate limit exceeded: provider=openai model=gpt-4",
		},
		{
			name: "with metadata fields",
			err: &RateLimitError{
				ProviderID:   "openrouter",
				ModelID:      "moonshotai/Kimi-K2.6",
				RetryAfter:   2 * time.Second,
				LimitType:    "tpm_uncached",
				RetryStrategy: &RetryStrategy{
					Type:        "tpm_uncached",
					InitialDelay: 2 * time.Second,
					MaxDelay:    60 * time.Second,
					Backoff:     "exponential",
					BackoffBase: 2.0,
					UseJitter:   true,
				},
				LimitBudget: &LimitBudget{
					Used:   289280,
					Limit:  200000,
					Window: "tpm_uncached",
				},
				Cause: errors.New("tpm limit exceeded"),
			},
			want: "rate limit exceeded: provider=openrouter model=moonshotai/Kimi-K2.6, retry-after=2s: tpm limit exceeded",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRateLimitError_MetadataFields(t *testing.T) {
	rlErr := &RateLimitError{
		ProviderID: "openrouter",
		ModelID:    "test-model",
		LimitType:  "rpm",
		RetryStrategy: &RetryStrategy{
			Type:        "rpm",
			InitialDelay: 1 * time.Second,
			MaxDelay:    30 * time.Second,
			Backoff:     "linear",
			BackoffBase: 1.5,
			UseJitter:   false,
		},
		LimitBudget: &LimitBudget{
			Used:   500,
			Limit:  1000,
			Window: "per_minute",
		},
	}

	// Verify all fields are populated
	if rlErr.LimitType != "rpm" {
		t.Errorf("LimitType = %q, want %q", rlErr.LimitType, "rpm")
	}
	if rlErr.RetryStrategy == nil {
		t.Fatal("RetryStrategy is nil")
	}
	if rlErr.RetryStrategy.Type != "rpm" {
		t.Errorf("RetryStrategy.Type = %q, want %q", rlErr.RetryStrategy.Type, "rpm")
	}
	if rlErr.RetryStrategy.InitialDelay != 1*time.Second {
		t.Errorf("RetryStrategy.InitialDelay = %v, want %v", rlErr.RetryStrategy.InitialDelay, 1*time.Second)
	}
	if rlErr.RetryStrategy.MaxDelay != 30*time.Second {
		t.Errorf("RetryStrategy.MaxDelay = %v, want %v", rlErr.RetryStrategy.MaxDelay, 30*time.Second)
	}
	if rlErr.RetryStrategy.Backoff != "linear" {
		t.Errorf("RetryStrategy.Backoff = %q, want %q", rlErr.RetryStrategy.Backoff, "linear")
	}
	if rlErr.RetryStrategy.BackoffBase != 1.5 {
		t.Errorf("RetryStrategy.BackoffBase = %v, want %v", rlErr.RetryStrategy.BackoffBase, 1.5)
	}
	if rlErr.RetryStrategy.UseJitter != false {
		t.Errorf("RetryStrategy.UseJitter = %v, want %v", rlErr.RetryStrategy.UseJitter, false)
	}
	if rlErr.LimitBudget == nil {
		t.Fatal("LimitBudget is nil")
	}
	if rlErr.LimitBudget.Used != 500 {
		t.Errorf("LimitBudget.Used = %d, want %d", rlErr.LimitBudget.Used, 500)
	}
	if rlErr.LimitBudget.Limit != 1000 {
		t.Errorf("LimitBudget.Limit = %d, want %d", rlErr.LimitBudget.Limit, 1000)
	}
	if rlErr.LimitBudget.Window != "per_minute" {
		t.Errorf("LimitBudget.Window = %q, want %q", rlErr.LimitBudget.Window, "per_minute")
	}
}

func TestIsRateLimitError(t *testing.T) {
	apiErr429 := &APIError{StatusCode: http.StatusTooManyRequests, Detail: "slow down"}
	apiErr500 := &APIError{StatusCode: http.StatusInternalServerError, Detail: "boom"}
	rlErr := &RateLimitError{ProviderID: "x", ModelID: "y"}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("nope"), false},
		{"RateLimitError", rlErr, true},
		{"APIError 429", apiErr429, true},
		{"APIError 500", apiErr500, false},
		{"wrapped RateLimitError", fmt.Errorf("wrap: %w", rlErr), true},
		{"wrapped APIError 429", fmt.Errorf("wrap: %w", apiErr429), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRateLimitError(tt.err); got != tt.want {
				t.Errorf("IsRateLimitError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestAsRateLimitError(t *testing.T) {
	rlErr := &RateLimitError{ProviderID: "p", ModelID: "m"}
	if got, ok := AsRateLimitError(rlErr, "ignored", "ignored"); !ok || got != rlErr {
		t.Errorf("expected to return original RateLimitError")
	}

	apiErr := &APIError{StatusCode: http.StatusTooManyRequests}
	got, ok := AsRateLimitError(apiErr, "anthropic", "claude")
	if !ok || got == nil {
		t.Fatalf("expected conversion from APIError 429")
	}
	if got.ProviderID != "anthropic" || got.ModelID != "claude" {
		t.Errorf("provider/model not propagated: %+v", got)
	}

	if _, ok := AsRateLimitError(errors.New("nope"), "p", "m"); ok {
		t.Errorf("expected false for plain error")
	}
	if _, ok := AsRateLimitError(nil, "p", "m"); ok {
		t.Errorf("expected false for nil error")
	}
}

func TestIsRateLimitErrorMessage(t *testing.T) {
	positives := []string{
		"rate limit exceeded",
		"HTTP 429 Too Many Requests",
		"too many requests",
		"quota exceeded",
		"rate_limit_error",
		"60 requests per minute",
		"5 api calls per second",
		"rpm limit reached",
		"tpm limit reached",
		"too many concurrent requests",
	}
	for _, s := range positives {
		if !IsRateLimitErrorMessage(s) {
			t.Errorf("expected %q to be detected", s)
		}
	}
	negatives := []string{"", "internal server error", "context cancelled", "permission denied"}
	for _, s := range negatives {
		if IsRateLimitErrorMessage(s) {
			t.Errorf("expected %q to NOT be detected", s)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter(""); got != 0 {
		t.Errorf("empty header should return 0, got %v", got)
	}
	if got := parseRetryAfter("30"); got != 30*time.Second {
		t.Errorf("seconds parsing failed: got %v", got)
	}
	if got := parseRetryAfter("invalid"); got != 0 {
		t.Errorf("invalid header should return 0, got %v", got)
	}
	// HTTP-date in the past
	past := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC1123)
	if got := parseRetryAfter(past); got != 0 {
		t.Errorf("past date should return 0, got %v", got)
	}
	// HTTP-date in the future, capped at 5m
	future := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC1123)
	got := parseRetryAfter(future)
	if got > 5*time.Minute || got <= 0 {
		t.Errorf("future date should be capped at 5m, got %v", got)
	}
}

// --- New tests for Task 1: Rich Error Types and JSON Body Parsing ---

func TestParseOpenRouterError_RealResponse(t *testing.T) {
	// This is the actual response from OpenRouter for a tpm_uncached rate limit.
	body := []byte(`{
  "error": {
    "message": "Error from provider(nw,moonshotai/Kimi-K2.6: 429): {\"error\":{\"type\":\"rate_limit_error\",\"code\":\"tpm_uncached_exceeded\",\"message\":\"Uncached token rate limit exceeded for moonshotai/Kimi-K2.6. Cold-prefill tokens: 289280, Limit: 200000.\",\"retry_after\":2.0,\"retry_strategy\":{\"type\":\"tpm_uncached\",\"suggested_initial_delay_s\":2.0,\"max_delay_s\":60.0,\"backoff\":\"exponential\",\"backoff_base\":2.0,\"jitter\":true},\"retriable\":true,\"context\":{\"budget\":5,\"in_flight\":0,\"model\":\"moonshotai/Kimi-K2.6\",\"limit_type\":\"tpm_uncached\",\"tpm_window_tokens\":289280,\"tpm_limit\":200000}}}",
    "code": 429
  }
}`)

	detail := ParseOpenRouterError(body)
	if detail == nil {
		t.Fatal("ParseOpenRouterError returned nil for valid OpenRouter response")
	}

	// Check basic fields
	if detail.Type != "rate_limit_error" {
		t.Errorf("Type = %q, want %q", detail.Type, "rate_limit_error")
	}
	if detail.Code != "tpm_uncached_exceeded" {
		t.Errorf("Code = %q, want %q", detail.Code, "tpm_uncached_exceeded")
	}
	if !strings.Contains(detail.Message, "Uncached token rate limit exceeded") {
		t.Errorf("Message = %q, want to contain 'Uncached token rate limit exceeded'", detail.Message)
	}
	if !detail.Retriable {
		t.Errorf("Retriable = %v, want true", detail.Retriable)
	}
	if detail.RetryAfter != 2*time.Second {
		t.Errorf("RetryAfter = %v, want %v", detail.RetryAfter, 2*time.Second)
	}

	// Check retry strategy
	if detail.RetryStrategy == nil {
		t.Fatal("RetryStrategy is nil")
	}
	if detail.RetryStrategy.Type != "tpm_uncached" {
		t.Errorf("RetryStrategy.Type = %q, want %q", detail.RetryStrategy.Type, "tpm_uncached")
	}
	if detail.RetryStrategy.InitialDelay != 2*time.Second {
		t.Errorf("RetryStrategy.InitialDelay = %v, want %v", detail.RetryStrategy.InitialDelay, 2*time.Second)
	}
	if detail.RetryStrategy.MaxDelay != 60*time.Second {
		t.Errorf("RetryStrategy.MaxDelay = %v, want %v", detail.RetryStrategy.MaxDelay, 60*time.Second)
	}
	if detail.RetryStrategy.Backoff != "exponential" {
		t.Errorf("RetryStrategy.Backoff = %q, want %q", detail.RetryStrategy.Backoff, "exponential")
	}
	if detail.RetryStrategy.BackoffBase != 2.0 {
		t.Errorf("RetryStrategy.BackoffBase = %v, want %v", detail.RetryStrategy.BackoffBase, 2.0)
	}
	if !detail.RetryStrategy.UseJitter {
		t.Errorf("RetryStrategy.UseJitter = %v, want true", detail.RetryStrategy.UseJitter)
	}

	// Check limit budget
	if detail.LimitBudget == nil {
		t.Fatal("LimitBudget is nil")
	}
	if detail.LimitBudget.Used != 289280 {
		t.Errorf("LimitBudget.Used = %d, want %d", detail.LimitBudget.Used, 289280)
	}
	if detail.LimitBudget.Limit != 200000 {
		t.Errorf("LimitBudget.Limit = %d, want %d", detail.LimitBudget.Limit, 200000)
	}
	if detail.LimitBudget.Window != "tpm_uncached" {
		t.Errorf("LimitBudget.Window = %q, want %q", detail.LimitBudget.Window, "tpm_uncached")
	}
}

func TestParseOpenRouterError_NoInnerJSON(t *testing.T) {
	// Valid outer envelope but no inner JSON
	body := []byte(`{
  "error": {
    "message": "Rate limit exceeded",
    "code": 429
  }
}`)

	detail := ParseOpenRouterError(body)
	if detail != nil {
		t.Errorf("expected nil for body without inner JSON, got %+v", detail)
	}
}

func TestParseOpenRouterError_InvalidJSON(t *testing.T) {
	tests := [][]byte{
		[]byte(`not json`),
		[]byte(`{"error": "not an object"}`),
		[]byte(`{}`),
		[]byte(`{"error": {"code": 429}}`),
		[]byte(``),
	}

	for i, body := range tests {
		t.Run(fmt.Sprintf("invalid_%d", i), func(t *testing.T) {
			if detail := ParseOpenRouterError(body); detail != nil {
				t.Errorf("expected nil for invalid body %d, got %+v", i, detail)
			}
		})
	}
}

func TestParseOpenRouterError_MissingRetryStrategy(t *testing.T) {
	// Inner JSON without retry_strategy or context
	innerJSON := `{"error":{"type":"rate_limit_error","code":"tpm_exceeded","message":"TPM limit","retriable":true}}`
	outerMsg := fmt.Sprintf("Error from provider(x,y: 429): %s", innerJSON)
	body := []byte(fmt.Sprintf(`{"error":{"message":%q,"code":429}}`, outerMsg))

	detail := ParseOpenRouterError(body)
	if detail == nil {
		t.Fatal("expected non-nil result")
	}
	if detail.Type != "rate_limit_error" {
		t.Errorf("Type = %q, want %q", detail.Type, "rate_limit_error")
	}
	if detail.RetryStrategy != nil {
		t.Errorf("expected nil RetryStrategy when not present, got %+v", detail.RetryStrategy)
	}
	if detail.LimitBudget != nil {
		t.Errorf("expected nil LimitBudget when not present, got %+v", detail.LimitBudget)
	}
}

func TestParseOpenRouterError_ContextWithoutRetryStrategy(t *testing.T) {
	// Context present but no retry_strategy — must not panic (nil dereference guard)
	innerJSON := `{"error":{"type":"rate_limit_error","code":"tpm_exceeded","message":"TPM limit","retriable":true,"context":{"limit_type":"tpm_uncached","tpm_window_tokens":100,"tpm_limit":200}}}`
	outerMsg := fmt.Sprintf("Error from provider(x,y: 429): %s", innerJSON)
	body := []byte(fmt.Sprintf(`{"error":{"message":%q,"code":429}}`, outerMsg))

	detail := ParseOpenRouterError(body)
	if detail == nil {
		t.Fatal("expected non-nil result")
	}
	if detail.LimitBudget == nil {
		t.Fatal("expected non-nil LimitBudget from context")
	}
	if detail.LimitBudget.Window != "tpm_uncached" {
		t.Errorf("LimitBudget.Window = %q, want %q", detail.LimitBudget.Window, "tpm_uncached")
	}
	if detail.RetryStrategy != nil {
		t.Errorf("expected nil RetryStrategy, got %+v", detail.RetryStrategy)
	}
}

func TestParseOpenRouterError_RetryStrategyWithoutContext(t *testing.T) {
	// Has retry_strategy but no context
	innerJSON := `{"error":{"type":"rate_limit_error","code":"rpm_exceeded","message":"RPM limit","retry_after":5.0,"retry_strategy":{"type":"rpm","suggested_initial_delay_s":5.0,"max_delay_s":120.0,"backoff":"linear","backoff_base":1.5,"jitter":false},"retriable":true}}`
	outerMsg := fmt.Sprintf("Error from provider(x,y: 429): %s", innerJSON)
	body := []byte(fmt.Sprintf(`{"error":{"message":%q,"code":429}}`, outerMsg))

	detail := ParseOpenRouterError(body)
	if detail == nil {
		t.Fatal("expected non-nil result")
	}
	if detail.RetryAfter != 5*time.Second {
		t.Errorf("RetryAfter = %v, want %v", detail.RetryAfter, 5*time.Second)
	}
	if detail.RetryStrategy == nil {
		t.Fatal("RetryStrategy is nil")
	}
	if detail.RetryStrategy.Backoff != "linear" {
		t.Errorf("RetryStrategy.Backoff = %q, want %q", detail.RetryStrategy.Backoff, "linear")
	}
	if detail.RetryStrategy.UseJitter != false {
		t.Errorf("RetryStrategy.UseJitter = %v, want false", detail.RetryStrategy.UseJitter)
	}
	if detail.LimitBudget != nil {
		t.Errorf("expected nil LimitBudget without context, got %+v", detail.LimitBudget)
	}
}

func TestParseGenericProviderError(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantType string
		wantCode string
		wantMsg  string
		wantNil  bool
	}{
		{
			name: "simple type+message",
			body: `{"error":{"type":"rate_limit_error","message":"Too many requests"}}`,
			wantType: "rate_limit_error",
			wantMsg:  "Too many requests",
		},
		{
			name:     "type+code+message",
			body:     `{"error":{"type":"authentication_error","code":"invalid_api_key","message":"Invalid API key"}}`,
			wantType: "authentication_error",
			wantCode: "invalid_api_key",
			wantMsg:  "Invalid API key",
		},
		{
			name:     "only message",
			body:     `{"error":{"message":"Something went wrong"}}`,
			wantMsg:  "Something went wrong",
		},
		{
			name: "empty error object",
			body: `{"error":{}}`,
			wantNil: true,
		},
		{
			name: "no error key",
			body: `{"status":"error"}`,
			wantNil: true,
		},
		{
			name: "not json",
			body: "plain text error",
			wantNil: true,
		},
		{
			name: "only code",
			body: `{"error":{"code":"tpm_exceeded"}}`,
			wantCode: "tpm_exceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detail := ParseGenericProviderError([]byte(tt.body))
			if tt.wantNil {
				if detail != nil {
					t.Errorf("expected nil, got %+v", detail)
				}
				return
			}
			if detail == nil {
				t.Fatalf("expected non-nil, got nil")
			}
			if tt.wantType != "" && detail.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", detail.Type, tt.wantType)
			}
			if tt.wantCode != "" && detail.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", detail.Code, tt.wantCode)
			}
			if tt.wantMsg != "" && detail.Message != tt.wantMsg {
				t.Errorf("Message = %q, want %q", detail.Message, tt.wantMsg)
			}
		})
	}
}

func TestParseRateLimitBody(t *testing.T) {
	t.Run("openrouter first", func(t *testing.T) {
		// Valid OpenRouter body should match as OpenRouter format
		body := []byte(`{
  "error": {
    "message": "Error from provider(nw,moonshotai/Kimi-K2.6: 429): {\"error\":{\"type\":\"rate_limit_error\",\"code\":\"tpm_uncached_exceeded\",\"message\":\"Uncached token rate limit exceeded\",\"retry_after\":2.0,\"retry_strategy\":{\"type\":\"tpm_uncached\",\"suggested_initial_delay_s\":2.0,\"max_delay_s\":60.0,\"backoff\":\"exponential\",\"backoff_base\":2.0,\"jitter\":true},\"retriable\":true,\"context\":{\"budget\":5,\"in_flight\":0,\"model\":\"moonshotai/Kimi-K2.6\",\"limit_type\":\"tpm_uncached\",\"tpm_window_tokens\":289280,\"tpm_limit\":200000}}}",
    "code": 429
  }
}`)
		detail := ParseRateLimitBody(body)
		if detail == nil {
			t.Fatal("expected non-nil for OpenRouter body")
		}
		if detail.Type != "rate_limit_error" {
			t.Errorf("Type = %q, want %q", detail.Type, "rate_limit_error")
		}
	})

	t.Run("generic fallback", func(t *testing.T) {
		// Generic format without inner JSON should match generic parser
		body := []byte(`{"error":{"type":"rate_limit_error","message":"Too many requests"}}`)
		detail := ParseRateLimitBody(body)
		if detail == nil {
			t.Fatal("expected non-nil for generic body")
		}
		if detail.Type != "rate_limit_error" {
			t.Errorf("Type = %q, want %q", detail.Type, "rate_limit_error")
		}
	})

	t.Run("no match falls back to nil", func(t *testing.T) {
		detail := ParseRateLimitBody([]byte("plain text"))
		if detail != nil {
			t.Errorf("expected nil for unparseable body, got %+v", detail)
		}
	})
}

func TestProviderErrorDetail_Error(t *testing.T) {
	tests := []struct {
		name string
		d    *ProviderErrorDetail
		want string
	}{
		{
			name: "with message",
			d:    &ProviderErrorDetail{Type: "rate_limit_error", Message: "TPM exceeded"},
			want: "rate_limit_error: TPM exceeded",
		},
		{
			name: "type only",
			d:    &ProviderErrorDetail{Type: "authentication_error"},
			want: "authentication_error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.Error(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractInnerJSON(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{
			name: "openrouter 429 prefix",
			msg:  "Error from provider(nw,moonshotai/Kimi-K2.6: 429): {\"error\":{\"type\":\"rate_limit_error\"}}",
			want: "{\"error\":{\"type\":\"rate_limit_error\"}}",
		},
		{
			name: "no brace prefix",
			msg:  "Error from provider(nw,model: 429){\"error\":{\"type\":\"rate_limit_error\"}}",
			want: "{\"error\":{\"type\":\"rate_limit_error\"}}",
		},
		{
			name: "just json",
			msg:  "{\"error\":{\"type\":\"test\"}}",
			want: "{\"error\":{\"type\":\"test\"}}",
		},
		{
			name: "no json at all",
			msg:  "plain text message",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractInnerJSON(tt.msg)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBackoffWithJitter(t *testing.T) {
	t.Run("zero delay", func(t *testing.T) {
		if got := BackoffWithJitter(0, 30*time.Second, false); got != 0 {
			t.Errorf("expected 0 for zero delay, got %v", got)
		}
	})

	t.Run("no jitter", func(t *testing.T) {
		delay := 5 * time.Second
		if got := BackoffWithJitter(delay, 0, false); got != delay {
			t.Errorf("expected %v for no-jitter, got %v", delay, got)
		}
	})

	t.Run("no jitter capped at max", func(t *testing.T) {
		delay := 100 * time.Second
		maxDelay := 30 * time.Second
		if got := BackoffWithJitter(delay, maxDelay, false); got != maxDelay {
			t.Errorf("expected %v (capped), got %v", maxDelay, got)
		}
	})

	t.Run("with jitter is in range", func(t *testing.T) {
		delay := 10 * time.Second
		for i := 0; i < 100; i++ {
			got := BackoffWithJitter(delay, 0, true)
			if got < 0 || got > delay {
				t.Errorf("iteration %d: jitter %v out of [0, %v]", i, got, delay)
			}
		}
	})

	t.Run("with jitter capped at max", func(t *testing.T) {
		delay := 100 * time.Second
		maxDelay := 10 * time.Second
		for i := 0; i < 100; i++ {
			got := BackoffWithJitter(delay, maxDelay, true)
			if got < 0 || got > maxDelay {
				t.Errorf("iteration %d: jitter %v out of [0, %v]", i, got, maxDelay)
			}
		}
	})

	t.Run("negative delay", func(t *testing.T) {
		if got := BackoffWithJitter(-5*time.Second, 30*time.Second, false); got != 0 {
			t.Errorf("expected 0 for negative delay, got %v", got)
		}
	})
}

func TestParseOpenRouterError_ByteEscapedMessage(t *testing.T) {
	// Test with the message field properly JSON-escaped (as it would be in real HTTP response)
	// The inner JSON is already escaped as a string value within the outer JSON
	outer := map[string]interface{}{
		"error": map[string]interface{}{
			"message": `Error from provider(nw,moonshotai/Kimi-K2.6: 429): {"error":{"type":"rate_limit_error","code":"tpm_uncached_exceeded","message":"Uncached token rate limit exceeded for moonshotai/Kimi-K2.6. Cold-prefill tokens: 289280, Limit: 200000.","retry_after":2.0,"retry_strategy":{"type":"tpm_uncached","suggested_initial_delay_s":2.0,"max_delay_s":60.0,"backoff":"exponential","backoff_base":2.0,"jitter":true},"retriable":true,"context":{"budget":5,"in_flight":0,"model":"moonshotai/Kimi-K2.6","limit_type":"tpm_uncached","tpm_window_tokens":289280,"tpm_limit":200000}}}`,
			"code": 429,
		},
	}

	body, err := json.Marshal(outer)
	if err != nil {
		t.Fatalf("failed to marshal test body: %v", err)
	}

	detail := ParseOpenRouterError(body)
	if detail == nil {
		t.Fatal("ParseOpenRouterError returned nil for properly escaped JSON")
	}

	if detail.Code != "tpm_uncached_exceeded" {
		t.Errorf("Code = %q, want %q", detail.Code, "tpm_uncached_exceeded")
	}
	if detail.RetryStrategy == nil {
		t.Fatal("RetryStrategy is nil")
	}
	if detail.RetryStrategy.UseJitter != true {
		t.Errorf("RetryStrategy.UseJitter = %v, want true", detail.RetryStrategy.UseJitter)
	}
	if detail.LimitBudget == nil {
		t.Fatal("LimitBudget is nil")
	}
	if detail.LimitBudget.Used != 289280 {
		t.Errorf("LimitBudget.Used = %d, want %d", detail.LimitBudget.Used, 289280)
	}
	if detail.LimitBudget.Limit != 200000 {
		t.Errorf("LimitBudget.Limit = %d, want %d", detail.LimitBudget.Limit, 200000)
	}
}

func TestRateLimitError_Unwrap(t *testing.T) {
	cause := errors.New("original error")
	rlErr := &RateLimitError{
		ProviderID:   "p",
		ModelID:      "m",
		RetryAfter:   5 * time.Second,
		LimitType:    "tpm_uncached",
		RetryStrategy: &RetryStrategy{Type: "tpm_uncached"},
		LimitBudget:   &LimitBudget{Used: 100, Limit: 200},
		Cause:        cause,
	}

	if !errors.Is(rlErr, cause) {
		t.Error("Unwrap should expose Cause")
	}
	if unwrapped := rlErr.Unwrap(); unwrapped != cause {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, cause)
	}
}
