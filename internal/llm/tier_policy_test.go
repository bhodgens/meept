package llm

// Tier policy tests (llm-serving-tiers tree leaf 01): degradation-depth
// limits on the ProviderManager rotation loop, per-turn capability
// ceilings, the typed ErrTierFloor terminal error, degraded-tier response
// metadata signaling, and the precedence regression guard (a quota-blocked
// candidate is skipped BEFORE any depth counting).
//
// Pinned design decision (master Open Questions): only dispatched-and-
// failed rotations consume depth. A quota-block skip dispatches nothing
// and consumes zero depth. Do not reopen.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// tierStubChatter is a counted Chatter stub: every Chat/ChatWithProgress
// dispatch increments calls, then it returns the fixed resp/err.
type tierStubChatter struct {
	calls atomic.Int32
	resp  *Response
	err   error
}

func (s *tierStubChatter) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	s.calls.Add(1)
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

func (s *tierStubChatter) ChatWithProgress(ctx context.Context, messages []ChatMessage, progress ProgressCallback, opts ...ChatOption) (*Response, error) {
	return s.Chat(ctx, messages, opts...)
}

func (s *tierStubChatter) Config() *ModelConfig { return &ModelConfig{} }

// newTierTestManager builds an initialized ProviderManager whose pool is
// exactly the given stub chatters (provider/model ids prov-N/model-N in
// priority order) with the given per-member capability maps, and installs
// the tier policy.
func newTierTestManager(t *testing.T, policy *TierPolicy, chatters []*tierStubChatter, caps []map[string]bool) (*ProviderManager, []*tierStubChatter) {
	t.Helper()
	pm := NewProviderManager(ProviderManagerConfig{Logger: discardLogger()})
	pm.providers = nil
	for i, ch := range chatters {
		cfg := &ModelConfig{
			ProviderID: fmt.Sprintf("prov-%d", i+1),
			ModelID:    fmt.Sprintf("model-%d", i+1),
			BaseURL:    "http://127.0.0.1:1/v1", // never dialed: stub chatters
		}
		cfg.Capabilities = caps[i]
		pm.providers = append(pm.providers, &ProviderEntry{
			Config:   cfg,
			Chatter:  ch,
			Priority: i,
			Health:   &ProviderHealth{ProviderID: cfg.ProviderID, Status: ProviderStatusHealthy},
		})
	}
	pm.initialized = true
	pm.SetTierPolicy(policy)
	return pm, chatters
}

func totalTierDispatches(chatters []*tierStubChatter) int {
	n := 0
	for _, ch := range chatters {
		n += int(ch.calls.Load())
	}
	return n
}

// TestTierFloorStopsRotationAtMaxDepth (T2 worked example): 4 members all
// returning empty responses; MaxDepth=2. The loop dispatches at most 3
// members (primary + 2 degradations) and the error satisfies
// errors.Is(err, ErrTierFloor).
func TestTierFloorStopsRotationAtMaxDepth(t *testing.T) {
	allFail := func() *tierStubChatter {
		return &tierStubChatter{err: ErrEmptyResponse}
	}
	m1, m2, m3, m4 := allFail(), allFail(), allFail(), allFail()
	pm, chatters := newTierTestManager(t,
		&TierPolicy{Enabled: true, MaxDepth: 2},
		[]*tierStubChatter{m1, m2, m3, m4},
		[]map[string]bool{nil, nil, nil, nil},
	)

	_, err := pm.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("expected the rotation to stop with an error at the tier floor")
	}
	if !errors.Is(err, ErrTierFloor) {
		t.Fatalf("errors.Is(err, ErrTierFloor) = false; err = %v", err)
	}
	if got := totalTierDispatches(chatters); got != 3 {
		t.Errorf("dispatched members = %d, want 3 (primary + 2 degradations)", got)
	}
	var floorErr *TierFloorError
	if !errors.As(err, &floorErr) {
		t.Fatalf("error is not a *TierFloorError: %v", err)
	}
	if floorErr.Depth != 3 {
		t.Errorf("floor error Depth = %d, want 3 (the refused next hop)", floorErr.Depth)
	}
	if floorErr.Limit != 2 {
		t.Errorf("floor error Limit = %d, want 2", floorErr.Limit)
	}
	// Member 4 must never be dispatched.
	if m4.calls.Load() != 0 {
		t.Errorf("member-4 dispatched %d times, want 0 (floor stopped rotation)", m4.calls.Load())
	}
}

