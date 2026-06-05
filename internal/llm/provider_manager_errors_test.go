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

// TestProviderManager_RateLimitRotatesImmediately verifies that a 429 error
// causes immediate rotation to the next provider without marking the provider
// unhealthy.
func TestProviderManager_RateLimitRotatesImmediately(t *testing.T) {
	var primaryCalls int32
	var backupCalls int32

	primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&primaryCalls, 1)
		// Return 429 (the OpenAI client returns APIError{429} for this)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error": {"message": "rate limit exceeded"}}`))
	}))
	defer primaryServer.Close()

	backupServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backupCalls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-123",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "Backup"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		}`))
	}))
	defer backupServer.Close()

	cfg := ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "primary", BaseURL: primaryServer.URL, ModelID: "m1"},
			{ProviderID: "backup", BaseURL: backupServer.URL, ModelID: "m2"},
		},
		FailoverTimeout: 5 * time.Second,
	}

	pm := NewProviderManager(cfg)
	ctx := context.Background()

	resp, err := pm.Chat(ctx, []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("expected success after failover: %v", err)
	}
	if resp.Content != "Backup" {
		t.Errorf("expected Backup response, got %q", resp.Content)
	}

	// Primary should have been called (it was rate-limited)
	if atomic.LoadInt32(&primaryCalls) < 1 {
		t.Error("expected primary to be called at least once")
	}

	// Backup should be called
	if atomic.LoadInt32(&backupCalls) < 1 {
		t.Error("expected backup to be called")
	}

	// Primary should NOT be marked unhealthy (rate limit is transient)
	health := pm.GetProviderHealth()
	for _, h := range health {
		if h.ProviderID == "primary" && h.Status == ProviderStatusUnhealthy {
			t.Error("primary should not be marked unhealthy for a rate limit error")
		}
	}
}

// TestProviderManager_AuthErrorMarksUnhealthy verifies that a 401 or 403 error
// marks the provider as unhealthy.
func TestProviderManager_AuthErrorMarksUnhealthy(t *testing.T) {
	var primaryCalls int32
	var backupCalls int32

	primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&primaryCalls, 1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": {"message": "invalid api key"}}`))
	}))
	defer primaryServer.Close()

	backupServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backupCalls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-123",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "Backup"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		}`))
	}))
	defer backupServer.Close()

	cfg := ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "primary", BaseURL: primaryServer.URL, ModelID: "m1"},
			{ProviderID: "backup", BaseURL: backupServer.URL, ModelID: "m2"},
		},
		FailoverTimeout: 5 * time.Second,
	}

	pm := NewProviderManager(cfg)
	ctx := context.Background()

	resp, err := pm.Chat(ctx, []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("expected success after failover: %v", err)
	}
	if resp.Content != "Backup" {
		t.Errorf("expected Backup response, got %q", resp.Content)
	}

	// Primary should be marked unhealthy due to auth error
	health := pm.GetProviderHealth()
	for _, h := range health {
		if h.ProviderID == "primary" && h.Status != ProviderStatusUnhealthy {
			t.Errorf("primary should be unhealthy after auth error, got %s", h.Status)
		}
	}
}

// TestProviderManager_ClientErrorDoesNotRotate verifies that a 400 error
// is returned directly without rotating to the next provider.
func TestProviderManager_ClientErrorDoesNotRotate(t *testing.T) {
	var primaryCalls int32
	var backupCalls int32

	primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&primaryCalls, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": {"message": "invalid request"}}`))
	}))
	defer primaryServer.Close()

	backupServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backupCalls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-123",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "Backup"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		}`))
	}))
	defer backupServer.Close()

	cfg := ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "primary", BaseURL: primaryServer.URL, ModelID: "m1"},
			{ProviderID: "backup", BaseURL: backupServer.URL, ModelID: "m2"},
		},
		FailoverTimeout: 5 * time.Second,
	}

	pm := NewProviderManager(cfg)
	ctx := context.Background()

	_, err := pm.Chat(ctx, []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("expected error for 400")
	}

	// Backup should NOT have been called (400 is request-level, not provider-level)
	if atomic.LoadInt32(&backupCalls) != 0 {
		t.Errorf("backup should not be called for 400 error, got %d calls", atomic.LoadInt32(&backupCalls))
	}

	// Error should contain something about the 400
	errMsg := err.Error()
	if errMsg == "" {
		t.Error("error message should not be empty")
	}
}

// TestIsAuthError verifies the isAuthError helper function.
func TestIsAuthError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("something"), false},
		{"401", &APIError{StatusCode: http.StatusUnauthorized, Detail: "bad key"}, true},
		{"403", &APIError{StatusCode: http.StatusForbidden, Detail: "forbidden"}, true},
		{"429", &APIError{StatusCode: http.StatusTooManyRequests, Detail: "slow down"}, false},
		{"500", &APIError{StatusCode: http.StatusInternalServerError, Detail: "boom"}, false},
		{"wrapped 401", &ClientError{Message: "failed", Cause: &APIError{StatusCode: http.StatusUnauthorized}}, true},
		{"wrapped 403", &ClientError{Message: "failed", Cause: &APIError{StatusCode: http.StatusForbidden}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAuthError(tt.err); got != tt.want {
				t.Errorf("isAuthError = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIsClientError verifies the isClientError helper function.
func TestIsClientError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("something"), false},
		{"400", &APIError{StatusCode: http.StatusBadRequest, Detail: "bad request"}, true},
		{"404", &APIError{StatusCode: http.StatusNotFound, Detail: "not found"}, true},
		{"408", &APIError{StatusCode: http.StatusRequestTimeout, Detail: "timeout"}, true},
		{"401 should not be client error", &APIError{StatusCode: http.StatusUnauthorized, Detail: "bad key"}, false},
		{"403 should not be client error", &APIError{StatusCode: http.StatusForbidden, Detail: "forbidden"}, false},
		{"429 should not be client error", &APIError{StatusCode: http.StatusTooManyRequests, Detail: "slow down"}, false},
		{"500", &APIError{StatusCode: http.StatusInternalServerError, Detail: "boom"}, false},
		{"wrapped 400", &ClientError{Message: "failed", Cause: &APIError{StatusCode: http.StatusBadRequest}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isClientError(tt.err); got != tt.want {
				t.Errorf("isClientError = %v, want %v", got, tt.want)
			}
		})
	}
}
