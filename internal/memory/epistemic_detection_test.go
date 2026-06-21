package memory

import (
	"context"
	"testing"
)

func TestEpistemicDetectorZeroValue(t *testing.T) {
	d := &EpistemicDetector{}
	edges, err := d.DetectRelationships(context.Background(), Memory{
		Type:    MemoryTypeClaim,
		Content: "x",
	})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(edges))
	}
}

func TestPotentialContradictionThreshold(t *testing.T) {
	if PotentialContradictionThreshold >= DefaultDetectionThreshold {
		t.Errorf("potential (%v) should be < default (%v)",
			PotentialContradictionThreshold, DefaultDetectionThreshold)
	}
}

func TestDetectRelationshipsNonEpistemic(t *testing.T) {
	d := &EpistemicDetector{}
	edges, _ := d.DetectRelationships(context.Background(), Memory{Type: MemoryTypeEpisodic})
	if len(edges) != 0 {
		t.Errorf("non-epistemic type should yield 0 edges, got %d", len(edges))
	}
}

func TestParseClassifierJSON(t *testing.T) {
	raw := []byte(`[{"relation":"contradicts","target_id":"abc","confidence":0.85,"explanation":"reverses"}]`)
	verdicts, err := ParseClassifierJSON(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(verdicts) != 1 {
		t.Fatalf("expected 1 verdict, got %d", len(verdicts))
	}
	if verdicts[0].Relation != "contradicts" || verdicts[0].TargetID != "abc" || verdicts[0].Confidence != 0.85 {
		t.Errorf("verdict mismatch: %+v", verdicts[0])
	}
}

func TestParseClassifierJSONCodeFence(t *testing.T) {
	raw := []byte("```json\n[{\"relation\":\"supports\",\"target_id\":\"d1\",\"confidence\":0.9,\"explanation\":\"\"}]\n```")
	verdicts, err := ParseClassifierJSON(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(verdicts) != 1 || verdicts[0].Relation != "supports" {
		t.Errorf("verdict mismatch: %+v", verdicts)
	}
}
