# Cluster G: Tool & Workflow Surface

## Goal
Implement preview-then-accept for destructive edits, integrate Ralph-loop-style persistence into plan execution, and enhance commit agent with atomic commit splitting.

## Background
User wants several workflow improvements:
- #10: Preview then accept for destructive operations
- #13: Ralph loop concepts — self-referential plan execution loop
- #14: Atomic commit splitting with validated messages
- Also: commit agent produces good commit messages

## Feature Checklist

### 1. Preview-then-Accept (resolve tool)
- oh-my-pi: `ast_edit` returns "(proposed)" card; `resolve` applies/discards
- Meept: No preview mechanism for destructive edits
- Implement:
  - Tools that modify files return a `pending` status with preview
  - New `resolve` tool accepts/rejects pending changes by ID
  - Supports batch accept/reject
  - Timeout: pending changes auto-expire after session ends or N minutes

### 2. Ralph Loop Concept Integration
- User question: "Don't we implement something akin to a ralf loop with our plan -> orchestrator -> agent(s) -> reviewer approach?"
- Answer: Partially. Our plan execution is structured but not self-referential:
  - Plan creates tasks -> agents execute -> no automatic re-planning on failure
  - Ralph loop: agent monitors its own output, re-plans, and continues until 100%
- Integration:
  - Add a "completion check" stage after agent execution
  - If task incomplete, auto-replan and re-execute
  - Evidence audit after each cycle
  - Maximum iterations to prevent infinite loops

### 3. Commit Agent Enhancements
- User wants:
  - Good commit messages (clear, understandable)
  - Atomic commit splitting (by dependency, source before tests)
  - Validated commit messages (conventional commit format)
- Current commit agent likely just does `git commit -m "..."`
- Enhancements:
  - Read working tree diff
  - Split unrelated changes into logical groups
  - Order groups by dependency (source > tests > docs > configs)
  - Validate each commit message against conventional commit format
  - Lock files excluded from analysis

## Implementation Plan

### Phase 1: Resolve / Preview System
1. Create `internal/tools/builtin/resolve.go`
2. Add `PendingChanges` registry (session-scoped)
3. Modify `file_edit`, `ast_edit`, `lsp_rename` to queue changes
4. `resolve` tool: accept/reject by change ID or wildcard
5. Show diff preview in tool result

### Phase 2: Ralph Loop Integration
1. In orchestrator, after agent completes a task:
   - Check if task requires automatic replanning (completion signal missing)
   - If incomplete, add new planning step with evidence from previous attempt
   - Continue up to max_replan_iterations
2. Add evidence audit: verify each claim with tool output
3. Store loop state in session metadata

### Phase 3: Commit Agent
1. Create `git_overview` tool: summarize working tree changes
2. Create `git_split` tool: suggest atomic commit groups
3. Enhance `git_commit` tool:
   - Accept list of files + message per commit
   - Validate conventional commit format
   - Order commits by dependency
4. `git_validate` tool: check commit message format

## Files to Modify / Create
- `internal/tools/builtin/resolve.go` (new) — Preview/accept system
- `internal/tools/builtin/pending_changes.go` (new) — Change registry
- `internal/agent/orchestrator.go` — Ralph loop completion check
- `internal/tools/builtin/git_overview.go` (new) — Working tree summary
- `internal/tools/builtin/git_split.go` (new) — Atomic commit splitting

## Success Criteria
- [ ] Edit tool returns preview; `resolve` accepts/rejects
- [ ] Incomplete tasks auto-replan up to N times
- [ ] Commit agent splits mixed changes into atomic commits
- [ ] Commit messages follow conventional commit format
