# Reflection: Auto Lint/Test Fix Loop

## Overview

Reflection is an automated quality assurance system that validates code changes immediately after edits and requests LLM-driven fixes when lint errors or test failures are detected.

**Key insight:** Reflection operates at the **code-edit level**, not the task level. It provides immediate feedback on code quality before the agent proceeds to other work.

## Problem

Agents that write code need systematic validation:

1. **Syntax errors** - LLM-generated code may have syntax mistakes
2. **Lint violations** - Code style and correctness issues
3. **Test failures** - Changes may break existing functionality
4. **Self-correction** - Agents should fix their own mistakes before moving on

Without reflection, every code change requires human review. With reflection, routine errors are caught and fixed automatically.

## Architecture

### Components

| Component | Location | Purpose |
|-----------|----------|---------|
| `ReflectionEngine` | `internal/agent/reflection.go` | Manages the validation loop |
| `lint.Registry` | `internal/lint/registry.go` | Language-specific linters |
| `lint.TestRunner` | `internal/lint/testrunner.go` | Test execution and parsing |
| `Orchestrator` | `internal/agent/orchestrator.go` | Integration point, external retry |

### System Flow

```
file_edit tool executed
         ↓
Orchestrator detects edit event
         ↓
Call ReflectionEngine.RunReflection()
         ↓
    ┌─────────────────┐
    │  Run Linters    │
    │  (tree-sitter,  │
    │   compile, vet) │
    └────────┬────────┘
             │
    ┌────────▼────────┐
    │ Lint Errors?    │───Yes───► Request LLM Fix ──► Return PendingFix
    └────────┬────────┘
             │ No
    ┌────────▼────────┐
    │   Run Tests     │
    └────────┬────────┘
             │
    ┌────────▼────────┐
    │ Test Failures?  │───Yes───► Request LLM Fix ──► Return PendingFix
    └────────┬────────┘
             │ No
    ┌────────▼────────┐
    │  Fixed = true   │
    └────────┬────────┘
             │
         Return
```

### Orchestrator Retry Pattern

The `ReflectionEngine` always returns after **one pass** (single iteration). The orchestrator implements **external retry** with a hardcoded 3-pass maximum:

1. **Pass 1**: Call `RunReflection()` on edited files → if `PendingFix`, apply it
2. **Pass 2**: Call `RunReflection()` on fixed files → if `PendingFix`, apply it
3. **Pass 3**: Apply one final fix **without re-verification**

This design separates concerns:
- **ReflectionEngine**: Diagnose errors, request fixes
- **Orchestrator**: Apply fixes, manage retry count

## Linter Pipeline

### Tree-sitter Syntax Validation

Fast syntax checking using tree-sitter parse trees:

```go
// Detects ERROR nodes in parse tree
func (r *Registry) treeSitterBasicLint(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error)
```

**Supported languages:** Go, Python, JavaScript, TypeScript, and any language with tree-sitter grammar.

### Language-Specific Linters

**Go:**
- `goTreeSitterLint` - Syntax validation
- `goCompileCheck` - Compilation check (`go build`)
- `goVet` - Semantic analysis (`go vet`)

**Python:**
- `pythonTreeSitterLint` - Syntax validation
- `pythonCompileCheck` - Syntax check (`python3 -m py_compile`)
- `pythonFlake8` - Fatal errors only (E9, F821, F823, F831, F406, F407, F701, F702, F704, F706)

**JavaScript/TypeScript:**
- `jsTreeSitterLint` / `tsTreeSitterLint` - Syntax validation

## Test Runner

Executes language-specific test commands and parses failures:

**Go:** `go test -json` with structured event parsing
**Python:** `pytest --json-report` or stderr parsing
**JavaScript:** `jest --passWithNoTests` with JSON output

### Test Result Structure

```go
type TestResult struct {
    Name     string  // Test name
    File     string  // Test file location
    Line     int     // Line number
    Passed   bool    // Success/failure
    Skipped  bool    // Skipped tests
    Error    string  // Failure message
    Duration time.Duration
    Output   string  // stdout/stderr
}
```

## LLM Fix Request Flow

When errors are found, reflection formats a fix request:

### Lint Fix Prompt

```
# Fix any errors below, if possible.

## file.go:10:5
Error (SA5001): nil pointer dereference

## Context for file.go
Lines 7-13:
  7: func getUser(id int) *User {
  8:     var u *User
>> 9:     u.Name = "test"  // ERROR: nil pointer
 10:    return u
 11: }
```

### Test Fix Prompt

```
# Fix the failing tests below.

## Test: TestGetUser
File: handler_test.go
Error: Expected 200, got 500

Output:
--- FAIL: TestGetUser (0.00s)
    handler_test.go:15: Expected status 200, got 500
    handler_test.go:18: Response body: {"error": "nil pointer"}
```

### LLM Response Format

The prompt instructs the LLM:

> "Use the file_edit tool to apply fixes, or if you're providing code directly, format it as a complete patch with the file path and corrected content."

**Expected format:**

