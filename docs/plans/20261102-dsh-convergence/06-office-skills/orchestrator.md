# 06 Office Skills - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Branch
- **Parent:** ../../master.md (root master, branch 06)
- **Children:** 00-research-assignment.md (research gate), 00T-tests.md (test
  gate), 01-docx-skill.md, 02-pptx-skill.md, 03-xlsx-skill.md, 04-docs.md
- **Scope:** Deliver a bundled skill-office capability (docx / pptx / xlsx)
  backed by a vetted many-star OSS Go library per format, gated on the
  existing meept `requires-tools` skill frontmatter. xlsx picks excelize
  (already pinned); docx/pptx picks are UNRESOLVED and gated by a research
  leaf that dispatches before any implementation.

## Goal

Meept ships no Office-authoring skills and no bundled document library tier
(DSH audit: skill-office, "GAP full", top recommendation #5 - the office
skill-bundle pattern is the most-requested practical automation case). The
engine exists (excelize is already a direct dependency and already powers the
`spreadsheet_write` tool writer), but no model-facing skill reads, edits, or
validates .docx / .pptx / .xlsx documents. This branch closes that gap by:

1. Resolving, with evidence, a vetted OSS Go library for each of docx and
   pptx (research-gated - the pure-Go OOXML ecosystem is thin and must be
   verified empirically, not assumed). xlsx is dominated by excelize
   (BSD-3-Clause, ~21k stars), already pinned at v2.11.0 - see
   `## Excelize compatibility`.
2. Shipping an `office` skill bundle under `config/skills/office/` (router +
   office-docx + office-pptx + office-xlsx), each format skill carrying a
   `requires-tools:` gate on its backing builtin tool, so execution is refused
   when the tool is unavailable (meept `internal/skills` executor gate).
3. Adding three builtin model tools - `office_docx`, `office_pptx`,
   `office_workbook` - that create / read / edit / validate documents in the
   session working directory, following the repo's path-safety + reopen-and-
   verify conventions (DSH audit: office-docx / office-xlsx / office-pptx are
   protocol-heavy: keep source in the workspace, the runtime is a read-only
   resource, reopen-and-verify after save, deliver the final path).
4. Documenting the bundle (`docs/workflows/office-skills.md`) and the
   requires-tools gate behavior.

License gate (user memory, frozen master D6): GPL/AGPL acceptable ONLY as
subprocess deps, never vendor/link into shipped binaries. **unioffice is
REJECTED** (commercial EULA) regardless of stars.

## Architecture

The `office` router skill is the model-facing entry point (mirrors dsh
`office`/SKILL.md: format skills chosen by extension). Each format skill is a
moderate-instruction SKILL.md whose frontmatter `requires-tools:` lists its
backing builtin tool. The tools live in `internal/tools/builtin`, modeled on
the existing `SpreadsheetWriteTool` (spreadsheet_write.go) so they inherit the
repo's session-working-directory path confinement and structured
Parameters/Execute contract. A skill body is loaded as read-only instruction
text; write authority stays with the tool + the existing fence engine (root
AGENTS.md), and file writes are confined to the session working directory. No
new engines are built - this is a skill bundle + three tool seams over
libraries the repo already tolerates (excelize) or that research picks (docx /
pptx). The two research-gated formats are adaptable in place: the tool's JSON
contract is library-agnostic, so a candidate swap changes only the internal
call into the chosen library, never the model tool or the skill body.

## Interface Contracts

### C11.1: Office skill bundle layout + requires-tools gate (Owner: 04-docs + each format leaf; Consumers: all)

