package languages

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// PythonLinter provides Python-specific linting functionality.
type PythonLinter struct{}

// NewPythonLinter creates a new Python linter.
func NewPythonLinter() *PythonLinter {
	return &PythonLinter{}
}

// CompileCheck runs Python syntax check using py_compile.
func (pl *PythonLinter) CompileCheck(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
	// Write content to temp file for syntax check
	tmpFile, err := writeTempPythonFile(content)
	if err != nil {
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}
	defer cleanupTempFile(tmpFile)

	// Run python3 -m py_compile to check syntax
	cmd := exec.CommandContext(ctx, "python3", "-m", "py_compile", tmpFile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err == nil {
		return nil, nil // No syntax errors
	}

	// Parse Python syntax errors
	return parsePythonSyntaxErrors(stderr.String(), relPath)
}

// Flake8 runs flake8 with fatal-only error codes.
// These are syntax errors, undefined names, and other critical issues.
func (pl *PythonLinter) Flake8(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
	// Write content to temp file
	tmpFile, err := writeTempPythonFile(content)
	if err != nil {
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}
	defer cleanupTempFile(tmpFile)

	// Check if flake8 is available
	if !isCommandAvailable("flake8") {
		// Fall back to py_compile
		return pl.CompileCheck(ctx, filePath, relPath, content)
	}

	cmd := exec.CommandContext(ctx, "flake8",
		"--isolated",
		"--select=E9,F821,F823,F831,F406,F407,F701,F702,F704,F706",
		"--show-source",
		tmpFile,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err == nil {
		return nil, nil // No errors
	}

	// Parse flake8 errors
	return parseFlake8Errors(stderr.String(), relPath)
}

// Pyright runs Pyright for type checking.
func (pl *PythonLinter) Pyright(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
	// Check if pyright is available
	if !isCommandAvailable("pyright") {
		return nil, nil // Skip if not available
	}

	// Write content to temp file
	tmpFile, err := writeTempPythonFile(content)
	if err != nil {
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}
	defer cleanupTempFile(tmpFile)

	cmd := exec.CommandContext(ctx, "pyright", tmpFile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	err = cmd.Run()
	if err == nil {
		return nil, nil // No errors
	}

	// Parse pyright output
	output := stderr.String() + stdout.String()
	return parsePyrightErrors(output, relPath)
}

// FormatCheck runs black to check formatting.
func (pl *PythonLinter) FormatCheck(ctx context.Context, filePath string) ([]LinterResult, error) {
	// Check if black is available
	if !isCommandAvailable("black") {
		return nil, nil // Skip if not available
	}

	cmd := exec.CommandContext(ctx, "black", "--check", "--diff", filePath)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return nil, nil // File is formatted correctly
	}

	// Check if it's a formatting issue or something else
	output := stdout.String() + stderr.String()
	if strings.Contains(output, "would reformat") {
		return []LinterResult{{
			File:     filePath,
			Line:     0,
			Message:  "File is not formatted correctly. Run `black` to fix.",
			Severity: "warning",
			Rule:     "black",
		}}, nil
	}

	return nil, nil
}

// writeTempPythonFile writes Python content to a temp file and returns its path.
func writeTempPythonFile(content string) (string, error) {
	f, err := os.CreateTemp("", "lint_*.py")
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

// parsePythonSyntaxErrors parses Python syntax error output.
func parsePythonSyntaxErrors(output, targetFile string) ([]LinterResult, error) {
	// Python syntax error format:
	// File "...", line 10
	//   content
	//   ^
	// SyntaxError: invalid syntax
	//
	// Or with python -m py_compile:
	// SyntaxError: invalid syntax (file, line 10)

	var results []LinterResult

	// Pattern for "line X" in error messages
	linePattern := regexp.MustCompile(`line (\d+)`)
	// Pattern for specific syntax error
	syntaxErrorPattern := regexp.MustCompile(`SyntaxError: (.+)`)

	lines := strings.Split(output, "\n")
	currentLine := 0

	for _, line := range lines {
		// Check for line number
		if matches := linePattern.FindStringSubmatch(line); matches != nil {
			fmt.Sscanf(matches[1], "%d", &currentLine)
			currentLine--
		}

		// Check for SyntaxError
		if matches := syntaxErrorPattern.FindStringSubmatch(line); matches != nil {
			results = append(results, LinterResult{
				File:     targetFile,
				Line:     currentLine,
				Message:  fmt.Sprintf("SyntaxError: %s", matches[1]),
				Severity: "error",
				Rule:     "syntax-error",
			})
		}
	}

	// If we couldn't parse specific line, return general error
	if len(results) == 0 && strings.Contains(output, "SyntaxError") {
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

// parseFlake8Errors parses flake8 output.
func parseFlake8Errors(output, targetFile string) ([]LinterResult, error) {
	// Flake8 format: filename.py:line:column: error_code message
	pattern := regexp.MustCompile(`([^:]+):(\d+):(\d+):\s*(\w+)\s+(.+)`)

	var results []LinterResult
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		if matches := pattern.FindStringSubmatch(line); matches != nil {
			lineNum := 0
			colNum := 0
			fmt.Sscanf(matches[2], "%d", &lineNum)
			fmt.Sscanf(matches[3], "%d", &colNum)

			results = append(results, LinterResult{
				File:     targetFile,
				Line:     lineNum - 1,
				Column:   colNum - 1,
				Message:  fmt.Sprintf("%s: %s", matches[4], matches[5]),
				Severity: "error",
				Rule:     matches[4],
			})
		}
	}

	return results, nil
}

// parsePyrightErrors parses Pyright type checker output.
func parsePyrightErrors(output, targetFile string) ([]LinterResult, error) {
	// Pyright error format:
	// file.py:10:5 - error: message (rule)
	// file.py:10:5 - warning: message (rule)

	pattern := regexp.MustCompile(`(\S+):(\d+):(\d+)\s*-\s*(error|warning):\s*(.+)`)

	var results []LinterResult
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		if matches := pattern.FindStringSubmatch(line); matches != nil {
			lineNum, colNum := 0, 0
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
				Rule:     "pyright",
			})
		}
	}

	return results, nil
}

// isCommandAvailable checks if a command is available in PATH.
func isCommandAvailable(name string) bool {
	cmd := exec.Command("which", name)
	return cmd.Run() == nil
}
