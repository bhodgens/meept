package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// restoreDefaultLogger snapshots the process-default logger so a test that
// installs a daemon logger cannot leak its level into other tests in the
// package.
func restoreDefaultLogger(t *testing.T) {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
}

// TestInstallDaemonLoggerHonoursConfiguredLevel is the regression test for the
// inert-log-level defect: daemon.log_level in meept.json5 must gate both the
// logger the daemon builds and the process default that component loggers fall
// back to. Before the fix, a lowercase "debug" resolved to INFO through the
// case-sensitive mapping, so a debug-configured daemon emitted zero DEBUG
// lines, and slog.Default() was never set at all.
func TestInstallDaemonLoggerHonoursConfiguredLevel(t *testing.T) {
	tests := []struct {
		name          string
		level         string
		wantLevel     slog.Level
		wantDebug     bool
		wantInfo      bool
		wantWarn      bool
		wantError     bool
		wantLevelWarn bool // "unrecognised daemon.log_level" startup warning
	}{
		{"lowercase debug", "debug", slog.LevelDebug, true, true, true, true, false},
		{"uppercase debug", "DEBUG", slog.LevelDebug, true, true, true, true, false},
		{"padded mixed-case debug", "  DeBuG ", slog.LevelDebug, true, true, true, true, false},
		{"lowercase info", "info", slog.LevelInfo, false, true, true, true, false},
		{"uppercase info", "INFO", slog.LevelInfo, false, true, true, true, false},
		{"lowercase warn", "warn", slog.LevelWarn, false, false, true, true, false},
		{"uppercase warning", "WARNING", slog.LevelWarn, false, false, true, true, false},
		{"lowercase error", "error", slog.LevelError, false, false, false, true, false},
		{"invalid falls back to info", "verbose", slog.LevelInfo, false, true, true, true, true},
		{"invalid uppercase falls back to info", "TRACE", slog.LevelInfo, false, true, true, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			restoreDefaultLogger(t)
			var buf bytes.Buffer

			_, resolved := installDaemonLogger(&buf, tc.level)
			if resolved != tc.wantLevel {
				t.Fatalf("resolved level = %v, want %v", resolved, tc.wantLevel)
			}

			// Probe the logger the daemon builds...
			logger, _, ok := newDaemonLogger(&buf, tc.level)
			logger.Debug("probe-debug")
			logger.Info("probe-info")
			logger.Warn("probe-warn")
			logger.Error("probe-error")
			_ = ok
			// ...and the process default the components fall back to.
			slog.Default().Debug("probe-default-debug")
			slog.Default().Info("probe-default-info")

			out := buf.String()
			checks := []struct {
				probe string
				want  bool
			}{
				{"probe-debug", tc.wantDebug},
				{"probe-info", tc.wantInfo},
				{"probe-warn", tc.wantWarn},
				{"probe-error", tc.wantError},
				{"probe-default-debug", tc.wantDebug},
				{"probe-default-info", tc.wantInfo},
			}
			for _, c := range checks {
				if got := strings.Contains(out, c.probe); got != c.want {
					t.Errorf("level %q: probe %q emitted = %v, want %v (log:\n%s)", tc.level, c.probe, got, c.want, out)
				}
			}
			// DEBUG lines must actually carry level=DEBUG — the parent's
			// verification grep was `grep -c level=DEBUG`.
			if got := strings.Contains(out, "level=DEBUG"); got != tc.wantDebug {
				t.Errorf("level %q: has level=DEBUG = %v, want %v (log:\n%s)", tc.level, got, tc.wantDebug, out)
			}

			gotLevelWarn := strings.Contains(out, "unrecognised daemon.log_level")
			if gotLevelWarn != tc.wantLevelWarn {
				t.Errorf("level %q: unrecognised-level warning = %v, want %v (log:\n%s)", tc.level, gotLevelWarn, tc.wantLevelWarn, out)
			}
			if tc.wantLevelWarn {
				if !strings.Contains(out, "configured="+strings.TrimSpace(tc.level)) {
					t.Errorf("level warning must name the offending value %q (log:\n%s)", tc.level, out)
				}
				if !strings.Contains(out, "using=INFO") {
					t.Errorf("level warning must name the fallback level (log:\n%s)", out)
				}
			} else if strings.Contains(out, "falls back to info") {
				t.Errorf("valid level %q must not warn about the level (log:\n%s)", tc.level, out)
			}
		})
	}
}

// TestDaemonLoggerDebugStartupLine proves the exact operator-visible symptom
// from the defect report: with log_level debug the daemon emits a DEBUG line
// at startup, with log_level info it does not.
func TestDaemonLoggerDebugStartupLine(t *testing.T) {
	t.Run("debug emits DEBUG", func(t *testing.T) {
		restoreDefaultLogger(t)
		var buf bytes.Buffer
		installDaemonLogger(&buf, "debug")
		out := buf.String()
		if !strings.Contains(out, "level=DEBUG") {
			t.Fatalf("debug level emitted no DEBUG line:\n%s", out)
		}
		if !strings.Contains(out, `msg="daemon logger configured"`) {
			t.Errorf("debug level must log the resolved level:\n%s", out)
		}
		if !strings.Contains(out, "log_level=DEBUG") {
			t.Errorf("startup line must carry log_level=DEBUG:\n%s", out)
		}
	})

	t.Run("info emits no DEBUG", func(t *testing.T) {
		restoreDefaultLogger(t)
		var buf bytes.Buffer
		installDaemonLogger(&buf, "info")
		if out := buf.String(); strings.Contains(out, "level=DEBUG") {
			t.Fatalf("info level emitted a DEBUG line:\n%s", out)
		}
	})
}
