# Office Docx Skill - Implementation Leaf (01)

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. If this branch runs a tests-first gate,
> the failing tests ALREADY EXIST and are committed (e2e/suites/office-skills/) -
> your job is to make them pass; do NOT delete, skip, weaken, or rewrite any
> gate test (report suspected-wrong gate tests to the orchestrator instead).
> Library pin: read `06-office-skills/library-picks.md` FIRST - your docx
> library choice comes from there, NOT from this document's seed. Do NOT
> commit. Do NOT git add. Do NOT use read_file on existing source - explore with
> search_files or terminal cat. After writing a file, do NOT read it back -
> write once and stop. Report what you built, files touched, and deviations.

## Meta

- **Parent:** ../../06-office-skills/orchestrator.md
- **Scope:** The `office_docx` builtin tool + the `office-docx` skill body,
  backed by the research-picked Go docx library.
- **Dependencies:** 00-research GO (docx library resolved), 00T-tests VALIDATED
- **Estimated Context:** 20K
- **Concurrency Group:** A

## Goal

Deliver a builtin model tool `office_docx` that creates / reads / edits /
validates Word .docx documents in the session working directory, and the
`office-docx` skill body whose frontmatter gates it on that tool. The tool's
JSON behavior contract is fixed (orchestrator C11.2); only the internal call
into the chosen docx library is research-dependent. Do NOT touch
`internal/daemon/components.go` - registration is the orchestrator's Phase-3
step (C11.5).

## Context

- Tool precedent: `internal/tools/builtin/spreadsheet_write.go`
  `NewSpreadsheetWriteTool()` + its methods (Name/Category/IsReadOnly/
  Description/Parameters/Execute) and its path-confinement guard
  (spreadsheet_write.go:103-115 - resolve `path` inside the session working
  directory, refuse otherwise).
- Skill precedent: the dsh `office-docx` body under
  `/tmp/deepseek-harness/packages/skill/skill-office/assets/office-docx/SKILL.md`
  - keep source scripts/files in the task workspace, the runtime + skill
  directory are read-only resources, edit only the requested runs (never
  `paragraph.text` which destroys formatting), do not rebuild documents (loses
  unsupported OOXML features), reopen-and-verify after save, deliver the final
  path. Port the ARCHETYPE, not the python-docx calls.
- Skill frontmatter format (meept): `name`, one-line `description`,
  `requires-tools:` YAML list (internal/skills/models.go `RequiresTools`,
  parser.go:113-121; enforced by executor.go checkRequiredTools).

## Verified Anchors

- `internal/tools/builtin/spreadsheet_write.go:29` - `NewSpreadsheetWriteTool()`
  + Tool method set (the contract to mirror).
- `internal/skills/models.go:46` - `RequiresTools []string`
  (`yaml:"requires-tools"`).
- `/tmp/deepseek-harness/packages/skill/skill-office/assets/office-docx/SKILL.md`
  - the dsh protocol to port (reopen-and-verify, preserve formatting).
- `06-office-skills/library-picks.md` - your docx library pin (READ FIRST).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/tools/builtin/office_docx.go - per C11.2
func NewOfficeDocxTool() *OfficeDocxTool          // Name "office_docx", Category "filesystem"
// Parameters JSON: path(string req, in-session), action("create"|"edit"|"read"|"validate"),
//   blocks []{type:"heading",text,level} | {type:"paragraph",text,style} | {type:"table",rows:[][]string,header_row:bool}
// Execute: resolve path in session cwd -> open/create via chosen docx lib ->
//   apply blocks preserving unrelated runs -> save new file unless in-place ->
//   reopen-and-verify -> return final absolute path. read/validate -> text + summary.
// IsReadOnly() true when action is "read"|"validate"
```

### What This Leaf Consumes

- C11.1 skill bundle layout + requires-tools gate (frontmatter contract)
- C11.5 registration: this leaf does NOT register; orchestrator wires at Phase 3

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | office_docx tool | Create: internal/tools/builtin/office_docx.go | `go build ./internal/tools/... && go vet ./internal/tools/...` | tool file absent; gate e2e RED (tool unregistered) | tool builds; C11.2 method set complete | Given the docx library pinned in library-picks.md vendored into go.mod; When the tool is built; Then `NewOfficeDocxTool()` compiles with Name/Category/IsReadOnly/Parameters/Execute per C11.2 |
| T2 | path confinement + actions | Modify: internal/tools/builtin/office_docx.go | `go vet ./internal/tools/builtin/...` | missing guard | Execute refuses out-of-session paths with the spreadsheet_write-style message; create/read/validate branch correctly | Given a session cwd; When `path` resolves outside it (e.g. `../`); Then Execute returns the refusal error; when inside, action dispatches |
| T3 | office-docx skill body | Create: config/skills/office/office-docx/SKILL.md | `go test -p 2 ./internal/skills/...` (parser) | skill absent | parser reads frontmatter name/description/requires-tools:[office_docx] | Given the SKILL.md with frontmatter; When the skills parser loads it; Then name/description/requires-tools are populated per C11.1 |
| T4 | reopen-and-verify round-trip | (tests already in 00T) | `make e2e-fast-area AREA=office-skills` | gate e2e RED | e2e_office_docx PASS (create->reopen->validate returns final path + text) | Given the gate suite; When the daemon runs office_docx create+read; Then the round-trip returns the final path and the reopened doc contains the requested heading/paragraph |

Library pin as a gate: T1 must import the library chosen in
`library-picks.md` (the seed names in orchestrator Open Questions are the
fallback order, NOT the pick). If library-picks says NO-GO for docx, STOP and
report - this leaf does not dispatch until the user resolves the OPEN QUESTION.

## Self-Verification Checklist

- [ ] office_docx.go implements C11.2 exactly (name, params, actions)
- [ ] Library pin matches library-picks.md; no unioffice anywhere
- [ ] Skill body frontmatter has requires-tools: [office_docx]
- [ ] Does NOT touch internal/daemon/components.go (C11.5)
- [ ] Gate e2e e2e_office_docx passes GREEN; gate tests unmodified
- [ ] No deviations (or documented)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** internal/tools/builtin/office_docx.go,
   config/skills/office/office-docx/SKILL.md
2. **Verify command + output:** `go build ./internal/tools/...`, `go vet
   ./internal/tools/builtin/...`, `make e2e-fast-area AREA=office-skills`
   (last 20 lines)
3. **Deviations:** none / list with rationale

## Review Checklist (For Review Agent)

- [ ] C11.2 satisfied (tool name/params/actions/path guard); library pin from
      library-picks.md honored
- [ ] Skill frontmatter requirement (requires-tools: [office_docx]) present and
      parse-verified
- [ ] Gate tests untouched; e2e_office_docx GREEN
- [ ] No unioffice / GPL-link violation; no scope creep
- [ ] No line-number corruption / debug artifacts

## Notes

- The model-facing surface of this tool is the tool itself + the skill-gate
  wiring; a CLI/TUI/GUI facade is deliberately deferred and documented in the
  04-docs leaf (root AGENTS.md wiring requirement - deferral, not omission).
- ToolActionMap (internal/agent/executor.go) may already have generic file
  action entries; add `office_docx` -> file_read mapping IF needed for read-only
  actions so the tool is not "Unknown action"-denied (bughunt H7 class) - only
  if such a map gates this tool.
