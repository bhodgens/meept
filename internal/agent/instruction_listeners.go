package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/preferences"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
)

// InstructionListener listens for bus events and triggers matching instructions.
type InstructionListener struct {
	store        *preferences.Store
	bus          *bus.MessageBus
	toolExecutor *tools.Registry
	logger       *slog.Logger
	handler      *bus.SubscriptionHandler
}

// NewInstructionListener creates a new instruction listener.
func NewInstructionListener(
	store *preferences.Store,
	msgBus *bus.MessageBus,
	toolExec *tools.Registry,
	logger *slog.Logger,
) *InstructionListener {
	return &InstructionListener{
		store:        store,
		bus:          msgBus,
		toolExecutor: toolExec,
		logger:       logger,
	}
}

// Start begins listening for trigger events.
func (l *InstructionListener) Start(ctx context.Context) {
	handler := bus.NewSubscriptionHandler(l.bus, l.logger)

	handler.Subscribe("tool.completed", func(msg *models.BusMessage) {
		l.checkPostHookInstructions("tool_complete", msg)
	})
	handler.Subscribe("file.written", func(msg *models.BusMessage) {
		l.checkPostHookInstructions("write_file", msg)
	})
	handler.Subscribe("session.started", func(msg *models.BusMessage) {
		l.checkEventInstructions("session_start", msg)
	})

	handler.Start(ctx)
	l.logger.Info("instruction listener started")
}

// Stop gracefully shuts down the listener.
func (l *InstructionListener) Stop() {
	l.logger.Info("instruction listener stopped")
}

// checkPostHookInstructions checks for post_hook instructions matching the event.
func (l *InstructionListener) checkPostHookInstructions(eventType string, msg *models.BusMessage) {
	instructions := l.store.GetActive()

	for _, instr := range instructions {
		if !strings.HasPrefix(instr.Trigger, "post_hook:") {
			continue
		}

		trigger := strings.TrimPrefix(instr.Trigger, "post_hook:")
		parts := strings.SplitN(trigger, ":", 2)
		if len(parts) != 2 {
			continue
		}

		triggerTool := parts[0]
		pathPattern := parts[1]

		// Check tool match
		if triggerTool != eventType && triggerTool != "*" {
			continue
		}

		// Check path pattern if applicable
		if pathPattern != "*" && msg.Topic != "" {
			if !l.matchPattern(msg.Topic, pathPattern) {
				continue
			}
		}

		// Execute the action
		l.executeAction(instr)
	}
}

// checkEventInstructions checks for event-based instructions.
func (l *InstructionListener) checkEventInstructions(eventType string, msg *models.BusMessage) {
	instructions := l.store.GetActive()

	for _, instr := range instructions {
		if !strings.HasPrefix(instr.Trigger, "event:") {
			continue
		}

		triggerEvent := strings.TrimPrefix(instr.Trigger, "event:")
		if triggerEvent != eventType && triggerEvent != "*" {
			continue
		}

		l.executeAction(instr)
	}
}

// executeAction executes the action for an instruction.
func (l *InstructionListener) executeAction(instr *preferences.UserInstruction) {
	l.logger.Info("executing instruction action",
		"id", instr.ID,
		"action", instr.Action,
	)

	switch instr.Action {
	case "shell_execute":
		command, _ := instr.ActionArgs["command"].(string)
		l.logger.Debug("would execute shell", "command", command)
		// In full implementation, call toolExecutor
	case "agent_trigger":
		agentID, _ := instr.ActionArgs["agent_id"].(string)
		l.logger.Debug("would trigger agent", "agent_id", agentID)
	case "notification":
		l.logger.Debug("would send notification")
	case "memory_retain":
		l.logger.Debug("would store to memory")
	}
}

// matchPattern checks if a path matches a glob pattern.
func (l *InstructionListener) matchPattern(path, pattern string) bool {
	matched, err := filepath.Match(pattern, path)
	return err == nil && matched
}
