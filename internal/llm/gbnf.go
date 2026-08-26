package llm

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// GBNF constraint modes. A provider endpoint declares which wire key it
// honors via its tool_constraint capability:
//
//	"llamacpp"    -> payload["grammar"]          (llama.cpp server)
//	"vllm"        -> payload["guided_grammar"]   (vLLM)
//	"json_schema" -> payload["response_format"]  (OpenAI-compat structured outputs)
//
// Empty means the endpoint accepts no grammar constraint; nothing is attached.
const (
	ToolConstraintLlamaCPP  = "llamacpp"
	ToolConstraintVLLM      = "vllm"
	ToolConstraintJSONSchea = "json_schema"
)

// maxGBNFDepth is the maximum nesting depth (of objects/arrays) the converter
// supports. Top-level tool object counts as depth 1.
const maxGBNFDepth = 3

// GrammarForTools converts tool definitions into a GBNF grammar constraining
// tool-call output to valid JSON for the supported schema subset.
//
// Root allows a single object OR an array of objects. Supported constructs:
// string (with optional enum), number, integer, boolean,
// array<supported>, object{properties,required} — nested to depth 3.
// Required properties are emitted before optional ones (GBNF ordering).
//
// Any tool containing an unsupported construct is excluded from the grammar.
// complete is false when any tool was excluded OR when defs is empty.
// This function never panics on arbitrary input.
func GrammarForTools(defs []ToolDefinition) (string, bool) {
	if len(defs) == 0 {
		return "", false
	}

	var b strings.Builder
	b.WriteString("root ::= object | array\n")
	b.WriteString("ws ::= [ \\t\\n]*\n")
	b.WriteString("string ::= \"\\\"\" char* \"\\\"\"\n")
	b.WriteString("char ::= [^\"\\\\] | \"\\\\\\\"\" | \"\\\\\\\\\" | \"\\\\n\" | \"\\\\t\" | \"\\\\r\"\n")
	b.WriteString("number ::= [\"-\"]? [0-9]+ [\".\" [0-9]+]? ([\"e\" \"E\"] [\"-\" \"+\"]? [0-9]+)?\n")
	b.WriteString("integer ::= [\"-\"]? [0-9]+\n")
	b.WriteString("boolean ::= \"true\" | \"false\"\n")
	b.WriteString("null ::= \"null\"\n")

	used := make(map[string]bool)
	objectAlts := make([]string, 0, len(defs))
	excludedAny := false

	for _, def := range defs {
		name := def.Function.Name
		rule := gbnfRuleName(name, used)
		objRule, ok := gbnfObjectRule(&b, rule, def.Function.Parameters, 1)
		if !ok {
			excludedAny = true
			continue
		}
		objectAlts = append(objectAlts, objRule)
	}

	if len(objectAlts) == 0 {
		return "", false
	}

	b.WriteString("object ::= ")
	if len(objectAlts) == 1 {
		b.WriteString(objectAlts[0])
	} else {
		b.WriteString(strings.Join(objectAlts, " | "))
	}
	b.WriteString("\n")
	b.WriteString("array ::= \"[\" ws (object (\",\" ws object)*)? ws \"]\"\n")

	if excludedAny {
		return b.String(), false
	}
	return b.String(), true
}

// gbnfRuleName derives a unique, GBNF-safe rule name from a tool name.
func gbnfRuleName(toolName string, used map[string]bool) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(toolName) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			sb.WriteRune(r)
		} else {
			sb.WriteByte('_')
		}
	}
	base := sb.String()
	if base == "" {
		base = "tool"
	}
	name := base
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s_%d", base, i)
	}
	used[name] = true
	return name
}

