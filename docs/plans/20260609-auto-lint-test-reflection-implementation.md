# Auto Lint/Test with Reflection Loop Implementation

**Created:** 2026-06-09
**Priority:** Critical (Essential for Debugger Agent)
**Estimated Effort:** 2-3 weeks
**Status:** Pending Approval

## Overview

Implement an automated quality-assurance feedback loop that:
1. Automatically runs linters and tests after code changes
2. Feeds failures back to the LLM with structured context
3. Allows the LLM to attempt automatic fixes (reflection)
4. Iterates up to a configurable limit before requiring human intervention

**Inspired by:** aider-ai/aider's auto-lint/test reflection system

## Problem Statement

Currently, Meept's agents can write code but have no systematic way to:
- Verify code quality after changes
- Catch syntax errors before commit
- Run tests to validate correctness
- Self-correct based on automated feedback

This results in:
- Higher error rates in generated code
- More human review required
- Slower iteration cycles
- Debugger agent lacks systematic failure analysis

## Solution Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Agent Loop (existing)                        │
│                                                                  │
│  ┌──────────────┐      ┌──────────────┐     ┌────────────────┐ │
│  │ LLM Request  │─────▶│ Apply Edits  │────▶│ Lint Edits     │ │
│  └──────────────┘      └──────────────┘     └────────────────┘ │
│                                                │                │
│                           ┌────────────────────┘                │
│                           ▼                                     │
│                    ┌──────────────┐     ┌────────────────┐     │
│                    │ Has Errors?  │────▶│ Run Tests      │     │
│                    └──────────────┘     └────────────────┘     │
│                           │                      │              │
│                           ▼                      ▼              │
│                    ┌──────────────┐     ┌────────────────┐     │
│                    │ Return OK    │     │ Has Failures?  │     │
│                    └──────────────┘     └────────────────┘     │
│                           │                      │              │
│        ┌──────────────────┘                     ▼               │
│        │                              ┌──────────────────┐     │
│        │                              │ Reflection Loop   │     │
│        │                              │ (up to N times)   │     │
│        │                              └──────────────────┘     │
│        │                                       │                │
│        │         ┌─────────────────────────────┘                │
│        │         ▼                                              │
│        │  ┌─────────────────┐                                   │
│        │  │ Format Errors   │                                   │
│        │  │ + Tree Context  │                                   │
│        │  └─────────────────┘                                   │
│        │         │                                              │
│        │         ▼                                              │
│        │  ┌─────────────────┐     ┌──────────────────┐         │
│        └─▶│ LLM Fix Request │────▶│ Apply Fixes      │─────────┘
│           └─────────────────┘     └──────────────────┘
```

## Detailed Implementation

### 1. Linter Registry (`internal/lint/registry.go`)

**Purpose:** Central registry of linters by language.

```go
package lint

import (
    "context"
    "fmt"
    "os/exec"
    "regexp"
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

// HasErrors returns true if any error-severity issues found
func (r LinterResult) HasErrors() bool {
    return r.Severity == "error"
}

// LinterFunc defines the signature for language-specific linters
type LinterFunc func(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error)

// Registry manages linters by language
type Registry struct {
    mu            sync.RWMutex
    linters       map[string][]LinterFunc  // language → linters
    globalLinter  LinterFunc               // catch-all
    knownLangs    map[string]bool
}

// NewRegistry creates a new linter registry
func NewRegistry() *Registry {
    r := &Registry{
        linters:    make(map[string][]LinterFunc),
        knownLangs: make(map[string]bool),
    }
    r.registerDefaults()
    return r
}

// Register adds a linter for a specific language
func (r *Registry) Register(lang string, fn LinterFunc) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.linters[lang] = append(r.linters[lang], fn)
    r.knownLangs[lang] = true
}

// SetGlobalLinter sets a catch-all linter for unknown languages
func (r *Registry) SetGlobalLinter(fn LinterFunc) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.globalLinter = fn
}