```
config/skills/office/
  office/SKILL.md           # router: pick office-docx|office-pptx|office-xlsx by extension
  office-docx/SKILL.md      # frontmatter requires-tools: [office_docx]
  office-pptx/SKILL.md      # frontmatter requires-tools: [office_pptx]
  office-xlsx/SKILL.md      # frontmatter requires-tools: [office_workbook]
```
- Each format SKILL.md frontmatter: `name`, one-line `description`, `requires-tools:`
  (YAML list of full registered tool names). Parse path:
  `internal/skills/models.go` `RequiresTools []string` (`yaml:"requires-tools"`,
  stringList) populated by `parser.go` `ParseSkillMetadataOnly` into
  `SkillIndexEntry.RequiresTools`; enforced by `internal/skills/executor.go`
  `checkRequiredTools` via `ToolAvailabilityFunc func(toolName string) bool`
  (WithToolAvailability / SetToolAvailability, executor.go:79-92,145-162). Gate
  fires only when `skills.validate_prerequisites` is enabled AND a checker is
  configured; otherwise checkRequiredTools returns nil (skills.md:84-89).
  Failure message contract: `skill <name> requires unavailable tool(s): <list>
  - run 'meept doctor' to diagnose`.
- The bundle ships via `config/skills/` -> `~/.meept/skills` on `make install`
  / `make sync-config` (merge, no-clobber; new defaults land as `<file>.new` -
  root AGENTS.md + `scripts/install-sync.py`). Router + format skills align
  with the dsh `office`/SKILL.md routing contract (choose the workflow skill by
  extension; a format skill is NOT a rendering engine).

### C11.2: office_docx builtin tool (Owner: 01-docx-skill.md)

```
// internal/tools/builtin/office_docx.go (NEW). Models SpreadsheetWriteTool:
// Name() "office_docx"; Category() "filesystem"; IsReadOnly() == (action=="read"|"validate")
// Parameters (JSON object): path (string, req, must resolve inside the session
//   working directory - refuse otherwise, spelling spreadsheet_write.go:103-115),
//   action ("create"|"edit"|"read"|"validate", req), and the format payload:
//   blocks []{type:"heading",text,level:"0..6"} | {type:"paragraph",text,style} |
//   {type:"table",rows:[][]string,header_row:bool}
// Execute: resolve path in session cwd -> open/create via the research-picked
//   docx library -> apply blocks (preserve unrelated runs/formatting; never rebuild
//   documents, per dsh office-docx) -> save to a NEW file unless in-place is
//   requested -> reopen and verify requested content/formatting -> return final
//   absolute path. read/validate return extracted text + a structure summary.
// Internal call to the chosen library is the ONLY research-dependent seam.
```

### C11.3: office_pptx builtin tool (Owner: 02-pptx-skill.md)

```
// internal/tools/builtin/office_pptx.go (NEW). Same shape as C11.2:
// Name() "office_pptx"; Category() "filesystem"; IsReadOnly() == (action=="read"|"validate")
// Parameters: path (req, in-session), action ("create"|"edit"|"read"|"validate"),
//   slides []{title:string, body:[]string, notes:string, image_path:string(opt, in-session)}
// Modeled on dsh office-pptx: inspect slide layouts + runs before editing, change only
//   requested content (preserve mixed formatting / charts / SmartArt - never rebuild a
//   deck), set slide dimensions + text sizes explicitly, reopen-and-verify slide count,
//   content, and notes, deliver final path.
```

### C11.4: office_workbook builtin tool (Owner: 03-xlsx-skill.md)

```
// internal/tools/builtin/office_workbook.go (NEW). Backed by excelize v2.11.0
// (already a direct dep + writer precedent internal/tools/builtin/spreadsheet_xlsx.go
// writeXLSX + SheetSpec{Name,Headers,Rows,HighlightRows}).
// Name() "office_workbook"; Category() "filesystem"; IsReadOnly() == (action=="read"|"validate")
// Parameters: path (req, in-session), action ("create"|"edit"|"read"|"validate"),
//   sheets []{name,headers:[]string,rows:[][]any,highlight_rows:[]int} // create aligns with writeXLSX
// Action semantics: create = new workbook (reuse writeXLSX seam or excelize directly);
//   read = sheet names + typed cell values (formulas preserved, data_only=false);
//   edit = targeted cell/style change on the existing workbook's ranges, save a separate
//   result unless in-place requested; validate = reopen and check sheet names/values/types/
//   formulas/style/totals. Formulas are NOT evaluated (excelize does not compute cached
//   results) - never report stale data_only values as newly calculated.
```

