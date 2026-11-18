// Kanban sidecar boot (plans/kanban-board 04/04 daemon-spawn-supervision):
// the daemon-side supervised spawn of cmd/meept-kanban, gated on
// [kanban].sidecar_enabled (default false = byte-identical boot: no process,
// no pid file, no record, no goroutine).
//
// The pattern is the voice child (internal/voice/childproc.go), followed by
// REUSE not reimplementation: the sidecar is spawned as an llm.RuntimeProcess
// under its own endpoint key, which brings for free the four lifecycle
// invariants internal/daemon/AGENTS.md demands of a long-lived daemon child:
//
//   - startup orphan sweep: RuntimeProcess.Start writes a durable spawn
//     record (AutoStop=true) beside the pid file under
//     config.MeeptPath("run") — exactly the source Daemon.StartupOrphanSweep
//     scans (ppid==1 + argv match + auto_stop + no live recorded owner).
//     StartupOrphanSweep runs earlier in Run() than this boot, so a leaked
//     sidecar from a hard-killed previous daemon is reaped BEFORE spawn
//     (sweep-before-spawn), and the sidecar's HTTP bind is free.
//   - ownership: the pid file carries this instance's token; a foreign-token
//     live pid is adopted observed-NOT-owned, Stop refuses it with
//     llm.ErrRuntimeNotOwned, so a sidecar owned by a LIVE daemon is never
//     killed by this one (no custom pid-file reaper exists here; the sweep
//     stays the only reaper).
//   - hard-kill containment: inside the real daemon the spawn rides the same
//     parent-death supervisor (llm/supervisor.go) the runtimes and the voice
//     child use, so a SIGKILLed daemon still takes the sidecar down.
//   - graceful SIGTERM shutdown: Daemon.shutdown → stopKanbanSidecar →
//     RuntimeProcess.Stop (group SIGTERM, grace, then SIGKILL; the sidecar
//     drains its own HTTP conns in 5s on SIGTERM, cmd/meept-kanban/main.go).
//
// The supervisor loop adds only what the voice child has no equivalent of:
// the kanban gateway must be UP for the whole daemon life (the board GUI
// dials it), so an unexpected child exit is respawned with exponential
// backoff, capped at kanbanRestartMax attempts inside kanbanRestartWindow —
// then the loop gives up with one loud ERROR (matching the runtime manager's
// auto-restart give-up posture, internal/llm/runtime_manager.go).
//
// The sidecar is a PROCESS: the daemon passes it its arguments only (daemon
// unix socket + configured bind + webdir) and never imports internal/kanban
// or internal/kanbansvc here (contract C7: no board logic is linked).

package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

const (
	// kanbanSidecarEndpointKey names the child in the shared run dir: the
	// pid file, the durable spawn record, and the sweep/ownership machinery
	// all key off it (voice uses "voice-child" the same way).
	kanbanSidecarEndpointKey = "kanban-sidecar"

	// defaultKanbanSidecarBind mirrors cmd/meept-kanban's defaultBind.
	defaultKanbanSidecarBind = "127.0.0.1:8790"

	// kanbanStopGrace bounds the graceful stop when the caller passes no
	// shutdown context: the sidecar drains HTTP for up to 5s on SIGTERM.
	kanbanStopGrace = 7 * time.Second

	// kanbanRestartMax / kanbanRestartWindow cap automatic respawns:
	// 5 attempts within 60s, then give up + one ERROR log.
	kanbanRestartMax    = 5
	kanbanRestartWindow = 60 * time.Second

	kanbanDefaultPollInterval = 1 * time.Second
	kanbanDefaultBackoffBase  = 1 * time.Second
	kanbanDefaultBackoffMax   = 30 * time.Second
)

// kanbanBinaryResolver is the child-binary resolution seam (mirrors
// llm.supervisorBinary's var pattern): tests replace it to point the
// daemon boot at a scripted fake sidecar without a PATH search.
var kanbanBinaryResolver = ResolveKanbanChildBinary

