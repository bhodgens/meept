package llm

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestMatchesSpawnCommand(t *testing.T) {
	cases := []struct {
		name    string
		command string
		spawn   []string
		want    bool
	}{
		{
			name:    "direct argv match",
			command: "/opt/homebrew/bin/llama-server --model /m/y.gguf --port 8080 --host 127.0.0.1",
			spawn:   []string{"llama-server", "--model", "/m/y.gguf", "--port", "8080", "--host", "127.0.0.1"},
			want:    true,
		},
		{
			name:    "interpreter prefix (mlx_lm is a script)",
			command: "/opt/homebrew/Cellar/python@3.12/3.12.13/Frameworks/Python.framework/Versions/3.12/bin/Python /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081",
			spawn:   []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"},
			want:    true,
		},
		{
			name:    "different port is a different endpoint",
			command: "/opt/homebrew/bin/mlx_lm server --model /m/x --port 18081",
			spawn:   []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"},
			want:    false,
		},
		{
			name:    "extra trailing flag is not this spawn",
			command: "/opt/homebrew/bin/mlx_lm server --model /m/x --port 8081 --verbose",
			spawn:   []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"},
			want:    false,
		},
		{
			name:    "unrelated process",
			command: "/usr/sbin/cupsd -l",
			spawn:   []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"},
			want:    false,
		},
		{
			name:    "shorter command line than the spawn",
			command: "/opt/homebrew/bin/mlx_lm server",
			spawn:   []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"},
			want:    false,
		},
		{
			name:    "no spawn command configured",
			command: "anything",
			spawn:   nil,
			want:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesSpawnCommand(tc.command, tc.spawn); got != tc.want {
				t.Errorf("matchesSpawnCommand(%q) = %v, want %v", tc.command, got, tc.want)
			}
		})
	}
}

func TestFindOrphanRuntimes(t *testing.T) {
	mlxSpawn := []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"}
	mlxCmd := "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081"
	llamaSpawn := []string{"llama-server", "--model", "/m/y.gguf", "--port", "8080"}
	llamaCmd := "/opt/homebrew/bin/llama-server --model /m/y.gguf --port 8080"
	keepSpawn := []string{"mlx_lm", "server", "--model", "/m/z", "--port", "8082"}
	keepCmd := "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/z --port 8082"

	cfgs := []*RuntimeConfig{
		{EndpointKey: "mlx:127.0.0.1:8081", AutoStop: true, SpawnCommand: mlxSpawn},
		{EndpointKey: "llama-cpp:127.0.0.1:8080", AutoStop: true, SpawnCommand: llamaSpawn},
		{EndpointKey: "mlx:127.0.0.1:8082", AutoStop: false, SpawnCommand: keepSpawn},
	}

	lister := func() ([]RuntimeProcInfo, error) {
		return []RuntimeProcInfo{
			{PID: 900, PPID: 1, Command: mlxCmd},               // leftover of a dead daemon
			{PID: 901, PPID: 4242, Command: mlxCmd},            // parent alive: owned, keep
			{PID: 902, PPID: 1, Command: llamaCmd},             // leftover of a dead daemon
			{PID: 903, PPID: 1, Command: "/usr/sbin/cupsd -l"}, // not ours
			{PID: 904, PPID: 1, Command: keepCmd},              // auto_stop_on_exit=false: keep
			{PID: 905, PPID: 1, Command: "/opt/homebrew/bin/mlx_lm server"},
		}, nil
	}

	got, err := FindOrphanRuntimes(cfgs, lister)
	if err != nil {
		t.Fatalf("FindOrphanRuntimes: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 orphans, got %d: %+v", len(got), got)
	}
	byPID := map[int]OrphanRuntime{}
	for _, o := range got {
		byPID[o.PID] = o
	}
	if _, ok := byPID[900]; !ok {
		t.Errorf("expected pid 900 (mlx leftover) to be reported, got %+v", got)
	}
	if o, ok := byPID[902]; ok && o.EndpointKey != "llama-cpp:127.0.0.1:8080" {
		t.Errorf("pid 902 reported for endpoint %q", o.EndpointKey)
	}
}

