// Package employee — api_handlers.go implements the HTTP handlers for the
// /api/v1/agents/* REST endpoints defined in the AI Employee Design spec
// (Phase 7, lines 504-525).
//
// Design: handlers dispatch through an injected RPCCallback (matching the
// internal/comm/http Server.rpcCall pattern). This keeps a single owner of
// the employee.Manager (the RPC layer) while letting HTTP clients reach the
// same code path. See the MCP-catalog implementation in
// internal/comm/http/api_handlers.go for the pattern this mirrors.
//
// The handler is constructed by the HTTP server via the WithAgentHandlers
// option and registered on the server's mux.
package employee

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxAgentBodySize = 1 << 20 // 1 MB

// RPCCallback dispatches a method/params pair to the RPC handler registry,
// matching the contract of internal/comm/http.Server.rpcCall. Using a function
// type avoids importing internal/rpc (which would create an import cycle when
// internal/comm/http wires this handler).
type RPCCallback func(ctx context.Context, method string, params json.RawMessage) (any, error)

// AgentAPIHandler handles HTTP requests under /api/v1/agents/*. It dispatches
// every request through the injected RPCCallback to the agents.* RPC methods
// registered by employee.RPCHandler (Phase 6).
type AgentAPIHandler struct {
	rpc RPCCallback
}

// NewAgentAPIHandler creates a handler that dispatches to the agents.* RPC
// methods via cb. cb may be nil; routes return 503 in that case.
func NewAgentAPIHandler(cb RPCCallback) *AgentAPIHandler {
	return &AgentAPIHandler{
		rpc: cb,
	}
}

// SetRPCCallback wires the RPC dispatch callback. Nil is ignored (typed-nil
// guard per CLAUDE.md).
func (h *AgentAPIHandler) SetRPCCallback(cb RPCCallback) {
	if cb == nil {
		return
	}
	h.rpc = cb
}

// RegisterRoutes wires all /api/v1/agents/* routes onto the provided mux.
// Routes inherit the server's authentication middleware because they are
// registered through the same mux as every other /api/v1/* endpoint.
func (h *AgentAPIHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/agents", h.handleList)
	mux.HandleFunc("POST /api/v1/agents", h.handleCreate)
	mux.HandleFunc("POST /api/v1/agents/migrate", h.handleMigrate)

	mux.HandleFunc("GET /api/v1/agents/{id}", h.handleGet)
	mux.HandleFunc("PATCH /api/v1/agents/{id}", h.handleUpdate)
	mux.HandleFunc("DELETE /api/v1/agents/{id}", h.handleDelete)
	mux.HandleFunc("POST /api/v1/agents/{id}/trigger", h.handleTrigger)
	mux.HandleFunc("POST /api/v1/agents/{id}/pause", h.handlePause)
	mux.HandleFunc("POST /api/v1/agents/{id}/resume", h.handleResume)

	mux.HandleFunc("GET /api/v1/agents/{id}/constitution", h.handleConstitutionGet)
	mux.HandleFunc("PATCH /api/v1/agents/{id}/constitution", h.handleConstitutionAmend)

	mux.HandleFunc("GET /api/v1/agents/{id}/goals", h.handleGoalsList)
	mux.HandleFunc("GET /api/v1/agents/{id}/goals/{gid}", h.handleGoalsGet)
	mux.HandleFunc("POST /api/v1/agents/{id}/goals/{gid}/plans/{pid}/approve", h.handleGoalPlanApprove)
	mux.HandleFunc("POST /api/v1/agents/{id}/goals/{gid}/plans/{pid}/reject", h.handleGoalPlanReject)

	mux.HandleFunc("GET /api/v1/agents/{id}/audit", h.handleAuditList)
	mux.HandleFunc("POST /api/v1/agents/{id}/audit/{fid}/resolve", h.handleAuditResolve)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// writeJSON writes a JSON response with the standard content type.
func (h *AgentAPIHandler) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		// Best-effort log; we cannot recover the response at this point.
		_, _ = fmt.Fprintf(io.Discard, "agent api: encode response: %v", err)
	}
}

// writeError writes an error response in the canonical {"error":"..."} shape.
func (h *AgentAPIHandler) writeError(w http.ResponseWriter, status int, msg string) {
	h.writeJSON(w, status, map[string]string{"error": sanitizeAgentErr(msg)})
}

// sanitizeAgentErr strips filesystem paths and Go package/import paths from
// error messages before sending them to HTTP clients. Mirrors the server's
// internal sanitizer.
func sanitizeAgentErr(msg string) string {
	// Reuse the same regex set as internal/comm/http via the shared patterns
	// compiled here. Keep this lightweight to avoid importing the http package
	// (which would create a cycle when http wires this handler).
	msg = absPathReAgent.ReplaceAllString(msg, "<path>")
	msg = goImportPathReAgent.ReplaceAllString(msg, "<pkg>")
	msg = fileLineReAgent.ReplaceAllString(msg, "")
	if len(msg) > 1024 {
		msg = msg[:1024] + "...(truncated)"
	}
	return msg
}

