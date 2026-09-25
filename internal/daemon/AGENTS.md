# internal/daemon/AGENTS.md

Guidance for AI agents working in `internal/daemon/` and `internal/llm`
runtime lifecycle code. Referenced from the root AGENTS.md; full runtime
lifecycle prose moved here from the root file. Update both in the same
commit per the root maintenance rule.

## Local runtime lifecycle: spawn guards and orphan reaping

Local LLM runtimes (`internal/llm`) are long-lived children of the daemon. Each
rule below was added after a real leak; changing any of them requires a
replacement mechanism, not just a deletion:

- **No spawn into a served endpoint.** `RuntimeProcess.Start` probes the
  endpoint address when `spawn_command` declares that port and refuses when
  something already listens there. Keep the refusal: `mlx_lm server` does not
  exit after a failed bind — it stays alive with the model loaded and no socket
  while the foreign listener answers its `/health`, so the spawn "succeeds" as a
  healthy-looking runtime that serves nothing. The check applies only to a spawn
  command that declares the port, so wrappers and test harnesses are unaffected.
- **Health requires a live process, and the run outlives its caller.**
  `HealthChecker.SetProcessAliveProbe` binds each endpoint's checker to its own
  `RuntimeProcess`; a dead child is unhealthy whatever the endpoint returns. The
  check run detaches from the caller's context and re-arms whenever no run is
  active: every caller passes a short-lived context (the daemon cancels its boot
  context when `StartAll` returns), so binding the run to it silently ended
  health monitoring seconds after boot.
- **Ownership forbids killing; the sweep permits it.**
  `RuntimeProcess.Stop` never stops a runtime whose PID file carries another
  instance's token (adopted observed-not-owned — that guard is what keeps a CLI
  or eval process from killing the daemon's server) and returns
  `ErrRuntimeNotOwned`, so no surface reports a stop that did not happen.
  `StopAsOperator` is the explicit operator override used by
  `meept runtime stop`, and it verifies the pid's identity before signalling.
  The startup sweep (`RuntimeManager.SweepOrphanRuntimes`, driven by
  `Daemon.StartupOrphanSweep`) is the only path that reaps a leftover, and only
  when all hold: `ppid == 1` (spawner gone), the command line matches the
  endpoint's `spawn_command` or its durable spawn record, the endpoint is
  sweepable under `auto_stop_on_exit` (absent means true), and no live recorded
  owner vetoes it. Every pid is re-validated against the process table
  immediately before it is signalled. Do not widen the sweep to untagged or
  unrelated processes.
- **Sweep before spawn.** `StartupOrphanSweep` runs after components exist and
  before `ContainerManager.StartAll`. Moving it later makes the duplicate-spawn
  guard refuse the spawn of the endpoint the sweep has not freed yet.

**Termination is two-sided; only one side is automatic.** Graceful shutdown
stops owned runtimes: `Daemon.Stop` calls `ContainerManager.StopAll(ctx)`
(`internal/daemon/daemon.go:1904`), which stops every endpoint whose
`auto_stop_on_exit` is true. SIGINT/SIGTERM therefore leaves no children
behind - verified 2026-09-12, killing a scratch daemon took its `mlx_lm` and
`llama-server` children down with it.

**Classifier runtime health gates startup (F-D8).** With
`orchestrator.classifier_boot_fail_fast` true (the default), the daemon —
after `ContainerManager.StartAll` launches in `Run` — waits for every LOCAL
SPAWNED runtime in the classifier chain (models.json5 `classifier_model` /
`classifier` alias members whose provider carries a lifecycle block on a
loopback baseURL) to answer `/health` within its configured window
(spawn timeout + unhealthy_threshold x interval). A local classifier still
unhealthy after the window is a platform failure: the gate logs an ERROR
naming endpoint, model path and spawn command, shuts down cleanly, and `Run`
returns the fatal error so `meept-daemon` exits non-zero. Cloud-only chains
and `classifier_boot_fail_fast: false` skip the gate. Keep the gate AFTER the
StartAll goroutine: StartAll arms the health checkers, and a checker that
was never started never reports healthy.

A hard kill no longer leaks. Each runtime is spawned under a supervisor (the
daemon binary in a hidden mode, `meept-daemon --supervise-parent <pid> -- <argv>`)
that kills the runtime's process group - SIGTERM, then SIGKILL after 10s - once
its parent disappears, and exits with the runtime. macOS has no parent-death
signal, so the supervisor uses a parent-death pipe plus a 2s pid poll. The
runtime keeps its original argv and pid: the sweep's command-line match and the
pid-file ownership token still identify it, never the wrapper. Per-endpoint
escape hatch: `supervise: false`. Cleanup after a hard kill, in order:

1. `kill <daemon-pid>` first (graceful) and re-check; do not kill children of
   a live daemon, its restart policy respawns them.
2. `pgrep -fl "llama-server|mlx_lm"` and
   `lsof -nP -iTCP:<port> -sTCP:LISTEN` to find what survived.
3. Kill each leftover child explicitly and confirm the port is free.

The boot sweep (`StartupOrphanSweep`) remains the backstop: it reaps what
self-termination cannot - a runtime whose supervisor was itself SIGKILLed, and
anything left by an older build. Windows has no `ps`, so it gets no sweep
(documented gap).
