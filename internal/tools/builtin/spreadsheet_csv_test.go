package builtin

import (
	"bytes"
	"testing"
)

// TestWriteCSV covers the 7 cases specified in
// docs/plans/20260905-research-audit-tools/03-spreadsheet/01-csv-writer.md.
// All cases run against bytes.Buffer only — no disk, no network.
func TestWriteCSV(t *testing.T) {
	tests := []struct {
		name    string
		hdr     bool
		rows    [][]any
		want    string
		wantN   int
		wantErr string
	}{
		{
			name: "hdr true: header line first, count includes header",
			hdr:  true,
			rows: [][]any{
				{"name", "qty"},
				{"widget", float64(2)},
				{"gadget", float64(5)},
			},
			want:  "name,qty\nwidget,2\ngadget,5\n",
			wantN: 3,
		},
		{
			name: "hdr false: no header, count excludes it",
			hdr:  false,
			rows: [][]any{
				{"widget", float64(2)},
				{"gadget", float64(5)},
			},
			want:  "widget,2\ngadget,5\n",
			wantN: 2,
		},
		{
			name: "cell types: string, float64, bool false, nil",
			hdr:  false,
			rows: [][]any{
				{"s", float64(42), false, nil},
			},
			want:  "s,42,false,\n",
			wantN: 1,
		},
		{
			name: "float formatting: never scientific notation",
			hdr:  false,
			rows: [][]any{
				{3.14},
				{1e21},
				{-0.5},
			},
			want:  "3.14\n1000000000000000000000\n-0.5\n",
			wantN: 3,
		},
		{
			name: "quoting: comma, quote, and newline cells",
			hdr:  false,
			rows: [][]any{
				{"a,b", "he said \"hi\"", "line1\nline2"},
			},
			want:  "\"a,b\",\"he said \"\"hi\"\"\",\"line1\nline2\"\n",
			wantN: 1,
		},
		{
			name: "empty rows: header only, count 1",
			hdr:  true,
			rows: [][]any{
				{"h1", "h2"},
			},
			want:  "h1,h2\n",
			wantN: 1,
		},
		{
			name:    "unsupported cell type: error names row, col, and type",
			hdr:     false,
			rows:    [][]any{{"ok", 7}},
			wantErr: "row 0 col 1: unsupported cell type int (want string, float64, bool, or nil)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			n, err := writeCSV(&buf, tt.hdr, tt.rows)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("writeCSV() error = nil, want %q", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("writeCSV() error = %q, want %q", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("writeCSV() unexpected error: %v", err)
			}
			if got := buf.String(); got != tt.want {
				t.Fatalf("writeCSV() output =\n%q\nwant\n%q", got, tt.want)
			}
			if n != tt.wantN {
				t.Fatalf("writeCSV() count = %d, want %d", n, tt.wantN)
			}
		})
	}
}

// TestSheetSpecShape pins the frozen seam struct so the xlsx leaf can rely
// on these exact fields being defined in spreadsheet_csv.go.
func TestSheetSpecShape(t *testing.T) {
	spec := SheetSpec{
		Name:          "audit",
		Headers:       []string{"a", "b"},
		Rows:          [][]any{{"x", float64(1)}},
		HighlightRows: []int{0},
	}
	if spec.Name != "audit" || len(spec.Headers) != 2 || len(spec.Rows) != 1 || len(spec.HighlightRows) != 1 {
		t.Fatalf("SheetSpec fields did not round-trip: %+v", spec)
	}
}
