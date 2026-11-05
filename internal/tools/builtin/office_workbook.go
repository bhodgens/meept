package builtin

// office_workbook builtin tool (contract C11.4, .hermes/contracts/06-office-skills.md;
// leaf 06-office-skills/03-xlsx-skill).
//
// Create / read / edit / validate Excel .xlsx workbooks in the session
// working directory, backed by excelize v2.11.0 (already pinned; the engine
// behind spreadsheet_write's writeXLSX seam). Model-facing rules honored:
//   - formulas are PRESERVED verbatim (data_only=false): read/validate
//     report the formula string and never claim a computed result
//     (excelize does not evaluate formulas; cached values are reported as
//     cached, never as newly calculated);
//   - every path resolves against the session working directory
//     (tools.WorkingDirFromContext) and escapes are refused — same fence
//     pattern as SpreadsheetWriteTool;
//   - reopen-and-verify: `create` and `edit` reopen the saved file and
//     report the verified sheet names + row/cell counts from disk, never
//     from the in-memory writer alone;
//   - numbers stay numbers: typed cell readback distinguishes numeric /
//     string / bool / formula / blank.
//
// Registration into the daemon tool container is the orchestrator's
// Phase-3 step (C11.5); this file only defines + constructs the tool.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	excelize "github.com/xuri/excelize/v2"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
)

// OfficeWorkbookTool implements office_workbook (C11.4).
type OfficeWorkbookTool struct {
	tools.ToolDefaults
}

// NewOfficeWorkbookTool creates the office_workbook tool (no constructor
// deps — the fence rides on the per-execute context).
func NewOfficeWorkbookTool() *OfficeWorkbookTool {
	return &OfficeWorkbookTool{}
}

func (t *OfficeWorkbookTool) Name() string { return "office_workbook" }

func (t *OfficeWorkbookTool) Category() string { return "filesystem" }

func (t *OfficeWorkbookTool) Description() string {
	return "Create, read, edit, or validate Excel .xlsx workbooks in the session working directory. action=create builds a new workbook from sheets {name,headers,rows,highlight_rows} (same shape as spreadsheet_write xlsx). action=read returns sheet names with typed cell values; formulas are returned verbatim (data_only=false) and are NOT evaluated. action=edit applies targeted cell changes {sheet,cell,value|formula}. action=validate reopens the file and reports verified sheet names, cell types, and formula preservation. Paths outside the session working directory are refused."
}

// IsReadOnly reports true for read/validate only (C11.4).
func (t *OfficeWorkbookTool) IsReadOnly(input map[string]any) bool {
	action, _ := input["action"].(string)
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "read", "validate":
		return true
	default:
		return false
	}
}

func (t *OfficeWorkbookTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"path": {
				Type:        schemaTypeString,
				Description: "Workbook path (.xlsx). Resolved against the session working directory; escapes are refused.",
			},
			"action": {
				Type:        schemaTypeString,
				Description: `One of "create" | "edit" | "read" | "validate".`,
				Enum:        []string{"create", "edit", "read", "validate"},
			},
			"sheets": {
				Type:        schemaTypeArray,
				Description: `create: array of sheet objects {"name": str, "headers": [str], "rows": [[cell]], "highlight_rows": [int]}. Cells: string, number, boolean, or null.`,
				Items: &llm.ParameterProperty{
					Type:        schemaTypeObject,
					Description: "One sheet: name, headers, rows, highlight_rows.",
				},
			},
			"cells": {
				Type:        schemaTypeArray,
				Description: `edit: array of targeted changes {"sheet": str, "cell": "A1", "value": cell OR "formula": "=SUM(A1:A3)"}. A formula cell keeps the formula text (never evaluated).`,
				Items: &llm.ParameterProperty{
					Type:        schemaTypeObject,
					Description: "One cell change: sheet, cell, value|formula.",
				},
			},
		},
		Required: []string{"path", "action"},
	}
}

