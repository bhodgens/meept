package lint

import (
	"context"
	"strings"
	"testing"
	"time"

	"log/slog"
)

func TestNewTestRunnerWithDefaults(t *testing.T) {
	logger := slog.Default()
	tr := NewTestRunner(logger)

	if tr == nil {
		t.Fatal("expected non-nil TestRunner")
	}

	if tr.config == nil {
		t.Error("expected config to be set")
	}

	if tr.config.Timeout == 0 {
		t.Error("expected default timeout to be set")
	}

	if tr.config.MaxOutputLines == 0 {
		t.Error("expected default max output lines to be set")
	}
}

func TestNewTestRunnerWithConfig(t *testing.T) {
	logger := slog.Default()
	config := &TestConfig{
		GoTestFlags:    []string{"-v"},
		Timeout:        10 * time.Second,
		MaxOutputLines: 100,
	}

	tr := NewTestRunnerWithConfig(logger, config)

	if tr.config.Timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", tr.config.Timeout)
	}

	if tr.config.MaxOutputLines != 100 {
		t.Errorf("expected max output lines 100, got %d", tr.config.MaxOutputLines)
	}
}

func TestNewTestRunnerWithNilConfig(t *testing.T) {
	logger := slog.Default()
	tr := NewTestRunnerWithConfig(logger, nil)

	if tr == nil {
		t.Fatal("expected non-nil TestRunner")
	}

	// Should have defaults
	if tr.config == nil {
		t.Error("expected config to be set")
	}
}

func TestTestRunnerHasFailures(t *testing.T) {
	tests := []struct {
		name     string
		results  []TestResult
		expected bool
	}{
		{
			name:     "all passed",
			results:  []TestResult{{Name: "test1", Passed: true}, {Name: "test2", Passed: true}},
			expected: false,
		},
		{
			name:     "one failed",
			results:  []TestResult{{Name: "test1", Passed: true}, {Name: "test2", Passed: false}},
			expected: true,
		},
		{
			name:     "all skipped",
			results:  []TestResult{{Name: "test1", Skipped: true}, {Name: "test2", Skipped: true}},
			expected: false,
		},
		{
			name:     "mixed passed and skipped",
			results:  []TestResult{{Name: "test1", Passed: true}, {Name: "test2", Skipped: true}},
			expected: false,
		},
		{
			name:     "empty results",
			results:  []TestResult{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasFailures(tt.results)
			if result != tt.expected {
				t.Errorf("expected HasFailures=%v, got %v", tt.expected, result)
			}
		})
	}
}

func TestTestRunnerFilterPassed(t *testing.T) {
	results := []TestResult{
		{Name: "test1", Passed: true},
		{Name: "test2", Passed: false},
		{Name: "test3", Passed: true},
	}

	passed := FilterPassed(results)
	if len(passed) != 2 {
		t.Errorf("expected 2 passed tests, got %d", len(passed))
	}
}

func TestTestRunnerFilterFailed(t *testing.T) {
	results := []TestResult{
		{Name: "test1", Passed: true},
		{Name: "test2", Passed: false},
		{Name: "test3", Passed: true},
	}

	failed := FilterFailed(results)
	if len(failed) != 1 {
		t.Errorf("expected 1 failed test, got %d", len(failed))
	}
}

func TestTestRunnerFilterSkipped(t *testing.T) {
	results := []TestResult{
		{Name: "test1", Skipped: true},
		{Name: "test2", Passed: false},
		{Name: "test3", Skipped: true},
	}

	skipped := FilterSkipped(results)
	if len(skipped) != 2 {
		t.Errorf("expected 2 skipped tests, got %d", len(skipped))
	}
}

func TestTestRunnerSummary(t *testing.T) {
	results := []TestResult{
		{Name: "test1", Passed: true},
		{Name: "test2", Passed: true},
		{Name: "test3", Passed: false},
		{Name: "test4", Skipped: true},
	}

	summary := Summary(results)
	expected := "tests: 2 passed, 1 failed, 1 skipped"
	if summary != expected {
		t.Errorf("expected %q, got %q", expected, summary)
	}
}

func TestTestRunnerTruncateOutput(t *testing.T) {
	logger := slog.Default()
	tr := NewTestRunner(logger)

	// Create output with multiple lines
	output := ""
	for i := 0; i < 10; i++ {
		output += "line \n"
	}

	truncated := tr.truncateOutput(output)
	lines := strings.Count(truncated, "\n")

	if lines > tr.config.MaxOutputLines {
		t.Errorf("expected output truncated to <= %d lines, got %d lines", tr.config.MaxOutputLines, lines)
	}
}

func TestTestRunnerTruncateOutputDisabled(t *testing.T) {
	logger := slog.Default()
	config := &TestConfig{
		MaxOutputLines: 0, // Disable truncation
	}
	tr := NewTestRunnerWithConfig(logger, config)

	output := "line1\nline2\nline3"
	truncated := tr.truncateOutput(output)

	if truncated != output {
		t.Error("expected no truncation when MaxOutputLines is 0")
	}
}

func TestRunGoTestsNoTestFiles(t *testing.T) {
	logger := slog.Default()
	tr := NewTestRunner(logger)

	// Use a temp dir to avoid recursive test execution
	tmpDir := t.TempDir()
	ctx := context.Background()
	results, err := tr.RunTests(ctx, "go", tmpDir, nil)

	// Should either succeed or return an error, not panic
	if err != nil {
		// Some errors are expected (e.g., no test files)
		t.Logf("RunTests returned error (expected): %v", err)
	}

	// Results may be nil when the test command fails (e.g., no test files)
	// or non-nil with empty slice — both are acceptable
	_ = results
}

func TestRunTestsInvalidLanguage(t *testing.T) {
	logger := slog.Default()
	tr := NewTestRunner(logger)

	ctx := context.Background()
	_, err := tr.RunTests(ctx, "invalid-lang", "/tmp", nil)

	if err == nil {
		t.Error("expected error for invalid language")
	}

	expectedErr := "no test runner for language: invalid-lang"
	if err.Error() != expectedErr {
		t.Errorf("expected error %q, got %q", expectedErr, err.Error())
	}
}