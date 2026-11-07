package groups

// RED tests for leaf 03-membership/02-user-groups T3 (contract C4, join
// rule): EffectiveGroups — the deterministic agent-groups ∪ session-owner
// user-groups join.
//
// Written BEFORE the implementation: EffectiveGroups does not exist yet, so
// every failure below is a missing-symbol compile error ("fn absent"), never
// a syntax error. GREEN state: leaf 02 T3 lands memberjoin.go and these pass
// without modification.
//
// Pinned join rule (C4):
//   - nil identity (single-user install): agentGroups only — the caller
//     decides whether to Warn; the helper never invents membership.
//   - identity with user groups (multiuser): union(agentGroups, userGroups)
//     with dedup and STABLE, deterministic order.
//   - identity present but no lookup result: user contribution is EMPTY
//     (fail-safe) — never synthesized from the key string (issue #76 class;
//     there is deliberately NO parameter that could receive one).

import (
	"slices"
	"testing"

	"github.com/caimlas/meept/internal/auth"
)

// M1: nil identity — single-user install — returns the agent groups alone.
func TestEffectiveGroups_NilIdentityAgentOnly(t *testing.T) {
	lookup := func(*auth.Identity) []string {
		t.Fatal("storeLookup must not be called for a nil identity (no synthesis, no lookup side effects)")
		return nil
	}
	got := EffectiveGroups([]string{"coders", "oncall"}, nil, lookup)
	if !slices.Equal(got, []string{"coders", "oncall"}) {
		t.Errorf("EffectiveGroups(agent=[coders oncall], nil identity) = %v, want [coders oncall] unchanged", got)
	}
}

// M2: identity present, owner carries groups — union of both sources.
func TestEffectiveGroups_IdentityUnionsOwnerGroups(t *testing.T) {
	lookup := func(*auth.Identity) []string { return []string{"admins", "auditors"} }
	got := EffectiveGroups([]string{"coders", "oncall"}, &auth.Identity{UserID: "u-1"}, lookup)
	for _, g := range []string{"coders", "oncall", "admins", "auditors"} {
		if !slices.Contains(got, g) {
			t.Errorf("EffectiveGroups = %v, want %q present (agent ∪ owner union)", got, g)
		}
	}
}

// M3: dedup — a group held by BOTH sources appears exactly once.
func TestEffectiveGroups_DedupsAcrossSources(t *testing.T) {
	lookup := func(*auth.Identity) []string { return []string{"oncall", "admins"} }
	got := EffectiveGroups([]string{"coders", "oncall"}, &auth.Identity{UserID: "u-1"}, lookup)
	counts := map[string]int{}
	for _, g := range got {
		counts[g]++
	}
	for g, n := range counts {
		if n != 1 {
			t.Errorf("group %q appears %d times in %v, want 1 (union dedup)", g, n, got)
		}
	}
}

// M4: stable, deterministic order — same inputs always produce the same
// sequence, regardless of map iteration inside the implementation.
func TestEffectiveGroups_StableOrder(t *testing.T) {
	lookup := func(*auth.Identity) []string { return []string{"zeta", "alpha", "mid"} }
	first := EffectiveGroups([]string{"coders", "oncall"}, &auth.Identity{UserID: "u-1"}, lookup)
	for i := 0; i < 50; i++ {
		again := EffectiveGroups([]string{"coders", "oncall"}, &auth.Identity{UserID: "u-1"}, lookup)
		if !slices.Equal(first, again) {
			t.Fatalf("EffectiveGroups order is not stable: run 0 = %v, run %d = %v", first, i, again)
		}
	}
	if !slices.IsSorted(first) {
		t.Errorf("EffectiveGroups = %v, want sorted (stable order contract: sorted+deduped)", first)
	}
}

// M5: identity present but the lookup finds nothing — user contribution is
// EMPTY, the agent groups stand, and nothing is invented.
func TestEffectiveGroups_IdentityWithEmptyContribution(t *testing.T) {
	lookup := func(*auth.Identity) []string { return nil }
	got := EffectiveGroups([]string{"coders"}, &auth.Identity{UserID: "u-unknown"}, lookup)
	if !slices.Equal(got, []string{"coders"}) {
		t.Errorf("EffectiveGroups = %v, want [coders]: an identity with no stored groups contributes nothing (fail-safe), never synthesized membership", got)
	}
}

// M6: no key-string synthesis — the helper takes no key material at all.
// The lookup seam receives the resolved Identity, never the raw key; a
// call that tried to pass key material would not compile. Pinned here by
// asserting the lookup argument is the identity we handed in.
func TestEffectiveGroups_LookupReceivesIdentityNotKey(t *testing.T) {
	want := &auth.Identity{UserID: "u-9", KeyID: "k-1"}
	var got *auth.Identity
	lookup := func(id *auth.Identity) []string {
		got = id
		return nil
	}
	_ = EffectiveGroups(nil, want, lookup)
	if got != want {
		t.Fatalf("storeLookup received %v, want the exact identity passed in: the join resolves through the Identity, never the key string (issue #76)", got)
	}
}