// readJSON decodes a JSON body into v with a size cap. Returns true on
// success; on failure it writes a 400 response and returns false.
func (h *AgentAPIHandler) readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAgentBodySize)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

// dispatch sends method+params to the RPC registry and writes the result.
// Returns true if the caller should consider the response written (always
// true; dispatch handles both success and error responses).
func (h *AgentAPIHandler) dispatch(w http.ResponseWriter, r *http.Request, method string, params any) {
	if h.rpc == nil {
		h.writeError(w, http.StatusServiceUnavailable, "agent service not available")
		return
	}
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "failed to encode request")
			return
		}
		raw = b
	} else {
		raw = json.RawMessage("{}")
	}
	result, err := h.rpc(r.Context(), method, raw)
	if err != nil {
		// Map common sentinel errors to HTTP status codes.
		msg := err.Error()
		switch {
		case strings.Contains(msg, "not found"):
			h.writeError(w, http.StatusNotFound, msg)
		case strings.Contains(msg, "invalid") || strings.Contains(msg, "required"):
			h.writeError(w, http.StatusBadRequest, msg)
		case strings.Contains(msg, "not configured") || strings.Contains(msg, "not available"):
			h.writeError(w, http.StatusServiceUnavailable, msg)
		default:
			h.writeError(w, http.StatusInternalServerError, msg)
		}
		return
	}
	h.writeJSON(w, http.StatusOK, result)
}

// ---------------------------------------------------------------------------
// Lifecycle handlers (spec lines 510-512)
// ---------------------------------------------------------------------------

// handleList handles GET /api/v1/agents.
func (h *AgentAPIHandler) handleList(w http.ResponseWriter, r *http.Request) {
	// Optional ?status= filter.
	status := r.URL.Query().Get("status")
	params := map[string]string{}
	if status != "" {
		params["status"] = status
	}
	h.dispatch(w, r, "agents.list", params)
}

// handleCreate handles POST /api/v1/agents.
func (h *AgentAPIHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if !h.readJSON(w, r, &body) {
		return
	}
	h.dispatch(w, r, "agents.create", body)
}

// handleGet handles GET /api/v1/agents/{id}.
func (h *AgentAPIHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}
	h.dispatch(w, r, "agents.get", map[string]string{"id": id})
}

// handleUpdate handles PATCH /api/v1/agents/{id}.
func (h *AgentAPIHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}
	var body map[string]any
	if !h.readJSON(w, r, &body) {
		return
	}
	body["id"] = id
	h.dispatch(w, r, "agents.update", body)
}

// handleDelete handles DELETE /api/v1/agents/{id}.
func (h *AgentAPIHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}
	h.dispatch(w, r, "agents.delete", map[string]string{"id": id})
}

// ---------------------------------------------------------------------------
// Runtime control (spec lines 513-515)
// ---------------------------------------------------------------------------

// handleTrigger handles POST /api/v1/agents/{id}/trigger.
//
// This endpoint replaces the legacy POST /api/v1/bot/{botID}/trigger. The
// request body is passed through as the trigger payload; the RPC method
// agents.trigger constructs the trigger context and queues the invocation.
// Callers that previously posted to /api/v1/bot/{id}/trigger should update
// their URL to /api/v1/agents/{id}/trigger.
func (h *AgentAPIHandler) handleTrigger(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}
	var payload map[string]any
	// Body is optional; empty body is fine (trigger with no payload).
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAgentBodySize)).Decode(&payload)
	h.dispatch(w, r, "agents.trigger", map[string]any{
		"id":      id,
		"payload": payload,
	})
}

// handlePause handles POST /api/v1/agents/{id}/pause.
func (h *AgentAPIHandler) handlePause(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}
	h.dispatch(w, r, "agents.pause", map[string]string{"id": id})
}

// handleResume handles POST /api/v1/agents/{id}/resume.
func (h *AgentAPIHandler) handleResume(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}
	h.dispatch(w, r, "agents.resume", map[string]string{"id": id})
}

// ---------------------------------------------------------------------------
// Constitution (spec line 516)
// ---------------------------------------------------------------------------

// handleConstitutionGet handles GET /api/v1/agents/{id}/constitution.
// The agents.get response already includes the constitution; we dispatch to
// agents.get and the handler returns the full employee object. Clients
// interested in just the constitution can extract the "constitution" field.
func (h *AgentAPIHandler) handleConstitutionGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}
	h.dispatch(w, r, "agents.get", map[string]string{"id": id})
}

