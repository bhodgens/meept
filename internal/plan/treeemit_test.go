package plan

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// treeFixture is a 3-phase compiled plan: two parallel producers plus one
// consumer of both artifacts. The root emitter's concurrency grouping must
// place the two producers in the same group and the consumer in a later
// group — groups derive from consumes edges, not phase order.
//
// Step counts are chosen so the default options partition into exactly six
// leaves: schema (7 steps) → [3,3,1], engine (4 steps) → [2,2], wiring
// (3 steps) → [3].
func treeFixture() *CompiledPlan {
	return &CompiledPlan{
		Phases: []PhaseSpec{
			{
				Name:        "schema",
				Description: "Define the sealed-dialect schema. It is the contract every later phase builds on.",
				Produces: []Artifact{
					{Name: "schema", Kind: "interface", Description: "the sealed-dialect schema", Required: true},
				},
				Steps: []StepSpec{
					{Description: "Define the schema tokens.", ToolHint: "code"},
					{Description: "Parse schema blocks.", ToolHint: "code"},
					{Description: "Validate schema fields.", ToolHint: "analyze"},
					{Description: "Normalize schema aliases.", ToolHint: "refactor"},
					{Description: "Emit schema diagnostics.", ToolHint: "code"},
					{Description: "Round-trip schema documents.", ToolHint: "debug"},
					{Description: "Benchmark schema parsing.", ToolHint: "analyze"},
				},
			},
			{
				Name:        "engine",
				Description: "Implement the execution engine that consumes the schema.",
				Produces: []Artifact{
					{Name: "engine-core", Kind: "interface", Description: "the engine core interface", Required: true},
				},
				Steps: []StepSpec{
					{Description: "Implement the core engine loop.", ToolHint: "code"},
					{Description: "Wire engine teardown.", ToolHint: "refactor"},
					{Description: "Add engine retry policy.", ToolHint: "code"},
					{Description: "Instrument engine metrics.", ToolHint: "code"},
				},
			},
			{
				Name:        "wiring",
				Description: "Wire schema and engine into the daemon surface.",
				Consumes: []Artifact{
					{Name: "schema", Kind: "interface", Description: "the sealed-dialect schema", Required: true},
					{Name: "engine-core", Kind: "interface", Description: "the engine core interface", Required: true},
				},
				Steps: []StepSpec{
					{Description: "Connect schema to engine.", ToolHint: "code"},
					{Description: "Expose wiring endpoints.", ToolHint: "code"},
					{Description: "Document wiring invariants.", ToolHint: "plan"},
				},
			},
		},
		Hash:     "deadbeefcafe0123456789abcdef0123456789abcdef0123456789abcdef0123",
		Warnings: []string{"phase 3 depends_on inferred from consumes"},
	}
}

// extractSection returns the lines of doc from the given "## ..." heading up
// to (not including) the next "## " heading.
func extractSection(doc, heading string) string {
	lines := strings.Split(doc, "\n")
	start := -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, heading) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

