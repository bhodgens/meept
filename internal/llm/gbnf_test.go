package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func toolDef(name string, params FunctionParameters) ToolDefinition {
	return NewToolDefinition(name, "test tool "+name, params)
}

// TestGrammarForTools_Table is the table-driven converter test covering the
// supported schema subset, exclusions, and root shape.
func TestGrammarForTools_Table(t *testing.T) {
	tests := []struct {
		name         string
		defs         []ToolDefinition
		wantComplete bool
		wantEmpty    bool // grammar must be ""
		check        func(t *testing.T, g string)
	}{
		{
			name:         "empty defs",
			defs:         nil,
			wantComplete: false,
			wantEmpty:    true,
		},
		{
			name: "primitives",
			defs: []ToolDefinition{toolDef("get_thing", FunctionParameters{
				Type: "object",
				Properties: map[string]ParameterProperty{
					"active":  {Type: "boolean"},
					"count":   {Type: "integer"},
					"name":    {Type: "string"},
					"ratio":   {Type: "number"},
					"nothing": {Type: "null"},
				},
				Required: []string{"name"},
			})},
			wantComplete: true,
			check: func(t *testing.T, g string) {
				for _, want := range []string{"integer ::= ", "number ::= ", "boolean ::= ", "\"name\""} {
					if !strings.Contains(g, want) {
						t.Errorf("grammar missing %q", want)
					}
				}
			},
		},
		{
			name: "enum strings become alternatives",
			defs: []ToolDefinition{toolDef("set_mode", FunctionParameters{
				Type: "object",
				Properties: map[string]ParameterProperty{
					"mode": {Type: "string", Enum: []string{"fast", "slow", "auto"}},
				},
				Required: []string{"mode"},
			})},
			wantComplete: true,
			check: func(t *testing.T, g string) {
				if !strings.Contains(g, `"fast" | "slow" | "auto"`) {
					t.Errorf("expected enum alternatives, got:\n%s", g)
				}
			},
		},
		{
			name: "required-first ordering",
			defs: []ToolDefinition{toolDef("ordered", FunctionParameters{
				Type: "object",
				Properties: map[string]ParameterProperty{
					"z_optional":  {Type: "string"},
					"a_required":  {Type: "string"},
					"m_optional2": {Type: "string"},
					"b_required2": {Type: "string"},
				},
				Required: []string{"a_required", "b_required2"},
			})},
			wantComplete: true,
			check: func(t *testing.T, g string) {
				iReq := strings.Index(g, `"a_required"`)
				iOpt := strings.Index(g, `"z_optional"`)
				if iReq < 0 || iOpt < 0 || iReq > iOpt {
					t.Errorf("required props must precede optional in rule:\n%s", g)
				}
				// Optional members wrapped with leading-comma group.
				if !strings.Contains(g, `("," ws "m_optional2" ws ":" ws`) {
					t.Errorf("optional member should be an optional comma group:\n%s", g)
				}
			},
		},
		{
			name: "depth-3 nesting ok",
			defs: []ToolDefinition{toolDef("nested3", FunctionParameters{
				Type: "object",
				Properties: map[string]ParameterProperty{
					"lvl1": {
						Type: "object",
						Properties: map[string]ParameterProperty{
							"lvl2": {
								Type: "object",
								Properties: map[string]ParameterProperty{
									"leaf": {Type: "string"},
								},
								Required: []string{"leaf"},
							},
						},
						Required: []string{"lvl2"},
					},
				},
				Required: []string{"lvl1"},
			})},
			wantComplete: true,
			check: func(t *testing.T, g string) {
				if !strings.Contains(g, "nested3-lvl1-lvl2 ::= ") {
					t.Errorf("missing nested object rule:\n%s", g)
				}
			},
		},
		{
			name: "depth-4 excluded",
			defs: []ToolDefinition{toolDef("too_deep", FunctionParameters{
				Type: "object",
				Properties: map[string]ParameterProperty{
					"a": {Type: "object", Properties: map[string]ParameterProperty{
						"b": {Type: "object", Properties: map[string]ParameterProperty{
							"c": {Type: "object", Properties: map[string]ParameterProperty{
								"d": {Type: "string"},
							}, Required: []string{"d"}},
						}, Required: []string{"c"}},
					}, Required: []string{"b"}},
				},
				Required: []string{"a"},
			})},
			wantComplete: false,
			wantEmpty:    true, // only tool excluded -> no objects at all
		},
		{
			name: "array of strings and arrays of objects",
			defs: []ToolDefinition{toolDef("listy", FunctionParameters{
				Type: "object",
				Properties: map[string]ParameterProperty{
					"tags": {Type: "array", Items: &ParameterProperty{Type: "string"}},
					"rows": {Type: "array", Items: &ParameterProperty{
						Type: "object",
						Properties: map[string]ParameterProperty{
							"id": {Type: "integer"},
						},
						Required: []string{"id"},
					}},
				},
				Required: []string{"tags"},
			})},
			wantComplete: true,
			check: func(t *testing.T, g string) {
				if !strings.Contains(g, "listy-tags ::= \"[\"") {
					t.Errorf("missing array rule:\n%s", g)
				}
			},
		},
		{
			name: "unsupported type excludes tool",
			defs: []ToolDefinition{
				toolDef("good", FunctionParameters{
					Type:       "object",
					Properties: map[string]ParameterProperty{"x": {Type: "string"}},
					Required:   []string{"x"},
				}),
				toolDef("bad", FunctionParameters{
					Type: "object",
					Properties: map[string]ParameterProperty{
						"weird": {Type: "oneOf"},
					},
					Required: []string{"weird"},
				}),
			},
			wantComplete: false,
			check: func(t *testing.T, g string) {
				if !strings.Contains(g, "good ::= ") {
					t.Error("good tool should be in grammar")
				}
				if strings.Contains(g, "bad ::= ") {
					t.Error("bad tool should be excluded from grammar")
				}
			},
		},
		{
			name:         "no properties excludes tool",
			defs:         []ToolDefinition{toolDef("noparams", FunctionParameters{Type: "object"})},
			wantComplete: false,
			wantEmpty:    true,
		},
		{
			name: "single tool root derives single-object OR array",
			defs: []ToolDefinition{toolDef("only", FunctionParameters{
				Type:       "object",
				Properties: map[string]ParameterProperty{"q": {Type: "string"}},
				Required:   []string{"q"},
			})},
			wantComplete: true,
			check: func(t *testing.T, g string) {
				if !strings.HasPrefix(g, "root ::= object | array\n") {
					t.Errorf("root must allow object or array:\n%s", g)
				}
				if !strings.Contains(g, "array ::= \"[\" ws (object (\",\" ws object)") {
					t.Errorf("array-of-objects root missing:\n%s", g)
				}
			},
		},
		{
			name: "multiple tools all present as alternatives",
			defs: []ToolDefinition{
				toolDef("alpha", FunctionParameters{
					Type:       "object",
					Properties: map[string]ParameterProperty{"x": {Type: "integer"}},
					Required:   []string{"x"},
				}),
				toolDef("beta!", FunctionParameters{
					Type:       "object",
					Properties: map[string]ParameterProperty{"y": {Type: "boolean"}},
					Required:   []string{"y"},
				}),
			},
			wantComplete: true,
			check: func(t *testing.T, g string) {
				if !strings.Contains(g, "alpha ::= ") || !strings.Contains(g, "beta_ ::= ") {
					t.Errorf("both tools (sanitized names) expected:\n%s", g)
				}
				if !strings.Contains(g, "object ::= alpha | beta_") {
					t.Errorf("root object rule should list alternatives:\n%s", g)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, complete := GrammarForTools(tt.defs)
			if complete != tt.wantComplete {
				t.Errorf("complete = %v, want %v", complete, tt.wantComplete)
			}
			if tt.wantEmpty && g != "" {
				t.Errorf("expected empty grammar, got:\n%s", g)
			}
			if !tt.wantEmpty && g == "" && tt.wantComplete {
				t.Error("unexpected empty grammar for complete=true case")
			}
			if tt.check != nil {
				tt.check(t, g)
			}
		})
	}
}

// TestGrammarEscapingGolden pins quote/backslash escaping behavior.
func TestGrammarEscapingGolden(t *testing.T) {
	def := toolDef("esc", FunctionParameters{
		Type: "object",
		Properties: map[string]ParameterProperty{
			"path": {Type: "string", Enum: []string{`C:\Users\test`, `say "hi"`}},
		},
		Required: []string{"path"},
	})
	g, complete := GrammarForTools([]ToolDefinition{def})
	if !complete {
		t.Fatal("expected complete grammar")
	}
	wantEnum := `"C:\\Users\\test" | "say \"hi\""`
	if !strings.Contains(g, wantEnum) {
		t.Errorf("escaping golden mismatch.\nwant substring: %s\ngot:\n%s", wantEnum, g)
	}
}

// TestGrammarFixtureGolden pins the full fixture grammar output.
func TestGrammarFixtureGolden(t *testing.T) {
	defs := []ToolDefinition{
		toolDef("read_file", FunctionParameters{
			Type: "object",
			Properties: map[string]ParameterProperty{
				"path":     {Type: "string", Description: "file path"},
				"line_num": {Type: "integer"},
			},
			Required: []string{"path"},
		}),
		toolDef("search", FunctionParameters{
			Type: "object",
			Properties: map[string]ParameterProperty{
				"query":  {Type: "string"},
				"engine": {Type: "string", Enum: []string{"local", "web"}},
			},
			Required: []string{"query"},
		}),
	}
	g, complete := GrammarForTools(defs)
	if !complete {
		t.Fatal("fixture grammar should be complete")
	}
	const golden = `root ::= object | array
ws ::= [ \t\n]*
string ::= "\"" char* "\""
char ::= [^"\\] | "\\\"" | "\\\\" | "\\n" | "\\t" | "\\r"
number ::= ["-"]? [0-9]+ ["." [0-9]+]? (["e" "E"] ["-" "+"]? [0-9]+)?
integer ::= ["-"]? [0-9]+
boolean ::= "true" | "false"
null ::= "null"
read_file-path-str ::= string
read_file-line_num ::= integer
read_file ::= "{" ws "path" ws ":" ws read_file-path-str ("," ws "line_num" ws ":" ws read_file-line_num)? ws "}"
search-query-str ::= string
search-engine ::= "local" | "web"
search ::= "{" ws "query" ws ":" ws search-query-str ("," ws "engine" ws ":" ws search-engine)? ws "}"
object ::= read_file | search
array ::= "[" ws (object ("," ws object)*)? ws "]"
`
	if g != golden {
		t.Errorf("golden grammar mismatch.\n--- want ---\n%s\n--- got ---\n%s", golden, g)
	}
}

// TestGrammarNeverPanics fuzzes arbitrary schemas through the converter.
func TestGrammarNeverPanics(t *testing.T) {
	nasty := []FunctionParameters{
		{Type: "", Properties: map[string]ParameterProperty{"": {}}},
		{Type: "object", Properties: map[string]ParameterProperty{
			"x": {Type: "array"}, // no items
		}},
		{Type: "object", Properties: map[string]ParameterProperty{
			"x": {Type: "ARRAY"},
		}},
		{Type: "object", Required: []string{"ghost"}, Properties: map[string]ParameterProperty{
			"a": {Type: "string"},
		}},
	}
	deep := ParameterProperty{Type: "object"}
	cur := &deep
	for range 50 { // pathological depth chain
		next := ParameterProperty{Type: "object"}
		next.Items = cur
		cur = &next
	}
	nasty = append(nasty, FunctionParameters{
		Type:       "object",
		Properties: map[string]ParameterProperty{"boom": *cur},
	})

	var defs []ToolDefinition
	for i, p := range nasty {
		defs = append(defs, toolDef(strings.Repeat("t", i+1), p))
	}
	_, _ = GrammarForTools(defs) // must not panic
}

// TestJSONSchemaForTools verifies valid JSON Schema roundtrip.
func TestJSONSchemaForTools(t *testing.T) {
	defs := []ToolDefinition{
		toolDef("a", FunctionParameters{
			Type:       "object",
			Properties: map[string]ParameterProperty{"x": {Type: "integer"}},
			Required:   []string{"x"},
		}),
	}
	s := JSONSchemaForTools(defs)
	if s == "" {
		t.Fatal("empty schema")
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("schema not valid JSON: %v\n%s", err, s)
	}
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		t.Fatalf("missing properties: %v", doc)
	}
	if _, ok := props["tool_calls"]; !ok {
		t.Errorf("missing tool_calls property")
	}
}

