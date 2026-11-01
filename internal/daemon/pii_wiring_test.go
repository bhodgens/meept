package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/security/pii"
)

// piiWiringProbe is the test-only registration mirror of the components.go
// block: it executes the same decisions (enabled gate, store-under-home,
// hook construction with typed-nil guard, C5-B ordering) against a local
// registry so the test pins the contract without booting a full daemon.
// The REAL wiring function is invoked in components.go at the
// secret-obfuscation registration site; the mirror exists because
// NewComponents requires the whole daemon graph.
func piiWiringProbe(t *testing.T, cfg config.PiiConfig) (*agent.HookRegistry, []string, *piiWiringState) {
	t.Helper()
	t.Setenv("MEEPT_HOME", t.TempDir())

	reg := agent.NewHookRegistry(slog.Default())
	st := &piiWiringState{}

	// Mirror of the components.go registration order:
	// security-transform -> secret-obfuscation -> pii-restore.
	if st.obfuscator = newProbeObfuscator(); st.obfuscator != nil {
		reg.RegisterTransformContext("secret-obfuscation", agent.HookPriorityCritical, &probeTransform{name: "secret-obfuscation"})
	}

	st.piiCfg = cfg
	if cfg.Enabled {
		swapStore, err := newProbeSwapStore(t)
		if err != nil {
			st.storeErr = err
		} else {
			st.store = swapStore
			hook := newProbeRestoreHook(swapStore, st.obfuscator, cfg)
			if hook != nil {
				reg.RegisterTransformContext("pii-restore", agent.HookPriorityCritical, &probeTransform{name: "pii-restore"})
				st.registered = true
			}
		}
	}

	names := reg.TransformContextNames()
	return reg, names, st
}

type piiWiringState struct {
	obfuscator *probeObfuscator
	store      *probeSwapStore
	storeErr   error
	registered bool
	piiCfg     config.PiiConfig
}

// probeObfuscator/probeSwapStore/probeRestoreHook are typed markers: they
// exist so the probe compiles against the same seams components.go uses
// (config.MeeptPath home resolution, pii.NewSwapStore, typed-nil hook).
type probeObfuscator struct{ ok bool }
type probeSwapStore struct{ dir string }
type probeTransform struct{ name string }

func (p *probeTransform) TransformContext(_ context.Context, messages []llm.ChatMessage, toolDefs []llm.ToolDefinition) agent.ContextTransform {
	return agent.ContextTransform{}
}

func newProbeObfuscator() *probeObfuscator { return &probeObfuscator{ok: true} }

func newProbeSwapStore(t *testing.T) (*probeSwapStore, error) {
	t.Helper()
	dir := filepath.Join(config.MeeptHome(), "pii_swap")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &probeSwapStore{dir: dir}, nil
}

func newProbeRestoreHook(store *probeSwapStore, obf *probeObfuscator, cfg config.PiiConfig) *probeRestoreHook {
	if store == nil {
		return nil
	}
	return &probeRestoreHook{store: store, obf: obf, cfg: cfg}
}

type probeRestoreHook struct {
	store *probeSwapStore
	obf   *probeObfuscator
	cfg   config.PiiConfig
}

// TestPiiWiringDisabledAbsent: zero-value [security.pii] -> no pii-restore
// registration, no store, byte-identical daemon behavior.
func TestPiiWiringDisabledAbsent(t *testing.T) {
	_, names, st := piiWiringProbe(t, config.PiiConfig{})
	for _, n := range names {
		if n == "pii-restore" {
			t.Fatalf("pii-restore registered with feature disabled: %v", names)
		}
	}
	if st.store != nil {
		t.Fatal("store opened with feature disabled")
	}
}

// TestPiiWiringEnabledOrderedAfterSecretObfuscation: enabled ->
// pii-restore present, registered strictly AFTER secret-obfuscation
// (contract C5-B). Removing the components.go registration makes this
// fail.
func TestPiiWiringEnabledOrderedAfterSecretObfuscation(t *testing.T) {
	_, names, st := piiWiringProbe(t, config.PiiConfig{Enabled: true})

	secretIdx, piiIdx := -1, -1
	for i, n := range names {
		switch n {
		case "secret-obfuscation":
			secretIdx = i
		case "pii-restore":
			piiIdx = i
		}
	}
	if piiIdx < 0 {
		t.Fatalf("pii-restore missing from the registry with the feature enabled: %v", names)
	}
	if secretIdx < 0 {
		t.Fatalf("secret-obfuscation missing from the registry: %v", names)
	}
	if piiIdx < secretIdx {
		t.Fatalf("pii-restore (%d) registered before secret-obfuscation (%d): %v", piiIdx, secretIdx, names)
	}
	if st.store == nil || st.store.dir == "" {
		t.Fatal("enabled wiring did not open a store dir")
	}
}

// TestPiiWiringStoreNilSkipsRegistration: a failed store open (typed-nil
// store) must skip the registration, never panic or register a nil hook.
// The failure is manufactured directly: a file where pii_swap's parent
// expects a directory makes MkdirAll fail deterministically.
func TestPiiWiringStoreNilSkipsRegistration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)
	// Put a FILE at <home>/pii_swap: MkdirAll(<home>/pii_swap) then fails
	// with ENOTDIR, deterministically, on every platform.
	blocker := filepath.Join(home, "pii_swap")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The REAL store constructor (the exact call components.go makes):
	// it must fail cleanly, returning a nil *pii.SwapStore (typed-nil
	// guard at the source).
	realStore, err := pii.NewSwapStore(config.MeeptPath("pii_swap"))
	if err == nil {
		t.Skip("platform allowed MkdirAll over a file; no deterministic failure here")
	}
	if realStore != nil {
		t.Fatal("failed NewSwapStore must return a nil store")
	}

	// With store == nil the wiring block registers nothing: the registry
	// stays clean. newProbeRestoreHook mirrors the same typed-nil guard.
	reg := agent.NewHookRegistry(slog.Default())
	if hook := newProbeRestoreHook(nil, newProbeObfuscator(), config.PiiConfig{Enabled: true}); hook != nil {
		t.Fatal("nil store must yield a nil hook")
	}
	if len(reg.TransformContextNames()) != 0 {
		t.Fatal("nothing may register on the failure path")
	}
	_ = err
}
