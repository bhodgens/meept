//go:build toolprov_gate

package daemon

import (
	"sort"
	"testing"

	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// F7 wiring tests. Guarded behind the `toolprov_gate` build tag: the
// tags are added to the go test command (e.g.
// `-tags toolprov_gate`) when wave B lands 01-seam.md; untagged runs
// skip this file so the RED suite compiles today.
//
// 03-migrate adjustment (reported to the orchestrator, NOT a
// weakening): the original version stub-registered the four real
// provider names, which (a) predates the real tools.Tool interface
// shape (the stub missed Execute) and (b) now collides with the REAL
// factories builtin's init() populates once leaf 03 lands - the
// duplicate-name guard correctly rejects the stubs. The assertions
// below are correspondingly STRONGER: migrated-group presence is
// pinned against the real init-populated factories, the scheduler and
// hermes groups are walked END-TO-END through their real providers
// (bound with zero-value handles, safe nil-tolerant constructors),
// and the acp group pins the default-off contract: an unbound factory
// must fail closed through RegisterAll with zero tools registered.

// schedulerToolsViaProvider is the exact tool list the former direct
// registration block built, in order.
var schedulerToolsViaProvider = []string{
	"schedule_create",
	"schedule_list",
	"schedule_get",
	"schedule_delete",
	"schedule_pause",
	"schedule_resume",
	"schedule_run_now",
	"cron_create",
}

// TestMigratedGroupsViaProviders pins F7: migrated groups (github,
// scheduler, hermes, acp) arrive in the registry through the provider
// path - registered in toolprov factories by init(), discovered by
// RegisterAll - with names unchanged.
func TestMigratedGroupsViaProviders(t *testing.T) {
	migrated := []string{"github", "scheduler", "hermes", "acp"}

	// Real factories exist for every migrated group (builtin init
	// populated them in this test binary).
	for _, n := range migrated {
		if _, ok := toolprov.Factory(n); !ok {
			t.Errorf("migrated provider %q missing from toolprov factories", n)
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

	// Scheduler group: the real provider walks end-to-end into a fresh
	// registry and contributes the exact former tool list.
	builtin.SetSchedulerProvider(nil, nil)
	reg := tools.NewRegistry(nil)
	count, err := toolprov.RegisterAll(reg, []string{"scheduler"})
	if err != nil {
		t.Fatalf("RegisterAll(scheduler): %v", err)
	}
	if count != len(schedulerToolsViaProvider) {
		t.Errorf("scheduler provider registered %d tools, want %d", count, len(schedulerToolsViaProvider))
	}
	for _, name := range schedulerToolsViaProvider {
		if reg.Get(name) == nil {
			t.Errorf("tool %q missing from the registry after the scheduler provider walk", name)
		}
	}

	// Hermes group: same end-to-end walk (nil client keeps Execute
	// fail-closed; registration shape is what F7 pins).
	builtin.SetHermesProvider(nil, false, nil)
	regHermes := tools.NewRegistry(nil)
	if _, err := toolprov.RegisterAll(regHermes, []string{"hermes"}); err != nil {
		t.Fatalf("RegisterAll(hermes): %v", err)
	}
	if regHermes.Get("hermes") == nil {
		t.Error("tool \"hermes\" missing from the registry after the hermes provider walk")
	}

	// ACP group, disabled default: the factory is unbound in this test
	// binary (no live manager - [acp] enabled defaults false), so the
	// walk must fail closed and register nothing: no subprocess path
	// arms. Reset to unbound FIRST: test parallelism/order can run an
	// enabled-manager wiring test before this one, and the "unbound =>
	// fail closed" property is what the disabled default pins.
	builtin.ResetACPProviderForTest()
	regACP := tools.NewRegistry(nil)
	if _, err := toolprov.RegisterAll(regACP, []string{"acp"}); err == nil {
		t.Fatal("RegisterAll(acp) with an unbound factory succeeded; the disabled default must fail closed")
	}
	if regACP.Get("acp_agent") != nil {
		t.Error("acp_agent registered while the acp provider is unbound (disabled default)")
	}
}
