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
	timeout         time.Duration
	client          *http.Client
	mu              sync.Mutex
	lastRequestTime time.Time
}

// NewWebSearchTool creates a new web search tool.
func NewWebSearchTool(timeout time.Duration) *WebSearchTool {
	if timeout == 0 {
		timeout = DefaultSearchTimeout
	}

	return &WebSearchTool{
		timeout: timeout,
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
	}
}

func (t *WebSearchTool) Name() string { return "web_search" }

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

// Execute performs a web search.
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

	// Build search URL
	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	// Execute request
	resp, err := t.client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("request timed out after %v", t.timeout)
		}
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search returned HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Read response with size limit to prevent memory exhaustion
	limitedReader := io.LimitReader(resp.Body, MaxSearchResponseSize)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse results
	results, truncated := t.parseDuckDuckGoHTML(string(body), limit)

	return SearchResults{
		Query:     query,
		Results:   results,
		Count:     len(results),
		Truncated: truncated,
	}, nil
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
		title = t.stripHTMLTags(title)
		title = strings.TrimSpace(title)

		// Extract snippet
		var snippet string
		for _, snippetPattern := range snippetPatterns {
			snippetMatch := snippetPattern.FindStringSubmatch(blockContent)
			if len(snippetMatch) >= 2 {
				rawSnippet := snippetMatch[1]
				snippet = t.decodeHTMLEntities(rawSnippet)
				snippet = t.stripHTMLTags(snippet)
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

// stripHTMLTags removes all HTML tags from a string.
func (t *WebSearchTool) stripHTMLTags(s string) string {
	// Remove any HTML tags
	tagRE := regexp.MustCompile(`<[^>]+>`)
	return tagRE.ReplaceAllString(s, "")
}

// Ensure WebSearchTool implements the Tool interface
var _ tools.Tool = (*WebSearchTool)(nil)
