package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// Task 1 (leaf 02-compiler.md): envelope parsing — seal gate on Open
// Questions. The doc below is valid except for the non-empty Open
// Questions section, so the gate is the ONLY problem reported.
//
// Note: the leaf's literal snippet asserts on a doc that also violates
// required-Meta/section rules; it is adapted here to keep exactly one
// problem while preserving the test's intent (see report deviations).
func TestCompileSealed_OpenQuestionsBlock(t *testing.T) {
	md := "# Plan: X\n\n## Meta\n\n- task_id: t-1\n- version: 1\n- status: draft\n- updated: 2026-09-06\n\n" +
		"## Goal\n\ndo it\n\n## Decisions\n\n## Open Questions\n\n- what size?\n\n" +
		"## Phases\n\n### Phase 1: only\n\nDo the thing.\n\n" +
		"**Produces:**\n\n- `out` (file) — the output artifact\n\n**Consumes:** none\n\n" +
		"**Steps:**\n\n1. Do it [code]\n"
	_, err := CompileSealed(md, 5)
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *CompileError, got %v", err)
	}
	if len(ce.Problems) != 1 {
		t.Fatalf("expected 1 problem, got %d: %+v", len(ce.Problems), ce.Problems)
	}
	if !strings.Contains(ce.Problems[0].Message, "Open Questions") {
		t.Fatalf("expected open-questions message, got %q", ce.Problems[0].Message)
	}
}

// ---- Task 2: table-driven validation tests ----

// validDoc is a minimal well-formed sealed draft used as the base for
// table-case mutations.
const validDoc = `# Plan: T

## Meta

- task_id: t-1
- version: 1
- status: sealed
- updated: 2026-09-06

## Goal

Do the thing.

## Decisions

- Decision: Do it simply — Rationale: simple is good.

## Open Questions

## Phases

### Phase 1: Alpha

Build the alpha foundation.

**Produces:**

- ` + "`alpha-art`" + ` (file) — the alpha artifact

**Consumes:** none

**Steps:**

1. Build alpha [code]
2. Check alpha [debug] (needs: Phase1.S1)

### Phase 2: Beta

Wire beta onto alpha.

**Produces:**

- ` + "`beta-art`" + ` (interface) — the beta artifact

**Consumes:**

- ` + "`alpha-art`" + ` (file) — the alpha artifact

**Steps:**

1. Wire beta [code] (needs: alpha-art)
`

func compileProblems(t *testing.T, md string) []CompileProblem {
	t.Helper()
	_, err := CompileSealed(md, 8)
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *CompileError, got %v", err)
	}
	return ce.Problems
}

func findProblem(t *testing.T, probs []CompileProblem, substr string) CompileProblem {
	t.Helper()
	for _, p := range probs {
		if strings.Contains(p.Message, substr) {
			return p
		}
	}
	t.Fatalf("no problem containing %q in %+v", substr, probs)
	return CompileProblem{}
}

func countProblems(probs []CompileProblem, substr string) int {
	n := 0
	for _, p := range probs {
		if strings.Contains(p.Message, substr) {
			n++
		}
	}
	return n
}

func TestCompileSealed_ValidBaseline(t *testing.T) {
	cp, err := CompileSealed(validDoc, 8)
	if err != nil {
		t.Fatalf("valid doc must compile: %v", err)
	}
	if len(cp.Phases) != 2 {
		t.Fatalf("expected 2 phases, got %d", len(cp.Phases))
	}
	if len(cp.Warnings) != 0 {
		t.Fatalf("expected no warnings on a fully explicit doc, got %+v", cp.Warnings)
	}
}