### C11.5: Registration + model-facing wiring (Owner: ORCHESTRATOR at Phase 3)

```
// New builtin tools register at the daemon wiring seam exactly like the
// spreadsheet precedent (internal/daemon/components.go:7458
// `registry.Register(builtin.NewSpreadsheetWriteTool())`). The ORCHESTRATOR
// adds, at Phase 3 integration, three lines to internal/daemon/components.go:
//   registry.Register(builtin.NewOfficeDocxTool())
//   registry.Register(builtin.NewOfficePptxTool())
//   registry.Register(builtin.NewOfficeWorkbookTool())
// Leaves 01-03 do NOT touch components.go (parallel shared-file hazard) and do
// NOT register - they deliver constructor + SKILL.md only. Registration is an
// explicit review-checklist + integration item, not silent. Each office tool's
// model-facing surface = the tool itself (agent wiring via the requires-tools
// gate); a CLI/TUI/GUI facade is NOT required - document the deferral in 04.
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00R | 00-research-assignment.md | research gate | none | 18K | Phase 0 |
| 00T | 00T-tests.md | test gate | none | 35K | Phase 0 |
| 01 | 01-docx-skill.md | leaf | 00R GO, 00T VALIDATED | 20K | A |
| 02 | 02-pptx-skill.md | leaf | 00R GO, 00T VALIDATED | 20K | A |
| 03 | 03-xlsx-skill.md | leaf | 00T VALIDATED (+ 00R confirm) | 18K | A |
| 04 | 04-docs.md | docs leaf | 00R GO, 01-03 REVIEWED | 14K | B |

Phases: the branch is research-gated (reference/research-gated-plan-trees.md).
Phase 0 dispatches `00-research-assignment` (research gate) AND `00T-tests`
(test gate) IN PARALLEL - the tests are written against the tool behavior
contracts (C11.2-C11.4), not the concrete docx/pptx library, so they do not
depend on the research outcome. Implementation leaves 01-03 dispatch only after
BOTH gates pass: research reaches GO with a chosen library per format AND tests
reach TESTS_VALIDATED. 04-docs (router SKILL.md + feature doc + gate
documentation) dispatches after research GO and 01-03 REVIEWED so it documents
the real library picks and tool names.

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution` + references/tests-first-gate.md +
references/research-gated-plan-trees.md.

### Phase 0: Gates (research + tests, before ANY implementation dispatch)

Dispatch `00-research-assignment.md` and `00T-tests.md` as subagents in
parallel. Research brief: "Resolve, WITH EVIDENCE, a vetted OSS Go library for
docx and pptx (xlsx=excelize, confirm only); record license + stars + go.mod
go directive + last-release activity + a working API signature per candidate;
unioffice is REJECTED (commercial EULA); write the GO/NO-GO report to
`06-office-skills/library-picks.md`; do NOT commit." Test brief: "Write tests
ONLY against the tool behavior contracts C11.2-C11.4; tests fail RED for
missing-implementation reasons (tools not registered / not built into the
daemon); do NOT commit."

Validate IN-SESSION (never delegate review): (a) against the `## Test Gate`
matrix below, applying the delta protocol (update the LESSER artifact; coverage
converges up); (b) against the research leaf's evidence checklist (every
candidate library has a license + stars + go-directive + activity evidence URL
-or is marked `OPEN QUESTION`; unioffice absent or marked REJECTED). Commit the
gate tests; set tests TESTS_VALIDATED. For research: GO (chosen library per
format recorded in `library-picks.md`) clears leaves 01/02 to dispatch; a NO-GO
for docx or pptx stops that leaf (user decides per research-gated rule 1 - the
user picks, never the agent) and is raised as an `OPEN QUESTION` to the parent.

### Phase 1: Dispatch Concurrency Group [A]

