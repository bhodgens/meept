package tui

import (
	"fmt"
	"time"
)

// FormatDuration returns a human-readable duration string. This is the TUI
// side of the countdown parity contract shared with leaf 09 (Flutter) and
// mirrors internal/llm errors_quota.go formatDuration semantics: values are
// truncated to the minute (never rounded up), so a 90s wait shows as "1m"
// and the user is never told more time remains than actually does.
//
//   - >= 1h with minutes -> "3h 12m"
//   - >= 1h, minutes == 0 -> "2h"
//   - < 1h               -> "45m" (0m for sub-minute remains)
//
// Negative-input sentinel layering (byte-match contract, all three sides —
// internal/llm/errors_quota.go formatDuration, this file, and
// ui/flutter_ui/lib/utils/format_duration.dart): the BARE formatter has no
// sentinel — negative inputs truncate toward zero exactly like positive ones
// (e.g. "-1m"). Past-due wording is owned by higher layers: "resets soon" by
// the badge layer (QuotaCountdownText here; quotaCountdownText in Flutter's
// quota_status.dart), "soon" by the TUI countdown-string layer
// (FormatQuotaCountdownAt). Do not add a negative branch here.
func FormatDuration(d time.Duration) string {
	d = d.Truncate(time.Minute)
	if d >= time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m > 0 {
			return fmt.Sprintf("%dh %dm", h, m)
		}
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}
