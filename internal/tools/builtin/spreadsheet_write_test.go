package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	excelize "github.com/xuri/excelize/v2"

	"github.com/caimlas/meept/internal/tools"
)

// newSpreadsheetTestCtx returns a context with the session working dir set to
// a fresh temp dir. spreadsheet_write refuses to run without a session
// working directory (Contract C fence), so every test sets one explicitly.
func newSpreadsheetTestCtx(t *testing.T) (context.Context, string) {
	t.Helper()
	dir := t.TempDir()
	return tools.ContextWithWorkingDir(context.Background(), dir), dir
}

// openXLSXFile re-opens a written xlsx file for inspection.
func openXLSXFile(path string) (*excelize.File, error) {
	f, err := excelize.OpenReader(strings.NewReader(mustReadFile(path)))
	return f, err
}

// mustReadFile reads a test output file, failing the test on error.
func mustReadFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// runSpreadsheetWrite executes the tool and unwraps the Contract C result map.
func runSpreadsheetWrite(t *testing.T, ctx context.Context, args map[string]any) (map[string]any, error) {
	t.Helper()
	res, err := NewSpreadsheetWriteTool().Execute(ctx, args)
	if err != nil {
		return nil, err
	}
	tr, ok := res.(tools.ToolResult)
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	m, ok := tr.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result payload type %T", tr.Result)
	}
	return m, nil
}

