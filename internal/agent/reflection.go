package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/lint"
	"github.com/caimlas/meept/internal/llm"
)

// ReflectionConfig holds reflection loop parameters
type ReflectionConfig struct {
	MaxReflections int    // Default: 3
	AutoLint       bool   // Enable auto-linting
	AutoTest       bool   // Enable auto-testing
	LintCmd        string // Custom lint command (optional)
	TestCmd        string // Custom test command (optional)
	WorkDir        string // Working directory for lint/test commands
}

// DefaultReflectionConfig returns the default configuration
func DefaultReflectionConfig() ReflectionConfig {
	return ReflectionConfig{
		MaxReflections: 3,
		AutoLint:       true,
		AutoTest:       true,
	}
}

// ReflectionEngine manages the auto-fix loop
type ReflectionEngine struct {
	config     ReflectionConfig
	linter     *lint.Registry
	testRunner *lint.TestRunner
	llmClient  llm.Chatter
	logger     *slog.Logger
	editAvail  bool // Whether file edit tool is available (for testing)
}

// NewReflectionEngine creates a new reflection engine with default config
func NewReflectionEngine(logger *slog.Logger, linter *lint.Registry, testRunner *lint.TestRunner, llmClient llm.Chatter) *ReflectionEngine {
	return NewReflectionEngineWithConfig(logger, linter, testRunner, llmClient, DefaultReflectionConfig())
}

// NewReflectionEngineWithConfig creates a new reflection engine with custom configuration
func NewReflectionEngineWithConfig(logger *slog.Logger, linter *lint.Registry, testRunner *lint.TestRunner, llmClient llm.Chatter, config ReflectionConfig) *ReflectionEngine {
	if config.MaxReflections == 0 {
		config.MaxReflections = 3
	}
	return &ReflectionEngine{
		config:     config,
		linter:     linter,
		testRunner: testRunner,
		llmClient:  llmClient,
		logger:     logger,
	}
}

// SetEditAvailability sets whether file edit is available
func (re *ReflectionEngine) SetEditAvailability(avail bool) {
	re.editAvail = avail
}

// ReflectionResult holds the outcome of a reflection cycle
type ReflectionResult struct {
	Fixed        bool
	Iterations   int
	LintErrors   []lint.LinterResult
	TestFailures []lint.TestResult
	FinalMessage string
	GaveUp       bool // True if max reflections reached without fix
}

// RunReflection executes the reflection loop after code edits
func (re *ReflectionEngine) RunReflection(ctx context.Context, editedFiles []string) (*ReflectionResult, error) {
	result := &ReflectionResult{}

	re.logger.Debug("starting reflection loop", "max_iterations", re.config.MaxReflections, "files", len(editedFiles))

	for i := 0; i < re.config.MaxReflections; i++ {
		result.Iterations = i + 1
		re.logger.Debug("reflection iteration", "iteration", result.Iterations)

		// Step 1: Run linters
		if re.config.AutoLint && re.linter != nil {
			lintErrors, err := re.runLinters(ctx, editedFiles)
			if err != nil {
				re.logger.Warn("linter failed", "error", err)
				// Don't fail the whole loop - try tests anyway
			} else if len(lintErrors) > 0 {
				result.LintErrors = append(result.LintErrors, lintErrors...)

				// Step 2: Ask LLM to fix
				re.logger.Info("lint errors found, requesting fix", "count", len(lintErrors))
				fixRequest := re.formatLintFixRequest(lintErrors, editedFiles)
				fixApplied, err := re.requestFix(ctx, fixRequest, editedFiles)
				if err != nil {
					re.logger.Warn("fix request failed", "error", err)
					result.FinalMessage = fmt.Sprintf("Failed to request fix: %v", err)
					result.GaveUp = true
					return result, nil
				}
				if !fixApplied {
					re.logger.Warn("fix was not applied")
					result.FinalMessage = fmt.Sprintf("Failed to apply fix after %d iterations", i+1)
					result.GaveUp = true
					return result, nil
				}
				continue // Retry linting after fix
			}
		}

		// Step 3: Run tests
		if re.config.AutoTest && re.testRunner != nil {
			testFailures, err := re.runTests(ctx, editedFiles)
			if err != nil {
				re.logger.Warn("tests failed to run", "error", err)
				// Don't fail - just report
			} else if len(testFailures) > 0 {
				result.TestFailures = append(result.TestFailures, testFailures...)

				// Step 4: Ask LLM to fix
				re.logger.Info("test failures found, requesting fix", "count", len(testFailures))
				fixRequest := re.formatTestFixRequest(testFailures)
				fixApplied, err := re.requestFix(ctx, fixRequest, editedFiles)
				if err != nil {
					re.logger.Warn("fix request failed", "error", err)
					result.FinalMessage = fmt.Sprintf("Failed to request fix: %v", err)
					result.GaveUp = true
					return result, nil
				}
				if !fixApplied {
					re.logger.Warn("fix was not applied")
					result.FinalMessage = fmt.Sprintf("Failed to fix test failures after %d iterations", i+1)
					result.GaveUp = true
					return result, nil
				}
				continue // Retry tests after fix
			}
		}

		// Success: no errors
		result.Fixed = true
		result.FinalMessage = "All checks passed"
		re.logger.Info("reflection completed successfully", "iterations", result.Iterations)
		return result, nil
	}

	// Max reflections reached
	result.GaveUp = true
	result.FinalMessage = fmt.Sprintf("Gave up after %d reflection iterations", re.config.MaxReflections)
	re.logger.Warn("reflection gave up", "iterations", result.Iterations, "message", result.FinalMessage)
	return result, nil
}

