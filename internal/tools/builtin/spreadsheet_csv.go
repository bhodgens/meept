package builtin

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
)

// SheetSpec describes one sheet of a tabular export for the
// spreadsheet_write tool (Contract C). It is the frozen writer seam from
// docs/plans/20260905-research-audit-tools/03-spreadsheet/orchestrator.md
// and is shared with the xlsx leaf: writeXLSX (spreadsheet_xlsx.go)
// consumes []SheetSpec and must NOT redefine the struct.
type SheetSpec struct {
	Name          string   // sheet name (ignored by CSV; single sheet)
	Headers       []string // column headers
	Rows          [][]any  // cell: string | float64 | bool | nil
	HighlightRows []int    // 0-based row indices into Rows (CSV ignores; XLSX fills yellow)
}

// writeCSV writes rows to w via encoding/csv (Comma ',', UseCRLF false,
// stdlib quoting). When hdr is true, rows[0] is the header row: it is
// written first and counted, and data rows follow. SheetSpec-level
// concerns (sheet name, highlighting) do not apply to the single-sheet CSV
// path. Float64 cells use strconv.FormatFloat(v, 'f', -1, 64) so values
// never render in scientific notation; bools render "true"/"false"; nil
// renders "". Returns the number of rows written (headers included in the
// count when hdr). Returns a descriptive error (naming row/col and the
// offending type) on an unsupported cell type, and wraps csv errors as
// "csv write: ...".
func writeCSV(w io.Writer, hdr bool, rows [][]any) (int, error) {
	cw := csv.NewWriter(w)
	count := 0

	for rowIdx, row := range rows {
		record := make([]string, len(row))
		for col, cell := range row {
			s, err := formatCSVCell(rowIdx, col, cell)
			if err != nil {
				return count, err
			}
			record[col] = s
		}
		if err := cw.Write(record); err != nil {
			return count, fmt.Errorf("csv write: %w", err)
		}
		count++
	}

	cw.Flush()
	if err := cw.Error(); err != nil {
		return count, fmt.Errorf("csv write: %w", err)
	}
	return count, nil
}

// formatCSVCell renders one cell per the Contract C mapping: string passes
// through, float64 renders without scientific notation, bool renders
// "true"/"false", nil renders "". Any other type is rejected with an error
// naming the row, column, and offending type.
func formatCSVCell(row, col int, cell any) (string, error) {
	switch v := cell.(type) {
	case string:
		return v, nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(v), nil
	case nil:
		return "", nil
	default:
		return "", fmt.Errorf("row %d col %d: unsupported cell type %T (want string, float64, bool, or nil)", row, col, v)
	}
}
