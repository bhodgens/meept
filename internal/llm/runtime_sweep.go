package llm

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Startup orphan sweep.
//
// A local runtime is a direct child of the meept process that spawned it, in
// its own process group (Setpgid, see RuntimeProcess.Start). macOS has no
// parent-death signal, so when a daemon dies hard (SIGKILL, panic, session
// teardown) its children are re-parented to init and keep running: they hold a
// full model in memory and the endpoint port. The ownership guard in
// runtime_process.go then prevents every LATER boot from stopping them (a
// foreign instance token is adopted observed-not-owned), so nothing ever reaps
// them — the leak is permanent until an operator kills the pid by hand.
//
// This sweep closes that hole with the two facts the daemon does have: the
// configured spawn command of every endpoint, and the process table. A process
// whose parent is init (ppid==1, so the meept process that spawned it is gone)
// and whose command line is exactly an endpoint's spawn command is a leftover
// of a dead generation. It is stopped before the current boot spawns its own
// runtimes, because it holds the endpoint port and the duplicate-spawn
// pre-check would otherwise refuse the spawn.
//
// Two guards keep the sweep from killing something it should not:
//   - only endpoints with auto_stop_on_exit=true are swept: a runtime the user
//     configured to outlive the daemon is left alone;
//   - a live owner recorded for the endpoint (its PID file names a different,
//     running process) vetoes the kill, so a user-managed server with the same
//     command line survives.
//
// The reap itself re-reads the process table between SIGTERM and SIGKILL: a pid
// whose entry changed (reused pid) is never signalled, and a pid is only
// reported as reaped once it is verifiably gone.
//
// Windows has no ps: the scan fails, the sweep skips, nothing is killed
// (documented platform gap, same posture as the rest of the lifecycle code).

// reapKillTimeout bounds the wait for a SIGKILLed pid to disappear.
const reapKillTimeout = 2 * time.Second

// OrphanRuntime is one runtime process left behind by a meept process that no
// longer exists.
type OrphanRuntime struct {
	EndpointKey string
	PID         int
	Command     string
}

// RuntimeProcInfo is one row of the process table used for orphan detection.
type RuntimeProcInfo struct {
	PID     int
	PPID    int
	Command string
}

// RuntimeProcLister lists the process table (test seam over ps).
type RuntimeProcLister func() ([]RuntimeProcInfo, error)

// ListRuntimeProcesses reads the process table with ps. The error is returned
// rather than swallowed so the sweep can skip instead of guessing.
func ListRuntimeProcesses() ([]RuntimeProcInfo, error) {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,command=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps scan failed: %w", err)
	}
	var procs []RuntimeProcInfo
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		if pidErr != nil {
			continue
		}
		ppid, ppidErr := strconv.Atoi(fields[1])
		if ppidErr != nil {
			continue
		}
		procs = append(procs, RuntimeProcInfo{
			PID:     pid,
			PPID:    ppid,
			Command: strings.Join(fields[2:], " "),
		})
	}
	return procs, nil
}

// FindOrphanRuntimes returns runtime processes that a dead meept process left
// behind, matched against endpoint configs only. Report-only: the caller decides
// whether to signal them. A pid matched by more than one endpoint config is
// reported once.
//
// Records exist so detection survives config drift; callers that can reach the
// durable records (the daemon and `meept doctor`) should use
// FindOrphanRuntimesWithRecords instead. This wrapper stays for the callers and
// tests that match on configs alone.
func FindOrphanRuntimes(cfgs []*RuntimeConfig, list RuntimeProcLister) ([]OrphanRuntime, error) {
	return FindOrphanRuntimesWithRecords(cfgs, nil, list)
}

