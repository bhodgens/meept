# master.md — Research & Audit Tooling (obscura MCP, pdf_read, spreadsheet_write)

## Goal

Close the two tooling gaps identified in the local-SEO audit-tooling discussion (2026-09-05 session):

1. **JS-heavy page reads** → enable the obscura MCP server (built at
   `/Users/caimlas/git/obscura`, Apache-2.0, stdio transport, ~30 browser
   tools, built-in anti-detect, geolocation/timezone shim for geo-vantaged
   reads) as a catalog entry in `config/mcp_servers.json5`.
2. **Spreadsheet / prioritized output** → native `spreadsheet_write` builtin
   tool (stdlib `encoding/csv` + `github.com/xuri/excelize/v2`), plus a
   DISABLED higher-capability MCP fallback (`uvx excel-mcp-server`) for
   pivot-table-class needs.
3. **PDF reading** → native `pdf_read` builtin tool (text-layer extraction
   via `github.com/ledongthuc/pdf`, MIT), fixing web_fetch's current
   binary-garbage-on-PDF failure. Primary value: correctness (PDF URLs
   today return stripHTML'd binary garbage — web_fetch.go:263-268 has no
   application/pdf branch); secondary value: token economy.

Out of scope (parked per user): audit employee, GBP API integration,
geo-proxy infrastructure, PDF writing, OCR.

## Architecture Overview

All three deliverables are meept-daemon capabilities:

- Branch 01 changes ONLY `config/mcp_servers.json5` (catalog data, no Go
  code). The MCP client (`internal/tools/mcp`) launches stdio subprocesses
  from catalog entries; entries follow the existing npx/uvx pattern.
- Branch 02 adds one builtin tool package (`internal/tools/builtin/pdf_read.go`)
  registered in `internal/daemon/components.go` behind an SSRF guard
  instance (same pattern as webFetchTool at components.go:5480-5487), plus
  a two-line web_fetch sniff fix (both fetch paths: :267 and :452).
- Branch 03 adds one builtin tool package (`internal/tools/builtin/spreadsheet_write.go`)
  registered adjacent to pdf_read, behind the allowed-paths fence.

Branches are independent: 01 is config-only; 02 and 03 touch disjoint Go
files except for the shared registration site (components.go). To avoid
merge friction, 02 and 03 register at clearly separated call sites and may
run concurrently — conflicts are unlikely and resolved by rebase.

## Interface Contracts (frozen)

### Contract A: pdf_read tool (Branch 02)

```
Tool name:            "pdf_read"
Category:             "web"
Constructor:          builtin.NewPDFReadTool(maxBytes int64, guard *ssrf.Guard) *PDFReadTool
                      (nil guard => legacy checkURL path, same as WebFetchTool)
Parameters:           path    string  (required; filesystem path OR http(s) URL;
                              URLs go through the SSRF guard; only .pdf validated
                              by content sniff, not extension)
                      pages   string  (optional; "" = all; "3" or "2-5" or "1,3,7-9")
Result (success):     {"pages_read": int, "total_pages": int, "truncated": bool,
                       "text": "...", "note": ""}
Result (no text layer): err == nil, text == "", note ==
                      "no text layer detected (scanned PDF?) - N pages, no extractable text"
                      (NEVER return empty silently; NEVER fabricate content)
Char cap:             maxBytes arg caps extracted text; truncation sets
                      truncated=true and appends "\n[TRUNCATED at cap]"
Dependencies (input): github.com/ledongthuc/pdf v0.3.0 (MIT)
Config:               none new; reuse [security.ssrf]
```

### Contract B: web_fetch PDF sniff (Branch 02, leaf 03)

```
Both fetch paths (web_fetch.go fetch() and the header-variant path at
:448-452): if Content-Type contains "application/pdf" OR body magic
bytes == "%PDF", return a WebFetchResult with Text:
  "PDF detected (N bytes). Use the pdf_read tool to extract text."
and ContentType "application/pdf". Never stripHTML a PDF.
```

### Contract C: spreadsheet_write tool (Branch 03)

