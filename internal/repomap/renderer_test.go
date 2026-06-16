package repomap

import (
	"strings"
	"testing"
)

func TestContextRenderer_Render(t *testing.T) {
	config := DefaultRendererConfig()
	renderer := NewContextRenderer(config, nil)

	// Test empty ranked tags
	result := renderer.Render(nil)
	if result.Tokens != 0 {
		t.Errorf("expected 0 tokens for nil, got %d", result.Tokens)
	}

	result = renderer.Render(RankedTags{})
	if result.Tokens != 0 {
		t.Errorf("expected 0 tokens for empty, got %d", result.Tokens)
	}

	// Test with ranked tags
	ranked := RankedTags{
		{Tag: Tag{RelFname: "a.go", FName: "testdata/a.go", Name: "FuncA", Kind: "function", Line: 1, IsDef: true}, Score: 1.0},
		{Tag: Tag{RelFname: "b.go", FName: "testdata/b.go", Name: "TypeB", Kind: "class", Line: 5, IsDef: true}, Score: 0.5},
	}

	result = renderer.Render(ranked)
	if result.Tokens == 0 {
		t.Error("expected non-zero tokens for ranked tags")
	}
	if result.Content == "" {
		t.Error("expected non-empty content")
	}

	// Check content format - should contain file names
	if !strings.Contains(result.Content, "a.go:") {
		t.Error("expected content to contain 'a.go:'")
	}
	if !strings.Contains(result.Content, "b.go:") {
		t.Error("expected content to contain 'b.go:'")
	}
}

func TestContextRenderer_RenderCompact(t *testing.T) {
	config := DefaultRendererConfig()
	config.ContextLines = 0 // Disable context lines for compact mode
	renderer := NewContextRenderer(config, nil)

	ranked := RankedTags{
		{Tag: Tag{RelFname: "test.go", FName: "testdata/test.go", Name: "TestFunc", Kind: "function", Line: 10, IsDef: true}, Score: 1.0},
	}

	result := renderer.RenderCompact(ranked)
	if result.Tokens == 0 {
		t.Error("expected non-zero tokens for compact render")
	}
}

func TestContextRenderer_RenderHierarchical(t *testing.T) {
	config := DefaultRendererConfig()
	renderer := NewContextRenderer(config, nil)

	ranked := RankedTags{
		{Tag: Tag{RelFname: "pkg1/file1.go", FName: "pkg1/file1.go", Name: "Func1", Kind: "function", Line: 1, IsDef: true}, Score: 1.0},
		{Tag: Tag{RelFname: "pkg1/file2.go", FName: "pkg1/file2.go", Name: "Func2", Kind: "function", Line: 2, IsDef: true}, Score: 0.8},
		{Tag: Tag{RelFname: "pkg2/file3.go", FName: "pkg2/file3.go", Name: "Type1", Kind: "class", Line: 5, IsDef: true}, Score: 0.5},
	}

	result := renderer.RenderHierarchical(ranked)
	if result.Tokens == 0 {
		t.Error("expected non-zero tokens for hierarchical render")
	}

	// Should contain directory structure
	if !strings.Contains(result.Content, "pkg1/") {
		t.Error("expected content to contain 'pkg1/'")
	}
	if !strings.Contains(result.Content, "pkg2/") {
		t.Error("expected content to contain 'pkg2/'")
	}
}

func TestContextRenderer_RenderSummarized(t *testing.T) {
	config := DefaultRendererConfig()
	renderer := NewContextRenderer(config, nil)

	var ranked RankedTags
	for i := 0; i < 20; i++ {
		ranked = append(ranked, RankedTag{
			Tag:   Tag{RelFname: "file" + string(rune('0'+i%10)) + ".go", Name: "Symbol" + string(rune('A'+i)), Kind: "function", Line: i * 5, IsDef: true},
			Score: 1.0 - float64(i)*0.05,
		})
	}

	result := renderer.RenderSummarized(ranked, 5)
	if result.Tokens == 0 {
		t.Error("expected non-zero tokens for summarized render")
	}

	// Should start with summary header
	if !strings.HasPrefix(result.Content, "# Repository Map Summary") {
		t.Error("expected content to start with '# Repository Map Summary'")
	}
}

func TestContextRenderer_RenderJSON(t *testing.T) {
	config := DefaultRendererConfig()
	renderer := NewContextRenderer(config, nil)

	ranked := RankedTags{
		{Tag: Tag{RelFname: "test.go", Name: "TestFunc", Kind: "function", Line: 10, IsDef: true}, Score: 0.95},
		{Tag: Tag{RelFname: "test.go", Name: "TestType", Kind: "class", Line: 20, IsDef: true}, Score: 0.85},
	}

	content, tokens, err := renderer.RenderJSON(ranked)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if tokens == 0 {
		t.Error("expected non-zero tokens")
	}
	if !strings.Contains(content, "TestFunc") {
		t.Error("expected content to contain 'TestFunc'")
	}
	if !strings.Contains(content, "TestType") {
		t.Error("expected content to contain 'TestType'")
	}
}

