// Child process lifecycle for the voice sidecar (leaf 13).
//
// ChildProc lazily spawns cmd/meept-voice on the first call, reuses it for
// further calls, stops it idle_stop_seconds after the last call ends, and
// never leaves it behind: the spawn rides the same supervised-spawn seam the
// LLM runtimes use, so a hard-killed daemon takes the child down with it
// (parent-death pipe + pid poll, internal/llm/supervisor.go) and the boot
// orphan sweep can still reap a leftover (durable spawn record in the run
// dir, ownership token in the pid file).
//
// Lifecycle invariants (internal/daemon/AGENTS.md, followed by REUSE not
// reimplementation):
//   - group kill (SIGTERM, bounded grace, SIGKILL group): llm.RuntimeProcess
//     owns it; ChildProc never signals a process itself.
//   - ownership token: the pid file carries this instance's token; Stop
//     refuses a foreign-token pid (llm.ErrRuntimeNotOwned), so a second
//     meept process can never kill our child through the shared file.
//   - orphan sweep compatibility: llm.RuntimeProcess.Start writes the
//     durable spawn record beside the pid file under config.MeeptPath("run")
//     with AutoStop=true, which is exactly the source the daemon's
//     StartupOrphanSweep scans (ppid==1 + argv match + auto_stop).
//   - secrets: the start token travels through STDIN only (leaf-07
//     contract); the child env carries no secret and no wholesale daemon
//     environment - only the VOICE_ONNX_LIB path when configured.
//
// Decision (wrap vs extract): WRAP llm.RuntimeProcess. Its kill logic, pid
// file, spawn record, and supervisor spawn are kept in one place; this file
// adds only two additive seams to internal/llm (RuntimeConfig.SpawnStdin and
// RuntimeConfig.SpawnEnv, both nil/empty = today's behavior byte-identically)
// because the voice child reads its start token from stdin before it
// connects, and its env must stay minimal. No kill logic is copied here.

package voice

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// ErrChildUnavailable reports that the voice child could not be spawned or
// did not complete its startup handshake within StartTimeout. The call is
// refused and the caller (leaf 14 manager) hangs the Telnyx leg up; the
// error is always wrapped, so callers test with errors.Is.
var ErrChildUnavailable = errors.New("voice child unavailable")

// childProc states. stopped -> starting (spawn + handshake, bounded by
// StartTimeout) -> running (>= 1 live call) -> draining (0 calls, idle timer
// armed) -> stopping -> stopped. A call during starting queues on the
// handshake result; a call during draining cancels the idle timer.
type childState int

const (
	stateStopped childState = iota
	stateStarting
	stateRunning
	stateDraining
	stateStopping
)

// idleStopKillGrace bounds the graceful stop when the idle timer fires: the
// group kill inside llm.RuntimeProcess.Stop escalates to SIGKILL when the
// context expires.
const idleStopKillGrace = 15 * time.Second

// handshakeStopGrace bounds the cleanup kill after a failed handshake.
const handshakeStopGrace = 10 * time.Second

// LiveCount reports the number of live voice calls. It adapts the C8
// Registry.Live() shape (master.md): the leaf-14 wiring supplies
// func() int { return len(registry.Live()) }. A nil dep means the documented
// single-call assumption: the only live-call guard is ChildProc's own call
// set, which is correct while at most one call is served at a time.
type LiveCount func() int

// ChildProcConfig configures the supervised voice child.
type ChildProcConfig struct {
	// BinaryPath is the resolved child binary (voice.child_binary_path).
	// Empty selects the lookup order in ResolveChildBinary: daemon-adjacent
	// first, then $MEEPT_HOME/bin. Never a PATH search.
	BinaryPath string
	// TextSocketPath is the daemon's shared text socket (C1).
	TextSocketPath string
	// AudioDir holds the per-call audio socket pairs (C2).
	AudioDir string
	// OnnxLib is the ONNX Runtime library path. It reaches the child as the
	// VOICE_ONNX_LIB env var - a path is not a secret, and it is the ONLY
	// env entry the child gets.
	OnnxLib string
	// IdleStop is how long the child lives after the last call ends. 0
	// disables the idle stop (the child runs until Stop).
	IdleStop time.Duration
	// StartTimeout bounds spawn + text-socket handshake. Exceeding it fails
	// the spawn with ErrChildUnavailable (the call is refused, never hung).
	StartTimeout time.Duration
	// Handshake blocks until the child holds the text-socket connection.
	// Production wiring (leaf 14) supplies HandshakeProbe(textServer).
	// Nil means the handshake is considered immediate: the spawn is
	// considered ready once the process is up and the caller observes the
	// connection through the TextServer itself.
	Handshake func(ctx context.Context) error
	// Live is the C8 live-call guard for the idle timer. Nil = single-call
	// assumption (see LiveCount).
	Live LiveCount
}

