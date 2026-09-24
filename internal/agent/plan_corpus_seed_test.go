package agent

// Plan-quality eval-corpus seed (tiered-iteration leaf 04).
//
// The leaf doc calls for a plan-quality mode in tools/classifier-eval
// (golden input → expected tier). That directory is a Python campaign
// workspace with no Go package, so this file is the SMALLEST HONEST
// seed: a Go table running representative golden inputs through the
// deterministic classifier (EvaluatePlanComplexity / tierForRequest)
// and — where cheap — asserting the plan-compiler shape on sealed
// golden drafts (plan.CompileSealed). Extend these tables as real
// request corpora accumulates; a future leaf can graduate them into
// the classifier-eval harness once that grows a Go seam.
//
// Scrubbed-text discipline: every entry below is synthetic. No real
// session text, task ids, or user phrasing is copied into the corpus —
// the same rule scan_replay_privacy.py enforces on the classifier
// replay corpus.

import (
	"testing"

	"github.com/caimlas/meept/internal/plan"
)

// planTierSeedCase is one golden input with its pinned expected tier.
type planTierSeedCase struct {
	name  string
	input string
	// isReplan / replanAttempt reproduce the routing-time request shape
	// (tierForRequest applies the replan-attempt policy on top of the
	// pure evaluator).
	isReplan      bool
	replanAttempt int
	want          ComplexityTier
}

// planTierSeedCases is the seed of the eval corpus: representative
// members of each tier, exactly the rows the leaf pins (trivial
// single-artifact, standard default, complex signal words, replan>=2).
var planTierSeedCases = []planTierSeedCase{
	// --- TierTrivial: single concrete artifact shape ---
	{name: "trivial_create_file", input: "create a file named notes.md", want: TierTrivial},
	{name: "trivial_write_config", input: "write the configuration yaml for the scheduler", want: TierTrivial},
	{name: "trivial_make_readme", input: "make a readme for this repo", want: TierTrivial},
	{name: "trivial_add_script", input: "add a script that prints the date", want: TierTrivial},
	// A first-attempt replan is never trivial (IsReplan outranks the
	// single-artifact shape and maps to Complex in the pure evaluator).
	{name: "replan_attempt1_single_artifact", input: "create a file named notes.md", isReplan: true, want: TierComplex},

	// --- TierStandard: the default tier ---
	{name: "standard_plain_feature", input: "support pagination in the client list view", want: TierStandard},
	{name: "standard_two_step", input: "refactor the session store and update its callers", want: TierStandard},
	{name: "standard_replan_first_attempt", input: "re-plan of 'do the thing'", isReplan: true, want: TierComplex},
}

// planReplanAttemptSeedCases pin the tierForRequest policy layer the
// pure evaluator must not absorb: ReplanAttempt >= 2 forces Complex
// regardless of input shape.
var planReplanAttemptSeedCases = []struct {
	name          string
	input         string
	replanAttempt int
	want          ComplexityTier
}{
	{name: "attempt2_forces_complex", input: "create a file named notes.md", replanAttempt: 2, want: TierComplex},
	{name: "attempt3_forces_complex", input: "create a file named notes.md", replanAttempt: 3, want: TierComplex},
	{name: "attempt1_keeps_evaluator", input: "create a file named notes.md", replanAttempt: 1, want: TierTrivial},
	{name: "attempt0_keeps_evaluator", input: "create a file named notes.md", replanAttempt: 0, want: TierTrivial},
}

// TestPlanCorpus_SeedTierTable runs the golden-input table through the
// deterministic classifier and asserts the expected tier per row.
func TestPlanCorpus_SeedTierTable(t *testing.T) {
	for _, tc := range planTierSeedCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := PlanRequest{Input: tc.input, IsReplan: tc.isReplan}
			if got := EvaluatePlanComplexity(req); got != tc.want {
				t.Errorf("EvaluatePlanComplexity(%q, isReplan=%v) = %q, want %q",
					tc.input, tc.isReplan, got, tc.want)
			}
		})
	}
}

