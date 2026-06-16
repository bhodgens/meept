package repomap

import (
	"testing"

	"gonum.org/v1/gonum/graph"
)

// fakeNode is a minimal graph.Node for testing weighted-line ID allocation.
type fakeNode struct{ id int64 }

func (n *fakeNode) ID() int64 { return n.id }

// TestWeightedLineID_NoCollision verifies that the new counter-based ID
// allocation does not collide for node-ID combinations that the old
// from.ID()*1e9 + to.ID() packing would have collided on.
func TestWeightedLineID_NoCollision(t *testing.T) {
	// Old packed formula: from=0,to=1000 -> 1000; from=1,to=0 -> 1e9.
	// These are distinct, but the reverse pair from=0,to=1 -> 1 and
	// from=0,to=0 + offset can collide. More concretely, from=1,to=0
	// yields 1e9 and from=0,to=X where X=1e9 collides. The counter
	// approach sidesteps all such collisions.
	n0 := &fakeNode{id: 0}
	n1 := &fakeNode{id: 1}

	l1 := newWeightedLine(n0, n1, 1.0)
	l2 := newWeightedLine(n1, n0, 1.0)
	l3 := newWeightedLine(n0, n1, 1.0)

	ids := map[int64]bool{l1.ID(): true, l2.ID(): true, l3.ID(): true}
	if len(ids) != 3 {
		t.Fatalf("expected 3 distinct edge IDs, got duplicates: %v", ids)
	}

	// Also verify reversed lines get fresh IDs.
	rev := l1.ReversedLine()
	if rev.ID() == l1.ID() {
		t.Fatalf("reversed line ID collided with original: %d", rev.ID())
	}

	_ = graph.Node(n0) // keep import used even if API changes
}
