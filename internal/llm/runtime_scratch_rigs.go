package llm

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Scratch-rig discovery for the daemon's boot orphan sweep (issue: 2026-09-22
// orphan audit). The e2e and bench harnesses run daemons with their own
// MEEPT_HOME under the OS temp dir (${TMPDIR}/meept-e2e.XXXX,
// meept-bench-async*). A rig daemon that dies hard leaves its runtimes
// re-parented to init, and the runtime spawn records that would identify them
// live in the RIG's run dir — invisible to a sweep that scans only
// config.MeeptPath("run"). Until this helper existed, such a leftover could
// never be reaped by any production boot: it held a full model in RAM and an
// ephemeral port forever.
//
// The rig daemon itself is long gone in the leak scenario, so there is no
// liveness to ask; the run dir and its spawn records are the only witness.
// The stale-record sweep (sweepStaleSpawnRecords) applies its own guards to
// every record found here: pid alive, re-parented to init, command line equal
// to the recorded argv, record older than the stale bound. A LIVE rig's
// records are young and its daemons own their runtimes (ppid != 1), so a live
// rig is untouched by construction.

// scratchRigDirs lists run dirs of meept scratch rigs under the OS temp dir.
// best-effort: an unreadable temp dir yields no entries, never an error — the
// sweep must not fail because housekeeping is impossible.
func scratchRigDirs() []string {
	var dirs []string
	for _, tmp := range tempDirs() {
		matches, err := filepath.Glob(filepath.Join(tmp, "meept-e2e.*"))
		if err != nil {
			continue
		}
		for _, dir := range matches {
			dirs = append(dirs, filepath.Join(dir, "home", ".meept", "run"))
		}
		matches, err = filepath.Glob(filepath.Join(tmp, "meept-bench-async*"))
		if err != nil {
			continue
		}
		for _, dir := range matches {
			dirs = append(dirs, filepath.Join(dir, "home", ".meept", "run"))
		}
	}
	return dirs
}

// tempDirs lists candidate temp roots: TMPDIR when set (macOS per-user
// /var/folders/...), then /tmp. Duplicates collapse. os.TempDir is NOT used
// directly because the rig layout check below wants every candidate root the
// harnesses may have used.
func tempDirs() []string {
	seen := make(map[string]bool)
	var roots []string
	add := func(dir string) {
		dir = strings.TrimRight(dir, "/")
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		roots = append(roots, dir)
	}
	add(os.TempDir())
	add("/tmp")
	add("/private/tmp")
	return roots
}

// CollectScratchRigSpawnRecords scans the given run dirs for parseable spawn
// records. A missing dir is skipped (a rig cleaned up properly); a scan of a
// present dir that fails is skipped the same way — the sweep's contract is
// best-effort over every source, never one bad dir aborting the rest.
func CollectScratchRigSpawnRecords(dirs []string) ([]SpawnRecord, error) {
	var all []SpawnRecord
	for _, dir := range dirs {
		records, err := ScanSpawnRecords(dir)
		if err != nil {
			continue
		}
		all = append(all, records...)
	}
	return all, nil
}

// ScratchRecordStaleAfter bounds how old a scratch-rig spawn record may be
// before a live, init-reparented, argv-identical runtime behind it is treated
// as a leftover of a dead rig. Rigs are short-lived by design: a scratch
// runtime that outlives its rig by this long is a leak, not a fixture.
// Deliberately much shorter than the production SpawnRecordStaleAfter (6h):
// no legitimate e2e or bench runtime serves a rig for hours after the rig's
// daemon went away.
const ScratchRecordStaleAfter = 30 * time.Minute

// ListScratchRigRunDirs exposes the scratch-rig run-dir discovery for callers
// outside this package (the daemon's boot sweep).
func ListScratchRigRunDirs() []string {
	return scratchRigDirs()
}
