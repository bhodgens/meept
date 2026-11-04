package agent

import (
	"log/slog"

	"github.com/caimlas/meept/internal/memory"
)

// Agent-spec memory scope resolution (docs/plans/20261030-memory-second-brain/
// 01-scoping/03-agent-recall-wiring.md, contract B1 + C3; gate pin
// scope_inputs_gate_test.go is the frozen shape).
//
// An AgentSpec declares the WIDEST recall scope its sessions may operate at
// (MemoryMaxScope, empty = project). resolveScopeInputs turns that
// declaration plus the serving session's identity into the tuple the C2
// WithScopeFilter predicate consumes:
//
//	resolveScopeInputs(spec, projectID, userID) ->
//	    (maxScope memory.Scope, projectID, userID string, multiUser bool)
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
func resolveScopeInputs(spec *AgentSpec, projectID, userID string) (memory.Scope, string, string, bool) {
	decl := ""
	if spec != nil {
		decl = spec.MemoryMaxScope
	}

	maxScope := memory.ScopeProject
	switch decl {
	case "":
		// Default: project isolation (D2).
		maxScope = memory.ScopeProject
	case string(memory.ScopeProject), string(memory.ScopeUser), string(memory.ScopeGlobal):
		maxScope = memory.Scope(decl)
	default:
		// Fail SAFE: an unrecognized declaration narrows to project, never
		// widens to global. Warn so a typo in AGENT.md is diagnosable.
		slog.Warn("invalid agent memory_max_scope; using project scope",
			"agent", func() string {
				if spec != nil {
					return spec.ID
				}
				return ""
			}(),
			"declared", decl,
		)
		maxScope = memory.ScopeProject
	}

	return maxScope, projectID, userID, userID != ""
}

// memoryScopeCeiling narrows a resolved max scope by an optional
// group-policy ceiling (agent-groups leaf 05 C7 consumes this via its own
// narrow fn; kept package-local so the loop can apply both without a
// second vocabulary). order: project < user < global; "" (unset ceiling)
// never narrows.
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
