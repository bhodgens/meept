package daemon

// Tests for kanban-board leaf 04/04 (daemon-spawn-supervision): the
// [kanban].sidecar_enabled gate, the supervised spawn + crash-respawn loop
// with the restart budget, the graceful stop, and the ownership posture that
// keeps a live instance's sidecar alive (StartupOrphanSweep semantics are
// exercised by internal/daemon/orphan_test.go and internal/llm's sweep
// suites; here the supervisor must NEVER signal the foreign-token pid).
//
// The fake sidecar is a shell sleep script (the cheap scripted-helper
// precedent in this package: daemonpath_test.go plants #!/bin/sh stubs):
// it appends its pid to a marker file (the spawn evidence, read back from
// disk — never trusted from the process table alone) and traps TERM for a
// clean exit, mirroring cmd/meept-kanban's SIGTERM drain.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// kanbanFakeSidecar is the scripted fake: one pid line per spawn into
// $KANBAN_FAKE_MARKER, SIGTERM-clean, SIGKILL-able for the crash test.
const kanbanFakeSidecar = `#!/bin/sh
echo $$ >> "$KANBAN_FAKE_MARKER"
trap 'exit 0' TERM
while :; do sleep 0.2; done
`

// newKanbanTestDaemon builds a Daemon whose kanban boot is fully exercisable
// without the component stack: the sidecar spawn path reads only
// config/logger/socket, and the fake binary arrives through the
// kanbanBinaryResolver seam.
func newKanbanTestDaemon(t *testing.T, sidecarEnabled, boardEnabled bool) *Daemon {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.EnvMeeptHome, home)
	cfg := config.DefaultConfig()
	cfg.Kanban.Enabled = boardEnabled
	cfg.Kanban.SidecarEnabled = sidecarEnabled
	cfg.Transport.RPC.Enabled = true
	d := &Daemon{
		config:     &Config{SocketPath: filepath.Join(home, "meept.sock")},
		fullConfig: cfg,
		logger:     slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	t.Cleanup(func() { d.stopKanbanSidecar(context.Background()) })
	return d
}

// plantFakeSidecar writes the fake child script and points both the
// binary-resolution seam and the marker env at it; restores the seam.
func plantFakeSidecar(t *testing.T) (marker string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "meept-kanban")
	if err := os.WriteFile(bin, []byte(kanbanFakeSidecar), 0o755); err != nil { //nolint:gosec // G306: test-only fixture script
		t.Fatalf("write fake sidecar: %v", err)
	}
	marker = filepath.Join(dir, "pids")
	if err := os.WriteFile(marker, nil, 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatalf("init marker: %v", err)
	}
	t.Setenv("KANBAN_FAKE_MARKER", marker)
	prev := kanbanBinaryResolver
	kanbanBinaryResolver = func(string) string { return bin }
	t.Cleanup(func() { kanbanBinaryResolver = prev })
	return marker
}

func readMarker(t *testing.T, marker string) []string {
	t.Helper()
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	line := strings.TrimSpace(string(data))
	if line == "" {
		return nil
	}
	return strings.Fields(line)
}

