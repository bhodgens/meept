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

