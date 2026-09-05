package llm

import (
	"testing"
	"time"
)

// TestRecordAliasFailure_QuotaErrorIsNoOp pins quota-reset-resilience master
// contract 4: a *QuotaResetError must NEVER advance alias health. The error
// unwraps to a 429 *APIError whose Classify verdict is FailureThrottle, so
// without the guard a quota error bumped ConsecutiveFails, set the cooldown,
// armed the endpoint block map, and released sticky pins — punishing the
// alias for a billing window. Analyzer/classifier call sites pass raw Chat
// errors here; the loop's quota branch is pre-guarded, this one is enforced
// at the source.
func TestRecordAliasFailure_QuotaErrorIsNoOp(t *testing.T) {
	r := NewResolver(&ProvidersConfig{
		Providers: map[string]ProviderConfig{
			"p1": {
				API: "openai",
				Models: map[string]ModelDef{
					"m1": {Name: "m1"},
				},
			},
		},
		ModelAliases: map[string]ModelAliasEntry{
			"alias1": {Models: []string{"p1/m1"}},
		},
	}, nil)

	quotaErr := &QuotaResetError{
		ProviderID: "p1",
		ModelID:    "m1",
		ResetAt:    time.Now().Add(time.Hour),
		Cause:      &APIError{StatusCode: 429, Detail: "quota"},
	}

	before := r.getOrCreateHealth("alias1")
	r.RecordAliasFailure("alias1", quotaErr, &ModelConfig{ProviderID: "p1", ModelID: "m1"})

	if before.ConsecutiveFails != 0 {
		t.Errorf("ConsecutiveFails = %d, want 0 (quota must not count as alias failure)", before.ConsecutiveFails)
	}
	if !before.CooldownUntil.IsZero() {
		t.Errorf("CooldownUntil = %v, want zero (quota must not set cooldown)", before.CooldownUntil)
	}
	if len(r.endpointBlocks) != 0 {
		t.Errorf("endpointBlocks = %v, want empty (quota must not arm endpoint blocks)", r.endpointBlocks)
	}
}

// Companion check: a REAL throttle (429 without quota shape) must still
// record — the guard must not over-suppress.
func TestRecordAliasFailure_BareThrottleStillRecords(t *testing.T) {
	r := NewResolver(&ProvidersConfig{
		Providers: map[string]ProviderConfig{
			"p1": {
				API: "openai",
				Models: map[string]ModelDef{
					"m1": {Name: "m1"},
				},
			},
		},
		ModelAliases: map[string]ModelAliasEntry{
			"alias1": {Models: []string{"p1/m1"}},
		},
	}, nil)

	r.RecordAliasFailure("alias1", &APIError{StatusCode: 429, Detail: "slow down"},
		&ModelConfig{ProviderID: "p1", ModelID: "m1"})

	h := r.getOrCreateHealth("alias1")
	if h.ConsecutiveFails != 1 {
		t.Errorf("ConsecutiveFails = %d, want 1 (bare 429 is a real alias failure)", h.ConsecutiveFails)
	}
}
