package runtime

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalBackend_Name(t *testing.T) {
	backend := NewLocalBackend()
	assert.Equal(t, "local", backend.Name())
}

func TestLocalBackend_Execute_Basic(t *testing.T) {
	backend := NewLocalBackend()
	result, err := backend.Execute(context.Background(), Command{
		Cmd: "echo hello",
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Output, "hello")
}

func TestLocalBackend_Execute_ExitCode(t *testing.T) {
	backend := NewLocalBackend()
	result, err := backend.Execute(context.Background(), Command{
		Cmd: "exit 42",
	})
	require.NoError(t, err)
	assert.Equal(t, 42, result.ExitCode)
}

func TestLocalBackend_Execute_WorkingDir(t *testing.T) {
	backend := NewLocalBackend()
	result, err := backend.Execute(context.Background(), Command{
		Cmd: "pwd",
		Dir: "/tmp",
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "/tmp")
}

func TestLocalBackend_Execute_Environment(t *testing.T) {
	backend := NewLocalBackend()
	result, err := backend.Execute(context.Background(), Command{
		Cmd: "echo $MYVAR",
		Env: map[string]string{"MYVAR": "test-value"},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "test-value")
}

func TestLocalBackend_Execute_ContextCancellation(t *testing.T) {
	backend := NewLocalBackend()
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately so the context expires before the command starts
	cancel()
	_, err := backend.Execute(ctx, Command{
		Cmd: "sleep 10",
	})
	assert.Error(t, err)
}

func TestLocalBackend_Close(t *testing.T) {
	backend := NewLocalBackend()
	err := backend.Close()
	assert.NoError(t, err)
}

func TestLocalBackend_Execute_Duration(t *testing.T) {
	backend := NewLocalBackend()
	result, err := backend.Execute(context.Background(), Command{
		Cmd: "echo done",
	})
	require.NoError(t, err)
	assert.Greater(t, result.Duration, time.Duration(0))
}

func TestLocalBackend_Execute_OutputToStderr(t *testing.T) {
	backend := NewLocalBackend()
	result, err := backend.Execute(context.Background(), Command{
		Cmd: "echo error >&2 && echo success",
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Output, "success")
	assert.Contains(t, result.Output, "error")
}

func TestLocalBackend_Execute_PassEnvOverride(t *testing.T) {
	backend := NewLocalBackend()
	// Set an existing env var and override it
	result, err := backend.Execute(context.Background(), Command{
		Cmd: "echo $HOME",
		Env: map[string]string{"HOME": "/nonexistent"},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "/nonexistent")
}

func TestLocalBackend_Execute_Noop(t *testing.T) {
	tmpDir := t.TempDir()
	backend := NewLocalBackend()

	// Create a file in the temp directory
	_, err := backend.Execute(context.Background(), Command{
		Cmd: "echo test > file.txt",
		Dir: tmpDir,
	})
	require.NoError(t, err)

	// Verify file exists
	content, err := os.ReadFile(tmpDir + "/file.txt")
	require.NoError(t, err)
	assert.Contains(t, string(content), "test")
}
