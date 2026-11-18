# 03 Sessions Tree - Implementation Leaf

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
- **Scope:** Project-grouped session tree
- **Dependencies:** 03-gui/00T-gui-tests.md; C2
- **Estimated Context:** 60K
- **Concurrency Group:** C3
- **Audit references:** none

## Goal

Add a Dart grouping helper implementing C2 and render sessions in sessions_list.dart as an expandable tree (ExpansionTile or TreeNode) with project/group headers. Keep activation, double-tap navigation, and project prompt behavior intact. Serialize with leaf 04 on sessions_list.dart.

## Context

Dart. `Session` carries projectId/projectPath at api_models.dart:611-612. Preserve the dialog-in-the-flow double-tap behavior from the flutter skill.

## Verified Anchors

- ui/flutter_ui/lib/features/sessions/sessions_list.dart:129-147 - activation flow
- ui/flutter_ui/lib/models/api_models.dart:611-612 - project fields
- ui/flutter_ui/lib/features/sessions/sessions_overview_tab.dart - container

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Dart C2 helper and tree UI in sessions_list.dart
```

### What This Leaf Consumes

- C2 bucketFor - owner: this leaf (Dart side)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Group helper | Create: ui/flutter_ui/lib/features/sessions/session_tree.dart | `flutter test --no-pub test/features/sessions/session_tree_test.dart` | helper missing | buckets correct | Given session with projectId; When bucket; Then id returned; Else path; Else unbound |
| T2 | Tree UI | Modify: ui/flutter_ui/lib/features/sessions/sessions_list.dart | same | flat list | expandable groups | Given 2 projects; When viewed; Then groups expand/collapse |

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
