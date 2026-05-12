package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/pkg/models"
)

// SkillsHandler handles skill-related RPC requests via the message bus.
type SkillsHandler struct {
	registry *skills.Registry
	executor *skills.Executor
	bus      *bus.MessageBus
	cancel   context.CancelFunc
}

// NewSkillsHandler creates a new skills handler.
func NewSkillsHandler(registry *skills.Registry, executor *skills.Executor, msgBus *bus.MessageBus) *SkillsHandler {
	return &SkillsHandler{
		registry: registry,
		executor: executor,
		bus:      msgBus,
	}
}

// Start begins listening for skill requests on the message bus.
func (h *SkillsHandler) Start(ctx context.Context) error {
	ctx, h.cancel = context.WithCancel(ctx)

	// Subscribe to skills topics
	listSub := h.bus.Subscribe("skills-handler", "skills.list")
	getSub := h.bus.Subscribe("skills-handler", "skills.get")
	execSub := h.bus.Subscribe("skills-handler", "skills.execute")

	go func() {
		for {
			select {
			case <-ctx.Done():
				h.bus.Unsubscribe(listSub)
				h.bus.Unsubscribe(getSub)
				h.bus.Unsubscribe(execSub)
				return
			case msg, ok := <-listSub.Channel:
				if !ok {
					return
				}
				h.handleList(ctx, msg)
			case msg, ok := <-getSub.Channel:
				if !ok {
					return
				}
				h.handleGet(ctx, msg)
			case msg, ok := <-execSub.Channel:
				if !ok {
					return
				}
				h.handleExecute(ctx, msg)
			}
		}
	}()

	return nil
}

// Stop stops the handler.
func (h *SkillsHandler) Stop(ctx context.Context) error {
	if h.cancel != nil {
		h.cancel()
	}
	return nil
}

// handleList handles skills.list requests.
func (h *SkillsHandler) handleList(_ context.Context, msg *models.BusMessage) {
	var result any
	var err error

	if h.registry == nil {
		err = fmt.Errorf("skill registry not configured")
	} else {
		skillsList := h.registry.List()
		skillsData := make([]map[string]any, len(skillsList))
		for i, s := range skillsList {
			skillsData[i] = map[string]any{
				"name":        s.Name,
				"description": s.Description,
				"tags":        s.Tags,
				"requires":    s.Requires,
				"path":        s.Path,
				"priority":    s.Priority,
				"risk_level":  s.RiskLevel,
			}
		}
		result = map[string]any{
			"skills": skillsData,
			"count":  len(skillsData),
		}
	}

	h.sendResponse(msg.ID, result, err)
}

// handleGet handles skills.get requests.
func (h *SkillsHandler) handleGet(_ context.Context, msg *models.BusMessage) {
	var params struct {
		Name string `json:"name"`
	}

	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		h.sendResponse(msg.ID, nil, fmt.Errorf("invalid parameters: %w", err))
		return
	}

	if h.registry == nil {
		h.sendResponse(msg.ID, nil, fmt.Errorf("skill registry not configured"))
		return
	}

	skill := h.registry.Get(params.Name)
	if skill == nil {
		h.sendResponse(msg.ID, nil, fmt.Errorf("skill not found: %s", params.Name))
		return
	}

	result := map[string]any{
		"name":           skill.Name,
		"description":    skill.Description,
		"tags":           skill.Tags,
		"requires":       skill.Requires,
		"examples":       skill.Examples,
		"body":           skill.Body,
		"path":           skill.Path,
		"priority":       skill.Priority,
		"allowed_tools":  skill.AllowedTools,
		"risk_level":     skill.RiskLevel,
		"max_iterations": skill.MaxIterations,
	}

	h.sendResponse(msg.ID, result, nil)
}