// gbnfObjectRule emits a rule for an object schema node and returns the rule
// name. depth is the nesting level of this object (top-level tool object = 1).
// Returns ok=false for unsupported constructs.
func gbnfObjectRule(b *strings.Builder, rule string, params FunctionParameters, depth int) (string, bool) {
	if depth > maxGBNFDepth {
		return "", false
	}
	props := params.Properties
	if len(props) == 0 {
		// Object with no declared properties cannot be constrained safely;
		// exclude the tool rather than brick generation.
		return "", false
	}

	requiredSet := make(map[string]bool, len(params.Required))
	for _, r := range params.Required {
		requiredSet[r] = true
	}

	names := make([]string, 0, len(props))
	for n := range props {
		names = append(names, n)
	}
	sort.Strings(names)

	// Required-first reordering: GNF alternatives need required keys before
	// optional ones so every generated object satisfies the required set.
	reqNames := make([]string, 0, len(names))
	optNames := make([]string, 0, len(names))
	for _, n := range names {
		if requiredSet[n] {
			reqNames = append(reqNames, n)
		} else {
			optNames = append(optNames, n)
		}
	}
	ordered := append(reqNames, optNames...)

	// Emit member rules first so forward references resolve naturally.
	members := make([]string, 0, len(ordered))
	for _, n := range ordered {
		prop := props[n]
		memRule := fmt.Sprintf("%s-%s", rule, gbnfKeySafe(n))
		valRule, ok := gbnfValueRule(b, memRule, prop, depth)
		if !ok {
			return "", false
		}
		members = append(members, fmt.Sprintf("%s ws \":\" ws %s", gbnfQuotedKey(n), valRule))
	}

	// Build comma-separated member sequence with required-first ordering.
	var seq strings.Builder
	for i, m := range members {
		if i > 0 {
			seq.WriteString(" \",\" ws ")
		}
		seq.WriteString(m)
	}

	// Optional tail: optional members (which follow all required members)
	// are grouped together with their leading separator so they may be
	// omitted entirely without leaving dangling commas.
	if len(optNames) > 0 {
		seq.Reset()
		for i, n := range ordered {
			m := members[i]
			if i == 0 {
				fmt.Fprintf(&seq, "%s ", m)
				continue
			}
			if requiredSet[n] {
				fmt.Fprintf(&seq, "\",\" ws %s ", m)
			} else {
				fmt.Fprintf(&seq, "(\",\" ws %s)? ", m)
			}
		}
	}

	fmt.Fprintf(b, "%s ::= \"{\" ws %sws \"}\"\n", rule, seq.String())
	return rule, true
}

// gbnfValueRule emits a rule for a single value node and returns the rule name.
func gbnfValueRule(b *strings.Builder, rule string, prop ParameterProperty, depth int) (string, bool) {
	switch prop.Type {
	case "string":
		if len(prop.Enum) > 0 {
			alts := make([]string, 0, len(prop.Enum))
			for _, e := range prop.Enum {
				alts = append(alts, gbnfQuotedString(e))
			}
			fmt.Fprintf(b, "%s ::= %s\n", rule, strings.Join(alts, " | "))
			return rule, true
		}
		fmt.Fprintf(b, "%s ::= string\n", rule+"-str")
		return rule + "-str", true
	case "number":
		fmt.Fprintf(b, "%s ::= number\n", rule)
		return rule, true
	case "integer":
		fmt.Fprintf(b, "%s ::= integer\n", rule)
		return rule, true
	case "boolean":
		fmt.Fprintf(b, "%s ::= boolean\n", rule)
		return rule, true
	case "null":
		fmt.Fprintf(b, "%s ::= null\n", rule)
		return rule, true
	case "array":
		if depth+1 > maxGBNFDepth {
			return "", false
		}
		if prop.Items == nil {
			return "", false
		}
		itemRule := rule + "-item"
		elem, ok := gbnfValueRule(b, itemRule, *prop.Items, depth+1)
		if !ok {
			return "", false
		}
		fmt.Fprintf(b, "%s ::= \"[\" ws (%s (\",\" ws %s)*)? ws \"]\"\n", rule, elem, elem)
		return rule, true
	case "object":
		if depth+1 > maxGBNFDepth {
			return "", false
		}
		return gbnfObjectRule(b, rule, FunctionParameters{
			Type:       "object",
			Properties: prop.Properties,
			Required:   prop.Required,
		}, depth+1)
	default:
		// Unsupported construct (e.g. oneOf/anyOf/allOf at this node).
		return "", false
	}
}

