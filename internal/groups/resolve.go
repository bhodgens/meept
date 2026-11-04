package groups

import (
	"fmt"
	"sort"
	"strings"
)

// ResolveOptions carries the injection seams so Resolve stays pure (no
// I/O, no clock, no registry import). Nil fields take the defaults:
// unnormalized tool names, no override allows.
type ResolveOptions struct {
	// NormalizeToolName maps a declared tool name to its registered
	// name before comparison (the C6 seam: leaf 04 injects the
	// registry-backed normalizer so internal/groups never imports
	// internal/agent or internal/tools). Nil = identity. The grant-side
	// precedent is internal/agent CanonicalToolName ("shell_execute" ->
	// "shell").
	NormalizeToolName func(string) string
	// ExtraAllows adds override allow names (leaf 07's time-boxed
	// overrides). Overrides ADD allows only — they can never rescue a
	// DENIED tool (deny beats override), never widen a ceiling, and are
	// still intersected with the spec (an override never widens the
	// agent's own grants). Nil/empty = no overrides.
	ExtraAllows []string
	// ExtraAllowsFor is leaf 07's per-subject variant, consulted only
	// when ExtraAllows is empty: given the agent subject ("agent:<id>"),
	// it returns the live override grants (OverrideSet.AllowsFor with
	// the query clock). Same add-only precedence as ExtraAllows.
	ExtraAllowsFor func(subject string) []string
}

// defaultManagedPolicy is the policy name routed for UNGROUPED subjects
// when default_managed=true (D7): a config section carrying a policy with
// this name makes unmanaged agents run fail-closed through it.
const defaultManagedPolicy = "default"

// normalize applies opts.NormalizeToolName with the nil guard.
func normalize(opts *ResolveOptions, name string) string {
	if opts != nil && opts.NormalizeToolName != nil {
		if n := opts.NormalizeToolName(name); n != "" {
			return n
		}
	}
	return name
}

// PoliciesByID indexes the config policy table by name. Duplicate names
// keep the FIRST entry and the conflict is reported by Validate — Resolve
// must stay total over any config.
func PoliciesByID(policies []Policy) map[string]Policy {
	out := make(map[string]Policy, len(policies))
	for _, p := range policies {
		if _, dup := out[p.Name]; !dup {
			out[p.Name] = p
		}
	}
	return out
}

// resolveLayered applies the FROZEN C3 precedence across the policies a
// subject's bindings selected:
//
//  1. denies first (deny beats allow — D4);
//  2. allows merge by union, then intersect with specTools (an binding
//     never widens the spec);
//  3. ceilings merge most-restrictive-wins (narrowest memory scope,
//     most restrictive network class);
//  4. FSRoots and AllowedModels union across policies (additional fence
//     roots and alias constraints are additive constraints; the fence
//     gate enforces the union of roots).
//
// managed is carried verbatim into the Resolved record so the caller can
// apply D8 fail-closed semantics.
func resolveLayered(specTools []string, policies []Policy, opts *ResolveOptions, managed bool) Resolved {
	res := Resolved{Managed: managed}

	specSet := make(map[string]struct{}, len(specTools))
	for _, t := range specTools {
		specSet[normalize(opts, t)] = struct{}{}
	}

	denySet := make(map[string]struct{})
	allowUnion := make(map[string]struct{})
	extra := make(map[string]struct{})
	if opts != nil {
		for _, t := range opts.ExtraAllows {
			extra[normalize(opts, t)] = struct{}{}
		}
	}

	narrowed := false
	sawMemory := false
	sawNetwork := false
	modelsConstrained := false
	modelIntersection := map[string]struct{}{}
	var fsRoots []string

	for i := range policies {
		p := policies[i]
		for _, d := range p.ToolsDenied {
			denySet[normalize(opts, d)] = struct{}{}
		}
		for _, a := range p.ToolsAllowed {
			allowUnion[normalize(opts, a)] = struct{}{}
		}
		if rank, ok := scopeRank[p.MemoryMaxScope]; ok && p.MemoryMaxScope != "" {
			if !sawMemory || rank < scopeRank[res.MemoryMaxScope] {
				res.MemoryMaxScope = p.MemoryMaxScope
			}
			sawMemory = true
			narrowed = true
		}
		if p.NetworkClass != "" {
			if rank, ok := networkRank[p.NetworkClass]; ok {
				if !sawNetwork || rank < networkRank[res.NetworkClass] {
					res.NetworkClass = p.NetworkClass
				}
				sawNetwork = true
				narrowed = true
			}
		}
		fsRoots = append(fsRoots, p.FSRoots...)
		if len(p.AllowedModels) > 0 {
			set := make(map[string]struct{}, len(p.AllowedModels))
			for _, m := range p.AllowedModels {
				set[m] = struct{}{}
			}
			if !modelsConstrained {
				modelIntersection = set
				modelsConstrained = true
			} else {
				for m := range modelIntersection {
					if _, keep := set[m]; !keep {
						delete(modelIntersection, m)
					}
				}
			}
		}
	}

	// Denied tools that the spec actually granted (the part of the deny
	// set that bit this agent).
	for t := range specSet {
		if _, denied := denySet[t]; denied {
			res.DeniedTools = append(res.DeniedTools, t)
		}
	}
	sort.Strings(res.DeniedTools)

	// Allowed = spec ∩ (allowUnion ∪ overrides) − denies. Empty policy
	// allowUnion with policies present means "no group allow" — under an
	// active policy table the union of allows governs; an entirely empty
	// effective allowlist yields AllowedTools empty + Managed=true (D8:
	// the caller denies everything fail-closed).
	effective := make(map[string]struct{})
	for t := range allowUnion {
		effective[t] = struct{}{}
	}
	for t := range extra {
		effective[t] = struct{}{}
	}
	for t := range specSet {
		if _, allowed := effective[t]; !allowed {
			continue
		}
		if _, denied := denySet[t]; denied {
			continue
		}
		res.AllowedTools = append(res.AllowedTools, t)
	}
	sort.Strings(res.AllowedTools)

	if modelsConstrained {
		res.AllowedModels = make([]string, 0, len(modelIntersection))
		for m := range modelIntersection {
			res.AllowedModels = append(res.AllowedModels, m)
		}
		sort.Strings(res.AllowedModels)
	}
	res.FSRoots = sortDedup(fsRoots)
	_ = narrowed
	return res
}