// TestPlanCorpus_SeedReplanAttemptPolicy runs the ReplanAttempt rows
// through the ROUTING seam (tierForRequest), not the pure evaluator —
// the replan>=2-forces-Complex rule is the corpus's routing ground
// truth.
func TestPlanCorpus_SeedReplanAttemptPolicy(t *testing.T) {
	for _, tc := range planReplanAttemptSeedCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := PlanRequest{Input: tc.input, ReplanAttempt: tc.replanAttempt}
			if got := tierForRequest(req); got != tc.want {
				t.Errorf("tierForRequest(%q, attempt=%d) = %q, want %q",
					tc.input, tc.replanAttempt, got, tc.want)
			}
		})
	}
}

// goldenSealedDraft is a compilable plan-dialect v1 document used for
// the step-count range pins: one phase, two steps. Synthetic (no real
// session text), like every corpus entry.
const goldenSealedDraft = `# Plan: golden corpus draft

## Meta

- task_id: corpus-golden
- version: 1
- status: sealed
- updated: 2026-09-24

## Goal

Produce the golden artifact end to end.

## Decisions

- Decision: do it directly — Rationale: scope is small and testable.

## Open Questions

## Phases

### Phase 1: Build

Build the artifact.

**Produces:**

- ` + "`golden-artifact`" + ` (file) — the finished artifact

**Consumes:** none

**Steps:**

1. Implement the artifact end to end [code]
2. Verify the artifact [analyze] (needs: Phase1.S1)
`

// TestPlanCorpus_SeedStepCountRange pins the expected step-count range
// the leaf asks for "where cheap": a sealed golden draft compiles with
// the plan compiler into a deterministic phase/step shape. Model or
// compiler changes that shift the compiled shape turn this red.
func TestPlanCorpus_SeedStepCountRange(t *testing.T) {
	compiled, err := plan.CompileSealed(goldenSealedDraft, 10)
	if err != nil {
		t.Fatalf("CompileSealed: %v", err)
	}
	if len(compiled.Phases) != 1 {
		t.Errorf("phases = %d, want 1", len(compiled.Phases))
	}
	var gotSteps int
	for _, p := range compiled.Phases {
		gotSteps += len(p.Steps)
	}
	if gotSteps < 1 || gotSteps > 3 {
		t.Errorf("compiled steps = %d, want range [1,3] for the golden single-phase draft", gotSteps)
	}
	if compiled.Hash == "" {
		t.Error("compiled hash empty; the compiler must stamp the sealed hash")
	}
}

// TestPlanCorpus_SeedCasesAreScrubbed pins the corpus scrub rule: no
// golden input may embed text that looks like a real session artifact —
// concrete session-/task-id prefixes, user paths, or credential-shaped
// strings. The rule is enforced structurally (every row is a compile
// constant reviewed in-tree), and this check is the belt to the
// suspenders: it fails the suite if a future edit smuggles in a
// session/task id or a home path.
func TestPlanCorpus_SeedCasesAreScrubbed(t *testing.T) {
	suspicious := []string{
		"session-", "conv-", "/Users/", "/home/", "password", "api_key",
		"token=", "BEGIN RSA", "dev_key",
	}
	for _, tc := range planTierSeedCases {
		for _, s := range suspicious {
			if containsFold(tc.input, s) {
				t.Errorf("corpus case %q input contains scrubbed-text pattern %q", tc.name, s)
			}
		}
	}
	for _, tc := range planReplanAttemptSeedCases {
		for _, s := range suspicious {
			if containsFold(tc.input, s) {
				t.Errorf("replan corpus case %q input contains scrubbed-text pattern %q", tc.name, s)
			}
		}
	}
}

// containsFold is a small ASCII case-insensitive substring helper for
// the scrub check (mirrors the planner's ASCII conventions).
func containsFold(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			a, b := lowerByte(s[i+j]), lowerByte(substr[j])
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func lowerByte(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}