// handleConstitutionAmend handles PATCH /api/v1/agents/{id}/constitution.
// Body: {"fields": {...}, "reason": "..."}. Routes to Plan signoff via
// agents.amend.
func (h *AgentAPIHandler) handleConstitutionAmend(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}
	var body struct {
		Fields map[string]any `json:"fields"`
		Reason string         `json:"reason,omitempty"`
	}
	if !h.readJSON(w, r, &body) {
		return
	}
	if len(body.Fields) == 0 {
		h.writeError(w, http.StatusBadRequest, "at least one constitution field is required")
		return
	}
	h.dispatch(w, r, "agents.amend", map[string]any{
		"id":     id,
		"fields": body.Fields,
		"reason": body.Reason,
	})
}

// ---------------------------------------------------------------------------
// Goals (spec lines 517-520)
// ---------------------------------------------------------------------------

// handleGoalsList handles GET /api/v1/agents/{id}/goals.
func (h *AgentAPIHandler) handleGoalsList(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}
	h.dispatch(w, r, "agents.goals.list", map[string]string{"employee_id": id})
}

// handleGoalsGet handles GET /api/v1/agents/{id}/goals/{gid}.
func (h *AgentAPIHandler) handleGoalsGet(w http.ResponseWriter, r *http.Request) {
	gid := r.PathValue("gid")
	if gid == "" {
		h.writeError(w, http.StatusBadRequest, "goal id is required")
		return
	}
	h.dispatch(w, r, "agents.goals.get", map[string]string{"id": gid})
}

// handleGoalPlanApprove handles
// POST /api/v1/agents/{id}/goals/{gid}/plans/{pid}/approve.
func (h *AgentAPIHandler) handleGoalPlanApprove(w http.ResponseWriter, r *http.Request) {
	gid := r.PathValue("gid")
	pid := r.PathValue("pid")
	if gid == "" || pid == "" {
		h.writeError(w, http.StatusBadRequest, "goal id and plan id are required")
		return
	}
	var body struct {
		Reason string `json:"reason,omitempty"`
	}
	// Optional body.
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAgentBodySize)).Decode(&body)
	h.dispatch(w, r, "agents.goals.approve", map[string]string{
		"goal_id": gid,
		"plan_id": pid,
		"reason":  body.Reason,
	})
}

// handleGoalPlanReject handles
// POST /api/v1/agents/{id}/goals/{gid}/plans/{pid}/reject.
func (h *AgentAPIHandler) handleGoalPlanReject(w http.ResponseWriter, r *http.Request) {
	gid := r.PathValue("gid")
	pid := r.PathValue("pid")
	if gid == "" || pid == "" {
		h.writeError(w, http.StatusBadRequest, "goal id and plan id are required")
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if !h.readJSON(w, r, &body) {
		return
	}
	if body.Reason == "" {
		h.writeError(w, http.StatusBadRequest, "reason is required when rejecting a plan")
		return
	}
	h.dispatch(w, r, "agents.goals.reject", map[string]string{
		"goal_id": gid,
		"plan_id": pid,
		"reason":  body.Reason,
	})
}

// ---------------------------------------------------------------------------
// Audit (spec lines 521-522)
// ---------------------------------------------------------------------------

// handleAuditList handles GET /api/v1/agents/{id}/audit.
// Query params: ?since=24h&severity=critical
func (h *AgentAPIHandler) handleAuditList(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}
	params := map[string]string{"employee_id": id}
	if s := r.URL.Query().Get("since"); s != "" {
		params["since"] = s
	}
	if s := r.URL.Query().Get("severity"); s != "" {
		params["severity"] = s
	}
	h.dispatch(w, r, "agents.audit.list", params)
}

// handleAuditResolve handles POST /api/v1/agents/{id}/audit/{fid}/resolve.
// Body: {"resolution": "false_positive|acknowledged|constitution_amended", "note": "..."}
func (h *AgentAPIHandler) handleAuditResolve(w http.ResponseWriter, r *http.Request) {
	fid := r.PathValue("fid")
	if fid == "" {
		h.writeError(w, http.StatusBadRequest, "finding id is required")
		return
	}
	var body struct {
		Resolution string `json:"resolution"`
		Note       string `json:"note,omitempty"`
	}
	if !h.readJSON(w, r, &body) {
		return
	}
	if body.Resolution == "" {
		h.writeError(w, http.StatusBadRequest, "resolution is required")
		return
	}
	h.dispatch(w, r, "agents.audit.resolve", map[string]string{
		"finding_id":  fid,
		"resolution":  body.Resolution,
		"note":        body.Note,
	})
}

// ---------------------------------------------------------------------------
// Migration (spec line 523)
// ---------------------------------------------------------------------------

// handleMigrate handles POST /api/v1/agents/migrate.
// Body: {"apply": "<bot_id>"} to write a specific proposal, or {} for dry-run.
func (h *AgentAPIHandler) handleMigrate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Apply string `json:"apply,omitempty"`
	}
	// Optional body; empty body is fine (dry-run all).
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAgentBodySize)).Decode(&body)
	params := map[string]string{}
	if body.Apply != "" {
		params["apply"] = body.Apply
	}
	h.dispatch(w, r, "agents.migrate", params)
}
