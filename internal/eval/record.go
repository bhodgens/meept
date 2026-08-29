// Package eval provides first-class eval harness primitives: run records,
// pass^k scoring, shell oracles, and harness identity hashing.
//
// Pass^k means K consecutive oracle passes, not one lucky run (Pass@k).
// Workdir is always an explicit argument; nothing in this package calls
// os.Getwd().
package eval

import "time"

// Kind identifies the type of evaluation run.
type Kind string

// Supported run kinds.
const (
	// KindPassK is a baseline pass^k measurement of a single model on a task.
	KindPassK Kind = "pass_k"
	// KindModelSwap measures the same harness with a different model.
	KindModelSwap Kind = "model_swap"
	// KindAblation measures a modified harness against a baseline.
	KindAblation Kind = "ablation"
)

// RunRecord is the top-level record for one evaluation run (frozen C1 shape).
type RunRecord struct {
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	Kind        Kind      `json:"kind"`
	TaskID      string    `json:"task_id"`
	HarnessHash string    `json:"harness_hash"`
	ModelID     string    `json:"model_id"`
	K           int       `json:"k"`
	Attempts    []Attempt `json:"attempts"`
	Passed      bool      `json:"passed"`
	OracleName  string    `json:"oracle_name"`
}

// Attempt is one scored execution attempt within a run.
type Attempt struct {
	Index        int          `json:"index"`
	ModelID      string       `json:"model_id"`
	Passed       bool         `json:"passed"`
	Oracle       OracleResult `json:"oracle"`
	TrajectoryID string       `json:"trajectory_id,omitempty"`
}
