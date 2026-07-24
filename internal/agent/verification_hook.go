package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/caimlas/meept/internal/agent/prompts"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

const verificationNudge = `NOTE: You just made 3+ file-modifying changes. Before writing your final summary, verify your work: run the tests, execute the code, check the output. You cannot self-assign PARTIAL by listing caveats — either verify or report what could not be verified.`

// Compile-time interface check.
var _ PrepareNextTurnHook = (*VerificationAutoTrigger)(nil)

// VerifierSpawner is the interface the hook needs to spawn an independent
// verifier subagent. AgentLoop implements this.
type VerifierSpawner interface {
	SpawnVerifier(ctx context.Context, prompt string, modelRef string) (string, error)
}

// VerificationAutoTrigger is a PrepareNextTurnHook that injects a verification
// nudge message when the tracked file-modifying tool call count reaches the
// configured threshold. When a VerifierSpawner is available, it also spawns an
// independent adversarial verifier and handles FAIL verdicts with a fix loop.
type VerificationAutoTrigger struct {
	tracker  *VerificationTracker
	config   VerificationConfig
	spawner  VerifierSpawner // nil = nudge-only mode
	fixCount int             // tracks fix loop iterations within a session
}

// NewVerificationAutoTrigger wires a tracker and verification config into a
// hook ready for registration with the HookRegistry.
func NewVerificationAutoTrigger(tracker *VerificationTracker, config VerificationConfig) *VerificationAutoTrigger {
	return &VerificationAutoTrigger{tracker: tracker, config: config}
}

// SetSpawner sets the verifier spawner for full adversarial verification.
// When nil, the hook operates in nudge-only mode.
func (h *VerificationAutoTrigger) SetSpawner(s VerifierSpawner) {
	if s != nil {
		h.spawner = s
	}
}

// PrepareNextTurn implements PrepareNextTurnHook. It returns a nudge message
// when verification is enabled, auto-trigger is on, and the threshold is met.
// When a spawner is available, it also runs the verifier and handles FAIL.
func (h *VerificationAutoTrigger) PrepareNextTurn(ctx context.Context, state TurnState) TurnModification {
	if !h.config.Enabled || !h.config.AutoTrigger {
		return TurnModification{}
	}
	if !h.tracker.ShouldTrigger() {
		return TurnModification{}
	}
	filesChanged := h.tracker.Snapshot()

	// Nudge-only mode (no spawner available).
	if h.spawner == nil {
		nudge := fmt.Sprintf("%s\n\nFiles changed:\n%s", verificationNudge, formatFileList(filesChanged))
		return TurnModification{
			Modified: true,
			ExtraMessages: []llm.ChatMessage{{
				Role:    llm.RoleUser,
				Content: nudge,
			}},
			Reason: "verification auto-trigger (nudge)",
		}
	}

	// Full adversarial verification: spawn verifier, parse verdict, handle FAIL.
	verifierPrompt := prompts.BuildVerifierPrompt(
		"", // agentRole — derived from state if available
		extractTaskDescription(state),
		filesChanged,
		"", // approach — not tracked yet
	)

	modelRef := h.config.EffectiveModel(state.ModelRef)
	result, err := h.spawner.SpawnVerifier(ctx, verifierPrompt, modelRef)
	if err != nil {
		slog.Warn("verification spawn failed", "error", err)
		// Fall back to nudge on spawn failure.
		nudge := fmt.Sprintf("%s\n\nFiles changed:\n%s", verificationNudge, formatFileList(filesChanged))
		return TurnModification{
			Modified:      true,
			ExtraMessages: []llm.ChatMessage{{Role: llm.RoleUser, Content: nudge}},
			Reason:        "verification auto-trigger (spawn failed, nudge fallback)",
		}
	}

	verdict, checks := ParseVerdict(result)
	switch verdict {
	case VerdictPass:
		slog.Info("verification passed", "checks", len(checks))
		h.fixCount = 0
		return TurnModification{} // continue normally

	case VerdictPartial:
		slog.Info("verification partial", "checks", len(checks))
		h.fixCount = 0
		return TurnModification{} // treat as pass with warning

	case VerdictFail:
		return h.handleFail(result, checks)

	default: // VerdictUnknown
		slog.Warn("could not parse verification verdict", "output_len", len(result))
		return TurnModification{}
	}
}

