package agent

import (
	"fmt"
	"math"

	"github.com/caimlas/meept/internal/task"
)

// AllotmentConfig controls how a model's context window is converted into
// work allotments and how steps are partitioned into batches.
type AllotmentConfig struct {
	// UsableRatio is the fraction of the (post-reserve) context window
	// usable for step input. Default 0.75.
	UsableRatio float64
	// ReserveTokens covers system prompt + tool definitions. Default 4096.
	ReserveTokens int
	// CharsPerToken is the character-to-token estimate ratio. Default 4.
	CharsPerToken float64
	// MinStepTokens is the floor for any single step's token estimate.
	// Default 512.
	MinStepTokens int
	// MaxBatchSteps caps steps per batch; 0 means no count cap.
	MaxBatchSteps int
}

// DefaultAllotmentConfig returns the pinned default configuration.
func DefaultAllotmentConfig() AllotmentConfig {
	return AllotmentConfig{
		UsableRatio:   0.75,
		ReserveTokens: 4096,
		CharsPerToken: 4,
		MinStepTokens: 512,
		MaxBatchSteps: 0,
	}
}

// EstimateStepTokens estimates the token cost of a step description as
// ceil(len(desc)/CharsPerToken), floored at MinStepTokens.
func EstimateStepTokens(desc string, cfg AllotmentConfig) int {
	charsPerToken := cfg.CharsPerToken
	if charsPerToken <= 0 {
		charsPerToken = 4
	}
	tokens := int(math.Ceil(float64(len(desc)) / charsPerToken))
	if tokens < cfg.MinStepTokens {
		return cfg.MinStepTokens
	}
	return tokens
}

// AllotmentTokens converts a context window limit into a work budget:
// (contextLimit - ReserveTokens) * UsableRatio, floored at 0. Returns 0
// when contextLimit <= 0 (unknown window).
func AllotmentTokens(contextLimit int, cfg AllotmentConfig) int {
	if contextLimit <= 0 {
		return 0
	}
	budget := float64(contextLimit-cfg.ReserveTokens) * cfg.UsableRatio
	if budget < 0 {
		return 0
	}
	return int(budget)
}

// SplitStepsByAllotment greedily partitions ordered steps into batches whose
// estimated token totals fit within allotmentTokens. An oversize step (its
// own estimate exceeds the allotment) gets its own batch. MaxBatchSteps > 0
// additionally caps steps per batch. allotmentTokens <= 0 returns a single
// batch containing every step.
func SplitStepsByAllotment(steps []*task.TaskStep, allotmentTokens int, cfg AllotmentConfig) [][]*task.TaskStep {
	if len(steps) == 0 {
		return nil
	}
	if allotmentTokens <= 0 {
		return [][]*task.TaskStep{steps}
	}
	var batches [][]*task.TaskStep
	var current []*task.TaskStep
	used := 0
	flush := func() {
		if len(current) > 0 {
			batches = append(batches, current)
			current = nil
			used = 0
		}
	}
	for _, step := range steps {
		cost := EstimateStepTokens(step.Description, cfg)
		if len(current) > 0 && (used+cost > allotmentTokens ||
			(cfg.MaxBatchSteps > 0 && len(current) >= cfg.MaxBatchSteps)) {
			flush()
		}
		current = append(current, step)
		used += cost
	}
	flush()
	return batches
}

// ContinuationDescription prefixes desc with a [continuation k/N] marker.
func ContinuationDescription(desc string, k, n int) string {
	return fmt.Sprintf("[continuation %d/%d] %s", k, n, desc)
}
