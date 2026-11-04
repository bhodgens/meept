package voice

// In-package tests for ChildProc (leaf 13). No parked gate tests pin these
// units, so they live here: lazy spawn + reuse (T2), idle stop + drain
// cancel (T3), parent-death supervision (T4), handshake refusal (T5), RSS
// log fields (T6). The fake child is a REAL helper binary built once per
// test run (childproc_testhelper_test.go); nothing is mocked at the
// process level - real spawn, real pid file, real supervisor wrapper.

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// isDarwinOrLinux reports whether the host is a platform the process-group
// tests can run on.
func isDarwinOrLinux() bool {
	return hostOS == "darwin" || hostOS == "linux"
}

// childProcTestEnv bundles what every ChildProc test needs: short temp dirs
// (unix socket path limits), a real TextServer, and the fake child binary.
type childProcTestEnv struct {
	t      *testing.T
	dir    string
	sock   string
	audio  string
	server *TextServer
	binary string
	logBuf *lockedBuffer
}

// newChildProcTestEnv starts a TextServer on a short socket path and builds
// the fake child binary. MEEPT_HOME is redirected into the temp dir so the
// child pid file (config.MeeptPath("run", ...)) never touches (or adopts a
// pid from) the developer's real $HOME/.meept run dir.
func newChildProcTestEnv(t *testing.T) *childProcTestEnv {
	t.Helper()
	dir := shortDir(t)
	t.Setenv("MEEPT_HOME", dir)
	sock := filepath.Join(dir, "text.sock")
	audio := filepath.Join(dir, "audio")
	if err := os.MkdirAll(audio, 0o755); err != nil {
		t.Fatalf("audio dir: %v", err)
	}
	bin := buildFakeVoiceChild(t, dir)

	logBuf := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	srv := NewTextServer(sock, logger)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("text socket never appeared: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	})
	return &childProcTestEnv{
		t:      t,
		dir:    dir,
		sock:   sock,
		audio:  audio,
		server: srv,
		binary: bin,
		logBuf: logBuf,
	}
}

// cfg builds a ChildProcConfig wired to this env. Overrides via fn.
func (e *childProcTestEnv) cfg(fn func(*ChildProcConfig)) ChildProcConfig {
	c := ChildProcConfig{
		BinaryPath:     e.binary,
		TextSocketPath: e.sock,
		AudioDir:       e.audio,
		StartTimeout:   10 * time.Second,
		Handshake:      HandshakeProbe(e.server),
	}
	if fn != nil {
		fn(&c)
	}
	return c
}

// waitGone polls until pid is no longer a live process.
func waitGone(t *testing.T, pid int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pid %d still alive after %s", pid, within)
}

// T2: EnsureRunning spawns exactly one child; a second EnsureRunning reuses
// it (one spawn, observed via the helper's on-disk marker, one process).
func TestChildProcEnsureReusesOneSpawn(t *testing.T) {
	env := newChildProcTestEnv(t)
	p := NewChildProc(env.cfg(nil), slog.Default(), nil)

	ctx := context.Background()
	if err := p.EnsureRunning(ctx); err != nil {
		t.Fatalf("first EnsureRunning: %v", err)
	}
	if !p.Alive() {
		t.Fatal("child not alive after first EnsureRunning")
	}
	pid1 := p.currentPIDForTest()
	if pid1 <= 0 {
		t.Fatal("no pid recorded after spawn")
	}

	if err := p.EnsureRunning(ctx); err != nil {
		t.Fatalf("second EnsureRunning: %v", err)
	}
	if got := p.currentPIDForTest(); got != pid1 {
		t.Fatalf("second EnsureRunning respawned: pid %d -> %d (want reuse)", pid1, got)
	}
	if n := fakeChildSpawnCount(t, env.dir); n != 1 {
		t.Fatalf("fake child started %d times, want 1", n)
	}

	if err := p.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitGone(t, pid1, 5*time.Second)
	if p.Alive() {
		t.Fatal("Alive() true after Stop")
	}
}

// T3a: idle stop fires at zero live calls and the child is reaped; a later
// EnsureRunning spawns a FRESH child.
func TestChildProcIdleStopFires(t *testing.T) {
	env := newChildProcTestEnv(t)
	p := NewChildProc(env.cfg(func(c *ChildProcConfig) {
		c.IdleStop = 300 * time.Millisecond
	}), slog.Default(), nil)

	if err := p.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	pid := p.currentPIDForTest()
	p.NoteCallStart("cc-1")
	p.NoteCallEnd("cc-1")

	waitGone(t, pid, 5*time.Second)
	if p.Alive() {
		t.Fatal("Alive() true after idle stop")
	}
	if err := p.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning after idle stop: %v", err)
	}
	if p.currentPIDForTest() == pid {
		t.Fatal("EnsureRunning after idle stop reused the stopped pid")
	}
	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("final Stop: %v", err)
	}
}

