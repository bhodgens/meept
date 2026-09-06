package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/caimlas/meept/internal/config"
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

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose and repair the meept installation",
		Long: `Run health checks against the local meept install.

Checks pidfile, socket, state dir writability, config parse, disk space,
and orphaned children. When the daemon is reachable its daemon.health
report is included.

--fix performs only safe repairs: removing a stale pidfile or socket file,
and killing orphaned meept child processes. Everything else is report-only.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(fix)
		},
	}

	cmd.Flags().BoolVar(&fix, "fix", false, "Apply safe repairs (stale pidfile/socket removal, orphan kill)")
	return cmd
}

func runDoctor(fix bool) error {
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

	orphanPIDs := findOrphanChildren()
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
	checks = append(checks, mcpDependencyChecksDoctor()...)

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
				killed := 0
				for _, pid := range orphanPIDs {
					p, err := os.FindProcess(pid)
					if err != nil {
						continue
					}
					if err := p.Signal(syscall.SIGTERM); err != nil {
						continue
					}
					killed++
				}
				c.detail += fmt.Sprintf(" [sigterm sent to %d]", killed)
				c.ok = true
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

// findOrphanChildren scans ps for MEEPT_DAEMON_CHILD-tagged processes that
// were re-parented to init (ppid==1). Client-side conservative variant:
// any tagged proc whose parent is init is treated as a candidate.
func findOrphanChildren() []int {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,command=").Output()
	if err != nil {
		return nil
	}
	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil || ppid != 1 {
			continue
		}
		cmd := strings.Join(fields[2:], " ")
		if strings.Contains(cmd, "MEEPT_DAEMON_CHILD=1") {
			if pid, err := strconv.Atoi(fields[0]); err == nil {
				pids = append(pids, pid)
			}
		}
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
// entry. A load failure yields a single warn check instead of aborting
// doctor; a nil/empty catalog yields no checks.
func mcpDependencyChecksDoctor() []doctorCheck {
	cfg, err := config.LoadMCPConfigDefault()
	if err != nil {
		return []doctorCheck{mcpCatalogFailureCheck(err.Error())}
	}
	return mcpDependencyChecks(cfg)
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