func TestCompileSealed_ValidationTable(t *testing.T) {
	art := func(name, kind string) string {
		return "- `" + name + "` (" + kind + ") — desc"
	}

	tests := []struct {
		name     string
		doc      string
		contains string
		line     int // 0 = don't assert line
	}{
		{
			name:     "unknown artifact kind",
			doc:      strings.Replace(validDoc, art("alpha-art", "file"), art("alpha-art", "table"), 1),
			contains: `unknown kind "table"`,
		},
		{
			name:     "duplicate artifact",
			doc:      strings.Replace(validDoc, art("beta-art", "interface")+"\n\n**Consumes:**\n\n- `alpha-art` (file) — the alpha artifact", art("alpha-art", "file")+" — dup", 1),
			contains: "duplicate artifact name",
		},
		{
			name:     "consume before produce",
			doc:      strings.Replace(validDoc, "- `alpha-art` (file) — the alpha artifact\n\n**Consumes:** none", "- `x` (file) — d\n\n**Consumes:**\n\n- `alpha-art` (file) — the alpha artifact", 1),
			contains: "produced by a later phase",
		},
		{
			name:     "self consume",
			doc:      strings.Replace(validDoc, "**Consumes:** none", "**Consumes:**\n\n- `alpha-art` (file) — the alpha artifact", 1),
			contains: "consumes must reference artifacts from earlier phases",
		},
		{
			name:     "unknown consume",
			doc:      strings.Replace(validDoc, art("alpha-art", "file")+" — the alpha artifact", art("alpha-art", "file")+" — the alpha artifact", 1) + "### Phase 3: Gamma\n\nG.\n\n**Produces:**\n\n- `gamma-art` (file) — g\n\n**Consumes:**\n\n- `no-such-thing` (file) — ghost\n\n**Steps:**\n\n1. Do [code]\n",
			contains: "unknown artifact",
		},
		{
			name:     "unknown tool hint",
			doc:      strings.Replace(validDoc, "2. Check alpha [debug]", "2. Check alpha [test]", 1),
			contains: "unknown tool_hint",
		},
		{
			name:     "over max phases",
			doc:      validDoc,
			contains: "maximum is 1",
		},
		{
			name:     "empty phase no steps",
			doc:      strings.Replace(validDoc, "1. Wire beta [code] (needs: alpha-art)\n", "", 1),
			contains: "declares no steps",
		},
		{
			name:     "malformed meta line",
			doc:      strings.Replace(validDoc, "- updated: 2026-09-06", "updated: 2026-09-06", 1),
			contains: `expected "- <key>: <value>"`,
		},
		{
			name:     "missing meta key",
			doc:      strings.Replace(validDoc, "- task_id: t-1\n", "", 1),
			contains: "Meta is missing required key: task_id",
		},
		{
			name:     "bad version",
			doc:      strings.Replace(validDoc, "- version: 1", "- version: 2", 1),
			contains: "unsupported dialect version",
		},
		{
			name:     "bad status",
			doc:      strings.Replace(validDoc, "- status: sealed", "- status: wip", 1),
			contains: `Meta status must be "draft" or "sealed" (got "wip")`,
		},
		{
			name:     "duplicate section",
			doc:      strings.Replace(validDoc, "## Goal\n", "## Goal\n\n## Meta\n\n- version: 1\n", 1),
			contains: "duplicate section: ## Meta",
		},
		{
			name:     "missing required section",
			doc:      strings.Replace(validDoc, "## Goal\n\nDo the thing.\n\n", "", 1),
			contains: "missing required section: ## Goal",
		},
		{
			name:     "no phases",
			doc:      "# Plan: T\n\n## Meta\n\n- task_id: t\n- version: 1\n- status: draft\n- updated: 2026-09-06\n\n## Goal\n\ng\n\n## Decisions\n\n## Open Questions\n\n## Phases\n",
			contains: "plan declares no phases",
		},
		{
			name:     "phase numbering gap",
			doc:      strings.Replace(validDoc, "### Phase 2: Beta", "### Phase 3: Beta", 1),
			contains: "phase numbering must be consecutive from 1 (expected Phase 2, got Phase 3)",
		},
		{
			name:     "phase heading with state suffix",
			doc:      strings.Replace(validDoc, "### Phase 1: Alpha", "### Phase 1: Alpha [pending]", 1),
			contains: `expected phase heading`,
		},
		{
			name:     "malformed artifact bullet",
			doc:      strings.Replace(validDoc, art("alpha-art", "file")+" — the alpha artifact", "- alpha art (file) — the alpha artifact", 1),
			contains: "kebab-case",
		},
		{
			name:     "missing produces block",
			doc:      strings.Replace(validDoc, "**Produces:**\n\n- `beta-art` (interface) — the beta artifact\n\n**Consumes:**", "**Consumes:**", 1),
			contains: "declares no Produces block",
		},
		{
			name:     "step numbering gap",
			doc:      strings.Replace(validDoc, "2. Check alpha [debug]", "3. Check alpha [debug]", 1),
			contains: "must start at 1 and increase by 1 (expected step 2, got step 3)",
		},
		{
			name:     "malformed step line",
			doc:      strings.Replace(validDoc, "1. Build alpha [code]", "1 Build alpha [code]", 1),
			contains: `expected "<n>. <description> [tool_hint] (needs: <refs>)"`,
		},
		{
			name:     "unknown needs ref",
			doc:      strings.Replace(validDoc, "(needs: Phase1.S1)", "(needs: ghost-artifact)", 1),
			contains: "has a needs reference",
		},
		{
			name:     "step ref to missing step",
			doc:      strings.Replace(validDoc, "(needs: Phase1.S1)", "(needs: Phase1.S9)", 1),
			contains: "but phase 1 has no step 9",
		},
		{
			name:     "step ref to later phase",
			doc:      strings.Replace(validDoc, "(needs: alpha-art)", "(needs: Phase3.S1)", 1),
			contains: "a later phase",
		},
		{
			name:     "step ref to later same-phase step",
			doc:      strings.Replace(validDoc, "1. Build alpha [code]", "1. Build alpha [code] (needs: Phase1.S2)", 1),
			contains: "a later step in the same phase",
		},
		{
			name:     "needs artifact from same phase",
			doc:      strings.Replace(validDoc, "1. Wire beta [code] (needs: alpha-art)", "1. Wire beta [code] (needs: beta-art)", 1),
			contains: "produced by the same phase",
		},
		{
			name:     "malformed depends on line",
			doc:      strings.Replace(validDoc, "**Consumes:**\n\n- `alpha-art` (file) — the alpha artifact", "**Depends on:** phase one\n\n**Consumes:**\n\n- `alpha-art` (file) — the alpha artifact", 1),
			contains: `expected "**Depends on:** Phases`,
		},
		{
			name:     "depends on nonexistent phase",
			doc:      strings.Replace(validDoc, "**Consumes:**\n\n- `alpha-art` (file) — the alpha artifact", "**Depends on:** Phases 9\n\n**Consumes:**\n\n- `alpha-art` (file) — the alpha artifact", 1),
			contains: "cites nonexistent phase 9",
		},
		{
			name:     "depends on forward phase",
			doc:      strings.Replace(validDoc, "**Consumes:** none", "**Depends on:** Phases 2\n\n**Consumes:** none", 1),
			contains: "only earlier phases may be cited",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			maxPhases := 8
			if tc.name == "over max phases" {
				maxPhases = 1
			}
			probs := compileProblemsMax(t, tc.doc, maxPhases)
			p := findProblem(t, probs, tc.contains)
			if tc.line != 0 && p.Line != tc.line {
				t.Fatalf("expected line %d, got %d", tc.line, p.Line)
			}
		})
	}
}

