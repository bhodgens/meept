// Package modelselect implements per-child model / provider / reasoning-effort
// selection for spawned delegations (dsh-convergence B4). Derived from dsh's
// tool-subagent model-selection.ts (requestedAgentOptions + assertAllowedModelSelection
// + preflightChildLlmRoute): the child's route is merged with parent < configured <
// request precedence, asserted against a route allowlist, and preflighted through
// meept's resolver before any child goroutine runs.
package modelselect

import (
	"context"
	"errors"
	"fmt"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
)

// AllowedModelRoute is an exact provider+model pair permitted by a
// ModelSelectionPolicy. Both fields must be non-empty.
type AllowedModelRoute struct {
	Provider string
	Model    string
}

// ModelSelectionPolicy is the route allowlist a delegation manager enforces,
// built from app config (e.g. delegation.allowed_routes). A nil policy means
// no allowlist is in force: the only permitted route is the parent's own.
type ModelSelectionPolicy struct {
	Routes []AllowedModelRoute
}

// Sentinel errors so callers can classify failures with errors.Is.
var (
	// ErrEmptyRoute indicates a requested or resolved provider+model route
	// collapsed to an empty value while model selection was in force.
	ErrEmptyRoute = errors.New("modelselect: empty provider/model route")
	// ErrProviderModelTogether indicates only one of provider/model was supplied.
	ErrProviderModelTogether = errors.New("modelselect: provider and model must be supplied together")
	// ErrRouteNotAllowed indicates the resolved route is absent from the allowlist.
	ErrRouteNotAllowed = errors.New("modelselect: route not allowed")
	// ErrUnresolvableRoute indicates preflight found the route unresolvable.
	ErrUnresolvableRoute = errors.New("modelselect: route does not resolve")
)

// ResolveChildRoute merges the per-child route from parent < configured < request
// and asserts the result against an allowlist. It mirrors dsh
// requestedAgentOptions + assertAllowedModelSelection:
//
//   - enabled=false: request selection fields are ignored; the configured route
//     wins (nil configured -> nil, meaning the caller keeps the parent route).
//   - with a policy in force and no selection fields on the request, the
//     FIRST (primary) allowlist route is resolved as the default (C9: the
//     delegate_task tool schema may not carry route fields on every caller,
//     so "no route supplied" must resolve to an ALLOWED route instead of
//     failing every spawn when an allowlist is enforced). Only a policy with
//     NO routes (allow-all) falls back to the configured route / parent route.
//   - a selection requests that provider+model be supplied together (exactly one
//     set is an error); empty strings are rejected.
//   - policy nil permits only the parent route; a non-nil policy requires the
//     resolved route be an exact member.
//   - a route change that does not specify reasoning_effort clears the configured
//     route-owned effort; an explicit request effort always survives.
//
// The returned *ChildOptions is always a fresh copy (never aliasing parent,
// configured, or req).
func ResolveChildRoute(parent agent.ChildOptions, configured *agent.ChildOptions,
	req agent.DelegateRequest, policy *ModelSelectionPolicy, enabled bool) (*agent.ChildOptions, error) {

	// Selection subsystem disabled: request fields are ignored; configured (or
	// nil -> parent route) wins.
	if !enabled {
		return cloneOptions(configured), nil
	}

	hasSelection := req.Provider != "" || req.Model != "" || req.ReasoningEffort != ""
	if !hasSelection {
		// No selection requested. With an allowlist in force, resolve the
		// FIRST (primary) allowed route as the default (C9); a policy with an
		// empty route list means no restriction — keep the configured (or
		// parent) route.
		if policy != nil && len(policy.Routes) > 0 {
			primary := policy.Routes[0]
			return &agent.ChildOptions{Provider: primary.Provider, Model: primary.Model}, nil
		}
		return cloneOptions(configured), nil
	}

	// Provider + model must be supplied together.
	if (req.Provider != "") != (req.Model != "") {
		return nil, fmt.Errorf("%w: provider=%q model=%q", ErrProviderModelTogether, req.Provider, req.Model)
	}

	// Baseline: configured route wins over the parent route; the request then
	// overrides both. A nil configured falls back to the parent route.
	base := parent
	if configured != nil {
		base = *configured
	}

	// Whether the request moves the route off the baseline (needed to decide
	// whether configured route-owned effort is cleared).
	routeChanged := false
	if req.Provider != "" && req.Model != "" {
		routeChanged = req.Provider != base.Provider || req.Model != base.Model
	}

	effort := base.ReasoningEffort
	if req.ReasoningEffort != "" {
		effort = req.ReasoningEffort
	} else if routeChanged {
		// A route change without an explicit effort drops the configured
		// route-owned effort.
		effort = ""
	}

	merged := agent.ChildOptions{
		Provider:        base.Provider,
		Model:           base.Model,
		ReasoningEffort: effort,
	}
	if req.Provider != "" {
		merged.Provider = req.Provider
	}
	if req.Model != "" {
		merged.Model = req.Model
	}

	if merged.Provider == "" || merged.Model == "" {
		return nil, fmt.Errorf("%w: provider=%q model=%q", ErrEmptyRoute, merged.Provider, merged.Model)
	}

	if !routeAllowed(merged, parent, policy) {
		return nil, fmt.Errorf("%w: provider=%q model=%q", ErrRouteNotAllowed, merged.Provider, merged.Model)
	}

	return &merged, nil
}

// routeAllowed reports whether the merged route is permitted: a set policy
// requires an exact member match; a nil policy permits only the parent route.
func routeAllowed(route agent.ChildOptions, parent agent.ChildOptions, policy *ModelSelectionPolicy) bool {
	if policy != nil {
		for _, r := range policy.Routes {
			if r.Provider == route.Provider && r.Model == route.Model {
				return true
			}
		}
		return false
	}
	return route.Provider == parent.Provider && route.Model == parent.Model
}

// PreflightRoute verifies the effective child route resolves through meept's
// resolver BEFORE the child spawns, so an unresolvable route errors visibly at
// the tool boundary with no child created. It uses resolver.HasAlias as the
// reachability test and never enforces quota/slot gating (that is the
// resolver's job at call time).
func PreflightRoute(ctx context.Context, resolver *llm.Resolver, opts agent.ChildOptions) error {
	if opts.Provider == "" || opts.Model == "" {
		return fmt.Errorf("%w: provider=%q model=%q", ErrUnresolvableRoute, opts.Provider, opts.Model)
	}
	if resolver == nil {
		return fmt.Errorf("%w: no resolver available to verify %s/%s", ErrUnresolvableRoute, opts.Provider, opts.Model)
	}
	if !resolver.HasAlias(opts.Model) {
		return fmt.Errorf("%w: model %q (provider %q) is not a known alias/reference", ErrUnresolvableRoute, opts.Model, opts.Provider)
	}
	return nil
}

// SetRoute maps a resolved route onto a delegation request so leaf 01's spawn
// carries the per-child selection verbatim. A nil route or nil on is a no-op.
func SetRoute(on *agent.DelegateRequest, route *agent.ChildOptions) {
	if on == nil || route == nil {
		return
	}
	on.Model = route.Model
	on.Provider = route.Provider
	on.ReasoningEffort = route.ReasoningEffort
}

// cloneOptions returns a copy of opts, or nil when opts is nil.
func cloneOptions(opts *agent.ChildOptions) *agent.ChildOptions {
	if opts == nil {
		return nil
	}
	c := *opts
	return &c
}
