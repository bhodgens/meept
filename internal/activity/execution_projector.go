package activity

// execution_projector.go - bus -> ExecutionStore projection (issue #160
// follow-up). Subscribes to the two typed execution-result topics and
// appends one durable row per execution. The WS relay keeps its own
// subscriptions for live frames; this projection is the queryable history.

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
)

// ExecutionProjector is the live bus reader for the execution-run history:
// one subscriber + reader goroutine per result topic. Unlike a bare
// bus.SubscribeT, the projector OWNS its reader goroutines so the shutdown
// path can wait for them to exit after their channels close.
//
// Why this matters (issue #160 M5): the daemon stop path unsubscribes and
// then closes the ExecutionStore. Unsubscribe closes the subscriber channel
// but the reader goroutine keeps draining the items already buffered in it,
// and each drain calls store.Append with an uncancellable
// context.Background(). Without a deterministic wait, Close can land
// mid-drain: the remaining appends fail with "database is closed", the rows
// are lost, and the projector only Warns. Stopping via Stop() closes that
// window: after it returns, every delivered event has been appended (or its
// append failure logged) and no Append can run after store.Close.
type ExecutionProjector struct {
	mu     sync.Mutex
	subs   []*bus.Subscriber
	wg     sync.WaitGroup
	closed bool
}

// WireExecutionProjector subscribes the store to bot.result and
// employee.result. Call Stop on shutdown to unsubscribe and drain the
// readers BEFORE closing the store. Nil bus or nil store is a no-op
// returning nil.
func WireExecutionProjector(b *bus.MessageBus, store *ExecutionStore, logger *slog.Logger) *ExecutionProjector {
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

	// Own the reader goroutines instead of bus.SubscribeT so Stop can wait
	// for the final buffered Append. Reader semantics mirror SubscribeT:
	// range the channel until close (a closed channel delivers all
	// buffered items before the range exits), undecodable payloads are
	// logged and dropped. Every Append runs outside any lock, and each
	// wg.Add happens during wiring (before Stop can observe the Wait), so
	// Stop's Wait is race-free.
	//
	// Subscribe-by-literal-NAME at each call site (same posture as the WS
	// relay's topic slice in internal/comm/http/server.go): "bot.result"
	// and "employee.result" are the frozen names of
	// agent.TopicBotResult / agent.TopicEmployeeResult (issue #140: the
	// topic name is the sole tier discriminator). The decode target stays
	// the shared typed payload struct, so publisher/subscriber payload
	// agreement is still enforced by the struct; the connectivity graph
	// (make graphs) detects the raw .Subscribe literal and pairs the
	// names. bus.SubscribeT cannot be used here because the projector must
	// OWN the reader goroutine to make Stop deterministic (M5).
	proj := &ExecutionProjector{}
	proj.startReader(b.Subscribe("execution-projector-bot", "bot.result"), "bot", appendRun, logger)
	proj.startReader(b.Subscribe("execution-projector-employee", "employee.result"), "employee", appendRun, logger)
	return proj
}

// startReader registers a (possibly nil) subscriber and launches its
// reader goroutine: decode each message into the shared ExecutionResultEvent
// payload (undecodable payloads are logged and dropped, mirroring
// bus.SubscribeT) and append one durable row.
func (p *ExecutionProjector) startReader(sub *bus.Subscriber, tier string,
	appendRun func(string, agent.ExecutionResultEvent), logger *slog.Logger) {
	if sub == nil {
		return
	}
	p.mu.Lock()
	p.subs = append(p.subs, sub)
	p.mu.Unlock()
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for m := range sub.Channel {
			var ev agent.ExecutionResultEvent
			if err := json.Unmarshal(m.Payload, &ev); err != nil {
				logger.Warn("execution projector: dropping undecodable payload",
					"topic", sub.Topic, "error", err)
				continue
			}
			appendRun(tier, ev)
		}
	}()
}

// Stop unsubscribes every reader from the bus (closing each channel so the
// readers exit after draining their buffers) and waits for all reader
// goroutines to finish. Idempotent; nil-safe. Call this before
// ExecutionStore.Close so no Append runs against a closed handle.
func (p *ExecutionProjector) Stop(b *bus.MessageBus) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	subs := p.subs
	p.subs = nil
	p.mu.Unlock()

	// Unsubscribe takes the bus lock and closes each channel; no bus lock
	// is held across the Wait, so a reader mid-Append cannot deadlock it.
	if b != nil {
		for _, sub := range subs {
			if sub != nil {
				b.Unsubscribe(sub)
			}
		}
	}
	// Every wg.Add happened during wiring, so Wait returns only after the
	// last buffered Append has landed.
	p.wg.Wait()
}