// kanbanSidecarConfig configures one supervised meept-kanban child.
type kanbanSidecarConfig struct {
	// BinaryPath is an explicit child binary (test seam / future knob).
	// Empty selects the voice-child resolution order: daemon-adjacent
	// first, then $MEEPT_HOME/bin. Never a PATH search.
	BinaryPath string
	// SocketPath is the daemon's RPC unix socket (the sidecar forwards
	// every kanban.* call over it).
	SocketPath string
	// Bind is the sidecar's HTTP listen address ([kanban].sidecar_bind).
	Bind string
	// Webdir is the optional web-bundle root; empty lets the sidecar
	// resolve $WEBDIR / $MEEPT_HOME/webui itself.
	Webdir string
	// AllowedOrigins mirrors [kanban].allowed_origins: each entry is
	// passed as a repeated -allow-origin flag so the sidecar's WS relay
	// accepts the TLS-proxy's public Origin. The sidecar stays
	// config-blind (C7) — flags are its only config channel. Empty =
	// no flags, byte-identical default posture.
	AllowedOrigins []string
	// PIDFile anchors the pid file + durable spawn record. Empty selects
	// the run dir (where StartupOrphanSweep scans).
	PIDFile string

	// Supervisor tuning (zero = defaults; tests shrink them).
	PollInterval  time.Duration
	BackoffBase   time.Duration
	BackoffMax    time.Duration
	RestartWindow time.Duration
	RestartMax    int
	StopGrace     time.Duration
}

func (c kanbanSidecarConfig) pollInterval() time.Duration {
	if c.PollInterval > 0 {
		return c.PollInterval
	}
	return kanbanDefaultPollInterval
}

func (c kanbanSidecarConfig) stopGrace() time.Duration {
	if c.StopGrace > 0 {
		return c.StopGrace
	}
	return kanbanStopGrace
}

func (c kanbanSidecarConfig) pidFile() string {
	if c.PIDFile != "" {
		return c.PIDFile
	}
	return config.MeeptPath("run", "meept-kanban.pid")
}

// resolveBinary applies the voice-child lookup order through the
// kanbanBinaryResolver seam: explicit path, then daemon-adjacent, then
// $MEEPT_HOME/bin. Never PATH.
func (c kanbanSidecarConfig) resolveBinary() string {
	return kanbanBinaryResolver(c.BinaryPath)
}

// ResolveKanbanChildBinary resolves the meept-kanban binary: an explicit
// configPath wins; otherwise the daemon-adjacent path (directory of the
// running executable) and then $MEEPT_HOME/bin are probed for an existing
// regular file. Mirrors voice.ResolveChildBinary's order and rationale: a
// privileged child is not resolved through operator-controlled PATH.
func ResolveKanbanChildBinary(configPath string) string {
	if configPath != "" {
		return configPath
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		cand := filepath.Join(filepath.Dir(exe), "meept-kanban")
		if kanbanRegularFile(cand) {
			return cand
		}
	}
	if home := os.Getenv(config.EnvMeeptHome); home != "" {
		cand := filepath.Join(home, "bin", "meept-kanban")
		if kanbanRegularFile(cand) {
			return cand
		}
	}
	return ""
}

// kanbanRegularFile reports whether path exists as a regular file.
func kanbanRegularFile(path string) bool {
	fi, err := os.Stat(path) //nolint:gosec // G703: candidate binary path is daemon-derived (exe dir / MEEPT_HOME), not user taint
	return err == nil && fi.Mode().IsRegular()
}

// kanbanSupervisor owns one supervised meept-kanban child and the respawn
// loop. Safe for concurrent use; no I/O happens under the mutex.
type kanbanSupervisor struct {
	cfg    kanbanSidecarConfig
	log    *slog.Logger
	stderr io.Writer // child stderr sink (daemon logger in production)

	mu       sync.Mutex
	rp       *llm.RuntimeProcess // current child (nil = down/stopped/adopted-absent)
	lastPID  int
	restarts []time.Time // rolling window of respawn/attempt timestamps
	stopCh   chan struct{}
}

func newKanbanSupervisor(cfg kanbanSidecarConfig, logger *slog.Logger) *kanbanSupervisor {
	if logger == nil {
		logger = slog.Default()
	}
	return &kanbanSupervisor{
		cfg:    cfg,
		log:    logger.With("component", "kanban-sidecar"),
		stderr: &kanbanLogWriter{log: logger.With("component", "kanban-sidecar")},
		stopCh: make(chan struct{}),
	}
}

// kanbanLogWriter adapts the child's stderr stream to the daemon logger:
// one line per write, trimmed, at debug level (the sidecar's own slog
// output is diagnostic noise for the daemon log otherwise). slog is
// goroutine-safe, so no local mutex is needed.
type kanbanLogWriter struct{ log *slog.Logger }

func (w *kanbanLogWriter) Write(p []byte) (int, error) {
	if line := strings.TrimSpace(string(p)); line != "" {
		w.log.Debug("sidecar stderr", "line", line)
	}
	return len(p), nil
}

