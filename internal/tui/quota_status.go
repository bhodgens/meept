// Package tui — quota_status.go renders quota_wait and blocked agent status
// in the agents list/detail views (quota-reset-resilience leaf 08).
//
// Wire: bus topic "agent.quota_wait" → WS type "agent_progress" (leaf 07)
// → EventStreamDataMsg (app.go) → quotaStateMsg (agents_panel.go).
//
// Parity: FormatQuotaCountdown is the exact countdown format leaf 09 mirrors
// in Flutter (ui/flutter_ui/lib/features/agents/quota_status.dart) — do not
// change the strings without updating the GUI side.
//
// M9 timezone convention: quota timestamps ride the wire as RFC3339 with the
// DAEMON's offset embedded (producers Format(time.RFC3339)). Surfaces render
// DAEMON-LOCAL time by default — the HH:MM shown is the wall-clock in the
// offset embedded in the string, never converted to the client's zone and
// never UTC. A client MAY opt into client-local rendering via
// client.json5 rendering.time_display ("daemon"|"local", default "daemon");
// the choice is plumbed through SetQuotaTimeDisplay at startup.
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

// Time display modes (M9): how quota HH:MM timestamps are rendered.
//
//	TimeDisplayDaemon — render the wall-clock embedded in the RFC3339
//	                   string (the daemon's local time). Default.
//	TimeDisplayLocal  — convert to the client's local zone before
//	                   formatting. Opt-in via rendering.time_display.
const (
	TimeDisplayDaemon = "daemon"
	TimeDisplayLocal  = "local"
)

// QuotaTimeDisplay selects how quota HH:MM timestamps render on this client.
// Set once at startup from client.json5 rendering.time_display via
// SetQuotaTimeDisplay. A package-level var is deliberate here (config-plumbing
// an instance through every render helper would thread the value through
// pure functions used by tests and the table builder); it is only written
// during client construction, before any rendering happens.
var QuotaTimeDisplay = TimeDisplayDaemon

// SetQuotaTimeDisplay applies a rendering.time_display value. Empty or
// unknown values keep the current setting (daemon default is installed at
// startup; unknown explicit values are logged by the caller).
func SetQuotaTimeDisplay(mode string) {
	switch mode {
	case TimeDisplayDaemon, TimeDisplayLocal:
		QuotaTimeDisplay = mode
	}
}

// renderQuotaHHmm formats t as HH:MM per the M9 convention: daemon-local
// (the offset embedded in the parsed RFC3339 value) by default, client-local
// when QuotaTimeDisplay == TimeDisplayLocal. Never UTC.
func renderQuotaHHmm(t time.Time) string {
	if QuotaTimeDisplay == TimeDisplayLocal {
		t = t.Local()
	}
	return t.Format("15:04")
}

// renderQuotaTime is renderQuotaHHmm plus the zone abbreviation, used by the
// detail view where the zone matters ("15:04 MST"). The zone shown is the
// daemon's when rendering daemon-local, the client's otherwise.
func renderQuotaTime(t time.Time) string {
	if QuotaTimeDisplay == TimeDisplayLocal {
		t = t.Local()
	}
	return t.Format("15:04 MST")
}

// Park lifecycle reason strings mirrored from
// internal/agent/parked_turn.go (consumers re-declare the wire vocabulary
// so internal/tui does not import internal/agent).
const (
	ReasonQuotaWait       = "quota_wait"       // park (class=quota)
	ReasonThrottleWait    = "throttle_wait"    // park (class=throttle)
	ReasonThrottleResumed = "throttle_resumed" // resume (class=throttle)
	ReasonThrottleGiveUp  = "throttle_give_up" // give-up past MaxWait (D8)
)

// reasonQuotaGiveUpText is the give-up badge text (I-M8): a throttle wait
// past MaxWait abandons the turn (D8 ThrottleGiveUpError) — the agent is no
// longer waiting, so the badge must not render a wait label. The Flutter
// badge renders this label in the red (error) tone, mirroring the blocked
// badge; the TUI table cell is plain text like every other status cell.
func reasonQuotaGiveUpText() string {
	return "throttle gave up · action required"
}

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
// (tree 03 leaf 04 Task 3 + I-M8 reason, TUI + Flutter parity — the Flutter
// side mirrors this byte-for-byte in
// ui/flutter_ui/lib/features/agents/quota_status.dart; change both together):
//
//	quota class (or absent — legacy events): "quota_wait · reset HH:MM"
//	throttle class:                          "quota_wait · throttle retry HH:MM"
//	throttle give-up reason (I-M8):          "throttle gave up · action required"
//
// reason is the park-event payload's reason key ("quota_wait" |
// "throttle_wait" | "throttle_resumed" | "throttle_give_up"; "" on legacy
// events). Resume and legacy events fall through to the class-selected wait
// label; only a give-up changes the shape (it is a failure surface, not a
// wait).
//
// HH:MM is the ABSOLUTE time of unblockAt per the M9 convention
// (renderQuotaHHmm): the daemon's local wall-clock embedded in the
// RFC3339 wire value by default, client-local when rendering.time_display
// is "local". Never relative countdown math: the GUI runs on web where
// client wall clocks cannot be trusted (leaf Notes). Lowercase per repo UI
// rule.
func QuotaWaitLabel(class, reason string, unblockAt time.Time) string {
	if reason == ReasonThrottleGiveUp {
		return reasonQuotaGiveUpText()
	}
	hhmm := renderQuotaHHmm(unblockAt)
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
// (unblockAt zero renders "unknown"). <time> follows the M9 convention
// (renderQuotaTime): the daemon-local wall-clock by default — never UTC as
// the pre-M9 implementation rendered — or client-local when the toggle is on.
func RenderQuotaDetailLines(primaryModel, fallbackModel string, unblockAt time.Time) []string {
	if fallbackModel == "" {
		return nil
	}
	until := "unknown"
	if !unblockAt.IsZero() {
		until = renderQuotaTime(unblockAt)
	}
	return []string{
		fmt.Sprintf("primary: %s (blocked until %s)", primaryModel, until),
		fmt.Sprintf("active: %s", fallbackModel),
	}
}
