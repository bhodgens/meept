package agent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// OTLPSpan represents a single telemetry span.
// Shape matches OpenInference for compatibility with HALO/Catalyst.
type OTLPSpan struct {
	TraceID    string            `json:"trace_id"`
	SpanID     string            `json:"span_id"`
	ParentID   string            `json:"parent_id,omitempty"`
	Name       string            `json:"name"`
	Kind       string            `json:"kind"` // "LLM", "TOOL", "AGENT"
	StartTime  time.Time         `json:"start_time"`
	EndTime    time.Time         `json:"end_time"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// TelemetryEmitter writes OTLP-style spans to a JSONL file.
// Enabled when HALO_TELEMETRY_PATH env var is set.
// Modeled after HALO's telemetry emission (main.py:217-232).
type TelemetryEmitter struct {
	mu      sync.Mutex
	path    string
	runID   string
	enabled bool
}

// NewTelemetryEmitter creates an emitter for the given run ID.
// Checks HALO_TELEMETRY_PATH env var; if unset, returns disabled emitter.
func NewTelemetryEmitter(runID string) *TelemetryEmitter {
	path := os.Getenv("HALO_TELEMETRY_PATH")
	if path == "" {
		return &TelemetryEmitter{} // disabled
	}
	return &TelemetryEmitter{
		path:    path,
		runID:   runID,
		enabled: true,
	}
}

// EmitSpan writes a single span to the telemetry JSONL file.
// Thread-safe via mutex; appends one JSON line per span.
func (te *TelemetryEmitter) EmitSpan(span OTLPSpan) error {
	if !te.enabled {
		return nil
	}

	te.mu.Lock()
	defer te.mu.Unlock()

	f, err := os.OpenFile(te.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(span)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

// EmitLLMSpan is a convenience helper for emitting LLM call spans.
func (te *TelemetryEmitter) EmitLLMSpan(name, model, prompt string, completion string, promptTokens, completionTokens int) error {
	spanID := generateSpanID()
	span := OTLPSpan{
		TraceID:   te.runID,
		SpanID:    spanID,
		Name:      name,
		Kind:      "LLM",
		StartTime: time.Now(),
		EndTime:   time.Now(),
		Attributes: map[string]string{
			"model":             model,
			"prompt":            prompt,
			"completion":        completion,
			"prompt_tokens":     fmt.Sprintf("%d", promptTokens),
			"completion_tokens": fmt.Sprintf("%d", completionTokens),
		},
	}
	return te.EmitSpan(span)
}

// EmitToolSpan is a convenience helper for emitting tool call spans.
func (te *TelemetryEmitter) EmitToolSpan(name, toolName, input, output string, durationMs int64) error {
	now := time.Now()
	span := OTLPSpan{
		TraceID:   te.runID,
		SpanID:    generateSpanID(),
		Name:      name,
		Kind:      "TOOL",
		StartTime: now.Add(-time.Duration(durationMs) * time.Millisecond),
		EndTime:   now,
		Attributes: map[string]string{
			"tool_name": toolName,
			"input":     input,
			"output":    output,
			"duration_ms": fmt.Sprintf("%d", durationMs),
		},
	}
	return te.EmitSpan(span)
}

// EmitAgentSpan is a convenience helper for emitting agent activity spans.
func (te *TelemetryEmitter) EmitAgentSpan(name, agentID, content string, depth int) error {
	span := OTLPSpan{
		TraceID:   te.runID,
		SpanID:    generateSpanID(),
		Name:      name,
		Kind:      "AGENT",
		StartTime: time.Now(),
		EndTime:   time.Now(),
		Attributes: map[string]string{
			"agent_id":  agentID,
			"content":   content,
			"depth":     fmt.Sprintf("%d", depth),
		},
	}
	return te.EmitSpan(span)
}

// IsEnabled returns true if telemetry is active.
func (te *TelemetryEmitter) IsEnabled() bool {
	return te.enabled
}

// Path returns the telemetry file path (empty if disabled).
func (te *TelemetryEmitter) Path() string {
	return te.path
}

// generateSpanID creates a cryptographically secure random span ID.
func generateSpanID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails (extremely rare).
		return fmt.Sprintf("span-%x", time.Now().UnixNano())
	}
	return "span-" + hex.EncodeToString(b)
}
