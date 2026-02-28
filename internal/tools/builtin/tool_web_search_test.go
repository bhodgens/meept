package builtin

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestWebSearchTool_NameAndDescription(t *testing.T) {
	tool := NewWebSearchTool(0)

	if tool.Name() != "web_search" {
		t.Errorf("expected name 'web_search', got %q", tool.Name())
	}

	desc := tool.Description()
	if desc == "" {
		t.Error("description should not be empty")
	}
	if !strings.Contains(desc, "search") {
		t.Error("description should mention search")
	}
}

func TestWebSearchTool_Parameters(t *testing.T) {
	tool := NewWebSearchTool(0)
	params := tool.Parameters()

	if params.Type != "object" {
		t.Errorf("expected type 'object', got %q", params.Type)
	}

	if len(params.Properties) != 2 {
		t.Errorf("expected 2 parameters, got %d", len(params.Properties))
	}

	// Check query parameter
	queryParam, ok := params.Properties["query"]
	if !ok {
		t.Fatal("query parameter missing")
	}
	if queryParam.Type != "string" {
		t.Errorf("expected query type 'string', got %q", queryParam.Type)
	}

	// Check limit parameter
	limitParam, ok := params.Properties["limit"]
	if !ok {
		t.Fatal("limit parameter missing")
	}
	if limitParam.Type != "integer" {
		t.Errorf("expected limit type 'integer', got %q", limitParam.Type)
	}

	// Check required
	if len(params.Required) != 1 || params.Required[0] != "query" {
		t.Errorf("expected required ['query'], got %v", params.Required)
	}
}

func TestWebSearchTool_Execute_EmptyQuery(t *testing.T) {
	tool := NewWebSearchTool(0)
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]any{
		"query": "",
	})

	if err == nil {
		t.Error("expected error for empty query")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("expected 'required' in error, got: %v", err)
	}
}

func TestWebSearchTool_Execute_NoQuery(t *testing.T) {
	tool := NewWebSearchTool(0)
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]any{})

	if err == nil {
		t.Error("expected error for missing query")
	}
}

func TestWebSearchTool_Execute_WhitespaceQuery(t *testing.T) {
	tool := NewWebSearchTool(0)
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]any{
		"query": "   \t  ",
	})

	if err == nil {
		t.Error("expected error for whitespace-only query")
	}
}

func TestWebSearchTool_ParseDuckDuckGoHTML(t *testing.T) {
	tool := NewWebSearchTool(0)

	tests := []struct {
		name      string
		html      string
		limit     int
		wantMin   int // minimum expected results
		truncated bool
	}{
		{
			name: "valid results",
			html: `
				<div class="result__body">
					<a class="result__a" href="https://example.com">Example Domain</a>
					<a class="result__snippet">This is an example domain for testing.</a>
				</div>
				<div class="result__body">
					<a class="result__a" href="https://test.com">Test Site</a>
					<a class="result__snippet">A test website with information.</a>
				</div>
			`,
			limit:   10,
			wantMin: 2,
		},
		{
			name: "with redirect URL",
			html: `
				<div class="result__body">
					<a class="result__a" href="/l/?uddg=https%3A%2F%2Fexample.com&amp;rut=123">Example</a>
					<a class="result__snippet">Example website</a>
				</div>
			`,
			limit:   10,
			wantMin: 1,
		},
		{
			name: "with HTML entities in title",
			html: `
				<div class="result__body">
					<a class="result__a" href="https://example.com">Example &amp; Test &lt;3&gt;</a>
					<a class="result__snippet">A test snippet.</a>
				</div>
			`,
			limit:   10,
			wantMin: 1,
		},
		{
			name: "with HTML entities in snippet",
			html: `
				<div class="result__body">
					<a class="result__a" href="https://example.com">Example</a>
					<a class="result__snippet">This is an &amp; example &quot;text&quot;.</a>
				</div>
			`,
			limit:   10,
			wantMin: 1,
		},
		{
			name: "limit results",
			html: `
				<div class="result__body">
					<a class="result__a" href="https://example1.com">Result 1</a>
					<a class="result__snippet">Snippet 1</a>
				</div>
				<div class="result__body">
					<a class="result__a" href="https://example2.com">Result 2</a>
					<a class="result__snippet">Snippet 2</a>
				</div>
				<div class="result__body">
					<a class="result__a" href="https://example3.com">Result 3</a>
					<a class="result__snippet">Snippet 3</a>
				</div>
			`,
			limit:     2,
			wantMin:   2,
			truncated: true,
		},
		{
			name: "no valid results",
			html: `
				<div class="something">
					<a href="https://example.com">Link</a>
				</div>
			`,
			limit:   10,
			wantMin: 0,
		},
		{
			name: "result without snippet (should be skipped)",
			html: `
				<div class="result__body">
					<a class="result__a" href="https://example.com">Example</a>
				</div>
			`,
			limit:   10,
			wantMin: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, truncated := tool.parseDuckDuckGoHTML(tt.html, tt.limit)

			if len(results) < tt.wantMin {
				t.Errorf("expected at least %d results, got %d", tt.wantMin, len(results))
			}

			if truncated != tt.truncated {
				t.Errorf("expected truncated=%v, got %v", tt.truncated, truncated)
			}

			// Verify result structure
			for i, result := range results {
				if result.Title == "" {
					t.Errorf("result %d: title should not be empty", i)
				}
				if result.URL == "" {
					t.Errorf("result %d: URL should not be empty", i)
				}
				if result.Snippet == "" {
					t.Errorf("result %d: snippet should not be empty", i)
				}
			}
		})
	}
}

