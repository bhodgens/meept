// Package groups implements the agent-groups policy model (plan tree
// docs/plans/20261030-agent-groups, contract C3): named policies bound to
// subjects via a binding table, resolved deterministically against an
// agent's spec tool grants.
//
// Resolution is a PURE function: no I/O, no clock — everything a caller
// might want to vary (tool-name normalization, injected override allows,
// future time-boxed overrides) travels through ResolveOptions. Validation
// against the LIVE tool registry is injected the same way (C6), so this
// package never imports internal/agent or internal/tools.
//
// Enforcement wiring (dispatch intersection, call-time check, memory
// ceiling) lives in the consuming leaves (04/05); this package is the
// single shared resolution core they must all call.
package groups

import "sort"

// Policy is a named bundle of access rules (C3 verbatim shape).
type Policy struct {
	Name           string   `json:"name"`
	ToolsAllowed   []string `json:"tools_allowed,omitempty"`    // registered tool names
	ToolsDenied    []string `json:"tools_denied,omitempty"`     // deny beats allow
	MemoryMaxScope string   `json:"memory_max_scope,omitempty"` // "", "project", "user", "global"
	FSRoots        []string `json:"fs_roots,omitempty"`         // additional fence roots
	NetworkClass   string   `json:"network_class,omitempty"`    // "", "unrestricted", "allowlist", "none"
	AllowedModels  []string `json:"allowed_models,omitempty"`   // alias names; empty = unrestricted
}

// Binding attaches a policy to a subject (C3 verbatim shape).
type Binding struct {
	Subject string `json:"subject"` // "agent:<id>" | "group:<name>" | "user:<id>"
	Policy  string `json:"policy"`  // policy name
	Scope   string `json:"scope"`   // "daemon" | "project:<id>"
}

// Resolved is the output of Resolve: what a subject is allowed once every
// applicable policy has been layered (C3 verbatim shape).
type Resolved struct {
	// AllowedTools is spec tools ∩ union(group allows) minus union(group
	// denies). Intersection never widens the spec (D4).
	AllowedTools []string
	// DeniedTools is the union of denied names the spec actually granted —
	// the part of the deny set that bit THIS agent (diagnosis surface).
	DeniedTools []string
	// MemoryMaxScope is the narrowest scope across groups
	// ("" = unrestricted).
	MemoryMaxScope string
	// FSRoots unions every group's fence roots.
	FSRoots []string
	// NetworkClass is the most restrictive class across groups.
	NetworkClass string
	// AllowedModels is the intersection of per-group model aliases when
	// groups constrain models (empty = unrestricted); the union members
	// are kept sorted+deduped for diagnosis when only one group
	// constrains.
	AllowedModels []string
	// Managed reports that group policy was ACTIVE for this subject
	// (a matching binding resolved, or default_managed routed it through
	// the default policy). An empty AllowedTools with Managed=true means
	// DENY-ALL (D8 — the caller decides fail-closed; FilteredToolRegistry's
	// legacy empty=all behavior must never fire for a managed subject).
	Managed bool
	// Groups carries the subject's effective group memberships that were
	// consulted for this resolution (diagnosis + decision-audit surface;
	// empty for the default-policy route and the unmanaged record).
	Groups []string
}

// scopeRank orders memory scopes from narrowest (0) to widest. Unknown
// values rank widest so an unrecognized scope never widens anything.
var scopeRank = map[string]int{
	"project": 0,
	"user":    1,
	"global":  2,
	"":        3,
}

// networkRank orders network classes from most restrictive (0) to
// least restrictive. Unknown values rank most restrictive so an
// unrecognized class never widens access.
var networkRank = map[string]int{
	"none":         0,
	"allowlist":    1,
	"unrestricted": 2,
	"":             2,
}

// sortDedup returns the input sorted and de-duplicated (stable, no
// allocation churn for empty input).
func sortDedup(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}
