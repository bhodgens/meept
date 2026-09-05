package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
)

// SpreadsheetWriteTool writes tabular audit data to CSV or XLSX files
// (Contract C, plan 20260905-research-audit-tools). The writer seams are
// writeCSV (spreadsheet_csv.go, leaf 01) and writeXLSX
// (spreadsheet_xlsx.go, leaf 02); this tool owns parameter parsing, format
// dispatch, parent-directory creation, and the session working-dir fence.
//
// Fence: every path resolves against the session working directory injected
// via tools.ContextWithWorkingDir (never the process cwd, per AGENTS.md),
// and paths resolving outside that directory are refused — there is no
// absolute-path escape outside the session sandbox.
type SpreadsheetWriteTool struct {
	tools.ToolDefaults
}

// NewSpreadsheetWriteTool creates a new spreadsheet_write tool (Contract C;
// no constructor deps — the fence rides on the per-execute context).
func NewSpreadsheetWriteTool() *SpreadsheetWriteTool {
	return &SpreadsheetWriteTool{}
}

func (t *SpreadsheetWriteTool) Name() string { return "spreadsheet_write" }

func (t *SpreadsheetWriteTool) Category() string { return "filesystem" }

func (t *SpreadsheetWriteTool) Description() string {
	return "Write tabular data to a CSV or XLSX file. Extension (.csv/.xlsx) selects the format unless an explicit format is given; XLSX supports multiple named sheets with headers and yellow-highlighted rows. Paths stay inside the session working directory."
}

func (t *SpreadsheetWriteTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"path": {
				Type:        schemaTypeString,
				Description: "Output file path (.csv or .xlsx; extension picks the format unless 'format' is set). Parent directories are created automatically. Resolved against the session working directory; escapes are refused.",
			},
			"format": {
				Type:        schemaTypeString,
				Description: `Output format: "csv" or "xlsx". Defaults from the file extension; an explicit format wins.`,
			},
			"rows": {
				Type:        schemaTypeArray,
				Description: `CSV data: array of row arrays (first row is the header when 'header' is true). Cells: string, number, boolean, or null (empty). Required when format resolves to csv.`,
				Items: &llm.ParameterProperty{
					Type:        schemaTypeArray,
					Description: "One row of cells: string, number, boolean, or null.",
				},
			},
			"header": {
				Type:        schemaTypeBoolean,
				Description: "CSV only: write the first row of 'rows' as a header row (default true).",
			},
			"sheets": {
				Type:        schemaTypeArray,
				Description: `XLSX data: array of sheet objects {"name": str, "headers": [str], "rows": [[cell]], "highlight_rows": [int] (0-based data rows to fill yellow)}. Required (>=1) when format resolves to xlsx.`,
				Items: &llm.ParameterProperty{
					Type:        schemaTypeObject,
					Description: "One sheet: name, headers, rows, highlight_rows.",
				},
			},
		},
		Required: []string{"path"},
	}
}

