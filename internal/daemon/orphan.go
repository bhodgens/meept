// orphan.go implements the startup orphan-runtime sweep.
//
// A local LLM runtime is a direct child of the meept process that spawned it,
// in its own process group (Setpgid, RuntimeProcess.Start). macOS has no
// parent-death signal, so a daemon that dies hard (SIGKILL, panic, session
// teardown) cannot stop its children: they are re-parented to init and keep
// running, holding a full model in memory and the endpoint port. The ownership
// guard in internal/llm then refuses to kill a runtime an earlier boot spawned
// (foreign instance token => observed, not owned), so no later boot would ever
// reap them either.
//
// This sweep closes that hole before the daemon starts its own runtimes: every
// configured endpoint with auto_stop_on_exit=true is matched against the
// process table, and a match whose parent is init (ppid==1, so the spawning
// meept process is gone) is stopped. Detection and signalling live in
// internal/llm (RuntimeManager.SweepOrphanRuntimes) because the endpoint
// configs and the PID-file format live there; this file is the daemon hook.
//
// Ordering requirement: AFTER components are constructed (the runtime manager
// owns the endpoint configs) and BEFORE StartAll (the leftover process holds
// the endpoint port, and the duplicate-spawn pre-check would refuse the spawn).
// Windows is not supported (no ps): the scan fails, the sweep skips, nothing is
// killed.
package daemon

import "time"

// orphanTermGrace bounds the SIGTERM grace period before a leftover runtime is
// SIGKILLed: long enough for llama-server / mlx_lm to exit cleanly, short
// enough not to hold up boot.
const orphanTermGrace = 2 * time.Second

// StartupOrphanSweep reaps runtime processes left behind by a dead meept
// generation. Best-effort: failures are logged, never fatal, and the sweep is a
// no-op when no runtime manager exists on this daemon.
func (d *Daemon) StartupOrphanSweep() {
	if d.components == nil || d.components.ContainerManager == nil {
		return
	}
	reaped := d.components.ContainerManager.SweepOrphanRuntimes(orphanTermGrace)
	if len(reaped) > 0 {
		d.logger.Warn("daemon: reaped orphaned runtime processes", "pids", reaped)
		return
	}
	d.logger.Info("daemon: orphan sweep complete", "reaped", 0)
}
