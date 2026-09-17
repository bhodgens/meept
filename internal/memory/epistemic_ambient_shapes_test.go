package memory

import (
	"testing"
)

// TestParseAmbientCandidates_Shapes covers the tolerated LLM output shapes
// (tools/memory-eval findings: every local model wraps the array in an object
// or emits fragments; production used to accept only the bare array).
func TestParseAmbientCandidates_Shapes(t *testing.T) {
	validElem := `{"type":"claim","text":"a","source":"conversation","confidence":0.9,"premises":["p"],"category":"technical"}`

	tests := []struct {
		name    string
		raw     string
		wantLen int
		wantErr bool
	}{
		{
			name:    "bare array (contract shape)",
			raw:     "[" + validElem + "]",
			wantLen: 1,
		},
		{
			name:    "empty bare array",
			raw:     "[]",
			wantLen: 0,
		},
		{
			name:    "candidates wrapper",
			raw:     `{"candidates":[` + validElem + `]}`,
			wantLen: 1,
		},
		{
			name:    "results wrapper",
			raw:     `{"results":[` + validElem + `]}`,
			wantLen: 1,
		},
		{
			name:    "items wrapper",
			raw:     `{"items":[` + validElem + `]}`,
			wantLen: 1,
		},
		{
			name:    "fenced bare array",
			raw:     "```json\n[" + validElem + "]\n```",
			wantLen: 1,
		},
		{
			name:    "fenced candidates wrapper",
			raw:     "```json\n{\"candidates\":[" + validElem + "]}\n```",
			wantLen: 1,
		},
		{
			name:    "prose around bare array",
			raw:     "Here are the extracted candidates:\n[" + validElem + "]\nHope that helps!",
			wantLen: 1,
		},
		{
			name:    "empty string",
			raw:     "",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			raw:     "   \n\t  ",
			wantErr: true,
		},
		{
			name:    "garbage",
			raw:     "not json at all",
			wantErr: true,
		},
		{
			name:    "object with unrelated keys",
			raw:     `{"foo":"bar"}`,
			wantErr: true,
		},
		{
			name:    "empty wrapper object counts as no candidates",
			raw:     `{"candidates":[]}`,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cands, err := ParseAmbientCandidates([]byte(tt.raw))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got candidates %+v", cands)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			if len(cands) != tt.wantLen {
				t.Fatalf("got %d candidates, want %d", len(cands), tt.wantLen)
			}
			if tt.wantLen > 0 {
				if cands[0].Type != "claim" || cands[0].Text != "a" || cands[0].Confidence != 0.9 {
					t.Errorf("candidate mismatch: %+v", cands[0])
				}
				if len(cands[0].Premises) != 1 || cands[0].Premises[0] != "p" {
					t.Errorf("premises mismatch: %+v", cands[0].Premises)
				}
				if cands[0].Category != "technical" {
					t.Errorf("category mismatch: %+v", cands[0].Category)
				}
			}
		})
	}
}

// TestParseAmbientCandidates_GrammarForcedShapeRoundTrip: a JSON document in
// the exact key order the llm.AmbientCandidateGrammar GBNF forces on the wire
// must parse via the bare-array path.
func TestParseAmbientCandidates_GrammarForcedShapeRoundTrip(t *testing.T) {
	// This is the literal wire shape llama-server b7730 produced under the
	// grammar during wire verification (llm package).
	raw := `[{"text":"we ship friday","source":"alice","confidence":0.9,"premises":["p1"],"type":"decision","category":"technical"}]`
	cands, err := ParseAmbientCandidates([]byte(raw))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}
	c := cands[0]
	if c.Type != "decision" || c.Text != "we ship friday" || c.Source != "alice" ||
		c.Confidence != 0.9 || c.Category != "technical" || len(c.Premises) != 1 {
		t.Errorf("round-trip mismatch: %+v", c)
	}
}
