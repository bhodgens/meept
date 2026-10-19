package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// stubACPDecisioner is the daemon-side Decisioner stub for wiring tests
// (acp-permission-decision leaf 03). Ask is never called through this seam
// here — wiring assertions check WHETHER the setter was applied, not lane
// behavior (lane behavior is pinned in internal/acp + e2e/suites/acp-wire).
type stubACPDecisioner struct{ available bool }

func (s stubACPDecisioner) Available() bool { return s.available }

func (s stubACPDecisioner) Ask(_ context.Context, _ llm.DecisionQuestion) (*llm.DecisionResult, error) {
	return &llm.DecisionResult{Answer: "allow", Confidence: 1.0}, nil
}

var _ acp.Decisioner = stubACPDecisioner{}

func TestApplyACPFromConfig_DisabledDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.ACP.Enabled {
		t.Fatal("DefaultConfig ACP.Enabled = true, want false")
	}

	mgr, err := applyACPFromConfig(cfg.ACP, nil)
	if err != nil {
		t.Fatalf("applyACPFromConfig(default) error = %v, want nil", err)
	}
	if mgr != nil {
		t.Fatal("applyACPFromConfig(default) manager = non-nil, want nil")
	}
}

func TestApplyACPFromConfig_DisabledEmitsNoACPLogs(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	mgr, err := applyACPFromConfig(config.DefaultConfig().ACP, stubACPDecisioner{available: true})
	if err != nil || mgr != nil {
		t.Fatalf("applyACPFromConfig(default) = (%v, %v), want (nil, nil)", mgr, err)
	}
	if strings.Contains(strings.ToLower(buf.String()), "acp") {
		t.Fatalf("disabled path logged acp: %q", buf.String())
	}
}

// ---------------------------------------------------------------------------
// Decision-lane wiring (acp-permission-decision leaf 03, Contract C)
// ---------------------------------------------------------------------------

// acpEnabledConfig is an enabled [acp] section with an isolated (missing)
// catalog file — empty catalog, no subprocess, no side effects.
func acpEnabledConfig(t *testing.T, mode string) config.ACPConfig {
	t.Helper()
	cfg := config.DefaultConfig().ACP
	cfg.Enabled = true
	cfg.AgentsFile = filepath.Join(t.TempDir(), "missing-acp-agents.json5")
	cfg.PermissionMode = mode
	return cfg
}

// TestApplyACPFromConfig_DecisionModeAppliesDecider pins Contract C:
// permission_mode "decision" + non-nil Decisioner → the manager carries
// the decider (setter applied).
func TestApplyACPFromConfig_DecisionModeAppliesDecider(t *testing.T) {
	cfg := acpEnabledConfig(t, "decision")
	dec := stubACPDecisioner{available: true}

	mgr, err := applyACPFromConfig(cfg, dec)
	if err != nil {
		t.Fatalf("applyACPFromConfig(decision) error = %v", err)
	}
	if mgr == nil {
		t.Fatal("applyACPFromConfig(decision) manager = nil")
	}
	t.Cleanup(mgr.StopAll)

	if !mgr.HasDecisionClient() {
		t.Fatal("decision mode: manager.HasDecisionClient() = false, want true (SetDecisionClient not applied)")
	}
}

// TestApplyACPFromConfig_PermissiveModeIgnoresDecider pins the mode gate:
// a Decisioner passed with mode "permissive" must NOT arm the lane —
// permissive/deny behavior stays byte-identical (master.md decision 1).
func TestApplyACPFromConfig_PermissiveModeIgnoresDecider(t *testing.T) {
	cfg := acpEnabledConfig(t, "permissive")

	mgr, err := applyACPFromConfig(cfg, stubACPDecisioner{available: true})
	if err != nil {
		t.Fatalf("applyACPFromConfig(permissive) error = %v", err)
	}
	if mgr == nil {
		t.Fatal("applyACPFromConfig(permissive) manager = nil")
	}
	t.Cleanup(mgr.StopAll)

	if mgr.HasDecisionClient() {
		t.Fatal("permissive mode: manager has a decision client, want none")
	}
}