// Resolve answers "given this agent's spec tools and groups, and the
// policy/binding tables, what is this agent allowed?" (C3). Deterministic,
// pure, table-tested; shared by every consumer (tool gates, memory
// ceiling, diagnosis).
//
// subject is the agent identity ("agent:<id>" form is built here from
// agentID). groups are the subject's group memberships; bindings whose
// subject is "agent:<agentID>" or one of those groups (exactly ONE level
// of nesting — group policies only, never group-of-group) select the
// policies to layer.
//
// opts may be nil.
func Resolve(specTools []string, agentID string, groupMemberships []string, policies []Policy, bindings []Binding, opts *ResolveOptions) Resolved {
	byName := PoliciesByID(policies)

	// Collect the policies bound to this agent directly or through its
	// groups (one level: a binding subject "group:X" selects policy P for
	// members of X — X is never itself re-expanded as a subject).
	member := make(map[string]struct{}, len(groupMemberships))
	for _, g := range groupMemberships {
		member[g] = struct{}{}
	}

	agentSubject := "agent:" + agentID
	var bound []Policy
	boundNames := make(map[string]struct{})
	for _, b := range bindings {
		selected := false
		switch {
		case b.Subject == agentSubject:
			selected = true
		case strings.HasPrefix(b.Subject, "group:"):
			if _, ok := member[strings.TrimPrefix(b.Subject, "group:")]; ok {
				selected = true
			}
		default:
			// "user:<id>" and unknown subject kinds never match an
			// agent resolution.
			continue
		}
		if !selected {
			continue
		}
		if _, dup := boundNames[b.Policy]; dup {
			continue
		}
		p, ok := byName[b.Policy]
		if !ok {
			// Dangling refs are Validate's defect (named error);
			// Resolve degrades to "binding not resolvable" by skipping.
			continue
		}
		boundNames[b.Policy] = struct{}{}
		bound = append(bound, p)
	}

	if len(bound) == 0 {
		// Ungrouped (D7): with default_managed=false the agent keeps
		// today's behavior — spec allowlist only, Managed=false so
		// every legacy path (including FilteredToolRegistry's empty=all)
		// stays byte-identical. Managed routing (default policy) is the
		// CALLER's decision via ResolveDefaultManaged.
		return Resolved{Managed: false, Groups: sortedCopy(groupMemberships)}
	}
	res := resolveLayered(specTools, bound, opts, true)
	res.Groups = sortedCopy(groupMemberships)
	return res
}

// sortedCopy returns a sorted, de-duplicated, nil-when-empty copy (the
// diagnosis/audit surface for the subject's groups).
func sortedCopy(in []string) []string {
	return sortDedup(in)
}

// ResolveDefaultManaged resolves a subject with NO matching bindings under
// default_managed=true (D7): the `default` policy applies fail-closed. A
// missing `default` policy is a Validate defect; at resolve time it yields
// an empty-allowlist Managed record (deny-everything), which is the safe
// direction.
func ResolveDefaultManaged(specTools []string, policies []Policy, opts *ResolveOptions) Resolved {
	byName := PoliciesByID(policies)
	p, ok := byName[defaultManagedPolicy]
	if !ok {
		// No default policy defined: deny-everything record, the reason
		// names the knob so diagnosis can point at the config.
		return Resolved{
			Managed:      true,
			NetworkClass: "none",
		}
	}
	return resolveLayered(specTools, []Policy{p}, opts, true)
}

// String renders a one-line summary for logs (diagnosis aid; never a
// substitute for explain output).
func (r Resolved) String() string {
	return fmt.Sprintf("managed=%t allowed=%d denied=%d memory=%q network=%q",
		r.Managed, len(r.AllowedTools), len(r.DeniedTools), r.MemoryMaxScope, r.NetworkClass)
}