// FindOrphanRuntimesWithRecords returns leftover runtime processes matched
// against BOTH the endpoint configs and the durable spawn records. A process is
// an orphan when its parent is init (ppid==1, so the meept process that spawned
// it is gone) and its command line is exactly a sweepable config's spawn command
// or a sweepable record's argv. A pid matched by both a config and a record (or
// by two of either) is reported once, with the config's endpoint key when a
// config matched first.
//
// Records make detection independent of the current config: an endpoint whose
// model volume is unmounted or whose provider was renamed no longer validates,
// so no config reaches this scan, but its recorded spawn command still matches
// the leftover it left behind.
func FindOrphanRuntimesWithRecords(cfgs []*RuntimeConfig, records []SpawnRecord, list RuntimeProcLister) ([]OrphanRuntime, error) {
	procs, err := list()
	if err != nil {
		return nil, err
	}
	// Endpoints whose runtime explicitly opted OUT of daemon-driven stopping.
	// An endpoint is spared when EITHER its current config or its durable record
	// says auto_stop=false, so neither source can outvote the other:
	//   - a config's auto_stop_on_exit=false is the operator's CURRENT intent
	//     (audit finding F57: a STALE record with AutoStop=true must not outvote
	//     an explicit false);
	//   - a record's AutoStop=false marks a runtime that was NOT spawned as a
	//     daemon-managed child (an out-of-daemon `meept runtime start`, whose
	//     record the CLI writes with AutoStop=false) — audit finding F58: the
	//     boot sweep must not reap a runtime the operator deliberately started.
	spared := endpointSparedSet(cfgs, records)
	var orphans []OrphanRuntime
	seen := make(map[int]struct{})
	add := func(endpointKey string, p RuntimeProcInfo) {
		if _, dup := seen[p.PID]; dup {
			return
		}
		seen[p.PID] = struct{}{}
		orphans = append(orphans, OrphanRuntime{
			EndpointKey: endpointKey,
			PID:         p.PID,
			Command:     p.Command,
		})
	}
	for _, cfg := range cfgs {
		if !sweepableEndpoint(cfg) || endpointSpared(spared, cfg.PIDFile, cfg.EndpointKey) {
			continue
		}
		for _, p := range procs {
			if p.PPID != 1 || !matchesSpawnCommand(p.Command, cfg.SpawnCommand) {
				continue
			}
			add(cfg.EndpointKey, p)
		}
	}
	for _, rec := range records {
		if !sweepableRecord(rec) || endpointSpared(spared, rec.PIDFile, rec.EndpointKey) {
			continue
		}
		for _, p := range procs {
			if p.PPID != 1 || !matchesSpawnCommand(p.Command, rec.Argv) {
				continue
			}
			add(rec.EndpointKey, p)
		}
	}
	return orphans, nil
}

// endpointSparedSet collects the PID-file paths and endpoint keys for which
// either a config or a record says auto_stop=false — the endpoints an orphan
// match must be spared for. See FindOrphanRuntimesWithRecords.
func endpointSparedSet(cfgs []*RuntimeConfig, records []SpawnRecord) map[string]bool {
	spared := make(map[string]bool)
	for _, cfg := range cfgs {
		if cfg == nil || cfg.AutoStop {
			continue
		}
		if cfg.PIDFile != "" {
			spared[cfg.PIDFile] = true
		}
		if cfg.EndpointKey != "" {
			spared[cfg.EndpointKey] = true
		}
	}
	for _, rec := range records {
		if rec.AutoStop {
			continue
		}
		// A record's auto_stop=false is the operator's CURRENT intent only
		// while the runtime that record names is still alive. Once its pid is
		// verifiably gone the record is a leftover of a finished out-of-daemon
		// `meept runtime start`, and sparing the endpoint for it is permanent:
		// nothing else removes the record (audit finding F78).
		if recordSpawnPIDGone(rec) {
			continue
		}
		if rec.PIDFile != "" {
			spared[rec.PIDFile] = true
		}
		if rec.EndpointKey != "" {
			spared[rec.EndpointKey] = true
		}
	}
	return spared
}

// recordSpawnPIDGone reports whether a durable record names a spawn pid that is
// verifiably gone.
//
// The record's auto_stop=false marks a runtime the operator started out of the
// daemon (`meept runtime start` writes it that way), and the boot sweep spares
// its endpoint so it is never reaped (finding F58). That sparing used to be
// unconditional and therefore PERMANENT: the runtime dies (SIGKILL, a reboot)
// and nothing removes the record, so every later boot spared the endpoint. The
// genuine leftover of an earlier generation then survived, held the endpoint
// port, and the daemon's own spawn was refused by the duplicate-spawn pre-check
// — leaving the install with no runtime at all (audit finding F78).
//
// A record with no recorded pid (PID == 0, written by a build older than the pid
// field) cannot be judged, so it keeps its old meaning and still spares.
func recordSpawnPIDGone(rec SpawnRecord) bool {
	return rec.PID > 0 && !processAlive(rec.PID)
}

// PruneStaleOperatorRecords removes the durable records whose auto_stop=false
// intent has expired: an operator-started runtime whose recorded pid is
// verifiably gone (see recordSpawnPIDGone). Only sparing records are pruned —
// an auto_stop=true record is sweepable MATCHING evidence even after the pid it
// names has exited (a later leftover running the same command line is still
// identified by it), and it never spares anything anyway. A record with no pid
// file is dropped but unlinks nothing. Best-effort: a removal failure is
// diagnostic only. Returns the surviving records.
func PruneStaleOperatorRecords(records []SpawnRecord) []SpawnRecord {
	var kept []SpawnRecord
	for _, rec := range records {
		if !rec.AutoStop && recordSpawnPIDGone(rec) {
			RemoveSpawnRecord(rec.PIDFile)
			slog.Debug("stale operator spawn record pruned",
				"pid_file", rec.PIDFile, "record_pid", rec.PID)
			continue
		}
		kept = append(kept, rec)
	}
	return kept
}

