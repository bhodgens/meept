package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/preferences"
	idpkg "github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// InstructionHandler handles instruction-related RPC requests.
type InstructionHandler struct {
	store    *preferences.Store
	parser   *agent.InstructionParser
	verifier *preferences.InstructionVerifier
	bus      *bus.MessageBus
	logger   *slog.Logger
	cancel   context.CancelFunc
}

// NewInstructionHandler creates a new instruction handler.
func NewInstructionHandler(
	store *preferences.Store,
	parser *agent.InstructionParser,
	verifier *preferences.InstructionVerifier,
	msgBus *bus.MessageBus,
	logger *slog.Logger,
) *InstructionHandler {
	return &InstructionHandler{
		store:    store,
		parser:   parser,
		verifier: verifier,
		bus:      msgBus,
		logger:   logger,
	}
}

// Start begins listening for instruction requests.
func (h *InstructionHandler) Start(ctx context.Context) error {
	ctx, h.cancel = context.WithCancel(ctx)

	addSub := h.bus.Subscribe("instruction-handler", "instruction.add")
	listSub := h.bus.Subscribe("instruction-handler", "instruction.list")
	delSub := h.bus.Subscribe("instruction-handler", "instruction.delete")
	execSub := h.bus.Subscribe("instruction-handler", "instruction.execute")
	previewSub := h.bus.Subscribe("instruction-handler", "instruction.preview")

	go func() {
		for {
			select {
			case <-ctx.Done():
				h.bus.Unsubscribe(addSub)
				h.bus.Unsubscribe(listSub)
				h.bus.Unsubscribe(delSub)
				h.bus.Unsubscribe(execSub)
				h.bus.Unsubscribe(previewSub)
				return
			case msg, ok := <-addSub.Channel:
				if !ok {
					return
				}
				h.handleAdd(ctx, msg)
			case msg, ok := <-listSub.Channel:
				if !ok {
					return
				}
				h.handleList(ctx, msg)
			case msg, ok := <-delSub.Channel:
				if !ok {
					return
				}
				h.handleDelete(ctx, msg)
			case msg, ok := <-execSub.Channel:
				if !ok {
					return
				}
				h.handleExecute(ctx, msg)
			case msg, ok := <-previewSub.Channel:
				if !ok {
					return
				}
				h.handlePreview(ctx, msg)
			}
		}
	}()

	h.logger.Info("instruction RPC handler started")
	return nil
}

// Stop gracefully shuts down the handler.
func (h *InstructionHandler) Stop() {
	if h.cancel != nil {
		h.cancel()
	}
}

type InstructionResponse struct {
	Success              bool                        `json:"success"`
	Instruction          *preferences.UserInstruction `json:"instruction,omitempty"`
	Instructions         []*preferences.UserInstruction `json:"instructions,omitempty"`
	ParsedInstruction    *preferences.ParsedInstruction `json:"parsed,omitempty"`
	ConfirmationRequired bool                         `json:"confirmation_required"`
	Error                string                       `json:"error,omitempty"`
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

	insID, _ := req["id"].(string)
	instr := h.store.Get(insID)
	if instr == nil {
		h.sendError(msg.ReplyTo, "instruction not found: "+insID)
		return
	}

	// Publish execution event
	payload, _ := json.Marshal(map[string]any{
		"instruction_id": instr.ID,
		"action":         instr.Action,
		"args":           instr.ActionArgs,
	})
	h.bus.Publish("instruction.executing", &models.BusMessage{
		ID:        idpkg.Generate("exec-"),
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
		ID:        idpkg.Generate("resp-"),
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

// RegisterInstructionMethods registers instruction RPC methods on the server.
func (h *InstructionHandler) RegisterInstructionMethods(server *Server) {
	server.RegisterHandler("instruction.list", h.handleListRPC)
	server.RegisterHandler("instruction.add", h.handleAddRPC)
	server.RegisterHandler("instruction.delete", h.handleDeleteRPC)
	server.RegisterHandler("instruction.preview", h.handlePreviewRPC)
}

// handleListRPC handles instruction.list RPC calls directly.
func (h *InstructionHandler) handleListRPC(ctx context.Context, params json.RawMessage) (any, error) {
	instructions := h.store.GetActive()
	return InstructionResponse{
		Success:      true,
		Instructions: instructions,
	}, nil
}

// handleAddRPC handles instruction.add RPC calls directly.
func (h *InstructionHandler) handleAddRPC(ctx context.Context, raw json.RawMessage) (any, error) {
	var params struct {
		Input string `json:"input"`
		Tier  string `json:"tier"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return InstructionResponse{Success: false, Error: "invalid params: " + err.Error()}, nil
	}

	parsed, err := h.parser.Parse(ctx, params.Input)
	if err != nil {
		return InstructionResponse{Success: false, Error: "parse error: " + err.Error()}, nil
	}

	result := h.verifier.Verify(parsed)
	if !result.Valid {
		return InstructionResponse{Success: false, Error: fmt.Sprint(result.Errors)}, nil
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

	if params.Tier == "" {
		params.Tier = h.store.DefaultTier()
	}

	if err := h.store.Save(instr, params.Tier); err != nil {
		return InstructionResponse{Success: false, Error: "save error: " + err.Error()}, nil
	}

	return InstructionResponse{
		Success:              true,
		Instruction:          instr,
		ConfirmationRequired: result.ConfirmationNeeded,
	}, nil
}

// handleDeleteRPC handles instruction.delete RPC calls.
func (h *InstructionHandler) handleDeleteRPC(ctx context.Context, raw json.RawMessage) (any, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return InstructionResponse{Success: false, Error: "invalid params"}, nil
	}

	if err := h.store.Delete(params.ID); err != nil {
		return InstructionResponse{Success: false, Error: "delete error: " + err.Error()}, nil
	}

	return InstructionResponse{Success: true}, nil
}

// handlePreviewRPC handles instruction.preview RPC calls.
func (h *InstructionHandler) handlePreviewRPC(ctx context.Context, raw json.RawMessage) (any, error) {
	var params struct {
		Input string `json:"input"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return InstructionResponse{Success: false, Error: "invalid params"}, nil
	}

	parsed, err := h.parser.Parse(ctx, params.Input)
	if err != nil {
		return InstructionResponse{Success: false, Error: "parse error: " + err.Error()}, nil
	}

	result := h.verifier.Verify(parsed)
	return InstructionResponse{
		Success:              true,
		ParsedInstruction:    parsed,
		ConfirmationRequired: result.ConfirmationNeeded,
	}, nil
}
