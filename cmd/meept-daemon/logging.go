package main

import (
	"io"
	"log/slog"

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