func waitForMarkerLines(t *testing.T, marker string, want int, timeout time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pids := readMarker(t, marker)
		if len(pids) >= want {
			return pids
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("marker %s never reached %d spawn lines (have %v)", marker, want, readMarker(t, marker))
	return nil
}

func pidAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// waitPidGone blocks until pid stops answering signal 0.
func waitPidGone(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("pid %d still alive after %v", pid, timeout)
}

func TestKanbanSidecarDisabledIsByteIdentical(t *testing.T) {
	d := newKanbanTestDaemon(t, false, false)
	resolved := 0
	prev := kanbanBinaryResolver
	kanbanBinaryResolver = func(string) string { resolved++; return "/nonexistent" }
	t.Cleanup(func() { kanbanBinaryResolver = prev })

	if err := d.StartKanbanSidecar(context.Background()); err != nil {
		t.Fatalf("disabled boot must return nil, got %v", err)
	}
	if d.kanbanSidecar != nil {
		t.Fatal("disabled boot must not construct a supervisor")
	}
	if resolved != 0 {
		t.Fatal("disabled boot must not even resolve the child binary")
	}
	// The default config lands here too (byte-identical guarantee).
	if config.DefaultConfig().Kanban.SidecarEnabled {
		t.Fatal("[kanban].sidecar_enabled default must be false")
	}
}

func TestKanbanSidecarSpawnsAndRestartsAfterKill(t *testing.T) {
	marker := plantFakeSidecar(t)
	d := newKanbanTestDaemon(t, true, true)
	// Fast loop timings for the test budget.
	d.StartKanbanSidecarForTest(t, kanbanSidecarConfig{
		PollInterval:  50 * time.Millisecond,
		BackoffBase:   50 * time.Millisecond,
		RestartWindow: time.Minute,
		RestartMax:    5,
	})

	pids := waitForMarkerLines(t, marker, 1, 5*time.Second)
	firstPID := atoiPID(t, pids[0])

	// Crash it (SIGKILL: the unexpected-exit the supervisor must notice).
	if err := syscall.Kill(firstPID, syscall.SIGKILL); err != nil {
		t.Fatalf("kill child: %v", err)
	}
	pids = waitForMarkerLines(t, marker, 2, 10*time.Second)
	secondPID := atoiPID(t, pids[1])
	if secondPID == firstPID {
		t.Fatal("respawn reused the same pid — marker is stale")
	}

	// Graceful stop takes the current child down and stops the loop.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := d.kanbanSidecar.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	waitPidGone(t, secondPID, 8*time.Second)

	// After Stop the loop must not respawn: freeze the marker, wait out
	// several poll intervals.
	before := len(readMarker(t, marker))
	time.Sleep(400 * time.Millisecond)
	if after := len(readMarker(t, marker)); after != before {
		t.Fatalf("supervisor respawned after Stop: marker grew %d -> %d lines", before, after)
	}
}

func TestKanbanSidecarStopIsGracefulSIGTERM(t *testing.T) {
	marker := plantFakeSidecar(t)
	d := newKanbanTestDaemon(t, true, true)
	d.StartKanbanSidecarForTest(t, kanbanSidecarConfig{
		PollInterval: 50 * time.Millisecond,
		StopGrace:    5 * time.Second,
	})
	pids := waitForMarkerLines(t, marker, 1, 5*time.Second)
	pid := atoiPID(t, pids[0])

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := d.kanbanSidecar.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	// The fake exits 0 on TERM: the pid must vanish (a reaped direct child
	// leaves no zombie after waitPidGone; the wrapper-less test-binary
	// spawn goes through RuntimeProcess's wait goroutine).
	waitPidGone(t, pid, 8*time.Second)
	// Stop is idempotent.
	if err := d.kanbanSidecar.Stop(ctx); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}

func TestKanbanSidecarRestartBudgetGivesUp(t *testing.T) {
	marker := plantFakeSidecar(t)
	d := newKanbanTestDaemon(t, true, true)
	d.StartKanbanSidecarForTest(t, kanbanSidecarConfig{
		PollInterval:  20 * time.Millisecond,
		BackoffBase:   10 * time.Millisecond,
		BackoffMax:    10 * time.Millisecond,
		RestartWindow: time.Hour, // freeze the window: budget = absolute cap
		RestartMax:    3,
	})
	// Kill each generation; after the 3rd respawn budget is exhausted the
	// loop logs ERROR and stops: the marker freezes at 4 lines (initial +
	// 3 restarts... the budget counts the initial attempt too).
	var last int
	for range 6 {
		pids := readMarker(t, marker)
		if len(pids) == 0 {
			pids = waitForMarkerLines(t, marker, 1, 5*time.Second)
		}
		last = atoiPID(t, pids[len(pids)-1])
		if !pidAlive(last) {
			break
		}
		if err := syscall.Kill(last, syscall.SIGKILL); err != nil && !strings.Contains(err.Error(), "no such process") {
			t.Fatalf("kill: %v", err)
		}
		time.Sleep(80 * time.Millisecond)
	}
	count := len(readMarker(t, marker))
	if count > 4 {
		t.Fatalf("restart budget not enforced: %d spawns (want <= 4 = initial + 3)", count)
	}
	// And it must STAY frozen once the give-up fired.
	time.Sleep(300 * time.Millisecond)
	if again := len(readMarker(t, marker)); again != count {
		t.Fatalf("loop kept respawning past the budget: %d -> %d", count, again)
	}
}

func TestKanbanSidecarSweepSkipsLiveForeignOwner(t *testing.T) {
	// The ownership posture the sweep depends on: a pid file carrying a
	// FOREIGN instance token names a sidecar a live daemon owns — this
	// boot adopts it observed-not-owned (no second spawn onto the bind)
	// and Stop refuses to signal it. The reap itself stays the exclusive
	// job of StartupOrphanSweep (internal/llm sweep, ppid==1 verified).
	home := t.TempDir()
	t.Setenv(config.EnvMeeptHome, home)
	plantFakeSidecar(t) // resolution seam only — the adoption branch never execs it

	// A live, unrelated long-running process stands in for the foreign
	// daemon's sidecar.
	live := exec.Command("sleep", "60") //nolint:gosec // test fixture
	if err := live.Start(); err != nil {
		t.Fatalf("start stand-in: %v", err)
	}
	t.Cleanup(func() {
		if live.Process != nil {
			_ = live.Process.Kill()
			_, _ = live.Process.Wait()
		}
	})

	pidFile := filepath.Join(home, "run", "meept-kanban.pid")
	if err := os.MkdirAll(filepath.Dir(pidFile), 0o700); err != nil {
		t.Fatalf("mkdir run: %v", err)
	}
	entry := map[string]any{"pid": live.Process.Pid, "token": "deadbeefdeadbeefdeadbeefdeadbeef"}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal pidfile: %v", err)
	}
	if err := os.WriteFile(pidFile, data, 0o600); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}

	sup := newKanbanSupervisor(kanbanSidecarConfig{
		PIDFile:      pidFile,
		SocketPath:   filepath.Join(home, "meept.sock"),
		Bind:         "127.0.0.1:0",
		PollInterval: 50 * time.Millisecond,
	}, slog.Default())
	t.Cleanup(func() { _ = sup.Stop(context.Background()) })

	if err := sup.spawnOnce(context.Background()); err != nil {
		t.Fatalf("adopting spawnOnce: %v", err)
	}
	// No second process: the live foreign pid is still the only one and
	// the supervisor recorded IT as its handle.
	if got := sup.childPID(); got != live.Process.Pid {
		t.Fatalf("supervisor pid = %d, want adopted %d", got, live.Process.Pid)
	}

	// Stop must refuse to kill it (ErrRuntimeNotOwned is swallowed into a
	// "not signalling" log by design — the observable contract is that the
	// process survives every automatic path).
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := sup.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !pidAlive(live.Process.Pid) {
		t.Fatal("supervisor KILLED a foreign-owned live sidecar — sweep-only reaping violated")
	}
}

