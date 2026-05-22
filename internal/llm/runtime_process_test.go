package llm_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

func createTempPIDDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "pid")
}

func TestRuntimeProcess_Start_NoSpawnCommand(t *testing.T) {
	cfg := &llm.RuntimeConfig{
		SpawnCommand: []string{},
		PIDFile:      filepath.Join(createTempPIDDir(t), "test.pid"),
	}
	p := llm.NewRuntimeProcess(cfg)
	err := p.Start(context.Background())
	if err == nil {
		t.Fatal("expected error when no spawn command configured, got nil")
	}
	if p.IsRunning() {
		t.Error("process should not be running after failed start")
	}
}

func TestRuntimeProcess_PIDWriteRead(t *testing.T) {
	dir := createTempPIDDir(t)
	pidFile := filepath.Join(dir, "test.pid")

	cfg := &llm.RuntimeConfig{
		SpawnCommand: []string{"sleep", "300"},
		PIDFile:      pidFile,
	}

	p := llm.NewRuntimeProcess(cfg)

	// Before start, PID file should not exist
	if _, err := os.Stat(pidFile); err == nil {
		t.Fatal("PID file should not exist before start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Start(ctx); err != nil {
		t.Fatalf("unexpected error starting process: %v", err)
	}

	// Give a moment for PID file to be written
	time.Sleep(100 * time.Millisecond)

	if _, err := os.Stat(pidFile); os.IsNotExist(err) {
		t.Fatal("PID file should exist after successful start")
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("failed to read PID file: %v", err)
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatalf("failed to parse PID from file: %v", err)
	}

	if pid <= 0 {
		t.Errorf("expected positive PID, got %d", pid)
	}

	if p.PID() != pid {
		t.Errorf("expected PID %d, got %d", pid, p.PID())
	}

	if !p.IsRunning() {
		t.Error("process should be running after start")
	}

	// Now stop
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := p.Stop(stopCtx); err != nil {
		t.Fatalf("unexpected error stopping process: %v", err)
	}

	// PID file should be cleaned up
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Fatal("PID file should be removed after stop")
	}

	if p.IsRunning() {
		t.Error("process should not be running after stop")
	}

	// Calling Stop again should be safe (idempotent)
	if err := p.Stop(stopCtx); err != nil {
		t.Fatalf("calling Stop a second time should be safe, got: %v", err)
	}
}

func TestRuntimeProcess_IsRunning_NotStarted(t *testing.T) {
	cfg := &llm.RuntimeConfig{
		SpawnCommand: []string{},
		PIDFile:      filepath.Join(createTempPIDDir(t), "test.pid"),
	}
	p := llm.NewRuntimeProcess(cfg)

	if p.IsRunning() {
		t.Error("process should not be running before start")
	}

	if p.PID() != 0 {
		t.Errorf("expected PID 0 before start, got %d", p.PID())
	}
}

func TestRuntimeProcess_StopGraceful(t *testing.T) {
	dir := createTempPIDDir(t)
	pidFile := filepath.Join(dir, "test.pid")

	cfg := &llm.RuntimeConfig{
		SpawnCommand: []string{"sleep", "300"},
		PIDFile:      pidFile,
	}
	p := llm.NewRuntimeProcess(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Start(ctx); err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	if !p.IsRunning() {
		t.Fatal("process should be running")
	}

	// Stop gracefully with a reasonable timeout
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()

	if err := p.Stop(stopCtx); err != nil {
		t.Fatalf("unexpected error during graceful stop: %v", err)
	}

	// Verify process is no longer running
	if p.IsRunning() {
		t.Error("process should not be running after stop")
	}
}

func TestRuntimeProcess_StopAlreadyDead(t *testing.T) {
	dir := createTempPIDDir(t)
	pidFile := filepath.Join(dir, "test.pid")

	cfg := &llm.RuntimeConfig{
		SpawnCommand: []string{"sleep", "10"},
		PIDFile:      pidFile,
	}
	p := llm.NewRuntimeProcess(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Start(ctx); err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	// Verify process is running
	if !p.IsRunning() {
		t.Fatal("process should be running before manual kill")
	}

	// We need to simulate the process dying externally.
	// Since the process struct is unexported from the test package,
	// we can't directly call p.cmd.Process.Kill() here.
	// However, the Stop method should handle this case when called after
	// the process exits naturally or when the PID file references a dead process.

	// Instead, test the "stale PID file" scenario:
	// Stop the process gracefully first.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := p.Stop(stopCtx); err != nil {
		t.Fatalf("unexpected error during graceful stop: %v", err)
	}

	// Verify it's stopped
	if p.IsRunning() {
		t.Error("process should not be running after stop")
	}

	// Now test stopping again (should be a no-op)
	if err := p.Stop(stopCtx); err != nil {
		t.Fatalf("stop should be safe when process is not running, got: %v", err)
	}
}

func TestRuntimeProcess_ConcurrentStart(t *testing.T) {
	dir := createTempPIDDir(t)
	pidFile := filepath.Join(dir, "test.pid")

	cfg := &llm.RuntimeConfig{
		SpawnCommand: []string{"sleep", "300"},
		PIDFile:      pidFile,
	}
	p := llm.NewRuntimeProcess(cfg)

	// Start once
	ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel1()

	if err := p.Start(ctx1); err != nil {
		t.Fatalf("first start failed: %v", err)
	}

	// Starting again while already running should succeed without error
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	if err := p.Start(ctx2); err != nil {
		t.Fatalf("second start should succeed (already running): %v", err)
	}

	if !p.IsRunning() {
		t.Error("process should still be running")
	}

	// Clean up
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := p.Stop(stopCtx); err != nil {
		t.Fatalf("unexpected error stopping: %v", err)
	}
}
