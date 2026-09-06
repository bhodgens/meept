package agent

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Contract A (master.md / leaf 01) presents the frontier identifiers as
// PhaseNode / ComputePhaseFrontier, but the package convention is
// unexported names (checkPhaseReady, artifactStore); leaf 02 consumes
// them from within package agent. This assertion pins that mapping at
// compile time.
var (
	_ = phaseNode{}
	_ = computePhaseFrontier
)

// pfReq builds a required artifact declaration for fixtures.
func pfReq(name string) Artifact { return Artifact{Name: name, Kind: "file", Required: true} }

// pfOpt builds an optional artifact declaration for fixtures.
func pfOpt(name string) Artifact { return Artifact{Name: name, Kind: "file"} }

// pfPhase builds a phaseNode with only the fields a case needs.
func pfPhase(name string, seq int, produces []string, consumes []Artifact, dependsOn ...string) phaseNode {
	return phaseNode{
		Name:           name,
		Sequence:       seq,
		Produces:       produces,
		Consumes:       consumes,
		DependsOnPhase: dependsOn,
	}
}

// pfReadyNames maps ready nodes to their names in output order.
func pfReadyNames(ready []phaseNode) []string {
	names := make([]string, 0, len(ready))
	for _, n := range ready {
		names = append(names, n.Name)
	}
	return names
}

// pfDeepCopyNodes copies nodes including inner slices so purity
// comparisons see any in-place mutation of nested fields.
func pfDeepCopyNodes(ns []phaseNode) []phaseNode {
	out := make([]phaseNode, len(ns))
	for i, n := range ns {
		n.Produces = append([]string(nil), n.Produces...)
		n.Consumes = append([]Artifact(nil), n.Consumes...)
		n.DependsOnPhase = append([]string(nil), n.DependsOnPhase...)
		out[i] = n
	}
	return out
}

