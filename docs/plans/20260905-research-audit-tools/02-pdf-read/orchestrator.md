# orchestrator.md — 02-pdf-read branch

## Goal

Deliver Contract A (`pdf_read` tool) and Contract B (web_fetch PDF sniff)
from the parent master (`docs/plans/20260905-research-audit-tools/master.md`).

## Architecture Overview

Two leaves plus one dependent leaf:

- 01-pdf-lib-wiring: add `github.com/ledongthuc/pdf` to go.mod, write the
  pure extraction core (`internal/tools/builtin/pdf_extract.go`) — text
  extraction, page ranges, no-text-layer detection. No tool surface.
- 02-tool-surface: wrap the core in the `pdf_read` Tool interface
  (params, SSRF guard for URLs, char cap, registry wiring in
  components.go). Depends on 01.
- 03-webfetch-sniff: independent two-path fix in web_fetch.go per
  Contract B + tests. Runs in parallel with 01.

Files are disjoint between 01 and 03; 02 touches components.go only.

## Interface Contracts

Contract A and Contract B are frozen in the parent master.md — restate
them verbatim in each dispatch context; do not paraphrase. The extraction
core seam (defined here, leaf 01 → leaf 02):

```go
// internal/tools/builtin/pdf_extract.go
// PDFExtractOptions controls extraction.
type PDFExtractOptions struct {
    PageRange string // "" = all; "3" or "2-5" or "1,3,7-9"
    MaxChars  int64  // <=0 = unlimited
}

// PDFExtractResult is the extraction outcome.
type PDFExtractResult struct {
    PagesRead  int
    TotalPages int
    Truncated  bool
    Text       string
    Note       string // "" normally; no-text-layer message per Contract A
}

// extractPDFText parses r (a PDF byte stream) per opts. Returns
// ErrNoTextLayer-style note in result (NOT an error) for scanned PDFs.
func extractPDFText(r io.Reader, opts PDFExtractOptions) (PDFExtractResult, error)
```

Leaf 02 consumes ONLY `extractPDFText` + these two types. Do not change
signatures during 02 without amending this contract in both leaves.

## Child Index

| Doc | Scope | Est. context | Dependencies |
|-----|-------|--------------|--------------|
| 01-pdf-lib-wiring.md | dep + core + tests | ~45K | none |
| 02-tool-surface.md | tool wrapper + wiring | ~50K | 01 |
| 03-webfetch-sniff.md | web_fetch guard both paths | ~35K | none |

Dispatch order: batch 01 + 03 in parallel (Wave 1), then 02 (Wave 2).

## Dispatch Protocol

Per parent master.md Dispatch Protocol: delegate each leaf with its
document + parent contracts verbatim; instruct "Do NOT commit. Do NOT run
git add. Write code, run tests, report results only."; review in-session
(build + tests + contract diff); re-dispatch on gaps (max 3); commit
after review; update tracking tables here and in master.md.

## Coding Conventions

Per parent master.md Coding Conventions section. Extra for this branch:
- ledongthuc/pdf API is `pdf.NewReader(r, size)` + `reader.NumPage()` +
  `page.GetPlainText(nil)` — do not invent helpers; verify signatures
  from the vendored module source (`go doc github.com/ledongthuc/pdf`)
  before writing code.
- Page-range parsing: pure function `parsePageRange(spec string, total int) ([]int, error)`
  in pdf_extract.go, table-tested; reject zero/over-range with clear errors.

## Completion Tracking Table

| Doc | Status | Notes |
|-----|--------|-------|
| 01-pdf-lib-wiring.md | PENDING | |
| 02-tool-surface.md | PENDING | blocked by 01 |
| 03-webfetch-sniff.md | PENDING | |

## Review Checklist (branch)

- [ ] Contract A verbatim (names, result shape, no-text-layer note text)
- [ ] Contract B verbatim (both fetch paths, magic-byte OR content-type)
- [ ] `go build ./...`; `go vet ./internal/tools/...`; package tests green
- [ ] No fabricated extraction: scanned-PDF path returns the note, empty text, nil error
- [ ] web_fetch unit test covers content-type AND magic-byte detection
- [ ] Registration compiles: pdf_read present iff wired in components.go
- [ ] No debug artifacts/TODOs; gofmt clean

## Integration Test Plan

After all three leaves REVIEWED: run `go test -p 2 ./internal/tools/builtin/ -run 'PDF|WebFetch' -count=1`;
build daemon (`go build ./cmd/meept-daemon`); confirm no unused-import or
unused-symbol failures (pre-commit U1000 — remove dead helpers, do not
nolint). Then mark branch COMPLETE in parent master.md.
