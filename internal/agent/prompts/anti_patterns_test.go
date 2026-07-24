package prompts

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBaselineIncludesAntiPatterns(t *testing.T) {
	prompt := BuildBaselinePrompt()
	if !strings.Contains(prompt, BaselineAntiPatterns) {
		t.Error("BuildBaselinePrompt() does not include BaselineAntiPatterns")
	}
}

func TestUniversalAntiPatternsContent(t *testing.T) {
	tests := []struct {
		name     string
		contains string
	}{
		{"premature abstraction", "premature abstraction"},
		{"false completion", "false completion"},
		{"over-engineering", "over-engineering"},
		{"unnecessary artifacts", "unnecessary artifacts"},
		{"process sections", "process sections"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lower := strings.ToLower(BaselineAntiPatterns)
			if !strings.Contains(lower, tt.contains) {
				t.Errorf("BaselineAntiPatterns missing %q", tt.contains)
			}
		})
	}
}

func conditionalPromptPath(filename string) string {
	_, currentFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(currentFile), "..", "..", "..")
	return filepath.Join(repoRoot, "config", "prompts", "conditional", filename)
}

func TestCodeAntiPatternsFileExists(t *testing.T) {
	path := conditionalPromptPath("anti_patterns_code.md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("conditional prompt file does not exist: %s", path)
	}
}

func TestPlanAntiPatternsFileExists(t *testing.T) {
	path := conditionalPromptPath("anti_patterns_plan.md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("conditional prompt file does not exist: %s", path)
	}
}

func TestDebugAntiPatternsFileExists(t *testing.T) {
	path := conditionalPromptPath("anti_patterns_debug.md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("conditional prompt file does not exist: %s", path)
	}
}

func TestAnalysisAntiPatternsFileExists(t *testing.T) {
	path := conditionalPromptPath("anti_patterns_analysis.md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("conditional prompt file does not exist: %s", path)
	}
}
