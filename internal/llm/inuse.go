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
// BuildModelsInUse. Callers adapt from config.AgentDefinition.
type AgentModelRef struct {
	Model   string
	Enabled bool
}

// BuildModelsInUse computes the set of "provider/model" identifiers that
// should gate local runtime startup at daemon boot. Sources, in order:
//  1. enabled agent definitions (agent.Model in provider/model form)
//  2. the four models.json5 slots (model, small_model, classifier_model,
//     summarizer_model)
//  3. alias expansion (single-level): for any added value that names an
//     alias, each model in that alias's Models list is also included
//  4. disabled-providers filter: any model whose provider appears in
//     `disabled` is removed from the set.
//
// Values without a "/" separator are skipped (with a debug log) since they
// cannot be matched against provider/model-key form.
func BuildModelsInUse(
	agents []AgentModelRef,
	slots ModelSlots,
	aliases map[string]ModelAliasEntry,
	disabled []string,
) map[string]struct{} {
	if agents == nil && slots == (ModelSlots{}) {
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

	// 3. Alias expansion (single-level).
	snapshot := make([]string, 0, len(out))
	for k := range out {
		snapshot = append(snapshot, k)
	}
	for _, ref := range snapshot {
		alias, ok := aliases[ref]
		if !ok {
			continue
		}
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
