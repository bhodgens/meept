# Office Xlsx Skill - Implementation Leaf (03)

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. If this branch runs a tests-first gate,
> the failing tests ALREADY EXIST and are committed (e2e/suites/office-skills/) -
> your job is to make them pass; do NOT delete, skip, weaken, or rewrite any
> gate test (report suspected-wrong gate tests to the orchestrator instead).
> xlsx library is FIXED: excelize v2.11.0 (BSD-3-Clause, already pinned). Do
> NOT commit. Do NOT git add. Do NOT use read_file on existing source - explore
> with search_files or terminal cat. After writing a file, do NOT read it back -
> write once and stop. Report what you built, files touched, and deviations.

## Meta

- **Parent:** ../../06-office-skills/orchestrator.md
- **Scope:** The `office_workbook` builtin tool + the `office-xlsx` skill body,
  both backed by excelize (already a direct dependency and the engine behind
  the existing `spreadsheet_write` xlsx writer).
- **Dependencies:** 00T-tests VALIDATED (research 00 confirms excelize only)
- **Estimated Context:** 18K
- **Concurrency Group:** A

## Goal

Deliver a builtin model tool `office_workbook` that creates / reads / edits /
validates Excel .xlsx workbooks in the session working directory, and the
`office-xlsx` skill body gated on that tool. xlsx is the resolved default:
`github.com/xuri/excelize/v2 v2.11.0` (go.mod:45) already ships the
`spreadsheet_write` writer, so this leaf extends that engine to a full
create/read/edit/validate model tool - no new library pin.
Do NOT touch `internal/daemon/components.go` - registration is the
orchestrator's Phase-3 step (C11.5).

## Context

- Tool + writer precedents: `internal/tools/builtin/spreadsheet_write.go`
  `NewSpreadsheetWriteTool()` (method set + path-confinement guard at
  spreadsheet_write.go:103-115) and `internal/tools/builtin/spreadsheet_xlsx.go:24`
  `writeXLSX(w io.Writer, sheets []SheetSpec)` + `SheetSpec{Name,Headers,Rows,
  HighlightRows}` - reuse the SheetSpec field names for the `create` action so
  the writer path is shared.
- Skill precedent: the dsh `office-xlsx` body under
  `/tmp/deepseek-harness/packages/skill/skill-office/assets/office-xlsx/SKILL.md`
  - load with `data_only=false` so formulas survive; inspect affected cell
  types / formulas / styles / merged ranges / referenced ranges before editing;
  a DataFrame/workbook round-trip can lose sheets/formulas/charts - write back
  to intended ranges; **writing a formula does not calculate its result**
  (excelize does not evaluate formulas; `data_only=true` may return absent or
  stale cached values - never report them as newly calculated); reopen-and-
  verify sheet names/values/types/formulas/styles/totals; preserve numbers,
  dates, booleans, identifiers as intended cell types; deliver the final path.
  Port the ARCHETYPE, not the openpyxl calls.
- Skill frontmatter format (meept): `name`, one-line `description`,
  `requires-tools:` YAML list (internal/skills/models.go:46, parser.go:113-121,
  executor.go checkRequiredTools).

## Verified Anchors

- `go.mod:45` - `github.com/xuri/excelize/v2 v2.11.0` (direct dep, already used).
- `internal/tools/builtin/spreadsheet_xlsx.go:24` - `writeXLSX(w io.Writer,
  sheets []SheetSpec)` (the shared writer).
- `internal/tools/builtin/spreadsheet_write.go:29` - `NewSpreadsheetWriteTool()`
  + Tool method set (the contract to mirror).
- `/tmp/deepseek-harness/packages/skill/skill-office/assets/office-xlsx/SKILL.md`
  - the dsh protocol to port (data_only=false, formulas-not-evaluated).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/tools/builtin/office_workbook.go - per C11.4
