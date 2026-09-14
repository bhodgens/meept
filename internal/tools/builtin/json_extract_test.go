package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// fakeChatter is a scripted llm.Chatter for extraction tests.
type fakeChatter struct {
	content string
	err     error
	gotMsgs []llm.ChatMessage
	gotOpts []llm.ChatOption
}

func (f *fakeChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	f.gotMsgs = messages
	f.gotOpts = opts
	if f.err != nil {
		return nil, f.err
	}
	return &llm.Response{Content: f.content}, nil
}

func (f *fakeChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return f.Chat(ctx, messages, opts...)
}

func (f *fakeChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ProviderID: "local-extract", ModelID: "lfm2-extract", CatalogRef: "local-extract/lfm2-extract"}
}

var _ llm.Chatter = (*fakeChatter)(nil)

const testExtractSchema = `{"type":"object","properties":{"title":{"type":"string"},"year":{"type":"integer"}},"required":["title"]}`

func TestJSONExtract_NameAndSchema(t *testing.T) {
	tool := NewJSONExtractTool(nil, time.Second)
	if tool.Name() != "json_extract" {
		t.Fatalf("Name = %q", tool.Name())
	}
	params := tool.Parameters()
	if _, ok := params.Properties["schema"]; !ok {
		t.Fatal("missing schema property")
	}
	if _, ok := params.Properties[schemaPropFilePath]; !ok {
		t.Fatal("missing file_path property")
	}
}

// TestJSONExtract_ArgumentShapingDescriptions guards the measured wording that
// keeps small local models (LFM2.5-8B-A1B) from folding the source text into
// the schema argument. Probe measurement (2026-09-14, llama-server Generic
// envelope, N>=12/variant): baseline description 10/12 correct; the tightened
// "schema = shape only, source text goes in text" description 31/32 pooled.
// Renaming text -> source_text (2/12) and typing schema as string (6/12)
// measurably HURT. If these strings must change, re-run the shaping probe.
func TestJSONExtract_ArgumentShapingDescriptions(t *testing.T) {
	params := NewJSONExtractTool(nil, time.Second).Parameters()
	schemaDesc := params.Properties["schema"].Description
	if !strings.Contains(schemaDesc, "SCHEMA") ||
		!strings.Contains(schemaDesc, "never any source text") ||
		!strings.Contains(schemaDesc, "text parameter") {
		t.Fatalf("schema description lost the shaping-critical wording: %q", schemaDesc)
	}
	if !strings.Contains(schemaDesc, `"type":"object"`) {
		t.Fatalf("schema description lost its example: %q", schemaDesc)
	}
	textDesc := params.Properties[schemaPropText].Description
	if !strings.Contains(textDesc, "SOURCE TEXT") ||
		!strings.Contains(textDesc, "Never put source text in schema") {
		t.Fatalf("text description lost the shaping-critical wording: %q", textDesc)
	}
	if params.Properties["schema"].Type != schemaTypeObject {
		t.Fatalf("schema property must stay type object (string type measured at 6/12): %q", params.Properties["schema"].Type)
	}
	if _, ok := params.Properties["source_text"]; ok {
		t.Fatal("text param must stay named text (source_text rename measured at 2/12)")
	}
}

func TestJSONExtract_NotConfigured(t *testing.T) {
	tool := NewJSONExtractTool(nil, time.Second)
	_, err := tool.Execute(context.Background(), map[string]any{
		"schema":       map[string]any{"type": "object"},
		schemaPropText: "hello",
	})
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected not-configured error, got %v", err)
	}
}

func TestJSONExtract_RequiresSchema(t *testing.T) {
	tool := NewJSONExtractTool(&fakeChatter{content: "{}"}, time.Second)
	_, err := tool.Execute(context.Background(), map[string]any{schemaPropText: "hello"})
	if err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("expected schema error, got %v", err)
	}
}

