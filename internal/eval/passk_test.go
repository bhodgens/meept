package eval

import "testing"

func TestPassK(t *testing.T) {
	p := Attempt{Index: 0, ModelID: "m", Passed: true, Oracle: OracleResult{Passed: true}}
	f := Attempt{Index: 0, ModelID: "m", Passed: false, Oracle: OracleResult{Passed: false}}

	tests := []struct {
		name     string
		attempts []Attempt
		k        int
		want     bool
	}{
		{name: "k=1 single pass", attempts: []Attempt{p}, k: 1, want: true},
		{name: "k=1 single fail", attempts: []Attempt{f}, k: 1, want: false},
		{name: "k=1 fail after pass checks only last", attempts: []Attempt{p, f}, k: 1, want: false},
		{name: "k=3 fail in middle of window", attempts: []Attempt{p, f, p}, k: 3, want: false},
		{name: "k=3 last three pass after earlier fail", attempts: []Attempt{f, p, p, p}, k: 3, want: true},
		{name: "k=3 only two consecutive at end", attempts: []Attempt{p, p, f, p, p}, k: 3, want: false},
		{name: "k=3 all pass", attempts: []Attempt{p, p, p}, k: 3, want: true},
		{name: "k=3 empty attempts", attempts: nil, k: 3, want: false},
		{name: "k=0", attempts: []Attempt{p, p, p}, k: 0, want: false},
		{name: "k negative", attempts: []Attempt{p, p, p}, k: -2, want: false},
		{name: "k larger than attempts", attempts: []Attempt{p, p}, k: 3, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PassK(tt.attempts, tt.k); got != tt.want {
				t.Errorf("PassK(%d attempts, k=%d) = %v, want %v", len(tt.attempts), tt.k, got, tt.want)
			}
		})
	}
}
