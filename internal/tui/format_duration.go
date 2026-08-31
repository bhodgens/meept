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
// Past-due durations are handled by FormatQuotaCountdown ("resets soon"),
// not here; a negative input formats via truncation like the llm helper.
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
