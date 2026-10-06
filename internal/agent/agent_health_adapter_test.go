package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// fakeHealthResolver is the minimal stand-in for the resolver slice the
// health adapter consumes — no real providers, no real Resolver state.
type fakeHealthResolver struct {
	endpointBlocked bool
	healthyAliases  map[string]bool
}

func (f *fakeHealthResolver) EndpointBlocked(*llm.ModelConfig) bool {
	return f.endpointBlocked
}

func (f *fakeHealthResolver) HasHealthyModels(aliasName string) bool {
	return f.healthyAliases[aliasName]
}

func testHealthLookup(bindings map[string]AgentModelBinding) AgentModelLookup {
	return func(agentID string) (AgentModelBinding, bool) {
		b, ok := bindings[agentID]
		return b, ok
	}
}

func TestAgentHealthAdapterUnknownAgent(t *testing.T) {
	adapter := NewAgentHealthAdapter(testHealthLookup(nil), &fakeHealthResolver{
		healthyAliases: map[string]bool{"coder": true},
	})
	if adapter.AgentParkedOrCooling("no-such-agent") {
		t.Fatal("unknown agent must report false (never parked)")
	}
}

func TestAgentHealthAdapterEndpointBlocked(t *testing.T) {
	bindings := map[string]AgentModelBinding{
		"coder": {Config: &llm.ModelConfig{ModelID: "m1"}, Alias: "coder"},
	}
	adapter := NewAgentHealthAdapter(testHealthLookup(bindings), &fakeHealthResolver{
		endpointBlocked: true,
		healthyAliases:  map[string]bool{"coder": true},
	})
	if !adapter.AgentParkedOrCooling("coder") {
		t.Fatal("resolver-reported endpoint block must report true")
	}
}

func TestAgentHealthAdapterAliasNoHealthyModels(t *testing.T) {
	bindings := map[string]AgentModelBinding{
		"coder": {Config: &llm.ModelConfig{ModelID: "m1"}, Alias: "coder"},
	}
	adapter := NewAgentHealthAdapter(testHealthLookup(bindings), &fakeHealthResolver{
		healthyAliases: map[string]bool{}, // "coder" absent -> no healthy models
	})
	if !adapter.AgentParkedOrCooling("coder") {
		t.Fatal("alias with no healthy models must report true")
	}
}

func TestAgentHealthAdapterHealthy(t *testing.T) {
	bindings := map[string]AgentModelBinding{
		"coder": {Config: &llm.ModelConfig{ModelID: "m1"}, Alias: "coder"},
	}
	adapter := NewAgentHealthAdapter(testHealthLookup(bindings), &fakeHealthResolver{
		healthyAliases: map[string]bool{"coder": true},
	})
	if adapter.AgentParkedOrCooling("coder") {
		t.Fatal("healthy endpoint + healthy alias must report false")
	}
}

func TestAgentHealthAdapterNilResolver(t *testing.T) {
	bindings := map[string]AgentModelBinding{
		"coder": {Config: &llm.ModelConfig{ModelID: "m1"}, Alias: "coder"},
	}
	var adapter = NewAgentHealthAdapter(testHealthLookup(bindings), nil)
	if adapter.AgentParkedOrCooling("coder") {
		t.Fatal("nil resolver must report false")
	}
	// Also nil-guard the adapter itself (typed-nil receiver safety).
	var nilAdapter *AgentHealthAdapter
	if nilAdapter.AgentParkedOrCooling("coder") {
		t.Fatal("nil adapter must report false")
	}
}

func TestAgentHealthAdapterNoAliasEndpointOnly(t *testing.T) {
	// An agent on the resolver default (no dedicated alias) is probed on
	// endpoint state only; an empty alias never counts as unhealthy.
	bindings := map[string]AgentModelBinding{
		"coder": {Config: &llm.ModelConfig{ModelID: "m1"}, Alias: ""},
	}
	adapter := NewAgentHealthAdapter(testHealthLookup(bindings), &fakeHealthResolver{
		healthyAliases: map[string]bool{},
	})
	if adapter.AgentParkedOrCooling("coder") {
		t.Fatal("no-alias agent with clear endpoint must report false")
	}
}