// ChildProc manages the single meept-voice child: lazy spawn, reuse, idle
// stop, supervised teardown. Safe for concurrent use.
type ChildProc struct {
	cfg  ChildProcConfig
	log  *slog.Logger
	live LiveCount

	mu        sync.Mutex
	state     childState
	calls     map[string]struct{}
	idleTimer *time.Timer
	startDone chan struct{} // closed when the in-flight start settles
	startGen  int           // bumped each starting entry; guards the epilogue
	// startCancel aborts the in-flight start's handshake (Stop during
	// starting calls it so the waiter settles promptly instead of blocking
	// on the child's full StartTimeout).
	startCancel context.CancelFunc
	firstCall   bool // first-call RSS log emitted

	rp *llm.RuntimeProcess
}

// NewChildProc builds a ChildProc. A nil logger falls back to slog.Default.
func NewChildProc(cfg ChildProcConfig, logger *slog.Logger, live LiveCount) *ChildProc {
	if logger == nil {
		logger = slog.Default()
	}
	return &ChildProc{
		cfg:   cfg,
		log:   logger,
		live:  live,
		state: stateStopped,
		calls: make(map[string]struct{}),
	}
}

// childPIDFile is the pid file under the run dir, where the daemon's startup
// orphan sweep scans durable spawn records (config.MeeptPath("run")).
func childPIDFile() string {
	return config.MeeptPath("run", "meept-voice.pid")
}

// ResolveChildBinary resolves the meept-voice binary: an explicit
// configPath (voice.child_binary_path) wins; otherwise the daemon-adjacent
// path (the directory of the running executable) and then $MEEPT_HOME/bin
// are probed for an existing regular file. Never a PATH search: a privileged
// child is not resolved through operator-controlled lookup order.
func ResolveChildBinary(configPath string) string {
	if configPath != "" {
		return configPath
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		cand := filepath.Join(filepath.Dir(exe), "meept-voice")
		if regularFile(cand) {
			return cand
		}
	}
	if home := os.Getenv("MEEPT_HOME"); home != "" {
		cand := filepath.Join(home, "bin", "meept-voice")
		if regularFile(cand) {
			return cand
		}
	}
	return ""
}

// regularFile reports whether path exists as a regular file.
func regularFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// HandshakeProbe returns a Handshake over srv: the probe frame's Send
// succeeds only once a child holds the single text-socket connection (Send
// refuses while no child is connected). The probe event is not a C1 event;
// the child tolerates unknown events (decode-and-continue), so the probe is
// side-effect free. Bounded by ctx (ChildProc applies StartTimeout).
func HandshakeProbe(srv *TextServer) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			if err := srv.Send(Frame{CallID: "_handshake", Event: "_handshake-probe"}); err == nil {
				return nil
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("voice child handshake: %w", ctx.Err())
			case <-ticker.C:
			}
		}
	}
}