// TestTierQuotaBlockedSkippedBeforeDepth (T4 precedence regression):
// member-1 quota-blocked and MaxDepth=2. Member-1 is skipped as unavailable
// (no request dispatched, zero depth consumed) and rotation starts at
// member-2, which serves at depth 0 (not degraded).
func TestTierQuotaBlockedSkippedBeforeDepth(t *testing.T) {
	okResp := &Response{Content: "served"}
	m1 := &tierStubChatter{err: ErrEmptyResponse}
	m2 := &tierStubChatter{resp: okResp}
	m3 := &tierStubChatter{resp: okResp}
	pm, chatters := newTierTestManager(t,
		&TierPolicy{Enabled: true, MaxDepth: 2, SignalDegraded: true},
		[]*tierStubChatter{m1, m2, m3},
		[]map[string]bool{nil, nil, nil},
	)

	// Persist the quota block on member-1's credential (the same map
	// getOrderedProviders filters on).
	pm.mu.Lock()
	pm.blockQuotaCredential(pm.providers[0], time.Now().Add(time.Hour))
	pm.mu.Unlock()

	resp, err := pm.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("expected success served by member-2: %v", err)
	}
	if resp.Content != "served" {
		t.Errorf("resp.Content = %q, want served-by-member-2", resp.Content)
	}
	if m1.calls.Load() != 0 {
		t.Errorf("quota-blocked member-1 dispatched %d times, want 0 (skipped before depth counting)", m1.calls.Load())
	}
	if totalTierDispatches(chatters) != 1 {
		t.Errorf("total dispatches = %d, want 1 (member-2 only)", totalTierDispatches(chatters))
	}
	// Serving tier metadata: depth 0, NOT degraded, tier = member-2.
	if resp.Metadata == nil {
		t.Fatal("metadata map missing; want tier signaling keys")
	}
	if resp.Metadata[MetaKeyLLMTier] != "prov-2/model-2" {
		t.Errorf("llm_tier = %v, want prov-2/model-2", resp.Metadata[MetaKeyLLMTier])
	}
	if degraded, ok := resp.Metadata[MetaKeyLLMTierDegraded].(bool); !ok || degraded {
		t.Errorf("llm_tier_degraded = %v, want false (primary-tier serve at depth 0)", resp.Metadata[MetaKeyLLMTierDegraded])
	}
	if depth, ok := resp.Metadata[MetaKeyLLMTierDepth].(int); !ok || depth != 0 {
		t.Errorf("llm_tier_depth = %v, want 0", resp.Metadata[MetaKeyLLMTierDepth])
	}
}