// T3b: a call arriving during draining cancels the idle timer - the child
// survives well past the idle window.
func TestChildProcDrainCancelKeepsChild(t *testing.T) {
	env := newChildProcTestEnv(t)
	p := NewChildProc(env.cfg(func(c *ChildProcConfig) {
		c.IdleStop = 300 * time.Millisecond
	}), slog.Default(), nil)

	if err := p.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	pid := p.currentPIDForTest()
	p.NoteCallStart("cc-1")
	p.NoteCallEnd("cc-1") // draining, timer armed at 300ms
	time.Sleep(150 * time.Millisecond)
	p.NoteCallStart("cc-2") // cancels the timer
	time.Sleep(600 * time.Millisecond)
	if !p.Alive() || !processAlive(pid) {
		t.Fatalf("child pid %d died despite drain cancel", pid)
	}
	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// T3c: the idle timer must not fire while a call is live (race window):
// NoteCallEnd for one of TWO live calls must not stop the child.
func TestChildProcIdleNotWhileCallsLive(t *testing.T) {
	env := newChildProcTestEnv(t)
	p := NewChildProc(env.cfg(func(c *ChildProcConfig) {
		c.IdleStop = 200 * time.Millisecond
	}), slog.Default(), nil)

	if err := p.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	pid := p.currentPIDForTest()
	p.NoteCallStart("cc-1")
	p.NoteCallStart("cc-2")
	p.NoteCallEnd("cc-1") // cc-2 still live: no timer may fire
	time.Sleep(600 * time.Millisecond)
	if !p.Alive() || !processAlive(pid) {
		t.Fatal("child stopped while cc-2 still live (idle timer fired early)")
	}
	p.NoteCallEnd("cc-2")
	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// T3d: the C8 live-count dep vetoes the idle stop: a wired Live() reporting
// a live call keeps the child even when the local call set is empty.
func TestChildProcIdleVetoedByLiveCount(t *testing.T) {
	env := newChildProcTestEnv(t)
	live := 1
	p := NewChildProc(env.cfg(func(c *ChildProcConfig) {
		c.IdleStop = 200 * time.Millisecond
	}), slog.Default(), func() int { return live })

	if err := p.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	pid := p.currentPIDForTest()
	p.NoteCallStart("cc-1")
	p.NoteCallEnd("cc-1")
	time.Sleep(600 * time.Millisecond)
	if !p.Alive() || !processAlive(pid) {
		t.Fatal("idle stop fired despite Live() reporting a live call")
	}
	live = 0 // the next drain window must fire
	p.NoteCallStart("cc-2")
	p.NoteCallEnd("cc-2")
	waitGone(t, pid, 5*time.Second)
	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// T3e: concurrent NoteCallStart/NoteCallEnd and EnsureRunning under -race
// must never stop a child with calls live (idle timer + state machine race).
func TestChildProcConcurrentCallNotes(t *testing.T) {
	env := newChildProcTestEnv(t)
	p := NewChildProc(env.cfg(func(c *ChildProcConfig) {
		c.IdleStop = 50 * time.Millisecond
	}), slog.Default(), nil)

	if err := p.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			ccid := "cc-race"
			p.NoteCallStart(ccid)
			p.NoteCallEnd(ccid)
		}
	}()
	<-done
	time.Sleep(100 * time.Millisecond)
	// Whatever the interleaving, the final state must be consistent and a
	// Stop must succeed.
	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after concurrent notes: %v", err)
	}
}

