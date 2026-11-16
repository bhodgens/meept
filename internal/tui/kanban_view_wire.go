package tui

// kanban_view_wire.go holds the tolerant RPC-reply decoding for the kanban
// view (split from kanban_view.go): both the C1 Go-field spelling (the
// daemon structs carry no json tags) and the snake_case the sidecar/GUI
// lanes use, mirroring ui/flutter_ui/lib/features/kanban/kanban_models.dart.

import (
	"encoding/json"
	"strconv"
	"strings"
)

func kanbanDecodeCard(m map[string]any) kanbanCardRow {
	c := kanbanCardRow{
		ID:         kanbanFieldStr(m, "id", "ID"),
		ColumnID:   kanbanFieldStr(m, "column_id", "ColumnID"),
		Title:      kanbanFieldStr(m, "title", "Title"),
		EmployeeID: kanbanFieldStr(m, "employee_id", "EmployeeID"),
		Order:      kanbanFieldInt(m, "order", "Order"),
	}
	for _, k := range []string{"ref", "Ref"} {
		if ref, ok := m[k].(map[string]any); ok {
			c.RefKind = kanbanFieldStr(ref, "kind", "Kind")
			c.RefID = kanbanFieldStr(ref, "id", "ID")
			break
		}
	}
	c.Labels = kanbanFieldStrs(m, "labels", "Labels")
	return c
}

// kanbanDecodeList returns a list of maps from any of: a bare JSON array,
// {"<key>": [...]} (either spelling), {"items"/"Items": [...]}.
func kanbanDecodeList(raw json.RawMessage, keys ...string) []map[string]any {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var items []map[string]any
		if err := json.Unmarshal(raw, &items); err == nil {
			return items
		}
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	candidates := append(append([]string{}, keys...), "items", "Items")
	for _, k := range candidates {
		if v, ok := obj[k].([]any); ok {
			return toMapList(v)
		}
	}
	return nil
}

// kanbanDecodeRecord unwraps {"<key>": {...}} envelopes, else returns the
// object itself (mirrors the GUI's _record tolerance).
func kanbanDecodeRecord(raw json.RawMessage, keys ...string) map[string]any {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return map[string]any{}
	}
	for _, k := range keys {
		if inner, ok := obj[k].(map[string]any); ok {
			return inner
		}
	}
	return obj
}

func toMapList(v []any) []map[string]any {
	out := make([]map[string]any, 0, len(v))
	for _, e := range v {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func kanbanFieldStr(m map[string]any, snake, pascal string) string {
	if s, ok := m[snake].(string); ok {
		return s
	}
	if s, ok := m[pascal].(string); ok {
		return s
	}
	return ""
}

func kanbanFieldInt(m map[string]any, snake, pascal string) int {
	for _, k := range []string{snake, pascal} {
		switch v := m[k].(type) {
		case float64:
			return int(v)
		case json.Number:
			if n, err := v.Int64(); err == nil {
				return int(n)
			}
		case string:
			if n, err := strconv.Atoi(v); err == nil {
				return n
			}
		}
	}
	return 0
}

func kanbanFieldStrs(m map[string]any, snake, pascal string) []string {
	for _, k := range []string{snake, pascal} {
		if v, ok := m[k].([]any); ok {
			var out []string
			for _, e := range v {
				if s, ok := e.(string); ok {
					out = append(out, s)
				}
			}
			return out
		}
	}
	return nil
}

// shortKanbanID trims ids to 8 chars for the table's ref column (the full
// id stays visible in the detail view).
func shortKanbanID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
