# Leaf 05-01: Per-Tool Declared Safety

## DISPATCH INSTRUCTION
Implement all tasks below. Do NOT commit. Do NOT run git add. Write code, run tests, report results only. See SHARED-CONVENTIONS.md for coding standards.

**Parent:** 05-combined-improvements/orchestrator.md
**Scope:** Add IsReadOnly and IsConcurrencySafe methods to the Tool interface with default implementations. Update the executor to use tool-declared safety instead of name-based heuristics. Migrate existing tools to declare their own safety.
**Dependencies:** None (soft dependency on Tree 01 for interface alignment — this leaf defines the interface extension itself)
**Estimated Context:** ~70K

## Interface Contract

This leaf exposes:
- `IsReadOnly(input map[string]any) bool` on `tools.Tool` interface
- `IsConcurrencySafe(input map[string]any) bool` on `tools.Tool` interface
- `ToolDefaults` embedded struct with fail-closed defaults (both return false)
- Updated `DependencyInferrer` and `AdaptiveParallelismLimiter` to use declared safety

## Tasks

### Task 1: Add methods to Tool interface with defaults

**File:** `internal/tools/interface.go`

Read the existing Tool interface. Add two methods:

```go
type Tool interface {
    // ... existing methods (Name, Category, Description, Parameters, Execute) ...

    // IsReadOnly returns true if the tool call with the given input
    // does not modify any state (files, memory, git, processes).
    // Default: false (assume writes — fail-closed).
    IsReadOnly(input map[string]any) bool

    // IsConcurrencySafe returns true if the tool call with the given input
    // can safely run concurrently with other tool calls.
    // Default: false (assume not safe — fail-closed).
    IsConcurrencySafe(input map[string]any) bool
}
```

Add a `ToolDefaults` struct that tools can embed for default implementations:

```go
// ToolDefaults provides fail-closed default implementations for optional
// Tool interface methods. Embed this in tool structs to get defaults,
// then override specific methods as needed.
type ToolDefaults struct{}

func (ToolDefaults) IsReadOnly(map[string]any) bool      { return false }
func (ToolDefaults) IsConcurrencySafe(map[string]any) bool { return false }
```

### Task 2: Update all builtin tools to embed ToolDefaults

**Files:** All files in `internal/tools/builtin/` that define tool structs

For each tool struct, add `tools.ToolDefaults` embedding. Then override for tools that ARE read-only or concurrency-safe:

Read-only tools (override IsReadOnly to return true):
- `file_read` — always read-only
- `file_grep` — always read-only
- `file_find` — always read-only
- `list_directory` — always read-only
- `memory_search` — always read-only
- `memory_get_context` — always read-only
- `memory_get_version` — always read-only
- `memory_get_version_history` — always read-only
- `web_search` — always read-only
- `web_fetch` — always read-only
- `task_list` — always read-only
- `task_get` — always read-only
- `schedule_list` — always read-only
- `schedule_get` — always read-only
- `template_list` — always read-only

Concurrency-safe tools (override IsConcurrencySafe to return true):
- All read-only tools above (read-only implies concurrency-safe)
- `file_write` — safe if different file paths (input-dependent)
- `file_edit` — safe if different file paths (input-dependent)

For `file_write` and `file_edit`, implement input-dependent safety:
```go
func (t *FileWriteTool) IsReadOnly(map[string]any) bool { return false }
func (t *FileWriteTool) IsConcurrencySafe(input map[string]any) bool {
    // Safe to run concurrently — the dependency inferrer handles
    // file-path conflicts via its existing overlap detection.
    return true
}
```

For `shell`, implement input-dependent read-only detection:
```go
func (t *ShellTool) IsReadOnly(input map[string]any) bool {
    cmd, _ := input["command"].(string)
    return t.classifyRisk(cmd) <= RiskMedium && t.isReadOnlyCommand(cmd)
}
func (t *ShellTool) IsConcurrencySafe(input map[string]any) bool {
    return t.IsReadOnly(input)
}
```

### Task 3: Update DependencyInferrer to use declared safety

**File:** `internal/agent/tool_dependency.go`

Read the existing `DependencyInferrer`. It currently uses name-based heuristics (`isWriteTool`, `isReadTool` based on name substring matching). Update to prefer tool-declared safety:

```go
func (d *DependencyInferrer) isReadOnly(toolName string, input map[string]any) bool {
    if tool, ok := d.registry.Get(toolName); ok {
        return tool.IsReadOnly(input)
    }
    // Fallback to name-based heuristic for unknown tools
    return d.isReadToolByName(toolName)
}
```

Keep the name-based heuristic as a fallback for tools not in the registry (e.g., MCP tools).

### Task 4: Update AdaptiveParallelismLimiter to use declared safety

**File:** `internal/agent/executor.go` (wherever the limiter and profiles are)

Read the existing `toolProfiles` map and `AdaptiveParallelismLimiter`. Update the profile assignment to prefer tool-declared safety:

```go
func (e *Executor) profileForTool(toolName string, input map[string]any) ToolConcurrencyProfile {
    if tool, ok := e.registry.Get(toolName); ok {
        if tool.IsReadOnly(input) {
            return ProfileIOBound // read-only tools get high parallelism
        }
        if tool.IsConcurrencySafe(input) {
            return ProfileIOBound
        }
    }
    // Fallback to existing profile map
    if profile, ok := toolProfiles[toolName]; ok {
        return profile
    }
    return ProfileStateful // default: conservative
}
```

### Task 5: Tests

**File:** `internal/tools/interface_test.go` (new or extend existing)

- `TestToolDefaults` — defaults return false for both methods
- `TestReadOnlyTools` — file_read, file_grep, etc. return true for IsReadOnly
- `TestWriteTools` — file_write, file_edit return false for IsReadOnly
- `TestShellReadOnly` — shell with "ls" returns true, shell with "rm" returns false
- `TestConcurrencySafe` — read-only tools are concurrency-safe

**File:** `internal/agent/tool_dependency_test.go` (extend existing)

- `TestDeclaredSafetyPreferred` — tool-declared safety used over name heuristic
- `TestFallbackToNameHeuristic` — unknown tools fall back to name matching

## Self-Verification Checklist

- [ ] `go build ./...` compiles (ALL packages — interface change is global)
- [ ] `go test ./internal/tools/... -race` passes
- [ ] `go test ./internal/agent/... -race -run TestDependency` passes
- [ ] All builtin tools compile with new interface
- [ ] ToolDefaults embedding doesn't break existing tool behavior
- [ ] No unused imports or functions

## Review Checklist (for orchestrator)

- [ ] Interface methods have fail-closed defaults (false, false)
- [ ] ALL builtin tools updated (count matches tool inventory)
- [ ] Shell tool's IsReadOnly uses existing classifyRisk/isReadOnlyCommand
- [ ] DependencyInferrer falls back to name heuristic for unknown tools
- [ ] AdaptiveParallelismLimiter falls back to profile map for unknown tools
- [ ] No behavioral change for existing tool execution (defaults match old behavior)
- [ ] No debug artifacts, no TODOs, no placeholder values
