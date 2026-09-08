package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	_, err := tool.Execute(context.Background(), map[string]any{
		"schema":           map[string]any{"type": "object"},
		schemaPropFilePath: path,
	})
	if err != nil {
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
