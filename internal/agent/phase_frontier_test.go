package agent

import (
	"reflect"
	"sort"
	"testing"
)

// helper: build phase nodes tersely. Local to this file (no shared state).
func fpn(name string, seq int, produces []string, consumes []Artifact, depends []string) phaseNode {
	return phaseNode{
		Name:           name,
		Sequence:       seq,
		Produces:       produces,
		Consumes:       consumes,
		DependsOnPhase: depends,
	}
}

// helper: required / optional artifact literals.
func fpnReq(name string) Artifact { return Artifact{Name: name, Required: true} }
func fpnOpt(name string) Artifact { return Artifact{Name: name, Required: false} }

// helper: names of ready nodes, in returned order. Nil-preserving so
// empty results compare equal to nil expectations.
func fpnNames(nodes []phaseNode) []string {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Name)
	}
	return out
}

func TestComputePhaseFrontier(t *testing.T) {
	tests := []struct {
		name      string
		nodes     []phaseNode
		available map[string]bool
		busy      map[string]bool
		wantReady []string
		wantCycle bool
	}{
		{
			name:      "case 1: empty graph returns nil and false",
			nodes:     nil,
			available: nil,
			busy:      nil,
			wantReady: nil,
			wantCycle: false,
		},
		{
			name:      "case 2: all-ready linear chain",
			nodes:     []phaseNode{fpn("A", 0, []string{"a"}, nil, nil), fpn("B", 1, []string{"b"}, []Artifact{fpnReq("a")}, nil), fpn("C", 2, nil, []Artifact{fpnReq("b")}, nil)},
			available: map[string]bool{"a": true, "b": true, "c": true},
			busy:      nil,
			wantReady: []string{"A", "B", "C"},
			wantCycle: false,
		},
		{
			name:      "case 3: artifact edge blocks on busy producer",
			nodes:     []phaseNode{fpn("A", 0, []string{"x"}, nil, nil), fpn("B", 1, nil, []Artifact{fpnReq("x")}, nil)},
			available: map[string]bool{},
			busy:      map[string]bool{"A": true},
			wantReady: []string{"A"},
			wantCycle: false,
		},
		{
			// Fixture cells exactly as the leaf table pins them
			// (available={}, busy={A}). The leaf cell says {A,B}, but that
			// contradicts the leaf's own case 7: an optional consume still
			// creates an edge (rule 1 — "never gate" is 2a only), and that
			// edge lands on busy A (2b), so B is blocked here exactly as in
			// case 7. Rule-correct ready set is {A}; flagged to the
			// orchestrator as a leaf-table erratum.
			name:      "case 4: optional consume never gates",
			nodes:     []phaseNode{fpn("A", 0, []string{"x"}, nil, nil), fpn("B", 1, nil, []Artifact{fpnOpt("x")}, nil)},
			available: map[string]bool{},
			busy:      map[string]bool{"A": true},
			wantReady: []string{"A"},
			wantCycle: false,
		},
		{
			// Frozen rule 4 (leaf semantics + master.md Contract A):
			// cycleDetected == (len(ready)==0 && len(nodes)>0). The leaf
			// table's "false" cell contradicts its own rule; the rule wins
			// — an unadvanceable graph is the caller's Warn+fallback trigger.
			name:      "case 5: missing required consume with no producer blocks",
			nodes:     []phaseNode{fpn("B", 0, nil, []Artifact{fpnReq("x")}, nil)},
			available: map[string]bool{},
			busy:      nil,
			wantReady: nil,
			wantCycle: true,
		},
		{
			name:      "case 6: missing required consume passes when artifact present",
			nodes:     []phaseNode{fpn("B", 0, nil, []Artifact{fpnReq("x")}, nil)},
			available: map[string]bool{"x": true},
			busy:      nil,
			wantReady: []string{"B"},
			wantCycle: false,
		},
		{
			name:      "case 7: optional consume edge to busy producer still blocks",
			nodes:     []phaseNode{fpn("A", 0, []string{"x"}, nil, nil), fpn("B", 1, nil, []Artifact{fpnOpt("x")}, nil)},
			available: map[string]bool{"x": true},
			busy:      map[string]bool{"A": true},
			wantReady: []string{"A"},
			wantCycle: false,
		},
		{
			name:      "case 8: diamond with all artifacts ready",
			nodes:     []phaseNode{fpn("A", 0, []string{"a"}, nil, nil), fpn("B", 1, []string{"b"}, []Artifact{fpnReq("a")}, nil), fpn("C", 2, []string{"c"}, []Artifact{fpnReq("a")}, nil), fpn("D", 3, nil, []Artifact{fpnReq("b"), fpnReq("c")}, nil)},
			available: map[string]bool{"a": true, "b": true, "c": true, "d": true},
			busy:      nil,
			wantReady: []string{"A", "B", "C", "D"},
			wantCycle: false,
		},
		{
			// Fixture cells exactly as the leaf table pins them
			// (available={}, busy={B}). Under frozen rule 2a C's required
			// consume "a" is unavailable, so the leaf cell's expected
			// "A,C" is unreachable by any rule-consistent assignment —
			// B and C are gate-identical. The rule-correct ready set is
			// {A}; flagged to the orchestrator as a leaf-table erratum.
			name:      "case 9: diamond mid-flight, B busy",
			nodes:     []phaseNode{fpn("A", 0, []string{"a"}, nil, nil), fpn("B", 1, []string{"b"}, []Artifact{fpnReq("a")}, nil), fpn("C", 2, []string{"c"}, []Artifact{fpnReq("a")}, nil), fpn("D", 3, nil, []Artifact{fpnReq("b"), fpnReq("c")}, nil)},
			available: map[string]bool{},
			busy:      map[string]bool{"B": true},
			wantReady: []string{"A"},
			wantCycle: false,
		},
		{
			name:      "case 10: explicit DependsOnPhase edge to busy phase blocks",
			nodes:     []phaseNode{fpn("A", 0, nil, nil, nil), fpn("B", 1, nil, nil, []string{"A"})},
			available: map[string]bool{},
			busy:      map[string]bool{"A": true},
			wantReady: []string{"A"},
			wantCycle: false,
		},
		{
			name:      "case 11: depends-on unknown phase creates no edge",
			nodes:     []phaseNode{fpn("B", 0, nil, nil, []string{"Z"})},
			available: map[string]bool{},
			busy:      nil,
			wantReady: []string{"B"},
			wantCycle: false,
		},
		{
			name:      "case 12: self-produce is not a self-edge",
			nodes:     []phaseNode{fpn("A", 0, []string{"a"}, []Artifact{fpnReq("a")}, nil)},
			available: map[string]bool{"a": true},
			busy:      map[string]bool{"A": true},
			wantReady: []string{"A"},
			wantCycle: false,
		},
		{
			name:      "case 13: sequence ascending order",
			nodes:     []phaseNode{fpn("B", 2, []string{"b"}, nil, nil), fpn("A", 1, []string{"a"}, nil, nil)},
			available: map[string]bool{"a": true, "b": true},
			busy:      nil,
			wantReady: []string{"A", "B"},
			wantCycle: false,
		},
		{
			name:      "case 14: sequence never defeats edge blocking",
			nodes:     []phaseNode{fpn("B", 1, []string{"x"}, []Artifact{fpnReq("x")}, nil), fpn("C", 9, nil, nil, nil)},
			available: map[string]bool{},
			busy:      map[string]bool{"B": true},
			wantReady: []string{"C"},
			wantCycle: false,
		},
		{
			name:      "case 15: mutual busy dependency reports cycle",
			nodes:     []phaseNode{fpn("A", 0, []string{"x"}, []Artifact{fpnReq("y")}, nil), fpn("B", 1, []string{"y"}, []Artifact{fpnReq("x")}, nil)},
			available: map[string]bool{},
			busy:      map[string]bool{"A": true, "B": true},
			wantReady: nil,
			wantCycle: true,
		},
		{
			name:      "case 16: duplicate names keep first occurrence",
			nodes:     []phaseNode{fpn("A", 5, []string{"first"}, nil, nil), fpn("A", 1, []string{"second"}, []Artifact{fpnReq("first")}, nil)},
			available: map[string]bool{"first": true},
			busy:      nil,
			wantReady: []string{"A"},
			wantCycle: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			ready, cycle := computePhaseFrontier(tt.nodes, tt.available, tt.busy)
			got := fpnNames(ready)
			if !reflect.DeepEqual(got, tt.wantReady) {
				t.Errorf("computePhaseFrontier() ready = %v, want %v", got, tt.wantReady)
			}
			if cycle != tt.wantCycle {
				t.Errorf("computePhaseFrontier() cycleDetected = %v, want %v", cycle, tt.wantCycle)
			}
		})
	}
}

