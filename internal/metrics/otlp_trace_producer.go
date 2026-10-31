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
	"log/slog"
	"time"
)

// TurnSpanSpec carries the decoded turn.terminal fields the producer maps
// to a chat.turn span (agent-free decode lives in the daemon wiring).
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
}

// StepSpanSpec carries the decoded task.step_completed fields.
type StepSpanSpec struct {
	TaskID     string
	StepID     string
	AgentID    string
	State      string
	DurationMS int64
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
