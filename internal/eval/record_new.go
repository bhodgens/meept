package eval

import "github.com/caimlas/meept/pkg/id"

// NewRun creates a RunRecord with a fresh pkg/id-generated ID and UTC
// creation timestamp.
func NewRun(kind Kind, taskID, modelID string, k int) *RunRecord {
	return &RunRecord{
		ID:        id.Generate("eval-"),
		CreatedAt: nowUTC(),
		Kind:      kind,
		TaskID:    taskID,
		ModelID:   modelID,
		K:         k,
		Attempts:  []Attempt{},
	}
}

// AddAttempt appends an attempt to the run.
func (r *RunRecord) AddAttempt(a Attempt) {
	r.Attempts = append(r.Attempts, a)
}