// Execute implements tools.Tool (C11.4 action semantics).
func (t *OfficeWorkbookTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	action, _ := args["action"].(string)
	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case "create", "edit", "read", "validate":
	default:
		return nil, fmt.Errorf("office_workbook: unknown action %q (want create|edit|read|validate)", action)
	}

	path, _ := args["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("office_workbook: no path specified")
	}

	// Fence: session working directory only (never the process cwd, per
	// AGENTS.md). Mirrors SpreadsheetWriteTool.Execute.
	sessionDir := tools.WorkingDirFromContext(ctx)
	if sessionDir == "" {
		return nil, fmt.Errorf("office_workbook: no session working directory in context: cannot resolve path %q safely", path)
	}
	absSession, err := filepath.Abs(sessionDir)
	if err != nil {
		return nil, fmt.Errorf("office_workbook: cannot resolve session working directory: %w", err)
	}
	resolved, err := resolvePath(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("office_workbook: invalid path %q: %w", path, err)
	}
	resolved = filepath.Clean(resolved)
	if resolved != absSession && !strings.HasPrefix(resolved, absSession+string(filepath.Separator)) {
		return nil, fmt.Errorf("office_workbook: path %q resolves to %s, which is outside the session working directory %s: refusing", path, resolved, absSession)
	}
	if strings.ToLower(filepath.Ext(resolved)) != ".xlsx" {
		return nil, fmt.Errorf("office_workbook: %q is not an .xlsx workbook", path)
	}

	switch action {
	case "create":
		return t.create(ctx, resolved, args)
	case "edit":
		return t.edit(ctx, resolved, args)
	case "read":
		return t.read(ctx, resolved)
	case "validate":
		return t.validate(ctx, resolved)
	}
	return nil, fmt.Errorf("office_workbook: unhandled action %q", action)
}

// create builds a new workbook from the sheets argument, reusing the
// spreadsheet_write SheetSpec writer seam, then reopens and verifies
// (T5 reopen-and-verify).
func (t *OfficeWorkbookTool) create(ctx context.Context, resolved string, args map[string]any) (any, error) {
	rawSheets, ok := args["sheets"].([]any)
	if !ok || len(rawSheets) == 0 {
		return nil, fmt.Errorf("office_workbook: create requires a non-empty \"sheets\" array; each sheet is an object with \"name\" (string), \"headers\" (array of strings), \"rows\" (array of row arrays), optional \"highlight_rows\"")
	}
	sheets, err := parseSheetSpecs(rawSheets)
	if err != nil {
		return nil, fmt.Errorf("office_workbook: %w", err)
	}
	rowsWritten, written, err := writeXLSXToFile(resolved, sheets)
	if err != nil {
		return nil, fmt.Errorf("office_workbook: %w", err)
	}

	// Reopen-and-verify: report the sheet names AS READ BACK FROM DISK.
	verify, err := verifyWorkbook(resolved)
	if err != nil {
		return nil, fmt.Errorf("office_workbook: created %s but reopen-and-verify failed: %w", resolved, err)
	}

	return tools.ToolResult{
		Success: true,
		Result: map[string]any{
			"path":         resolved,
			"action":       "create",
			"rows_written": rowsWritten,
			"bytes":        written,
			"verified":     verify,
		},
		Evidence: []models.Evidence{
			models.NewEvidence(
				models.EvidenceFileExists,
				resolved,
				fmt.Sprintf("action=create,rows_written=%d,bytes=%d,sheets=%d", rowsWritten, written, len(verify.SheetNames)),
				t.Name(),
			),
		},
	}, nil
}

