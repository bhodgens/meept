# leaf 03-spreadsheet/03 — spreadsheet_write tool + wiring

## DISPATCH INSTRUCTION

You are an implementation agent. Implement every task with TDD. **Do NOT
commit. Do NOT run `git add`.** Write code, run tests, report results.

- Parent: `docs/plans/20260905-research-audit-tools/03-spreadsheet/orchestrator.md`
- Scope: two new files (`internal/tools/builtin/spreadsheet_write.go`,
  `spreadsheet_write_test.go`) + ONE registration block in
  `internal/daemon/components.go`. No other files.
- Dependencies: leaves 01 (writeCSV + SheetSpec) and 02 (writeXLSX) both COMPLETE.
- Estimated context: ~30K.

## Goal

Wrap the two writers into the `spreadsheet_write` Tool per Contract C in
the root master.md, register it, and prove round-trip e2e in tests.

## Interface Contract (exposed)

```
Tool name:   "spreadsheet_write"     Category: "filesystem"
Constructor: builtin.NewSpreadsheetWriteTool() *SpreadsheetWriteTool
Parameters:  path (string, required), format (string, optional:
             "csv"|"xlsx", default from extension, explicit wins),
             header (bool, optional, default true — CSV only),
             rows ([]any rows, CSV), sheets (array of sheet objects, XLSX;
             each {"name","headers","rows","highlight_rows"})
Result:      {"path","format","rows_written","bytes"}
```

Restate the full Contract C from the parent master in your head from the
leaf text above; where this brief and Contract C differ, Contract C wins
and you note the discrepancy in your report.

## Tasks

### Task 1: TDD (spreadsheet_write_test.go first)

Table-driven, using t.TempDir() for outputs:

1. path `out.csv` + rows → file exists, content matches writeCSV
   semantics, result map {format: "csv", rows_written: N, bytes > 0}.
2. path `out.xlsx` + sheets → file exists, re-open with excelize, spot
   check A1 and one highlighted row style; result map format "xlsx".
3. explicit `format: "csv"` with `.txt` path → CSV content, format "csv".
4. unknown extension (`out.tsv`, no format) → descriptive error.
5. xlsx format without sheets → error listing required sheet shape.
6. path in a nonexistent subdir → parent dirs auto-created (MkdirAll).
7. fence: path resolving OUTSIDE the session working dir → refused with
   a clear error (mirror how filesystem tools resolve the working dir;
   if the tool struct has no working-dir support, add
   `SetWorkingDir(dir string)` with nil-safe semantics per AGENTS.md and
   resolve relative paths against it; empty dir = fall back to OS temp
   is NOT acceptable — empty dir means tests set it explicitly).

### Task 2: implement spreadsheet_write.go

- Parse params (two-value assertions on map[string]any).
- Map JSON sheets array → []SheetSpec (rows cells as any).
- Dispatch: csv → writeCSV to a created file; xlsx → writeXLSX.
- Build and return the Contract C result map; count bytes written.
- Doc-comment citing Contract C.

### Task 3: register in components.go

Immediately after the pdf_read registration block (branch 02 leaf 02 —
if that block is absent because branch 02 has not landed, place the
registration directly after the webSearchTool registration at ~:5494
and note placement in your report):

```go
	// spreadsheet_write (plan 20260905-research-audit-tools, Contract C):
	// CSV/XLSX output for audit data; writes pass through the session
	// working-dir fence.
	registry.Register(builtin.NewSpreadsheetWriteTool())
```

### Task 4: verify

```
go build ./...
go vet ./internal/tools/builtin/ ./internal/daemon/
gofmt -l internal/tools/builtin/
go test -p 2 ./internal/tools/builtin/ -run 'Spreadsheet|CSV|XLSX' -count=1
go test -p 2 ./internal/daemon/ -run 'Wiring|Registry' -count=1
```

## Self-Verification Checklist

- [ ] Tool name/category/params/result match Contract C
- [ ] All 7 cases pass incl. fence refusal
- [ ] Registration compiles; placement per Task 3
- [ ] gofmt/vet green; no TODOs; no os.Getwd; no debug prints

## Review Checklist (for orchestrator)

- [ ] Contract C result map exact; fence refusal tested
- [ ] components.go diff is the one block
- [ ] Tests self-contained in t.TempDir(); excelize used only in tests + via writers

Do NOT commit.
