package lint

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"log/slog"
)

// TestResult represents a single test execution result
type TestResult struct {
	Name     string        `json:"name"`
	File     string        `json:"file,omitempty"`
	Line     int           `json:"line,omitempty"`
	Passed   bool          `json:"passed"`
	Skipped  bool          `json:"skipped,omitempty"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration"`
	Output   string        `json:"output,omitempty"`
}

// TestRunner executes language-specific test commands
type TestRunner struct {
	config *TestConfig
	logger *slog.Logger
}

// TestConfig holds test runner configuration
type TestConfig struct {
	GoTestFlags     []string // e.g., ["-race", "-count=1"]
	PytestFlags     []string // e.g., ["-x", "-v"]
	JestFlags       []string // e.g., ["--passWithNoTests"]
	Timeout         time.Duration
	MaxOutputLines  int // Truncate output to N lines
	CustomGoCmd     string
	CustomPytestCmd string
	CustomJestCmd   string
	BaseDir         string
}

// NewTestRunner creates a new TestRunner with default configuration
func NewTestRunner(logger *slog.Logger) *TestRunner {
	return NewTestRunnerWithConfig(logger, &TestConfig{
		GoTestFlags:     []string{"-race", "-count=1"},
		PytestFlags:     []string{"-x", "-v"},
		JestFlags:       []string{"--passWithNoTests"},
		Timeout:         5 * time.Minute,
		MaxOutputLines:  500,
		CustomGoCmd:     "",
		CustomPytestCmd: "",
		CustomJestCmd:   "",
	})
}

// NewTestRunnerWithConfig creates a new TestRunner with custom configuration
func NewTestRunnerWithConfig(logger *slog.Logger, config *TestConfig) *TestRunner {
	if config == nil {
		config = &TestConfig{}
	}
	// Set defaults
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Minute
	}
	if config.MaxOutputLines == 0 {
		config.MaxOutputLines = 500
	}
	if len(config.GoTestFlags) == 0 {
		config.GoTestFlags = []string{"-race", "-count=1"}
	}
	if len(config.PytestFlags) == 0 {
		config.PytestFlags = []string{"-x", "-v"}
	}
	if len(config.JestFlags) == 0 {
		config.JestFlags = []string{"--passWithNoTests"}
	}

	return &TestRunner{
		config: config,
		logger: logger,
	}
}

// RunTests executes tests for the given files or directory
func (tr *TestRunner) RunTests(ctx context.Context, lang, dirPath string, testFiles []string) ([]TestResult, error) {
	// Apply timeout if not set
	if ctx.Err() == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, tr.config.Timeout)
		defer cancel()
	}

	switch lang {
	case "go":
		return tr.runGoTests(ctx, dirPath, testFiles)
	case "python":
		return tr.runPytestTests(ctx, dirPath, testFiles)
	case "javascript", "typescript":
		return tr.runJestTests(ctx, dirPath, testFiles)
	default:
		return nil, fmt.Errorf("no test runner for language: %s", lang)
	}
}

