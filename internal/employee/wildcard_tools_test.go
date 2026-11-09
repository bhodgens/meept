package employee

import "testing"

// Pins the 2026-11-09 voice-companion defect: hire-time validation treated
// tools_forbidden: ["*"] as an unknown tool name and silently stripped it,
// which re-armed the full toolset on a talk-only employee (companion then
// executed schedule_list mid-call and its raw JSON reply was spoken aloud).
// The wildcard is a reserved semantic token: validation must never report it
// as unknown, and the enforcement engine must deny every tool when present.
func TestCheckToolReferences_PreservesWildcard(t *testing.T) {
	known := map[string]struct{}{"web_fetch": {}}

	t.Run("wildcard forbidden is never unknown", func(t *testing.T) {
		c := Constitution{Constraints: ConstitutionalConstraints{
			ToolsForbidden: []string{"*"},
		}}
		_, unknownF := c.CheckToolReferences(known, "*")
		if len(unknownF) != 0 {
			t.Fatalf("wildcard reported unknown: %v", unknownF)
		}
	})

	t.Run("wildcard allowed is never unknown", func(t *testing.T) {
		c := Constitution{Constraints: ConstitutionalConstraints{
			ToolsAllowed: []string{"*"},
		}}
		unknownA, _ := c.CheckToolReferences(known, "*")
		if len(unknownA) != 0 {
			t.Fatalf("wildcard reported unknown: %v", unknownA)
		}
	})

	t.Run("real unknown tools still detected alongside wildcard", func(t *testing.T) {
		c := Constitution{Constraints: ConstitutionalConstraints{
			ToolsForbidden: []string{"*", "ghost_tool"},
		}}
		_, unknownF := c.CheckToolReferences(known, "*")
		if len(unknownF) != 1 || unknownF[0] != "ghost_tool" {
			t.Fatalf("unknown forbidden = %v, want [ghost_tool]", unknownF)
		}
	})
}

func TestEnforcement_WildcardToolsForbiddenDeniesEveryTool(t *testing.T) {
	c := &Constitution{
		Purpose:      "talk-only",
		Role:         "Companion",
		Charter:      "converse only",
		AutonomyTier: Tier2Propose,
		EscalatesTo:  []string{"user"},
		Constraints: ConstitutionalConstraints{
			ToolsForbidden: []string{"*"},
		},
	}
	p := NewPreExecChecker("companion", c)
	for _, tool := range []string{"schedule_list", "web_fetch", "shell_execute", "memory_store"} {
		d := p.Check("execute", tool, nil)
		if d.Allowed {
			t.Fatalf("tool %q allowed under wildcard tools_forbidden", tool)
		}
	}
}
