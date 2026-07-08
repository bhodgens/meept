package daemon

import (
	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/skills/lifecycle"
)

// reflectionProposerAdapter bridges *agent.ReflectionCollector to the
// lifecycle.ReflectionProposer interface. It exists in the daemon package
// (not in agent or lifecycle) to avoid an import cycle: agent already
// imports lifecycle transitively, so lifecycle cannot import agent. The
// daemon imports both and adapts at the boundary.
//
// The adapter converts agent.ReflectionProposal values to
// lifecycle.ReflectionProposal values field-by-field. This is a thin
// mapping — the field names and semantics are intentionally identical —
// but keeping the types separate means lifecycle never depends on agent.
type reflectionProposerAdapter struct {
	rc *agent.ReflectionCollector
}

// DrainPending delegates to ReflectionCollector.DrainPendingProposals and
// converts each agent.ReflectionProposal into a lifecycle.ReflectionProposal.
// Returns nil, nil when the collector has no queue configured (e.g., test
// constructs that bypass NewReflectionCollector) — the evolver treats this
// as "no proposals this cycle".
func (a *reflectionProposerAdapter) DrainPending() ([]lifecycle.ReflectionProposal, error) {
	if a == nil || a.rc == nil {
		return nil, nil
	}
	raw, err := a.rc.DrainPendingProposals()
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]lifecycle.ReflectionProposal, len(raw))
	for i, p := range raw {
		out[i] = lifecycle.ReflectionProposal{
			Type:          p.Type,
			Target:        p.Target,
			Change:        p.Change,
			Justification: p.Justification,
			Confidence:    p.Confidence,
		}
	}
	return out, nil
}
