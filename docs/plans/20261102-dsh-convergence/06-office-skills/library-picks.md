# Office Library Picks - Research Gate Leaf (06-office-skills)

Gate leaf: `06-research-assignment.md`. Date: 2026-11-02. Branch:
`plan/dsh-convergence`. This file is the authority for library choice. An
implementation leaf's seed library loses to it (research-gated rule 2).

Method: every numeric claim below was pulled live on 2026-11-02 from the GitHub
repository API (`https://api.github.com/repos/<owner>/<repo>`) or the Go module
proxy (`https://proxy.golang.org/<escaped-module>/@latest` plus its `.mod`).
Every API signature was read from the downloaded module source under
`$(go env GOMODCACHE)`, not from a README.

## Decision

- xlsx = `github.com/xuri/excelize/v2` v2.11.0 (GO, proven pin). Already in
  `go.mod:45` and already builds into `spreadsheet_write`. Not re-litigated.
- docx = `github.com/gomutex/godocx` v0.1.5 (GO). MIT, 273 stars, direct
  create + heading + paragraph + read + save API, zero non-test dependencies.
  Fallback order: `github.com/mmonterroca/docxgo/v2` v2.15.0, then
  `github.com/mgilbir/spine` v0.4.0 (`/docx`).
- pptx = `github.com/mmonterroca/pptxgo`
  v0.0.0-20260802145618-9e8c8b31d00c (GO, with a caveat). MIT, verified
  create + open/edit + save, zero dependencies, same author as docxgo. No
  many-star pptx library exists in Go - see OPEN QUESTION 1. Fallback order:
  `github.com/mgilbir/spine` v0.4.0 (`/pptx`), then `github.com/F31/go-pptx/v2`
  v2.1.0 (Apache-2.0).

## Evidence

### xlsx (confirm-only)

| Candidate | License | Stars (evidence) | go directive | Last activity | Verified API signature | Verdict |
|---|---|---|---|---|---|---|
| `github.com/xuri/excelize/v2` | BSD-3-Clause (api.github.com/repos/xuri/excelize `license.spdx_id`) | 21,043 (api.github.com/repos/xuri/excelize `stargazers_count`) | `go 1.26.0` at v2.11.1 (proxy.golang.org `.mod`); meept pins v2.11.0 | pushed 2026-11-02 (api.github.com `pushed_at`) | `writeXLSX(w io.Writer, sheets []SheetSpec)` at `internal/tools/builtin/spreadsheet_xlsx.go:24` (meept source) | GO - proven, already linked |

