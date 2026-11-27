package gatecapture

import (
	"fmt"
	"testing"
)

// TestFormatBand pins the 0.1-step band grid. The regression (bughunt A1-17)
// was that scores in [0.95, 1.0] rendered as "0.0" (the lowest label) because
// band=10 made whole=1, and the old []string{...}[whole] indexing fell out of
// the 11-entry table (whole is only ever 0 or 1, and [1] is "0.1"). The fix
// renders fmt.Sprintf("%d.%d", whole, frac) so 0.95 -> "1.0".
//
// This MUST fail at HEAD on the 0.95 case (old code returns "0.0").
func TestFormatBand(t *testing.T) {
	tests := []struct {
		score float64
		want  string
	}{
		{0, "0.0"},
		{0.44, "0.4"},
		{0.45, "0.5"},
		{0.5, "0.5"},
		{0.94, "0.9"},
		{0.95, "1.0"}, // regression: old code rendered "0.0"
		{1.0, "1.0"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("score_%v", tt.score), func(t *testing.T) {
			if got := formatBand(tt.score); got != tt.want {
				t.Errorf("formatBand(%v) = %q, want %q", tt.score, got, tt.want)
			}
		})
	}
}
