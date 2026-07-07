package shadow

import (
	"context"
	"fmt"
)

// minRecordsForGate is the floor below which an adapter is considered
// under-trained regardless of eval score. Trained on too few records, the
// adapter is statistically unreliable.
const minRecordsForGate = 20

// EvalGate decides whether a TrainingRun is safe to deploy.
type EvalGate struct {
	threshold float64
}

// NewEvalGate constructs an EvalGate. A threshold of 0.0 disables the gate.
func NewEvalGate(threshold float64) *EvalGate {
	return &EvalGate{threshold: threshold}
}

// Check returns nil if the training run passes the gate, or an error
// describing why it failed. The context is accepted for future
// LLM-judge-based eval gates but is not used by the threshold check itself.
func (g *EvalGate) Check(_ context.Context, run *TrainingRun) error {
	if g.threshold <= 0.0 {
		return nil
	}
	if run == nil {
		return fmt.Errorf("eval gate: training run is nil")
	}
	if run.RecordsUsed < minRecordsForGate {
		return fmt.Errorf("eval gate: only %d records used (minimum %d)", run.RecordsUsed, minRecordsForGate)
	}
	if run.EvalScore < g.threshold {
		return fmt.Errorf("eval gate: eval score %.3f below threshold %.3f", run.EvalScore, g.threshold)
	}
	return nil
}
