package llm

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// gbnfFixtureTool returns a fully supported tool definition.
func gbnfFixtureTool() ToolDefinition {
	return NewToolDefinition("ping", "ping a host", FunctionParameters{
		Type: "object",
		Properties: map[string]ParameterProperty{
			"host": {Type: "string"},
		},
		Required: []string{"host"},
	})
}

// newGbnfTestClient creates a client with a capturing logger.
func newGbnfTestClient(cfg *ModelConfig, buf *bytes.Buffer) *Client {
	logger := slog.New(slog.NewTextHandler(buf, nil))
	return NewClient(cfg, WithLogger(logger))
}

func TestAttachToolGrammar_Integration(t *testing.T) {
	restore := func(t *testing.T, on bool) {
		prev := GBNFConstrainedEnabled()
		SetGBNFConstrained(on)
		t.Cleanup(func() { SetGBNFConstrained(prev) })
	}

	t.Run("config off -> payload untouched", func(t *testing.T) {
		restore(t, false)
		buf := &bytes.Buffer{}
		c := newGbnfTestClient(&ModelConfig{ToolConstraint: ToolConstraintLlamaCPP}, buf)
		_, payload, _ := c.buildChatRequest(
			[]ChatMessage{{Role: RoleUser, Content: "hi"}},
			c.Config(),
			[]ChatOption{WithTools([]ToolDefinition{gbnfFixtureTool()}), WithGrammar(ToolConstraintLlamaCPP)},
			false,
		)
		if _, ok := payload["grammar"]; ok {
			t.Error("grammar must not be attached when global switch is off")
		}
	})

	t.Run("capability-empty provider untouched", func(t *testing.T) {
		restore(t, true)
		buf := &bytes.Buffer{}
		c := newGbnfTestClient(&ModelConfig{ToolConstraint: ""}, buf)
		_, payload, _ := c.buildChatRequest(
			[]ChatMessage{{Role: RoleUser, Content: "hi"}},
			c.Config(),
			[]ChatOption{WithTools([]ToolDefinition{gbnfFixtureTool()}), WithGrammar(ToolConstraintLlamaCPP)},
			false,
		)
		if _, ok := payload["grammar"]; ok {
			t.Error("provider without declared capability must not get a grammar")
		}
	})

	t.Run("mode mismatch untouched", func(t *testing.T) {
		restore(t, true)
		buf := &bytes.Buffer{}
		c := newGbnfTestClient(&ModelConfig{ToolConstraint: ToolConstraintVLLM}, buf)
		_, payload, _ := c.buildChatRequest(
			[]ChatMessage{{Role: RoleUser, Content: "hi"}},
			c.Config(),
			[]ChatOption{WithTools([]ToolDefinition{gbnfFixtureTool()}), WithGrammar(ToolConstraintLlamaCPP)},
			false,
		)
		if _, ok := payload["grammar"]; ok {
			t.Error("llamacpp grammar must not attach to vllm-constrained provider")
		}
	})

	t.Run("no tools untouched", func(t *testing.T) {
		restore(t, true)
		buf := &bytes.Buffer{}
		c := newGbnfTestClient(&ModelConfig{ToolConstraint: ToolConstraintLlamaCPP}, buf)
		_, payload, _ := c.buildChatRequest(
			[]ChatMessage{{Role: RoleUser, Content: "hi"}},
			c.Config(),
			[]ChatOption{WithGrammar(ToolConstraintLlamaCPP)},
			false,
		)
		if _, ok := payload["grammar"]; ok {
			t.Error("no grammar without tools")
		}
	})

	t.Run("config on + capability + mode match attaches llamacpp grammar", func(t *testing.T) {
		restore(t, true)
		buf := &bytes.Buffer{}
		c := newGbnfTestClient(&ModelConfig{ToolConstraint: ToolConstraintLlamaCPP}, buf)
		_, payload, _ := c.buildChatRequest(
			[]ChatMessage{{Role: RoleUser, Content: "hi"}},
			c.Config(),
			[]ChatOption{WithTools([]ToolDefinition{gbnfFixtureTool()}), WithGrammar(ToolConstraintLlamaCPP)},
			false,
		)
		g, ok := payload["grammar"].(string)
		if !ok || !strings.Contains(g, "root ::= object | array") {
			t.Errorf("expected llamacpp grammar attached, got %v", payload["grammar"])
		}
	})

	t.Run("json_schema mode attaches response_format", func(t *testing.T) {
		restore(t, true)
		buf := &bytes.Buffer{}
		c := newGbnfTestClient(&ModelConfig{ToolConstraint: ToolConstraintJSONSchea}, buf)
		_, payload, _ := c.buildChatRequest(
			[]ChatMessage{{Role: RoleUser, Content: "hi"}},
			c.Config(),
			[]ChatOption{WithTools([]ToolDefinition{gbnfFixtureTool()}), WithGrammar(ToolConstraintJSONSchea)},
			false,
		)
		rf, ok := payload["response_format"].(map[string]any)
		if !ok || rf["type"] != "json_schema" {
			t.Errorf("expected response_format json_schema, got %v", payload["response_format"])
		}
	})

	t.Run("incomplete grammar warns once only", func(t *testing.T) {
		restore(t, true)
		// Reset the dedupe table for determinism.
		gbnfWarnOnce = sync.Map{}

		bad := NewToolDefinition("broken_tool", "unsupported schema", FunctionParameters{
			Type: "object",
			Properties: map[string]ParameterProperty{
				"x": {Type: "polymorph"},
			},
			Required: []string{"x"},
		})
		buf := &bytes.Buffer{}
		c := newGbnfTestClient(&ModelConfig{ToolConstraint: ToolConstraintLlamaCPP}, buf)

		buildTwice := func() map[string]any {
			_, payload, _ := c.buildChatRequest(
				[]ChatMessage{{Role: RoleUser, Content: "hi"}},
				c.Config(),
				[]ChatOption{WithTools([]ToolDefinition{bad}), WithGrammar(ToolConstraintLlamaCPP)},
				false,
			)
			return payload
		}
		p1 := buildTwice()
		p2 := buildTwice()

		warnCount := strings.Count(buf.String(), "excluding affected tools")
		if warnCount != 1 {
			t.Errorf("expected exactly 1 warning, got %d:\n%s", warnCount, buf.String())
		}
		// Grammar for zero supported tools is empty -> nothing attached.
		if _, ok := p1["grammar"]; ok || len(p2) > 0 && func() bool {
			_, ok := p2["grammar"]
			return ok
		}() {
			t.Error("empty grammar must not be attached")
		}
	})
}

