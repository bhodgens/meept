// Package builtin provides built-in tool implementations for meept.
package builtin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/security/ssrf"
	"github.com/caimlas/meept/internal/tools"
)

const (
	// DefaultSearchTimeout is the default timeout for search requests.
	DefaultSearchTimeout = 15 * time.Second
	// DefaultResultLimit is the default number of results to return.
	DefaultResultLimit = 10
	// MaxResultLimit is the maximum number of results allowed.
	MaxResultLimit = 30
	// MinRequestInterval is the minimum interval between requests to respect rate limits.
	MinRequestInterval = 500 * time.Millisecond
	// MaxSearchResponseSize is the maximum allowed response body size (5MB).
	// This prevents memory exhaustion from malicious or oversized responses.
	MaxSearchResponseSize = 5 * 1024 * 1024
)

// SearchResult represents a single search result.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// SearchResults is the result of a web search operation.
type SearchResults struct {
	Query     string         `json:"query"`
	Results   []SearchResult `json:"results"`
	Count     int            `json:"count"`
	Truncated bool           `json:"truncated,omitempty"`
}

// WebSearchTool performs web searches using DuckDuckGo's HTML interface.
type WebSearchTool struct {
	tools.ToolDefaults
	timeout         time.Duration
	client          *http.Client
	mu              sync.Mutex
	lastRequestTime time.Time

	// guard is the centralized SSRF guard ([security.ssrf] enabled, default).
	// When non-nil it supersedes the legacy checkURL/ssrfDialContext path.
	guard   *ssrf.Guard
	guardMu sync.Mutex

	// searchProvider is the optional MCP-first search provider (searxng
	// behind the searxng-mcp server). Nil => the direct DuckDuckGo scraper
	// is the only backend. When set, Execute prefers it and falls back to
	// the scraper on any provider error.
	searchProvider   SearchProvider
	searchProviderMu sync.Mutex
}

// SearchProvider is a pluggable search backend consulted before the direct
// DuckDuckGo scraper. Implemented by MCPSearchProvider (searxng MCP).
type SearchProvider interface {
	Search(ctx context.Context, query string, limit int) (SearchResults, error)
}

// NewWebSearchTool creates a new web search tool.
func NewWebSearchTool(timeout time.Duration) *WebSearchTool {
	if timeout == 0 {
		timeout = DefaultSearchTimeout
	}

	t := &WebSearchTool{
		timeout: timeout,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxConnsPerHost: 8,
				DialContext:     ssrfDialContext(false),
			},
		},
	}
	t.client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		// SSRF guard on redirect targets to prevent redirects to
		// private/loopback/link-local addresses. Uses the centralized
		// guard when installed, legacy checkURL otherwise.
		if err := t.checkURLGuarded(req.URL.String()); err != nil {
			return fmt.Errorf("redirect blocked: %w", err)
		}
		return nil
	}
	return t
}

// checkURLGuarded validates raw against the centralized SSRF guard when one
// is installed, falling back to the legacy package-level checkURL otherwise.
func (t *WebSearchTool) checkURLGuarded(raw string) error {
	t.guardMu.Lock()
	g := t.guard
	t.guardMu.Unlock()
	if g != nil {
		return g.CheckURL(raw)
	}
	return checkURL(raw)
}

// guardEnabled reports whether a centralized SSRF guard is installed.
func (t *WebSearchTool) guardEnabled() bool {
	t.guardMu.Lock()
	defer t.guardMu.Unlock()
	return t.guard != nil
}

