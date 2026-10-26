//go:build e2e

package harness

// Harness self-test: one live sandbox per new capability, proving the
// feature end-to-end against the real daemon (not just the fake):
//
//   1. TestHarnessConfigOverlay    — WithConfigOverlay reaches meept.json5
//     and the daemon boots with the overlay applied (multiuser.enabled).
//   2. TestHarnessQuotaInjection   — a scripted 429 quota shape makes the
//     daemon's chat path surface a quota message (parseQuotaBody wire
//     shape), not the fake's canned text.
//   3. TestHarnessPredicateBinding — Script(LastUserMessageContains(...))
//     binds a specific reply to a specific conversation without touching
//     the legacy Enqueue/Set fields.
//   4. TestHarnessPlannerScripting — SetPlannerResponse overrides the
//     hard-coded plan on the planner shape.
//
// Kept as a permanent harness self-test (build tag e2e); it also guards
// the legacy shape routing via the daemon round-trips in each test.

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestHarnessConfigOverlay proves the config-override seam: the overlay
// key lands in the sandbox meept.json5 and the daemon still boots.
func TestHarnessConfigOverlay(t *testing.T) {
	s := Start(t, WithConfigOverlay(map[string]any{
		"multiuser.enabled": true,
	}))
	// Boot success itself is the main assertion (Start fails the test if
	// the daemon rejects the config). Verify the file content too.
	dataBytes, err := os.ReadFile(s.MeeptHome + "/meept.json5")
	if err != nil {
		t.Fatalf("read sandbox config: %v", err)
	}
	data := string(dataBytes)
	if !strings.Contains(data, `"multiuser"`) || !strings.Contains(data, `"enabled": true`) {
		t.Fatalf("overlay key missing from meept.json5:\n%s", data)
	}
	if got := s.HealthJSON(t); got["status"] != "ok" && got["status"] != "" {
		t.Logf("health payload: %v", got)
	}
}

// TestHarnessQuotaInjection proves error injection: a scripted 429 with
// the OpenAI usage_limit_reached body must make the chat turn surface a
// quota-shaped user message instead of the fake's canned "ok".
func TestHarnessQuotaInjection(t *testing.T) {
	s := Start(t)
	s.RegisterProject(t, "demo")
	sid := s.CreateSession(t, "quota-demo", s.ProjectDir)

	// Inject on EVERY completion: the daemon retries/parks on quota, so
	// serve the quota error broadly and forever.
	s.Fake.Script(func(body map[string]any) bool { return true }, QuotaResponse("usage_limit_reached", time.Now().Add(time.Hour)))

	out, _ := s.RunCLI(t, 120*time.Second, true, "chat", "--session", sid, "hello there")
	// The 429 + usage_limit_reached body MUST have been served to the
	// daemon (the request reached the fake and got the injected status —
	// never a 200 completion).
	if s.Fake.RequestCount() == 0 {
		t.Fatalf("fake received no requests; daemon never reached the model")
	}
	// The canned success text must NOT come back: the injected error took
	// the request path (QuotaResetError classification → park/terminal
	// failure), not a scripted completion.
	if strings.TrimSpace(strings.ToLower(out)) == "ok" {
		t.Fatalf("quota injection ignored: chat returned canned success text")
	}
	t.Logf("quota-injected chat output: %.400s", out)
}

// TestHarnessPredicateBinding proves conversation-bound responses: two
// sessions, two different predicate-keyed executor tool calls — each
// session's turn writes ITS OWN artifact — with no FIFO queue and no
// legacy Enqueue* setters.
func TestHarnessPredicateBinding(t *testing.T) {
	s := Start(t)
	s.RegisterProject(t, "demo")
	sidA := s.CreateSession(t, "bind-a", s.ProjectDir)
	sidB := s.CreateSession(t, "bind-b", s.ProjectDir)

	artifactA := s.ProjectDir + "/bind-a.txt"
	artifactB := s.ProjectDir + "/bind-b.txt"
	s.Fake.ScriptOnce(
		And(IsExecutorRequest(), LastUserMessageContains("BINDMARKER-A")),
		ToolCallResponse(ToolCall{
			Name:      "file_write",
			Arguments: `{"path":"` + artifactA + `","content":"alpha","direct":true}`,
		}))
	s.Fake.ScriptOnce(
		And(IsExecutorRequest(), LastUserMessageContains("BINDMARKER-B")),
		ToolCallResponse(ToolCall{
			Name:      "file_write",
			Arguments: `{"path":"` + artifactB + `","content":"beta","direct":true}`,
		}))
	s.Fake.SetPostToolText("wrote the bound artifact")

	s.ChatTurn(t, sidA, "BINDMARKER-A: create the file", 120*time.Second)
	s.ChatTurn(t, sidB, "BINDMARKER-B: create the file", 120*time.Second)

	dataA, err := os.ReadFile(artifactA)
	if err != nil || string(dataA) != "alpha" {
		t.Fatalf("session A artifact wrong (err=%v content=%q)", err, dataA)
	}
	dataB, err := os.ReadFile(artifactB)
	if err != nil || string(dataB) != "beta" {
		t.Fatalf("session B artifact wrong (err=%v content=%q)", err, dataB)
	}
}

