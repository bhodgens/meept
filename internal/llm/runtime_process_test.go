package llm_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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
	err := p.Start(context.Background(), io.Discard, io.Discard)
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

	if err := p.Start(ctx, io.Discard, io.Discard); err != nil {
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

	// The pidfile is JSON: {"pid":N,"token":"<instance token>"}.
	var entry struct {
		PID   int    `json:"pid"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("failed to parse PID file JSON: %v", err)
	}

	if entry.PID <= 0 {
		t.Errorf("expected positive PID, got %d", entry.PID)
	}
	if entry.Token == "" {
		t.Error("pidfile written by Start must carry a non-empty instance token")
	}

	if p.PID() != entry.PID {
		t.Errorf("expected PID %d, got %d", entry.PID, p.PID())
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

	if err := p.Start(ctx, io.Discard, io.Discard); err != nil {
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

	if err := p.Start(ctx, io.Discard, io.Discard); err != nil {
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

	if err := p.Start(ctx1, io.Discard, io.Discard); err != nil {
		t.Fatalf("first start failed: %v", err)
	}

	// Starting again while already running should succeed without error
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	if err := p.Start(ctx2, io.Discard, io.Discard); err != nil {
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

// TestRuntimeProcess_Start_RefusesWhenEndpointHasListener pins the
// duplicate-spawn guard. A child that cannot bind is not harmless: mlx_lm keeps
// running with the model loaded and no socket, and the health check cannot see
// the failure because the foreign listener answers /health. When the spawn
// command declares the endpoint port, Start must refuse.
func TestRuntimeProcess_Start_RefusesWhenEndpointHasListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to occupy a port for the test: %v", err)
	}
	defer func() {
		if cerr := ln.Close(); cerr != nil {
			t.Logf("listener close: %v", cerr)
		}
	}()
	addr := ln.Addr().String()
	_, port, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		t.Fatalf("split listener address %q: %v", addr, splitErr)
	}

	pidFile := filepath.Join(createTempPIDDir(t), "duplicate.pid")
	cfg := &llm.RuntimeConfig{
		BaseURL:      "http://" + addr + "/v1",
		SpawnCommand: []string{"mlx_lm", "server", "--model", "/m/x", "--port", port},
		PIDFile:      pidFile,
	}
	p := llm.NewRuntimeProcess(cfg)

	startErr := p.Start(context.Background(), io.Discard, io.Discard)
	if startErr == nil {
		t.Fatal("expected Start to refuse an endpoint that already has a listener")
	}
	if !strings.Contains(startErr.Error(), "already has a listener") {
		t.Errorf("unexpected error text: %v", startErr)
	}
	if p.IsRunning() {
		t.Error("no process may run after a refused spawn")
	}
	if _, statErr := os.Stat(pidFile); statErr == nil {
		t.Error("a refused spawn must not write a pid file")
	}
}

// TestRuntimeProcess_Start_IgnoresPortNotBoundBySpawnCommand pins the scoping
// of the guard: the check belongs to the process that binds the port. A spawn
// command that never declares the endpoint port (a fake runtime in a test
// harness, or a wrapper that binds elsewhere) must not be refused for it.
func TestRuntimeProcess_Start_IgnoresPortNotBoundBySpawnCommand(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to occupy a port for the test: %v", err)
	}
	defer func() {
		if cerr := ln.Close(); cerr != nil {
			t.Logf("listener close: %v", cerr)
		}
	}()

	pidFile := filepath.Join(createTempPIDDir(t), "notbound.pid")
	cfg := &llm.RuntimeConfig{
		BaseURL:      "http://" + ln.Addr().String() + "/v1",
		SpawnCommand: []string{"sleep", "300"},
		PIDFile:      pidFile,
	}
	p := llm.NewRuntimeProcess(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Start(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("a spawn command that does not bind the port must not be refused: %v", err)
	}
	defer func() {
		if stopErr := p.Stop(ctx); stopErr != nil {
			t.Logf("stop: %v", stopErr)
		}
	}()

	if p.PID() == 0 {
		t.Error("expected a running pid after the spawn")
	}
}

// TestRuntimeProcess_Start_SpawnsWhenEndpointFree is the control for the guard:
// a free endpoint with a port-declaring spawn command must spawn normally.
func TestRuntimeProcess_Start_SpawnsWhenEndpointFree(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve a port for the test: %v", err)
	}
	addr := ln.Addr().String()
	if cerr := ln.Close(); cerr != nil {
		t.Fatalf("failed to release the reserved port: %v", cerr)
	}
	_, port, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		t.Fatalf("split reserved address %q: %v", addr, splitErr)
	}

	pidFile := filepath.Join(createTempPIDDir(t), "free.pid")
	cfg := &llm.RuntimeConfig{
		BaseURL: "http://" + addr + "/v1",
		// The port token makes the pre-check apply; /bin/sh ignores the extra
		// arguments after the command string.
		SpawnCommand: []string{"/bin/sh", "-c", "sleep 300", "--port", port},
		PIDFile:      pidFile,
	}
	p := llm.NewRuntimeProcess(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Start(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("unexpected refusal on a free endpoint: %v", err)
	}
	defer func() {
		if stopErr := p.Stop(ctx); stopErr != nil {
			t.Logf("stop: %v", stopErr)
		}
	}()

	if p.PID() == 0 {
		t.Error("expected a running pid after a successful spawn")
	}
}

// TestRuntimeProcess_Stop_RefusesNonOwned pins the ownership contract: Stop must
// tell the caller it stopped nothing when the runtime belongs to another meept
// process. Returning nil there (the old behaviour) made every stop surface —
// RPC, GUI, CLI — report success while the process kept the model and the port.
func TestRuntimeProcess_Stop_RefusesNonOwned(t *testing.T) {
	pidDir := createTempPIDDir(t)
	if err := os.MkdirAll(pidDir, 0o700); err != nil {
		t.Fatalf("create pid dir: %v", err)
	}
	pidFile := filepath.Join(pidDir, "foreign.pid")
	// Any live pid is enough: Stop must refuse before it inspects the process.
	entry := fmt.Sprintf(`{"pid":%d,"token":"foreign"}`, os.Getpid())
	if err := os.WriteFile(pidFile, []byte(entry), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	p := llm.NewRuntimeProcess(&llm.RuntimeConfig{PIDFile: pidFile})
	stopErr := p.Stop(context.Background())
	if !errors.Is(stopErr, llm.ErrRuntimeNotOwned) {
		t.Fatalf("Stop error = %v, want ErrRuntimeNotOwned", stopErr)
	}
	if _, statErr := os.Stat(pidFile); statErr != nil {
		t.Errorf("a refused stop must leave the pid file alone: %v", statErr)
	}
}

// TestRuntimeProcess_StopAsOperatorStopsRecordedPID pins the operator override:
// an explicit stop must actually stop the process the PID file names and clear
// the file. The victim is launched in its own process group and re-parented to
// init, so the process-group kill can only reach it.
func TestRuntimeProcess_StopAsOperatorStopsRecordedPID(t *testing.T) {
	launcher := exec.Command("/bin/sh", "-c", "nohup sleep 300 >/dev/null 2>&1 &")
	launcher.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := launcher.Run(); err != nil {
		t.Fatalf("launch victim: %v", err)
	}

	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		procs, listErr := llm.ListRuntimeProcesses()
		if listErr != nil {
			t.Fatalf("ListRuntimeProcesses: %v", listErr)
		}
		for _, proc := range procs {
			if proc.PPID == 1 && proc.Command == "sleep 300" {
				pid = proc.PID
				break
			}
		}
		if pid != 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pid == 0 {
		t.Skip("no re-parented sleep observed in this environment")
	}
	t.Cleanup(func() {
		if killErr := syscall.Kill(pid, syscall.SIGKILL); killErr != nil {
			t.Logf("cleanup kill %d: %v", pid, killErr)
		}
	})

	pidDir := createTempPIDDir(t)
	if err := os.MkdirAll(pidDir, 0o700); err != nil {
		t.Fatalf("create pid dir: %v", err)
	}
	pidFile := filepath.Join(pidDir, "operator.pid")
	entry := fmt.Sprintf(`{"pid":%d,"token":"foreign"}`, pid)
	if err := os.WriteFile(pidFile, []byte(entry), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// The config carries the runtime's spawn command so StopAsOperator can
	// verify the pid still runs this runtime before signalling it (audit
	// finding F16: a command-less config made the identity check inert).
	p := llm.NewRuntimeProcess(&llm.RuntimeConfig{
		PIDFile:      pidFile,
		SpawnCommand: []string{"sleep", "300"},
	})
	if err := p.StopAsOperator(ctx); err != nil {
		t.Fatalf("StopAsOperator: %v", err)
	}
	if aliveErr := syscall.Kill(pid, 0); aliveErr == nil {
		t.Errorf("pid %d must be gone after StopAsOperator", pid)
	}
	if _, statErr := os.Stat(pidFile); !os.IsNotExist(statErr) {
		t.Errorf("the stopped runtime's pid file must be removed, stat err = %v", statErr)
	}
}

// TestRuntimeProcess_StopAsOperator_FailsClosedWithoutIdentity pins audit
// finding F16's fail-closed rule at the operator surface: with neither a
// durable spawn record nor a configured spawn command, identity cannot be
// verified, so StopAsOperator must refuse instead of signalling a pid a stale
// pidfile names.
func TestRuntimeProcess_StopAsOperator_FailsClosedWithoutIdentity(t *testing.T) {
	victim := exec.Command("sleep", "300")
	victim.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := victim.Start(); err != nil {
		t.Fatalf("start victim: %v", err)
	}
	t.Cleanup(func() {
		_ = victim.Process.Kill()
		_, _ = victim.Process.Wait()
	})

	pidDir := createTempPIDDir(t)
	if err := os.MkdirAll(pidDir, 0o700); err != nil {
		t.Fatalf("create pid dir: %v", err)
	}
	pidFile := filepath.Join(pidDir, "noid.pid")
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf(`{"pid":%d,"token":"foreign"}`, victim.Process.Pid)), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := llm.NewRuntimeProcess(&llm.RuntimeConfig{PIDFile: pidFile}) // no SpawnCommand, no record
	err := p.StopAsOperator(ctx)
	if err == nil {
		t.Fatal("StopAsOperator must fail closed when identity cannot be verified (F16)")
	}
	if !strings.Contains(err.Error(), "cannot verify") {
		t.Errorf("unexpected error text: %v", err)
	}
	if aliveErr := syscall.Kill(victim.Process.Pid, 0); aliveErr != nil {
		t.Errorf("the unverifiable pid must be untouched, kill(0) err = %v", aliveErr)
	}
}

// TestRuntimeProcess_StartWritesAndStopRemovesSpawnRecord pins the durable
// record's lifecycle. It is what lets the orphan sweep match a leftover after
// the endpoint's config stops validating, and a record left behind would make
// the sweep chase a pid that no longer exists.
func TestRuntimeProcess_StartWritesAndStopRemovesSpawnRecord(t *testing.T) {
	pidDir := createTempPIDDir(t)
	if err := os.MkdirAll(pidDir, 0o700); err != nil {
		t.Fatalf("create pid dir: %v", err)
	}
	pidFile := filepath.Join(pidDir, "record.pid")

	cfg := &llm.RuntimeConfig{
		EndpointKey:  "mlx:127.0.0.1:8081",
		AutoStop:     true,
		PIDFile:      pidFile,
		SpawnCommand: []string{"sleep", "300"},
	}
	p := llm.NewRuntimeProcess(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Start(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("start: %v", err)
	}
	rec, err := llm.ReadSpawnRecord(pidFile)
	if err != nil {
		t.Fatalf("spawn record missing after a successful start: %v", err)
	}
	if len(rec.Argv) != len(cfg.SpawnCommand) || rec.Argv[0] != cfg.SpawnCommand[0] {
		t.Errorf("record argv = %v, want %v", rec.Argv, cfg.SpawnCommand)
	}
	if !rec.AutoStop {
		t.Error("record must carry the endpoint's auto_stop setting")
	}
	if rec.PID != p.PID() {
		t.Errorf("record pid = %d, want the spawned pid %d", rec.PID, p.PID())
	}

	if err := p.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, readErr := llm.ReadSpawnRecord(pidFile); readErr == nil {
		t.Error("the spawn record must be removed together with the runtime")
	}
}

// TestRuntimeProcess_ExitPathClearsStalePIDFileAndRecord pins audit finding F97:
// a runtime that exits on its own leaves no stale PID file/record behind for a
// later Stop's recovery branch to misread (StalePIDRemoval was dead code before
// this; the cleanup now runs on the wait goroutine's exit path).
func TestRuntimeProcess_ExitPathClearsStalePIDFileAndRecord(t *testing.T) {
	pidFile := filepath.Join(createTempPIDDir(t), "exit.pid")
	cfg := &llm.RuntimeConfig{
		EndpointKey:  "llama-cpp:127.0.0.1:8099",
		AutoStop:     true,
		PIDFile:      pidFile,
		SpawnCommand: []string{"sleep", "1"},
	}
	p := llm.NewRuntimeProcess(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.Start(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := llm.ReadSpawnRecord(pidFile); err != nil {
		t.Fatalf("record must exist after start: %v", err)
	}

	// The runtime exits on its own; the exit path must remove the stale handle.
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(pidFile); os.IsNotExist(err) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Fatalf("an exited runtime must leave no stale pid file (F97), stat err = %v", err)
	}
	if _, err := llm.ReadSpawnRecord(pidFile); err == nil {
		t.Error("an exited runtime must leave no stale spawn record (F97)")
	}
}

// TestRuntimeProcess_StopAsOperator_RefusesReusedPID pins the identity guard on
// the operator path: the PID file names a pid, but that pid runs something else
// (the recorded runtime is gone and the pid was reused). StopAsOperator must
// refuse instead of signalling an unrelated process.
func TestRuntimeProcess_StopAsOperator_RefusesReusedPID(t *testing.T) {
	launcher := exec.Command("/bin/sh", "-c", "nohup sleep 300 >/dev/null 2>&1 &")
	launcher.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := launcher.Run(); err != nil {
		t.Fatalf("launch victim: %v", err)
	}

	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		procs, listErr := llm.ListRuntimeProcesses()
		if listErr != nil {
			t.Fatalf("ListRuntimeProcesses: %v", listErr)
		}
		for _, proc := range procs {
			if proc.PPID == 1 && proc.Command == "sleep 300" {
				pid = proc.PID
				break
			}
		}
		if pid != 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pid == 0 {
		t.Skip("no re-parented sleep observed in this environment")
	}
	t.Cleanup(func() {
		if killErr := syscall.Kill(pid, syscall.SIGKILL); killErr != nil {
			t.Logf("cleanup kill %d: %v", pid, killErr)
		}
	})

	pidDir := createTempPIDDir(t)
	if err := os.MkdirAll(pidDir, 0o700); err != nil {
		t.Fatalf("create pid dir: %v", err)
	}
	pidFile := filepath.Join(pidDir, "reused.pid")
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf(`{"pid":%d,"token":"foreign"}`, pid)), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
	// The durable record says this endpoint's runtime is a model server; the pid
	// named by the pid file is not it.
	if err := llm.WriteSpawnRecord(llm.SpawnRecord{
		EndpointKey: "mlx:127.0.0.1:8081",
		PIDFile:     pidFile,
		Argv:        []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"},
		AutoStop:    true,
		PID:         pid,
	}); err != nil {
		t.Fatalf("write spawn record: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	p := llm.NewRuntimeProcess(&llm.RuntimeConfig{PIDFile: pidFile})
	stopErr := p.StopAsOperator(ctx)
	if stopErr == nil {
		t.Fatal("StopAsOperator must refuse a pid that does not run the recorded runtime")
	}
	if !strings.Contains(stopErr.Error(), "refusing to signal it") {
		t.Errorf("unexpected error text: %v", stopErr)
	}
	if aliveErr := syscall.Kill(pid, 0); aliveErr != nil {
		t.Errorf("the unrelated process %d must be untouched, kill(0) err = %v", pid, aliveErr)
	}
}
