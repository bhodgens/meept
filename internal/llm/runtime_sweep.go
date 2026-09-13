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
		if rec.PIDFile != "" {
			spared[rec.PIDFile] = true
		}
		if rec.EndpointKey != "" {
			spared[rec.EndpointKey] = true
		}
	}
	return spared
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
	orphans, err := FindOrphanRuntimesWithRecords(cfgs, records, ListRuntimeProcesses)
	if err != nil {
		return nil, err
	}
	// Apply the SAME live-owner veto the daemon's sweep applies, so the two
	// surfaces cannot disagree about the same process (audit finding F61):
	// `meept doctor --fix` used to reap a pid the daemon's boot sweep would have
	// left alone because another endpoint records a live owner for it.
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
	orphans, err := OrphanRuntimesFromConfigsAndRecords(cfgs, records)
	if err != nil {
		if log != nil {
			log.Debug("orphan sweep: process scan unavailable", "error", err)
		}
		return nil, nil
	}
	return orphans, ReapRuntimeProcesses(orphans, waitAfterTerm, nil, nil, log)
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
