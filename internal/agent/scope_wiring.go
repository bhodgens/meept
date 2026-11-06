package agent

import (
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/tools"
)

// Loop-side scope wiring (leaf 03-agent-recall-wiring T2/T3). The loop
// resolves its B1 scope tuple once per use from its spec + session identity
// and threads it into (a) recall filters and (b) the tool-execution context
// so the memory tools stamp/filter with the SAME identity the loop recalls
// under (three-site rule: template loop, registry loops, and task loops all
// funnel through executeToolCallsUngated / buildMemoryContext).
//
// Session identity inputs:
//   - projectID: l.projectID — the per-session bound project (ChatHandler
//     sessionLoop + Dispatcher.resolveAgent both SetProjectID from the
//     session row). NEVER the daemon CWD (AGENTS.md).
//   - userID: l.scopeUserID — the session's OwnerID (auth user id), set by
//     the same two sites. Empty in single-user installs.
//   - multiUser: l.multiUserEnabled (cfg multiuser.enabled) OR the resolved
//     userID being non-empty. OR-ing the config bit is the Flag-3
//     fail-closed guarantee: multiuser enabled + a resolution gap (empty
//     OwnerID) must hide user-scope rows, not match the empty userID.

// SetScopeUserID wires the serving session's multiuser identity (OwnerID).
// Nil-safe per the setter convention.
func (l *AgentLoop) SetScopeUserID(uid string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.scopeUserID = uid
}

// SetMultiUserScopeEnabled wires the daemon-wide multiuser.enabled bit.
// Nil-safe per the setter convention.
func (l *AgentLoop) SetMultiUserScopeEnabled(enabled bool) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.multiUserEnabled = enabled
}

// resolveMemoryScope resolves the loop's B1 tuple with the group ceiling
// folded in (agent-groups leaf 05 T2, C7): the ceiling rides the
// resolveScopeInputs path itself via EffectiveMemoryScope, so every
// consumer of this tuple (recall filter, tool-execution ScopeIdentity,
// memory tools) sees the SAME narrowed scope — groups NEVER widen.
func (l *AgentLoop) resolveMemoryScope() (memory.Scope, string, string, bool) {
	l.mu.RLock()
	spec := l.spec
	projectID := l.projectID
	userID := l.scopeUserID
	multiUserCfg := l.multiUserEnabled
	var ceiling string
	if l.groupResolution != nil {
		ceiling = l.groupResolution.MemoryMaxScope
	}
	l.mu.RUnlock()

	maxScope, proj, uid, multi := resolveScopeInputsWithCeiling(spec, ceiling, projectID, userID)
	return maxScope, proj, uid, multi || multiUserCfg
}

// memoryScopeOptions folds the loop's resolved tuple into C2 search options
// for the memory.Manager surfaces. The agent-level "global" declaration
// (chief-of-staff) maps to the filter's USER purview: the committed C2
// predicate treats a global-scope CALLER as identity-less (only global
// rows), while the D2 chief-of-staff contract requires cross-project reach.
// A user-purview caller sees every project row plus user rows (matched to
// its identity when multiuser is on, all of them in single-user mode) —
// exactly the span an explicitly-declared global agent is authorized for.
// Project/user declarations map verbatim.
func memoryScopeOptions(maxScope memory.Scope, projectID, userID string, multiUser bool) []memory.SearchOption {
	filterScope := maxScope
	switch maxScope {
	case memory.ScopeGlobal:
		// Chief-of-staff: span projects via the user purview (see doc).
		filterScope = memory.ScopeUser
	case memory.ScopeProject, memory.ScopeUser:
		// Verbatim.
	default:
		// Unknown (cannot happen post-resolveScopeInputs): fail closed to
		// the narrowest purview.
		filterScope = memory.ScopeProject
	}
	return []memory.SearchOption{
		memory.WithScopeFilter(filterScope, projectID, userID),
		memory.WithMultiUserEnabled(multiUser),
	}
}

// memoryScopeCtxIdentity resolves the tuple and packages it for the
// tool-execution context (memory tools stamp/filter from it).
func (l *AgentLoop) memoryScopeCtxIdentity() tools.ScopeIdentity {
	maxScope, projectID, userID, multiUser := l.resolveMemoryScope()
	return tools.ScopeIdentity{
		MaxScope:  string(maxScope),
		ProjectID: projectID,
		UserID:    userID,
		MultiUser: multiUser,
	}
}