func TestFindOrphanRuntimes_DedupesPidMatchedByTwoConfigs(t *testing.T) {
	spawn := []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"}
	cmd := "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081"
	cfgs := []*RuntimeConfig{
		{EndpointKey: "mlx:127.0.0.1:8081", AutoStop: true, SpawnCommand: spawn},
		{EndpointKey: "mlx:127.0.0.1:8081", AutoStop: true, SpawnCommand: spawn},
	}
	lister := func() ([]RuntimeProcInfo, error) {
		return []RuntimeProcInfo{{PID: 900, PPID: 1, Command: cmd}}, nil
	}
	got, err := FindOrphanRuntimes(cfgs, lister)
	if err != nil {
		t.Fatalf("FindOrphanRuntimes: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("a pid matched by two configs must be reported once, got %+v", got)
	}
}

func TestFindOrphanRuntimes_SkipsWhenScanFails(t *testing.T) {
	failing := func() ([]RuntimeProcInfo, error) {
		return nil, os.ErrPermission
	}
	cfgs := []*RuntimeConfig{{EndpointKey: "mlx:127.0.0.1:8081", AutoStop: true, SpawnCommand: []string{"mlx_lm"}}}
	got, err := FindOrphanRuntimes(cfgs, failing)
	if err == nil {
		t.Fatal("expected the scan error to surface so callers can skip")
	}
	if len(got) != 0 {
		t.Errorf("expected no orphans on a failed scan, got %+v", got)
	}
}

func TestReapRuntimeProcesses_ConfirmsOnlyGonePids(t *testing.T) {
	const cmd = "mlx_lm server --model /m/x --port 8081"
	killed := false
	lister := func() ([]RuntimeProcInfo, error) {
		if killed {
			return nil, nil
		}
		return []RuntimeProcInfo{{PID: 900, PPID: 1, Command: cmd}}, nil
	}
	var signals []syscall.Signal
	signal := func(_ int, sig syscall.Signal) error {
		signals = append(signals, sig)
		if sig == syscall.SIGKILL {
			killed = true
		}
		return nil
	}

	got := ReapRuntimeProcesses([]OrphanRuntime{{PID: 900, Command: cmd}}, 0, lister, signal, nil)
	if len(got) != 1 || got[0] != 900 {
		t.Fatalf("confirmed = %v, want [900]", got)
	}
	if len(signals) != 2 || signals[0] != syscall.SIGTERM || signals[1] != syscall.SIGKILL {
		t.Errorf("signals = %v, want SIGTERM then SIGKILL", signals)
	}
}

func TestReapRuntimeProcesses_SkipsEntryThatChangedBeforeKill(t *testing.T) {
	const cmd = "mlx_lm server --model /m/x --port 8081"
	lister := func() ([]RuntimeProcInfo, error) {
		// First post-SIGTERM scan: the pid now belongs to a different process
		// (reused). The reap must not escalate to SIGKILL.
		return []RuntimeProcInfo{{PID: 900, PPID: 1, Command: "/usr/bin/otherd"}}, nil
	}
	var signals []syscall.Signal
	signal := func(_ int, sig syscall.Signal) error {
		signals = append(signals, sig)
		return nil
	}

	got := ReapRuntimeProcesses([]OrphanRuntime{{PID: 900, Command: cmd}}, 0, lister, signal, nil)
	if len(got) != 0 {
		t.Errorf("a changed pid entry must not be confirmed as reaped, got %v", got)
	}
	for _, sig := range signals {
		if sig == syscall.SIGKILL {
			t.Error("SIGKILL must not be sent to a pid whose entry changed")
		}
	}
}

func TestSweepOrphanRuntimes_ReapsConfirmedOrphanAndClearsPIDFile(t *testing.T) {
	mgr := NewRuntimeManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	pidFile := filepath.Join(t.TempDir(), "runtime.pid")
	// The endpoint's PID file names the leftover: corroboration that this is
	// the runtime meept recorded for the endpoint.
	if err := os.WriteFile(pidFile, []byte(`{"pid":900,"token":"deadbeef"}`), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	cfg := &RuntimeConfig{
		EndpointKey:  "mlx:127.0.0.1:8081",
		AutoStop:     true,
		PIDFile:      pidFile,
		SpawnCommand: []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"},
	}
	if err := mgr.RegisterConfig("local-mlx", cfg, "http://127.0.0.1:8081/v1"); err != nil {
		t.Fatalf("RegisterConfig: %v", err)
	}

	const cmd = "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081"
	killed := false
	mgr.sweepLister = func() ([]RuntimeProcInfo, error) {
		if killed {
			return nil, nil
		}
		return []RuntimeProcInfo{{PID: 900, PPID: 1, Command: cmd}}, nil
	}

	type signal struct {
		pid int
		sig syscall.Signal
	}
	var sent []signal
	mgr.sweepSignal = func(pid int, sig syscall.Signal) error {
		sent = append(sent, signal{pid: pid, sig: sig})
		if sig == syscall.SIGKILL {
			killed = true
		}
		return nil
	}

	reaped := mgr.SweepOrphanRuntimes(0, nil)
	if len(reaped) != 1 || reaped[0] != 900 {
		t.Fatalf("reaped = %v, want [900]", reaped)
	}
	if len(sent) != 2 {
		t.Fatalf("expected SIGTERM then SIGKILL, got %+v", sent)
	}
	if sent[0] != (signal{pid: 900, sig: syscall.SIGTERM}) {
		t.Errorf("first signal = %+v, want SIGTERM to 900", sent[0])
	}
	if sent[1] != (signal{pid: 900, sig: syscall.SIGKILL}) {
		t.Errorf("second signal = %+v, want SIGKILL to 900", sent[1])
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Errorf("the reaped runtime's pid file must be removed, stat err = %v", err)
	}
}

func TestSweepOrphanRuntimes_LeavesEndpointWithLiveOwnerAlone(t *testing.T) {
	mgr := NewRuntimeManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	pidFile := filepath.Join(t.TempDir(), "runtime.pid")
	// The endpoint's PID file names a DIFFERENT, live process: a live owner
	// vetoes the kill, even though a leftover matches the spawn command.
	owner := fmt.Sprintf(`{"pid":%d,"token":"other"}`, os.Getpid())
	if err := os.WriteFile(pidFile, []byte(owner), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	cfg := &RuntimeConfig{
		EndpointKey:  "mlx:127.0.0.1:8081",
		AutoStop:     true,
		PIDFile:      pidFile,
		SpawnCommand: []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"},
	}
	if err := mgr.RegisterConfig("local-mlx", cfg, "http://127.0.0.1:8081/v1"); err != nil {
		t.Fatalf("RegisterConfig: %v", err)
	}
	mgr.sweepLister = func() ([]RuntimeProcInfo, error) {
		return []RuntimeProcInfo{{
			PID:     900,
			PPID:    1,
			Command: "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081",
		}}, nil
	}
	signalled := 0
	mgr.sweepSignal = func(int, syscall.Signal) error {
		signalled++
		return nil
	}

	if got := mgr.SweepOrphanRuntimes(0, nil); len(got) != 0 {
		t.Fatalf("a live owner must veto the reap, got %v", got)
	}
	if signalled != 0 {
		t.Errorf("no signal may reach an endpoint with a live owner, got %d", signalled)
	}
	if _, err := os.Stat(pidFile); err != nil {
		t.Errorf("the live owner's pid file must survive: %v", err)
	}
}

// TestSweepOrphanRuntimes_UnmanagedEndpointVetoesManagedReap pins the DECIDED
// scope of the live-owner veto (audit finding F78, low): configs with
// auto_stop_on_exit=false reach the veto as well, so an operator-owned endpoint
// sharing a spawn command line protects the live process its PID file names —
// a server meept did not start and must not reap. Narrowing the veto to
// auto_stop=true configs would kill that server whenever a managed endpoint
// shares its command line, so the narrowing has to be a conscious decision
// (flip this pin) rather than a silent regression. The sweep reports which
// endpoint vetoed so the operator can act on a genuinely stuck leftover.
func TestSweepOrphanRuntimes_UnmanagedEndpointVetoesManagedReap(t *testing.T) {
	mgr := NewRuntimeManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	dir := t.TempDir()
	argv := []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"}
	managedPidFile := filepath.Join(dir, "managed.pid")
	unmanagedPidFile := filepath.Join(dir, "unmanaged.pid")

	// The unmanaged endpoint's PID file names a DIFFERENT, live process: a
	// hand-started server on the same command line.
	owner := fmt.Sprintf(`{"pid":%d,"token":"other"}`, os.Getpid())
	if err := os.WriteFile(unmanagedPidFile, []byte(owner), 0o600); err != nil {
		t.Fatalf("write owner pid file: %v", err)
	}
	managed := &RuntimeConfig{EndpointKey: "mlx:127.0.0.1:8081", AutoStop: true, PIDFile: managedPidFile, SpawnCommand: argv}
	unmanaged := &RuntimeConfig{EndpointKey: "mlx:localhost:8081", AutoStop: false, PIDFile: unmanagedPidFile, SpawnCommand: argv}
	if err := mgr.RegisterConfig("local-mlx", managed, "http://127.0.0.1:8081/v1"); err != nil {
		t.Fatalf("RegisterConfig(managed): %v", err)
	}
	if err := mgr.RegisterConfig("hand-started-mlx", unmanaged, "http://localhost:8081/v1"); err != nil {
		t.Fatalf("RegisterConfig(unmanaged): %v", err)
	}
	mgr.sweepLister = func() ([]RuntimeProcInfo, error) {
		return []RuntimeProcInfo{{
			PID:     1904,
			PPID:    1,
			Command: "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081",
		}}, nil
	}
	signalled := 0
	mgr.sweepSignal = func(int, syscall.Signal) error {
		signalled++
		return nil
	}

	if got := mgr.SweepOrphanRuntimes(0, nil); len(got) != 0 {
		t.Fatalf("an unmanaged endpoint's live owner must veto the reap, got %v (F78 scope)", got)
	}
	if signalled != 0 {
		t.Errorf("no signal may reach a process an unmanaged endpoint records as live, got %d (F78 scope)", signalled)
	}
}

// TestFindOrphanRuntimes_RealReparentedProcess drives the real ps scan against a
// real process: a program whose parent exits immediately is re-parented to init,
// which is exactly the shape a runtime left behind by a dead daemon has. The
// reap decision for that shape is covered by the fake-lister tests above.
func TestFindOrphanRuntimes_RealReparentedProcess(t *testing.T) {
	// nohup execs its target in place, so the surviving process command line is
	// exactly "sleep 300" and the shell that launched it is gone.
	launcher := exec.Command("/bin/sh", "-c", "nohup sleep 300 >/dev/null 2>&1 &")
	if err := launcher.Run(); err != nil {
		t.Fatalf("launch re-parented process: %v", err)
	}

	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		procs, err := ListRuntimeProcesses()
		if err != nil {
			t.Fatalf("ListRuntimeProcesses: %v", err)
		}
		for _, p := range procs {
			if p.PPID == 1 && matchesSpawnCommand(p.Command, []string{"sleep", "300"}) {
				pid = p.PID
				break
			}
		}
		if pid != 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pid == 0 {
		t.Skip("no re-parented process observed in this environment")
	}
	defer func() {
		if killErr := syscall.Kill(pid, syscall.SIGKILL); killErr != nil {
			t.Logf("cleanup kill %d: %v", pid, killErr)
		}
	}()

	cfg := &RuntimeConfig{
		EndpointKey:  "test:127.0.0.1:1",
		AutoStop:     true,
		SpawnCommand: []string{"sleep", "300"},
	}
	orphans, err := FindOrphanRuntimes([]*RuntimeConfig{cfg}, ListRuntimeProcesses)
	if err != nil {
		t.Fatalf("FindOrphanRuntimes: %v", err)
	}
	for _, o := range orphans {
		if o.PID == pid {
			return
		}
	}
	t.Errorf("re-parented process %d (sleep 300) was not reported as an orphan: %+v", pid, orphans)
}

// --- durable spawn records ---

// liveSpawnPID starts a real process and returns its pid; the process is killed
// and reaped at cleanup. The sweep's operator-record liveness rule (audit
// finding F78) signals 0 to the RECORDED pid, so a record that must count as
// "still live" needs a real process behind it — a hardcoded pid would be
// somebody else's process, or nothing at all.
func liveSpawnPID(t *testing.T, arg ...string) int {
	t.Helper()
	cmd := exec.Command(arg[0], arg[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn live pid holder %v: %v", arg, err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait() // reap so the pid is fully gone after the test
	})
	return cmd.Process.Pid
}

// deadSpawnPID returns a pid that is verifiably gone: a real process is spawned
// and reaped, so the pid is released before it is used. Skips on the (rare)
// environment where the reaped pid still signals alive.
func deadSpawnPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn true: %v", err)
	}
	pid := cmd.Process.Pid
	_, _ = cmd.Process.Wait()
	if processAlive(pid) {
		t.Skip("reaped pid still reports alive; environment reaps oddly")
	}
	return pid
}

// TestFindOrphanRuntimesWithRecords_MatchesRecordWithoutConfig is the finding
// this whole mechanism exists for: no endpoint config reaches the scan (its
// model volume is gone, its provider renamed), so only the durable record can
// identify the leftover.
func TestFindOrphanRuntimesWithRecords_MatchesRecordWithoutConfig(t *testing.T) {
	argv := []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"}
	cmd := "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081"
	records := []SpawnRecord{{
		EndpointKey: "mlx:127.0.0.1:8081",
		PIDFile:     "/tmp/mlx.pid",
		Argv:        argv,
		AutoStop:    true,
		PID:         900,
	}}
	lister := func() ([]RuntimeProcInfo, error) {
		return []RuntimeProcInfo{
			{PID: 900, PPID: 1, Command: cmd},               // leftover of a dead daemon
			{PID: 901, PPID: 4242, Command: cmd},            // parent alive: owned, keep
			{PID: 903, PPID: 1, Command: "/usr/sbin/cupsd"}, // not ours
		}, nil
	}

	got, err := FindOrphanRuntimesWithRecords(nil, records, lister)
	if err != nil {
		t.Fatalf("FindOrphanRuntimesWithRecords: %v", err)
	}
	if len(got) != 1 || got[0].PID != 900 {
		t.Fatalf("expected only pid 900 from a record-only match, got %+v", got)
	}
	if got[0].EndpointKey != "mlx:127.0.0.1:8081" {
		t.Errorf("endpoint key = %q, want the record's key", got[0].EndpointKey)
	}
}

func TestFindOrphanRuntimesWithRecords_SkipsRecordWithAutoStopFalse(t *testing.T) {
	argv := []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"}
	cmd := "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081"
	// The recorded pid is LIVE: an operator-started runtime the record still
	// speaks for (finding F78 — a record whose pid is gone no longer spares).
	live := liveSpawnPID(t, "sleep", "300")
	records := []SpawnRecord{{PIDFile: "/tmp/mlx.pid", Argv: argv, AutoStop: false, PID: live}}
	lister := func() ([]RuntimeProcInfo, error) {
		return []RuntimeProcInfo{{PID: live, PPID: 1, Command: cmd}}, nil
	}

	got, err := FindOrphanRuntimesWithRecords(nil, records, lister)
	if err != nil {
		t.Fatalf("FindOrphanRuntimesWithRecords: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a record with auto_stop=false must not be reaped, got %+v", got)
	}
}

func TestFindOrphanRuntimesWithRecords_DedupesPidMatchedByConfigAndRecord(t *testing.T) {
	spawn := []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"}
	cmd := "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081"
	cfgs := []*RuntimeConfig{{EndpointKey: "mlx:127.0.0.1:8081", AutoStop: true, SpawnCommand: spawn}}
	records := []SpawnRecord{{
		EndpointKey: "mlx:127.0.0.1:8081",
		PIDFile:     "/tmp/mlx.pid",
		Argv:        spawn,
		AutoStop:    true,
		PID:         900,
	}}
	lister := func() ([]RuntimeProcInfo, error) {
		return []RuntimeProcInfo{{PID: 900, PPID: 1, Command: cmd}}, nil
	}

	got, err := FindOrphanRuntimesWithRecords(cfgs, records, lister)
	if err != nil {
		t.Fatalf("FindOrphanRuntimesWithRecords: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("a pid matched by a config and a record must be reported once, got %+v", got)
	}
}

// TestSweepOrphanRuntimes_ReapsRecordOnlyLeftover proves the leftover is reaped
// end to end with no config registered: the record alone drives the sweep.
func TestSweepOrphanRuntimes_ReapsRecordOnlyLeftover(t *testing.T) {
	mgr := NewRuntimeManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	records := []SpawnRecord{{
		EndpointKey: "mlx:127.0.0.1:8081",
		PIDFile:     filepath.Join(t.TempDir(), "runtime.pid"),
		Argv:        []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"},
		AutoStop:    true,
		PID:         900,
	}}
	const cmd = "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081"
	killed := false
	mgr.sweepLister = func() ([]RuntimeProcInfo, error) {
		if killed {
			return nil, nil
		}
		return []RuntimeProcInfo{{PID: 900, PPID: 1, Command: cmd}}, nil
	}
	var signals []syscall.Signal
	mgr.sweepSignal = func(_ int, sig syscall.Signal) error {
		signals = append(signals, sig)
		if sig == syscall.SIGKILL {
			killed = true
		}
		return nil
	}

	reaped := mgr.SweepOrphanRuntimes(0, records)
	if len(reaped) != 1 || reaped[0] != 900 {
		t.Fatalf("reaped = %v, want [900]", reaped)
	}
	if len(signals) != 2 || signals[0] != syscall.SIGTERM || signals[1] != syscall.SIGKILL {
		t.Errorf("signals = %v, want SIGTERM then SIGKILL", signals)
	}
}

func TestSweepOrphanRuntimes_LeavesRecordWithAutoStopFalseAlone(t *testing.T) {
	mgr := NewRuntimeManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	records := []SpawnRecord{{
		EndpointKey: "mlx:127.0.0.1:8081",
		PIDFile:     filepath.Join(t.TempDir(), "runtime.pid"),
		Argv:        []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"},
		AutoStop:    false,
		PID:         900,
	}}
	mgr.sweepLister = func() ([]RuntimeProcInfo, error) {
		return []RuntimeProcInfo{{
			PID:     900,
			PPID:    1,
			Command: "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081",
		}}, nil
	}
	signalled := 0
	mgr.sweepSignal = func(int, syscall.Signal) error {
		signalled++
		return nil
	}

	if got := mgr.SweepOrphanRuntimes(0, records); len(got) != 0 {
		t.Fatalf("a record with auto_stop=false must not be reaped, got %v", got)
	}
	if signalled != 0 {
		t.Errorf("no signal may reach an auto_stop=false runtime, got %d", signalled)
	}
}

// --- audit findings F57 / F58 / F61 pins ---

// TestSweepOrphanRuntimes_RemovesSpawnRecordOnReap pins audit finding F57: the
// reap removes the durable spawn record too, so records do not accumulate and a
// stale one cannot later outvote an explicit auto_stop_on_exit:false.
func TestSweepOrphanRuntimes_RemovesSpawnRecordOnReap(t *testing.T) {
	mgr := NewRuntimeManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	pidFile := filepath.Join(t.TempDir(), "runtime.pid")
	if err := os.WriteFile(pidFile, []byte(`{"pid":900,"token":"deadbeef"}`), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
	if err := WriteSpawnRecord(SpawnRecord{
		EndpointKey: "mlx:127.0.0.1:8081",
		PIDFile:     pidFile,
		Argv:        []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"},
		AutoStop:    true,
		PID:         900,
	}); err != nil {
		t.Fatalf("write spawn record: %v", err)
	}

	cfg := &RuntimeConfig{
		EndpointKey:  "mlx:127.0.0.1:8081",
		AutoStop:     true,
		PIDFile:      pidFile,
		SpawnCommand: []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"},
	}
	if err := mgr.RegisterConfig("local-mlx", cfg, "http://127.0.0.1:8081/v1"); err != nil {
		t.Fatalf("RegisterConfig: %v", err)
	}

	killed := false
	mgr.sweepLister = func() ([]RuntimeProcInfo, error) {
		if killed {
			return nil, nil
		}
		return []RuntimeProcInfo{{
			PID:     900,
			PPID:    1,
			Command: "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081",
		}}, nil
	}
	mgr.sweepSignal = func(_ int, sig syscall.Signal) error {
		if sig == syscall.SIGKILL {
			killed = true
		}
		return nil
	}

	if reaped := mgr.SweepOrphanRuntimes(0, nil); len(reaped) != 1 || reaped[0] != 900 {
		t.Fatalf("reaped = %v, want [900]", reaped)
	}
	if _, err := os.Stat(SpawnRecordPath(pidFile)); !os.IsNotExist(err) {
		t.Errorf("the reaped runtime's spawn record must be removed, stat err = %v (F57)", err)
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Errorf("the reaped runtime's pid file must be removed, stat err = %v", err)
	}
}

// TestFindOrphanRuntimesWithRecords_ConfigAutoStopFalseOutvotesStaleRecord pins
// audit finding F57: a STALE record with AutoStop=true must not reap a leftover
// when the endpoint's CURRENT config says auto_stop_on_exit=false.
func TestFindOrphanRuntimesWithRecords_ConfigAutoStopFalseOutvotesStaleRecord(t *testing.T) {
	argv := []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"}
	cmd := "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081"
	pidFile := "/tmp/mlx-optout.pid"
	cfgs := []*RuntimeConfig{{EndpointKey: "mlx:127.0.0.1:8081", AutoStop: false, PIDFile: pidFile, SpawnCommand: argv}}
	records := []SpawnRecord{{EndpointKey: "mlx:127.0.0.1:8081", PIDFile: pidFile, Argv: argv, AutoStop: true, PID: 900}}
	lister := func() ([]RuntimeProcInfo, error) {
		return []RuntimeProcInfo{{PID: 900, PPID: 1, Command: cmd}}, nil
	}

	got, err := FindOrphanRuntimesWithRecords(cfgs, records, lister)
	if err != nil {
		t.Fatalf("FindOrphanRuntimesWithRecords: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a stale record must not outvote config auto_stop_on_exit=false, got %+v (F57)", got)
	}
}

// TestFindOrphanRuntimesWithRecords_OperatorRecordSparesConfigMatch pins audit
// finding F58: a record with AutoStop=false (an out-of-daemon `meept runtime
// start`, whose record the CLI writes with AutoStop=false) spares the SAME
// endpoint from the config match too — the boot sweep must not reap a runtime
// the operator deliberately started. The record's pid must be LIVE for the
// sparing to stand (finding F78).
func TestFindOrphanRuntimesWithRecords_OperatorRecordSparesConfigMatch(t *testing.T) {
	argv := []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"}
	cmd := "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081"
	pidFile := "/tmp/mlx-operator.pid"
	live := liveSpawnPID(t, "sleep", "300")
	cfgs := []*RuntimeConfig{{EndpointKey: "mlx:127.0.0.1:8081", AutoStop: true, PIDFile: pidFile, SpawnCommand: argv}}
	records := []SpawnRecord{{EndpointKey: "mlx:127.0.0.1:8081", PIDFile: pidFile, Argv: argv, AutoStop: false, PID: live}}
	lister := func() ([]RuntimeProcInfo, error) {
		return []RuntimeProcInfo{{PID: live, PPID: 1, Command: cmd}}, nil
	}

	got, err := FindOrphanRuntimesWithRecords(cfgs, records, lister)
	if err != nil {
		t.Fatalf("FindOrphanRuntimesWithRecords: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("an operator-started runtime (record auto_stop=false) must survive the config match, got %+v (F58)", got)
	}
}

// TestFindOrphanRuntimesWithRecords_StaleOperatorRecordDoesNotSpare pins audit
// finding F78: a record whose auto_stop=false runtime is GONE must not spare the
// endpoint any more. The operator ran `meept runtime start`, that runtime died
// (SIGKILL, a reboot) and nothing removed the record, so every later boot spared
// the endpoint — the genuine leftover of an earlier generation survived, held
// the endpoint port, and the daemon's own spawn was refused by the
// duplicate-spawn pre-check: the install ended up with no runtime at all.
func TestFindOrphanRuntimesWithRecords_StaleOperatorRecordDoesNotSpare(t *testing.T) {
	argv := []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"}
	cmd := "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081"
	pidFile := "/tmp/mlx-stale-operator.pid"
	cfgs := []*RuntimeConfig{{EndpointKey: "mlx:127.0.0.1:8081", AutoStop: true, PIDFile: pidFile, SpawnCommand: argv}}
	records := []SpawnRecord{{
		EndpointKey: "mlx:127.0.0.1:8081",
		PIDFile:     pidFile,
		Argv:        argv,
		AutoStop:    false,
		PID:         deadSpawnPID(t), // the operator's runtime is gone
	}}
	lister := func() ([]RuntimeProcInfo, error) {
		return []RuntimeProcInfo{{PID: 1900, PPID: 1, Command: cmd}}, nil
	}

	got, err := FindOrphanRuntimesWithRecords(cfgs, records, lister)
	if err != nil {
		t.Fatalf("FindOrphanRuntimesWithRecords: %v", err)
	}
	if len(got) != 1 || got[0].PID != 1900 {
		t.Fatalf("a record whose recorded pid is gone must not spare the endpoint, got %+v (F78)", got)
	}
}

// TestSweepOrphanRuntimes_LeavesOperatorStartedRuntimeAlone is the end-to-end
// form of F58: the operator's `meept runtime start` runtime (ppid==1, argv
// match, config auto_stop=true, record auto_stop=false) is not reaped. The
// record's pid is the live runtime's own pid — the state the record describes
// while the operator's runtime is up (finding F78 makes liveness load-bearing).
func TestSweepOrphanRuntimes_LeavesOperatorStartedRuntimeAlone(t *testing.T) {
	mgr := NewRuntimeManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	argv := []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"}
	pidFile := filepath.Join(t.TempDir(), "runtime.pid")
	cfg := &RuntimeConfig{EndpointKey: "mlx:127.0.0.1:8081", AutoStop: true, PIDFile: pidFile, SpawnCommand: argv}
	if err := mgr.RegisterConfig("local-mlx", cfg, "http://127.0.0.1:8081/v1"); err != nil {
		t.Fatalf("RegisterConfig: %v", err)
	}
	live := liveSpawnPID(t, "sleep", "300")
	records := []SpawnRecord{{EndpointKey: "mlx:127.0.0.1:8081", PIDFile: pidFile, Argv: argv, AutoStop: false, PID: live}}

	mgr.sweepLister = func() ([]RuntimeProcInfo, error) {
		return []RuntimeProcInfo{{PID: live, PPID: 1, Command: "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081"}}, nil
	}
	signalled := 0
	mgr.sweepSignal = func(int, syscall.Signal) error {
		signalled++
		return nil
	}

	if got := mgr.SweepOrphanRuntimes(0, records); len(got) != 0 {
		t.Fatalf("an operator-started runtime must not be reaped, got %v (F58)", got)
	}
	if signalled != 0 {
		t.Errorf("no signal may reach an operator-started runtime, got %d (F58)", signalled)
	}
}

// TestSweepOrphanRuntimes_ReapsLeftoverAfterStaleOperatorRecord pins both halves
// of audit finding F78 end to end: an operator record whose recorded runtime is
// GONE (a finished `meept runtime start`, a SIGKILLed runtime, a reboot) loses
// its spare, the genuine leftover of an earlier generation IS reaped, and the
// dead record file is pruned from disk instead of sparing every later boot.
func TestSweepOrphanRuntimes_ReapsLeftoverAfterStaleOperatorRecord(t *testing.T) {
	mgr := NewRuntimeManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	argv := []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "runtime.pid")
	// The stale operator record on disk: auto_stop=false, pid already gone.
	if err := WriteSpawnRecord(SpawnRecord{
		EndpointKey: "mlx:127.0.0.1:8081",
		PIDFile:     pidFile,
		Argv:        argv,
		AutoStop:    false,
		PID:         deadSpawnPID(t),
	}); err != nil {
		t.Fatalf("write spawn record: %v", err)
	}
	cfg := &RuntimeConfig{EndpointKey: "mlx:127.0.0.1:8081", AutoStop: true, PIDFile: pidFile, SpawnCommand: argv}
	if err := mgr.RegisterConfig("local-mlx", cfg, "http://127.0.0.1:8081/v1"); err != nil {
		t.Fatalf("RegisterConfig: %v", err)
	}

	const cmd = "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081"
	killed := false
	mgr.sweepLister = func() ([]RuntimeProcInfo, error) {
		if killed {
			return nil, nil
		}
		return []RuntimeProcInfo{{PID: 1901, PPID: 1, Command: cmd}}, nil
	}
	var signals []syscall.Signal
	mgr.sweepSignal = func(_ int, sig syscall.Signal) error {
		signals = append(signals, sig)
		if sig == syscall.SIGKILL {
			killed = true
		}
		return nil
	}

	records := []SpawnRecord{{
		EndpointKey: "mlx:127.0.0.1:8081",
		PIDFile:     pidFile,
		Argv:        argv,
		AutoStop:    false,
		PID:         deadSpawnPID(t),
	}}
	reaped := mgr.SweepOrphanRuntimes(0, records)
	if len(reaped) != 1 || reaped[0] != 1901 {
		t.Fatalf("reaped = %v, want [1901] — the stale record must not spare the leftover (F78)", reaped)
	}
	if len(signals) == 0 || signals[0] != syscall.SIGTERM {
		t.Fatalf("signals = %v, want a SIGTERM first", signals)
	}
	if _, err := os.Stat(SpawnRecordPath(pidFile)); !os.IsNotExist(err) {
		t.Errorf("the stale operator record must be pruned, stat err = %v (F78)", err)
	}
}

// TestPruneStaleOperatorRecords_KeepsLiveOperatorRecord is the other direction
// of the prune: a record whose operator runtime is still alive — and every
// auto_stop=true record, which is sweepable MATCHING evidence even after its pid
// exits — survives on disk.
func TestPruneStaleOperatorRecords_KeepsLiveOperatorRecord(t *testing.T) {
	dir := t.TempDir()
	livePidFile := filepath.Join(dir, "live.pid")
	managedPidFile := filepath.Join(dir, "managed.pid")
	deadRecordPid := deadSpawnPID(t)

	for _, rec := range []SpawnRecord{
		{PIDFile: livePidFile, Argv: []string{"mlx_lm", "server"}, AutoStop: false, PID: liveSpawnPID(t, "sleep", "300")},
		{PIDFile: managedPidFile, Argv: []string{"mlx_lm", "server"}, AutoStop: true, PID: deadRecordPid},
	} {
		if err := WriteSpawnRecord(rec); err != nil {
			t.Fatalf("write spawn record: %v", err)
		}
	}

	kept := PruneStaleOperatorRecords([]SpawnRecord{
		{PIDFile: livePidFile, Argv: []string{"mlx_lm", "server"}, AutoStop: false, PID: liveSpawnPID(t, "sleep", "300")},
		{PIDFile: managedPidFile, Argv: []string{"mlx_lm", "server"}, AutoStop: true, PID: deadRecordPid},
	})
	if len(kept) != 2 {
		t.Fatalf("kept %d records, want both (a live operator record and a managed record) (F78)", len(kept))
	}
	for _, pidFile := range []string{livePidFile, managedPidFile} {
		if _, err := os.Stat(SpawnRecordPath(pidFile)); err != nil {
			t.Errorf("record %s must survive: %v", SpawnRecordPath(pidFile), err)
		}
	}
}

// TestRemoveRuntimeHandlesForPids_RemovesOnlyConfirmedPids pins the reap-path
// cleanup that arms audit finding F78: a reaped runtime's PID file AND durable
// record are removed (the CLI/doctor reap path used to remove neither, so the
// record kept sparing the endpoint on every later boot), while a handle naming a
// different pid is untouched — a concurrent Start may have rewritten it for a
// fresh runtime.
func TestRemoveRuntimeHandlesForPids_RemovesOnlyConfirmedPids(t *testing.T) {
	dir := t.TempDir()
	gonePidFile := filepath.Join(dir, "gone.pid")
	keptPidFile := filepath.Join(dir, "kept.pid")
	gonePid := deadSpawnPID(t)
	keptPid := deadSpawnPID(t)

	handles := []SpawnRecord{
		{PIDFile: gonePidFile, Argv: []string{"sleep", "300"}, AutoStop: true, PID: gonePid},
		{PIDFile: keptPidFile, Argv: []string{"sleep", "300"}, AutoStop: true, PID: keptPid},
	}
	for _, h := range handles {
		if err := os.WriteFile(h.PIDFile, []byte(fmt.Sprintf(`{"pid":%d,"token":"deadbeef"}`, h.PID)), 0o600); err != nil {
			t.Fatalf("write pid file: %v", err)
		}
		if err := WriteSpawnRecord(h); err != nil {
			t.Fatalf("write spawn record: %v", err)
		}
	}

	RemoveRuntimeHandlesForPids(nil, handles, []int{gonePid})

	if _, err := os.Stat(gonePidFile); !os.IsNotExist(err) {
		t.Errorf("the reaped pid's PID file must be removed, stat err = %v (F78)", err)
	}
	if _, err := ReadSpawnRecord(gonePidFile); err == nil {
		t.Error("the reaped pid's durable record must be removed (F78)")
	}
	if _, err := os.Stat(keptPidFile); err != nil {
		t.Errorf("a handle naming another pid must be left alone: %v", err)
	}
	if _, err := ReadSpawnRecord(keptPidFile); err != nil {
		t.Errorf("a record naming another pid must be left alone: %v", err)
	}
}

// TestReapOrphanRuntimes_RemovesHandlesOfConfirmedPids pins the WIRING of the
// reap-path handle cleanup (audit finding F78): the CLI reaper and
// `meept doctor --fix` call this function, and before the fix a reaped
// runtime's PID file and durable record both survived — the record then spared
// the endpoint on every later boot and the daemon's own spawn was refused.
func TestReapOrphanRuntimes_RemovesHandlesOfConfirmedPids(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "runtime.pid")
	argv := []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"}
	const cmd = "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081"

	if err := os.WriteFile(pidFile, []byte(`{"pid":1903,"token":"deadbeef"}`), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
	records := []SpawnRecord{{
		EndpointKey: "mlx:127.0.0.1:8081",
		PIDFile:     pidFile,
		Argv:        argv,
		AutoStop:    true,
		PID:         1903,
	}}
	if err := WriteSpawnRecord(records[0]); err != nil {
		t.Fatalf("write spawn record: %v", err)
	}
	cfgs := []*RuntimeConfig{{EndpointKey: "mlx:127.0.0.1:8081", AutoStop: true, PIDFile: pidFile, SpawnCommand: argv}}

	killed := false
	lister := func() ([]RuntimeProcInfo, error) {
		if killed {
			return nil, nil
		}
		return []RuntimeProcInfo{{PID: 1903, PPID: 1, Command: cmd}}, nil
	}
	signal := func(_ int, sig syscall.Signal) error {
		if sig == syscall.SIGKILL {
			killed = true
		}
		return nil
	}

	orphans, confirmed := reapOrphanRuntimes(cfgs, records, 0, lister, signal, nil)
	if len(orphans) != 1 || orphans[0].PID != 1903 {
		t.Fatalf("orphans = %+v, want the record's leftover", orphans)
	}
	if len(confirmed) != 1 || confirmed[0] != 1903 {
		t.Fatalf("confirmed = %v, want [1903]", confirmed)
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Errorf("the reaped runtime's PID file must be removed, stat err = %v (F78)", err)
	}
	if _, err := ReadSpawnRecord(pidFile); err == nil {
		t.Error("the reaped runtime's durable record must be removed (F78)")
	}
}

// TestFilterLiveOwned_DropsTargetWithLiveOwner pins audit finding F61: the
// doctor reap path and the daemon sweep share ONE live-owner predicate, so
// `meept doctor --fix` cannot signal a pid an endpoint records a different live
// owner for.
func TestFilterLiveOwned_DropsTargetWithLiveOwner(t *testing.T) {
	command := "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081"
	argv := []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"}

	// The endpoint's PID file names a DIFFERENT, live owner (this test process).
	ownerFile := filepath.Join(t.TempDir(), "owner.pid")
	if err := os.WriteFile(ownerFile, []byte(fmt.Sprintf(`{"pid":%d,"token":"other"}`, os.Getpid())), 0o600); err != nil {
		t.Fatalf("write owner pid file: %v", err)
	}
	cfgs := []*RuntimeConfig{{EndpointKey: "mlx:127.0.0.1:8081", AutoStop: true, PIDFile: ownerFile, SpawnCommand: argv}}

	if RuntimeHasLiveOwner(cfgs, command, 900) != true {
		t.Fatal("RuntimeHasLiveOwner must report the recorded live owner (F61)")
	}
	dropped := FilterLiveOwned(cfgs, []OrphanRuntime{{EndpointKey: "mlx:127.0.0.1:8081", PID: 900, Command: command}})
	if len(dropped) != 0 {
		t.Fatalf("FilterLiveOwned must drop a target with a recorded live owner, kept %+v (F61)", dropped)
	}

	// Control: no live owner recorded -> the target survives the filter.
	emptyFile := filepath.Join(t.TempDir(), "empty.pid")
	cfgs2 := []*RuntimeConfig{{EndpointKey: "mlx:127.0.0.1:8081", AutoStop: true, PIDFile: emptyFile, SpawnCommand: argv}}
	if RuntimeHasLiveOwner(cfgs2, command, 900) {
		t.Fatal("RuntimeHasLiveOwner must be false when no live owner is recorded")
	}
	kept := FilterLiveOwned(cfgs2, []OrphanRuntime{{EndpointKey: "mlx:127.0.0.1:8081", PID: 900, Command: command}})
	if len(kept) != 1 {
		t.Fatalf("FilterLiveOwned must keep a target with no live owner, got %+v", kept)
	}
}

// containsPID reports whether list names pid.
func containsPID(list []OrphanRuntime, pid int) bool {
	for _, o := range list {
		if o.PID == pid {
			return true
		}
	}
	return false
}

// TestOrphanRuntimesFromConfigsAndRecords_AppliesLiveOwnerVeto is the WIRING
// form of the F61 pin: the doctor entry point (OrphanRuntimesFromConfigsAndRecords,
// used by BOTH the report path and --fix) must drop a matched leftover when an
// endpoint config records a different live owner for it — exactly what the
// daemon's sweep does. Uses a real re-parented process; skips when the
// environment does not produce one.
func TestOrphanRuntimesFromConfigsAndRecords_AppliesLiveOwnerVeto(t *testing.T) {
	launcher := exec.Command("/bin/sh", "-c", "nohup sleep 300 >/dev/null 2>&1 &")
	if err := launcher.Run(); err != nil {
		t.Fatalf("launch re-parented process: %v", err)
	}

	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		procs, err := ListRuntimeProcesses()
		if err != nil {
			t.Fatalf("ListRuntimeProcesses: %v", err)
		}
		for _, p := range procs {
			if p.PPID == 1 && matchesSpawnCommand(p.Command, []string{"sleep", "300"}) {
				pid = p.PID
				break
			}
		}
		if pid != 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pid == 0 {
		t.Skip("no re-parented process observed in this environment")
	}
	defer func() {
		if killErr := syscall.Kill(pid, syscall.SIGKILL); killErr != nil {
			t.Logf("cleanup kill %d: %v", pid, killErr)
		}
	}()

	// The endpoint's PID file names a DIFFERENT, live owner (this process), so
	// the leftover must be vetoed.
	ownerFile := filepath.Join(t.TempDir(), "owner.pid")
	if err := os.WriteFile(ownerFile, []byte(fmt.Sprintf(`{"pid":%d,"token":"other"}`, os.Getpid())), 0o600); err != nil {
		t.Fatalf("write owner pid file: %v", err)
	}
	cfgs := []*RuntimeConfig{{EndpointKey: "t:127.0.0.1:1", AutoStop: true, PIDFile: ownerFile, SpawnCommand: []string{"sleep", "300"}}}

	raw, err := FindOrphanRuntimesWithRecords(cfgs, nil, ListRuntimeProcesses)
	if err != nil {
		t.Fatalf("FindOrphanRuntimesWithRecords: %v", err)
	}
	if !containsPID(raw, pid) {
		t.Fatalf("test precondition: expected the re-parented process %d in the raw detection: %+v", pid, raw)
	}

	got, err := OrphanRuntimesFromConfigsAndRecords(cfgs, nil)
	if err != nil {
		t.Fatalf("OrphanRuntimesFromConfigsAndRecords: %v", err)
	}
	if containsPID(got, pid) {
		t.Fatalf("the doctor entry point must apply the live-owner veto, reported %+v (F61)", got)
	}
}
