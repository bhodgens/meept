package daemon

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureHandler is an slog.Handler that records message strings for
// assertions in tests.
type captureHandler struct {
	mu      sync.Mutex
	records []string
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(name string) slog.Handler       { return h }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Message)
	return nil
}

func (h *captureHandler) contains(substr string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range h.records {
		if strings.Contains(m, substr) {
			return true
		}
	}
	return false
}

// newCaptureLogger returns a logger backed by a captureHandler.
func newCaptureLogger(h *captureHandler) *slog.Logger {
	return slog.New(h)
}

// TestBuildRuntimeConfig_InheritModeWarns verifies that mapping
// [runtime].env_mode = "inherit" into runtime.Config emits a startup
// warning: full environment inheritance can leak daemon secrets into
// agent-run shells.
func TestBuildRuntimeConfig_InheritModeWarns(t *testing.T) {
	h := &captureHandler{}
	logger := newCaptureLogger(h)

	rc := config.RuntimeConfig{
		Enabled:   true,
		EnvPolicy: config.EnvPolicyConfig{Mode: "inherit"},
	}

	got := buildRuntimeConfig(rc, logger)

	require.True(t, h.contains("env_mode"), "expected a warning mentioning env_mode; got %v", h.records)
	require.True(t, h.contains("inherit"), "expected the warning to mention inherit mode; got %v", h.records)
	assert.Equal(t, runtime.EnvModeInherit, got.EnvPolicy.Mode)
}

// TestBuildRuntimeConfig_DefaultNormalizesToAllowlist verifies the secure
// default: an unset env_mode silently normalizes to allowlist with default
// deny globs, and emits NO warning.
func TestBuildRuntimeConfig_DefaultNormalizesToAllowlist(t *testing.T) {
	h := &captureHandler{}
	logger := newCaptureLogger(h)

	rc := config.RuntimeConfig{Enabled: true} // EnvPolicy.Mode intentionally empty

	got := buildRuntimeConfig(rc, logger)

	assert.Equal(t, runtime.EnvModeAllowlist, got.EnvPolicy.Mode)
	assert.NotEmpty(t, got.EnvPolicy.DenyGlobs, "deny globs must be defaulted")
	for _, m := range h.records {
		assert.NotContains(t, m, "env_mode", "no warning expected for secure default")
	}
}

// TestBuildRuntimeConfig_UserSettingsPreserved verifies explicit user
// allowlist/deny settings survive mapping.
func TestBuildRuntimeConfig_UserSettingsPreserved(t *testing.T) {
	h := &captureHandler{}
	logger := newCaptureLogger(h)

	rc := config.RuntimeConfig{
		Enabled: true,
		EnvPolicy: config.EnvPolicyConfig{
			Mode:      "allowlist",
			Allowlist: []string{"MY_VAR"},
			DenyGlobs: []string{"*CUSTOM*"},
		},
	}

	got := buildRuntimeConfig(rc, logger)

	assert.Equal(t, runtime.EnvModeAllowlist, got.EnvPolicy.Mode)
	assert.Equal(t, []string{"MY_VAR"}, got.EnvPolicy.Allowlist)
	assert.Equal(t, []string{"*CUSTOM*"}, got.EnvPolicy.DenyGlobs)
}
