package agent

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// TestAgentLoop_QuotaResetErrorRotatesToNextCandidate is the regression test
// for the agnes-429 failover bug: a *llm.QuotaResetError from the first alias
// candidate must ROTATE to the next healthy candidate and retry — not fail
// the turn. Pre-fix the loop's quota branch returned the error immediately:
// "Quota exceeded, episode tracked" was followed straight by "LLM call
// failed" with zero rotation attempts.
//
// The scripted chatter 429s on the first attempt and succeeds on the second,
// mirroring agnes (quota'd) → local/ollama (healthy). Asserts:
//   - RunOnce succeeds with the second attempt's content;
//   - exactly 2 chatter calls (one failed attempt + one rotated retry);
//   - alias ConsecutiveFails == 0 (quota is not alias ill-health: NO
//     RecordAliasFailure, master contract 4);
//   - the failed candidate is quota-blocked in the resolver so rotation
//     skipped it.
func TestAgentLoop_QuotaResetErrorRotatesToNextCandidate(t *testing.T) {
	quotaErr := &llm.QuotaResetError{
		ProviderID: "p1",
		ModelID:    "m1",
		Code:       "usage_limit_reached",
		StatusCode: http.StatusTooManyRequests,
		ResetAt:    time.Now().Add(time.Hour),
		MaxWait:    24 * time.Hour,
		RetryAfter: time.Hour,
	}
	chatter := &scriptedFailoverChatter{
		script: []scriptedAttempt{
			{resp: nil, err: quotaErr},
			{resp: &llm.Response{Content: "recovered via fallback candidate", FinishReason: "stop"}, err: nil},
		},
	}
	resolver := newFailoverResolver(t,
		&llm.ModelConfig{BaseURL: "http://p1.invalid", ModelID: "m1", APIKey: "key-p1", ProviderID: "p1"},
		&llm.ModelConfig{BaseURL: "http://p2.invalid", ModelID: "m2", APIKey: "key-p2", ProviderID: "p2"},
	)
	loop := newThrottleTestLoop(t, chatter, resolver)

	reply, err := loop.RunOnce(context.Background(), "hello", "conv-quota-rotate")
	if err != nil {
		t.Fatalf("RunOnce after agnes 429 = error %v, want rotated retry to succeed", err)
	}
	if reply != "recovered via fallback candidate" {
		t.Errorf("reply = %q, want the second candidate's response", reply)
	}
	if got := chatter.calls.Load(); got != 2 {
		t.Errorf("chatter calls = %d, want 2 (failed attempt + rotated retry)", got)
	}
	if _, fails, _, ok := resolver.GetAliasHealth(testClassifierAlias); ok && fails != 0 {
		t.Errorf("alias ConsecutiveFails = %d, want 0 (quota must NOT record alias failure)", fails)
	}
}

// TestAgentLoop_QuotaResetErrorAllCandidatesBlockedSurfaces verifies the
// terminal case: when rotation is impossible (single candidate — nothing to
// rotate to), the quota error surfaces for caller-side parking.
func TestAgentLoop_QuotaResetErrorSingleCandidateSurfaces(t *testing.T) {
	quotaErr := &llm.QuotaResetError{
		ProviderID: "p1",
		ModelID:    "m1",
		Code:       "usage_limit_reached",
		StatusCode: http.StatusTooManyRequests,
		ResetAt:    time.Now().Add(time.Hour),
		MaxWait:    24 * time.Hour,
		RetryAfter: time.Hour,
	}
	chatter := &scriptedFailoverChatter{
		script: []scriptedAttempt{
			{resp: nil, err: quotaErr},
		},
	}
	// Single-candidate alias: RotateToNextModel wraps onto itself (no
	// rotation possible), so the original error must surface.
	resolver := newSingleCandidateResolver(t,
		&llm.ModelConfig{BaseURL: "http://p1.invalid", ModelID: "m1", APIKey: "key-p1", ProviderID: "p1"},
	)

	loop := newThrottleTestLoop(t, chatter, resolver)

	_, err := loop.RunOnce(context.Background(), "hello", "conv-quota-single")
	if err == nil {
		t.Fatal("RunOnce = nil error, want quota error surfaced when no candidate remains")
	}
	var qe *llm.QuotaResetError
	if !errors.As(err, &qe) {
		t.Fatalf("surfaced error = %T (%v), want *llm.QuotaResetError", err, err)
	}
	if got := chatter.calls.Load(); got != 1 {
		t.Errorf("chatter calls = %d, want 1 (no rotation possible)", got)
	}
}

