//go:build e2e

// Gate test for the zero-decision compile rule (leaf 05; contract C7 in
// docs/plans/20261101-fresh-context-critic/master.md).
//
// PARKED as .staged. Leaf 05 re-activates ONLY this file. It pins:
//
//	F7 TestMutatingPhaseWithoutDecisionFailsCompile (C7: a phase with a
//	   mutating tool_hint (code, shell, file_write, git, commit) that
//	   neither produces nor consumes a decision artifact fails
//	   CompileSealed with "phase %q changes state but names no decision
//	   artifact")
//
// This test CAN call plan.CompileSealed today. Written so it FAILS now
// (today the mutating-without-decision phase compiles, err == nil) and
// passes when leaf 05 lands. Still parked: a live failing test would
// fail the hook if the suite were mapped.
//
// Self-contained on purpose: this file compiles alone when the sibling
// .staged files are still parked.
package plancritic

import (
	"errors"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/plan"
)

// zeroDecisionED is the plan-dialect em-dash via escape (ASCII-only).
const zeroDecisionED = "\u2014"

// F7 (C7): a mutating phase with no decision artifact must fail compile.
func TestMutatingPhaseWithoutDecisionFailsCompile(t *testing.T) {
	// Valid dialect shape: phase 1 is mutating (tool_hint code), produces
	// only a file artifact, and consumes nothing. Under C7 this draft
	// must be REJECTED because the phase changes state but names no
	// decision artifact.
	md := "# Plan: Zero decision e2e\n\n## Meta\n\n- task_id: t-zero-decision-e2e\n- version: 1\n- status: sealed\n- updated: 2026-11-01\n\n" +
		"## Goal\n\nProduce the artifact.\n\n## Decisions\n\n- Decision: do it directly " + zeroDecisionED +
		" Rationale: small scope.\n\n## Open Questions\n\n## Phases\n\n### Phase 1: Build\n\nBuild the artifact.\n\n" +
		"**Produces:**\n\n- `zero-decision-artifact` (file) " + zeroDecisionED + " the finished artifact\n\n**Consumes:** none\n\n" +
		"**Steps:**\n\n1. Implement the artifact end to end [code]\n"

	cp, err := plan.CompileSealed(md, 5)

	// RED on today's tree: the compiler accepts this draft (err == nil).
	if err == nil {
		t.Fatalf("F7 (C7): mutating phase %q compiled without a decision artifact; CompileSealed must reject it (got compiled plan %v)", "Build", cp)
	}
	var ce *plan.CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("F7 (C7): want *plan.CompileError, got %T: %v", err, err)
	}
	found := false
	for _, p := range ce.Problems {
		if strings.Contains(p.Message, "changes state but names no decision artifact") && strings.Contains(p.Message, "Build") {
			found = true
		}
	}
	if !found {
		t.Fatalf("F7 (C7): no C7 problem for phase %q in %+v; want message matching %q", "Build", ce.Problems, "changes state but names no decision artifact")
	}

	// Companion pin (C7 unchanged behavior): a phase with only
	// non-mutating hints still compiles without a decision artifact.
	// (Write this AFTER the main assertion so a leaf-05 regression in
	// the main rule still fails the test at the Fatalf above.)
	nonMutating := strings.Replace(md,
		"1. Implement the artifact end to end [code]",
		"1. Analyze the artifact design [analyze]",
		1)
	if _, nmErr := plan.CompileSealed(nonMutating, 5); nmErr != nil {
		var nmCe *plan.CompileError
		if errors.As(nmErr, &nmCe) {
			for _, p := range nmCe.Problems {
				if strings.Contains(p.Message, "names no decision artifact") {
					t.Fatalf("F7 (C7): non-mutating phase must still compile without a decision artifact, got: %+v", nmCe.Problems)
				}
			}
		}
	}

	// Companion pin (C7 pass path): a phase that consumes an earlier
	// decision artifact compiles.
	consuming := "# Plan: Zero decision consume e2e\n\n## Meta\n\n- task_id: t-zero-decision-e2e\n- version: 1\n- status: sealed\n- updated: 2026-11-01\n\n" +
		"## Goal\n\nProduce the artifact.\n\n## Decisions\n\n- Decision: do it directly " + zeroDecisionED +
		" Rationale: small scope.\n\n## Open Questions\n\n## Phases\n\n" +
		"### Phase 1: Decide\n\nDecide the storage engine.\n\n**Produces:**\n\n- `storage-decision` (decision) " + zeroDecisionED +
		" the storage choice\n\n**Consumes:** none\n\n**Steps:**\n\n1. Record the decision [plan]\n\n" +
		"### Phase 2: Build\n\nBuild against the decision.\n\n**Produces:**\n\n- `zero-decision-artifact` (file) " + zeroDecisionED +
		" the finished artifact\n\n**Consumes:**\n\n- `storage-decision` (decision) " + zeroDecisionED +
		" the storage choice\n\n**Steps:**\n\n1. Implement the artifact end to end [code] (needs: storage-decision)\n"
	if _, cErr := plan.CompileSealed(consuming, 5); cErr != nil {
		var cCe *plan.CompileError
		if errors.As(cErr, &cCe) {
			for _, p := range cCe.Problems {
				if strings.Contains(p.Message, "names no decision artifact") {
					t.Fatalf("F7 (C7): a phase consuming an earlier decision artifact must compile, got: %+v", cCe.Problems)
				}
			}
		}
	}
}