func TestJSONExtract_RequiresText(t *testing.T) {
	tool := NewJSONExtractTool(&fakeChatter{content: "{}"}, time.Second)
	_, err := tool.Execute(context.Background(), map[string]any{
		"schema": map[string]any{"type": "object"},
	})
	if err == nil || !strings.Contains(err.Error(), "no input text") {
		t.Fatalf("expected no-input error, got %v", err)
	}
}

func TestJSONExtract_Success(t *testing.T) {
	fake := &fakeChatter{content: `{"title":"Test Paper","year":2024}`}
	tool := NewJSONExtractTool(fake, time.Second)
	res, err := tool.Execute(context.Background(), map[string]any{
		"schema":       testExtractSchema,
		schemaPropText: "Some text about Test Paper from 2024.",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr, ok := res.(*tools.ToolResult)
	if !ok || !tr.Success {
		t.Fatalf("expected success ToolResult, got %#v", res)
	}
	m, ok := tr.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", tr.Result)
	}
	if m["model"] != "local-extract/lfm2-extract" {
		t.Errorf("model = %v", m["model"])
	}
	record, ok := m["record"].(map[string]any)
	if !ok || record["title"] != "Test Paper" {
		t.Errorf("record = %#v", m["record"])
	}
	// System prompt + user turn shape.
	if len(fake.gotMsgs) != 2 || fake.gotMsgs[0].Role != llm.RoleSystem {
		t.Errorf("messages = %#v", fake.gotMsgs)
	}
	if !strings.Contains(fake.gotMsgs[1].Content, "Test Paper from 2024") {
		t.Errorf("user turn missing text: %q", fake.gotMsgs[1].Content)
	}
	if !strings.Contains(fake.gotMsgs[1].Content, `"title"`) {
		t.Errorf("user turn missing schema: %q", fake.gotMsgs[1].Content)
	}
}

func TestJSONExtract_SchemaAsString(t *testing.T) {
	fake := &fakeChatter{content: `{"title":"X"}`}
	tool := NewJSONExtractTool(fake, time.Second)
	_, err := tool.Execute(context.Background(), map[string]any{
		"schema":       testExtractSchema,
		schemaPropText: "text",
	})
	if err != nil {
		t.Fatalf("schema-as-string rejected: %v", err)
	}
}

func TestJSONExtract_InvalidModelOutput(t *testing.T) {
	fake := &fakeChatter{content: "not json at all"}
	tool := NewJSONExtractTool(fake, time.Second)
	_, err := tool.Execute(context.Background(), map[string]any{
		"schema":       map[string]any{"type": "object"},
		schemaPropText: "text",
	})
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("expected invalid-JSON error, got %v", err)
	}
}

func TestJSONExtract_MissingRequiredField(t *testing.T) {
	fake := &fakeChatter{content: `{"year": 2024}`}
	tool := NewJSONExtractTool(fake, time.Second)
	_, err := tool.Execute(context.Background(), map[string]any{
		"schema":       testExtractSchema,
		schemaPropText: "text",
	})
	if err == nil || !strings.Contains(err.Error(), "required fields") {
		t.Fatalf("expected required-fields error, got %v", err)
	}
}