// EnsureRunning spawns the child if needed and blocks until it is ready:
// the spawn, the text-socket handshake, or the caller's ctx bounds it. A
// call arriving during an in-flight start queues on the same result. A
// handshake timeout returns ErrChildUnavailable (the call is refused).
func (p *ChildProc) EnsureRunning(ctx context.Context) error {
	p.mu.Lock()
	switch p.state {
	case stateRunning, stateDraining:
		if p.rp != nil && p.rp.IsRunning() {
			p.stopIdleTimerLocked()
			p.state = stateRunning
			p.mu.Unlock()
			return nil
		}
		// The child died underneath us: drop the stale handle and respawn.
		p.rp = nil
		p.state = stateStopped
		fallthrough
	case stateStopped:
		p.state = stateStarting
		p.startGen++
		p.startDone = make(chan struct{})
		// Detach the start from the CALLER's ctx: a request that gives up
		// must not tear down a spawn another queued caller is waiting on.
		// StartTimeout still bounds it; Stop can abort it via startCancel.
		sctx, scancel := context.WithTimeout(context.WithoutCancel(ctx), p.cfg.StartTimeout)
		p.startCancel = scancel
		done := p.startDone
		gen := p.startGen
		p.mu.Unlock()

		err := p.spawn(sctx)

		scancel()
		p.mu.Lock()
		// Only the current cycle's epilogue may leave the starting state:
		// a Stop that arrived mid-spawn already moved it to stopped.
		if p.startGen == gen {
			if err != nil {
				p.state = stateStopped
			} else {
				p.state = stateRunning
			}
		}
		close(done)
		p.mu.Unlock()
		return err
	case stateStarting:
		done := p.startDone
		p.mu.Unlock()
		select {
		case <-done:
			// The starter's return value already propagated to its caller;
			// a queued caller re-checks on its next EnsureRunning.
			p.mu.Lock()
			st := p.state
			p.mu.Unlock()
			if st == stateRunning || st == stateDraining {
				return nil
			}
			return ErrChildUnavailable
		case <-ctx.Done():
			return fmt.Errorf("voice child start queued: %w", ctx.Err())
		}
	default: // stopping: a concurrent Stop owns the teardown
		p.mu.Unlock()
		return fmt.Errorf("voice child is stopping: %w", ErrChildUnavailable)
	}
}

// spawn runs the actual spawn + handshake. Called without p.mu held (I/O).
func (p *ChildProc) spawn(ctx context.Context) error {
	if p.cfg.TextSocketPath == "" || p.cfg.AudioDir == "" {
		return fmt.Errorf("voice child: text socket and audio dir are required")
	}
	bin := ResolveChildBinary(p.cfg.BinaryPath)
	if bin == "" {
		return fmt.Errorf("voice child: no meept-voice binary found (config child_binary_path, daemon dir, or $MEEPT_HOME/bin)")
	}

	// Stale socket files from a previous child must not survive a spawn.
	// The text socket is NOT cleaned here: it is the daemon's OWN live
	// listener (TextServer.Start removes stale state at bind time), and
	// removing the live socket file would break every future dial to it.
	// Only the per-call audio pair files (leaf 03 AudioPair removes its own
	// pair at Close; leftovers come from a crashed call) are swept.
	if err := os.MkdirAll(p.cfg.AudioDir, 0o755); err != nil {
		return fmt.Errorf("voice child: create audio dir %s: %w", p.cfg.AudioDir, err)
	}
	if stale, err := filepath.Glob(filepath.Join(p.cfg.AudioDir, "*.in")); err == nil {
		for _, f := range stale {
			if rmErr := os.Remove(f); rmErr != nil { //nolint:mutexio // best-effort stale cleanup, no lock held
				p.log.Debug("voice: stale audio remove", "path", f, "err", rmErr)
			}
		}
	}
	if stale, err := filepath.Glob(filepath.Join(p.cfg.AudioDir, "*.out")); err == nil {
		for _, f := range stale {
			if rmErr := os.Remove(f); rmErr != nil { //nolint:mutexio // best-effort stale cleanup, no lock held
				p.log.Debug("voice: stale audio remove", "path", f, "err", rmErr)
			}
		}
	}

	// Start token: crypto/rand, delivered through STDIN only (never argv,
	// never env). Generation failure fails the spawn - an empty token is a
	// refusal, not a fallback (the child refuses empty tokens too).
	token, err := newStartToken()
	if err != nil {
		return fmt.Errorf("voice child: start token: %w", err)
	}
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("voice child: stdin pipe: %w", err)
	}
	go func() {
		// The pipe buffer holds the token line; the write never blocks and
		// the child sees EOF after the line, which readToken accepts.
		if _, wErr := stdinW.WriteString(token + "\n"); wErr != nil {
			slog.Debug("voice: child stdin write", "err", wErr)
		}
		if cErr := stdinW.Close(); cErr != nil {
			slog.Debug("voice: child stdin close", "err", cErr)
		}
	}()

	// Child env: minimal by contract. Only the ONNX library path (a path is
	// not a secret); no wholesale daemon environment, no credentials.
	var env []string
	if p.cfg.OnnxLib != "" {
		env = []string{"VOICE_ONNX_LIB=" + p.cfg.OnnxLib}
	} else {
		env = []string{}
	}

	rc := &llm.RuntimeConfig{
		EndpointKey:  "voice-child",
		PIDFile:      childPIDFile(),
		AutoStop:     true, // sweepable by the startup orphan sweep
		SpawnCommand: []string{bin, "--text-socket", p.cfg.TextSocketPath, "--audio-dir", p.cfg.AudioDir},
		SpawnStdin:   stdinR,
		SpawnEnv:     env,
	}
	rp := llm.NewRuntimeProcess(rc)
	// Start detaches from the caller ctx internally (request-scoped contexts
	// must not kill a long-lived child) and writes the pid file with this
	// instance's ownership token plus the durable spawn record.
	if err := rp.Start(ctx, nil, nil); err != nil {
		return fmt.Errorf("voice child spawn: %w", err)
	}

	pid := rp.PID()
	if !rp.IsRunning() {
		if stopErr := rp.Stop(shortStopCtx(handshakeStopGrace)); stopErr != nil {
			p.log.Debug("voice: refused child stop", "pid", pid, "err", stopErr)
		}
		return fmt.Errorf("voice child died immediately: %w", ErrChildUnavailable)
	}

	if p.cfg.Handshake != nil {
		// ctx is the start context (StartTimeout-bounded by EnsureRunning,
		// abortable by Stop); spawn's handshake just follows it.
		herr := p.cfg.Handshake(ctx)
		if herr != nil {
			if stopErr := rp.Stop(shortStopCtx(handshakeStopGrace)); stopErr != nil {
				p.log.Debug("voice: handshake-fail child stop", "pid", pid, "err", stopErr)
			}
			return fmt.Errorf("voice child handshake: %w (%v)", ErrChildUnavailable, herr)
		}
	}

	p.mu.Lock()
	p.rp = rp
	p.mu.Unlock()

	p.logRSS("spawn", pid)
	return nil
}

