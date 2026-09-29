package harness

import "github.com/caimlas/meept/internal/tools/builtin"

// fake_search.go is the hermetic-tier opt-in for web_search. The daemon
// subprocess honors MEEPT_E2E_FAKE_SEARCH by swapping web_search's
// MCP-first/DuckDuckGo chain for an in-memory fake provider
// (internal/tools/builtin/fake_search_provider.go), so suites exercising
// search never touch the network. Assertions use FakeSearchResults — the
// same canned results the daemon-side fake serves — so the contract lives
// in one place.

// WithFakeSearch opts this sandbox stack's daemon into the fake search
// provider (env-injected, like WithExtraEnv). Any web_search call the
// agent makes returns the deterministic FakeSearchResults instead of
// contacting DuckDuckGo.
func WithFakeSearch() StartOption {
	return WithExtraEnv(map[string]string{"MEEPT_E2E_FAKE_SEARCH": "1"})
}

// FakeSearchResult is one canned search result (mirror of
// builtin.SearchResult, kept local so suite code reads declaratively).
type FakeSearchResult struct {
	Title   string
	URL     string
	Snippet string
}

// FakeSearchResults returns the canned results the daemon-side fake search
// provider serves, re-exported from internal/tools/builtin so suites
// assert the exact contract in one place.
func FakeSearchResults() []FakeSearchResult {
	raw := builtin.FakeSearchCannedResults()
	out := make([]FakeSearchResult, len(raw))
	for i, r := range raw {
		out[i] = FakeSearchResult{Title: r.Title, URL: r.URL, Snippet: r.Snippet}
	}
	return out
}

// FakeSearchSnippetMarker is the marker string every canned snippet
// carries: its presence in a tool envelope proves the fake provider path
// ran (a live DuckDuckGo response can never contain it).
func FakeSearchSnippetMarker() string { return "hermetic e2e stub snippet" }
