package agent

// RED tests for leaf 03-membership/01-frontmatter step 5 (contract C4):
// AgentSpec.Groups and its mergeSpec semantics.
//
// Written BEFORE the implementation: AgentSpec.Groups does not exist yet,
// so every failure below is a missing-symbol compile error ("field absent"),
// never a syntax error. GREEN state: leaf 01 step 4 (definitionToSpec maps
// metadata.Groups -> spec.Groups) and step 5 (mergeSpec SCALAR-REPLACES,
// documented against the model_alias precedent) make all of these pass
// without modification.
//
// Pinned merge contract: when an AGENT.md re-load merges into an existing
// spec, a non-empty override groups list REPLACES the base list wholesale —
// it is NOT unioned (contrast AdditionalTools/Intents). An absent override
// (empty list) preserves the base list.

import (
	"slices"
	"testing"

	"github.com/caimlas/meept/internal/agents"
)

// baseSpecWithGroups builds a minimal spec carrying a base groups list,
// mirroring what definitionToSpec produces once step 4 lands.
func baseSpecWithGroups(t *testing.T) *AgentSpec {
	t.Helper()
	reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
	text := `---
id: mergeg
name: MergeG
role: executor
groups: [base-a, base-b]
---

Body.
`
	def, err := agents.ParseAgentText(text)
	if err != nil {
		t.Fatalf("ParseAgentText: %v", err)
	}
	return reg.definitionToSpec(def)
}

// T2a (leaf 01): definitionToSpec maps frontmatter groups onto the spec.
func TestDefinitionToSpec_Groups(t *testing.T) {
	spec := baseSpecWithGroups(t)
	if !slices.Equal(spec.Groups, []string{"base-a", "base-b"}) {
		t.Fatalf("AgentSpec.Groups = %v, want [base-a base-b]: definitionToSpec must map AgentMetadata.Groups (step 4 of the C4 pipeline)", spec.Groups)
	}
}

// T2b (leaf 01): mergeSpec SCALAR-REPLACES groups — the override AGENT.md's
// list replaces the base list wholesale, never a union. This is the
// model_alias precedent, applied to groups.
func TestSpecMerge_GroupsOverrideReplacesNotUnions(t *testing.T) {
	reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
	base := baseSpecWithGroups(t)

	override, err := agents.ParseAgentText(`---
id: mergeg
name: MergeG
role: executor
groups: [override-only]
---

Body.
`)
	if err != nil {
		t.Fatalf("ParseAgentText: %v", err)
	}

	merged := reg.mergeSpec(base, override)
	if !slices.Equal(merged.Groups, []string{"override-only"}) {
		t.Errorf("merged Groups = %v, want exactly [override-only]: mergeSpec must scalar-replace groups (model_alias precedent), NOT union with base [base-a base-b]", merged.Groups)
	}
	if slices.Contains(merged.Groups, "base-a") || slices.Contains(merged.Groups, "base-b") {
		t.Errorf("merged Groups = %v leaks base members: the override replaces, it never unions", merged.Groups)
	}
}

// T2c (leaf 01): an override that omits groups: preserves the base list.
func TestSpecMerge_GroupsOmittedPreservesBase(t *testing.T) {
	reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
	base := baseSpecWithGroups(t)

	override, err := agents.ParseAgentText("---\nid: mergeg\nname: MergeG\nrole: executor\n---\n\nBody.\n")
	if err != nil {
		t.Fatalf("ParseAgentText: %v", err)
	}

	merged := reg.mergeSpec(base, override)
	if !slices.Equal(merged.Groups, []string{"base-a", "base-b"}) {
		t.Errorf("merged Groups = %v, want the base [base-a base-b] preserved when the override omits groups:", merged.Groups)
	}
}