Dispatch 01, 02, 03 together via `delegate_task`: goal = "Implement all tasks
from [leaf]"; context = full leaf text + contracts C11.1-C11.5 INLINED +
conventions + relevant source INLINED (anchor snippets) + the no-commit,
no-git-add, no-read_file-on-source, write-once-and-stop rules. Each leaf is
pinned to its library (01/02 from `library-picks.md`; 03 = excelize). They do
NOT register in components.go (C11.5) - the orchestrator wires at Phase 3.

### Phase 2: Review and Commit Each Child

Review in-session (the main model - never a delegated reviewer). Check leaf
spec, contracts, Review Checklist; run the leaf's verify commands; gate-test
diff must be empty. Pass: commit + status REVIEWED. Gaps: re-dispatch with
findings, max 3 cycles, then escalate to parent.

### Phase 3: Integration Review

After ALL children REVIEWED: (1) verify contracts C11.1-C11.5 across the
combined branch; (2) add the three `registry.Register(builtin.NewOffice*Tool())`
lines to `internal/daemon/components.go` (C11.5) - the tool-surface wiring the
reviews never reached; (3) `go build ./...` then `make test` (short, -p 2); (4)
run `e2e/suites/office-skills/` in isolation (`make e2e-fast-area
AREA=office-skills`) - GREEN now that registration exists; (5) `go vet ./...`;
(6) line-number corruption grep over all new source. Gaps: re-dispatch the
failing leaf. Pass: set children COMPLETE and report COMPLETE to parent.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts C11.1-C11.5 satisfied (signatures, types, paths,
      library pins match `library-picks.md`)
- [ ] All specified files created/modified at exact paths
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification
      (any such diff = automatic NEEDS_FIXES)
- [ ] Tests written and passing (e2e for features per root e2e policy)
- [ ] Code follows project conventions (no ignored errors, no mutex across
      I/O, typed-nil guards, predictable-ID prevention)
- [ ] No scope creep; no obvious bugs or security issues
- [ ] No debug artifacts (print debugging, TODOs, placeholders)
- [ ] No line-number corruption (`     N|` prefixes) in source files
- [ ] Library license gate holds: no GPL/AGPL vendored/linked into shipped
      binaries; unioffice absent everywhere
- [ ] Phase 3: three `registry.Register` lines added to components.go; office
      e2e suite GREEN in isolation
- [ ] Model-facing rule: every tool (`office_docx`/`office_pptx`/
      `office_workbook`) has its tool registration wired (C11.5) and the skill
      gate ties it to the model surface; CLI/TUI/GUI facade deferral is
      documented in 04 (root AGENTS.md wiring requirement)

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language:** Go 1.22+ (repo pins GOTOOLCHAIN; go.mod `go 1.26.0`,
  toolchain go1.26.5). All new code Go; skill bodies + docs are markdown.
- **Naming:** exported/unexported per repo; model tool names snake_case
  (`office_docx`, `office_pptx`, `office_workbook`); constructors
  `NewOfficeDocxTool()` etc.
- **Imports:** stdlib group first, then third-party; no unused (U1000-strict).
  New library deps (docx/pptx) are resolved ONLY by research leaf 00 before any
  `go get`; excelize is already pinned (v2.11.0).
- **Error handling:** wrap with context; no ignored errors; no bare panic(err).
- **Security:** predictable-ID prevention (pkg/id.Generate, never UnixNano);
  typed-nil guards on every Set*/wiring seam; tool paths must resolve inside
  the session working directory and refuse outside (spreadsheet_write.go:
  103-115 precedent); office tools that only read set IsReadOnly()=true.
- **Testing:** NEW feature tests go in the hermetic e2e tier (root AGENTS.md);
  ALL office-skill behavior is e2e-covered in `e2e/suites/office-skills/` +
  `e2e/manifest.json` path_map same commit (the 00T test gate owns this).
- **Formatting:** gofmt before reporting; ASCII only in new .md.
- **Model-facing rule:** every capability ships a user-facing surface (CLI,
  TUI, GUI, or HTTP) + agent wiring, or documents the deferral (04 documents
  the tool-only surface + skill-gate wiring explicitly).

