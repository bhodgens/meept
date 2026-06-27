package cluster

import (
	"testing"
	"time"

	"github.com/caimlas/meept/pkg/models"
)

func TestConflictResolver_Resolve_LastWriteWins(t *testing.T) {
	r := NewConflictResolver(nil)

	e1 := &models.ClusterEvent{
		EventID:   "e1",
		NodeID:    "b",
		Timestamp: time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC),
	}
	e2 := &models.ClusterEvent{
		EventID:   "e2",
		NodeID:    "a",
		Timestamp: time.Date(2026, 6, 26, 9, 0, 0, 0, time.UTC),
	}

	res, err := r.Resolve(e1, e2)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if res.EventID != "e1" {
		t.Errorf("expected e1 (later), got %s", res.EventID)
	}
}

func TestConflictResolver_Resolve_NodeIDTiebreak(t *testing.T) {
	r := NewConflictResolver(nil)

	ts := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	e1 := &models.ClusterEvent{EventID: "b-node", NodeID: "b", Timestamp: ts}
	e2 := &models.ClusterEvent{EventID: "a-node", NodeID: "a", Timestamp: ts}

	res, err := r.Resolve(e1, e2)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if res.EventID != "b-node" {
		t.Errorf("expected b-node (lex greater), got %s", res.EventID)
	}
}

func TestConflictResolver_Resolve_NilEvent(t *testing.T) {
	r := NewConflictResolver(nil)

	e1 := &models.ClusterEvent{EventID: "only"}
	res, err := r.Resolve(nil, e1)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if res != e1 {
		t.Error("expected nil + e1 => e1")
	}

	res, err = r.Resolve(e1, nil)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if res != e1 {
		t.Error("expected e1 + nil => e1")
	}
}

func TestCompareVectorClocks(t *testing.T) {
	cases := []struct {
		name     string
		vc1      map[string]int64
		vc2      map[string]int64
		expected int
	}{
		{"equal", map[string]int64{"a": 1}, map[string]int64{"a": 1}, 0},
		{"before", map[string]int64{"a": 1}, map[string]int64{"a": 2}, -1},
		{"after", map[string]int64{"a": 2}, map[string]int64{"a": 1}, 1},
		{"concurrent", map[string]int64{"a": 2, "b": 1}, map[string]int64{"a": 1, "b": 2}, 0},
		{"empty", map[string]int64{}, map[string]int64{}, 0},
		{"vc1 empty, vc2 populated", map[string]int64{}, map[string]int64{"a": 1}, -1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CompareVectorClocks(tc.vc1, tc.vc2)
			if got != tc.expected {
				t.Errorf("CompareVectorClocks(%v, %v) = %d, want %d",
					tc.vc1, tc.vc2, got, tc.expected)
			}
		})
	}
}
