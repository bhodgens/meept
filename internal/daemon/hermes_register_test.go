package daemon

import (
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tools"
)

func TestRegisterHermesTools_DisabledNoTools(t *testing.T) {
	cfg := config.DefaultConfig().Hermes // Enabled=false by default
	reg := tools.NewRegistry(slog.Default())
	RegisterHermesTools(reg, cfg)
	if names := reg.Names(); len(names) != 0 {
		t.Fatalf("RegisterHermesTools(disabled) registered %v, want none", names)
	}
}

func TestRegisterHermesTools_NilRegistryNoPanic(t *testing.T) {
	cfg := config.DefaultConfig().Hermes
	cfg.Enabled = true
	t.Setenv("MEEPT_TEST_HERMES_KEY", "k")
	cfg.APIKeyEnv = "MEEPT_TEST_HERMES_KEY"
	RegisterHermesTools(nil, cfg) // must not panic
}

func TestRegisterHermesTools_EnabledNoKeyNoTools(t *testing.T) {
	cfg := config.DefaultConfig().Hermes
	cfg.Enabled = true
	cfg.APIKeyEnv = "MEEPT_TEST_HERMES_KEY_UNSET_XYZ"
	reg := tools.NewRegistry(slog.Default())
	RegisterHermesTools(reg, cfg)
	if names := reg.Names(); len(names) != 0 {
		t.Fatalf("RegisterHermesTools(enabled, no key) registered %v, want none", names)
	}
}

func TestRegisterHermesTools_EnabledWithKeyRegistersHermes(t *testing.T) {
	cfg := config.DefaultConfig().Hermes
	cfg.Enabled = true
	cfg.APIKeyEnv = "MEEPT_TEST_HERMES_KEY"
	t.Setenv("MEEPT_TEST_HERMES_KEY", "secret-value")

	reg := tools.NewRegistry(slog.Default())
	RegisterHermesTools(reg, cfg)
	names := reg.Names()
	if len(names) != 1 || names[0] != "hermes" {
		t.Fatalf("RegisterHermesTools(enabled+key) names = %v, want [hermes]", names)
	}
}

func TestRegisterHermesTools_AutoApproveRegisters(t *testing.T) {
	cfg := config.DefaultConfig().Hermes
	cfg.Enabled = true
	cfg.AutoApprove = true
	cfg.APIKeyEnv = "MEEPT_TEST_HERMES_KEY"
	t.Setenv("MEEPT_TEST_HERMES_KEY", "secret-value")

	reg := tools.NewRegistry(slog.Default())
	RegisterHermesTools(reg, cfg)
	names := reg.Names()
	if len(names) != 1 || names[0] != "hermes" {
		t.Fatalf("RegisterHermesTools(auto_approve) names = %v, want [hermes]", names)
	}
}
