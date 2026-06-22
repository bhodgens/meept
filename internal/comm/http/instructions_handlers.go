package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/preferences"
	"github.com/caimlas/meept/internal/rpc"
)

// InstructionsHandler handles HTTP requests for user instructions.
type InstructionsHandler struct {
	store          *preferences.Store
	parser         *agent.InstructionParser
	verifier       *preferences.InstructionVerifier
	instructionRPC *rpc.InstructionHandler // RPC bridge for CRUD operations
}

// NewInstructionsHandler creates a new instructions handler.
func NewInstructionsHandler(
	store *preferences.Store,
	parser *agent.InstructionParser,
	verifier *preferences.InstructionVerifier,
	rpcHandler *rpc.InstructionHandler,
) *InstructionsHandler {
	return &InstructionsHandler{
		store:          store,
		parser:         parser,
		verifier:       verifier,
		instructionRPC: rpcHandler,
	}
}

// InstructionResponse is the HTTP response structure.
type InstructionResponse struct {
	Success              bool                         `json:"success"`
	Instruction          *preferences.UserInstruction `json:"instruction,omitempty"`
	Instructions         []*preferences.UserInstruction `json:"instructions,omitempty"`
	ParsedInstruction    *preferences.ParsedInstruction `json:"parsed,omitempty"`
	ConfirmationRequired bool                          `json:"confirmation_required"`
	Error                string                        `json:"error,omitempty"`
}

// RegisterRoutes wires up instruction routes on the ServeMux.
func (h *InstructionsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/instructions", h.handleList)
	mux.HandleFunc("POST /api/v1/instructions", h.handleCreate)
	mux.HandleFunc("GET /api/v1/instructions/", h.handleGetByID)
	mux.HandleFunc("PUT /api/v1/instructions/", h.handleUpdateByID)
	mux.HandleFunc("DELETE /api/v1/instructions/", h.handleDeleteByID)
	mux.HandleFunc("POST /api/v1/instructions/preview", h.handlePreview)
}

// handleList handles GET /api/v1/instructions
func (h *InstructionsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instructions := h.store.GetActive()

	resp := InstructionResponse{
		Success:      true,
		Instructions: instructions,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
	_ = ctx
}

// handleCreate handles POST /api/v1/instructions
func (h *InstructionsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Input string `json:"input"`
		Tier  string `json:"tier,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(InstructionResponse{
			Success: false,
			Error:   "invalid request body: " + err.Error(),
		})
		return
	}

	if req.Input == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(InstructionResponse{
			Success: false,
			Error:   "input is required",
		})
		return
	}

	// Parse the instruction
	parsed, err := h.parser.Parse(r.Context(), req.Input)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(InstructionResponse{
			Success: false,
			Error:   "parse error: " + err.Error(),
		})
		return
	}

	// Verify the instruction
	result := h.verifier.Verify(parsed)
	if !result.Valid {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(InstructionResponse{
			Success: false,
			Error:   "validation failed: " + strings.Join(result.Errors, "; "),
		})
		return
	}

	// Create the instruction
	instr := &preferences.UserInstruction{
		ID:         generateInstructionID(),
		Trigger:    parsed.Trigger.Type + ":" + parsed.Trigger.Pattern,
		Action:     parsed.Action.Tool,
		ActionArgs: parsed.Action.Args,
		Enabled:    true,
		Scope:      parsed.Scope,
		Priority:   parsed.Priority,
		CreatedAt:  time.Now(),
	}

	tier := req.Tier
	if tier == "" {
		tier = h.store.DefaultTier()
	}

	if err := h.store.Save(instr, tier); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(InstructionResponse{
			Success: false,
			Error:   "save error: " + err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(InstructionResponse{
		Success:              true,
		Instruction:          instr,
		ConfirmationRequired: result.ConfirmationNeeded,
	})
}

// handleGetByID handles GET /api/v1/instructions/:id
func (h *InstructionsHandler) handleGetByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/instructions/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(InstructionResponse{
			Success: false,
			Error:   "id is required",
		})
		return
	}

	instr := h.store.Get(id)
	if instr == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(InstructionResponse{
			Success: false,
			Error:   "instruction not found: " + id,
		})
		return
	}

	json.NewEncoder(w).Encode(InstructionResponse{
		Success:     true,
		Instruction: instr,
	})
}

// handleUpdateByID handles PUT /api/v1/instructions/:id
func (h *InstructionsHandler) handleUpdateByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/instructions/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(InstructionResponse{
			Success: false,
			Error:   "id is required",
		})
		return
	}

	var req struct {
		Input   *string `json:"input,omitempty"`
		Enabled *bool   `json:"enabled,omitempty"`
		Tier    string  `json:"tier,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(InstructionResponse{
			Success: false,
			Error:   "invalid request body: " + err.Error(),
		})
		return
	}

	instr := h.store.Get(id)
	if instr == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(InstructionResponse{
			Success: false,
			Error:   "instruction not found: " + id,
		})
		return
	}

	// Update fields if provided
	if req.Enabled != nil {
		instr.Enabled = *req.Enabled
	}

	if req.Input != nil {
		// Re-parse if input changed
		parsed, err := h.parser.Parse(r.Context(), *req.Input)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(InstructionResponse{
				Success: false,
				Error:   "parse error: " + err.Error(),
			})
			return
		}

		result := h.verifier.Verify(parsed)
		if !result.Valid {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(InstructionResponse{
				Success: false,
				Error:   "validation failed: " + strings.Join(result.Errors, ", "),
			})
			return
		}

		instr.Trigger = parsed.Trigger.Type + ":" + parsed.Trigger.Pattern
		instr.Action = parsed.Action.Tool
		instr.ActionArgs = parsed.Action.Args
	}

	tier := req.Tier
	if tier == "" {
		tier = h.store.DefaultTier()
	}

	if err := h.store.Save(instr, tier); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(InstructionResponse{
			Success: false,
			Error:   "save error: " + err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(InstructionResponse{
		Success:     true,
		Instruction: instr,
	})
}

// handleDeleteByID handles DELETE /api/v1/instructions/:id
func (h *InstructionsHandler) handleDeleteByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/instructions/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(InstructionResponse{
			Success: false,
			Error:   "id is required",
		})
		return
	}

	if err := h.store.Delete(id); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(InstructionResponse{
			Success: false,
			Error:   "delete error: " + err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(InstructionResponse{
		Success: true,
	})
}

// handlePreview handles POST /api/v1/instructions/preview
func (h *InstructionsHandler) handlePreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Input string `json:"input"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(InstructionResponse{
			Success: false,
			Error:   "invalid request body: " + err.Error(),
		})
		return
	}

	if req.Input == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(InstructionResponse{
			Success: false,
			Error:   "input is required",
		})
		return
	}

	parsed, err := h.parser.Parse(r.Context(), req.Input)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(InstructionResponse{
			Success: false,
			Error:   "parse error: " + err.Error(),
		})
		return
	}

	result := h.verifier.Verify(parsed)

	resp := InstructionResponse{
		Success:              true,
		ParsedInstruction:    parsed,
		ConfirmationRequired: result.ConfirmationNeeded,
	}

	if !result.Valid {
		resp.Success = false
		resp.Error = strings.Join(result.Errors, "; ")
		w.WriteHeader(http.StatusBadRequest)
	}

	json.NewEncoder(w).Encode(resp)
}

// generateInstructionID generates a unique ID for an instruction.
func generateInstructionID() string {
	return "instr_" + strconv.FormatInt(time.Now().UnixNano(), 10)
}