// newStartToken generates 32 hex-encoded random bytes.
func newStartToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// shortStopCtx bounds a cleanup stop. The returned context carries its own
// timeout and must NOT be cancelled by the caller: llm.RuntimeProcess.Stop
// treats ctx.Done as "escalate to SIGKILL", so a cleanup stop that outlives
// its grace window self-escalates when the deadline passes.
func shortStopCtx(grace time.Duration) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	runtime.SetFinalizer(&ctx, func(p *context.Context) { cancel() })
	return ctx
}

// NoteCallStart records a live call and cancels any armed idle timer (a call
// during draining keeps the child alive). The first call logs the child RSS.
func (p *ChildProc) NoteCallStart(ccid string) {
	p.mu.Lock()
	p.calls[ccid] = struct{}{}
	p.stopIdleTimerLocked()
	if p.state == stateDraining {
		p.state = stateRunning
	}
	first := !p.firstCall
	p.firstCall = true
	pid := p.currentPIDLocked()
	p.mu.Unlock()
	if first {
		p.logRSS("first-call", pid)
	}
}

// NoteCallEnd drops a live call; at zero the child enters draining and the
// idle timer is armed (IdleStop; 0 disables). The timer fires only at zero
// live calls: idleFired re-checks both the call set and the C8 live count.
func (p *ChildProc) NoteCallEnd(ccid string) {
	p.mu.Lock()
	delete(p.calls, ccid)
	if len(p.calls) > 0 {
		p.mu.Unlock()
		return
	}
	if p.state != stateRunning && p.state != stateDraining {
		p.mu.Unlock()
		return
	}
	if p.cfg.IdleStop <= 0 {
		p.mu.Unlock()
		return
	}
	p.state = stateDraining
	idle := p.cfg.IdleStop
	p.idleTimer = time.AfterFunc(idle, p.idleFired)
	p.mu.Unlock()
}

