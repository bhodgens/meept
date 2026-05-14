package shadow

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

func TestNewManager_Disabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false

	mgr, err := NewManager(ManagerConfig{
		Config: cfg,
		Logger: slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	if mgr.IsEnabled() {
		t.Error("Manager should not be enabled when config.Enabled is false")
	}
}

func TestNewManager_EnabledWithoutTeacher(t *testing.T) {
	// Create a temp directory for the test
	tmpDir := t.TempDir()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Teacher.Model = "" // No teacher model
	cfg.DataDir = tmpDir

	mgr, err := NewManager(ManagerConfig{
		Config: cfg,
		Logger: slog.Default(),
	})
	if err != nil {
		// Skip if FTS5 is not available
		if containsFTS5(err.Error()) {
			t.Skip("SQLite FTS5 module not available")
		}
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	// IsEnabled should be false when teacher model is not configured
	if mgr.IsEnabled() {
		t.Error("Manager should not be enabled when teacher model is not set")
	}
}

// containsFTS5 is a helper for error message checking
func containsFTS5(s string) bool {
	const substr = "fts5"
	return len(s) >= len(substr) && (s == substr || s != "" && containsStringHelper(s, substr))
}

func containsStringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestNewManager_EnabledWithTeacher(t *testing.T) {
	// Create a temp directory for the test
	tmpDir := t.TempDir()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Teacher.Model = "test-model"
	cfg.DataDir = tmpDir

	mgr, err := NewManager(ManagerConfig{
		Config: cfg,
		Logger: slog.Default(),
	})
	if err != nil {
		// Skip if FTS5 is not available
		if containsFTS5(err.Error()) {
			t.Skip("SQLite FTS5 module not available")
		}
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	if !mgr.IsEnabled() {
		t.Error("Manager should be enabled when config.Enabled is true and teacher model is set")
	}
}

func TestManager_CaptureInteraction_Disabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false

	mgr, err := NewManager(ManagerConfig{
		Config: cfg,
		Logger: slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	// Should not panic when disabled
	ctx := context.Background()
	messages := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "Hello"},
	}
	response := &llm.Response{
		Content: "Hi there!",
		Usage: llm.TokenUsage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	// This should be a no-op when disabled
	mgr.CaptureInteraction(ctx, "conv-1", messages, response, "test-model")
}

func TestManager_CaptureToolInteraction_Disabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false

	mgr, err := NewManager(ManagerConfig{
		Config: cfg,
		Logger: slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	// Should not panic when disabled
	ctx := context.Background()
	messages := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "Read the file"},
	}
	response := &llm.Response{
		Content: "",
		ToolCalls: []llm.ToolCall{
			{
				ID:   "call-1",
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "read_file",
					Arguments: `{"path": "/tmp/test.txt"}`,
				},
			},
		},
		Usage: llm.TokenUsage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	// This should be a no-op when disabled
	mgr.CaptureToolInteraction(ctx, "conv-1", messages, response, "test-model")
}

func TestManager_GetFewShotExamples_Disabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false

	mgr, err := NewManager(ManagerConfig{
		Config: cfg,
		Logger: slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	ctx := context.Background()
	examples, err := mgr.GetFewShotExamples(ctx, DomainCode, TaskTypeChat, "test query", 3)
	if err != nil {
		t.Fatalf("GetFewShotExamples failed: %v", err)
	}
	if len(examples) != 0 {
		t.Errorf("Expected 0 examples when disabled, got %d", len(examples))
	}
}

func TestManager_FormatExamplesForInjection_Empty(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false

	mgr, err := NewManager(ManagerConfig{
		Config: cfg,
		Logger: slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	// Should return nil for empty examples when selector is nil
	messages := mgr.FormatExamplesForInjection([]*FewShotExample{})
	if messages != nil {
		t.Error("Expected nil for empty examples when selector is nil")
	}
}

func TestManager_GetStats_Disabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false

	mgr, err := NewManager(ManagerConfig{
		Config: cfg,
		Logger: slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	ctx := context.Background()
	stats, err := mgr.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if stats == nil {
		t.Error("Expected non-nil stats even when disabled")
	}
}

func TestManager_ProcessRecord(t *testing.T) {
	// Create a temp directory for the test
	tmpDir := t.TempDir()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Teacher.Model = "test-model"
	cfg.DataDir = tmpDir
	cfg.Quality.Method = MethodHeuristic // Use heuristic to avoid needing a real LLM

	mgr, err := NewManager(ManagerConfig{
		Config: cfg,
		Logger: slog.Default(),
	})
	if err != nil {
		// Skip if FTS5 is not available
		if containsFTS5(err.Error()) {
			t.Skip("SQLite FTS5 module not available")
		}
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	// Create a test record
	messages := []Message{
		{Role: "user", Content: "Write a function to add two numbers"},
	}
	record := NewShadowRecord("conv-1", messages, "student-model", "def add(a, b):\n    return a + b")
	record.Domain = DomainCode
	record.TaskType = TaskTypeChat

	ctx := context.Background()
	err = mgr.ProcessRecord(ctx, record)
	if err != nil {
		t.Fatalf("ProcessRecord failed: %v", err)
	}

	// Verify record was scored
	if record.QualityScore == 0 {
		t.Error("Expected non-zero quality score")
	}
}

func TestManager_Config(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Teacher.Model = "test-model"
	cfg.DataDir = tmpDir

	mgr, err := NewManager(ManagerConfig{
		Config: cfg,
		Logger: slog.Default(),
	})
	if err != nil {
		// Skip if FTS5 is not available
		if containsFTS5(err.Error()) {
			t.Skip("SQLite FTS5 module not available")
		}
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	if mgr.Config() != cfg {
		t.Error("Config() should return the configuration")
	}
}

func TestManager_Close(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Teacher.Model = "test-model"
	cfg.DataDir = tmpDir

	mgr, err := NewManager(ManagerConfig{
		Config: cfg,
		Logger: slog.Default(),
	})
	if err != nil {
		// Skip if FTS5 is not available
		if containsFTS5(err.Error()) {
			t.Skip("SQLite FTS5 module not available")
		}
		t.Fatalf("NewManager failed: %v", err)
	}

	// Close should not error
	if err := mgr.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

// TestExpandPath tests the path expansion helper
func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("Cannot get user home dir")
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"~/.meept", home + "/.meept"},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}

	for _, tc := range tests {
		result := expandPath(tc.input)
		if result != tc.expected {
			t.Errorf("expandPath(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}

	// Test that ~/ expands to home directory (with or without trailing slash)
	result := expandPath("~/")
	if result != home && result != home+"/" {
		t.Errorf("expandPath(\"~/\") = %q, want %q or %q", result, home, home+"/")
	}
}