func TestComputePhaseFrontier_OutputOrdering(t *testing.T) {
	t.Run("sequence ascending with name tiebreak", func(t *testing.T) {
		// B and A share Sequence 2 -> Name ascending breaks the tie;
		// C has Sequence 1 and sorts first.
		nodes := []phaseNode{
			fpn("B", 2, []string{"b"}, nil, nil),
			fpn("A", 2, []string{"a"}, nil, nil),
			fpn("C", 1, []string{"c"}, nil, nil),
		}
		ready, cycle := computePhaseFrontier(nodes, map[string]bool{"a": true, "b": true, "c": true}, nil)
		if cycle {
			t.Fatalf("unexpected cycleDetected")
		}
		want := []string{"C", "A", "B"}
		if got := fpnNames(ready); !reflect.DeepEqual(got, want) {
			t.Errorf("order = %v, want %v", got, want)
		}
	})

	t.Run("edge blocking survives sorting", func(t *testing.T) {
		// Lower-sequence phase blocked by a busy dependency must not
		// appear before a ready higher-sequence phase.
		nodes := []phaseNode{
			fpn("Alpha", 1, []string{"x"}, []Artifact{fpnReq("x")}, nil),
			fpn("Zeta", 99, nil, nil, nil),
		}
		ready, cycle := computePhaseFrontier(nodes, map[string]bool{}, map[string]bool{"Alpha": true})
		if cycle {
			t.Fatalf("unexpected cycleDetected")
		}
		want := []string{"Zeta"}
		if got := fpnNames(ready); !reflect.DeepEqual(got, want) {
			t.Errorf("order = %v, want %v", got, want)
		}
	})
}