// runLinters runs all registered linters on the edited files
func (re *ReflectionEngine) runLinters(ctx context.Context, editedFiles []string) ([]lint.LinterResult, error) {
	var allErrors []lint.LinterResult

	workDir := re.config.WorkDir
	if workDir == "" {
		workDir = "."
	}

	for _, filePath := range editedFiles {
		// Determine absolute or relative path
		absPath := filePath
		if !filepath.IsAbs(filePath) {
			absPath = filepath.Join(workDir, filePath)
		}

		// Read file content
		content, err := os.ReadFile(absPath)
		if err != nil {
			re.logger.Debug("failed to read file for linting", "file", filePath, "error", err)
			continue
		}

		// Detect language
		lang := detectLanguageFromExt(filePath)
		if lang == "" {
			continue
		}

		// Run linters
		results, err := re.linter.Lint(ctx, lang, absPath, filePath, string(content))
		if err != nil {
			re.logger.Warn("linter error", "file", filePath, "error", err)
			continue
		}

		// Filter to only errors (skip warnings/info for reflection)
		for _, r := range results {
			if r.HasErrors() {
				allErrors = append(allErrors, r)
			}
		}
	}

	return allErrors, nil
}

// runTests runs tests on the edited files
func (re *ReflectionEngine) runTests(ctx context.Context, editedFiles []string) ([]lint.TestResult, error) {
	workDir := re.config.WorkDir
	if workDir == "" {
		workDir = "."
	}

	// Detect project language from files
	lang := detectProjectLanguage(editedFiles, workDir)
	if lang == "" {
		return nil, fmt.Errorf("could not detect project language")
	}

	// Run tests
	results, err := re.testRunner.RunTests(ctx, lang, workDir, nil)
	if err != nil {
		return nil, fmt.Errorf("test execution failed: %w", err)
	}

	// Filter to only failures
	var failures []lint.TestResult
	for _, r := range results {
		if !r.Passed && !r.Skipped {
			failures = append(failures, r)
		}
	}

	return failures, nil
}

// requestFix sends error context to LLM and attempts to apply fixes
func (re *ReflectionEngine) requestFix(ctx context.Context, fixRequest string, _ []string) (bool, error) {
	if re.llmClient == nil {
		re.logger.Warn("no LLM client available for fix requests")
		return false, nil // Not an error - just can't fix
	}

	// Build prompt with error context
	prompt := re.buildFixPrompt(fixRequest)

	// Get fix from LLM
	re.logger.Debug("requesting fix from LLM")
	messages := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: prompt},
	}

	response, err := re.llmClient.Chat(ctx, messages)
	if err != nil {
		re.logger.Warn("LLM request failed", "error", err)
		return false, err
	}

	if response == nil || response.Content == "" {
		re.logger.Warn("empty response from LLM")
		return false, nil
	}

	// Log the response for debugging
	re.logger.Debug("received fix response from LLM", "content_length", len(response.Content))

	// In a real implementation, this would parse the response and apply edits
	// For now, we return success if we got a response (the LLM is expected to output edits)
	// The actual edit application would happen through the file_edit tool
	return true, nil
}

// formatLintFixRequest creates a formatted prompt for lint errors
func (re *ReflectionEngine) formatLintFixRequest(errors []lint.LinterResult, _ []string) string {
	var sb strings.Builder
	sb.WriteString("# Fix any errors below, if possible.\n\n")

	// First, list all errors
	for _, err := range errors {
		sb.WriteString(fmt.Sprintf("## %s:%d:%d\n", err.File, err.Line+1, err.Column+1))
		sb.WriteString(fmt.Sprintf("Error (%s): %s\n\n", err.Rule, err.Message))
	}

	// Add tree context for each file with errors
	filesWithErrors := uniqueFilesFromErrors(errors)
	workDir := re.config.WorkDir
	if workDir == "" {
		workDir = "."
	}

	for _, file := range filesWithErrors {
		fileErrors := filterErrorsForFile(errors, file)

		// Build absolute path
		absPath := file
		if !filepath.IsAbs(file) {
			absPath = filepath.Join(workDir, file)
		}

		ctx := re.buildTreeContext(absPath, fileErrors)
		sb.WriteString(ctx)
		sb.WriteString("\n")
	}

	return sb.String()
}

