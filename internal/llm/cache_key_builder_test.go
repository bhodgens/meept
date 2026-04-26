package llm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewCacheKeyBuilder(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	if b == nil {
		t.Fatal("expected non-nil builder")
	}
	if !b.FileAware {
		t.Error("expected FileAware=true")
	}
	if b.Logger == nil {
		t.Error("expected non-nil logger")
	}

	b2 := NewCacheKeyBuilder(false)
	if b2.FileAware {
		t.Error("expected FileAware=false")
	}
}

// -----------------------------------------------------------------------
// ExtractFileReferences
// -----------------------------------------------------------------------

func TestExtractFileReferences_FileAwareDisabled(t *testing.T) {
	b := NewCacheKeyBuilder(false)
	refs := b.ExtractFileReferences("see file:///path/to/file.go")
	if refs != nil {
		t.Fatalf("expected nil when file-aware disabled, got %v", refs)
	}
}

func TestExtractFileReferences_EmptyPrompt(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	refs := b.ExtractFileReferences("")
	if refs != nil {
		t.Fatalf("expected nil for empty prompt, got %v", refs)
	}
}

func TestExtractFileReferences_AbsolutePath(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	prompt := "Check /Users/caimlas/git/meept/internal/llm/client.go for issues"
	refs := b.ExtractFileReferences(prompt)
	if len(refs) != 1 {
		t.Fatalf("expected 1 reference, got %d: %v", len(refs), refs)
	}
	if refs[0] != "/Users/caimlas/git/meept/internal/llm/client.go" {
		t.Errorf("expected absolute path, got %s", refs[0])
	}
}

func TestExtractFileReferences_FilePrefix(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	prompt := "Review file: /path/to/code.go then move on"
	refs := b.ExtractFileReferences(prompt)
	if len(refs) != 1 {
		t.Fatalf("expected 1 reference, got %d: %v", len(refs), refs)
	}
	if refs[0] != "/path/to/code.go" {
		t.Errorf("expected /path/to/code.go, got %s", refs[0])
	}
}

func TestExtractFileReferences_AtNotation(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	prompt := "Use @src/main.go as entry point"
	refs := b.ExtractFileReferences(prompt)
	if len(refs) != 1 {
		t.Fatalf("expected 1 reference, got %d: %v", len(refs), refs)
	}
	if refs[0] != "src/main.go" {
		t.Errorf("expected src/main.go, got %s", refs[0])
	}
}

func TestExtractFileReferences_PathWithLine(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	prompt := "See path/to/file.go:42 for the bug"
	refs := b.ExtractFileReferences(prompt)
	if len(refs) != 1 {
		t.Fatalf("expected 1 reference, got %d: %v", len(refs), refs)
	}
	// The line number should be stripped
	if refs[0] != "path/to/file.go" {
		t.Errorf("expected path/to/file.go, got %s", refs[0])
	}
}

func TestExtractFileReferences_Deduplication(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	// Same file appears in multiple patterns -- use relative paths so
	// both patterns extract the identical string and can deduplicate.
	prompt := "Check @src/main.go and also file: src/main.go"
	refs := b.ExtractFileReferences(prompt)
	if len(refs) != 1 {
		t.Fatalf("expected 1 (deduplicated) reference, got %d: %v", len(refs), refs)
	}
}

func TestExtractFileReferences_Sorting(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	prompt := "Files: /z/file.go and /a/first.go and /m/middle.go"
	refs := b.ExtractFileReferences(prompt)
	if len(refs) != 3 {
		t.Fatalf("expected 3 references, got %d: %v", len(refs), refs)
	}
	for i := 1; i < len(refs); i++ {
		if refs[i] < refs[i-1] {
			t.Errorf("paths not sorted: %q > %q", refs[i-1], refs[i])
		}
	}
}

func TestExtractFileReferences_KnownNonFileExtensions(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	prompt := "Visit example.com and foo.org for docs"
	refs := b.ExtractFileReferences(prompt)
	if len(refs) != 0 {
		t.Fatalf("expected no file refs, got %v", refs)
	}

	// .io should be excluded
	prompt2 := "Check /tmp/test.io"
	refs2 := b.ExtractFileReferences(prompt2)
	if len(refs2) != 0 {
		t.Errorf("expected .io to be excluded, got %v", refs2)
	}

	// .txt should be excluded
	prompt3 := "Read /tmp/data.txt"
	refs3 := b.ExtractFileReferences(prompt3)
	if len(refs3) != 0 {
		t.Errorf("expected .txt to be excluded, got %v", refs3)
	}
}

