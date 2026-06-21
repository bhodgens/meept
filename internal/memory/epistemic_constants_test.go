package memory

import "testing"

func TestEpistemicMemoryTypes(t *testing.T) {
	cases := []struct {
		got, want MemoryType
	}{
		{MemoryTypeClaim, MemoryType("claim")},
		{MemoryTypeDecision, MemoryType("decision")},
		{MemoryTypePrediction, MemoryType("prediction")},
		{MemoryTypeQuestion, MemoryType("question")},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestEpistemicEdgeTypes(t *testing.T) {
	cases := []struct {
		got, want EdgeType
	}{
		{EdgeTypeContradicts, EdgeType("contradicts")},
		{EdgeTypeSuperseded, EdgeType("superseded")},
		{EdgeTypeEvidenceFor, EdgeType("evidence_for")},
		{EdgeTypeEvidenceAgainst, EdgeType("evidence_against")},
		{EdgeTypeDerivesFrom, EdgeType("derives_from")},
		{EdgeTypeSupports, EdgeType("supports")},
		{EdgeTypePotentialContradicts, EdgeType("potential_contradicts")},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}
