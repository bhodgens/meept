package agent

import (
	"encoding/json"
	"testing"
)

func TestPairModality_String(t *testing.T) {
	tests := []struct {
		modality PairModality
		want     string
	}{
		{PairModalityNone, "none"},
		{PairModalitySpecReview, "spec_review"},
		{PairModalityPairSession, "pair_session"},
		{PairModalityDebate, "debate"},
		{PairModalityInline, "inline"},
	}
	for _, tt := range tests {
		if got := tt.modality.String(); got != tt.want {
			t.Errorf("PairModality(%d).String() = %q, want %q", tt.modality, got, tt.want)
		}
	}
}

func TestPairModality_MarshalJSON(t *testing.T) {
	m := PairModalitySpecReview
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}
	if string(data) != `"spec_review"` {
		t.Errorf("MarshalJSON = %s, want %q", data, `"spec_review"`)
	}
}

func TestPairModality_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		input string
		want  PairModality
	}{
		{`"none"`, PairModalityNone},
		{`"spec_review"`, PairModalitySpecReview},
		{`"pair_session"`, PairModalityPairSession},
		{`"debate"`, PairModalityDebate},
		{`"inline"`, PairModalityInline},
		{`"unknown"`, PairModalityNone},
	}
	for _, tt := range tests {
		var got PairModality
		if err := json.Unmarshal([]byte(tt.input), &got); err != nil {
			t.Errorf("UnmarshalJSON(%s) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("UnmarshalJSON(%s) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParsePairModality(t *testing.T) {
	tests := []struct {
		input string
		want  PairModality
	}{
		{"spec_review", PairModalitySpecReview},
		{"SPEC_REVIEW", PairModalitySpecReview},
		{"pair_session", PairModalityPairSession},
		{"debate", PairModalityDebate},
		{"inline", PairModalityInline},
		{"none", PairModalityNone},
		{"", PairModalityNone},
		{"bogus", PairModalityNone},
	}
	for _, tt := range tests {
		got := ParsePairModality(tt.input)
		if got != tt.want {
			t.Errorf("ParsePairModality(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestPairModality_IsActive(t *testing.T) {
	if PairModalityNone.IsActive() {
		t.Error("PairModalityNone should not be active")
	}
	if !PairModalitySpecReview.IsActive() {
		t.Error("PairModalitySpecReview should be active")
	}
	if !PairModalityPairSession.IsActive() {
		t.Error("PairModalityPairSession should be active")
	}
	if !PairModalityDebate.IsActive() {
		t.Error("PairModalityDebate should be active")
	}
	if !PairModalityInline.IsActive() {
		t.Error("PairModalityInline should be active")
	}
}
