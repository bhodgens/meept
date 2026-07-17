package agent

import (
	"fmt"
	"math"
	"strings"
)

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
// All fields are exported for JSON serialization through the RLM output path.
type nudgeInfo struct {
	// Message is the human-readable nudge text for injection into the agent prompt.
	Message string `json:"message"`
	// Threshold is the threshold fraction that was crossed (0.0 if no threshold crossed).
	Threshold float64 `json:"threshold"`
	// Tag categorizes the nudge urgency level.
	Tag string `json:"tag"`
	// CurrentTurn is the turn at the time of the nudge.
	CurrentTurn int `json:"current_turn"`
	// Limit is the configured maximum turn limit.
	Limit int `json:"limit"`
	// Remaining is the turns left after increment.
	Remaining int `json:"remaining"`
	// Exhausted is true when the turn limit was reached.
	Exhausted bool `json:"exhausted"`
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
	if remaining < 0 {
		remaining = 0
	}

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
	if remaining < 0 {
		remaining = 0
	}

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

// GetRemainingTurns returns the number of turns remaining.
// This method exists so RLM analyzer code can report remaining turns
// in telemetry without needing to construct the nudge string.
func (tc *turnCounter) GetRemainingTurns() int {
	return tc.Remaining()
}

// GetNudgeThreshold returns the highest threshold fraction that the
// current turn position has crossed (0.0 if below the first threshold).
// Callers can use this to decide whether to inject progressive-urgency
// context into the LLM prompt.
func (tc *turnCounter) GetNudgeThreshold() float64 {
	tc.mu.Lock()
	cur := tc.current
	lim := tc.limit
	tc.mu.Unlock()

	return thresholdFor(cur, lim)
}

// RoundTo returns f rounded to n decimal places.
func RoundTo(f float64, n int) float64 {
	pow := math.Pow(10, float64(n))
	return math.Round(f*pow) / pow
}

// String returns a compact representation of the nudge info.
func (n nudgeInfo) String() string {
	parts := []string{
		fmt.Sprintf("threshold=%.2f", n.Threshold),
		fmt.Sprintf("tag=%s", n.Tag),
		fmt.Sprintf("turn=%d", n.CurrentTurn),
		fmt.Sprintf("limit=%d", n.Limit),
		fmt.Sprintf("remaining=%d", n.Remaining),
	}
	if n.Exhausted {
		parts = append(parts, "exhausted=true")
	}
	return strings.Join(parts, ", ")
}
