package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// tierDescriptions maps each effort tier to a short human-readable label.
// These are returned by reasoning.list_tiers so UI clients can render
// dropdowns without hardcoding the descriptions.
var tierDescriptions = map[string]string{
	"none":   "disabled",
	"low":    "light thinking",
	"medium": "balanced",
	"high":   "deeper thinking",
	"xhigh":  "extensive thinking",
	"max":    "maximum thinking budget",
}

// orderedTiers is the canonical display order for tiers.
var orderedTiers = []string{"none", "low", "medium", "high", "xhigh", "max"}

// ReasoningHandler provides RPC methods for reasoning configuration.
// It reads/writes the daemon's loaded *config.Config for global budgets and
// the agent registry for per-agent reasoning settings. Persistence uses the
// atomic-write pattern (write .tmp then rename).
type ReasoningHandler struct {
	registry    *agent.AgentRegistry
	cfg         *config.Config
	cfgPath     string // path to meept.json5 (for budgets persistence)
	modelsCfg   *config.ModelsConfig
	modelsPath  string // path to models.json5 (for model default persistence)
}

// NewReasoningHandler creates a new handler.
//
// Any of registry, cfg, modelsCfg may be nil; the corresponding RPC methods
// return a "not available" error rather than panicking. This lets the daemon
// register the handler unconditionally and let individual methods degrade
// gracefully when their backing store isn't wired.
func NewReasoningHandler(
	registry *agent.AgentRegistry,
	cfg *config.Config,
	cfgPath string,
	modelsCfg *config.ModelsConfig,
	modelsPath string,
) *ReasoningHandler {
	return &ReasoningHandler{
		registry:   registry,
		cfg:        cfg,
		cfgPath:    cfgPath,
		modelsCfg:  modelsCfg,
		modelsPath: modelsPath,
	}
}

// RegisterReasoningMethods registers reasoning.* RPC methods on the server.
// Call this once during daemon startup after the registry and config are
// populated.
func (h *ReasoningHandler) RegisterReasoningMethods(server *Server) {
	server.RegisterHandler("reasoning.list_tiers", h.handleListTiers)
	server.RegisterHandler("reasoning.get", h.handleGet)
	server.RegisterHandler("reasoning.set", h.handleSet)
	server.RegisterHandler("reasoning.get_budgets", h.handleGetBudgets)
	server.RegisterHandler("reasoning.set_budgets", h.handleSetBudgets)
	server.RegisterHandler("reasoning.get_model_default", h.handleGetModelDefault)
	server.RegisterHandler("reasoning.set_model_default", h.handleSetModelDefault)
	server.RegisterHandler("reasoning.session_set", h.handleSessionSet)
	server.RegisterHandler("reasoning.session_clear", h.handleSessionClear)
	server.RegisterHandler("reasoning.list_agents", h.handleListAgents)
}

// handleListTiers returns the list of recognized effort tiers with their
// descriptions and default token budgets.
//
// Params: {} (ignored).
// Result: {"tiers": [{name, description, default_budget}]}.
func (h *ReasoningHandler) handleListTiers(_ context.Context, _ json.RawMessage) (any, error) {
	defaults := llm.DefaultBudgetTable()
	tiers := make([]map[string]any, 0, len(orderedTiers))
	for _, name := range orderedTiers {
		tiers = append(tiers, map[string]any{
			"name":           name,
			"description":    tierDescriptions[name],
			"default_budget": defaults[name],
		})
	}
	return map[string]any{"tiers": tiers}, nil
}

// reasoningGetResult is the response shape for reasoning.get.
type reasoningGetResult struct {
	AgentID            string                  `json:"agent_id"`
	HasReasoning       bool                    `json:"has_reasoning"`
	Config             *llm.AgentReasoningConfig `json:"config,omitempty"`
	EffectiveEffort    string                  `json:"effective_effort"`
}