## Requirement Traceability

| Req | Source (user words, verbatim when possible) | Plan element (leaf/task/contract) | Verified by |
|-----|---------------------------------------------|-----------------------------------|-------------|
| R5 | "find some good (many stars) online to use" | 00-research-assignment.md -> docx/pptx picks (C11.2/C11.3); xlsx=excelize (C11.4) | research leaf evidence checklist + Test Gate F14 |
| R5b | license gate: "GPL/AGPL OK as subprocess deps, never vendor/link into shipped binaries" | every impl leaf library pin (review item) | library-picks.md license evidence; Review Checklist license gate |
| D6 | "unioffice is commercial-EULA and REJECTED; excelize ... is the xlsx choice" | all leaves; research REJECTS unioffice | research leaf + Review Checklist |
| R4 / D7 | "END TO END and unit tests for all features as a prerequisite" | 00T-tests.md Phase 0 gate | Test Gate matrix + Phase 0 validation |

DEFERRED / escalated rows: if research returns NO-GO for docx or pptx (no
passing Go library), the corresponding impl leaf stays BLOCKED and the choice
is escalated to the user as an OPEN QUESTION (research-gated rule 1) - the
user picks the library or approves a non-Go runtime, never the agent silently.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00-research-assignment | PENDING | 0 | research gate; GO unlocks 01-04 |
| 00T-tests | PENDING | 0 | Phase 0 test gate |
| 01-docx-skill | PENDING | 0 | gated on 00R GO + 00T VALIDATED |
| 02-pptx-skill | PENDING | 0 | gated on 00R GO + 00T VALIDATED |
| 03-xlsx-skill | PENDING | 0 | gated on 00T VALIDATED (+00R confirm) |
| 04-docs | PENDING | 0 | after 01-03 REVIEWED + 00R GO |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: implementation children 01-04 stay PENDING until the branch test
gate reaches TESTS_VALIDATED AND the research gate reaches GO.

## Integration Test Plan

- `go build ./...` ; `make test` (short, -p 2) - all GREEN.
- `make e2e-fast-area AREA=office-skills` - the hermetic office-skills suite
  (after Phase 3 registration). Assertions: `office_docx`/`office_pptx`/
  `office_workbook` present in the live daemon's tool registry; `office-xlsx`
  skill execution is REFUSED with `requires unavailable tool(s)` when the tool
  is not registered (validates the gate red path); a create->reopen->validate
  round-trip returns the final path and its content (validates reopen-and-verify).
- Cross-boundary: office-pptx/office-docx e2e round-trips exercise the
  research-picked libraries through the tool seam (proves C11.2/C11.3 compose
  with their pinned libs).
- `scripts/config-drift-check.py` clean (config/skills bundle added via the
  standard ship path).
- No graphs-change expected (no bus/RPC surface); verify with `make
  graphs-check` anyway in case builtin tool registration alters the tool map.

## Test Gate

Test leaves: 00T-tests.md (single test leaf covering F14) - dispatched in
Phase 0 in parallel with the research gate, validated by the control agent
(orchestrator, in-session). Root e2e policy applies: no new unit files for
features; ALL office behavior is e2e-covered. (The master F14 row lists
"branch unit | branch e2e"; this branch reconciles unit to `n/a` because root
AGENTS.md forbids new feature unit files - recorded here as the delta.)

