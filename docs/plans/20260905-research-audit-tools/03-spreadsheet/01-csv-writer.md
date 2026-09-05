# leaf 03-spreadsheet/01 — CSV writer

## DISPATCH INSTRUCTION

You are an implementation agent. Implement every task with TDD. **Do NOT
commit. Do NOT run `git add`.** Write code, run tests, report results.

- Parent: `docs/plans/20260905-research-audit-tools/03-spreadsheet/orchestrator.md`
- Scope: two new files (`internal/tools/builtin/spreadsheet_csv.go`,
  `spreadsheet_csv_test.go`). No other files.
- Dependencies: none.
- Estimated context: ~40K.

## Goal

Implement the frozen writer seam from the branch orchestrator:

```go
type SheetSpec struct {
    Name          string
    Headers       []string
    Rows          [][]any
    HighlightRows []int
}
func writeCSV(w io.Writer, hdr bool, rows [][]any) (int, error)
```

## Interface Contract (exposed to leaf 03)

Exactly the two symbols above in package `builtin` (SheetSpec is shared
with the xlsx leaf — define it in spreadsheet_csv.go; the xlsx leaf will
NOT redefine it, it imports from this file). Cell values: string,
float64, bool, nil. Formatting: float64 uses `strconv.FormatFloat(v,
'f', -1, 64)` (no scientific notation); bool "true"/"false"; nil → "".

## Tasks

### Task 1: TDD (spreadsheet_csv_test.go first)

Table-driven, all against bytes.Buffer:

1. hdr=true with headers+2 rows → header line first; returned count = 3.
2. hdr=false with 2 rows → no header; count = 2.
3. cell types: one row of [string, float64(42), bool(false), nil] →
   `s,42,false,` line (trailing empty field for nil).
4. float formatting: 3.14 → `3.14`; 1e21 → `1000000000000000000000` (not
   `1e+21`); -0.5 → `-0.5`.
5. quoting: cell containing comma, quote, and newline → stdlib-quoted
   correctly (assert exact quoted output).
6. empty rows slice with hdr=true and headers → header only, count 1.
7. unsupported cell type (e.g. int as any — pass a raw `int` in a test
   to prove the guard) → descriptive error naming the row/col and type.

### Task 2: implement

- encoding/csv via `csv.NewWriter(w)`; write records; `w.Flush()`;
  check `w.Error()`.
- Error wrapping `fmt.Errorf("csv write: %w", err)`.
- Doc-comment citing the orchestrator seam and Contract C.
- No filesystem access (io.Writer only). No os.Getwd.

### Task 3: verify

```
go build ./internal/tools/...
go vet ./internal/tools/builtin/
gofmt -l internal/tools/builtin/
go test -p 2 ./internal/tools/builtin/ -run CSV -count=1
```

## Self-Verification Checklist

- [ ] Seam signatures match the orchestrator contract byte-for-byte
- [ ] SheetSpec defined here (xlsx leaf relies on it)
- [ ] All 7 cases pass; float formatting proven non-scientific
- [ ] gofmt/vet green; no TODOs; no line-number prefixes

## Review Checklist (for orchestrator)

- [ ] Contract fields verbatim; error message quality (row/col named)
- [ ] Tests use bytes.Buffer only (no disk, no network)
- [ ] Diff confined to the two files

Do NOT commit.