// endpointSpared reports whether pidFile or endpointKey is in the spared set.
func endpointSpared(spared map[string]bool, pidFile, endpointKey string) bool {
	if pidFile != "" && spared[pidFile] {
		return true
	}
	return endpointKey != "" && spared[endpointKey]
}

// OrphanRuntimesFromConfigs reports leftover runtime processes for a set of
// runtime configs (used by `meept doctor`, which has no RuntimeManager).
func OrphanRuntimesFromConfigs(cfgs []*RuntimeConfig) ([]OrphanRuntime, error) {
	return OrphanRuntimesFromConfigsAndRecords(cfgs, nil)
}

// OrphanRuntimesFromConfigsAndRecords reports leftover runtime processes for a
// set of runtime configs AND durable spawn records with the real process scan.
// Records let `meept doctor` still see a leftover whose endpoint config no
// longer validates (unmounted model volume, renamed provider).
func OrphanRuntimesFromConfigsAndRecords(cfgs []*RuntimeConfig, records []SpawnRecord) ([]OrphanRuntime, error) {
	return findOrphanRuntimesFiltered(cfgs, records, nil)
}

// findOrphanRuntimesFiltered is the shared detection body: find leftovers with
// the given scan (nil = the real ps scan) and apply the SAME live-owner veto the
// daemon's sweep applies, so the two surfaces cannot disagree about the same
// process (audit finding F61): `meept doctor --fix` used to reap a pid the
// daemon's boot sweep would have left alone because another endpoint records a
// live owner for it.
func findOrphanRuntimesFiltered(cfgs []*RuntimeConfig, records []SpawnRecord, list RuntimeProcLister) ([]OrphanRuntime, error) {
	if list == nil {
		list = ListRuntimeProcesses
	}
	orphans, err := FindOrphanRuntimesWithRecords(cfgs, records, list)
	if err != nil {
		return nil, err
	}
	return FilterLiveOwned(cfgs, orphans), nil
}

// RuntimeHasLiveOwner reports whether any endpoint config whose spawn command
// matches command records a live owner other than pid. Such an owner is a live
// process meept did not leave behind, so the matched process must not be
// signalled. The check spans EVERY matching config, not just the endpoint the
// scan attributed the pid to: two providers can share one spawn command line
// with different PID files (endpoint keys do not normalize localhost against
// 127.0.0.1), so attribution is ambiguous and any recorded live owner vetoes.
//
// SCOPE (audit finding F78, low): the veto deliberately includes endpoints whose
// config says auto_stop_on_exit=false — the unmanaged, operator-owned endpoints.
// That is the whole point of the predicate: an auto_stop=false endpoint IS the
// "user-managed server with the same command line" this veto exists to protect,
// and (because attribution of a shared command line is ambiguous) a live pid
// file that is not the detected leftover is the only evidence available that the
// process belongs to somebody who did not ask meept to reap it. Narrowing the
// veto to auto_stop=true configs would make the sweep kill an operator-owned
// server whenever a managed endpoint shares its command line — the F58/F61
// failure mode, not a fix. The known cost is accepted: an unmanaged endpoint
// can keep a managed endpoint's genuine leftover alive, and the sweep reports
// that ("leaving process alone — an endpoint with this command records a
// different live owner") so the operator can stop it. The record half of the
// same rule is narrower on purpose: a record only spares when the pid it names
// is still alive (see endpointSparedSet / recordSpawnPIDGone).
//
// Exported so the daemon's sweep (RuntimeManager.SweepOrphanRuntimes) and
// `meept doctor --fix` share ONE predicate and cannot make different decisions
// about the same process (audit finding F61).
func RuntimeHasLiveOwner(cfgs []*RuntimeConfig, command string, pid int) bool {
	for _, cfg := range cfgs {
		if cfg == nil || cfg.PIDFile == "" || !matchesSpawnCommand(command, cfg.SpawnCommand) {
			continue
		}
		filePID, err := ParsePIDFile(cfg.PIDFile)
		if err != nil || filePID == pid {
			continue
		}
		if processAlive(filePID) {
			return true
		}
	}
	return false
}