// runGoTests runs Go tests with JSON output parsing
func (tr *TestRunner) runGoTests(ctx context.Context, dirPath string, testFiles []string) ([]TestResult, error) {
	goCmd := "go"
	if tr.config.CustomGoCmd != "" {
		goCmd = tr.config.CustomGoCmd
	}

	args := []string{"test", "-json"}
	args = append(args, tr.config.GoTestFlags...)

	// Determine which packages to test
	if len(testFiles) > 0 {
		for _, f := range testFiles {
			if strings.HasSuffix(f, "_test.go") {
				pkg := extractGoPackage(f, dirPath)
				if pkg != "" {
					args = append(args, pkg)
				}
			}
		}
	}

	if len(testFiles) == 0 || len(args) <= 2 {
		args = append(args, "./...")
	}

	tr.logger.Debug("running go tests", "cmd", goCmd, "args", args, "dir", dirPath)

	cmd := exec.CommandContext(ctx, goCmd, args...)
	if dirPath != "" {
		cmd.Dir = dirPath
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start go test: %w", err)
	}

	// Parse JSON test output
	decoder := json.NewDecoder(stdout)
	var results []TestResult
	testOutput := make(map[string]*strings.Builder) // Aggregate output per test

	for {
		var event GoTestEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			tr.logger.Debug("error decoding test event", "error", err)
			break
		}

		// Aggregate output
		if event.Action == "output" && event.Test != "" {
			key := event.Package + "/" + event.Test
			if _, ok := testOutput[key]; !ok {
				testOutput[key] = &strings.Builder{}
			}
			testOutput[key].WriteString(event.Output)
		}

		// Capture final results
		if event.Action == "pass" || event.Action == "fail" || event.Action == "skip" {
			if event.Test == "" {
				continue // Package-level event
			}

			output := ""
			if outputBuilder, ok := testOutput[event.Package+"/"+event.Test]; ok {
				output = outputBuilder.String()
			}

			result := TestResult{
				Name:    event.Test,
				File:    findGoTestFile(event.Package, event.Test),
				Passed:  event.Action == "pass",
				Skipped: event.Action == "skip",
				Output:  tr.truncateOutput(output),
			}

			if !result.Passed && !result.Skipped {
				result.Error = "Test failed"
			}

			results = append(results, result)
		}
	}

	waitErr := cmd.Wait()
	if waitErr != nil && stderr.Len() > 0 {
		// Build error from stderr (compilation errors, etc.)
		results = append(results, TestResult{
			Name:   "build",
			Error:  stderr.String(),
			Passed: false,
		})
	}

	// Check for package-level failures
	if waitErr != nil && len(results) == 0 {
		return nil, fmt.Errorf("go test failed: %v", waitErr)
	}

	return results, nil
}

// GoTestEvent represents a single event from go test -json output
type GoTestEvent struct {
	Time    string `json:"Time"`
	Action  string `json:"Action"` // "run", "pass", "fail", "skip", "output"
	Package string `json:"Package"`
	Test    string `json:"Test"`
	Output  string `json:"Output"`
}

// extractGoPackage extracts the package path from a test file path
func extractGoPackage(filePath, baseDir string) string {
	// If filePath is already a package path, return it
	if !strings.Contains(filePath, "/") && !strings.HasPrefix(filePath, "./") {
		if baseDir != "" {
			// Convert to package path
			relPath := strings.TrimPrefix(filePath, "./")
			relPath = strings.TrimSuffix(relPath, "_test.go")
			return relPath
		}
	}

	// Try to determine package from directory
	dir := baseDir
	if idx := strings.LastIndex(filePath, "/"); idx > 0 {
		dir = filePath[:idx]
	}

	// Run go list to get package name
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "list", "-f", "{{.ImportPath}}", dir)
	output, err := cmd.Output()
	if err != nil {
		// Fallback: convert path to package
		relPath := strings.TrimPrefix(filePath, baseDir)
		relPath = strings.TrimPrefix(relPath, "/")
		relPath = strings.TrimSuffix(relPath, "_test.go")
		relPath = strings.ReplaceAll(relPath, "/", ".")
		return relPath
	}

	return strings.TrimSpace(string(output))
}

// findGoTestFile finds the test file for a given test name and package
func findGoTestFile(pkg, testName string) string {
	// Convert package path to file path pattern
	pkgPath := strings.ReplaceAll(pkg, ".", "/")
	patterns := []string{
		pkgPath + "/" + testName + "_test.go",
		pkgPath + "/*_test.go",
	}

	// Return a reasonable guess
	return patterns[1]
}

