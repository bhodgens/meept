# Office Skills Documentation + Router (Docs Leaf 04)

> **For the implementing agent:** You are the implementer for this docs leaf.
> This is a DOCS + SKILL-ROUTER leaf: you write the `office` router skill body
> and the user-facing documentation for the office skill bundle and the
> requires-tools gate. You do NOT add Go code or tests - the tools/skills/tests
> land in leaves 01-03 and 00T. Do NOT commit. Do NOT git add. Do NOT use
> read_file on existing source - explore with search_files or terminal cat.
> Write once and stop.

## Meta

- **Parent:** ../../06-office-skills/orchestrator.md
- **Scope:** The `office` router SKILL.md + `docs/workflows/office-skills.md`
  feature doc + a cross-reference in `docs/workflows/skills.md` documenting the
  requires-tools gate for the office tools.
- **Dependencies:** 00-research GO (library picks known), 01-03 REVIEWED (tool
  names final)
- **Estimated Context:** 14K
- **Concurrency Group:** B

## Goal

Document the shipped office capability so a human or model can discover and use
it: (1) an `office` router skill that routes to `office-docx` / `office-pptx` /
`office-xlsx` by file extension (mirrors the dsh `office` router, which must
load the format skill before creating/reading/editing Office files), and (2) a
feature doc under `docs/workflows/` (root AGENTS.md mapping: subsystem ->
docs/workflows/<name>.md; config/skills additions are documented here) plus a
cross-reference in `docs/workflows/skills.md` pinning the requires-tools gate
behavior against these tools.

## Context

- dsh `office` router archetype:
  `/tmp/deepseek-harness/packages/skill/skill-office/assets/office/SKILL.md` -
  descriptions only (the router is a routing index, not redundant instructions:
  pick the workflow skill by extension; a format skill is not a rendering
  engine).
- Meept skill docs: `docs/workflows/skills.md` is the skills feature doc
  (frontmatter `requires-tools:` gate described at lines 65-89; failure message
  contract; `skills.validate_prerequisites` must be enabled for the gate to
  fire). Add a section cross-referencing the office bundle + its three tool
  gates.
- Skill frontmatter format: `name`, one-line `description`, optional
  `requires-tools:` YAML list (the router `office` itself lists NO requires-tools
  - it routes; the format skills carry the gates).

## Verified Anchors

- `docs/workflows/skills.md:65-89` - the requires-tools gate section to
  cross-link / extend.
- `/tmp/deepseek-harness/packages/skill/skill-office/assets/office/SKILL.md` -
  the dsh router archetype (description-only routing index).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// config/skills/office/office/SKILL.md - the router skill (per C11.1 layout)
// frontmatter: name: office ; description: one line "Create, edit, inspect..."
// body: routing index only - choose office-docx|office-pptx|office-xlsx by
//   extension; do NOT restate the format skills' workflows.
// docs/workflows/office-skills.md - feature doc: bundle layout, tool names +
//   JSON action contracts (C11.2-4), requires-tools gate default-off note.
// docs/workflows/skills.md - add a short cross-reference section for the
//   office bundle + the three tool gates.
```

### What This Leaf Consumes

- C11.1 skill bundle layout + requires-tools gate (exact names
  office-docx/office-pptx/office-xlsx + tools office_docx/office_pptx/
  office_workbook, frozen in the orchestrator)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | office router skill | Create: config/skills/office/office/SKILL.md | `go test -p 2 ./internal/skills/...` (parser round-trip) | no router | parser reads name/description; body routes by extension | Given the router SKILL.md; When the skills parser loads it; Then name/description are populated and the body tells the model to load office-docx/office-pptx/office-xlsx by extension |
| T2 | feature doc | Create: docs/workflows/office-skills.md | cross-reference resolution (grep the paths/names used exist) | no doc | doc lists bundle layout + three tools + gate semantics | Given docs/workflows/office-skills.md; When a reader resolves every referenced path/name; Then all exist (config/skills/office/*, office_docx/office_pptx/office_workbook) |
| T3 | gate cross-reference | Modify: docs/workflows/skills.md | `grep -n "office" docs/workflows/skills.md` | no cross-ref | a short section notes the office bundle ships requires-tools:[office_docx|office_pptx|office_workbook] and the gate fires only when skills.validate_prerequisites is enabled | Given the edit; When skills.md is read; Then the office bundle + its gate is discoverable with the default-off note |

Docs-only leaf: "run tests" is replaced by "verify cross-references resolve"
per the top-mapping; the parser round-trip (T1) is the one executable gate.

## Self-Verification Checklist

- [ ] office/SKILL.md router present and parse-verified (name/description)
- [ ] docs/workflows/office-skills.md references only existing paths/names
- [ ] docs/workflows/skills.md cross-section added with the default-off gate note
- [ ] No Go code, no tests, no registration edits
- [ ] ASCII only; no git commit

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** config/skills/office/office/SKILL.md,
   docs/workflows/office-skills.md, docs/workflows/skills.md
2. **Verify:** parser round-trip for the router; grep that every referenced
   office tool name / skill path exists
3. **Deviations:** none / list with rationale (e.g. if a tool/skill name
   differed from C11.2-4, flag it - the doc must match the frozen contracts)

## Review Checklist (For Review Agent)

- [ ] Router SKILL.md is a description-only routing index (no redrawn format
      workflows), name `office`, no requires-tools on the router itself
- [ ] office-skills.md matches the frozen C11.2-C11.4 tool names + action
      contracts and the library picks in library-picks.md
- [ ] skills.md cross-reference present with the `skills.validate_prerequisites`
      default-off note
- [ ] Cross-references resolve; ASCII; no debug artifacts
- [ ] No scope creep (no code/tests/registration)

## Notes

- The router is deliberately thin (dsh archetype) - it routes by extension and
  defers to the format skills; the model loads office-docx/office-pptx/
  office-xlsx by name.
- The CLI/TUI/GUI surface for the office tools is a DOCUMENTED DEFERRAL here
  (root AGENTS.md wiring requirement): the model-facing surface is the tool +
  the requires-tools skill gate; a GUI/tasks-page or CLI facade is not in this
  tree and is named as the deferral with a justification (the per-format tools
  are agent-facing; a UI over them is a separate human-surface affordance).
