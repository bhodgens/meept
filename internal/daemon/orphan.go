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
// Two sources feed the match, and the durable spawn records are the
// config-independent one. A runtime writes a SpawnRecord beside its PID file at
// spawn time; the record carries the expanded spawn command, so a leftover is
// still identifiable after the endpoint's config drifts — an unmounted model
// volume or a renamed provider makes the config fail ValidateAndNormalize, it is
// never registered, and a config-only sweep would never see the leftover. The
// daemon therefore scans the run dir (config.MeeptPath("run")) for records and
// passes them alongside the registered endpoint configs.
//
// This sweep closes that hole before the daemon starts its own runtimes: every
// sweepable endpoint config and every sweepable record is matched against the
// process table, and a match whose parent is init (ppid==1, so the spawning
// meept process is gone) is stopped. Detection and signalling live in
// internal/llm (RuntimeManager.SweepOrphanRuntimes) because the endpoint
// configs, the records, and the PID-file format live there; this file is the
// daemon hook.
//
// Ordering requirement: AFTER components are constructed (the runtime manager
// owns the endpoint configs) and BEFORE StartAll (the leftover process holds
// the endpoint port, and the duplicate-spawn pre-check would refuse the spawn).
// Windows is not supported (no ps): the scan fails, the sweep skips, nothing is
// killed.
package daemon

import (
	"os"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

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

	// Durable spawn records: the config-independent source. A record survives a
	// config that no longer validates (unmounted model volume, renamed
	// provider), which is exactly when a leftover would otherwise be
	// unreapable. A scan failure is a debug note, never fatal: the sweep still
	// runs on the registered configs.
	records, err := llm.ScanSpawnRecords(config.MeeptPath("run"))
	if err != nil {
		d.logger.Debug("daemon: spawn-record scan unavailable", "error", err)
		records = nil
	}
	d.logger.Debug("daemon: orphan sweep considering spawn records", "records", len(records))

	reaped := d.components.ContainerManager.SweepOrphanRuntimes(orphanTermGrace, records)
	if len(reaped) > 0 {
		d.logger.Warn("daemon: reaped orphaned runtime processes", "pids", reaped)
	} else {
		d.logger.Info("daemon: orphan sweep complete", "reaped", 0)
	}

	// Age-keyed second pass (issue #54): a leftover whose daemon was
	// SIGKILLed can survive the ppid==1 sweep above through the ownership
	// guard (observed-not-owned). A spawn record older than
	// llm.SpawnRecordStaleAfter whose pid is STILL alive, re-parented to
	// init, and command-line-identical to the record is exactly such a
	// leftover, so it is reaped here — after the existing sweep, and only
	// through the guarded stale-record path. Ages are read BEFORE the call
	// because a reaped record's PID file is removed by it.
	staleReaped := llm.SweepStaleSpawnRecords(records, llm.SpawnRecordStaleAfter, time.Now)

	// Scratch-rig records (2026-09-22 orphan audit): e2e/bench rigs run
	// daemons with their own MEEPT_HOME under the OS temp dir, so a rig
	// daemon that died hard left runtimes whose spawn records no
	// production-home sweep could ever see. Scan those run dirs and run the
	// SAME guarded stale-record path over them: a record is reaped only when
	// its pid is alive, re-parented to init, command-line-identical to the
	// record, and older than the stale bound. A live rig's daemons own their
	// runtimes (ppid != 1) and young records are skipped by the age gate, so
	// a rig mid-run is untouched by construction.
	rigRecords, err := llm.CollectScratchRigSpawnRecords(scratchRigRunDirs())
	if err != nil {
		d.logger.Debug("daemon: scratch-rig record scan unavailable", "error", err)
	}
	if len(rigRecords) > 0 {
		d.logger.Debug("daemon: orphan sweep considering scratch-rig spawn records", "records", len(rigRecords))
	}
	rigReaped := llm.SweepStaleSpawnRecords(rigRecords, llm.ScratchRecordStaleAfter, time.Now)

	if len(staleReaped) > 0 || len(rigReaped) > 0 {
		ageOf := make(map[int]time.Duration, len(records)+len(rigRecords))
		for _, rec := range append(records, rigRecords...) {
			if info, err := os.Stat(rec.PIDFile); err == nil {
				ageOf[rec.PID] = time.Since(info.ModTime())
			}
		}
		for _, pid := range staleReaped {
			d.logger.Warn("daemon: reaped stale-record orphan runtime",
				"pid", pid, "record_age", ageOf[pid])
		}
		for _, pid := range rigReaped {
			d.logger.Warn("daemon: reaped scratch-rig orphan runtime",
				"pid", pid, "record_age", ageOf[pid])
		}
	}
}
