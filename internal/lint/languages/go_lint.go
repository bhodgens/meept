package languages

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// LinterResult represents the output of a linting run
type LinterResult struct {
	File     string
	Line     int    // 0-based line number
	Column   int    // 0-based column (optional)
	Message  string
	Severity string // "error" | "warning" | "info"
	Rule     string // Lint rule identifier
}

// GoLinter provides Go-specific linting functionality.
type GoLinter struct{}

// NewGoLinter creates a new Go linter.
func NewGoLinter() *GoLinter {
	return &GoLinter{}
}

// CompileCheck runs Go compilation as a lint check.
// It writes content to a temp file and tries to compile it.
func (gl *GoLinter) CompileCheck(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
	// Determine the directory to work in
	dir := filepath.Dir(filePath)
	if dir == "." {
		// Try to find a go module in current directory or parent
		dir = findGoModuleRoot(filePath)
	}

	// Write content to temp file for compilation check
	tmpFile, err := writeTempGoFile(filePath, content)
	if err != nil {
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}
	defer cleanupTempFile(tmpFile)

	// Run `go build` to check for compilation errors
	cmd := exec.CommandContext(ctx, "go", "build", "-o", os.DevNull, tmpFile)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err == nil {
		return nil, nil // No errors
	}

	// Parse Go compiler errors
	return parseGoErrors(stderr.String(), relPath)
}

// Vet runs `go vet` for semantic issues in the directory.
func (gl *GoLinter) Vet(ctx context.Context, dirPath string) ([]LinterResult, error) {
	// Find the go module root
	moduleRoot := findGoModuleRoot(dirPath)
	if moduleRoot == "" {
		moduleRoot = dirPath
	}

	cmd := exec.CommandContext(ctx, "go", "vet", "./...")
	cmd.Dir = moduleRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	_ = cmd.Run() // go vet returns non-zero on warnings/errors

	// Parse vet output - it's similar to compiler errors
	results, _ := parseGoErrors(stderr.String(), "")
	return results, nil
}

// TypeCheck runs `go build` or `go vet` with type checking.
func (gl *GoLinter) TypeCheck(ctx context.Context, dirPath string) ([]LinterResult, error) {
	moduleRoot := findGoModuleRoot(dirPath)
	if moduleRoot == "" {
		moduleRoot = dirPath
	}

	// Use go build which does type checking
	cmd := exec.CommandContext(ctx, "go", "build", "-o", os.DevNull, ".")
	cmd.Dir = moduleRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return nil, nil
	}

	results, _ := parseGoErrors(stderr.String(), "")
	return results, nil
}

// FormatCheck runs `gofmt` to check formatting.
func (gl *GoLinter) FormatCheck(ctx context.Context, filePath string) ([]LinterResult, error) {
	cmd := exec.CommandContext(ctx, "gofmt", "-l", filePath)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	err := cmd.Run()
	if err == nil {
		return nil, nil // File is formatted correctly
	}

	// File needs formatting
	output := strings.TrimSpace(stdout.String())
	if output != "" {
		return []LinterResult{{
			File:     filePath,
			Line:     0,
			Message:  "File is not formatted correctly. Run `gofmt -w` to fix.",
			Severity: "warning",
			Rule:     "go_fmt",
		}}, nil
	}

	return nil, nil
}

// findGoModuleRoot finds the go module root directory.
func findGoModuleRoot(path string) string {
	dir := filepath.Dir(path)
	if dir == "." {
		dir, _ = os.Getwd()
	}

	// Walk up looking for go.mod
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Fall back to current directory
	if dir, err := os.Getwd(); err == nil {
		return dir
	}

	return "."
}

// writeTempGoFile writes Go content to a temp file and returns its path.
func writeTempGoFile(originalPath, content string) (string, error) {
	// Create temp file with .go extension
	f, err := os.CreateTemp("", "lint_*.go")
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

// cleanupTempFile removes a temp file.
func cleanupTempFile(path string) {
	if path != "" {
		os.Remove(path)
	}
}

// parseGoErrors parses Go compiler error output
func parseGoErrors(output, targetFile string) ([]LinterResult, error) {
	// Go error format: ./file.go:line:column: error message or file.go:line: error message
	errorPattern := regexp.MustCompile(`([^:]+):(\d+):(\d+)?\s*:\s*(.+)`)
	simplePattern := regexp.MustCompile(`([^:]+):(\d+):\s*(.+)`)

	var results []LinterResult
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var result LinterResult

		if matches := errorPattern.FindStringSubmatch(line); matches != nil {
			lineNum := 0
			colNum := 0
			lineNum, _ = strconv.Atoi(matches[2])
			if matches[3] != "" {
				colNum, _ = strconv.Atoi(matches[3])
			}
			file := targetFile
			if file == "" {
				file = matches[1]
			}
			result = LinterResult{
				File:     file,
				Line:     lineNum - 1, // Convert to 0-based
				Column:   colNum - 1,
				Message:  matches[4],
				Severity: "error",
			}
		} else if matches := simplePattern.FindStringSubmatch(line); matches != nil {
			lineNum, _ := strconv.Atoi(matches[2])
			file := targetFile
			if file == "" {
				file = matches[1]
			}
			result = LinterResult{
				File:     file,
				Line:     lineNum - 1,
				Message:  matches[3],
				Severity: "error",
			}
		}

		if result.File != "" || result.Message != "" {
			results = append(results, result)
		}
	}

	return results, nil
}