```
Tool name:            "spreadsheet_write"
Category:             "filesystem"
Constructor:          builtin.NewSpreadsheetWriteTool() *SpreadsheetWriteTool
                      (no deps; fence integration via SetWorkingDir per
                      filesystem tool convention)
Parameters:           path    string  (required; .csv or .xlsx; extension
                              selects the writer; parent dirs auto-created)
                      format  string  (optional; "csv"|"xlsx"; default from
                              extension; explicit format wins)
                      sheets  array   (xlsx; required >=1 when format xlsx;
                              each: {"name": str, "headers": [str],
                                     "rows": [[cell]], "highlight_rows":
                                     [int] optional -> yellow fill})
                      rows    array   (csv; required when format csv;
                              first row may be headers via "header": true flag)
                      header  bool    (csv; default true; write header row)
Cell types:           string | number (float64) | bool | null (empty cell)
Result (success):     {"path": "...", "format": "csv"|"xlsx",
                       "rows_written": int, "bytes": int}
Errors:               unknown extension/format -> descriptive error;
                      xlsx without sheets -> error listing required shape.
Dependencies (input): github.com/xuri/excelize/v2 v2.8.1 (BSD-3)  [VERIFY
                      latest license at implementation: excelize is BSD-3;
                      if actually MIT, record actual in go.sum; either is
                      link-safe per the no-GPL-in-binary rule]
Fence:                writes pass through session working-dir fence; no
                      absolute-path escape outside session sandbox.
```

### Contract D: MCP catalog entries (Branch 01)

```
File: config/mcp_servers.json5 only.

Entry 1 (ENABLED):
  name:       "obscura"
  category:   "browser"
  enabled:    true
  command:    ["/Users/caimlas/git/obscura/target/release/obscura", "mcp"]
  note:       comments must state: binary is a local build (0.1.0-dev,
              2026-09-05); stdio transport; anti-detect + geo shim
              (OBSCURA_GEO_LOCATION lat,lon) + OBSCURA_PROXY supported via
              env passthrough; upstream https://github.com/h4ckf0r0day/obscura
  (path is ABSOLUTE because obscura is not on a standard PATH for the
   daemon's launch environment; comment documents the assumption)

Entry 2 (DISABLED fallback):
  name:       "excel"
  category:   "data"
  enabled:    false
  command:    ["uvx", "excel-mcp-server", "stdio"]
  note:       comments must state: higher-capability xlsx fallback (pivot
              tables, charts, conditional formatting) for when
              spreadsheet_write is insufficient; disabled by default
              because a subprocess write bypasses the pending_changes
              fence; upstream https://github.com/haris-musa/excel-mcp-server
```

## Child Index

| Doc | Type | Scope | Est. context | Dependencies |
|-----|------|-------|--------------|--------------|
| 01-mcp-catalog.md | leaf | config only: 2 catalog entries + smoke check | ~25K | none |
| 02-pdf-read/ | branch | pdf_read tool + web_fetch fix | | |
| 02-pdf-read/01-pdf-lib-wiring.md | leaf | go.mod dep + extract core + tests | ~45K | none |
| 02-pdf-read/02-tool-surface.md | leaf | tool wrapper, params, SSRF, registration | ~50K | 01 |
| 02-pdf-read/03-webfetch-sniff.md | leaf | web_fetch PDF guard, both paths + tests | ~35K | none (parallel with 01) |
| 03-spreadsheet/ | branch | spreadsheet_write tool | | |
| 03-spreadsheet/01-csv-writer.md | leaf | CSV path + tests | ~40K | none |
| 03-spreadsheet/02-xlsx-writer.md | leaf | excelize dep + XLSX path + tests | ~55K | none (parallel with 01) |
| 03-spreadsheet/03-registration.md | leaf | components.go wiring + e2e smoke | ~30K | 01+02 |

Concurrency groups:
- Wave 1 (parallel): 01-mcp-catalog, 02/01-pdf-lib-wiring, 02/03-webfetch-sniff, 03/01-csv-writer, 03/02-xlsx-writer
- Wave 2: 02/02-tool-surface (after 02/01)
- Wave 3: 03/03-registration (after 03/01 + 03/02)

## Dispatch Protocol

For each child document:

1. **Dispatch implementation agent** via `delegate_task` with the leaf
   document content as context, plus this master's Interface Contracts
   section, plus: "Do NOT commit. Do NOT run git add. Write code, run
   tests, report results only. The orchestrator handles all git
   operations." Batch Wave-1 leaves up to `delegation.max_concurrent_children`.
2. **Review in-session** (main model, NOT a delegated reviewer): build,
   run tests, verify contracts A-D verbatim, check for debug artifacts
   and stray TODOs. Re-dispatch with specific feedback on gaps (max 3
   iterations).
3. **Commit** only after review passes: `git add` the leaf's exact file
   list, commit `feat(tools): <leaf summary> (plan 20260905-research-audit-tools)`.
