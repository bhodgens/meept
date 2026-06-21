package memory

import (
	"context"
	"testing"
)

func TestAmbientExtractorZeroValue(t *testing.T) {
	ex := &AmbientExtractor{}
	// With nil classifier, extract returns no candidates and no error.
	candidates, err := ex.Extract(context.Background(), []string{"hello world"})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(candidates))
	}
}

func TestAmbientExtractionCandidateFields(t *testing.T) {
	c := AmbientCandidate{Type: "claim", Text: "x", Confidence: 0.8}
	if c.Type != "claim" || c.Text != "x" || c.Confidence != 0.8 {
		t.Error("candidate fields did not round-trip")
	}
}

func TestParseAmbientCandidates(t *testing.T) {
	raw := []byte(`[{"type":"claim","text":"a","source":"conversation","confidence":0.9,"category":"technical"}]`)
	cands, err := ParseAmbientCandidates(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}
	if cands[0].Type != "claim" || cands[0].Text != "a" || cands[0].Confidence != 0.9 {
		t.Errorf("candidate mismatch: %+v", cands[0])
	}
}

func TestParseAmbientCandidatesCodeFence(t *testing.T) {
	raw := []byte("```json\n[{\"type\":\"prediction\",\"text\":\"p\",\"confidence\":0.5}]\n```")
	cands, err := ParseAmbientCandidates(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(cands) != 1 || cands[0].Type != "prediction" {
		t.Errorf("candidate mismatch: %+v", cands)
	}
}