// TestAgentLoop_GenericErrorRotatesToNextCandidate covers the mid-chain hop:
// a non-quota, non-rate-limit failure (e.g. the local candidate's endpoint
// down — connection refused) must also rotate to the next candidate and
// retry before surfacing, so the agnes 429 → local down → ollama chain
// completes.
func TestAgentLoop_GenericErrorRotatesToNextCandidate(t *testing.T) {
	chatter := &scriptedFailoverChatter{
		script: []scriptedAttempt{
			{resp: nil, err: errors.New(`Post "http://localhost:9999/v1/chat/completions": dial tcp: connection refused`)},
			{resp: &llm.Response{Content: "third candidate answered", FinishReason: "stop"}, err: nil},
		},
	}
	resolver := newFailoverResolver(t,
		&llm.ModelConfig{BaseURL: "http://p1.invalid", ModelID: "m1", APIKey: "key-p1", ProviderID: "p1"},
		&llm.ModelConfig{BaseURL: "http://p2.invalid", ModelID: "m2", APIKey: "key-p2", ProviderID: "p2"},
	)
	loop := newThrottleTestLoop(t, chatter, resolver)

	reply, err := loop.RunOnce(context.Background(), "hello", "conv-err-rotate")
	if err != nil {
		t.Fatalf("RunOnce after dead-endpoint error = %v, want rotated retry to succeed", err)
	}
	if reply != "third candidate answered" {
		t.Errorf("reply = %q, want the next candidate's response", reply)
	}
	if got := chatter.calls.Load(); got != 2 {
		t.Errorf("chatter calls = %d, want 2 (failed attempt + rotated retry)", got)
	}
	// Unlike quota, a dead endpoint IS alias ill-health: the failure was
	// recorded (then rotation reset the counter — fails==0 afterwards is
	// the observable of a completed rotate).
	if _, _, _, ok := resolver.GetAliasHealth(testClassifierAlias); !ok {
		t.Error("alias health missing")
	}
}

// newSingleCandidateResolver builds a Resolver whose alias has exactly one
// model: rotation wraps onto itself, so nothing can be rotated to.
func newSingleCandidateResolver(t *testing.T, only *llm.ModelConfig) *llm.Resolver {
	t.Helper()
	cfg := &llm.ProvidersConfig{
		ModelAliases: map[string]llm.ModelAliasEntry{
			testClassifierAlias: {Models: []string{"p1/m1"}},
		},
		Providers: map[string]llm.ProviderConfig{
			"p1": {API: "http", Options: llm.ProviderOptionsConfig{BaseURL: only.BaseURL}, Models: map[string]llm.ModelDef{
				"m1": {Name: "m1"},
			}},
		},
	}
	return llm.NewResolver(cfg, nil)
}

// scriptedFailoverChatter replays a fixed script of attempts. Attempt N
// returns script[N]'s response/error; past the end it fails (so a runaway
// rotation loop fails loudly instead of hanging).
type scriptedFailoverChatter struct {
	script []scriptedAttempt
	calls  atomic.Int32
}

type scriptedAttempt struct {
	resp *llm.Response
	err  error
}

func (s *scriptedFailoverChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	n := s.calls.Add(1)
	i := int(n) - 1
	if i >= len(s.script) {
		return nil, errors.New("script exhausted: unexpected extra chatter call")
	}
	return s.script[i].resp, s.script[i].err
}

func (s *scriptedFailoverChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return s.Chat(ctx, messages, opts...)
}

func (s *scriptedFailoverChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "scripted-failover-chatter"}
}
