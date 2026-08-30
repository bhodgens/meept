package agent

import (
	"context"

	"github.com/caimlas/meept/internal/llm"
)

// WithSkillStateRuntime sets the SKILL.state-mode runtime (WikiSkill state
// layer, arXiv:2608.26263). When set, RunWithSkill routes skills that declare
// state: true to the runtime instead of the conversation loop. Nil guard
// prevents typed-nil panic; disabled/unset ⇒ byte-identical RunWithSkill
// behavior. Mirrors WithUsageTracker's shape (mutex-protected field + getter,
// because RunWithSkill reads it while other goroutines may configure the
// loop).
func WithSkillStateRuntime(r *SkillStateRuntime) LoopOption {
	return func(l *AgentLoop) {
		if r == nil {
			return
		}
		l.mu.Lock()
		l.skillStateRuntime = r
		l.mu.Unlock()
	}
}

// SkillStateRuntime returns the configured SKILL.state-mode runtime, or nil
// when state mode is not wired.
func (l *AgentLoop) SkillStateRuntime() *SkillStateRuntime {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.skillStateRuntime
}

// ExecuteSkillToolCalls is the exported tool-execution seam for the skill
// state runtime: leaf 05 wires it via SkillStateRuntime.WithToolRunner so
// state-mode tool calls share the loop's executor pipeline (memory gating,
// conversation-ID propagation, working-directory injection) instead of
// building a second Executor. Pure pass-through to the unexported
// executeToolCalls.
func (l *AgentLoop) ExecuteSkillToolCalls(ctx context.Context, toolCalls []llm.ToolCall) []*ExecutionResult {
	return l.executeToolCalls(ctx, toolCalls)
}
