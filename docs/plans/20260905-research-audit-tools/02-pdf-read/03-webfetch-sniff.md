# leaf 02-pdf-read/03 — web_fetch PDF sniff guard

## DISPATCH INSTRUCTION

You are an implementation agent. Implement every task with TDD. **Do NOT
commit. Do NOT run `git add`.** Write code, run tests, report results.

- Parent: `docs/plans/20260905-research-audit-tools/02-pdf-read/orchestrator.md`
- Scope: `internal/tools/builtin/web_fetch.go` (two sites) +
  `web_fetch_test.go`. No other files.
- Dependencies: none — runs in parallel with leaf 01 (disjoint files).
- Estimated context: ~35K.

## Goal

Implement Contract B from the root master.md: web_fetch must NEVER pass
PDF bytes through stripHTML. Both fetch paths must detect PDFs and
return a redirect-to-tool hint instead.

## Interface Contract (exposed)

Contract B, verbatim from parent master.md:

> Both fetch paths (web_fetch.go fetch() and the header-variant path at
> :448-452): if Content-Type contains "application/pdf" OR body magic
> bytes == "%PDF", return a WebFetchResult with Text:
> `PDF detected (N bytes). Use the pdf_read tool to extract text.`
> and ContentType "application/pdf". Never stripHTML a PDF.

(N is the body length in bytes, formatted with commas if large is fine;
keep it a plain integer.)

## Tasks

### Task 1: locate both sites

`search_files` web_fetch.go for `stripHTML(` — there are two call sites
(~:267 and ~:452, the second in a headers-variant fetch path). Read
enough of each enclosing function to place the guard BEFORE the
stripHTML call and before any other body-text processing.

### Task 2: TDD (web_fetch_test.go additions first)

Table-driven cases using httptest servers:

1. Content-Type `application/pdf` with arbitrary bytes → result Text is
   the hint, ContentType "application/pdf", no garbage text.
2. Content-Type empty/generic but body starts with `%PDF-` → hint fires
   (magic-byte path).
3. Normal HTML → unchanged behavior (hint does NOT fire).
4. Binary garbage, no PDF markers → unchanged behavior (today's path;
   only PDF is special-cased).

### Task 3: implement

Small helper in web_fetch.go (unexported):

```go
// pdfSniff reports whether the response is a PDF, per Contract B
// (plan 20260905-research-audit-tools): content-type OR %PDF magic bytes.
func pdfSniff(contentType string, body []byte) bool
```

At both fetch sites, after reading the body and before stripHTML:

```go
if pdfSniff(contentType, body) {
    return WebFetchResult{
        Text:        fmt.Sprintf("PDF detected (%d bytes). Use the pdf_read tool to extract text.", len(body)),
        ContentType: "application/pdf",
        ... // preserve the fields the existing result struct carries at this site
    }, nil
}
```

Match the actual result-construction shape at each site (the two paths
may build slightly different structs — mirror each one; do not unify the
two paths in this leaf, that is a refactor out of scope).

### Task 4: verify

```
go build ./internal/tools/...
go vet ./internal/tools/builtin/
gofmt -l internal/tools/builtin/
go test -p 2 ./internal/tools/builtin/ -run WebFetch -count=1
```

## Self-Verification Checklist

- [ ] Guard present at BOTH sites (grep stripHTML → 2 sites, each preceded by pdfSniff check)
- [ ] Hint text matches Contract B exactly
- [ ] 4 table cases pass; no regression in existing web_fetch tests
- [ ] gofmt/vet green; no TODOs; no line-number prefixes

## Review Checklist (for orchestrator)

- [ ] Both sites guarded; struct fields preserved per site
- [ ] pdfSniff unexported, unit-tested (content-type and magic-byte branches)
- [ ] Diff confined to web_fetch.go + web_fetch_test.go

Do NOT commit.
