// Package builtin provides built-in tool implementations for meept.
package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/tools"
)

// ErrNoSearchBackend is returned by MCPSearchProvider.Search when no MCP
// search backend is installed or no search MCP server is currently
// connected. Callers treat it (like any search error) as the signal to fall
// back to the direct DuckDuckGo scraper.
var ErrNoSearchBackend = errors.New("no search mcp backend connected")

// SearchMCPBackend is the seam between the websearch tool and the MCP
// manager. internal/tools/builtin cannot import internal/tools/mcp (import
// cycle — see internal/tools/mcp/transport/ssrf.go), so the daemon wires a
// thin adapter over *mcp.Manager in at startup.
//
// SearchServer reports the fully-qualified name ("server.tool") of a
// connected, healthy search MCP tool, or ok=false when none is available.
// Call dispatches that tool call through the MCP manager (rate limiting,
// stats, sanitization all apply as for any MCP call).
type SearchMCPBackend interface {
	SearchServer() (fullName string, ok bool)
	Call(ctx context.Context, fullName string, args map[string]any) (*tools.ToolResult, error)
}

// MCPSearchProvider searches through a connected search MCP server
// (searxng). It is the preferred link of the websearch fallback chain: when
// SearchServer reports a healthy server, searches route through MCP; every
// failure mode (no backend, server absent, call error, unparseable payload)
// surfaces as an error so WebSearchTool can fall back to the DuckDuckGo
// scraper.
type MCPSearchProvider struct {
	mu        sync.Mutex
	backend   SearchMCPBackend
	timeout   time.Duration
	maxParsed int
}

// NewMCPSearchProvider creates an MCP search provider. A zero timeout
// defaults to DefaultSearchTimeout.
func NewMCPSearchProvider(timeout time.Duration) *MCPSearchProvider {
	if timeout == 0 {
		timeout = DefaultSearchTimeout
	}
	return &MCPSearchProvider{timeout: timeout, maxParsed: MaxResultLimit}
}

// SetBackend installs the MCP backend adapter. Follows the typed-nil guard
// pattern: a nil backend leaves the provider reporting ErrNoSearchBackend.
// Must be called before the provider serves requests.
func (p *MCPSearchProvider) SetBackend(b SearchMCPBackend) {
	if b == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.backend = b
}

// Search routes one query through the connected search MCP server. The
// returned SearchResults carries the raw payload parsed from the server's
// text response (SearXNG JSON when the server emits it, an honest error
// otherwise).
func (p *MCPSearchProvider) Search(ctx context.Context, query string, limit int) (SearchResults, error) {
	p.mu.Lock()
	backend := p.backend
	timeout := p.timeout
	p.mu.Unlock()

	if backend == nil {
		return SearchResults{}, ErrNoSearchBackend
	}
	fullName, ok := backend.SearchServer()
	if !ok {
		return SearchResults{}, ErrNoSearchBackend
	}
	if limit < 1 {
		limit = DefaultResultLimit
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := backend.Call(callCtx, fullName, map[string]any{
		"query": query,
		"limit": limit,
	})
	if err != nil {
		return SearchResults{}, fmt.Errorf("search mcp call failed: %w", err)
	}
	if result == nil {
		return SearchResults{}, fmt.Errorf("search mcp call returned no result")
	}
	if !result.Success {
		if result.Error != "" {
			return SearchResults{}, fmt.Errorf("search mcp error: %s", result.Error)
		}
		return SearchResults{}, fmt.Errorf("search mcp call failed")
	}

	payload := ""
	switch v := result.Result.(type) {
	case string:
		payload = v
	default:
		if v != nil {
			if b, marshalErr := json.Marshal(v); marshalErr == nil {
				payload = string(b)
			}
		}
	}
	if strings.TrimSpace(payload) == "" {
		return SearchResults{}, fmt.Errorf("search mcp returned an empty payload")
	}

	parsed, parseErr := parseSearxngPayload(payload, query, limit)
	if parseErr != nil {
		return SearchResults{}, parseErr
	}
	return parsed, nil
}

// searxngWireResult mirrors the result objects of SearXNG's JSON API.
// searxng-mcp servers emit either `url` or `link` and either `snippet` or
// `content` depending on version, so both spellings are accepted.
type searxngWireResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Link    string `json:"link"`
	Snippet string `json:"snippet"`
	Content string `json:"content"`
}

// searxngWirePayload is the top level of a SearXNG JSON API response.
type searxngWirePayload struct {
	Query   string              `json:"query"`
	Results []searxngWireResult `json:"results"`
}

// parseSearxngPayload parses a SearXNG JSON API response (as emitted by the
// searxng MCP server's text blocks) into SearchResults, capping at limit.
// Anything that is not a valid SearXNG JSON payload is an error so the
// caller can fall back to the DuckDuckGo scraper.
func parseSearxngPayload(payload, query string, limit int) (SearchResults, error) {
	var wire searxngWirePayload
	if err := json.Unmarshal([]byte(payload), &wire); err != nil {
		return SearchResults{}, fmt.Errorf("search mcp returned unparseable results: %w", err)
	}

	results := make([]SearchResult, 0, min(len(wire.Results), limit))
	for _, r := range wire.Results {
		if len(results) >= limit {
			break
		}
		u := strings.TrimSpace(r.URL)
		if u == "" {
			u = strings.TrimSpace(r.Link)
		}
		snippet := strings.TrimSpace(r.Snippet)
		if snippet == "" {
			snippet = strings.TrimSpace(r.Content)
		}
		if u == "" || strings.TrimSpace(r.Title) == "" {
			continue
		}
		results = append(results, SearchResult{
			Title:   strings.TrimSpace(r.Title),
			URL:     u,
			Snippet: snippet,
		})
	}

	truncated := len(wire.Results) > len(results)
	out := SearchResults{
		Query:     query,
		Results:   results,
		Count:     len(results),
		Truncated: truncated,
	}
	return out, nil
}