// StartKanbanSidecarForTest drives the production gate path
// (StartKanbanSidecar) with tightened supervisor timings via the
// kanbanTuning seam, so the tested behavior and the wired behavior cannot
// diverge.
func (d *Daemon) StartKanbanSidecarForTest(t *testing.T, tuning kanbanSidecarConfig) {
	t.Helper()
	d.kanbanTuning = &tuning
	if err := d.StartKanbanSidecar(context.Background()); err != nil {
		t.Fatalf("StartKanbanSidecar: %v", err)
	}
	if d.kanbanSidecar == nil {
		t.Fatal("StartKanbanSidecar enabled but no supervisor was built")
	}
}

// childPID reports the current child's pid under the mutex (test seam).
func (s *kanbanSupervisor) childPID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastPID
}

func atoiPID(t *testing.T, s string) int {
	t.Helper()
	var pid int
	if _, err := fmt.Sscanf(s, "%d", &pid); err != nil {
		t.Fatalf("marker pid %q not an int: %v", s, err)
	}
	return pid
}

func TestKanbanBindAllowLANTurn(t *testing.T) {
	// Pure predicate pinning (C7 posture): loopback/localhost never need
	// -allow-lan; a routable address does; garbage never does (the
	// sidecar's own validation reports it).
	for bind, want := range map[string]bool{
		"127.0.0.1:8790": false,
		"localhost:8790": false,
		"0.0.0.0:8790":   true,
		"192.168.1.5:80": true,
		"not-an-addr":    false,
	} {
		if got := kanbanBindNeedsAllowLAN(bind); got != want {
			t.Errorf("kanbanBindNeedsAllowLAN(%q) = %v, want %v", bind, got, want)
		}
	}
}
