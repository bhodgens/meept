package session

import (
	"time"
)

// IsInteractive reports whether a session is interactive right now,
// per DECISIONS.md D11: a recent USER message on the session (within
// `window`, Q1 default 5m from queue.interactive_window) OR the
// client-declared Foreground flag. WS presence is deliberately not a
// signal (rejected in D11).
//
// ACTIVITY-SOURCE DECISION (leaf 04/01 Task 1): this helper reads
// Session.LastUserMessageAt, a dedicated user-message timestamp — NOT
// Session.LastActivity. Write-path audit of the three candidate sources:
//
//   - Session.LastActivity is the WRONG source. Its writers are
//     Attach/Detach/AddWorker/RemoveWorker (session.go:413-484),
//     store mutations (store_sqlite.go updateSession/SetNoFence/
//     SetProject/rename), and internal/debug touches. No chat or
//     message path writes it, so a session would read as "interactive"
//     merely because a user attached a client or re-pointed its project.
//   - ActivityTracker.HasRecentActivity(sessionID, window) has the
//     right shape but its feed (RecordActivity) has NO production
//     callers anywhere in the repo (grep-verified) — it only records
//     in tests, so it would always answer false in a real deployment.
//   - thread.LastActivityAt (Touch() from thread_router.go on message
//     routing) is message-proximate but thread-scoped; an IsInteractive
//     over a session would need a max() across N threads, and legacy
//     sessions with no threads would never qualify.
//
// A dedicated per-session last-user-message timestamp is therefore the
// source of truth for D11. Its chat-path writer is wired separately
// (session-handler message routing calls SetLastUserMessage, see
// session.go); leaf 02's enqueue stamp depends on exactly this field.
//
// The function is pure: `now` is injected so callers (and tests) control
// the clock. window <= 0 disables the recency check entirely, leaving
// Foreground as the only qualifying signal. A nil session is never
// interactive, and a zero LastUserMessageAt (never messaged) never
// qualifies, even with a huge window.
func IsInteractive(s *Session, now time.Time, window time.Duration) bool {
	if s == nil {
		return false
	}
	if s.Foreground {
		return true
	}
	if window <= 0 {
		return false
	}
	if s.LastUserMessageAt.IsZero() {
		return false
	}
	return now.Sub(s.LastUserMessageAt) <= window
}
