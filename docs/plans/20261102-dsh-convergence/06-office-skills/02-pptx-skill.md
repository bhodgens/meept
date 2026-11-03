# Office Pptx Skill - Implementation Leaf (02)

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. If this branch runs a tests-first gate,
> the failing tests ALREADY EXIST and are committed (e2e/suites/office-skills/) -
> your job is to make them pass; do NOT delete, skip, weaken, or rewrite any
> gate test (report suspected-wrong gate tests to the orchestrator instead).
> Library pin: read `06-office-skills/library-picks.md` FIRST - your pptx
> library choice comes from there, NOT from this document. Do NOT commit. Do
> NOT git add. Do NOT use read_file on existing source - explore with
> search_files or terminal cat. After writing a file, do NOT read it back -
> write once and stop. Report what you built, files touched, and deviations.

## Meta

- **Parent:** ../../06-office-skills/orchestrator.md
- **Scope:** The `office_pptx` builtin tool + the `office-pptx` skill body,
  backed by the research-picked Go pptx library.
- **Dependencies:** 00-research GO (pptx library resolved), 00T-tests VALIDATED
- **Estimated Context:** 20K
- **Concurrency Group:** A

## Goal

Deliver a builtin model tool `office_pptx` that creates / reads / edits /
validates PowerPoint .pptx presentations in the session working directory, and
the `office-pptx` skill body gated on that tool. The tool's JSON behavior
contract is fixed (orchestrator C11.3); only the internal call into the chosen
pptx library is research-dependent. Do NOT touch `internal/daemon/components.go`
- registration is the orchestrator's Phase-3 step (C11.5).

## Context

- Tool precedent: `internal/tools/builtin/spreadsheet_write.go`
  `NewSpreadsheetWriteTool()` + its method set and its path-confinement guard
  (spreadsheet_write.go:103-115 - resolve `path` inside the session working
  directory, refuse otherwise).
- Skill precedent: the dsh `office-pptx` body under
  `/tmp/deepseek-harness/packages/skill/skill-office/assets/office-pptx/SKILL.md`
  - inspect slide layouts / text runs / images / tables / charts before editing;
  change ONLY the requested content and preserve mixed formatting (rebuilding
  slides discards animation / SmartArt / extension content); set slide
  dimensions, text sizes, and chart data explicitly; reopen-and-verify slide
  count, content, chart data, and notes; deliver the final path. A library's
  PPTX support is NOT a rendering engine - validate content, not appearance.
  Port the ARCHETYPE, not the python-pptx calls.
- Skill frontmatter format (meept): `name`, one-line `description`,
  `requires-tools:` YAML list (internal/skills/models.go:46, parser.go:113-121,
  executor.go checkRequiredTools).

## Verified Anchors

- `internal/tools/builtin/spreadsheet_write.go:29` - `NewSpreadsheetWriteTool()`
  + Tool method set (the contract to mirror).
- `internal/skills/models.go:46` - `RequiresTools []string`
  (`yaml:"requires-tools"`).
- `/tmp/deepseek-harness/packages/skill/skill-office/assets/office-pptx/SKILL.md`
  - the dsh protocol to port.
- `06-office-skills/library-picks.md` - your pptx library pin (READ FIRST).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/tools/builtin/office_pptx.go - per C11.3
func NewOfficePptxTool() *OfficePptxTool            // Name "office_pptx", Category "filesystem"
// Parameters JSON: path(string req, in-session), action("create"|"edit"|"read"|"validate"),
//   slides []{title:string, body:[]string, notes:string, image_path:string(opt, in-session)}
// Execute: resolve path in session cwd -> open/create via chosen pptx lib ->
//   apply slides preserving unrelated slides/formatting -> save new file unless
//   in-place -> reopen-and-verify slide count/content/notes -> return final absolute path.
// read/validate -> slide summary (count, titles, notes). IsReadOnly() true when action=="read"|"validate"
```

### What This Leaf Consumes

- C11.1 skill bundle layout + requires-tools gate
- C11.5 registration: this leaf does NOT register; orchestrator wires at Phase 3

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | office_pptx tool | Create: internal/tools/builtin/office_pptx.go | `go build ./internal/tools/... && go vet ./internal/tools/...` | tool file absent; gate e2e RED | tool builds; C11.3 method set complete | Given the pptx library pinned in library-picks.md vendored into go.mod; When the tool is built; Then `NewOfficePptxTool()` compiles with Name/Category/IsReadOnly/Parameters/Execute per C11.3 |
| T2 | path confinement + actions | Modify: internal/tools/builtin/office_pptx.go | `go vet ./internal/tools/builtin/...` | missing guard | Execute refuses out-of-session paths; create/read/validate branch correctly; image_path also confined in-session | Given a session cwd; When `path` or `image_path` resolves outside it; Then Execute returns the refusal error |
| T3 | office-pptx skill body | Create: config/skills/office/office-pptx/SKILL.md | `go test -p 2 ./internal/skills/...` (parser) | skill absent | parser reads frontmatter name/description/requires-tools:[office_pptx] | Given the SKILL.md; When the skills parser loads it; Then name/description/requires-tools populated per C11.1 |
| T4 | reopen-and-verify round-trip | (tests already in 00T) | `make e2e-fast-area AREA=office-skills` | gate e2e RED | e2e_office_pptx PASS (create->reopen->validate returns final path + slide content) | Given the gate suite; When the daemon runs office_pptx create+read; Then the round-trip returns the final path and the reopened deck has the requested title/body |

Library pin as a gate: T1 must import the library chosen in
`library-picks.md`. The Go PPTX ecosystem is thin; if library-picks says NO-GO
for pptx, STOP and report - this leaf does not dispatch until the user resolves
the OPEN QUESTION (research-gated rule 1: the user picks, never silently).

## Self-Verification Checklist

- [ ] office_pptx.go implements C11.3 exactly (name, params, actions)
- [ ] Library pin matches library-picks.md; no unioffice anywhere
- [ ] Skill body frontmatter has requires-tools: [office_pptx]
- [ ] Does NOT touch internal/daemon/components.go (C11.5)
- [ ] Gate e2e e2e_office_pptx passes GREEN; gate tests unmodified
- [ ] No deviations (or documented)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** internal/tools/builtin/office_pptx.go,
   config/skills/office/office-pptx/SKILL.md
2. **Verify command + output:** `go build ./internal/tools/...`, `go vet
   ./internal/tools/builtin/...`, `make e2e-fast-area AREA=office-skills`
   (last 20 lines)
3. **Deviations:** none / list with rationale

## Review Checklist (For Review Agent)

- [ ] C11.3 satisfied (tool name/params/actions/path guard incl. image_path);
      library pin from library-picks.md honored
- [ ] Skill frontmatter requirement (requires-tools: [office_pptx]) present and
      parse-verified
- [ ] Gate tests untouched; e2e_office_pptx GREEN
- [ ] No unioffice / GPL-link violation; no scope creep
- [ ] No line-number corruption / debug artifacts

## Notes

- The model-facing surface of this tool is the tool itself + the skill-gate
  wiring; a CLI/TUI/GUI facade is deferred and documented in 04-docs (root
  AGENTS.md wiring requirement - documented deferral).
- ToolActionMap: add `office_pptx` -> file_read mapping IF the live action map
  would otherwise "Unknown action"-deny a read-only call (bughunt H7 class) -
  only if such a map gates this tool.
