package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
)

// executorBudget returns 40% of the model's context limit — the
// per-step token budget for chunking decisions.
func executorBudget(modelCfg *llm.ModelConfig) int {
	if modelCfg == nil || modelCfg.ContextLimit <= 0 {
		return 12000 // safe default
	}
	return int(float64(modelCfg.ContextLimit) * 0.40)
}

// toolOutputBudget returns an estimated upper bound on tool output size
// per tool-hint class. Used by estimateStepTokens.
func toolOutputBudget(toolHint string) int {
	switch toolHint {
	case "code", "refactor":
		return 8000
	case "debug", "fix":
		return 4000
	case "git", "commit":
		return 1000
	case "chat":
		return 1000
	default:
		return 2000
	}
}

// estimateStepTokens returns a rough estimate of the tokens consumed by a
// single step: description + accumulated context + tool output budget.
func estimateStepTokens(step *task.TaskStep, modelCfg *llm.ModelConfig) int {
	desc := llm.EstimateTokenCountHeuristic(step.Description)
	acc := llm.EstimateTokenCountHeuristic(step.AccumulatedContext)
	toolBudget := toolOutputBudget(step.ToolHint)
	return desc + acc + toolBudget
}

// chunkToExecutorCapacity walks a task's steps and splits any that exceed
// the executor's budget into sub-steps. Per-task split counter caps at 5
// to prevent cascading splits.
//
// Chunking is advisory: if any dependency (tactical, registry, stepStore,
// templateReg) is not wired, the method returns nil immediately.
//
//nolint:U1000 // wired by Task 5/6 of Plan C+F (startNextPhase + daemon wiring)
func (o *Orchestrator) chunkToExecutorCapacity(ctx context.Context, taskID string) error {
	if o.tactical == nil || o.registry == nil || o.stepStore == nil || o.templateReg == nil {
		o.logger.Debug("chunkToExecutorCapacity skipped: dependencies not wired",
			"task_id", taskID,
			"tactical", o.tactical != nil,
			"registry", o.registry != nil,
			"stepStore", o.stepStore != nil,
			"templateReg", o.templateReg != nil,
		)
		return nil
	}
	steps, err := o.stepStore.ListByTaskID(taskID)
	if err != nil {
		return fmt.Errorf("list steps for chunking: %w", err)
	}
	splitsThisTask := 0
	const maxSplitsPerTask = 5
	for _, step := range steps {
		if splitsThisTask >= maxSplitsPerTask {
			o.logger.Warn("Per-task split cap reached", "task_id", taskID, "cap", maxSplitsPerTask)
			return nil
		}
		executorID := o.tactical.SelectAgentForHint(step.ToolHint)
		modelCfg, err := o.registry.GetModelConfig(executorID)
		if err != nil {
			continue // fall back to ContextFirewall at runtime
		}
		budget := executorBudget(modelCfg)
		cost := estimateStepTokens(step, modelCfg)
		if cost > budget {
			subSteps, splitErr := o.splitStep(ctx, step, budget, modelCfg)
			if splitErr != nil {
				o.logger.Warn("splitStep failed; leaving step oversized",
					"step_id", step.ID, "error", splitErr)
				continue
			}
			if err := o.stepStore.ReplaceWithSubSteps(step.ID, subSteps); err != nil {
				o.logger.Warn("ReplaceWithSubSteps failed", "step_id", step.ID, "error", err)
				continue
			}
			splitsThisTask++
		}
	}
	return nil
}

// splitStep uses the split.md template and the planner agent's LLM to
// decompose an oversized step into sub-steps that each fit within the
// executor's budget.
//
//nolint:U1000 // called by chunkToExecutorCapacity, wired in Task 5/6 of Plan C+F
func (o *Orchestrator) splitStep(ctx context.Context, step *task.TaskStep, budget int, modelCfg *llm.ModelConfig) ([]*task.TaskStep, error) {
	if o.templateReg == nil {
		return nil, fmt.Errorf("template registry not wired")
	}
	executorID := o.tactical.SelectAgentForHint(step.ToolHint)
	prompt, err := o.templateReg.render("orchestrator/split.md", map[string]any{
		"BudgetTokens":    budget,
		"StepDescription": step.Description,
		"ToolHint":        step.ToolHint,
		"ExecutorID":      executorID,
		"ContextLimit":    modelCfg.ContextLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("render split template: %w", err)
	}
	// Use planner model for splitting
	splitLLM, err := o.registry.Get(config.AgentIDPlanner)
	if err != nil {
		return nil, fmt.Errorf("get planner for split: %w", err)
	}
	conversationID := fmt.Sprintf("split-%s-%s", step.TaskID, step.ID)
	splitCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	output, err := splitLLM.RunOnce(splitCtx, prompt, conversationID)
	if err != nil {
		return nil, fmt.Errorf("split LLM call failed: %w", err)
	}
	jsonStr := ExtractJSON(output)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON in split output")
	}
	var parsed struct {
		SubSteps []plannerStep `json:"sub_steps"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil, fmt.Errorf("parse split JSON: %w", err)
	}
	if len(parsed.SubSteps) == 0 {
		return nil, fmt.Errorf("split produced no sub-steps")
	}
	if len(parsed.SubSteps) > 5 {
		parsed.SubSteps = parsed.SubSteps[:5]
	}
	// Build sub-steps preserving dependencies
	var subSteps []*task.TaskStep
	var prevID string
	for i, ss := range parsed.SubSteps {
		sub := task.NewTaskStep(step.TaskID, ss.Description, step.Sequence+i+1)
		sub.ToolHint = ss.ToolHint
		sub.Phase = step.Phase
		sub.TaskID = step.TaskID
		// Preserve original dependencies on the first sub-step
		if i == 0 {
			sub.DependsOn = step.DependsOn
		} else if prevID != "" {
			// Chain sub-steps: each depends on prior
			sub.DependsOn = []string{prevID}
		}
		subSteps = append(subSteps, sub)
		prevID = sub.ID
	}
	return subSteps, nil
}
