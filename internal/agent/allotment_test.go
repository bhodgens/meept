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

// TestSplitStepsByAllotment_Boundaries pins the batch-splitting edge cases
// the audit called out as unpinned (2026-09-10 audit M10). Table-driven:
// each case builds its steps and asserts the full batch shape.
func TestSplitStepsByAllotment_Boundaries(t *testing.T) {
	mk := func(desc string) *task.TaskStep { return task.NewTaskStep("t1", desc, 0) }
	cfg := DefaultAllotmentConfig() // MinStepTokens 512, MaxBatchSteps 0

	t.Run("MaxBatchSteps caps steps per batch", func(t *testing.T) {
		capped := cfg
		capped.MaxBatchSteps = 2
		batches := SplitStepsByAllotment(mkN("t1", 5, 512), 4096, capped)
		// 5 x 512-token steps fit 4-per-batch token-wise, but the count cap
		// forces 2/2/1. Order preserved, nothing dropped.
		if len(batches) != 3 || len(batches[0]) != 2 || len(batches[1]) != 2 || len(batches[2]) != 1 {
			t.Fatalf("want 2/2/1 batches, got %v", batchLens(batches))
		}
	})

	t.Run("MaxBatchSteps 0 means no count cap", func(t *testing.T) {
		batches := SplitStepsByAllotment(mkN("t1", 5, 512), 4096, cfg)
		if len(batches) != 1 || len(batches[0]) != 5 {
			t.Fatalf("want single 5-step batch, got %v", batchLens(batches))
		}
	})

	t.Run("exact fit stays (used+cost == allotment)", func(t *testing.T) {
		// 3 x 512 == 1536 exactly: the boundary fill must NOT flush at
		// equality — only used+cost > allotment does.
		batches := SplitStepsByAllotment(mkN("t1", 3, 512), 1536, cfg)
		if len(batches) != 1 || len(batches[0]) != 3 {
			t.Fatalf("exact-fit wave split: want 1 batch of 3, got %v", batchLens(batches))
		}
		// And the next step overflows: 4th step opens batch 2.
		batches = SplitStepsByAllotment(mkN("t1", 4, 512), 1536, cfg)
		if len(batches) != 2 || len(batches[0]) != 3 || len(batches[1]) != 1 {
			t.Fatalf("one-over wave split: want 3/1, got %v", batchLens(batches))
		}
	})

	t.Run("allotment below MinStepTokens: every step oversize", func(t *testing.T) {
		// Allotment 256 < MinStepTokens 512: every 512-token step exceeds
		// the allotment, and each must land in its OWN batch (greedy fill
		// must not stack two oversize steps into one 256-token batch).
		batches := SplitStepsByAllotment(mkN("t1", 3, 512), 256, cfg)
		if len(batches) != 3 {
			t.Fatalf("want 3 solo batches, got %v", batchLens(batches))
		}
		for i, b := range batches {
			if len(b) != 1 {
				t.Errorf("batch %d holds %d steps, want 1 (no oversize stacking)", i, len(b))
			}
		}
	})

	t.Run("negative allotment single batch", func(t *testing.T) {
		batches := SplitStepsByAllotment(mkN("t1", 3, 512), -100, cfg)
		if len(batches) != 1 || len(batches[0]) != 3 {
			t.Fatalf("want 1 batch of 3, got %v", batchLens(batches))
		}
	})

	t.Run("MinStepTokens 0 zero-cost degenerate", func(t *testing.T) {
		// MinStepTokens 0: short descriptions cost ~0 tokens, so a huge
		// allotment never flushes — everything rides one batch. The loop
		// must still terminate and preserve order.
		zero := cfg
		zero.MinStepTokens = 0
		steps := []*task.TaskStep{mk("one"), mk("two"), mk("three")}
		batches := SplitStepsByAllotment(steps, 4096, zero)
		if len(batches) != 1 || len(batches[0]) != 3 {
			t.Fatalf("want 1 batch of 3, got %v", batchLens(batches))
		}
		for i, want := range []string{"one", "two", "three"} {
			if got := batches[0][i].Description; got != want {
				t.Errorf("order: batch[0][%d] = %q, want %q", i, got, want)
			}
		}
		// And with a zero allotment too (both knobs at zero).
		batches = SplitStepsByAllotment(steps, 0, zero)
		if len(batches) != 1 || len(batches[0]) != 3 {
			t.Fatalf("zero allotment want 1 batch of 3, got %v", batchLens(batches))
		}
	})
}

// mkN builds n steps of an exact token cost: desc repeated so
// len(desc)/CharsPerToken == tokens (4 chars/token at defaults, no floor
// impact when tokens >= MinStepTokens).
func mkN(taskID string, n, tokens int) []*task.TaskStep {
	desc := strings.Repeat("a", tokens*4)
	out := make([]*task.TaskStep, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, task.NewTaskStep(taskID, desc, i))
	}
	return out
}

func batchLens(batches [][]*task.TaskStep) []int {
	lens := make([]int, len(batches))
	for i, b := range batches {
		lens[i] = len(b)
	}
	return lens
}

func TestContinuationDescription(t *testing.T) {
	got := ContinuationDescription("do the thing", 2, 3)
	want := "[continuation 2/3] do the thing"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
