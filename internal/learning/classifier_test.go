package learning

import "testing"

func TestClassifyDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		query      string
		toolOutput string
		want       string
	}{
		{
			name:       "code domain",
			query:      "how to write a function in go",
			toolOutput: "package main import fmt",
			want:       "code",
		},
		{
			name:       "debugging domain",
			query:      "why am I getting this error panic stack trace",
			toolOutput: "goroutine fail bug error",
			want:       "debugging",
		},
		{
			name:       "api_research domain",
			query:      "how to authenticate with the REST API endpoint",
			toolOutput: "http GET /v1/endpoint",
			want:       "api_research",
		},
		{
			name:       "empty input defaults to code",
			query:      "",
			toolOutput: "",
			want:       "code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyDomain(tt.query, tt.toolOutput)
			if got != tt.want {
				t.Errorf("ClassifyDomain(%q, %q) = %q, want %q", tt.query, tt.toolOutput, got, tt.want)
			}
		})
	}
}

func TestCountKeywords(t *testing.T) {
	t.Parallel()

	count := countKeywords("hello world hello", []string{"hello"})
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}

	count = countKeywords("code code code function", []string{"code", "function"})
	if count != 4 {
		t.Errorf("expected 4, got %d", count)
	}

	count = countKeywords("", []string{"code"})
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}
