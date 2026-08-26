package agent

import (
	"github.com/caimlas/meept/internal/llm"
)

// classificationHardCeiling caps any derived cap so a misconfigured
// max_output cannot make a single classification call unbounded.
// 2048 matches the largest max_output commonly declared for small
// auxiliary models in models.json5 and is far more than any single
// JSON classification verdict needs.
const classificationHardCeiling = 2048

// classificationFloor is the minimum workable budget: enough for a
// thinking model to close its think block plus emit a short JSON verdict.
const classificationFloor = 1024

// effectiveClassificationCap returns the token budget for one
// classification call:
//
//  1. Start from cfg.MaxTokens (the model's declared max_output) when
//     cfg != nil and MaxTokens > 0.
//  2. Otherwise start from the floor directly.
//
// The result is clamped to [classificationFloor, classificationHardCeiling].
//
// Note that the floor beats a smaller declared max_output: an empty
// classification is strictly worse than exceeding the model's declared
// output by a bit — llama.cpp clamps internally anyway, so the overage
// request degrades gracefully instead of truncating to an unusable
// empty response.
func effectiveClassificationCap(cfg *llm.ModelConfig) int {
	cap := 0
	if cfg != nil {
		cap = cfg.MaxTokens
	}
	if cap < classificationFloor {
		return classificationFloor
	}
	if cap > classificationHardCeiling {
		return classificationHardCeiling
	}
	return cap
}
