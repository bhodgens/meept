package daemon

// Context-warm daemon integration (phase2-context-warm leaf 02, root
// Contracts C4/C6). The state machine lives in internal/voice/contextwarm.go;
// this file supplies the two real closures the voice.SetContextWarmRouting
// seam needs and the warm-target client resolution:
//
//   - BuildPrefix renders the prefix the imminent employee turn sends at
//     its SYSTEM+HISTORY lane: the per-employee system prompt (the voice
//     override lane of agent.AgentLoop.buildSystemPromptWithOverride —
//     persona override + guided-intake extension + global rules + moment
//     block, mirrored here because the agent builders are unexported), the
//     live conversation history (conv.GetMessages, the stored system row
//     replaced by the fresh render), and the partial as the trailing user
//     row. The turn's boundary-wrap/clock stamp only touches the tail
//     message, so the system+history prefix the provider caches stays
//     byte-identical between the warm and the turn.
//   - Warm forwards to the concrete client the voice turn actually rides:
//     the session's agent loop chatter unwrapped per call (ContextFirewall
//     -> ProviderManager primary entry -> *llm.Client), so the warm lands
//     on the same endpoint + affinity lane as the turn. The scope is the
//     session/conversation id — the voice lane sets SessionHandle.ID ==
//     ConversationID (sessionFor), and RunOnce stamps that conversation id
//     as the turn's accounting scope (loop.go turnAccountingSessionID ->
//     WithTaskScope), so prompt_cache_key + the affinity header match by
//     construction.
//
// Posture: voice.context_warm defaults false — the warmer is NOT
// constructed when disabled (zero overhead: the partial hook is guarded by
// ContextWarmRouting() != nil before it does ANY per-frame work) and the
// final/end hooks ride the nil-safe voice.Route* package seams. Partial
// text never reaches a log line above DEBUG (the warmer owns its
// emissions; these closures just pass data). No mutex is held across the
// employee sqlite read or the agent-manager reads — all run outside any
// lock, on the warmer's debounce-goroutine clock, never the frame path.

import (
	"context"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/voice"
)

// contextWarmEmployeeLookupBudget bounds the employee-definition read
// inside BuildPrefix — the same 2 s posture the turn runner's
// resolveVoiceTurnLocation lookup carries (voice_employee.go).
const contextWarmEmployeeLookupBudget = 2 * time.Second

// warmDispatcher is the warm lane every llm client class implements
// (llm.Client real; Anthropic/Codex explicit no-op twins) so the closure
// never type-switches provider classes out of scope.
type warmDispatcher interface {
	Warm(ctx context.Context, alias string, messages []llm.ChatMessage, sessionScope string)
}

// contextWarmSources are the daemon components the warmer's closures read.
// Hand-built test wirings may leave fields nil — every consumer below is
// nil-guarded and degrades to "skip the warm" (ok=false / nil dispatcher),
// never a panic.
type contextWarmSources struct {
	Loops     *agent.Manager
	Template  *agent.AgentLoop
	Employees EmployeeDefinitionResolver
	// Sessions resolves the session -> conversation id duality: the
	// real turn's accounting scope (RunOnce conversationID ->
	// WithTaskScope -> prompt_cache_key + affinity header) is the
	// session's CONVERSATION id, while the routeFrame hook hands the
	// warmer the calls.db session id. nil (hand-built wiring) = the
	// scope falls back to the session id (a mismatch costs the cache
	// hit, never the turn).
	Sessions session.Store
	// Moment is the awareness "# Moment" renderer resolver (the same seam
	// the turn runner stamps per Run); nil = no block appended.
	Moment func(employeeID string) func() string
	// Dispatch overrides the client-resolution warm path (unit tests
	// record the fires); production leaves it nil.
	Dispatch func(ctx context.Context, alias string, messages []llm.ChatMessage, scope string)
}

