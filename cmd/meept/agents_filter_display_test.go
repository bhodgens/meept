package main

import "testing"

// TestRenderOutputFiltersSection verifies the `output filters:` section of
// `agents show` (output-filters leaf 04 Task 4): lowercase values per the
// repo UI convention, exact strings per the leaf spec.
func TestRenderOutputFiltersSection(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]any
		want string
	}{
		{
			name: "enabled with overrides",
			in: map[string]any{
				"enabled":            true,
				"filters":            []any{"json_format", "language_en"},
				"max_passes":         float64(2),
				"max_filter_retries": float64(2),
			},
			want: "\noutput filters: enabled, filters: json_format, language_en, max passes: 2, retries: 2",
		},
		{
			name: "disabled",
			in: map[string]any{
				"enabled": false,
			},
			want: "\noutput filters: off (daemon default disabled)",
		},
		{
			name: "missing enabled key",
			in:   map[string]any{},
			want: "\noutput filters: off (daemon default disabled)",
		},
		{
			name: "enabled with no filters",
			in: map[string]any{
				"enabled":            true,
				"filters":            []any{},
				"max_passes":         float64(3),
				"max_filter_retries": float64(5),
			},
			want: "\noutput filters: enabled, max passes: 3, retries: 5",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := renderOutputFiltersSection(tc.in)
			if got != tc.want {
				t.Errorf("renderOutputFiltersSection() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRenderOutputFiltersSection_Lowercase pins the repo UI convention:
// the rendered section contains no uppercase characters.
func TestRenderOutputFiltersSection_Lowercase(t *testing.T) {
	out := renderOutputFiltersSection(map[string]any{
		"enabled":            true,
		"filters":            []any{"json_format", "language_en"},
		"max_passes":         float64(2),
		"max_filter_retries": float64(2),
	})
	for _, r := range out {
		if r >= 'A' && r <= 'Z' {
			t.Errorf("rendered section contains uppercase %q: %q", string(r), out)
		}
	}
}
