package llm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractFileReferences_VariousFormats(t *testing.T) {
	builder := NewCacheKeyBuilder(true)

	tests := []struct {
		name     string
		prompt   string
		expected []string
	}{
		{
			name:     "file: prefix absolute path",
			prompt:   "Check file: /Users/test/project/main.go for issues",
			expected: []string{"/Users/test/project/main.go"},
		},
		{
			name:     "file: prefix relative path",
			prompt:   "Read file: src/utils.go please",
			expected: []string{"src/utils.go"},
		},
		{
			name:     "@ notation",
			prompt:   "Look at @src/main.go and @lib/helper.ts",
			expected: []string{"lib/helper.ts", "src/main.go"},
		},
		{
			name:     "path with line number uses rePathWithLine",
			prompt:   "Error at Users/name/project/file.go:42 on this line",
			expected: []string{"Users/name/project/file.go"},
		},
		{
			name:     "relative path with line number",
			prompt:   " internal/pkg/service.go:123 has a bug",
			expected: []string{"internal/pkg/service.go"},
		},
		{
			name:     "bare absolute path",
			prompt:   "The file /Users/dev/code/app.ts needs changes",
			expected: []string{"/Users/dev/code/app.ts"},
		},
		{
			name:     "multiple references",
			prompt:   "Compare file: /path/one.go with @path/two.go and check path/three.go:10",
			expected: []string{"/path/one.go", "path/three.go", "path/two.go"},
		},
		{
			name:     "empty prompt",
			prompt:   "",
			expected: nil,
		},
		{
			name:     "no file references",
			prompt:   "This is just a plain question with no files",
			expected: nil,
		},
		{
			name:     "deduplication",
			prompt:   "Check file: /path/file.go and also @path/file.go for issues",
			expected: []string{"/path/file.go", "path/file.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := builder.ExtractFileReferences(tt.prompt)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d paths, got %d: %v", len(tt.expected), len(result), result)
				return
			}
			for i, path := range result {
				if path != tt.expected[i] {
					t.Errorf("path[%d] = %q, want %q", i, path, tt.expected[i])
				}
			}
		})
	}
}

func TestExtractFileReferences_FiltersTLDExtensions(t *testing.T) {
	builder := NewCacheKeyBuilder(true)

	tests := []struct {
		name          string
		prompt        string
		shouldInclude bool
	}{
		{
			name:          ".com is filtered",
			prompt:        "Visit example.com for more info",
			shouldInclude: false,
		},
		{
			name:          ".io is filtered",
			prompt:        "Check github.io docs",
			shouldInclude: false,
		},
		{
			name:          ".org is filtered",
			prompt:        "See golang.org/pkg/os",
			shouldInclude: false,
		},
		{
			name:          ".go is included",
			prompt:        "Check @path/to/main.go",
			shouldInclude: true,
		},
		{
			name:          ".ts is included",
			prompt:        "Look at @src/component.ts",
			shouldInclude: true,
		},
		{
			name:          ".py is included",
			prompt:        "file: scripts/test.py",
			shouldInclude: true,
		},
		{
			name:          ".js is included",
			prompt:        "Review @lib/utils.js code",
			shouldInclude: true,
		},
		{
			name:          ".rs is included",
			prompt:        "file: src/main.rs",
			shouldInclude: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := builder.ExtractFileReferences(tt.prompt)
			hasResult := len(result) > 0
			if hasResult != tt.shouldInclude {
				t.Errorf("shouldInclude = %v, got results: %v", tt.shouldInclude, result)
			}
		})
	}
}

func TestExtractFileReferences_FileAwareDisabled(t *testing.T) {
	builder := NewCacheKeyBuilder(false)

	result := builder.ExtractFileReferences("Check file: /path/to/main.go")
	if result != nil {
		t.Errorf("expected nil when FileAware is false, got %v", result)
	}
}

func TestCleanExtractedPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strips trailing punctuation",
			input:    "file.go.",
			expected: "file.go",
		},
		{
			name:     "strips trailing colon and digits",
			input:    "path/to/file.go:42",
			expected: "path/to/file.go",
		},
		{
			name:     "strips multiple trailing punctuation",
			input:    "file.go...,",
			expected: "file.go",
		},
		{
			name:     "trims whitespace",
			input:    "  file.go  ",
			expected: "file.go",
		},
		{
			name:     "returns empty for too short",
			input:    "a",
			expected: "",
		},
		{
			name:     "keeps valid path",
			input:    "/path/to/file.go",
			expected: "/path/to/file.go",
		},
		{
			name:     "handles complex line reference",
			input:    "path/file.go:123:45",
			expected: "path/file.go:123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanExtractedPath(tt.input)
			if result != tt.expected {
				t.Errorf("cleanExtractedPath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsAllDigits(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"123", true},
		{"0", true},
		{"999999", true},
		{"", false},
		{"12a3", false},
		{"abc", false},
		{" 123", false},
		{"123 ", false},
		{"-1", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isAllDigits(tt.input)
			if result != tt.expected {
				t.Errorf("isAllDigits(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsKnownNonFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"example.com", true},
		{"github.io", true},
		{"golang.org", true},
		{"site.net", true},
		{"main.go", false},
		{"app.ts", false},
		{"script.py", false},
		{"noextension", false},
		{"file.json", false},
		{"config.yaml", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := isKnownNonFile(tt.path)
			if result != tt.expected {
				t.Errorf("isKnownNonFile(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestComputeFileHashes_ExistingFiles(t *testing.T) {
	builder := NewCacheKeyBuilder(true)
	tmpDir := t.TempDir()

	// Create test files
	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")

	//nolint:gosec // test directory/file
	if err := os.WriteFile(file1, []byte("content1"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	//nolint:gosec // test directory/file
	if err := os.WriteFile(file2, []byte("content2"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	paths := []string{file1, file2}
	hashes := builder.ComputeFileHashes(paths)

	if len(hashes) != 2 {
		t.Fatalf("expected 2 hashes, got %d", len(hashes))
	}

	// Verify hashes exist and are non-empty
	for _, path := range paths {
		hash, ok := hashes[path]
		if !ok {
			t.Errorf("missing hash for %s", path)
			continue
		}
		if hash == "" {
			t.Errorf("empty hash for %s", path)
		}
		// SHA256 hex string should be 64 characters
		if len(hash) != 64 {
			t.Errorf("hash length for %s = %d, want 64", path, len(hash))
		}
	}

	// Different content should produce different hashes
	if hashes[file1] == hashes[file2] {
		t.Error("different files should have different hashes")
	}
}

func TestComputeFileHashes_MissingFiles(t *testing.T) {
	builder := NewCacheKeyBuilder(true)

	paths := []string{"/nonexistent/file1.go", "/nonexistent/file2.go"}
	hashes := builder.ComputeFileHashes(paths)

	// Should return empty map (files skipped)
	if len(hashes) != 0 {
		t.Errorf("expected 0 hashes for missing files, got %d", len(hashes))
	}

	// Map should never be nil
	if hashes == nil {
		t.Error("hashes should not be nil")
	}
}

func TestComputeFileHashes_MixedExisting(t *testing.T) {
	builder := NewCacheKeyBuilder(true)
	tmpDir := t.TempDir()

	existingFile := filepath.Join(tmpDir, "exists.txt")
	//nolint:gosec // test directory/file
	if err := os.WriteFile(existingFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	paths := []string{existingFile, "/nonexistent/missing.go"}
	hashes := builder.ComputeFileHashes(paths)

	// Only the existing file should have a hash
	if len(hashes) != 1 {
		t.Errorf("expected 1 hash, got %d", len(hashes))
	}
	if _, ok := hashes[existingFile]; !ok {
		t.Error("expected hash for existing file")
	}
}

func TestComputePromptHash_EmptyMessages(t *testing.T) {
	builder := NewCacheKeyBuilder(true)

	// Empty slice
	hash1 := builder.ComputePromptHash([]ChatMessage{})
	if hash1 == "" {
		t.Error("hash should not be empty for empty slice")
	}

	// Nil slice
	hash2 := builder.ComputePromptHash(nil)
	if hash2 == "" {
		t.Error("hash should not be empty for nil slice")
	}

	// Both should produce the same hash
	if hash1 != hash2 {
		t.Error("empty and nil slices should produce same hash")
	}
}

func TestComputePromptHash_DeterministicOrdering(t *testing.T) {
	builder := NewCacheKeyBuilder(true)

	messages := []ChatMessage{
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleAssistant, Content: "Hi there"},
		{Role: RoleUser, Content: "How are you?"},
	}

	hash1 := builder.ComputePromptHash(messages)
	hash2 := builder.ComputePromptHash(messages)

	if hash1 != hash2 {
		t.Error("same messages should produce same hash")
	}

	// Different order should produce different hash
	reversedMessages := []ChatMessage{
		{Role: RoleUser, Content: "How are you?"},
		{Role: RoleAssistant, Content: "Hi there"},
		{Role: RoleUser, Content: "Hello"},
	}

	hash3 := builder.ComputePromptHash(reversedMessages)
	if hash1 == hash3 {
		t.Error("different message order should produce different hash")
	}
}

func TestComputePromptHash_IncludesAllFields(t *testing.T) {
	builder := NewCacheKeyBuilder(true)

	baseMsg := []ChatMessage{{Role: RoleUser, Content: "test"}}
	baseHash := builder.ComputePromptHash(baseMsg)

	// Different role
	diffRole := []ChatMessage{{Role: RoleAssistant, Content: "test"}}
	if builder.ComputePromptHash(diffRole) == baseHash {
		t.Error("different role should produce different hash")
	}

	// Different content
	diffContent := []ChatMessage{{Role: RoleUser, Content: "test2"}}
	if builder.ComputePromptHash(diffContent) == baseHash {
		t.Error("different content should produce different hash")
	}

	// Different name
	diffName := []ChatMessage{{Role: RoleUser, Content: "test", Name: "user1"}}
	if builder.ComputePromptHash(diffName) == baseHash {
		t.Error("different name should produce different hash")
	}

	// Different tool call ID
	diffToolCallID := []ChatMessage{{Role: RoleUser, Content: "test", ToolCallID: "call_123"}}
	if builder.ComputePromptHash(diffToolCallID) == baseHash {
		t.Error("different tool call ID should produce different hash")
	}

	// Different tool calls
	diffToolCalls := []ChatMessage{{
		Role:    RoleUser,
		Content: "test",
		ToolCalls: []ToolCall{{
			ID:   "tc1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "get_weather",
				Arguments: `{"city":"NYC"}`,
			},
		}},
	}}
	if builder.ComputePromptHash(diffToolCalls) == baseHash {
		t.Error("different tool calls should produce different hash")
	}
}

func TestBuild_FullKeyConstruction(t *testing.T) {
	builder := NewCacheKeyBuilder(true)
	tmpDir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(tmpDir, "test.go")
	//nolint:gosec // test directory/file
	if err := os.WriteFile(testFile, []byte("package main"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	prompt := "Check file: " + testFile
	modelID := "test-model"
	messages := []ChatMessage{
		{Role: RoleUser, Content: "What's in this file?"},
	}

	key := builder.Build(prompt, modelID, messages)

	// Verify model ID
	if key.ModelID != modelID {
		t.Errorf("ModelID = %q, want %q", key.ModelID, modelID)
	}

	// Verify prompt hash is set
	if key.PromptHash == "" {
		t.Error("PromptHash should not be empty")
	}

	// Verify file hashes are populated
	if len(key.FileHashes) != 1 {
		t.Errorf("expected 1 file hash, got %d", len(key.FileHashes))
	}
	if _, ok := key.FileHashes[testFile]; !ok {
		t.Error("expected hash for test file")
	}
}

func TestBuild_FileAwareDisabled(t *testing.T) {
	builder := NewCacheKeyBuilder(false)
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "test.go")
	//nolint:gosec // test directory/file
	if err := os.WriteFile(testFile, []byte("package main"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	prompt := "Check file: " + testFile
	key := builder.Build(prompt, "model", nil)

	// File hashes should not be populated when FileAware is false
	if key.FileHashes != nil {
		t.Errorf("FileHashes should be nil when FileAware is false, got %v", key.FileHashes)
	}
}

func TestBuild_ExtractsFromMessages(t *testing.T) {
	builder := NewCacheKeyBuilder(true)
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "message_file.go")
	//nolint:gosec // test directory/file
	if err := os.WriteFile(testFile, []byte("package test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// File reference in message content, not prompt
	messages := []ChatMessage{
		{Role: RoleUser, Content: "Check file: " + testFile},
	}

	key := builder.Build("", "model", messages)

	// Should find file from message content
	if len(key.FileHashes) != 1 {
		t.Errorf("expected 1 file hash from message, got %d", len(key.FileHashes))
	}
	if _, ok := key.FileHashes[testFile]; !ok {
		t.Error("expected hash for file referenced in message")
	}
}

func TestNewCacheKeyBuilder(t *testing.T) {
	builder := NewCacheKeyBuilder(true)
	if builder == nil {
		t.Fatal("NewCacheKeyBuilder returned nil")
	}
	if !builder.FileAware {
		t.Error("FileAware should be true")
	}
	if builder.Logger == nil {
		t.Error("Logger should not be nil")
	}

	builder2 := NewCacheKeyBuilder(false)
	if builder2.FileAware {
		t.Error("FileAware should be false")
	}
}
