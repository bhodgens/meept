package daemon

// GRP-1 wiring remediation: the C6 live-registry leg of the groups boot
// hook. A [security.groups] policy naming a tool that is NOT registered in
// the live registry must FAIL NewComponents naming the offender — the
// promise the boot-hook comment makes. The construction-time
// ValidatePolicies cannot check this (the resolver is built before the
// registry fills), so the LIVE revalidation at the boot hook is the only
// leg that can catch it; these tests pin that leg through the real
// NewComponents path.

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
)

func TestGroupsBootValidateLiveRegistry(t *testing.T) {
	logger := testLogger(t)

	t.Run("policy naming an unregistered tool fails boot naming it", func(t *testing.T) {
		msgBus := bus.New(nil, logger)
		tmpDir := t.TempDir()
		cfg := &config.Config{
			Daemon: config.DaemonConfig{DataDir: tmpDir},
			Security: config.SecurityConfig{
				AllowedPaths: []string{tmpDir},
				Groups: config.SecurityGroupsConfig{
					Policies: []config.GroupsPolicyConfig{
						{Name: "coders", ToolsAllowed: []string{"file_read", "definitely_not_a_registered_tool"}},
					},
					Bindings: []config.GroupsBindingConfig{
						{Subject: "group:coders", Policy: "coders", Scope: "daemon"},
					},
				},
			},
		}
		_, err := NewComponents(context.Background(), cfg, msgBus, logger)
		if err == nil {
			t.Fatal("policy naming an unregistered tool booted; want boot failure (C6 live-registry leg)")
		}
		if !strings.Contains(err.Error(), "definitely_not_a_registered_tool") {
			t.Fatalf("boot error does not NAME the offending tool: %v", err)
		}
	})

	t.Run("policy naming only registered tools boots", func(t *testing.T) {
		msgBus := bus.New(nil, logger)
		tmpDir := t.TempDir()
		cfg := &config.Config{
			Daemon: config.DaemonConfig{DataDir: tmpDir},
			Security: config.SecurityConfig{
				AllowedPaths: []string{tmpDir},
				Groups: config.SecurityGroupsConfig{
					Policies: []config.GroupsPolicyConfig{
						// file_read is registered by registerBuiltinTools.
						{Name: "coders", ToolsAllowed: []string{"file_read"}},
					},
					Bindings: []config.GroupsBindingConfig{
						{Subject: "group:coders", Policy: "coders", Scope: "daemon"},
					},
				},
			},
		}
		comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
		if err != nil {
			t.Fatalf("clean live-registry policy rejected at boot: %v", err)
		}
		t.Cleanup(func() { _ = comps.Stop(context.Background()) })
	})

	t.Run("grant alias normalizes to its registered name", func(t *testing.T) {
		msgBus := bus.New(nil, logger)
		tmpDir := t.TempDir()
		cfg := &config.Config{
			Daemon: config.DaemonConfig{DataDir: tmpDir},
			Security: config.SecurityConfig{
				AllowedPaths: []string{tmpDir},
				Groups: config.SecurityGroupsConfig{
					Policies: []config.GroupsPolicyConfig{
						// "shell_execute" is the agent-grant alias for the
						// registered "shell" tool; the C6 check normalizes
						// through the same vocabulary and must pass.
						{Name: "coders", ToolsAllowed: []string{"shell_execute"}},
					},
					Bindings: []config.GroupsBindingConfig{
						{Subject: "group:coders", Policy: "coders", Scope: "daemon"},
					},
				},
			},
		}
		comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
		if err != nil {
			t.Fatalf("grant alias rejected by the live C6 leg: %v", err)
		}
		t.Cleanup(func() { _ = comps.Stop(context.Background()) })
	})
}
