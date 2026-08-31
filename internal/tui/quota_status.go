// Package tui — quota_status.go renders quota_wait and blocked agent
// status in the agents table and bus-event wiring for quota state transitions.
//
// Wire: topic "agent.quota_wait" (leaf 05 payload, see app.go EventStreamDataMsg)
// Parity: FormatQuotaCountdown is the exact countdown format leaf 09
// mirrors in Flutter — do not change the strings without updating the GUI.
package tui

import (
	"time"
)

const (
	AgentStateQuotaWait = "quota_wait"
	AgentStateBlocked   = "blocked"
)

// FormatQuotaCountdown returns a human-readable countdown from now until
// unblock_at. When unblock_at is zero or in the past it returns "resuming…".
// Uses FormatDuration for the actual formatting.
func FormatQuotaCountdown(unblockAt time.Time) string {
	d := time.Until(unblockAt)
	if d <= 0 {
		return "resuming…"
	}
	return FormatDuration(d)
}

// RefreshQuotaCountdown is a no-op stub kept for the API contract.
// The actual row rebuild happens in updateAgentsTable via quotaStatusBadge.
func (p *AgentsPanel) RefreshQuotaCountdown() {}
