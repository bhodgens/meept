package agent

import (
	"context"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/session"
)

// Dispatcher-side memory-scope wiring (leaf 03-agent-recall-wiring).
//
// The dispatcher owns recall for the chat/dispatch lane: buildMemoryContext
// (pre-routing, classification context) and RouteToAgent (prompt-facing
// context block). The session identity (bound project + OwnerID) comes from
// the session store; the agent scope comes from the routed spec. The
// multiuser bit rides SetMultiUserScopeEnabled from daemon wiring
// (cfg multiuser.enabled); it is OR-ed with the resolved identity so a
// resolution gap under enabled multiuser fails CLOSED (Flag 3).

// SetMultiUserScopeEnabled wires the daemon-wide multiuser.enabled bit onto
// the dispatcher. Nil-safe per the setter convention. Written once at
// daemon wiring, before any dispatch traffic; read without a lock is safe
// for this write-once-then-read pattern.
func (d *Dispatcher) SetMultiUserScopeEnabled(enabled bool) {
	if d == nil {
		return
	}
	d.multiUserEnabled = enabled
}

// lookupSession resolves a session row by conversation id, primary id, or
// thread-scoped id (the same dual-lookup + thread unwrap the working-dir
// resolution uses). Nil when nothing resolves.
func (d *Dispatcher) lookupSession(sessionID string) *session.Session {
	if d.sessionStore == nil || sessionID == "" {
		return nil
	}
	sess := session.ResolveThreadConversationID(sessionID,
		d.sessionStore.GetByConversationID, d.sessionStore.Get)
	if sess == nil {
		return nil
	}
	return sess
}

// sessionScopeOptions resolves the B1 tuple for (agent spec, session) and
// folds it into C2 search options for the Manager surfaces. No memory
// manager wired = nil (unfiltered legacy). The agent-level "global"
// declaration maps to the user purview exactly like the loop-side
// memoryScopeOptions (chief-of-staff spans projects; the committed
// predicate treats a global CALLER as identity-less).
//
// The group ceiling rides the same resolution (agent-groups leaf 05 T2,
// C7): the spec's Resolved record is consulted through the registry's
// resolveForSpec — the SAME record stamped onto the loop at creation — so
// the dispatcher lane recalls under the effective (narrowed) ceiling and
// groups NEVER widen here either. An ungrouped spec resolves an
// unmanaged record with an empty ceiling: behavior is byte-identical to
// the post-#70 baseline.
func (d *Dispatcher) sessionScopeOptions(agentID, sessionID string) []memory.SearchOption {
	if d.memoryMgr == nil {
		return nil
	}
	spec, _ := d.registry.GetSpec(agentID)

	var projectID, userID string
	if sess := d.lookupSession(sessionID); sess != nil {
		projectID = sess.ProjectID
		userID = sess.OwnerID
	}

	// multiuser.enabled is wired once at daemon startup, before any
	// dispatch traffic; the plain read below is safe for this
	// write-once-then-read pattern.
	multiUserCfg := d.multiUserEnabled

	var ceiling string
	if res := d.registry.resolveForSpec(spec); res != nil {
		ceiling = res.MemoryMaxScope
	}

	maxScope, proj, uid, multi := resolveScopeInputsWithCeiling(spec, ceiling, projectID, userID)
	return memoryScopeOptions(maxScope, proj, uid, multi || multiUserCfg)
}

// rescopeMemoryContextForAgent re-runs the prompt-facing recall under the
// ROUTED agent's scope (RouteToAgent, after agent resolution). The
// Dispatch-stage search was filtered at the conservative chat-lane scope;
// an agent with a wider declared MemoryMaxScope (chief-of-staff) needs its
// own pass, and a narrower one must never see more than the default.
// Memory content never reached any prompt before this point.
func (d *Dispatcher) rescopeMemoryContextForAgent(ctx context.Context, result *DispatchResult, loop *AgentLoop, conversationID string) {
	if d.memoryMgr == nil || result == nil || result.OriginalInput == "" {
		return
	}
	opts := d.sessionScopeOptions(result.AgentID, conversationID)
	results, err := d.memoryMgr.Search(ctx, memory.MemoryQuery{
		Query:    result.OriginalInput,
		Limit:    5,
		MatchAny: true,
	}, opts...)
	if err != nil {
		d.logger.Debug("scoped memory re-search failed; keeping dispatch-stage results",
			"error", err,
		)
		return
	}
	result.MemoryContext = results
	// Recall telemetry (leaf 04 T2): this re-scope IS the prompt-facing
	// recall — what it returns is what buildContextMessage renders. Stage
	// the injected-item snapshot on the executing loop's conversation so
	// the turn.terminal subscriber can drain it (nil loop / conversation:
	// skip, telemetry never blocks routing).
	if loop != nil {
		d.recallOutcomeSnapshot(result, loop, conversationID, results)
	}
}

// chatAgentScopeOptions is the conservative chat-lane filter used at
// Dispatch step 3 (documented at the buildMemoryContext call site).
func (d *Dispatcher) chatAgentScopeOptions(sessionID string) []memory.SearchOption {
	return d.sessionScopeOptions(config.AgentIDChat, sessionID)
}