// FilterLiveOwned returns the orphans that are safe to reap: every target whose
// command line does not match an endpoint config recording a different live
// owner. Shared so `meept doctor --fix` applies exactly the daemon's veto.
func FilterLiveOwned(cfgs []*RuntimeConfig, orphans []OrphanRuntime) []OrphanRuntime {
	var out []OrphanRuntime
	for _, o := range orphans {
		if RuntimeHasLiveOwner(cfgs, o.Command, o.PID) {
			continue
		}
		out = append(out, o)
	}
	return out
}

// ReapOrphanRuntimesFromConfigs finds and reaps leftovers for the given configs
// with the real process scan and process-group signals. It returns the orphans
// it considered and the pids confirmed gone — callers must report the confirmed
// count, never the candidate count.
func ReapOrphanRuntimesFromConfigs(cfgs []*RuntimeConfig, waitAfterTerm time.Duration, log *slog.Logger) ([]OrphanRuntime, []int) {
	return ReapOrphanRuntimesFromConfigsAndRecords(cfgs, nil, waitAfterTerm, log)
}

// ReapOrphanRuntimesFromConfigsAndRecords finds and reaps leftovers for the
// given configs and durable spawn records with the real process scan and
// process-group signals. Records are the config-independent source, so a
// leftover survives neither a drifted config nor an invalid one. It returns the
// orphans it considered and the pids confirmed gone — callers must report the
// confirmed count, never the candidate count.
func ReapOrphanRuntimesFromConfigsAndRecords(cfgs []*RuntimeConfig, records []SpawnRecord, waitAfterTerm time.Duration, log *slog.Logger) ([]OrphanRuntime, []int) {
	return reapOrphanRuntimes(cfgs, records, waitAfterTerm, nil, nil, log)
}

// reapOrphanRuntimes is ReapOrphanRuntimesFromConfigsAndRecords with the
// process-scan and signal seams exposed (nil = the real ps scan and the real
// process-group signals), mirroring ReapRuntimeProcesses so the whole reap path
// is testable without signalling a live runtime.
func reapOrphanRuntimes(cfgs []*RuntimeConfig, records []SpawnRecord, waitAfterTerm time.Duration, list RuntimeProcLister, signal runtimeSignaler, log *slog.Logger) ([]OrphanRuntime, []int) {
	records = PruneStaleOperatorRecords(records)
	orphans, err := findOrphanRuntimesFiltered(cfgs, records, list)
	if err != nil {
		if log != nil {
			log.Debug("orphan sweep: process scan unavailable", "error", err)
		}
		return nil, nil
	}
	confirmed := ReapRuntimeProcesses(orphans, waitAfterTerm, list, signal, log)
	// A reaped runtime must leave no handle behind: this path (the CLI reaper
	// and `meept doctor --fix`) used to remove neither, so the pid file kept
	// naming a dead pid and the durable record kept sparing its endpoint on
	// every later boot (audit finding F78). The manager's own sweep does the
	// same for its confirmed pids (removePIDFileForPid).
	RemoveRuntimeHandlesForPids(cfgs, records, confirmed)
	return orphans, confirmed
}

// RemoveRuntimeHandlesForPids removes the PID file and durable spawn record of
// every handle that still names one of pids — the pids a reap CONFIRMED gone
// (never a candidate). A config carries no pid, so the PID file itself is the
// only link; a durable record carries the pid it spawned, which is the link
// that survives config drift. Best-effort: a removal failure is diagnostic
// only, and a handle naming a pid that is NOT in pids is left alone (a
// concurrent Start may have rewritten it for a fresh runtime).
//
// The pids arrive from a SNAPSHOT taken earlier in the sweep (detection →
// reap), and a health-driven restart or a CLI `runtime start` can have
// installed a REPLACEMENT runtime behind the same PID file path in that
// window (audit finding F26). removeHandle therefore re-reads the PID file
// immediately before each removal and skips when its content no longer names
// the snapshot pid — mirroring ReapRuntimeProcesses' re-validation before the
// signal, on the handle-removal side. The .cmd record removal follows the
// same check: a record whose pid differs from the snapshot belongs to the
// replacement.
func RemoveRuntimeHandlesForPids(cfgs []*RuntimeConfig, records []SpawnRecord, pids []int) {
	if len(pids) == 0 {
		return
	}
	gone := make(map[int]struct{}, len(pids))
	for _, pid := range pids {
		gone[pid] = struct{}{}
	}
	removed := make(map[string]struct{})
	removeHandle := func(pidFile string, snapshotPID int) {
		if pidFile == "" {
			return
		}
		if _, dup := removed[pidFile]; dup {
			return
		}
		// Re-validate against the CURRENT PID file right before removal:
		// a replacement runtime installed since the snapshot (different
		// pid behind the same path) must keep its handles.
		current, err := ParsePIDFile(pidFile)
		if err == nil && current != snapshotPID {
			slog.Debug("orphan reap: pid file was rewritten since the snapshot; keeping the replacement's handles",
				"pid_file", pidFile, "snapshot_pid", snapshotPID, "current_pid", current)
			return
		}
		removed[pidFile] = struct{}{}
		if rmErr := os.Remove(pidFile); rmErr != nil && !os.IsNotExist(rmErr) {
			slog.Debug("orphan reap: pid file removal failed", "pid_file", pidFile, "error", rmErr)
		}
		// Same guard for the durable record: a record rewritten for a
		// replacement runtime (different pid) is the replacement's, not
		// the reaped runtime's.
		if rec, recErr := ReadSpawnRecord(pidFile); recErr == nil && rec.PID != 0 && rec.PID != snapshotPID {
			slog.Debug("orphan reap: spawn record was rewritten since the snapshot; keeping the replacement's record",
				"pid_file", pidFile, "snapshot_pid", snapshotPID, "record_pid", rec.PID)
			return
		}
		RemoveSpawnRecord(pidFile)
	}
	for _, rec := range records {
		if _, ok := gone[rec.PID]; ok {
			removeHandle(rec.PIDFile, rec.PID)
		}
	}
	for _, cfg := range cfgs {
		if cfg == nil || cfg.PIDFile == "" {
			continue
		}
		filePID, parseErr := ParsePIDFile(cfg.PIDFile)
		if parseErr != nil {
			continue
		}
		if _, ok := gone[filePID]; ok {
			removeHandle(cfg.PIDFile, filePID)
		}
	}
}

