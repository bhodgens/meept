package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/caimlas/meept/internal/bot"
)

// botTriggerHandler is the daemon-side implementation of bot.BotTriggerHandler.
// It bridges a router bus-event trigger into the employee GoalLoop trigger path
// by calling employee.Manager.Trigger with the frozen TriggerEvent payload
// shape (source="bus_event", prompt=<event prompt>).
//
// The manager is resolved lazily from Components at trigger time because the
// router is constructed (components.go:5349) BEFORE the employee.Manager
// (components.go:5391); holding a stable *Components pointer avoids a rewrite
// of that construction order and keeps the wiring in one place.
type botTriggerHandler struct {
	components *Components
	logger     *slog.Logger
}

// Compile-time pin: the handler must satisfy the router's callback contract.
var _ bot.BotTriggerHandler = (*botTriggerHandler)(nil)

// newBotTriggerHandler builds the callback installed on the bot router. A nil
// components or logger is tolerated (the handler degrades to a logged error on
// trigger), but production wiring always supplies both.
func newBotTriggerHandler(c *Components, logger *slog.Logger) *botTriggerHandler {
	return &botTriggerHandler{components: c, logger: logger}
}

// HandleBotTrigger routes a bus-event trigger to the target bot's GoalLoop.
// The payload mirrors triggerViaGoalLoop's TriggerEvent shape
// (internal/employee/manager.go): Manager.Trigger reads the "source"
// key to set TriggerEvent.Source and marshals the whole map into
// TriggerEvent.Payload, so "bus_event" + "prompt" lands verbatim on the loop.
// The error is returned unwrapped so the router logs it; it is also logged
// here so the failure is visible even if a caller swallows the return.
func (h *botTriggerHandler) HandleBotTrigger(ctx context.Context, botID, prompt string) error {
	if h == nil || h.components == nil || h.components.EmployeeManager == nil {
		err := fmt.Errorf("bot trigger %q: employee manager not configured", botID)
		if h != nil && h.logger != nil {
			h.logger.Error("bot trigger handler: employee manager not configured", "bot_id", botID)
		}
		return err
	}

	payload := map[string]any{
		"source":      "bus_event",
		"prompt":      prompt,
		"trigger_ref": busEventTriggerRef(prompt),
	}

	// A bus event calls a bot to run one task; it is not an employee looking
	// for work. The result tier is set PER ROUND by Manager.triggerViaGoalLoop
	// from the source="bus_event" payload below (issue-140), so this handler
	// does not stamp sticky state on the shared GoalLoop: a later goal round
	// on the same entity must publish employee.result, not bot.result.
	if _, err := h.components.EmployeeManager.Trigger(ctx, botID, payload); err != nil {
		if h.logger != nil {
			h.logger.Error("bot trigger handler: GoalLoop trigger failed",
				"bot_id", botID, "error", err)
		}
		return err
	}
	return nil
}

// busEventTriggerRef is the history label for a bus round that has no
// active goal. The handler already has source bus_event; the default
// router prompt also carries the topic ("Event received on topic %s
// from %s"). A custom template has no topic, so the source alone is
// the label — never empty.
func busEventTriggerRef(prompt string) string {
	const prefix = "Event received on topic "
	if strings.HasPrefix(prompt, prefix) {
		rest := strings.TrimPrefix(prompt, prefix)
		if i := strings.Index(rest, " from "); i > 0 {
			topic := strings.TrimSpace(rest[:i])
			if topic != "" {
				return "bus_event:" + topic
			}
		}
	}
	return "bus_event"
}
