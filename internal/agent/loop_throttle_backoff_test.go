package agent

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/security"
)

// errChatter is a Chatter stub that always fails with err. It exists so the
// error-branch tests can drive RunOnce's LLM call without an HTTP server.
type errChatter struct {
	err       error
	callCount int32
}

func (m *errChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	atomic.AddInt32(&m.callCount, 1)
	return nil, m.err
}

func (m *errChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *errChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "err-chatter"}
}

// chatterCalls returns the LLM call count (atomic; the loop may retry).
func (m *errChatter) chatterCalls() int { return int(atomic.LoadInt32(&m.callCount)) }

// newThrottleTestLoop builds a loop wired to a two-model alias resolver and
// the failing chatter, mirroring the shadow-specialist same-package pattern
// (RegistryConfig only accepts a concrete *llm.Client, so the stub Chatter
// is injected onto the loop directly). The LLM backoff override is set to
// zero delays so any loop-level retry loop cannot stall the test.
func newThrottleTestLoop(t *testing.T, chatter llm.Chatter, resolver *llm.Resolver) *AgentLoop {
	t.Helper()
	SetDefaultBackoffOverride(BackoffConfig{
		BaseDelay:   time.Millisecond,
		MaxDelay:    2 * time.Millisecond,
		MaxAttempts: 2,
	})
	t.Cleanup(clearDefaultBackoffOverride)
	loop := NewAgentLoop("sess-throttle-test", t.TempDir(),
		WithMessageBus(bus.New(nil, slog.New(slog.DiscardHandler))),
		WithResolver(resolver),
		WithModelRef(testClassifierAlias),
		WithLLMChatter(chatter),
	)
	loop.security = security.NewPermissionChecker(security.Config{})
	return loop
}

// TestAgentLoop_ThrottleBackoffErrorPassthrough (tree 02 leaf 03 Task 4):
// when the LLM client returns a *llm.ThrottleBackoffError the agent loop
// returns it UNCHANGED — no model rotation, no RecordAliasFailure. The
// loop's RateLimitError branch treats throttling as alias health and rotates
// models; ThrottleBackoffError is load-shedding (DECISIONS.md D4/D8):
// rotation is for dead models, not throttled live ones, and tree 03 replaces
// this pass-through with parking on RetryAt. ConsecutiveFails == 0 after the
// turn proves the failure was never recorded on the alias.
func TestAgentLoop_ThrottleBackoffErrorPassthrough(t *testing.T) {
	retryAt := time.Now().Add(time.Hour)
	wantErr := &llm.ThrottleBackoffError{
		ProviderID: "p1",
		ModelID:    "m1",
		RetryAt:    retryAt,
		Attempt:    3,
		Cause:      &llm.APIError{StatusCode: http.StatusTooManyRequests, Detail: "slow down"},
	}

	chatter := &errChatter{err: wantErr}
	resolver := newFailoverResolver(t,
		&llm.ModelConfig{BaseURL: "http://p1.invalid", ModelID: "m1", APIKey: "k", ProviderID: "p1"},
		&llm.ModelConfig{BaseURL: "http://p2.invalid", ModelID: "m2", APIKey: "k", ProviderID: "p2"},
	)
	loop := newThrottleTestLoop(t, chatter, resolver)

	_, err := loop.RunOnce(context.Background(), "hello", "conv-throttle-passthrough")
	if err == nil {
		t.Fatal("expected the ThrottleBackoffError to propagate")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error changed in transit: got %T (%v), want the same *llm.ThrottleBackoffError", err, err)
	}

	// No failure recorded on the alias: consecutive fails is the Resolver's
	// observable alias-health counter that RecordAliasFailure advances.
	if _, fails, _, ok := resolver.GetAliasHealth(testClassifierAlias); ok && fails != 0 {
		t.Errorf("alias ConsecutiveFails = %d, want 0 (no RecordAliasFailure on throttle)", fails)
	}
	// No rotation: the single-attempt chatter proves the loop made exactly
	// one LLM call (a rotation would retry with a second call).
	if got := chatter.chatterCalls(); got != 1 {
		t.Errorf("chatter calls = %d, want 1 (no rotation retry)", got)
	}
}

// TestAgentLoop_RateLimitErrorStillRotates is the control for the test
// above: the pre-existing RateLimitError branch still rotates and records
// the failure, so the new branch is provably a pass-through (not a broken
// rotation path).
func TestAgentLoop_RateLimitErrorStillRotates(t *testing.T) {
	wantErr := &llm.RateLimitError{
		ProviderID: "p1",
		ModelID:    "m1",
		RetryAfter: time.Millisecond,
	}
	chatter := &errChatter{err: wantErr}
	resolver := newFailoverResolver(t,
		&llm.ModelConfig{BaseURL: "http://p1.invalid", ModelID: "m1", APIKey: "k", ProviderID: "p1"},
		&llm.ModelConfig{BaseURL: "http://p2.invalid", ModelID: "m2", APIKey: "k", ProviderID: "p2"},
	)
	loop := newThrottleTestLoop(t, chatter, resolver)
	loop.config.MaxIterations = 1

	_, _ = loop.RunOnce(context.Background(), "hello", "conv-ratelimit-rotate")
	if got := chatter.chatterCalls(); got < 2 {
		t.Errorf("chatter calls = %d, want ≥ 2 (RateLimitError rotates for another attempt)", got)
	}
	if _, fails, _, ok := resolver.GetAliasHealth(testClassifierAlias); !ok || fails == 0 {
		t.Errorf("alias health = (ok=%v, fails=%d), want recorded failure (RecordAliasFailure ran)", ok, fails)
	}
}
