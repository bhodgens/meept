package main

import (
	"io"
	"log/slog"

	"github.com/caimlas/meept/internal/config"
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
