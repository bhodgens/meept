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

// CellPaddingWidth is the horizontal space bubbles/table's default Cell and
// Header styles add around every column value (Padding(0, 1)).
const CellPaddingWidth = 2

// ContentBudget returns how many columns of cell TEXT fit inside a viewport of
// viewportWidth holding count columns: the viewport minus the per-cell padding
// bubbles/table adds.
//
// This is the number a column layout must fit. bubbles/table renders each cell
// at style.Width(col.Width).MaxWidth(col.Width) and then pads it, so a column
// set whose widths sum past this budget renders a box WIDER than its viewport,
// overflows its container, and — because every cell is MaxWidth-truncated —
// silently drops the tail of the right-most cells.
func ContentBudget(viewportWidth, count int) int {
	return viewportWidth - CellPaddingWidth*count
}

// MinColumnWidth is the narrowest column FitWidths will hand out. One cell of
// text is still a cell; a zero-width column is skipped entirely by
// bubbles/table's renderer (it `continue`s on col.Width <= 0), which shifts
// every later cell one column left.
const MinColumnWidth = 1

// FitWidths distributes budget columns across len(weights) columns in
// proportion to the weights, guaranteeing the result never exceeds budget.
//
// The guarantee is the point. Hand-computed percentage widths (e.g. "name gets
// 22% of width-10, the rest are fixed") drift out of the container as soon as
// the container narrows — a fixed-column floor plus a floor on the elastic
// column can sum well past the viewport, and the overflow is invisible until
// you measure the render.
//
// Distribution is largest-remainder, so the widths sum to EXACTLY budget when
// budget allows it (no column silently loses a column of space) and to at most
// budget when it does not. Every column is at least MinColumnWidth.
func FitWidths(budget int, weights ...int) []int {
	n := len(weights)
	if n == 0 {
		return nil
	}

	// Not enough room for one cell per column: share what there is.
	if budget < n*MinColumnWidth {
		widths := make([]int, n)
		for i := range widths {
			widths[i] = MinColumnWidth
		}
		return widths
	}

	total := 0
	for _, w := range weights {
		if w > 0 {
			total += w
		}
	}
	if total <= 0 {
		// No usable weights: split evenly.
		for i := range weights {
			weights[i] = 1
		}
		total = n
	}

	widths := make([]int, n)
	assigned := 0
	remainders := make([]int, n)
	for i, w := range weights {
		if w <= 0 {
			// A zero weight still needs a visible cell.
			widths[i] = MinColumnWidth
			assigned += MinColumnWidth
			continue
		}
		exact := budget * w
		widths[i] = exact / total
		remainders[i] = exact % total
		assigned += widths[i]
	}

	// Largest-remainder: hand the leftover columns to the columns with the
	// biggest fractional part, so the sum lands exactly on budget.
	left := budget - assigned
	for left > 0 {
		best, bestRem := -1, -1
		for i, r := range remainders {
			if widths[i] < MinColumnWidth {
				continue
			}
			if r > bestRem {
				best, bestRem = i, r
			}
		}
		if best < 0 {
			break
		}
		widths[best]++
		remainders[best] = -1 // one round each
		left--
	}
	// Any column still at the floor could not take a share: give the remainder
	// to the widest columns so the budget is not left on the floor.
	for left > 0 {
		widest := 0
		for i, w := range widths {
			if w > widths[widest] {
				widest = i
			}
		}
		widths[widest]++
		left--
	}

	for i, w := range widths {
		if w < MinColumnWidth {
			widths[i] = MinColumnWidth
		}
	}
	return widths
}

// FitWidthsMin is FitWidths with per-column minimums: every column gets at
// least min[i] and the SURPLUS above those minimums is split by weight.
//
// A pure proportional split starves the columns whose content has a hard
// width floor (a progress bar plus its " n/m" label, an icon plus a state
// word) as soon as the container narrows, and bubbles/table silently
// MaxWidth-truncates the cell, so the render loses content that the producer
// always emits. With minimums, the fixed-content columns keep their width and
// the elastic ones (name) give up the slack.
//
// If the budget cannot cover the minimums, every column still gets at least
// MinColumnWidth — the caller is over-subscribed and the render will truncate,
// but it will not render past the viewport.
func FitWidthsMin(budget int, mins, weights []int) []int {
	n := len(mins)
	if n == 0 {
		return nil
	}
	if len(weights) != n {
		weights = make([]int, n)
		for i := range weights {
			weights[i] = 1
		}
	}

	floors := make([]int, n)
	assigned := 0
	for i := range floors {
		floors[i] = max(mins[i], MinColumnWidth)
		assigned += floors[i]
	}

	// Not enough room for the floors: fall back to an even split so the sum
	// never exceeds the budget.
	if assigned >= budget {
		return FitWidths(budget, weights...)
	}

	surplus := budget - assigned
	extra := FitWidths(surplus, weights...)
	widths := make([]int, n)
	for i := range widths {
		widths[i] = floors[i] + extra[i]
	}
	return widths
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
		fixed := make(table.Row, columns)
		copy(fixed, row)
		normalized[i] = fixed
	}
	t.SetRows(normalized)
}
