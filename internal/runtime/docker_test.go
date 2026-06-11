package runtime

import (
	"context"
	"os"
	"testing"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hasDocker checks if Docker daemon is actually available.
func hasDocker() bool {
	return os.Getenv("TEST_DOCKER") != ""
}

// dockerAvailable checks if the Docker socket is accessible by attempting a ping.
func dockerAvailable() bool {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		return false
	}
	return client.Ping() == nil
}

func TestDockerBackend_New_FailsWithoutDocker(t *testing.T) {
	// Skip if Docker is available (we can't reliably detect this without a running socket)
	if dockerAvailable() {
		t.Skip("Docker is available, skip this test")
	}

	_, err := NewDockerBackend(DockerConfig{
		Image: "alpine:latest",
	})
	// Without Docker, construction should fail gracefully
	assert.Error(t, err)
}

func TestDockerBackend_Execute_Basic(t *testing.T) {
	if !hasDocker() {
		t.Skip("Docker not available, set TEST_DOCKER=1 to run")
	}

	backend, err := NewDockerBackend(DockerConfig{
		Image: "alpine:latest",
	})
	require.NoError(t, err)
	defer backend.Close()

	result, err := backend.Execute(context.Background(), Command{
		Cmd: "echo hello-docker",
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Output, "hello-docker")
}

func TestDockerBackend_Execute_ExitCode(t *testing.T) {
	if !hasDocker() {
		t.Skip("Docker not available, set TEST_DOCKER=1 to run")
	}

	backend, err := NewDockerBackend(DockerConfig{
		Image: "alpine:latest",
	})
	require.NoError(t, err)
	defer backend.Close()

	result, err := backend.Execute(context.Background(), Command{
		Cmd: "exit 42",
	})
	require.NoError(t, err)
	assert.Equal(t, 42, result.ExitCode)
}

func TestDockerBackend_Execute_WorkingDir(t *testing.T) {
	if !hasDocker() {
		t.Skip("Docker not available, set TEST_DOCKER=1 to run")
	}

	backend, err := NewDockerBackend(DockerConfig{
		Image:   "alpine:latest",
		Workdir: "/tmp",
	})
	require.NoError(t, err)
	defer backend.Close()

	result, err := backend.Execute(context.Background(), Command{
		Cmd: "pwd",
		Dir: "/app",
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "/app")
}

func TestDockerBackend_Name(t *testing.T) {
	if !hasDocker() {
		t.Skip("Docker not available, set TEST_DOCKER=1 to run")
	}

	backend, err := NewDockerBackend(DockerConfig{
		Image: "alpine:latest",
	})
	require.NoError(t, err)
	defer backend.Close()

	assert.Equal(t, "docker", backend.Name())
}
