package fuzzy

// ContainsRatio pins (BARGE-IN CONTRACT (2026-11-11)): the whole-query
// containment measure that backs the voicechild echo-provenance gate.
// Values computed against the frozen per-token similarity (1 -
// editDistance/maxLen), pinned with the epsilon slack of Pitfall 26.

import (
	"testing"
)

func TestContainsRatioEchoShapes(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		text    string
		wantMin float64
		wantMax float64
	}{
		{"identical", "the weather today is sunny", "the weather today is sunny", 1.0, 1.0},
		// Garbled echo: whisper re-hears the TTS with one word mangled;
		// every other token matches exactly, the mangled one lands high.
		{"garbled tail", "the weather today is sunney", "the weather today is sunny", 0.9, 1.0},
		// Case/punctuation are normalized away on both sides.
		{"normalized", "Stop the car!", "stop, the car.", 1.0, 1.0},
		// A human sentence that shares only function words with the
		// agent's speech stays LOW — that is the property the echo gate
		// relies on to keep genuine barge-ins authorized.
		{"unrelated", "actually wait I need to ask something", "the weather today is sunny", 0.0, 0.5},
		{"empty query", "", "whatever", 0, 0},
		{"empty text", "hello there", "", 0, 0},
		{"both empty", "", "", 0, 0},
		// Subset: every query token appears verbatim in the longer text.
		{"subset of longer", "one moment", "sure, one moment while I check", 1.0, 1.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ContainsRatio(tc.query, tc.text)
			if got < tc.wantMin-epsilon || got > tc.wantMax+epsilon {
				t.Fatalf("ContainsRatio(%q, %q) = %v, want in [%v, %v]", tc.query, tc.text, got, tc.wantMin, tc.wantMax)
			}
		})
	}
}

// TestSimilarityVsContainsRatio pins WHY the echo gate cannot reuse
// Similarity: its top-two-token average confidently matches unrelated
// sentences that share two strong words, while containment over the
// whole query does not.
func TestSimilarityVsContainsRatio(t *testing.T) {
	a := "mark the work order done"
	b := "call the inspector back"
	// Shared {mark/call-like, work/back-like} top-2 pairs push the
	// top-two average up; containment over all six query tokens cannot.
	if s := Similarity(a, b); s < 0.7 {
		t.Fatalf("fixture invariant: Similarity top-2 average should stay high on shared words, got %v", s)
	}
	if r := ContainsRatio(a, b); r > 0.6 {
		t.Fatalf("ContainsRatio must NOT over-match unrelated sentences, got %v", r)
	}
}
