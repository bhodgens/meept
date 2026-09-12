package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools/mcp"
	"github.com/spf13/cobra"
)

// doctor.go implements `meept doctor [--fix]` (loop-economics/06-doctor-lifecycle).
//
// Client-side checks run first (pidfile, socket, state dir, disk); if the
// daemon is reachable the richer daemon.health RPC result is shown too.
// --fix performs ONLY safe repairs: stale pidfile removal, stale socket file
// removal, orphan-child kill. Everything else is report-only.

// doctorCheck is one formatted diagnostic line.
type doctorCheck struct {
	name    string
	ok      bool
	warn    bool
	detail  string
	fixable bool // safe to repair under --fix
}

func newDoctorCmd() *cobra.Command {
	var fix bool
	var installMissing bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose and repair the meept installation",
		Long: `Run health checks against the local meept install.

Checks pidfile, socket, state dir writability, config parse, disk space,
and orphaned children. When the daemon is reachable its daemon.health
report is included.

--fix performs only safe repairs: removing a stale pidfile or socket file,
and killing orphaned meept child processes. Everything else is report-only.

--install-missing runs the install_hint shell command of each missing mcp
server dependency. It requires --fix and prompts before every command;
hints come from your mcp_servers.json5 and are executed verbatim.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(fix, installMissing)
		},
	}

	cmd.Flags().BoolVar(&fix, "fix", false, "Apply safe repairs (stale pidfile/socket removal, orphan kill)")
	cmd.Flags().BoolVar(&installMissing, "install-missing", false,
		"run install_hint commands for missing mcp server dependencies "+
			"(requires --fix; prompts before each; hints come from your mcp_servers.json5 and are executed verbatim)")
	return cmd
}

// validateDoctorFlags is the explicit flag-combination guard for doctor.
// --install-missing executes shell commands, so it may only run under the
// user's explicit --fix opt-in. This check (not cobra's
// MarkFlagsRequiredTogether) is used on purpose: required-together would
// reject `doctor --fix` alone, which must keep working.
func validateDoctorFlags(fix, installMissing bool) error {
	if installMissing && !fix {
		return fmt.Errorf("--install-missing requires --fix")
	}
	return nil
}

func runDoctor(fix, installMissing bool) error {
	if err := validateDoctorFlags(fix, installMissing); err != nil {
		return err
	}

	stateDirPath := stateDir
	if stateDirPath == "" {
		home, _ := os.UserHomeDir()
		stateDirPath = filepath.Join(home, ".meept")
	}
	sock := getSocketPath()
	pidFile := filepath.Join(stateDirPath, "meept.pid")

	var checks []doctorCheck

	// --- client-side checks ---

	pidAlive, pidExists := checkPIDFileClient(pidFile)
	checks = append(checks, doctorCheck{
		name:    "pidfile",
		ok:      !pidExists || pidAlive,
		detail:  pidDetail(pidFile, pidExists, pidAlive),
		fixable: pidExists && !pidAlive,
	})

	socketLive := false
	conn, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err == nil {
		if cerr := conn.Close(); cerr != nil {
			socketLive = false
		} else {
			socketLive = true
		}
	}
	socketStale := false
	if _, statErr := os.Stat(sock); statErr == nil && !socketLive {
		socketStale = true
	}
	checks = append(checks, doctorCheck{
		name:    "socket-listening",
		ok:      socketLive,
		detail:  socketDetail(sock, socketLive, socketStale),
		fixable: socketStale,
	})

	checks = append(checks, checkStateDirWritable(stateDirPath))
	checks = append(checks, checkConfigReadable())
	checks = append(checks, checkDiskFreeDoctor(stateDirPath))

	// Build the sweep config list once: the report path and the --fix reaper
	// share it, so they can never disagree about what counts as an orphan.
	orphanCfgs := runtimeConfigsForSweep()
	orphanPIDs := findOrphanRuntimePIDs(orphanCfgs)
	if len(orphanPIDs) > 0 {
		checks = append(checks, doctorCheck{
			name:    "orphan-children",
			ok:      false,
			detail:  fmt.Sprintf("%d orphaned meept children: %s", len(orphanPIDs), intList(orphanPIDs)),
			fixable: true,
		})
	} else {
		checks = append(checks, doctorCheck{name: "orphan-children", ok: true, detail: "none found"})
	}

	// --- mcp catalog dependency checks (Contract B) ---
	// One line per ENABLED stdio catalog entry. A catalog-load failure is
	// a single warn line, never an abort.
	mcpChecks, mcpEntries := mcpDependencyChecksDoctor()
	checks = append(checks, mcpChecks...)

	// --- --install-missing (Contract E) ---
	// Collect the missing-with-hint servers from the check data and offer
	// to run each one's install_hint (verbatim, prompted). Runs after the
	// checks block so the diagnostics report is already on screen.
	if installMissing {
		candidates := missingInstallCandidates(mcpChecks, mcpEntries)
		if err := runInstallMissingFlow(context.Background(), candidates, os.Stdout, os.Stderr,
			os.Stdin, stdinIsTerminal(), runInstallHintFn); err != nil {
			return err
		}
	}

	// --- RPC fallback / enrichment ---
	if client, err := connectDaemon(); err == nil {
		raw, err := client.Call("daemon.health", nil)
		if err == nil {
			var health struct {
				OK      bool   `json:"ok"`
				Version string `json:"version"`
				UptimeS int64  `json:"uptime_s"`
			}
			if jsonErr := json.Unmarshal(raw, &health); jsonErr == nil {
				status := "fail"
				if health.OK {
					status = "pass"
				}
				checks = append(checks, doctorCheck{
					name:   "daemon-health",
					ok:     health.OK,
					detail: fmt.Sprintf("daemon reports %s (version %s, uptime %ds)", status, health.Version, health.UptimeS),
				})
			}
		}
		client.Close()
	}

	// --- safe repairs ---
	if fix {
		for i := range checks {
			c := &checks[i]
			if !c.fixable || c.ok {
				continue
			}
			switch c.name {
			case "pidfile":
				if err := os.Remove(pidFile); err == nil {
					c.detail += " [removed stale pidfile]"
					c.ok = true
				}
			case "socket-listening":
				if err := os.Remove(sock); err == nil {
					c.detail += " [removed stale socket file]"
					c.ok = true
				}
			case "orphan-children":
				// Reap through the shared sweep configs. The reaper re-scans
				// the process table, SIGTERMs then SIGKILLs, and returns only
				// the pids CONFIRMED gone — a SIGTERM sent is not a kill, so
				// the check is repaired only when every candidate is confirmed
				// dead. Survivors keep ok=false and say how many.
				candidates, confirmed := llm.ReapOrphanRuntimesFromConfigs(orphanCfgs, 2*time.Second, slog.Default())
				if len(confirmed) == len(candidates) {
					c.detail += fmt.Sprintf(" [stopped %d/%d orphans]", len(confirmed), len(candidates))
					c.ok = true
				} else {
					c.detail += fmt.Sprintf(" [stopped %d/%d orphans; %d survived]",
						len(confirmed), len(candidates), len(candidates)-len(confirmed))
					c.ok = false
				}
			}
		}
	}

	return printDoctorChecks(checks, fix)
}

func printDoctorChecks(checks []doctorCheck, fix bool) error {
	fmt.Println("meept doctor")
	fmt.Println("------------")
	failed := 0
	for _, c := range checks {
		mark := "[ok]"
		if c.warn {
			mark = "[warn]"
		} else if !c.ok {
			mark = "[fail]"
			failed++
		}
		line := fmt.Sprintf("%s %-18s %s", mark, c.name, c.detail)
		fmt.Println(strings.ToLower(line))
	}
	if failed > 0 && !fix {
		fmt.Printf("\n%d check(s) failing. re-run with --fix for safe repairs.\n", failed)
	} else if failed > 0 {
		fmt.Printf("\n%d check(s) could not be repaired automatically.\n", failed)
	} else {
		fmt.Println("\nall checks passed.")
	}
	return nil
}

func checkPIDFileClient(pidFile string) (alive, exists bool) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false, true
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false, true
	}
	if err := p.Signal(syscall.Signal(0)); err != nil {
		return false, true
	}
	return true, true
}

func pidDetail(pidFile string, exists, alive bool) string {
	if !exists {
		return "no pidfile (daemon not running)"
	}
	if alive {
		return "pidfile present and process alive"
	}
	return "stale pidfile points at dead process"
}

func socketDetail(sock string, live, stale bool) string {
	switch {
	case live:
		return "daemon listening on " + sock
	case stale:
		return "stale socket file with no listener"
	default:
		return "no socket file (daemon not running)"
	}
}

func checkStateDirWritable(dir string) doctorCheck {
	probe := filepath.Join(dir, ".doctor-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return doctorCheck{name: "data-dir-writable", ok: false, detail: dir + " not writable"}
	}
	if err := os.Remove(probe); err != nil {
		return doctorCheck{name: "data-dir-writable", ok: true, warn: true, detail: dir + " writable (probe cleanup failed)"}
	}
	return doctorCheck{name: "data-dir-writable", ok: true, detail: dir + " writable"}
}

func checkConfigReadable() doctorCheck {
	home, err := os.UserHomeDir()
	if err != nil {
		return doctorCheck{name: "config-parse", ok: true, warn: true, detail: "could not resolve home directory"}
	}
	cfgPath := filepath.Join(home, ".meept", "meept.json5")
	f, err := os.Open(cfgPath)
	if err != nil {
		return doctorCheck{name: "config-parse", ok: true, warn: true, detail: "no config file (using defaults)"}
	}
	if err := f.Close(); err != nil {
		return doctorCheck{name: "config-parse", ok: true, warn: true, detail: cfgPath + " readable (close failed)"}
	}
	return doctorCheck{name: "config-parse", ok: true, detail: cfgPath + " readable"}
}

func checkDiskFreeDoctor(dir string) doctorCheck {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return doctorCheck{name: "disk-free", ok: true, warn: true, detail: "could not stat filesystem"}
	}
	free := int64(st.Bavail) * int64(st.Bsize)
	const warnAt = int64(200) * 1024 * 1024
	human := fmt.Sprintf("%.0fmb", float64(free)/(1024*1024))
	if free < warnAt {
		return doctorCheck{name: "disk-free", ok: false, detail: human + " free (below 200mb threshold)"}
	}
	return doctorCheck{name: "disk-free", ok: true, detail: human + " free"}
}

// runtimeConfigsForSweep builds the runtime configs the orphan scan matches
// against: every provider with a lifecycle whose base URL is loopback,
// normalized and tagged with its endpoint key. Built ONCE per doctor run and
// shared by the report path and the --fix reaper, so the two can never
// disagree about which endpoints count.
//
// The detection replaced a scan for a MEEPT_DAEMON_CHILD environment tag: ps
// never printed that tag on macOS, so the old check could not report anything
// on this platform no matter what the daemon spawned.
func runtimeConfigsForSweep() []*llm.RuntimeConfig {
	providers, err := llm.LoadProvidersConfigDefault()
	if err != nil {
		return nil
	}
	var cfgs []*llm.RuntimeConfig
	for _, provider := range providers.Providers {
		if provider.Lifecycle == nil || !llm.IsLoopbackBaseURL(provider.Options.BaseURL) {
			continue
		}
		rtCfg, normErr := llm.ValidateAndNormalize(*provider.Lifecycle)
		if normErr != nil {
			continue
		}
		rtCfg.EndpointKey = llm.ComputeEndpointKey(string(rtCfg.Type), provider.Options.BaseURL)
		cfgs = append(cfgs, rtCfg)
	}
	return cfgs
}

// findOrphanRuntimePIDs returns the pids of local-LLM runtime processes left
// behind by a meept process that no longer exists: the parent is init (ppid==1)
// and the command line is exactly one of the configured spawn commands whose
// endpoint asks for auto_stop_on_exit. Report-only — the caller (--fix) reaps
// through the SAME config list with llm.ReapOrphanRuntimesFromConfigs.
func findOrphanRuntimePIDs(cfgs []*llm.RuntimeConfig) []int {
	orphans, err := llm.OrphanRuntimesFromConfigs(cfgs)
	if err != nil {
		return nil
	}
	pids := make([]int, 0, len(orphans))
	for _, o := range orphans {
		pids = append(pids, o.PID)
	}
	return pids
}

func intList(pids []int) string {
	parts := make([]string, len(pids))
	for i, p := range pids {
		parts[i] = strconv.Itoa(p)
	}
	return strings.Join(parts, ", ")
}

// --- mcp catalog dependency checks (Contract B) ---

// mcpDependencyChecksDoctor loads the mcp catalog via the existing config
// loading path and builds the dependency checks for every enabled stdio
// entry, plus the same entries as (name, binary, hint) triples. The two
// slices are index-parallel; a load failure yields a single warn check
// (with nil entries) instead of aborting doctor. A nil/empty catalog
// yields no checks and no entries.
func mcpDependencyChecksDoctor() ([]doctorCheck, []mcpDepEntry) {
	cfg, err := config.LoadMCPConfigDefault()
	if err != nil {
		return []doctorCheck{mcpCatalogFailureCheck(err.Error())}, nil
	}
	entries := mcpDependencyEntries(cfg)
	return mcpDependencyChecks(cfg), entries
}

// mcpDependencyChecks builds one check per enabled stdio catalog entry.
// Disabled servers and http-transport servers produce no check: http has
// no local binary dependency, and disabled entries are not launched.
func mcpDependencyChecks(cfg *config.MCPServersConfig) []doctorCheck {
	if cfg == nil {
		return nil
	}
	var checks []doctorCheck
	for _, srv := range cfg.Servers {
		if !srv.IsEnabled() || !isStdioServer(srv) {
			continue
		}
		if len(srv.Command) == 0 {
			continue
		}
		checks = append(checks, mcpDependencyCheck(srv.Name, srv.Command[0], srv.InstallHint))
	}
	return checks
}

// isStdioServer reports whether the entry launches a local stdio process.
// Mirrors the manager's inference: explicit type, else command presence.
func isStdioServer(srv mcp.ServerConfig) bool {
	if srv.Type != "" {
		return srv.Type == "stdio"
	}
	return len(srv.Command) > 0
}

// mcpDepEntry is one enabled stdio catalog entry as (name, binary, hint):
// the data install-missing needs without string-parsing check details.
type mcpDepEntry struct {
	name        string
	command     string
	installHint string
}

// mcpDependencyEntries returns the enabled stdio entries as triples. It
// keeps mcpDependencyChecks' filtering and catalog order exactly — the
// check slice and the install candidates can never disagree about which
// servers are enabled or missing.
func mcpDependencyEntries(cfg *config.MCPServersConfig) []mcpDepEntry {
	if cfg == nil {
		return nil
	}
	var entries []mcpDepEntry
	for _, srv := range cfg.Servers {
		if !srv.IsEnabled() || !isStdioServer(srv) {
			continue
		}
		if len(srv.Command) == 0 {
			continue
		}
		entries = append(entries, mcpDepEntry{name: srv.Name, command: srv.Command[0], installHint: srv.InstallHint})
	}
	return entries
}

// mcpDependencyCheck builds the doctorCheck for one enabled stdio server.
// Bare command names are resolved via exec.LookPath; absolute paths are
// stat()'d instead (LookPath rejects absolute paths containing path
// separators on some platforms). Missing binaries are data in the detail,
// never an abort.
func mcpDependencyCheck(name, command0, installHint string) doctorCheck {
	_, found := lookupBinary(command0)
	check := doctorCheck{
		name: "mcp:" + name,
		ok:   found,
	}
	if found {
		check.detail = command0 + " found in path"
		return check
	}
	check.detail = command0 + " not found"
	if installHint != "" {
		check.detail += " — install: " + installHint
	}
	return check
}

// mcpCatalogFailureCheck is the single warn line emitted when the catalog
// itself cannot be loaded; doctor continues with the remaining checks.
func mcpCatalogFailureCheck(errText string) doctorCheck {
	return doctorCheck{
		name:   "mcp:catalog",
		ok:     false,
		warn:   true,
		detail: "could not load mcp catalog — " + errText,
	}
}

// lookupBinary resolves command0: bare names via exec.LookPath, absolute
// paths via os.Stat (+ executable bit enforcement via Mode().Perm()).
func lookupBinary(command0 string) (string, bool) {
	if filepath.IsAbs(command0) {
		info, err := os.Stat(command0)
		if err != nil {
			return "", false
		}
		return command0, info.Mode().Perm()&0o111 != 0
	}
	if path, err := exec.LookPath(command0); err == nil {
		return path, true
	}
	return "", false
}

// --- --install-missing executor (Contract E) ---

// runInstallFn is the runner signature shared by runInstallHint and the
// test stubs that swap in for it.
type runInstallFn func(ctx context.Context, hint string, stdout, stderr io.Writer) error

// runInstallHintFn is the seam for runInstallHint. Tests swap it to record
// or stub hint execution; production code always calls the var, so flow
// tests can prove the consent boundary without executing anything.
var runInstallHintFn runInstallFn = runInstallHint

// stdinIsTerminal reports whether os.Stdin is a character device (a TTY).
// Piped or redirected stdin means no human can answer the consent prompts,
// so install-missing refuses up front.
func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// missingInstallCandidates pairs the enabled-stdio entries with their
// index-parallel checks and returns the missing ones that carry an
// install_hint, in catalog order. A missing binary without a hint cannot
// be installed, so it is not offered. Data-driven — no string parsing of
// check details.
func missingInstallCandidates(checks []doctorCheck, entries []mcpDepEntry) []mcpDepEntry {
	if len(entries) != len(checks) {
		return nil
	}
	var missing []mcpDepEntry
	for i, c := range checks {
		if c.ok || entries[i].installHint == "" || c.name != "mcp:"+entries[i].name {
			continue
		}
		missing = append(missing, entries[i])
	}
	return missing
}

// runInstallMissingFlow drives the per-server install loop. stdout/stderr
// carry all output; r is the consent stream; tty reports whether stdin is
// interactive — the refusal is printed before any prompt; run executes a
// consented hint. A failed install prints its result line and the loop
// continues to the next server.
func runInstallMissingFlow(ctx context.Context, entries []mcpDepEntry, stdout, stderr io.Writer,
	r io.Reader, tty bool, run runInstallFn) error {
	if !tty {
		fmt.Fprintln(stdout, "--install-missing requires an interactive terminal")
		return nil
	}
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "no missing mcp dependencies.")
		return nil
	}
	for _, e := range entries {
		fmt.Fprintf(stdout, "install for %s: %s\n", e.name, e.installHint)
		if !confirmInstall(r, stdout, e.installHint) {
			continue
		}
		err := run(ctx, e.installHint, stdout, stderr)
		if err == nil {
			fmt.Fprintf(stdout, "installed %s: ok\n", e.name)
			continue
		}
		fmt.Fprintf(stdout, "installed %s: failed (%v)\n", e.name, err)
	}
	return nil
}

// confirmInstall writes the prompt to w, reads exactly one line from r,
// and accepts only y/yes case-insensitively. Empty input or EOF is a
// refusal — it never hangs and never auto-consents.
func confirmInstall(r io.Reader, w io.Writer, hint string) bool {
	fmt.Fprintf(w, "run this command? [y/N] ")
	line, _ := bufio.NewReader(r).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// runInstallHint executes the catalog's install_hint verbatim under sh -c
// with a 10m ceiling. meept never constructs or rewrites the command. It
// is a plain function; tests stub the runInstallHintFn var above.
func runInstallHint(ctx context.Context, hint string, stdout, stderr io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", hint)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