// SetSSRFGuard installs the centralized SSRF guard from
// internal/security/ssrf ([security.ssrf] config, enabled by default). The
// client is rebuilt via Guard.WrapClient with a fresh transport, which
// installs per-hop redirect re-validation and dial-time IP re-checks,
// superseding the legacy checkURL/ssrfDialContext path. Follows the
// typed-nil guard pattern: a nil g leaves legacy behavior in place. Must be
// called before the tool serves requests.
func (t *WebSearchTool) SetSSRFGuard(g *ssrf.Guard) {
	if g == nil {
		return
	}
	t.guardMu.Lock()
	defer t.guardMu.Unlock()
	t.guard = g
	// Fresh transport (no legacy ssrfDialContext) so AllowedCIDRs are
	// honored; WrapClient installs CheckRedirect and the guarded dialer.
	t.client = g.WrapClient(&http.Client{
		Timeout:   t.timeout,
		Transport: &http.Transport{MaxConnsPerHost: 8},
	})
}

func (t *WebSearchTool) Name() string { return "web_search" }

func (t *WebSearchTool) Category() string { return "web" }

func (t *WebSearchTool) Description() string {
	return "Search the web using DuckDuckGo and return results with titles, URLs, and snippets. Useful for finding current information, researching topics, and discovering relevant web pages."
}

func (t *WebSearchTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropQuery: {
				Type:        schemaTypeString,
				Description: "The search query string.",
			},
			schemaPropLimit: {
				Type:        schemaTypeInteger,
				Description: "Maximum number of results to return (default 10, max 30).",
			},
		},
		Required: []string{"query"},
	}
}

// SetSearchProvider installs the MCP-first search provider (searxng).
// Follows the typed-nil guard pattern: a nil provider leaves the direct
// DuckDuckGo scraper as the only backend. Must be called before the tool
// serves requests.
func (t *WebSearchTool) SetSearchProvider(p SearchProvider) {
	if p == nil {
		return
	}
	t.searchProviderMu.Lock()
	defer t.searchProviderMu.Unlock()
	t.searchProvider = p
}

// currentSearchProvider returns the installed provider, or nil.
func (t *WebSearchTool) currentSearchProvider() SearchProvider {
	t.searchProviderMu.Lock()
	defer t.searchProviderMu.Unlock()
	return t.searchProvider
}

// searchViaMCP runs one query through the installed search provider. It
// returns (results, true) on success; (zero, false) on any failure — the
// fallback chain then consults the DuckDuckGo scraper.
func (t *WebSearchTool) searchViaMCP(ctx context.Context, query string, limit int) (SearchResults, bool) {
	provider := t.currentSearchProvider()
	if provider == nil {
		return SearchResults{}, false
	}
	results, err := provider.Search(ctx, query, limit)
	if err != nil {
		return SearchResults{}, false
	}
	return results, true
}

