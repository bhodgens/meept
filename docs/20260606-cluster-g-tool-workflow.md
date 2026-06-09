# Cluster G: Tool & Workflow Surface

## Goal
Implement preview-then-accept for destructive edits, integrate Ralph-loop-style persistence into plan execution, and enhance commit agent with atomic commit splitting.

## Background
User wants several workflow improvements:
- #10: Preview then accept for destructive operations
- #13: Ralph loop concepts -- self-referential plan execution loop
- #14: Atomic commit splitting with validated messages
- Also: commit agent produces good commit messages

## Feature Checklist

### 1. Preview-then-Accept (resolve tool)
- oh-my-pi: `ast_edit` returns "(proposed)" card; `resolve` applies/discards
- Meept: [x] Implemented
  - [x] `internal/tools/builtin/pending_changes.go` -- session-scoped registry with expiry, per-session removal, wildcard cleanup
  - [x] `internal/tools/builtin/resolve.go` -- accept/reject by change ID or wildcard (`"all"`), batch operations
  - [x] `internal/tools/builtin/file_edit.go` -- when `pendingChangesRegistry` is set, queues changes instead of writing directly; returns diff preview and change ID
  - [x] `internal/code/tools/lsp_rename.go` -- integrated with `pendingChangesRegistry` when `apply=false`
  - [x] Registered in `internal/daemon/components.go`

### 2. Ralph Loop Concept Integration
- [x] Implemented
  - [x] `internal/agent/ralph_loop.go` -- `RalphLoop` struct with `CheckCompletion` (evidence-based), `TriggerReplan` (bus-published), iteration tracking, max iterations guard
  - [x] `internal/agent/orchestrator.go` (lines 231-248) -- wires Ralph loop into task completion: checks evidence, triggers replan or resets on max iterations
  - [x] Evidence audit: `validateEvidence` uses key-term extraction from task description to verify evidence claims
  - [x] Loop state stored per-task in `iterations` map

### 3. Commit Agent Enhancements
- [x] Implemented
  - [x] `internal/tools/builtin/git_overview.go` -- working tree summary (staged, unstaged, untracked, ahead/behind)
  - [x] `internal/tools/builtin/git_split.go` -- atomic commit grouping by dependency/feature/file_type; lock file exclusion
  - [x] `internal/tools/builtin/git_commit.go` -- single or batch commits (`commits` parameter), conventional commit validation, file-specific staging
  - [x] `internal/tools/builtin/git_validate.go` -- conventional commit regex validation, batch validation, git state check
  - [x] All registered in `internal/daemon/components.go`

## Implementation Plan

### Phase 1: Resolve / Preview System
- [x] 1. Create `internal/tools/builtin/resolve.go`
- [x] 2. Add `PendingChanges` registry (session-scoped)
- [x] 3. Modify `file_edit`, `lsp_rename` to queue changes
- [x] 4. `resolve` tool: accept/reject by change ID or wildcard
- [x] 5. Show diff preview in tool result

### Phase 2: Ralph Loop Integration
- [x] 1. In orchestrator, after agent completes a task:
  - [x] Check if task requires automatic replanning (completion signal missing)
  - [x] If incomplete, add new planning step with evidence from previous attempt
  - [x] Continue up to max_replan_iterations
- [x] 2. Add evidence audit: verify each claim with tool output
- [x] 3. Store loop state in session metadata (per-task iteration tracking)

### Phase 3: Commit Agent
- [x] 1. Create `git_overview` tool: summarize working tree changes
- [x] 2. Create `git_split` tool: suggest atomic commit groups (with lock file exclusion)
- [x] 3. Enhance `git_commit` tool:
  - [x] Accept list of files + message per commit (single or batch via `commits` parameter)
  - [x] Validate conventional commit format
  - [x] Order commits by dependency (batch mode creates commits in caller-specified order)
- [x] 4. `git_validate` tool: check commit message format

## Files Modified / Created
- `internal/tools/builtin/resolve.go` (new) -- Preview/accept system
- `internal/tools/builtin/pending_changes.go` (new) -- Change registry
- `internal/tools/builtin/git_overview.go` (new) -- Working tree summary
- `internal/tools/builtin/git_split.go` (new) -- Atomic commit splitting with lock file exclusion
- `internal/tools/builtin/git_commit.go` (enhanced) -- Batch commit support via `commits` parameter
- `internal/tools/builtin/git_validate.go` (new) -- Conventional commit validation
- `internal/tools/builtin/file_edit.go` (modified) -- Pending changes integration
- `internal/agent/ralph_loop.go` (new) -- Self-referential plan execution loop
- `internal/agent/orchestrator.go` (modified) -- Ralph loop wiring
- `internal/daemon/components.go` (modified) -- Tool registration
- `internal/task/step.go` (fixed) -- Schema column count mismatch for `model_override`

## Success Criteria
- [x] Edit tool returns preview; `resolve` accepts/rejects
- [x] Incomplete tasks auto-replan up to N times
- [x] Commit agent splits mixed changes into atomic commits
- [x] Commit messages follow conventional commit format
