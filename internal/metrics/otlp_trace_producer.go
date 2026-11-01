// Package metrics — otlp_trace_producer.go: bus-driven span producer for
// the OTLP trace exporter (leaf 07 T2). Lives in the daemon package's
// neighbor because internal/agent imports internal/metrics (an in-metrics
// producer would cycle); this file is the agent-payload-free half:
// producer machinery over anonymous span specs. The daemon wiring decodes
// bus payloads and calls RecordTurnSpan / RecordStepSpan here.
//
// Subscriptions are ASYNC bus consumers (the daemon owns the goroutines):
// the agent loop never blocks on export, and the producer is nil-safe
// everywhere (a nil exporter is the disabled posture — nothing is built).
package metrics

import (
	"encoding/hex"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// TurnSpanSpec carries the decoded turn.terminal fields the producer maps
// to a chat.turn span (agent-free decode lives in the daemon wiring).
// TraceID/TurnSpanID (llm-hop-tracing leaf 01) are struct fields ONLY --
// the producer behavior is unchanged; leaf 03 wires span production.
type TurnSpanSpec struct {
	ConversationID string
	SessionID      string
	TurnID         string
	TaskID         string
	IntentType     string
	AgentID        string
	HandlerCase    string
	Status         string
	Model          string
	Error          string
	DurationMS     int64
	TraceID        string // 32-hex; "" on pre-mint early failures
	TurnSpanID     string // 16-hex turn span; parent of leaf 03's hop spans
}

// StepSpanSpec carries the decoded task.step_completed fields.
type StepSpanSpec struct {
	TaskID     string
	StepID     string
	AgentID    string
	State      string
	DurationMS int64
}

// spanErrorTruncateLen bounds the error.message attribute (C6): longer
// provider error strings are truncated so the attribute cannot carry an
// unbounded payload.
const spanErrorTruncateLen = 256

// LLMCallSpanSpec carries one completed LLM call (llm-hop-tracing leaf 03,
// C6) - the daemon wiring maps the SetLLMCallObserver LLMCallRecord onto it.
// IDs arrive as lowercase-hex strings from leaf 01; malformed hex degrades
// to the exporter's random fill (a programming error upstream, noted by no
// one - the span still exports under a fresh identity).
type LLMCallSpanSpec struct {
	TraceID, SpanID               string
	ParentSpanID                  string // turn span id; "" = no parent linkage
	Provider, ModelID             string
	AgentID, SessionID            string
	ConversationID                string
	TokensSent, TokensRecv        int
	TokensCached, ReasoningTokens int
	CacheCreationTokens           int
	IsError                       bool
	ErrorMessage                  string // truncated to 256 chars by the producer
	LatencyMS                     int64  // > 0 drives Start/End; 0 = instantaneous span
}

// ToolCallSpanSpec carries one finished tool execution (leaf 03, C6),
// decoded from the raw agent.event.tool_execution_end topic. Shape-only:
// ArgumentsKeys/ArgumentsBytes are key NAMES and a byte count - raw argument
// values never reach this struct or any attribute.
type ToolCallSpanSpec struct {
	TraceID, SpanID, ParentSpanID string
	ToolName, ToolCallID          string
	ArgumentsKeys                 string
	ArgumentsBytes                int
	Success, Blocked, Cached      bool
	Error                         string // truncated to 256 chars
	DurationMS                    int64
}

// decodeHex16/decodeHex8 decode fixed-size hex IDs. Malformed input yields
// the zero array, which the exporter treats as "random fill" - silent
// degradation, exactly the C6 posture for upstream programming errors.
func decodeHex16(s string) [16]byte {
	var out [16]byte
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != len(out) {
		return out
	}
	copy(out[:], b)
	return out
}

func decodeHex8(s string) [8]byte {
	var out [8]byte
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != len(out) {
		return out
	}
	copy(out[:], b)
	return out
}

// TraceProducer maps decoded lifecycle specs into spans on a
// TraceExporter. Nil exporter = disabled posture: record methods are
// no-ops.
type TraceProducer struct {
	exp      *TraceExporter
	sampler  func(Span) bool
	logger   *slog.Logger
	startRef time.Time // producer-start floor for span timestamps
}

// NewTraceProducer builds a producer over the exporter. The BUS
// subscriptions live in the daemon wiring (payload decode needs
// internal/agent types, which cannot import metrics). A nil exporter is
// the disabled posture: the producer is returned but records nothing.
func NewTraceProducer(exp *TraceExporter, sampleRate float64, logger *slog.Logger) *TraceProducer {
	if logger == nil {
		logger = slog.Default()
	}
	p := &TraceProducer{
		exp:      exp,
		logger:   logger.With("component", "otlp-trace-producer"),
		startRef: time.Now(),
	}
	if exp != nil {
		p.sampler = func(sp Span) bool { return sampleTrace(sp, sampleRate) }
	}
	return p
}

// RecordTurnSpan maps a turn terminal event to one chat.turn span.
// Best-effort: sampling and a disabled exporter silently skip.
func (p *TraceProducer) RecordTurnSpan(ev TurnSpanSpec) {
	if p == nil || p.exp == nil {
		return
	}
	end := time.Now()
	start := end
	if ev.DurationMS > 0 {
		start = end.Add(-time.Duration(ev.DurationMS) * time.Millisecond)
	}
	if start.Before(p.startRef) {
		start = p.startRef
	}
	attrs := map[string]string{
		"handler_case": ev.HandlerCase,
		"status":       ev.Status,
	}
	if ev.TurnID != "" {
		attrs["turn_id"] = ev.TurnID
	}
	if ev.IntentType != "" {
		attrs["intent_type"] = ev.IntentType
	}
	if ev.TaskID != "" {
		attrs["task_id"] = ev.TaskID
	}
	if ev.Model != "" {
		attrs["model"] = ev.Model
	}
	if ev.Error != "" {
		attrs["error"] = ev.Error
	}
	sp := Span{
		// Leaf 03 (C5/C6): the handler's minted trace/turn-span IDs, when
		// present, override the exporter's random fill so every hop span
		// of the turn shares one trace and parents on THIS span id. Empty
		// values keep the zero arrays = legacy random-fill behavior.
		TraceID:    decodeHex16(ev.TraceID),
		SpanID:     decodeHex8(ev.TurnSpanID),
		Name:       "chat.turn",
		Start:      start,
		End:        end,
		Attributes: attrs,
		// Langfuse correlation rides on the span via the sink's base
		// attributes; the raw fields feed it.
		ConversationID: ev.ConversationID,
		SessionID:      ev.SessionID,
		AgentID:        ev.AgentID,
		IsError:        ev.Status == "failed",
	}
	if p.sampler != nil && !p.sampler(sp) {
		return
	}
	p.exp.RecordSpan(sp)
}

// spanStartEnd resolves the span timestamps: DurationMS > 0 backdates the
// start (bounded by the producer-start floor); 0 = instantaneous span.
// Mirrors RecordTurnSpan's posture (posture to mirror per C6).
func (p *TraceProducer) spanStartEnd(durationMS int64) (time.Time, time.Time) {
	end := time.Now()
	start := end
	if durationMS > 0 {
		start = end.Add(-time.Duration(durationMS) * time.Millisecond)
	}
	if start.Before(p.startRef) {
		start = p.startRef
	}
	return start, end
}

// record applies sampling (TraceID is already set, so the decision is
// deterministic per trace) and exports. Shared by the hop-span methods.
func (p *TraceProducer) record(sp Span) {
	if p.sampler != nil && !p.sampler(sp) {
		return
	}
	p.exp.RecordSpan(sp)
}

// RecordLLMCallSpan maps one completed LLM call to one llm.call span with
// the GenAI semantic-convention attributes (FINAL ATTRIBUTE TABLE, leaf 00;
// C8 prohibition: no prompt/completion/error-payload content beyond the
// truncated error.message). ev.SpanID is REUSED as the exported span id so
// the SQL llm_calls.span_id and the trace join exactly. Zero-valued
// attributes are omitted. Nil exporter = no-op.
func (p *TraceProducer) RecordLLMCallSpan(ev LLMCallSpanSpec) {
	if p == nil || p.exp == nil {
		return
	}
	start, end := p.spanStartEnd(ev.LatencyMS)
	attrs := map[string]string{
		"gen_ai.operation.name": "chat",
	}
	if ev.Provider != "" {
		attrs["gen_ai.provider.name"] = ev.Provider
	}
	if ev.ModelID != "" {
		attrs["gen_ai.request.model"] = ev.ModelID
	}
	if ev.TokensSent > 0 {
		attrs["gen_ai.usage.input_tokens"] = strconv.Itoa(ev.TokensSent)
	}
	if ev.TokensRecv > 0 {
		attrs["gen_ai.usage.output_tokens"] = strconv.Itoa(ev.TokensRecv)
	}
	if ev.TokensCached > 0 {
		attrs["gen_ai.usage.cache_read.input_tokens"] = strconv.Itoa(ev.TokensCached)
	}
	if ev.CacheCreationTokens > 0 {
		attrs["gen_ai.usage.cache_write.input_tokens"] = strconv.Itoa(ev.CacheCreationTokens)
	}
	if ev.ReasoningTokens > 0 {
		attrs["gen_ai.usage.reasoning.output_tokens"] = strconv.Itoa(ev.ReasoningTokens)
	}
	if ev.IsError && ev.ErrorMessage != "" {
		// Documented C8 deviation (RESEARCH-FINDINGS note E1): the
		// convention-preferred channel is the span status description;
		// error.message is kept in addition for the SQL-join workflow.
		attrs["error.message"] = spanErrorTruncate(ev.ErrorMessage)
		attrs["error.type"] = llmErrorClass(ev.ErrorMessage)
	}
	sp := Span{
		TraceID:        decodeHex16(ev.TraceID),
		SpanID:         decodeHex8(ev.SpanID),
		ParentSpanID:   decodeHex8(ev.ParentSpanID),
		Name:           "llm.call",
		Start:          start,
		End:            end,
		Attributes:     attrs,
		ConversationID: ev.ConversationID,
		SessionID:      ev.SessionID,
		AgentID:        ev.AgentID,
		IsError:        ev.IsError,
	}
	p.record(sp)
}

// RecordToolCallSpan maps one finished tool execution to one tool.call span
// with shape-only attributes (key names + byte count; NEVER argument
// values). ev.SpanID is reused as the exported span id. Nil exporter = no-op.
func (p *TraceProducer) RecordToolCallSpan(ev ToolCallSpanSpec) {
	if p == nil || p.exp == nil {
		return
	}
	start, end := p.spanStartEnd(ev.DurationMS)
	attrs := map[string]string{
		"gen_ai.operation.name": "execute_tool",
	}
	if ev.ToolName != "" {
		attrs["gen_ai.tool.name"] = ev.ToolName
	}
	if ev.ToolCallID != "" {
		attrs["gen_ai.tool.call.id"] = ev.ToolCallID
	}
	if ev.ArgumentsKeys != "" {
		attrs["meept.tool.args.keys"] = ev.ArgumentsKeys
	}
	if ev.ArgumentsBytes > 0 {
		attrs["meept.tool.args.bytes"] = strconv.Itoa(ev.ArgumentsBytes)
	}
	if ev.Blocked {
		attrs["meept.tool.blocked"] = "true"
	}
	if !ev.Success && ev.Error != "" {
		attrs["error.message"] = spanErrorTruncate(ev.Error)
		attrs["error.type"] = "_OTHER"
	}
	sp := Span{
		TraceID:      decodeHex16(ev.TraceID),
		SpanID:       decodeHex8(ev.SpanID),
		ParentSpanID: decodeHex8(ev.ParentSpanID),
		Name:         "tool.call",
		Start:        start,
		End:          end,
		Attributes:   attrs,
		IsError:      !ev.Success,
	}
	p.record(sp)
}

// spanErrorTruncate cuts an error string to the C6 bound (runes, so a
// multi-byte message never splits mid-rune).
func spanErrorTruncate(msg string) string {
	r := []rune(msg)
	if len(r) <= spanErrorTruncateLen {
		return msg
	}
	return string(r[:spanErrorTruncateLen])
}

// llmErrorClass buckets an LLM error message into a low-cardinality
// error.type (OTel error conventions: low cardinality, "_OTHER" is the
// sanctioned fallback). Classification matches on the stable sentinel
// prefixes meept's own failure paths produce - it never parses provider
// payload content.
func llmErrorClass(msg string) string {
	switch {
	case strings.Contains(msg, "quota"):
		return "quota"
	case strings.Contains(strings.ToLower(msg), "refus"):
		return "refusal"
	case strings.Contains(msg, "empty content"):
		return "empty_response"
	default:
		return "_OTHER"
	}
}

// RecordStepSpan maps a step terminal event to one task.step span.
func (p *TraceProducer) RecordStepSpan(ev StepSpanSpec) {
	if p == nil || p.exp == nil {
		return
	}
	end := time.Now()
	start := end
	if ev.DurationMS > 0 {
		start = end.Add(-time.Duration(ev.DurationMS) * time.Millisecond)
	}
	if start.Before(p.startRef) {
		start = p.startRef
	}
	state := ev.State
	if state == "" {
		state = "completed"
	}
	sp := Span{
		Name:  "task.step",
		Start: start,
		End:   end,
		Attributes: map[string]string{
			"task_id": ev.TaskID,
			"step_id": ev.StepID,
			"state":   state,
		},
		// No session id rides on task.step_completed; the task id is the
		// trace grouping key for step spans.
		SessionID: ev.TaskID,
		AgentID:   ev.AgentID,
		IsError:   state == "failed",
	}
	if p.sampler != nil && !p.sampler(sp) {
		return
	}
	p.exp.RecordSpan(sp)
}
