package agent

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

func TestDefaultAllotmentConfig(t *testing.T) {
	cfg := DefaultAllotmentConfig()
	if cfg.UsableRatio != 0.75 {
		t.Errorf("UsableRatio = %v, want 0.75", cfg.UsableRatio)
	}
	if cfg.ReserveTokens != 4096 {
		t.Errorf("ReserveTokens = %v, want 4096", cfg.ReserveTokens)
	}
	if cfg.CharsPerToken != 4 {
		t.Errorf("CharsPerToken = %v, want 4", cfg.CharsPerToken)
	}
	if cfg.MinStepTokens != 512 {
		t.Errorf("MinStepTokens = %v, want 512", cfg.MinStepTokens)
	}
}

func TestEstimateStepTokens(t *testing.T) {
	cfg := DefaultAllotmentConfig()
	tests := []struct {
		name string
		desc string
		want int
	}{
		{"empty", "", cfg.MinStepTokens},
		{"short", "fix bug", cfg.MinStepTokens},
		{"exact min", strings.Repeat("a", 2048), 512},
		{"large", strings.Repeat("a", 40000), 10000},
	}
	for _, tt := range tests {
		if got := EstimateStepTokens(tt.desc, cfg); got != tt.want {
			t.Errorf("%s: EstimateStepTokens = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestAllotmentTokens(t *testing.T) {
	cfg := DefaultAllotmentConfig()
	tests := []struct {
		name       string
		contextLim int
		want       int
	}{
		{"unknown window", 0, 0},
		{"negative", -5, 0},
		{"8k model", 8192, int(float64(8192-4096) * 0.75)},
		{"32k model", 32768, int(float64(32768-4096) * 0.75)},
		{"128k model", 131072, int(float64(131072-4096) * 0.75)},
		{"tiny window below reserve", 1000, 0},
	}
	for _, tt := range tests {
		if got := AllotmentTokens(tt.contextLim, cfg); got != tt.want {
			t.Errorf("%s: got %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestSplitStepsByAllotment(t *testing.T) {
	mk := func(n int) []*task.TaskStep {
		var out []*task.TaskStep
		for i := 0; i < n; i++ {
			s := task.NewTaskStep("t1", strings.Repeat("a", 2048), i) // 512 tok each
			out = append(out, s)
		}
		return out
	}
	cfg := DefaultAllotmentConfig()

	t.Run("zero allotment single batch", func(t *testing.T) {
		batches := SplitStepsByAllotment(mk(5), 0, cfg)
		if len(batches) != 1 || len(batches[0]) != 5 {
			t.Errorf("want 1 batch of 5, got %d batches", len(batches))
		}
	})
	t.Run("greedy fill", func(t *testing.T) {
		// allotment 1536 fits three 512-token steps
		batches := SplitStepsByAllotment(mk(7), 1536, cfg)
		if len(batches) != 3 {
			t.Errorf("want 3 batches (3/3/1), got %d", len(batches))
		}
	})
	t.Run("oversize step own batch", func(t *testing.T) {
		big := task.NewTaskStep("t1", strings.Repeat("a", 40000), 0) // 10000 tok
		small := task.NewTaskStep("t1", "fix bug", 1)
		batches := SplitStepsByAllotment([]*task.TaskStep{big, small}, 2048, cfg)
		if len(batches) != 2 || len(batches[0]) != 1 || len(batches[1]) != 1 {
			t.Errorf("want 2 solo batches, got %d", len(batches))
		}
	})
	t.Run("empty input", func(t *testing.T) {
		batches := SplitStepsByAllotment(nil, 2048, cfg)
		if len(batches) != 0 {
			t.Errorf("want 0 batches, got %d", len(batches))
		}
	})
}

func TestContinuationDescription(t *testing.T) {
	got := ContinuationDescription("do the thing", 2, 3)
	want := "[continuation 2/3] do the thing"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
