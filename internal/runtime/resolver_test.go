package runtime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureHandler is an slog.Handler recording full records for assertions in
// resolver tests.
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(name string) slog.Handler       { return h }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}

func (h *captureHandler) messages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	msgs := make([]string, 0, len(h.records))
	for _, r := range h.records {
		msgs = append(msgs, r.Message)
	}
	return msgs
}

func (h *captureHandler) contains(substr string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range h.records {
		if strings.Contains(m.Message, substr) {
			return true
		}
	}
	return false
}

func newCaptureLogger(h *captureHandler) *slog.Logger {
	return slog.New(h)
}

// fakeBackend is a stand-in ExecutionBackend for resolver selection tests.
type fakeBackend struct{ name string }

func (f *fakeBackend) Execute(context.Context, Command) (*CommandResult, error) {
	return nil, nil
}
func (f *fakeBackend) Name() string { return f.name }
func (f *fakeBackend) Close() error { return nil }

// fakeManager builds a ContainerManager pre-populated with the named
// backends (same package, so internals are reachable for tests).
func fakeManager(names ...string) *ContainerManager {
	m := &ContainerManager{
		backends: make(map[string]ExecutionBackend, len(names)),
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	hasLocal := false
	for _, n := range names {
		m.backends[n] = &fakeBackend{name: n}
		if n == "local" {
			hasLocal = true
		}
	}
	if hasLocal {
		m.defaultBackend = "local"
	}
	return m
}

// stubBwrapCtor returns a bwrap constructor yielding a fake backend (or err),
// so selection tests never depend on a real bwrap binary or GOOS.
func stubBwrapCtor(name string, err error) bwrapConstructor {
	return func(_ *ContainerManager, _ *slog.Logger) (ExecutionBackend, error) {
		if err != nil {
			return nil, err
		}
		return &fakeBackend{name: name}, nil
	}
}

func alwaysAvail(*ContainerManager) bool { return true }
func neverAvail(*ContainerManager) bool  { return false }
func bwYes() bool                        { return true }
func bwNo() bool                         { return false }

func TestQualifies(t *testing.T) {
	cases := []struct {
		name  string
		want  bool
	}{
		{"docker", true},
		{"bwrap", true},
		{"local", false},
		{"podman", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Qualifies(tc.name))
		})
	}
}