// runPytestTests runs pytest tests
func (tr *TestRunner) runPytestTests(ctx context.Context, dirPath string, testFiles []string) ([]TestResult, error) {
	pytestCmd := "pytest"
	if tr.config.CustomPytestCmd != "" {
		pytestCmd = tr.config.CustomPytestCmd
	}

	args := []string{"-v", "--tb=short"}
	args = append(args, tr.config.PytestFlags...)

	if len(testFiles) > 0 {
		args = append(args, testFiles...)
	} else {
		args = append(args, ".")
	}

	tr.logger.Debug("running pytest", "cmd", pytestCmd, "args", args, "dir", dirPath)

	cmd := exec.CommandContext(ctx, pytestCmd, args...)
	if dirPath != "" {
		cmd.Dir = dirPath
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		// pytest returns non-zero on test failures, which is expected
		if _, ok := err.(*exec.ExitError); ok {
			// Parse output for failures
			return tr.parsePytestOutput(string(output), string(output))
		}
		// Return error if not an exit error
		return nil, fmt.Errorf("pytest failed: %w: %s", err, output)
	}

	return tr.parsePytestOutput(string(output), string(output))
}

// parsePytestOutput parses pytest output to TestResults
func (tr *TestRunner) parsePytestOutput(stdout, stderr string) ([]TestResult, error) {
	output := stdout + "\n" + stderr
	results := []TestResult{}

	// Parse pytest output
	// Look for test result lines like:
	// test_file.py::test_name PASSED
	// test_file.py::test_name FAILED
	// test_file.py::test_name SKIPPED

	linePattern := regexp.MustCompile(`^(.+?)::(.+?)\s+(PASSED|FAILED|SKIPPED|ERROR)`)
	outputLines := strings.Split(output, "\n")

	for _, line := range outputLines {
		matches := linePattern.FindStringSubmatch(line)
		if matches != nil {
			testName := matches[2]
			status := matches[3]

			result := TestResult{
				Name:    testName,
				Passed:  status == "PASSED",
				Skipped: status == "SKIPPED",
			}

			if status == "FAILED" || status == "ERROR" {
				result.Error = "Test failed"
				// Try to capture error message from surrounding lines
				result.Output = tr.findPytestErrorContext(outputLines, testName)
			}

			results = append(results, result)
		}
	}

	// If no results parsed, try to extract from summary
	if len(results) == 0 {
		summaryPattern := regexp.MustCompile(`(\d+) passed|(\d+) failed|(\d+) skipped`)
		matches := summaryPattern.FindAllStringSubmatch(output, -1)
		if len(matches) > 0 {
			// Just add a summary result
			results = append(results, TestResult{
				Name:   "summary",
				Passed: strings.Contains(output, "0 failed"),
				Error:  "See output for details",
			})
		}
	}

	return results, nil
}

// findPytestErrorContext finds error context for a failed pytest test
func (tr *TestRunner) findPytestErrorContext(lines []string, testName string) string {
	var ctx strings.Builder
	found := false

	for _, line := range lines {
		if strings.Contains(line, testName) && (strings.Contains(line, "FAILED") || strings.Contains(line, "ERROR")) {
			found = true
		}
		if found {
			ctx.WriteString(line)
			ctx.WriteString("\n")
			// Stop after a few lines
			if strings.Contains(line, "=====") {
				break
			}
		}
	}

	return tr.truncateOutput(ctx.String())
}

// runJestTests runs Jest tests
func (tr *TestRunner) runJestTests(ctx context.Context, dirPath string, testFiles []string) ([]TestResult, error) {
	jestCmd := "jest"
	if tr.config.CustomJestCmd != "" {
		jestCmd = tr.config.CustomJestCmd
	}

	args := []string{"--json"}
	args = append(args, tr.config.JestFlags...)

	if len(testFiles) > 0 {
		args = append(args, testFiles...)
	}

	tr.logger.Debug("running jest", "cmd", jestCmd, "args", args, "dir", dirPath)

	cmd := exec.CommandContext(ctx, jestCmd, args...)
	if dirPath != "" {
		cmd.Dir = dirPath
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Jest returns non-zero on test failures
		tr.logger.Debug("jest failed", "error", err, "output", string(output))
	}

	return tr.parseJestOutput(string(output))
}

