package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTestHarness_Validate(t *testing.T) {
	backend := NewLocalBackend()
	harness := NewTestHarness(TestHarnessConfig{
		InstallCommand: "echo installing",
		TestCommand:    "echo 'tests passed'",
	}, backend)

	result, err := harness.Validate(context.Background(), "/tmp")
	require.NoError(t, err)
	assert.True(t, result.Passed)
	assert.Contains(t, result.Output, "tests passed")
}

func TestTestHarness_Validate_WithoutInstall(t *testing.T) {
	backend := NewLocalBackend()
	harness := NewTestHarness(TestHarnessConfig{
		TestCommand: "echo pass",
	}, backend)

	result, err := harness.Validate(context.Background(), "/tmp")
	require.NoError(t, err)
	assert.True(t, result.Passed)
	assert.Contains(t, result.Output, "pass")
}

func TestTestHarness_Validate_FailingTest(t *testing.T) {
	backend := NewLocalBackend()
	harness := NewTestHarness(TestHarnessConfig{
		TestCommand: "exit 1",
	}, backend)

	result, err := harness.Validate(context.Background(), "/tmp")
	require.NoError(t, err)
	assert.False(t, result.Passed)
}

func TestTestHarness_Validate_NullBackend(t *testing.T) {
	// Harness with nil backend should return Passed (no tests to run)
	harness := NewTestHarness(TestHarnessConfig{
		TestCommand: "echo should-not-run",
	}, nil)

	result, err := harness.Validate(context.Background(), "/tmp")
	require.NoError(t, err)
	// With nil backend, result defaults to Passed (no-op)
	assert.True(t, result.Passed)
}

func TestTestHarness_Validate_InstallFails(t *testing.T) {
	backend := NewLocalBackend()
	harness := NewTestHarness(TestHarnessConfig{
		InstallCommand: "exit 1",
		TestCommand:    "echo should-not-run",
	}, backend)

	result, err := harness.Validate(context.Background(), "/tmp")
	require.NoError(t, err)
	assert.False(t, result.Passed)
}
