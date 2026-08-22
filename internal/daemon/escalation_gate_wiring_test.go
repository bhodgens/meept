package daemon

import (
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/employee"
)

// TestEscalationGateForTier_WiredOnlyForTier3 verifies the leaf-02 wiring
// helper: tier-3 constitutions get a gate wrapping ShouldEscalate that
// escalates on a constitution-matching candidate; tier-1 and tier-2
// constitutions get nil (loops stay in their default routing).
func TestEscalationGateForTier_WiredOnlyForTier3(t *testing.T) {
	// Minimal valid tier-2 constitution (mirrors employee test fixtures;
	// exported constructor does not exist, so build inline).
	t3 := employee.Constitution{
		Purpose:      "keep CI green",
		Role:         "CI Reliability Engineer",
		Charter:      "investigate failures, open issues, never merge code",
		AutonomyTier: employee.Tier2Propose,
		EscalatesTo:  []string{"user"},
	}
	t3.AutonomyTier = employee.Tier3Autonomous
	t3.Constraints.EscalationTriggers = []employee.EscalationTrigger{
		{On: employee.EscalateOnTool, Match: "shell_execute", Reason: "shell needs signoff"},
	}
	matching := employee.CandidatePlan{Title: "shell op", Prompt: "use shell_execute now"}
	nonMatching := employee.CandidatePlan{Title: "safe op", Prompt: "web_fetch the page"}

	logger := slog.Default()

	t.Run("tier3 gets a working gate", func(t *testing.T) {
		gate := escalationGateForTier(&t3, "emp-t3", logger)
		if gate == nil {
			t.Fatal("tier-3 constitution should produce a non-nil gate")
		}
		var c *employee.Constitution
		escalate, _ := gate(c, matching)
		if !escalate {
			t.Error("matching candidate should escalate")
		}
		escalate, _ = gate(c, nonMatching)
		if escalate {
			t.Error("non-matching candidate should not escalate")
		}
	})

	for _, tier := range []struct {
		name string
		c    employee.Constitution
	}{
		{"tier1", func() employee.Constitution { c := t3; c.AutonomyTier = employee.Tier1Reactive; return c }()},
		{"tier2", func() employee.Constitution { c := t3; c.AutonomyTier = employee.Tier2Propose; return c }()},
	} {
		t.Run(tier.name+"_gets no gate", func(t *testing.T) {
			c := tier.c
			if gate := escalationGateForTier(&c, "emp-"+tier.name, logger); gate != nil {
				t.Errorf("%s constitution should NOT produce a gate, got %v", tier.name, gate)
			}
		})
	}
}
