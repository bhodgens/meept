package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// failfastProbeServer serves the given chat-completion body for every
// request and counts each HTTP request it receives, so tests can assert
// exact request counts per endpoint.
func failfastProbeServer(t *testing.T, body string) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// TestIntentAnalyzer_FailFast_True_NoRotation verifies classifier fail-fast:
// when FailFast is true, a failed primary attempt returns the primary error
// immediately — exactly one HTTP request, the rotate target is never
// contacted, and the resolver observes no failure recording (alias health is
// never created because chatWithFailover returns before any resolver call).
func TestIntentAnalyzer_FailFast_True_NoRotation(t *testing.T) {
	primarySrv, primaryCalls := failfastProbeServer(t, emptyContentResponse())
	secondarySrv, secondaryCalls := failfastProbeServer(t, validAnalysisResponse())

	primaryCfg := &llm.ModelConfig{BaseURL: primarySrv.URL, ModelID: "primary", APIKey: "test-key"}
	secondaryCfg := &llm.ModelConfig{BaseURL: secondarySrv.URL, ModelID: "secondary", APIKey: "test-key"}
	resolver := newFailoverResolver(t, primaryCfg, secondaryCfg)

	ia := newIntentAnalyzerWithConfig(IntentAnalyzerConfig{
		ModelConfig: primaryCfg,
		Resolver:    resolver,
		AliasName:   testClassifierAlias,
		FailFast:    true,
	}, llm.NewClient(primaryCfg), nil)

	analysis, err := ia.AnalyzeTrueIntent(context.Background(), "fix this bug please", nil)
	if err == nil {
		t.Fatalf("expected primary error under fail-fast, got analysis %+v", analysis)
	}
	if !strings.Contains(err.Error(), "empty content") {
		t.Fatalf("error should name the primary failure (empty content), got: %v", err)
	}
	if got := atomic.LoadInt32(primaryCalls); got != 1 {
		t.Errorf("expected exactly 1 request under fail-fast, got %d", got)
	}
	if got := atomic.LoadInt32(secondaryCalls); got != 0 {
		t.Errorf("rotate target must not be contacted under fail-fast, got %d requests", got)
	}
	// No RecordAliasFailure side effect: with fail-fast the analyzer never
	// touches the resolver, so alias health must either not exist or carry
	// zero consecutive failures.
	if _, fails, _, ok := resolver.GetAliasHealth(testClassifierAlias); ok && fails != 0 {
		t.Errorf("expected no recorded alias failure under fail-fast, got consecutiveFails=%d", fails)
	}
}

// TestIntentAnalyzer_FailFast_False_Rotates verifies the default (FailFast
// unset): rotation is preserved exactly as today — the primary fails, the
// resolver advances to the next candidate, the retry hits the rotate target
// and succeeds (1 request to each endpoint).
func TestIntentAnalyzer_FailFast_False_Rotates(t *testing.T) {
	primarySrv, primaryCalls := failfastProbeServer(t, emptyContentResponse())
	secondarySrv, secondaryCalls := failfastProbeServer(t, validAnalysisResponse())

	primaryCfg := &llm.ModelConfig{BaseURL: primarySrv.URL, ModelID: "primary", APIKey: "test-key"}
	secondaryCfg := &llm.ModelConfig{BaseURL: secondarySrv.URL, ModelID: "secondary", APIKey: "test-key"}
	resolver := newFailoverResolver(t, primaryCfg, secondaryCfg)

	ia := newIntentAnalyzerWithConfig(IntentAnalyzerConfig{
		ModelConfig: primaryCfg,
		Resolver:    resolver,
		AliasName:   testClassifierAlias,
		// FailFast intentionally unset (false) — production default.
	}, llm.NewClient(primaryCfg), nil)

	analysis, err := ia.AnalyzeTrueIntent(context.Background(), "fix this bug please", nil)
	if err != nil {
		t.Fatalf("expected success after rotation, got error: %v", err)
	}
	if analysis == nil || analysis.Goal != "fix bug" {
		t.Fatalf("expected goal 'fix bug', got %+v", analysis)
	}
	if got := atomic.LoadInt32(primaryCalls); got != 1 {
		t.Errorf("expected exactly 1 primary request, got %d", got)
	}
	if got := atomic.LoadInt32(secondaryCalls); got != 1 {
		t.Errorf("expected exactly 1 rotate-target request, got %d", got)
	}
}
