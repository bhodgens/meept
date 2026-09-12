package tableutil

import (
	"testing"

	"charm.land/bubbles/v2/table"
)

// SetRows must absorb any cell count: bubbles/table panics when a row is longer
// than the column list and silently drops trailing cells when it is shorter.
func TestSetRowsNormalizesToColumnCount(t *testing.T) {
	m := table.New(table.WithColumns([]table.Column{
		{Title: "a", Width: 5},
		{Title: "b", Width: 5},
		{Title: "c", Width: 5},
		{Title: "d", Width: 5},
	}))

	SetRows(&m, []table.Row{
		{"one", "two", "three", "four"},
		{"short"},
		{"a", "b", "c", "d", "e", "f", "g", "h", "i"}, // the 7-task-rows-in-4-columns case
	})

	rows := m.Rows()
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	for i, row := range rows {
		if len(row) != 4 {
			t.Errorf("row %d has %d cells, want 4", i, len(row))
		}
	}
	if rows[1][0] != "short" || rows[1][3] != "" {
		t.Errorf("short row = %v, want [short  <empty> <empty>]", rows[1])
	}
	if rows[2][3] != "d" {
		t.Errorf("long row = %v, want the first 4 cells kept", rows[2])
	}
}

func TestSetRowsEmptyAndNil(t *testing.T) {
	m := table.New(table.WithColumns([]table.Column{{Title: "a", Width: 5}}))
	SetRows(&m, nil)
	if got := len(m.Rows()); got != 0 {
		t.Errorf("rows after nil = %d, want 0", got)
	}
	SetRows(&m, []table.Row{})
	if got := len(m.Rows()); got != 0 {
		t.Errorf("rows after empty = %d, want 0", got)
	}
}

func TestSizeSetsBothViewportAxes(t *testing.T) {
	m := table.New(table.WithColumns([]table.Column{{Title: "a", Width: 5}}))
	// Both axes must be set: the table viewport starts at width 0, and a
	// zero-width viewport renders no rows (header only, blank body).
	Size(&m, 80, 12)

	if got := m.Width(); got != 78 {
		t.Errorf("table width = %d, want 78 (outer width minus border frame)", got)
	}
	if got := m.Height(); got != 11 {
		t.Errorf("table height = %d, want 11 (requested 12 minus the header line)", got)
	}
}

func TestSizeClampsDegenerateInput(t *testing.T) {
	m := table.New(table.WithColumns([]table.Column{{Title: "a", Width: 5}}))
	Size(&m, 1, 0)

	if got := m.Width(); got < 1 {
		t.Errorf("table width = %d, want >= 1", got)
	}
	if got := m.Height(); got < 1 {
		t.Errorf("table height = %d, want >= 1", got)
	}
}