````
```go
// File: handler.go
func getUser(id int) *User {
    u := &User{}
    u.Name = "test"
    return u
}
```
````

**Fixed:** `parseFixResponse` now correctly parses per-file code blocks and tool call JSON. Only referenced files are targeted for fixes.

See: `docs/plans/2026-06-12-review-gaps-research-design.md` - Task 2 (resolved)

## Configuration

```json5
{
  agent: {
    reflection: {
      enabled: true,
      max_reflections: 3,  // Deprecated: Use orchestrator's external retry (3-pass fixed)
      auto_lint: true,
      auto_test: true,
    },
  }
}
```

**Note:** `max_reflections` is deprecated. The orchestrator's hardcoded 3-pass retry is the actual behavior.

## When Reflection Runs

| Trigger | Reflection Runs? |
|---------|-----------------|
| `file_write` tool | No - only validates write success |
| `file_edit` tool | Yes - validates code quality |
| `shell_execute` (non-test) | No |
| `shell_execute` (test command) | No - reflection runs after edits, not shell |

**Integration point:** `internal/agent/orchestrator.go:627-744` - `handleToolExecutionComplete()` subscribes to `tool.execution.complete` bus events where `ToolName == "file_edit"` and `Success == true`.

## Relationship to Ralph Loop

Reflection and Ralph Loop are **complementary** verification layers at different abstraction levels:

| Aspect | Reflection | Ralph Loop |
|--------|-----------|------------|
| **Layer** | Code-edit validation | Task-completion validation |
| **Trigger** | After `file_edit` tool | After job completion |
| **What it checks** | Lint errors, test failures | Evidence of task success |
| **Fix mechanism** | LLM code fix via `PendingFix` | Replan with new task steps |
| **Iteration** | Immediate (synchronous with edit) | Deferred (after job completes) |
| **Max iterations** | 3 (orchestrator-controlled) | 3 (configurable via `MaxIterations`) |
| **Success criteria** | No lint/test errors | Evidence mentions task keywords |

### Example Workflow

```
User: "Fix the login bug"
    ↓
Planner creates task with steps
    ↓
Worker executes step: "Edit login.go to fix session timeout"
    ↓
[LATENCY INSERTED BY SYSTEM]
    ↓
file_edit tool applied
    ↓
Reflection runs: go vet, go test ./...
    ↓
Test fails: TestLoginTimeout still failing
    ↓
Reflection returns PendingFix
    ↓
Orchestrator applies fix, re-runs reflection
    ↓
Tests pass
    ↓
Job completes
    ↓
Ralph Loop checks: Does result mention "login", "timeout", "session"?
    ↓
Yes → Task complete
No  → Trigger replan
```

## Known Issues

### 1. Orchestrator-Managed Retry (Fixed)

**Status:** Fixed - single-pass design with orchestrator retry

The `RunReflection` function now explicitly executes a **single pass**. The previous `MaxReflections` loop was removed. Multi-pass retry is handled by the orchestrator externally (`orchestrator.go:654-744`):

1. Call `RunReflection()` -> apply `PendingFix`
2. Re-call `RunReflection()` on fixed files
3. Apply one final fix **without re-verification**

This design separates concerns:
- **ReflectionEngine**: Diagnose errors, request fixes (single pass)
- **Orchestrator**: Apply fixes, manage retry count

### 2. `parseFixResponse` File Filtering (Fixed)

**Status:** Fixed - now parses per-file code blocks

The `parseFixResponse` function now correctly:
1. Parses `file_edit` tool call JSON blocks to extract `filepath` from arguments
2. Parses markdown code blocks with file path annotations (`// File: path`, `## File: path`)
3. Falls back to filename mention heuristic if no structured format found
4. Returns only files actually referenced in the LLM response

Tests added:
- `TestParseFixResponse_MultiFile_ParsesPerFileCodeBlocks`
- `TestParseFixResponse_FiltersUnreferencedFiles`
- `TestParseFixResponse_ToolCallJSON`

### 3. Test Coverage Gaps

**Status:** Partially addressed

Tests still needed:
- `applyFix` file application (integration test with orchestrator)
- `extractCodeFromMarkdown` code block extraction
- End-to-end reflection loop with mock LLM

## Related Documentation

- [Auto Lint/Test with Reflection Loop Implementation Plan](../plans/20260609-auto-lint-test-reflection-implementation.md)
- [Review Gaps Research](../plans/2026-06-12-review-gaps-research-design.md) - Task 1, Task 2
- [Ralph Loop](./ralph-loop.md) - Task-level verification
- [Multi-Agent Orchestration](../workflows/agent-orchestration.md) - Orchestrator integration

## Files

- `internal/agent/reflection.go` - ReflectionEngine implementation
- `internal/agent/orchestrator.go` - Integration (lines 627-744)
- `internal/lint/registry.go` - Linter registry
- `internal/lint/testrunner.go` - Test execution
- `internal/lint/treelint.go` - Tree-sitter syntax validation
- `internal/lint/languages/*.go` - Language-specific linters
- `internal/agent/reflection_test.go` - Unit tests (partial coverage)