func TestExtractFileReferences_MultiplePatterns(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	prompt := "@src/main.go and file: /path/lib.go and also /abs/cmd.go"
	refs := b.ExtractFileReferences(prompt)
	if len(refs) != 3 {
		t.Fatalf("expected 3 references, got %d: %v", len(refs), refs)
	}
	expected := map[string]bool{
		"src/main.go":   true,
		"/path/lib.go":  true,
		"/abs/cmd.go":   true,
	}
	for _, r := range refs {
		if !expected[r] {
			t.Errorf("unexpected reference: %s", r)
		}
	}
}

func TestExtractFileReferences_CaseInsensitiveFilePrefix(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	// The file: prefix should be case-insensitive
	prompt := "Check FILE: /path/to/upper.go"
	refs := b.ExtractFileReferences(prompt)
	if len(refs) != 1 {
		t.Fatalf("expected 1 reference for uppercase FILE:, got %d: %v", len(refs), refs)
	}
}

// -----------------------------------------------------------------------
// CleanExtractedPath (exercised through ExtractFileReferences)
// -----------------------------------------------------------------------

func TestCleanExtractedPath_TrailingPunctuation(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"file.go", "file.go"},
		{"file.go.", "file.go"},
		{"file.go,.", "file.go"},
		{"file.go;", "file.go"},
	}

	for _, tt := range tests {
		got := cleanExtractedPath(tt.input)
		if got != tt.expected {
			t.Errorf("cleanExtractedPath(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestCleanExtractedPath_TooShort(t *testing.T) {
	got := cleanExtractedPath("a")
	if got != "" {
		t.Errorf("expected empty for too-short path, got %q", got)
	}

	got2 := cleanExtractedPath("ab")
	if got2 != "" {
		t.Errorf("expected empty for 2-char path, got %q", got2)
	}
}

// -----------------------------------------------------------------------
// isKnownNonFile
// -----------------------------------------------------------------------

func TestIsKnownNonFile(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	// Directly test through ExtractFileReferences since isKnownNonFile is unexported.
	for _, ext := range []string{".com", ".org", ".net", ".io", ".dev", ".ai", ".co", ".uk", ".gov", ".edu", ".mil", ".us", ".eu", ".tv", ".me", ".app", ".txt"} {
		path := "/tmp/test" + ext
		refs := b.ExtractFileReferences("see " + path)
		if len(refs) != 0 {
			t.Errorf("expected %s to be excluded, got %v", ext, refs)
		}
	}
}

func TestIsKnownNonFile_ValidExtensions(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	gotFiles := []string{}
	for _, ext := range []string{".go", ".rs", ".py", ".js", ".ts", ".tsx", ".java", ".cpp", ".c", ".h", ".rb", ".php"} {
		path := "/tmp/test" + ext
		refs := b.ExtractFileReferences("see " + path)
		if len(refs) == 1 && refs[0] == "/tmp/test"+ext {
			gotFiles = append(gotFiles, ext)
		}
	}
	if len(gotFiles) == 0 {
		t.Error("at least some valid extensions should be matched")
	}
}

// -----------------------------------------------------------------------
// ComputeFileHashes
// -----------------------------------------------------------------------

func TestComputeFileHashes(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	tmpDir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(tmpDir, "test.go")
	content := []byte("package test\n\nfunc Main() {}")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	hashes := b.ComputeFileHashes([]string{testFile})
	if len(hashes) != 1 {
		t.Fatalf("expected 1 hash, got %d", len(hashes))
	}
	if hashes[testFile] == "" {
		t.Error("expected non-empty hash")
	}
}

func TestComputeFileHashes_MissingFile(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	// Non-existent file should be silently skipped
	hashes := b.ComputeFileHashes([]string{"/nonexistent/path/file.go"})
	if len(hashes) != 0 {
		t.Fatalf("expected 0 hashes for missing file, got %d", len(hashes))
	}
}

func TestComputeFileHashes_EmptyInput(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	hashes := b.ComputeFileHashes([]string{})
	if len(hashes) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(hashes))
	}
}

