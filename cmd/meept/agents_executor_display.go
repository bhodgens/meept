package main

import (
	"fmt"
	"strings"
)

// renderExecutorSection renders the `executor:` section of
// `meept agents show` (acp-employees dim 3A), following the Verification:
// section pattern. All values are lowercase per the repo UI convention.
// The section renders only when the daemon returned a non-empty executor.
//
// Shape:
//
//	executor: acp
//	  acp agent: hermes
//	  persona: bob
//	  permission mode: human
//	  command_args: -p borg
//
// An empty persona renders "-"; an empty permission mode renders "global"
// (the [acp] config fallback the daemon applies); a missing command_args
// renders "-".
func renderExecutorSection(m map[string]any) string {
	executor := strings.ToLower(strings.TrimSpace(getStringOr(m, "executor", "")))
	if executor == "" {
		return ""
	}
	acpBlock, _ := m["acp"].(map[string]any)

	var b strings.Builder
	fmt.Fprintf(&b, "\nexecutor: %s\n", executor)
	fmt.Fprintf(&b, "  acp agent: %s\n", executorSectionValue(acpBlock, "agent", "-"))
	fmt.Fprintf(&b, "  persona: %s\n", executorSectionValue(acpBlock, "persona", "-"))
	fmt.Fprintf(&b, "  permission mode: %s\n", executorSectionValue(acpBlock, "permission_mode", "global"))
	fmt.Fprintf(&b, "  command_args: %s\n", executorCommandArgs(acpBlock))
	return b.String()
}

// employeeExecutorLine renders the employee detail-view executor line
// for `meept agents show` (acp-employees phase 3 display parity). The
// employee's `executor_agent` names an acp-backed registry agent whose
// loop carries the employee's EXECUTE turns; the id is lowercased per
// the repo UI convention. Returns "" when the daemon omitted the field
// (a local-executor employee renders no line at all).
//
// This is the EMPLOYEE surface — agent DEFINITIONS use
// renderExecutorSection above, which renders the full executor block.
func employeeExecutorLine(m map[string]any) string {
	agent := strings.ToLower(strings.TrimSpace(getStringOr(m, "executor_agent", "")))
	if agent == "" {
		return ""
	}
	return "executor: " + agent + " (acp)"
}

// executorSectionValue reads a string field from the acp block, falling
// back to def for a missing block or an empty value.
func executorSectionValue(acpBlock map[string]any, key, def string) string {
	if acpBlock == nil {
		return def
	}
	v := strings.ToLower(strings.TrimSpace(getStringOr(acpBlock, key, "")))
	if v == "" {
		return def
	}
	return v
}

// executorCommandArgs joins the acp block's command_args for display;
// "-" when absent or empty.
func executorCommandArgs(acpBlock map[string]any) string {
	if acpBlock == nil {
		return "-"
	}
	raw, ok := acpBlock["command_args"].([]any)
	if !ok || len(raw) == 0 {
		return "-"
	}
	args := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			args = append(args, s)
		}
	}
	if len(args) == 0 {
		return "-"
	}
	return strings.Join(args, " ")
}
