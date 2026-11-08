package signal

// signal-cli subprocess supervisor (contract C3, owner leaf 10).
//
// Repo rule: NEVER hand-roll child lifecycle. The supervisor WRAPS
// llm.RuntimeProcess (internal/llm/runtime_process.go) — pid file, kill,
// orphan-sweep reuse — exactly like the voice child does. This file adds
// only the signal-cli specifics:
//   - the `jsonRpc --socket` spawn command shape (one process per
//     Signal account);
//   - a BOUNDED wait for the socket file before Start returns;
//   - restart-on-crash with exponential backoff (capped);
//   - optional idle stop (no client activity for IdleStopSeconds).
//
// Credentials: signal-cli holds its own account state (linked device);
// the supervisor only spawns the configured binary with the configured
// account handle. No secret material passes through this package.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// Named sentinel errors (repo rule).
var (
	// ErrSupervisorStopped is returned by Client/Start after Stop.
	ErrSupervisorStopped = errors.New("signal supervisor: stopped")
	// ErrSupervisorSocketTimeout means the child did not create its
	// socket within the bounded start window.
	ErrSupervisorSocketTimeout = errors.New("signal supervisor: socket did not appear in time")
)

// SupervisorConfig carries the lifecycle knobs. Zero values select the
// documented defaults; IdleStopSeconds=0 disables idle stop.
type SupervisorConfig struct {
	// IdleStopSeconds stops the child when no client was handed out for
	// this long (signal-cli is expensive to keep warm). 0 disables.
	IdleStopSeconds int
	// RestartBackoffMs is the base crash-restart backoff (default 1000).
	RestartBackoffMs int
	// RestartMaxBackoffMs caps the exponential backoff (default 30000).
	RestartMaxBackoffMs int
	// StartTimeoutSeconds bounds the socket-file wait (default 15).
	StartTimeoutSeconds int
}

// Supervisor manages one signal-cli json-rpc child per Signal account.
type Supervisor struct {
	binary     string
	account    string
	socketPath string
	pidFile    string
	cfg        SupervisorConfig
	log        *slog.Logger

	// startMu serializes Start/Stop; child spawn + socket wait happen
	// while held (no analyzer-flagged I/O names under it).
	startMu sync.Mutex

	stateMu     sync.Mutex
	proc        *llm.RuntimeProcess
	client      *Client
	running     bool
	stopped     bool
	stopWatch   chan struct{}
	watchOnce   sync.Once
	lastDial    time.Time // idle-stop clock; guarded by stateMu
	restarts    int
	restartHook func() // test seam: fired after each successful restart
}

// NewSupervisor builds a supervisor for `binary -u <account> jsonRpc
// --socket <socketPath>`. Zero-value config knobs resolve to defaults.
func NewSupervisor(binary string, account string, socketPath string, cfg SupervisorConfig, log *slog.Logger) *Supervisor {
	if log == nil {
		log = slog.Default()
	}
	if cfg.RestartBackoffMs <= 0 {
		cfg.RestartBackoffMs = 1000
	}
	if cfg.RestartMaxBackoffMs <= 0 {
		cfg.RestartMaxBackoffMs = 30000
	}
	if cfg.RestartMaxBackoffMs < cfg.RestartBackoffMs {
		cfg.RestartMaxBackoffMs = cfg.RestartBackoffMs
	}
	if cfg.StartTimeoutSeconds <= 0 {
		cfg.StartTimeoutSeconds = 15
	}
	return &Supervisor{
		binary:     binary,
		account:    account,
		socketPath: socketPath,
		pidFile:    socketPath + ".pid",
		cfg:        cfg,
		log:        log,
		stopWatch:  make(chan struct{}),
	}
}

// spawnCommand is the signal-cli json-rpc argv. One process per account.
func (s *Supervisor) spawnCommand() []string {
	cmd := []string{s.binary}
	if s.account != "" {
		cmd = append(cmd, "-u", s.account)
	}
	cmd = append(cmd, "jsonRpc", "--socket", s.socketPath, "--no-autostart")
	return cmd
}

// newRuntimeProcess builds the wrapped RuntimeProcess for one spawn.
func (s *Supervisor) newRuntimeProcess() *llm.RuntimeProcess {
	return llm.NewRuntimeProcess(&llm.RuntimeConfig{
		PIDFile:      s.pidFile,
		SpawnCommand: s.spawnCommand(),
		AutoStop:     true,
	})
}

