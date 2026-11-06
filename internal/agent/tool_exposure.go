package agent

// Token-efficiency branch 03 leaf 05: the as-needed tool-exposure engine
// (docs/research/dynamic-tools-design.md §3.1). This file defines the
// pure-logic tier (mode resolution + frontier-class check) and the
// per-episode ExposurePlan type. The warn-once gate that consumes
// EffectiveExposureMode lives in loop.go (applyExposureGateLocked,
// called from ApplyRequestModel). The filtered-registry payload
// construction and tool_view activation hook are leaf 06/07.

import (
	"context"
	"fmt"
	"sync"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// ToolExposureMode is the effective tool-exposure tier for a turn.
type ToolExposureMode string

// The four tiers (design §3.1). "full" and "indexed" are the legacy
// loop-economics modes; "predicted" and "search" are the as-needed
// modes gated on the dynamic_tool_exposure flag + the frontier
// capability.
const (
	ExposureFull      ToolExposureMode = "full"
	ExposureIndexed   ToolExposureMode = "indexed"
	ExposurePredicted ToolExposureMode = "predicted"
	ExposureSearch    ToolExposureMode = "search"
)

// IsFrontierClass reports whether mc carries the "frontier" capability
// tag (llm.CapFrontier). A nil config or a model without the tag is
// NOT frontier-class (fail-safe: unknown models are treated as
// non-frontier, so search downgrades to predicted for them — design
// §5.1 "unknown/unclassified is non-frontier").
func IsFrontierClass(mc *llm.ModelConfig) bool {
	if mc == nil {
		return false
	}
	return mc.HasCapability(llm.CapFrontier)
}

// EffectiveExposureMode resolves the tool-exposure mode for a turn.
// Resolution order (design §3.4):
//
//	per-model ToolExposure (models.json5 model entry)
//	  > per-agent frontmatter (AgentSpec.ToolExposure)
//	  > global [agent.tools].tool_exposure
//	  > default: "predicted" (flag on) / "indexed" (flag off)
//
// The capability gate then downgrades configured "search" to
// "predicted" for non-frontier models (the warn-once is emitted by
// the caller, not here — this is a pure function). Flag off forces
// "indexed" (byte-identical legacy payload).
func EffectiveExposureMode(mc *llm.ModelConfig, agentMode, globalMode string, flagOn bool) ToolExposureMode {
	// Resolution order: per-model (mc.ToolExposure) > per-agent > global > default.
	mode := ""
	if mc != nil {
		mode = mc.ToolExposure
	}
	if mode == "" {
		mode = agentMode
	}
	if mode == "" {
		mode = globalMode
	}
	if mode == "" {
		if flagOn {
			mode = string(ExposurePredicted)
		} else {
			mode = string(ExposureIndexed)
		}
	}

	// Flag off: everything is indexed (legacy, byte-identical).
	if !flagOn {
		return ExposureIndexed
	}

	// Capability gate: configured "search" + non-frontier model →
	// "predicted" (design §5.2.1). The caller emits the one-time
	// downgrade warn; this function is pure.
	if ToolExposureMode(mode) == ExposureSearch && !IsFrontierClass(mc) {
		return ExposurePredicted
	}

	return ToolExposureMode(mode)
}

// ExposurePlan is the per-episode active-set of tool names that ship
// full schemas (design §3.1). It is append-only within an episode
// (Promote adds, never removes — the prefix-cache-invalidation
// invariant, §4.3). Seeded at episode start from the predicted set
// (intent map) + core always-full + the meta-tool pair.
//
// ceilingNames is the role ceiling (design §1 layer 1): the FULL
// registry scoped to this agent's granted tools. Promote refuses a
// name outside the ceiling and the error names the ceiling (§6 row 5),
// so the model knows the tool is not merely unlisted but unavailable
// to this role at all. A nil ceilingNames means "no ceiling" (the
// flag-off legacy posture: Promote never refuses on ceiling).
//
// knownNames is the FULL registry's name set at episode start (design
// §6 row 4): the executor uses IsKnown to distinguish a real registered
// tool that is simply not active yet (emit the actionable "call
// tool_view{X} first" directive) from a hallucinated / truly-unknown
// name (the generic unknown-tool error). A nil knownNames means the
// knowledge set is unset; IsKnown is then false, so row 4 only fires
// when the full registry's names were supplied at construction.
type ExposurePlan struct {
	mu           sync.Mutex
	active       []string
	ceilingNames map[string]struct{}
	knownNames   map[string]struct{}
	// enforcing is true only in the dynamic modes (search/predicted). A
	// non-enforcing plan (full/indexed) never gates execution: the active
	// set is advisory there, matching the pre-gate byte-identical posture
	// (bughunt agent-core F-1).
	enforcing bool
}

// exposurePlanCtxKey keys the per-turn ExposurePlan on the turn context
// (bughunt agent-core F-3). The plan rides the CONTEXT, not shared
// executor state: one executor field cannot adjudicate interleaved
// conversations on a session-long loop — episode A's earned promotions
// would go invisible to B's plan and vice versa. The loop arms the
// conversation's plan into the turn ctx at reasoningCycle entry; the
// executor's §6 row-4 gate reads it per call.
type exposurePlanCtxKey struct{}

// WithExposurePlan attaches the episode's plan to ctx for the duration of
// the turn. A nil plan leaves ctx untouched (legacy posture: the row-4
// execution gate stays off).
func WithExposurePlan(ctx context.Context, plan *ExposurePlan) context.Context {
	if ctx == nil || plan == nil {
		return ctx
	}
	return context.WithValue(ctx, exposurePlanCtxKey{}, plan)
}

// exposurePlanFromContext returns the plan the turn carries, or nil when
// the execution gate is off (flag off, legacy mode, or a non-loop caller).
func exposurePlanFromContext(ctx context.Context) *ExposurePlan {
	if ctx == nil {
		return nil
	}
	p, _ := ctx.Value(exposurePlanCtxKey{}).(*ExposurePlan)
	return p
}

// metaTools is the always-active meta-tool pair (design §1 layer 3): the
// discoverer + the expander. In dynamic modes they are always full, so a
// stubbed discoverer is undiscoverable and the pair must be active so the
// model can reach a non-core tool.
var metaTools = []string{"tool_search", "tool_view"}

// NewExposurePlan builds the episode's active set for the given mode.
// ceiling is the role ceiling (design §1 layer 1): the set of tool
// names this agent's role may ever activate. A nil ceiling means
// "no ceiling" (the flag-off legacy posture). In search mode the
// predicted set is empty (budget 0) so only core always-full + the
// meta pair (tool_search, tool_view) are active. In predicted mode
// the caller passes the intent-selected set; the core always-full set
// and the meta pair are unioned in. knownNames is the FULL registry's
// name set (design §6 row 4) so the executor can distinguish a real
// registered tool that is not active yet from a genuinely unknown name.
func NewExposurePlan(mode ToolExposureMode, predicted []string, budget int, ceiling []string, knownNames []string) *ExposurePlan {
	p := &ExposurePlan{}
	if len(ceiling) > 0 {
		p.ceilingNames = make(map[string]struct{}, len(ceiling))
		for _, c := range ceiling {
			p.ceilingNames[c] = struct{}{}
		}
	}
	if len(knownNames) > 0 {
		p.knownNames = make(map[string]struct{}, len(knownNames))
		for _, n := range knownNames {
			p.knownNames[n] = struct{}{}
		}
	}
	// Core always-full tools are active in every dynamic mode.
	core := config.DefaultAlwaysFullTools()
	switch mode {
	case ExposureSearch:
		// No predicted tools; only core + the meta pair.
		p.enforcing = true
		p.active = append(append([]string{}, core...), metaTools...)
	case ExposurePredicted:
		// Predicted set (capped at budget) + core + meta pair.
		if len(predicted) > budget && budget > 0 {
			predicted = predicted[:budget]
		}
		p.enforcing = true
		p.active = append(append([]string{}, predicted...), core...)
		p.active = append(p.active, metaTools...)
	default:
		// Full or indexed: no filtering; the plan is a no-op.
		p.active = nil
	}
	return p
}

// Enforcing reports whether this plan hard-gates tool execution (dynamic
// modes only). Full/indexed plans never gate: the active set there is
// advisory, matching the pre-gate posture (bughunt agent-core F-1).
func (p *ExposurePlan) Enforcing() bool {
	return p != nil && p.enforcing
}

// Promote adds a tool name to the active set (append-only, design §3.1/§4.3).
// Returns nil if the tool is already active. §6 row 5: a name OUTSIDE the
// role ceiling is refused with an error that names the ceiling (the model
// learns the tool is not merely unlisted but unavailable to this role at
// all). A nil ceiling (flag off) disables the refusal — every name is
// allowed, the legacy posture.
func (p *ExposurePlan) Promote(name string) error {
	if p == nil {
		return fmt.Errorf("exposure: nil plan")
	}
	if name == "" {
		return fmt.Errorf("exposure: cannot promote empty tool name")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, n := range p.active {
		if n == name {
			return nil // already active
		}
	}
	if p.ceilingNames != nil {
		if _, ok := p.ceilingNames[name]; !ok {
			return fmt.Errorf("exposure: cannot activate %q: outside this agent's role ceiling (%d tools)", name, len(p.ceilingNames))
		}
	}
	p.active = append(p.active, name)
	return nil
}

// IsActive reports whether name is in the episode's active set. The executor's
// §6 row 4 error-shaping uses this to distinguish a known-but-inactive tool
// (emit the actionable "call tool_view{X} first" directive) from a genuinely
// unknown one (the generic unknown-tool error). A nil plan returns false,
// so the flag-off path never triggers the active-set error branch.
func (p *ExposurePlan) IsActive(name string) bool {
	if p == nil || name == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, n := range p.active {
		if n == name {
			return true
		}
	}
	return false
}

// IsKnown reports whether name is registered in the FULL registry (regardless
// of active state). Used by the executor's §6 row 4 branch: a known tool that
// is NOT active gets the actionable "call tool_view{X} first" error; an
// unknown name falls through to the legacy "unknown tool: X" error. A nil
// plan or unset known set returns false (the flag-off posture).
func (p *ExposurePlan) IsKnown(name string) bool {
	if p == nil || name == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.knownNames[name]
	return ok
}

// Active returns the current active set (a copy; safe to iterate).
func (p *ExposurePlan) Active() []string {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.active))
	copy(out, p.active)
	return out
}
