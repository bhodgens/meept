package builtin

import (
	"fmt"
	"io"

	excelize "github.com/xuri/excelize/v2"
)

// writeXLSX serializes sheets as an Excel workbook streamed to w, per
// Contract C (orchestrator seam: func writeXLSX(w io.Writer, sheets
// []SheetSpec) (int, error)).
//
// Behavior: the first sheet uses SheetSpec.Name when non-empty, otherwise
// "Sheet1"; additional sheets use their names, deduplicated with "_2",
// "_3", ... suffixes on collision; Headers land on row 1 when non-empty
// and Rows start at row 2; rows listed in HighlightRows (0-based data-row
// indices) get a yellow (#FFFF00) pattern fill spanning the sheet's used
// width. Returns the total rows written across all sheets, headers
// included. Supported cell values: string, float64, bool, nil (empty).
//
// The workbook is streamed: excelize.NewFile → populate → WriteTo; no
// SaveAs and no temp files.
func writeXLSX(w io.Writer, sheets []SheetSpec) (int, error) {
	if len(sheets) == 0 {
		return 0, fmt.Errorf("no sheets provided")
	}
	f := excelize.NewFile()
	defer f.Close()

	total := 0
	for i, sheet := range sheets {
		name := sheet.Name
		if name == "" {
			name = fmt.Sprintf("Sheet%d", i+1)
		}
		if i == 0 {
			// Rename the default sheet instead of adding a new one.
			if err := f.SetSheetName("Sheet1", name); err != nil {
				return 0, fmt.Errorf("xlsx write: rename sheet: %w", err)
			}
		} else {
			name = dedupeSheetName(f, name)
			if _, err := f.NewSheet(name); err != nil {
				return 0, fmt.Errorf("xlsx write: new sheet %q: %w", name, err)
			}
		}

		width := len(sheet.Headers)
		for _, row := range sheet.Rows {
			if len(row) > width {
				width = len(row)
			}
		}

		headerRows := 0
		if len(sheet.Headers) > 0 {
			headerRows = 1
			for c, h := range sheet.Headers {
				cell, err := excelize.CoordinatesToCellName(c+1, 1)
				if err != nil {
					return 0, fmt.Errorf("xlsx write: %w", err)
				}
				if err := f.SetCellValue(name, cell, h); err != nil {
					return 0, fmt.Errorf("xlsx write: %w", err)
				}
			}
		}

		for ri, row := range sheet.Rows {
			sheetRow := headerRows + ri + 1 // data starts after headers
			for ci, v := range row {
				if v == nil {
					continue
				}
				cell, err := excelize.CoordinatesToCellName(ci+1, sheetRow)
				if err != nil {
					return 0, fmt.Errorf("xlsx write: %w", err)
				}
				switch val := v.(type) {
				case string, float64, bool:
					if err := f.SetCellValue(name, cell, val); err != nil {
						return 0, fmt.Errorf("xlsx write: %w", err)
					}
				default:
					return 0, fmt.Errorf(
						"xlsx write: unsupported cell type %T at row %d col %d (sheet %q)",
						v, sheetRow, ci+1, name)
				}
			}
		}

		for _, hi := range sheet.HighlightRows {
			if hi < 0 || hi >= len(sheet.Rows) {
				continue
			}
			sheetRow := headerRows + hi + 1
			topLeft, err := excelize.CoordinatesToCellName(1, sheetRow)
			if err != nil {
				return 0, fmt.Errorf("xlsx write: %w", err)
			}
			bottomRight, err := excelize.CoordinatesToCellName(width, sheetRow)
			if err != nil {
				return 0, fmt.Errorf("xlsx write: %w", err)
			}
			styleID, err := f.NewStyle(&excelize.Style{
				Fill: excelize.Fill{
					Type:    "pattern",
					Pattern: 1, // solid
					Color:   []string{"FFFF00"},
				},
			})
			if err != nil {
				return 0, fmt.Errorf("xlsx write: %w", err)
			}
			if err := f.SetCellStyle(name, topLeft, bottomRight, styleID); err != nil {
				return 0, fmt.Errorf("xlsx write: %w", err)
			}
		}

		total += headerRows + len(sheet.Rows)
	}

	if _, err := f.WriteTo(w); err != nil {
		return 0, fmt.Errorf("xlsx write: %w", err)
	}
	return total, nil
}

// dedupeSheetName returns name, or name_2, name_3, ... for the first
// variant not already present in the workbook. GetSheetIndex returns -1
// (not an error) when the sheet does not exist.
func dedupeSheetName(f *excelize.File, name string) string {
	if idx, _ := f.GetSheetIndex(name); idx < 0 {
		return name
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s_%d", name, n)
		if idx, _ := f.GetSheetIndex(candidate); idx < 0 {
			return candidate
		}
	}
}