// Execute implements the Tool interface. See Contract C in
// docs/plans/20260905-research-audit-tools/master.md.
func (t *SpreadsheetWriteTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	path, _ := args["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("no path specified: spreadsheet_write requires an output file path")
	}

	format, _ := args["format"].(string)
	format = strings.ToLower(strings.TrimSpace(format))

	header := true
	if h, ok := args["header"].(bool); ok {
		header = h
	}

	// Fence: resolve against the session working directory (context
	// convention, mirroring the filesystem tools) and refuse any path that
	// escapes it. No session working dir => hard refusal, never a
	// process-cwd or temp-dir fallback.
	sessionDir := tools.WorkingDirFromContext(ctx)
	if sessionDir == "" {
		return nil, fmt.Errorf("spreadsheet_write: no session working directory in context: cannot resolve output path %q safely", path)
	}
	absSession, err := filepath.Abs(sessionDir)
	if err != nil {
		return nil, fmt.Errorf("spreadsheet_write: cannot resolve session working directory: %w", err)
	}
	resolved, err := resolvePath(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("spreadsheet_write: invalid path %q: %w", path, err)
	}
	resolved = filepath.Clean(resolved)
	if resolved != absSession && !strings.HasPrefix(resolved, absSession+string(filepath.Separator)) {
		return nil, fmt.Errorf("spreadsheet_write: path %q resolves to %s, which is outside the session working directory %s: refusing to write", path, resolved, absSession)
	}

	if format == "" {
		// Format from extension.
		switch strings.ToLower(filepath.Ext(resolved)) {
		case ".csv":
			format = "csv"
		case ".xlsx":
			format = "xlsx"
		default:
			return nil, fmt.Errorf("spreadsheet_write: unknown output format for %q: unsupported extension %q (want .csv or .xlsx, or pass an explicit \"format\")", path, filepath.Ext(resolved))
		}
	}

	var (
		rowsWritten int
		written     int64
		err2        error
	)
	switch format {
	case "csv":
		rawRows, ok := args["rows"].([]any)
		if !ok || len(rawRows) == 0 {
			return nil, fmt.Errorf("spreadsheet_write: csv output requires a non-empty \"rows\" array (array of row arrays; first row is the header when \"header\" is true)")
		}
		rows, err := parseCSVRows(rawRows)
		if err != nil {
			return nil, err
		}
		rowsWritten, written, err2 = writeCSVToFile(resolved, header, rows)
	case "xlsx":
		rawSheets, ok := args["sheets"].([]any)
		if !ok || len(rawSheets) == 0 {
			return nil, fmt.Errorf("spreadsheet_write: xlsx output requires a non-empty \"sheets\" array; each sheet must be an object with \"name\" (string), \"headers\" (array of strings), \"rows\" (array of row arrays), and optional \"highlight_rows\" (array of 0-based row indices)")
		}
		sheets, err := parseSheetSpecs(rawSheets)
		if err != nil {
			return nil, err
		}
		rowsWritten, written, err2 = writeXLSXToFile(resolved, sheets)
	default:
		return nil, fmt.Errorf("spreadsheet_write: unknown format %q (want \"csv\" or \"xlsx\")", format)
	}
	if err2 != nil {
		return nil, err2
	}

	return tools.ToolResult{
		Success: true,
		Result: map[string]any{
			"path":         resolved,
			"format":       format,
			"rows_written": rowsWritten,
			"bytes":        written,
		},
		Evidence: []models.Evidence{
			models.NewEvidence(
				models.EvidenceFileExists,
				resolved,
				fmt.Sprintf("format=%s,rows_written=%d,bytes=%d", format, rowsWritten, written),
				t.Name(),
			),
		},
	}, nil
}

// writeCSVToFile creates parent directories and streams the CSV to the
// file, returning rows written (headers included) and bytes written.
func writeCSVToFile(path string, header bool, rows [][]any) (int, int64, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, 0, fmt.Errorf("spreadsheet_write: cannot create parent directories: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return 0, 0, fmt.Errorf("spreadsheet_write: cannot create %s: %w", path, err)
	}
	defer f.Close()
	n, err := writeCSV(f, header, rows)
	if err != nil {
		return n, 0, err
	}
	size, err := syncAndSize(f)
	if err != nil {
		return n, 0, err
	}
	return n, size, nil
}

// writeXLSXToFile creates parent directories and streams the workbook to
// the file, returning rows written (headers included) and bytes written.
func writeXLSXToFile(path string, sheets []SheetSpec) (int, int64, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, 0, fmt.Errorf("spreadsheet_write: cannot create parent directories: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return 0, 0, fmt.Errorf("spreadsheet_write: cannot create %s: %w", path, err)
	}
	defer f.Close()
	n, err := writeXLSX(f, sheets)
	if err != nil {
		return n, 0, err
	}
	size, err := syncAndSize(f)
	if err != nil {
		return n, 0, err
	}
	return n, size, nil
}

