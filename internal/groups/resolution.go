// resolution.go — the loop-carried resolution seam (agent-groups leaf
// 04-tool-gates). GroupsResolver owns the policy/binding tables plus the
// injection seams (tool-name normalization, override extra-allows, live
// registry names for C6 validation) and answers one question per agent:
//
//	what is this subject allowed? (groups.Resolve / ResolveDefaultManaged)
//
// internal/agent consumes this (not the raw Resolve calls) so every gate —
// dispatch intersection in filterTools, call-time check in the executor,
// memory ceiling (leaf 05) — reads ONE resolution of the SAME tables. The
// resolver is read-only after construction: config hot reloads build a NEW
// resolver and re-wire it (Snapshot returns the current value atomically),
// never a mutation under an in-flight check.
package groups

import "sync"

// Resolver resolves agent subjects against a fixed policy/binding table.
// Construct with NewResolver (nil options is a valid, fully default
// resolver). Safe for concurrent use: the tables are immutable, so reads
// need no lock.
type Resolver struct {
	policies       []Policy
	bindings       []Binding
	defaultManaged bool
	// opts feeds the pure Resolve calls (normalizer + extra allows).
	opts *ResolveOptions
	// ropts keeps the construction options so ValidatePolicies can
	// reach the injected live-registry name set (C6).
	ropts *ResolverOptions
}

// ResolverOptions carries the construction seams. All fields optional.
type ResolverOptions struct {
	// NormalizeToolName maps declared names to registered names
	// ("shell_execute" -> "shell"); injected by the daemon from the
	// agent-grant vocabulary so internal/groups never imports
	// internal/agent or internal/tools. Nil = identity.
	NormalizeToolName func(string) string
	// ExtraAdds are override allow names (leaf 07's reader fills this
	// later; nil default). They ADD allows only — never rescue a deny,
	// never widen the spec.
	ExtraAllows []string
	// ExtraAllowsFor is leaf 07's PER-SUBJECT override seam: given the
	// agent subject ("agent:<id>"), it returns the live override grants
	// for that subject (OverrideSet.AllowsFor bound to the load clock).
	// Consulted only when ExtraAllows is empty. Same precedence: adds
	// allows only, deny beats override, spec is the ceiling.
	ExtraAllowsFor func(subject string) []string
	// knownTools is the LIVE registered-tool-name set (C6). Boot and
	// RPC validation reject policy tool names outside this set; set via
	// WithKnownTools (below) so the raw field stays construction-only.
	knownTools map[string]struct{}
}

// WithKnownTools returns a copy carrying the live registry name set.
func (o ResolverOptions) WithKnownTools(known map[string]struct{}) ResolverOptions {
	o.knownTools = known
	return o
}

// NewResolver builds a resolver over the given tables. tables may carry
// nil slices (an empty resolver denies nothing and manages nothing).
func NewResolver(policies []Policy, bindings []Binding, defaultManaged bool, opts *ResolverOptions) *Resolver {
	r := &Resolver{
		policies:       policies,
		bindings:       bindings,
		defaultManaged: defaultManaged,
	}
	if opts != nil {
		r.ropts = &ResolverOptions{
			NormalizeToolName: opts.NormalizeToolName,
			ExtraAllows:       opts.ExtraAllows,
			ExtraAllowsFor:    opts.ExtraAllowsFor,
			knownTools:        opts.knownTools,
		}
		r.opts = &ResolveOptions{
			NormalizeToolName: opts.NormalizeToolName,
			ExtraAllows:       opts.ExtraAllows,
			ExtraAllowsFor:    opts.ExtraAllowsFor,
		}
	}
	return r
}

// extraAllowsForSubject answers the per-subject override query through the
// injected seam (nil-safe). Used by ResolveForAgent so each resolution
// consults the LIVE set for its own subject instead of a construction-time
// snapshot.
func (r *Resolver) extraAllowsForSubject(subject string) []string {
	if r == nil || r.ropts == nil || r.ropts.ExtraAllowsFor == nil {
		return nil
	}
	return r.ropts.ExtraAllowsFor(subject)
}

// DefaultManaged reports whether ungrouped subjects route through the
// fail-closed `default` policy (D7). The dispatch gate consults this to
// keep ungrouped agents byte-identical when the knob is false.
func (r *Resolver) DefaultManaged() bool {
	if r == nil {
		return false
	}
	return r.defaultManaged
}

// ResolveForAgent answers C3 for one agent: a matching binding (agent or
// one-level group) resolves through the layered policies; with no matching
// binding, default_managed=true routes through the fail-closed `default`
// policy, false yields the unmanaged record (Managed false).
//
// Leaf 07 per-subject overrides: when no static ExtraAllows list is set,
// the injected ExtraAllowsFor seam answers THIS agent's live grant set
// ("agent:<agentID>") before resolving — a per-resolution query through
// the reader's clock, so overrides loaded mid-window expire correctly
// without rebuilding the resolver. Identical precedence: deny beats
// override, spec is the ceiling.
func (r *Resolver) ResolveForAgent(agentID string, groupMemberships, specTools []string) Resolved {
	if r == nil {
		return Resolved{Managed: false}
	}
	resOpts := r.opts
	if (resOpts == nil || len(resOpts.ExtraAllows) == 0) &&
		(len(r.bindings) > 0 || r.defaultManaged) &&
		r.ropts != nil && r.ropts.ExtraAllowsFor != nil {
		allow := r.extraAllowsForSubject("agent:" + agentID)
		if resOpts == nil {
			resOpts = &ResolveOptions{NormalizeToolName: r.ropts.NormalizeToolName}
		}
		resOpts = &ResolveOptions{
			NormalizeToolName: resOpts.NormalizeToolName,
			ExtraAllows:       allow,
		}
	}
	if len(r.bindings) > 0 {
		res := Resolve(specTools, agentID, groupMemberships, r.policies, r.bindings, resOpts)
		if res.Managed {
			return res
		}
	}
	if r.defaultManaged {
		return ResolveDefaultManaged(specTools, r.policies, resOpts)
	}
	return Resolved{Managed: false}
}

// AtomicResolver carries a *Resolver behind a mutex so daemon wiring can
// swap it on config reload without racing an in-flight resolution. internal
// /agent reads through it at loop-creation time; the zero value is a valid
// empty resolver. Assignment via Set / Store, never direct field writes.
type AtomicResolver struct {
	mu sync.RWMutex
	r  *Resolver
}

// Set stores the resolver (nil clears — every gate then sees unmanaged).
func (a *AtomicResolver) Set(r *Resolver) {
	a.mu.Lock()
	a.r = r
	a.mu.Unlock()
}

// Get returns the current resolver (nil = none wired).
func (a *AtomicResolver) Get() *Resolver {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.r
}

// ValidatePolicies runs the structural validation (Validate) plus the C6
// live-registry tool-name check (ValidatePolicyTools) against this
// resolver's tables. Used at daemon boot and exposed for the groups.*
// RPC surface. Without a known-tools set (nil registry, tests) the C6
// leg is inert — structural validation still applies.
func (r *Resolver) ValidatePolicies() error {
	if r == nil {
		return nil
	}
	if err := Validate(r.policies, r.bindings); err != nil {
		return err
	}
	known := r.ropts.KnownTools()
	if len(known) == 0 {
		return nil
	}
	return ValidatePolicyTools(r.policies, known, r.ropts.NormalizeToolName)
}

// KnownTools returns the injected registered-tool-name set (nil-safe).
func (o *ResolverOptions) KnownTools() map[string]struct{} {
	if o == nil {
		return nil
	}
	return o.knownTools
}