func NewOfficeWorkbookTool() *OfficeWorkbookTool    // Name "office_workbook", Category "filesystem"
// Parameters JSON: path(string req, in-session), action("create"|"edit"|"read"|"validate"),
//   sheets []{name,headers:[]string,rows:[][]any,highlight_rows:[]int}  // create aligns with writeXLSX/SheetSpec
// Execute: resolve path in session cwd -> create (excelize.NewFile or reuse writeXLSX)
//   / read (sheet names + typed cell values, data_only=false) / edit (targeted cell/style
//   change on existing ranges, save separate result unless in-place) / validate (reopen;
//   check sheet names, values, types, formulas, styles, totals) -> return final absolute path.
// IsReadOnly() true when action=="read"|"validate". Formulas are preserved, never evaluated.
```

### What This Leaf Consumes

- C11.1 skill bundle layout + requires-tools gate
- C11.5 registration: this leaf does NOT register; orchestrator wires at Phase 3

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | office_workbook tool | Create: internal/tools/builtin/office_workbook.go | `go build ./internal/tools/... && go vet ./internal/tools/...` | tool file absent; gate e2e RED | tool builds; C11.4 method set complete | Given excelize v2.11.0 pinned; When the tool is built; Then `NewOfficeWorkbookTool()` compiles with Name/Category/IsReadOnly/Parameters/Execute per C11.4 |
| T2 | path confinement + create seam | Modify: internal/tools/builtin/office_workbook.go | `go vet ./internal/tools/builtin/...` | missing guard / no writer share | Execute refuses out-of-session paths; `create` reuses the SheetSpec/writeXLSX writer path | Given a session cwd; When `path` resolves outside it; Then Execute returns the refusal error; when inside create, sheets map to SheetSpec |
| T3 | read/edit keep formulas (data_only=false) | Modify: internal/tools/builtin/office_workbook.go | `go test -p 2 ./internal/tools/builtin/...` (if any unit pins) | formula cell lost / stale result reported | read returns formula text unchanged; edit preserves it; never a "calculated" claim | Given a workbook with a formula cell; When read/validate runs (data_only=false); Then the formula string is preserved and NOT reported as a computed value |
| T4 | office-xlsx skill body | Create: config/skills/office/office-xlsx/SKILL.md | `go test -p 2 ./internal/skills/...` (parser) | skill absent | parser reads frontmatter name/description/requires-tools:[office_workbook] | Given the SKILL.md; When the skills parser loads it; Then name/description/requires-tools populated per C11.1 |
| T5 | reopen-and-verify round-trip | (tests already in 00T) | `make e2e-fast-area AREA=office-skills` | gate e2e RED | e2e_office_xlsx PASS (create->reopen->validate returns final path + sheet names + typed cells); e2e_office_gate PASS | Given the gate suite; When the daemon runs office_workbook create+read + the office-xlsx gate scenario; Then round-trip returns path + sheet values; gate refuses when tool unregistered |

## Self-Verification Checklist

- [ ] office_workbook.go implements C11.4 exactly (name, params, actions)
- [ ] excelize is the only workbook dependency; no unioffice / new pin
- [ ] Skill body frontmatter has requires-tools: [office_workbook]
- [ ] Does NOT touch internal/daemon/components.go (C11.5)
- [ ] Gate e2e e2e_office_xlsx + e2e_office_gate pass GREEN; gate tests unmodified
- [ ] No deviations (or documented)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** internal/tools/builtin/office_workbook.go,
   config/skills/office/office-xlsx/SKILL.md
2. **Verify command + output:** `go build ./internal/tools/...`, `go vet
   ./internal/tools/builtin/...`, `make e2e-fast-area AREA=office-skills`
   (last 20 lines)
3. **Deviations:** none / list with rationale

## Review Checklist (For Review Agent)

- [ ] C11.4 satisfied (tool name/params/actions/path guard); excelize v2.11.0
      only; create reuses the SheetSpec/writeXLSX seam
- [ ] Formula semantics correct (data_only=false, preserved, never "calculated")
- [ ] Skill frontmatter requirement (requires-tools: [office_workbook]) present
      and parse-verified
- [ ] Gate tests untouched; e2e_office_xlsx + e2e_office_gate GREEN
- [ ] No unioffice / GPL-link violation; no scope creep
- [ ] No line-number corruption / debug artifacts

## Notes

- xlsx is the resolved / proven partner in this branch - it is grouped with a
  research gate only to CONFIRM excelize on the record (orchestrator OPEN
  QUESTION #3 is already RESOLVED: meept go 1.26.0 fits excelize's go 1.25.0).
- The model-facing surface of this tool is the tool itself + the skill-gate
  wiring; a GUI/tasks-page facade is deferred and documented in 04-docs.
- ToolActionMap: add `office_workbook` -> file_read mapping IF the live action
  map would otherwise deny a read-only call (bughunt H7 class) - only if such a
  map gates this tool.
