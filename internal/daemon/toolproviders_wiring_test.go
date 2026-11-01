//go:build toolprov_gate

package daemon

import (
	"context"
	"sort"
	"testing"

	"github.com/caimlas/meept/internal/secrets"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
)

// F7 wiring tests. Guarded behind the `toolprov_gate` build tag: the
// tags are added to the go test command (e.g.
// `-tags toolprov_gate`) when wave B lands 01-seam.md; untagged runs
// skip this file so the RED suite compiles today.

// stubWiringProvider is a minimal Provider used to prove the routing
// path without depending on the real provider packages.
type stubWiringProvider struct {
	name  string
	tools []tools.Tool
}

func (p *stubWiringProvider) Name() string                        { return p.name }
func (p *stubWiringProvider) Discover() []tools.Tool              { return p.tools }
func (p *stubWiringProvider) PermissionClass() toolprov.RiskClass { return toolprov.RiskMutating }
func (p *stubWiringProvider) Broker() *secrets.Broker             { return nil }

// wiringTool is a minimal tool a stub provider contributes.
type wiringTool struct {
	name string
}

func (t *wiringTool) Name() string        { return t.name }
func (t *wiringTool) Description() string { return "wiring stub " + t.name }

// TestMigratedGroupsViaProviders pins F7: migrated groups (github,
// scheduler, hermes, acp) arrive in the registry through the provider
// path - registered in toolprov factories, discovered by RegisterAll -
// with names unchanged.
func TestMigratedGroupsViaProviders(t *testing.T) {
	migrated := []string{"github", "scheduler", "hermes", "acp"}

	for _, name := range migrated {
		n := name
		if err := toolprov.Register(n, func() toolprov.Provider {
			return &stubWiringProvider{
				name:  n,
				tools: []tools.Tool{&wiringTool{name: n + "_probe_tool"}},
			}
		}); err != nil {
			t.Fatalf("Register(%s): %v", n, err)
		}
	}

	got := toolprov.Names()
	sort.Strings(got)
	if len(got) < len(migrated) {
		t.Fatalf("Names() = %v, want at least %v", got, migrated)
	}
	for _, n := range migrated {
		found := false
		for _, g := range got {
			if g == n {
				found = true
			}
		}
		if !found {
			t.Errorf("migrated provider %q missing from Names() = %v", n, got)
		}
	}

	reg := tools.NewRegistry(nil)
	if _, err := toolprov.RegisterAll(reg, migrated); err != nil {
		t.Fatalf("RegisterAll(%v): %v", migrated, err)
	}
	for _, n := range migrated {
		if reg.Get(n+"_probe_tool") == nil {
			t.Errorf("provider %q tools did not land in the registry via RegisterAll", n)
		}
	}
	_ = context.Background()
}
