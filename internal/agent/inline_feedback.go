package agent

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/selfimprove"
)

// FeedbackPipeline is the interface for submitting skill improvement proposals.
// It is satisfied by selfimprove.LearningPipeline.
type FeedbackPipeline interface {
	SubmitProposal(ctx context.Context, proposal selfimprove.SkillProposal) error
}

// ClassifyFunc analyzes recent messages to determine if a skill improvement
// is warranted. It returns a structured result string or an error.
type ClassifyFunc func(ctx context.Context, prompt string, recentMessages []string) (string, error)

// InlineFeedbackTrigger implements PrepareNextTurnHook. Every N messages it
// fires a non-blocking classification goroutine that may submit a skill
// improvement proposal to the learning pipeline. PrepareNextTurn always
// returns a zero-value TurnModification so the agent loop is never delayed.
type InlineFeedbackTrigger struct {
	mu           sync.Mutex
	messageCount int
	interval     int
	activeSkill  string
	pipeline     FeedbackPipeline
	classifyFn   ClassifyFunc
}

// NewInlineFeedbackTrigger creates a trigger that fires every interval messages.
// If pipeline or classifyFn is nil the trigger is inert (never fires).
func NewInlineFeedbackTrigger(interval int, pipeline FeedbackPipeline, classifyFn ClassifyFunc) *InlineFeedbackTrigger {
	if interval < 1 {
		interval = 10
	}
	return &InlineFeedbackTrigger{
		interval:   interval,
		pipeline:   pipeline,
		classifyFn: classifyFn,
	}
}

// SetActiveSkill sets the skill currently in use. Classification only fires
// when a skill is active.
func (t *InlineFeedbackTrigger) SetActiveSkill(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.activeSkill = name
}

// PrepareNextTurn implements PrepareNextTurnHook. It increments the message
// counter and, when the interval is reached and a skill is active, launches a
// non-blocking goroutine to classify and potentially submit a proposal.
// It always returns a zero-value TurnModification.
func (t *InlineFeedbackTrigger) PrepareNextTurn(ctx context.Context, state TurnState) TurnModification {
	t.mu.Lock()
	t.messageCount++
	shouldFire := t.messageCount%t.interval == 0 && t.activeSkill != ""
	skill := t.activeSkill
	t.mu.Unlock()

	if shouldFire && t.pipeline != nil && t.classifyFn != nil {
		recent := recentMessageStrings(state.Messages, 5)
		go t.classifyAndSubmit(ctx, skill, state.LastResponse, recent)
	}

	return TurnModification{}
}

// classifyAndSubmit runs classification and submits a proposal if an
// improvement is identified. Errors are silently discarded — feedback is
// best-effort and must never disrupt the agent loop.
func (t *InlineFeedbackTrigger) classifyAndSubmit(ctx context.Context, skill, prompt string, recent []string) {
	result, err := t.classifyFn(ctx, prompt, recent)
	if err != nil {
		return
	}

	proposal, ok := parseFeedbackResult(result, skill)
	if !ok {
		return
	}

	if err := t.pipeline.SubmitProposal(ctx, proposal); err != nil {
		slog.Debug("inline feedback submission failed", "error", err)
	}
}

// parseFeedbackResult parses a classification result string into a
// SkillProposal. The expected format is:
//
//	IMPROVEMENT
//	SKILL: <skill-name>
//	CHANGE: <description of change>
//
// Returns ok=false for NO_IMPROVEMENT or unparseable results.
func parseFeedbackResult(result, activeSkill string) (selfimprove.SkillProposal, bool) {
	trimmed := strings.TrimSpace(result)
	if trimmed == "" || trimmed == "NO_IMPROVEMENT" {
		return selfimprove.SkillProposal{}, false
	}

	if !strings.HasPrefix(trimmed, "IMPROVEMENT") {
		return selfimprove.SkillProposal{}, false
	}

	lines := strings.Split(trimmed, "\n")
	var skillName, change string

	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "SKILL:"):
			skillName = strings.TrimSpace(strings.TrimPrefix(line, "SKILL:"))
		case strings.HasPrefix(line, "CHANGE:"):
			change = strings.TrimSpace(strings.TrimPrefix(line, "CHANGE:"))
		}
	}

	if skillName == "" {
		skillName = activeSkill
	}
	if change == "" {
		return selfimprove.SkillProposal{}, false
	}

	return selfimprove.SkillProposal{
		Description: "Inline feedback: " + change,
		SkillName:   skillName,
		Change:      change,
		Source:      "inline-feedback",
	}, true
}

// recentMessageStrings extracts the content of the last n messages as strings.
func recentMessageStrings(messages []llm.ChatMessage, n int) []string {
	if len(messages) == 0 {
		return nil
	}
	start := len(messages) - n
	if start < 0 {
		start = 0
	}
	out := make([]string, 0, len(messages)-start)
	for _, m := range messages[start:] {
		out = append(out, m.Content)
	}
	return out
}
