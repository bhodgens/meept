package worker

import (
	"testing"
)

func TestState_String(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateIdle, "idle"},
		{StateClaiming, "claiming"},
		{StateProcessing, "processing"},
		{StateComplete, "complete"},
		{StateError, "error"},
		{StateStopping, "stopping"},
		{StateStopped, "stopped"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("State.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestState_IsActive(t *testing.T) {
	tests := []struct {
		state    State
		expected bool
	}{
		{StateIdle, false},
		{StateClaiming, true},
		{StateProcessing, true},
		{StateComplete, false},
		{StateError, false},
		{StateStopping, false},
		{StateStopped, false},
	}

	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			if got := tt.state.IsActive(); got != tt.expected {
				t.Errorf("State(%s).IsActive() = %v, want %v", tt.state, got, tt.expected)
			}
		})
	}
}

func TestState_CanClaim(t *testing.T) {
	tests := []struct {
		state    State
		expected bool
	}{
		{StateIdle, true},
		{StateClaiming, false},
		{StateProcessing, false},
		{StateComplete, true},
		{StateError, true},
		{StateStopping, false},
		{StateStopped, false},
	}

	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			if got := tt.state.CanClaim(); got != tt.expected {
				t.Errorf("State(%s).CanClaim() = %v, want %v", tt.state, got, tt.expected)
			}
		})
	}
}

func TestIsValidTransition(t *testing.T) {
	tests := []struct {
		name  string
		from  State
		to    State
		valid bool
	}{
		// Valid transitions
		{"stopped to idle", StateStopped, StateIdle, true},
		{"idle to claiming", StateIdle, StateClaiming, true},
		{"idle to stopping", StateIdle, StateStopping, true},
		{"claiming to processing", StateClaiming, StateProcessing, true},
		{"claiming to idle", StateClaiming, StateIdle, true},
		{"claiming to error", StateClaiming, StateError, true},
		{"claiming to stopping", StateClaiming, StateStopping, true},
		{"processing to complete", StateProcessing, StateComplete, true},
		{"processing to error", StateProcessing, StateError, true},
		{"processing to stopping", StateProcessing, StateStopping, true},
		{"complete to idle", StateComplete, StateIdle, true},
		{"complete to stopping", StateComplete, StateStopping, true},
		{"error to idle", StateError, StateIdle, true},
		{"error to stopping", StateError, StateStopping, true},
		{"stopping to stopped", StateStopping, StateStopped, true},

		// Invalid transitions
		{"idle to processing", StateIdle, StateProcessing, false},
		{"idle to complete", StateIdle, StateComplete, false},
		{"processing to idle", StateProcessing, StateIdle, false},
		{"complete to processing", StateComplete, StateProcessing, false},
		{"stopped to processing", StateStopped, StateProcessing, false},
		{"stopping to idle", StateStopping, StateIdle, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidTransition(tt.from, tt.to); got != tt.valid {
				t.Errorf("IsValidTransition(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.valid)
			}
		})
	}
}

func TestIsValidTransition_UnknownState(t *testing.T) {
	if IsValidTransition(State("unknown"), StateIdle) {
		t.Error("expected unknown state to have no valid transitions")
	}
}
