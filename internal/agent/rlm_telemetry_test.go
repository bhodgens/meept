package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTelemetryEmitter_EmitSpan(t *testing.T) {
	// Create temp file.
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.jsonl")
	t.Setenv("HALO_TELEMETRY_PATH", path)

	emitter := NewTelemetryEmitter("test-run-123")
	if !emitter.IsEnabled() {
		t.Fatal("expected emitter to be enabled")
	}

	span := OTLPSpan{
		TraceID:   "test-trace-456",
		SpanID:    "span-789",
		Name:      "llm.call",
		Kind:      "LLM",
		StartTime: time.Now().Add(-1 * time.Second),
		EndTime:   time.Now(),
		Attributes: map[string]string{
			"model": "gpt-4.1-nano",
		},
	}

	err := emitter.EmitSpan(span)
	if err != nil {
		t.Fatal(err)
	}

	// Read back and verify.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var decoded OTLPSpan
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.TraceID != span.TraceID {
		t.Errorf("trace_id mismatch: got %s, want %s", decoded.TraceID, span.TraceID)
	}
	if decoded.Kind != "LLM" {
		t.Errorf("kind mismatch: got %s, want LLM", decoded.Kind)
	}
	if decoded.Name != "llm.call" {
		t.Errorf("name mismatch: got %s, want llm.call", decoded.Name)
	}
}

func TestTelemetryEmitter_Disabled(t *testing.T) {
	// Don't set HALO_TELEMETRY_PATH - should be disabled.
	t.Setenv("HALO_TELEMETRY_PATH", "")

	emitter := NewTelemetryEmitter("test-run-abc")
	if emitter.IsEnabled() {
		t.Fatal("expected emitter to be disabled when env var not set")
	}

	// Emit should be no-op.
	err := emitter.EmitSpan(OTLPSpan{})
	if err != nil {
		t.Fatal("expected no error on disabled emitter")
	}
}

func TestTelemetryEmitter_EmitLLMSpan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.jsonl")
	t.Setenv("HALO_TELEMETRY_PATH", path)

	emitter := NewTelemetryEmitter("test-run-llm")

	err := emitter.EmitLLMSpan(
		"rlm.analyze",
		"gpt-4.1-nano",
		"Analyze these traces...",
		"Found 3 failure modes",
		150,
		80,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Verify.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var decoded OTLPSpan
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Kind != "LLM" {
		t.Errorf("expected LLM kind, got %s", decoded.Kind)
	}
	if decoded.Attributes["model"] != "gpt-4.1-nano" {
		t.Errorf("expected gpt-4.1-nano model, got %s", decoded.Attributes["model"])
	}
	if decoded.Attributes["prompt_tokens"] != "150" {
		t.Errorf("expected 150 prompt tokens, got %s", decoded.Attributes["prompt_tokens"])
	}
}

func TestTelemetryEmitter_EmitToolSpan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.jsonl")
	t.Setenv("HALO_TELEMETRY_PATH", path)

	emitter := NewTelemetryEmitter("test-run-tool")

	err := emitter.EmitToolSpan(
		"trace.analysis",
		"view_trace",
		`{"trace_id": "abc123"}`,
		`{"spans": [...]}`,
		250,
	)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var decoded OTLPSpan
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Kind != "TOOL" {
		t.Errorf("expected TOOL kind, got %s", decoded.Kind)
	}
	if decoded.Attributes["tool_name"] != "view_trace" {
		t.Errorf("expected view_trace tool, got %s", decoded.Attributes["tool_name"])
	}
	if decoded.Attributes["duration_ms"] != "250" {
		t.Errorf("expected 250ms duration, got %s", decoded.Attributes["duration_ms"])
	}
}

func TestTelemetryEmitter_EmitAgentSpan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.jsonl")
	t.Setenv("HALO_TELEMETRY_PATH", path)

	emitter := NewTelemetryEmitter("test-run-agent")

	err := emitter.EmitAgentSpan(
		"subagent.spawn",
		"agent-001",
		"Analyzing trace failures...",
		1,
	)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var decoded OTLPSpan
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Kind != "AGENT" {
		t.Errorf("expected AGENT kind, got %s", decoded.Kind)
	}
	if decoded.Attributes["agent_id"] != "agent-001" {
		t.Errorf("expected agent-001, got %s", decoded.Attributes["agent_id"])
	}
	if decoded.Attributes["depth"] != "1" {
		t.Errorf("expected depth 1, got %s", decoded.Attributes["depth"])
	}
}

func TestTelemetryEmitter_MultipleSpans(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.jsonl")
	t.Setenv("HALO_TELEMETRY_PATH", path)

	emitter := NewTelemetryEmitter("test-run-multi")

	// Emit 3 spans.
	err := emitter.EmitLLMSpan("plan", "gpt-4", "plan prompt", "plan response", 100, 50)
	if err != nil {
		t.Fatal(err)
	}
	err = emitter.EmitToolSpan("tool", "view_trace", "{}", "{}", 100)
	if err != nil {
		t.Fatal(err)
	}
	err = emitter.EmitAgentSpan("agent", "root", "content", 0)
	if err != nil {
		t.Fatal(err)
	}

	// Read and verify 3 lines.
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineCount := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var span OTLPSpan
		if err := json.Unmarshal([]byte(line), &span); err != nil {
			t.Fatalf("failed to parse line %d: %v", lineCount, err)
		}
		lineCount++
	}

	if lineCount != 3 {
		t.Errorf("expected 3 spans, got %d", lineCount)
	}
}
