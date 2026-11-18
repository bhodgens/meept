# 01 Popup Shell - Implementation Leaf

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
- **Scope:** Full-window popup shell
- **Dependencies:** 03-gui/00T-gui-tests.md
- **Estimated Context:** 45K
- **Concurrency Group:** C1
- **Audit references:** none

## Goal

Replace the fixed 800x560 Dialog in session_info_overlay.dart with a full-window dialog. Keep the header, tab bar, and IndexedStack. Do not change tab bodies (leaf 02 does that).

## Context

Dart. Use `Dialog.fullscreen` or `insetPadding: EdgeInsets.zero` with `SizedBox.expand`. Preserve the existing tab controller lifecycle.

## Verified Anchors

- ui/flutter_ui/lib/features/home/session_info_overlay.dart:61-67 - Dialog with width/height
- ui/flutter_ui/lib/features/home/session_info_overlay.dart:75-99 - Column structure

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
fullscreen session popup
```

### What This Leaf Consumes

none (UI shell only)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Fullscreen dialog | Modify: ui/flutter_ui/lib/features/home/session_info_overlay.dart | `flutter test --no-pub test/features/home/session_info_overlay_test.dart` | fixed 800x560 | fills window | Given a window; When popup opened; Then dialog matches window |

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
