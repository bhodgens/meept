package prompt

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repoRoot returns the repository root directory.
func repoRoot() string {
	_, currentFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(currentFile), "..", "..", "..")
}

// testLoader creates a Loader pointing at the repo's config/prompts directory.
func testLoader() *Loader {
	return NewLoader([]string{filepath.Join(repoRoot(), "config", "prompts")})
}

func TestCodeAntiPatternsLoaded(t *testing.T) {
	loader := testLoader()
	builder := NewBuilder(loader)

	ctx := NewPromptContext().WithCondition("has_code_task", true)
	result, err := builder.Build([]string{"conditional.anti_patterns_code"}, ctx)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if !strings.Contains(result, "Code-Specific Anti-Patterns") && !strings.Contains(result, "Comments") {
		t.Error("code anti-patterns component not loaded with has_code_task=true")
	}
}

func TestPlanAntiPatternsLoaded(t *testing.T) {
	loader := testLoader()
	builder := NewBuilder(loader)

	ctx := NewPromptContext().WithCondition("has_plan_task", true)
	result, err := builder.Build([]string{"conditional.anti_patterns_plan"}, ctx)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if !strings.Contains(result, "Planning Anti-Patterns") && !strings.Contains(result, "Scope Creep") {
		t.Error("plan anti-patterns component not loaded with has_plan_task=true")
	}
}

func TestDebugAntiPatternsLoaded(t *testing.T) {
	loader := testLoader()
	builder := NewBuilder(loader)

	ctx := NewPromptContext().WithCondition("has_debug_task", true)
	result, err := builder.Build([]string{"conditional.anti_patterns_debug"}, ctx)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if !strings.Contains(result, "Debugging Anti-Patterns") && !strings.Contains(result, "Shotgun Debugging") {
		t.Error("debug anti-patterns component not loaded with has_debug_task=true")
	}
}

func TestAnalysisAntiPatternsLoaded(t *testing.T) {
	loader := testLoader()
	builder := NewBuilder(loader)

	ctx := NewPromptContext().WithCondition("has_analysis_task", true)
	result, err := builder.Build([]string{"conditional.anti_patterns_analysis"}, ctx)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if !strings.Contains(result, "Analysis Anti-Patterns") && !strings.Contains(result, "Hedging") {
		t.Error("analysis anti-patterns component not loaded with has_analysis_task=true")
	}
}

func TestNoCrossContamination(t *testing.T) {
	loader := testLoader()
	builder := NewBuilder(loader)

	// Coder prompt: has_code_task=true, all others false
	ctx := NewPromptContext().
		WithCondition("has_code_task", true).
		WithCondition("has_plan_task", false).
		WithCondition("has_debug_task", false).
		WithCondition("has_analysis_task", false)

	result, err := builder.Build([]string{
		"conditional.anti_patterns_code",
		"conditional.anti_patterns_plan",
		"conditional.anti_patterns_debug",
		"conditional.anti_patterns_analysis",
	}, ctx)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Should contain code anti-patterns
	if !strings.Contains(result, "Comments") && !strings.Contains(result, "Code-Specific") {
		t.Error("coder prompt missing code anti-patterns")
	}

	// Should NOT contain plan/debug/analysis anti-patterns
	if strings.Contains(result, "Scope Creep") || strings.Contains(result, "Planning Anti-Patterns") {
		t.Error("coder prompt contains plan anti-patterns (cross-contamination)")
	}
	if strings.Contains(result, "Shotgun Debugging") || strings.Contains(result, "Debugging Anti-Patterns") {
		t.Error("coder prompt contains debug anti-patterns (cross-contamination)")
	}
	if strings.Contains(result, "Hedging") || strings.Contains(result, "Analysis Anti-Patterns") {
		t.Error("coder prompt contains analysis anti-patterns (cross-contamination)")
	}
}

func TestConditionalNotLoadedWithoutFlag(t *testing.T) {
	loader := testLoader()
	builder := NewBuilder(loader)

	// No conditions set — conditional components should be excluded
	ctx := NewPromptContext()
	result, err := builder.Build([]string{
		"conditional.anti_patterns_code",
		"conditional.anti_patterns_plan",
	}, ctx)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if strings.Contains(result, "Code-Specific") || strings.Contains(result, "Planning Anti-Patterns") {
		t.Error("conditional components loaded without flags set")
	}
}

func TestConditionalFilesExist(t *testing.T) {
	promptsDir := filepath.Join(repoRoot(), "config", "prompts", "conditional")
	files := []string{
		"anti_patterns_code.md",
		"anti_patterns_plan.md",
		"anti_patterns_debug.md",
		"anti_patterns_analysis.md",
	}
	for _, f := range files {
		path := filepath.Join(promptsDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("conditional prompt file missing: %s", path)
		}
	}
}
