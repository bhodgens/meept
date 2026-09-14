package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/security/taint"
	"github.com/caimlas/meept/internal/tools"
)

const toolJSONExtract = "json_extract"

const jsonExtractSystemPrompt = `You are a strict JSON extraction engine. ` +
	`Extract data from the input text according to the schema given by the user. ` +
	`Output ONLY one JSON object conforming to the schema. ` +
	`Output only the properties the schema declares — never echo schema keywords such as required, type, or items. ` +
	`No prose, no markdown fences, no explanation. ` +
	`Use null for fields not present in the text. Never invent values.`

// maxExtractInputBytes bounds the text handed to the extraction model
// (context_limit of the default extraction model is ~16k tokens; the text
// plus schema and prompt must fit).
const maxExtractInputBytes = 24 * 1024

// JSONExtractTool extracts schema-shaped JSON from text using the
// models.json5 extraction model (extract_model slot; typically a local
// small LLM such as LFM2-Extract served by llama.cpp). The extraction
// turn is grammar-constrained (GBNF) when the endpoint declares the
// llamacpp tool_constraint capability and [agent.tools].gbnf_constrained
// is on, so the output is ALWAYS parseable JSON when constrained.
//
// The tool never runs the agent's own loop model: the dedicated client
// (built at daemon wiring time from the extract_model ref) is reused so
// callers pay local-token prices, not chat-model prices.
type JSONExtractTool struct {
	tools.ToolDefaults
	chatter  llm.Chatter // nil = not configured; Execute returns an actionable error
	timeout  time.Duration
	validate bool // reject output that fails strict schema shape check (default on)
}

// NewJSONExtractTool creates a json_extract tool. chatter is the dedicated
// extraction-model client (llm.Chatter so tests can inject fakes); nil is
// legal and makes Execute report the config error.
func NewJSONExtractTool(chatter llm.Chatter, timeout time.Duration) *JSONExtractTool {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &JSONExtractTool{chatter: chatter, timeout: timeout, validate: true}
}

// SetChatter wires or replaces the extraction client (nil-safe per repo rule).
func (t *JSONExtractTool) SetChatter(c llm.Chatter) {
	if t != nil && c != nil {
		t.chatter = c
	}
}

// SetValidate toggles the strict post-parse schema check.
func (t *JSONExtractTool) SetValidate(on bool) {
	if t != nil {
		t.validate = on
	}
}

func (t *JSONExtractTool) Name() string { return toolJSONExtract }

func (t *JSONExtractTool) Category() string { return "data" }

func (t *JSONExtractTool) Description() string {
	return "Extract structured JSON from text using the local extraction model. " +
		"Pass the source text (or a file path) plus the JSON schema of the record to extract. " +
		"Returns the parsed JSON object. Use for research notes, transcripts, and scraped pages that need consistent machine-readable records. " +
		"Paths: a relative file_path/output_path resolves against the session working dir and must stay inside it (so \"../..\" is refused); " +
		"an absolute path is an explicit target and is honored anywhere, so pass /tmp/e2e/paper.json (not ~/paper.json) when the file must land outside the working dir."
}

func (t *JSONExtractTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropText: {
				Type:        schemaTypeString,
				Description: "Raw text to extract from. Required unless file_path is given.",
			},
			schemaPropFilePath: {
				Type:        schemaTypeString,
				Description: "Read text from this file instead of the text parameter. Relative paths resolve against the session working dir and may not escape it (\"../..\" is refused). An absolute path is an explicit target and is read as given, inside or outside the working dir; \"~\" is refused - pass the absolute path.",
			},
			"schema": {
				Type:        schemaTypeObject,
				Description: "JSON schema (type/properties/required) describing the record to extract. Example: {\"type\":\"object\",\"properties\":{\"title\":{\"type\":\"string\"},\"year\":{\"type\":\"integer\"}},\"required\":[\"title\"]}",
			},
			"instructions": {
				Type:        schemaTypeString,
				Description: "Optional extra guidance for the extractor (field semantics, normalization rules).",
			},
			schemaPropOutputPath: {
				Type:        schemaTypeString,
				Description: "Optional file to write the extracted JSON to. Relative paths resolve against the session working dir and may not escape it (\"../..\" is refused). An absolute path is an explicit target and is written as given, inside or outside the working dir (e.g. /tmp/out/paper.json); \"~\" is refused - pass the absolute path.",
			},
		},
		Required: []string{"schema"},
	}
}

