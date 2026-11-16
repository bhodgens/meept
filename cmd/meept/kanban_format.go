package main

// Human-readable table renderers for `meept kanban` (CLI convention from
// effects.go/acp_cmd.go: text/tabwriter, lowercase headers per repo UI
// conventions). All rendering works on the decoded JSON shape rather than
// kanban's Go types: the CLI speaks RPC, so field names are the wire
// contract, and a tolerant decode never fails the command because one
// field is missing.

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// renderBoards prints the ID / NAME table for kanban.board.list.
func renderBoards(w io.Writer, result map[string]any) error {
	rows := listMaps(result["boards"])
	if rows == nil {
		rows = listMaps(result["items"])
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "no boards found")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "id\tname")
	for _, b := range rows {
		fmt.Fprintf(tw, "%s\t%s\n", getStringOr(b, "id", "-"), getStringOr(b, "name", "-"))
	}
	return tw.Flush()
}

// renderColumns prints the ID / NAME / POSITION / WIP table for
// kanban.column.list. WIP 0 means no limit (C1).
func renderColumns(w io.Writer, result map[string]any) error {
	rows := listMaps(result["columns"])
	if rows == nil {
		rows = listMaps(result["items"])
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "no columns found")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "id\tname\tposition\twip")
	for _, c := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			getStringOr(c, "id", "-"),
			getStringOr(c, "name", "-"),
			numberString(c, "position", "position"),
			wipString(c),
		)
	}
	return tw.Flush()
}

// renderCards prints the ID / TITLE / ORDER / EMPLOYEE / LABELS table for
// kanban.card.list.
func renderCards(w io.Writer, result map[string]any) error {
	rows := listMaps(result["cards"])
	if rows == nil {
		rows = listMaps(result["items"])
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "no cards found")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "id\ttitle\torder\temployee\tlabels")
	for _, c := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			getStringOr(c, "id", "-"),
			getStringOr(c, "title", "-"),
			numberString(c, "order", "Order"),
			orDash(getStringOr(c, "employee_id", "")),
			labelsString(c["labels"]),
		)
	}
	return tw.Flush()
}

// renderCardResult prints one card from kanban.card.get as key/value lines
// (C1 fields, provenance included when set).
func renderCardResult(w io.Writer, result map[string]any) error {
	card, ok := result["card"].(map[string]any)
	if !ok {
		// Tolerate a handler that returns the card at the top level.
		if _, has := result["id"].(string); has {
			card = result
		} else {
			fmt.Fprintln(w, "no card in response")
			return nil
		}
	}
	fmt.Fprintf(w, "id:          %s\n", getStringOr(card, "id", "-"))
	fmt.Fprintf(w, "title:       %s\n", getStringOr(card, "title", "-"))
	fmt.Fprintf(w, "board_id:    %s\n", getStringOr(card, "board_id", "-"))
	fmt.Fprintf(w, "column_id:   %s\n", getStringOr(card, "column_id", "-"))
	if ref, ok := card["ref"].(map[string]any); ok {
		fmt.Fprintf(w, "ref:         %s/%s\n", getStringOr(ref, "kind", "-"), getStringOr(ref, "id", "-"))
	}
	fmt.Fprintf(w, "order:       %s\n", numberString(card, "order", "Order"))
	fmt.Fprintf(w, "labels:      %s\n", labelsString(card["labels"]))
	for _, p := range []struct{ key, label string }{
		{"goal_id", "goal"}, {"employee_id", "employee"}, {"plan_id", "plan"},
		{"session_id", "session"}, {"trigger_ref", "trigger"},
	} {
		if v := getStringOr(card, p.key, ""); v != "" {
			fmt.Fprintf(w, "%-11s %s\n", p.label+":", v)
		}
	}
	return nil
}

// renderBoardView prints the composed kanban show view: columns as section
// headers with their cards under them. views is assembled by
// runKanbanShowBoard from column.list + per-column card.list results.
type boardColumnView struct {
	Column map[string]any
	Cards  []map[string]any
	Note   string
}

func renderBoardView(w io.Writer, boardID string, views []boardColumnView) {
	fmt.Fprintf(w, "board: %s\n", boardID)
	if len(views) == 0 {
		fmt.Fprintln(w, "  no columns")
		return
	}
	for _, v := range views {
		name := getStringOr(v.Column, "name", "-")
		id := getStringOr(v.Column, "id", "-")
		fmt.Fprintf(w, "\n%s (%s) — wip %s\n", name, id, wipString(v.Column))
		if v.Note != "" {
			fmt.Fprintf(w, "  ! cards unavailable: %s\n", v.Note)
			continue
		}
		if len(v.Cards) == 0 {
			fmt.Fprintln(w, "  (empty)")
			continue
		}
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "  id\ttitle\temployee\tlabels")
		for _, c := range v.Cards {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n",
				getStringOr(c, "id", "-"),
				getStringOr(c, "title", "-"),
				orDash(getStringOr(c, "employee_id", "")),
				labelsString(c["labels"]))
		}
		tw.Flush() // house pattern (plans.go renderers): void writer, flush errors unreportable
	}
}

