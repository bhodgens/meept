package builtin

// ConfirmationResponse builds the phase-1 response map returned by destructive
// tools when their `confirmed` argument is false or absent. The UI inspects
// requires_confirmation, prompts the user, and re-invokes the tool with
// confirmed=true on approval.
//
//   - action         tool name (e.g., "mark_superseded")
//   - reversibleFlag true when the operation can be undone (adds "reversible": true)
//   - summary        one-line human-readable description of what will happen
//   - details        structured preview (old_preview, new_preview, affected_edges, ...)
func ConfirmationResponse(action string, reversibleFlag bool, summary string, details map[string]any) map[string]any {
	out := map[string]any{
		"requires_confirmation": true,
		"action":                action,
		"reversible":            reversibleFlag,
		"summary":               summary,
		"confirm_arg":           "confirmed",
	}
	if len(details) > 0 {
		out["details"] = details
	}
	return out
}

// IsConfirmationRequest reports whether a tool result map is asking the
// caller for explicit confirmation. Only maps whose requires_confirmation
// field is exactly bool true are treated as confirmation requests.
func IsConfirmationRequest(result map[string]any) bool {
	if result == nil {
		return false
	}
	flag, ok := result["requires_confirmation"].(bool)
	return ok && flag
}

// DeclineResponse builds the response map returned when the user declines a
// confirmation prompt. Mirrors the action/summary fields from the original
// phase-1 response and adds a user_note slot the caller can fill in.
func DeclineResponse(orig map[string]any) map[string]any {
	action, _ := orig["action"].(string)
	summary, _ := orig["summary"].(string)
	return map[string]any{
		"declined":  true,
		"action":    action,
		"summary":   summary,
		"user_note": "",
	}
}