func compileProblemsMax(t *testing.T, md string, maxPhases int) []CompileProblem {
	t.Helper()
	_, err := CompileSealed(md, maxPhases)
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *CompileError, got %v", err)
	}
	return ce.Problems
}

// All-problems-at-once: a doc with several independent classes must
// report them all in one pass.
func TestCompileSealed_CollectsAllProblems(t *testing.T) {
	md := strings.Replace(validDoc, "1. Build alpha [code]", "1. Build alpha [bogus]", 1)
	md = strings.Replace(md, "- version: 1", "- version: 7", 1)
	md = strings.Replace(md, "2. Check alpha [debug]", "5. Check alpha [debug]", 1)
	probs := compileProblems(t, md)
	if got := countProblems(probs, "unknown tool_hint"); got != 1 {
		t.Fatalf("expected 1 tool_hint problem, got %d: %+v", got, probs)
	}
	if got := countProblems(probs, "unsupported dialect version"); got != 1 {
		t.Fatalf("expected version problem, got %+v", probs)
	}
	if got := countProblems(probs, "increase by 1"); got != 1 {
		t.Fatalf("expected numbering problem, got %+v", probs)
	}
}

// ---- Task 3: inference, explicit deps, cycle, hash ----

func TestCompileSealed_Infer(t *testing.T) {
	cp, err := CompileSealed(validDoc, 8)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Phase 2 omits "**Depends on:**" and consumes alpha-art (phase 1)
	// → DependsOn {1} + W1 warning.
	if len(cp.Phases[1].DependsOn) != 1 || cp.Phases[1].DependsOn[0] != 1 {
		t.Fatalf("expected DependsOn [1], got %+v", cp.Phases[1].DependsOn)
	}
	found := false
	for _, w := range cp.Warnings {
		if strings.Contains(w, "depends_on inferred for phase") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected W1 inference warning, got %+v", cp.Warnings)
	}
	// Phase 1 has no deps.
	if len(cp.Phases[0].DependsOn) != 0 {
		t.Fatalf("expected no deps for phase 1, got %+v", cp.Phases[0].DependsOn)
	}
}