// Case 1: path out.csv + rows → file exists, content matches writeCSV
// semantics (header row first), result map has format/rows_written/bytes.
func TestSpreadsheetWriteCSVOut(t *testing.T) {
	ctx, dir := newSpreadsheetTestCtx(t)
	m, err := runSpreadsheetWrite(t, ctx, map[string]any{
		"path": "out.csv",
		"rows": []any{
			[]any{"city", "rank"},
			[]any{"Austin", 1.0},
			[]any{"boole", true},
			[]any{"empty", nil},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := m["format"]; got != "csv" {
		t.Errorf("format = %v, want csv", got)
	}
	if got := m["rows_written"]; got != 4 { // header + 3 data rows
		t.Errorf("rows_written = %v, want 4", got)
	}
	if b, ok := m["bytes"].(int64); !ok || b <= 0 {
		t.Errorf("bytes = %v (%T), want int64 > 0", m["bytes"], m["bytes"])
	}
	wantPath := filepath.Join(dir, "out.csv")
	if got := m["path"]; got != wantPath {
		t.Errorf("path = %v, want %v", got, wantPath)
	}
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	want := "city,rank\nAustin,1\nboole,true\nempty,\n"
	if string(data) != want {
		t.Errorf("content = %q, want %q", string(data), want)
	}
}

// Case 2: path out.xlsx + sheets → file exists; re-open with excelize and
// spot-check A1 plus one highlighted row style.
func TestSpreadsheetWriteXLSXOut(t *testing.T) {
	ctx, dir := newSpreadsheetTestCtx(t)
	m, err := runSpreadsheetWrite(t, ctx, map[string]any{
		"path": "out.xlsx",
		"sheets": []any{
			map[string]any{
				"name":    "Audit",
				"headers": []any{"Name", "Score"},
				"rows": []any{
					[]any{"alice", 30.0},
					[]any{"bob", 41.0},
					[]any{"carol", 52.0},
				},
				"highlight_rows": []any{1.0}, // 0-based data row 1 = sheet row 3
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := m["format"]; got != "xlsx" {
		t.Errorf("format = %v, want xlsx", got)
	}
	if got := m["rows_written"]; got != 4 { // 1 header + 3 data rows
		t.Errorf("rows_written = %v, want 4", got)
	}

	f, err := openXLSXFile(filepath.Join(dir, "out.xlsx"))
	if err != nil {
		t.Fatalf("re-open xlsx: %v", err)
	}
	defer f.Close()

	if got := mustCell(t, f, "Audit", "A1", false); got != "Name" {
		t.Errorf("A1 = %q, want %q", got, "Name")
	}
	high := mustCellStyle(t, f, "Audit", "A3")  // highlighted data row
	plain := mustCellStyle(t, f, "Audit", "A2") // non-highlighted neighbor
	if high == 0 {
		t.Errorf("highlighted A3 has no style applied")
	}
	if high == plain {
		t.Errorf("highlighted A3 style %d matches non-highlighted A2", high)
	}
}

// Case 3: explicit format "csv" with a .txt path → CSV content, format csv.
func TestSpreadsheetWriteExplicitFormatCSVWithTxtPath(t *testing.T) {
	ctx, dir := newSpreadsheetTestCtx(t)
	m, err := runSpreadsheetWrite(t, ctx, map[string]any{
		"path":   "out.txt",
		"format": "csv",
		"rows":   []any{[]any{"a", "b"}, []any{"1", "2"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := m["format"]; got != "csv" {
		t.Errorf("format = %v, want csv", got)
	}
	data, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(data) != "a,b\n1,2\n" {
		t.Errorf("content = %q, want %q", string(data), "a,b\n1,2\n")
	}
}

// Case 4: unknown extension with no explicit format → descriptive error.
func TestSpreadsheetWriteUnknownExtensionError(t *testing.T) {
	ctx, _ := newSpreadsheetTestCtx(t)
	_, err := runSpreadsheetWrite(t, ctx, map[string]any{
		"path": "out.tsv",
		"rows": []any{[]any{"a"}},
	})
	if err == nil {
		t.Fatal("expected error for unknown extension, got nil")
	}
	msg := strings.ToLower(err.Error())
	for _, want := range []string{".tsv", "csv", "xlsx", "format"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}

// Case 5: xlsx format without sheets → error listing the required shape.
func TestSpreadsheetWriteXLSXWithoutSheetsError(t *testing.T) {
	ctx, _ := newSpreadsheetTestCtx(t)
	_, err := runSpreadsheetWrite(t, ctx, map[string]any{
		"path": "out.xlsx",
	})
	if err == nil {
		t.Fatal("expected error for xlsx without sheets, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"sheets", "name", "headers", "rows", "highlight_rows"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q (shape not fully described)", msg, want)
		}
	}
}

// Extra (dispatch brief req 3): explicit csv format without rows → error.
func TestSpreadsheetWriteCSVWithoutRowsError(t *testing.T) {
	ctx, _ := newSpreadsheetTestCtx(t)
	_, err := runSpreadsheetWrite(t, ctx, map[string]any{
		"path":   "out.csv",
		"format": "csv",
	})
	if err == nil {
		t.Fatal("expected error for csv without rows, got nil")
	}
	if !strings.Contains(err.Error(), "rows") {
		t.Errorf("error %q missing %q", err.Error(), "rows")
	}
}

// Case 6: path in a nonexistent subdir → parent dirs auto-created.
func TestSpreadsheetWriteNestedSubdirAutoCreate(t *testing.T) {
	ctx, dir := newSpreadsheetTestCtx(t)
	_, err := runSpreadsheetWrite(t, ctx, map[string]any{
		"path": "reports/2026/q3/out.csv",
		"rows": []any{[]any{"k"}, []any{"v"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "reports", "2026", "q3", "out.csv"))
	if err != nil {
		t.Fatalf("nested output missing: %v", err)
	}
	if string(data) != "k\nv\n" {
		t.Errorf("content = %q, want %q", string(data), "k\nv\n")
	}
}

// Case 7: fence — a path resolving outside the session working dir is
// refused with a clear error (relative escape and absolute escape), and a
// missing session working dir is a hard refusal (never a process-cwd
// fallback).
func TestSpreadsheetWriteFenceRefusal(t *testing.T) {
	t.Run("relative escape", func(t *testing.T) {
		ctx, _ := newSpreadsheetTestCtx(t)
		_, err := runSpreadsheetWrite(t, ctx, map[string]any{
			"path": "../escape.csv",
			"rows": []any{[]any{"a"}},
		})
		if err == nil {
			t.Fatal("expected fence refusal, got nil")
		}
		if !strings.Contains(err.Error(), "outside the session working directory") {
			t.Errorf("error = %q, want fence message", err.Error())
		}
	})

	t.Run("absolute outside sandbox", func(t *testing.T) {
		ctx, _ := newSpreadsheetTestCtx(t)
		outside := filepath.Join(t.TempDir(), "other.csv") // different temp dir
		_, err := runSpreadsheetWrite(t, ctx, map[string]any{
			"path":   outside,
			"format": "csv",
			"rows":   []any{[]any{"a"}},
		})
		if err == nil {
			t.Fatal("expected fence refusal, got nil")
		}
		if !strings.Contains(err.Error(), "outside the session working directory") {
			t.Errorf("error = %q, want fence message", err.Error())
		}
	})

	t.Run("no session working dir", func(t *testing.T) {
		_, err := runSpreadsheetWrite(t, context.Background(), map[string]any{
			"path":   "out.csv",
			"format": "csv",
			"rows":   []any{[]any{"a"}},
		})
		if err == nil {
			t.Fatal("expected refusal without session working dir, got nil")
		}
		if !strings.Contains(err.Error(), "session working directory") {
			t.Errorf("error = %q, want session-dir message", err.Error())
		}
	})
}

// In-sandbox absolute paths (not just relative ones) must be allowed.
func TestSpreadsheetWriteAbsoluteInSandboxAllowed(t *testing.T) {
	ctx, dir := newSpreadsheetTestCtx(t)
	abs := filepath.Join(dir, "sub", "abs.csv")
	if _, err := runSpreadsheetWrite(t, ctx, map[string]any{
		"path":   abs,
		"format": "csv",
		"rows":   []any{[]any{"a"}},
	}); err != nil {
		t.Fatalf("in-sandbox absolute path refused: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("output missing: %v", err)
	}
}

// Bad sheet payloads fail with descriptive errors rather than panics
// (two-value assertion coverage).
func TestSpreadsheetWriteBadSheetPayloads(t *testing.T) {
	ctx, _ := newSpreadsheetTestCtx(t)
	for name, sheets := range map[string]any{
		"non-array sheets":  "nope",
		"non-object sheet":  []any{"nope"},
		"bad highlight row": []any{map[string]any{"rows": []any{}, "highlight_rows": []any{"x"}}},
		"non-array rows":    []any{map[string]any{"rows": "nope"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := runSpreadsheetWrite(t, ctx, map[string]any{
				"path":   "out.xlsx",
				"format": "xlsx",
				"sheets": sheets,
			})
			if err == nil {
				t.Fatalf("expected error for %s, got nil", name)
			}
		})
	}
}
