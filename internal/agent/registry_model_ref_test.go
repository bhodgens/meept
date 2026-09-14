package agent

import (
	"io"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// testResolver builds a resolver whose aliases are named after agents, the way
// models.json5 does (a "coder" alias for the coder agent, and so on).
func testResolver(t *testing.T, aliasNames ...string) *llm.Resolver {
	t.Helper()
	aliases := make(map[string]llm.ModelAliasEntry, len(aliasNames))
	for _, name := range aliasNames {
		aliases[name] = llm.ModelAliasEntry{Models: []string{"local/lfm-8b-q4"}}
	}
	cfg := &llm.ProvidersConfig{
		Providers: map[string]llm.ProviderConfig{
			"local": {
				Options: llm.ProviderOptionsConfig{BaseURL: "http://127.0.0.1:8080/v1"},
				Models: map[string]llm.ModelDef{
					"lfm-8b-q4": {Name: "LFM2.5-8B-A1B-Q4_K_M.gguf"},
				},
			},
		},
		ModelAliases: aliases,
	}
	return llm.NewResolver(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// TestModelRefForAgent_UsesOwnAliasWhenSpecHasNoModel is the regression for the
// routing defect: the coder agent has no model: in its AGENT.md, so the loop
// resolved no alias at all and every turn ran on the config's default model
// (agnes) even with the local driver first in the coder alias. The local
// llama.cpp path was therefore never exercised through the daemon.
func TestModelRefForAgent_UsesOwnAliasWhenSpecHasNoModel(t *testing.T) {
	resolver := testResolver(t, "coder", "planner")

	if got := modelRefForAgent(&AgentSpec{ID: "coder"}, resolver); got != "coder" {
		t.Fatalf("modelRef = %q, want the agent's own alias %q", got, "coder")
	}
	if got := modelRefForAgent(&AgentSpec{ID: "planner"}, resolver); got != "planner" {
		t.Fatalf("modelRef = %q, want planner", got)
	}
}

// TestModelRefForAgent_ExplicitModelWins pins the precedence: a spec that names
// its model (alias or provider/model) is never overridden by the fallback.
func TestModelRefForAgent_ExplicitModelWins(t *testing.T) {
	resolver := testResolver(t, "coder")

	for _, explicit := range []string{"coder", "agnes/agnes-2.5-flash", "small"} {
		spec := &AgentSpec{ID: "coder", Model: explicit}
		if got := modelRefForAgent(spec, resolver); got != explicit {
			t.Fatalf("modelRef = %q, want the explicit %q", got, explicit)
		}
	}
}

// TestModelRefForAgent_NoAliasKeepsTheDefault covers the agents the operator
// has not given an alias: no alias is invented, so the loop keeps the default
// model client it was built with.
func TestModelRefForAgent_NoAliasKeepsTheDefault(t *testing.T) {
	resolver := testResolver(t, "coder")

	if got := modelRefForAgent(&AgentSpec{ID: "librarian"}, resolver); got != "" {
		t.Fatalf("modelRef = %q, want empty for an agent with no alias", got)
	}
	if got := modelRefForAgent(&AgentSpec{ID: "coder"}, nil); got != "" {
		t.Fatalf("modelRef = %q, want empty with no resolver", got)
	}
	if got := modelRefForAgent(nil, resolver); got != "" {
		t.Fatalf("modelRef = %q, want empty for a nil spec", got)
	}
}
