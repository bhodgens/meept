# 01 Kanban Default - Implementation Leaf

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
- **Scope:** Enable kanban by default and add GUI tools menu entry
- **Dependencies:** 04-config-docs/00T-e2e-pin.md; C5; C6
- **Estimated Context:** 35K
- **Concurrency Group:** D1
- **Audit references:** none

## Goal

Flip DefaultConfig().Kanban.Enabled to true, uncomment and update the config template block, add kanban to `toolRoutePaths` and `HamburgerMenu._knownTools`, and add any missing `_hasRoute` set entries in the home screens. Keep SidecarEnabled false.

## Context

Go config + Dart menu. The route and command palette entry already exist at router.dart:112 and command_palette.dart:99-104.

## Verified Anchors

- internal/config/schema.go:5318 - DefaultConfig Kanban.Enabled=false
- config/meept.json5:1621-1634 - commented template block
- ui/flutter_ui/lib/features/home/tools_dropdown.dart:14-27,83-91 - menu maps
- ui/flutter_ui/lib/core/router.dart:112 - /kanban route

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
default-on kanban config and GUI menu entry
```

### What This Leaf Consumes

- C5, C6 - owner: this leaf

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Config default | Modify: internal/config/schema.go, config/meept.json5 | `go test -p 2 ./internal/config/... -run TestKanbanDefault` | false | true | Given DefaultConfig; When read; Then Kanban.Enabled=true |
| T2 | E2E pin green | Run: e2e suite | `make e2e-fast-area AREA=kanban` | pin RED | pin GREEN | Given rewritten pin; When run; Then passes |
| T3 | Tools menu | Modify: ui/flutter_ui/lib/features/home/tools_dropdown.dart, ui/flutter_ui/lib/features/home/home_screen.dart, ui/flutter_ui/lib/features/home/sidebar_home_screen.dart | `flutter analyze --no-pub lib/` | no menu item | kanban item opens route | Given hamburger menu; When kanban tapped; Then /kanban opens |

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
