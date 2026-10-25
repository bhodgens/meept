# Tree 05: Combined Improvements

## Goal

Seven targeted improvements across tools, hooks, memory, security, and agent definitions. Each addresses a specific gap from the Claude Code analysis. Grouped into 4 leaves by architectural proximity.

## Architecture Overview

| Item | Package | Leaf |
|------|---------|------|
| Per-tool declared safety | internal/tools | 01 |
| FILE_UNCHANGED optimization | internal/tools/builtin | 02 |
| Command exit code semantics | internal/tools/builtin | 02 |
| Inline skill feedback | internal/selfimprove + internal/agent | 03 |
| Memory staleness caveat | internal/memory | 03 |
| Bash injection checks | internal/security | 04 |
| Secret discovery scanning | internal/security | 04 |
| Explore agent | config/agents + internal/agent/prompts | 04 |

## Interface Contracts

See SHARED-CONVENTIONS.md §6 for per-tool safety interface extension.

### Per-Tool Safety (Leaf 01)
```go
// Added to tools.Tool interface:
type Tool interface {
    // ... existing methods ...
    IsReadOnly(input map[string]any) bool
    IsConcurrencySafe(input map[string]any) bool
}

// Default implementation (embedded struct):
type ToolDefaults struct{}
func (ToolDefaults) IsReadOnly(map[string]any) bool { return false }
func (ToolDefaults) IsConcurrencySafe(map[string]any) bool { return false }
```

### FILE_UNCHANGED Stub (Leaf 02)
```go
const FileUnchangedStub = "File unchanged since last read. The content from the earlier read is still current — refer to that instead of re-reading."
```

### Command Semantics Map (Leaf 02)
```go
type CommandSemantic struct {
    IsError  func(exitCode int) bool
    Message  func(exitCode int) string
}
var commandSemantics = map[string]CommandSemantic{
    "grep": {IsError: func(c int) bool { return c >= 2 }, Message: func(c int) string { if c == 1 { return "no matches found" }; return "" }},
    "rg":   {IsError: func(c int) bool { return c >= 2 }, Message: func(c int) string { if c == 1 { return "no matches found" }; return "" }},
    "diff": {IsError: func(c int) bool { return c >= 2 }, Message: func(c int) string { if c == 1 { return "files differ" }; return "" }},
    "find": {IsError: func(c int) bool { return c >= 2 }, Message: func(c int) string { if c == 1 { return "some directories were inaccessible" }; return "" }},
    "test": {IsError: func(c int) bool { return c >= 2 }, Message: func(c int) string { if c == 1 { return "condition is false" }; return "" }},
}
```

### Memory Staleness Caveat (Leaf 03)
```go
func MemoryFreshnessText(ageDays int) string {
    if ageDays <= 1 { return "" }
    return fmt.Sprintf("This memory is %d days old. Memories are point-in-time observations, not live state — claims about code behavior or file:line citations may be outdated. Verify against current code before asserting as fact.", ageDays)
}
```

### Explore Agent (Leaf 04)
```json5
{
  "id": "explore",
  "role": "explorer",
  "model_tier": "fast",
  "tools_allow": ["file_read", "file_grep", "file_find", "list_directory", "shell"],
  "tools_deny": ["file_write", "file_edit", "file_delete", "git_commit", "memory_store", "task_create", "task_update", "ask", "resolve"],
  "verification": { "enabled": false }
}
```

## Child Index

| # | Leaf | Est. Context | Dependencies | Files Touched |
|---|------|-------------|--------------|---------------|
| 01 | per-tool-safety | 70K | none | ~15 files |
| 02 | file-unchanged-cmdsem | 50K | none | ~5 files |
| 03 | feedback-staleness | 55K | none | ~8 files |
| 04 | security-explore | 65K | none | ~10 files |

All 4 leaves are independent — dispatch concurrently (Wave 2, after Tree 01 completes for interface alignment).

## Dispatch Protocol

1. Dispatch all 4 leaves concurrently.
2. Review each in-session. Commit after review.
3. Integration: verify tool interface change doesn't break existing tools.

## Review Checklist

