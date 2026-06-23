package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/preferences"
	"github.com/caimlas/meept/pkg/models"
)

// InstructionHandler manages bus subscriptions and message handling for user instructions.
type InstructionHandler struct {
	store    *preferences.Store
	bus      *bus.MessageBus
	parser   *InstructionParser
	logger   *slog.Logger
	verifier *preferences.InstructionVerifier
	handler  *bus.SubscriptionHandler
	mu       sync.RWMutex //nolint:unused -- reserved for future concurrent instruction handling
}

// InstructionResponse is the standard response format for instruction operations.
type InstructionResponse struct {
	Success              bool                        `json:"success"`
	Instruction          *preferences.UserInstruction `json:"instruction,omitempty"`
	Instructions         []*preferences.UserInstruction `json:"instructions,omitempty"`
	ParsedInstruction    *preferences.ParsedInstruction `json:"parsed,omitempty"`
	ConfirmationRequired bool                         `json:"confirmation_required"`
	Error                string                       `json:"error,omitempty"`
}

// NewInstructionHandler creates a new handler with the given dependencies.
func NewInstructionHandler(
	store *preferences.Store,
	msgBus *bus.MessageBus,
	parser *InstructionParser,
	verifier *preferences.InstructionVerifier,
	logger *slog.Logger,
) *InstructionHandler {
	handler := bus.NewSubscriptionHandler(msgBus, logger)
	return &InstructionHandler{
		store:    store,
		bus:      msgBus,
		parser:   parser,
		logger:   logger,
		verifier: verifier,
		handler:  handler,
	}
}

// Start subscribes to all instruction bus topics and begins handling messages.
func (h *InstructionHandler) Start(ctx context.Context) {
	h.handler.Subscribe("instruction.add", func(ctx context.Context, topic string, msg any) {
		if bm, ok := msg.(*models.BusMessage); ok {
			h.handleAdd(ctx, bm)
		}
	})
	h.handler.Subscribe("instruction.list", func(ctx context.Context, topic string, msg any) {
		if bm, ok := msg.(*models.BusMessage); ok {
			h.handleList(ctx, bm)
		}
	})
	h.handler.Subscribe("instruction.delete", func(ctx context.Context, topic string, msg any) {
		if bm, ok := msg.(*models.BusMessage); ok {
			h.handleDelete(ctx, bm)
		}
	})
	h.handler.Subscribe("instruction.execute", func(ctx context.Context, topic string, msg any) {
		if bm, ok := msg.(*models.BusMessage); ok {
			h.handleExecute(ctx, bm)
		}
	})
	h.handler.Subscribe("instruction.preview", func(ctx context.Context, topic string, msg any) {
		if bm, ok := msg.(*models.BusMessage); ok {
			h.handlePreview(ctx, bm)
		}
	})

	h.handler.Start(ctx)
	h.logger.Info("instruction handler started")
}

// Stop gracefully shuts down the handler.
func (h *InstructionHandler) Stop() {
	h.handler.Stop()
}

func (h *InstructionHandler) handleAdd(ctx context.Context, msg *models.BusMessage) {
	var req map[string]any
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		h.sendError(msg.ReplyTo, "invalid payload: "+err.Error())
		return
	}

	input, _ := req["input"].(string)
	tier, _ := req["tier"].(string)

	parsed, err := h.parser.Parse(ctx, input)
	if err != nil {
		h.sendError(msg.ReplyTo, "parse error: "+err.Error())
		return
	}

	result := h.verifier.Verify(parsed)
	if !result.Valid {
		h.sendError(msg.ReplyTo, "validation failed: "+fmt.Sprint(result.Errors))
		return
	}

	instr := &preferences.UserInstruction{
		ID:         fmt.Sprintf("instr_%d", time.Now().UnixNano()),
		Trigger:    parsed.Trigger.Type + ":" + parsed.Trigger.Pattern,
		Action:     parsed.Action.Tool,
		ActionArgs: parsed.Action.Args,
		Enabled:    true,
		Scope:      parsed.Scope,
		Priority:   parsed.Priority,
	}

	if tier == "" {
		tier = h.store.DefaultTier()
	}

	if err := h.store.Save(instr, tier); err != nil {
		h.sendError(msg.ReplyTo, "save error: "+err.Error())
		return
	}

	h.sendResponse(msg.ReplyTo, InstructionResponse{
		Success:              true,
		Instruction:          instr,
		ConfirmationRequired: result.ConfirmationNeeded,
	})
}

func (h *InstructionHandler) handleList(ctx context.Context, msg *models.BusMessage) {
	instructions := h.store.GetActive()
	h.sendResponse(msg.ReplyTo, InstructionResponse{
		Success:      true,
		Instructions: instructions,
	})
}

func (h *InstructionHandler) handleDelete(ctx context.Context, msg *models.BusMessage) {
	var req map[string]any
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		h.sendError(msg.ReplyTo, "invalid payload: "+err.Error())
		return
	}

	id, _ := req["id"].(string)
	if err := h.store.Delete(id); err != nil {
		h.sendError(msg.ReplyTo, "delete error: "+err.Error())
		return
	}

	h.sendResponse(msg.ReplyTo, InstructionResponse{Success: true})
}

func (h *InstructionHandler) handleExecute(ctx context.Context, msg *models.BusMessage) {
	var req map[string]any
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		h.sendError(msg.ReplyTo, "invalid payload: "+err.Error())
		return
	}

	id, _ := req["id"].(string)
	instr := h.store.Get(id)
	if instr == nil {
		h.sendError(msg.ReplyTo, "instruction not found: "+id)
		return
	}

	payload, _ := json.Marshal(map[string]any{
		"instruction_id": instr.ID,
		"action":         instr.Action,
		"args":           instr.ActionArgs,
	})
	h.bus.Publish("instruction.executing", &models.BusMessage{
		ID:        fmt.Sprintf("exec_%d", time.Now().UnixNano()),
		Topic:     "instruction.executing",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	})

	h.sendResponse(msg.ReplyTo, InstructionResponse{
		Success:     true,
		Instruction: instr,
	})
}

func (h *InstructionHandler) handlePreview(ctx context.Context, msg *models.BusMessage) {
	var req map[string]any
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		h.sendError(msg.ReplyTo, "invalid payload: "+err.Error())
		return
	}

	input, _ := req["input"].(string)
	parsed, err := h.parser.Parse(ctx, input)
	if err != nil {
		h.sendError(msg.ReplyTo, "parse error: "+err.Error())
		return
	}

	result := h.verifier.Verify(parsed)
	h.sendResponse(msg.ReplyTo, InstructionResponse{
		Success:              true,
		ParsedInstruction:    parsed,
		ConfirmationRequired: result.ConfirmationNeeded,
	})
}

func (h *InstructionHandler) sendResponse(replyTo string, resp InstructionResponse) {
	payload, _ := json.Marshal(resp)
	h.bus.Publish(replyTo, &models.BusMessage{
		ID:        fmt.Sprintf("resp_%d", time.Now().UnixNano()),
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	})
}

func (h *InstructionHandler) sendError(replyTo string, errMsg string) {
	h.sendResponse(replyTo, InstructionResponse{
		Success: false,
		Error:   errMsg,
	})
}
