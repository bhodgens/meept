package plan

import (
	"crypto/sha256"
	_ "embed"
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
// table-case mutations. Line map (1-based):
//
//	1  # Plan: T
//	5  - task_id: t-1
//	6  - version: 1
//	7  - status: sealed
//	8  - updated: 2026-09-06
//	10 ## Goal
//	14 ## Decisions
//	18 ## Open Questions
//	20 ## Phases
//	22 ### Phase 1: Alpha
//	28 - `alpha-art` (file) produce
//	34 1. Build alpha [code]
//	35 2. Check alpha [debug] (needs: Phase1.S1)
//	37 ### Phase 2: Beta
//	43 - `beta-art` (interface) produce
//	47 - `alpha-art` (file) consume
//	51 1. Wire beta [code] (needs: alpha-art)
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

// validDocWithAlphaTest is validDoc plus a second phase-1 artifact that
// nothing consumes — used to exercise the W3 warning.
var validDocWithAlphaTest = strings.Replace(validDoc,
	"- `alpha-art` (file) — the alpha artifact\n\n**Consumes:** none",
	"- `alpha-art` (file) — the alpha artifact\n- `alpha-test` (test_suite) — the alpha tests\n\n**Consumes:** none", 1)

func compileProblems(t *testing.T, md string) []CompileProblem {
	t.Helper()
	return compileProblemsMax(t, md, 8)
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

func warningsContaining(cp *CompiledPlan, substr string) int {
	n := 0
	for _, w := range cp.Warnings {
		if strings.Contains(w, substr) {
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
	// Phase 2 consumes without an explicit Depends on line → W1 (spec'd).
	if warningsContaining(cp, "depends_on inferred") != 1 {
		t.Fatalf("expected exactly one W1 warning, got %+v", cp.Warnings)
	}
	// Derived Required: alpha-art consumed by later phase 2 → true;
	// beta-art consumed by nobody → false.
	if !cp.Phases[0].Produces[0].Required {
		t.Fatal("alpha-art must be Required (consumed by later phase)")
	}
	if cp.Phases[1].Produces[0].Required {
		t.Fatal("beta-art must not be Required")
	}
	// Phase 1 has no deps; step 2 depends on step 1.
	if len(cp.Phases[0].DependsOn) != 0 {
		t.Fatalf("expected no deps for phase 1, got %+v", cp.Phases[0].DependsOn)
	}
	if got := cp.Phases[0].Steps[1].DependsOn; len(got) != 1 || got[0] != 1 {
		t.Fatalf("expected step 2 DependsOn [1], got %+v", got)
	}
	// Descriptions carry the phase intent prose.
	if cp.Phases[0].Description != "Build the alpha foundation." {
		t.Fatalf("unexpected phase 1 description %q", cp.Phases[0].Description)
	}
}

func TestCompileSealed_ValidationTable(t *testing.T) {
	tests := []struct {
		name     string
		doc      string
		contains string
		line     int // 0 = don't assert line
	}{
		{
			name:     "unknown artifact kind",
			doc:      strings.Replace(validDoc, "- `alpha-art` (file) — the alpha artifact", "- `alpha-art` (table) — the alpha artifact", 1),
			contains: `artifact "alpha-art" has unknown kind "table" (must be one of file, interface, schema, decision, test_suite)`,
			line:     28,
		},
		{
			name: "duplicate artifact",
			doc: strings.Replace(validDoc,
				"- `beta-art` (interface) — the beta artifact",
				"- `alpha-art` (file) — the alpha artifact", 1),
			contains: `duplicate artifact name "alpha-art" (already produced by phase "Alpha")`,
			line:     43,
		},
		{
			name: "consume before produce",
			doc: strings.Replace(validDoc,
				"- `alpha-art` (file) — the alpha artifact\n\n**Consumes:** none",
				"- `alpha-art` (file) — kept local\n\n**Consumes:**\n\n- `beta-art` (interface) — the beta artifact", 1),
			contains: `phase "Alpha" consumes "beta-art", which is produced by a later phase ("Beta"): consumes must reference artifacts from earlier phases`,
			line:     32,
		},
		{
			name:     "self consume",
			doc:      strings.Replace(validDoc, "**Consumes:** none", "**Consumes:**\n\n- `alpha-art` (file) — the alpha artifact", 1),
			contains: "consumes must reference artifacts from earlier phases",
		},
		{
			name:     "unknown consume",
			doc:      validDoc + "### Phase 3: Gamma\n\nG.\n\n**Produces:**\n\n- `gamma-art` (file) — g\n\n**Consumes:**\n\n- `no-such-thing` (file) — ghost\n\n**Steps:**\n\n1. Do [code]\n",
			contains: `unknown artifact "no-such-thing" consumed by phase "Gamma": no phase in this plan produces it`,
			line:     62,
		},
		{
			name:     "unknown tool hint",
			doc:      strings.Replace(validDoc, "2. Check alpha [debug]", "2. Check alpha [test]", 1),
			contains: `step 2 of phase "Alpha" has unknown tool_hint "test"`,
			line:     35,
		},
		{
			name:     "over max phases",
			doc:      validDoc,
			contains: "plan declares 2 phases; maximum is 1",
		},
		{
			name:     "empty phase no steps",
			doc:      strings.Replace(validDoc, "1. Wire beta [code] (needs: alpha-art)\n", "", 1),
			contains: `phase "Beta" declares no steps`,
			line:     37,
		},
		{
			name:     "malformed meta line",
			doc:      strings.Replace(validDoc, "- updated: 2026-09-06", "updated: 2026-09-06", 1),
			contains: `expected "- <key>: <value>", got "updated: 2026-09-06"`,
			line:     8,
		},
		{
			name:     "missing meta key",
			doc:      strings.Replace(validDoc, "- task_id: t-1\n", "", 1),
			contains: "Meta is missing required key: task_id",
		},
		{
			name:     "bad version",
			doc:      strings.Replace(validDoc, "- version: 1", "- version: 2", 1),
			contains: `unsupported dialect version: got "2"`,
			line:     6,
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
			line:     37,
		},
		{
			name:     "phase heading with state suffix",
			doc:      strings.Replace(validDoc, "### Phase 1: Alpha", "### Phase 1: Alpha [pending]", 1),
			contains: `expected phase heading "### Phase N: <name>", got "### Phase 1: Alpha [pending]"`,
		},
		{
			name:     "malformed artifact bullet",
			doc:      strings.Replace(validDoc, "- `alpha-art` (file) — the alpha artifact", "- User_Schema (file) — the alpha artifact", 1),
			contains: `artifact name "User_Schema" is not kebab-case`,
			line:     28,
		},
		{
			name:     "missing produces block",
			doc:      strings.Replace(validDoc, "**Produces:**\n\n- `beta-art` (interface) — the beta artifact\n\n**Consumes:**", "**Consumes:**", 1),
			contains: `phase "Beta" declares no Produces block`,
		},
		{
			name:     "step numbering gap",
			doc:      strings.Replace(validDoc, "2. Check alpha [debug]", "3. Check alpha [debug]", 1),
			contains: "must start at 1 and increase by 1 (expected step 2, got step 3)",
			line:     35,
		},
		{
			name:     "malformed step line",
			doc:      strings.Replace(validDoc, "1. Build alpha [code]", "1 Build alpha [code]", 1),
			contains: `expected "<n>. <description> [tool_hint] (needs: <refs>)", got "1 Build alpha [code]"`,
		},
		{
			name:     "unknown needs ref",
			doc:      strings.Replace(validDoc, "(needs: Phase1.S1)", "(needs: ghost-artifact)", 1),
			contains: `has a needs reference "ghost-artifact" that matches no artifact name or step`,
		},
		{
			name:     "step ref to missing step",
			doc:      strings.Replace(validDoc, "1. Wire beta [code] (needs: alpha-art)", "1. Wire beta [code] (needs: Phase1.S9)", 1),
			contains: `references "Phase1.S9", but phase 1 has no step 9`,
		},
		{
			name:     "step ref to later phase",
			doc:      strings.Replace(validDoc, "(needs: alpha-art)", "(needs: Phase3.S1)", 1),
			contains: `references "Phase3.S1" from phase 3, a later phase`,
		},
		{
			name:     "step ref to later same-phase step",
			doc:      strings.Replace(validDoc, "1. Build alpha [code]", "1. Build alpha [code] (needs: Phase1.S2)", 1),
			contains: `references "Phase1.S2", a later step in the same phase`,
		},
		{
			name:     "needs artifact from same phase",
			doc:      strings.Replace(validDoc, "1. Wire beta [code] (needs: alpha-art)", "1. Wire beta [code] (needs: beta-art)", 1),
			contains: `references artifact "beta-art", which is produced by the same phase`,
		},
		{
			name:     "malformed depends on line",
			doc:      strings.Replace(validDoc, "**Consumes:**\n\n- `alpha-art` (file) — the alpha artifact", "**Depends on:** phase one\n\n**Consumes:**\n\n- `alpha-art` (file) — the alpha artifact", 1),
			contains: `expected "**Depends on:** Phases <n>[, <n>]...", got "**Depends on:** phase one"`,
		},
		{
			name:     "depends on nonexistent phase",
			doc:      strings.Replace(validDoc, "**Consumes:**\n\n- `alpha-art` (file) — the alpha artifact", "**Depends on:** Phases 9\n\n**Consumes:**\n\n- `alpha-art` (file) — the alpha artifact", 1),
			contains: `cites nonexistent phase 9 in "Depends on"`,
		},
		{
			name:     "depends on forward phase",
			doc:      strings.Replace(validDoc, "**Consumes:** none", "**Depends on:** Phases 2\n\n**Consumes:** none", 1),
			contains: `cites phase 2 in "Depends on", but only earlier phases may be cited`,
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

// ---- Task 3: inference, explicit deps, warnings ----

func TestCompileSealed_Infer(t *testing.T) {
	cp, err := CompileSealed(validDoc, 8)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Phase 2 omits "**Depends on:**" and consumes alpha-art (phase 1)
	// → DependsOn {1} + W1 warning. DependsOn carries the producer's
	// phase ordinal (dialect doc section 8.2 walkthrough).
	if len(cp.Phases[1].DependsOn) != 1 || cp.Phases[1].DependsOn[0] != 1 {
		t.Fatalf("expected DependsOn [1], got %+v", cp.Phases[1].DependsOn)
	}
	if warningsContaining(cp, `depends_on inferred for phase "Beta"`) != 1 {
		t.Fatalf("expected W1 inference warning, got %+v", cp.Warnings)
	}
}

func TestCompileSealed_ExplicitDeps(t *testing.T) {
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
	if warningsContaining(cp, "duplicates dependencies already implied") != 1 {
		t.Fatalf("expected W2 duplicate warning, got %+v", cp.Warnings)
	}
	if warningsContaining(cp, "depends_on inferred") != 0 {
		t.Fatalf("explicit deps must not also warn W1, got %+v", cp.Warnings)
	}
}

func TestCompileSealed_NeedsWarningW3(t *testing.T) {
	// Beta's step needs alpha-test (produced by phase 1) but alpha-test
	// is not in Beta's Consumes block → W3 warning, no hard error.
	md := strings.Replace(validDocWithAlphaTest,
		"1. Wire beta [code] (needs: alpha-art)",
		"1. Wire beta [code] (needs: alpha-test)", 1)
	cp, err := CompileSealed(md, 8)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if got := warningsContaining(cp, `references artifact "alpha-test" in needs, but it does not appear in the phase's Consumes block`); got != 1 {
		t.Fatalf("expected exactly one W3 warning, got %d: %+v", got, cp.Warnings)
	}
	// The needs-referenced artifact also implies the phase dependency.
	if len(cp.Phases[1].DependsOn) != 1 || cp.Phases[1].DependsOn[0] != 1 {
		t.Fatalf("expected inferred [1], got %+v", cp.Phases[1].DependsOn)
	}
}

func TestCompileSealed_PhaseNameCannotForgeW3(t *testing.T) {
	// A phase NAMED with the W3 message's marker literal must not trick
	// warning classification: W3 problems are tagged structurally at
	// creation, never reclassified by substring-matching the rendered
	// message. The crafted phase carries a real error (unknown consumed
	// artifact) whose message embeds the phase name; it must stay a
	// hard problem and fail the compile.
	md := strings.NewReplacer(
		"### Phase 2: Beta",
		"### Phase 2: does not appear in the phase's Consumes block",
		"- `alpha-art` (file) — the alpha artifact\n\n**Steps:**\n\n1. Wire beta [code] (needs: alpha-art)",
		"- `ghost-art` (file) — the ghost artifact\n\n**Steps:**\n\n1. Wire beta [code]",
	).Replace(validDoc)
	probs := compileProblems(t, md)
	if got := countProblems(probs, `unknown artifact "ghost-art"`); got != 1 {
		t.Fatalf("crafted phase name must not downgrade the real problem; got %+v", probs)
	}
	if len(probs) != 1 {
		t.Fatalf("expected exactly 1 problem, got %+v", probs)
	}
	for _, p := range probs {
		if p.Warning {
			t.Fatalf("real problem tagged as warning: %+v", p)
		}
	}
}

func TestCompileSealed_EarlierPhaseStepRefImpliesDep(t *testing.T) {
	md := strings.Replace(validDoc,
		"1. Wire beta [code] (needs: alpha-art)",
		"1. Wire beta [code] (needs: alpha-art, Phase1.S2)", 1)
	cp, err := CompileSealed(md, 8)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(cp.Phases[1].DependsOn) != 1 || cp.Phases[1].DependsOn[0] != 1 {
		t.Fatalf("expected inferred [1] from earlier-phase step ref, got %+v", cp.Phases[1].DependsOn)
	}
}

// compileCycleCheck is the white-box cycle detector (class 5 is
// unreachable through CompileSealed in v1 — every validated edge points
// backward — so it is exercised directly, per the dialect doc section 7
// note).
func TestCompileSealed_CycleDetector(t *testing.T) {
	names := []string{"A", "B", "C"}
	// A's dependents: C; B's dependents: A; C's dependents: B.
	edges := [][]int{{2}, {0}, {1}}
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
	cp, err := CompileSealed(validDoc, 8)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	sum := sha256.Sum256([]byte(validDoc))
	want := hex.EncodeToString(sum[:])
	if cp.Hash != want {
		t.Fatalf("hash mismatch: %s != %s", cp.Hash, want)
	}
	// Pinned literal: sha256 of validDoc computed independently via
	// shasum -a 256 on the same bytes.
	const wantLiteral = "b6103616bcad4b65b5420b11ed85397ad9c081acb89f4f03b72a695f8dd4f81d"
	if wantLiteral != want {
		t.Fatalf("sanity literal drift: %s != %s", wantLiteral, want)
	}
}

func TestCompileSealed_ErrorRendering(t *testing.T) {
	md := strings.Replace(validDoc, "- version: 1", "- version: 2", 1)
	_, err := CompileSealed(md, 8)
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *CompileError, got %v", err)
	}
	if got := ce.Error(); got != "plan compile failed: 1 problems" {
		t.Fatalf("unexpected Error() %q", got)
	}
}

// ---- Task 4: golden round-trip vs the dialect doc's section-8 examples ----
//
// The examples are embedded verbatim (go:embed) from
// docs/workflows/plan-dialect.md section 8 at extraction time; the test
// asserts the spec'd compile behavior for each.

//go:embed testdata/dialect-8.1-minimal.md
var goldenMinimal string

//go:embed testdata/dialect-8.2-parallel.md
var goldenParallel string

//go:embed testdata/dialect-8.3-errors.md
var goldenErrors string

func TestCompileSealed_GoldenMinimal(t *testing.T) {
	cp, err := CompileSealed(goldenMinimal, 8)
	if err != nil {
		t.Fatalf("8.1 must compile: %v", err)
	}
	if len(cp.Phases) != 2 {
		t.Fatalf("expected 2 phases, got %d", len(cp.Phases))
	}
	// Walk-through: avatar-store consumed by later phase 2 → Required.
	if !cp.Phases[0].Produces[0].Required {
		t.Fatal("avatar-store must be Required")
	}
	if cp.Phases[1].Produces[0].Required {
		t.Fatal("avatar-upload-endpoint must not be Required")
	}
	// Phase 2 DependsOn inferred from the consume (W1) → [1].
	if got := cp.Phases[1].DependsOn; len(got) != 1 || got[0] != 1 {
		t.Fatalf("expected DependsOn [1], got %+v", got)
	}
	if warningsContaining(cp, `depends_on inferred for phase "Upload endpoint"`) != 1 {
		t.Fatalf("expected W1 warning, got %+v", cp.Warnings)
	}
	// Phase1.S1 prior same-phase step ref → step 2 depends on step 1.
	if got := cp.Phases[0].Steps[1].DependsOn; len(got) != 1 || got[0] != 1 {
		t.Fatalf("expected step DependsOn [1], got %+v", got)
	}
	// Tool hints kept verbatim.
	if cp.Phases[0].Steps[1].ToolHint != "code" {
		t.Fatalf("unexpected hint %q", cp.Phases[0].Steps[1].ToolHint)
	}
}

func TestCompileSealed_GoldenParallel(t *testing.T) {
	cp, err := CompileSealed(goldenParallel, 8)
	if err != nil {
		t.Fatalf("8.2 must compile: %v", err)
	}
	if len(cp.Phases) != 3 {
		t.Fatalf("expected 3 phases, got %d", len(cp.Phases))
	}
	// Walk-through: phases 2 and 3 both depend only on phase 1 → the
	// frontier can run them in parallel. DependsOn sets are {1} (the
	// producer's ordinal).
	for i, want := range []struct {
		phase int
		deps  []int
	}{{1, []int{1}}, {2, []int{1}}} {
		got := cp.Phases[want.phase].DependsOn
		if len(got) != len(want.deps) || got[0] != want.deps[0] {
			t.Fatalf("phase %d: expected DependsOn %v, got %+v", want.phase, want.deps, got)
		}
		_ = i
	}
	if warningsContaining(cp, "depends_on inferred") != 2 {
		t.Fatalf("expected two W1 warnings, got %+v", cp.Warnings)
	}
	// config-schema consumed by two later phases → Required.
	if !cp.Phases[0].Produces[0].Required {
		t.Fatal("config-schema must be Required")
	}
	// Phase2.S3 demonstrates an earlier-phase step ref: it implies the
	// phase-1 dependency and leaves phases 2 and 3 uncoupled.
	if got := cp.Phases[1].DependsOn; len(got) != 1 || got[0] != 1 {
		t.Fatalf("phase 2 expected [1], got %+v", got)
	}
	if got := cp.Phases[2].DependsOn; len(got) != 1 || got[0] != 1 {
		t.Fatalf("phase 3 expected [1], got %+v", got)
	}
}

func TestCompileSealed_GoldenErrors(t *testing.T) {
	_, err := CompileSealed(goldenErrors, 8)
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("8.3 must fail compile, got %v", err)
	}
	if got := ce.Error(); got != "plan compile failed: 2 problems" {
		t.Fatalf("expected exactly 2 problems, got %q: %+v", got, ce.Problems)
	}
	// Spec output, verbatim (lines are exact per the dialect doc):
	//   line 18: Open Questions must be empty to seal (1 unresolved)
	//   line 49: unknown artifact "payment-gateway" consumed by phase
	//            "Render CSV": no phase in this plan produces it
	want := []CompileProblem{
		{Line: 18, Message: "Open Questions must be empty to seal (1 unresolved)"},
		{Line: 49, Message: `unknown artifact "payment-gateway" consumed by phase "Render CSV": no phase in this plan produces it`},
	}
	if len(ce.Problems) != len(want) {
		t.Fatalf("problem count %d != %d: %+v", len(ce.Problems), len(want), ce.Problems)
	}
	for i, w := range want {
		if ce.Problems[i] != w {
			t.Fatalf("problem %d:\n got %+v\nwant %+v", i, ce.Problems[i], w)
		}
	}
}