// TestResolveBackend_Table exercises backend selection with injected
// availability probes so results are deterministic on every platform.
func TestResolveBackend_Table(t *testing.T) {
	cases := []struct {
		name        string
		order       SandboxOrder
		require     bool
		backends    []string // manager contents
		dockerAvail func(*ContainerManager) bool
		bwrapAvail  func() bool
		ctorErr     error
		wantName    string
		wantErr     bool
		wantWarnSub string // empty = no warning asserted
	}{
		{
			name:        "auto_docker_wins",
			order:       SandboxOrderAuto,
			backends:    []string{"local", "docker"},
			dockerAvail: alwaysAvail,
			bwrapAvail:  bwNo,
			wantName:    "docker",
		},
		{
			name:        "auto_bwrap_when_no_docker",
			order:       SandboxOrderAuto,
			backends:    []string{"local"},
			dockerAvail: neverAvail,
			bwrapAvail:  bwYes,
			wantName:    "bwrap",
		},
		{
			name:        "auto_falls_to_local_with_warning",
			order:       SandboxOrderAuto,
			backends:    []string{"local"},
			dockerAvail: neverAvail,
			bwrapAvail:  bwNo,
			wantName:    "local",
			wantWarnSub: "UNSANDBOXED",
		},
		{
			name:        "explicit_docker_respected",
			order:       SandboxOrderDocker,
			backends:    []string{"local", "docker"},
			dockerAvail: alwaysAvail,
			bwrapAvail:  bwNo,
			wantName:    "docker",
		},
		{
			name:        "explicit_docker_unavailable_skips_bwrap_falls_local",
			order:       SandboxOrderDocker,
			backends:    []string{"local"},
			dockerAvail: neverAvail,
			bwrapAvail:  bwYes, // must NOT be considered under explicit docker
			wantName:    "local",
			wantWarnSub: "UNSANDBOXED",
		},
		{
			name:        "explicit_bwrap_respected",
			order:       SandboxOrderBwrap,
			backends:    []string{"local"},
			dockerAvail: neverAvail,
			bwrapAvail:  bwYes,
			wantName:    "bwrap",
		},
		{
			name:        "explicit_bwrap_unavailable_skips_docker_falls_local",
			order:       SandboxOrderBwrap,
			backends:    []string{"local"},
			dockerAvail: alwaysAvail, // must NOT be considered under explicit bwrap
			bwrapAvail:  bwNo,
			wantName:    "local",
			wantWarnSub: "UNSANDBOXED",
		},
		{
			name:     "explicit_local_no_fallback_warning",
			order:    SandboxOrderLocal,
			backends: []string{"local"},
			dockerAvail: alwaysAvail,
			bwrapAvail:  bwYes,
			wantName:    "local",
		},
		{
			name:        "empty_order_treated_as_auto",
			order:       "",
			backends:    []string{"local", "docker"},
			dockerAvail: alwaysAvail,
			bwrapAvail:  bwNo,
			wantName:    "docker",
		},
		{
			name:        "unknown_order_warns_and_treated_as_auto",
			order:       SandboxOrder("podman"),
			backends:    []string{"local", "docker"},
			dockerAvail: alwaysAvail,
			bwrapAvail:  bwNo,
			wantName:    "docker",
			wantWarnSub: "unknown",
		},
		{
			name:        "require_true_nothing_qualifies_refuses",
			order:       SandboxOrderAuto,
			require:     true,
			backends:    []string{"local"},
			dockerAvail: neverAvail,
			bwrapAvail:  bwNo,
			wantErr:     true,
		},
		{
			name:        "require_true_explicit_local_contradiction_refuses",
			order:       SandboxOrderLocal,
			require:     true,
			backends:    []string{"local"},
			dockerAvail: alwaysAvail,
			bwrapAvail:  bwYes,
			wantErr:     true,
		},
		{
			name:        "require_true_ctor_failure_refuses",
			order:       SandboxOrderAuto,
			require:     true,
			backends:    []string{"local"},
			dockerAvail: neverAvail,
			bwrapAvail:  bwYes,
			ctorErr:     errors.New("boom"),
			wantErr:     true,
		},
		{
			name:        "require_false_ctor_failure_falls_local_with_warning",
			order:       SandboxOrderAuto,
			backends:    []string{"local"},
			dockerAvail: neverAvail,
			bwrapAvail:  bwYes,
			ctorErr:     errors.New("boom"),
			wantName:    "local",
			wantWarnSub: "UNSANDBOXED",
		},
		{
			name:        "nil_manager_require_false_yields_local",
			order:       SandboxOrderAuto,
			backends:    nil,
			dockerAvail: neverAvail,
			bwrapAvail:  bwNo,
			wantName:    "local",
			wantWarnSub: "UNSANDBOXED",
		},
		{
			name:        "nil_manager_require_true_refuses",
			order:       SandboxOrderAuto,
			require:     true,
			backends:    nil,
			dockerAvail: neverAvail,
			bwrapAvail:  bwNo,
			wantErr:     true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &captureHandler{}
			logger := newCaptureLogger(h)
			var mgr *ContainerManager
			if tc.backends != nil {
				mgr = fakeManager(tc.backends...)
			}

			backend, err := resolveBackend(
				mgr,
				ResolverConfig{Order: tc.order, RequireSandbox: tc.require},
				logger,
				tc.dockerAvail,
				tc.bwrapAvail,
				stubBwrapCtor("bwrap", tc.ctorErr),
			)

			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, backend)
				assert.True(t, errors.Is(err, ErrSandboxRequired),
					"expected ErrSandboxRequired, got: %v", err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, backend)
			assert.Equal(t, tc.wantName, backend.Name())
			if tc.wantWarnSub != "" {
				assert.True(t, h.contains(tc.wantWarnSub),
					"expected warning containing %q; got %v", tc.wantWarnSub, h.messages())
			} else if tc.order == SandboxOrderLocal {
				assert.False(t, h.contains("UNSANDBOXED"),
					"deliberate local order must not emit fallback warning; got %v", h.messages())
			}
		})
	}
}

// TestResolveBackend_PublicAPI_FailClosed verifies the public entry point
// refuses deterministically on every platform: require_sandbox=true combined
// with an explicit local order can never qualify (local never provides OS
// confinement), independent of installed binaries.
func TestResolveBackend_PublicAPI_FailClosed(t *testing.T) {
	mgr, err := NewContainerManager(Config{DefaultBackend: "local"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)

	backend, err := ResolveBackend(mgr, ResolverConfig{
		Order:          SandboxOrderLocal,
		RequireSandbox: true,
	}, nil)

	require.Error(t, err)
	assert.Nil(t, backend)
	assert.True(t, errors.Is(err, ErrSandboxRequired))
}

// TestResolveBackend_PublicAPI_ExplicitLocal verifies the public entry point
// honors an explicit local order without probing platform binaries.
func TestResolveBackend_PublicAPI_ExplicitLocal(t *testing.T) {
	mgr, err := NewContainerManager(Config{DefaultBackend: "local"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)

	backend, err := ResolveBackend(mgr, ResolverConfig{Order: SandboxOrderLocal}, nil)

	require.NoError(t, err)
	require.NotNil(t, backend)
	assert.Equal(t, "local", backend.Name())
}
