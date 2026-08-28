// Package agent — loop_rewake.go implements the subscriber half of the
// hook.async_rewake contract. Publishers (HTTPHook, FileWatcherHook) emit
// a hook.async_rewake bus signal after a successful async hook; the
// AgentLoop that armed its consumer receives the signal and wakes itself:
// the payload is injected into the conversation as a system note at the
// top of the next reasoning iteration (reasoningCycle), so the model sees
// the completed-hook event and can react — a real in-process wake.
//
// Session scoping: a signal with a session_id is delivered only by the
// loop whose current conversation matches it. A signal with an empty
// session_id (e.g. FileWatcherHook, whose watcher is bound to the daemon
// loop rather than any one conversation) is treated as a broadcast and
// delivered by every loop with an armed consumer.
//
// Lifecycle: the consumer is armed lazily on the first turn (RunOnce path)
// via sync.Once, and torn down in Close(). When no bus is wired, nothing
// is armed and published signals remain observable to external
// subscribers only.
package agent

import (
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/bus"
)

// rewakeSubscriberID is the bus subscriber ID for the loop's
// hook.async_rewake subscription.
const rewakeSubscriberID = "agent-loop-async-rewake"

// rewakeChannelCapacity bounds the buffered rewake signals per loop.
// Signals arriving while the buffer is full are dropped with a warning:
// a rewake is advisory (the next hook event re-signals), so backpressure
// must never block the bus dispatch path.
const rewakeChannelCapacity = 8

// rewakeNotePrefix frames the injected system note. The square-bracket
// system-note form matches the loop's other synthetic nudges (empty
// response, reasoning watchdog) so the model treats it as a system event
// rather than user discourse.
const rewakeNotePrefix = "[system: async hook completed (async_rewake " +
	"received). A background integration finished successfully; take any " +
	"follow-up action its completion requires, or continue.]"

// RewakePayload mirrors the payload published by HTTPHook and
// FileWatcherHook under HookAsyncRewakeTopic.
type RewakePayload struct {
	SessionID string `json:"session_id"`
	HookType  string `json:"hook_type"`
	HookName  string `json:"hook_name"`
	Path      string `json:"path,omitempty"`
}

// parseRewakePayload decodes the raw payload of a hook.async_rewake
// message, wrapping the error with %w so publisher/subscriber drift
// surfaces in logs with its root cause.
func parseRewakePayload(raw json.RawMessage) (RewakePayload, error) {
	var p RewakePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return RewakePayload{}, fmt.Errorf("parse async_rewake payload: %w", err)
	}
	return p, nil
}

// rewakeNote renders the system-note content injected for one signal.
func rewakeNote(p RewakePayload) string {
	switch {
	case p.Path != "":
		return fmt.Sprintf("%s (hook: %s, path: %s)", rewakeNotePrefix, p.HookName, p.Path)
	case p.HookName != "":
		return fmt.Sprintf("%s (hook: %s)", rewakeNotePrefix, p.HookName)
	default:
		return rewakeNotePrefix
	}
}

// armRewakeConsumer subscribes the loop to HookAsyncRewakeTopic exactly
// once and starts the pump goroutine. Called at turn entry (RunOnce path
// and reasoningCycle); no-op without a bus or after Close.
func (l *AgentLoop) armRewakeConsumer() {
	if l.bus == nil || l.closed.Load() {
		return
	}
	l.rewakeOnce.Do(func() {
		ch := make(chan RewakePayload, rewakeChannelCapacity)
		sub := l.bus.Subscribe(rewakeSubscriberID, HookAsyncRewakeTopic)
		l.rewakeCh = ch
		l.rewakeSub = sub
		l.rewakeWG.Add(1)
		go l.rewakeConsumer(sub)
	})
}

// rewakeConsumer pumps signals from the bus into the loop's rewake
// channel until the subscription channel closes (Unsubscribe or bus
// Close). Malformed payloads are skipped; overflow is dropped with a
// warning — neither may kill the consumer.
func (l *AgentLoop) rewakeConsumer(sub *bus.Subscriber) {
	defer l.rewakeWG.Done()
	for msg := range sub.Channel {
		payload, err := parseRewakePayload(msg.Payload)
		if err != nil {
			l.logger.Warn("async rewake: skipping malformed signal",
				"error", err)
			continue
		}
		select {
		case l.rewakeCh <- payload:
		default:
			l.logger.Warn("async rewake: wake buffer full, dropping signal",
				"session_id", payload.SessionID,
				"hook_type", payload.HookType,
			)
		}
	}
}

// stopRewake tears down the rewake consumer. Idempotent; safe when never
// armed. Unsubscribe closes the subscription channel, which exits the
// consumer; we then wait for it before closing the signal channel so no
// send happens on a closed channel.
func (l *AgentLoop) stopRewake() {
	if l == nil {
		return
	}
	l.rewakeOnce.Do(func() {}) // consume the arm Once so no later arm starts
	l.rewakeStopOnce.Do(func() {
		sub := l.rewakeSub
		l.rewakeSub = nil
		if sub != nil && l.bus != nil {
			l.bus.Unsubscribe(sub)
		}
		l.rewakeWG.Wait()
		if l.rewakeCh != nil {
			close(l.rewakeCh)
			// Deliberately do NOT nil l.rewakeCh: drainRewakes reads the
			// field from the loop goroutine — a close-then-nil write would
			// race that reader, and a nil channel would make its select
			// block forever. rewakeStopOnce makes re-close impossible.
		}
	})
}

// drainRewakes returns buffered rewake signals addressed to
// conversationID (empty session_id = broadcast). Signals for other
// sessions are dropped. Non-blocking; safe when the consumer was never
// armed (nil channel).
func (l *AgentLoop) drainRewakes(conversationID string) []RewakePayload {
	if l.rewakeCh == nil {
		return nil
	}
	var out []RewakePayload
	for {
		select {
		case p, ok := <-l.rewakeCh:
			if !ok {
				// Channel closed by stopRewake mid-drain (Close during an
				// in-flight turn); stop consuming.
				return out
			}
			if p.SessionID != "" && p.SessionID != conversationID {
				l.logger.Debug("async rewake for another session; dropped",
					"signal_session", p.SessionID,
					"conversation", conversationID,
				)
				continue
			}
			out = append(out, p)
		default:
			return out
		}
	}
}

// injectRewakes drains rewake signals for this conversation and injects
// each as a system note so the model can react on this iteration. Called
// at the top of every reasoningCycle iteration, before the steering
// check.
func (l *AgentLoop) injectRewakes(conv *Conversation, conversationID string, iteration int) {
	signals := l.drainRewakes(conversationID)
	for _, p := range signals {
		conv.AddUserMessage(rewakeNote(p))
		l.logger.Info("async rewake injected",
			"conversation", conversationID,
			"iteration", iteration,
			"hook_type", p.HookType,
			"hook_name", p.HookName,
		)
	}
}
