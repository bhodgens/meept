# 02 Popup Tabs - Implementation Leaf

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
- **Scope:** Reuse canonical tabs and add roster display
- **Dependencies:** 03-gui/00T-gui-tests.md; 03-gui/01-popup-shell.md; C1
- **Estimated Context:** 50K
- **Concurrency Group:** C2
- **Audit references:** none

## Goal

Delete the four private tab body classes in session_info_overlay.dart and embed the canonical `SessionsOverviewTab`, `PlansTab`, `TasksTab`, and `AgentsTab`. Extend the Dart Agent model with C1 fields and render type/active/cost in the agents tab. Ensure plans/tasks load for the session (canonical widgets already do this in initState).

## Context

Dart. `PlansTab` loads in initState at plans_tab.dart:24-29; `TasksList` loads at tasks_list.dart:31. Reuse them; do not reimplement loaders.

## Verified Anchors

- ui/flutter_ui/lib/features/home/tab_content.dart:25-45 - canonical tabs
- ui/flutter_ui/lib/features/home/session_info_overlay.dart:246-531 - duplicate bodies to delete
- ui/flutter_ui/lib/models/api_models.dart:706-716 - Agent model
- ui/flutter_ui/lib/features/agents/agents_tab.dart:284-370 - agent card rendering

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
canonical tabs in popup; Agent model with C1 fields
```

### What This Leaf Consumes

- C1 RosterEntry - owner: 01-backend/01-roster-rpc.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Reuse tabs | Modify: ui/flutter_ui/lib/features/home/session_info_overlay.dart | `flutter test --no-pub test/features/home/session_info_overlay_test.dart` | empty states | loaded tabs | Given providers seeded; When plans tab tapped; Then plan rows present | Given tasks tab; Then task rows present |
| T2 | Agent fields | Modify: ui/flutter_ui/lib/models/api_models.dart, ui/flutter_ui/lib/features/agents/agents_tab.dart | same | fields absent | type/active/cost rendered | Given C1 rows; When agents tab; Then columns shown |

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
