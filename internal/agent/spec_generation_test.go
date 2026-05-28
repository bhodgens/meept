package agent

import (
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

func TestGenerateSpecFromSteps(t *testing.T) {
	steps := []*task.TaskStep{
		task.NewTaskStep("task-1", "Implement the login handler function", 0).WithToolHint("code"),
		task.NewTaskStep("task-1", "Write unit tests for login handler", 1).WithToolHint("code"),
	}

	spec := GenerateSpecFromSteps(steps)
	if spec.TaskID != "task-1" {
		t.Errorf("spec.TaskID = %q, want %q", spec.TaskID, "task-1")
	}
	if len(spec.Criteria) != 2 {
		t.Fatalf("expected 2 criteria, got %d", len(spec.Criteria))
	}

	c0 := spec.Criteria[0]
	if c0.StepSequence != 0 {
		t.Errorf("criterion[0].StepSequence = %d, want 0", c0.StepSequence)
	}
	if c0.Description == "" {
		t.Error("criterion[0].Description should not be empty")
	}
	if !c0.Required {
		t.Error("criterion[0].Required should be true by default")
	}
}

func TestGenerateSpecFromSteps_Empty(t *testing.T) {
	spec := GenerateSpecFromSteps(nil)
	if spec.Criteria != nil {
		t.Errorf("expected nil criteria for empty steps, got %v", spec.Criteria)
	}
}

func TestGenerateSpecFromSteps_SetsAcceptanceCriteria(t *testing.T) {
	steps := []*task.TaskStep{
		task.NewTaskStep("task-1", "Fix the nil pointer dereference in server.go line 42", 0).WithToolHint("fix"),
	}

	spec := GenerateSpecFromSteps(steps)
	if len(spec.Criteria) != 1 {
		t.Fatalf("expected 1 criterion, got %d", len(spec.Criteria))
	}

	c := spec.Criteria[0]
	if c.AcceptanceCriteria == "" {
		t.Error("AcceptanceCriteria should not be empty")
	}
}

func TestSpecJSONRoundTrip(t *testing.T) {
	spec := &TaskSpec{
		TaskID: "task-test",
		Criteria: []StepCriterion{
			{
				StepSequence:       0,
				Description:        "Implement feature X",
				AcceptanceCriteria: "Feature X works end-to-end",
				Required:           true,
			},
		},
	}

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var restored TaskSpec
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if restored.TaskID != spec.TaskID {
		t.Errorf("TaskID mismatch: got %q, want %q", restored.TaskID, spec.TaskID)
	}
	if len(restored.Criteria) != 1 {
		t.Fatalf("Criteria count: got %d, want 1", len(restored.Criteria))
	}
	if restored.Criteria[0].AcceptanceCriteria != "Feature X works end-to-end" {
		t.Errorf("AcceptanceCriteria = %q", restored.Criteria[0].AcceptanceCriteria)
	}
}

func TestExtractSpecFromTask(t *testing.T) {
	spec := &TaskSpec{
		TaskID: "task-extract",
		Criteria: []StepCriterion{
			{StepSequence: 0, Description: "do the thing", Required: true},
		},
	}
	wrapper := specMetadataWrapper{Spec: spec}
	wrapperJSON, _ := json.Marshal(wrapper)

	tsk := task.NewTask("test", "test task")
	tsk.Metadata = wrapperJSON

	extracted := ExtractSpecFromTask(tsk)
	if extracted == nil {
		t.Fatal("expected non-nil spec from task metadata")
	}
	if extracted.TaskID != "task-extract" {
		t.Errorf("TaskID = %q, want %q", extracted.TaskID, "task-extract")
	}
}

func TestExtractSpecFromTask_NoMetadata(t *testing.T) {
	tsk := task.NewTask("test", "test task")
	extracted := ExtractSpecFromTask(tsk)
	if extracted != nil {
		t.Error("expected nil spec when task has no metadata")
	}
}

func TestExtractSpecFromTask_InvalidJSON(t *testing.T) {
	tsk := task.NewTask("test", "test task")
	tsk.Metadata = json.RawMessage(`{"not_a_spec": true}`)
	extracted := ExtractSpecFromTask(tsk)
	if extracted != nil {
		t.Error("expected nil spec when metadata does not contain a valid spec")
	}
}