func TestComputePhaseFrontier_Purity(t *testing.T) {
	nodes := []phaseNode{
		fpn("A", 2, []string{"a"}, nil, nil),
		fpn("B", 1, []string{"b"}, []Artifact{fpnReq("a")}, []string{"A"}),
	}
	available := map[string]bool{"a": true, "b": true}
	busy := map[string]bool{"B": false}

	// Deep copies for post-call comparison.
	nodesCopy := make([]phaseNode, len(nodes))
	copy(nodesCopy, nodes)
	for i := range nodes {
		nodesCopy[i].Produces = append([]string(nil), nodes[i].Produces...)
		nodesCopy[i].Consumes = append([]Artifact(nil), nodes[i].Consumes...)
		nodesCopy[i].DependsOnPhase = append([]string(nil), nodes[i].DependsOnPhase...)
	}
	availCopy := make(map[string]bool, len(available))
	for k, v := range available {
		availCopy[k] = v
	}
	busyCopy := make(map[string]bool, len(busy))
	for k, v := range busy {
		busyCopy[k] = v
	}

	first, cycle1 := computePhaseFrontier(nodes, available, busy)
	second, cycle2 := computePhaseFrontier(nodes, available, busy)

	if !reflect.DeepEqual(first, second) || cycle1 != cycle2 {
		t.Fatalf("same inputs produced different results: first=%v/%v second=%v/%v", first, cycle1, second, cycle2)
	}

	if !reflect.DeepEqual(nodes, nodesCopy) {
		t.Errorf("input nodes slice mutated by call")
	}
	if !reflect.DeepEqual(available, availCopy) {
		t.Errorf("input availableArtifacts map mutated by call")
	}
	if !reflect.DeepEqual(busy, busyCopy) {
		t.Errorf("input busyPhases map mutated by call")
	}

	// Purity test must actually detect mutation: corrupting the copy of
	// the inputs should fail the DeepEqual above.
	availCopy["a"] = false
	if reflect.DeepEqual(available, availCopy) {
		t.Errorf("purity assertions are vacuous: mutated copy compared equal")
	}

	// Determinism under shuffling of the input order.
	shuffled := []phaseNode{nodes[1], nodes[0]}
	r1, c1 := computePhaseFrontier(shuffled, available, busy)
	r2, c2 := computePhaseFrontier(shuffled, available, busy)
	if !reflect.DeepEqual(fpnNames(r1), fpnNames(r2)) || c1 != c2 {
		t.Errorf("shuffled input order changed results: %v/%v vs %v/%v", fpnNames(r1), c1, fpnNames(r2), c2)
	}
	// Sorted ready set is identical regardless of input order.
	s1 := fpnNames(r1)
	sort.Strings(s1)
	s2 := fpnNames(r2)
	sort.Strings(s2)
	if !reflect.DeepEqual(s1, s2) {
		t.Errorf("sorted ready sets differ: %v vs %v", s1, s2)
	}
}
