package agents

// RED tests for leaf 03-membership/01-frontmatter (contract C4, agent half):
// the `groups:` frontmatter field and its merge semantics.
//
// These tests are written BEFORE the implementation exists. They reference
// AgentMetadata.Groups (embedded into AgentDefinition), which is
// deliberately absent — every failure below must be a missing-symbol
// compile error ("field absent"), never a syntax error. When leaf 01
// lands, these go GREEN without modification.
//
// Contract being pinned (C4):
//   - AgentMetadata.Groups []string `yaml:"groups,omitempty"` parses the
//     underscore form `groups:` natively in parseMetadata.
//   - Discovery shadowing takes the higher tier's groups OUTRIGHT
//     (scalar-replacement semantics, like model_alias — NOT the additive
//     union additional_tools gets).

import (
	"log/slog"
	"os"
	"slices"
	"testing"
)

// T1a: `groups:` in AGENT.md frontmatter parses into AgentMetadata.Groups.
func TestParseAgentText_Groups(t *testing.T) {
	text := `---
id: coder
name: Coder
role: executor
groups: [coders, oncall]
---

Body.
`
	def, err := ParseAgentText(text)
	if err != nil {
		t.Fatalf("ParseAgentText: %v", err)
	}
	if len(def.Groups) != 2 {
		t.Fatalf("Groups = %v, want [coders oncall] (frontmatter groups: must parse into AgentMetadata.Groups)", def.Groups)
	}
	if !slices.Equal(def.Groups, []string{"coders", "oncall"}) {
		t.Errorf("Groups = %v, want exactly [coders oncall] in declaration order", def.Groups)
	}
}

// T1b: absent groups: leaves Groups empty (no synthesis from other fields).
func TestParseAgentText_GroupsAbsent(t *testing.T) {
	def, err := ParseAgentText("---\nid: plain\nname: Plain\nrole: executor\n---\n\nBody.\n")
	if err != nil {
		t.Fatalf("ParseAgentText: %v", err)
	}
	if len(def.Groups) != 0 {
		t.Errorf("Groups = %v, want empty when the frontmatter declares no groups:", def.Groups)
	}
}

// T3 (leaf 01): discovery shadowing takes the higher tier's groups OUTRIGHT —
// the shadowed (lower-priority) definition's groups are NOT unioned in.
// Deliberate contrast with AdditionalTools, which unions on shadowing.
func TestDiscovery_ShadowingTakesGroupsOutright(t *testing.T) {
	userDir := t.TempDir()
	bundledDir := t.TempDir()

	// Higher-priority user tier: declares its own group membership.
	writeAgentDef(t, userDir, "coder", "groups: [mine]\n")
	// Lower-priority bundled tier: carries a different membership (plus an
	// additive field to prove the shadowing path ran at all).
	writeAgentDef(t, bundledDir, "coder", "additional_tools:\n  - file_read\ngroups: [bundled-g]\n")

	d := NewDiscovery(
		WithTiers([]DiscoveryTier{{Path: userDir, Priority: PriorityUser}}),
		WithBundledPath(bundledDir),
		WithDiscoveryLogger(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))),
	)
	defs, err := d.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("got %d definitions, want 1", len(defs))
	}
	got := defs[0]

	if !slices.Equal(got.Groups, []string{"mine"}) {
		t.Errorf("Groups = %v, want exactly [mine]: shadowing must take the higher tier's groups outright (scalar replacement), NOT union with the shadowed definition's [bundled-g]", got.Groups)
	}
	// Sanity: the shadowing itself happened (the union lane ran for the
	// additive field), so a future union mistake in groups is caught for
	// the right reason.
	if !slices.Contains(got.AdditionalTools, "file_read") {
		t.Errorf("AdditionalTools = %v, want the shadowed bundled tool unioned in (proves shadowing ran)", got.AdditionalTools)
	}
}
