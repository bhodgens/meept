package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/skills/lifecycle"
)

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
			"gaps":     report.Gaps,
			"skipped":  report.Skipped,
			"rejected": report.Rejected,
			"planned":  report.Planned,
			"details":  report.Details,
		}, nil
	})

	// skills.gaps - show low-match queries (coverage gaps).
	// Returns queries that recurred without matching any existing skill above
	// threshold, ranked by descending count. Used to identify what new skills
	// should be created.
	server.RegisterHandler("skills.gaps", func(ctx context.Context, params json.RawMessage) (any, error) {
		if tracker == nil {
			return nil, fmt.Errorf("skill usage tracker not configured")
		}

		gaps, err := tracker.GetLowMatchQueries(ctx, 0.5, 50)
		if err != nil {
			return nil, fmt.Errorf("failed to get low-match queries: %w", err)
		}

		return map[string]any{
			"gaps":  gaps,
			"count": len(gaps),
		}, nil
	})
}