// StartKanbanSidecar boots the supervised meept-kanban sidecar when
// [kanban].sidecar_enabled. Disabled (the default) returns nil immediately
// and touches nothing — the boot path stays byte-identical.
//
// The first spawn attempt runs synchronously so the caller (Run) reports a
// missing binary / refused gate at boot; the respawn loop then runs on its
// own goroutine for the lifetime of ctx (the daemon run context). The
// returned error is non-fatal by contract: the board gateway degrades, the
// daemon boots (a sidecar crash never takes the daemon down).
func (d *Daemon) StartKanbanSidecar(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if d.fullConfig == nil || !d.fullConfig.Kanban.SidecarEnabled {
		return nil // disabled: no process, no socket, byte-identical
	}
	if d.kanbanSidecar != nil {
		return nil // already supervising (defensive: one supervisor per daemon)
	}
	if !d.fullConfig.Kanban.Enabled {
		// C9: the sidecar forwards kanban.* RPC; without the subsystem
		// enabled every call 404s. Refuse the spawn with a pointer, do
		// not start a doomed gateway.
		d.logger.Warn("kanban sidecar: [kanban].sidecar_enabled set without [kanban].enabled; skipping spawn (the gateway serves nothing without the board RPC)")
		return nil
	}
	if !d.fullConfig.Transport.RPC.Enabled {
		d.logger.Warn("kanban sidecar: [kanban].sidecar_enabled needs [transport].rpc.enabled (the sidecar forwards every call over the daemon unix socket); skipping spawn")
		return nil
	}
	socketPath := ""
	if d.config != nil {
		socketPath = d.config.SocketPath
	}
	if socketPath == "" {
		socketPath = config.MeeptPath("meept.sock")
	}
	bind := defaultKanbanSidecarBind
	webdir := ""
	if cfg := d.fullConfig.Kanban; cfg.SidecarBind != "" {
		bind = cfg.SidecarBind
	}
	if bin := kanbanBinaryResolver(""); bin == "" {
		return errors.New("kanban sidecar: no meept-kanban binary found (build it and place it next to the daemon binary or in $MEEPT_HOME/bin)")
	}

	sup := newKanbanSupervisor(kanbanSidecarConfig{
		SocketPath:     socketPath,
		Bind:           bind,
		Webdir:         webdir,
		AllowedOrigins: d.fullConfig.Kanban.AllowedOrigins,
	}, d.logger)
	// Test seam: tightened timings applied BEFORE the loop goroutine
	// starts, so the loop never reads cfg while it is being mutated.
	if d.kanbanTuning != nil {
		tuning := *d.kanbanTuning
		if tuning.PollInterval > 0 {
			sup.cfg.PollInterval = tuning.PollInterval
		}
		if tuning.BackoffBase > 0 {
			sup.cfg.BackoffBase = tuning.BackoffBase
		}
		if tuning.BackoffMax > 0 {
			sup.cfg.BackoffMax = tuning.BackoffMax
		}
		if tuning.RestartWindow > 0 {
			sup.cfg.RestartWindow = tuning.RestartWindow
		}
		if tuning.RestartMax > 0 {
			sup.cfg.RestartMax = tuning.RestartMax
		}
		if tuning.StopGrace > 0 {
			sup.cfg.StopGrace = tuning.StopGrace
		}
	}
	d.kanbanSidecar = sup

	ready := make(chan error, 1) // buffered: the loop never blocks on send
	go sup.run(ctx, ready)
	select {
	case err := <-ready:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// stopKanbanSidecar terminates the supervised child on daemon shutdown
// (graceful SIGTERM group, then SIGKILL inside RuntimeProcess when ctx
// expires). No-op when the sidecar was never enabled.
func (d *Daemon) stopKanbanSidecar(ctx context.Context) {
	if d.kanbanSidecar == nil {
		return
	}
	if err := d.kanbanSidecar.Stop(ctx); err != nil {
		d.logger.Warn("kanban sidecar: stop failed", "error", err)
		return
	}
	d.kanbanSidecar = nil
	d.logger.Info("kanban sidecar stopped")
}

// run is the supervisor loop: initial spawn (result delivered on ready),
// watch, respawn with backoff until the budget is exhausted or the loop is
// stopped (daemon shutdown via Stop, or ctx cancellation).
func (s *kanbanSupervisor) run(ctx context.Context, ready chan<- error) {
	first := true
	for {
		err := s.spawnOnce(ctx)
		if first {
			first = false
			ready <- err
		}
		if err != nil {
			s.log.Warn("kanban sidecar: spawn failed", "error", err)
			if !s.allowRestart() {
				s.log.Error("kanban sidecar: giving up after repeated spawn failures",
					"max_attempts", s.restartMax(), "window", s.restartWindow().String())
				return
			}
			if !s.sleepBackoff(ctx) {
				return
			}
			continue
		}
		if !s.watch(ctx) {
			return // stopped: Stop (or watch's ctx path) owns the teardown
		}
		s.mu.Lock()
		deadPID := s.lastPID
		s.mu.Unlock()
		s.log.Warn("kanban sidecar: child exited unexpectedly", "pid", deadPID)
		s.clearCurrentRP()
		if !s.allowRestart() {
			s.log.Error("kanban sidecar: giving up after repeated crashes — the board gateway is down until the next daemon start",
				"max_attempts", s.restartMax(), "window", s.restartWindow().String())
			return
		}
		if !s.sleepBackoff(ctx) {
			return
		}
	}
}

// spawnOnce starts one child via llm.RuntimeProcess (pid file + durable
// spawn record + supervisor wrapper + ownership token all come with it). A
// foreign-token live pid file is adopted observed-not-OWNED and NOT killed,
// ever — the daemon-AGENTS ownership rule the voice child relies on.
// Called with no lock held (I/O).

// buildKanbanArgs assembles the child's argv. The sidecar is config-blind
// (C7): every knob arrives as a flag. Empty AllowedOrigins appends nothing
// (byte-identical spawn args pre-knob).
func buildKanbanArgs(bin string, cfg kanbanSidecarConfig) []string {
	args := []string{bin, "-daemon-socket", cfg.SocketPath, "-bind", cfg.Bind}
	if cfg.Webdir != "" {
		args = append(args, "-webdir", cfg.Webdir)
	}
	if kanbanBindNeedsAllowLAN(cfg.Bind) {
		// The bind is an explicit operator knob ([kanban].sidecar_bind);
		// pass the flag the sidecar's C7 guard demands, so a deliberate
		// LAN bind works instead of crash-looping on the refusal.
		args = append(args, "-allow-lan")
	}
	for _, origin := range cfg.AllowedOrigins {
		if trimmed := strings.TrimSpace(origin); trimmed != "" {
			args = append(args, "-allow-origin", trimmed)
		}
	}
	return args
}

func (s *kanbanSupervisor) spawnOnce(ctx context.Context) error {
	bin := s.cfg.resolveBinary()
	if bin == "" {
		return errors.New("no meept-kanban binary found (daemon-adjacent dir or $MEEPT_HOME/bin)")
	}
	args := buildKanbanArgs(bin, s.cfg)

	rc := &llm.RuntimeConfig{
		EndpointKey:  kanbanSidecarEndpointKey,
		PIDFile:      s.cfg.pidFile(),
		AutoStop:     true, // sweepable by the startup orphan sweep
		SpawnCommand: args,
	}
	rp := llm.NewRuntimeProcess(rc)
	// Start detaches from the caller ctx internally (a boot-scoped ctx must
	// not SIGKILL a long-lived child) and serializes with the endpoint
	// start lock, so no second spawn can slip into the probe window.
	if err := rp.Start(ctx, io.Discard, s.stderr); err != nil {
		return err
	}
	// A Stop that raced the spawn must not orphan the child it just
	// brought up: re-check the stop channel before publishing the handle
	// and take the fresh child straight down.
	select {
	case <-s.stopCh:
		s.mu.Lock()
		s.rp = rp
		s.lastPID = rp.PID()
		s.mu.Unlock()
		sctx, scancel := context.WithTimeout(context.Background(), s.cfg.stopGrace())
		defer scancel()
		return s.Stop(sctx)
	default:
	}
	pid := rp.PID()
	s.mu.Lock()
	s.rp = rp
	s.lastPID = pid
	s.mu.Unlock()
	s.log.Info("kanban sidecar child up", "pid", pid, "bind", s.cfg.Bind, "socket", s.cfg.SocketPath)
	return nil
}

// watch polls the child until it dies (returns true) or the loop must exit
// (returns false). A stopped/never-adopted child (rp nil) exits at once.
func (s *kanbanSupervisor) watch(ctx context.Context) bool {
	ticker := time.NewTicker(s.cfg.pollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return false
		case <-ctx.Done():
			// The run context died without an explicit Stop (e.g. the
			// reload path): take the child down gracefully ourselves,
			// bounded, so nothing outlives the daemon goroutine.
			s.shutdownChild()
			return false
		case <-ticker.C:
			s.mu.Lock()
			rp := s.rp
			s.mu.Unlock()
			if rp == nil {
				return true
			}
			if !rp.IsRunning() {
				return true
			}
		}
	}
}

// shutdownChild stops the current child under the internal grace bound.
func (s *kanbanSupervisor) shutdownChild() {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.stopGrace())
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		s.log.Debug("kanban sidecar: ctx-shutdown stop", "err", err)
	}
}

