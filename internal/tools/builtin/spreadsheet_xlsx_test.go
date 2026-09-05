package builtin

import (
	"bytes"
	"strings"
	"testing"

	excelize "github.com/xuri/excelize/v2"
)

func roundTripXLSX(t *testing.T, buf *bytes.Buffer) *excelize.File {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func mustCell(t *testing.T, f *excelize.File, sheet, cell string, raw bool) string {
	t.Helper()
	v, err := f.GetCellValue(sheet, cell, excelize.Options{RawCellValue: raw})
	if err != nil {
		t.Fatalf("GetCellValue(%s, %s): %v", sheet, cell, err)
	}
	return v
}

func mustCellStyle(t *testing.T, f *excelize.File, sheet, cell string) int {
	t.Helper()
	s, err := f.GetCellStyle(sheet, cell)
	if err != nil {
		t.Fatalf("GetCellStyle(%s, %s): %v", sheet, cell, err)
	}
	return s
}

// Case 1: one sheet, headers + 2 rows; values land in row 1 (headers) and
// rows 2-3 (data); count includes headers.
func TestWriteXLSXSingleSheet(t *testing.T) {
	var buf bytes.Buffer
	n, err := writeXLSX(&buf, []SheetSpec{{
		Headers: []string{"Name", "Age"},
		Rows:    [][]any{{"alice", float64(30)}, {"bob", float64(41)}},
	}})
	if err != nil {
		t.Fatalf("writeXLSX: %v", err)
	}
	if n != 3 {
		t.Errorf("count = %d, want 3", n)
	}
	f := roundTripXLSX(t, &buf)
	sheets := f.GetSheetList()
	if len(sheets) != 1 || sheets[0] != "Sheet1" {
		t.Fatalf("sheets = %v, want [Sheet1]", sheets)
	}
	for cell, want := range map[string]string{
		"A1": "Name", "B1": "Age",
		"A2": "alice", "A3": "bob",
	} {
		if got := mustCell(t, f, "Sheet1", cell, false); got != want {
			t.Errorf("%s = %q, want %q", cell, got, want)
		}
	}
	if got := mustCell(t, f, "Sheet1", "B2", false); got != "30" {
		t.Errorf("B2 = %q, want %q", got, "30")
	}
}

// Case 2: multiple sheets with distinct names are both written.
func TestWriteXLSXMultiSheet(t *testing.T) {
	var buf bytes.Buffer
	n, err := writeXLSX(&buf, []SheetSpec{
		{Name: "Alpha", Headers: []string{"H"}, Rows: [][]any{{"a"}}},
		{Name: "Beta", Headers: []string{"H"}, Rows: [][]any{{"b"}}},
	})
	if err != nil {
		t.Fatalf("writeXLSX: %v", err)
	}
	if n != 4 { // 2 sheets x (1 header + 1 data row)
		t.Errorf("count = %d, want 4", n)
	}
	f := roundTripXLSX(t, &buf)
	got := f.GetSheetList()
	if len(got) != 2 || got[0] != "Alpha" || got[1] != "Beta" {
		t.Fatalf("sheets = %v, want [Alpha Beta]", got)
	}
	if v := mustCell(t, f, "Alpha", "A2", false); v != "a" {
		t.Errorf("Alpha!A2 = %q, want %q", v, "a")
	}
	if v := mustCell(t, f, "Beta", "A2", false); v != "b" {
		t.Errorf("Beta!A2 = %q, want %q", v, "b")
	}
}

// Case 3: colliding sheet names get _2 suffixes.
func TestWriteXLSXSheetNameDedupe(t *testing.T) {
	var buf bytes.Buffer
	_, err := writeXLSX(&buf, []SheetSpec{
		{Name: "Data", Headers: []string{"H"}, Rows: [][]any{{1.0}}},
		{Name: "Data", Headers: []string{"H"}, Rows: [][]any{{2.0}}},
	})
	if err != nil {
		t.Fatalf("writeXLSX: %v", err)
	}
	f := roundTripXLSX(t, &buf)
	got := f.GetSheetList()
	if len(got) != 2 || got[0] != "Data" || got[1] != "Data_2" {
		t.Fatalf("sheets = %v, want [Data Data_2]", got)
	}
	if v := mustCell(t, f, "Data_2", "A2", true); v != "2" {
		t.Errorf("Data_2!A2 = %q, want %q", v, "2")
	}
}

// Case 4: string/float64/bool/nil land as such; nil is empty.
func TestWriteXLSXCellTypes(t *testing.T) {
	var buf bytes.Buffer
	_, err := writeXLSX(&buf, []SheetSpec{{
		Headers: []string{"S", "F", "B", "N"},
		Rows:    [][]any{{"hello", 3.14, true, nil}},
	}})
	if err != nil {
		t.Fatalf("writeXLSX: %v", err)
	}
	f := roundTripXLSX(t, &buf)
	if v := mustCell(t, f, "Sheet1", "A2", false); v != "hello" {
		t.Errorf("A2 = %q, want %q", v, "hello")
	}
	if v := mustCell(t, f, "Sheet1", "A2", true); v != "hello" {
		t.Errorf("A2 raw = %q, want %q", v, "hello")
	}
	if v := mustCell(t, f, "Sheet1", "B2", true); v != "3.14" {
		t.Errorf("B2 raw = %q, want %q", v, "3.14")
	}
	if v := mustCell(t, f, "Sheet1", "C2", true); v != "1" {
		t.Errorf("C2 raw = %q, want %q (true)", v, "1")
	}
	if v := mustCell(t, f, "Sheet1", "D2", false); v != "" {
		t.Errorf("D2 = %q, want empty for nil", v)
	}
}

// Case 5: HighlightRows get a yellow fill that differs from non-highlighted
// neighbor cells in the same column.
func TestWriteXLSXHighlightStyle(t *testing.T) {
	var buf bytes.Buffer
	_, err := writeXLSX(&buf, []SheetSpec{{
		Headers:       []string{"H1", "H2"},
		Rows:          [][]any{{"a", "b"}, {"c", "d"}, {"e", "f"}},
		HighlightRows: []int{1},
	}})
	if err != nil {
		t.Fatalf("writeXLSX: %v", err)
	}
	f := roundTripXLSX(t, &buf)
	// HighlightRows indexes Rows (0-based): data row 1 = sheet row 3.
	high := mustCellStyle(t, f, "Sheet1", "A3")
	plainAbove := mustCellStyle(t, f, "Sheet1", "A2")
	plainBelow := mustCellStyle(t, f, "Sheet1", "A4")
	if high == 0 {
		t.Errorf("highlighted A3 has no style applied")
	}
	if high == plainAbove || high == plainBelow {
		t.Errorf("highlighted A3 style %d matches a non-highlighted neighbor", high)
	}
	// Fill spans the used width (2 columns).
	if highB := mustCellStyle(t, f, "Sheet1", "B3"); highB != high {
		t.Errorf("B3 style %d != A3 style %d", highB, high)
	}
}

// Case 6: empty sheets slice is an error, not an empty workbook.
func TestWriteXLSXEmptySheetsError(t *testing.T) {
	var buf bytes.Buffer
	n, err := writeXLSX(&buf, nil)
	if err == nil {
		t.Fatal("writeXLSX(nil sheets): want error, got nil")
	}
	if n != 0 {
		t.Errorf("count = %d, want 0", n)
	}
	if !strings.Contains(err.Error(), "no sheets provided") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "no sheets provided")
	}
}

// Case 7: unsupported cell values produce a descriptive error naming
// row, column, and type.
func TestWriteXLSXUnsupportedCellTypeError(t *testing.T) {
	var buf bytes.Buffer
	_, err := writeXLSX(&buf, []SheetSpec{{
		Headers: []string{"H"},
		Rows:    [][]any{{struct{}{}}, {"ok"}},
	}})
	if err == nil {
		t.Fatal("writeXLSX: want error for unsupported type, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"unsupported cell type", "struct", "row 2", "col 1"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", msg, want)
		}
	}
}
