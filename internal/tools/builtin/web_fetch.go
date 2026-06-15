package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
	"golang.org/x/net/html"
)

const (
	// DefaultFetchTimeout is the default timeout for HTTP requests.
	DefaultFetchTimeout = 10 * time.Second
	// MaxResponseSize is the maximum response body size (100 KB).
	MaxResponseSize = 100 * 1024
	// DefaultMaxOutputLength is the maximum output length after processing.
	DefaultMaxOutputLength = 50000
)

var (
	// multiWhitespaceRE collapses runs of 3+ newlines to exactly two.
	multiWhitespaceRE = regexp.MustCompile(`\n{3,}`)
)

// WebFetchTool fetches content from URLs and converts to plain text.
type WebFetchTool struct {
	timeout   time.Duration
	maxLength int
	client    *http.Client
	secOrch   SecurityChecker
	// allowPrivateRanges disables the SSRF private/loopback IP filter.
	// Production code never sets this; it exists for unit tests that run
	// against httptest.NewServer (which binds to 127.0.0.1).
	allowPrivateRanges bool
}

// SecurityChecker is an interface for pre-fetch security checks.
type SecurityChecker interface {
	CheckWebFetch(url string) (blocked bool, reason string)
}

// NewWebFetchTool creates a new web fetch tool.
func NewWebFetchTool(timeout time.Duration, maxLength int) *WebFetchTool {
	if timeout == 0 {
		timeout = DefaultFetchTimeout
	}
	if maxLength == 0 {
		maxLength = DefaultMaxOutputLength
	}

	return &WebFetchTool{
		timeout:   timeout,
		maxLength: maxLength,
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

func (t *WebFetchTool) Name() string { return "web_fetch" }

// SetSecurityOrchestrator sets the security orchestrator for taint/exfil checks.
func (t *WebFetchTool) SetSecurityOrchestrator(orch SecurityChecker) {
	t.secOrch = orch
}

// SetAllowPrivateRanges disables SSRF protection for private/loopback IPs.
// Intended only for unit tests that exercise the fetch path against
// httptest.NewServer. Production callers must never invoke this.
func (t *WebFetchTool) SetAllowPrivateRanges(allow bool) {
	t.allowPrivateRanges = allow
}

func (t *WebFetchTool) Category() string { return "web" }

func (t *WebFetchTool) Description() string {
	return "Fetch the content of a URL and return it as plain text. HTML is automatically stripped. Useful for reading web pages, API responses, and documentation."
}

func (t *WebFetchTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"url": {
				Type:        schemaTypeString,
				Description: "The URL to fetch.",
			},
			"max_length": {
				Type:        schemaTypeInteger,
				Description: "Maximum characters to return (default 50000).",
			},
		},
		Required: []string{"url"},
	}
}

// FetchResult contains the result of a web fetch operation.
type FetchResult struct {
	Content     string `json:"content"`
	URL         string `json:"url"`
	StatusCode  int    `json:"status_code"`
	ContentType string `json:"content_type"`
	Truncated   bool   `json:"truncated,omitempty"`
}