func TestShouldEmitTree(t *testing.T) {
	small := &CompiledPlan{Phases: []PhaseSpec{{
		Name: "only", Steps: []StepSpec{
			{Description: "a"}, {Description: "b"},
		}}},
	} // 2 steps, 1 phase → flat
	require.False(t, ShouldEmitTree(small, TreeEmitOptions{}))

	big := &CompiledPlan{Phases: []PhaseSpec{{
		Name: "p1", Description: strings.Repeat("x", 20_000),
		Steps: []StepSpec{{Description: "a"}},
	}}}
	require.True(t, ShouldEmitTree(big, TreeEmitOptions{}))

	tests := []struct {
		name string
		cp   *CompiledPlan
		opts TreeEmitOptions
		want bool
	}{
		{
			name: "nil plan is flat",
			cp:   nil,
			want: false,
		},
		{
			name: "six steps across two fitting phases stays flat",
			cp: &CompiledPlan{Phases: []PhaseSpec{
				{Name: "a", Steps: []StepSpec{
					{Description: "1"}, {Description: "2"}, {Description: "3"},
				}},
				{Name: "b", Steps: []StepSpec{
					{Description: "4"}, {Description: "5"}, {Description: "6"},
				}},
			}},
			want: false,
		},
		{
			name: "nine fitting steps across three phases exceeds the flat total",
			cp: &CompiledPlan{Phases: []PhaseSpec{
				{Name: "a", Steps: []StepSpec{
					{Description: "1"}, {Description: "2"}, {Description: "3"},
				}},
				{Name: "b", Steps: []StepSpec{
					{Description: "4"}, {Description: "5"}, {Description: "6"},
				}},
				{Name: "c", Steps: []StepSpec{
					{Description: "7"}, {Description: "8"}, {Description: "9"},
				}},
			}},
			want: true,
		},
		{
			name: "seven steps in one phase goes tree",
			cp: &CompiledPlan{Phases: []PhaseSpec{{Name: "p", Steps: []StepSpec{
				{Description: "1"}, {Description: "2"}, {Description: "3"},
				{Description: "4"}, {Description: "5"}, {Description: "6"},
				{Description: "7"},
			}}}},
			want: true,
		},
		{
			name: "phase over the leaf cap goes tree",
			cp: &CompiledPlan{Phases: []PhaseSpec{{Name: "p", Steps: []StepSpec{
				{Description: "1"}, {Description: "2"}, {Description: "3"}, {Description: "4"},
			}}}},
			opts: TreeEmitOptions{MaxLeavesPerPhase: 3},
			want: true,
		},
		{
			name: "tiny char budget forces tree",
			cp: &CompiledPlan{Phases: []PhaseSpec{{
				Name:        "p",
				Description: strings.Repeat("x", 200),
				Steps:       []StepSpec{{Description: "1"}, {Description: "2"}},
			}}},
			opts: TreeEmitOptions{LeafCharBudget: 10},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ShouldEmitTree(tt.cp, tt.opts))
		})
	}
}

func TestEmitTree_RootHasRequiredSections(t *testing.T) {
	cp := treeFixture()
	tree, err := EmitTree(cp, TreeEmitOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, tree.Root)
	// [3,3,1] + [2,2] + [3] leaves under default options.
	require.Len(t, tree.Leaves, 6)

	for _, section := range RequiredRootSections {
		require.True(t, sectionPresent(tree.Root, section),
			"root missing required section %q", section)
	}

	// Interface Contracts: one per phase.
	require.Equal(t, len(cp.Phases), strings.Count(tree.Root, "### Contract "))

	// Child Document Index: one row per leaf.
	childIdx := extractSection(tree.Root, "## Child Document Index")
	rows := 0
	for _, line := range strings.Split(childIdx, "\n") {
		if strings.HasPrefix(line, "| ") && strings.Contains(line, ".md |") {
			rows++
		}
	}
	require.Equal(t, len(tree.Leaves), rows)

	// Concurrency groups: the two producers share group 1; the consumer
	// lands in a later group — derived from consumes edges, not phase order.
	for _, leaf := range tree.Leaves {
		var row string
		for _, line := range strings.Split(childIdx, "\n") {
			if strings.Contains(line, leaf.Path) {
				row = line
				break
			}
		}
		require.NotEmpty(t, row, "no child-index row for %s", leaf.Path)
		row = strings.TrimRight(row, " \t")
		require.True(t, strings.HasSuffix(row, "| 1 |") || strings.HasSuffix(row, "| 2 |"),
			"row %q has no concurrency group cell", row)
		switch leaf.Path[:2] {
		case "01", "02", "03", "04", "05":
			require.True(t, strings.HasSuffix(row, "| 1 |"),
				"producer leaf %s not in group 1: %q", leaf.Path, row)
		case "06":
			require.True(t, strings.HasSuffix(row, "| 2 |"),
				"consumer leaf %s not in a later group: %q", leaf.Path, row)
		}
	}
}