// formatTestFixRequest creates a formatted prompt for test failures
func (re *ReflectionEngine) formatTestFixRequest(failures []lint.TestResult) string {
	var sb strings.Builder
	sb.WriteString("# Fix the failing tests below.\n\n")

	for _, f := range failures {
		if !f.Passed && !f.Skipped {
			sb.WriteString(fmt.Sprintf("## Test: %s\n", f.Name))
			if f.File != "" {
				sb.WriteString(fmt.Sprintf("File: %s\n", f.File))
			}
			if f.Error != "" {
				sb.WriteString(fmt.Sprintf("Error: %s\n", f.Error))
			}
			if f.Output != "" {
				sb.WriteString("\nOutput:\n```\n")
				sb.WriteString(reflectionTruncate(f.Output, 2000))
				sb.WriteString("\n```\n\n")
			}
		}
	}

	return sb.String()
}

// buildTreeContext generates tree-sitter context for a file with error markers
func (re *ReflectionEngine) buildTreeContext(filePath string, errors []lint.LinterResult) string {
	// Use AST package for tree context if available
	errorLines := make(map[int]bool)
	for _, e := range errors {
		errorLines[e.Line] = true
	}

	// Try to use ast.TreeContextWithMarkers if available
	// This is a placeholder - the actual implementation would use AST parsing
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Context for %s\n\n", filePath))

	// Read the file and show lines around errors
	content, err := os.ReadFile(filePath)
	if err != nil {
		return sb.String()
	}

	lines := strings.Split(string(content), "\n")
	padding := 3

	// Show context around each error line
	for lineNum := range errorLines {
		start := max(0, lineNum-padding)
		end := min(len(lines), lineNum+padding+1)

		sb.WriteString(fmt.Sprintf("Lines %d-%d:\n", start+1, end))
		for i := start; i < end; i++ {
			marker := "  "
			if i == lineNum {
				marker = ">> "
			}
			sb.WriteString(fmt.Sprintf("%s%d: %s\n", marker, i+1, lines[i]))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// buildFixPrompt builds the full prompt for the LLM
func (re *ReflectionEngine) buildFixPrompt(fixContext string) string {
	return fmt.Sprintf(`You are helping fix code errors in a codebase.

%s

Please analyze the errors above and provide corrected code. Use the file_edit tool to apply fixes, or if you're providing code directly, format it as a complete patch with the file path and corrected content.

Focus on fixing the specific errors mentioned.`, fixContext)
}

// detectLanguageFromExt determines language from file extension
func detectLanguageFromExt(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "typescript"
	case ".jsx":
		return "javascript"
	default:
		return ""
	}
}

// detectProjectLanguage detects the primary language of a project
func detectProjectLanguage(editedFiles []string, workDir string) string {
	// Check for go.mod, package.json, requirements.txt, etc.
	indicators := map[string]string{
		"go.mod":            "go",
		"package.json":      "javascript",
		"requirements.txt":  "python",
		"setup.py":          "python",
		"pyproject.toml":    "python",
		"Cargo.toml":        "rust",
		"Gemfile":           "ruby",
	}

	for _, file := range editedFiles {
		// Check the base directory
		baseDir := workDir
		if idx := strings.LastIndex(file, "/"); idx > 0 {
			baseDir = filepath.Join(workDir, file[:idx])
		}

		for indicator, lang := range indicators {
			checkPath := filepath.Join(baseDir, indicator)
			if _, err := os.Stat(checkPath); err == nil {
				return lang
			}
		}
	}

	// Fallback: check first file extension
	for _, f := range editedFiles {
		if lang := detectLanguageFromExt(f); lang != "" {
			return lang
		}
	}

	return ""
}

// Helper functions for error filtering

func uniqueFilesFromErrors(errors []lint.LinterResult) []string {
	seen := make(map[string]bool)
	var files []string
	for _, e := range errors {
		if !seen[e.File] {
			seen[e.File] = true
			files = append(files, e.File)
		}
	}
	return files
}

func filterErrorsForFile(errors []lint.LinterResult, file string) []lint.LinterResult {
	var filtered []lint.LinterResult
	for _, e := range errors {
		if e.File == file {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// reflectionTruncate truncates a string to a maximum length
func reflectionTruncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return s
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... (truncated)"
}