// Package tableutil holds the shared sizing and row-normalization helpers for
// the bubbles/table widgets used by the TUI.
//
// bubbles/table owns a viewport. The viewport width defaults to 0 and a
// viewport of width 0 renders an empty string, so a table that only ever gets
// SetHeight draws its header and its border but no rows — even when rows are
// populated and cursor navigation works. Every table must be sized on both
// axes from the space its container leaves inside its border frame.
package tableutil

import "charm.land/bubbles/v2/table"

// BorderFrameWidth is the horizontal space a one-cell rounded border adds
// around a table (1 column left + 1 column right).
const BorderFrameWidth = 2

// Size sets the table viewport to the inner area of a rounded border whose
// outer width is outerWidth, and to height rows. The table spans its own header
// line, so the viewport keeps height-1 rows; the floor keeps one row visible.
func Size(t *table.Model, outerWidth, height int) {
	t.SetWidth(max(outerWidth-BorderFrameWidth, 1))
	t.SetHeight(max(height, 2))
}

// SetRows replaces the table rows, normalizing every row to the table's
// current column count.
//
// bubbles/table.renderRow indexes the column slice with the row-cell index, so
// a row with MORE cells than columns panics with "index out of range" and a row
// with FEWER cells silently drops its trailing columns. Rows and columns live
// in separate model state and are updated by different events — switching view
// mode installs a new column set while an in-flight fetch still delivers rows
// for the previous mode — so the invariant is enforced here, at the single
// write boundary, instead of at every call site.
func SetRows(t *table.Model, rows []table.Row) {
	columns := len(t.Columns())
	normalized := make([]table.Row, len(rows))
	for i, row := range rows {
		if len(row) == columns {
			normalized[i] = row
			continue
		}
		// Extra cells are dropped, missing cells render empty.
		fixed := make(table.Row, columns)
		copy(fixed, row)
		normalized[i] = fixed
	}
	t.SetRows(normalized)
}
