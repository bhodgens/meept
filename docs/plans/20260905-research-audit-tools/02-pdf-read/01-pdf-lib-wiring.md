# leaf 02-pdf-read/01 — PDF extraction core

## DISPATCH INSTRUCTION

You are an implementation agent. Implement every task with TDD. **Do NOT
commit. Do NOT run `git add`.** Write code, run tests, report results.

- Parent: `docs/plans/20260905-research-audit-tools/02-pdf-read/orchestrator.md`
- Scope: go.mod/go.sum + two new files (`internal/tools/builtin/pdf_extract.go`,
  `pdf_extract_test.go`). No other files.
- Dependencies: none.
- Estimated context: ~45K.

## Goal

Implement the extraction core seam defined in the branch orchestrator's
Interface Contracts section, exactly:

```go
type PDFExtractOptions struct { PageRange string; MaxChars int64 }
type PDFExtractResult struct { PagesRead, TotalPages int; Truncated bool; Text, Note string }
func extractPDFText(r io.Reader, opts PDFExtractOptions) (PDFExtractResult, error)
```

## Interface Contract (exposed to leaf 02)

The three symbols above, at those exact names in package `builtin`.
Everything else in the file is unexported. The no-text-layer case returns
`(result, nil)` with empty Text and Note exactly:
`no text layer detected (scanned PDF?) - N pages, no extractable text`
where N is TotalPages (Contract A in parent master.md).

## Tasks

### Task 1: dependency

`go get github.com/ledongthuc/pdf@latest` (MIT; expected v0.3.0). Run
`go mod tidy`. Confirm go.mod diff contains ONLY this addition.

### Task 2: verify library API from source

Run `go doc github.com/ledongthuc/pdf` (and `-all` for detail). Use ONLY
verified signatures. Known shape: `pdf.NewReader(r io.Reader, size int64)
(*Reader, error)`, `(*Reader).NumPage() int`, `(*Reader).Page(n int)
(Page, error)`, `(Page).GetPlainText(w io.Writer) (string, error)` — but
TRUST go doc output over this brief; adjust if the real API differs and
record the diff in your report.

### Task 3: TDD the core

Write `pdf_extract_test.go` first, table-driven:

- valid text PDF fixture: build a minimal PDF in-test as a `[]byte`
  literal (simplest: a hand-written one-page PDF with the classic
  "Hello World" content stream — a ~20-line byte string; the library
  parses uncompressed PDFs). Assert extracted text contains "Hello".
- multi-page range fixtures: reuse the fixture N times is NOT possible
  with a hand-rolled literal for N>1 — instead test `parsePageRange`
  thoroughly as a pure function ("", "3", "2-5", "1,3,7-9", "0", "9-2",
  "abc", "2-") with a fake total, and test page-range behavior via
  extractPDFText on a real 2-page fixture built by concatenating two
  page objects in the literal (keep it simple; if the 2-page literal
  proves brittle after 2 attempts, drop to a 1-page extract test plus
  exhaustive parsePageRange tests and note the deviation).
- truncated: MaxChars smaller than text → Truncated true, text ends
  with "\n[TRUNCATED at cap]".
- no text layer: PDF with empty content stream → Note per contract,
  nil error, empty Text.
- corrupt input: random bytes → non-nil error, zero result.

### Task 4: implement

`pdf_extract.go`: doc-comments citing Contract A and the orchestrator
seam; `parsePageRange` pure function; error wrapping with
`fmt.Errorf("pdf extract: %w", err)`; NO os.Getwd, no network, no
filesystem access (io.Reader in).

### Task 5: verify

```
go build ./internal/tools/...
go vet ./internal/tools/builtin/
gofmt -l internal/tools/builtin/   # must print nothing
go test -p 2 ./internal/tools/builtin/ -run PDF -count=1
```

## Self-Verification Checklist

- [ ] go.mod contains only the one new require line (+ go.sum entries)
- [ ] Only symbols per contract exported (package-internal check)
- [ ] All table cases pass; corrupt-input and no-text-layer covered
- [ ] No TODOs, no fmt.Println, no read_file-style line-number prefixes
- [ ] gofmt/vet/test green

## Review Checklist (for orchestrator)

- [ ] Seam signatures match orchestrator contract byte-for-byte
- [ ] Note string matches Contract A exactly
- [ ] go.mod diff minimal
- [ ] Tests table-driven, no network

Do NOT commit.