// handleGet returns the per-agent reasoning config for a single agent.
//
// Params: {"agent_id": string}. When agent_id is empty, returns the config
// for the dispatcher agent.
// Result: reasoningGetResult.
func (h *ReasoningHandler) handleGet(_ context.Context, params json.RawMessage) (any, error) {
	if h.registry == nil {
		return nil, fmt.Errorf("agent registry not available")
	}
	var req struct {
		AgentID string `json:"agent_id"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
	}
	agentID := req.AgentID
	if agentID == "" {
		agentID = config.AgentIDDispatcher
	}

	spec, ok := h.registry.GetSpec(agentID)
	if !ok {
		return nil, fmt.Errorf("agent not found: %s", agentID)
	}

	result := reasoningGetResult{
		AgentID: agentID,
	}
	if spec.Constraints.Reasoning != nil {
		result.HasReasoning = true
		result.Config = spec.Constraints.Reasoning
		result.EffectiveEffort = spec.Constraints.Reasoning.Effort
	}

	// If a loop is already instantiated, surface its mutable currentEffort
	// as the effective tier — this reflects any self-modulation that has
	// occurred since startup.
	if loop, err := h.registry.Get(agentID); err == nil && loop != nil {
		if effort := loop.CurrentReasoningEffort(); effort != "" {
			result.EffectiveEffort = effort
		}
	}
	return result, nil
}

// handleSet updates an agent's reasoning config in-memory and (when the
// agent is instantiated) propagates it to the running loop.
//
// Params: {
//   "agent_id": string,
//   "effort": string (optional),
//   "allow_self_modulation": bool (optional),
//   "min_effort": string (optional),
//   "max_effort": string (optional),
//   "budget_tokens": int (optional),
//   "force": bool (optional)
// }
//
// Persistence to AGENT.md is handled by the existing config_service SaveAgent
// path. This RPC only updates the in-memory spec; callers wanting disk
// persistence should follow up with the HTTP /api/v1/config/agents/{id} POST
// endpoint.
//
// Result: {"ok": true, "effective_effort": string}.
func (h *ReasoningHandler) handleSet(_ context.Context, params json.RawMessage) (any, error) {
	if h.registry == nil {
		return nil, fmt.Errorf("agent registry not available")
	}
	var req struct {
		AgentID            string `json:"agent_id"`
		Effort             string `json:"effort"`
		AllowSelfModulation *bool `json:"allow_self_modulation,omitempty"`
		MinEffort          string `json:"min_effort"`
		MaxEffort          string `json:"max_effort"`
		BudgetTokens       *int   `json:"budget_tokens,omitempty"`
		Force              *bool  `json:"force,omitempty"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.AgentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	// Validate effort tier before mutating state.
	if req.Effort != "" && !llm.IsValidEffort(req.Effort) {
		return nil, fmt.Errorf("invalid effort tier: %q", req.Effort)
	}
	if req.MinEffort != "" && !llm.IsValidEffort(req.MinEffort) {
		return nil, fmt.Errorf("invalid min_effort: %q", req.MinEffort)
	}
	if req.MaxEffort != "" && !llm.IsValidEffort(req.MaxEffort) {
		return nil, fmt.Errorf("invalid max_effort: %q", req.MaxEffort)
	}

	spec, ok := h.registry.GetSpec(req.AgentID)
	if !ok {
		return nil, fmt.Errorf("agent not found: %s", req.AgentID)
	}

	// Build or update the reasoning config on the spec. We snapshot the
	// spec pointer under the registry's lock by mutating in place —
	// AgentRegistry.GetSpec returns the same pointer that ListSpecs
	// iterates, so this is consistent with how other callers (e.g. CLI
	// config set) mutate specs.
	rc := spec.Constraints.Reasoning
	if rc == nil {
		rc = &llm.AgentReasoningConfig{}
		spec.Constraints.Reasoning = rc
	}
	if req.Effort != "" {
		rc.Effort = req.Effort
	}
	if req.AllowSelfModulation != nil {
		rc.AllowSelfModulation = *req.AllowSelfModulation
	}
	if req.MinEffort != "" {
		rc.MinEffort = req.MinEffort
	}
	if req.MaxEffort != "" {
		rc.MaxEffort = req.MaxEffort
	}
	if req.BudgetTokens != nil {
		v := *req.BudgetTokens
		rc.BudgetTokens = &v
	}
	if req.Force != nil {
		rc.Force = *req.Force
	}

	// Propagate to the running loop if one exists. SetReasoningOverride is
	// not appropriate here — this is a config change, not a per-request
	// override. Instead we reconstruct via WithAgentReasoning on the next
	// loop creation. For an already-instantiated loop, the
	// agentReasoning field is immutable; the runtime tier can still shift
	// via SetReasoningForNextTurn, but the bounds cannot. This matches the
	// spec §4.4 design: loop restart picks up new bounds.

	return map[string]any{
		"ok":               true,
		"effective_effort": rc.Effort,
	}, nil
}

// handleGetBudgets returns the current global tier→budget mapping.
//
// Params: {} (ignored).
// Result: {"budgets": {low, medium, high, xhigh, max}, "source": "config"|"default"}.
func (h *ReasoningHandler) handleGetBudgets(_ context.Context, _ json.RawMessage) (any, error) {
	if h.cfg != nil && len(h.cfg.Reasoning.Budgets) > 0 {
		return map[string]any{
			"budgets": h.cfg.Reasoning.Budgets,
			"source":  "config",
		}, nil
	}
	return map[string]any{
		"budgets": llm.DefaultBudgetTable(),
		"source":  "default",
	}, nil
}

// handleSetBudgets updates the global tier→budget mapping and persists it
// to the meept.json5 config file.
//
// Params: {"low": int, "medium": int, "high": int, "xhigh": int, "max": int}.
// At least one tier must be present. Zero values clear that tier from the
// map (falls through to default).
//
// Result: {"ok": true, "budgets": <updated map>}.
func (h *ReasoningHandler) handleSetBudgets(_ context.Context, params json.RawMessage) (any, error) {
	if h.cfg == nil {
		return nil, fmt.Errorf("config not available")
	}
	if h.cfgPath == "" {
		return nil, fmt.Errorf("config path not configured")
	}
	var req map[string]int
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if len(req) == 0 {
		return nil, fmt.Errorf("at least one tier budget is required")
	}
	// Validate keys.
	for k := range req {
		if !llm.IsValidEffort(k) || k == "none" || k == "" {
			return nil, fmt.Errorf("invalid tier %q (expected low/medium/high/xhigh/max)", k)
		}
	}

	// Mutate under config pointer. The ReasoningGlobalConfig.Budgets map
	// may be nil if it was never initialized.
	if h.cfg.Reasoning.Budgets == nil {
		h.cfg.Reasoning.Budgets = make(map[string]int)
	}
	for tier, budget := range req {
		if budget > 0 {
			h.cfg.Reasoning.Budgets[tier] = budget
		} else {
			delete(h.cfg.Reasoning.Budgets, tier)
		}
	}

	if err := saveConfigAtomic(h.cfgPath, h.cfg); err != nil {
		return nil, fmt.Errorf("failed to persist config: %w", err)
	}

	return map[string]any{
		"ok":      true,
		"budgets": h.cfg.Reasoning.Budgets,
	}, nil
}

// handleGetModelDefault returns the default reasoning config for a model.
//
// Params: {"model_id": string}. model_id is the "provider/model" ref used
// in models.json5.
// Result: {"model_id": string, "default_reasoning": {effort, budget_tokens}|null}.
func (h *ReasoningHandler) handleGetModelDefault(_ context.Context, params json.RawMessage) (any, error) {
	if h.modelsCfg == nil {
		return nil, fmt.Errorf("models config not available")
	}
	var req struct {
		ModelID string `json:"model_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.ModelID == "" {
		return nil, fmt.Errorf("model_id is required")
	}

	// Split "provider/model-id" and look up the model entry.
	providerID, modelID := splitModelRef(req.ModelID)
	provider, ok := h.modelsCfg.Providers[providerID]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", providerID)
	}
	if _, ok := provider.Models[modelID]; !ok {
		return nil, fmt.Errorf("model not found: %s", req.ModelID)
	}

	// config.Model does not carry DefaultReasoning today (the runtime
	// llm.ModelConfig does). Return null until the schema migration is
	// complete.
	return map[string]any{
		"model_id":           req.ModelID,
		"default_reasoning": nil,
	}, nil
}

// handleSetModelDefault updates the default reasoning config for a model.
//
// Params: {"model_id": string, "effort": string, "budget_tokens": int (optional)}.
// effort="none" or empty clears the default.
//
// Result: {"ok": true}.
//
// Note: config.Model does not yet carry a DefaultReasoning field (the
// runtime llm.ModelConfig does). Until the on-disk schema migration in
// spec §3.3 is complete, this method returns a not-implemented error.
// Callers should use the agent-level reasoning.set RPC to configure
// per-agent defaults today.
func (h *ReasoningHandler) handleSetModelDefault(_ context.Context, params json.RawMessage) (any, error) {
	if h.modelsCfg == nil {
		return nil, fmt.Errorf("models config not available")
	}
	var req struct {
		ModelID      string `json:"model_id"`
		Effort       string `json:"effort"`
		BudgetTokens *int   `json:"budget_tokens,omitempty"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.ModelID == "" {
		return nil, fmt.Errorf("model_id is required")
	}
	if req.Effort != "" && !llm.IsValidEffort(req.Effort) {
		return nil, fmt.Errorf("invalid effort tier: %q", req.Effort)
	}

	// Look up the model entry so we can return a clear error for typos.
	providerID, modelID := splitModelRef(req.ModelID)
	provider, ok := h.modelsCfg.Providers[providerID]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", providerID)
	}
	if _, ok := provider.Models[modelID]; !ok {
		return nil, fmt.Errorf("model not found: %s/%s", providerID, modelID)
	}

	// Schema migration pending (spec §3.3 on-disk default_reasoning field
	// for config.Model). The runtime llm.ModelConfig carries the field;
	// the on-disk config.ModelsConfig does not. Until migration is
	// complete, return a clear not-implemented message so callers know
	// the change didn't persist.
	return map[string]any{
		"ok":    false,
		"error": "model default persistence requires schema migration; use reasoning.set per-agent instead",
	}, nil
}

// splitModelRef splits a "provider/model-id" ref into its components.
// Returns empty strings on malformed input.
func splitModelRef(ref string) (providerID, modelID string) {
	for i, r := range ref {
		if r == '/' {
			return ref[:i], ref[i+1:]
		}
	}
	return "", ""
}

// handleSessionSet sets a per-session reasoning override on the agent loop
// currently handling that session. Translates the HTTP session-scoped request
// into a SetReasoningOverride call on the target AgentLoop.
//
// Params: {
//   "session_id": string,         // conversation ID (required)
//   "agent_id":   string,         // target agent (optional; see note)
//   "effort":         string,     // optional tier
//   "budget_tokens":  int,        // optional token budget override
//   "force":          bool,       // optional capability bypass
// }
//
// Resolution of agent_id → loop:
//  1. When `agent_id` is supplied, use registry.Get(agent_id) directly.
//  2. TODO: When agent_id is absent, look up the loop currently servicing
//     session_id via the AgentRegistry's conversation→loop map. Today no
//     such map exists (the registry tracks conversation→queue only), so
//     the MVP requires `agent_id` in the body. See plan P3-C.
//
// Result: {"ok": true, "session_id": string, "agent_id": string}.
// Error: "session not found" when the target loop can't be resolved.
func (h *ReasoningHandler) handleSessionSet(_ context.Context, params json.RawMessage) (any, error) {
	if h.registry == nil {
		return nil, fmt.Errorf("agent registry not available")
	}
	var req struct {
		SessionID    string `json:"session_id"`
		AgentID      string `json:"agent_id"`
		Effort       string `json:"effort"`
		BudgetTokens *int   `json:"budget_tokens,omitempty"`
		Force        *bool  `json:"force,omitempty"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if req.Effort != "" && !llm.IsValidEffort(req.Effort) {
		return nil, fmt.Errorf("invalid effort tier: %q", req.Effort)
	}

	// TODO(P3-C): look up loop via conversation→loop mapping when it lands.
	// Until then the caller must supply agent_id.
	if req.AgentID == "" {
		return nil, fmt.Errorf("session %q has no bound agent loop (agent_id required)", req.SessionID)
	}

	loop, err := h.registry.Get(req.AgentID)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %s", req.AgentID)
	}
	if loop == nil {
		return nil, fmt.Errorf("agent loop is nil: %s", req.AgentID)
	}

	rc := &llm.ReasoningConfig{
		Effort:       req.Effort,
		BudgetTokens: req.BudgetTokens,
		Force:        req.Force != nil && *req.Force,
	}
	loop.SetReasoningOverride(rc)

	return map[string]any{
		"ok":         true,
		"session_id": req.SessionID,
		"agent_id":   req.AgentID,
	}, nil
}

// handleSessionClear clears a per-session reasoning override on the agent loop
// currently handling that session.
//
// Params: {"session_id": string, "agent_id": string (optional; same MVP
// constraint as session_set)}.
//
// Result: {"ok": true, "session_id": string, "agent_id": string}.
func (h *ReasoningHandler) handleSessionClear(_ context.Context, params json.RawMessage) (any, error) {
	if h.registry == nil {
		return nil, fmt.Errorf("agent registry not available")
	}
	var req struct {
		SessionID string `json:"session_id"`
		AgentID   string `json:"agent_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if req.AgentID == "" {
		return nil, fmt.Errorf("session %q has no bound agent loop (agent_id required)", req.SessionID)
	}

	loop, err := h.registry.Get(req.AgentID)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %s", req.AgentID)
	}
	if loop == nil {
		return nil, fmt.Errorf("agent loop is nil: %s", req.AgentID)
	}

	loop.ClearReasoningOverride()

	return map[string]any{
		"ok":         true,
		"session_id": req.SessionID,
		"agent_id":   req.AgentID,
	}, nil
}