// Execute performs a web search, preferring the MCP search provider
// (searxng) when one is installed and healthy, and falling back to the
// direct DuckDuckGo scraper (html endpoint, lite endpoint on challenge) on
// any provider error or absence.
func (t *WebSearchTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query is required")
	}

	// Parse limit
	limit := DefaultResultLimit
	if limitFloat, ok := args["limit"].(float64); ok && limitFloat > 0 {
		limit = min(int(limitFloat), MaxResultLimit)
	}

	// MCP-first: when a search provider (searxng MCP) is installed and
	// answers, its results ARE the answer; the DuckDuckGo path below is
	// the fallback for provider absence/failure only. The provider applies
	// its own timeout, so the DDG rate-limit slot below is only taken on
	// the fallback path.
	if results, ok := t.searchViaMCP(ctx, query, limit); ok {
		return results, nil
	}

	// Rate limiting: ensure minimum interval between requests
	t.mu.Lock()
	sinceLastRequest := time.Since(t.lastRequestTime)
	if sinceLastRequest < MinRequestInterval {
		waitTime := MinRequestInterval - sinceLastRequest
		t.lastRequestTime = time.Now().Add(waitTime) // reserve slot before waiting
		t.mu.Unlock()
		select {
		case <-time.After(waitTime):
		case <-ctx.Done():
			t.mu.Lock()
			t.lastRequestTime = time.Time{}
			t.mu.Unlock()
			return nil, ctx.Err()
		}
		t.mu.Lock()
	}
	t.lastRequestTime = time.Now()
	t.mu.Unlock()

	// Build search URL. The primary html endpoint now answers many scripted
	// requests with an anti-bot challenge (HTTP 202 + challenge HTML); when
	// that happens we retry once against the lite endpoint, which still
	// serves parseable results (verified live 2026-09-15).
	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))

	resp, status, fetchErr := t.fetchSearchPage(ctx, searchURL)
	if fetchErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("request timed out after %v", t.timeout)
		}
		return nil, fmt.Errorf("search request failed: %w", fetchErr)
	}
	defer func() {
		if resp != nil {
			resp.Body.Close()
		}
	}()

	if isChallengeStatus(status) {
		// Primary endpoint challenged — retry against the lite endpoint.
		if resp != nil {
			resp.Body.Close()
			resp = nil
		}
		liteURL := fmt.Sprintf("https://lite.duckduckgo.com/lite/?q=%s", url.QueryEscape(query))
		resp, status, fetchErr = t.fetchSearchPage(ctx, liteURL)
		if fetchErr != nil {
			return nil, fmt.Errorf("search request failed (primary endpoint blocked by anti-bot challenge; lite fallback also failed): %w", fetchErr)
		}
		defer resp.Body.Close()
	}

	if status != http.StatusOK {
		if isChallengeStatus(status) {
			return nil, fmt.Errorf("search blocked: duckduckgo returned an anti-bot challenge (HTTP %d) on both the html and lite endpoints; try again later or use a different search backend", status)
		}
		return nil, fmt.Errorf("search returned HTTP %d: %s", status, http.StatusText(status))
	}

	// Read response with size limit to prevent memory exhaustion
	limitedReader := io.LimitReader(resp.Body, MaxSearchResponseSize)
	bodyBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	body := string(bodyBytes)

	// Parse results — lite pages use the lite parser, html pages the html one
	var results []SearchResult
	var truncated bool
	if isLitePage(body) {
		results, truncated = t.parseDuckDuckGoLite(body, limit)
	} else {
		results, truncated = t.parseDuckDuckGoHTML(body, limit)
	}

	return SearchResults{
		Query:     query,
		Results:   results,
		Count:     len(results),
		Truncated: truncated,
	}, nil
}

// isChallengeStatus reports whether an HTTP status from DuckDuckGo indicates
// an anti-bot challenge rather than real results (202 Accepted with a
// challenge form, 403 Forbidden).
func isChallengeStatus(status int) bool {
	return status == http.StatusAccepted || status == http.StatusForbidden
}

// isLitePage detects the lite-endpoint response shape (table-based, no
// result__body divs).
func isLitePage(body string) bool {
	return !strings.Contains(body, "result__body") && strings.Contains(body, "duckduckgo.com/l/?uddg=")
}

// fetchSearchPage GETs a DuckDuckGo search page and returns the response,
// its status code, and any transport error. On a non-2xx status the caller
// is responsible for closing the returned response (it may want the body).
func (t *WebSearchTool) fetchSearchPage(ctx context.Context, pageURL string) (*http.Response, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, http.NoBody)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := t.client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, 0, fmt.Errorf("request timed out after %v", t.timeout)
		}
		return nil, 0, err
	}
	return resp, resp.StatusCode, nil
}