// edit applies targeted cell changes to an existing workbook, saves in
// place, then reopens and verifies the touched cells (T5). Formula cells
// keep their formula text — excelize never evaluates them (T3).
func (t *OfficeWorkbookTool) edit(ctx context.Context, resolved string, args map[string]any) (any, error) {
	rawCells, ok := args["cells"].([]any)
	if !ok || len(rawCells) == 0 {
		return nil, fmt.Errorf("office_workbook: edit requires a non-empty \"cells\" array of {sheet,cell,value|formula} objects")
	}
	if _, err := os.Stat(resolved); err != nil {
		return nil, fmt.Errorf("office_workbook: cannot edit %s: %w (create it first)", resolved, err)
	}
	f, err := excelize.OpenFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("office_workbook: open %s: %w", resolved, err)
	}
	defer f.Close()

	applied := make([]map[string]any, 0, len(rawCells))
	for i, raw := range rawCells {
		obj, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("office_workbook: cells[%d] is %T, want an object {sheet,cell,value|formula}", i, raw)
		}
		sheet, _ := obj["sheet"].(string)
		cell, _ := obj["cell"].(string)
		sheet = strings.TrimSpace(sheet)
		cell = strings.TrimSpace(strings.ToUpper(cell))
		if sheet == "" || cell == "" {
			return nil, fmt.Errorf("office_workbook: cells[%d] needs both \"sheet\" and \"cell\"", i)
		}
		if idx, err := f.GetSheetIndex(sheet); err != nil || idx < 0 {
			return nil, fmt.Errorf("office_workbook: cells[%d]: sheet %q not found in %s", i, sheet, resolved)
		}
		formula, hasFormula := obj["formula"].(string)
		value, hasValue := obj["value"]
		switch {
		case hasFormula && strings.TrimSpace(formula) != "":
			if err := f.SetCellFormula(sheet, cell, formula); err != nil {
				return nil, fmt.Errorf("office_workbook: cells[%d] set formula %s!%s: %w", i, sheet, cell, err)
			}
			applied = append(applied, map[string]any{"sheet": sheet, "cell": cell, "formula": formula})
		case hasValue:
			if err := f.SetCellValue(sheet, cell, value); err != nil {
				return nil, fmt.Errorf("office_workbook: cells[%d] set value %s!%s: %w", i, sheet, cell, err)
			}
			applied = append(applied, map[string]any{"sheet": sheet, "cell": cell, "value": value})
		default:
			return nil, fmt.Errorf("office_workbook: cells[%d] has neither \"value\" nor \"formula\"", i)
		}
	}

	if err := f.Save(); err != nil {
		return nil, fmt.Errorf("office_workbook: save %s: %w", resolved, err)
	}

	// Reopen-and-verify each touched cell (formulas verbatim).
	readBack := make([]map[string]any, 0, len(applied))
	g, err := excelize.OpenFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("office_workbook: saved %s but reopen failed: %w", resolved, err)
	}
	defer g.Close()
	for _, a := range applied {
		sheet := a["sheet"].(string)
		cell := a["cell"].(string)
		entry := map[string]any{"sheet": sheet, "cell": cell}
		if formula, err := g.GetCellFormula(sheet, cell); err == nil && formula != "" {
			entry["formula"] = formula
			entry["evaluated"] = false // excelize never evaluates: report, never claim
		} else if val, err := g.GetCellValue(sheet, cell); err == nil {
			entry["value"] = val
		}
		readBack = append(readBack, entry)
	}

	return tools.ToolResult{
		Success: true,
		Result: map[string]any{
			"path":     resolved,
			"action":   "edit",
			"applied":  len(applied),
			"verified": readBack,
		},
	}, nil
}