// TestTierTurnCeilingCapsDegradations: TurnCeilings["small"]=1 caps the
// small-capability turn to 1 degradation even though MaxDepth=2. The
// counter is per-turn: a SECOND call resets it and again dispatches
// exactly 2 members.
func TestTierTurnCeilingCapsDegradations(t *testing.T) {
	allFail := func() *tierStubChatter { return &tierStubChatter{err: ErrEmptyResponse} }
	m1, m2, m3 := allFail(), allFail(), allFail()
	pm, chatters := newTierTestManager(t,
		&TierPolicy{
			Enabled:      true,
			MaxDepth:     2,
			TurnCeilings: map[string]int{"small": 1},
		},
		[]*tierStubChatter{m1, m2, m3},
		[]map[string]bool{{"small": true}, nil, nil},
	)

	for turn := 1; turn <= 2; turn++ {
		_, err := pm.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
		if !errors.Is(err, ErrTierFloor) {
			t.Fatalf("turn %d: errors.Is(err, ErrTierFloor) = false; err = %v", turn, err)
		}
		if got := totalTierDispatches(chatters); got != 2*turn {
			t.Fatalf("turn %d: total dispatches = %d, want %d (ceiling 1 => primary + 1 degradation per turn)", turn, got, 2*turn)
		}
		if m3.calls.Load() != 0 {
			t.Fatalf("turn %d: member-3 dispatched %d times, want 0 (ceiling stops rotation first)", turn, m3.calls.Load())
		}
		// Reset the health state recordFailure left behind so the next
		// turn starts from the same topology (per-turn counter isolation
		// is what this test pins; cross-turn health decay is not).
		for _, e := range pm.providers {
			pm.mu.Lock()
			e.Health.Status = ProviderStatusHealthy
			e.Health.ConsecutiveFails = 0
			pm.mu.Unlock()
		}
	}
}

// TestTierSignalMetadataOnDegradedTurn (T3): a turn served by the 2nd
// alias member with tiers enabled+signaling carries llm_tier=<member-2>,
// llm_tier_degraded=true, llm_tier_depth=1.
func TestTierSignalMetadataOnDegradedTurn(t *testing.T) {
	m1 := &tierStubChatter{err: ErrEmptyResponse}
	m2 := &tierStubChatter{resp: &Response{Content: "second"}}
	pm, _ := newTierTestManager(t,
		&TierPolicy{Enabled: true, MaxDepth: 2, SignalDegraded: true},
		[]*tierStubChatter{m1, m2},
		[]map[string]bool{nil, nil},
	)

	resp, err := pm.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	if resp.Metadata[MetaKeyLLMTier] != "prov-2/model-2" {
		t.Errorf("llm_tier = %v, want prov-2/model-2", resp.Metadata[MetaKeyLLMTier])
	}
	if degraded, ok := resp.Metadata[MetaKeyLLMTierDegraded].(bool); !ok || !degraded {
		t.Errorf("llm_tier_degraded = %v, want true", resp.Metadata[MetaKeyLLMTierDegraded])
	}
	if depth, ok := resp.Metadata[MetaKeyLLMTierDepth].(int); !ok || depth != 1 {
		t.Errorf("llm_tier_depth = %v, want 1", resp.Metadata[MetaKeyLLMTierDepth])
	}
}

// TestTierSignalMetadataOnPrimaryTurn: a primary-served turn still carries
// the three keys when signaling is on (depth 0, degraded=false).
func TestTierSignalMetadataOnPrimaryTurn(t *testing.T) {
	m1 := &tierStubChatter{resp: &Response{Content: "first"}}
	pm, _ := newTierTestManager(t,
		&TierPolicy{Enabled: true, MaxDepth: 2, SignalDegraded: true},
		[]*tierStubChatter{m1},
		[]map[string]bool{nil},
	)

	resp, err := pm.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	if resp.Metadata[MetaKeyLLMTier] != "prov-1/model-1" {
		t.Errorf("llm_tier = %v, want prov-1/model-1", resp.Metadata[MetaKeyLLMTier])
	}
	if degraded, ok := resp.Metadata[MetaKeyLLMTierDegraded].(bool); !ok || degraded {
		t.Errorf("llm_tier_degraded = %v, want false", resp.Metadata[MetaKeyLLMTierDegraded])
	}
	if depth, ok := resp.Metadata[MetaKeyLLMTierDepth].(int); !ok || depth != 0 {
		t.Errorf("llm_tier_depth = %v, want 0", resp.Metadata[MetaKeyLLMTierDepth])
	}
}

