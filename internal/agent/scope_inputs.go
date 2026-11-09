package agent

import (
	"github.com/caimlas/meept/internal/memory"
)

// Agent-spec memory scope resolution (docs/plans/20261030-memory-second-brain/
// 01-scoping/03-agent-recall-wiring.md, contract B1 + C3; gate pin
// scope_inputs_gate_test.go is the frozen shape).
//
// An AgentSpec declares the WIDEST recall scope its sessions may operate at
// (MemoryMaxScope, empty = project). scopeIdentityInputs is the identity
// pass-through half of the resolution (project + user + multiuser bit), split
// out so the group-ceiling variant (resolveScopeInputsWithCeiling — the ONLY
// constructor scope_wiring.go consumes; the plain resolveScopeInputs wrapper
// was deleted as deadcode wave-1, zero callers at HEAD) reuses the exact
// same identity semantics.
//
// Precedence per B1: maxScope from the agent declaration (fail-SAFE:
// invalid values land on project, never global); projectID is the
// session's bound project ("" when unbound — an unbound session under a
// project max recalls NOTHING project-scoped; global + own-user rows stay
// recallable). The daemon CWD is never consulted anywhere in this chain
// (AGENTS.md: os.Getwd() is not a sanctioned fallback).
//
// The multiUser return is the Flag-3 carrier: the WithScopeFilter predicate
// must distinguish "empty userID because multiuser is disabled" (empty
// matches) from "empty userID under multiuser" (fail closed). A resolved
// identity is only ever non-empty when multiuser is enabled, so the bit
// rides as multiUser = userID != "" at this layer; loop/dispatcher callers
// OR in their config-level multiuser.enabled bit at the WithMultiUserEnabled
// call site (scope_wiring.go) so a resolution GAP under enabled multiuser
// still fails closed.
// scopeIdentityInputs is the identity pass-through half of the scope
// resolution (project + user + multiuser bit), consumed by
// resolveScopeInputsWithCeiling.
func scopeIdentityInputs(projectID, userID string) (string, string, bool) {
	return projectID, userID, userID != ""
}

// resolveScopeInputsWithCeiling resolves the B1 tuple with the group
// MemoryMaxScope ceiling folded in (agent-groups leaf 05 T2, C7):
// effective = min(agent declaration, group ceiling); groups NEVER widen.
// The identity legs are byte-identical to resolveScopeInputs. decl "" /
// invalid still resolves through EffectiveMemoryScope's fail-safe matrix,
// so a grouped agent with a malformed AGENT.md declaration cannot have
// the typo widened by a permissive group either.
func resolveScopeInputsWithCeiling(spec *AgentSpec, ceiling, projectID, userID string) (memory.Scope, string, string, bool) {
	decl := ""
	if spec != nil {
		decl = spec.MemoryMaxScope
	}
	maxScope := EffectiveMemoryScope(decl, ceiling)
	proj, uid, multi := scopeIdentityInputs(projectID, userID)
	return maxScope, proj, uid, multi
}

// EffectiveMemoryScope narrows an agent's declared max scope by a group
// policy ceiling (agent-groups leaf 05, C7 verbatim):
//
//	effective = min(agent MemoryMaxScope, group Resolved.MemoryMaxScope)
//
// scope order project < user < global. Groups NEVER widen (T3 pin): a
// wider ceiling than the declaration is inert. agentDecl follows the B1
// vocabulary with the same fail-safe defaults resolveScopeInputs applies
// (empty = project; invalid = project, never global). groupCeiling "" =
// unrestricted (no group policy bound, or the bound policies declare no
// ceiling) and never narrows; an INVALID ceiling never narrows either —
// both fail toward the agent's own resolution rather than inventing a
// scope the agent did not declare.
func EffectiveMemoryScope(agentDecl, groupCeiling string) memory.Scope {
	maxScope := memory.ScopeProject
	switch agentDecl {
	case string(memory.ScopeUser):
		maxScope = memory.ScopeUser
	case string(memory.ScopeGlobal):
		maxScope = memory.ScopeGlobal
	default:
		// "", project, or invalid all resolve to project scope (fail-safe
		// narrow, same posture as resolveScopeInputs); maxScope is already
		// initialized to memory.ScopeProject.
	}
	return memoryScopeCeiling(maxScope, groupCeiling)
}

// memoryScopeCeiling narrows a resolved max scope by an optional
// group-policy ceiling (leaf 05 C7). order: project < user < global;
// "" (unset ceiling) and unknown values never narrow.
func memoryScopeCeiling(max memory.Scope, ceiling string) memory.Scope {
	if ceiling == "" {
		return max
	}
	var rankCeil int
	switch ceiling {
	case string(memory.ScopeProject):
		rankCeil = 0
	case string(memory.ScopeUser):
		rankCeil = 1
	case string(memory.ScopeGlobal):
		rankCeil = 2
	default:
		return max
	}
	rank := 2
	switch max {
	case memory.ScopeProject:
		rank = 0
	case memory.ScopeUser:
		rank = 1
	}
	if rankCeil < rank {
		return memory.Scope(ceiling)
	}
	return max
}