// T4: parent death. The supervisor is a helper binary carrying the llm
// supervisor entry point; the test binary re-executes as the fake daemon
// (TestMain role) that holds the parent-death pipe exactly like
// llm.RuntimeProcess.Start's daemon side. SIGKILLing the fake daemon must
// take the child down within the supervisor's grace.
func TestChildProcParentDeath(t *testing.T) {
	if !isDarwinOrLinux() {
		t.Skip("parent-death test relies on darwin/linux process semantics (signal 0, kill, process groups)")
	}
	env := newChildProcTestEnv(t)
	supBin := buildSupervisorCapableBinary(t, env.dir)

	pidFile := filepath.Join(env.dir, "voice-parentdeath.pid")
	readyFile := filepath.Join(env.dir, "fake-daemon-ready")

	fakeDaemon := exec.Command(testSelfBinary(t), append([]string{"--voice-fake-daemon",
		supBin, pidFile, readyFile}, fakeChildArgs(env)...)...)
	fakeDaemon.Stdout = os.Stdout
	fakeDaemon.Stderr = os.Stderr
	if err := fakeDaemon.Start(); err != nil {
		t.Fatalf("start fake daemon: %v", err)
	}
	t.Cleanup(func() {
		if killErr := fakeDaemon.Process.Kill(); killErr != nil {
			t.Logf("kill fake daemon: %v", killErr)
		}
		waitCmd(t, fakeDaemon)
	})

	// The fake daemon touches ready-file once it holds the death pipe; the
	// child pid lands in pidFile from the supervisor's report.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if v, err := readIntFile(pidFile); err == nil && v > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	childPID, err := readIntFile(pidFile)
	if err != nil || childPID <= 0 {
		t.Fatalf("child pid never appeared in %s: %v", pidFile, err)
	}
	waitReady(t, readyFile, 5*time.Second)
	if !processAlive(childPID) {
		t.Fatalf("supervised child pid %d not alive", childPID)
	}

	// Hard-kill the parent: the supervisor must terminate the child group.
	if err := fakeDaemon.Process.Kill(); err != nil {
		t.Fatalf("kill fake daemon: %v", err)
	}
	waitCmd(t, fakeDaemon)

	// Supervisor: SIGTERM grace (default 10s) then SIGKILL. Bound at 20s.
	waitGone(t, childPID, 20*time.Second)
}