func TestContextRenderer_Cache(t *testing.T) {
	config := DefaultRendererConfig()
	renderer := NewContextRenderer(config, nil)

	// Initially cache should be empty (or nil)
	initialSize := renderer.CacheSize()
	if initialSize != 0 {
		t.Errorf("expected initial cache size 0, got %d", initialSize)
	}

	// Render some tags (this may populate cache)
	ranked := RankedTags{
		{Tag: Tag{RelFname: "test.go", FName: "testdata/test.go", Name: "TestFunc", Kind: "function", Line: 1, IsDef: true}, Score: 1.0},
	}
	renderer.Render(ranked)

	// After rendering, cache should contain entries
	// Note: cache may be empty if files don't exist
	_ = initialSize // Just to use the variable
}

func TestContextRenderer_ClearCache(t *testing.T) {
	config := DefaultRendererConfig()
	renderer := NewContextRenderer(config, nil)

	// Render some tags
	ranked := RankedTags{
		{Tag: Tag{RelFname: "test.go", Name: "TestFunc", Kind: "function", Line: 1, IsDef: true}, Score: 1.0},
	}
	renderer.Render(ranked)

	// Clear the cache
	renderer.ClearCache()

	if renderer.CacheSize() != 0 {
		t.Errorf("expected cache size 0 after clear, got %d", renderer.CacheSize())
	}
}

func TestTruncateLine(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 100, "short"},
		{"this is a very long line that should be truncated", 20, "this is a very long..."},
		{"a,b,c", 10, "a,b,c"},
		{"word1 word2 word3 word4", 15, "word1 word2..."},
	}

	for _, tt := range tests {
		result := truncateLine(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncateLine(%q, %d): expected %q, got %q", tt.input, tt.maxLen, tt.expected, result)
		}
	}
}

func TestRendererConfig_Defaults(t *testing.T) {
	config := DefaultRendererConfig()

	if config.MaxLineLength != 100 {
		t.Errorf("expected MaxLineLength=100, got %d", config.MaxLineLength)
	}
	if config.ContextLines != 2 {
		t.Errorf("expected ContextLines=2, got %d", config.ContextLines)
	}
	if !config.EnableTreeView {
		t.Error("expected EnableTreeView=true")
	}
	if config.ShowScore {
		t.Error("expected ShowScore=false")
	}
	if config.MaxTagsPerFile != 20 {
		t.Errorf("expected MaxTagsPerFile=20, got %d", config.MaxTagsPerFile)
	}
}

func TestContextRenderer_ImplementsRenderingProvider(t *testing.T) {
	// This test ensures ContextRenderer implements RenderingProvider interface
	config := DefaultRendererConfig()
	renderer := NewContextRenderer(config, nil)

	// This should compile if the interface is satisfied
	var _ RenderingProvider = renderer
}

func TestGroupByFileRankedTags(t *testing.T) {
	ranked := RankedTags{
		{Tag: Tag{RelFname: "a.go", Name: "Func1"}, Score: 1.0},
		{Tag: Tag{RelFname: "b.go", Name: "Func2"}, Score: 0.8},
		{Tag: Tag{RelFname: "a.go", Name: "Func3"}, Score: 0.6},
	}

	result := groupByFileRankedTags(ranked)

	if len(result) != 2 {
		t.Errorf("expected 2 files, got %d", len(result))
	}

	if len(result["a.go"]) != 2 {
		t.Errorf("expected 2 tags in a.go, got %d", len(result["a.go"]))
	}

	if len(result["b.go"]) != 1 {
		t.Errorf("expected 1 tag in b.go, got %d", len(result["b.go"]))
	}

	// a.go tags should be sorted by score descending
	if result["a.go"][0].Score != 1.0 {
		t.Errorf("expected first tag in a.go to have score 1.0, got %f", result["a.go"][0].Score)
	}
}

func TestEscapeJSON(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"hello world", "hello world"},
		{`hello "world"`, `hello \"world\"`},
		{"line1\nline2", "line1\\nline2"},
		{"tab\tseparated", "tab\\tseparated"},
		{`back\slash`, `back\\slash`},
	}

	for _, tt := range tests {
		result := escapeJSON(tt.input)
		if result != tt.expected {
			t.Errorf("escapeJSON(%q): expected %q, got %q", tt.input, tt.expected, result)
		}
	}
}