// parseJestOutput parses Jest JSON output to TestResults
func (tr *TestRunner) parseJestOutput(output string) ([]TestResult, error) {
	// Try to parse as JSON first
	var jestResult struct {
		NumTotalTests    int `json:"numTotalTests"`
		NumPassedTests   int `json:"numPassedTests"`
		NumFailedTests   int `json:"numFailedTests"`
		NumPendingTests  int `json:"numPendingTests"`
		TestResults      []struct {
			Name          string `json:"name"`
			Status        string `json:"status"`
			NumPassingTests int   `json:"numPassingTests"`
			NumFailingTests int   `json:"numFailingTests"`
			AssertionResults []struct {
				Status string `json:"status"`
				Name   string `json:"name"`
			} `json:"assertionResults"`
		} `json:"testResults"`
	}

	if err := json.Unmarshal([]byte(output), &jestResult); err != nil {
		// Not JSON, parse as text
		return tr.parseJestTextOutput(output)
	}

	results := []TestResult{}
	for _, testFile := range jestResult.TestResults {
		for _, assertion := range testFile.AssertionResults {
			result := TestResult{
				Name:    assertion.Name,
				Passed:  assertion.Status == "passed",
				Skipped: assertion.Status == "pending",
			}

			if assertion.Status == "failed" {
				result.Error = "Test failed"
			}

			results = append(results, result)
		}
	}

	return results, nil
}

// parseJestTextOutput parses Jest text output (fallback)
func (tr *TestRunner) parseJestTextOutput(output string) ([]TestResult, error) {
	results := []TestResult{}

	// Look for test results
	passPattern := regexp.MustCompile(`^\s*✓\s+(.+)$`)
	failPattern := regexp.MustCompile(`^\s*✕\s+(.+)$`)
	skipPattern := regexp.MustCompile(`^\s*○\s+(.+)$`)

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if matches := passPattern.FindStringSubmatch(line); matches != nil {
			results = append(results, TestResult{
				Name:   matches[1],
				Passed: true,
			})
		} else if matches := failPattern.FindStringSubmatch(line); matches != nil {
			results = append(results, TestResult{
				Name:   matches[1],
				Passed: false,
				Error:  "Test failed",
			})
		} else if matches := skipPattern.FindStringSubmatch(line); matches != nil {
			results = append(results, TestResult{
				Name:    matches[1],
				Passed:  false,
				Skipped: true,
			})
		}
	}

	return results, nil
}

// truncateOutput truncates test output to MaxOutputLines
func (tr *TestRunner) truncateOutput(output string) string {
	if tr.config.MaxOutputLines <= 0 {
		return output
	}

	lines := strings.Split(output, "\n")
	if len(lines) <= tr.config.MaxOutputLines {
		return output
	}

	return strings.Join(lines[:tr.config.MaxOutputLines], "\n") + "\n... (truncated)"
}

// HasFailures checks if any test results have failures
func HasFailures(results []TestResult) bool {
	for _, r := range results {
		if !r.Passed && !r.Skipped {
			return true
		}
	}
	return false
}

// FilterPassed returns only passing test results
func FilterPassed(results []TestResult) []TestResult {
	var filtered []TestResult
	for _, r := range results {
		if r.Passed {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// FilterFailed returns only failing test results
func FilterFailed(results []TestResult) []TestResult {
	var filtered []TestResult
	for _, r := range results {
		if !r.Passed && !r.Skipped {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// FilterSkipped returns only skipped test results
func FilterSkipped(results []TestResult) []TestResult {
	var filtered []TestResult
	for _, r := range results {
		if r.Skipped {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// Summary returns a human-readable summary of test results
func Summary(results []TestResult) string {
	passed := 0
	skipped := 0
	failed := 0

	for _, r := range results {
		if r.Passed {
			passed++
		} else if r.Skipped {
			skipped++
		} else {
			failed++
		}
	}

	return fmt.Sprintf("tests: %d passed, %d failed, %d skipped", passed, failed, skipped)
}