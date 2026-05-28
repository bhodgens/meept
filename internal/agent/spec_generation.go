package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/task"
)

// TaskSpec is the specification artifact stored in task metadata under the key "spec".
// It contains acceptance criteria for each step in the plan, giving the reviewer
// an explicit checklist to validate against.
type TaskSpec struct {
	TaskID   string          `json:"task_id"`
	Criteria []StepCriterion `json:"criteria,omitempty"`
}

// StepCriterion defines what a single step must accomplish to be accepted.
type StepCriterion struct {
	StepSequence       int    `json:"step_sequence"`
	Description        string `json:"description"`
	AcceptanceCriteria string `json:"acceptance_criteria"`
	Required           bool   `json:"required"`
}

// specMetadataWrapper is the structure stored in task.Metadata (json.RawMessage).
type specMetadataWrapper struct {
	Spec *TaskSpec `json:"spec,omitempty"`
}

// GenerateSpecFromSteps creates a TaskSpec from a slice of planned steps.
func GenerateSpecFromSteps(steps []*task.TaskStep) *TaskSpec {
	if len(steps) == 0 {
		return &TaskSpec{}
	}

	taskID := steps[0].TaskID
	criteria := make([]StepCriterion, 0, len(steps))

	for _, step := range steps {
		criterion := StepCriterion{
			StepSequence:       step.Sequence,
			Description:        step.Description,
			AcceptanceCriteria: deriveAcceptanceCriteria(step),
			Required:           true,
		}
		criteria = append(criteria, criterion)
	}

	return &TaskSpec{
		TaskID:   taskID,
		Criteria: criteria,
	}
}

// deriveAcceptanceCriteria produces a human-readable acceptance checklist
// from a step's description and tool hint.
func deriveAcceptanceCriteria(step *task.TaskStep) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Step %q must be fully completed.", step.Description))

	switch step.ToolHint {
	case string(IntentCode), KeywordRefactor:
		sb.WriteString(" Code must compile without errors. Changes must be syntactically correct.")
		sb.WriteString(" No regressions in existing functionality.")
	case KeywordFix, string(IntentDebug):
		sb.WriteString(" Root cause must be identified and resolved.")
		sb.WriteString(" Fix must not introduce new errors.")
	case string(IntentGit), KeywordCommit:
		sb.WriteString(" Git operations must succeed. Changes must be committed to the correct branch.")
	case string(IntentPlan):
		sb.WriteString(" Plan must be actionable and cover all stated requirements.")
	case string(IntentAnalyze), string(IntentResearch):
		sb.WriteString(" Analysis must be thorough and address the question posed.")
	default:
		sb.WriteString(" Output must be meaningful and address the stated goal.")
	}

	return sb.String()
}

// StoreSpecInTask serializes the spec into the task's Metadata field.
// It preserves any existing metadata keys by merging the "spec" key in.
func StoreSpecInTask(t *task.Task, spec *TaskSpec) {
	if spec == nil {
		return
	}

	if len(t.Metadata) > 0 {
		var wrapper specMetadataWrapper
		if json.Unmarshal(t.Metadata, &wrapper) == nil {
			wrapper.Spec = spec
			if merged, err := json.Marshal(wrapper); err == nil {
				t.Metadata = merged
				return
			}
		}
	}

	wrapper := specMetadataWrapper{Spec: spec}
	if data, err := json.Marshal(wrapper); err == nil {
		t.Metadata = data
	}
}

// ExtractSpecFromTask reads the spec from a task's Metadata field.
// Returns nil if the task has no metadata or the metadata does not contain a spec.
func ExtractSpecFromTask(t *task.Task) *TaskSpec {
	if len(t.Metadata) == 0 {
		return nil
	}

	var wrapper specMetadataWrapper
	if err := json.Unmarshal(t.Metadata, &wrapper); err != nil {
		return nil
	}
	return wrapper.Spec
}
