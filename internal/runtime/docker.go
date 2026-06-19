package runtime

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	docker "github.com/fsouza/go-dockerclient"
)

// DockerBackend executes commands inside Docker containers.
type DockerBackend struct {
	client      *docker.Client
	config      DockerConfig
	image       string
	containerID string
	logger      *slog.Logger
	mu          sync.Mutex
}

// NewDockerBackend creates a new Docker execution backend with a persistent container.
// It is exported for testing; uses alpine:latest if image is empty.
func NewDockerBackend(cfg DockerConfig) (*DockerBackend, error) {
	image := cfg.Image
	if image == "" {
		image = "alpine:latest"
	}
	return newDockerBackend(cfg, image, slog.Default())
}

// newDockerBackend creates a new Docker execution backend with a persistent container.
func newDockerBackend(cfg DockerConfig, image string, logger *slog.Logger) (*DockerBackend, error) {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	// Verify Docker daemon is accessible
	if err := client.Ping(); err != nil {
		return nil, fmt.Errorf("Docker daemon not reachable: %w", err)
	}

	// Ensure image exists
	if err := ensureImage(client, image); err != nil {
		return nil, fmt.Errorf("failed to ensure Docker image %q: %w", image, err)
	}

	// Create a persistent container for repeated execution
	hostConfig := &docker.HostConfig{
		Binds:       cfg.VolumeBinds,
		NetworkMode: cfg.NetworkMode,
	}

	container, err := client.CreateContainer(docker.CreateContainerOptions{
		Config: &docker.Config{
			Image:     image,
			Cmd:       []string{"sleep", "infinity"},
			OpenStdin: true,
			Tty:       false,
		},
		HostConfig: hostConfig,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker container: %w", err)
	}

	if err := client.StartContainer(container.ID, nil); err != nil {
		// Best-effort cleanup
		client.RemoveContainer(docker.RemoveContainerOptions{
			ID:            container.ID,
			RemoveVolumes: true,
			Force:         true,
		})
		return nil, fmt.Errorf("failed to start Docker container: %w", err)
	}

	logger.Info("Docker container started", "id", container.ID, "image", image)

	return &DockerBackend{
		client:      client,
		config:      cfg,
		image:       image,
		containerID: container.ID,
		logger:      logger,
	}, nil
}

// Name returns the backend identifier.
func (b *DockerBackend) Name() string {
	return "docker"
}

// Execute runs a command inside the container. It snapshots the container ID
// under a brief lock, then runs the exec without holding the lock so multiple
// concurrent Execute calls do not serialize.
func (b *DockerBackend) Execute(ctx context.Context, cmd Command) (*CommandResult, error) {
	b.mu.Lock()
	containerID := b.containerID
	b.mu.Unlock()

	if containerID == "" {
		return nil, fmt.Errorf("docker backend is closed")
	}

	timeout := b.config.Timeout
	if cmd.Timeout > 0 {
		timeout = cmd.Timeout
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	start := time.Now()

	// Build environment list
	env := make([]string, 0, len(cmd.Env))
	for k, v := range cmd.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Create exec instance
	execOpts := docker.CreateExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Container:    containerID,
		Cmd:          []string{"/bin/sh", "-c", cmd.Cmd},
		WorkingDir:   cmd.Dir,
		Env:          env,
	}

	exec, err := b.client.CreateExec(execOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create container exec: %w", err)
	}

	// Start exec and capture output. The blocking StartExec call does not
	// observe ctx cancellation, so we use StartExecNonBlocking which returns
	// a CloseWaiter. We then Wait() in a goroutine and select on ctx.Done()
	// to honor caller deadlines. On cancellation we Close() the waiter, which
	// terminates the exec's hijacked connection and stops the process.
	var stdout, stderr bytes.Buffer

	// Enable raw terminal so sh output is unbuffered
	startOpts := docker.StartExecOptions{
		Detach:       false,
		Tty:          false,
		OutputStream: &stdout,
		ErrorStream:  &stderr,
		RawTerminal:  true,
	}

	waiter, startErr := b.client.StartExecNonBlocking(exec.ID, startOpts)
	if startErr != nil {
		// Retry without raw terminal for certain errors
		startOpts.RawTerminal = false
		retryWaiter, retryErr := b.client.StartExecNonBlocking(exec.ID, startOpts)
		if retryErr != nil {
			return nil, fmt.Errorf("failed to start container exec: %w (raw retry: %v)", startErr, retryErr)
		}
		waiter = retryWaiter
	}

	type execResult struct{ err error }
	done := make(chan execResult, 1)

	go func() {
		done <- execResult{waiter.Wait()}
	}()

	select {
	case <-ctx.Done():
		// Best-effort: terminate the exec's hijacked connection. This causes
		// the process inside the container to receive SIGHUP / be reaped.
		_ = waiter.Close()
		// Drain the goroutine so it doesn't leak; ignore its result since we
		// already decided to return a cancellation error.
		go func() { <-done }()
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("%w: %s", ErrTimeout, cmd.Cmd)
		}
		return nil, fmt.Errorf("%w: %s", ErrCanceled, cmd.Cmd)
	case res := <-done:
		_ = waiter.Close()
		if res.err != nil {
			return nil, fmt.Errorf("container exec failed: %w", res.err)
		}
	}

	// Get exit code
	inspect, err := b.client.InspectExec(exec.ID)
	if err != nil {
		// If inspect fails, we can still return the captured output
		return &CommandResult{
			Output:   stdout.String() + stderr.String(),
			ExitCode: 1,
			Duration: time.Since(start),
		}, nil
	}

	output := stdout.String()
	if stderr.Len() > 0 {
		output += stderr.String()
	}

	return &CommandResult{
		Output:   output,
		ExitCode: inspect.ExitCode,
		Duration: time.Since(start),
	}, nil
}

// Close removes the container.
func (b *DockerBackend) Close() error {
	// Snapshot container ID under lock, then perform Docker API calls
	// outside the lock (CLAUDE.md "Mutex scope" rule).
	b.mu.Lock()
	containerID := b.containerID
	b.containerID = ""
	b.mu.Unlock()

	if containerID == "" {
		return nil
	}

	// Stop container
	if err := b.client.StopContainer(containerID, 5); err != nil {
		// Container may already be stopped
		b.logger.Debug("Docker container already stopped", "id", containerID)
	}

	// Remove container
	opts := docker.RemoveContainerOptions{
		ID:            containerID,
		RemoveVolumes: true,
		Force:         true,
	}

	if err := b.client.RemoveContainer(opts); err != nil {
		return fmt.Errorf("failed to remove Docker container: %w", err)
	}

	return nil
}

// ensureImage pulls the image if not present locally.
func ensureImage(client *docker.Client, image string) error {
	// Check if image exists
	_, err := client.InspectImage(image)
	if err == nil {
		return nil // Image already present
	}

	// Pull image
	return client.PullImage(docker.PullImageOptions{
		Repository: image,
	}, docker.AuthConfiguration{})
}

// Ensure ExecutionBackend interface is satisfied at compile time.
var _ ExecutionBackend = (*DockerBackend)(nil)