// handleListAgents returns reasoning configs for all registered agents.
func (h *ReasoningHandler) handleListAgents(_ context.Context, _ json.RawMessage) (any, error) {
	if h.registry == nil {
		return nil, fmt.Errorf("agent registry not available")
	}
	specs := h.registry.ListSpecs()
	agents := make([]reasoningGetResult, 0, len(specs))
	for _, spec := range specs {
		result := reasoningGetResult{
			AgentID: spec.ID,
		}
		if spec.Constraints.Reasoning != nil {
			result.HasReasoning = true
			result.Config = spec.Constraints.Reasoning
			result.EffectiveEffort = spec.Constraints.Reasoning.Effort
		}
		if loop, err := h.registry.Get(spec.ID); err == nil && loop != nil {
			if effort := loop.CurrentReasoningEffort(); effort != "" {
				result.EffectiveEffort = effort
			}
		}
		agents = append(agents, result)
	}
	return agents, nil
}

// saveConfigAtomic writes a *config.Config to path atomically (write .tmp,
// rename). Used for reasoning budget persistence. Uses JSON (not JSON5) —
// the JSON5 loader accepts plain JSON, so the round-trip works.
func saveConfigAtomic(path string, cfg *config.Config) error {
	if path == "" {
		return fmt.Errorf("empty config path")
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return atomicWrite(path, data)
}

// saveModelsConfigAtomic writes a *config.ModelsConfig to path atomically.
//nolint:unused -- reserved for future atomic model config updates
func saveModelsConfigAtomic(path string, cfg *config.ModelsConfig) error {
	if path == "" {
		return fmt.Errorf("empty models config path")
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal models config: %w", err)
	}
	return atomicWrite(path, data)
}

// atomicWrite writes data to path via a temp file then rename. The temp
// file is created in the same directory to guarantee the rename is atomic
// on POSIX filesystems.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".meept-cfg-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	// Best-effort cleanup on any failure path.
	cleanup := func() { _ = os.Remove(tmpPath) }

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file: %w", err)
	}
	// Restricted perms: config may contain API keys / tokens.
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
