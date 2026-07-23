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

// TestNewAgentLoop_NilOnEmptySessionID verifies the sessionID guard logs and
// returns nil instead of panicking.
func TestNewAgentLoop_NilOnEmptySessionID(t *testing.T) {
	loop := NewAgentLoop("", "/tmp")
	assert.Nil(t, loop, "NewAgentLoop with empty sessionID should return nil")
}

// TestNewAgentLoop_AllowsEmptyWorkingDir verifies that the constructor permits
// an empty workingDir for two-phase initialization (registry creates loops
// with empty workingDir, then calls SetWorkingDir once the session binds to
// a project). There is no os.Getwd() default fallback.
func TestNewAgentLoop_AllowsEmptyWorkingDir(t *testing.T) {
	loop := NewAgentLoop("s", "")

	assert.Equal(t, "", loop.GetWorkingDir(), "empty workingDir should stay empty (no default fallback)")
	assert.Equal(t, "s", loop.GetSessionID(), "sessionID should still be wired")
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