// newContextWarmer constructs the per-daemon warmer from the resolved C6
// knobs. A disabled config (the default) returns nil WITHOUT constructing
// anything — the zero-overhead contract is enforced at this entry point.
// The debounce window inherits the resolved global voice.turn_confirm_ms;
// 0/legacy leaves it zero so the warmer applies its 900 ms default (C6:
// global-only this wave — no employee_leg override for the knob).
// baseCtx parents every warm request so daemon shutdown cancels in-flight
// warms (the caller passes the subsystem ctx).
func (w *VoiceWiring) newContextWarmer(baseCtx context.Context, src contextWarmSources) *voice.ContextWarmer {
	if !w.cfg.ContextWarm {
		return nil
	}
	window := time.Duration(w.cfg.TurnConfirmMs) * time.Millisecond
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	warmFn := func(ctx context.Context, alias string, messages []llm.ChatMessage, scope string) {
		if src.Dispatch != nil {
			src.Dispatch(ctx, alias, messages, scope)
			return
		}
		if d := w.resolveContextWarmDispatcher(src, scope); d != nil {
			d.Warm(ctx, alias, messages, scope)
		}
	}
	return voice.NewContextWarmer(
		voice.ContextWarmConfig{
			Enabled:   true,
			MinTokens: w.cfg.ContextWarmMinTokens,
			Window:    window,
		},
		voice.ContextWarmDeps{
			Warm:        warmFn,
			BuildPrefix: w.buildContextWarmPrefix(src),
			Logger:      w.logger.With("sub", "context-warm"),
			BaseCtx:     baseCtx,
		},
	)
}

// contextWarmLoopFor finds the session's agent loop. The fast path is the
// manager's plain-key Get (agent/manager.go cacheKey collapses to the bare
// session id while the template agent is the daemon default — the voice
// template); the List() scan covers a rebound/aliased template key without
// re-implementing the hashing. Read-only — never creates a loop.
func contextWarmLoopFor(mgr *agent.Manager, sessionID string) *agent.AgentLoop {
	if mgr == nil || sessionID == "" {
		return nil
	}
	if loop, ok := mgr.Get(sessionID); ok && loop != nil {
		return loop
	}
	for _, loop := range mgr.List() {
		if loop != nil && loop.GetSessionID() == sessionID {
			return loop
		}
	}
	return nil
}

// buildContextWarmPrefix returns the warmer's prefix renderer. ok=false
// (skip; the warmer DEBUGs and counts it) on every unresolvable input: no
// session, no partial text, no loop source, or the template-persona lane —
// a system prompt built by the full PromptBuilder (memory/skill sections,
// stable-prefix ordering) cannot be mirrored cheaply from the daemon, and
// guessing would warm a DIVERGED prefix, which silently kills the cache
// hit the whole feature exists for. A miss is always safe: it costs the
// warm, never the turn.
func (w *VoiceWiring) buildContextWarmPrefix(src contextWarmSources) voice.WarmPrefixFn {
	return func(employeeID, sessionID, partialText string) (voice.WarmPrefix, bool) {
		if sessionID == "" || strings.TrimSpace(partialText) == "" {
			return voice.WarmPrefix{}, false
		}
		loop := contextWarmLoopFor(src.Loops, sessionID)
		var cfg agent.AgentConfig
		haveCfg := false
		if loop != nil {
			cfg = loop.GetConfig()
			haveCfg = true
		} else if src.Template != nil {
			cfg = src.Template.GetConfig()
			haveCfg = true
		}
		if !haveCfg {
			return voice.WarmPrefix{}, false
		}

		// DEBUG-attribution alias: the same precedence the ledger reads —
		// the loop's persistent employee model override, then its model
		// ref. Warm serves whatever the resolved client holds; this only
		// correlates log lines.
		alias := ""
		if loop != nil {
			if a := loop.GetModelOverride(); a != "" {
				alias = a
			} else if a := loop.GetModelRef(); a != "" {
				alias = a
			}
		}

		// Persona lane: mirror loopTurnRunner.Run's per-turn stamping
		// EXACTLY — applyEmployeeDefinition OVERWRITES SystemPromptOveride
		// with the employee definition on EVERY Run (fresh or cached
		// clone), and when the definition is unresolvable/promptless it
		// writes "" = the template-persona warning path. So whenever an
		// employee id is bound, the definition is the authority: derive
		// it here (single bounded read, outside any lock — the warmer's
		// debounce goroutine, never the frame path). A definition-less
		// or lookup-failed case yields "" and the template-persona skip
		// below, which is exactly the turn's own degraded shape (that
		// turn would run the unmirrable full PromptBuilder — warming a
		// guessed prefix is worse than not warming).
		sys := cfg.SystemPromptOveride
		if employeeID != "" {
			def, ok := contextWarmEmployeeDef(src, employeeID)
			if ok {
				sys = employeeSystemPrompt(def)
				if alias == "" {
					alias = strings.TrimSpace(def.Model)
				}
			} else if loop == nil {
				return voice.WarmPrefix{}, false
			}
		}
		// Guided intake (leaf 05 order): the turn runner appends the
		// extension INTO SystemPromptOveride after the persona stamp,
		// whenever the row opts in — same here.
		if ext := guidedIntakePromptExtension(w.cfg, employeeID); ext != "" {
			if sys != "" {
				sys = sys + "\n\n" + ext
			} else {
				sys = ext
			}
		}
		if sys == "" {
			// Template-persona lane (no override, no intake extension):
			// unmirrorable, see the function doc.
			return voice.WarmPrefix{}, false
		}
		if cfg.GlobalRules != "" {
			sys = sys + "\n\n## Global Rules\n\n" + cfg.GlobalRules
		}
		if src.Moment != nil {
			if render := src.Moment(employeeID); render != nil {
				if block := render(); block != "" {
					sys = sys + "\n\n" + block
				}
			}
		}

		msgs := make([]llm.ChatMessage, 0, 8)
		msgs = append(msgs, llm.ChatMessage{Role: llm.RoleSystem, Content: sys})
		// Identity duality (root AGENTS.md): the routeFrame hook hands
		// the warmer the calls.db SESSION id; the real turn keys BOTH
		// its conversation history and its accounting scope on the
		// session's CONVERSATION id (RunOnce(ctx, prompt,
		// sess.ConversationID) -> turnAccountingSessionID ->
		// WithTaskScope -> prompt_cache_key + affinity header). Resolve
		// the pair once through the session store; a store miss falls
		// back to the session id for BOTH (history lookup and scope —
		// consistent, and the warm/turn pair stays identity-matched
		// whenever the store answers, which is the whole cache-hit
		// contract).
		convID := sessionID
		if src.Sessions != nil {
			if sess := src.Sessions.Get(sessionID); sess != nil && sess.ConversationID != "" {
				convID = sess.ConversationID
			}
		}
		if loop != nil {
			if conv := loop.GetConversation(convID); conv != nil {
				// buildMessageList puts the conversation's stored system
				// prompt at index 0 — the fresh render above replaces
				// ONLY that row. Mid-history system-role rows (the
				// [step-context] anchor, injected notes) are real
				// history the turn sends and must survive: dropping
				// them would diverge the prefix at row 1 and kill the
				// very cache hit this warm exists to plant.
				history := conv.GetMessages()
				if len(history) > 0 && history[0].Role == llm.RoleSystem {
					history = history[1:]
				}
				msgs = append(msgs, history...)
			}
		}
		msgs = append(msgs, llm.ChatMessage{Role: llm.RoleUser, Content: partialText})
		return voice.WarmPrefix{Messages: msgs, Alias: alias, Scope: convID}, true
	}
}