// spawnRecordStaleAfter bounds how old a durable spawn record may be before
// its pid — if it is STILL alive — is treated as a leftover of a dead meept
// generation rather than a runtime somebody owns. No legitimate e2e or dev
// runtime lives this long. The daemon's own live runtimes are excluded because
// their daemon re-validates ownership at ITS boot (RuntimeManager's
// live-owner veto in SweepOrphanRuntimes) and while it runs it is the recorded
// parent (ppid != 1), which the stale sweep requires.
const spawnRecordStaleAfter = 6 * time.Hour

// SpawnRecordStaleAfter is the exported read of spawnRecordStaleAfter for the
// daemon call site (internal/daemon/orphan.go) — the constant itself stays
// unexported so the sweep's own tests are the only other consumer.
const SpawnRecordStaleAfter = spawnRecordStaleAfter

// SweepStaleSpawnRecords reaps runtimes that the ppid==1 match of the boot
// orphan sweep cannot see through the ownership guard: a record older than
// maxAge whose pid is still ALIVE, re-parented to init, and whose command line
// matches the record's argv (issue #54 — two orphaned llama-servers survived
// --keep runs whose daemons were SIGKILLed). The PID file's ModTime is the
// record age proxy.
//
// Guards, in order: a record at or under maxAge is skipped; a dead pid is
// skipped (the existing orphan sweep and record pruning own dead-pid
// cleanup — double-processing here would race a replacement spawn);
// a process whose parent is not init is skipped (a live meept daemon owns
// it); a command line that does not match the record's argv is skipped
// (matchesSpawnCommand, the same identity-revalidation helper the boot sweep
// matches with). A surviving pid keeps its handles.
//
// maxAge is a parameter so tests can pin the boundary; production callers
// pass spawnRecordStaleAfter. now is likewise a test seam. Returns the pids
// confirmed gone. Best-effort, like every sweep: never returns an error.
func SweepStaleSpawnRecords(records []SpawnRecord, maxAge time.Duration, now func() time.Time) []int {
	return sweepStaleSpawnRecords(records, maxAge, now, orphanTermGraceDefault, nil, nil, slog.Default())
}

// orphanTermGraceDefault bounds the SIGTERM grace period in
// SweepStaleSpawnRecords (internal/daemon/orphan.go passes its own
// orphanTermGrace to the manager sweeps; this sweep has no manager, so the
// value is mirrored here).
const orphanTermGraceDefault = 2 * time.Second

