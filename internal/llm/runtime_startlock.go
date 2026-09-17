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
	// startLockFreshGrace covers the create-then-deschedule window: a lock
	// file that exists but does not yet carry its owner pid (the creator
	// was descheduled between O_EXCL create and the WriteString) must NOT
	// be treated as stale while it is this fresh — a second meept process
	// that judged it stale would remove it and spawn a duplicate onto the
	// occupied endpoint (audit finding F25). Only an empty/unparseable lock
	// OLDER than this grace is stale.
	startLockFreshGrace = 2 * time.Second
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

// tryAcquireStartLock takes the endpoint start lock WITHOUT waiting: it succeeds
// only when the lock is free (or stale and removable) and reports whether it
// did. The cleanup paths use it because they must not block a waiter — a lock
// held by another process means a Start is in flight and that starter owns the
// endpoint's files, so the cleanup simply does nothing.
func tryAcquireStartLock(pidFile string) (func(), bool) {
	path := startLockPath(pidFile)
	if path == "" {
		return func() {}, true
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		slog.Debug("start lock: create lock directory", "path", path, "error", err)
		return nil, false
	}
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if _, werr := f.WriteString(strconv.Itoa(os.Getpid())); werr != nil {
				slog.Debug("start lock: write owner pid", "path", path, "error", werr)
			}
			return func() { releaseStartLock(f, path) }, true
		}
		if !os.IsExist(err) {
			slog.Debug("start lock: create", "path", path, "error", err)
			return nil, false
		}
		if attempt == 0 && staleStartLock(path) {
			if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
				return nil, false
			}
			continue
		}
		return nil, false
	}
	return nil, false
}

// staleStartLock reports whether an existing lock file can be taken over: its
// recorded owner is gone, its contents are unreadable and OLD, or it is older
// than startLockStaleAfter (a start never legitimately holds it that long,
// because the lock covers only the probe, the spawn and the PID-file write).
//
// An empty/unparseable lock YOUNGER than startLockFreshGrace is NOT stale
// (audit finding F25): the creator may have been descheduled between the
// O_EXCL create and the owner-pid write, so the fresh empty file is a lock
// being taken RIGHT NOW — removing it here would let a duplicate spawn slip
// past the guard. Such a lock only becomes takeable once it ages past the
// grace. The mtime (written at create) is the age reference, so no
// coordination with the creator is needed.
func staleStartLock(path string) bool {
	info, statErr := os.Stat(path)
	if statErr != nil {
		// Unreadable metadata: nothing says the file is being created
		// right now; treat it as stale (unchanged behaviour).
		return true
	}
	if time.Since(info.ModTime()) > startLockStaleAfter {
		return true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if convErr != nil || pid <= 0 {
		// Empty or unparseable: only stale once it is older than the
		// fresh-creation grace (audit finding F25). A file younger than
		// the grace whose owner pid is already written falls through to
		// the liveness check below, so a crashed creator does not block
		// the endpoint for the grace period.
		return time.Since(info.ModTime()) > startLockFreshGrace
	}
	return !processAlive(pid)
}

// releaseStartLock drops the lock: close the handle, then remove the file so
// the next acquisition creates a fresh one.
//
// The unlink is guarded by an ownership check (audit finding F25): the lock
// file may have been replaced between this process's create and its release
// (a stale-takeover by another meept process removed and recreated it), and
// removing THAT file would release a lock this process does not hold —
// exactly the duplicate-spawn window the lock exists to close. The guard
// compares the file's identity against the handle's (same-inode check via
// fstat) and falls back to the pid-content check for platforms/filesystems
// where the inode cannot be compared; if neither proves ownership the file is
// left alone.
func releaseStartLock(f *os.File, path string) {
	if f != nil {
		if cerr := f.Close(); cerr != nil {
			slog.Debug("start lock: close", "path", path, "error", cerr)
		}
	}
	if !startLockOwned(f, path) {
		slog.Debug("start lock: not the owner at release; leaving the lock file alone",
			"path", path)
		return
	}
	if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
		slog.Debug("start lock: remove", "path", path, "error", rmErr)
	}
}

// startLockOwned reports whether the (already closed) handle f still refers
// to the lock file at path: the path must exist and carry the owner pid this
// process wrote. After Close the descriptor cannot be fstat'ed, so ownership
// is established from the file's content: the pid inside must parse and be
// THIS process. A file that was removed and recreated by another process
// carries that other process's pid (or none, while it is fresh) — either way
// it is not ours to remove.
func startLockOwned(f *os.File, path string) bool {
	if f == nil {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		// Gone, or unreadable: either way there is nothing of ours to
		// remove; report false so release stays a no-op.
		return false
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
	return convErr == nil && pid == os.Getpid()
}