func (t *JSONExtractTool) IsReadOnly(in map[string]any) bool {
	// Writing output_path mutates; text-only extraction is read-only.
	if _, ok := in[schemaPropOutputPath]; ok {
		return false
	}
	return true
}

func (t *JSONExtractTool) IsConcurrencySafe(in map[string]any) bool {
	return t.IsReadOnly(in)
}

// Execute implements tools.Tool.
func (t *JSONExtractTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t == nil || t.chatter == nil {
		return nil, fmt.Errorf("json_extract: extraction model not configured (set extract_model in models.json5 to a provider/model ref)")
	}
	// Typed-nil guard (2026-09-10 panic, outcome-loop session): the daemon
	// passes (*llm.Client)(nil) when extract_model resolves to nothing, and
	// the interface holds the typed nil — the t.chatter == nil check above
	// does NOT catch it, and Chat panicked dereferencing the nil receiver's
	// mutex. reflect-based check catches interface-wrapped nil pointers.
	if reflect.ValueOf(t.chatter).Kind() == reflect.Ptr &&
		reflect.ValueOf(t.chatter).IsNil() {
		return nil, fmt.Errorf("json_extract: extraction model not configured (set extract_model in models.json5 to a provider/model ref)")
	}
	schema, err := extractSchemaArg(args)
	if err != nil {
		return nil, err
	}
	text, err := t.resolveText(ctx, args)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("json_extract: no input text (pass text or file_path)")
	}
	if len(text) > maxExtractInputBytes {
		text = truncateUTF8(text, maxExtractInputBytes)
	}

	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: jsonExtractSystemPrompt},
		{Role: llm.RoleUser, Content: t.buildUserTurn(schema, args, text)},
	}

	extractCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	resp, err := t.chatter.Chat(extractCtx, messages,
		llm.WithTemperature(0.0),
		llm.WithRawGrammar(gbnfJSONGrammar),
	)
	if err != nil {
		return nil, fmt.Errorf("json_extract: extraction model call failed: %w", err)
	}
	raw := strings.TrimSpace(resp.Content)
	raw = stripJSONFences(raw)

	var record map[string]any
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return nil, fmt.Errorf("json_extract: model output is not valid JSON: %w (output: %.200s)", err, raw)
	}
	// Conform the record to the schema BEFORE validation: small extractors
	// sometimes echo schema keywords (required/type/items) as fields, and
	// null-valued required fields mean "not found", not "violation".
	record = conformToSchema(record, schema)
	if t.validate {
		if missing := checkRequired(record, schema); len(missing) > 0 {
			return nil, fmt.Errorf("json_extract: extracted record missing required fields %v", missing)
		}
	}

	out, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("json_extract: encode result: %w", err)
	}

	result := map[string]any{
		"success": true,
		"record":  record,
		"json":    string(out),
		"model":   extractionModelRef(t.chatter),
	}
	if path := stringArg(args, schemaPropOutputPath); path != "" {
		written, werr := t.writeOutput(ctx, path, string(out))
		if werr != nil {
			return nil, werr
		}
		result["path"] = written
	}
	tr := tools.NewSuccessResult(result)
	// The extracted payload derives from external text; label it so
	// downstream policy can treat it as untrusted-derived data.
	tr.TaintLabel = taint.TaintExternal
	return tr, nil
}

// buildUserTurn assembles the extraction prompt: schema first, optional
// instructions, then the text.
func (t *JSONExtractTool) buildUserTurn(schema map[string]any, args map[string]any, text string) string {
	var b strings.Builder
	schemaJSON, err := json.Marshal(schema)
	if err == nil {
		b.WriteString("Schema:\n")
		b.Write(schemaJSON)
		b.WriteString("\n\n")
	}
	if instr := stringArg(args, "instructions"); instr != "" {
		b.WriteString("Instructions: ")
		b.WriteString(instr)
		b.WriteString("\n\n")
	}
	b.WriteString("Text:\n")
	b.WriteString(text)
	return b.String()
}

