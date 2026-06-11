package runtime

import (
	"context"
	"testing"

	"log/slog"
	"os"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, nil))
}

func TestManager_GetBackend_Local(t *testing.T) {
	mgr, err := NewContainerManager(Config{
		DefaultBackend: "local",
	}, testLogger())
	require.NoError(t, err)
	require.NotNil(t, mgr)

	backend := mgr.GetBackend("local")
	assert.NotNil(t, backend)
	assert.Equal(t, "local", backend.Name())
}

func TestManager_GetBackend_Unknown(t *testing.T) {
	mgr, err := NewContainerManager(Config{
		DefaultBackend: "local",
	}, testLogger())
	require.NoError(t, err)

	backend := mgr.GetBackend("nonexistent")
	assert.Nil(t, backend)
}

func TestManager_GetDefaultBackend(t *testing.T) {
	mgr, err := NewContainerManager(Config{
		DefaultBackend: "local",
	}, testLogger())
	require.NoError(t, err)

	backend := mgr.GetDefaultBackend()
	assert.NotNil(t, backend)
	assert.Equal(t, "local", backend.Name())
}

func TestManager_ListBackends(t *testing.T) {
	mgr, err := NewContainerManager(Config{
		DefaultBackend: "local",
	}, testLogger())
	require.NoError(t, err)

	names := mgr.ListBackends()
	assert.Contains(t, names, "local")
}

func TestManager_Close(t *testing.T) {
	mgr, err := NewContainerManager(Config{
		DefaultBackend: "local",
	}, testLogger())
	require.NoError(t, err)

	err = mgr.Close()
	assert.NoError(t, err)

	// Subsequent GetBackend should return nil
	backend := mgr.GetBackend("local")
	assert.Nil(t, backend)
}

func TestManager_DefaultBackend_Name(t *testing.T) {
	mgr, err := NewContainerManager(Config{
		DefaultBackend: "local",
	}, testLogger())
	require.NoError(t, err)

	assert.Equal(t, "local", mgr.DefaultBackend())
}

func TestManager_InvalidDefaultBackend(t *testing.T) {
	_, err := NewContainerManager(Config{
		DefaultBackend: "invalid",
	}, testLogger())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown default backend")
}

func TestManager_ExecAfterClose(t *testing.T) {
	mgr, err := NewContainerManager(Config{
		DefaultBackend: "local",
	}, testLogger())
	require.NoError(t, err)

	err = mgr.Close()
	assert.NoError(t, err)

	// Exec after close should fail through nil backend
	backend := mgr.GetDefaultBackend()
	assert.Nil(t, backend)
}

func TestManager_LocalBackendExecute(t *testing.T) {
	mgr, err := NewContainerManager(Config{
		DefaultBackend: "local",
	}, testLogger())
	require.NoError(t, err)
	defer mgr.Close()

	backend := mgr.GetDefaultBackend()
	require.NotNil(t, backend)

	result, err := backend.Execute(context.Background(), Command{
		Cmd: "echo manager-test",
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Output, "manager-test")
}

func TestManager_DockerBackend_FallbackOrUse(t *testing.T) {
	mgr, err := NewContainerManager(Config{
		DefaultBackend: "docker",
		Docker: DockerConfig{
			Image: "alpine:latest",
		},
	}, testLogger())
	require.NoError(t, err)
	require.NotNil(t, mgr)

	backend := mgr.GetDefaultBackend()
	require.NotNil(t, backend)

	result, err := backend.Execute(context.Background(), Command{
		Cmd: "echo fallback-test",
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "fallback-test")
}
