package agent

import "strings"

// Plan-complexity evaluation (issue #58 capability 4).
//
// EvaluatePlanComplexity classifies a plan request into a coarse tier that a
// later change can use to route planning effort (e.g. skip interviews for
// trivial work, prefer multi-phase decomposition for complex work). It is
// deliberately deterministic and pure: no LLM call, no stores, no clocks —
// the same request always yields the same tier.

// ComplexityTier is the coarse planning-effort classification for a plan
// request. String-valued so it logs cleanly.
type ComplexityTier string

const (
	// TierTrivial marks work with a single concrete artifact shape — a
	// deterministic single step is the right plan.
	TierTrivial ComplexityTier = "trivial"

	// TierStandard is the default tier: ordinary multi-step planning.
	TierStandard ComplexityTier = "standard"

	// TierComplex marks work the user signaled as involved, or a re-plan
	// attempt of something that already failed once.
	TierComplex ComplexityTier = "complex"
)

// complexitySignalWords are the explicit user-signal vocabulary. A plan
// request whose input contains one of these (case-insensitive, word-boundary
// match) is Complex regardless of other signals.
var complexitySignalWords = []string{
	"iterative",
	"comprehensive",
	"thorough",
	// Multi-phase phrasing: "in phases/stages", "multi-phase/-stage".
	"phases",
	"stages",
	"multi-phase",
	"multi-phase",
	"multiphase",
	"multi-stage",
	"multistage",
}

// EvaluatePlanComplexity classifies a plan request by complexity tier.
//
// Signals, in priority order:
//  1. Explicit user signal — the input contains "iterative"/"comprehensive"/
//     "thorough" or multi-phase phrasing → TierComplex.
//  2. Re-plan attempt — req.IsReplan set by the escalation/replan path
//     → TierComplex.
//  3. Single-artifact shape (isSingleArtifactTask) → TierTrivial.
//  4. Otherwise → TierStandard.
//
// The first matching signal wins; nothing here is cumulative.
func EvaluatePlanComplexity(req PlanRequest) ComplexityTier {
	if planInputHasComplexitySignal(req.Input) {
		return TierComplex
	}
	if req.IsReplan {
		return TierComplex
	}
	if isSingleArtifactTask(req.Input) {
		return TierTrivial
	}
	return TierStandard
}

// planInputHasComplexitySignal reports whether the input carries an explicit
// complexity word. Matching is case-insensitive on ASCII and word-boundary
// aware: "phases" matches "in three phases" but not "phaseset".
func planInputHasComplexitySignal(input string) bool {
	if input == "" {
		return false
	}
	lowered := strings.ToLower(input)
	for _, w := range complexitySignalWords {
		if containsWord(lowered, w) {
			return true
		}
	}
	return false
}

// containsWord reports whether substr appears in s as a whole word:
// preceded by a non-letter/non-digit (or string start) and followed by a
// non-letter/non-digit (or string end). ASCII-only, matching the repo's
// ASCII case-conversion conventions elsewhere in the planner.
func containsWord(s, substr string) bool {
	start := 0
	for {
		idx := strings.Index(s[start:], substr)
		if idx < 0 {
			return false
		}
		idx += start
		beforeOK := idx == 0 || !isWordByte(s[idx-1])
		end := idx + len(substr)
		afterOK := end == len(s) || !isWordByte(s[end])
		if beforeOK && afterOK {
			return true
		}
		start = idx + 1
	}
}

// isWordByte reports whether b is an ASCII letter or digit (word character
// for containsWord's boundary check).
func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