// Stop terminates the child (daemon shutdown path and the ctx-cancel path).
// Idempotent; a foreign-owned pid is refused by llm.ErrRuntimeNotOwned and
// deliberately NOT killed — the live owner reaps it, never us.
func (s *kanbanSupervisor) Stop(ctx context.Context) error {
	s.mu.Lock()
	s.stoppedLocked()
	rp := s.rp
	s.mu.Unlock()
	if rp == nil {
		return nil
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), s.cfg.stopGrace())
		defer cancel()
	}
	err := rp.Stop(ctx)
	switch {
	case err == nil:
		s.mu.Lock()
		if s.rp == rp {
			s.rp = nil
		}
		s.mu.Unlock()
		return nil
	case errors.Is(err, llm.ErrRuntimeNotOwned):
		s.log.Info("kanban sidecar: pid is owned by another live instance; not signalling it",
			"pid_file", s.cfg.pidFile())
		s.mu.Lock()
		if s.rp == rp {
			s.rp = nil
		}
		s.mu.Unlock()
		return nil
	default:
		return fmt.Errorf("kanban sidecar stop: %w", err)
	}
}

// stoppedLocked closes the stop channel. Caller holds s.mu.
func (s *kanbanSupervisor) stoppedLocked() {
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
}

// clearCurrentRP drops the handle of the exited child so a later Stop is a
// no-op. The wait goroutine inside llm already removed the pid file +
// durable record (the child is verified dead), so nothing stale survives.
func (s *kanbanSupervisor) clearCurrentRP() {
	s.mu.Lock()
	if s.rp != nil && !s.rp.IsRunning() {
		s.rp = nil
	}
	s.mu.Unlock()
}

