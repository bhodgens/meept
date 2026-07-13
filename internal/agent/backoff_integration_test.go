package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/security"
)

// fastBackoffConfig returns a BackoffConfig with millisecond delays suitable
// for integration tests that must complete in well under 1 second.
func fastBackoffConfig() BackoffConfig {
	return BackoffConfig{
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
		Multiplier:  2.0,
		Jitter:      0,
		MaxAttempts: 5,
	}
}

// ---------------------------------------------------------------------------
// 1. TestLLMCall_WithRateLimit
// ---------------------------------------------------------------------------

// TestLLMCall_WithRateLimit simulates an LLM-call retry sequence: the first
// call returns a *llm.RateLimitError, the second returns a success.
// RunWithRetry must consume exactly one budget unit and invoke fn exactly
// twice.
func TestLLMCall_WithRateLimit(t *testing.T) {
	t.Parallel()

	var calls int32

	// Note: the actual LLM response type is llm.ChatResponse (not
	// ChatCompletionResponse — that type does not exist in this codebase).
	cfg := fastBackoffConfig()
	budget := NewRetryBudget(5)
	ctx := WithRetryBudget(context.Background(), budget)

	fn := func() (*llm.ChatResponse, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			return nil, &llm.RateLimitError{
				ProviderID: "test",
				ModelID:    "test-model",
				RetryAfter: 5 * time.Millisecond,
			}
		}
		return &llm.ChatResponse{
			ID:     "resp-1",
			Object: "chat.completion",
			Model:  "test-model",
			Choices: []llm.Choice{
				{Index: 0, FinishReason: "stop"},
			},
		}, nil
	}

	result, err := RunWithRetry[*llm.ChatResponse](ctx, cfg, fn)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("expected 2 invocations, got %d", got)
	}
	if budget.Used() != 1 {
		t.Errorf("expected budget.Used()=1, got %d", budget.Used())
	}
}

// ---------------------------------------------------------------------------
// 2. TestToolExecution_NetworkFailure
// ---------------------------------------------------------------------------

