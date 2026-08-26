// orphan.go implements the startup orphan-child sweep
// (loop-economics/06-doctor-lifecycle). Children spawned by the daemon carry
// MEEPT_DAEMON_CHILD=1 in their command line; when the daemon crashes, they
// are re-parented to ppid==1. On boot we reap those whose recorded parent
// start marker predates the current daemon start.
//
// Platform note: implemented via `ps` parsing on macOS and Linux (/proc-style
// output through ps for portability). Windows is NOT supported — the sweep is
// a no-op there (documented gap).
package daemon

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ChildEnvTag is the environment marker set on processes spawned by the daemon.
const ChildEnvTag = "MEEPT_DAEMON_CHILD=1"

// Signal abstracts the signal sent to orphaned children (test seam).
type Signal int

const (
	SigTerm Signal = iota
	SigKill
)

func (s Signal) syscall() syscall.Signal {
	if s == SigKill {
		return syscall.SIGKILL
	}
	return syscall.SIGTERM
}

// ProcInfo is one process row from the scanner.
type ProcInfo struct {
	PID         int
	PPID        int
	EnvTag      bool   // MEEPT_DAEMON_CHILD=1 present in command line
	StartMarker string // parent start-time marker carried in the tag, if any
}

// ProcScanner lists candidate child processes.
type ProcScanner func() ([]ProcInfo, error)

// Signaler sends a signal to a pid (test seam over syscall.Kill).
type Signaler func(pid int, sig Signal) error

// SweepOrphans scans via scanner and SIGTERM->SIGKILLs confirmed orphans:
//
//	env-tagged AND re-parented (ppid==1) AND whose start marker predates
//	currentMarker (the current daemon start time formatted as unix seconds).
//
// Own children (any other ppid) and recently spawned tagged procs whose
// marker is >= currentMarker are kept. Non-meept processes never match the
// env tag so they can never be signalled. waitAfterTerm bounds the grace
// period between SIGTERM and SIGKILL.
func SweepOrphans(scanner ProcScanner, signaler Signaler, currentMarker string, waitAfterTerm time.Duration) {
	procs, err := scanner()
	if err != nil {
		return // scan unavailable: skip-on-uncertain, never kill blindly
	}
	cutoff := int64(-1)
	if v, err := strconv.ParseInt(currentMarker, 10, 64); err == nil {
		cutoff = v
	}
	var orphans []int
	for _, p := range procs {
		if !p.EnvTag || p.PPID != 1 {
			continue
		}
		marker, err := strconv.ParseInt(p.StartMarker, 10, 64)
		if err != nil {
			continue // uncertain -> skip
		}
		if cutoff >= 0 && marker >= cutoff {
			continue // started by current daemon generation: keep
		}
		orphans = append(orphans, p.PID)
	}
	for _, pid := range orphans {
		if sigErr := signaler(pid, SigTerm); sigErr != nil {
			slog.Debug("orphan sweep: SIGTERM failed", "pid", pid, "error", sigErr)
		}
	}
	if len(orphans) > 0 && waitAfterTerm > 0 {
		time.Sleep(waitAfterTerm)
	}
	for _, pid := range orphans {
		if sigErr := signaler(pid, SigKill); sigErr != nil {
			slog.Debug("orphan sweep: SIGKILL failed", "pid", pid, "error", sigErr)
		}
	}
}

// ScanChildProcs parses `ps` output for meept-tagged processes.
// Robustness guard rails: lines that don't parse as "<pid> <ppid> <command>"
// are skipped rather than guessed at.
func ScanChildProcs() ([]ProcInfo, error) {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,command=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps scan failed: %w", err)
	}
	var procs []ProcInfo
	for _, line := range strings.Split(string(out), "\n") {
		if info, ok := parsePsLine(line); ok {
			procs = append(procs, info)
		}
	}
	return procs, nil
}

// parsePsLine parses one ps line into ProcInfo. ok=false means "not a meept
// candidate / unparseable" — callers must skip it.
func parsePsLine(line string) (ProcInfo, bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return ProcInfo{}, false
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return ProcInfo{}, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return ProcInfo{}, false
	}
	cmd := strings.Join(fields[2:], " ")
	idx := strings.Index(cmd, ChildEnvTag)
	if idx < 0 {
		return ProcInfo{PID: pid, PPID: ppid, EnvTag: false}, false
	}
	// Marker format: MEEPT_DAEMON_CHILD=1:<unix-seconds>. Extract what's there.
	rest := cmd[idx+len(ChildEnvTag):]
	marker := ""
	if strings.HasPrefix(rest, ":") {
		end := 1
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		marker = rest[1:end]
	}
	return ProcInfo{PID: pid, PPID: ppid, EnvTag: true, StartMarker: marker}, true
}

// currentStartMarker returns this daemon's start-time marker value.
func currentStartMarker(start time.Time) string {
	return strconv.FormatInt(start.Unix(), 10)
}

// StartupOrphanSweep is called during daemon boot before binding the socket.
// Best-effort: failures are logged, never fatal.
func (d *Daemon) StartupOrphanSweep() {
	scanner := ScanChildProcs
	SweepOrphans(scanner, func(pid int, sig Signal) error {
		return syscall.Kill(pid, sig.syscall())
	}, currentStartMarker(d.startTime), 3*time.Second)
	d.logger.Info("daemon: orphan sweep complete")
}
