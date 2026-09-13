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
	records := []SpawnRecord{{PIDFile: "/tmp/mlx.pid", Argv: argv, AutoStop: false, PID: 900}}
	lister := func() ([]RuntimeProcInfo, error) {
		return []RuntimeProcInfo{{PID: 900, PPID: 1, Command: cmd}}, nil
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
// the operator deliberately started.
func TestFindOrphanRuntimesWithRecords_OperatorRecordSparesConfigMatch(t *testing.T) {
	argv := []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"}
	cmd := "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081"
	pidFile := "/tmp/mlx-operator.pid"
	cfgs := []*RuntimeConfig{{EndpointKey: "mlx:127.0.0.1:8081", AutoStop: true, PIDFile: pidFile, SpawnCommand: argv}}
	records := []SpawnRecord{{EndpointKey: "mlx:127.0.0.1:8081", PIDFile: pidFile, Argv: argv, AutoStop: false, PID: 900}}
	lister := func() ([]RuntimeProcInfo, error) {
		return []RuntimeProcInfo{{PID: 900, PPID: 1, Command: cmd}}, nil
	}

	got, err := FindOrphanRuntimesWithRecords(cfgs, records, lister)
	if err != nil {
		t.Fatalf("FindOrphanRuntimesWithRecords: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("an operator-started runtime (record auto_stop=false) must survive the config match, got %+v (F58)", got)
	}
}

// TestSweepOrphanRuntimes_LeavesOperatorStartedRuntimeAlone is the end-to-end
// form of F58: the operator's `meept runtime start` runtime (ppid==1, argv
// match, config auto_stop=true, record auto_stop=false) is not reaped.
func TestSweepOrphanRuntimes_LeavesOperatorStartedRuntimeAlone(t *testing.T) {
	mgr := NewRuntimeManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	argv := []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"}
	pidFile := filepath.Join(t.TempDir(), "runtime.pid")
	cfg := &RuntimeConfig{EndpointKey: "mlx:127.0.0.1:8081", AutoStop: true, PIDFile: pidFile, SpawnCommand: argv}
	if err := mgr.RegisterConfig("local-mlx", cfg, "http://127.0.0.1:8081/v1"); err != nil {
		t.Fatalf("RegisterConfig: %v", err)
	}
	records := []SpawnRecord{{EndpointKey: "mlx:127.0.0.1:8081", PIDFile: pidFile, Argv: argv, AutoStop: false, PID: 900}}

	mgr.sweepLister = func() ([]RuntimeProcInfo, error) {
		return []RuntimeProcInfo{{PID: 900, PPID: 1, Command: "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --model /m/x --port 8081"}}, nil
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
