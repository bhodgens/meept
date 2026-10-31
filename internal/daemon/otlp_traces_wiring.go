// Package daemon — otlp_traces_wiring.go: construction + subscription
// wiring for the opt-in OTLP trace exporter (metrics.otlp_traces, leaf
// 07-langfuse-sink). Mirrors otlp_wiring.go (metrics):
//
//   - disabled (the default): exactly ONE Warn log line at boot, no
//     exporter, no producer, no bus subscriptions, ZERO network activity.
//   - enabled: stdlib OTLP/HTTP protobuf sink + async batch exporter built
//     and started, and the span producer subscribes to the typed
//     turn.terminal topic (internal/agent.TopicTurnTerminal via
//     bus.SubscribeT) plus the raw task.step_completed topic — bus-driven,
//     never a synchronous hook into the agent loop. Export failures never
//     fail a turn: async batch, bounded queue, drop+counter on overflow,
//     circuit-open with bounded logging in the sink.
package daemon

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/metrics"
)

// otlpTraceSubs holds the live bus subscriptions of an enabled trace
// pipeline. Zero values on disabled boots; Unsubscribe is nil-safe.
type otlpTraceSubs struct {
	subs []*bus.Subscriber
}

// Unsubscribe tears down every bus subscription (daemon shutdown).
func (s *otlpTraceSubs) Unsubscribe(b *bus.MessageBus) {
	if s == nil || b == nil {
		return
	}
	for _, sub := range s.subs {
		b.Unsubscribe(sub)
	}
	s.subs = nil
}

// wireOTLPTraces constructs and starts the OTLP trace exporter + span
// producer when metrics.otlp_traces.enabled=true. Never returns an error:
// trace export is best-effort and must not block daemon boot. The subs
// return value is nil when disabled (nothing to tear down).
func wireOTLPTraces(cfg config.OTLPTracesConfig, b *bus.MessageBus, logger *slog.Logger) (*metrics.TraceExporter, *metrics.TraceProducer, *otlpTraceSubs) {
	if !cfg.Enabled {
		// One honest line at boot; nothing else for the lifetime of the
		// process (no steady-state noise, no goroutines, no network).
		logger.Warn("OTLP trace export disabled (metrics.otlp_traces.enabled=false); no spans leave the host",
			"endpoint", cfg.Endpoint)
		return nil, nil, nil
	}

	sink := metrics.NewOTLPTraceHTTPSink(cfg.Endpoint, cfg.Headers, logger)
	exp := metrics.NewTraceExporter(metrics.TraceExporterConfig{
		ExportInterval: cfg.ExportInterval, // config-load normalization clamps zero/negative to 5s
	}, sink)
	exp.Start()

	producer := metrics.NewTraceProducer(exp, cfg.SampleRate, logger)

	subs := &otlpTraceSubs{}
	if b != nil {
		// chat.turn: the typed turn.terminal topic is the proven terminal
		// signal (one event per turn, direct and task-relayed alike).
		turnSub := bus.SubscribeT(b, "otlp-trace-turns", agent.TopicTurnTerminal, func(ev agent.TurnTerminalEvent) {
			producer.RecordTurnSpan(metrics.TurnSpanSpec{
				ConversationID: ev.ConversationID,
				SessionID:      ev.SessionID,
				TurnID:         ev.TurnID,
				TaskID:         ev.TaskID,
				IntentType:     ev.IntentType,
				AgentID:        ev.AgentID,
				HandlerCase:    ev.HandlerCase,
				Status:         ev.Status,
				Model:          ev.Model,
				Error:          ev.Error,
				DurationMS:     ev.DurationMS,
			})
		})
		subs.subs = append(subs.subs, turnSub)

		// task.step: step terminal signals from the tactical scheduler
		// (raw multi-shape topic; decode leniently). Covers completed AND
		// failed steps — the state attribute is the honest verdict.
		stepSub := b.Subscribe("otlp-trace-steps", "task.step_completed")
		subs.subs = append(subs.subs, stepSub)
		go func() {
			for msg := range stepSub.Channel {
				recordStepSpanFromBus(producer, msg.Payload)
			}
		}()
	}

	logger.Info("OTLP trace export enabled",
		"endpoint", cfg.Endpoint,
		"sample_rate", cfg.SampleRate,
		"interval", cfg.ExportInterval.String(),
	)
	return exp, producer, subs
}

// stepCompletedPayload is the lenient decode of the task.step_completed
// raw topic (multi-shape: the filter-exhaustion publisher omits agent_id
// and carries a filter error; both decode fine here).
type stepCompletedPayload struct {
	TaskID   string `json:"task_id"`
	StepID   string `json:"step_id"`
	AgentID  string `json:"agent_id"`
	State    string `json:"state"`
	Duration string `json:"duration"`
}

// recordStepSpanFromBus decodes one task.step_completed payload and records
// the span. Undecodable payloads are dropped at Debug (observability must
// never turn into error noise).
func recordStepSpanFromBus(producer *metrics.TraceProducer, payload []byte) {
	var pl stepCompletedPayload
	if err := json.Unmarshal(payload, &pl); err != nil {
		return
	}
	var durMS int64
	if d, err := time.ParseDuration(pl.Duration); err == nil && d > 0 {
		durMS = d.Milliseconds()
	}
	producer.RecordStepSpan(metrics.StepSpanSpec{
		TaskID:     pl.TaskID,
		StepID:     pl.StepID,
		AgentID:    pl.AgentID,
		State:      pl.State,
		DurationMS: durMS,
	})
}

// stopOTLPTraces flushes the exporter and tears down the producer's bus
// subscriptions. Safe on nils (every disabled boot); called from
// stopComponents on the daemon shutdown path so the final batch reaches the
// receiver.
func stopOTLPTraces(b *bus.MessageBus, exp *metrics.TraceExporter, subs *otlpTraceSubs) {
	subs.Unsubscribe(b)
	if exp != nil {
		exp.Stop()
	}
}