func (t *WebFetchTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	url, _ := args["url"].(string)
	if url == "" {
		return nil, fmt.Errorf("no URL specified")
	}

	// Check taint/exfiltration policy
	if t.secOrch != nil {
		if blocked, reason := t.secOrch.CheckWebFetch(url); blocked {
			return nil, fmt.Errorf("web fetch blocked by security policy: %s", reason)
		}
	}

	// SSRF guard: refuse private/loopback/link-local targets before
	// constructing the request. This catches both raw-IP and hostname
	// targets that resolve to blocked ranges.
	if !t.allowPrivateRanges {
		if err := checkURL(url); err != nil {
			return nil, fmt.Errorf("web_fetch blocked: %w", err)
		}
	}

	// Validate URL scheme (defense in depth; checkURL also enforces this)
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("only http:// and https:// URLs are supported")
	}

	maxLength := t.maxLength
	if ml, ok := args["max_length"].(float64); ok && ml > 0 {
		maxLength = int(ml)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Meept/0.2 (autonomous assistant)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,*/*")

	// Execute request
	resp, err := t.client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("request timed out after %v", t.timeout)
		}
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Read body with size limit
	limitedReader := io.LimitReader(resp.Body, MaxResponseSize)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	text := string(body)

	// Strip HTML if the response looks like HTML
	if strings.Contains(strings.ToLower(contentType), "html") || strings.HasPrefix(strings.TrimSpace(text), "<!") {
		text = stripHTML(text)
	}

	// Truncate to max length
	truncated := false
	if len(text) > maxLength {
		text = text[:maxLength]
		truncated = true
	}

	result := FetchResult{
		Content:     text,
		URL:         resp.Request.URL.String(),
		StatusCode:  resp.StatusCode,
		ContentType: contentType,
		Truncated:   truncated,
	}

	// Build evidence: API response confirmation
	evidence := []models.Evidence{
		models.NewEvidence(
			models.EvidenceAPIResponse,
			url,
			fmt.Sprintf("status=%d,size=%d,content_type=%s", resp.StatusCode, len(body), contentType),
			t.Name(),
		),
	}

	return tools.ToolResult{
		Success:  true,
		Result:   result,
		Evidence: evidence,
	}, nil
}

// stripHTML converts HTML to plain text using a proper HTML parser.
func stripHTML(s string) string {
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		// Should never happen with valid html.Parse, but return raw on error
		return s
	}

	var b strings.Builder
	renderText(&b, doc)

	text := b.String()
	text = multiWhitespaceRE.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

// renderText recursively walks the HTML node tree and writes text content.
// It skips <script> and <style> blocks entirely and adds whitespace
// around block-level elements.
func renderText(b *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(n.Data)
	case html.ElementNode:
		switch n.Data {
		case "script", "style", "noscript":
			// Skip these subtrees entirely
			return
		case "br":
			b.WriteByte('\n')
		case "p", "div", "h1", "h2", "h3", "h4", "h5", "h6":
			if n.FirstChild != nil {
				b.WriteByte('\n')
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				renderText(b, c)
			}
			b.WriteByte('\n')
			return
		case "li":
			b.WriteString("- ")
		case "tr":
			if n.FirstChild != nil {
				b.WriteByte('\n')
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			renderText(b, c)
		}
	case html.DocumentNode, html.DoctypeNode:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			renderText(b, c)
		}
	}
}

// Ensure WebFetchTool implements the Tool interface
var _ tools.Tool = (*WebFetchTool)(nil)

// ExecuteStreaming implements tools.StreamingTool with progress updates during fetch.
func (t *WebFetchTool) ExecuteStreaming(ctx context.Context, args map[string]any, onUpdate func(tools.ProgressUpdate)) (any, error) {
	url, _ := args["url"].(string)
	if url == "" {
		return nil, fmt.Errorf("no URL specified")
	}

	onUpdate(tools.ProgressUpdate{Message: fmt.Sprintf("fetching %s...", url), Percent: 10})

	// Check taint/exfiltration policy
	if t.secOrch != nil {
		if blocked, reason := t.secOrch.CheckWebFetch(url); blocked {
			return nil, fmt.Errorf("web fetch blocked by security policy: %s", reason)
		}
	}

	// SSRF guard: refuse private/loopback/link-local targets before
	// constructing the request.
	if !t.allowPrivateRanges {
		if err := checkURL(url); err != nil {
			return nil, fmt.Errorf("web_fetch blocked: %w", err)
		}
	}

	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("only http:// and https:// URLs are supported")
	}

	maxLength := t.maxLength
	if ml, ok := args["max_length"].(float64); ok && ml > 0 {
		maxLength = int(ml)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Meept/0.2 (autonomous assistant)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,*/*")

	resp, err := t.client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("request timed out after %v", t.timeout)
		}
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	onUpdate(tools.ProgressUpdate{Message: fmt.Sprintf("received response, reading body (%d status)...", resp.StatusCode), Percent: 40})

	limitedReader := io.LimitReader(resp.Body, MaxResponseSize)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	onUpdate(tools.ProgressUpdate{
		Message: fmt.Sprintf("received %d bytes, parsing response...", len(body)),
		Percent: 70,
	})

	contentType := resp.Header.Get("Content-Type")
	text := string(body)

	if strings.Contains(strings.ToLower(contentType), "html") || strings.HasPrefix(strings.TrimSpace(text), "<!") {
		text = stripHTML(text)
	}

	truncated := false
	if len(text) > maxLength {
		text = text[:maxLength]
		truncated = true
	}

	result := FetchResult{
		Content:     text,
		URL:         resp.Request.URL.String(),
		StatusCode:  resp.StatusCode,
		ContentType: contentType,
		Truncated:   truncated,
	}

	evidence := []models.Evidence{
		models.NewEvidence(
			models.EvidenceAPIResponse,
			url,
			fmt.Sprintf("status=%d,size=%d,content_type=%s", resp.StatusCode, len(body), contentType),
			t.Name(),
		),
	}

	partialJSON, _ := json.Marshal(map[string]any{"url": result.URL, "status": result.StatusCode, "size": len(body)})
	onUpdate(tools.ProgressUpdate{Message: "complete", Percent: 100, PartialResult: partialJSON})

	return tools.ToolResult{
		Success:  true,
		Result:   result,
		Evidence: evidence,
	}, nil
}

// Ensure WebFetchTool implements the StreamingTool interface.
var _ tools.StreamingTool = (*WebFetchTool)(nil)
