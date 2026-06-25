package agent

import (
	"testing"
)

func TestArtifactStore_AddAndHas(t *testing.T) {
	s := newArtifactStore()
	s.Add(Artifact{Name: "auth.go", Kind: "file"}, "step-1")
	if !s.Has("auth.go") {
		t.Error("Has(auth.go) = false; want true")
	}
	if !s.IsProducedBy("auth.go", "step-1") {
		t.Error("IsProducedBy wrong")
	}
	if s.IsProducedBy("auth.go", "step-2") {
		t.Error("IsProducedBy wrong for non-producer")
	}
}

func TestArtifactStore_Get(t *testing.T) {
	s := newArtifactStore()
	a := Artifact{Name: "design", Kind: "decision", Description: "use JWT"}
	s.Add(a, "step-1")
	got, ok := s.Get("design")
	if !ok {
		t.Fatal("not found")
	}
	if got.Description != "use JWT" {
		t.Errorf("desc = %q", got.Description)
	}
}

func TestArtifactStore_ConcurrentSafe(t *testing.T) {
	s := newArtifactStore()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			s.Add(Artifact{Name: "x", Kind: "file"}, "step-1")
		}
		close(done)
	}()
	for i := 0; i < 100; i++ {
		s.Has("x")
	}
	<-done
}

func TestArtifact_IsValidKind(t *testing.T) {
	cases := []struct {
		kind string
		want bool
	}{
		{"file", true},
		{"interface", true},
		{"schema", true},
		{"decision", true},
		{"test_suite", true},
		{"unknown", false},
		{"", false},
	}
	for _, c := range cases {
		a := Artifact{Kind: c.kind}
		if got := a.IsValidKind(); got != c.want {
			t.Errorf("IsValidKind(%q) = %v; want %v", c.kind, got, c.want)
		}
	}
}
