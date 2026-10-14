package main

import (
	"io"
	"log/slog"
	"os"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/logrotate"
)

// newDaemonLogger builds the process-wide logger for the daemon from the
// configured log level name.
//
// The level comes from daemon.log_level in meept.json5 (AGENTS.md: the log
// level is that field, NOT an env var). This is the same construction
// internal/daemon.New performs for its own logger — a text handler writing to
// the given writer with the configured level as the handler threshold.
//
// The second return value is the resolved level; the third reports whether the
// configured name was recognised. Unrecognised names resolve to info and report
// false so the caller can emit exactly one startup warning (see
// installDaemonLogger) instead of silently running at an arbitrary level.
func newDaemonLogger(out io.Writer, level string) (*slog.Logger, slog.Level, bool) {
	resolved, ok := config.ParseLogLevelValue(level)
	handler := slog.NewTextHandler(out, &slog.HandlerOptions{Level: resolved})
	return slog.New(handler), resolved, ok
}

// installDaemonLogger resolves daemon.log_level, installs the resulting logger
// as the process default, and reports an unrecognised value once.
//
// Installing the default matters as much as the daemon's own logger: agent
// loops, chat handlers, and the package-level slog.Debug call sites fall back
// to slog.Default() when no logger is wired, so without this they stayed at
// info no matter what meept.json5 said.
func installDaemonLogger(out io.Writer, level string) (*slog.Logger, slog.Level) {
	logger, resolved, ok := newDaemonLogger(out, level)
	slog.SetDefault(logger)
	if !ok {
		logger.Warn("unrecognised daemon.log_level; falling back to info",
			"configured", level,
			"using", resolved.String())
	}
	logger.Debug("daemon logger configured",
		"log_level", resolved.String(),
		"source", "daemon.log_level")
	return logger, resolved
}

// consoleWriter returns os.Stderr when it is a terminal (foreground run) and
// nil otherwise.
//
// In a detached spawn the child's stderr is a raw O_APPEND fd on the same
// meept.log the rotating writer caps (the spawners hand the file fd as the
// child's Stdout/Stderr to capture pre-logger output). Teeing to that fd would
// write every slog line TWICE — once through the capped writer, once through
// the uncapped fd — and after the first rotation the fd appends to a
// renamed-then-unlinked inode: unbounded, unreclaimable growth, the exact shape
// d8baa793 was written to stop (bughunt H3). Teeing only on a terminal keeps
// exactly ONE writer on the log file in the detached case while foreground
// runs still print.
func consoleWriter() io.Writer {
	if fi, err := os.Stderr.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		return os.Stderr
	}
	return nil
}

// newRotatingLogWriter returns an io.Writer for the daemon log that is capped at
// maxBytes, with the writer's Close kept for shutdown.
//
// Why the cap lives HERE and not at the os.OpenFile call sites
// (cmd/meept/daemon.go:171, internal/services/daemon_service.go:149): those sites
// open the file in the PARENT, hand the raw fd to the child, and close it. The
// child then holds that descriptor for its whole life, so a parent-side rotate
// would rename the file out from under an open fd and the child would keep
// appending to the unlinked inode — silently losing the cap and the disk
// bound. On 2026-10-08 exactly that shape ran unbounded: meept.log reached
// 145 GB and filled a 927 GB disk to 95%.
//
// Teed so console output is preserved when the daemon runs in the foreground.
// If the log file cannot be opened, or no console writer was supplied, the
// writer degrades to whichever stream it does have: io.MultiWriter panics on a
// nil member, so a nil console must be dropped rather than passed through.
// Losing the console is worse than losing the file, hence the fallback order.
func newRotatingLogWriter(logPath string, maxBytes int64, console io.Writer) (io.Writer, func() error) {
	cap, err := logrotate.New(logPath, maxBytes)
	if err != nil {
		return fallbackWriter(console), func() error { return nil }
	}
	if console == nil {
		return cap, cap.Close
	}
	return io.MultiWriter(console, cap), cap.Close
}

// fallbackWriter returns console, or io.Discard when console is nil, so callers
// always get a usable io.Writer.
func fallbackWriter(console io.Writer) io.Writer {
	if console == nil {
		return io.Discard
	}
	return console
}
