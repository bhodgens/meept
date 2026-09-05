# orchestrator.md — 03-spreadsheet branch

## Goal

Deliver Contract C (`spreadsheet_write` tool: CSV + XLSX writers and
components.go registration) from the parent master
(`docs/plans/20260905-research-audit-tools/master.md`).

## Architecture Overview

Three leaves:

- 01-csv-writer: pure CSV path (`internal/tools/builtin/spreadsheet_csv.go`)
  via stdlib encoding/csv + tests. No tool surface.
- 02-xlsx-writer: excelize dependency + XLSX path
  (`internal/tools/builtin/spreadsheet_xlsx.go`) + tests. Parallel with 01.
- 03-registration: the `spreadsheet_write` Tool wrapper
  (`spreadsheet_write.go`) that dispatches to csv/xlsx writers per
  Contract C, components.go wiring, e2e round-trip test.

01 and 02 define identical writer seams so 03 composes them without
coordination.

## Interface Contracts

Writer seam (identical shape in both leaves; frozen here):

```go
// spreadsheet_csv.go
type SheetSpec struct {
    Name          string
    Headers       []string
    Rows          [][]any          // cell: string | float64 | bool | nil
    HighlightRows []int            // 0-based row indices into Rows
}

// writeCSV writes rows (headers first when hdr) to w. CSV ignores
// HighlightRows and SheetSpec.Name (single sheet). Returns rows written
// (headers included in the count when hdr).
func writeCSV(w io.Writer, hdr bool, rows [][]any) (int, error)

// spreadsheet_xlsx.go
// writeXLSX writes sheets to w as a single-sheet-streamed xlsx (excelize
// WriteTo). Sheet 0 gets highlightRows filled yellow (#FFFF00) on A:N of
// each highlighted row (N = len(Headers) or len(row)).
// Returns total data rows written across sheets (headers included).
func writeXLSX(w io.Writer, sheets []SheetSpec) (int, error)
```

Tool-level Contract C is frozen in the parent master.md — restate it
verbatim in leaf 03's dispatch context. Cell type mapping is part of the
contract: string→string, float64→number, bool→bool, nil→empty.

## Child Index

| Doc | Scope | Est. context | Dependencies |
|-----|-------|--------------|--------------|
| 01-csv-writer.md | writeCSV + tests | ~40K | none |
| 02-xlsx-writer.md | excelize + writeXLSX + tests | ~55K | none |
| 03-registration.md | tool wrapper + wiring + round-trip | ~30K | 01+02 |

Dispatch order: batch 01 + 02 in parallel (Wave 1), then 03 (Wave 3 in
parent terms; Wave 2 of this branch).

## Dispatch Protocol

Per parent master.md Dispatch Protocol. Leaf contexts must inline the
writer seam above + Contract C verbatim. "Do NOT commit" on every
dispatch. In-session review (build, tests, contract diff, stray-artifact
grep). Max 3 re-dispatch iterations per leaf. Commit after review with
the leaf's exact file list; update tracking tables here and in master.md.

## Coding Conventions

Per parent master.md. Extras:
- CSV: encoding/csv with `csv.Writer`, `UseCRLF=false`, `Comma=','`.
  Quoting is the stdlib's default; do not hand-roll quoting.
- XLSX: excelize `NewFile()` → `SetSheetName`/`NewSheet` →
  `SetCellValue` per cell → `WriteTo(w)`. Do not use
  `SaveAs` (path-based); keep io.Writer streaming. Cell coordinate
  helper: excelize `CoordinatesToCellName`.
- Highlight fill: `excelize.NewStyle` with fill `#FFFF00` applied via
  `SetCellStyle` per highlighted row range.
- License verification for excelize is part of leaf 02 Task 1: record
  the actual license from the module's LICENSE file in go.sum-adjacent
  report notes (expected BSD-3; parent Contract C note governs).

## Completion Tracking Table

| Doc | Status | Notes |
|-----|--------|-------|
| 01-csv-writer.md | PENDING | |
| 02-xlsx-writer.md | PENDING | |
| 03-registration.md | PENDING | blocked by 01+02 |

## Review Checklist (branch)

- [ ] Contract C verbatim (params, result map, cell types, fence)
- [ ] Writer seams match this orchestrator's frozen shape in both leaves
- [ ] `go build ./...`; `go vet ./internal/tools/...`; tests green
- [ ] XLSX round-trip test re-opens via excelize Reader and asserts values
- [ ] No path escape: leaf 03 test proves writes outside session sandbox are refused
- [ ] gofmt clean; no TODOs/debug prints; go.mod additions limited to excelize

## Integration Test Plan

After all leaves REVIEWED: `go test -p 2 ./internal/tools/builtin/ -run 'Spreadsheet|CSV|XLSX' -count=1`;
`go build ./cmd/meept-daemon`; U1000 sweep (no unused helpers — remove,
not nolint); mark branch COMPLETE in parent master.md.
