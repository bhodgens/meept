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

// TestBuildModelsInUse_BareAliasNameSlots reproduces the live config shape:
// classifier_model / summarizer_model hold bare alias names (no "/"), and
// the 8B endpoint's model appears ONLY as an alias fallback member. Before
// the fix, slash-less values were dropped before alias expansion, so bare
// alias names never resolved and fallback members never entered the set —
// leaving their endpoints dead until a manual `meept runtime start`.
func TestBuildModelsInUse_BareAliasNameSlots(t *testing.T) {
	agents := []llm.AgentModelRef{}
	slots := llm.ModelSlots{
		Model:           "agnes/agnes-2.5-flash",
		SmallModel:      "local-classifier/lfm-1.2b-q8",
		ClassifierModel: "classifier", // bare alias name
		SummarizerModel: "summarizer", // bare alias name
	}
	aliases := map[string]llm.ModelAliasEntry{
		"classifier": {Models: []string{
			"local-classifier/lfm-1.2b-q8",
			"local/lfm-8b-q4",
			"agnes/agnes-2.5-flash",
			"zai/glm-4.5-air",
			"ollama/llama3.2",
		}},
		"summarizer": {Models: []string{
			"local-classifier/lfm-1.2b-q8",
			"local/lfm-8b-q4",
			"agnes/agnes-2.5-flash",
			"zai/glm-4.5-air",
			"ollama/llama3.2",
		}},
		"coder": {Models: []string{"agnes/agnes-2.5-flash", "local/lfm-8b-q4", "ollama/llama3.2"}},
	}
	got := llm.BuildModelsInUse(agents, slots, aliases, nil)
	want := map[string]struct{}{
		"agnes/agnes-2.5-flash":        {},
		"local-classifier/lfm-1.2b-q8": {},
		"local/lfm-8b-q4":              {},
		"zai/glm-4.5-air":              {},
		"ollama/llama3.2":              {},
	}
	if !mapsEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestBuildModelsInUse_AliasAllMembersContribute verifies that an alias with
// three members contributes ALL three (head + fallbacks) to the in-use set —
// each member's endpoint is a configured failover target that should
// pre-warm at boot.
func TestBuildModelsInUse_AliasAllMembersContribute(t *testing.T) {
	agents := []llm.AgentModelRef{}
	slots := llm.ModelSlots{ClassifierModel: "coder"}
	aliases := map[string]llm.ModelAliasEntry{
		"coder": {Models: []string{"agnes/agnes-2.5-flash", "local/lfm-8b-q4", "ollama/llama3.2"}},
	}
	got := llm.BuildModelsInUse(agents, slots, aliases, nil)
	want := map[string]struct{}{
		"agnes/agnes-2.5-flash": {},
		"local/lfm-8b-q4":       {},
		"ollama/llama3.2":       {},
	}
	if !mapsEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestBuildModelsInUse_AliasesOnlyConfig: a config that defines aliases but
// no agents/slots still contributes alias members (aliased models are
// usable at request time regardless of slot wiring).
func TestBuildModelsInUse_AliasesOnlyConfig(t *testing.T) {
	aliases := map[string]llm.ModelAliasEntry{
		"planner": {Models: []string{"agnes/agnes-2.5-flash", "ollama/llama3.2"}},
	}
	got := llm.BuildModelsInUse(nil, llm.ModelSlots{}, aliases, nil)
	want := map[string]struct{}{
		"agnes/agnes-2.5-flash": {},
		"ollama/llama3.2":       {},
	}
	if !mapsEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestBuildModelsInUse_UnreferencedEndpointStillGated: a model in no agent,
// slot, or alias must NOT enter the set — the boot gate stays for genuinely
// unreferenced endpoints.
func TestBuildModelsInUse_UnreferencedEndpointStillGated(t *testing.T) {
	aliases := map[string]llm.ModelAliasEntry{
		"coder": {Models: []string{"agnes/agnes-2.5-flash", "local/lfm-8b-q4"}},
	}
	got := llm.BuildModelsInUse(nil, llm.ModelSlots{}, aliases, nil)
	if _, ok := got["local/unrelated-model"]; ok {
		t.Errorf("unreferenced model should be gated, got %v", got)
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
