package daemon

// GRP-2 wiring remediation: the daemon template loop must carry the
// loop-carried group resolution record exactly like the registry lane does.
// Under security.groups.default_managed=true a nil record on the template
// lane skipped the executor's call-time gate entirely — the bypass class
// leaf 04 claims to close. These tests pin the wiring through the real
// NewComponents path and through the shared wiring helper.

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/groups"
)

func TestTemplateLoopGroupResolutionWiring(t *testing.T) {
	logger := testLogger(t)

	t.Run("template loop carries a resolution record", func(t *testing.T) {
		msgBus := bus.New(nil, logger)
		tmpDir := t.TempDir()
		cfg := &config.Config{
			Daemon: config.DaemonConfig{DataDir: tmpDir},
			Security: config.SecurityConfig{
				AllowedPaths: []string{tmpDir},
				Groups: config.SecurityGroupsConfig{
					// default_managed routes every subject (the template
					// loop's "daemon" subject included) through the default
					// policy fail-closed posture (D7).
					DefaultManaged: true,
					Policies: []config.GroupsPolicyConfig{
						{Name: "default", ToolsAllowed: []string{"file_read"}},
					},
				},
			},
		}
		comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
		if err != nil {
			t.Fatalf("NewComponents: %v", err)
		}
		t.Cleanup(func() { _ = comps.Stop(context.Background()) })
		if comps.AgentLoop == nil {
			t.Fatal("nil template loop")
		}
		res := comps.AgentLoop.GroupResolution()
		if res == nil {
			t.Fatal("template loop carries NO group resolution record (GRP-2 bypass class)")
		}
		if !res.Managed {
			t.Fatalf("record not managed under default_managed=true: %+v", res)
		}
		// The allowlist is the spec-tools intersection; the template loop
		// grants no spec tools, so the managed record denies everything
		// (D8 fail-closed). What matters for the bypass class is that the
		// gate is ARMED (Managed=true), not the empty set itself.
		if len(res.AllowedTools) != 0 {
			t.Logf("note: template record allows %v (spec grants none)", res.AllowedTools)
		}
	})

	t.Run("empty groups config stamps an unmanaged record", func(t *testing.T) {
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
			t.Fatalf("NewComponents: %v", err)
		}
		t.Cleanup(func() { _ = comps.Stop(context.Background()) })
		if comps.AgentLoop == nil {
			t.Fatal("nil template loop")
		}
		res := comps.AgentLoop.GroupResolution()
		if res == nil {
			t.Fatal("empty-table resolver still stamps a record (the explicit-unmanaged posture)")
		}
		if res.Managed {
			t.Fatalf("empty groups config must keep the legacy unmanaged posture: %+v", res)
		}
	})
}

// TestResolveForAgentNilSpecTools pins the resolver semantics the template
// wiring relies on: with no bindings and default_managed off, a record
// resolves unmanaged; with default_managed on it resolves managed
// (fail-closed, D8) even when the subject grants no tools.
func TestResolveForAgentNilSpecTools(t *testing.T) {
	empty := groups.NewResolver(nil, nil, false, &groups.ResolverOptions{
		NormalizeToolName: agent.CanonicalToolName,
	})
	res := empty.ResolveForAgent("daemon", nil, nil)
	if res.Managed {
		t.Fatalf("empty resolver + no default_managed resolved managed: %+v", res)
	}

	managed := groups.NewResolver(
		[]groups.Policy{{Name: "default", ToolsAllowed: []string{"file_read"}}},
		nil, true,
		&groups.ResolverOptions{NormalizeToolName: agent.CanonicalToolName},
	)
	res = managed.ResolveForAgent("daemon", nil, nil)
	if !res.Managed {
		t.Fatalf("default_managed resolver resolved unmanaged: %+v", res)
	}
}