// pfCopySet copies a bool set for purity comparisons.
func pfCopySet(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// pfDiamond builds the A→(B,C)→D diamond: A produces x; B consumes x and
// produces y; C produces z; D consumes y and z.
func pfDiamond() []phaseNode {
	return []phaseNode{
		pfPhase("A", 1, []string{"x"}, nil),
		pfPhase("B", 2, []string{"y"}, []Artifact{pfReq("x")}),
		pfPhase("C", 3, []string{"z"}, nil),
		pfPhase("D", 4, nil, []Artifact{pfReq("y"), pfReq("z")}),
	}
}

func TestComputePhaseFrontier(t *testing.T) {
	tests := []struct {
		name      string
		nodes     []phaseNode
		available map[string]bool
		busy      map[string]bool
		wantReady []string
		wantNil   bool
		wantCycle bool
	}{
		{
			name:    "1: empty graph yields nil ready and no cycle",
			wantNil: true,
		},
		{
			name: "2: all-ready linear chain",
			nodes: []phaseNode{
				pfPhase("A", 1, []string{"x"}, nil),
				pfPhase("B", 2, []string{"y"}, []Artifact{pfReq("x")}),
				pfPhase("C", 3, nil, []Artifact{pfReq("y")}),
			},
			available: map[string]bool{"x": true, "y": true},
			wantReady: []string{"A", "B", "C"},
		},
		{
			name: "3: required-consume edge into busy phase blocks",
			nodes: []phaseNode{
				pfPhase("A", 1, []string{"x"}, nil),
				pfPhase("B", 2, nil, []Artifact{pfReq("x")}),
			},
			busy:      map[string]bool{"A": true},
			wantReady: []string{"A"},
		},
		{
			name: "4: optional consume never gates readiness",
			nodes: []phaseNode{
				pfPhase("A", 1, []string{"x"}, nil),
				pfPhase("B", 2, nil, []Artifact{pfOpt("x")}),
			},
			available: map[string]bool{},
			wantReady: []string{"A", "B"},
		},
		{
			name:      "5: missing required consume with no in-graph producer stalls the graph",
			nodes:     []phaseNode{pfPhase("B", 1, nil, []Artifact{pfReq("x")})},
			available: map[string]bool{},
			// Frozen rule 4: empty ready over a non-empty incomplete set
			// is cycleDetected — the leaf table row says "false", but the
			// normative semantics win (see leaf-01 report deviation note).
			wantCycle: true,
		},
		{
			name:      "6: required consume satisfied by the store passes without an in-graph producer",
			nodes:     []phaseNode{pfPhase("B", 1, nil, []Artifact{pfReq("x")})},
			available: map[string]bool{"x": true},
			wantReady: []string{"B"},
		},
		{
			name: "7: optional consume still creates an edge into a busy phase",
			nodes: []phaseNode{
				pfPhase("A", 1, []string{"x"}, nil),
				pfPhase("B", 2, nil, []Artifact{pfOpt("x")}),
			},
			available: map[string]bool{},
			busy:      map[string]bool{"A": true},
			// B is edge-blocked; A itself has no gating.
			wantReady: []string{"A"},
		},
		{
			name:      "8: diamond fully ready",
			nodes:     pfDiamond(),
			available: map[string]bool{"x": true, "y": true, "z": true},
			wantReady: []string{"A", "B", "C", "D"},
		},
		{
			name:      "9: diamond mid-flight — busy B blocks D, missing x blocks B",
			nodes:     pfDiamond(),
			available: map[string]bool{},
			busy:      map[string]bool{"B": true},
			wantReady: []string{"A", "C"},
		},
		{
			name: "10: explicit DependsOnPhase edge into busy phase blocks",
			nodes: []phaseNode{
				pfPhase("A", 1, nil, nil),
				pfPhase("B", 2, nil, nil, "A"),
			},
			available: map[string]bool{},
			busy:      map[string]bool{"A": true},
			wantReady: []string{"A"},
		},
		{
			name:      "11: DependsOnPhase to a phase outside the graph creates no edge",
			nodes:     []phaseNode{pfPhase("B", 1, nil, nil, "Z")},
			available: map[string]bool{},
			wantReady: []string{"B"},
		},
		{
			name: "12: self-produce creates no self-edge",
			nodes: []phaseNode{
				pfPhase("A", 1, []string{"a"}, []Artifact{pfReq("a")}),
			},
			available: map[string]bool{"a": true},
			busy:      map[string]bool{"A": true},
			wantReady: []string{"A"},
		},
		{
			name: "13: Sequence ascending with Name tiebreak",
			nodes: []phaseNode{
				pfPhase("B", 2, nil, nil),
				pfPhase("A", 1, nil, nil),
			},
			available: map[string]bool{},
			wantReady: []string{"A", "B"},
		},
		{
			name: "14: Sequence never defeats an edge block",
			// A is busy AND gate-blocked (its own required consume w is
			// missing), so only C passes: B (seq 1) is edge-blocked by busy
			// A and must not outrank C (seq 9). Frozen rule 2 never excludes
			// a busy phase on its own — only gate failures and busy-edge
			// blocks keep a phase out.
			nodes: []phaseNode{
				pfPhase("A", 1, []string{"x"}, []Artifact{pfReq("w")}),
				pfPhase("B", 1, nil, []Artifact{pfReq("x")}),
				pfPhase("C", 9, nil, nil),
			},
			available: map[string]bool{},
			busy:      map[string]bool{"A": true},
			wantReady: []string{"C"},
		},
		{
			name: "15: two-phase produce/consume stall is a cycle",
			nodes: []phaseNode{
				pfPhase("A", 1, []string{"y"}, []Artifact{pfReq("x")}),
				pfPhase("B", 2, []string{"x"}, []Artifact{pfReq("y")}),
			},
			available: map[string]bool{},
			busy:      map[string]bool{"A": true, "B": true},
			wantCycle: true,
		},
		{
			name: "16: duplicate names keep the first occurrence only",
			nodes: []phaseNode{
				pfPhase("A", 1, []string{"x"}, nil),
				pfPhase("A", 7, []string{"x"}, nil),
			},
			available: map[string]bool{"x": true},
			wantReady: []string{"A"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, cycleDetected := computePhaseFrontier(tt.nodes, tt.available, tt.busy)
			assert.Equal(t, tt.wantCycle, cycleDetected, "cycleDetected mismatch")
			if tt.wantCycle {
				assert.Empty(t, got, "ready must be empty when the graph cannot advance")
				return
			}
			require.False(t, cycleDetected)
			if tt.wantNil {
				assert.Nil(t, got, "empty input must return a nil ready set")
				return
			}
			assert.Equal(t, tt.wantReady, pfReadyNames(got), "ready set (ordered)")
		})
	}
}

func TestComputePhaseFrontier_OutputOrdering(t *testing.T) {
	t.Run("Sequence ascending, Name ascending on ties", func(t *testing.T) {
		nodes := []phaseNode{
			pfPhase("gamma", 2, nil, nil),
			pfPhase("beta", 1, nil, nil),
			pfPhase("alpha", 2, nil, nil),
			pfPhase("delta", 1, nil, nil),
		}
		got, cycleDetected := computePhaseFrontier(nodes, map[string]bool{}, nil)
		require.False(t, cycleDetected)
		assert.Equal(t, []string{"beta", "delta", "alpha", "gamma"}, pfReadyNames(got))
	})

	t.Run("a low Sequence never rescues an edge-blocked phase", func(t *testing.T) {
		nodes := []phaseNode{
			pfPhase("X", 0, []string{"x"}, nil),
			pfPhase("Y", 1, nil, []Artifact{pfReq("x")}),
			pfPhase("Z", 999, nil, nil),
		}
		got, cycleDetected := computePhaseFrontier(nodes, map[string]bool{}, map[string]bool{"X": true})
		require.False(t, cycleDetected)
		// Frozen rule 2 never drops a busy phase on its own: ungated busy
		// X stays in the ready set. What Sequence must NOT do is rescue
		// edge-blocked Y (seq 1) ahead of Z (seq 999) — the property under
		// test is Y's absence and the surviving relative order.
		assert.Equal(t, []string{"X", "Z"}, pfReadyNames(got))
	})
}

func TestComputePhaseFrontier_Purity(t *testing.T) {
	nodes := []phaseNode{
		pfPhase("A", 1, []string{"x"}, nil),
		pfPhase("B", 2, nil, []Artifact{pfReq("x"), pfOpt("q")}),
		pfPhase("C", 3, nil, nil, "A"),
	}
	available := map[string]bool{"x": true}
	busy := map[string]bool{"A": true}

	// Snapshots taken before any call; the function must not disturb
	// inputs and must not vary between identical calls.
	nodesSnapshot := pfDeepCopyNodes(nodes)
	availableSnapshot := pfCopySet(available)
	busySnapshot := pfCopySet(busy)

	got1, cycle1 := computePhaseFrontier(nodes, available, busy)
	got2, cycle2 := computePhaseFrontier(nodes, available, busy)

	assert.Equal(t, cycle1, cycle2)
	assert.Equal(t, got1, got2, "identical inputs must produce identical results")

	assert.True(t, reflect.DeepEqual(nodes, nodesSnapshot), "nodes input was mutated")
	assert.True(t, reflect.DeepEqual(available, availableSnapshot), "availableArtifacts input was mutated")
	assert.True(t, reflect.DeepEqual(busy, busySnapshot), "busyPhases input was mutated")

	// Guard the guard: the comparator above detects a corrupted input.
	nodesSnapshot[0].Name = "CORRUPTED"
	assert.False(t, reflect.DeepEqual(nodes, nodesSnapshot),
		"purity comparator failed to detect a deliberate mutation")
}
