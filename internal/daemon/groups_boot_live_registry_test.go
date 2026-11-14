package daemon

// GRP-1 wiring remediation: the C6 live-registry leg of the groups boot
// hook. A [security.groups] policy naming a tool that is NOT registered in
// the live registry must FAIL Components.Start naming the offender — the
// promise the boot-hook comment makes. Commit 479666b7 moved the check
// from NewComponents to Start (a construction-time check rejected names
// registered later in Start: scheduler, ledger, platform, employee
// messaging, voice), so these tests drive the REAL production path:
// NewComponents(...) then comps.Start(ctx). The construction-time
// ValidatePolicies cannot check this (the resolver is built before the
// registry fills), and Start is the only leg that runs after every
// registration path; these tests pin that leg.

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
		comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
		if err != nil {
			t.Fatalf("construction failed before Start could run the live C6 leg: %v", err)
		}
		if comps == nil {
			t.Fatal("NewComponents returned nil components with nil error")
		}
		t.Cleanup(func() { _ = comps.Stop(context.Background()) })
		// The C6 live-registry check runs inside Start, after every
		// registration path (builtin, scheduler, ledger, platform,
		// employee messaging, voice). A policy naming an unregistered
		// tool must fail HERE, naming the offender.
		if err := comps.Start(context.Background()); err == nil {
			t.Fatal("policy naming an unregistered tool started; want Start failure (C6 live-registry leg)")
		} else {
			if !strings.Contains(err.Error(), "definitely_not_a_registered_tool") {
				t.Fatalf("Start error does not NAME the offending tool: %v", err)
			}
			// Fix 3 pin: the fatal C6 error must carry the
			// conditional-registration hint so ops can distinguish a
			// typo from a degraded/degradable dependency.
			if !strings.Contains(err.Error(), "CONDITIONAL") {
				t.Fatalf("Start error carries no conditional-registration hint: %v", err)
			}
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
			t.Fatalf("clean live-registry policy rejected at construction: %v", err)
		}
		t.Cleanup(func() { _ = comps.Stop(context.Background()) })
		if err := comps.Start(context.Background()); err != nil {
			t.Fatalf("clean live-registry policy rejected at Start (C6 live-registry leg): %v", err)
		}
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
			t.Fatalf("grant alias rejected at construction: %v", err)
		}
		t.Cleanup(func() { _ = comps.Stop(context.Background()) })
		if err := comps.Start(context.Background()); err != nil {
			t.Fatalf("grant alias rejected by the live C6 leg at Start: %v", err)
		}
	})
}