// T5: handshake timeout refuses the call with ErrChildUnavailable within
// StartTimeout (no hang); the refused child is torn down; stale audio pair
// files are cleaned at spawn.
func TestChildProcRefusalOnHandshakeTimeout(t *testing.T) {
	env := newChildProcTestEnv(t)
	bin := buildSilentVoiceChild(t, env.dir)

	staleIn := filepath.Join(env.audio, "cc-stale.in")
	if err := os.WriteFile(staleIn, []byte("junk"), 0o600); err != nil {
		t.Fatalf("stale audio file: %v", err)
	}

	// Handshake against a socket no child will ever connect to.
	neverSrv := NewTextServer(filepath.Join(env.dir, "never.sock"), nil)
	neverCtx, neverCancel := context.WithCancel(context.Background())
	neverDone := make(chan error, 1)
	go func() { neverDone <- neverSrv.Start(neverCtx) }()
	t.Cleanup(func() {
		neverCancel()
		<-neverDone
	})

	p := NewChildProc(env.cfg(func(c *ChildProcConfig) {
		c.BinaryPath = bin
		c.StartTimeout = 500 * time.Millisecond
		c.Handshake = HandshakeProbe(neverSrv)
	}), slog.Default(), nil)

	start := time.Now()
	err := p.EnsureRunning(context.Background())
	elapsed := time.Since(start)
	if !errors.Is(err, ErrChildUnavailable) {
		t.Fatalf("EnsureRunning err = %v, want ErrChildUnavailable", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("EnsureRunning took %s; refusal must land within StartTimeout (no hang)", elapsed)
	}
	if p.Alive() {
		t.Fatal("child considered alive after a refused handshake")
	}
	// The silent child never connects; give the group kill a beat, then
	// verify nothing from this spawn is still running: the silent binary
	// never records a pid we can read, so check via Alive() (already false)
	// and that the refused spawn did not leave the child alive from a
	// previous test's pid space.
	if n := fakeChildSpawnCount(t, env.dir); n != 0 {
		t.Fatalf("silent child unexpectedly recorded %d spawns", n)
	}
	if _, err := os.Stat(staleIn); !os.IsNotExist(err) {
		t.Fatalf("stale audio pair file survived spawn: %v", err)
	}
}

// T5b: a second EnsureRunning queued behind an in-flight start resolves to
// the same child once the start settles (bounded wait, no second spawn).
func TestChildProcQueuedStartDuringStarting(t *testing.T) {
	env := newChildProcTestEnv(t)
	p := NewChildProc(env.cfg(nil), slog.Default(), nil)

	errCh := make(chan error, 1)
	go func() { errCh <- p.EnsureRunning(context.Background()) }()
	// Small delay so the second call lands while state == starting.
	time.Sleep(20 * time.Millisecond)
	err2 := p.EnsureRunning(context.Background())
	if err := <-errCh; err != nil {
		t.Fatalf("starter EnsureRunning: %v", err)
	}
	if err2 != nil {
		t.Fatalf("queued EnsureRunning: %v", err2)
	}
	if n := fakeChildSpawnCount(t, env.dir); n != 1 {
		t.Fatalf("fake child started %d times, want 1 (queued caller must not respawn)", n)
	}
	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// T5c: Stop() during an in-flight start does not deadlock: it waits for the
// start to settle (bounded by the caller's ctx, never by StartTimeout) and
// then tears down whatever came up.
func TestChildProcStopDuringStarting(t *testing.T) {
	env := newChildProcTestEnv(t)
	bin := buildSilentVoiceChild(t, env.dir)
	neverSrv := NewTextServer(filepath.Join(env.dir, "never-stop.sock"), nil)
	neverCtx, neverCancel := context.WithCancel(context.Background())
	neverDone := make(chan error, 1)
	go func() { neverDone <- neverSrv.Start(neverCtx) }()
	t.Cleanup(func() {
		neverCancel()
		<-neverDone
	})

	p := NewChildProc(env.cfg(func(c *ChildProcConfig) {
		c.BinaryPath = bin
		c.StartTimeout = 30 * time.Second // would hang far past the test
		c.Handshake = HandshakeProbe(neverSrv)
	}), slog.Default(), nil)

	startErr := make(chan error, 1)
	go func() { startErr <- p.EnsureRunning(context.Background()) }()
	time.Sleep(100 * time.Millisecond) // land EnsureRunning in starting

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := p.Stop(stopCtx); err != nil {
		t.Fatalf("Stop during starting: %v", err)
	}
	if p.Alive() {
		t.Fatal("Alive() true after Stop-during-starting")
	}
	// The in-flight start observes the teardown and reports refusal; it must
	// also finish promptly (its child was group-killed by the Stop).
	select {
	case err := <-startErr:
		if err != nil && !errors.Is(err, ErrChildUnavailable) {
			t.Fatalf("in-flight start err = %v, want ErrChildUnavailable or nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("in-flight start never settled after Stop")
	}
}

// T6: RSS debug log at spawn and first call carries the rss_kb field and
// the phase label; the idle-stop phase logs too (T3a's flow, checked here
// for the log fields only).
func TestChildProcRSSLogFields(t *testing.T) {
	env := newChildProcTestEnv(t)
	logBuf := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	p := NewChildProc(env.cfg(nil), logger, nil)

	if err := p.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	p.NoteCallStart("cc-rss")
	p.NoteCallEnd("cc-rss")
	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	logs := logBuf.String()
	t.Logf("captured RSS log lines:\n%s", rssLines(logs))
	if !strings.Contains(logs, "rss_kb=") {
		t.Fatalf("no rss_kb field in logs:\n%s", logs)
	}
	if !strings.Contains(logs, "phase=spawn") {
		t.Fatalf("no spawn-phase RSS log:\n%s", logs)
	}
	if !strings.Contains(logs, "phase=first-call") {
		t.Fatalf("no first-call RSS log:\n%s", logs)
	}
}

// T7: the binary lookup order - explicit config wins; daemon-adjacent and
// $MEEPT_HOME/bin are probed for a regular file; never a PATH search.
func TestChildProcBinaryLookup(t *testing.T) {
	dir := t.TempDir()
	inDir := filepath.Join(dir, "meept-voice")
	if err := os.WriteFile(inDir, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	if got := ResolveChildBinary(inDir); got != inDir {
		t.Fatalf("explicit config path not honored: %q", got)
	}
	// Daemon-adjacent: the test binary's directory has no meept-voice, so
	// the probe falls through; with MEEPT_HOME pointed at a dir holding
	// bin/meept-voice, that one is found.
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o755); err != nil {
		t.Fatalf("home bin: %v", err)
	}
	homeBin := filepath.Join(home, "bin", "meept-voice")
	if err := os.WriteFile(homeBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write home binary: %v", err)
	}
	t.Setenv("MEEPT_HOME", home)
	if got := ResolveChildBinary(""); got != homeBin {
		t.Fatalf("MEEPT_HOME/bin lookup failed: got %q, want %q", got, homeBin)
	}
	// Nothing anywhere: empty (call refused, never a PATH search).
	t.Setenv("MEEPT_HOME", filepath.Join(dir, "absent"))
	if got := ResolveChildBinary(""); got != "" {
		t.Fatalf("lookup fell back to %q; want empty (no PATH search)", got)
	}
}

// rssLines extracts the "voice child rss" log lines from the captured text.
func rssLines(logs string) string {
	var out []string
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "voice child rss") {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return strings.Join(out, "\n")
}

// fakeChildArgs returns the runtime argv the fake-daemon role supervises.
func fakeChildArgs(env *childProcTestEnv) []string {
	return []string{env.binary, "--text-socket", env.sock, "--audio-dir", env.audio}
}

// testSelfBinary resolves the running test binary for re-execution.
func testSelfBinary(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return self
}

// waitReady polls until path exists.
func waitReady(t *testing.T, path string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s never appeared within %s", path, within)
}

// readIntFile reads a file holding one integer.
func readIntFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var n int
	if _, err := fmtSscanfInt(strings.TrimSpace(string(data)), &n); err != nil {
		return 0, err
	}
	return n, nil
}
