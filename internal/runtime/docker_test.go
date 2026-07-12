package runtime

import (
	"context"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hasDocker checks if Docker daemon is actually available.
func hasDocker() bool {
	return os.Getenv("TEST_DOCKER") != ""
}

// dockerAvailable checks if the Docker socket is accessible by attempting a ping.
// Uses a short dial timeout and skips the server API version check (which has
// no timeout in the library) to avoid hanging the test suite when Docker is
// not running.
func dockerAvailable() bool {
	dockerHost := os.Getenv("DOCKER_HOST")
	if dockerHost == "" {
		dockerHost = "unix:///var/run/docker.sock"
	}

	client, err := docker.NewClient(dockerHost)
	if err != nil {
		return false
	}
	// SkipServerVersionCheck avoids an internal GET /version call that
	// doesn't accept a context and will hang on a missing Unix socket.
	client.SkipServerVersionCheck = true
	// Override the HTTP client with a short dial timeout.
	client.HTTPClient = &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 2 * time.Second}
				// Extract the socket path from dockerHost to support
				// non-standard locations (Colima: run/docker.sock, Podman:
				// /run/storage/docker.sock, DOCKER_HOST overrides).
				path := dockerHost
				if len(path) > len("unix://") {
					path = path[len("unix://"):]
				}
				return d.DialContext(ctx, "unix", path)
			},
		},
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
