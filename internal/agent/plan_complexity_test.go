package agent

// Plan-complexity evaluation pins (issue #58 capability 4).
//
// EvaluatePlanComplexity is a pure signal table: explicit user signal >
// replan marker > single-artifact shape > default. These tests pin each
// signal, the precedence between them, and the deterministic no-LLM contract.

import (
	"testing"
)

// TestEvaluatePlanComplexity pins the full signal/precedence table: one case
// per signal plus precedence checks (Complex beats replan beats single-
// artifact; single-artifact without other signals is Trivial; anything else
// is Standard).
func TestEvaluatePlanComplexity(t *testing.T) {
	cases := []struct {
		name string
		req  PlanRequest
		want ComplexityTier
	}{
		{
			name: "empty request defaults to standard",
			req:  PlanRequest{},
			want: TierStandard,
		},
		{
			name: "plain multi-step input defaults to standard",
			req:  PlanRequest{Input: "refactor the scheduler package and update the docs"},
			want: TierStandard,
		},

		// Signal 1: explicit user-signal vocabulary → Complex.
		{
			name: "signal word: comprehensive",
			req:  PlanRequest{Input: "do a comprehensive audit of the auth flow"},
			want: TierComplex,
		},
		{
			name: "signal word: iterative",
			req:  PlanRequest{Input: "iterative refinement of the ranking model"},
			want: TierComplex,
		},
		{
			name: "signal word: thorough",
			req:  PlanRequest{Input: "a thorough review of the storage layer"},
			want: TierComplex,
		},
		{
			name: "signal word: multi-phase phrasing",
			req:  PlanRequest{Input: "migrate the database in three phases"},
			want: TierComplex,
		},
		{
			name: "signal word: multi-stage hyphenated",
			req:  PlanRequest{Input: "multi-stage rollout of the feature flags"},
			want: TierComplex,
		},
		{
			name: "signal word matching is case-insensitive",
			req:  PlanRequest{Input: "COMPREHENSIVE cleanup of dead code"},
			want: TierComplex,
		},
		{
			name: "signal substring alone does not trigger (word boundary)",
			req:  PlanRequest{Input: "bump phaseset version and recompile"},
			want: TierStandard,
		},

		// Signal 2: replan marker → Complex.
		{
			name: "replan marker alone",
			req:  PlanRequest{IsReplan: true, Input: "retry the failed deployment steps"},
			want: TierComplex,
		},

		// Signal 3: single-artifact shape → Trivial (via isSingleArtifactTask).
		{
			name: "single artifact: create a config file",
			req:  PlanRequest{Input: "create a config file for the scheduler"},
			want: TierTrivial,
		},
		{
			name: "single artifact: write the readme",
			req:  PlanRequest{Input: "write the readme"},
			want: TierTrivial,
		},

		// Precedence: Complex signals beat replan and single-artifact.
		{
			name: "precedence: comprehensive beats single-artifact shape",
			req:  PlanRequest{Input: "create a comprehensive config file for the scheduler"},
			want: TierComplex,
		},
		{
			name: "precedence: comprehensive beats replan marker",
			req:  PlanRequest{Input: "comprehensive retry of the migration", IsReplan: true},
			want: TierComplex,
		},
		{
			name: "precedence: replan marker beats single-artifact shape",
			req:  PlanRequest{Input: "create a config file for the scheduler", IsReplan: true},
			want: TierComplex,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EvaluatePlanComplexity(tc.req); got != tc.want {
				t.Errorf("EvaluatePlanComplexity(%+v) = %q, want %q", tc.req, got, tc.want)
			}
		})
	}
}

// TestEvaluatePlanComplexity_Deterministic pins the purity contract: repeated
// evaluation of the same request always yields the same tier (no clocks, no
// randomness, no hidden state).
func TestEvaluatePlanComplexity_Deterministic(t *testing.T) {
	req := PlanRequest{Input: "comprehensive refactor of the planner"}
	want := EvaluatePlanComplexity(req)
	for i := 0; i < 10; i++ {
		if got := EvaluatePlanComplexity(req); got != want {
			t.Fatalf("iteration %d: EvaluatePlanComplexity = %q, want stable %q", i, got, want)
		}
	}
}

// TestComplexityTierValues pins the exported string values so downstream
// consumers (metrics tags, logs) can rely on them.
func TestComplexityTierValues(t *testing.T) {
	cases := map[ComplexityTier]string{
		TierTrivial:  "trivial",
		TierStandard: "standard",
		TierComplex:  "complex",
	}
	for tier, want := range cases {
		if string(tier) != want {
			t.Errorf("tier %q != want %q", string(tier), want)
		}
	}
}