// sweepStaleSpawnRecords is the seam-complete core of SweepStaleSpawnRecords.
func sweepStaleSpawnRecords(records []SpawnRecord, maxAge time.Duration, now func() time.Time,
	waitAfterTerm time.Duration, list RuntimeProcLister, signal runtimeSignaler, log *slog.Logger) []int {

	if now == nil {
		now = time.Now
	}
	if len(records) == 0 {
		return nil
	}

	// Age gate first: the PID file's ModTime is the record age proxy. A
	// missing PID file is not stale-by-mtime — without an age the record is
	// left to the existing sweep paths.
	var stale []SpawnRecord
	for _, rec := range records {
		if rec.PIDFile == "" {
			continue
		}
		info, err := os.Stat(rec.PIDFile)
		if err != nil {
			log.Debug("stale-record sweep: pid file unstattable; skipping", "pid_file", rec.PIDFile, "error", err)
			continue
		}
		if age := now().Sub(info.ModTime()); age <= maxAge {
			continue
		}
		stale = append(stale, rec)
	}
	if len(stale) == 0 {
		return nil
	}

	// Identity re-validation with the SAME helper the boot sweep matches
	// with: the pid must be alive, re-parented to init, and its command
	// line must equal the record's argv. Anything else is left alone — a
	// dead pid belongs to the existing cleanup paths, a ppid != 1 process
	// to its live parent, a mismatched command to whoever runs it.
	live, err := scanByPID(listOrFallback(list))
	if err != nil {
		log.Debug("stale-record sweep: process scan unavailable; skipping", "error", err)
		return nil
	}
	var targets []OrphanRuntime
	argvOf := make(map[int][]string, len(stale))
	for _, rec := range stale {
		info, ok := live[rec.PID]
		if !ok || info.PPID != 1 || !matchesSpawnCommand(info.Command, rec.Argv) {
			continue
		}
		targets = append(targets, OrphanRuntime{EndpointKey: rec.EndpointKey, PID: rec.PID, Command: info.Command})
		argvOf[rec.PID] = rec.Argv
	}
	if len(targets) == 0 {
		return nil
	}

	// TERM → grace → KILL with the same re-validated machinery the boot
	// sweep uses, then the existing removeHandle discipline: no reaped
	// runtime may leave a PID file or a spawn record behind.
	confirmed := ReapRuntimeProcesses(targets, waitAfterTerm, listOrFallback(list), signal, log)
	RemoveRuntimeHandlesForPids(nil, stale, confirmed)

	for _, pid := range confirmed {
		log.Warn("stale-record sweep: reaped runtime whose spawn record passed the age bound",
			"pid", pid, "argv", argvOf[pid])
	}
	return confirmed
}

// listOrFallback resolves a nil process-table seam to the real ps scan.
func listOrFallback(list RuntimeProcLister) RuntimeProcLister {
	if list == nil {
		return ListRuntimeProcesses
	}
	return list
}

