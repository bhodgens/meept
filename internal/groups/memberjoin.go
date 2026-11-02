package groups

import (
	"sort"

	"github.com/caimlas/meept/internal/auth"
)

// EffectiveGroups joins an agent's own groups with the session-owner user's
// stored groups (contract C4, plan tree docs/plans/20261030-agent-groups,
// leaf 03-membership/02): agent effective groups = agentGroups ∪ owner user
// groups WHEN the request carries an auth-store Identity; a nil identity
// (single-user install) contributes nothing and agentGroups stand.
//
// Fail-safe rules (issue #76 class):
//   - The helper takes NO key material. Identity resolution happens
//     upstream (internal/comm/http middleware attaching the store Identity
//     to the context); the storeLookup seam receives that Identity, never a
//     raw key string. There is deliberately no parameter that could receive
//     one — no code path may synthesize identity from the key string.
//   - An identity whose lookup finds nothing contributes an EMPTY set; the
//     agent groups stand. Whether to Warn about a missing identity under
//     multiuser is the CALLER's decision (leaf 09 documents the limitation).
//
// The result is sorted and deduplicated: deterministic regardless of map
// iteration or input order, so resolution is reproducible across calls.
//
// This package must stay import-light: importing internal/auth here is the
// allowed direction (auth imports nothing internal). The agent/tooling
// packages stay out — same discipline as Resolve's C6 seam.
func EffectiveGroups(agentGroups []string, identity *auth.Identity, storeLookup func(*auth.Identity) []string) []string {
	if identity == nil {
		// Single-user install: no store identity exists; return the agent
		// groups alone. The lookup is not invoked — no lookup side effects
		// for a nil identity.
		return dedupSortGroups(agentGroups)
	}

	var userGroups []string
	if storeLookup != nil {
		userGroups = storeLookup(identity)
	}
	merged := make([]string, 0, len(agentGroups)+len(userGroups))
	merged = append(merged, agentGroups...)
	merged = append(merged, userGroups...)
	return dedupSortGroups(merged)
}

// dedupSortGroups returns the union input deduplicated and sorted (stable,
// deterministic order contract).
func dedupSortGroups(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, g := range in {
		if g == "" {
			continue // never surface an empty membership name
		}
		if _, dup := seen[g]; dup {
			continue
		}
		seen[g] = struct{}{}
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}
