package llm

import (
	"errors"
	"fmt"
)

// ErrTierFloor is the tier-policy floor sentinel (llm-serving-tiers tree
// leaf 01, Contract C2): the alias-rotation loop returns it (wrapped in
// *TierFloorError) when the next rotation hop would exceed the configured
// degradation depth limit. It is a CLASSIFIED terminal error in the
// ErrAllModelsQuotaBlocked / ErrAllEndpointsBlocked family - check with
// errors.Is, never string matching. It is never retried, never surfaced
// as a completed turn, and never parks a turn.
var ErrTierFloor = errors.New("llm: tier degradation floor reached")

// TierFloorError is the typed tier-floor verdict: the rotation served
// Depth members (the primary plus Depth-1 degradations) and the next hop
// would have exceeded Limit (the effective MaxDepth or the capability's
// TurnCeiling). Like ContextOverflowError (errors_overflow.go) it is
// NonRetryable, so every client short-retry loop must exit immediately;
// recovery (relaxing the tier policy or accepting the failure) is the
// caller's decision, not the transport's. Cause carries the last member's
// dispatch failure for diagnosis.
type TierFloorError struct {
	// ProviderID/ModelID identify the LAST member that was dispatched
	// (not the refused next hop - that one was never touched).
	ProviderID string
	ModelID    string
	// Depth is the total members dispatched when the floor fired
	// (primary + degradations). Always Limit+1 on the stopped path.
	Depth int
	// Limit is the effective depth limit: MaxDepth, or the capability's
	// TurnCeiling when that was the binding constraint.
	Limit int
	// Capability names the TurnCeilings key when a per-turn ceiling (not
	// the global MaxDepth) stopped the rotation; empty otherwise.
	Capability string
	Cause      error
}

func (e *TierFloorError) Error() string {
	msg := fmt.Sprintf("%s: provider=%s model=%s depth=%d limit=%d", ErrTierFloor.Error(), e.ProviderID, e.ModelID, e.Depth, e.Limit)
	if e.Capability != "" {
		msg += fmt.Sprintf(" (turn ceiling for capability %q)", e.Capability)
	}
	if e.Cause != nil {
		msg += fmt.Sprintf(": %v", e.Cause)
	}
	return msg
}

func (e *TierFloorError) Unwrap() []error {
	// Unwrap to BOTH the cause and the sentinel (Go 1.20+ multi-unwrap):
	// errors.Is(err, ErrTierFloor) must hold through any wrap (the
	// classified-terminal contract), and errors.As recovers the typed
	// verdict while Is(err, ErrEmptyResponse) still sees the dispatch
	// failure that motivated the rotation.
	return []error{ErrTierFloor, e.Cause}
}

// NonRetryable returns true so the client short-retry loops exit
// immediately: a policy floor is not a transient provider condition.
func (e *TierFloorError) NonRetryable() bool {
	return true
}

var _ NonRetryableError = (*TierFloorError)(nil)

// IsTierFloorError reports whether err is (or wraps) a *TierFloorError.
func IsTierFloorError(err error) bool {
	if err == nil {
		return false
	}
	var fe *TierFloorError
	return errors.As(err, &fe)
}

// AsTierFloorError returns the *TierFloorError in err's chain, mirroring
// AsContextOverflowError (errors_overflow.go).
func AsTierFloorError(err error) (*TierFloorError, bool) {
	if err == nil {
		return nil, false
	}
	return errors.AsType[*TierFloorError](err)
}
