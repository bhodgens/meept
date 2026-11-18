# 02 Docs Ag - Implementation Leaf

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
- **Scope:** Update docs and AGENTS.md for changed defaults
- **Dependencies:** 04-config-docs/00T-e2e-pin.md; 04-config-docs/01-kanban-default.md
- **Estimated Context:** 30K
- **Concurrency Group:** D2
- **Audit references:** none

## Goal

Update `docs/agents/opt-in-defaults.md`, root `AGENTS.md` kanban summary, `docs/workflows/kanban.md`, and any config reference generated docs affected by the default flip. State that kanban is now default-on while the sidecar remains opt-in.

## Context

Markdown only. The root AGENTS.md maintenance rule requires invariant docs change in the same commit as the flip. Do not edit docs/generated/ by hand; regenerate with make targets if required.

## Verified Anchors

- docs/agents/opt-in-defaults.md - opt-in list
- AGENTS.md: kanban summary in Architecture Overview
- docs/workflows/kanban.md - operator docs
- config/meept.json5:1610-1634 - template comment

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
updated invariant docs
```

### What This Leaf Consumes

none (docs only)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Docs sync | Modify: docs/agents/opt-in-defaults.md, AGENTS.md, docs/workflows/kanban.md | `grep -rn 'default-off\|opt-in' docs/agents/opt-in-defaults.md` | stale text | consistent text | Given new default; When docs read; Then they say default-on |

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
