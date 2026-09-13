package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

// tcToolDef builds a minimal tool definition with one declared property so the
// request carries a realistic tool list.
func tcToolDef(name string) ToolDefinition {
	return NewToolDefinition(name, "test tool", FunctionParameters{
		Type: "object",
		Properties: map[string]ParameterProperty{
			"path": {Type: "string", Description: "target path"},
		},
	})
}

// buildTCRequest builds a chat payload with the given config tool_choice and
// request options, returning the raw payload map.
func buildTCRequest(t *testing.T, cfgToolChoice string, opts ...ChatOption) map[string]any {
	t.Helper()
	c := NewClient(&ModelConfig{ModelID: "test-model", ToolChoice: cfgToolChoice})
	_, payload, err := c.buildChatRequest(
		[]ChatMessage{{Role: RoleUser, Content: "do the thing"}},
		c.config,
		opts,
		false,
	)
	if err != nil {
		t.Fatalf("buildChatRequest: %v", err)
	}
	return payload
}

// TestBuildChatRequest_ToolChoiceRequiredWithTools: tools present + caller
// marked the turn an action turn + the model opted in => the request carries
// "tool_choice":"required".
func TestBuildChatRequest_ToolChoiceRequiredWithTools(t *testing.T) {
	payload := buildTCRequest(t, ToolChoiceRequired,
		WithTools([]ToolDefinition{tcToolDef("shell")}),
		WithToolChoice(ToolChoiceRequired),
	)
	got, ok := payload["tool_choice"]
	if !ok {
		t.Fatalf("payload missing tool_choice; keys=%v", payloadKeys(payload))
	}
	if got != ToolChoiceRequired {
		t.Fatalf("tool_choice = %v, want %q", got, ToolChoiceRequired)
	}
	// The exact wire shape the provider sees.
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"tool_choice":"required"`) {
		t.Fatalf("wire payload lacks \"tool_choice\":\"required\": %s", raw)
	}
}

// TestBuildChatRequest_ToolChoiceWithoutToolsOmitted: no tools on the request
// (even if the caller and model ask) => the field must be absent.
func TestBuildChatRequest_ToolChoiceWithoutToolsOmitted(t *testing.T) {
	payload := buildTCRequest(t, ToolChoiceRequired, WithToolChoice(ToolChoiceRequired))
	if _, ok := payload["tool_choice"]; ok {
		t.Fatalf("tool_choice present without tools: %v", payload["tool_choice"])
	}
}

// TestBuildChatRequest_ToolChoiceDefaultConfigOmitted: the DEFAULT model
// config (tool_choice unset in models.json5) never sends tool_choice, even
// when the caller marks an action turn. This is the no-regression guarantee.
func TestBuildChatRequest_ToolChoiceDefaultConfigOmitted(t *testing.T) {
	payload := buildTCRequest(t, "",
		WithTools([]ToolDefinition{tcToolDef("shell")}),
		WithToolChoice(ToolChoiceRequired),
	)
	if _, ok := payload["tool_choice"]; ok {
		t.Fatalf("default config sent tool_choice: %v", payload["tool_choice"])
	}
	if _, ok := payload["tools"]; !ok {
		t.Fatalf("tools must still be present; keys=%v", payloadKeys(payload))
	}
}

// TestBuildChatRequest_ToolChoiceProseTurnOmitted: the model opted in and
// tools are present, but the caller did NOT mark the turn an action turn
// (prose/analysis) => the field must be absent. This is the measured fix for
// the 5/5 spurious-call failure.
func TestBuildChatRequest_ToolChoiceProseTurnOmitted(t *testing.T) {
	payload := buildTCRequest(t, ToolChoiceRequired,
		WithTools([]ToolDefinition{tcToolDef("shell")}),
	)
	if _, ok := payload["tool_choice"]; ok {
		t.Fatalf("prose turn sent tool_choice: %v", payload["tool_choice"])
	}
}

// TestBuildChatRequest_ToolChoiceOnlyRequiredHonored: a per-turn value that is
// not "required" is inert today.
func TestBuildChatRequest_ToolChoiceOnlyRequiredHonored(t *testing.T) {
	for _, v := range []string{"auto", "none", "bogus"} {
		payload := buildTCRequest(t, ToolChoiceRequired,
			WithTools([]ToolDefinition{tcToolDef("shell")}),
			WithToolChoice(v),
		)
		if _, ok := payload["tool_choice"]; ok {
			t.Fatalf("WithToolChoice(%q) sent tool_choice=%v, want omitted", v, payload["tool_choice"])
		}
	}
}

// TestToolChoiceOf: the inspection seam reports the requested per-turn value.
func TestToolChoiceOf(t *testing.T) {
	if got := ToolChoiceOf(nil); got != "" {
		t.Fatalf("ToolChoiceOf(nil) = %q, want empty", got)
	}
	if got := ToolChoiceOf([]ChatOption{WithToolChoice(ToolChoiceRequired)}); got != ToolChoiceRequired {
		t.Fatalf("ToolChoiceOf = %q, want %q", got, ToolChoiceRequired)
	}
}

// TestResolveToolChoiceMatrix pins the three-gate rule directly.
func TestResolveToolChoiceMatrix(t *testing.T) {
	tools := []ToolDefinition{tcToolDef("shell")}
	cases := []struct {
		name     string
		cfg      *ModelConfig
		opts     []ChatOption
		expected string
	}{
		{"model opt-in + action turn + tools", &ModelConfig{ToolChoice: ToolChoiceRequired}, []ChatOption{WithTools(tools), WithToolChoice(ToolChoiceRequired)}, ToolChoiceRequired},
		{"no model opt-in", &ModelConfig{}, []ChatOption{WithTools(tools), WithToolChoice(ToolChoiceRequired)}, ""},
		{"no action turn", &ModelConfig{ToolChoice: ToolChoiceRequired}, []ChatOption{WithTools(tools)}, ""},
		{"no tools", &ModelConfig{ToolChoice: ToolChoiceRequired}, []ChatOption{WithToolChoice(ToolChoiceRequired)}, ""},
		{"auto model value is not forcing", &ModelConfig{ToolChoice: "auto"}, []ChatOption{WithTools(tools), WithToolChoice(ToolChoiceRequired)}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var o chatOptions
			for _, opt := range tc.opts {
				opt(&o)
			}
			if got := resolveToolChoice(tc.cfg, &o); got != tc.expected {
				t.Fatalf("resolveToolChoice = %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestProviderConfigToolChoiceResolution: models.json5 tool_choice resolves
// per-model over provider, defaults to empty, and unknown values are cleared.
func TestProviderConfigToolChoiceResolution(t *testing.T) {
	cfg := &ProvidersConfig{
		Providers: map[string]ProviderConfig{
			"prov": {
				Options: ProviderOptionsConfig{ToolChoice: ToolChoiceRequired},
				Models: map[string]ModelDef{
					"inherits": {Name: "inherits"},
					"override": {Name: "override", ToolChoice: "auto"},
					"bogus":    {Name: "bogus", ToolChoice: "definitely-not-a-value"},
					"plain":    {Name: "plain"},
				},
			},
			"plain-prov": {
				Options: ProviderOptionsConfig{},
				Models:  map[string]ModelDef{"m": {Name: "m"}},
			},
		},
	}
	// override: per-model "auto" wins over provider "required".
	if got := ResolveModelRef("prov/override", cfg); got == nil || got.ToolChoice != "auto" {
		t.Fatalf("prov/override ToolChoice = %v, want \"auto\"", got)
	}
	// bogus: unknown value cleared to "" (warned, not fatal).
	if got := ResolveModelRef("prov/bogus", cfg); got == nil || got.ToolChoice != "" {
		t.Fatalf("prov/bogus ToolChoice = %v, want \"\"", got)
	}
	// no provider-level value: default empty (nothing regresses).
	if got := ResolveModelRef("plain-prov/m", cfg); got == nil || got.ToolChoice != "" {
		t.Fatalf("plain-prov/m ToolChoice = %v, want \"\"", got)
	}
}

// TestProviderConfigToolChoiceProviderDefaultInherited: a model with no
// per-model value inherits the provider-level tool_choice.
func TestProviderConfigToolChoiceProviderDefaultInherited(t *testing.T) {
	cfg := &ProvidersConfig{
		Providers: map[string]ProviderConfig{
			"prov": {
				Options: ProviderOptionsConfig{ToolChoice: ToolChoiceRequired},
				Models:  map[string]ModelDef{"inherits": {Name: "inherits"}},
			},
		},
	}
	got := ResolveModelRef("prov/inherits", cfg)
	if got == nil {
		t.Fatal("ResolveModelRef returned nil")
	}
	if got.ToolChoice != ToolChoiceRequired {
		t.Fatalf("inherited ToolChoice = %q, want %q", got.ToolChoice, ToolChoiceRequired)
	}
}

// TestToolChoiceValid pins the accepted vocabulary.
func TestToolChoiceValid(t *testing.T) {
	for _, v := range []string{"", "auto", "none", "required"} {
		if !ToolChoiceValid(v) {
			t.Errorf("ToolChoiceValid(%q) = false, want true", v)
		}
	}
	if ToolChoiceValid("nonsense") {
		t.Error("ToolChoiceValid(\"nonsense\") = true, want false")
	}
}

func payloadKeys(payload map[string]any) []string {
	keys := make([]string, 0, len(payload))
	for k := range payload {
		keys = append(keys, k)
	}
	return keys
}
