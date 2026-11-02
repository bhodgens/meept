package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
)

// TestGroupsBootValidate pins T4: a structurally defective
// [security.groups] config FAILS NewComponents with an error naming the
// defect; a clean (or empty) groups config boots fine.
func TestGroupsBootValidate(t *testing.T) {
	logger := testLogger(t)

	t.Run("clean config boots", func(t *testing.T) {
		msgBus := bus.New(nil, logger)
		tmpDir := t.TempDir()
		cfg := &config.Config{
			Daemon: config.DaemonConfig{DataDir: tmpDir},
			Security: config.SecurityConfig{
				AllowedPaths: []string{tmpDir},
				Groups: config.SecurityGroupsConfig{
					Policies: []config.GroupsPolicyConfig{
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
			t.Fatalf("clean groups config rejected at boot: %v", err)
		}
		t.Cleanup(func() {
			_ = comps.Stop(context.Background())
		})
	})

	t.Run("dangling policy reference fails boot naming it", func(t *testing.T) {
		msgBus := bus.New(nil, logger)
		tmpDir := t.TempDir()
		cfg := &config.Config{
			Daemon: config.DaemonConfig{DataDir: tmpDir},
			Security: config.SecurityConfig{
				AllowedPaths: []string{tmpDir},
				Groups: config.SecurityGroupsConfig{
					Bindings: []config.GroupsBindingConfig{
						{Subject: "agent:web-bot", Policy: "missing_policy", Scope: "daemon"},
					},
				},
			},
		}
		_, err := NewComponents(context.Background(), cfg, msgBus, logger)
		if err == nil {
			t.Fatal("dangling groups policy ref booted; want boot failure")
		}
		if !strings.Contains(err.Error(), "missing_policy") {
			t.Fatalf("boot error does not NAME the defect: %v", err)
		}
		if !strings.Contains(err.Error(), "dangling_policy") {
			t.Fatalf("boot error does not carry the defect kind: %v", err)
		}
	})

	t.Run("cyclic group chain fails boot naming the group", func(t *testing.T) {
		msgBus := bus.New(nil, logger)
		tmpDir := t.TempDir()
		cfg := &config.Config{
			Daemon: config.DaemonConfig{DataDir: tmpDir},
			Security: config.SecurityConfig{
				AllowedPaths: []string{tmpDir},
				Groups: config.SecurityGroupsConfig{
					Policies: []config.GroupsPolicyConfig{
						{Name: "b"}, {Name: "a"},
					},
					Bindings: []config.GroupsBindingConfig{
						{Subject: "group:a", Policy: "b", Scope: "daemon"},
						{Subject: "group:b", Policy: "a", Scope: "daemon"},
					},
				},
			},
		}
		_, err := NewComponents(context.Background(), cfg, msgBus, logger)
		if err == nil {
			t.Fatal("cyclic group chain booted; want boot failure")
		}
		if !strings.Contains(err.Error(), "cyclic_group") {
			t.Fatalf("boot error does not carry cyclic_group kind: %v", err)
		}
	})

	t.Run("empty groups section is a no-op", func(t *testing.T) {
		msgBus := bus.New(nil, logger)
		tmpDir := t.TempDir()
		cfg := &config.Config{
			Daemon: config.DaemonConfig{DataDir: tmpDir},
			Security: config.SecurityConfig{
				AllowedPaths: []string{tmpDir},
			},
		}
		comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
		if err != nil {
			t.Fatalf("default (empty) groups section rejected: %v", err)
		}
		t.Cleanup(func() {
			_ = comps.Stop(context.Background())
		})
	})
}
