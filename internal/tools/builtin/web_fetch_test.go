package builtin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/tools"
)

func TestWebFetchTool_Execute(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plain":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("Hello, World!"))
		case "/html":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>Test</title></head>
<body>
<script>alert('test');</script>
<style>.hidden { display: none; }</style>
<h1>Welcome</h1>
<p>This is a &amp; test &lt;page&gt;.</p>
</body>
</html>`))
		case "/error":
			http.Error(w, "Not Found", http.StatusNotFound)
		case "/slow":
			time.Sleep(5 * time.Second)
			w.Write([]byte("slow"))
		default:
			w.Write([]byte("default"))
		}
	}))
	defer server.Close()

	tool := NewWebFetchTool(time.Second*5, 50000)
	ctx := context.Background()

	// Test plain text
	t.Run("plain text", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"url": server.URL + "/plain",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		fetchResult := unwrapFetchResult(t, result)

		if fetchResult.StatusCode != 200 {
			t.Errorf("expected status 200, got %d", fetchResult.StatusCode)
		}
		if fetchResult.Content != "Hello, World!" {
			t.Errorf("expected 'Hello, World!', got %q", fetchResult.Content)
		}
	})

	// Test HTML stripping
	t.Run("html stripping", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"url": server.URL + "/html",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		fetchResult := unwrapFetchResult(t, result)

		// Should not contain script or style content
		if strings.Contains(fetchResult.Content, "alert") {
			t.Error("content should not contain script content")
		}
		if strings.Contains(fetchResult.Content, ".hidden") {
			t.Error("content should not contain style content")
		}

		// Should contain the actual text
		if !strings.Contains(fetchResult.Content, "Welcome") {
			t.Error("content should contain 'Welcome'")
		}

		// HTML entities should be decoded
		if !strings.Contains(fetchResult.Content, "& test") {
			t.Error("HTML entities should be decoded")
		}
	})

	// Test HTTP error
	t.Run("http error", func(t *testing.T) {
		_, err := tool.Execute(ctx, map[string]any{
			"url": server.URL + "/error",
		})
		if err == nil {
			t.Error("expected error for 404")
		}
		if !strings.Contains(err.Error(), "404") {
			t.Errorf("expected 404 in error, got: %v", err)
		}
	})

	// Test invalid URL scheme
	t.Run("invalid scheme", func(t *testing.T) {
		_, err := tool.Execute(ctx, map[string]any{
			"url": "ftp://example.com/file",
		})
		if err == nil {
			t.Error("expected error for invalid scheme")
		}
	})

	// Test empty URL
	t.Run("empty url", func(t *testing.T) {
		_, err := tool.Execute(ctx, map[string]any{
			"url": "",
		})
		if err == nil {
			t.Error("expected error for empty URL")
		}
	})

	// Test max_length
	t.Run("max length", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"url":        server.URL + "/plain",
			"max_length": float64(5),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		fetchResult := unwrapFetchResult(t, result)

		if len(fetchResult.Content) != 5 {
			t.Errorf("expected content length 5, got %d", len(fetchResult.Content))
		}
		if !fetchResult.Truncated {
			t.Error("expected truncated to be true")
		}
	})
}

func TestStripHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain text",
			input: "Hello, World!",
			want:  "Hello, World!",
		},
		{
			name:  "simple tags",
			input: "<p>Hello</p>",
			want:  "Hello",
		},
		{
			name:  "script removal",
			input: "<script>alert('test');</script>Hello",
			want:  "Hello",
		},
		{
			name:  "style removal",
			input: "<style>.hidden{display:none}</style>Hello",
			want:  "Hello",
		},
		{
			name:  "html entities",
			input: "&amp; &lt; &gt; &quot; &apos;",
			want:  "& < > \" '",
		},
		{
			name:  "block tags to newlines",
			input: "<p>Para 1</p><p>Para 2</p>",
			want:  "Para 1\nPara 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripHTML(tt.input)
			// Normalize whitespace for comparison
			got = strings.TrimSpace(got)
			if got != tt.want {
				t.Errorf("stripHTML(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestWebFetchTool_Timeout(t *testing.T) {
	// Create a slow server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte("slow response"))
	}))
	defer server.Close()

	tool := NewWebFetchTool(100*time.Millisecond, 50000)
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]any{
		"url": server.URL,
	})
	if err == nil {
		t.Error("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") && !strings.Contains(err.Error(), "deadline") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

func TestWebFetchTool_Redirects(t *testing.T) {
	redirectCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/final" {
			w.Write([]byte("final destination"))
			return
		}
		redirectCount++
		http.Redirect(w, r, "/final", http.StatusFound)
	}))
	defer server.Close()

	tool := NewWebFetchTool(time.Second*5, 50000)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"url": server.URL + "/start",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fetchResult := unwrapFetchResult(t, result)

	if fetchResult.Content != "final destination" {
		t.Errorf("expected 'final destination', got %q", fetchResult.Content)
	}
	if !strings.Contains(fetchResult.URL, "/final") {
		t.Errorf("expected final URL to contain '/final', got %q", fetchResult.URL)
	}
}

func unwrapFetchResult(t *testing.T, result any) FetchResult {
	t.Helper()
	toolResult, ok := result.(tools.ToolResult)
	if !ok {
		t.Fatalf("expected tools.ToolResult, got %T", result)
	}
	fetchResult, ok := toolResult.Result.(FetchResult)
	if !ok {
		t.Fatalf("expected FetchResult in ToolResult.Result, got %T", toolResult.Result)
	}
	return fetchResult
}
