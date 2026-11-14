package task

import (
	"errors"
	"testing"
)

func TestTask_SetStateChecked(t *testing.T) {
	tests := []struct {
		name    string
		initial TaskState
		target  TaskState
		wantErr bool
	}{
		{"Valid: Pending to Planning", StatePending, StatePlanning, false},
		{"Valid: Pending to Executing", StatePending, StateExecuting, false},
		{"Valid: Planning to Executing", StatePlanning, StateExecuting, false},
		{"Valid: Executing to Testing", StateExecuting, StateTesting, false},
		{"Valid: Testing to Completed", StateTesting, StateCompleted, false},
		{"Invalid: Pending to Completed", StatePending, StateCompleted, true},
		{"Invalid: Completed to Pending", StateCompleted, StatePending, true},
		{"Invalid: Terminal to anything", StateCompleted, StatePlanning, true},
		{"Invalid: Self transition", StateExecuting, StateExecuting, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{
				State: tt.initial,
			}
			err := task.SetStateChecked(tt.target)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetStateChecked() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && task.State != tt.target {
				t.Errorf("SetStateChecked() state = %v, want %v", task.State, tt.target)
			}
			if tt.wantErr && !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("SetStateChecked() error = %v, want ErrInvalidTransition", err)
			}
		})
	}
}

func TestTask_StateTransitions(t *testing.T) {
	tests := []struct {
		from TaskState
		to   TaskState
		ok   bool
	}{
		// Pending
		{StatePending, StatePlanning, true},
		{StatePending, StateAwaitingApproval, true},
		{StatePending, StateExecuting, true},
		{StatePending, StateCancelled, true},
		{StatePending, StateCompleted, false},

		// Planning
		{StatePlanning, StateAwaitingApproval, true},
		{StatePlanning, StateExecuting, true},
		{StatePlanning, StateTesting, true},
		{StatePlanning, StateCancelled, true},
		{StatePlanning, StateFailed, true},
		{StatePlanning, StateCompleted, false},

		// AwaitingApproval
		{StateAwaitingApproval, StateExecuting, true},
		{StateAwaitingApproval, StateTesting, true},
		{StateAwaitingApproval, StateCancelled, true},
		{StateAwaitingApproval, StateFailed, true},
		{StateAwaitingApproval, StateRejected, true},
		{StateAwaitingApproval, StatePlanning, false},

		// Executing
		{StateExecuting, StateTesting, true},
		{StateExecuting, StateCompleted, true},
		{StateExecuting, StateFailed, true},
		{StateExecuting, StateRejected, true},
		{StateExecuting, StateCancelled, true},
		{StateExecuting, StatePending, false},
		{StateExecuting, StatePlanning, false},
		{StateExecuting, StateExecuting, false},

		// Testing
		{StateTesting, StateCompleted, true},
		{StateTesting, StateFailed, true},
		{StateTesting, StateRejected, true},
		{StateTesting, StateCancelled, true},
		{StateTesting, StateExecuting, true},
		{StateTesting, StatePlanning, false},

		// Terminal
		{StateCompleted, StatePlanning, false},
		{StateFailed, StatePlanning, false},
		{StateCancelled, StatePlanning, false},
		{StateRejected, StatePlanning, false},
	}

	for _, tt := range tests {
		got := taskAllows(tt.from, tt.to)
		if got != tt.ok {
			t.Errorf("taskAllows(%s, %s) = %v; want %v", tt.from, tt.to, got, tt.ok)
		}
	}
}
