# Office Skills (docx, pptx, xlsx)

Meept can create, read, edit, and validate Microsoft Office files through three
builtin tools, surfaced to the model as a bundle of skills plus a router.

This page documents the bundle's layout, the three tools and their action
contracts, and how the requires-tools gate applies. It is the user-facing
feature doc (root AGENTS.md mapping: subsystem to `docs/workflows/<name>.md`;
`config/skills` additions are documented here).

## Bundle layout

The office capability ships under `config/skills/office/`:

```
config/skills/office/
  office/SKILL.md          router: picks a format skill by extension
  office-docx/SKILL.md     Word documents  (.docx)  -> office_docx     tool
  office-pptx/SKILL.md     PowerPoint decks (.pptx)  -> office_pptx     tool
  office-xlsx/SKILL.md     Excel workbooks  (.xlsx)  -> office_workbook tool
```

The router (`office`) is a description-only routing index. It inspects the file
extension and tells the model to load the matching format skill before any tool
call. It carries NO requires-tools frontmatter of its own - it routes, it does
not execute. The three format skills each declare the single tool they gate on.

Backing tools (builtin, in `internal/tools/builtin/`):

| Tool              | Format | Skill      | Library (see library-picks.md)            |
|-------------------|--------|------------|-------------------------------------------|
| `office_docx`     | `.docx`| office-docx| `github.com/gomutex/godocx` v0.1.5 (MIT)   |
| `office_pptx`     | `.pptx`| office-pptx| `github.com/mmonterroca/pptxgo` (MIT)      |
| `office_workbook` | `.xlsx`| office-xlsx| `github.com/xuri/excelize/v2` v2.11.0      |

These three tools are model-facing; there is no CLI/TUI/GUI facade over them in
this tree. That is a documented deferral: the per-format tools are agent-facing,
and a human surface over them is a separate affordability not shipped here.

## Tool contracts

Each format skill documents its tool's `action` contracts in full. The summary
below is the cross-bundle view; read the format skill for the exact JSON action
arguments.

### office_docx (.docx)

- `create` - `{blocks: [...]}` of headings, paragraphs, and tables; saved at
  `path`, reopened, and verified.
- `read` - document text, one node per line; formatting is not represented.
- `edit` - APPENDS blocks; never rewrites earlier content. Default writes a new
  file `<name>.edited.docx`; pass `in_place: true` to overwrite.
- `validate` - `{expect_text: [...]}`; reports `valid` plus any `missing`
  fragments.

All paths resolve inside the session working directory; escapes are refused.

### office_pptx (.pptx)

- `create` - `{slides: [...]}` with title (required), bullet body, notes, and
  optional `image_path`. Standard 16:9; saved at `path`, reopened, verified.
- `read` - slide count plus each slide's title, text, and notes. Charts and
  SmartArt are not represented; do not claim structure the text does not show.
- `edit` - APPENDS slides; existing slides pass through unchanged. Optional
  `replace: {find, with}` alters text inside an existing slide. Default writes
  `<name>.edited.pptx`; `in_place: true` overwrites.
- `validate` - `{expect_text: [...]}` over the deck's slide text; reports
  `valid` plus `missing`.

The tool edits content, not appearance: it cannot verify text fit, contrast, or
rendering fidelity.

### office_workbook (.xlsx)

- `create` - `{sheets: [...]}` with `name`, `headers`, `rows`, optional
  `highlight_rows`. Cells are string, number, boolean, or null; numbers stay
  numbers.
- `read` - every sheet with typed values. Formula cells return as
  `{formula, evaluated: false}` - formulas are NEVER evaluated by this tool, and
  `cached_value` may be absent or stale. When the user needs computed values,
  say recalculation requires opening the workbook in Excel/LibreOffice.
- `edit` - `{cells: [...]}` of `{sheet, cell, value}` or `{sheet, cell,
  formula}` (include the leading `=`); targeted, untouched content survives.
- `validate` - reopens and reports verified sheet names, row/cell counts, and
  formula-cell counts.

## requires-tools gate

Each format skill's frontmatter declares the tool it needs:

```yaml
# office-docx
requires-tools:
  - office_docx
# office-pptx
requires-tools:
  - office_pptx
# office-xlsx
requires-tools:
  - office_workbook
```

Matching semantics: each entry is a full registered tool name checked against
the live builtin tool registry. When `skills.validate_prerequisites` is enabled
(default: OFF), the executor refuses to run a format skill if its tool is
unavailable, with an error naming the missing tool and pointing at
`meept doctor`. The router `office` carries no gate - it routes only.

Because the gate is default-off, out of the box a format skill is offered to the
model even if the backing tool is not registered. Enable
`skills.validate_prerequisites` to turn on the refusal behavior for all skills,
office included. The bundled tools always register when the `builtin` package is
linked, so the gate only fires on exotic builds or provider-disable configs.