// Lint runs all registered linters for the given file
func (r *Registry) Lint(ctx context.Context, lang, filePath, relPath, content string) ([]LinterResult, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    var allResults []LinterResult

    // Run language-specific linters
    linters := r.linters[lang]
    for _, fn := range linters {
        results, err := fn(ctx, filePath, relPath, content)
        if err != nil {
            return nil, fmt.Errorf("linter %s failed: %w", lang, err)
        }
        allResults = append(allResults, results...)
    }

    // Run global linter if no language-specific linters
    if len(linters) == 0 && r.globalLinter != nil {
        results, err := r.globalLinter(ctx, filePath, relPath, content)
        if err != nil {
            return nil, fmt.Errorf("global linter failed: %w", err)
        }
        allResults = append(allResults, results...)
    }

    return allResults, nil
}

// registerDefaults sets up built-in linters
func (r *Registry) registerDefaults() {
    // Go linter chain
    r.Register("go", r.goTreeSitterLint)
    r.Register("go", r.goCompileCheck)
    r.Register("go", r.goVet)

    // Python linter chain
    r.Register("python", r.pythonTreeSitterLint)
    r.Register("python", r.pythonCompileCheck)
    r.Register("python", r.pythonFlake8)

    // JavaScript/TypeScript
    r.Register("javascript", r.jsTreeSitterLint)
    r.Register("typescript", r.tsTreeSitterLint)

    // Global fallback: tree-sitter syntax check
    r.SetGlobalLinter(r.treeSitterBasicLint)
}
```

**File:** `internal/lint/registry.go` (NEW)

---

### 2. Tree-sitter Basic Lint (`internal/lint/treelint.go`)

**Purpose:** Fast syntax validation using tree-sitter error detection.

```go
package lint

import (
    "fmt"
    "strings"

    "github.com/smacker/go-tree-sitter"
)

// treeSitterBasicLint checks for syntax errors using tree-sitter
func (r *Registry) treeSitterBasicLint(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
    lang := detectLanguage(filePath)
    parser := tree-sitter.NewParser()

    tsLang, err := getTreeSitterLanguage(lang)
    if err != nil {
        return nil, nil // Skip if language not supported
    }

    parser.SetLanguage(tsLang)

    tree, err := parser.ParseCtx(ctx, nil, []byte(content))
    if err != nil {
        return []LinterResult{{
            File:     filePath,
            Line:     0,
            Message:  fmt.Sprintf("Parse error: %v", err),
            Severity: "error",
        }}, nil
    }

    // Check for ERROR nodes in the parse tree
    var results []LinterResult
    cursor := tree.Walk()
    traverseTree(&cursor, &results, filePath, content)

    return results, nil
}

func traverseTree(cursor *tree-sitter.TreeCursor, results *[]LinterResult, file, content string) {
    node := cursor.CurrentNode()

    // Check for ERROR nodes (syntax errors)
    if node.Type() == "ERROR" || node.IsMissing() {
        startPos := node.StartPoint()
        endPos := node.EndPoint()

        *results = append(*results, LinterResult{
            File:     file,
            Line:     int(startPos.Row),  // 0-based
            Column:   int(startPos.Column),
            Message:  fmt.Sprintf("Syntax error: unexpected token at line %d, column %d", startPos.Row+1, startPos.Column+1),
            Severity: "error",
        })
    }

    // Recurse into children
    if cursor.GotoFirstChild() {
        traverseTree(cursor, results, file, content)
        cursor.GotoParent()
    }

    if cursor.GotoNextSibling() {
        traverseTree(cursor, results, file, content)
        cursor.GotoParent()
    }
}

