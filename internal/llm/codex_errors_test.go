// Tests for CodexClient typed HTTP-status error classification
// (codex_errors.go): 429 → *RateLimitError with Retry-After honored (or
// *QuotaResetError for usage-window bodies), other non-200s → *APIError —
// matching the OpenAI-compat client's error lanes so PM rotation and the
// quota NonRetryable early-exits classify codex failures correctly.
package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newCodexStatusServer returns a test server that answers every request
// with the given status, headers, and body — the typed-error tests need
// Retry-After headers, which the shared codex test servers don't set.
func newCodexStatusServer(t *testing.T, status int, header http.Header, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, vs := range header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestCodex429TypedAsRateLimitError pins the headline fix: a codex 429 with
// a Retry-After header surfaces as *RateLimitError (not the old generic
// *ClientError), RetryAfter parsed from the header, and the cause chain
// carrying the *APIError with the 429 status for downstream lanes.
func TestCodex429TypedAsRateLimitError(t *testing.T) {
	hdr := http.Header{"Retry-After": []string{"30"}}
	srv := newCodexStatusServer(t, http.StatusTooManyRequests, hdr,
		`{"error":{"message":"slow down"}}`)
	client := newCodexClientForTest(t, srv.URL)

	_, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "x"}})
	if err == nil {
		t.Fatal("expected error for 429")
	}

	var rlErr *RateLimitError
	if !errors.As(err, &rlErr) {
		t.Fatalf("error %v (%T) is not *RateLimitError", err, err)
	}
	if rlErr.RetryAfter != 30*time.Second {
		t.Errorf("RetryAfter = %v, want 30s (from Retry-After header)", rlErr.RetryAfter)
	}
	if rlErr.ProviderID != "openai-codex" {
		t.Errorf("ProviderID = %q, want openai-codex", rlErr.ProviderID)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("cause chain missing *APIError(429): err=%v", err)
	}
	// The classification the PM rotation lane depends on.
	if !IsRateLimitError(err) {
		t.Errorf("IsRateLimitError(err) = false, want true (rotate lane)")
	}
}

// TestCodex429QuotaWindowBodyTypedAsQuotaReset pins the quota early-exit
// contract on the codex path: a 429 whose body carries the usage-window
// shape classifies as *QuotaResetError (NonRetryable), NOT RateLimitError —
// so no client retry loop short-retries an hours-long quota window.
func TestCodex429QuotaWindowBodyTypedAsQuotaReset(t *testing.T) {
	resetsAt := time.Now().Add(2 * time.Hour).Unix()
	body := `{"error":{"type":"usage_limit_reached","message":"limit reached","resets_at":` +
		formatUnix(resetsAt) + `}}`
	srv := newCodexStatusServer(t, http.StatusTooManyRequests, nil, body)
	client := newCodexClientForTest(t, srv.URL)

	_, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "x"}})
	if err == nil {
		t.Fatal("expected error for 429 quota body")
	}

	var qe *QuotaResetError
	if !errors.As(err, &qe) {
		t.Fatalf("error %v (%T) is not *QuotaResetError", err, err)
	}
	if qe.Code != "usage_limit_reached" {
		t.Errorf("Code = %q, want usage_limit_reached", qe.Code)
	}
	if !IsNonRetryable(err) {
		t.Errorf("IsNonRetryable(err) = false, want true (quota early-exit)")
	}
	if rlErr, ok := errors.AsType[*RateLimitError](err); ok {
		t.Errorf("error also classified as *RateLimitError (%v) — quota must win", rlErr)
	}
}

// TestCodex5xxTypedAsAPIError pins the other non-200 lane: 500 surfaces as
// *APIError (retryable-status lane), never the old *ClientError.
func TestCodex5xxTypedAsAPIError(t *testing.T) {
	srv := newCodexStatusServer(t, http.StatusInternalServerError, nil,
		`{"error":{"message":"upstream exploded"}}`)
	client := newCodexClientForTest(t, srv.URL)

	_, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "x"}})
	if err == nil {
		t.Fatal("expected error for 500")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v (%T) is not *APIError", err, err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
	}
	if rlErr, ok := errors.AsType[*RateLimitError](err); ok {
		t.Errorf("500 must not classify as *RateLimitError (%v)", rlErr)
	}
}

// TestCodex429TypedErrorOnStreamingPath proves the SSE streaming exchange
// classifies pre-stream 429s identically: the status check runs before the
// scanner consumes the body, so rotation sees a typed RateLimitError there
// too.
func TestCodex429TypedErrorOnStreamingPath(t *testing.T) {
	hdr := http.Header{"Retry-After": []string{"5"}}
	srv := newCodexStatusServer(t, http.StatusTooManyRequests, hdr, `{}`)
	client := newCodexClientForTest(t, srv.URL)

	_, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "x"}},
		func(string) error { return nil })
	if err == nil {
		t.Fatal("expected error for 429 on streaming path")
	}

	var rlErr *RateLimitError
	if !errors.As(err, &rlErr) {
		t.Fatalf("streaming error %v (%T) is not *RateLimitError", err, err)
	}
	if rlErr.RetryAfter != 5*time.Second {
		t.Errorf("RetryAfter = %v, want 5s", rlErr.RetryAfter)
	}
}

// TestCodex429QuotaWindowBodyOnStreamingPath pins the quota early-exit on
// the STREAMING error path: a 429 whose body carries the usage-window shape
// must classify as *QuotaResetError even in streaming mode. The error path
// drains the body on both modes — with the body unread, streaming 429s
// degraded to RateLimitError and short-retried a hours-long quota window.
func TestCodex429QuotaWindowBodyOnStreamingPath(t *testing.T) {
	resetsAt := time.Now().Add(2 * time.Hour).Unix()
	body := `{"error":{"type":"usage_limit_reached","message":"limit reached","resets_at":` +
		formatUnix(resetsAt) + `}}`
	srv := newCodexStatusServer(t, http.StatusTooManyRequests, nil, body)
	client := newCodexClientForTest(t, srv.URL)

	_, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "x"}},
		func(string) error { return nil })
	if err == nil {
		t.Fatal("expected error for 429 on streaming path")
	}

	if _, ok := errors.AsType[*QuotaResetError](err); !ok {
		t.Fatalf("streaming error %v (%T) is not *QuotaResetError — quota body unread", err, err)
	}
}

// formatUnix renders a unix-seconds timestamp as a JSON number literal
// (parseQuotaBody decodes resets_at as float64).
func formatUnix(sec int64) string {
	return strconvFormatInt(sec)
}

func strconvFormatInt(sec int64) string {
	// Small local helper to avoid importing strconv for one call site.
	if sec == 0 {
		return "0"
	}
	neg := sec < 0
	if neg {
		sec = -sec
	}
	var digits [20]byte
	i := len(digits)
	for sec > 0 {
		i--
		digits[i] = byte('0' + sec%10)
		sec /= 10
	}
	if neg {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
