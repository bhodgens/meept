package shadow

import "testing"

func TestDedupState_ThresholdHonored(t *testing.T) {
	tests := []struct {
		name      string
		threshold float64
		steps     []struct {
			text string
			dup  bool
			desc string
		}
	}{
		{
			name:      "identical_short_circuits",
			threshold: 0.95,
			steps: []struct {
				text string
				dup  bool
				desc string
			}{
				{"alpha beta gamma delta", false, "first text kept"},
				{"alpha beta gamma delta", true, "identical text skipped"},
			},
		},
		{
			name:      "identical_first_100_chars_short_circuits",
			threshold: 0.95,
			steps: []struct {
				text string
				dup  bool
				desc string
			}{
				{repeatText("shared prefix ", 8) + "tail-one", false, "first long text kept"},
				{repeatText("shared prefix ", 8) + "tail-two", true, "same first-100 fingerprint skipped"},
			},
		},
		{
			name:      "paraphrase_above_threshold_skipped",
			threshold: 0.95,
			steps: []struct {
				text string
				dup  bool
				desc string
			}{
				{repeatText("alpha beta gamma ", 50), false, "base kept"},
				// Differs in the first 100 chars (no hash short-circuit), but
				// token Jaccard = 150/151 ~= 0.993 >= 0.95 -> skipped.
				{"zulu " + repeatText("alpha beta gamma ", 50), true, "near-dup above threshold skipped"},
			},
		},
		{
			name:      "distinct_below_threshold_kept",
			threshold: 0.95,
			steps: []struct {
				text string
				dup  bool
				desc string
			}{
				{"alpha beta gamma delta", false, "base kept"},
				{"alpha beta gamma epsilon", false, "paraphrase below threshold kept"},
			},
		},
		{
			name:      "zero_threshold_disables_near_dup_but_not_exact",
			threshold: 0,
			steps: []struct {
				text string
				dup  bool
				desc string
			}{
				{"alpha beta gamma delta", false, "base kept"},
				{"alpha beta gamma delta alpha", false, "near-identical kept when disabled"},
				{"alpha beta gamma delta", true, "exact duplicate still skipped"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dedup := newDedupState(tc.threshold)
			for _, step := range tc.steps {
				if got := dedup.isDuplicate(step.text); got != step.dup {
					t.Errorf("isDuplicate(%q) with threshold %v = %v, want %v (%s)",
						step.text, tc.threshold, got, step.dup, step.desc)
				}
			}
		})
	}
}

// repeatText repeats s n times.
func repeatText(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for range n {
		out = append(out, s...)
	}
	return string(out)
}

func TestJaccardSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        map[string]int
		b        map[string]int
		expected float64
	}{
		{"identical_multisets", map[string]int{"x": 2, "y": 1}, map[string]int{"x": 2, "y": 1}, 1.0},
		{"disjoint", map[string]int{"x": 1}, map[string]int{"y": 1}, 0},
		{"empty_a", map[string]int{}, map[string]int{"y": 1}, 0},
		{"empty_b", map[string]int{"x": 1}, map[string]int{}, 0},
		{"partial_overlap", map[string]int{"a": 1, "b": 1, "g": 1, "d": 1}, map[string]int{"a": 1, "b": 1, "g": 1, "e": 1}, 0.6},
		{"multiset_counts", map[string]int{"a": 1, "b": 1, "g": 1, "d": 1}, map[string]int{"a": 2, "b": 1, "g": 1, "d": 1}, 0.8},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := jaccardSimilarity(tc.a, tc.b)
			tolerance := 1e-9
			if got < tc.expected-tolerance || got > tc.expected+tolerance {
				t.Errorf("jaccardSimilarity(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.expected)
			}
		})
	}
}
