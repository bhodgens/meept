package repomap

import (
	"context"
	"log/slog"
	"testing"
)

func TestRepoMapGenerator_New(t *testing.T) {
	config := DefaultRepoMapConfig()
	config.Enabled = true

	generator, err := NewRepoMapGenerator(config, slog.Default(), []string{})
	if err != nil {
		t.Fatalf("failed to create generator: %v", err)
	}

	if generator == nil {
		t.Fatal("generator should not be nil")
	}

	if generator.config.Enabled != true {
		t.Error("generator should be enabled")
	}
}

func TestRepoMapGenerator_Generate_EmptyFiles(t *testing.T) {
	config := DefaultRepoMapConfig()
	config.Enabled = true

	generator, _ := NewRepoMapGenerator(config, slog.Default(), []string{})

	result, err := generator.Generate(context.Background(), []string{}, []string{})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if result != nil {
		t.Error("result should be nil when no watched files")
	}
}

func TestRepoMapGenerator_UpdateWatchedFiles(t *testing.T) {
	config := DefaultRepoMapConfig()
	generator, _ := NewRepoMapGenerator(config, slog.Default(), []string{})

	files := []string{"file1.go", "file2.go"}
	generator.UpdateWatchedFiles(files)

	// Just verify it doesn't panic
}

func TestRepoMapGenerator_InvalidateCache(t *testing.T) {
	config := DefaultRepoMapConfig()
	generator, _ := NewRepoMapGenerator(config, slog.Default(), []string{})

	// Should not panic
	generator.InvalidateCache()
}

func TestExtractIdentifiers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantMin  int
		wantMax  int
	}{
		{
			name:     "simple identifiers",
			input:    "I want to modify the handleRequest function and update the cache",
			wantMin:  2,
			wantMax:  10,
		},
		{
			name:     "no identifiers",
			input:    "the quick brown fox",
			wantMin:  0,
			wantMax:  5,
		},
		{
			name:     "code-like text",
			input:    "call the initialize method on the Configuration struct",
			wantMin:  2,
			wantMax:  10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractIdentifiers(tt.input)
			if len(result) < tt.wantMin {
				t.Errorf("ExtractIdentifiers(%q) returned %d, want at least %d", tt.input, len(result), tt.wantMin)
			}
			if len(result) > tt.wantMax {
				t.Errorf("ExtractIdentifiers(%q) returned %d, want at most %d", tt.input, len(result), tt.wantMax)
			}
		})
	}
}