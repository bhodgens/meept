package builtin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
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
	// HTML entity replacements
	htmlEntityMap = map[string]string{
		"&amp;":  "&",
		"&lt;":   "<",
		"&gt;":   ">",
		"&quot;": "\"",
		"&#39;":  "'",
		"&apos;": "'",
		"&nbsp;": " ",
	}

	// Regex patterns for HTML stripping
	scriptRE         = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRE          = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	blockTagRE       = regexp.MustCompile(`(?i)<(br|p|div|h[1-6]|li|tr)[^>]*>`)
	tagRE            = regexp.MustCompile(`<[^>]+>`)
	multiWhitespaceRE = regexp.MustCompile(`\n{3,}`)
)

// WebFetchTool fetches content from URLs and converts to plain text.
type WebFetchTool struct {
	timeout   time.Duration
	maxLength int
	client    *http.Client
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

func (t *WebFetchTool) Description() string {
	return "Fetch the content of a URL and return it as plain text. HTML is automatically stripped. Useful for reading web pages, API responses, and documentation."
}

func (t *WebFetchTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"url": {
				Type:        "string",
				Description: "The URL to fetch.",
			},
			"max_length": {
				Type:        "integer",
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

	// Validate URL scheme
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("only http:// and https:// URLs are supported")
	}

	maxLength := t.maxLength
	if ml, ok := args["max_length"].(float64); ok && ml > 0 {
		maxLength = int(ml)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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

// stripHTML converts HTML to plain text.
func stripHTML(html string) string {
	// Remove script and style blocks
	text := scriptRE.ReplaceAllString(html, "")
	text = styleRE.ReplaceAllString(text, "")

	// Replace block-level tags with newlines
	text = blockTagRE.ReplaceAllString(text, "\n")

	// Strip remaining tags
	text = tagRE.ReplaceAllString(text, "")

	// Decode common HTML entities
	for entity, char := range htmlEntityMap {
		text = strings.ReplaceAll(text, entity, char)
	}

	// Collapse excessive whitespace
	text = multiWhitespaceRE.ReplaceAllString(text, "\n\n")

	return strings.TrimSpace(text)
}

// Ensure WebFetchTool implements the Tool interface
var _ tools.Tool = (*WebFetchTool)(nil)