// resolveExtractPath is THE path policy for json_extract's file_path (read)
// and output_path (write) arguments. ArgName is the argument being resolved
// (schemaPropFilePath / schemaPropOutputPath) and is named in every refusal
// so the model knows which argument it must change.
//
// POLICY (2026-09-13) - keyed on EXPLICITNESS, not on location:
//
//  1. ABSOLUTE path: honored, inside or outside the session working dir.
//     The caller wrote the exact file, so the target is an intended one;
//     containment adds no protection against a stated intent - it only
//     pushed the caller to a different mechanism (live 2026-09-13: an
//     agent told to write /tmp/final-e2e/paper.json was refused, then
//     produced the artifact with a shell copy). Sibling tools agree:
//     file_read/file_write resolve absolute paths and leave enforcement
//     to the optional security-engine fenceChecker, with no hard
//     working-dir fence at all.
//  2. RELATIVE path: resolved against the session working dir and fenced
//     to it - "../../etc/passwd" traversal is still refused. A relative
//     path is the ambiguous case (the caller believes it is writing
//     "inside the workspace"), so that is where the fence earns its keep.
//  3. "~": refused. Tilde is home-dir shorthand, not an explicit absolute
//     path: it resolves against a root the caller never named (and it is
//     the classic accidental-escape into dotfiles). The refusal prints
//     the expanded candidate so the model can simply re-issue it.
//  4. No session working dir: relative paths are refused (there is no
//     root to resolve against); absolute paths still work, since they
//     need no root. Never fall back to the process cwd (AGENTS.md).
//
// There is no bypass flag and no "allow anything" mode: every path is
// classified by this function before it reaches the filesystem.
func resolveExtractPath(ctx context.Context, rawPath, argName string) (string, error) {
	if strings.HasPrefix(rawPath, "~") {
		if home, herr := os.UserHomeDir(); herr == nil {
			expanded := filepath.Join(home, rawPath[1:])
			return "", fmt.Errorf("json_extract: %s %q rejected: \"~\" resolves against the user home dir, not the session working dir; pass the absolute path explicitly instead (for example %q)", argName, rawPath, expanded)
		}
		return "", fmt.Errorf("json_extract: %s %q rejected: \"~\" resolves against the user home dir, not the session working dir; pass the absolute path explicitly instead", argName, rawPath)
	}
	explicitAbs := filepath.IsAbs(rawPath)
	wd := tools.WorkingDirFromContext(ctx)
	if !explicitAbs && wd == "" {
		return "", fmt.Errorf("json_extract: %s %q rejected: no session working dir to resolve a relative path against; pass an absolute path explicitly (or pass the text directly)", argName, rawPath)
	}
	p, err := resolveToolPath(ctx, rawPath)
	if err != nil {
		return "", fmt.Errorf("json_extract: %s %q: %w", argName, rawPath, err)
	}
	if explicitAbs {
		// Explicit target: honored as written (cleaned), no fence.
		return p, nil
	}
	if err := containPath(p, wd); err != nil {
		return "", fmt.Errorf("json_extract: %s %q rejected: relative paths resolve against the session working dir %q and may not escape it (resolved %q); pass the absolute path explicitly if you mean a file outside it", argName, rawPath, wd, p)
	}
	return p, nil
}

// resolveText returns the input text: the text argument, or the contents of
// file_path. Relative file_path resolves inside the session working dir; an
// explicit absolute file_path is honored anywhere (resolveExtractPath -
// reads and writes share one policy). No os.Getwd anywhere: the daemon
// carries the working dir through the context (AGENTS.md).
func (t *JSONExtractTool) resolveText(ctx context.Context, args map[string]any) (string, error) {
	if text := stringArg(args, schemaPropText); text != "" {
		return text, nil
	}
	rawPath := stringArg(args, schemaPropFilePath)
	if rawPath == "" {
		return "", nil
	}
	p, err := resolveExtractPath(ctx, rawPath, schemaPropFilePath)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("json_extract: read %s: %w", p, err)
	}
	return string(data), nil
}

// writeOutput writes the JSON document through the same path policy as
// resolveText (resolveExtractPath): relative output_path stays fenced to the
// session working dir, an explicit absolute output_path is honored.
func (t *JSONExtractTool) writeOutput(ctx context.Context, rawPath, doc string) (string, error) {
	p, err := resolveExtractPath(ctx, rawPath, schemaPropOutputPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", fmt.Errorf("json_extract: create output dir: %w", err)
	}
	if err := os.WriteFile(p, []byte(doc+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("json_extract: write %s: %w", p, err)
	}
	return p, nil
}

// extractSchemaArg pulls and normalizes the schema argument. A JSON-encoded
// string is accepted too (some callers hold schemas as text). Parsed
// map[string]any schemas arrive as-is from JSON tool-call decoding.
func extractSchemaArg(args map[string]any) (map[string]any, error) {
	switch v := args["schema"].(type) {
	case map[string]any:
		if len(v) == 0 {
			return nil, fmt.Errorf("json_extract: schema is empty")
		}
		return v, nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil, fmt.Errorf("json_extract: schema is empty")
		}
		return parseSchemaJSON(trimmed)
	default:
		return nil, fmt.Errorf("json_extract: schema is required (object or JSON string)")
	}
}

