package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewAgentLoop_SessionAndWorkingDir verifies that the constructor wires
// the sessionID and workingDir arguments into the corresponding getters.
func TestNewAgentLoop_SessionAndWorkingDir(t *testing.T) {
	loop := NewAgentLoop("s", "/tmp/work")

	assert.Equal(t, "s", loop.GetSessionID(), "GetSessionID should return the constructor argument")
	assert.Equal(t, "/tmp/work", loop.GetWorkingDir(), "GetWorkingDir should return the constructor argument")
}

// TestNewAgentLoop_PanicsOnEmptySessionID verifies the sessionID panic guard.
func TestNewAgentLoop_PanicsOnEmptySessionID(t *testing.T) {
	require.PanicsWithValue(t, "NewAgentLoop: sessionID required", func() {
		_ = NewAgentLoop("", "/tmp")
	})
}

// TestNewAgentLoop_PanicsOnEmptyWorkingDir verifies the workingDir panic guard.
func TestNewAgentLoop_PanicsOnEmptyWorkingDir(t *testing.T) {
	require.PanicsWithValue(t, "NewAgentLoop: workingDir required", func() {
		_ = NewAgentLoop("s", "")
	})
}

// TestNewAgentLoop_NoDefaultFallbacks verifies that a freshly constructed loop
// has zero-value session-context fields (no default-fallback behavior).
func TestNewAgentLoop_NoDefaultFallbacks(t *testing.T) {
	loop := NewAgentLoop("s", "/tmp")

	require.Nil(t, loop.GetDetectionContext(), "detectionContext should be nil by default")
	assert.Equal(t, "", loop.GetProjectID(), "projectID should be empty by default")
	assert.Equal(t, 0, loop.GetWorkerID(), "workerID should be zero by default")
	assert.False(t, loop.IsActive(), "isActive should be false by default")
}