func (s *kanbanSupervisor) restartMax() int {
	if s.cfg.RestartMax > 0 {
		return s.cfg.RestartMax
	}
	return kanbanRestartMax
}

func (s *kanbanSupervisor) restartWindow() time.Duration {
	if s.cfg.RestartWindow > 0 {
		return s.cfg.RestartWindow
	}
	return kanbanRestartWindow
}

// allowRestart records one attempt and reports whether the budget holds:
// at most restartMax attempts inside restartWindow (the give-up shape of
// the runtime manager's auto-restart gate).
func (s *kanbanSupervisor) allowRestart() bool {
	now := time.Now()
	window := s.restartWindow()
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.restarts[:0]
	for _, t := range s.restarts {
		if now.Sub(t) < window {
			kept = append(kept, t)
		}
	}
	s.restarts = append(kept, now)
	return len(s.restarts) <= s.restartMax()
}

// sleepBackoff waits the current exponential delay, aborting early (false)
// when the supervisor stops or ctx cancels. The child-alive path resets the
// ladder: a respawn that runs longer than the window starts clean.
func (s *kanbanSupervisor) sleepBackoff(ctx context.Context) bool {
	base := s.cfg.BackoffBase
	if base <= 0 {
		base = kanbanDefaultBackoffBase
	}
	maxDelay := s.cfg.BackoffMax
	if maxDelay <= 0 {
		maxDelay = kanbanDefaultBackoffMax
	}
	s.mu.Lock()
	attempts := len(s.restarts)
	s.mu.Unlock()
	delay := base
	for i := 1; i < attempts && delay < maxDelay; i++ {
		delay *= 2
	}
	if delay > maxDelay {
		delay = maxDelay
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-s.stopCh:
		return false
	case <-ctx.Done():
		s.shutdownChild()
		return false
	}
}

// kanbanBindNeedsAllowLAN reports whether the configured bind is anything
// other than loopback (the sidecar refuses it without -allow-lan per C7).
// A malformed address returns false: the sidecar's own validation reports
// the precise error and exits, which is the correct loud failure.
func kanbanBindNeedsAllowLAN(bind string) bool {
	host, _, err := net.SplitHostPort(bind)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return false
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}

// Compile-time guard: kanbanLogWriter is an io.Writer for cmd.Stderr.
var _ io.Writer = (*kanbanLogWriter)(nil)
