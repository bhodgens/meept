package activity

// execution_projector.go - bus -> ExecutionStore projection (issue #160
// follow-up). Subscribes to the two typed execution-result topics and
// appends one durable row per execution. The WS relay keeps its own
// subscriptions for live frames; this projection is the queryable history.

import (
	"context"
	"log/slog"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
)

// WireExecutionProjector subscribes the store to bot.result and
// employee.result. Returns the subscribers so the daemon can unsubscribe
// them on shutdown (goroutine teardown, same posture as WireBusBridge).
// Nil bus or nil store is a no-op returning nil.
func WireExecutionProjector(b *bus.MessageBus, store *ExecutionStore, logger *slog.Logger) []*bus.Subscriber {
	if b == nil || store == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}

	appendRun := func(tier string, ev agent.ExecutionResultEvent) {
		run := ExecutionRun{
			Tier:       tier,
			AgentID:    ev.AgentID,
			Status:     ev.Status,
			Output:     ev.Output,
			TokensUsed: ev.TokensUsed,
			Error:      ev.Error,
			DurationMS: ev.DurationMS,
			StopReason: string(ev.StopReason),
			TriggerRef: ev.TriggerRef,
			SessionID:  ev.SessionID,
			CreatedAt:  ev.Timestamp,
		}
		// A zero timestamp on the event means the producer did not stamp
		// one; Append fills server time. Never drop a run for that.
		if err := store.Append(context.Background(), run); err != nil {
			logger.Warn("execution projector: append failed", "tier", tier, "agent_id", ev.AgentID, "error", err)
		}
	}

	subs := make([]*bus.Subscriber, 0, 2)
	subs = append(subs, bus.SubscribeT(b, "execution-projector-bot", agent.TopicBotResult, func(ev agent.ExecutionResultEvent) {
		appendRun("bot", ev)
	}))
	subs = append(subs, bus.SubscribeT(b, "execution-projector-employee", agent.TopicEmployeeResult, func(ev agent.ExecutionResultEvent) {
		appendRun("employee", ev)
	}))
	return subs
}
