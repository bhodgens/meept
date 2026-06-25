package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/skills/lifecycle"
	"github.com/caimlas/meept/pkg/id"
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
				RPCKeyName:        s.Name,
				RPCKeyDescription: s.Description,
				RPCKeyTags:        s.Tags,
				RPCKeyRequires:    s.Requires,
				RPCKeyPath:        s.Path,
				RPCKeyPriority:    s.Priority,
				RPCKeyRiskLevel:   s.RiskLevel,
			}
		}
		result = map[string]any{
			"skills":    skillsData,
			RPCKeyCount: len(skillsData),
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
		RPCKeyName:        skill.Name,
		RPCKeyDescription: skill.Description,
		RPCKeyTags:        skill.Tags,
		RPCKeyRequires:    skill.Requires,
		"examples":        skill.Examples,
		"body":            skill.Body,
		RPCKeyPath:        skill.Path,
		RPCKeyPriority:    skill.Priority,
		"allowed_tools":   skill.AllowedTools,
		RPCKeyRiskLevel:   skill.RiskLevel,
		"max_iterations":  skill.MaxIterations,
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
		RPCKeyContent:       execResult.Content,
		RPCKeyModel:         execResult.Model,
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
		ID:        id.Generate("skills-"),
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
// The tracker, writer, versioner, and evolver parameters enable skills.stats,
// skills.archive, skills.restore (archive un-move OR version restore),
// skills.history, and skills.evolve. They may be nil (those handlers return an error).
func RegisterSkillsHandlers(server *Server, registry *skills.Registry, executor *skills.Executor, tracker lifecycle.UsageTracker, writer *lifecycle.Writer, versioner *lifecycle.Versioner, evolver *lifecycle.Evolver) {
	// skills.list - list all available skills
	server.RegisterHandler("skills.list", func(ctx context.Context, params json.RawMessage) (any, error) {
		if registry == nil {
			return nil, fmt.Errorf("skill registry not configured")
		}

		skillsList := registry.List()
		skillsData := make([]map[string]any, len(skillsList))
		for i, s := range skillsList {
			skillsData[i] = map[string]any{
				RPCKeyName:        s.Name,
				RPCKeyDescription: s.Description,
				RPCKeyTags:        s.Tags,
				RPCKeyRequires:    s.Requires,
				RPCKeyPath:        s.Path,
				RPCKeyPriority:    s.Priority,
				RPCKeyRiskLevel:   s.RiskLevel,
			}
		}

		return map[string]any{
			"skills":    skillsData,
			RPCKeyCount: len(skillsData),
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
			RPCKeyName:        skill.Name,
			RPCKeyDescription: skill.Description,
			RPCKeyTags:        skill.Tags,
			RPCKeyRequires:    skill.Requires,
			"examples":        skill.Examples,
			"body":            skill.Body,
			RPCKeyPath:        skill.Path,
			RPCKeyPriority:    skill.Priority,
			"allowed_tools":   skill.AllowedTools,
			RPCKeyRiskLevel:   skill.RiskLevel,
			"max_iterations":  skill.MaxIterations,
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
			RPCKeyContent:       result.Content,
			RPCKeyModel:         result.Model,
			"prompt_tokens":     result.PromptTokens,
			"completion_tokens": result.CompletionTokens,
			"total_tokens":      result.TotalTokens,
		}, nil
	})

	// skills.stats - get usage statistics for one or all skills
	server.RegisterHandler("skills.stats", func(ctx context.Context, params json.RawMessage) (any, error) {
		if tracker == nil {
			return nil, fmt.Errorf("skill usage tracker not configured")
		}

		var p struct {
			Name string `json:"name"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("invalid parameters: %w", err)
			}
		}

		if p.Name != "" {
			stats, err := tracker.GetStats(p.Name)
			if err != nil {
				return nil, fmt.Errorf("failed to get stats: %w", err)
			}
			return stats, nil
		}

		// Return all stats.
		all, err := tracker.GetAllStats()
		if err != nil {
			return nil, fmt.Errorf("failed to get all stats: %w", err)
		}
		return map[string]any{
			"stats": all,
			"count": len(all),
		}, nil
	})

	// skills.archive - archive a skill (move to archived dir, unregister)
	server.RegisterHandler("skills.archive", func(ctx context.Context, params json.RawMessage) (any, error) {
		if writer == nil {
			return nil, fmt.Errorf("skill writer not configured")
		}

		var p struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		if p.Name == "" {
			return nil, fmt.Errorf("skill name is required")
		}

		if err := writer.ArchiveSkill(p.Name); err != nil {
			return nil, fmt.Errorf("failed to archive skill: %w", err)
		}

		return map[string]any{
			"archived": true,
			"name":     p.Name,
		}, nil
	})

	// skills.restore - restore an archived skill (move back, re-register) or
	// restore a prior version (when version > 0).
	server.RegisterHandler("skills.restore", func(ctx context.Context, params json.RawMessage) (any, error) {
		var p struct {
			Name    string `json:"name"`
			Version int    `json:"version"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		if p.Name == "" {
			return nil, fmt.Errorf("skill name is required")
		}

		// Version-restore path: revert SKILL.md from the version bundle.
		if p.Version > 0 {
			if versioner == nil {
				return nil, fmt.Errorf("skill versioner not configured")
			}
			if err := versioner.Restore(p.Name, p.Version); err != nil {
				return nil, fmt.Errorf("failed to restore skill version: %w", err)
			}
			return map[string]any{
				"restored": true,
				"name":     p.Name,
				"version":  p.Version,
			}, nil
		}

		// Archive-restore path (default): move from archived dir back to live.
		if writer == nil {
			return nil, fmt.Errorf("skill writer not configured")
		}
		if err := writer.RestoreSkill(p.Name); err != nil {
			return nil, fmt.Errorf("failed to restore skill: %w", err)
		}
		return map[string]any{
			"restored": true,
			"name":     p.Name,
		}, nil
	})

	// skills.history - list version history for a skill
	server.RegisterHandler("skills.history", func(ctx context.Context, params json.RawMessage) (any, error) {
		if versioner == nil {
			return nil, fmt.Errorf("skill versioner not configured")
		}

		var p struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		if p.Name == "" {
			return nil, fmt.Errorf("skill name is required")
		}

		entries, err := versioner.History(p.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get skill history: %w", err)
		}

		return map[string]any{
			"name":    p.Name,
			"entries": entries,
			"count":   len(entries),
		}, nil
	})

	// skills.evolve - manually trigger an evolver cycle.
	// Runs one full cycle (refine + promote + prune) synchronously and returns
	// the EvolutionReport. The evolver respects its configured AutoApply flag:
	// when false, proposals are emitted as plans (if a plan manager is wired)
	// rather than applied directly.
	server.RegisterHandler("skills.evolve", func(ctx context.Context, params json.RawMessage) (any, error) {
		if evolver == nil {
			return nil, fmt.Errorf("skill evolver not configured")
		}

		report, err := evolver.RunCycle(ctx)
		if err != nil {
			return nil, fmt.Errorf("evolution cycle failed: %w", err)
		}

		return map[string]any{
			"refined":  report.Refined,
			"promoted": report.Promoted,
			"pruned":   report.Pruned,
			"skipped":  report.Skipped,
			"rejected": report.Rejected,
			"planned":  report.Planned,
			"details":  report.Details,
		}, nil
	})
}