// TestToolExecution_NetworkFailure exercises the executor's tool-retry path
// via executeToolWithRetry. A stub tool fails twice with a Retryable-wrapped
// network error, then succeeds on the third call. The final result must be
// successful with exactly 3 invocations.
func TestToolExecution_NetworkFailure(t *testing.T) {
	t.Parallel()

	var calls int32
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("web_fetch", "web fetch", func(ctx context.Context, args map[string]any) (any, error) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			return nil, Retryable(
				errors.New("connection reset"),
				true, 0, "transient network",
			)
		}
		return "ok", nil
	}))

	secChecker := security.NewPermissionChecker(security.Config{})
	executor := NewExecutor(registry, secChecker, WithParallelism(1))

	// Use a fast backoff config to avoid the 2s BaseDelay that
	// getRetryConfigForTool("web_fetch") would apply.
	fastCfg := fastBackoffConfig()
	tool := registry.tools["web_fetch"]

	result, err := executor.executeToolWithRetry(
		context.Background(),
		tool, fastCfg,
		"call_net_1", "web_fetch",
		map[string]any{"url": "http://example.com"},
	)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("expected 3 tool invocations, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// 3. TestRetryBudget_LLMAndToolShareBudget
// ---------------------------------------------------------------------------

// TestRetryBudget_LLMAndToolShareBudget verifies that a shared RetryBudget
// correctly tracks usage across distinct operations. Because RunWithRetry
// currently passes a generic "operation" label to TryUse (not a per-caller
// label), this test uses direct budget.TryUse calls to verify per-operation
// accounting and RunWithRetry calls to verify aggregate accounting.
func TestRetryBudget_LLMAndToolShareBudget(t *testing.T) {
	t.Parallel()

	// --- Direct per-op accounting verification ---
	// RunWithRetry calls budget.TryUse("operation") for every retry,
	// so all retries share the same "operation" key. We verify per-op
	// accounting with explicit labels here, and aggregate accounting via
	// RunWithRetry below.

	budget := NewRetryBudget(3)
	ctx := WithRetryBudget(context.Background(), budget)
	cfg := fastBackoffConfig()

	// Op1 (LLM simulation): fails twice with retryable errors, then succeeds.
	// Each failure triggers one budget consumption via RunWithRetry.
	var llmCalls int32
	llmFn := func() (string, error) {
		n := atomic.AddInt32(&llmCalls, 1)
		if n < 3 {
			return "", Retryable(
				errors.New("rate limited"),
				true, 0, "simulated llm rate limit",
			)
		}
		return "llm-ok", nil
	}

	_, err := RunWithRetry[string](ctx, cfg, llmFn)
	if err != nil {
		t.Fatalf("op1 RunWithRetry failed: %v", err)
	}

	// Op2 (tool simulation): fails once, then succeeds.
	var toolCalls int32
	toolFn := func() (string, error) {
		n := atomic.AddInt32(&toolCalls, 1)
		if n < 2 {
			return "", Retryable(
				errors.New("connection refused"),
				true, 0, "transient network",
			)
		}
		return "tool-ok", nil
	}

	_, err = RunWithRetry[string](ctx, cfg, toolFn)
	if err != nil {
		t.Fatalf("op2 RunWithRetry failed: %v", err)
	}

	// Budget started at 3; op1 consumed 2, op2 consumed 1 = 3 total.
	if budget.Used() != 3 {
		t.Errorf("budget.Used() = %d, want 3", budget.Used())
	}
	if budget.Remaining() != 0 {
		t.Errorf("budget.Remaining() = %d, want 0", budget.Remaining())
	}

	// RunWithRetry uses TryUse("operation") internally, so all retries are
	// attributed to the "operation" label. Verify that:
	if got := budget.UsedBy("operation"); got != 3 {
		t.Errorf("budget.UsedBy(\"operation\") = %d, want 3", got)
	}

	// Also verify direct per-op accounting works correctly via TryUse.
	smallBudget := NewRetryBudget(5)
	smallBudget.TryUse("llm")
	smallBudget.TryUse("llm")
	smallBudget.TryUse("tool")
	if got := smallBudget.UsedBy("llm"); got != 2 {
		t.Errorf("UsedBy(\"llm\") = %d, want 2", got)
	}
	if got := smallBudget.UsedBy("tool"); got != 1 {
		t.Errorf("UsedBy(\"tool\") = %d, want 1", got)
	}
	if got := smallBudget.Used(); got != 3 {
		t.Errorf("Used() = %d, want 3", got)
	}
}

// ---------------------------------------------------------------------------
// 4. TestBackoff_HTTPErrorClassification
// ---------------------------------------------------------------------------

// TestBackoff_HTTPErrorClassification is a table-driven test verifying that
// HTTPError.IsRetryable() and isRetryableHTTPStatus() correctly classify
// retryable vs non-retryable HTTP status codes.
func TestBackoff_HTTPErrorClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		code      int
		retryable bool
	}{
		// Retryable statuses.
		{"429 too many requests", 429, true},
		{"500 internal server error", 500, true},
		{"502 bad gateway", 502, true},
		{"503 service unavailable", 503, true},
		{"504 gateway timeout", 504, true},
		{"408 request timeout", 408, true},
		{"409 conflict", 409, true},

		// Non-retryable statuses.
		{"400 bad request", 400, false},
		{"401 unauthorized", 401, false},
		{"403 forbidden", 403, false},
		{"404 not found", 404, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Verify isRetryableHTTPStatus directly.
			if got := isRetryableHTTPStatus(tt.code); got != tt.retryable {
				t.Errorf("isRetryableHTTPStatus(%d) = %v, want %v", tt.code, got, tt.retryable)
			}

			// Verify HTTPError.IsRetryable method.
			httpErr := &HTTPError{
				StatusCode: tt.code,
				Body:       "test body",
				URL:        "http://example.com",
			}
			if got := httpErr.IsRetryable(); got != tt.retryable {
				t.Errorf("HTTPError{StatusCode:%d}.IsRetryable() = %v, want %v", tt.code, got, tt.retryable)
			}
		})
	}
}
