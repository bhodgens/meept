package daemon

import (
	"fmt"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/groups"
)

// validateGroupsConfig is the agent-groups boot hook (leaf 02, task T4):
// ONE structural validation of the [security.groups] section at daemon
// config load, invoked from NewComponents BEFORE any component wiring, so
// a defective groups config fails boot with a NAMED defect instead of
// silently half-enforcing (fail closed).
//
// STRUCTURE ONLY this leaf (C6): names non-empty/unique, subjects
// well-formed, scopes well-formed, no dangling policy references, no
// cyclic group-membership chain. Validation against the LIVE tool
// registry (ValidatePolicyTools) is wired by leaf 04, which injects the
// registry-backed normalizer + known set here.
func validateGroupsConfig(cfg *config.Config) error {
	g := cfg.Security.Groups
	if err := groups.Validate(
		groupsPoliciesFromConfig(g.Policies),
		groupsBindingsFromConfig(g.Bindings),
	); err != nil {
		return fmt.Errorf("boot validation failed: %w", err)
	}
	return nil
}

// groupsPoliciesFromConfig maps the config schema types to the model types
// (plain field copies; both are C3 shapes).
func groupsPoliciesFromConfig(in []config.GroupsPolicyConfig) []groups.Policy {
	if len(in) == 0 {
		return nil
	}
	out := make([]groups.Policy, 0, len(in))
	for _, p := range in {
		out = append(out, groups.Policy{
			Name:           p.Name,
			ToolsAllowed:   p.ToolsAllowed,
			ToolsDenied:    p.ToolsDenied,
			MemoryMaxScope: p.MemoryMaxScope,
			FSRoots:        p.FSRoots,
			NetworkClass:   p.NetworkClass,
			AllowedModels:  p.AllowedModels,
		})
	}
	return out
}

// groupsBindingsFromConfig maps binding config rows to model rows.
func groupsBindingsFromConfig(in []config.GroupsBindingConfig) []groups.Binding {
	if len(in) == 0 {
		return nil
	}
	out := make([]groups.Binding, 0, len(in))
	for _, b := range in {
		out = append(out, groups.Binding{
			Subject: b.Subject,
			Policy:  b.Policy,
			Scope:   b.Scope,
		})
	}
	return out
}