// Start launches the signal-cli child (or adopts a live one via the
// shared pid file) and waits, bounded, for the json-rpc socket to
// appear. Idempotent while running.
func (s *Supervisor) Start(ctx context.Context) error {
	s.startMu.Lock()
	defer s.startMu.Unlock()

	s.stateMu.Lock()
	if s.stopped {
		s.stateMu.Unlock()
		return ErrSupervisorStopped
	}
	if s.running {
		s.stateMu.Unlock()
		return nil
	}
	s.stateMu.Unlock()

	proc, err := s.spawnAndWait(ctx)
	if err != nil {
		return err
	}
	s.stateMu.Lock()
	s.proc = proc
	s.running = true
	s.lastDial = time.Now()
	s.stateMu.Unlock()
	s.startWatch(proc)
	if s.cfg.IdleStopSeconds > 0 {
		go s.idleStopLoop()
	}
	return nil
}

// spawnAndWait spawns the child and blocks until the socket exists
// (bounded by StartTimeoutSeconds and ctx). Runs outside the state
// mutex; the returned RuntimeProcess carries the pid-file lifecycle.
func (s *Supervisor) spawnAndWait(ctx context.Context) (*llm.RuntimeProcess, error) {
	proc := s.newRuntimeProcess()
	// stdout/stderr: the child's own diagnostics go to the daemon's
	// streams; our slog carries lifecycle events.
	if err := proc.Start(ctx, os.Stdout, os.Stderr); err != nil {
		return nil, fmt.Errorf("signal supervisor: spawn %s: %w", s.binary, err)
	}
	if err := s.waitSocket(ctx, proc); err != nil {
		// Never leave a half-started child behind.
		_ = proc.Stop(context.WithoutCancel(ctx))
		return nil, err
	}
	return proc, nil
}

// waitSocket polls for the socket file until it appears, the bound
// expires, or ctx dies. Stat is cheap; the poll interval stays well
// under the smallest backoff.
func (s *Supervisor) waitSocket(ctx context.Context, proc *llm.RuntimeProcess) error {
	deadline := time.After(time.Duration(s.cfg.StartTimeoutSeconds) * time.Second)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("signal supervisor: %w while waiting for %s", ctx.Err(), s.socketPath)
		case <-deadline:
			return fmt.Errorf("%w: %s", ErrSupervisorSocketTimeout, s.socketPath)
		case <-tick.C:
		}
		if _, err := os.Stat(s.socketPath); err == nil {
			return nil
		}
		if !proc.IsRunning() {
			return fmt.Errorf("signal supervisor: child exited before binding %s", s.socketPath)
		}
	}
}

// startWatch arms the crash-restart loop (once per supervisor).
func (s *Supervisor) startWatch(proc *llm.RuntimeProcess) {
	s.watchOnce.Do(func() {
		go s.watchLoop(proc)
	})
}

// watchLoop restarts the child on crash with exponential backoff capped
// at RestartMaxBackoffMs. It shares s.startMu with Start/Stop so a
// restart never races a shutdown.
func (s *Supervisor) watchLoop(proc *llm.RuntimeProcess) {
	backoff := time.Duration(s.cfg.RestartBackoffMs) * time.Millisecond
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-s.stopWatch:
			return
		case <-tick.C:
		}
		s.stateMu.Lock()
		stopped := s.stopped
		s.stateMu.Unlock()
		if stopped {
			return
		}
		if proc.IsRunning() {
			continue
		}
		// Crash detected: backoff, then respawn through the SAME
		// RuntimeProcess (pid-file lifecycle handles the dead pid).
		s.log.Warn("signal-cli child exited; restarting", "backoff_ms", backoff.Milliseconds())
		select {
		case <-s.stopWatch:
			return
		case <-time.After(backoff):
		}
		s.startMu.Lock()
		s.stateMu.Lock()
		stoppedNow := s.stopped
		current := s.proc
		s.stateMu.Unlock()
		if stoppedNow || current != proc {
			s.startMu.Unlock()
			return
		}
		newProc, err := s.spawnAndWait(context.WithoutCancel(context.Background()))
		if err != nil {
			s.startMu.Unlock()
			s.log.Warn("signal-cli restart failed", "error", err)
			backoff = s.nextBackoff(backoff)
			continue
		}
		// Swap the child and drop the stale client (its conn points at
		// the dead process; the next Client() dials the fresh socket).
		s.stateMu.Lock()
		s.proc = newProc
		oldClient := s.client
		s.client = nil
		s.lastDial = time.Now()
		s.restarts++
		s.stateMu.Unlock()
		s.startMu.Unlock()
		if oldClient != nil {
			_ = oldClient.Close()
		}
		if s.restartHook != nil {
			s.restartHook()
		}
		backoff = time.Duration(s.cfg.RestartBackoffMs) * time.Millisecond
		proc = newProc
	}
}

