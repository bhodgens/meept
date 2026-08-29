package agent

import (
	"errors"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

func TestToolRetryBreaker_ZeroValueDefaults(t *testing.T) {
	b := &ToolRetryBreaker{}
	warn, veto := b.thresholds()
	if warn != 3 || veto != 5 {
		t.Fatalf("zero-value thresholds = (%d, %d), want (3, 5)", warn, veto)
	}
	if got := NewToolRetryBreaker(); got.WarnAt != 3 || got.VetoAt != 5 {
		t.Fatalf("NewToolRetryBreaker = (%d, %d), want (3, 5)", got.WarnAt, got.VetoAt)
	}
}

func TestToolRetryBreaker_VetoAtConsecutiveFailures(t *testing.T) {
	tests := []struct {
		name     string
		call     func(b *ToolRetryBreaker) bool
		wantVeto bool
	}{
		{
			name: "veto only after VetoAt consecutive failures",
			call: func(b *ToolRetryBreaker) bool {
				for i := 0; i < 4; i++ {
					if b.Observe("sh", map[string]any{"cmd": "bad"}, true) {
						return true
					}
				}
				return false
			},
			wantVeto: false,
		},
		{
			name: "fifth identical failure vetoes",
			call: func(b *ToolRetryBreaker) bool {
				for i := 0; i < 5; i++ {
					v := b.Observe("sh", map[string]any{"cmd": "bad"}, true)
					if i == 4 && !v {
						t.Fatal("expected veto on 5th consecutive failure")
					}
					if v {
						return true
					}
				}
				return false
			},
			wantVeto: true,
		},
		{
			name: "success resets the counter",
			call: func(b *ToolRetryBreaker) bool {
				for i := 0; i < 4; i++ {
					b.Observe("sh", map[string]any{"cmd": "bad"}, true)
				}
				b.Observe("sh", map[string]any{"cmd": "bad"}, false) // success resets
				for i := 0; i < 4; i++ {
					if b.Observe("sh", map[string]any{"cmd": "bad"}, true) {
						return true
					}
				}
				return false
			},
			wantVeto: false,
		},
		{
			name: "different args are different keys",
			call: func(b *ToolRetryBreaker) bool {
				for i := 0; i < 5; i++ {
					// Alternate args: no single key reaches 5.
					args := map[string]any{"cmd": "a"}
					if i%2 == 1 {
						args = map[string]any{"cmd": "b"}
					}
					if b.Observe("sh", args, true) {
						return true
					}
				}
				return false
			},
			wantVeto: false,
		},
		{
			name: "different tool names are different keys",
			call: func(b *ToolRetryBreaker) bool {
				for i := 0; i < 3; i++ {
					if b.Observe("sh", map[string]any{"cmd": "x"}, true) {
						return true
					}
					if b.Observe("web", map[string]any{"cmd": "x"}, true) {
						return true
					}
				}
				return false
			},
			wantVeto: false,
		},
		{
			name: "map key order does not change identity",
			call: func(b *ToolRetryBreaker) bool {
				a1 := map[string]any{"a": 1, "b": 2}
				a2 := map[string]any{"b": 2, "a": 1}
				for i := 0; i < 4; i++ {
					if b.Observe("t", a1, true) {
						return true
					}
					if b.Observe("t", a2, true) {
						return true
					}
				}
				// 4+4 same-key failures reach veto.
				return b.Observe("t", a2, true)
			},
			wantVeto: true,
		},
		{
			name: "nil args count as empty args",
			call: func(b *ToolRetryBreaker) bool {
				for i := 0; i < 4; i++ {
					if b.Observe("t", nil, true) {
						return true
					}
				}
				return b.Observe("t", map[string]any{}, true)
			},
			wantVeto: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewToolRetryBreaker()
			if got := tt.call(b); got != tt.wantVeto {
				t.Fatalf("veto = %v, want %v", got, tt.wantVeto)
			}
		})
	}
}

func TestToolRetryBreaker_ConcurrentObserve(t *testing.T) {
	b := NewToolRetryBreaker()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				b.Observe("tool", map[string]any{"i": n}, j%2 == 0)
			}
		}(i)
	}
	wg.Wait()
}

func TestVerifyFromToolResults(t *testing.T) {
	tests := []struct {
		name    string
		results []tools.ToolResult
		want    bool
	}{
		{
			name:    "empty fails closed",
			results: nil,
			want:    false,
		},
		{
			name: "all success passes",
			results: []tools.ToolResult{
				{Success: true, Result: "ok"},
				{Success: true, Result: "fine"},
			},
			want: true,
		},
		{
			name: "any failure fails",
			results: []tools.ToolResult{
				{Success: true},
				{Success: false, Error: "exit 1"},
			},
			want: false,
		},
		{
			name: "error string without success flag fails",
			results: []tools.ToolResult{
				{Success: true, Error: "warning but set"},
			},
			want: false,
		},
		{
			name: "typed error with message fails",
			results: []tools.ToolResult{
				{Success: false, Error: "boom", Err: errors.New("boom")},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VerifyFromToolResults(tt.results); got != tt.want {
				t.Fatalf("VerifyFromToolResults = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestVerifyFromToolResults_IgnoresProse pins the input-isolation principle:
// a successful-looking assistant claim inside Result text cannot flip the
// verdict when the structured fields say failure.
func TestVerifyFromToolResults_IgnoresProse(t *testing.T) {
	results := []tools.ToolResult{
		{Success: false, Error: "tests failed", Result: "the assistant said: all tests passed"},
	}
	if VerifyFromToolResults(results) {
		t.Fatal("prose claim overrode structured failure")
	}
}
