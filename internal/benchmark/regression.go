package benchmark

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// RegressionGate implements a regression check using benchmark instances.
// It satisfies the selfimprove.RegressionChecker interface without importing
// the selfimprove package (structural typing).
type RegressionGate struct {
	runner    *Runner
	instances []Instance
	// BaselineScore is the minimum acceptable score (0-100). If the current
	// run scores below this, a regression is reported.
	BaselineScore float64
	logger        *slog.Logger
}

// NewRegressionGate creates a regression gate from a runner and instance set.
func NewRegressionGate(runner *Runner, instances []Instance, baselineScore float64, logger *slog.Logger) *RegressionGate {
	if logger == nil {
		logger = slog.Default()
	}
	return &RegressionGate{
		runner:        runner,
		instances:     instances,
		BaselineScore: baselineScore,
		logger:        logger,
	}
}

// Check runs the benchmark instances and compares against the baseline.
// Returns (passed, summary, error).
func (g *RegressionGate) Check(ctx context.Context) (bool, string, error) {
	if len(g.instances) == 0 {
		return true, "no benchmark instances configured", nil
	}

	start := time.Now()
	report, err := g.runner.Run(ctx, g.instances)
	if err != nil {
		return false, "", fmt.Errorf("regression benchmark failed: %w", err)
	}

	summary := fmt.Sprintf("score=%.1f%% (baseline=%.1f%%) resolved=%d/%d duration=%v",
		report.Score, g.BaselineScore, report.Resolved, report.Total,
		time.Since(start).Round(time.Second))

	if report.Score < g.BaselineScore {
		g.logger.Warn("regression detected",
			"score", report.Score,
			"baseline", g.BaselineScore,
			"resolved", report.Resolved,
			"total", report.Total)
		return false, summary, nil
	}

	return true, summary, nil
}
