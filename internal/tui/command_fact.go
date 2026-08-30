package tui

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (h *CommandHandler) executeFacts(args []string) *CommandResult {
	if h.rpc == nil || !h.rpc.IsConnected() {
		return &CommandResult{Output: ErrNotConnected, IsError: true}
	}
	query := ""
	kind := ""
	if len(args) > 0 {
		lower := strings.ToLower(args[0])
		switch lower {
		case "preference", "restriction", "account", "temporal":
			kind = lower
			if len(args) > 1 {
				query = strings.Join(args[1:], " ")
			}
		default:
			query = strings.Join(args, " ")
		}
	}
	params := map[string]any{}
	if kind != "" {
		params["kind"] = kind
	}
	if query != "" {
		params["query"] = query
	}
	result, err := h.rpc.Call("memory.fact.list", params)
	if err != nil {
		return &CommandResult{Output: fmt.Sprintf("memory.fact.list unavailable: %v", err), IsError: false}
	}
	var payload struct {
		Facts []map[string]any `json:"facts"`
		Error string           `json:"error,omitempty"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return &CommandResult{Output: fmt.Sprintf("parse facts: %v", err), IsError: true}
	}
	if payload.Error != "" {
		return &CommandResult{Output: payload.Error, IsError: true}
	}
	if len(payload.Facts) == 0 {
		return &CommandResult{Output: "no facts"}
	}
	var sb strings.Builder
	sb.WriteString("active facts:\n\n")
	for _, f := range payload.Facts {
		key := strMap(f, "key")
		value := strMap(f, "value")
		fkind := strMap(f, "kind")
		updated := strMap(f, "updated_at")
		sb.WriteString(fmt.Sprintf("  [%s] %s: %s  %s\n", fkind, key, value, updated))
	}
	return &CommandResult{Output: sb.String()}
}
