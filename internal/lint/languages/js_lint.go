package languages

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// JSLinter provides JavaScript/TypeScript-specific linting functionality.
type JSLinter struct{}

// NewJSLinter creates a new JavaScript/TypeScript linter.
func NewJSLinter() *JSLinter {
	return &JSLinter{}
}

// NewTSLinter creates a new TypeScript linter (uses same implementation).
func NewTSLinter() *JSLinter {
	return &JSLinter{}
}

// CompileCheck runs TypeScript compilation check.
// For JavaScript, this returns nil as JS is interpreted.
func (jl *JSLinter) CompileCheck(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
	ext := strings.ToLower(filepath.Ext(filePath))

	// TypeScript files need compilation check
	if ext == ".ts" || ext == ".tsx" {
		return jl.typeScriptCheck(ctx, filePath, relPath, content)
	}

	// JavaScript doesn't need compilation, do basic syntax check via node
	return jl.javaScriptSyntaxCheck(ctx, filePath, relPath, content)
}

// typeScriptCheck runs tsc to check TypeScript compilation.
func (jl *JSLinter) typeScriptCheck(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
	// Check if typescript is available
	if !isCommandAvailable("tsc") {
		// Try npx tsc
		cmd := exec.CommandContext(ctx, "npx", "--yes", "typescript", "--version")
		if cmd.Run() != nil {
			return nil, nil // Skip if not available
		}
	}

	// Write content to temp file
	tmpFile, err := writeTempTSFile(filePath, content)
	if err != nil {
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}
	defer cleanupTempFile(tmpFile)

	// Run tsc --noEmit to check for errors
	cmd := exec.CommandContext(ctx, "tsc", "--noEmit", "--skipLibCheck", tmpFile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	err = cmd.Run()
	if err == nil {
		return nil, nil // No errors
	}

	// Parse TypeScript errors
	output := stderr.String() + stdout.String()
	return parseTypeScriptErrors(output, relPath)
}

// javaScriptSyntaxCheck checks JavaScript syntax using node.
func (jl *JSLinter) javaScriptSyntaxCheck(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
	// Write content to temp file
	tmpFile, err := writeTempJSFile(content)
	if err != nil {
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}
	defer cleanupTempFile(tmpFile)

	// Run node --check to check syntax
	cmd := exec.CommandContext(ctx, "node", "--check", tmpFile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err == nil {
		return nil, nil // No syntax errors
	}

	// Parse Node.js syntax errors
	return parseJavaScriptErrors(stderr.String(), relPath)
}

// ESLint runs ESLint for linting JavaScript/TypeScript.
func (jl *JSLinter) ESLint(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
	// Check if eslint is available
	if !isCommandAvailable("eslint") {
		return nil, nil // Skip if not available
	}

	// Write content to temp file
	ext := strings.ToLower(filepath.Ext(filePath))
	var tmpFile string
	if ext == ".ts" || ext == ".tsx" {
		tmpFile, err := writeTempTSFile(filePath, content)
		if err != nil {
			return nil, fmt.Errorf("failed to write temp file: %w", err)
		}
		defer cleanupTempFile(tmpFile)
	} else {
		tmpFile, err := writeTempJSFile(content)
		if err != nil {
			return nil, fmt.Errorf("failed to write temp file: %w", err)
		}
		defer cleanupTempFile(tmpFile)
	}

	// Run eslint
	cmd := exec.CommandContext(ctx, "eslint", "--format", "compact", tmpFile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return nil, nil // No errors
	}

	// Parse ESLint errors
	return parseESLintErrors(stderr.String(), relPath)
}

// FormatCheck runs prettier or eslint --fix to check formatting.
func (jl *JSLinter) FormatCheck(ctx context.Context, filePath string) ([]LinterResult, error) {
	// Try prettier first
	if isCommandAvailable("prettier") {
		cmd := exec.CommandContext(ctx, "prettier", "--check", filePath)
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		err := cmd.Run()
		output := stdout.String() + stderr.String()
		if err == nil {
			return nil, nil // File is formatted correctly
		}

		if strings.Contains(output, "Code is not formatted") || strings.Contains(output, "should be formatted") {
			return []LinterResult{{
				File:     filePath,
				Line:     0,
				Message:  "File is not formatted correctly. Run `prettier --write` to fix.",
				Severity: "warning",
				Rule:     "prettier",
			}}, nil
		}
	}

	// Fall back to eslint --fix-dry-run if available
	if isCommandAvailable("eslint") {
		cmd := exec.CommandContext(ctx, "eslint", "--fix-dry-run", "--format", "json", filePath)
		var stdout bytes.Buffer
		cmd.Stdout = &stdout

		err := cmd.Run()
		if err == nil {
			return nil, nil // File is formatted correctly
		}

		// Check if there are fixable issues
		var results []LinterResult
		output := stdout.String()
		if strings.Contains(output, "fixable") {
			results = append(results, LinterResult{
				File:     filePath,
				Line:     0,
				Message:  "File has fixable formatting issues. Run `eslint --fix` to fix.",
				Severity: "warning",
				Rule:     "eslint-fix",
			})
		}
		return results, nil
	}

	return nil, nil
}

// writeTempJSFile writes JavaScript content to a temp file and returns its path.
func writeTempJSFile(content string) (string, error) {
	f, err := os.CreateTemp("", "lint_*.js")
	if err != nil {
		return "", err
	}
	defer f.Close()

	_, err = f.WriteString(content)
	if err != nil {
		return "", err
	}

	return f.Name(), nil
}

// writeTempTSFile writes TypeScript content to a temp file and returns its path.
func writeTempTSFile(filePath, content string) (string, error) {
	ext := ".ts"
	if strings.HasSuffix(filePath, ".tsx") {
		ext = ".tsx"
	} else if strings.HasSuffix(filePath, ".jsx") {
		ext = ".jsx"
	}

	f, err := os.CreateTemp("", "lint_*"+ext)
	if err != nil {
		return "", err
	}
	defer f.Close()

	_, err = f.WriteString(content)
	if err != nil {
		return "", err
	}

	return f.Name(), nil
}

// parseTypeScriptErrors parses TypeScript compiler output.
func parseTypeScriptErrors(output, targetFile string) ([]LinterResult, error) {
	// TypeScript error format:
	// file.ts(line,col): error TSxxxx: message

	pattern := regexp.MustCompile(`(\S+)\((\d+),(\d+)\):\s*error\s+TS\d+:\s*(.+)`)

	var results []LinterResult
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		if matches := pattern.FindStringSubmatch(line); matches != nil {
			var lineNum, colNum int
			fmt.Sscanf(matches[2], "%d", &lineNum)
			fmt.Sscanf(matches[3], "%d", &colNum)
			lineNum--
			colNum--

			results = append(results, LinterResult{
				File:     targetFile,
				Line:     lineNum,
				Column:   colNum,
				Message:  matches[4],
				Severity: "error",
				Rule:     "typescript",
			})
		}
	}

	return results, nil
}

// parseJavaScriptErrors parses Node.js syntax error output.
func parseJavaScriptErrors(output, targetFile string) ([]LinterResult, error) {
	// Node.js error format:
	// /path/to/file:line:col
	// error message

	var results []LinterResult

	// Try to extract line number from various formats
	linePattern := regexp.MustCompile(`line (\d+)`)
	colPattern := regexp.MustCompile(`column (\d+)`)
	messagePattern := regexp.MustCompile(`^(?:SyntaxError|ReferenceError|TypeError|EvalError|RangeError):\s*(.+)`)

	lines := strings.Split(output, "\n")
	var lineNum, colNum int

	for _, line := range lines {
		if matches := linePattern.FindStringSubmatch(line); matches != nil {
			fmt.Sscanf(matches[1], "%d", &lineNum)
			lineNum--
		}
		if matches := colPattern.FindStringSubmatch(line); matches != nil {
			fmt.Sscanf(matches[1], "%d", &colNum)
			colNum--
		}
		if matches := messagePattern.FindStringSubmatch(line); matches != nil {
			results = append(results, LinterResult{
				File:     targetFile,
				Line:     lineNum,
				Column:   colNum,
				Message:  matches[1],
				Severity: "error",
				Rule:     "syntax-error",
			})
		}
	}

	// If we couldn't parse, return general error
	if len(results) == 0 && output != "" {
		results = append(results, LinterResult{
			File:     targetFile,
			Line:     0,
			Message:  strings.TrimSpace(output),
			Severity: "error",
			Rule:     "syntax-error",
		})
	}

	return results, nil
}

// parseESLintErrors parses ESLint output.
func parseESLintErrors(output, targetFile string) ([]LinterResult, error) {
	// ESLint compact format:
	// /path/to/file:line:col: error: message [rule]

	pattern := regexp.MustCompile(`(\S+):(\d+):(\d+):\s*(error|warning):\s*(.+?)\s*\[(\w+)\]`)

	var results []LinterResult
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		if matches := pattern.FindStringSubmatch(line); matches != nil {
			var lineNum, colNum int
			fmt.Sscanf(matches[2], "%d", &lineNum)
			fmt.Sscanf(matches[3], "%d", &colNum)
			lineNum--
			colNum--

			severity := matches[4]
			if severity != "error" {
				severity = "warning"
			}

			results = append(results, LinterResult{
				File:     targetFile,
				Line:     lineNum,
				Column:   colNum,
				Message:  matches[5],
				Severity: severity,
				Rule:     matches[6],
			})
		}
	}

	return results, nil
}