// gbnfKeySafe makes a key safe for embedding in a rule name.
func gbnfKeySafe(k string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(k) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			sb.WriteRune(r)
		} else {
			sb.WriteByte('_')
		}
	}
	if sb.Len() == 0 {
		return "k"
	}
	return sb.String()
}

// gbnfQuotedString renders s as a GBNF literal string, escaping backslash
// and double-quote.
func gbnfQuotedString(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString("\\\"")
		case '\\':
			sb.WriteString("\\\\")
		default:
			sb.WriteRune(r)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// gbnfQuotedKey renders a JSON object key as a GBNF literal.
func gbnfQuotedKey(k string) string {
	return gbnfQuotedString(k)
}

// JSONSchemaForTools converts tool definitions into a JSON Schema document
// (as a JSON-encoded string) suitable for response_format structured-output
// endpoints. Unlike the GBNF converter this path tolerates the full schema
// surface (oneOf etc.), so ALL tools are included; enum tightness may be
// lower than the GBNF path depending on server support.
// Never panics on arbitrary input.
func JSONSchemaForTools(defs []ToolDefinition) string {
	schema := jsonSchemaForToolsMap(defs)
	data, err := json.Marshal(schema)
	if err != nil {
		return ""
	}
	return string(data)
}

func jsonSchemaForToolsMap(defs []ToolDefinition) map[string]any {
	perTool := make([]map[string]any, 0, len(defs))
	for _, d := range defs {
		params := d.Function.Parameters
		if params.Type == "" {
			params.Type = "object"
		}
		paramBytes, err := json.Marshal(params)
		if err != nil {
			continue
		}
		var paramSchema map[string]any
		if err := json.Unmarshal(paramBytes, &paramSchema); err != nil {
			continue
		}
		perTool = append(perTool, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":      map[string]any{"const": d.Function.Name},
				"arguments": paramSchema,
			},
			"required": []string{"name", "arguments"},
		})
	}

	items := map[string]any{"oneOf": perTool}
	if len(perTool) == 1 {
		items = perTool[0]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tool_calls": map[string]any{
				"type":  "array",
				"items": items,
			},
		},
		"required": []string{"tool_calls"},
	}
}

// AttachGrammar attaches a grammar constraint to a request payload using the
// wire format appropriate for the given mode. Unknown modes are a no-op.
//
//	mode "llamacpp":    payload["grammar"] = g
//	mode "vllm":        payload["guided_grammar"] = g
//	mode "json_schema": payload["response_format"] =
//	                    {"type":"json_schema","json_schema":<g parsed as JSON schema>}
//
// For json_schema mode g should be the JSON-encoded schema produced by
// JSONSchemaForTools.
func AttachGrammar(reqPayload map[string]any, mode string, g string) {
	if reqPayload == nil || g == "" {
		return
	}
	switch mode {
	case ToolConstraintLlamaCPP:
		reqPayload["grammar"] = g
	case ToolConstraintVLLM:
		reqPayload["guided_grammar"] = g
	case ToolConstraintJSONSchea:
		reqPayload["response_format"] = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "meept_tool_calls",
				"schema": json.RawMessage(g),
			},
		}
	default:
		// Unknown mode: no-op.
	}
}

// ToolConstraintSupported reports whether a mode string is a recognized
// constraint mode.
func ToolConstraintSupported(mode string) bool {
	switch mode {
	case ToolConstraintLlamaCPP, ToolConstraintVLLM, ToolConstraintJSONSchea:
		return true
	}
	return false
}