// contextWarmEmployeeDef reads the bound employee's definition (the same
// narrow resolver seam the turn runner stamps from). ok=false on any
// failure: the caller decides between "skip" (no loop to mirror) and
// "fall back to the cached config" (a transient lookup miss on a loop
// that already carries the stamped persona).
func contextWarmEmployeeDef(src contextWarmSources, employeeID string) (*employee.Employee, bool) {
	if src.Employees == nil || employeeID == "" {
		return nil, false
	}
	lookupCtx, cancel := context.WithTimeout(context.Background(), contextWarmEmployeeLookupBudget)
	defer cancel()
	def, err := src.Employees.GetEmployee(lookupCtx, employeeID)
	if err != nil || def == nil {
		return nil, false
	}
	return def, true
}

// resolveContextWarmDispatcher unwraps the session's serving chatter to the
// concrete warm-capable client, following the SAME chain the turn
// dispatches through: ContextFirewall.Inner -> ProviderManager primary
// entry -> the provider's Chatter. A direct *llm.Client chatter resolves
// immediately. Returns nil (skip) when the chain holds no warm-aware
// client — an Anthropic/Codex-terminal deployment no-ops there by C4
// contract, and an unresolvable chain never guesses an endpoint.
func (w *VoiceWiring) resolveContextWarmDispatcher(src contextWarmSources, sessionID string) warmDispatcher {
	var chatter llm.Chatter
	if loop := contextWarmLoopFor(src.Loops, sessionID); loop != nil {
		chatter = loop.Chatter()
	}
	if chatter == nil && src.Template != nil {
		chatter = src.Template.Chatter()
	}
	// Bounded unwrap: firewall -> manager -> entry; a self-referential
	// chain is a wiring bug we refuse to follow forever.
	for hop := 0; chatter != nil && hop < 4; hop++ {
		switch t := chatter.(type) {
		case *llm.ContextFirewall:
			chatter = t.Inner()
			continue
		case *llm.ProviderManager:
			entry := t.GetPrimaryProvider()
			if entry == nil {
				return nil
			}
			chatter = entry.Chatter
			continue
		}
		if d, ok := chatter.(warmDispatcher); ok {
			return d
		}
		return nil
	}
	return nil
}
