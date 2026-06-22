package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/preferences"
	"github.com/caimlas/meept/internal/selfimprove"
)

// ContextInjector merges learning patterns and user instructions
// into system prompts for context enrichment.
type ContextInjector struct {
	learning     *selfimprove.LearningPipeline
	instructions *preferences.Store
}

// NewContextInjector creates a new context injector.
func NewContextInjector(
	learning *selfimprove.LearningPipeline,
	instructions *preferences.Store,
) *ContextInjector {
	return &ContextInjector{
		learning:     learning,
		instructions: instructions,
	}
}

// BuildSystemPrompt builds a system prompt with both learned patterns
// and active user instructions injected.
//
// Per Phase 4 spec 4.2:
// - Merges Learning patterns AND User Instructions
// - Format: "## Standing Instructions" + "## Learned Patterns"
// - Queries instructionStore.GetActive() for active instructions
func (c *ContextInjector) BuildSystemPrompt(ctx context.Context, base string) string {
	var sb strings.Builder
	sb.WriteString(base)

	// Get active instructions
	var instructions []*preferences.UserInstruction
	if c.instructions != nil {
		instructions = c.instructions.GetActive()
	}

	// Get learned patterns (if learning pipeline is available)
	var patterns []*selfimprove.LearnedPattern
	if c.learning != nil {
		// Retrieve top patterns for general context
		patterns, _ = c.learning.Retrieve(ctx, "general", "all", 10)
	}

	// Inject context if we have either instructions or patterns
	if len(instructions) > 0 || len(patterns) > 0 {
		sb.WriteString("\n\n# Active Context\n")

		// Standing instructions section
		if len(instructions) > 0 {
			sb.WriteString("\n## Standing Instructions\n")
			sb.WriteString("The following automated actions are configured and will execute when their triggers match:\n\n")
			for i, instr := range instructions {
				sb.WriteString(fmt.Sprintf("%d. **%s** (trigger: `%s`, action: `%s`)\n",
					i+1, instr.ID, instr.Trigger, instr.Action))
				if instr.Scope == "project" {
					sb.WriteString("   _Scope: This project only_\n")
				}
			}
		}

		// Learned patterns section
		if len(patterns) > 0 {
			sb.WriteString("\n## Learned Patterns\n")
			sb.WriteString("The following patterns have been learned from past interactions:\n\n")
			for i, p := range patterns {
				sb.WriteString(fmt.Sprintf("%d. %s (confidence: %.2f, type: %s)\n",
					i+1, p.Description, p.Confidence, p.Type))
			}
		}

		sb.WriteString("\nWhen triggers occur, execute associated actions automatically.\n")
	}

	return sb.String()
}

// HasActiveInstructions returns true if there are active user instructions.
func (c *ContextInjector) HasActiveInstructions() bool {
	if c.instructions == nil {
		return false
	}
	return len(c.instructions.GetActive()) > 0
}

// HasLearnedPatterns returns true if there are learned patterns available.
func (c *ContextInjector) HasLearnedPatterns(ctx context.Context) bool {
	if c.learning == nil {
		return false
	}
	patterns, _ := c.learning.Retrieve(ctx, "general", "all", 1)
	return len(patterns) > 0
}

// GetActiveInstructions returns all active user instructions.
func (c *ContextInjector) GetActiveInstructions() []*preferences.UserInstruction {
	if c.instructions == nil {
		return nil
	}
	return c.instructions.GetActive()
}
