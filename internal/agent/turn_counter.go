package agent

import "fmt"

// The WIRED half of the turn counter (HALO Phase 4/7): the turnCounter type
// lives in subagent_pool.go (Increment/Remaining/GetCount/Reset) and feeds
// the rlm_analyzer Nudge chain. This file keeps only the nudge MESSAGE
// policy that chain calls (Nudge + nudgeMessageFor + thresholdFor/thresholdTagFor).
//
// The unwired remainder — GetRemainingTurns, GetNudgeThreshold, RoundTo,
// nudgeInfo.String — was deleted per the HALO Phase 7 plan decision
// (docs/plans/2026-07-17-halo-implementation-status.md:126 "PARTIAL — turn
// counter exists, nudges missing"; :165 recommendation "Skip Phase 7 — UX
// polish, not capability gap"): zero production consumers at HEAD.

// turnNudge defines the threshold and message for a self-pacing nudge.
type turnNudge struct {
	Threshold float64 // fraction of limit at which this fires (0.25, 0.50, etc.)
	Message   string  // the nudge text (%d is replaced with remaining count)
	Tag       string  // category tag: "outline", "prioritize", "consolidate", "finalize", "exhausted"
}

// nudgeTable defines the HALO-style progressive urgency thresholds.
// Messages are ordered by increasing urgency; the last entry (100%) is
// the sentinel that fires when the turn limit is reached.
var nudgeTable = []turnNudge{
	{0.25, "You have %d turns remaining. Consider outlining your approach.", "outline"},
	{0.50, "You have %d turns remaining. Ensure you're prioritizing key findings.", "prioritize"},
	{0.75, "You have %d turns remaining. Begin consolidating findings.", "consolidate"},
	{0.90, "You have %d turns remaining. Prepare to finalize your analysis.", "finalize"},
	{1.00, "Turn limit reached. Finalize immediately.", ""},
}

// nudgeInfo carries the result of a Nudge() call.
type nudgeInfo struct {
	Message     string  `json:"message"`
	Threshold   float64 `json:"threshold"`
	Tag         string  `json:"tag"`
	CurrentTurn int     `json:"current_turn"`
	Limit       int     `json:"limit"`
	Remaining   int     `json:"remaining"`
	Exhausted   bool    `json:"exhausted"`
}

// thresholdFor returns the highest nudge threshold that applies for turn/tot.
// Returns 0.0 if the fraction hasn't reached the first threshold.
func thresholdFor(turn, tot int) float64 {
	if tot <= 0 {
		return 0.0
	}
	frac := float64(turn) / float64(tot)
	for i := len(nudgeTable) - 1; i >= 0; i-- {
		if frac >= nudgeTable[i].Threshold {
			return nudgeTable[i].Threshold
		}
	}
	return 0.0
}

// thresholdTagFor returns the tag for the given turn against the limit.
func thresholdTagFor(turn, tot int) string {
	if tot <= 0 {
		return ""
	}
	frac := float64(turn) / float64(tot)
	for i := len(nudgeTable) - 1; i >= 0; i-- {
		if frac >= nudgeTable[i].Threshold {
			return nudgeTable[i].Tag
		}
	}
	return ""
}

// nudgeMessageFor returns the formatted nudge message for turn/tot.
func nudgeMessageFor(turn, tot int) string {
	if tot <= 0 {
		return fmt.Sprintf("[HALO: turn %d of 0 — 0 turns left]", turn)
	}

	remaining := tot - turn
	remaining = max(remaining, 0)

	if turn >= tot {
		return "Turn limit reached. Finalize immediately."
	}

	frac := float64(turn) / float64(tot)

	// Scan thresholds from highest to lowest for progressive urgency.
	for i := len(nudgeTable) - 1; i >= 0; i-- {
		tbl := nudgeTable[i]
		if frac >= tbl.Threshold {
			if tbl.Tag != "" {
				return fmt.Sprintf(tbl.Message, remaining)
			}
			return tbl.Message
		}
	}

	// Below 25%: return the basic HALO banner (original behavior).
	return fmt.Sprintf("[HALO: turn %d of %d — %d turns left]", turn, tot, remaining)
}

// Nudge returns a contextual self-pacing message based on the current
// turn fraction relative to the limit. It returns structured nudgeInfo
// so callers can inject the message and act on threshold crossings.
//
// At each threshold (25%, 50%, 75%, 90%, 100%) a progressively more
// urgent message is returned. Thresholds fire in descending order so
// that at 80% of turns consumed the 75% message fires, not 25%.
func (tc *turnCounter) Nudge() nudgeInfo {
	tc.mu.Lock()
	cur := tc.current
	lim := tc.limit
	tc.mu.Unlock()

	remaining := lim - cur
	remaining = max(remaining, 0)

	exhausted := cur >= lim

	if exhausted {
		return nudgeInfo{
			Message:     "Turn limit reached. Finalize immediately.",
			Threshold:   1.0,
			Tag:         "exhausted",
			CurrentTurn: cur,
			Limit:       lim,
			Remaining:   0,
			Exhausted:   true,
		}
	}

	if lim <= 0 {
		return nudgeInfo{
			Message:     fmt.Sprintf("[HALO: turn %d of 0 — 0 turns left]", cur),
			Threshold:   0.0,
			Tag:         "",
			CurrentTurn: cur,
			Limit:       lim,
			Remaining:   0,
			Exhausted:   false,
		}
	}

	return nudgeInfo{
		Message:     nudgeMessageFor(cur, lim),
		Threshold:   thresholdFor(cur, lim),
		Tag:         thresholdTagFor(cur, lim),
		CurrentTurn: cur,
		Limit:       lim,
		Remaining:   remaining,
		Exhausted:   false,
	}
}