func TestEmitTree_LeafHasRequiredSections(t *testing.T) {
	tree, err := EmitTree(treeFixture(), TreeEmitOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, tree.Leaves)
	for _, leaf := range tree.Leaves {
		for _, section := range RequiredLeafSections {
			require.True(t, sectionPresent(leaf.Content, section),
				"leaf %s missing required section %q", leaf.Path, section)
		}
		require.Contains(t, leaf.Content, "DO NOT COMMIT",
			"leaf %s missing the DO NOT COMMIT rule", leaf.Path)
	}
}

func TestEmitTree_LeafPartitioning(t *testing.T) {
	// 4-step phase, MaxLeavesPerPhase=3 ⇒ 2 leaves; no step duplicated;
	// union of leaf steps == phase steps; order preserved.
	phase := PhaseSpec{
		Name:        "p",
		Description: "One phase.",
		Steps: []StepSpec{
			{Description: "step one."}, {Description: "step two."},
			{Description: "step three."}, {Description: "step four."},
		},
	}
	tree, err := EmitTree(&CompiledPlan{Phases: []PhaseSpec{phase}},
		TreeEmitOptions{MaxLeavesPerPhase: 3})
	require.NoError(t, err)
	require.Len(t, tree.Leaves, 2)

	var got []string
	seen := map[string]bool{}
	for _, leaf := range tree.Leaves {
		for _, s := range phase.Steps {
			if !strings.Contains(leaf.Content, "**Objective:** "+s.Description) {
				continue
			}
			require.False(t, seen[s.Description], "step duplicated: %s", s.Description)
			seen[s.Description] = true
			got = append(got, s.Description)
		}
	}
	require.Len(t, got, len(phase.Steps), "union of leaf steps != phase steps")
	for i, s := range phase.Steps {
		require.Equal(t, s.Description, got[i], "order not preserved at step %d", i)
	}

	// LeafCharBudget forces a split of an oversized phase even when the
	// leaf-cap sizing alone would have kept it in 2 leaves.
	long := strings.Repeat("x", 300)
	big := PhaseSpec{
		Name:        "big",
		Description: "Big phase.",
		Steps: []StepSpec{
			{Description: long}, {Description: long},
			{Description: long}, {Description: long},
		},
	}
	tree2, err := EmitTree(&CompiledPlan{Phases: []PhaseSpec{big}},
		TreeEmitOptions{MaxLeavesPerPhase: 3, LeafCharBudget: 300})
	require.NoError(t, err)
	// Cap 3 alone gives 2 leaves ([2,2]); the 300-char budget forces 4.
	require.Len(t, tree2.Leaves, 4)
}

func TestEmitTree_Determinism(t *testing.T) {
	cp := treeFixture()
	first, err := EmitTree(cp, TreeEmitOptions{})
	require.NoError(t, err)
	second, err := EmitTree(cp, TreeEmitOptions{})
	require.NoError(t, err)
	require.Equal(t, first.Root, second.Root, "root not byte-identical across renders")
	require.Len(t, second.Leaves, len(first.Leaves))
	for i := range first.Leaves {
		require.Equal(t, first.Leaves[i], second.Leaves[i],
			"leaf %d not byte-identical across renders", i)
	}

	// Golden: the root's Meta section renders verbatim, and the Child
	// Document Index header + row shapes are pinned.
	require.Contains(t, first.Root, goldenMeta)
	childIdx := extractSection(first.Root, "## Child Document Index")
	require.True(t, strings.HasPrefix(childIdx, goldenChildIndexHeader),
		"child index header drifted:\n%s", childIdx)
	for _, want := range goldenChildRows {
		require.Contains(t, first.Root, want)
	}
}

const goldenMeta = `## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 6
- **Scope:** Execute the compiled plan as a hierarchical tree; every leaf bounds one implementation agent's scope of work.
- **Sealed input:** sha256:deadbeefcafe0123456789abcdef0123456789abcdef0123456789abcdef0123`

const goldenChildIndexHeader = `## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|--------------|--------------|-------------|`

var goldenChildRows = []string{
	"| 1 | 01-define-the-schema-tokens.md | leaf | none |",
	"| 6 | 06-connect-schema-to-engine.md | leaf | 01-define-the-schema-tokens.md, 04-implement-the-core-engine-loop.md |",
}

