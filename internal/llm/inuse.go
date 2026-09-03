package llm

import (
	"log/slog"
	"strings"
)

// ModelSlots bundles the four slot fields from ProvidersConfig / models.json5.
type ModelSlots struct {
	Model           string
	SmallModel      string
	ClassifierModel string
	SummarizerModel string
}

// AgentModelRef is a minimal view of an agent definition used by
// BuildModelsInUse. Callers adapt from agents.AgentMetadata.
type AgentModelRef struct {
	Model   string
	Enabled bool
}

// BuildModelsInUse computes the set of "provider/model" identifiers that
// should gate local runtime startup at daemon boot. Sources, in order:
//  1. enabled agent definitions (agent.Model in provider/model form)
//  2. the four models.json5 slots (model, small_model, classifier_model,
//     summarizer_model)
//  3. alias expansion: every configured model alias contributes ALL of its
//     members to the set. Alias members are explicitly configured failover
//     targets, so their endpoints pre-warm at boot instead of being found
//     dead on first failover. Slot/agent values may name an alias by bare
//     name (e.g. classifier_model: "classifier"); such names cannot enter
//     the provider/model set themselves, but expanding every configured
//     alias covers them (and provider/model-form alias keys equally).
//  4. disabled-providers filter: any model whose provider appears in
//     `disabled` is removed from the set.
//
// Values without a "/" separator are skipped (with a debug log) since they
// cannot be matched against provider/model-key form. Endpoints whose models
// appear in no agent ref, slot, or alias member remain gated off at boot.
func BuildModelsInUse(
	agents []AgentModelRef,
	slots ModelSlots,
	aliases map[string]ModelAliasEntry,
	disabled []string,
) map[string]struct{} {
	if agents == nil && slots == (ModelSlots{}) && len(aliases) == 0 {
		return nil
	}

	out := make(map[string]struct{})
	disabledSet := make(map[string]struct{}, len(disabled))
	for _, d := range disabled {
		if d != "" {
			disabledSet[d] = struct{}{}
		}
	}

	add := func(ref string) {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return
		}
		normalized := normalizeModelRef(ref)
		if normalized == "" {
			slog.Debug("Skipping non-provider/model reference in in-use set", "ref", ref)
			return
		}
		out[normalized] = struct{}{}
	}

	// 1. Agents.
	for _, agent := range agents {
		if !agent.Enabled {
			continue
		}
		add(agent.Model)
	}

	// 2. Slots.
	add(slots.Model)
	add(slots.SmallModel)
	add(slots.ClassifierModel)
	add(slots.SummarizerModel)

	// 3. Alias expansion: every configured alias's full member list is
	// included. Alias members are explicitly configured failover targets
	// (the local 8B failover endpoint is only reachable through alias
	// membership), so their runtimes pre-warm at boot instead of failing
	// over onto a dead endpoint. Bare alias names (e.g. slots naming
	// "classifier") carry no "/" and never enter the provider/model set,
	// so the lookup cannot be driven from set membership — iterate the
	// alias map itself. This also covers provider/model-form alias keys.
	for _, alias := range aliases {
		for _, m := range alias.Models {
			add(m)
		}
	}

	// 4. Disabled-providers filter.
	for ref := range out {
		providerID, _, hasSlash := strings.Cut(ref, "/")
		if !hasSlash {
			continue
		}
		if _, disabled := disabledSet[providerID]; disabled {
			delete(out, ref)
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// normalizeModelRef returns the value in "provider/model" form if it contains
// a "/", otherwise an empty string (signalling the caller should skip it).
// The first "/" splits provider from model key; everything after is the
// model key verbatim.
func normalizeModelRef(ref string) string {
	idx := strings.Index(ref, "/")
	if idx <= 0 || idx == len(ref)-1 {
		return ""
	}
	return ref
}