// idleFired is the idle timer callback. It stops the child only when the
// call set is empty AND the C8 live count (when wired) reports no live call.
func (p *ChildProc) idleFired() {
	p.mu.Lock()
	if p.state != stateDraining || len(p.calls) > 0 {
		p.mu.Unlock()
		return
	}
	if p.live != nil && p.live() > 0 {
		// A live call the local call set does not know about (the registry
		// is the authority per C8): keep the child, back to running.
		p.state = stateRunning
		p.mu.Unlock()
		return
	}
	rp := p.rp
	pid := p.currentPIDLocked()
	p.state = stateStopping
	p.mu.Unlock()

	if rp == nil {
		p.mu.Lock()
		p.state = stateStopped
		p.mu.Unlock()
		return
	}

	p.logRSS("idle-stop", pid)
	if stopErr := rp.Stop(shortStopCtx(idleStopKillGrace)); stopErr != nil {
		p.log.Debug("voice: idle-stop child stop", "pid", pid, "err", stopErr)
	}

	p.mu.Lock()
	// A spawn raced the teardown and installed a fresh child: keep it.
	if p.state == stateStopping {
		p.state = stateStopped
		if p.rp == rp {
			p.rp = nil
		}
	}
	p.mu.Unlock()
}

// Stop tears the child down gracefully (SIGTERM group, bounded by ctx, then
// SIGKILL inside llm.RuntimeProcess). Daemon shutdown calls this. Stop while
// a start is in flight waits for it to settle (bounded by StartTimeout, so
// it cannot deadlock) and then stops whatever came up.
func (p *ChildProc) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	p.mu.Lock()
	for p.state == stateStarting {
		done := p.startDone
		// Abort the in-flight handshake so this stop settles promptly
		// instead of waiting out the child's full StartTimeout.
		if p.startCancel != nil {
			p.startCancel()
		}
		p.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			return fmt.Errorf("voice child stop: start still in flight: %w", ctx.Err())
		}
		p.mu.Lock()
	}
	p.stopIdleTimerLocked()
	rp := p.rp
	p.rp = nil
	p.state = stateStopped
	p.mu.Unlock()

	if rp == nil {
		return nil
	}
	return rp.Stop(ctx)
}

// Alive reports whether a spawned child process is still running.
func (p *ChildProc) Alive() bool {
	p.mu.Lock()
	rp := p.rp
	st := p.state
	p.mu.Unlock()
	if rp == nil || (st != stateRunning && st != stateDraining) {
		return false
	}
	return rp.IsRunning()
}

// stopIdleTimerLocked cancels the armed idle timer. Caller holds p.mu.
func (p *ChildProc) stopIdleTimerLocked() {
	if p.idleTimer != nil {
		p.idleTimer.Stop()
		p.idleTimer = nil
	}
}

// currentPIDLocked reads the child pid. Caller holds p.mu.
func (p *ChildProc) currentPIDLocked() int {
	if p.rp == nil {
		return 0
	}
	return p.rp.PID()
}

// logRSS emits one debug log line with the child's resident set size at the
// given lifecycle phase (spawn / first-call / idle-stop). A plain log line,
// deliberately not a metric-store write in this leaf. rss_kb is present only
// when the measurement succeeded (ps -o rss on darwin/linux).
func (p *ChildProc) logRSS(phase string, pid int) {
	if pid <= 0 {
		return
	}
	kb, err := childRSSKB(pid)
	if err != nil {
		p.log.Debug("voice child rss unavailable", "phase", phase, "pid", pid, "error", err)
		return
	}
	p.log.Debug("voice child rss", "phase", phase, "pid", pid, "rss_kb", kb)
}

// childRSSKB returns the child's resident set size in KB via ps -o rss
// (darwin + linux both report KB). Windows has no ps: measured nowhere,
// logged without the field.
func childRSSKB(pid int) (int64, error) {
	var out bytes.Buffer
	cmd := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid))
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("ps rss: %w", err)
	}
	field := strings.TrimSpace(out.String())
	// ps prints one line per pid plus a header-less value; a multi-line
	// output (pid reuse) takes the last line.
	for _, line := range strings.Split(field, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		v, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("ps rss parse %q: %w", line, err)
		}
		return v, nil
	}
	return 0, fmt.Errorf("ps rss: empty output")
}

// Compile-time guards: the child writes frames (unused here) and the stdin
// pipe reader must satisfy exec.Cmd's Stdin interface.
var (
	_ io.Reader = (*os.File)(nil)
	_ io.Writer = (*os.File)(nil)
)