// nextBackoff doubles, capped at RestartMaxBackoffMs.
func (s *Supervisor) nextBackoff(cur time.Duration) time.Duration {
	maxBackoff := time.Duration(s.cfg.RestartMaxBackoffMs) * time.Millisecond
	next := cur * 2
	if next > maxBackoff {
		return maxBackoff
	}
	return next
}

// idleStopLoop stops the child when no client was handed out for
// IdleStopSeconds. A later Start (or Client-driven EnsureRunning by the
// legs) brings it back.
func (s *Supervisor) idleStopLoop() {
	interval := time.Duration(s.cfg.IdleStopSeconds) * time.Second
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-s.stopWatch:
			return
		case <-tick.C:
		}
		s.stateMu.Lock()
		last := s.lastDial
		s.stateMu.Unlock()
		if time.Since(last) < interval {
			continue
		}
		s.log.Info("signal-cli idle stop", "idle_seconds", s.cfg.IdleStopSeconds)
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = s.Stop(stopCtx)
		cancel()
		return
	}
}

// Client dials (or returns the cached) json-rpc client for the child's
// socket. A cached client whose conn has died (child crash — the demux
// exited) is dropped so the caller dials the REPLACEMENT child's fresh
// socket instead of surfacing a closed-conn error; after a supervised
// restart the crash-recovery contract is that the next Client() call
// recovers.
func (s *Supervisor) Client() (*Client, error) {
	s.stateMu.Lock()
	if s.stopped {
		s.stateMu.Unlock()
		return nil, ErrSupervisorStopped
	}
	cached := s.client
	running := s.running
	s.stateMu.Unlock()
	if cached != nil && cachedAlive(cached) {
		return cached, nil
	}
	if cached != nil {
		// Stale client (child died): drop it before dialing fresh.
		s.stateMu.Lock()
		if s.client == cached {
			s.client = nil
		}
		s.stateMu.Unlock()
		_ = cached.Close()
	}
	if !running {
		return nil, fmt.Errorf("signal supervisor: not running (call Start first)")
	}
	c, err := Dial(s.socketPath)
	if err != nil {
		return nil, fmt.Errorf("signal supervisor: dial %s: %w", s.socketPath, err)
	}
	s.stateMu.Lock()
	// Another goroutine may have cached one first; prefer theirs and
	// drop ours (an extra dial is harmless).
	if s.client != nil {
		ours := c
		c = s.client
		s.stateMu.Unlock()
		_ = ours.Close()
		return c, nil
	}
	s.client = c
	s.lastDial = time.Now()
	s.stateMu.Unlock()
	return c, nil
}

// cachedAlive reports whether the cached client's demux is still
// running (the demux exits when the child's socket conn dies). The
// done channel is closed exactly once by that goroutine — a read, no
// I/O, safe without the supervisor mutex.
func cachedAlive(c *Client) bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

// Stop terminates the child (process group, via the wrapped
// RuntimeProcess), closes the cached client, and retires the
// supervisor. Idempotent.
//
// Mutex scope: startMu is held ONLY for the state swap; the client /
// child teardown I/O (Close, proc.Stop) runs after it is released via
// stopTeardown, so no mutex is ever held across I/O (mutexio rule).
func (s *Supervisor) Stop(ctx context.Context) error {
	s.startMu.Lock()
	s.stateMu.Lock()
	s.stopped = true
	proc := s.proc
	client := s.client
	s.proc = nil
	s.client = nil
	s.running = false
	s.stateMu.Unlock()

	// Retire the watch/idle loops first so they cannot respawn.
	select {
	case <-s.stopWatch:
	default:
		close(s.stopWatch)
	}
	s.startMu.Unlock()

	return s.stopTeardown(ctx, proc, client)
}

// stopTeardown performs the I/O phase of Stop with NO supervisor mutex
// held. A racing second Stop sees proc==client==nil and returns nil
// (idempotent); the first caller owns the teardown.
func (s *Supervisor) stopTeardown(ctx context.Context, proc *llm.RuntimeProcess, client *Client) error {
	var firstErr error
	if client != nil {
		if err := client.Close(); err != nil {
			firstErr = err
		}
	}
	if proc != nil {
		if err := proc.Stop(ctx); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	// Best-effort socket cleanup: a dead child can leave the socket
	// file behind, and a stale file would make a later Start's socket
	// wait pass before the new child binds.
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		s.log.Debug("signal socket removal failed", "path", s.socketPath, "error", err)
	}
	return firstErr
}
