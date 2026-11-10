package daemon

import (
	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/skills/lifecycle"
)

// reflectionClassifierClient picks the LLM client that serves per-turn
// reflection (plan docs/plans/20261109-voice-comms-fixes leaf 03).
//
// The former wiring passed c.LLMClient — the MAIN chat client, whose
// models.json5 default ref is the cloud model — straight into the
// ReflectionCollector. Two failure modes made that a per-turn 401 storm
// on the live voice rig:
//
//  1. The main client's ModelConfig is resolved through
//     config.LoadModelsConfig, whose ExpandEnvVars consults ONLY the
//     process environment (internal/config/config.go). A launchd-spawned
//     daemon does not inherit shell-profile exports, so the cloud key in
//     "apiKey": "${...}" expanded to empty and every direct (non-failover)
//     call went unauthenticated — HTTP 401 "Token not provided" per turn.
//     The resolver path used by the auxiliary clients
//     (llm.LoadProvidersConfigDefault -> expandEnvVars, internal/llm/
//     providers.go) DOES fall through to the ~/.meept/env script, so those
//     clients carry real keys / noAuth local endpoints.
//  2. Even with a key, per-turn background reflection on the main cloud
//     default wastes cloud quota on a 500-token housekeeping call.
//
// Reflection is a generative task (render the trajectory, emit a JSON
// proposal), so it rides the summarizer chain first — a local generative
// model. The classifier chain's alias head on this class of rig is the
// prompt-router intent sidecar, which answers with intent-score JSON, not
// a reflection proposal, so the classifier client is only the second
// choice. LLMClient stays the final fallback to preserve behavior for
// installs with no auxiliary clients configured at all.
func reflectionClassifierClient(c *Components) *llm.Client {
	if c == nil {
		return nil
	}
	if c.SummarizerClient != nil {
		return c.SummarizerClient
	}
	if c.ClassifierClient != nil {
		return c.ClassifierClient
	}
	return c.LLMClient
}

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