// parseDuckDuckGoHTML parses DuckDuckGo's HTML response to extract search results.
// DuckDuckGo HTML format uses:
// - <a class="result__a"> for title and URL
// - <a class="result__snippet"> for snippets
// - Results are contained in <div class="result__body"> blocks
func (t *WebSearchTool) parseDuckDuckGoHTML(html string, limit int) ([]SearchResult, bool) {
	var results []SearchResult

	// DuckDuckGo HTML search result patterns
	// Results are typically in <div class="result__body" ...> blocks
	resultBlockPattern := regexp.MustCompile(`(?si)<div[^>]*class="result__body[^"]*"[^>]*>(.*?)</div>`)

	// Title/URL link pattern: <a class="result__a" href="URL">TITLE</a>
	titleLinkPattern := regexp.MustCompile(`<a[^>]*class="result__a"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)

	// Snippet pattern - can be in different formats
	// DuckDuckGo uses <a class="result__snippet"> or <div class="result__snippet">
	snippetPatterns := []*regexp.Regexp{
		regexp.MustCompile(`<a[^>]*class="result__snippet"[^>]*>(.*?)</a>`),
		regexp.MustCompile(`<div[^>]*class="result__snippet"[^>]*>(.*?)</div>`),
		regexp.MustCompile(`<span[^>]*class="result__snippet"[^>]*>(.*?)</span>`),
	}

	// Find all result blocks
	blocks := resultBlockPattern.FindAllStringSubmatch(html, -1)

	for _, block := range blocks {
		if len(results) >= limit {
			return results, true // Hit limit, results truncated
		}

		if len(block) < 2 {
			continue
		}

		blockContent := block[1]

		// Extract title and URL
		titleMatch := titleLinkPattern.FindStringSubmatch(blockContent)
		if len(titleMatch) < 3 {
			continue
		}

		rawURL := titleMatch[1]
		rawTitle := titleMatch[2]

		// Clean up URL - DuckDuckGo sometimes adds redirect parameters
		cleanURL := t.cleanDuckDuckGoURL(rawURL)
		if cleanURL == "" {
			continue
		}

		// Decode HTML entities in title
		title := t.decodeHTMLEntities(rawTitle)
		title = stripHTML(title)
		title = strings.TrimSpace(title)

		// Extract snippet
		var snippet string
		for _, snippetPattern := range snippetPatterns {
			snippetMatch := snippetPattern.FindStringSubmatch(blockContent)
			if len(snippetMatch) >= 2 {
				rawSnippet := snippetMatch[1]
				snippet = t.decodeHTMLEntities(rawSnippet)
				snippet = stripHTML(snippet)
				snippet = strings.TrimSpace(snippet)
				// Clean up multiple spaces and newlines
				snippet = strings.Join(strings.Fields(snippet), " ")
				if snippet != "" {
					break
				}
			}
		}

		// Skip if no snippet - it's likely not a real search result
		if snippet == "" {
			continue
		}

		results = append(results, SearchResult{
			Title:   title,
			URL:     cleanURL,
			Snippet: snippet,
		})
	}

	return results, false
}

// parseDuckDuckGoLite parses the lite-endpoint response. The lite page is a
// flat table: one row holds the result link
// (<a rel="nofollow" href="//duckduckgo.com/l/?uddg=<real url>&rut=...">TITLE</a>),
// the next row holds the snippet text. Verified live 2026-09-15.
func (t *WebSearchTool) parseDuckDuckGoLite(html string, limit int) ([]SearchResult, bool) {
	var results []SearchResult
	truncated := false

	linkPattern := regexp.MustCompile(`(?si)<a[^>]*href="((?://)?duckduckgo\.com/l/\?uddg=[^"]*)"[^>]*>(.*?)</a>`)
	matches := linkPattern.FindAllStringSubmatch(html, -1)

	for i, match := range matches {
		if len(results) >= limit {
			truncated = true
			break
		}
		if len(match) < 3 {
			continue
		}

		cleanURL := t.cleanDuckDuckGoURL(match[1])
		if cleanURL == "" {
			continue
		}

		title := t.decodeHTMLEntities(match[2])
		title = stripHTML(title)
		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}

		// The snippet lives in the text between this link and the next one.
		snippet := ""
		thisEnd := strings.Index(html, match[0]) + len(match[0])
		nextStart := len(html)
		if i+1 < len(matches) {
			if nextIdx := strings.Index(html[thisEnd:], matches[i+1][0]); nextIdx >= 0 {
				nextStart = thisEnd + nextIdx
			}
		}
		between := html[thisEnd:nextStart]
		// Snippet text is plain text between tags; strip everything else.
		snippet = stripHTML(t.decodeHTMLEntities(between))
		snippet = strings.Join(strings.Fields(snippet), " ")
		// Trim the "..." continuation marker DDG appends, and cap length.
		snippet = strings.TrimSuffix(snippet, "...")
		if len(snippet) > 300 {
			snippet = snippet[:300]
		}

		results = append(results, SearchResult{
			Title:   title,
			URL:     cleanURL,
			Snippet: snippet,
		})
	}

	return results, truncated
}

