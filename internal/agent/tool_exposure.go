package agent

// Token-efficiency branch 03 leaf 05: the as-needed tool-exposure engine
// (docs/research/dynamic-tools-design.md §3.1). This file defines the
// pure-logic tier (mode resolution + frontier-class check) and the
// per-episode ExposurePlan type. The warn-once gate that consumes
// EffectiveExposureMode lives in loop.go (applyExposureGateLocked,
// called from ApplyRequestModel). The filtered-registry payload
// construction and tool_view activation hook are leaf 06/07.

import (
	"fmt"
	"sync"

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
// TODO(dynamic-tools 06): NewExposurePlan is wired into the loop's
// iteration site (GetDefinitions + StabilizeToolPrefix) in leaf 06;
// this leaf (05) provides the type + the warn-once gate.
type ExposurePlan struct {
	mu      sync.Mutex
	active  []string
	ceiling int
}

// NewExposurePlan builds the episode's active set for the given mode.
// In search mode, the predicted set is empty (budget 0) so only core
// always-full + the meta pair (tool_search, tool_view) are active.
// In predicted mode, the caller passes the intent-selected set via
// the IntentMap.For result; the core + meta are unioned in.
//
// m may be nil (no intent map loaded) → the predicted set is empty
// and only core + meta are active.
func NewExposurePlan(mode ToolExposureMode, predicted []string, budget int) *ExposurePlan {
	p := &ExposurePlan{}
	switch mode {
	case ExposureSearch:
		// No predicted tools; only the meta pair.
		p.active = []string{"tool_search", "tool_view"}
	case ExposurePredicted:
		// Predicted set (capped at budget) + meta pair.
		if len(predicted) > budget && budget > 0 {
			predicted = predicted[:budget]
		}
		p.active = append(append([]string{}, predicted...), "tool_search", "tool_view")
	default:
		// Full or indexed: no filtering; the plan is a no-op.
		p.active = nil
	}
	return p
}

// Promote adds a tool name to the active set (append-only). Returns
// nil if the tool is already active. Returns an error for an empty
// name or when the set is at its ceiling.
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
	if p.ceiling > 0 && len(p.active) >= p.ceiling {
		return fmt.Errorf("exposure: cannot promote %q: at ceiling (%d)", name, p.ceiling)
	}
	p.active = append(p.active, name)
	return nil
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
