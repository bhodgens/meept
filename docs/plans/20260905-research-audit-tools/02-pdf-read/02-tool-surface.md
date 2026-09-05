# leaf 02-pdf-read/02 — pdf_read tool surface

## DISPATCH INSTRUCTION

You are an implementation agent. Implement every task with TDD. **Do NOT
commit. Do NOT run `git add`.** Write code, run tests, report results.

- Parent: `docs/plans/20260905-research-audit-tools/02-pdf-read/orchestrator.md`
- Scope: two new files (`internal/tools/builtin/pdf_read.go`,
  `pdf_read_test.go`) + ONE registration block in
  `internal/daemon/components.go`. No other files.
- Dependencies: leaf 01 (extractPDFText seam) must be COMPLETE.
- Estimated context: ~50K.

## Goal

Wrap the leaf-01 seam into the `pdf_read` Tool per Contract A in the
root master.md, wire it in components.go, and prove it with tests.

## Interface Contract (exposed)

```
Tool name:   "pdf_read"          Category: "web"
Constructor: builtin.NewPDFReadTool(maxBytes int64, guard *ssrf.Guard) *PDFReadTool
Parameters:  path (string, required), pages (string, optional)
Execute returns the Contract A result map:
  {"pages_read": int, "total_pages": int, "truncated": bool, "text": "...", "note": ""}
```

Mirror the structure of `internal/tools/builtin/web_fetch.go`
(Name/Category/Description/Parameters/Execute, guard handling, schema
validation helpers). Typed-nil guard: `func (t *PDFReadTool) SetWorkingDir(...)`-style
setters (if any) must nil-guard per AGENTS.md.

## Tasks

### Task 1: TDD the tool (pdf_read_test.go first)

Table-driven cases:

1. local file path: write a PDF fixture to t.TempDir(), Execute, assert
   result map fields match the fixture text.
2. URL path: `httptest.NewServer` serving the same fixture bytes;
   Execute with `path: <test URL>`; assert fetch-through worked.
   SSRF guard is nil in tests (legacy checkURL path permits the test
   server) — same pattern as ssrf_guard_wiring_test.go.
3. missing required param → validation error.
4. nonexistent file → error mentioning the path.
5. non-PDF bytes at a `.pdf`-looking path → error from the core
   (garbage input), NOT a fabricated result.
6. pages param "1" on the fixture → pages_read 1.
7. truncated path via small maxBytes constructor arg.

### Task 2: implement pdf_read.go

- Execute resolves `path`: if it starts with http:// or https://, fetch
  via the same guard-wrapped client pattern as web_fetch.go (timeout
  30s, MaxConnsPerHost 8); else read from disk relative to the session
  working dir (use the session working-dir convention — NOT os.Getwd).
- Sniff content: first 5 bytes `%PDF-` (files and responses); reject
  otherwise with a clear error ("not a PDF: <path>").
- Call `extractPDFText` (leaf 01). Map result to the Contract A map.
- Description string: "Read a PDF's text layer from a local path or URL. Returns extracted text; scanned PDFs report 'no text layer' instead of failing."
- Cite Contract A in a comment at the constructor.

### Task 3: register in components.go

In `internal/daemon/components.go`, immediately AFTER the webFetchTool
registration block (the `registry.Register(webFetchTool)` call,
~line 5487), add:

```go
	// PDF reading tool (plan 20260905-research-audit-tools, Contract A):
	// text-layer extraction for PDF URLs/files; scanned PDFs return an
	// explicit no-text-layer note rather than binary garbage.
	pdfReadTool := builtin.NewPDFReadTool(100000, webSSRFGuard)
	if secOrch != nil {
		pdfReadTool.SetSecurityOrchestrator(secOrch)
	}
	registry.Register(pdfReadTool)
```

Add `SetSecurityOrchestrator` on PDFReadTool ONLY if webFetchTool's
orchestrator wiring is trivially mirrorable (taint/exfil output
sanitization on the text result); otherwise omit those two lines and
note the omission in your report. Do not half-implement taint handling.

### Task 4: verify

```
go build ./...
go vet ./internal/tools/builtin/ ./internal/daemon/
gofmt -l internal/tools/builtin/
go test -p 2 ./internal/tools/builtin/ -run 'PDF' -count=1
go test -p 2 ./internal/daemon/ -run 'Wiring|Registry' -count=1
```

If the daemon package test needs TEST_PACKAGE_PARALLELISM=2, use
`go test -p 2` as written.

## Self-Verification Checklist

- [ ] Tool name/category/params match Contract A byte-for-byte
- [ ] URL path goes through the guard when guard non-nil (nil = legacy)
- [ ] No os.Getwd anywhere in the new files
- [ ] components.go block placed after webFetchTool; builds clean
- [ ] All 7 table cases pass; gofmt/vet green
- [ ] No TODOs, no debug prints, no line-number prefixes

## Review Checklist (for orchestrator)

- [ ] Contract A result map exact
- [ ] Registration site matches Task 3 spec
- [ ] Tests use httptest, no external network
- [ ] `grep -n "os.Getwd" internal/tools/builtin/pdf_read.go` = 0 hits

Do NOT commit.