// renderTimeline prints the append-only card event log from
// kanban.card.timeline (C3 kanban_card_events).
func renderTimeline(w io.Writer, result map[string]any) error {
	rows := listMaps(result["events"])
	if rows == nil {
		rows = listMaps(result["items"])
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "no timeline events")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "created\tkind\tactor\tbody")
	for _, e := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			orDash(getStringOr(e, "created_at", "")),
			getStringOr(e, "kind", "-"),
			orDash(getStringOr(e, "actor", "")),
			oneLine(getStringOr(e, "body", "")))
	}
	return tw.Flush()
}

// renderReconcile prints the drift-pass summary from kanban.reconcile.now
// (ReconcileStats: created/moved/fixed/deleted). A handler may also answer
// with only an "ok"/"total" shape; missing counters render as 0.
func renderReconcile(w io.Writer, result map[string]any) error {
	stats := result
	if inner, ok := result["stats"].(map[string]any); ok {
		stats = inner
	}
	created := numField(stats, "created", "Created")
	moved := numField(stats, "moved", "Moved")
	fixed := numField(stats, "fixed", "Fixed")
	deleted := numField(stats, "deleted", "Deleted")
	fmt.Fprintf(w, "reconcile pass: %d created, %d moved, %d fixed, %d deleted\n",
		created, moved, fixed, deleted)
	if note := getStringOr(result, "note", ""); note != "" {
		fmt.Fprintf(w, "note: %s\n", note)
	}
	return nil
}

// renderCreated returns a renderer for a create mutation: it echoes the
// subject and the id the daemon assigned (accepting "id" at the top level
// or nested under the entity key).
func renderCreated(subject string) func(io.Writer, map[string]any) error {
	return func(w io.Writer, result map[string]any) error {
		id := getStringOr(result, "id", "")
		if id == "" {
			for _, key := range []string{"board", "card", "column"} {
				if inner, ok := result[key].(map[string]any); ok {
					if id = getStringOr(inner, "id", ""); id != "" {
						break
					}
				}
			}
		}
		if id == "" {
			fmt.Fprintf(w, "created %s\n", subject)
		} else {
			fmt.Fprintf(w, "created %s: %s\n", subject, id)
		}
		return nil
	}
}

// renderMutation returns a renderer for a mutation whose success matters
// more than its payload: echo the ok line, surface a wip note, and show a
// lease_blocked flag when the daemon stamped one.
func renderMutation(okLine string) func(io.Writer, map[string]any) error {
	return func(w io.Writer, result map[string]any) error {
		fmt.Fprintf(w, "%s\n", okLine)
		if blocked, ok := result["lease_blocked"].(bool); ok && blocked {
			fmt.Fprintln(w, "  note: the source work item still holds a lease — the projection may re-move this card")
		}
		if note := getStringOr(result, "note", ""); note != "" {
			fmt.Fprintf(w, "  note: %s\n", note)
		}
		return nil
	}
}

// --- small field helpers -------------------------------------------------

func wipString(col map[string]any) string {
	n := numField(col, "wip", "WIP")
	if n == 0 {
		return "no limit"
	}
	return fmt.Sprintf("%d", n)
}

// labelsString renders a []any of label names ("a, b"); empty/missing is "-".
func labelsString(v any) string {
	items, ok := v.([]any)
	if !ok || len(items) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok && s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}

// numberString formats a numeric field for a table cell; the fallback key
// covers a Go-side encoder that marshals the exported field name.
func numberString(m map[string]any, key, fallback string) string {
	return fmt.Sprintf("%d", numField(m, key, fallback))
}

func numField(m map[string]any, key, fallback string) int64 {
	if v, ok := m[key]; ok {
		if n, ok := jsonNumber(v); ok {
			return n
		}
	}
	if fallback != "" {
		if v, ok := m[fallback]; ok {
			if n, ok := jsonNumber(v); ok {
				return n
			}
		}
	}
	return 0
}

func jsonNumber(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// oneLine collapses embedded newlines so a multi-line comment body stays a
// single table row.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