| # | Feature (from plan) | Source | Unit tests | E2E tests | Status |
|---|---------------------|--------|-----------|-----------|--------|
| F14a | office_docx create/edit/read/validate | 01-docx-skill.md (C11.2) | n/a (e2e-only per root policy) | e2e_office_docx: round-trip create->reopen->validate via live daemon | PENDING |
| F14b | office_pptx create/edit/read/validate | 02-pptx-skill.md (C11.3) | n/a (e2e-only per root policy) | e2e_office_pptx: round-trip create->reopen->validate via live daemon | PENDING |
| F14c | office_workbook create/edit/read/validate | 03-xlsx-skill.md (C11.4) | n/a (e2e-only per root policy) | e2e_office_xlsx: round-trip create->reopen->validate via live daemon | PENDING |
| F14d | requires-tools gate refuses on missing tool | 04-docs.md + C11.1 | n/a (gate behavior) | e2e_office_gate: office-xlsx execution refused with `requires unavailable tool(s)` when tool unregistered | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: [date, feature ID, direction, one-line reason]

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as dated
addenda. Never edit a dispatched leaf document mid-execution. The research
leaf's `library-picks.md` is the authority for library choices at dispatch
time; leaf seed libraries LOSE to it if they conflict (research-gated rule 2).

## Open Questions

1. **docx library - UNRESOLVED.** No pure-Go many-star docx writer/editor is
   assumed. Research leaf 00 verifies candidates empirically (license/stars/
   go-directive/activity). unioffice is REJECTED regardless. If research
   returns NO-GO, escalate to the user (they choose the library or a runtime
   departure) - never silently pick.
2. **pptx library - UNRESOLVED.** The Go PPTX authoring ecosystem is
   thin; the same evidence-first research gate applies. NO-GO escalates to
   the user.
3. **excelize Go-version fit - RESOLVED, no open question.** meept go.mod is
   `go 1.26.0` (toolchain go1.26.5); excelize v2.11.0 go.mod is `go 1.25.0`
   (verified on disk at `.../excelize/v2@v2.11.0/go.mod`). excelize is an
   already-building direct dependency (it powers `spreadsheet_write`). Fits;
   no pin change needed.
4. Research leaf is a RESEARCH gate, not a user gate: it records evidence and
   a recommendation; library adoption still needs Phase-1 approval from the
   parent/user only on NO-GO (research-gated rule 1 applies to NO-GO, not to
   GO-with-a-clear-winner).

## Notes

- The whole branch SOURCES from the DSH audit `skills-schedule-storage.md`
  FEATURE 1e (skill-office) + the actual dsh source under
  `/tmp/deepseek-harness/packages/skill/skill-office/` (src/index.ts registers
  7 bundled skills over a `dsh-office` provider, rank 600; office-docx /
  office-xlsx / office-pptx SKILL.md bodies are protocol-heavy: keep source in
  the task workspace, the runtime and skill directory are read-only resources,
  name scripts to avoid shadowing, reopen-and-verify, deliver the final path).
  meept ports the ARCHETYPE (a bundled skill set + an environment/tool gate),
  NOT the literal python-docx/openpyxl/python-pptx + LibreOffice Kit runtime -
  meept's runtime is Go libraries (C11). Grep the dsh source before porting any
  constant; the audit file:line is the re-read point, not the final say.
- meept anchors: `go.mod:45` already pins `github.com/xuri/excelize/v2 v2.11.0`;
  `internal/tools/builtin/spreadsheet_xlsx.go:24` `writeXLSX(w io.Writer, sheets
  []SheetSpec)` is the writer precedent (SheetSpec{Name,Headers,Rows,
  HighlightRows}); `internal/tools/builtin/spreadsheet_write.go:29`
  `NewSpreadsheetWriteTool()` is the tool-contract precedent; wiring precedent
  `internal/daemon/components.go:7458`
  `registry.Register(builtin.NewSpreadsheetWriteTool())`; skills gate anchors
  `internal/skills/parser.go:113-121` (RequiresTools populates SkillIndexEntry),
  `internal/skills/executor.go:234-278` (checkRequiredTools), `models.go:46`
  `RequiresTools []string`.
- The `office` skill bundle is a `requires-tools`-gated SKILL; it does NOT use
  SKILL.state mode (`skills.state` is a separate opt-in, root AGENTS.md) - no
  state-mode dependency.
- Registration of the three office tools is intentionally a single Phase-3
  orchestrator step (C11.5) to avoid three parallel leaves editing the shared
  `internal/daemon/components.go` (skill Pitfall 14 shared-file writes).
