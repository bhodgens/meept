package llm

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Endpoint start lock.
//
// RuntimeProcess.Start probes the endpoint port before spawning, but the probe
// alone cannot close the window: two meept processes (a second daemon, a CLI, a
// test binary) can both probe the same free port, both see it free, and both
// spawn. The loser cannot bind, and for a script runtime that survives a failed
// bind (mlx_lm) it then reads healthy because the winner answers its health
// checks — the same false-healthy the duplicate-spawn guard exists to prevent,
// reached through a race instead of a config.
//
// The probe + adoption check + spawn + PID-file write therefore run under one
// exclusive lock file beside the PID file. The lock is deliberately short-lived
// (it is NOT held across the health wait) and self-healing: a lock whose owner
// pid is gone, or that is older than startLockStaleAfter, is taken over. That
// staleness rule is also why this uses O_EXCL create + owner liveness instead of
// syscall.Flock — the same pattern the daemon uses for its own PID file, and one
// that stays portable.
const (
	startLockStaleAfter = 30 * time.Second
	startLockWait       = 10 * time.Second
)

// startLockPath returns the lock path for a runtime PID file, or "" when there
// is no PID file to key on.
func startLockPath(pidFile string) string {
	if pidFile == "" {
		return ""
	}
	return pidFile + ".lock"
}

// acquireStartLock takes the endpoint start lock for pidFile. The returned
// release function is never nil when the error is nil. A runtime without a PID
// file cannot be keyed (CLI/eval stacks that never write one): the lock is
// skipped and the release function is a no-op.
func acquireStartLock(pidFile string) (func(), error) {
	path := startLockPath(pidFile)
	if path == "" {
		return func() {}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create start lock directory: %w", err)
	}

	deadline := time.Now().Add(startLockWait)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			// The owner pid is diagnostic only: it lets a later acquisition
			// decide the lock is stale. A write failure must not block the
			// spawn, so it is reported and the lock is still honoured.
			if _, werr := f.WriteString(strconv.Itoa(os.Getpid())); werr != nil {
				slog.Debug("start lock: write owner pid", "path", path, "error", werr)
			}
			return func() { releaseStartLock(f, path) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("create endpoint start lock %s: %w", path, err)
		}

		if staleStartLock(path) {
			if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
				return nil, fmt.Errorf("remove stale endpoint start lock %s: %w", path, rmErr)
			}
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("endpoint start lock %s is held by another meept process; retry once that start finishes", path)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// staleStartLock reports whether an existing lock file can be taken over: its
// recorded owner is gone, its contents are unreadable, or it is older than
// startLockStaleAfter (a start never legitimately holds it that long, because
// the lock covers only the probe, the spawn and the PID-file write).
func staleStartLock(path string) bool {
	if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > startLockStaleAfter {
		return true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if convErr != nil || pid <= 0 {
		return true
	}
	return !processAlive(pid)
}

// releaseStartLock drops the lock: close the handle, then remove the file so the
// next acquisition creates a fresh one.
func releaseStartLock(f *os.File, path string) {
	if f != nil {
		if cerr := f.Close(); cerr != nil {
			slog.Debug("start lock: close", "path", path, "error", cerr)
		}
	}
	if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
		slog.Debug("start lock: remove", "path", path, "error", rmErr)
	}
}