// -----------------------------------------------------------------------
// isAllDigits
// -----------------------------------------------------------------------

func TestIsAllDigits(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"12345", true},
		{"0", true},
		{"12a5", false},
		{"", false},
		{"abc", false},
	}
	for _, tt := range tests {
		got := isAllDigits(tt.input)
		if got != tt.want {
			t.Errorf("isAllDigits(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// -----------------------------------------------------------------------
// ComputePromptHash
// -----------------------------------------------------------------------

func TestComputePromptHash_Deterministic(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	messages := []ChatMessage{
		{Role: RoleUser, Content: "hello", Name: "", ToolCallID: ""},
		{Role: RoleAssistant, Content: "hi there", Name: "", ToolCallID: ""},
	}

	hash1 := b.ComputePromptHash(messages)
	hash2 := b.ComputePromptHash(messages)

	if hash1 != hash2 {
		t.Fatal("same messages should produce same hash")
	}
	if hash1 == "" {
		t.Error("expected non-empty hash")
	}
}

func TestComputePromptHash_DifferentContent(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	messages1 := []ChatMessage{{Role: RoleUser, Content: "hello"}}
	messages2 := []ChatMessage{{Role: RoleUser, Content: "goodbye"}}

	if b.ComputePromptHash(messages1) == b.ComputePromptHash(messages2) {
		t.Error("different content should produce different hashes")
	}
}

func TestComputePromptHash_DifferentRole(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	messages1 := []ChatMessage{{Role: RoleUser, Content: "hello"}}
	messages2 := []ChatMessage{{Role: RoleAssistant, Content: "hello"}}

	if b.ComputePromptHash(messages1) == b.ComputePromptHash(messages2) {
		t.Error("different roles should produce different hashes")
	}
}

func TestComputePromptHash_WithToolCalls(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	messages := []ChatMessage{
		{
			Role: RoleAssistant,
			Content: "",
			ToolCalls: []ToolCall{
				{
					ID:     "call_1",
					Type:   "function",
					Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.go"}`},
				},
			},
		},
	}

	hash := b.ComputePromptHash(messages)
	if hash == "" {
		t.Error("expected non-empty hash with tool calls")
	}
}

// -----------------------------------------------------------------------
// Build
// -----------------------------------------------------------------------

func TestBuild_FileAwareDisabled(t *testing.T) {
	b := NewCacheKeyBuilder(false)
	key := b.Build("check file.go", "model-1", []ChatMessage{
		{Role: RoleUser, Content: "look at file.go"},
	})

	if key.ModelID != "model-1" {
		t.Errorf("expected model-1, got %s", key.ModelID)
	}
	if key.PromptHash == "" {
		t.Error("expected non-empty prompt hash")
	}
	if key.FileHashes != nil {
		t.Errorf("expected nil FileHashes when file-aware disabled, got %v", key.FileHashes)
	}
}

func TestBuild_FileAwareEnabled_NoFiles(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	key := b.Build("just some text", "model-2", []ChatMessage{
		{Role: RoleUser, Content: "no file references here"},
	})

	if key.FileHashes != nil {
		t.Errorf("expected nil FileHashes when no files found, got %v", key.FileHashes)
	}
}

func TestBuild_FileAwareEnabled_WithFiles(t *testing.T) {
	b := NewCacheKeyBuilder(true)
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(testFile, []byte("package main"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	prompt := "Check " + testFile + " for bugs"
	messages := []ChatMessage{
		{Role: RoleUser, Content: ""},
	}

	key := b.Build(prompt, "model-3", messages)
	if key.FileHashes == nil {
		t.Fatal("expected FileHashes when file-aware enabled")
	}
	if _, ok := key.FileHashes[testFile]; !ok {
		t.Errorf("expected hash for %s, got %v", testFile, key.FileHashes)
	}
}

func TestBuild_EmptyMessages(t *testing.T) {
	b := NewCacheKeyBuilder(false)
	key := b.Build("", "model-4", nil)
	if key.PromptHash == "" {
		t.Error("expected non-empty hash for empty messages")
	}
}
