# 03 Migration Hint Intersection (D4) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests ALREADY EXIST and are
> committed - your job is to make them pass; do NOT delete, skip, weaken,
> or rewrite any gate test. Do NOT commit - the orchestrator handles all
> git operations after review. Do NOT use read_file on existing source
> files - explore with search_files or terminal cat. After writing a file,
> do NOT read it back - write once and stop.

## Meta

- **Parent:** master.md
- **Scope:** Replace the verbatim tools_allowed_hint override with base ∩ hint intersection (D4), sorted+deduped.
- **Dependencies:** 00T gate tests; MUST run AFTER leaf 01 is REVIEWED + committed (shared file internal/employee/manager.go)
- **Estimated Context:** 40K
- **Concurrency Group:** B (serialized behind 01)
- **Audit references:** migration hint widening vector (manager.go:2844-2858)

## Goal

Make the migration LLM's proposed tool allowlist unable to widen tool grants: when the base constitution carries a non-empty allowlist, the LLM hint intersects with it. When the base is empty (inherit-everything universe), the hint remains the effective list — replacement is correct there because the intersection with "everything" is the hint itself.

## Context

- Site: internal/employee/manager.go:2844-2858 in synthesizeConstitutionWithLLM — today: `if len(hint) > 0 { base.Constraints.ToolsAllowed = cleaned }` (verbatim replace).
- Base: synthesizeConservativeConstitution -> DefaultConservativeConstitution (constitution.go:691) has ToolsAllowed NIL = inherit default toolset (constitution.go:250-253).
- D4 rule: internal/groups/groups.go:40 — "Intersection never widens the spec (D4)". The algorithm to mirror lives in internal/groups/resolve.go:163-171 (iterate one set keeping members that are in the effective allow and not denied; sort.Strings). resolveLayered is UNEXPORTED and internal/employee must NOT import internal/groups — mirror the ~10-line set logic inline.
- Note: ApplyMigration (manager.go:2561) persists the conservative constitution only; the LLM constitution is the proposal shown for review. The fix protects both the proposal surface and any future wiring that applies it.
- Hire-time validation (manager.go:1222-1231 CheckToolReferences + removeUnknownTools) strips unknown names later — do not duplicate registry filtering here; the intersection is policy-level (base vs hint), not registry-level.

## Verified Anchors

- internal/employee/manager.go:2844-2858 - the replace block to rewrite
- internal/employee/manager.go:2736 - llmConstitutionResponse.ToolsAllowedHint
- internal/employee/manager.go:2763 - base := synthesizeConservativeConstitution(def)
- internal/groups/resolve.go:163-172 - reference algorithm (spec ∩ allow, sort) - mirror inline, do not import
- internal/employee/migrate_llm_test.go - gate tests land here (TestSynthesizeConstitution_HintIntersectsBase etc.)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// Semantics (Contract C3) at manager.go:2844-2858:
//   cleaned := trim + drop empties (unchanged)
//   if len(cleaned) == 0 { keep base as-is }                    // empty hint = inherit
//   else if len(base.Constraints.ToolsAllowed) > 0 {
//       cleaned = sortedDedup(cleaned ∩ base.Constraints.ToolsAllowed)
//   }
//   base.Constraints.ToolsAllowed = cleaned
// Review note (existing notes append at :2855-2856) should say the hint was
// INTERSECTED with the base allowlist (and how many survived) when the base
// was non-empty.
```

### What This Leaf Consumes

- Gate tests in internal/employee/migrate_llm_test.go (already committed)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Implement the intersection branch per C3 | Modify: internal/employee/manager.go:2844-2858 (block only) | `go test -p 2 ./internal/employee/ -run TestSynthesizeConstitution_HintIntersectsBase` | gate RED (replace yields superset) | GREEN | Given base=[file_read], hint=[file_read, shell]; When synthesize; Then ToolsAllowed == [file_read] |
| T2 | Edge semantics preserved: empty hint keeps base (incl. nil stays nil); base nil + hint non-empty -> hint verbatim (sorted/deduped as today); all-unknown-vs-nonempty-base hint -> base members only | same block | `go test -p 2 ./internal/employee/ -run TestSynthesizeConstitution_` | gate RED on one/more edge tests | GREEN all | Given base=nil, hint=[shell]; When synthesize; Then [shell] |
| T3 | Review-note accuracy: when intersection dropped names, the note counts kept vs dropped | same block (notes append) | `go test -p 2 ./internal/employee/ -run TestSynthesizeConstitution_HintIntersectsBase -v` (note assertion in gate test) | RED | GREEN | Given 1-of-2 hint names survive; When note built; Then note says intersected, 1 kept, 1 dropped |
| T4 | Migration-proposal snapshot check: any existing test snapshotting the proposed constitution still passes | none (verification only) | `go test -p 2 ./internal/employee/` full package | n/a | full package GREEN (incl. migrate_llm_test.go, constitution_test.go, wildcard_tools_test.go) | Given all existing migration tests; When run; Then green |

## Self-Verification Checklist

- [ ] Contract C3 semantics exact (three branches)
- [ ] No import of internal/groups; inline set logic ~10 lines, commented with the D4 rule reference
- [ ] Only the :2844-2858 region + note text changed in manager.go
- [ ] Full internal/employee package GREEN

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [paths]
2. **Verify command + output:** [last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Intersection only when base non-empty; empty-base replacement preserved
- [ ] Sorted+deduped result; note text accurate
- [ ] No other manager.go behavior changed (leaf 01's resume code untouched)
- [ ] No debug artifacts

Output: APPROVED or specific gaps with file + line references.

## Notes

- This leaf edits the SAME file as leaf 01 — dispatch strictly after 01 is committed. Verify with `git log -1 --stat internal/employee/manager.go` that 01 landed first.
- Keep the existing trim/drop-empties behavior byte-identical for the empty-hint path.