// TestTierSignalDisabledWritesNothing (T3 zero-behavior rule): with tiers
// disabled (nil policy) or with signal_degraded=false, the metadata map
// contains NONE of the three keys.
func TestTierSignalDisabledWritesNothing(t *testing.T) {
	cases := []struct {
		name   string
		policy *TierPolicy
	}{
		{name: "nil policy (disabled)", policy: nil},
		{name: "enabled=false", policy: &TierPolicy{Enabled: false, SignalDegraded: true}},
		{name: "signal_degraded=false", policy: &TierPolicy{Enabled: true, MaxDepth: 2, SignalDegraded: false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m1 := &tierStubChatter{err: ErrEmptyResponse}
			m2 := &tierStubChatter{resp: &Response{Content: "second"}}
			pm, _ := newTierTestManager(t, tc.policy, []*tierStubChatter{m1, m2}, []map[string]bool{nil, nil})

			resp, err := pm.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
			if err != nil {
				t.Fatalf("expected success: %v", err)
			}
			for _, key := range []string{MetaKeyLLMTier, MetaKeyLLMTierDegraded, MetaKeyLLMTierDepth} {
				if _, present := resp.Metadata[key]; present {
					t.Errorf("metadata key %q present; want absent (zero-behavior rule)", key)
				}
			}
		})
	}
}

// TestTierFloorErrorClassifiedTerminal: the floor error is NonRetryable,
// reachable via errors.Is through wraps, and the bare sentinel matches by
// identity.
func TestTierFloorErrorClassifiedTerminal(t *testing.T) {
	wrapped := fmt.Errorf("rotation context: %w", &TierFloorError{
		ProviderID: "prov-1",
		ModelID:    "model-1",
		Depth:      3,
		Limit:      2,
		Cause:      ErrEmptyResponse,
	})
	if !errors.Is(wrapped, ErrTierFloor) {
		t.Error("errors.Is(wrapped, ErrTierFloor) = false; want true")
	}
	var nonRetryable NonRetryableError = &TierFloorError{}
	if !nonRetryable.NonRetryable() {
		t.Error("TierFloorError must be NonRetryable (classified terminal)")
	}
	if !IsTierFloorError(wrapped) {
		t.Error("IsTierFloorError(wrapped) = false; want true")
	}
	if !strings.Contains(wrapped.Error(), "tier degradation floor") {
		t.Errorf("Error() = %q, want the sentinel text", wrapped.Error())
	}
}

// TestTierPolicyEffectiveDepth: MaxDepth<=0 normalizes to the default 2
// when enabled (DefaultTierPolicyMaxDepth from the config schema contract).
func TestTierPolicyEffectiveDepth(t *testing.T) {
	p := TierPolicy{Enabled: true}
	if got := p.EffectiveDepth(); got != DefaultTierDepth {
		t.Errorf("EffectiveDepth() = %d, want %d (default when MaxDepth unset)", got, DefaultTierDepth)
	}
	p.MaxDepth = 5
	if got := p.EffectiveDepth(); got != 5 {
		t.Errorf("EffectiveDepth() = %d, want 5", got)
	}
}

// TestTierUnlimitedWhenDisabled: with the policy off the rotation is
// byte-identical to pre-leaf behavior - all 4 failing members dispatch and
// the plain all-failed error surfaces (NOT the floor error).
func TestTierUnlimitedWhenDisabled(t *testing.T) {
	allFail := func() *tierStubChatter { return &tierStubChatter{err: ErrEmptyResponse} }
	m1, m2, m3, m4 := allFail(), allFail(), allFail(), allFail()
	pm, chatters := newTierTestManager(t, nil, []*tierStubChatter{m1, m2, m3, m4}, []map[string]bool{nil, nil, nil, nil})

	_, err := pm.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("expected the all-failed error")
	}
	if errors.Is(err, ErrTierFloor) {
		t.Error("disabled policy must never surface ErrTierFloor")
	}
	if got := totalTierDispatches(chatters); got != 4 {
		t.Errorf("dispatched members = %d, want 4 (unbounded rotation when disabled)", got)
	}
}