// read returns sheet names with typed cell values; formulas preserved
// verbatim, never reported as computed results (T3).
func (t *OfficeWorkbookTool) read(ctx context.Context, resolved string) (any, error) {
	f, err := excelize.OpenFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("office_workbook: open %s: %w", resolved, err)
	}
	defer f.Close()

	sheets := make([]map[string]any, 0, 4)
	for _, name := range f.GetSheetList() {
		rows, err := f.Rows(name)
		if err != nil {
			return nil, fmt.Errorf("office_workbook: read sheet %q: %w", name, err)
		}
		cells := make([]map[string]any, 0, 64)
		rowIdx := 0
		for rows.Next() {
			rowIdx++
			cols, err := rows.Columns()
			if err != nil {
				rows.Close()
				return nil, fmt.Errorf("office_workbook: sheet %q row %d: %w", name, rowIdx, err)
			}
			for colIdx, raw := range cols {
				cellName, err := excelize.CoordinatesToCellName(colIdx+1, rowIdx)
				if err != nil {
					continue
				}
				formula, fErr := f.GetCellFormula(name, cellName)
				if fErr == nil && strings.TrimSpace(formula) != "" {
					// T3: preserve the formula text; never claim a computed value.
					entry := map[string]any{"cell": cellName, "formula": formula, "evaluated": false}
					if cached := strings.TrimSpace(raw); cached != "" {
						entry["cached_value"] = cached // may be absent/stale — reported AS cached
					}
					cells = append(cells, entry)
					continue
				}
				if strings.TrimSpace(raw) == "" {
					continue
				}
				cells = append(cells, map[string]any{"cell": cellName, "value": typedCellValue(raw)})
			}
		}
		rows.Close()
		sheets = append(sheets, map[string]any{
			"name":       name,
			"cell_count": len(cells),
			"cells":      cells,
		})
	}

	return tools.ToolResult{
		Success: true,
		Result: map[string]any{
			"path":   resolved,
			"action": "read",
			"sheets": sheets,
		},
	}, nil
}

// validate reopens the workbook and checks sheet structure, cell types and
// formula preservation (C11.4: reopen and check sheet names/values/types/
// formulas).
func (t *OfficeWorkbookTool) validate(ctx context.Context, resolved string) (any, error) {
	verify, err := verifyWorkbook(resolved)
	if err != nil {
		return tools.ToolResult{
			Success: false,
			ErrCode: "validate_failed",
			Error:   fmt.Sprintf("office_workbook: validation failed for %s: %v", resolved, err),
		}, nil
	}
	return tools.ToolResult{
		Success: true,
		Result: map[string]any{
			"path":     resolved,
			"action":   "validate",
			"valid":    true,
			"verified": verify,
		},
	}, nil
}

// verifiedWorkbook is the reopen-and-verify payload (T5): facts read back
// from the saved file, never from the writer's in-memory state.
type verifiedWorkbook struct {
	SheetNames   []string        `json:"sheet_names"`
	Sheets       []verifiedSheet `json:"sheets"`
	FormulaCells int             `json:"formula_cells"`
	NumericCells int             `json:"numeric_cells"`
	StringCells  int             `json:"string_cells"`
}

type verifiedSheet struct {
	Name     string `json:"name"`
	Rows     int    `json:"rows"`
	Cells    int    `json:"cells"`
	Formulas int    `json:"formulas"`
}

func verifyWorkbook(path string) (*verifiedWorkbook, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := &verifiedWorkbook{SheetNames: f.GetSheetList()}
	for _, name := range out.SheetNames {
		rows, err := f.Rows(name)
		if err != nil {
			return nil, err
		}
		vs := verifiedSheet{Name: name}
		rowIdx := 0
		for rows.Next() {
			rowIdx++
			cols, err := rows.Columns()
			if err != nil {
				rows.Close()
				return nil, err
			}
			for colIdx, raw := range cols {
				cellName, err := excelize.CoordinatesToCellName(colIdx+1, rowIdx)
				if err != nil {
					continue
				}
				if formula, err := f.GetCellFormula(name, cellName); err == nil && strings.TrimSpace(formula) != "" {
					vs.Cells++
					vs.Formulas++
					out.FormulaCells++
					continue
				}
				if strings.TrimSpace(raw) == "" {
					continue
				}
				vs.Cells++
				if _, err := strconv.ParseFloat(raw, 64); err == nil {
					out.NumericCells++
				} else {
					out.StringCells++
				}
			}
		}
		rows.Close()
		vs.Rows = rowIdx
		out.Sheets = append(out.Sheets, vs)
	}
	return out, nil
}

// typedCellValue converts a raw cell string to its typed JSON value
// (number stays a number, bool stays a bool) so read results never
// stringify numeric cells.
func typedCellValue(raw string) any {
	if v, err := strconv.ParseFloat(raw, 64); err == nil {
		return v
	}
	if v, err := strconv.ParseBool(raw); err == nil {
		return v
	}
	return raw
}
