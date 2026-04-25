# GitHub Issues to Create

**Date:** 2026-04-24

---

## Issue 1: Teatest Integration Tests for TUI

**Title:** `tui: Add teatest integration tests for bubbletea v2`

**Labels:** `testing`, `tui`, `tech-debt`

**Description:**

The TUI integration tests using `teatest` are currently disabled due to bubbletea v2 migration. The test file `internal/tui/app_test.go` lines 428-429 contains:

```go
// TODO: Re-enable teatest integration tests once a
// bubbletea v2-compatible teatest package is available.
```

### Tasks

- [ ] Check if charm.land/bubbletea/v2 has a compatible teatest package
- [ ] Update imports from `github.com/charmbracelet/bubbletea` to `charm.land/bubbletea/v2`
- [ ] Migrate existing tests to v2 API
- [ ] Re-enable disabled tests
- [ ] Add new integration tests for:
  - Task tree expand/collapse
  - Fuzzy finder search
  - Session picker

### References

- Test file: `internal/tui/app_test.go:428-429`
- bubbletea v2: https://charm.land/bubbletea/v2
- Original teatest: https://github.com/charmbracelet/bubbletea/tree/master/teatest

---

## Issue 2: Remove OpenClaw Compatibility from ClawSkills

**Title:** `clawskills: Remove OpenClaw compatibility layer`

**Labels:** `cleanup`, `clawskills`, `breaking-change`

**Description:**

The clawskills system currently maintains compatibility with the OpenClaw registry format. This compatibility layer should be removed to simplify the codebase and focus on the native Meept skill format.

### What Needs to Be Removed

1. **`claw:` prefix handling**
   - Remove namespace isolation code
   - Remove prefix stripping logic

2. **OpenClaw-specific fields**
   - Remove any OpenClaw-only metadata fields
   - Simplify skill manifest structure

3. **Backwards compatibility code**
   - Remove legacy format converters
   - Remove OpenClaw version compatibility checks

### Files to Review

- `internal/clawskills/*.go`
- `internal/skills/*.go` (any OpenClaw references)

### Migration Path

1. Audit existing clawskills installations
2. Document breaking changes
3. Update third-party skill documentation
4. Update CLAUDE.md CLI reference

### Acceptance Criteria

- [ ] No references to "openclaw" or "OpenClaw" in codebase
- [ ] No `claw:` prefix handling
- [ ] Simplified skill manifest schema
- [ ] Updated documentation
- [ ] Changelog entry for breaking change

---

## Issue 3: Task Tree - Multi-level Subtask Support

**Title:** `tui: Support multi-level task tree nesting`

**Labels:** `enhancement`, `tui`, `tasks`

**Description:**

The current task tree implementation in `internal/tui/models/tasks.go` supports single-level parent-child relationships via `InheritedFrom`. This should be extended to support arbitrary nesting depth.

### Current Behavior

- Parent tasks show with ▶/▼ indicators
- Subtasks (InheritedFrom != "") show indented with └─
- Only one level of children supported

### Desired Behavior

- Support N levels of nesting
- Visual indicators per level (different indent characters)
- Collapse/expand at any level
- Performance considerations for deep trees

### Implementation Notes

- Modify `buildFlatListWithChildren()` for recursive tree building
- Track expanded state per task ID (already implemented)
- Consider virtual scrolling for large task trees

---

## Issue 4: Fuzzy Finder - True Fuzzy Matching Algorithm

**Title:** `tui: Implement true fuzzy matching algorithm for finder`

**Labels:** `enhancement`, `tui`, `search`

**Description:**

The current fuzzy finder (ModalFuzzyFinderModal) uses simple substring matching. This should be upgraded to a proper fuzzy matching algorithm like fuzzyjs or similar.

### Current Behavior

```go
if query == "" || strings.Contains(name, query) || strings.Contains(desc, query)
```

### Desired Behavior

- Implement fuzzy matching (e.g., smithwaterman, levenshtein, or similar)
- Score and rank results by match quality
- Highlight matched characters in results
- Consider using existing Go fuzzy library

### Suggested Libraries

- https://github.com/lithammer/fuzzysearch
- https://github.com/sahilm/fuzzy

### Implementation Notes

- Add match scoring to fuzzyFinderItem
- Sort results by score
- Add threshold for minimum match quality

---

## Issue 5: Task Detail - Error Rate Tracking

**Title:** `tui: Track and display error rates in task detail view`

**Labels:** `enhancement`, `tui`, `metrics`

**Description:**

The task detail view now shows `ErrorCount` field, but this field is not populated by the backend. Error rate tracking should be implemented to provide meaningful data.

### Current State

- `TaskExtended.ErrorCount` field added
- UI displays error count if > 0
- Backend does not populate this field

### Required Work

1. **Backend: Error tracking**
   - Add error counting to task execution
   - Track errors per task in task registry
   - Expose via `task.extended` RPC method

2. **Frontend: Enhanced display**
   - Show error rate (errors / total jobs)
   - Link to error details
   - Graph error trend over time

### Acceptance Criteria

- [ ] ErrorCount populated by backend
- [ ] Error rate displayed as percentage
- [ ] Error details accessible
- [ ] Metrics dashboard updated

---

## Prioritization

| Issue | Priority | Estimated Effort |
|-------|----------|-----------------|
| Teatest Tests | High | 2-3 days |
| OpenClaw Removal | High | 1-2 days |
| Multi-level Task Tree | Medium | 1-2 days |
| Fuzzy Matching | Medium | 0.5-1 day |
| Error Rate Tracking | Low | 2-3 days |
