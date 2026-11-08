package modelselect

// T4 gate (01-delegation/00T-tests.md): B4 model-route selection pins.
// Contract .hermes/contracts/01-delegation.md B4 (exact signatures):
//   type ModelSelectionPolicy struct { Routes []AllowedModelRoute }
//   func ResolveChildRoute(parent ChildOptions, configured *ChildOptions,
//       req DelegateRequest, policy *ModelSelectionPolicy, enabled bool) (*ChildOptions, error)
//   func PreflightRoute(ctx context.Context, resolver *llm.Resolver, opts ChildOptions) error
// dsh model-selection.ts rules: provider+model supplied together; empty
// string rejected; policy nil permits only the parent route; route changed
// + no effort -> configured route-owned effort cleared; no selection
// requested -> configured (may be nil -> parent route).
//
// RED until leaf 04-model-selection lands (package does not exist).

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/agent"
)

func parentRoute() agent.ChildOptions {
	return agent.ChildOptions{Provider: "local", Model: "lfm-8b"}
}

func TestResolveChildRoute_NoSelectionRequested_NilConfigured(t *testing.T) {
	got, err := ResolveChildRoute(parentRoute(), nil, agent.DelegateRequest{}, nil, true)
	if err != nil {
		t.Fatalf("ResolveChildRoute: %v", err)
	}
	if got != nil {
		t.Errorf("no selection + nil configured -> %+v, want nil (parent route kept)", got)
	}
}

func TestResolveChildRoute_NoSelectionRequested_Configured(t *testing.T) {
	configured := &agent.ChildOptions{Provider: "local", Model: "lfm-1.2b"}
	got, err := ResolveChildRoute(parentRoute(), configured, agent.DelegateRequest{}, nil, true)
	if err != nil {
		t.Fatalf("ResolveChildRoute: %v", err)
	}
	if got == nil || got.Model != "lfm-1.2b" {
		t.Fatalf("no selection requested -> configured, got %+v", got)
	}
}

func TestResolveChildRoute_PolicyAllowsRequest(t *testing.T) {
	policy := &ModelSelectionPolicy{Routes: []AllowedModelRoute{
		{Provider: "cloud", Model: "gpt"},
	}}
	got, err := ResolveChildRoute(parentRoute(), nil, agent.DelegateRequest{
		Provider: "cloud", Model: "gpt",
	}, policy, true)
	if err != nil {
		t.Fatalf("ResolveChildRoute: %v", err)
	}
	if got == nil || got.Provider != "cloud" || got.Model != "gpt" {
		t.Fatalf("route = %+v, want cloud:gpt", got)
	}
}

func TestResolveChildRoute_PolicyDeniesOutsideAllowlist(t *testing.T) {
	policy := &ModelSelectionPolicy{Routes: []AllowedModelRoute{
		{Provider: "cloud", Model: "gpt"},
	}}
	if _, err := ResolveChildRoute(parentRoute(), nil, agent.DelegateRequest{
		Provider: "other", Model: "w",
	}, policy, true); err == nil {
		t.Fatal("route outside the policy allowlist must error")
	}
}

func TestResolveChildRoute_NilPolicyPermitsParentRouteOnly(t *testing.T) {
	// A requested change away from the parent route with no policy is denied.
	if _, err := ResolveChildRoute(parentRoute(), nil, agent.DelegateRequest{
		Provider: "cloud", Model: "gpt",
	}, nil, true); err == nil {
		t.Error("nil policy must reject a route change away from the parent route")
	}
	// The parent route itself is always permitted.
	got, err := ResolveChildRoute(parentRoute(), nil, agent.DelegateRequest{
		Provider: "local", Model: "lfm-8b",
	}, nil, true)
	if err != nil {
		t.Fatalf("ResolveChildRoute: %v", err)
	}
	if got != nil && (got.Provider != "local" || got.Model != "lfm-8b") {
		t.Errorf("route = %+v, want parent local:lfm-8b", got)
	}
}

func TestResolveChildRoute_ProviderModelTogether(t *testing.T) {
	policy := &ModelSelectionPolicy{Routes: []AllowedModelRoute{
		{Provider: "cloud", Model: "gpt"},
	}}
	if _, err := ResolveChildRoute(parentRoute(), nil,
		agent.DelegateRequest{Model: "gpt"}, policy, true); err == nil {
		t.Error("model without provider must error")
	}
	if _, err := ResolveChildRoute(parentRoute(), nil,
		agent.DelegateRequest{Provider: "cloud"}, policy, true); err == nil {
		t.Error("provider without model must error")
	}
	if _, err := ResolveChildRoute(parentRoute(), nil,
		agent.DelegateRequest{Provider: "", Model: ""}, policy, true); err == nil {
		t.Error("explicit empty provider+model must error, not fall through")
	}
}

func TestResolveChildRoute_RouteChangeClearsConfiguredEffort(t *testing.T) {
	configured := &agent.ChildOptions{
		Provider: "local", Model: "lfm-1.2b", ReasoningEffort: "high",
	}
	policy := &ModelSelectionPolicy{Routes: []AllowedModelRoute{
		{Provider: "cloud", Model: "gpt"},
	}}

	got, err := ResolveChildRoute(parentRoute(), configured, agent.DelegateRequest{
		Provider: "cloud", Model: "gpt",
	}, policy, true)
	if err != nil {
		t.Fatalf("ResolveChildRoute: %v", err)
	}
	if got != nil && got.ReasoningEffort != "" {
		t.Errorf("route change kept configured effort %q, want cleared", got.ReasoningEffort)
	}

	// An explicit request effort survives the change.
	got, err = ResolveChildRoute(parentRoute(), configured, agent.DelegateRequest{
		Provider: "cloud", Model: "gpt", ReasoningEffort: "low",
	}, policy, true)
	if err != nil {
		t.Fatalf("ResolveChildRoute: %v", err)
	}
	if got == nil || got.ReasoningEffort != "low" {
		t.Errorf("explicit request effort = %+v, want low", got)
	}
}

func TestResolveChildRoute_DisabledFallsThroughToConfigured(t *testing.T) {
	// enabled=false: request fields are ignored; configured (or nil) wins.
	configured := &agent.ChildOptions{Provider: "local", Model: "lfm-1.2b"}
	got, err := ResolveChildRoute(parentRoute(), configured, agent.DelegateRequest{
		Provider: "cloud", Model: "gpt",
	}, nil, false)
	if err != nil {
		t.Fatalf("ResolveChildRoute: %v", err)
	}
	if got == nil || got.Model != "lfm-1.2b" {
		t.Errorf("disabled selection -> %+v, want configured lfm-1.2b", got)
	}
}

func TestPreflightRoute_Unresolvable(t *testing.T) {
	// A nil/empty route must fail preflight BEFORE any spawn (visible at the
	// tool boundary). The resolver seam is exercised by the impl leaf; here we
	// pin only the empty-route rejection which needs no live resolver.
	if err := PreflightRoute(context.Background(), nil, agent.ChildOptions{}); err == nil {
		t.Error("empty route must fail preflight")
	}
}
