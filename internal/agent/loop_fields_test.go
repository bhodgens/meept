package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAgentLoop_Fields_DefaultZero verifies that the new worker pool fields
// default to zero values on a freshly constructed loop.
func TestAgentLoop_Fields_DefaultZero(t *testing.T) {
	loop := NewAgentLoop("test-session", "/tmp")

	require.Nil(t, loop.GetDetectionContext(), "detectionContext should be nil by default")
	assert.Equal(t, "", loop.GetProjectID(), "projectID should be empty by default")
	assert.Equal(t, 0, loop.GetWorkerID(), "workerID should be zero by default")
	assert.False(t, loop.IsActive(), "isActive should be false by default")
}

// TestAgentLoop_DetectionContext verifies SetDetectionContext / GetDetectionContext.
func TestAgentLoop_DetectionContext(t *testing.T) {
	loop := NewAgentLoop("test-session", "/tmp")

	dc := &DetectionContext{
		CWD:               "/home/user/project",
		DetectedProjectID: "proj-123",
		CLIArgs:           []string{"--verbose", "--port=8080"},
	}
	loop.SetDetectionContext(dc)

	got := loop.GetDetectionContext()
	require.NotNil(t, got)
	assert.Equal(t, "/home/user/project", got.CWD)
	assert.Equal(t, "proj-123", got.DetectedProjectID)
	assert.Equal(t, []string{"--verbose", "--port=8080"}, got.CLIArgs)

	// Nil guard: calling SetDetectionContext(nil) should not clear existing value
	loop.SetDetectionContext(nil)
	assert.NotNil(t, loop.GetDetectionContext(), "nil SetDetectionContext should not clear existing value")
}

// TestAgentLoop_ProjectID verifies SetProjectID / GetProjectID.
func TestAgentLoop_ProjectID(t *testing.T) {
	loop := NewAgentLoop("test-session", "/tmp")

	loop.SetProjectID("my-project")
	assert.Equal(t, "my-project", loop.GetProjectID())

	loop.SetProjectID("other-project")
	assert.Equal(t, "other-project", loop.GetProjectID())

	// Empty string is a valid value (clears the binding)
	loop.SetProjectID("")
	assert.Equal(t, "", loop.GetProjectID())
}

// TestAgentLoop_WorkerID verifies SetWorkerID / GetWorkerID.
func TestAgentLoop_WorkerID(t *testing.T) {
	loop := NewAgentLoop("test-session", "/tmp")

	assert.Equal(t, 0, loop.GetWorkerID())

	loop.SetWorkerID(5)
	assert.Equal(t, 5, loop.GetWorkerID())

	loop.SetWorkerID(42)
	assert.Equal(t, 42, loop.GetWorkerID())
}

// TestAgentLoop_IsActive verifies SetActive / IsActive toggle.
func TestAgentLoop_IsActive(t *testing.T) {
	loop := NewAgentLoop("test-session", "/tmp")

	assert.False(t, loop.IsActive())

	loop.SetActive(true)
	assert.True(t, loop.IsActive())

	loop.SetActive(false)
	assert.False(t, loop.IsActive())
}

// TestAgentLoop_Options verifies WithDetectionContext, WithProjectID, WithWorkerID
// functional options work in the constructor.
func TestAgentLoop_Options(t *testing.T) {
	dc := &DetectionContext{
		CWD:               "/workspace",
		DetectedProjectID: "detected-456",
		CLIArgs:           []string{"chat"},
	}

	loop := NewAgentLoop("test-session", "/tmp",
		WithDetectionContext(dc),
		WithProjectID("bound-project"),
		WithWorkerID(7),
	)

	// DetectionContext
	got := loop.GetDetectionContext()
	require.NotNil(t, got)
	assert.Equal(t, "/workspace", got.CWD)
	assert.Equal(t, "detected-456", got.DetectedProjectID)
	assert.Equal(t, []string{"chat"}, got.CLIArgs)

	// ProjectID
	assert.Equal(t, "bound-project", loop.GetProjectID())

	// WorkerID
	assert.Equal(t, 7, loop.GetWorkerID())
}

// TestAgentLoop_Options_NilGuards verifies that With* options are nil-safe.
func TestAgentLoop_Options_NilGuards(t *testing.T) {
	t.Run("WithDetectionContext nil is safe", func(t *testing.T) {
		loop := NewAgentLoop("test-session", "/tmp",
			WithDetectionContext(nil),
		)
		assert.Nil(t, loop.GetDetectionContext(), "nil WithDetectionContext should leave field nil")
	})

	t.Run("WithDetectionContext does not overwrite with nil", func(t *testing.T) {
		dc := &DetectionContext{CWD: "/first"}
		loop := NewAgentLoop("test-session", "/tmp",
			WithDetectionContext(dc),
			WithDetectionContext(nil), // should be a no-op
		)
		got := loop.GetDetectionContext()
		require.NotNil(t, got)
		assert.Equal(t, "/first", got.CWD)
	})
}

// TestAgentLoop_IsActive_Concurrent verifies atomic read/write under concurrency.
func TestAgentLoop_IsActive_Concurrent(t *testing.T) {
	loop := NewAgentLoop("test-session", "/tmp")

	done := make(chan struct{})

	// Writer goroutine
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			loop.SetActive(true)
			loop.SetActive(false)
		}
	}()

	// Reader goroutine: just ensure no panic/race
	for i := 0; i < 1000; i++ {
		_ = loop.IsActive()
	}

	<-done
}
