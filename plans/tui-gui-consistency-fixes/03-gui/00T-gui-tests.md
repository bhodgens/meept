# 00T Gui Tests - Implementation Leaf

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
- **Scope:** RED widget tests for popup, tabs, tree, search
- **Dependencies:** none
- **Estimated Context:** 28K
- **Concurrency Group:** C0
- **Audit references:** none

## Goal

Write widget tests that fail RED for: fullscreen popup; canonical tab reuse with loaded plans/tasks; agent active/cost/type display; grouped session tree with search button. Tests only, no implementation.

## Context

Dart widget tests with ProviderScope and SdkApiClient stub subclass. Follow the flutter-agent-testing skill test conventions.

## Verified Anchors

- ui/flutter_ui/lib/features/home/session_info_overlay.dart:61-67 - fixed 800x560
- ui/flutter_ui/lib/features/sessions/sessions_list.dart - flat list
- ui/flutter_ui/lib/features/search/search_panel.dart:17-43 - SearchPanel
- ui/flutter_ui/lib/models/api_models.dart:706-716 - Agent model

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
test files for GUI changes
```

### What This Leaf Consumes

- C1, C2, C4 (root master.md and this branch)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | popup fullscreen test | Create: ui/flutter_ui/test/features/home/session_info_overlay_test.dart | `flutter test --no-pub test/features/home/session_info_overlay_test.dart` | fixed size | full window | Given popup; When pumped; Then fills screen | Given a session; When opened; Then tabs visible |
| T2 | tabs populated test | Modify: same test file | same | empty states | canonical tabs shown | Given loaded providers; When plans tab tapped; Then plan rows present |

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
