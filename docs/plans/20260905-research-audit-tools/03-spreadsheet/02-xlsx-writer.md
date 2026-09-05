# leaf 03-spreadsheet/02 — XLSX writer (excelize)

## DISPATCH INSTRUCTION

You are an implementation agent. Implement every task with TDD. **Do NOT
commit. Do NOT run `git add`.** Write code, run tests, report results.

- Parent: `docs/plans/20260905-research-audit-tools/03-spreadsheet/orchestrator.md`
- Scope: go.mod/go.sum (excelize only) + two new files
  (`internal/tools/builtin/spreadsheet_xlsx.go`, `spreadsheet_xlsx_test.go`).
  No other files.
- Dependencies: none (SheetSpec comes from leaf 01's spreadsheet_csv.go —
  DISPATCHED IN PARALLEL. To avoid a blocking import in tests, your test
  file defines a local test helper of the same shape ONLY if leaf 01 has
  not landed when you start; your implementation file must NOT redefine
  SheetSpec — reference it from spreadsheet_csv.go and, if that file is
  absent at your build time, create a minimal placeholder
  spreadsheet_csv.go containing ONLY the SheetSpec struct so the package
  compiles, and SAY SO in your report so the orchestrator reconciles with
  leaf 01).
- Estimated context: ~55K.

## Goal

Implement the frozen writer seam from the branch orchestrator:

```go
func writeXLSX(w io.Writer, sheets []SheetSpec) (int, error)
```

## Interface Contract (exposed to leaf 03)

Exactly `writeXLSX` above in package `builtin`, consuming the shared
SheetSpec. Behavior: first sheet uses SheetSpec.Name if non-empty else
"Sheet1"; additional sheets get their names (deduplicated with suffixes
"_2", "_3" if colliding); headers on row 1 when Headers non-empty; data
from row 2; HighlightRows get yellow fill across the row's used width;
returns total rows written (headers included).

## Tasks

### Task 1: dependency + license check

`go get github.com/xuri/excelize/v2@latest && go mod tidy`. READ the
module's LICENSE file (`go env GOMODCACHE`, find
github.com/xuri/excelize/v2@*/LICENSE). Expected BSD-3. Record the
actual license in your report. If it is GPL/AGPL, STOP and report —
per user rule, GPL must not link into shipped binaries.

### Task 2: verify API from source

`go doc github.com/xuri/excelize/v2` — confirm: NewFile, NewSheet,
SetSheetName, SetCellValue, CoordinatesToCellName, NewStyle,
NewStyle*Fill/StyleFill, SetCellStyle, WriteTo. Trust go doc over this
brief; adjust and report any drift.

### Task 3: TDD (spreadsheet_xlsx_test.go first)

Round-trip tests: write to bytes.Buffer via writeXLSX, re-open with
`excelize.OpenReader(bytes.NewReader(buf))`, assert cell values:

1. one sheet, headers+2 rows → header at A1/B1, values at A2/B2; count 3.
2. multi-sheet (2 sheets, distinct names) → both named, both populated.
3. sheet name collision ("Data" twice) → second becomes "Data_2".
4. cell types: string/float64/bool/nil land as such (GetCellValue with
   RawCellValue where relevant; nil → empty string).
5. highlight: sheet with 3 rows, HighlightRows=[1] → GetCellStyle on a
   row-2 cell shows fill (assert via excelize GetStyle or fill ID
   presence — check API; simplest reliable assertion: style differs
   from non-highlighted neighbor cell).
6. empty sheets slice → error "no sheets provided".
7. unsupported cell type → descriptive error row/col/type.

### Task 4: implement

- Per orchestrator Coding Conventions: NewFile → sheets → SetCellValue →
  WriteTo. Streaming, no SaveAs, no temp files.
- Error wrap `fmt.Errorf("xlsx write: %w", err)`.
- Doc-comment citing Contract C and the orchestrator seam.
- Close the excelize File (`defer f.Close()`).

### Task 5: verify

```
go build ./internal/tools/...
go vet ./internal/tools/builtin/
gofmt -l internal/tools/builtin/
go test -p 2 ./internal/tools/builtin/ -run XLSX -count=1
```

## Self-Verification Checklist

- [ ] writeXLSX signature matches seam; SheetSpec imported not redefined
- [ ] License recorded; go.mod has ONLY excelize added
- [ ] All 7 round-trip cases pass
- [ ] gofmt/vet green; no TODOs; no debug prints; no line-number prefixes

## Review Checklist (for orchestrator)

- [ ] Contract behavior verbatim (dedup names, counts, highlight)
- [ ] Streaming write proven (bytes.Buffer round-trip, no temp file)
- [ ] License check result stated in leaf report
- [ ] Diff: go.mod/go.sum + two files (+ placeholder csv file if needed, flagged)

Do NOT commit.