// TestApplyACPFromConfig_DecisionModeNilDeciderAllowed pins fail-closed
// layering: decision mode with a nil decider is NOT an error at the daemon
// wiring layer — the manager keeps no client and sessions fail closed with
// no_decisioner (pinned by leaf 02 tests).
func TestApplyACPFromConfig_DecisionModeNilDeciderAllowed(t *testing.T) {
	cfg := acpEnabledConfig(t, "decision")

	mgr, err := applyACPFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("applyACPFromConfig(decision, nil dec) error = %v, want nil (manager fails closed downstream)", err)
	}
	if mgr == nil {
		t.Fatal("applyACPFromConfig(decision, nil dec) manager = nil")
	}
	t.Cleanup(mgr.StopAll)

	if mgr.HasDecisionClient() {
		t.Fatal("nil decider must leave the manager without a decision client")
	}
}

func TestApplyACPFromConfig_EnabledMissingCatalog(t *testing.T) {
	cfg := config.DefaultConfig().ACP
	cfg.Enabled = true
	cfg.AgentsFile = filepath.Join(t.TempDir(), "missing-acp-agents.json5")

	mgr, err := applyACPFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("applyACPFromConfig(enabled, missing catalog) error = %v, want nil (empty catalog)", err)
	}
	if mgr == nil {
		t.Fatal("applyACPFromConfig(enabled, missing catalog) manager = nil, want non-nil")
	}
	t.Cleanup(mgr.StopAll)

	if !mgr.Enabled() {
		t.Fatal("Enabled() = false, want true")
	}
	if agents := mgr.Agents(); len(agents) != 0 {
		t.Fatalf("Agents() = %+v, want empty", agents)
	}
}

func TestRegisterACPTools_NilManagerNoPanic(t *testing.T) {
	reg := tools.NewRegistry(slog.Default())
	RegisterACPTools(reg, nil)
	if names := reg.Names(); len(names) != 0 {
		t.Fatalf("RegisterACPTools(nil mgr) registered %v", names)
	}
}

func TestRegisterACPTools_NilRegistryNoPanic(t *testing.T) {
	cfg := config.DefaultConfig().ACP
	cfg.Enabled = true
	cfg.AgentsFile = filepath.Join(t.TempDir(), "missing-acp-agents.json5")
	mgr, err := applyACPFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("applyACPFromConfig: %v", err)
	}
	if mgr != nil {
		t.Cleanup(mgr.StopAll)
	}
	RegisterACPTools(nil, mgr)
}

func TestRegisterACPTools_DisabledNoTools(t *testing.T) {
	reg := tools.NewRegistry(slog.Default())
	mgr, err := applyACPFromConfig(config.DefaultConfig().ACP, nil)
	if err != nil {
		t.Fatalf("applyACPFromConfig: %v", err)
	}
	RegisterACPTools(reg, mgr)
	if names := reg.Names(); len(names) != 0 {
		t.Fatalf("RegisterACPTools(disabled) registered %v, want none", names)
	}
}

func TestRegisterACPTools_EnabledRegistersACPAgent(t *testing.T) {
	cfg := config.DefaultConfig().ACP
	cfg.Enabled = true
	cfg.AgentsFile = filepath.Join(t.TempDir(), "missing-acp-agents.json5")
	mgr, err := applyACPFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("applyACPFromConfig: %v", err)
	}
	if mgr == nil {
		t.Fatal("manager is nil")
	}
	t.Cleanup(mgr.StopAll)

	reg := tools.NewRegistry(slog.Default())
	RegisterACPTools(reg, mgr)
	names := reg.Names()
	if len(names) != 1 || names[0] != "acp_agent" {
		t.Fatalf("RegisterACPTools(enabled) names = %v, want [acp_agent]", names)
	}
}

func TestApplyACPFromConfig_StopAllIdempotent(t *testing.T) {
	cfg := config.DefaultConfig().ACP
	cfg.Enabled = true
	cfg.AgentsFile = filepath.Join(t.TempDir(), "missing-acp-agents.json5")

	mgr, err := applyACPFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("applyACPFromConfig: %v", err)
	}
	if mgr == nil {
		t.Fatal("manager is nil")
	}
	mgr.StopAll()
	mgr.StopAll()
}