func TestJSONExtract_MissingRequiredAllowedWhenValidationOff(t *testing.T) {
	fake := &fakeChatter{content: `{"year": 2024}`}
	tool := NewJSONExtractTool(fake, time.Second)
	tool.SetValidate(false)
	res, err := tool.Execute(context.Background(), map[string]any{
		"schema":       testExtractSchema,
		schemaPropText: "text",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if tr, ok := res.(*tools.ToolResult); !ok || !tr.Success {
		t.Fatalf("expected success, got %#v", res)
	}
}

func TestJSONExtract_FencedOutputStripped(t *testing.T) {
	fake := &fakeChatter{content: "```json\n{\"title\":\"Fenced\"}\n```"}
	tool := NewJSONExtractTool(fake, time.Second)
	res, err := tool.Execute(context.Background(), map[string]any{
		"schema":       testExtractSchema,
		schemaPropText: "text",
	})
	if err != nil {
		t.Fatalf("fenced output rejected: %v", err)
	}
	tr := res.(*tools.ToolResult)
	m := tr.Result.(map[string]any)
	record := m["record"].(map[string]any)
	if record["title"] != "Fenced" {
		t.Errorf("record = %#v", record)
	}
}

func TestJSONExtract_FilePathInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("content for extraction"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &fakeChatter{content: `{"title":"F"}`}
	tool := NewJSONExtractTool(fake, time.Second)
	// H11: file reads are wd-scoped, so the ctx carries the wd.
	ctx := tools.ContextWithWorkingDir(context.Background(), dir)
	if _, err := tool.Execute(ctx, map[string]any{
		"schema":           map[string]any{"type": "object"},
		schemaPropFilePath: path,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(fake.gotMsgs[1].Content, "content for extraction") {
		t.Errorf("file content missing from prompt: %q", fake.gotMsgs[1].Content)
	}
}

func TestJSONExtract_FilePathRelativeToWorkdir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("relative workdir content"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := tools.ContextWithWorkingDir(context.Background(), dir)
	fake := &fakeChatter{content: `{"title":"R"}`}
	tool := NewJSONExtractTool(fake, time.Second)
	if _, err := tool.Execute(ctx, map[string]any{
		"schema":           map[string]any{"type": "object"},
		schemaPropFilePath: "notes.txt",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(fake.gotMsgs[1].Content, "relative workdir content") {
		t.Errorf("workdir resolution failed: %q", fake.gotMsgs[1].Content)
	}
}

func TestJSONExtract_OutputPathWrite(t *testing.T) {
	dir := t.TempDir()
	ctx := tools.ContextWithWorkingDir(context.Background(), dir)
	fake := &fakeChatter{content: `{"title":"W"}`}
	tool := NewJSONExtractTool(fake, time.Second)
	res, err := tool.Execute(ctx, map[string]any{
		"schema":             map[string]any{"type": "object"},
		schemaPropText:       "text",
		schemaPropOutputPath: "data/out.json",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr := res.(*tools.ToolResult)
	m := tr.Result.(map[string]any)
	written := m["path"].(string)
	data, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if !strings.Contains(string(data), `"title": "W"`) {
		t.Errorf("written content = %q", string(data))
	}
	// Writing makes the call mutating.
	if tool.IsReadOnly(map[string]any{schemaPropOutputPath: "x"}) {
		t.Error("output_path call should not be read-only")
	}
	if !tool.IsReadOnly(map[string]any{}) {
		t.Error("text-only call should be read-only")
	}
}

func TestJSONExtract_LongInputTruncated(t *testing.T) {
	fake := &fakeChatter{content: `{"title":"T"}`}
	tool := NewJSONExtractTool(fake, time.Second)
	big := strings.Repeat("x", maxExtractInputBytes+5000)
	if _, err := tool.Execute(context.Background(), map[string]any{
		"schema":       map[string]any{"type": "object"},
		schemaPropText: big,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := len(fake.gotMsgs[1].Content); got > maxExtractInputBytes+200 {
		t.Errorf("user turn not truncated: %d bytes", got)
	}
}

func TestJSONExtract_InstructionsIncluded(t *testing.T) {
	fake := &fakeChatter{content: `{"title":"I"}`}
	tool := NewJSONExtractTool(fake, time.Second)
	if _, err := tool.Execute(context.Background(), map[string]any{
		"schema":       map[string]any{"type": "object"},
		schemaPropText: "text",
		"instructions": "normalize years to integers",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(fake.gotMsgs[1].Content, "normalize years to integers") {
		t.Errorf("instructions missing: %q", fake.gotMsgs[1].Content)
	}
}

func TestStripJSONFences(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a":1}`, `{"a":1}`},
		{"```json\n{\"a\":1}\n```", `{"a":1}`},
		{"```\n{\"a\":1}\n```", `{"a":1}`},
		{"```json\n{\"a\":1}", `{"a":1}`},
	}
	for _, c := range cases {
		if got := stripJSONFences(c.in); got != c.want {
			t.Errorf("stripJSONFences(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCheckRequired(t *testing.T) {
	schema := map[string]any{"required": []any{"a", "b"}}
	record := map[string]any{"a": "x", "b": nil}
	missing := checkRequired(record, schema)
	if len(missing) != 1 || missing[0] != "b" {
		t.Errorf("missing = %v", missing)
	}
	if got := checkRequired(map[string]any{}, map[string]any{}); got != nil {
		t.Errorf("no-required schema returned %v", got)
	}
}

func TestJSONExtract_StripsUndeclaredSchemaEcho(t *testing.T) {
	// Regression (live smoke, 2026-09-07): the extractor echoed the
	// schema's "required" keyword as an output field.
	fake := &fakeChatter{content: `{"title":"Attention Is All You Need","year":2017,"required":["title"],"type":"object"}`}
	tool := NewJSONExtractTool(fake, time.Second)
	res, err := tool.Execute(context.Background(), map[string]any{
		"schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{"type": "string"},
				"year":  map[string]any{"type": "integer"},
			},
			"required": []any{"title"},
		},
		schemaPropText: "Attention Is All You Need appeared in 2017.",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr := res.(*tools.ToolResult)
	m := tr.Result.(map[string]any)
	record := m["record"].(map[string]any)
	if _, present := record["required"]; present {
		t.Errorf("schema keyword 'required' leaked into record: %#v", record)
	}
	if _, present := record["type"]; present {
		t.Errorf("schema keyword 'type' leaked into record: %#v", record)
	}
	if record["title"] != "Attention Is All You Need" || record["year"] != float64(2017) {
		t.Errorf("declared fields damaged: %#v", record)
	}
	// The emitted JSON document must match the conformed record too.
	doc := m["json"].(string)
	if strings.Contains(doc, `"required"`) {
		t.Errorf("json document still contains undeclared key: %s", doc)
	}
}

func TestConformToSchema(t *testing.T) {
	schema := map[string]any{
		"properties": map[string]any{
			"a": map[string]any{"type": "string"},
			"b": map[string]any{"type": "integer"},
		},
	}
	record := map[string]any{"a": "x", "b": 1.0, "required": []any{"a"}, "extra": true}
	got := conformToSchema(record, schema)
	if len(got) != 2 {
		t.Fatalf("conformed record = %#v, want 2 keys", got)
	}
	// No properties key: record passes through untouched.
	passthrough := conformToSchema(record, map[string]any{})
	if len(passthrough) != 4 {
		t.Errorf("no-schema passthrough damaged record: %#v", passthrough)
	}
	// Nil properties map (malformed schema): passthrough.
	malformed := conformToSchema(record, map[string]any{"properties": "junk"})
	if len(malformed) != 4 {
		t.Errorf("malformed schema passthrough damaged record: %#v", malformed)
	}
}

func TestJSONExtract_SetChatterNilSafe(t *testing.T) {
	tool := NewJSONExtractTool(nil, time.Second)
	tool.SetChatter(nil) // must not panic, must not change state
	if tool.chatter != nil {
		t.Error("nil SetChatter should be a no-op")
	}
	tool.SetChatter(&fakeChatter{content: "{}"})
	if tool.chatter == nil {
		t.Error("non-nil SetChatter should wire the client")
	}
}

// --- Path policy tests (explicit absolute honored; relative stays fenced) ---
//
// POLICY (2026-09-13): an ABSOLUTE file_path/output_path is an explicit
// target and is honored anywhere; a RELATIVE path resolves against the
// session working dir and may not escape it (traversal or symlink); "~" is
// refused (home shorthand, not an explicit path). See resolveExtractPath.

// TestJSONExtract_ExplicitAbsoluteOutputPathHonored is the live 2026-09-13
// case: "Use the json_extract tool to extract the paper metadata and write
// it to /tmp/final-e2e/paper.json" was refused by the containment check, so
// the model produced the artifact with a shell copy instead. It now works.
func TestJSONExtract_ExplicitAbsoluteOutputPathHonored(t *testing.T) {
	wd := t.TempDir()
	outside := t.TempDir() // the target lives OUTSIDE the session working dir
	target := filepath.Join(outside, "final-e2e", "paper.json")
	ctx := tools.ContextWithWorkingDir(context.Background(), wd)
	fake := &fakeChatter{content: `{"title":"Attention Is All You Need","year":2017}`}
	tool := NewJSONExtractTool(fake, time.Second)

	res, err := tool.Execute(ctx, map[string]any{
		"schema":             testExtractSchema,
		schemaPropText:       "Attention Is All You Need (2017).",
		schemaPropOutputPath: target,
	})
	if err != nil {
		t.Fatalf("explicit absolute output_path %q rejected: %v", target, err)
	}
	m := res.(*tools.ToolResult).Result.(map[string]any)
	if got := m["path"]; got != target {
		t.Errorf("result path = %v, want %q", got, target)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("absolute output_path did not write the file: %v", err)
	}
	if !strings.Contains(string(data), `"title": "Attention Is All You Need"`) {
		t.Errorf("written content = %q", string(data))
	}
	// A missing parent dir outside the wd is created (MkdirAll), and the
	// write happens even with no session working dir at all.
	res, err = tool.Execute(context.Background(), map[string]any{
		"schema":             testExtractSchema,
		schemaPropText:       "text",
		schemaPropOutputPath: target,
	})
	if err != nil {
		t.Fatalf("absolute output_path with no working dir rejected: %v", err)
	}
	if got := res.(*tools.ToolResult).Result.(map[string]any)["path"]; got != target {
		t.Errorf("no-wd absolute path = %v, want %q", got, target)
	}
}

// TestJSONExtract_ExplicitAbsoluteFilePathReadHonored: the same class of
// refusal hit file_path reads outside the working dir; they are honored now.
func TestJSONExtract_ExplicitAbsoluteFilePathReadHonored(t *testing.T) {
	wd := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(outside, "paper.txt")
	if err := os.WriteFile(src, []byte("absolute read content"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &fakeChatter{content: `{"title":"A"}`}
	tool := NewJSONExtractTool(fake, time.Second)
	for _, ctx := range []context.Context{
		tools.ContextWithWorkingDir(context.Background(), wd),
		context.Background(), // no working dir: absolute needs no root
	} {
		if _, err := tool.Execute(ctx, map[string]any{
			"schema":           map[string]any{"type": "object"},
			schemaPropFilePath: src,
		}); err != nil {
			t.Fatalf("explicit absolute file_path %q rejected: %v", src, err)
		}
	}
	if !strings.Contains(fake.gotMsgs[1].Content, "absolute read content") {
		t.Errorf("absolute file content missing from prompt: %q", fake.gotMsgs[1].Content)
	}
}

// TestJSONExtract_RelativeTraversalStillDenied: the fence that must survive.
func TestJSONExtract_RelativeTraversalStillDenied(t *testing.T) {
	wd := t.TempDir()
	ctx := tools.ContextWithWorkingDir(context.Background(), wd)
	tool := NewJSONExtractTool(&fakeChatter{content: `{}`}, time.Second)

	// Write escapes.
	for _, raw := range []string{"../../x.json", "../../etc/x.json", ".."} {
		_, err := tool.Execute(ctx, map[string]any{
			"schema":             map[string]any{"type": "object"},
			schemaPropText:       "text",
			schemaPropOutputPath: raw,
		})
		if err == nil {
			t.Errorf("output_path %q: expected rejection, got nil error", raw)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, "may not escape it") {
			t.Errorf("output_path %q: error = %v, want containment message", raw, err)
		}
		// The refusal must tell the model what to do instead.
		if !strings.Contains(msg, "pass the absolute path explicitly") {
			t.Errorf("output_path %q: refusal is not actionable: %v", raw, err)
		}
		if !strings.Contains(msg, wd) {
			t.Errorf("output_path %q: refusal omits the working dir root: %v", raw, err)
		}
	}
	// Read escapes.
	for _, raw := range []string{"../../etc/passwd", "../../x.txt"} {
		_, err := tool.Execute(ctx, map[string]any{
			"schema":           map[string]any{"type": "object"},
			schemaPropFilePath: raw,
		})
		if err == nil {
			t.Errorf("file_path %q: expected rejection, got nil error", raw)
			continue
		}
		if !strings.Contains(err.Error(), "may not escape it") ||
			!strings.Contains(err.Error(), "pass the absolute path explicitly") {
			t.Errorf("file_path %q: error = %v, want actionable containment message", raw, err)
		}
	}
	// Nothing was written outside the workspace.
	if _, err := os.Stat(filepath.Join(filepath.Dir(wd), "x.json")); err == nil {
		t.Error("relative escape wrote a file outside the working dir")
	}
}

// TestJSONExtract_RelativeSymlinkEscapeStillDenied: a relative path that
// resolves through a symlink out of the wd is still refused - the fence is
// not hollowed out by the explicit-absolute rule.
func TestJSONExtract_RelativeSymlinkEscapeStillDenied(t *testing.T) {
	wd := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(wd, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	ctx := tools.ContextWithWorkingDir(context.Background(), wd)
	tool := NewJSONExtractTool(&fakeChatter{content: `{}`}, time.Second)
	_, err := tool.Execute(ctx, map[string]any{
		"schema":             map[string]any{"type": "object"},
		schemaPropText:       "text",
		schemaPropOutputPath: "link/escaped.json",
	})
	if err == nil || !strings.Contains(err.Error(), "may not escape it") {
		t.Errorf("relative symlink escape: error = %v, want containment message", err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "escaped.json")); statErr == nil {
		t.Error("relative symlink escape wrote through the link")
	}
}

// TestJSONExtract_TildePathDenied: "~" is not an explicit absolute path.
func TestJSONExtract_TildePathDenied(t *testing.T) {
	wd := t.TempDir()
	ctx := tools.ContextWithWorkingDir(context.Background(), wd)
	tool := NewJSONExtractTool(&fakeChatter{content: `{}`}, time.Second)
	_, err := tool.Execute(ctx, map[string]any{
		"schema":             map[string]any{"type": "object"},
		schemaPropText:       "text",
		schemaPropOutputPath: "~/outside.json",
	})
	if err == nil {
		t.Fatal("tilde output_path accepted")
	}
	msg := err.Error()
	if !strings.Contains(msg, "resolves against the user home dir") {
		t.Errorf("tilde refusal unclear: %v", err)
	}
	// The refusal must hand the model the expanded absolute path to re-issue.
	if !strings.Contains(msg, "pass the absolute path explicitly") {
		t.Errorf("tilde refusal not actionable: %v", err)
	}
	if home, herr := os.UserHomeDir(); herr == nil && !strings.Contains(msg, filepath.Join(home, "outside.json")) {
		t.Errorf("tilde refusal omits the expanded candidate: %v", err)
	}
	// Same for reads.
	if _, err := tool.Execute(ctx, map[string]any{
		"schema":           map[string]any{"type": "object"},
		schemaPropFilePath: "~/.ssh/id_rsa",
	}); err == nil || !strings.Contains(err.Error(), "user home dir") {
		t.Errorf("tilde file_path: error = %v, want home-dir refusal", err)
	}
}

// TestJSONExtract_NoWorkdir_RejectsRelativePaths: a relative path with no
// session working dir has no root to resolve against - refused, never
// resolved against the daemon's own cwd (AGENTS.md).
func TestJSONExtract_NoWorkdir_RejectsRelativePaths(t *testing.T) {
	fake := &fakeChatter{content: `{}`}
	tool := NewJSONExtractTool(fake, time.Second)
	if _, err := tool.Execute(context.Background(), map[string]any{
		"schema":           map[string]any{"type": "object"},
		schemaPropFilePath: "notes.txt",
	}); err == nil || !strings.Contains(err.Error(), "no session working dir") {
		t.Errorf("file_path without wd: error = %v, want no-session-working-dir message", err)
	}
	_, err := tool.Execute(context.Background(), map[string]any{
		"schema":             map[string]any{"type": "object"},
		schemaPropText:       "text",
		schemaPropOutputPath: "out.json",
	})
	if err == nil || !strings.Contains(err.Error(), "no session working dir") {
		t.Errorf("output_path without wd: error = %v, want no-session-working-dir message", err)
	}
	if err != nil && !strings.Contains(err.Error(), "pass an absolute path") {
		t.Errorf("no-wd refusal not actionable: %v", err)
	}
}

// TestJSONExtract_RelativePathInsideWorkdirAllowed: the ordinary case the
// fence exists to protect - a relative path under the working dir, including
// one whose ".." segments stay inside it.
func TestJSONExtract_RelativePathInsideWorkdirAllowed(t *testing.T) {
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "notes.txt"), []byte("fenced read"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := tools.ContextWithWorkingDir(context.Background(), wd)
	fake := &fakeChatter{content: `{"title":"R"}`}
	tool := NewJSONExtractTool(fake, time.Second)

	// Read: relative, and absolute-inside-wd, both work.
	for _, raw := range []string{"notes.txt", "./notes.txt", filepath.Join(wd, "notes.txt")} {
		if _, err := tool.Execute(ctx, map[string]any{
			"schema":           map[string]any{"type": "object"},
			schemaPropFilePath: raw,
		}); err != nil {
			t.Errorf("file_path %q rejected: %v", raw, err)
		}
	}
	// Write: relative, nested, and ".."-inside-wd resolve under the wd.
	for _, tc := range []struct{ raw, want string }{
		{"data/out.json", filepath.Join(wd, "data", "out.json")},
		{"nested/../inside.json", filepath.Join(wd, "inside.json")},
		{"./dot.json", filepath.Join(wd, "dot.json")},
	} {
		res, err := tool.Execute(ctx, map[string]any{
			"schema":             testExtractSchema,
			schemaPropText:       "text",
			schemaPropOutputPath: tc.raw,
		})
		if err != nil {
			t.Errorf("output_path %q rejected: %v", tc.raw, err)
			continue
		}
		if got := res.(*tools.ToolResult).Result.(map[string]any)["path"]; got != tc.want {
			t.Errorf("output_path %q -> %v, want %q", tc.raw, got, tc.want)
		}
		if _, err := os.Stat(tc.want); err != nil {
			t.Errorf("output_path %q not written to %s: %v", tc.raw, tc.want, err)
		}
	}
}

// --- M20 conform ordering tests ---

func TestConformToSchema_PreservesRequiredKeyMissingFromProperties(t *testing.T) {
	// M20: required:[a,b] with properties:{a} must NOT drop a
	// model-produced b — conform ran first and checkRequired would
	// then fail with an unfixable "missing required b".
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"a": map[string]any{"type": "string"},
		},
		"required": []any{"a", "b"},
	}
	record := map[string]any{"a": "x", "b": 1.0}
	got := conformToSchema(record, schema)
	if got["b"] != 1.0 {
		t.Fatalf("required-listed key dropped by conform: %#v", got)
	}
	// And end-to-end: Execute must succeed rather than error.
	fake := &fakeChatter{content: `{"a":"x","b":7}`}
	tool := NewJSONExtractTool(fake, time.Second)
	res, err := tool.Execute(context.Background(), map[string]any{
		"schema":       map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{}}, "required": []any{"a", "b"}},
		schemaPropText: "text",
	})
	if err != nil {
		t.Fatalf("required-key-preserved flow failed: %v", err)
	}
	m := res.(*tools.ToolResult).Result.(map[string]any)
	if m["record"].(map[string]any)["b"] != float64(7) {
		t.Errorf("record lost required key b: %#v", m["record"])
	}
}

func TestConformToSchema_AdditionalPropertiesTrueKeepsExtras(t *testing.T) {
	schema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"a": map[string]any{"type": "string"}},
		"additionalProperties": true,
	}
	record := map[string]any{"a": "x", "extra": true}
	got := conformToSchema(record, schema)
	if got["extra"] != true {
		t.Fatalf("additionalProperties:true stripped extras: %#v", got)
	}
	// Explicit false still strips.
	schema["additionalProperties"] = false
	if got := conformToSchema(record, schema); len(got) != 1 {
		t.Fatalf("additionalProperties:false kept extras: %#v", got)
	}
	// A sub-schema value for extras is also treated as permissive
	// (best-effort shaping, not a validator).
	schema["additionalProperties"] = map[string]any{"type": "string"}
	if got := conformToSchema(record, schema); got["extra"] != true {
		t.Fatalf("additionalProperties sub-schema stripped extras: %#v", got)
	}
}

// --- LOW-utf8 truncation tests ---

func TestTruncateUTF8_TrimsToRuneBoundary(t *testing.T) {
	// "héllo" — é is 2 bytes, so index 2 is mid-rune when cut at 3.
	s := "héllo"
	got := truncateUTF8(s, 3)
	if got != "hé" {
		t.Errorf("truncateUTF8(%q, 3) = %q, want %q", s, got, "hé")
	}
	if !utf8.ValidString(got) {
		t.Errorf("truncated string is not valid UTF-8: %q", got)
	}
	// Cut exactly at a rune start: unchanged prefix.
	if got := truncateUTF8(s, 2); got != "h" {
		t.Errorf("truncateUTF8(%q, 2) = %q, want %q (cut lands mid-rune, backs off to byte 0)", s, got, "h")
	}
	// Cut at 1 = also mid-rune (é spans bytes 1-2), backs off to "h".
	if got := truncateUTF8(s, 1); got != "h" {
		t.Errorf("truncateUTF8(%q, 1) = %q, want %q", s, got, "h")
	}
	// ASCII: pure byte cut.
	if got := truncateUTF8("abcdef", 3); got != "abc" {
		t.Errorf("ascii cut = %q, want abc", got)
	}
	// Multibyte at every boundary stays valid.
	// U+65E5 written as an escape so this source file stays ASCII; the
	// runtime string is still 3 bytes per rune, which is the point.
	long := strings.Repeat("\u65e5", 100) // 3 bytes each
	for i := 0; i <= len(long); i++ {
		if got := truncateUTF8(long, i); !utf8.ValidString(got) {
			t.Fatalf("truncateUTF8(multibyte, %d) produced invalid UTF-8: %q", i, got)
		}
	}
}

func TestJSONExtract_LongInputTruncatedOnRuneBoundary(t *testing.T) {
	fake := &fakeChatter{content: `{}`}
	tool := NewJSONExtractTool(fake, time.Second)
	// Pad ASCII then put a 3-byte char so the cut lands mid-rune.
	big := strings.Repeat("a", maxExtractInputBytes-1) + "\u65e5\u672c\u8a9e\u30c6\u30ad\u30b9\u30c8"
	if _, err := tool.Execute(context.Background(), map[string]any{
		"schema":       map[string]any{"type": "object"},
		schemaPropText: big,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	userTurn := ""
	for _, m := range fake.gotMsgs {
		if m.Role == llm.RoleUser {
			userTurn = m.Content
		}
	}
	// The text must be truncated and never carry an invalid sequence.
	if !utf8.ValidString(userTurn) {
		t.Error("truncated prompt is not valid UTF-8 (rune split mid-sequence)")
	}
}