// handleFail implements the fix loop: inject verifier findings, loop up to
// MaxFixLoops, then escalate to user.
func (h *VerificationAutoTrigger) handleFail(verifierOutput string, checks []CheckResult) TurnModification {
	maxLoops := h.config.MaxFixLoops
	if maxLoops < 1 {
		maxLoops = 3
	}

	h.fixCount++

	if h.fixCount > maxLoops {
		// Max loops exhausted — escalate to user.
		h.fixCount = 0
		return TurnModification{
			Modified: true,
			ExtraMessages: []llm.ChatMessage{{
				Role: llm.RoleUser,
				Content: fmt.Sprintf(
					"Adversarial verification failed after %d fix attempts. Manual review needed.\n\nLast verifier output:\n%s",
					maxLoops, verifierOutput,
				),
			}},
			Reason: "verification escalation",
		}
	}

	// Build fix instruction with check details.
	var checkSummary strings.Builder
	for _, c := range checks {
		if !c.Passed {
			checkSummary.WriteString(fmt.Sprintf("- FAIL: %s\n", c.Name))
			if c.Command != "" {
				checkSummary.WriteString(fmt.Sprintf("  command: %s\n", c.Command))
			}
			if c.Output != "" {
				checkSummary.WriteString(fmt.Sprintf("  output: %s\n", c.Output))
			}
		}
	}

	fixInstruction := fmt.Sprintf(
		"Adversarial verification FAILED (iteration %d/%d). Fix the following issues:\n\n%s\n\nFailed checks:\n%s\nAfter fixing, the verifier will re-check your work.",
		h.fixCount, maxLoops, verifierOutput, checkSummary.String(),
	)

	return TurnModification{
		Modified: true,
		ExtraMessages: []llm.ChatMessage{{
			Role:    llm.RoleUser,
			Content: fixInstruction,
		}},
		Reason: fmt.Sprintf("verification fix loop (%d/%d)", h.fixCount, maxLoops),
	}
}

// extractTaskDescription pulls a task description from the turn state messages.
func extractTaskDescription(state TurnState) string {
	// Use the last user message as the task description.
	for i := len(state.Messages) - 1; i >= 0; i-- {
		if state.Messages[i].Role == llm.RoleUser && state.Messages[i].Content != "" {
			content := state.Messages[i].Content
			if len(content) > 500 {
				return content[:500] + "..."
			}
			return content
		}
	}
	return "unknown task"
}

func formatFileList(files []string) string {
	if len(files) == 0 {
		return "  (none tracked)"
	}
	var b strings.Builder
	for _, f := range files {
		b.WriteString("- " + f + "\n")
	}
	return b.String()
}

// -----------------------------------------------------------------------
// SpawnVerifier — AgentLoop implementation of VerifierSpawner
// -----------------------------------------------------------------------

// verifierAllowedTools is the restricted tool set for the verifier subagent.
var verifierAllowedTools = map[string]bool{
	"shell_execute":  true,
	"file_read":      true,
	"file_grep":      true,
	"file_find":      true,
	"list_directory": true,
	"web_fetch":      true,
}

// filteredToolRegistry wraps a ToolRegistry and only exposes allowed tools.
type filteredToolRegistry struct {
	parent  ToolRegistry
	allowed map[string]bool
}

func (r *filteredToolRegistry) Get(name string) tools.Tool {
	if !r.allowed[name] {
		return nil
	}
	return r.parent.Get(name)
}

func (r *filteredToolRegistry) List() []tools.Tool {
	var result []tools.Tool
	for _, t := range r.parent.List() {
		if r.allowed[t.Name()] {
			result = append(result, t)
		}
	}
	return result
}

func (r *filteredToolRegistry) GetDefinitions() []llm.ToolDefinition {
	var result []llm.ToolDefinition
	for _, def := range r.parent.GetDefinitions() {
		if r.allowed[def.Function.Name] {
			result = append(result, def)
		}
	}
	return result
}

// SpawnVerifier implements VerifierSpawner. It creates a child AgentLoop with
// a restricted tool set and the adversarial verifier prompt, runs it, and
// returns the raw output.
func (l *AgentLoop) SpawnVerifier(ctx context.Context, prompt string, modelRef string) (string, error) {
	if l.registry == nil {
		return "", fmt.Errorf("no tool registry available for verifier")
	}

	// Create a filtered registry with only read-only tools.
	filtered := &filteredToolRegistry{
		parent:  l.registry,
		allowed: verifierAllowedTools,
	}

	// Build verifier config: no verification on the verifier itself,
	// limited iterations, system prompt override.
	verifierConfig := l.config // shallow copy
	verifierConfig.SystemPromptOveride = prompt
	verifierConfig.MaxIterations = 20

	// Create child loop with restricted tools.
	childOpts := []LoopOption{
		WithLLMChatter(l.llm),
		WithResolver(l.resolver),
		WithToolRegistry(filtered),
		WithSecurityChecker(l.security),
		WithSecurityOrchestrator(l.securityOrch),
		WithLoopLogger(l.logger),
		WithAgentConfig(verifierConfig),
	}

	// Use the verifier model if specified.
	if modelRef != "" {
		childOpts = append(childOpts, WithModelRef(modelRef))
	} else {
		childOpts = append(childOpts, WithModelRef(l.modelRef))
	}

	child := NewAgentLoop("verifier-"+l.sessionID, l.workingDir, childOpts...)
	if child == nil {
		return "", fmt.Errorf("failed to create verifier agent loop")
	}

	// Run the verifier with a simple prompt that triggers the system prompt.
	result, err := child.RunOnce(ctx, "Verify the implementation described in your system prompt.", "verifier")
	if err != nil {
		return "", fmt.Errorf("verifier run failed: %w", err)
	}

	return result, nil
}
