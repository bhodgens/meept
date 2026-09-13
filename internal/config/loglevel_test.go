package config

import (
	"log/slog"
	"testing"
)

// TestParseLogLevelValue pins the daemon log-level mapping. Regression: the
// mapping was case-sensitive on uppercase names only, so the lowercase values
// documented for daemon.log_level ("debug", "info", "warn", "error" in
// docs/configuration/config-sync.md) silently resolved to INFO — the daemon
// ran at info while meept.json5 said debug, emitting zero DEBUG lines.
func TestParseLogLevelValue(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   slog.Level
		wantOK bool
	}{
		{"lowercase debug", "debug", slog.LevelDebug, true},
		{"uppercase debug", "DEBUG", slog.LevelDebug, true},
		{"mixed case debug", "Debug", slog.LevelDebug, true},
		{"padded debug", "  debug\n", slog.LevelDebug, true},
		{"lowercase info", "info", slog.LevelInfo, true},
		{"uppercase info", "INFO", slog.LevelInfo, true},
		{"lowercase warn", "warn", slog.LevelWarn, true},
		{"uppercase warning", "WARNING", slog.LevelWarn, true},
		{"lowercase error", "error", slog.LevelError, true},
		{"uppercase error", "ERROR", slog.LevelError, true},
		// Empty means "unset": resolve to info but do not flag it so the
		// daemon does not warn about a key that was never written.
		{"empty is unset", "", slog.LevelInfo, true},
		{"whitespace only is unset", "   ", slog.LevelInfo, true},
		{"unknown falls back to info", "verbose", slog.LevelInfo, false},
		{"unknown uppercase falls back to info", "TRACE", slog.LevelInfo, false},
		{"bogus falls back to info", "loud", slog.LevelInfo, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseLogLevelValue(tc.input)
			if got != tc.want {
				t.Errorf("ParseLogLevelValue(%q) level = %v, want %v", tc.input, got, tc.want)
			}
			if ok != tc.wantOK {
				t.Errorf("ParseLogLevelValue(%q) ok = %v, want %v", tc.input, ok, tc.wantOK)
			}
			// ParseLogLevel must stay a thin wrapper: same level, no ok.
			if lvl := ParseLogLevel(tc.input); lvl != tc.want {
				t.Errorf("ParseLogLevel(%q) = %v, want %v", tc.input, lvl, tc.want)
			}
		})
	}
}

// TestParseLogLevelInvalidKeepsInfoDefault confirms the fallback level for an
// unrecognised name is INFO (never silent DEBUG), regardless of casing.
func TestParseLogLevelInvalidKeepsInfoDefault(t *testing.T) {
	for _, in := range []string{"nonsense", "garbage=1", "0", "-1"} {
		if lvl := ParseLogLevel(in); lvl != slog.LevelInfo {
			t.Errorf("ParseLogLevel(%q) = %v, want INFO", in, lvl)
		}
	}
}
