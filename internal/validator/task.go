package validator

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/task"
)

// TaskValidator validates task-level completion.
type TaskValidator struct {
	stepValidator *StepValidator
	taskStore     *task.Store
	stepStore     *task.StepStore
}

// NewTaskValidator creates a new TaskValidator.
func NewTaskValidator(stepValidator *StepValidator, taskStore *task.Store, stepStore *task.StepStore) *TaskValidator {
	return &TaskValidator{
		stepValidator: stepValidator,
		taskStore:     taskStore,
		stepStore:     stepStore,
	}
}

// ValidateTaskCompletion validates that all steps in a task are properly validated.
func (v *TaskValidator) ValidateTaskCompletion(ctx context.Context, taskID string) error {
	steps, err := v.stepStore.ListByTaskID(taskID)
	if err != nil {
		return err
	}

	var validationErrors []string
	for _, step := range steps {
		// Check if step completed but not validated
		if step.State.IsSuccessfullyTerminal() && !step.Validated {
			validationErrors = append(validationErrors,
				fmt.Sprintf("step %s completed but not validated", step.ID))
		}

		// Check if step has validation errors
		if step.ValidationError != "" {
			validationErrors = append(validationErrors,
				fmt.Sprintf("step %s has validation error: %s", step.ID, step.ValidationError))
		}
	}

	if len(validationErrors) > 0 {
		return fmt.Errorf("task validation incomplete: %s", strings.Join(validationErrors, ", "))
	}

	return nil
}

// ValidateStep validates a single step and updates its validation status.
func (v *TaskValidator) ValidateStep(ctx context.Context, step *task.TaskStep) error {
	if v.stepValidator == nil {
		return nil
	}

	result := v.stepValidator.Validate(ctx, step)
	if !result.Valid {
		step.Validated = false
		step.ValidationError = strings.Join(result.Errors, ", ")
		return fmt.Errorf("step validation failed: %s", step.ValidationError)
	}

	step.Validated = true
	step.ValidationError = ""
	return nil
}