// cleanDuckDuckGoURL removes DuckDuckGo redirect parameters from URLs.
// DuckDuckGo URLs can be in formats like:
// - /l/?uddg=https://example.com&rut=...
// - https://example.com (direct)
func (t *WebSearchTool) cleanDuckDuckGoURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)

	// Handle redirect URLs
	if strings.HasPrefix(rawURL, "/l/?") || strings.HasPrefix(rawURL, "//duckduckgo.com/l/?") {
		// Extract the uddg parameter which contains the real URL
		parts := strings.Split(rawURL, "uddg=")
		if len(parts) > 1 {
			encodedURL := strings.Split(parts[1], "&")[0]
			if unescaped, err := url.QueryUnescape(encodedURL); err == nil {
				rawURL = unescaped
			}
		}
	}

	// Ensure URL has a scheme
	if rawURL != "" && !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		if strings.HasPrefix(rawURL, "//") {
			rawURL = "https:" + rawURL
		} else {
			rawURL = "https://" + rawURL
		}
	}

	// Validate URL format
	if u, err := url.Parse(rawURL); err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}

	return rawURL
}

// decodeHTMLEntities decodes common HTML entities.
func (t *WebSearchTool) decodeHTMLEntities(s string) string {
	// Common HTML entities
	replacements := map[string]string{
		"&amp;":    "&",
		"&lt;":     "<",
		"&gt;":     ">",
		"&quot;":   "\"",
		"&#39;":    "'",
		"&apos;":   "'",
		"&nbsp;":   " ",
		"&mdash;":  "—",
		"&ndash;":  "–",
		"&hellip;": "...",
		"&euro;":   "€",
		"&pound;":  "£",
		"&copy;":   "(C)",
		"&reg;":    "(R)",
		"&trade;":  "(TM)",
	}

	// Handle numeric entities like &#123; and &#x1F600;
	numericEntity := regexp.MustCompile(`&#(\d+);`)
	hexEntity := regexp.MustCompile(`&#x([0-9a-fA-F]+);`)

	// First replace named entities
	for entity, char := range replacements {
		s = strings.ReplaceAll(s, entity, char)
	}

	// Replace decimal numeric entities
	s = numericEntity.ReplaceAllStringFunc(s, func(match string) string {
		numStr := match[2 : len(match)-1] // Skip &# and ;
		if num, err := strconv.ParseInt(numStr, 10, 32); err == nil {
			return string(rune(num))
		}
		return match
	})

	// Replace hex numeric entities
	s = hexEntity.ReplaceAllStringFunc(s, func(match string) string {
		hexStr := match[3 : len(match)-1] // Skip &#x and ;
		if num, err := strconv.ParseInt(hexStr, 16, 32); err == nil {
			return string(rune(num))
		}
		return match
	})

	return s
}

// IsReadOnly reports that web searches are always read-only.
func (t *WebSearchTool) IsReadOnly(map[string]any) bool { return true }

// IsConcurrencySafe reports that web searches are safe for concurrent execution.
func (t *WebSearchTool) IsConcurrencySafe(map[string]any) bool { return true }

// Ensure WebSearchTool implements the Tool interface
var _ tools.Tool = (*WebSearchTool)(nil)
