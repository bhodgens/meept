package agent

import "testing"

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a    []float64
		b    []float64
		want float64
	}{
		{
			name: "identical vectors",
			a:    []float64{1, 0, 0},
			b:    []float64{1, 0, 0},
			want: 1.0,
		},
		{
			name: "orthogonal vectors",
			a:    []float64{1, 0, 0},
			b:    []float64{0, 1, 0},
			want: 0.0,
		},
		{
			name: "opposite vectors",
			a:    []float64{1, 0, 0},
			b:    []float64{-1, 0, 0},
			want: -1.0,
		},
		{
			name: "mismatched lengths",
			a:    []float64{1, 2, 3},
			b:    []float64{1, 2},
			want: 0,
		},
		{
			name: "empty vectors",
			a:    []float64{},
			b:    []float64{},
			want: 0,
		},
		{
			name: "45 degree angle",
			a:    []float64{1, 0},
			b:    []float64{1, 1},
			want: 0.7071067811865475,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CosineSimilarity(tt.a, tt.b)
			if tt.want == 0 && got != 0 {
				t.Errorf("CosineSimilarity() = %v, want %v", got, tt.want)
			} else if tt.want != 0 && (got-tt.want > 1e-9 || got-tt.want < -1e-9) {
				t.Errorf("CosineSimilarity() = %v, want %v", got, tt.want)
			}
		})
	}
}