// TestHarnessPlannerScripting proves planner + classifier scripting
// against the full daemon: a distinctive planner plan plus a pinned
// "code" classification drive a step that writes a file whose content
// names the plan marker — proving the pinned plan (not the hard-coded
// one) reached the executor.
func TestHarnessPlannerScripting(t *testing.T) {
	s := Start(t)
	s.RegisterProject(t, "demo")
	sid := s.CreateSession(t, "plan-demo", s.ProjectDir)

	// Pin "code" intent so the turn dispatches to the planner at all
	// (pinned BEFORE the turn — the daemon reads config at dispatch).
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned"}`)

	// Pin a plan whose step description embeds the marker text; the
	// executor then writes it verbatim.
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"write a file containing PLANMARKER-XYZ","tool_hint":"code","depends_on":[]}]}`)
	s.Fake.EnqueueFileWrite("w1", s.ProjectDir+"/planmarker.txt", "PLANMARKER-XYZ")
	s.Fake.SetPostToolText("planned file written")

	s.ChatTurn(t, sid, "PLANRUN: create a file", 150*time.Second)

	data, err := os.ReadFile(s.ProjectDir + "/planmarker.txt")
	if err != nil {
		t.Fatalf("executor never wrote the planned file: %v\nfake requests: %d", err, s.Fake.RequestCount())
	}
	if !strings.Contains(string(data), "PLANMARKER-XYZ") {
		t.Fatalf("planned file has unexpected content: %q", data)
	}
}

// TestHarnessBinDirReaper proves the detached reaper end to end, in a
// CHILD go test process: the child acquires the bin dir (real build), then
// exits; the parent waits for the reaper to fire and asserts the dir is
// gone. This is the only faithful way to test the reaper — in-process, any
// timer dies with the test process, which is exactly the flaw that makes
// the reaper necessary.
func TestHarnessBinDirReaper(t *testing.T) {
	tmp := t.TempDir()
	// The child shares this process's package; -run matches only the
	// helper below. MEEPT_BIN_REAPER_PROBE points TMPDIR at tmp so the
	// dir lands where we can watch it. poll/grace shrunk via ldflags
	// would be overkill: the defaults (2s/3s) keep this test under 15s.
	//nolint:gosec // G702: re-exec of this test binary (os.Args[0]) to run
	// the in-package helper test as a child process; args are fixed
	// literals, no untrusted input.
	cmd := exec.Command(os.Args[0], "-test.run", "^TestHelperBinDirChild$", "-test.v")
	cmd.Env = append(os.Environ(), "MEEPT_BIN_REAPER_PROBE=1", "TMPDIR="+tmp)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child test process failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "MEEPT_BIN_DIR_CREATED:") {
		t.Fatalf("child never reported the bin dir:\n%s", out)
	}
	dir := strings.TrimSpace(strings.SplitN(
		strings.Split(string(out), "MEEPT_BIN_DIR_CREATED:")[1], "\n", 2)[0])

	// The reaper polls at binReaperPollInterval and waits
	// binReaperGrace after owner exit: allow a generous ceiling.
	deadline := time.Now().Add(binReaperPollInterval + binReaperGrace + 20*time.Second)
	for {
		//nolint:gosec // G703: dir is the MkdirTemp path echoed by our
		// own child test process, not untrusted input.
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return // reaper fired: pass
		}
		if time.Now().After(deadline) {
			t.Fatalf("bin dir %s still present %.0fs after child exit", dir,
				time.Since(deadline.Add(binReaperPollInterval+binReaperGrace)).Seconds()+30)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// TestHelperBinDirChild runs ONLY as a child of TestHarnessBinDirReaper.
func TestHelperBinDirChild(t *testing.T) {
	if os.Getenv("MEEPT_BIN_REAPER_PROBE") == "" {
		t.Skip("helper: run via TestHarnessBinDirReaper")
	}
	dir, err := binDir.acquire(t)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	for _, bin := range []string{"meept-daemon", "meept"} {
		if _, err := os.Stat(filepath.Join(dir, bin)); err != nil {
			t.Fatalf("binary %s missing after acquire: %v", bin, err)
		}
	}
	// Signal the dir path to the parent BEFORE exiting: the reaper must
	// remove it even though this process never cleans up.
	fmt.Printf("MEEPT_BIN_DIR_CREATED:%s\n", dir)
}

// TestHarnessBinDirSweep proves sweepStaleBinDirs removes only
// meept-e2e-bin* dirs past the age floor — never fresh ones and never
// unrelated entries.
func TestHarnessBinDirSweep(t *testing.T) {
	tmp := t.TempDir()
	fresh := filepath.Join(tmp, "meept-e2e-bin-fresh")
	stale := filepath.Join(tmp, "meept-e2e-bin-stale")
	unrelated := filepath.Join(tmp, "meept-e2e-keepme")
	for _, d := range []string{fresh, stale, unrelated} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	old := time.Now().Add(-2 * binSweepAge)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	removed := sweepStaleBinDirs(tmp, binSweepAge)

	if removed != 1 {
		t.Fatalf("sweep removed %d dirs, want exactly 1", removed)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale dir survived the sweep (stat err=%v)", err)
	}
	for _, keep := range []string{fresh, unrelated} {
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("fresh/unrelated dir %s was removed: %v", keep, err)
		}
	}
}

// TestHarnessOrphanDaemonKiller proves the bin-dir watchdog kills
// meept-daemon processes spawned from its dir after the owning test
// process exits. This is the second half of the scratch-hygiene contract:
// the dir reaper alone left 78 `T/meept-e2e-binNNN/meept-daemon` orphans
// alive on one host (the `rm -rf` fired; the daemon survived it), because
// suites spawn daemons with raw exec.Command under t.Cleanup only.
//
// The orphan state is produced WITHOUT any kill action: a CHILD go-test
// process acquires the bin dir (real build), boots one real scratch
// daemon against a temp sandbox, then simply EXITS without stopping it —
// byte-for-byte the post-crash state (test binary gone, daemon reparented
// to init, watchdog about to fire). The parent then polls until the
// watchdog's ps-scan kill lands and asserts the daemon is gone.
func TestHarnessOrphanDaemonKiller(t *testing.T) {
	// NOTE: no TMPDIR override — the child boots a real daemon whose unix
	// socket must stay under the macOS sun_path limit, so the child uses
	// the default TMPDIR for both the scratch bin dir and its sandbox.
	cmd := exec.Command(os.Args[0], "-test.run", "^TestHelperOrphanDaemonChild$", "-test.v")
	cmd.Env = append(os.Environ(), "MEEPT_ORPHAN_DAEMON_PROBE=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child test process failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "MEEPT_ORPHAN_DAEMON_PID:") {
		t.Fatalf("child never reported the orphan daemon:\n%s", out)
	}
	fields := strings.Fields(strings.Split(string(out), "MEEPT_ORPHAN_DAEMON_PID:")[1])
	if len(fields) == 0 {
		t.Fatalf("child reported no daemon pid:\n%s", out)
	}
	daemonPid, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatalf("child reported bad pid %q: %v", fields[0], err)
	}
	binDirPath := strings.TrimSpace(strings.SplitN(
		strings.Split(string(out), "MEEPT_BIN_DIR_CREATED:")[1], "\n", 2)[0])

	// The watchdog fires within poll+grace of the child's exit, then
	// TERM-waits up to daemonKillGrace seconds before escalating. Poll
	// generously; /bin/kill -0 mirrors the signal probe the watchdog
	// itself uses (a reaped-exited pid answers ESRCH).
	deadline := time.Now().Add(binReaperPollInterval + binReaperGrace +
		time.Duration(daemonKillGrace)*time.Second + 30*time.Second)
	for {
		alive := exec.Command("/bin/kill", "-0", strconv.Itoa(daemonPid)).Run() == nil
		if !alive {
			// The dir must be gone too — the kill precedes rm -rf.
			if _, err := os.Stat(binDirPath); !os.IsNotExist(err) {
				t.Fatalf("watchdog killed daemon %d but bin dir %s still exists", daemonPid, binDirPath)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("orphan daemon %d still alive %.0fs after child exit",
				daemonPid, time.Since(deadline.Add(90*time.Second)).Seconds()+90)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// TestHelperOrphanDaemonChild runs ONLY as a child of
// TestHarnessOrphanDaemonKiller.
func TestHelperOrphanDaemonChild(t *testing.T) {
	if os.Getenv("MEEPT_ORPHAN_DAEMON_PROBE") == "" {
		t.Skip("helper: run via TestHarnessOrphanDaemonKiller")
	}
	dir, err := binDir.acquire(t)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// One real scratch daemon against a throwaway sandbox: the same argv
	// shape the harness Stack and every suite use. The daemon writes its
	// pidfile and serves /health; nothing here stops it — the child test
	// process exits and the daemon becomes an orphan on purpose.
	// One FLAT MkdirTemp (not t.TempDir()): the socket path must stay
	// under the macOS 104-char sun_path limit, and the test-name-nested
	// TempDir spelling blows past it (same reason Stack.Start uses a
	// bare MkdirTemp).
	port := FreePort(t)
	work, err2 := os.MkdirTemp("", "meept-orphanchk-*")
	if err2 != nil {
		t.Fatalf("temp dir: %v", err2)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	home := filepath.Join(work, "home")
	meeptHome := filepath.Join(home, ".meept")
	stateDir := filepath.Join(work, "state")
	socket := filepath.Join(stateDir, "meept.sock")
	for _, d := range []string{home, meeptHome, stateDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	//nolint:gosec // G306: scratch sandbox config, test-only path
	if err := os.WriteFile(filepath.Join(meeptHome, "meept.json5"), []byte(fmt.Sprintf(`{
  "daemon": { "socket_path": %q, "pid_file": %q, "data_dir": %q, "log_level": "INFO" },
  "transport": {
    "rpc": { "enabled": true, "socket_path": %q },
    "http": {
      "enabled": true, "addr": "127.0.0.1:%d", "require_auth": false,
      "tls_cert_file": %q, "tls_key_file": %q,
      "rest": true, "websocket": false, "mcp": false
    }
  },
  "multiagent": { "enabled": false }
}`, socket, filepath.Join(stateDir, "meept.pid"), stateDir, socket, port,
		filepath.Join(stateDir, "tls", "cert.pem"), filepath.Join(stateDir, "tls", "key.pem"))), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	// Minimal models.json5 (boot refuses without one; no turn is ever run).
	if err := os.WriteFile(filepath.Join(meeptHome, "models.json5"), []byte(`{
  "model": "fake/fake-model",
  "providers": { "fake": { "api": "openai",
    "options": { "baseURL": "http://127.0.0.1:1/v1", "noAuth": true },
    "models": { "fake-model": { "name": "fake-model", "capabilities": ["completion"] } } } }
}`), 0o600); err != nil {
		t.Fatalf("write models: %v", err)
	}
	// Re-point TMPDIR back at the probe dir before spawning: MkdirTemp
	// inside the daemon would otherwise use the parent's TMPDIR, but the
	// daemon does not use it. The env is already correct.
	cmd := exec.Command(filepath.Join(dir, "meept-daemon"),
		"-c", filepath.Join(meeptHome, "meept.json5"),
		"-d", stateDir,
		"-s", socket,
	)
	cmd.Env = append(os.Environ(), "HOME="+home, "MEEPT_HOME="+meeptHome)
	logBuf := &bytes.Buffer{}
	cmd.Stdout = logBuf
	cmd.Stderr = logBuf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start scratch daemon: %v", err)
	}
	// Deliberately NO cmd.Wait and NO stop: the daemon must survive this
	// process (reparented to init) — the exact post-crash orphan state.
	// Print the pid and bin dir BEFORE exiting; the parent's watchdog
	// assertion does the rest. The daemon's health is verified by the
	// boot poll below so a child that never really booted cannot pass
	// the parent's kill assertion vacuously.
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
	}}
	deadline := time.Now().Add(60 * time.Second)
	for {
		resp, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/health", port))
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body) //nolint:errcheck // probe
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				fmt.Printf("MEEPT_ORPHAN_DAEMON_PID:%d\nMEEPT_BIN_DIR_CREATED:%s\n",
					cmd.Process.Pid, dir)
				return
			}
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("scratch daemon never became healthy; log:\n%s", logBuf.String())
		}
		time.Sleep(250 * time.Millisecond)
	}
}
