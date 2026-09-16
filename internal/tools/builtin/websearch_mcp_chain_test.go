package builtin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The websearch tool prefers the MCP search provider (searxng) when one is
// installed and healthy, and falls back to the DuckDuckGo scraper when the
// provider is absent or fails.

type failingProvider struct{ err error }

func (f failingProvider) Search(ctx context.Context, query string, limit int) (SearchResults, error) {
	return SearchResults{}, f.err
}

func TestWebSearch_MCPSuccessSkipsDuckDuckGo(t *testing.T) {
	// The DDG test server must never be hit when the provider succeeds.
	ddgHit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ddgHit = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(liteHTMLPage()))
	}))
	defer srv.Close()

	tm := &WebSearchTool{timeout: DefaultSearchTimeout, client: srv.Client()}
	tm.client.Transport = &rewriteTransport{
		host: strings.TrimPrefix(srv.URL, "http://"),
		base: http.DefaultTransport,
	}
	tm.SetSearchProvider(stubSearchProvider{results: SearchResults{
		Query:   "flaky tests",
		Results: []SearchResult{{Title: "From MCP", URL: "https://mcp.example", Snippet: "s"}},
		Count:   1,
	}})

	res, err := tm.Execute(__searchCtx(t), map[string]any{"query": "flaky tests"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sr, ok := res.(SearchResults)
	if !ok {
		t.Fatalf("result type = %T", res)
	}
	if sr.Count != 1 || sr.Results[0].URL != "https://mcp.example" {
		t.Errorf("results = %+v, want the MCP provider's result", sr.Results)
	}
	if ddgHit {
		t.Error("DuckDuckGo fallback was consulted despite a successful MCP search")
	}
}

// stubSearchProvider returns fixed results (kept separate from
// failingProvider for clarity in table tests).
type stubSearchProvider struct{ results SearchResults }

func (s stubSearchProvider) Search(ctx context.Context, query string, limit int) (SearchResults, error) {
	return s.results, nil
}

func TestWebSearch_MCPFailureFallsBackToDuckDuckGo(t *testing.T) {
	var stage int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stage++
		if stage == 1 {
			// primary html endpoint: anti-bot challenge
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("<html>Anomaly detected.</html>"))
			return
		}
		// lite endpoint: real results
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(liteHTMLPage()))
	}))
	defer srv.Close()

	tm := &WebSearchTool{timeout: DefaultSearchTimeout, client: srv.Client()}
	tm.client.Transport = &rewriteTransport{
		host: strings.TrimPrefix(srv.URL, "http://"),
		base: http.DefaultTransport,
	}
	tm.SetSearchProvider(failingProvider{err: errors.New("mcp down")})

	res, err := tm.Execute(__searchCtx(t), map[string]any{"query": "flaky tests"})
	if err != nil {
		t.Fatalf("DDG fallback should succeed: %v", err)
	}
	sr, ok := res.(SearchResults)
	if !ok {
		t.Fatalf("result type = %T", res)
	}
	if sr.Count == 0 {
		t.Fatal("DDG fallback should return results")
	}
	if !strings.Contains(sr.Results[0].URL, "example.com/flaky") {
		t.Errorf("first URL = %q, want the DDG lite result", sr.Results[0].URL)
	}
}

func TestWebSearch_NilProviderIsPureDDG(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(liteHTMLPage()))
	}))
	defer srv.Close()

	tm := &WebSearchTool{timeout: DefaultSearchTimeout, client: srv.Client()}
	tm.client.Transport = &rewriteTransport{
		host: strings.TrimPrefix(srv.URL, "http://"),
		base: http.DefaultTransport,
	}
	// No SetSearchProvider call: DDG path must work unchanged.
	res, err := tm.Execute(__searchCtx(t), map[string]any{"query": "flaky tests"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sr, ok := res.(SearchResults); !ok || sr.Count == 0 {
		t.Fatalf("results = %+v", res)
	}
}

func TestWebSearch_NilSetterGuardIsNoOp(t *testing.T) {
	t.Parallel()
	tm := NewWebSearchTool(0)
	tm.SetSearchProvider(nil) // must be a no-op, not a panic
	if tm.currentSearchProvider() != nil {
		t.Error("nil provider setter changed state")
	}
}