- [ ] All existing tools still compile with new interface (ToolDefaults embedding)
- [ ] FILE_UNCHANGED stub returned for unchanged files
- [ ] Command semantics map handles grep/rg/diff/find/test exit codes
- [ ] Inline skill feedback triggers every N messages and feeds into selfimprove pipeline
- [ ] Memory staleness caveat appears for memories > 1 day old
- [ ] Bash injection checks cover IFS, brace expansion, unicode whitespace, process substitution
- [ ] Secret discovery scanning detects AWS/GitHub/OpenAI/Stripe key patterns
- [ ] Explore agent definition parses and uses fast model tier
- [ ] No debug artifacts, no TODOs, no placeholder values

## Coding Conventions

See SHARED-CONVENTIONS.md §2-§3.

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| 01-per-tool-safety | COMPLETE | IsReadOnly/IsConcurrencySafe on Tool interface |
| 02-file-unchanged-cmdsem | COMPLETE | FILE_UNCHANGED stub + command exit semantics |
| 03-feedback-staleness | COMPLETE | InlineFeedbackTrigger + MemoryFreshnessText |
| 04-security-explore | COMPLETE | SecretScanner, bash injection, explore agent |

## Integration Test Plan

1. `go build ./...` — full compilation (critical: interface change)
2. `go test ./internal/tools/... -race`
3. `go test ./internal/security/... -race`
4. `go test ./internal/memory/... -race`
5. `go test ./internal/selfimprove/... -race`
6. Verify all builtin tools implement new interface methods
7. Verify explore agent loads from config/agents/

## Test Gate

<!-- Tests-first gate (hierarchical-planning skill v1.3.0, patched 2026-10-24).
     Coverage matrix derived mechanically from leaf task headers; the control
     agent (orchestrator, in-session) refines rows and fills test names at
     Phase 0 dispatch. No implementation leaf dispatches until every row is
     VALIDATED. Delta protocol: lesser of plan/tests updates to the greater.
     Recipe: skill hierarchical-planning -> references/tests-first-gate.md -->

| # | Feature (from plan) | Source | Unit tests | E2E tests | Status |
|---|---------------------|--------|-----------|-----------|--------|
| F1 | Add methods to Tool interface with defaults | 01-per-tool-safety.md | (pending) | (pending) | PENDING |
| F2 | Update all builtin tools to embed ToolDefaults | 01-per-tool-safety.md | (pending) | (pending) | PENDING |
| F3 | Update DependencyInferrer to use declared safety | 01-per-tool-safety.md | (pending) | (pending) | PENDING |
| F4 | Update AdaptiveParallelismLimiter to use declared safety | 01-per-tool-safety.md | (pending) | (pending) | PENDING |
| F5 | Tests | 01-per-tool-safety.md | (pending) | (pending) | PENDING |
| F6 | Add unchanged detection to file_read | 02-file-unchanged-cmdsem.md | (pending) | (pending) | PENDING |
| F7 | Ensure hashline anchors still work with unchanged stub | 02-file-unchanged-cmdsem.md | (pending) | (pending) | PENDING |
| F8 | Tests for FILE_UNCHANGED | 02-file-unchanged-cmdsem.md | (pending) | (pending) | PENDING |
| F9 | Add command semantics map to shell tool | 02-file-unchanged-cmdsem.md | (pending) | (pending) | PENDING |
| F10 | Wire semantics into result construction | 02-file-unchanged-cmdsem.md | (pending) | (pending) | PENDING |
| F11 | Handle compound commands | 02-file-unchanged-cmdsem.md | (pending) | (pending) | PENDING |
| F12 | Tests for command semantics | 02-file-unchanged-cmdsem.md | (pending) | (pending) | PENDING |
| F13 | Create InlineFeedbackTrigger | 03-feedback-staleness.md | (pending) | (pending) | PENDING |
| F14 | Wire into selfimprove pipeline | 03-feedback-staleness.md | (pending) | (pending) | PENDING |
| F15 | Wire trigger into agent loop | 03-feedback-staleness.md | (pending) | (pending) | PENDING |
| ... | +15 more features (expand from leaf task headers at dispatch) | various | (pending) | (pending) | PENDING |

Statuses: PENDING | DELTA | VALIDATED

Delta log: (none yet)