// parseSchemaJSON unmarshals a JSON-encoded schema string.
func parseSchemaJSON(s string) (map[string]any, error) {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(s), &parsed); err != nil {
		return nil, fmt.Errorf("json_extract: schema is not valid JSON: %w", err)
	}
	return parsed, nil
}

// conformToSchema shapes the model's record to the declared schema by
// dropping top-level fields the schema does not declare. Small extractors
// sometimes echo schema keywords ("required", "type", "items") as fields —
// the grammar guarantees parseable JSON, not conformance. Null values are
// left in place (null means "not found", a legitimate record); nested
// values are untouched. Best-effort, never errors.
//
// Ordering contract (M20): keys listed in schema "required" count as
// declared even when "properties" omits them — otherwise conform would
// drop a model-produced required field and checkRequired would then fail
// with "missing required b", an error the model cannot fix by any output.
// When the schema sets "additionalProperties": true, extra keys are kept.
func conformToSchema(record map[string]any, schema map[string]any) map[string]any {
	propsRaw, ok := schema["properties"]
	if !ok {
		return record
	}
	props, ok := propsRaw.(map[string]any)
	if !ok || len(props) == 0 {
		return record
	}
	// Required-listed names are treated as declared even without a
	// properties entry (M20).
	declared := make(map[string]bool, len(props)+4)
	for name := range props {
		declared[name] = true
	}
	if reqList, ok := schema["required"].([]any); ok {
		for _, r := range reqList {
			if name, ok := r.(string); ok && name != "" {
				declared[name] = true
			}
		}
	}
	// additionalProperties: true (or a non-false schema value) permits
	// extras — don't strip them.
	allowExtra := false
	switch ap := schema["additionalProperties"].(type) {
	case bool:
		allowExtra = ap
	case map[string]any:
		// A sub-schema for extras: keep extra keys (the sub-schema is
		// not enforced — best-effort shaping, never a validator).
		allowExtra = true
	}
	out := make(map[string]any, len(record))
	for name, value := range record {
		if declared[name] {
			out[name] = value
			continue
		}
		if allowExtra {
			out[name] = value
		}
	}
	return out
}

// checkRequired verifies required fields are present and non-null in the
// record. Best-effort shape check, not a full schema validator.
func checkRequired(record map[string]any, schema map[string]any) []string {
	reqRaw, ok := schema["required"]
	if !ok {
		return nil
	}
	reqList, ok := reqRaw.([]any)
	if !ok {
		return nil
	}
	var missing []string
	for _, r := range reqList {
		name, ok := r.(string)
		if !ok {
			continue
		}
		v, present := record[name]
		if !present || v == nil {
			missing = append(missing, name)
		}
	}
	return missing
}

// stripJSONFences removes a markdown code fence some models add despite
// instructions. The GBNF path prevents fences; this keeps the tool honest
// when the endpoint has no grammar support.
func stripJSONFences(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if idx := strings.Index(s, "\n"); idx >= 0 {
		s = s[idx+1:]
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	return strings.TrimSpace(s)
}

// extractionModelRef reports which model served the extraction turn.
func extractionModelRef(c llm.Chatter) string {
	if c == nil {
		return ""
	}
	cfg := c.Config()
	if cfg == nil {
		return ""
	}
	if cfg.CatalogRef != "" {
		return cfg.CatalogRef
	}
	return cfg.ProviderID + "/" + cfg.ModelID
}

// gbnfJSONGrammar constrains output to one top-level JSON object with
// string, number, integer, boolean, null, array, and nested object values.
// Attached via WithRawGrammar; llama.cpp enforces it natively. Endpoints
// without grammar support ignore it — the strict parse above still reports
// malformed output.
const gbnfJSONGrammar = `root ::= object
ws ::= [ \t\n]*
string ::= "\"" char* "\""
char ::= [^"\\] | "\\" ["\\/bfnrt]
number ::= ["-"]? [0-9]+ ["." [0-9]+]? (["e" "E"] ["-" "+"]? [0-9]+)?
integer ::= ["-"]? [0-9]+
boolean ::= "true" | "false"
null ::= "null"
value ::= object | array | string | number | boolean | null
object ::= "{" ws (member ("," ws member)*)? ws "}"
member ::= string ws ":" ws value
array ::= "[" ws (value ("," ws value)*)? ws "]"`