Fit check (resolves the leaf's OPEN QUESTION #3): meept declares `go 1.26.0`
(`go.mod:3`). excelize v2.11.1 declares `go 1.26.0`; the on-disk v2.11.0 module
declares `go 1.25.0`. meept satisfies both. No conflict.

### docx

| Candidate | License | Stars (evidence) | go directive | Last activity | Verified API signature | Verdict |
|---|---|---|---|---|---|---|
| `github.com/gomutex/godocx` | MIT (api.github.com `license.spdx_id`) | 273 (api.github.com) | `go 1.18` (proxy `.mod`) | pushed 2025-08-11 | `func NewDocument() (*docx.RootDoc, error)`; `func AddHeading(text string, level uint) (*Paragraph, error)`; `func AddParagraph(text string) *Paragraph`; `func OpenDocument(fileName string) (*docx.RootDoc, error)`; `func SaveTo(fileName string) error` (module source: `document.go`, `heading.go`, `paragraph.go`, `writer.go`) | GO - chosen |
| `github.com/mmonterroca/docxgo/v2` | MIT (`raw.githubusercontent.com/mmonterroca/docxgo/master/LICENSE` - GitHub reports NOASSERTION because the file carries extra copyright lines) | 124 (api.github.com) | `go 1.23` (proxy `.mod`) | pushed 2026-11-02 (most active) | `func NewDocument() domain.Document`; `func OpenDocument(path string) (domain.Document, error)`; `func (d *document) AddParagraph() (domain.Paragraph, error)`; `func (r *run) SetText(text string) error`; `func (d *document) SaveAs(path string) error`; headings via `func (p *paragraph) SetStyle(styleName string) error` (module source: `docx.go`, `internal/core/*.go`) | GO - fallback 1 |
| `github.com/mgilbir/spine` (`/docx`) | MIT (`LICENSE` in module) | 3 (api.github.com) | `go 1.26.0` (proxy `.mod`) | pushed 2026-10-15 | `func Create() *Document`; `func AddHeading(text string, level int) *Paragraph`; `func AddParagraphWithText(text string) *Paragraph`; `func Open(path string, opts ...opc.ReaderOption) (*Document, error)`; `func Save(path string) error` (module source: `docx/document.go`) | GO - fallback 2 (also covers pptx) |
| `github.com/zerx-lab/wordZero` | MIT (api.github.com) | 687 (api.github.com) | `go 1.22` (proxy `.mod`) | pushed 2026-01-17 | not verifiable - see OPEN QUESTION 2 | BLOCKED - module path mismatch |
| `github.com/fumiama/go-docx` | AGPL-3.0 (api.github.com) | 307 (api.github.com) | `go 1.20` (proxy `.mod`) | pushed 2025-05-06 | `docx.New().WithDefaultTheme()`, `AddParagraph()`, `AddText(...)` (README) | REJECTED - AGPL copyleft; the user license gate allows GPL/AGPL only as a subprocess dependency, never linked into a shipped binary |
| `github.com/nguyenthenguyen/docx` | MIT (api.github.com) | 620 (api.github.com) | `go 1.16` (proxy `.mod`) | pushed 2024-07-29 | `func ReadDocxFile(path string)` - text replacement only; no create, no heading, no table (repo README + issue #2 "Newline, dynamic table are not supported!") | REJECTED - fails the usability bar (cannot create a document) |
| `github.com/gonfva/docxlib` | MIT (api.github.com) | 57 (api.github.com) | `go 1.16` (proxy `.mod`) | pushed 2025-01-28; last module pseudo-version 2021-05-17 | partial read/write only (repo README) | REJECTED - stale, partial |
| `github.com/aksteps/godocx` | MIT (api.github.com) | 4 (api.github.com) | `go 1.25` (proxy `.mod`) | created 2026-08-23 | `godocx.New()`, `doc.AddHeading(...)`, `doc.AddTable(2, 3, "Table Grid")` (pkg.go.dev README) | NOT CHOSEN - claims python-docx parity but is 4 stars and 2 months old |
| `github.com/unidoc/unioffice` | Commercial EULA (`raw.githubusercontent.com/unidoc/unioffice/master/LICENSE.md`: "This software package is a commercial product and requires a license code to operate"; EULA at `https://unidoc.io/eula/`) | 4,939 (api.github.com) | n/a | pushed 2026-09-21 | n/a | REJECTED - master D6 bars it regardless of stars |

### pptx

| Candidate | License | Stars (evidence) | go directive | Last activity | Verified API signature | Verdict |
|---|---|---|---|---|---|---|
| `github.com/mmonterroca/pptxgo` | MIT (`raw.githubusercontent.com/mmonterroca/pptxgo/master/LICENSE`; GitHub reports NOASSERTION) | 0 (api.github.com) | `go 1.23` (proxy `.mod`) | pushed 2026-08-02 | `func New(opts ...Option) *Presentation`; `func (p *Presentation) AddSlide(opts ...SlideOption) *Slide`; `func (s *Slide) AddTextBox(x, y, w, h int) *TextBox`; `func (s *Slide) AddImage(path string, x, y int) *PictureRef`; `func Open(pth string) (*Template, error)`; `func OpenFromBytes(data []byte) (*Template, error)`; `func Save(w io.Writer) error` (module source: `pptx/presentation.go`, `pptx/slide.go`, `pptx/open.go`) | GO - chosen (see OPEN QUESTION 1) |
| `github.com/mgilbir/spine` (`/pptx`) | MIT (module `LICENSE`) | 3 (api.github.com) | `go 1.26.0` (proxy `.mod`) | pushed 2026-10-15 | `func Create() *Presentation`; `func (p *Presentation) AddSlide() *Slide`; `func (s *Slide) AddTextBox() *TextBox`; `func SetText(text string)`; `func Open(path string, opts ...opc.ReaderOption) (*Presentation, error)`; `func Save(path string) error` (module source: `pptx/presentation.go`, `pptx/slide.go`) | GO - fallback 1 (one library covers docx + pptx) |
| `github.com/F31/go-pptx/v2` | Apache-2.0 (api.github.com) | 1 (api.github.com) | `go 1.24.0` (proxy `.mod`) | pushed 2026-10-20 | `pptx.New()`, `p.AddSlide(layouts[0])`, `slide.AddTextBox(pptx.TextBoxSpec{...})`, `p.Save(ctx, "out.pptx")` (pkg.go.dev README) | GO - fallback 2 |
| `github.com/Muprprpr/Go-pptx` | MIT (api.github.com) | 6 (api.github.com) | `go 1.24.5` (proxy `.mod`) | pushed 2026-04-02 | `opc.OpenFile`, `pkg.GetPartsByType(opc.ContentTypeSlide)`, `pkg.SaveFile` - package/part level only; no verified slide-authoring API | NOT CHOSEN - low-level OPC only |
| `github.com/kenny-not-dead/gopptx` | BSD-3-Clause (`LICENSE` in module) | 2 (api.github.com) | `go 1.24.0` (proxy `.mod`) | pushed 2026-10-30 | `gopptx.NewFile()`, `f.NewSlide()`, `f.SetShapeTextBody(...)` - verbose decode-struct API | NOT CHOSEN - awkward shape for a model tool |
| `github.com/djinn-soul/gopptx` | Apache-2.0 (api.github.com) | 0 (api.github.com) | `go 1.25.12` (proxy `.mod`) | pushed 2026-09-03 | `pptx.CreateWithSlides("My Deck", slides)` (README) | NOT CHOSEN - pre-release (v0.2.0-alpha.5) |
| `github.com/casibase/goppt` | Apache-2.0 (api.github.com) | 0 (api.github.com) | `go 1.24.0` (proxy `.mod`) | created 2026-04-25 | `ppt.New()`, `p.CreateSlide()`, `slide.CreateRichTextShape()` (README) | NOT CHOSEN - 0 stars, includes a slide renderer meept does not need |
| `github.com/unidoc/unioffice` | Commercial EULA (see above) | 4,939 | n/a | n/a | n/a | REJECTED - master D6 |

## OPEN QUESTION

1. `OPEN QUESTION: no many-star Go pptx library exists.` The user decision D6
   asks for a "many stars" OSS library. For pptx the whole field is under 10
   stars (highest: `Muprprpr/Go-pptx` at 6, and it is part-level only). The
   chosen pick (`mmonterroca/pptxgo`) is MIT and API-verified but has 0 stars
   and no release tag. This needs a user decision: accept a low-star MIT
   dependency, or ship `office_pptx` as a NO-GO and deliver docx + xlsx only.
   Ranked alternatives if the user rejects low-star deps: `mgilbir/spine`
   (MIT, 3 stars, one library for all three formats, `go 1.26.0`), then
   `F31/go-pptx/v2` (Apache-2.0, 1 star).
2. `OPEN QUESTION: github.com/zerx-lab/wordZero (687 stars, MIT) cannot be
   imported under its repo name.` `go get github.com/zerx-lab/wordZero@v1.6.0`
   fails with: "module declares its path as: github.com/ZeroHawkeye/wordZero,
   but was required as: github.com/zerx-lab/wordZero". The repo was renamed and
   the module path was not updated to match. Closest real alternatives, ranked:
   `gomutex/godocx` (chosen), `mmonterroca/docxgo/v2`. If the user wants
   wordZero, the import path must be `github.com/ZeroHawkeye/wordZero` - that
   path was not verified in this pass.
3. `OPEN QUESTION: docx heading API differs between the two leading picks.`
   `gomutex/godocx` has a direct `AddHeading(text, level uint)`. `docxgo/v2`
   has no `AddHeading`; a heading is a paragraph with
   `SetStyle("Heading1")`. The `office_docx` tool contract (orchestrator C11.2)
   stays lib-agnostic, so either pick satisfies it - but the leaf's internal
   call must branch if the fallback is used.
4. `OPEN QUESTION: excelize v2.11.0 is one patch behind upstream (v2.11.1).`
   Not acted on here: bumping a pinned dependency is a separate decision and
   this leaf adds no dependencies.

## Fallback

| Format | Primary | Fallback 1 | Fallback 2 | Rejected (never use) |
|---|---|---|---|---|
| xlsx | `xuri/excelize/v2` v2.11.0 (pinned, in-tree) | n/a | n/a | `unidoc/unioffice` (EULA) |
| docx | `gomutex/godocx` v0.1.5 (MIT) | `mmonterroca/docxgo/v2` v2.15.0 (MIT) | `mgilbir/spine` v0.4.0 `/docx` (MIT) | `unidoc/unioffice` (EULA), `fumiama/go-docx` (AGPL, linked use), `nguyenthenguyen/docx` (cannot create) |
| pptx | `mmonterroca/pptxgo` (MIT, 0 stars - see OPEN QUESTION 1) | `mgilbir/spine` v0.4.0 `/pptx` (MIT, 3 stars) | `F31/go-pptx/v2` v2.1.0 (Apache-2.0) | `unidoc/unioffice` (EULA) |

Single-library alternative: `mgilbir/spine` v0.4.0 covers docx, pptx, and
xlsx under one MIT license and declares `go 1.26.0`, matching meept exactly.
It is not the primary pick because it pulls 10 transitive modules (including
`mgilbir/pdf0`, `mgilbir/forme`, `mgilbir/gowemf`) for a capability that needs
none of them, and it has 3 stars.
