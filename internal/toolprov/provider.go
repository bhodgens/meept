// Package toolprov is the compiled-in tool provider seam (contract C1,
// docs/plans/20261031-tool-provider-seam/master.md).
//
// Provider packages contribute tools through the Provider interface and
// self-register a construction factory via Register (typically from
// init()). Daemon wiring walks RegisterAll to construct the enabled
// providers and land their tools in a tools.Registry. The import
// direction is strictly provider -> toolprov -> tools (+ secrets):
// toolprov NEVER imports a provider package, internal/tools/builtin, or
// internal/githubsvc - the same inversion proven by
// githubsvc.ToolFactories, with builtin populating factories from its
// own init().
//
// Construction is lazy: Register stores the thunk, Factory returns it
// without invoking, and only RegisterAll (and config mapping in leaf 04)
// constructs. A disabled provider therefore never runs any work beyond
// map insertion.
package toolprov

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/secrets"
	"github.com/caimlas/meept/internal/tools"
)

// RiskClass is the permission ceiling a provider declares for ALL tools
// it contributes. Fail-closed: a provider that does not declare a class
// is treated as the most restrictive tier by consumers.
type RiskClass string

const (
	// RiskReadOnly marks a provider whose tools never mutate state.
	RiskReadOnly RiskClass = "read_only"
	// RiskMutating marks a provider whose tools may mutate state.
	RiskMutating RiskClass = "mutating"
	// RiskPrivileged marks a provider whose tools may spawn subprocesses
	// or cross trust boundaries.
	RiskPrivileged RiskClass = "privileged"
)

// Provider is the compiled-in provider seam (C1). Provider packages
// contribute tools through Discover(); the PermissionClass is the
// ceiling for every tool the provider contributes.
type Provider interface {
	// Name returns the provider identifier; it is the config key used
	// by [tool_providers] and must be unique across factories.
	Name() string
	// Discover returns the tools this provider contributes.
	Discover() []tools.Tool
	// PermissionClass returns the provider-wide risk ceiling.
	PermissionClass() RiskClass
	// Broker returns the secrets broker binding. Providers authenticate
	// via in-process Value only; child lanes get placeholders.
	Broker() *secrets.Broker
}

// factories is the package-level provider factory map. It is populated
// exclusively through Register (provider packages' init()); the mutex
// guards the map, the seed state below, and the tool-posture reverse
// index ProviderForTool serves.
var (
	factoriesMu sync.Mutex
	factories   = make(map[string]func() Provider)

	// toolPostures is the reverse index canonical tool name -> the
	// provider that contributed it and that provider's declared risk
	// ceiling. It is written ONLY by RegisterAll (the sole path provider
	// tools take into a registry), never pruned - factory registration
	// is init-only and immutable in a running binary.
	toolPostures = make(map[string]toolPosture)
)

// normalizeToolName lowercases and trims a tool name so ProviderForTool
// accepts the same name shapes the registry's canonical key comparison
// does (internal/tools keeps the equivalent helper private, and toolprov
// cannot reach into the registry's map).
func normalizeToolName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// toolPosture is one entry of the reverse index.
type toolPosture struct {
	provider string
	class    RiskClass
}

// recordToolPostures inserts every tool the provider contributes into
// the reverse index under the provider's declared class. Duplicate
// canonical names across providers are a registration-order conflict:
// the first seam walk wins and later overwrites are refused by the
// caller's registry duplicate detection before this map can disagree.
func recordToolPostures(provider Provider, discovered []tools.Tool) {
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	class := provider.PermissionClass()
	for _, tool := range discovered {
		if tool == nil {
			continue
		}
		cn := tools.CanonicalName(tool)
		if cn == "" {
			continue
		}
		toolPostures[cn] = toolPosture{provider: provider.Name(), class: class}
	}
}

// Register stores the construction factory for the named provider. The
// factory is NOT invoked here (construction is lazy); a duplicate name
// returns an error naming the clash and leaves the first factory
// untouched - a duplicate provider name is a registration-time error,
// never a silent overwrite. Safe for concurrent use.
func Register(name string, f func() Provider) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("toolprov: Register: provider name must not be empty")
	}
	if f == nil {
		return fmt.Errorf("toolprov: Register(%q): nil factory", name)
	}
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	if _, exists := factories[name]; exists {
		return fmt.Errorf("toolprov: Register: provider %q is already registered; duplicate provider names are rejected, never silently overwritten", name)
	}
	factories[name] = f
	return nil
}

// Factory returns the registered construction thunk for the named
// provider without invoking it. The second return reports whether the
// name is registered at all.
func Factory(name string) (func() Provider, bool) {
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	f, ok := factories[name]
	return f, ok
}

// Names returns the sorted registered provider names, for doctor and
// config surfacing.
func Names() []string {
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	names := make([]string, 0, len(factories))
	for name := range factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ProviderForTool reports which registered provider contributed the
// named tool, and the risk ceiling that provider declared. The reverse
// index is maintained at RegisterAll time (the only path provider tools
// take into a registry), so a tool that never went through the seam has
// no entry and ok is false - callers then fall back to the fail-closed
// default (mutating) posture.
//
// Concurrency: the same mutex as the factory map guards the index;
// lookups are map reads only and stay cheap.
func ProviderForTool(toolName string) (providerName string, riskClass RiskClass, ok bool) {
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	p, found := toolPostures[normalizeToolName(toolName)]
	if !found {
		return "", "", false
	}
	return p.provider, p.class, true
}

// RegisterAll constructs each named provider and registers every tool
// its Discover() returns into r, returning the number of tools
// registered. Behavior:
//
//   - Unknown provider name: error naming the unknown entry and listing
//     the known (sorted) names; tools registered before the failure stay
//     registered.
//   - A tool whose canonical name collides with an entry already in the
//     registry, or with a tool the same walk already registered, is an
//     error: the registry's Register replaces silently, so the seam adds
//     duplicate detection here and never renames on the provider's
//     behalf.
//   - A factory that returns a nil Provider is an error.
func RegisterAll(r *tools.Registry, names []string) (int, error) {
	if r == nil {
		return 0, fmt.Errorf("toolprov: RegisterAll: nil registry")
	}
	registered := 0
	for _, name := range names {
		f, ok := Factory(name)
		if !ok {
			known := Names()
			if len(known) == 0 {
				return registered, fmt.Errorf("toolprov: unknown tool provider %q (no providers are registered)", name)
			}
			return registered, fmt.Errorf("toolprov: unknown tool provider %q (known: %s)", name, strings.Join(known, ", "))
		}
		p := f()
		if p == nil {
			return registered, fmt.Errorf("toolprov: provider factory %q returned a nil Provider", name)
		}
		discovered := p.Discover()
		recordToolPostures(p, discovered)
		existing := r.CanonicalNames()
		seen := make(map[string]struct{})
		for _, tool := range discovered {
			if tool == nil {
				continue
			}
			cn := tools.CanonicalName(tool)
			if _, dup := seen[cn]; dup {
				return registered, fmt.Errorf("toolprov: provider %q contributes duplicate tool name %q", p.Name(), tool.Name())
			}
			if _, dup := existing[cn]; dup {
				return registered, fmt.Errorf("toolprov: provider %q contributes tool %q which is already registered in the registry", p.Name(), tool.Name())
			}
			seen[cn] = struct{}{}
			r.Register(tool)
			registered++
		}
	}
	return registered, nil
}