// syncAndSize flushes an output file to disk and reports its byte size.
func syncAndSize(f *os.File) (int64, error) {
	if err := f.Sync(); err != nil {
		return 0, fmt.Errorf("spreadsheet_write: flush: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("spreadsheet_write: stat output: %w", err)
	}
	return info.Size(), nil
}

// parseCSVRows converts the JSON rows array into [][]any for the CSV seam.
// Cells arrive from JSON decoding already typed string/float64/bool/nil.
func parseCSVRows(rawRows []any) ([][]any, error) {
	rows := make([][]any, 0, len(rawRows))
	for i, r := range rawRows {
		cells, ok := r.([]any)
		if !ok {
			return nil, fmt.Errorf("spreadsheet_write: rows[%d] is %T, want an array of cells", i, r)
		}
		rows = append(rows, cells)
	}
	return rows, nil
}

// parseSheetSpecs converts the JSON sheets array into []SheetSpec for the
// XLSX seam, validating every field with two-value type assertions.
func parseSheetSpecs(rawSheets []any) ([]SheetSpec, error) {
	sheets := make([]SheetSpec, 0, len(rawSheets))
	for i, s := range rawSheets {
		obj, ok := s.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("spreadsheet_write: sheets[%d] is %T, want an object with \"name\", \"headers\", \"rows\", \"highlight_rows\"", i, s)
		}
		name, _ := obj["name"].(string)

		var headers []string
		if raw, present := obj["headers"]; present && raw != nil {
			arr, ok := raw.([]any)
			if !ok {
				return nil, fmt.Errorf("spreadsheet_write: sheets[%d].headers is %T, want an array of strings", i, raw)
			}
			headers = make([]string, 0, len(arr))
			for c, h := range arr {
				hs, ok := h.(string)
				if !ok {
					return nil, fmt.Errorf("spreadsheet_write: sheets[%d].headers[%d] is %T, want a string", i, c, h)
				}
				headers = append(headers, hs)
			}
		}

		rawRows, present := obj["rows"]
		if !present || rawRows == nil {
			return nil, fmt.Errorf("spreadsheet_write: sheets[%d] is missing \"rows\" (want an array of row arrays)", i)
		}
		arr, ok := rawRows.([]any)
		if !ok {
			return nil, fmt.Errorf("spreadsheet_write: sheets[%d].rows is %T, want an array of row arrays", i, rawRows)
		}
		rows := make([][]any, 0, len(arr))
		for r, rr := range arr {
			cells, ok := rr.([]any)
			if !ok {
				return nil, fmt.Errorf("spreadsheet_write: sheets[%d].rows[%d] is %T, want an array of cells", i, r, rr)
			}
			rows = append(rows, cells)
		}

		var highlights []int
		if raw, present := obj["highlight_rows"]; present && raw != nil {
			harr, ok := raw.([]any)
			if !ok {
				return nil, fmt.Errorf("spreadsheet_write: sheets[%d].highlight_rows is %T, want an array of integers", i, raw)
			}
			for h, hv := range harr {
				fv, ok := hv.(float64)
				if !ok {
					return nil, fmt.Errorf("spreadsheet_write: sheets[%d].highlight_rows[%d] is %T, want an integer", i, h, hv)
				}
				highlights = append(highlights, int(fv))
			}
		}

		sheets = append(sheets, SheetSpec{
			Name:          name,
			Headers:       headers,
			Rows:          rows,
			HighlightRows: highlights,
		})
	}
	return sheets, nil
}

// IsReadOnly reports that spreadsheet writes mutate the filesystem.
func (t *SpreadsheetWriteTool) IsReadOnly(map[string]any) bool { return false }

// IsConcurrencySafe: two invocations may target the same path; keep the
// conservative default (false) rather than risking clobbered outputs.
func (t *SpreadsheetWriteTool) IsConcurrencySafe(map[string]any) bool { return false }

// Ensure SpreadsheetWriteTool implements the Tool interface.
var _ tools.Tool = (*SpreadsheetWriteTool)(nil)
