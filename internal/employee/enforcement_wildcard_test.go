package employee

import (
	"strings"
	"testing"
)

// Pins the " *" (stray-space) wildcard defect: the enforcement wildcard
// check compared RAW entries via slices.Contains while the voice visibility
// filter trims before matching "*" — a config entry " *" therefore produced
// an empty visible registry (deny-all shown to the model) but enforcement
// passed it, so the tool still executed if invoked. The wildcard match is
// now canonical (lower + trim via canonicalToolName), matching hire-time
// validation and the visibility filter.
func TestEnforcement_WhitespacePaddedWildcardForbiddenDenies(t *testing.T) {
	c := &Constitution{
		Purpose:      "talk-only",
		Role:         "Companion",
		Charter:      "converse only",
		AutonomyTier: Tier2Propose,
		EscalatesTo:  []string{"user"},
		Constraints: ConstitutionalConstraints{
			ToolsForbidden: []string{" *"},
		},
	}
	p := NewPreExecChecker("companion", c)
	for _, tool := range []string{"schedule_list", "web_fetch", "shell_execute", "memory_store"} {
		d := p.Check("execute", tool, nil)
		if d.Allowed {
			t.Fatalf("tool %q allowed under whitespace-padded wildcard tools_forbidden", tool)
		}
		if !strings.Contains(d.Reason, "forbidden") {
			t.Fatalf("tool %q denial reason %q does not mention forbidden", tool, d.Reason)
		}
	}
}
