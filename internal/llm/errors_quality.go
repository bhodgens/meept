package llm

import (
	"errors"
	"fmt"
)

// ErrQualityFloor is the quality-floor sentinel (Contract C2,
// sovereign-router-parity leaf 01): the capability-escalation path returns
// it (wrapped in *QualityFloorError) when EVERY candidate scores below
// QualityFloorFor(difficulty, capability) — it never returns the
// best-scoring sub-floor model. It is a CLASSIFIED terminal error in the
// ErrTierFloor / ErrAllModelsQuotaBlocked family — check with errors.Is,
// never string matching. It is never retried, never parked, and never
// surfaces as a completed turn.
var ErrQualityFloor = errors.New("llm: quality floor not met")

// QualityFloorError is the typed quality-floor verdict: the candidate set
// was measured, the floor bound, and every candidate (including the
// current/default model) scored below it. Like TierFloorError
// (errors_tier.go) it is NonRetryable — a floor verdict is a policy
// decision, not a transient provider condition, so client short-retry
// loops must exit immediately.
type QualityFloorError struct {
	// Capability names the quality class the floor was derived from (the
	// raw skill requirement string, e.g. "code").
	Capability string
	// Difficulty is the classified difficulty the floor was evaluated at.
	Difficulty int
	// Floor is the minimum acceptable score (QualityFloorFor result).
	Floor int
	// Best is the highest score any candidate achieved (0 when none was
	// measured).
	Best int
	// Candidates is how many models were evaluated before the floor
	// blocked them all.
	Candidates int
}

func (e *QualityFloorError) Error() string {
	return fmt.Sprintf("%s: capability=%q difficulty=%d floor=%d best=%d candidates=%d",
		ErrQualityFloor.Error(), e.Capability, e.Difficulty, e.Floor, e.Best, e.Candidates)
}

// Unwrap to the sentinel (Go 1.20+ multi-unwrap shape, matching
// TierFloorError): errors.Is(err, ErrQualityFloor) must hold through any
// wrap, and errors.As recovers the typed verdict.
func (e *QualityFloorError) Unwrap() []error {
	return []error{ErrQualityFloor}
}

// NonRetryable returns true so the client short-retry loops exit
// immediately: a measured-quality floor is not a transient provider
// condition.
func (e *QualityFloorError) NonRetryable() bool {
	return true
}

var _ NonRetryableError = (*QualityFloorError)(nil)