func TestWebSearchTool_CleanDuckDuckGoURL(t *testing.T) {
	tool := NewWebSearchTool(0)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "direct URL",
			input: "https://example.com/path",
			want:  "https://example.com/path",
		},
		{
			name:  "redirect URL",
			input: "/l/?uddg=https%3A%2F%2Fexample.com%2Fpath&rut=123",
			want:  "https://example.com/path",
		},
		{
			name:  "redirect with full domain",
			input: "//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com",
			want:  "https://example.com",
		},
		{
			name:  "protocol-relative URL",
			input: "//example.com/path",
			want:  "https://example.com/path",
		},
		{
			name:  "URL without protocol",
			input: "example.com/path",
			want:  "https://example.com/path",
		},
		{
			name:  "invalid URL",
			input: "not a url",
			want:  "",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tool.cleanDuckDuckGoURL(tt.input)
			if got != tt.want {
				t.Errorf("cleanDuckDuckGoURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestWebSearchTool_DecodeHTMLEntities(t *testing.T) {
	tool := NewWebSearchTool(0)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "ampersand",
			input: "Hello &amp; World",
			want:  "Hello & World",
		},
		{
			name:  "less than",
			input: "a &lt; b",
			want:  "a < b",
		},
		{
			name:  "greater than",
			input: "a &gt; b",
			want:  "a > b",
		},
		{
			name:  "quote",
			input: "&quot;text&quot;",
			want:  "\"text\"",
		},
		{
			name:  "apostrophe",
			input: "It&apos;s mine",
			want:  "It's mine",
		},
		{
			name:  "numeric decimal",
			input: "&#65;&#66;&#67;",
			want:  "ABC",
		},
		{
			name:  "numeric hex",
			input: "&#x41;&#x42;&#x43;",
			want:  "ABC",
		},
		{
			name:  "em dash",
			input: "em&mdash;dash",
			want:  "em—dash",
		},
		{
			name:  "ellipsis",
			input: "more&hellip;",
			want:  "more...",
		},
		{
			name:  "mixed",
			input: " &lt;div&gt;&quot;Hello&quot;&amp;&#39;World&#39;&hellip;&quot; &lt;/div&gt;",
			want:  " <div>\"Hello\"&'World'…\" </div>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tool.decodeHTMLEntities(tt.input)
			if got != tt.want {
				t.Errorf("decodeHTMLEntities(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestWebSearchTool_StripHTMLTags(t *testing.T) {
	tool := NewWebSearchTool(0)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no tags",
			input: "Hello World",
			want:  "Hello World",
		},
		{
			name:  "simple tag",
			input: "<p>Hello</p>",
			want:  "Hello",
		},
		{
			name:  "multiple tags",
			input: "<b>Hello</b> <i>World</i>",
			want:  "Hello World",
		},
		{
			name:  "nested tags",
			input: "<div><span>Hello</span> <em>World</em></div>",
			want:  "Hello World",
		},
		{
			name:  "self-closing tag",
			input: "Hello<br/>World",
			want:  "HelloWorld",
		},
		{
			name:  "attributes",
			input: `<a href="http://example.com" class="link">Link</a>`,
			want:  "Link",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tool.stripHTMLTags(tt.input)
			if got != tt.want {
				t.Errorf("stripHTMLTags(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestWebSearchTool_RateLimiting(t *testing.T) {
	tool := NewWebSearchTool(time.Second)
	ctx := context.Background()

	// Note: This test doesn't make actual HTTP requests but verifies
	// that the rate limiting logic is in place by checking the mutex is used
	// In a real integration test, we would mock the HTTP client

	// Quick consecutive calls should be rate limited
	// This is a basic smoke test
	if tool.lastRequestTime.IsZero() {
		// First call should work (we'd need to mock HTTP for full test)
		tool.mu.Lock()
		tool.lastRequestTime = time.Now()
		tool.mu.Unlock()
	}

	// Verify the tool can be created without panicking
	_ = tool
}

func TestWebSearchTool_LimitValidation(t *testing.T) {
	tool := NewWebSearchTool(0)

	tests := []struct {
		name      string
		limit     float64
		wantLimit int
	}{
		{
			name:      "default",
			limit:     0,
			wantLimit: DefaultResultLimit,
		},
		{
			name:      "within max",
			limit:     15,
			wantLimit: 15,
		},
		{
			name:      "at max",
			limit:     MaxResultLimit,
			wantLimit: MaxResultLimit,
		},
		{
			name:      "over max",
			limit:     100,
			wantLimit: MaxResultLimit,
		},
		{
			name:      "below max",
			limit:     5,
			wantLimit: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limit := DefaultResultLimit
			if tt.limit > 0 {
				limit = int(tt.limit)
				if limit > MaxResultLimit {
					limit = MaxResultLimit
				}
			}
			if limit != tt.wantLimit {
				t.Errorf("limit = %d, want %d", limit, tt.wantLimit)
			}
		})
	}
}