// handleExecute handles skills.execute requests.
func (h *SkillsHandler) handleExecute(ctx context.Context, msg *models.BusMessage) {
	var params struct {
		Name  string `json:"name"`
		Input string `json:"input"`
	}

	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		h.sendResponse(msg.ID, nil, fmt.Errorf("invalid parameters: %w", err))
		return
	}

	if h.registry == nil {
		h.sendResponse(msg.ID, nil, fmt.Errorf("skill registry not configured"))
		return
	}

	if h.executor == nil {
		h.sendResponse(msg.ID, nil, fmt.Errorf("skill executor not configured"))
		return
	}

	skill := h.registry.Get(params.Name)
	if skill == nil {
		h.sendResponse(msg.ID, nil, fmt.Errorf("skill not found: %s", params.Name))
		return
	}

	// Execute the skill
	execResult, err := h.executor.Execute(ctx, skill, params.Input)
	if err != nil {
		h.sendResponse(msg.ID, nil, fmt.Errorf("skill execution failed: %w", err))
		return
	}

	result := map[string]any{
		"content":           execResult.Content,
		"model":             execResult.Model,
		"prompt_tokens":     execResult.PromptTokens,
		"completion_tokens": execResult.CompletionTokens,
		"total_tokens":      execResult.TotalTokens,
	}

	h.sendResponse(msg.ID, result, nil)
}

// sendResponse sends a response message.
func (h *SkillsHandler) sendResponse(replyTo string, result any, err error) {
	var payload []byte

	if err != nil {
		payload, _ = json.Marshal(map[string]any{
			"error": err.Error(),
		})
	} else {
		payload, _ = json.Marshal(result)
	}

	respMsg := &models.BusMessage{
		ID:        fmt.Sprintf("skills-%d", time.Now().UnixNano()),
		Type:      models.MessageTypeResponse,
		Topic:     "skills.result",
		Source:    "skills-handler",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
		ReplyTo:   replyTo,
	}

	h.bus.Publish("skills.result", respMsg)
}

// RegisterDirectHandlers registers skill handlers directly on the RPC server.
// This is an alternative to using the message bus for skills operations.
func RegisterSkillsHandlers(server *Server, registry *skills.Registry, executor *skills.Executor) {
	// skills.list - list all available skills
	server.RegisterHandler("skills.list", func(ctx context.Context, params json.RawMessage) (any, error) {
		if registry == nil {
			return nil, fmt.Errorf("skill registry not configured")
		}

		skillsList := registry.List()
		skillsData := make([]map[string]any, len(skillsList))
		for i, s := range skillsList {
			skillsData[i] = map[string]any{
				"name":        s.Name,
				"description": s.Description,
				"tags":        s.Tags,
				"requires":    s.Requires,
				"path":        s.Path,
				"priority":    s.Priority,
				"risk_level":  s.RiskLevel,
			}
		}

		return map[string]any{
			"skills": skillsData,
			"count":  len(skillsData),
		}, nil
	})

	// skills.get - get skill details by name
	server.RegisterHandler("skills.get", func(ctx context.Context, params json.RawMessage) (any, error) {
		var p struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		if registry == nil {
			return nil, fmt.Errorf("skill registry not configured")
		}

		skill := registry.Get(p.Name)
		if skill == nil {
			return nil, fmt.Errorf("skill not found: %s", p.Name)
		}

		return map[string]any{
			"name":           skill.Name,
			"description":    skill.Description,
			"tags":           skill.Tags,
			"requires":       skill.Requires,
			"examples":       skill.Examples,
			"body":           skill.Body,
			"path":           skill.Path,
			"priority":       skill.Priority,
			"allowed_tools":  skill.AllowedTools,
			"risk_level":     skill.RiskLevel,
			"max_iterations": skill.MaxIterations,
		}, nil
	})

	// skills.execute - execute a skill
	server.RegisterHandler("skills.execute", func(ctx context.Context, params json.RawMessage) (any, error) {
		var p struct {
			Name  string `json:"name"`
			Input string `json:"input"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		if registry == nil {
			return nil, fmt.Errorf("skill registry not configured")
		}

		if executor == nil {
			return nil, fmt.Errorf("skill executor not configured")
		}

		skill := registry.Get(p.Name)
		if skill == nil {
			return nil, fmt.Errorf("skill not found: %s", p.Name)
		}

		// Execute the skill
		result, err := executor.Execute(ctx, skill, p.Input)
		if err != nil {
			return nil, fmt.Errorf("skill execution failed: %w", err)
		}

		return map[string]any{
			"content":           result.Content,
			"model":             result.Model,
			"prompt_tokens":     result.PromptTokens,
			"completion_tokens": result.CompletionTokens,
			"total_tokens":      result.TotalTokens,
		}, nil
	})
}