func TestCompileSealed_InferExplicitDeps(t *testing.T) {
	md := strings.Replace(validDoc,
		"**Consumes:**\n\n- `alpha-art` (file) — the alpha artifact",
		"**Depends on:** Phases 1\n\n**Consumes:**\n\n- `alpha-art` (file) — the alpha artifact", 1)
	cp, err := CompileSealed(md, 8)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(cp.Phases[1].DependsOn) != 1 || cp.Phases[1].DependsOn[0] != 1 {
		t.Fatalf("expected explicit DependsOn [1], got %+v", cp.Phases[1].DependsOn)
	}
	// W2: explicit set fully implied by consumes → duplicate warning.
	found := false
	for _, w := range cp.Warnings {
		if strings.Contains(w, "duplicates dependencies already implied") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected W2 duplicate warning, got %+v", cp.Warnings)
	}
	// W3: needs names an artifact absent from Consumes.
	md2 := strings.Replace(validDoc,
		"1. Wire beta [code] (needs: alpha-art)",
		"1. Wire beta [code] (needs: alpha-art, Phase1.S2)", 1)
	cp2, err := CompileSealed(md2, 8)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Earlier-phase step ref implies a phase dependency on 1 as well.
	if len(cp2.Phases[1].DependsOn) != 1 || cp2.Phases[1].DependsOn[0] != 1 {
		t.Fatalf("expected inferred [1] from earlier-phase step ref, got %+v", cp2.Phases[1].DependsOn)
	}
	found3 := false
	for _, w := range cp2.Warnings {
		if strings.Contains(w, "does not appear in the phase's Consumes block") {
			found3 = true
		}
	}
	if !found3 {
		t.Fatalf("expected W3 warning, got %+v", cp2.Warnings)
	}
	// Step-level depends_on from same-phase prior step ref.
	if got := cp.Phases[0].Steps[1].DependsOn; len(got) != 1 || got[0] != 1 {
		t.Fatalf("expected step 2 DependsOn [1], got %+v", got)
	}
}

// compileCycleCheck is the white-box cycle detector (class 5 is
// unreachable through CompileSealed in v1 — every validated edge points
// backward — so it is exercised directly).
func TestCompileSealed_CycleDetector(t *testing.T) {
	names := []string{"A", "B", "C"}
	// C depends on B, B depends on A, A depends on C.
	edges := [][]int{
		{2}, // A's dependents: C (index 2)
		{0}, // B's dependents: A (index 0)
		{1}, // C's dependents: B (index 1)
	}
	cycle := compileCycleCheck(3, edges, names)
	if cycle == "" {
		t.Fatal("expected cycle detection, got none")
	}
	if !strings.Contains(cycle, " → ") {
		t.Fatalf("expected arrow-joined names, got %q", cycle)
	}
	// DAG: no cycle.
	dag := [][]int{{1}, nil, nil}
	if got := compileCycleCheck(3, dag, names); got != "" {
		t.Fatalf("expected no cycle in DAG, got %q", got)
	}
}

func TestCompileSealed_Hash(t *testing.T) {
	// sha256("# Plan: hash me") precomputed via shasum -a 256.
	const input = "# Plan: hash me"
	const want = "d20393fb3a1a6e1a53a01aadc846f1bf611837b2ddb9dec05e7fc5acd8d6d10f"
	cp, err := CompileSealed(input, 8)
	if err == nil {
		// A bare title isn't a valid doc, but Hash must still be set on
		// success paths only; expect an error here.
		t.Fatalf("expected error for invalid doc, got %+v", cp)
	}
	// Hash of a valid doc equals sha256 of the exact input bytes.
	cp2, err := CompileSealed(validDoc, 8)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	sum := sha256.Sum256([]byte(validDoc))
	got := hex.EncodeToString(sum[:])
	if cp2.Hash != got {
		t.Fatalf("hash mismatch: %s != %s", cp2.Hash, got)
	}
	if got != want {
		t.Fatalf("sanity: sha256 implementation drift? %s", got)
	}
}
