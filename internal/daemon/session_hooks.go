package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// wireSessionLifecycleHooks registers default session lifecycle hooks on the
// agent loop. Each hook publishes a bus event and can be extended by
// component-specific lifecycle handlers later.
func wireSessionLifecycleHooks(agentLoop *agent.AgentLoop, logger *slog.Logger, bus *bus.MessageBus) {
	if agentLoop == nil {
		return
	}
	hr := agentLoop.HookRegistry()
	if hr == nil {
		return
	}

	// SessionStartHook: publish session_start event and log duration boundary.
	startHook := &sessionStartBusHook{bus: bus, logger: logger.With("hook", "session-start")}
	hr.RegisterSessionStartHook("session_start_bus", agent.HookPriorityMonitor, startHook)

	// SessionEndHook: publish session_end event with outcome metadata.
	endHook := &sessionEndBusHook{bus: bus, logger: logger.With("hook", "session-end")}
	hr.RegisterSessionEndHook("session_end_bus", agent.HookPriorityMonitor, endHook)

	// Async session-end telemetry hook: fires-and-forgets via background goroutine
	// so that session teardown is never blocked by network I/O to the metrics
	// collector. Wrapped in a session end hook so it participates in the
	// lifecycle and appears in HookRegistry.Unregister.
	asyncTele := &asyncSessionEndHook{bus: bus, logger: logger.With("hook", "async-session-end")}
	hr.RegisterSessionEndHook("async_session_end", agent.HookPriorityMonitor+1, asyncTele)

	logger.Info("session lifecycle hooks wired")
}

// sessionStartBusHook implements agent.SessionStartHook by publishing a
// session_start bus event with session metadata.
type sessionStartBusHook struct {
	bus    *bus.MessageBus
	logger *slog.Logger
}

func (h *sessionStartBusHook) OnSessionStart(ctx context.Context, state agent.SessionLifecycleState) agent.ContextTransform {
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "session", agent.SessionLifecyclePayload{
		Event:        "start",
		SessionID:    state.SessionID,
		AgentID:      state.AgentID,
		StartTimeSec: float64(state.StartTime.Unix()),
		Metadata:     metadataToJSON(state.Metadata),
	})
	if err == nil && h.bus != nil {
		h.bus.Publish(bus.EventSessionStart, msg)
	}
	h.logger.Debug("session start hook fired", "session", state.SessionID, "agent", state.AgentID)
	return agent.ContextTransform{} // no modification
}

// sessionEndBusHook implements agent.SessionEndHook by publishing a
// session_end bus event with outcome metadata.
type sessionEndBusHook struct {
	bus    *bus.MessageBus
	logger *slog.Logger
}

func (h *sessionEndBusHook) OnSessionEnd(ctx context.Context, state agent.SessionLifecycleState, result agent.SessionLifecycleResult) error {
	duration := result.EndTime.Sub(state.StartTime)
	var msg *models.BusMessage
	if h.bus != nil {
		var err error
		msg, err = models.NewBusMessage(models.MessageTypeEvent, "session", agent.SessionLifecyclePayload{
			Event:        "end",
			SessionID:    state.SessionID,
			AgentID:      state.AgentID,
			EndTimeSec:   float64(result.EndTime.Unix()),
			DurationSec:  duration.Seconds(),
			Success:      result.Success,
			Metadata:     metadataToJSON(nil),
		})
		if err == nil {
			h.bus.Publish(bus.EventSessionEnd, msg)
		}
	}
	h.logger.Debug("session end hook fired",
		"session", state.SessionID,
		"success", result.Success,
		"duration_ms", duration.Milliseconds(),
	)
	return nil
}

// metadataToJSON serializes a map[string]any to a compact JSON string.
func metadataToJSON(m map[string]any) string {
	if m == nil {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// asyncSessionEndHook implements agent.SessionEndHook by running the session
// end event publication in a background goroutine. This ensures that session
// teardown latency is not affected by network I/O or message bus congestion.
// The hook returns immediately and only logs errors asynchronously.
//
//nolint:U1000 // methods used via SessionEndHook interface indirection
type asyncSessionEndHook struct {
	bus    *bus.MessageBus
	logger *slog.Logger
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

func (h *asyncSessionEndHook) OnSessionEnd(_ context.Context, state agent.SessionLifecycleState, result agent.SessionLifecycleResult) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.wg.Add(1)
	h.mu.Unlock()

	go func() {
		defer h.wg.Done()

		duration := result.EndTime.Sub(state.StartTime)
		var msg *models.BusMessage
		if h.bus != nil {
			var err error
			msg, err = models.NewBusMessage(models.MessageTypeEvent, "session", agent.SessionLifecyclePayload{
				Event:        "end_async",
				SessionID:    state.SessionID,
				AgentID:      state.AgentID,
				EndTimeSec:   float64(result.EndTime.Unix()),
				DurationSec:  duration.Seconds(),
				Success:      result.Success,
				Metadata:     metadataToJSON(nil),
			})
			if err == nil {
				h.bus.Publish(bus.EventSessionEnd, msg)
			}
		}
		h.logger.Debug("async session end hook fired",
			"session", state.SessionID,
			"success", result.Success,
			"duration_ms", duration.Milliseconds(),
		)
	}()
	return nil // fire-and-forget — never blocks the caller
}

// Close waits for all in-flight async hooks to complete.
func (h *asyncSessionEndHook) Close() {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	h.wg.Wait()
}
