package agent

// 20261121-interactive-usability leaf 05 (degraded-signals) helpers.
//
// Honesty signals for degraded lanes:
//   - appendDegradedClassifierNote: one bracketed lowercase note appended to
//     the terminal reply when classification routed via a degraded method.
//   - planningDegradedNote: the fixed sentence for the degraded-planning
//     fallback path.
//   - planQualityFromMetadata: reads the persisted 'plan_quality' marker.
//   - cooldownForAttempt: attempt-aware unavailability cooldown (15s first,
//     exponential regrowth to the 60s cap).

import (
	"encoding/json"
	"strings"
	"time"
)

const (
	// degradedClassifierNote is the exact user-facing honesty note appended
	// to replies routed via a degraded classification method (leaf 05,
	// label posture mirrors the loop's [unverified: ...] lane).
	degradedClassifierNote = "[routed by keyword fallback - classification degraded]"

	// planningDegradedNote is the exact user-facing sentence appended when
	// planning degraded to a deterministic single direct step (leaf 05).
	planningDegradedNote = "[planning degraded: single direct step]"
)

// appendDegradedClassifierNote appends the degraded-classification honesty
// note to a terminal reply when the turn's Intent.Method is a degraded
// method (heuristic_fallback, fallback). Healthy lanes (llm, keyword,
// semantic) return the reply byte-identical; a nil intent is healthy. The
// note is idempotent (never duplicated) and sits on its own line.
func appendDegradedClassifierNote(reply string, intent *Intent) string {
	if intent == nil {
		return reply
	}
	switch intent.Method {
	case "heuristic_fallback", "fallback":
	default:
		return reply
	}
	if strings.Contains(reply, degradedClassifierNote) {
		return reply
	}
	if strings.TrimSpace(reply) == "" {
		return degradedClassifierNote
	}
	return reply + "\n\n" + degradedClassifierNote
}

// planQualityFromMetadata reads the persisted 'plan_quality' key from task
// metadata JSON, returning "" when absent, unparsable, or not a string.
func planQualityFromMetadata(metadataJSON []byte) string {
	if len(metadataJSON) == 0 {
		return ""
	}
	meta := map[string]any{}
	if err := json.Unmarshal(metadataJSON, &meta); err != nil {
		return ""
	}
	if q, ok := meta["plan_quality"].(string); ok {
		return q
	}
	return ""
}

// cooldownForAttempt computes the unavailability cooldown for the Nth
// consecutive failure: firstCooldown for attempt 0, doubling up to the cap.
// Attempt-aware shape (leaf 05): a transient classifier blip costs the
// user one short fallback window instead of a blanket 60s.
func cooldownForAttempt(attempt int, first, cap time.Duration) time.Duration {
	if first <= 0 {
		first = defaultUnavailableCooldown
	}
	if cap <= 0 {
		cap = defaultUnavailableCooldown
	}
	d := first
	for i := 0; i < attempt && d < cap; i++ {
		d *= 2
	}
	if d > cap {
		d = cap
	}
	return d
}