4. **Update tracking tables** in this file and the branch orchestrator.

 AGENTS.md obligations for every leaf: no `_ = fn()` ignored errors, no
 bare `panic(err)`, two-value type assertions on `map[string]any`,
 typed-nil guards in all Set* methods, no `os.Getwd()` in daemon code,
 `TEST_PACKAGE_PARALLELISM=2` on any full-suite run.

## Coding Conventions

- Go stdlib + existing dep graph only, plus the two new deps pinned in
  Contracts A and C. No other new dependencies.
- Errors: `fmt.Errorf("context: %w", err)` wrapping; errors checked on
  every call (pre-commit blocks ignored-error introductions).
- Tool naming/structure mirrors `internal/tools/builtin/web_fetch.go`:
  Name()/Category()/Description()/Parameters()/Execute(), schema
  validation via `schema_validation.go` helpers, registered only in
  components.go (single registration site per tool).
- Tests: table-driven, `_test.go` beside source, httptest servers for
  URL-path tests; no network in unit tests.
- Comments explain WHY; cite contract letters (A-D) at definition sites.
- gofmt clean; `go vet ./internal/tools/...` clean before report.

## Completion Tracking Table

| Doc | Status | Notes |
|-----|--------|-------|
| 01-mcp-catalog.md | COMPLETE | 2026-09-05. COMMITTED 826a5651. Catalog tests re-run by orchestrator. |
| 02-pdf-read/01-pdf-lib-wiring.md | COMPLETE | 2026-09-05. COMMITTED 49841775. API-drift claims verified via go doc. |
| 02-pdf-read/02-tool-surface.md | COMPLETE | 2026-09-05. COMMITTED 9b017602. Registration + secOrch mirror verified. |
| 02-pdf-read/03-webfetch-sniff.md | COMPLETE | 2026-09-05. COMMITTED 49841775. Both paths guarded pre-stripHTML. |
| 03-spreadsheet/01-csv-writer.md | COMPLETE | 2026-09-05. COMMITTED 49841775. |
| 03-spreadsheet/02-xlsx-writer.md | COMPLETE | 2026-09-05. COMMITTED 49841775. BSD-3 license verified in module cache. |
| 03-spreadsheet/03-registration.md | COMPLETE | 2026-09-05. COMMITTED dffca16e. |

## PROJECT COMPLETE (2026-09-05)

Integration verified: `go build ./...` clean, daemon builds, short suites for
internal/tools/..., internal/config, internal/tools/mcp, internal/daemon all ok.
Line-number corruption grep: 0 hits. Docs: AGENTS.md + tool-routing.md +
external-integrations.md updated in f0b0c62f.

## Review Checklist (root)

- [ ] Every contract (A-D) verified verbatim against implementation
- [ ] `go build ./...` clean; `go vet ./internal/tools/...` clean
- [ ] `go test -p 2 ./internal/tools/... ./internal/daemon/...` green
- [ ] `pdf_fetch`/`spreadsheet` tools appear in registry wiring test
- [ ] No new deps beyond Contracts A and C; go.sum clean
- [ ] No debug artifacts, no TODOs, no placeholder values
- [ ] No line-number corruption (`grep -rcE '^\s+[0-9]+\|' --include='*.go' internal/tools/` = 0)
- [ ] AGENTS.md updated in final integration commit (new tools listed,
      mcp catalog entries documented under Configuration)
- [ ] `docs/workflows/tools.md` (or nearest feature doc) updated for both tools

## Integration Test Plan

1. `make build` succeeds end-to-end.
2. `go test -p 2 ./...` (short mode) green repo-wide.
3. Manual smoke (orchestrator, in-session): launch daemon, confirm
   `pdf_read` and `spreadsheet_write` appear in the tool list; run
   `pdf_read` against a generated test PDF (leaf 02 test fixture reused);
   run `spreadsheet_write` to a temp dir, re-open XLSX via excelize in a
   Go test to verify round-trip.
4. MCP smoke: with catalog entry enabled, `meept` mcp list shows obscura;
   (optional, requires daemon restart) obscura browser_navigate against
   example.com returns page title.
5. `make lint-ci` (golangci-lint + analyzers) clean on touched packages.

## Open Questions

None blocking. Two recorded decisions: (1) obscura binary referenced by
absolute build path — revisit if obscura gets installed to PATH (would
become bare `obscura`); (2) excelize license recorded at implementation
time per Contract C note.
