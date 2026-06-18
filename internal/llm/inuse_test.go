package llm_test

import (
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

func TestBuildModelsInUse_Agents(t *testing.T) {
	agents := []llm.AgentModelRef{
		{Model: "local/alpha", Enabled: true},
		{Model: "local/beta", Enabled: false}, // disabled: skip
		{Model: "remote/gamma", Enabled: true},
	}
	slots := llm.ModelSlots{}
	got := llm.BuildModelsInUse(agents, slots, nil, nil)
	want := map[string]struct{}{
		"local/alpha":  {},
		"remote/gamma": {},
	}
	if !mapsEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildModelsInUse_Slots(t *testing.T) {
	agents := []llm.AgentModelRef{}
	slots := llm.ModelSlots{
		Model:      "local/main",
		SmallModel: "local/small",
	}
	got := llm.BuildModelsInUse(agents, slots, nil, nil)
	want := map[string]struct{}{
		"local/main":  {},
		"local/small": {},
	}
	if !mapsEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildModelsInUse_AliasExpansion(t *testing.T) {
	agents := []llm.AgentModelRef{
		{Model: "local/primary", Enabled: true},
	}
	slots := llm.ModelSlots{}
	aliases := map[string]llm.ModelAliasEntry{
		"local/primary": {Models: []string{"local/secondary", "local/tertiary"}},
	}
	got := llm.BuildModelsInUse(agents, slots, aliases, nil)
	want := map[string]struct{}{
		"local/primary":   {},
		"local/secondary": {},
		"local/tertiary":  {},
	}
	if !mapsEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildModelsInUse_DisabledProviders(t *testing.T) {
	agents := []llm.AgentModelRef{
		{Model: "local/alpha", Enabled: true},
		{Model: "remote/beta", Enabled: true},
	}
	slots := llm.ModelSlots{}
	disabled := []string{"remote"}
	got := llm.BuildModelsInUse(agents, slots, nil, disabled)
	want := map[string]struct{}{
		"local/alpha": {},
	}
	if !mapsEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildModelsInUse_SkipsValuesWithoutSlash(t *testing.T) {
	agents := []llm.AgentModelRef{
		{Model: "no-slash-here", Enabled: true},
		{Model: "local/valid", Enabled: true},
	}
	got := llm.BuildModelsInUse(agents, llm.ModelSlots{}, nil, nil)
	if _, ok := got["local/valid"]; !ok {
		t.Errorf("expected local/valid in set, got %v", got)
	}
	if _, ok := got["no-slash-here"]; ok {
		t.Errorf("no-slash value should be skipped, got %v", got)
	}
}

func TestBuildModelsInUse_Empty(t *testing.T) {
	got := llm.BuildModelsInUse(nil, llm.ModelSlots{}, nil, nil)
	if got != nil {
		t.Errorf("expected nil for empty inputs, got %v", got)
	}
}

func mapsEqual(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}
