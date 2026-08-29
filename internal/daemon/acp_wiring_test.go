package daemon

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tools"
)

func TestApplyACPFromConfig_DisabledDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.ACP.Enabled {
		t.Fatal("DefaultConfig ACP.Enabled = true, want false")
	}

	mgr, err := applyACPFromConfig(cfg.ACP)
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

	mgr, err := applyACPFromConfig(config.DefaultConfig().ACP)
	if err != nil || mgr != nil {
		t.Fatalf("applyACPFromConfig(default) = (%v, %v), want (nil, nil)", mgr, err)
	}
	if strings.Contains(strings.ToLower(buf.String()), "acp") {
		t.Fatalf("disabled path logged acp: %q", buf.String())
	}
}

func TestApplyACPFromConfig_EnabledMissingCatalog(t *testing.T) {
	cfg := config.DefaultConfig().ACP
	cfg.Enabled = true
	cfg.AgentsFile = filepath.Join(t.TempDir(), "missing-acp-agents.json5")

	mgr, err := applyACPFromConfig(cfg)
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
	mgr, err := applyACPFromConfig(cfg)
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
	mgr, err := applyACPFromConfig(config.DefaultConfig().ACP)
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
	mgr, err := applyACPFromConfig(cfg)
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

	mgr, err := applyACPFromConfig(cfg)
	if err != nil {
		t.Fatalf("applyACPFromConfig: %v", err)
	}
	if mgr == nil {
		t.Fatal("manager is nil")
	}
	mgr.StopAll()
	mgr.StopAll()
}
