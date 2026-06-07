package bot

import (
	"fmt"
	"strings"
	"time"
)

const maxConsecutiveFailures = 10

type BotRunner struct {
	definition BotDefinition
	namespace  *MemoryNamespace
}

func NewBotRunner(def BotDefinition) *BotRunner {
	return &BotRunner{
		definition: def,
		namespace:  NewMemoryNamespace(def.ID),
	}
}

func (r *BotRunner) Definition() BotDefinition {
	return r.definition
}

// ShouldRun checks whether the bot should execute given its current state.
func (r *BotRunner) ShouldRun(state *BotState) bool {
	if state == nil {
		return true
	}

	if state.ConsecutiveFailures >= maxConsecutiveFailures {
		return false
	}

	if r.definition.Constraints.DailyBudgetCents > 0 {
		today := time.Now().Format("2006-01-02")
		if state.TodayDate == today && state.TodayCostCents >= r.definition.Constraints.DailyBudgetCents {
			return false
		}
	}

	if r.definition.Constraints.MaxInvocationsPerDay > 0 {
		today := time.Now().Format("2006-01-02")
		if state.TodayDate == today && state.TodayRuns >= r.definition.Constraints.MaxInvocationsPerDay {
			return false
		}
	}

	return true
}

// BuildSystemPrompt constructs the system prompt for a bot invocation.
func (r *BotRunner) BuildSystemPrompt(triggerContext string) string {
	var b strings.Builder

	b.WriteString(r.definition.Prompt)

	b.WriteString("\n\n## Bot Identity\n")
	b.WriteString(fmt.Sprintf("You are bot %q (%s).\n", r.definition.ID, r.definition.Name))
	b.WriteString(fmt.Sprintf("Description: %s\n", r.definition.Description))

	b.WriteString("\n## Current Invocation\n")
	b.WriteString(fmt.Sprintf("Trigger context: %s\n", triggerContext))
	b.WriteString(fmt.Sprintf("Timestamp: %s\n", time.Now().UTC().Format(time.RFC3339)))

	b.WriteString("\n## Instructions\n")
	b.WriteString("Perform your task and store any important observations in memory for future invocations.\n")
	b.WriteString("Be concise. You are running autonomously - there is no user to interact with.\n")

	return b.String()
}

// BuildUserMessage constructs the user message for a bot invocation.
func (r *BotRunner) BuildUserMessage(triggerContext string) string {
	return fmt.Sprintf("[Bot %s triggered] %s", r.definition.ID, triggerContext)
}