func TestEmitTree_RejectsEmptyInput(t *testing.T) {
	_, err := EmitTree(nil, TreeEmitOptions{})
	require.Error(t, err)
	_, err = EmitTree(&CompiledPlan{}, TreeEmitOptions{})
	require.Error(t, err)
}

// --- leafDependencies: in-phase chaining (M14) -------------------------------

func TestEmitTree_SplitPhaseLeavesChainInOrder(t *testing.T) {
	// A 4-step phase with MaxLeavesPerPhase=3 splits into 2 leaves
	// (perLeaf=2 ⇒ [2,2]). The leaves share one concurrency group (no
	// consumes edges anywhere in the plan) but must NOT claim
	// independence: leaf 2 depends on leaf 1 — in the Child Document
	// Index, the leaf's own Dependencies line, and the Dispatch Protocol.
	phase := PhaseSpec{
		Name:        "solo",
		Description: "One phase split across leaves.",
		Steps: []StepSpec{
			{Description: "step one."}, {Description: "step two."},
			{Description: "step three."}, {Description: "step four."},
		},
	}
	tree, err := EmitTree(&CompiledPlan{Phases: []PhaseSpec{phase}},
		TreeEmitOptions{MaxLeavesPerPhase: 3})
	require.NoError(t, err)
	require.Len(t, tree.Leaves, 2)

	leaf01, leaf02 := tree.Leaves[0].Path, tree.Leaves[1].Path

	// Child Document Index: leaf 1 none, leaf 2 → leaf 1.
	require.Contains(t, tree.Root, "| 1 | "+leaf01+" | leaf | none |")
	require.Contains(t, tree.Root, "| 2 | "+leaf02+" | leaf | "+leaf01+" |")

	// The leaf document itself names its in-phase predecessor.
	require.Contains(t, tree.Leaves[1].Content, "**Dependencies:** "+leaf01+"\n")
	require.Contains(t, tree.Leaves[0].Content, "**Dependencies:** none\n")

	// The Dispatch Protocol no longer tells leaves within a group they
	// are independent; the chaining rule is spelled out.
	proto := extractSection(tree.Root, "## Dispatch Protocol")
	require.NotContains(t, proto, "independent — dispatch them together")
	require.Contains(t, proto, leaf01)
}

func TestEmitTree_SplitPhaseKeepsArtifactEdges(t *testing.T) {
	// Phase consume consumes phase produce's artifact AND splits into two
	// leaves: leaf 2 of consume chains to leaf 1 of consume (in-phase
	// rule) and still names produce's leaf (artifact rule) — chaining
	// adds edges, never drops consumes-derived ones.
	produce := PhaseSpec{
		Name:        "produce",
		Description: "Producer phase.",
		Produces:    []Artifact{{Name: "art", Kind: "file", Description: "the artifact"}},
		Steps:       []StepSpec{{Description: "make the artifact."}},
	}
	consume := PhaseSpec{
		Name:        "consume",
		Description: "Consumer phase split across leaves.",
		Consumes:    []Artifact{{Name: "art", Kind: "file", Description: "the artifact"}},
		Steps: []StepSpec{
			{Description: "step one."}, {Description: "step two."},
			{Description: "step three."}, {Description: "step four."},
		},
	}
	tree, err := EmitTree(&CompiledPlan{Phases: []PhaseSpec{produce, consume}},
		TreeEmitOptions{MaxLeavesPerPhase: 3})
	require.NoError(t, err)
	require.Len(t, tree.Leaves, 3) // produce → [1], consume → [2,1]

	leafP1, leafC1, leafC2 := tree.Leaves[0].Path, tree.Leaves[1].Path, tree.Leaves[2].Path
	// leafC2's dependencies name BOTH its in-phase predecessor (first)
	// and the artifact producer.
	require.Contains(t, tree.Root, "| 3 | "+leafC2+" | leaf | "+leafC1+", "+leafP1+" |")
	require.Contains(t, tree.Leaves[2].Content, "**Dependencies:** "+leafC1+", "+leafP1+"\n")
}
