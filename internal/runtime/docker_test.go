package runtime

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
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

// newTestUnixSocket creates a real Unix socket listener at path (with parent
// dirs) so candidate socket presence is provable in dockerHostFromEnv tests.
func newTestUnixSocket(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	l, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { l.Close(); os.RemoveAll(path) })
}

func TestDockerHostFromEnv_ExplicitEnvWins(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///explicit/sock")
	assert.Equal(t, "unix:///explicit/sock", dockerHostFromEnv())
}

func TestDockerHostFromEnv_LegacyDefaultWhenNothingPresent(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	// Empty home -> no per-user candidates; the historical default is the
	// answer (CI Linux runners have no /var/run/docker.sock either).
	t.Setenv("HOME", t.TempDir())
	got := dockerHostFromEnv()
	// The historical default is the answer (no per-user sockets exist
	// under an empty HOME).
	alt := "unix:///var/run/docker.sock"
	require.Equal(t, alt, got, "expected legacy default when HOME has no sockets")
}

func TestDockerHostFromEnv_FindsOrbStackSocket(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	skipIfSystemDockerSocket(t)
	home := shortTempDir(t)
	t.Setenv("HOME", home)
	orb := filepath.Join(home, ".orbstack", "run", "docker.sock")
	newTestUnixSocket(t, orb)
	assert.Equal(t, "unix://"+orb, dockerHostFromEnv())
}

func TestDockerHostFromEnv_DesktopBeatsOrbStack(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	skipIfSystemDockerSocket(t)
	home := shortTempDir(t)
	t.Setenv("HOME", home)
	newTestUnixSocket(t, filepath.Join(home, ".orbstack", "run", "docker.sock"))
	desktop := filepath.Join(home, ".docker", "run", "docker.sock")
	newTestUnixSocket(t, desktop)
	assert.Equal(t, "unix://"+desktop, dockerHostFromEnv())
}

// skipIfSystemDockerSocket skips the candidate-precedence tests on hosts
// where the system socket exists: it outranks per-user candidates by
// design, so a real daemon there makes the per-user expectation invalid.
func skipIfSystemDockerSocket(t *testing.T) {
	t.Helper()
	if fi, err := os.Stat("/var/run/docker.sock"); err == nil && fi.Mode()&os.ModeSocket != 0 {
		t.Skip("/var/run/docker.sock is live on this host; system socket intentionally outranks per-user candidates")
	}
}

// shortTempDir returns a temp HOME short enough that "<home>/.orbstack/run/
// docker.sock" stays under the macOS 104-byte sun_path limit (a Test-prefixed
// t.TempDir() blows past it).
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "dbx")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
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
