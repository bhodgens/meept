# 04 Search Entry - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. If this branch runs a tests-first
> gate, the failing tests ALREADY EXIST and are committed - your job is to
> make them pass; do NOT delete, skip, weaken, or rewrite any gate test
> (report suspected-wrong gate tests to the orchestrator instead). Do NOT
> commit - the orchestrator handles all git operations after review. Do NOT
> use read_file on existing source files - explore with search_files or
> terminal cat. After writing a file, do NOT read it back - write once and
> stop. Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Search button on the session tree
- **Dependencies:** 03-gui/00T-gui-tests.md; 03-gui/03-sessions-tree.md; C4
- **Estimated Context:** 30K
- **Concurrency Group:** C4
- **Audit references:** none

## Goal

Add a search button to the session tree header that opens the existing `SearchPanel` with `SearchScope.sessions`. Reuse C4; do not write a new search implementation. Serialize with leaf 03 on sessions_list.dart.

## Context

Dart. SearchPanel is a full-screen tool route; call the existing `goToolSearch()`/`context.go('/tools/search')` path and set scope.

## Verified Anchors

- ui/flutter_ui/lib/features/search/search_panel.dart:17-43 - SearchPanel
- ui/flutter_ui/lib/models/api_models.dart:1012 - SearchScope.sessions
- ui/flutter_ui/lib/core/router.dart - /tools/search route

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
search button wired to scoped SearchPanel
```

### What This Leaf Consumes

- C4 SearchPanel - owner: existing code, reuse only

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Search button | Modify: ui/flutter_ui/lib/features/sessions/sessions_list.dart | `flutter test --no-pub test/features/sessions/session_tree_test.dart` | button absent | SearchPanel opens scoped | Given tree header; When search tapped; Then SearchPanel opens with sessions scope |

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing, gate tests untouched
- [ ] Interface contracts match exactly (signatures, types, paths)
- [ ] Conventions followed; no bugs, no security issues
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

None.
