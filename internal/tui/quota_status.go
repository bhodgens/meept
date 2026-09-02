// Package tui — quota_status.go renders quota_wait and blocked agent status
// in the agents list/detail views (quota-reset-resilience leaf 08).
//
// Wire: bus topic "agent.quota_wait" → WS type "agent_progress" (leaf 07)
// → EventStreamDataMsg (app.go) → quotaStateMsg (agents_panel.go).
//
// Parity: FormatQuotaCountdown is the exact countdown format leaf 09 mirrors
// in Flutter (ui/flutter_ui/lib/features/agents/quota_status.dart) — do not
// change the strings without updating the GUI side.
package tui

import (
	"fmt"
	"time"
)

// Agent-level quota state strings (mirror agent.AgentState wire values from
// internal/agent/agent_state.go).
const (
	AgentStateQuotaWait = "quota_wait"
	AgentStateBlocked   = "blocked"
)

// QuotaCountdownText returns the countdown hint for an unblock time. The
// prefix ("quota resets in") is shared with the Flutter badge so both
// surfaces read identically.
func QuotaCountdownText(unblockAt time.Time) string {
	return QuotaCountdownTextAt(time.Now(), unblockAt)
}

// QuotaCountdownTextAt is the pure form of QuotaCountdownText: the caller
// supplies the reference time so countdown math is deterministic (tests),
// while production callers use the Now wrapper.
func QuotaCountdownTextAt(now, unblockAt time.Time) string {
	d := unblockAt.Sub(now)
	if d <= 0 {
		return "resets soon"
	}
	return "quota resets in " + FormatDuration(d)
}

// FormatQuotaCountdown returns just the formatted remaining time from now
// until unblockAt ("3h 12m", "45m", or "soon" when past due). Kept exported
// for direct parity testing against the Flutter formatter.
func FormatQuotaCountdown(unblockAt time.Time) string {
	return FormatQuotaCountdownAt(time.Now(), unblockAt)
}

// FormatQuotaCountdownAt is the pure form of FormatQuotaCountdown.
func FormatQuotaCountdownAt(now, unblockAt time.Time) string {
	d := unblockAt.Sub(now)
	if d <= 0 {
		return "soon"
	}
	return FormatDuration(d)
}

// RenderAgentStatus returns the status cell text for an agent row in the
// agents list (pure function so tests exercise it without a live panel).
// Agents never quota-hit render byte-identically to before: the switch has
// no case for "running"/"paused"/"error" and falls through to the plain
// status string.
func RenderAgentStatus(status string) string {
	switch status {
	case AgentStateQuotaWait:
		return "quota wait"
	case AgentStateBlocked:
		return "blocked · action required"
	default:
		return status
	}
}

// QuotaWaitLabel renders the agents-tab wait label for a parked turn
// (tree 03 leaf 04 Task 3, TUI + Flutter parity — the Flutter side mirrors
// this byte-for-byte in ui/flutter_ui/lib/features/agents/quota_status.dart;
// change both together):
//
//	quota class (or absent — legacy events): "quota_wait · reset HH:MM"
//	throttle class:                          "quota_wait · throttle retry HH:MM"
//
// HH:MM is the ABSOLUTE time of unblockAt (the daemon-provided resume
// instant), never relative countdown math: the GUI runs on web where client
// wall clocks cannot be trusted (leaf Notes). Lowercase per repo UI rule.
func QuotaWaitLabel(class string, unblockAt time.Time) string {
	hhmm := unblockAt.Format("15:04")
	if class == "throttle" {
		return "quota_wait · throttle retry " + hhmm
	}
	// "quota" and legacy/absent class both name the reset wait.
	return "quota_wait · reset " + hhmm
}

// RenderQuotaDetailLines returns the primary/active model lines for the
// agent detail view. Returns nil unless fallbackModel is set; when set the
// block is two lines:
//
//	primary: <model> (blocked until <time>)
//	active: <fallback model>
//
// (unblockAt zero renders "unknown").
func RenderQuotaDetailLines(primaryModel, fallbackModel string, unblockAt time.Time) []string {
	if fallbackModel == "" {
		return nil
	}
	until := "unknown"
	if !unblockAt.IsZero() {
		until = unblockAt.Format("15:04 MST")
	}
	return []string{
		fmt.Sprintf("primary: %s (blocked until %s)", primaryModel, until),
		fmt.Sprintf("active: %s", fallbackModel),
	}
}
