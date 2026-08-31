package tui

import (
	"fmt"
	"time"
)

// FormatDuration returns a human-readable duration string matching the Flutter
// parity contract (leaf 09). This is exported so quota_status_test can exercise
// it directly; agents_panel uses it via FormatQuotaCountdown.
//
//  - negative (past-due)    -> "resuming…"
//  - >= 1h                 -> "Nh Mm" (omit minutes when 0)
//  - >= 1m < 1h            -> "Mm"
//  - < 1m                  -> "Ss"
func FormatDuration(d time.Duration) string {
	if d < 0 {
		return "resuming…"
	}
	if d >= time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if d >= time.Minute {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}