func TestAttachGrammar_Modes(t *testing.T) {
	g := "root ::= object"

	t.Run("llamacpp", func(t *testing.T) {
		p := map[string]any{}
		AttachGrammar(p, ToolConstraintLlamaCPP, g)
		if p["grammar"] != g {
			t.Errorf("payload[grammar] = %v", p["grammar"])
		}
	})
	t.Run("vllm", func(t *testing.T) {
		p := map[string]any{}
		AttachGrammar(p, ToolConstraintVLLM, g)
		if p["guided_grammar"] != g {
			t.Errorf("payload[guided_grammar] = %v", p["guided_grammar"])
		}
	})
	t.Run("json_schema", func(t *testing.T) {
		schema := JSONSchemaForTools(nil)
		p := map[string]any{}
		AttachGrammar(p, ToolConstraintJSONSchea, schema)
		rf, ok := p["response_format"].(map[string]any)
		if !ok {
			t.Fatalf("response_format missing: %v", p)
		}
		if rf["type"] != "json_schema" {
			t.Errorf("response_format.type = %v", rf["type"])
		}
		js, ok := rf["json_schema"].(map[string]any)
		if !ok || js["schema"] == nil {
			t.Fatalf("json_schema.schema missing: %v", rf)
		}
	})
	t.Run("unknown mode no-op", func(t *testing.T) {
		p := map[string]any{}
		AttachGrammar(p, "openai_strict", g)
		if len(p) != 0 {
			t.Errorf("unknown mode must not modify payload: %v", p)
		}
	})
	t.Run("empty grammar no-op", func(t *testing.T) {
		p := map[string]any{}
		AttachGrammar(p, ToolConstraintLlamaCPP, "")
		if len(p) != 0 {
			t.Errorf("empty grammar must not modify payload: %v", p)
		}
	})
}

// TestWithGrammarChatOption verifies chatOptions wiring.
func TestWithGrammarChatOption(t *testing.T) {
	opts := &chatOptions{}
	o := WithGrammar(ToolConstraintLlamaCPP)
	o(opts)
	if opts.grammarMode != ToolConstraintLlamaCPP {
		t.Errorf("grammarMode = %q", opts.grammarMode)
	}
}