// goTreeSitterLint is Go-specific tree-sitter linting
func (r *Registry) goTreeSitterLint(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
    return r.treeSitterBasicLint(ctx, filePath, relPath, content)
}
```

**File:** `internal/lint/treelint.go` (NEW)

---

### 3. Language-Specific Linters (`internal/lint/languages/*.go`)

**Purpose:** Implement language-specific linting chains.

#### Go Linters (`internal/lint/languages/go_lint.go`)

```go
package languages

import (
    "bytes"
    "context"
    "fmt"
    "go/parser"
    "go/token"
    "os/exec"
    "regexp"
    "strconv"
    "strings"

    "github.com/caimlas/meept/internal/lint"
)

// goCompileCheck runs Go compilation as a lint check
func (r *Registry) goCompileCheck(ctx context.Context, filePath, relPath, content string) ([]lint.LinterResult, error) {
    // Write content to temp file for compilation
    tmpFile, err := createTempFile(filePath, content)
    if err != nil {
        return nil, err
    }
    defer cleanupTempFile(tmpFile)

    // Run `go build` or `go vet`
    cmd := exec.CommandContext(ctx, "go", "build", "-o", "/dev/null", tmpFile)
    var stderr bytes.Buffer
    cmd.Stderr = &stderr

    err = cmd.Run()
    if err == nil {
        return nil, nil
    }

    // Parse Go compiler errors
    return parseGoErrors(stderr.String(), filePath)
}

func parseGoErrors(output, targetFile string) ([]lint.LinterResult, error) {
    // Go error format: ./file.go:line:column: error message or file.go:line: error message
    errorPattern := regexp.MustCompile(`([^:]+):(\d+):(\d+)?\s*:\s*(.+)`)
    simplePattern := regexp.MustCompile(`([^:]+):(\d+):\s*(.+)`)

    var results []lint.LinterResult
    lines := strings.Split(output, "\n")

    for _, line := range lines {
        line = strings.TrimSpace(line)
        if line == "" {
            continue
        }

        var result lint.LinterResult

        if matches := errorPattern.FindStringSubmatch(line); matches != nil {
            lineNum, _ := strconv.Atoi(matches[2])
            colNum := 0
            if matches[3] != "" {
                colNum, _ = strconv.Atoi(matches[3])
            }
            result = lint.LinterResult{
                File:     targetFile,
                Line:     lineNum - 1,  // Convert to 0-based
                Column:   colNum - 1,
                Message:  matches[4],
                Severity: "error",
            }
        } else if matches := simplePattern.FindStringSubmatch(line); matches != nil {
            lineNum, _ := strconv.Atoi(matches[2])
            result = lint.LinterResult{
                File:     targetFile,
                Line:     lineNum - 1,
                Message:  matches[3],
                Severity: "error",
            }
        }

        if result.File != "" {
            results = append(results, result)
        }
    }

    return results, nil
}

// goVet runs `go vet` for semantic issues
func (r *Registry) goVet(ctx context.Context, dirPath, relPath, _ string) ([]lint.LinterResult, error) {
    cmd := exec.CommandContext(ctx, "go", "vet", "./...")
    cmd.Dir = dirPath
    var stderr bytes.Buffer
    cmd.Stderr = &stderr

    _ = cmd.Run()  // go vet returns non-zero on warnings

    return parseGoErrors(stderr.String(), relPath)
}
```

#### Python Linters (`internal/lint/languages/python_lint.go`)

```go
package languages

import (
    "bytes"
    "context"
    "regexp"
    "strconv"
    "strings"

    "github.com/caimlas/meept/internal/lint"
)

// pythonCompileCheck runs Python syntax check
func (r *Registry) pythonCompileCheck(ctx context.Context, filePath, relPath, content string) ([]lint.LinterResult, error) {
    cmd := exec.CommandContext(ctx, "python3", "-m", "py_compile", filePath)
    var stderr bytes.Buffer
    cmd.Stderr = &stderr

    err := cmd.Run()
    if err == nil {
        return nil, nil
    }

    return parsePythonErrors(stderr.String(), filePath)
}

// pythonFlake8 runs flake8 with fatal-only error codes
func (r *Registry) pythonFlake8(ctx context.Context, filePath, relPath, content string) ([]lint.LinterResult, error) {
    // E9, F821, F823, F831, F406, F407, F701, F702, F704, F706
    // These are syntax errors and undefined names
    cmd := exec.CommandContext(ctx, "flake8",
        "--isolated",
        "--select=E9,F821,F823,F831,F406,F407,F701,F702,F704,F706",
        "--show-source",
        filePath)

    var stderr bytes.Buffer
    cmd.Stderr = &stderr

    err := cmd.Run()
    if err == nil {
        return nil, nil
    }

    return parsePythonErrors(stderr.String(), filePath)
}

func parsePythonErrors(output, targetFile string) ([]lint.LinterResult, error) {
    // Flake8 format: filename.py:line:column: error_code message
    pattern := regexp.MustCompile(`([^:]+):(\d+):(\d+):\s*(\w+)\s+(.+)`)

    var results []lint.LinterResult
    lines := strings.Split(output, "\n")

    for _, line := range lines {
        line = strings.TrimSpace(line)
        if matches := pattern.FindStringSubmatch(line); matches != nil {
            lineNum, _ := strconv.Atoi(matches[2])
            colNum, _ := strconv.Atoi(matches[3])

            results = append(results, lint.LinterResult{
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
```

**File:** `internal/lint/languages/go_lint.go` (NEW)
**File:** `internal/lint/languages/python_lint.go` (NEW)
**File:** `internal/lint/languages/js_lint.go` (NEW)

---

### 4. Test Runner (`internal/lint/testrunner.go`)

**Purpose:** Execute tests and parse failures.

```go
package lint

import (
    "context"
    "encoding/json"
    "fmt"
    "os/exec"
    "regexp"
    "strings"
    "time"
)

// TestResult represents a single test execution result
type TestResult struct {
    Name      string
    File      string
    Line      int
    Passed    bool
    Skipped   bool
    Error     string
    Duration  time.Duration
    Output    string  // stdout/stderr from test
}

// TestRunner executes language-specific test commands
type TestRunner struct {
    config *TestConfig
    logger *slog.Logger
}

// TestConfig holds test runner configuration
type TestConfig struct {
    GoTestFlags     []string  // e.g., ["-race", "-count=1"]
    PytestFlags     []string  // e.g., ["-x", "-v"]
    JestFlags       []string  // e.g., ["--passWithNoTests"]
    Timeout         time.Duration
    MaxOutputLines  int  // Truncate output to N lines
}

// RunTests executes tests for the given files or directory
func (tr *TestRunner) RunTests(ctx context.Context, lang, dirPath string, testFiles []string) ([]TestResult, error) {
    switch lang {
    case "go":
        return tr.runGoTests(ctx, dirPath, testFiles)
    case "python":
        return tr.runPytestTests(ctx, dirPath, testFiles)
    case "javascript":
        return tr.runJestTests(ctx, dirPath, testFiles)
    default:
        return nil, fmt.Errorf("no test runner for language: %s")
    }
}

// runGoTests runs Go tests with JSON output parsing
func (tr *TestRunner) runGoTests(ctx context.Context, dirPath string, testFiles []string) ([]TestResult, error) {
    args := []string{"test", "-json"}
    args = append(args, tr.config.GoTestFlags...)

    // If specific test files, add them
    for _, f := range testFiles {
        if strings.HasSuffix(f, "_test.go") {
            pkg := extractPackage(f)
            args = append(args, pkg)
        }
    }
    if len(testFiles) == 0 {
        args = append(args, "./...")
    }

    cmd := exec.CommandContext(ctx, "go", args...)
    cmd.Dir = dirPath
    stdout, err := cmd.StdoutPipe()
    if err != nil {
        return nil, err
    }

    var stderr strings.Builder
    cmd.Stderr = &stderr

    if err := cmd.Start(); err != nil {
        return nil, err
    }

    // Parse JSON test output
    decoder := json.NewDecoder(stdout)
    var results []TestResult
    testOutput := make(map[string]*strings.Builder)  // Aggregate output per test

    for {
        var event struct {
            Time    string
            Action  string  // "run", "pass", "fail", "skip", "output"
            Package string
            Test    string
            Output  string
        }

        if err := decoder.Decode(&event); err != nil {
            if err == io.EOF {
                break
            }
            return nil, err
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
                continue  // Package-level event
            }

            result := TestResult{
                Name:   event.Test,
                File:   findTestFile(event.Package, event.Test),
                Passed: event.Action == "pass",
                Skipped: event.Action == "skip",
                Output: testOutput[event.Package+"/"+event.Test].String(),
            }

            if !result.Passed && !result.Skipped {
                result.Error = "Test failed"
            }

            results = append(results, result)
        }
    }

    if err := cmd.Wait(); err != nil && stderr.Len() > 0 {
        // Build error from stderr (compilation errors, etc.)
        results = append(results, TestResult{
            Name:   "build",
            Error:  stderr.String(),
            Passed: false,
        })
    }

    return results, nil
}

// runPytestTests runs pytest with JSON output
func (tr *TestRunner) runPytestTests(ctx context.Context, dirPath string, testFiles []string) ([]TestResult, error) {
    args := []string{"--json-report", "-v"}
    args = append(args, tr.config.PytestFlags...)

    if len(testFiles) > 0 {
        args = append(args, testFiles...)
    } else {
        args = append(args, "./tests")
    }

    cmd := exec.CommandContext(ctx, "pytest", args...)
    cmd.Dir = dirPath

    stdout, err := cmd.Output()
    if err != nil {
        // pytest returns non-zero on test failures, which is expected
        if exitErr, ok := err.(*exec.ExitError); ok {
            // Parse stderr for failures
            return parsePytestFailures(string(exitErr.Stderr), string(stdout))
        }
        return nil, err
    }

    return parsePytestResults(string(stdout))
}
```

**File:** `internal/lint/testrunner.go` (NEW)

---

### 5. Reflection Loop Engine (`internal/agent/reflection.go`)

**Purpose:** Manage the auto-fix iteration loop.

```go
package agent

import (
    "context"
    "fmt"
    "strings"

    "github.com/caimlas/meept/internal/lint"
    "github.com/caimlas/meept/internal/llm"
)

// ReflectionConfig holds reflection loop parameters
type ReflectionConfig struct {
    MaxReflections  int     // Default: 3
    AutoLint        bool    // Enable auto-linting
    AutoTest        bool    // Enable auto-testing
    LintCmd         string  // Custom lint command (optional)
    TestCmd         string  // Custom test command (optional)
}

// ReflectionEngine manages the auto-fix loop
type ReflectionEngine struct {
    config       ReflectionConfig
    linter       *lint.Registry
    testRunner   *lint.TestRunner
    llmClient    llm.Client
    logger       *slog.Logger
}

// ReflectionResult holds the outcome of a reflection cycle
type ReflectionResult struct {
    Fixed         bool
    Iterations    int
    LintErrors    []lint.LinterResult
    TestFailures  []lint.TestResult
    FinalMessage  string
    GaveUp        bool  // True if max reflections reached without fix
}

// RunReflection executes the reflection loop after code edits
func (re *ReflectionEngine) RunReflection(ctx context.Context, editedFiles []string, workDir string) (*ReflectionResult, error) {
    result := &ReflectionResult{}

    for i := 0; i < re.config.MaxReflections; i++ {
        result.Iterations = i + 1

        // Step 1: Run linters
        if re.config.AutoLint {
            lintErrors, err := re.runLinters(ctx, editedFiles)
            if err != nil {
                return nil, fmt.Errorf("linter failed: %w", err)
            }

            if len(lintErrors) > 0 {
                result.LintErrors = append(result.LintErrors, lintErrors...)

                // Step 2: Ask LLM to fix
                fixRequest := re.formatLintFixRequest(lintErrors, workDir)
                fixApplied, err := re.requestFix(ctx, fixRequest, editedFiles)
                if err != nil || !fixApplied {
                    result.FinalMessage = fmt.Sprintf("Failed to apply fix after %d iterations", i+1)
                    result.GaveUp = true
                    return result, nil
                }
                continue  // Retry linting after fix
            }
        }

        // Step 3: Run tests
        if re.config.AutoTest {
            testFailures, err := re.runTests(ctx, workDir, editedFiles)
            if err != nil {
                return nil, fmt.Errorf("tests failed: %w", err)
            }

            if len(testFailures) > 0 {
                result.TestFailures = append(result.TestFailures, testFailures...)

                // Step 4: Ask LLM to fix
                fixRequest := re.formatTestFixRequest(testFailures, workDir)
                fixApplied, err := re.requestFix(ctx, fixRequest, editedFiles)
                if err != nil || !fixApplied {
                    result.FinalMessage = fmt.Sprintf("Failed to fix test failures after %d iterations", i+1)
                    result.GaveUp = true
                    return result, nil
                }
                continue  // Retry tests after fix
            }
        }

        // Success: no errors
        result.Fixed = true
        result.FinalMessage = "All checks passed"
        return result, nil
    }

    // Max reflections reached
    result.GaveUp = true
    result.FinalMessage = fmt.Sprintf("Gave up after %d reflection iterations", re.config.MaxReflections)
    return result, nil
}

// requestFix sends error context to LLM and applies the fix
func (re *ReflectionEngine) requestFix(ctx context.Context, fixRequest string, editedFiles []string) (bool, error) {
    // Build prompt with error context and file contents
    prompt := fmt.Sprintf(`I encountered errors in the code. Please fix them.

%s

Edited files:
%s

Provide the corrected code.`, fixRequest, strings.Join(editedFiles, ", "))

    // Get fix from LLM
    response, err := re.llmClient.Complete(ctx, prompt)
    if err != nil {
        return false, err
    }

    // Parse and apply edits from response
    // (Use existing edit parsing infrastructure)
    if err := re.applyEdits(response, editedFiles); err != nil {
        return false, err
    }

    return true, nil
}

// formatLintFixRequest creates a formatted prompt for lint errors
func (re *ReflectionEngine) formatLintFixRequest(errors []lint.LinterResult, workDir string) string {
    var sb strings.Builder
    sb.WriteString("# Fix any errors below, if possible.\n\n")

    for _, err := range errors {
        sb.WriteString(fmt.Sprintf("## %s:%d:%d\n", err.File, err.Line+1, err.Column+1))
        sb.WriteString(fmt.Sprintf("Error: %s\n\n", err.Message))
    }

    // Add tree context for each file with errors
    filesWithErrors := uniqueFiles(errors)
    for _, file := range filesWithErrors {
        fileErrors := filterErrors(errors, file)
        ctx := re.buildTreeContext(file, fileErrors)
        sb.WriteString(ctx)
        sb.WriteString("\n")
    }

    return sb.String()
}

// formatTestFixRequest creates a formatted prompt for test failures
func (re *ReflectionEngine) formatTestFixRequest(failures []lint.TestResult, workDir string) string {
    var sb strings.Builder
    sb.WriteString("# Fix the failing tests below.\n\n")

    for _, f := range failures {
        if !f.Passed && !f.Skipped {
            sb.WriteString(fmt.Sprintf("## Test: %s\n", f.Name))
            sb.WriteString(fmt.Sprintf("File: %s\n", f.File))
            sb.WriteString(fmt.Sprintf("Error: %s\n\n", f.Error))
            if f.Output != "" {
                sb.WriteString("Output:\n```\n")
                sb.WriteString(truncate(f.Output, 2000))
                sb.WriteString("\n```\n\n")
            }
        }
    }

    return sb.String()
}

// buildTreeContext generates tree-sitter context for a file with error markers
func (re *ReflectionEngine) buildTreeContext(filePath string, errors []lint.LinterResult) string {
    // Use existing ast.TreeContext with error line highlighting
    errorLines := make(map[int]bool)
    for _, e := range errors {
        errorLines[e.Line] = true
    }

    // TreeContext shows ~3 lines of padding around each significant line
    ctx := ast.TreeContextWithMarkers(filePath, errorLines, 3)
    return ctx.String()
}
```

**File:** `internal/agent/reflection.go` (NEW)

---

### 6. Integration with Agent Loop

**Purpose:** Wire reflection into the main agent loop.

```go
// In internal/agent/orchestrator.go, modify the edit application flow:

type Orchestrator struct {
    // ... existing fields ...
    reflectionEngine *ReflectionEngine
    linter           *lint.Registry
    testRunner       *lint.TestRunner
}

func (o *Orchestrator) Run(ctx context.Context, task *Task) (*TaskResult, error) {
    // ... existing setup ...

    // Main iteration loop
    for iteration := 0; iteration < o.config.MaxIterations; iteration++ {
        // ... existing LLM interaction and edit application ...

        // NEW: Run reflection loop after edits
        if o.config.ReflectionEnabled {
            o.logger.Debug("Starting reflection loop", "iteration", iteration)

            result, err := o.reflectionEngine.RunReflection(ctx, o.currentEditedFiles, o.workDir)
            if err != nil {
                o.logger.Warn("Reflection loop failed", "error", err)
                // Continue without fixing
            } else {
                o.logger.Info("Reflection completed",
                    "fixed", result.Fixed,
                    "iterations", result.Iterations,
                    "gave_up", result.GaveUp,
                )

                // Add reflection summary to conversation context
                if !result.Fixed {
                    o.addSystemMessage(fmt.Sprintf(
                        "⚠️ Auto-fix gave up after %d iterations:\n%s",
                        result.Iterations,
                        result.FinalMessage,
                    ))
                }
            }
        }

        // ... existing completion checks ...
    }

    return result, nil
}
```

**File:** `internal/agent/orchestrator.go` (MODIFY)

---

### 7. Tree Context with Error Markers

**Purpose:** Show code structure with error locations highlighted.

```go
// Add to internal/code/ast/treectx.go:

// TreeContextWithMarkers shows code structure with specific lines marked
func TreeContextWithMarkers(filePath string, markedLines map[int]bool, padding int) string {
    // Parse the file
    parser := getParser(filePath)
    tree := parser.ParseFile(filePath)

    // Collect marked lines with context
    var ctxLines []int
    for line := range markedLines {
        for i := line - padding; i <= line + padding; i++ {
            if i >= 0 {
                ctxLines = append(ctxLines, i)
            }
        }
    }
    ctxLines = uniqueSorted(ctxLines)

    // Build tree-aware context
    var sb strings.Builder
    cursor := tree.Walk()

    for _, line := range ctxLines {
        // Navigate to node containing this line
        node := findNodeAtLine(cursor, line)

        // Show structural context (class/function scope)
        indent := getNodeIndent(node)
        sb.WriteString(indent)
        sb.WriteString(getNodeSignature(node))
        sb.WriteString(":\n")
    }

    // Mark error lines with █ symbol
    // Format: lines show signature + "█" for error locations

    return sb.String()
}
```

**File:** `internal/code/ast/treectx.go` (MODIFY)

---

## Configuration Schema

Add to `config/meept.json5`:

```json5
{
  agent: {
    reflection: {
      enabled: true,
      max_reflections: 3,
      auto_lint: true,
      auto_test: true,
      lint_cmd: "",  // Optional custom lint command (e.g., "golangci-lint run")
      test_cmd: "",  // Optional custom test command
    },
    lint: {
      go_flags: ["-race", "-count=1"],
      pytest_flags: ["-x", "-v"],
      jest_flags: ["--passWithNoTests"],
      timeout_seconds: 300,
      max_output_lines: 500,
    },
  },
}
```

---

## Testing Plan

### Unit Tests

1. **Linter Registry tests**
   - Language-specific linter registration
   - Global fallback behavior
   - Error parsing accuracy

2. **Tree-sitter Lint tests**
   - Syntax error detection for Go, Python, JS
   - ERROR node identification
   - Missing node detection

3. **Test Runner tests**
   - Go test JSON output parsing
   - Pytest result parsing
   - Timeout handling

4. **Reflection Engine tests**
   - Lint → Fix → Lint cycle
   - Test → Fix → Test cycle
   - Max reflections behavior

### Integration Tests

1. End-to-end edit → lint → fix cycle
2. End-to-end edit → test → fix cycle
3. Reflection loop with real LLM
4. Custom lint/test command integration

---

## Debugger Agent Integration

The reflection engine is **critical** for the Debugger agent:

```go
// In internal/agent/debugger.go:

type DebuggerAgent struct {
    // ... existing fields ...
    reflectionEngine *agent.ReflectionEngine
}

// DebugTask runs debugging with auto-fix attempts
func (d *DebuggerAgent) DebugTask(ctx context.Context, bugReport string) (*DebugResult, error) {
    // 1. Reproduce the bug (run failing tests)
    testFailures, err := d.reproTests.Run(ctx, d.workDir)

    // 2. Analyze error output
    analysis, err := d.analyzeFailures(testFailures)

    // 3. Generate fix hypothesis
    fix, err := d.generateFix(ctx, analysis)

    // 4. Apply fix
    if err := d.applyFix(fix); err != nil {
        return nil, err
    }

    // 5. NEW: Run reflection loop
    reflectionResult, err := d.reflectionEngine.RunReflection(ctx, d.editedFiles, d.workDir)
    if err != nil {
        return nil, err
    }

    // 6. Verify fix
    if reflectionResult.Fixed {
        return &DebugResult{
            Fixed: true,
            Message: "Bug fixed and verified",
        }, nil
    }

    return &DebugResult{
        Fixed: false,
        Message: reflectionResult.FinalMessage,
        Iterations: reflectionResult.Iterations,
    }, nil
}
```

**File:** `internal/agent/debugger.go` (MODIFY)

---

## Metrics Integration

Add to existing metrics schema (`internal/metrics/store.go`):

```sql
-- Linting and test metrics
CREATE TABLE IF NOT EXISTS lint_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    task_id TEXT,
    agent_id TEXT,
    language TEXT,
    files_checked INTEGER,
    linters_runned INTEGER,
    errors_found INTEGER,
    errors_fixed INTEGER,
    duration_ms INTEGER,
    reflection_iterations INTEGER,
    success BOOLEAN
);

CREATE TABLE IF NOT EXISTS test_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    task_id TEXT,
    agent_id TEXT,
    language TEXT,
    tests_run INTEGER,
    tests_passed INTEGER,
    tests_failed INTEGER,
    tests_skipped INTEGER,
    duration_ms INTEGER,
    reflection_iterations INTEGER,
    success BOOLEAN
);
```

**File:** `internal/metrics/store.go` (MODIFY)

---

## Success Criteria

- [ ] Auto-lint runs after every code edit (when enabled)
- [ ] Auto-test runs after successful linting (when enabled)
- [ ] Reflection loop fixes >70% of lint/test failures without human intervention
- [ ] LLM successfully fixes errors in ≤2 iterations on average
- [ ] Proper error formatting with tree context for LLM understanding
- [ ] Configurable max reflections (default: 3)
- [ ] Custom lint/test commands supported
- [ ] Metrics collected for lint/test/reflection performance
- [ ] Integrated with Debugger agent workflow
- [ ] Comprehensive test coverage (>80%)

---

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Linter takes too long | Timeout enforcement, parallel execution |
| LLM enters infinite fix loop | Max reflections limit, failure detection |
| Test output too verbose | Truncation, intelligent filtering |
| LLM can't parse errors | Structured error format, tree context |
| False positive lint errors | Configurable linter strictness |

---

## Related Documentation

- `docs/workflows/debugger-agent.md` — Debugger agent workflow
- `docs/reference/tools.md` — Tool execution framework
- `internal/code/ast/` — Tree-sitter infrastructure