// ReapRuntimeProcesses stops the given leftover runtimes: SIGTERM to each
// process group, a grace period, then SIGKILL for those still alive. The process
// table is re-read BEFORE any signal (a pid whose entry changed since detection
// is dropped, never signalled) and again before the SIGKILL escalation, and a
// pid is reported only once it is verifiably gone. Returns the pids confirmed
// gone — a caller may only treat those as stopped. Duplicate targets collapse to
// one signal and one entry.
//
// list and signal are seams: nil means the real ps scan and the real
// process-group kill, which is what the daemon and the CLI both use.
func ReapRuntimeProcesses(targets []OrphanRuntime, waitAfterTerm time.Duration, list RuntimeProcLister, signal runtimeSignaler, log *slog.Logger) []int {
	if len(targets) == 0 {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	if list == nil {
		list = ListRuntimeProcesses
	}
	send := func(pid int, sig syscall.Signal) error {
		if signal != nil {
			return signal(pid, sig)
		}
		return killProcessGroupPID(pid, sig)
	}

	// Validate before signalling: the targets come from an earlier scan, and a
	// pid whose entry changed in that window (reused pid, process replaced)
	// must not receive a signal.
	live, scanErr := scanByPID(list)
	if scanErr != nil {
		log.Debug("orphan sweep: process scan unavailable; skipping", "error", scanErr)
		return nil
	}
	seen := make(map[int]struct{}, len(targets))
	valid := make([]OrphanRuntime, 0, len(targets))
	for _, t := range targets {
		if _, dup := seen[t.PID]; dup {
			continue
		}
		seen[t.PID] = struct{}{}
		info, ok := live[t.PID]
		if !ok || info.PPID != 1 || info.Command != t.Command {
			log.Debug("orphan sweep: pid entry changed since detection; skipping", "pid", t.PID)
			continue
		}
		valid = append(valid, t)
	}
	if len(valid) == 0 {
		return nil
	}

	for _, t := range valid {
		log.Warn("orphan sweep: stopping runtime left behind by a previous meept process",
			"endpoint_key", t.EndpointKey, "pid", t.PID, "command", t.Command)
		if sigErr := send(t.PID, syscall.SIGTERM); sigErr != nil {
			log.Debug("orphan sweep: SIGTERM failed", "pid", t.PID, "error", sigErr)
		}
	}
	if waitAfterTerm > 0 {
		time.Sleep(waitAfterTerm)
	}

	liveNow, scanErr := scanByPID(list)
	if scanErr != nil {
		log.Debug("orphan sweep: post-SIGTERM scan unavailable; not escalating", "error", scanErr)
		return nil
	}

	var confirmed []int
	for _, t := range valid {
		info, stillThere := liveNow[t.PID]
		if !stillThere {
			confirmed = append(confirmed, t.PID) // gone on SIGTERM (or already gone)
			continue
		}
		if info.PPID != 1 || info.Command != t.Command {
			log.Debug("orphan sweep: pid entry changed before SIGKILL; leaving it alone", "pid", t.PID)
			continue
		}
		if sigErr := send(t.PID, syscall.SIGKILL); sigErr != nil {
			log.Debug("orphan sweep: SIGKILL failed", "pid", t.PID, "error", sigErr)
		}
		if waitForPIDGone(t.PID, list, reapKillTimeout) {
			confirmed = append(confirmed, t.PID)
			continue
		}
		log.Warn("orphan sweep: process survived SIGKILL; keeping its pid file", "pid", t.PID)
	}
	return confirmed
}

// scanByPID indexes a process-table scan by pid.
func scanByPID(list RuntimeProcLister) (map[int]RuntimeProcInfo, error) {
	procs, err := list()
	if err != nil {
		return nil, err
	}
	out := make(map[int]RuntimeProcInfo, len(procs))
	for _, p := range procs {
		out[p.PID] = p
	}
	return out, nil
}

// waitForPIDGone polls the process table until pid disappears or the timeout
// elapses. It reads the same view the sweep matched against, so the confirmed
// verdict can never disagree with the detection.
func waitForPIDGone(pid int, list RuntimeProcLister, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		live, err := scanByPID(list)
		if err == nil {
			if _, stillThere := live[pid]; !stillThere {
				return true
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// processAlive reports whether a pid exists (signal 0 succeeds). Used for
// processes this instance does not own, so it must never signal beyond 0.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// OrphanRuntimes reports leftover runtime processes for this manager's
// endpoints without signalling anything.
func (m *RuntimeManager) OrphanRuntimes() ([]OrphanRuntime, error) {
	return FindOrphanRuntimes(m.sweepCandidates(), m.listRuntimeProcesses())
}

// SweepOrphanRuntimes stops every runtime a previous meept generation left
// behind: endpoint configs whose auto_stop_on_exit is true, a process whose
// parent is init, and a command line equal to the endpoint's spawn command — or,
// for an endpoint whose current config no longer validates, to a durable spawn
// record's argv (records are the config-independent source). An endpoint with a
// live recorded owner is skipped. Returns the pids confirmed gone (nil when
// nothing matched). It never returns an error: a failed sweep must not block
// daemon start.
func (m *RuntimeManager) SweepOrphanRuntimes(waitAfterTerm time.Duration, records []SpawnRecord) []int {
	// Drop operator records whose runtime is gone BEFORE the match, so a
	// finished `meept runtime start` can no longer spare its endpoint forever
	// (audit finding F78); the spared set inside FindOrphanRuntimesWithRecords
	// ignores such a record even for callers that do not prune.
	records = PruneStaleOperatorRecords(records)
	candidates := m.sweepCandidates()
	orphans, err := FindOrphanRuntimesWithRecords(candidates, records, m.listRuntimeProcesses())
	if err != nil {
		m.logger.Warn("orphan sweep: process scan unavailable; skipping", "error", err)
		return nil
	}

	targets := make([]OrphanRuntime, 0, len(orphans))
	endpointOf := make(map[int]string, len(orphans))
	for _, o := range orphans {
		if m.hasLiveOwnerAmong(candidates, o.Command, o.PID) {
			m.logger.Warn("orphan sweep: leaving process alone — an endpoint with this command records a different live owner",
				"endpoint_key", o.EndpointKey, "pid", o.PID)
			continue
		}
		targets = append(targets, o)
		endpointOf[o.PID] = o.EndpointKey
	}

	confirmed := ReapRuntimeProcesses(targets, waitAfterTerm, m.listRuntimeProcesses(), m.signalRuntime, m.logger)
	for _, pid := range confirmed {
		m.removePIDFileForPid(endpointOf[pid], pid)
	}
	return confirmed
}

// hasLiveOwnerAmong reports whether any candidate endpoint whose spawn command
// matches command records a live owner other than pid. Delegates to the shared
// RuntimeHasLiveOwner predicate so the daemon's sweep and `meept doctor --fix`
// cannot disagree (audit finding F61).
func (m *RuntimeManager) hasLiveOwnerAmong(cfgs []*RuntimeConfig, command string, pid int) bool {
	return RuntimeHasLiveOwner(cfgs, command, pid)
}

// sweepCandidates snapshots the endpoint configs the sweep is given. It returns
// EVERY registered endpoint config, not just the auto_stop_on_exit=true ones:
// FindOrphanRuntimesWithRecords needs the auto_stop=false configs to build the
// spared set (their current intent must override a stale record — audit finding
// F57). The sweepability filter is applied inside FindOrphanRuntimesWithRecords.
//
// The auto_stop=false configs also reach the live-owner veto
// (hasLiveOwnerAmong → RuntimeHasLiveOwner) from here, which is INTENTIONAL:
// an operator-owned endpoint with the same command line is exactly what that
// veto protects (audit finding F78, low — see the SCOPE note on
// RuntimeHasLiveOwner for why the veto is not narrowed to managed endpoints).
// The manager lock is not held across the scan or the signals.
func (m *RuntimeManager) sweepCandidates() []*RuntimeConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*RuntimeConfig, 0, len(m.endpoints))
	for _, ep := range m.endpoints {
		if ep.cfg != nil {
			out = append(out, ep.cfg)
		}
	}
	return out
}

// sweepableEndpoint reports whether a leftover process for cfg may be reaped:
// the config must ask for the runtime to stop with the daemon, and it must have
// a spawn command to match the process table against.
func sweepableEndpoint(cfg *RuntimeConfig) bool {
	return cfg != nil && cfg.AutoStop && len(cfg.SpawnCommand) > 0
}

// sweepableRecord reports whether a record may drive a reap, applying the same
// rule as sweepableEndpoint: the recorded runtime asked to stop with the daemon
// (auto_stop_on_exit=true) and carries an argv to match the process table
// against. A record with AutoStop=false is a runtime the user configured to
// outlive the daemon and must be left alone.
func sweepableRecord(rec SpawnRecord) bool {
	return rec.AutoStop && len(rec.Argv) > 0
}

// signalRuntime sends sig to a runtime pid (process group when resolvable).
func (m *RuntimeManager) signalRuntime(pid int, sig syscall.Signal) error {
	if m.sweepSignal == nil {
		return killProcessGroupPID(pid, sig)
	}
	return m.sweepSignal(pid, sig)
}

// listRuntimeProcesses returns the process-table scanner for the sweep.
func (m *RuntimeManager) listRuntimeProcesses() RuntimeProcLister {
	if m.sweepLister == nil {
		return ListRuntimeProcesses
	}
	return m.sweepLister
}

// removePIDFileForPid removes the endpoint PID file when it still names pid,
// so the next Start() does not adopt a process that is now gone.
func (m *RuntimeManager) removePIDFileForPid(endpointKey string, pid int) {
	m.mu.Lock()
	ep, ok := m.endpoints[endpointKey]
	m.mu.Unlock()
	if !ok || ep.cfg == nil || ep.cfg.PIDFile == "" {
		return
	}
	filePID, err := ParsePIDFile(ep.cfg.PIDFile)
	if err != nil || filePID != pid {
		return
	}
	if rmErr := os.Remove(ep.cfg.PIDFile); rmErr != nil && !os.IsNotExist(rmErr) {
		m.logger.Debug("orphan sweep: pid file removal failed",
			"pid_file", ep.cfg.PIDFile, "error", rmErr)
	}
	// Remove the durable spawn record too: leaving it behind makes the daemon
	// and `meept doctor` chase a pid that no longer exists on every later boot,
	// and a STALE record (AutoStop=true) can later outvote an explicit
	// auto_stop_on_exit=false for the same endpoint (audit finding F57). Records
	// otherwise accumulate one file per spawn for the life of the install.
	RemoveSpawnRecord(ep.cfg.PIDFile)
}

// matchesSpawnCommand reports whether a process command line came from spawn.
// The trailing arguments must equal spawn[1:] exactly, and either argv[0] or
// argv[1] must name the same binary as spawn[0] — script-based runtimes get an
// interpreter prefix in ps (mlx_lm shows as "<python> /path/mlx_lm server ...").
func matchesSpawnCommand(command string, spawn []string) bool {
	if len(spawn) == 0 {
		return false
	}
	fields := strings.Fields(command)
	if len(fields) < len(spawn) {
		return false
	}
	want := filepath.Base(spawn[0])
	head := fields[:len(fields)-len(spawn)+1]
	headMatch := false
	for _, token := range head {
		if filepath.Base(token) == want {
			headMatch = true
			break
		}
	}
	if !headMatch {
		return false
	}
	tail := fields[len(fields)-len(spawn)+1:]
	for i, want := range spawn[1:] {
		if tail[i] != want {
			return false
		}
	}
	return true
}