func TestToolConstraintForRuntime(t *testing.T) {
	if got := ToolConstraintForRuntime(RuntimeLlamaCpp); got != ToolConstraintLlamaCPP {
		t.Errorf("llama-cpp constraint = %q, want llamacpp", got)
	}
	if got := ToolConstraintForRuntime(RuntimeMLX); got != "" {
		t.Errorf("mlx constraint = %q, want empty", got)
	}
}

func TestModelConfigFrom_ToolConstraintPlumbing(t *testing.T) {
	cfg := &ProvidersConfig{
		Providers: map[string]ProviderConfig{
			"local": {
				API: "openai_chat",
				Options: ProviderOptionsConfig{
					BaseURL:        "http://127.0.0.1:8080",
					ToolConstraint: ToolConstraintVLLM,
				},
				Models: map[string]ModelDef{
					"inherit": {Name: "inherit-model"},
					"own":     {Name: "own-model", ToolConstraint: ToolConstraintJSONSchea},
					"junk":    {Name: "junk-model", ToolConstraint: "bogus_mode"},
				},
			},
		},
	}
	models := GetAllModels(cfg)
	byName := map[string]*ModelConfig{}
	for _, m := range models {
		byName[m.ProviderID+"/"+m.ModelID] = m
	}

	inh := byName["local/inherit-model"]
	if inh == nil || inh.ToolConstraint != ToolConstraintVLLM || !inh.HasCapability(CapToolConstraint) {
		t.Errorf("inherit model should inherit provider vllm constraint: %+v", inh)
	}
	own := byName["local/own-model"]
	if own == nil || own.ToolConstraint != ToolConstraintJSONSchea || !own.HasCapability(CapToolConstraint) {
		t.Errorf("per-model override failed: %+v", own)
	}
	junk := byName["local/junk-model"]
	if junk == nil || junk.ToolConstraint != "" || junk.HasCapability(CapToolConstraint) {
		t.Errorf("unknown mode must resolve to empty/no capability: %+v", junk)
	}
}
