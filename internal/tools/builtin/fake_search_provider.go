package builtin

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
)

// FakeSearchProviderEnv, when set truthy in the daemon environment, swaps
// web_search's MCP-first/DuckDuckGo fallback chain for an in-memory fake
// provider. This is the hermetic e2e tier's opt-in: sandboxed suites flip
// it via e2e/harness's WithFakeSearch so search never touches the network
// (same env-gated pattern as MEEPT_DETERMINISTIC_TOOLS). Never set it in
// production environments.
const FakeSearchProviderEnv = "MEEPT_E2E_FAKE_SEARCH"

// FakeSearchProviderEnvEnabled reports whether the process-level
// FakeSearchProviderEnv override is truthy.
func FakeSearchProviderEnvEnabled() bool {
	switch os.Getenv(FakeSearchProviderEnv) {
	case "1", "true", "TRUE", "True", "yes", "on":
		return true
	default:
		return false
	}
}

// fakeSearchResults are the canned deterministic results the fake provider
// serves for every query. Marker strings (fake.example URLs, "Fake search
// result" titles) double as e2e assertions: a live DuckDuckGo response can
// never contain them, so their presence in a tool envelope proves the
// provider path ran.
var fakeSearchResults = []SearchResult{
	{Title: "Fake search result one", URL: "https://fake.example/one", Snippet: "hermetic e2e stub snippet one"},
	{Title: "Fake search result two", URL: "https://fake.example/two", Snippet: "hermetic e2e stub snippet two"},
	{Title: "Fake search result three", URL: "https://fake.example/three", Snippet: "hermetic e2e stub snippet three"},
}

// FakeSearchCannedResults returns a copy of the canned results the fake
// provider serves. e2e suites assert against these via the harness
// (e2e/harness FakeSearchResults) so the contract lives in one place.
func FakeSearchCannedResults() []SearchResult {
	out := make([]SearchResult, len(fakeSearchResults))
	copy(out, fakeSearchResults)
	return out
}

// FakeSearchProvider is an in-memory SearchProvider returning the canned
// deterministic results above with no network access. It implements the
// same SearchProvider seam as MCPSearchProvider and is installed over the
// web_search tool when FakeSearchProviderEnv is set (daemon startup).
type FakeSearchProvider struct {
	mu        sync.Mutex
	lastQuery string
}

// NewFakeSearchProvider creates a ready-to-install fake search provider.
func NewFakeSearchProvider() *FakeSearchProvider { return &FakeSearchProvider{} }

// LastQuery returns the most recent query served (test observation seam).
func (p *FakeSearchProvider) LastQuery() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastQuery
}

// Search implements SearchProvider: the canned results, echoed query,
// honored limit, no I/O. An empty query is a clean error, mirroring the
// real tool's argument validation.
func (p *FakeSearchProvider) Search(_ context.Context, query string, limit int) (SearchResults, error) {
	if strings.TrimSpace(query) == "" {
		return SearchResults{}, fmt.Errorf("fake search provider: query is required")
	}
	p.mu.Lock()
	p.lastQuery = query
	p.mu.Unlock()

	results := fakeSearchResults
	truncated := false
	if limit > 0 && limit < len(results) {
		results = results[:limit]
		truncated = true
	}
	out := make([]SearchResult, len(results))
	copy(out, results)
	return SearchResults{
		Query:     query,
		Results:   out,
		Count:     len(out),
		Truncated: truncated,
	}, nil
}

// Ensure FakeSearchProvider implements the SearchProvider seam.
var _ SearchProvider = (*FakeSearchProvider)(nil)
