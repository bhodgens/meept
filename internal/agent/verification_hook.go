package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

const verificationNudge = `NOTE: You just made 3+ file-modifying changes. Before writing your final summary, verify your work: run the tests, execute the code, check the output. You cannot self-assign PARTIAL by listing caveats — either verify or report what could not be verified.`

// Compile-time interface check.
var _ PrepareNextTurnHook = (*VerificationAutoTrigger)(nil)

// VerificationAutoTrigger is a PrepareNextTurnHook that injects a verification
// nudge message when the tracked file-modifying tool call count reaches the
// configured threshold.
type VerificationAutoTrigger struct {
	tracker *VerificationTracker
	config  VerificationConfig
}

// NewVerificationAutoTrigger wires a tracker and verification config into a
// hook ready for registration with the HookRegistry.
func NewVerificationAutoTrigger(tracker *VerificationTracker, config VerificationConfig) *VerificationAutoTrigger {
	return &VerificationAutoTrigger{tracker: tracker, config: config}
}

// PrepareNextTurn implements PrepareNextTurnHook. It returns a nudge message
// when verification is enabled, auto-trigger is on, and the threshold is met.
func (h *VerificationAutoTrigger) PrepareNextTurn(_ context.Context, _ TurnState) TurnModification {
	if !h.config.Enabled || !h.config.AutoTrigger {
		return TurnModification{}
	}
	if !h.tracker.ShouldTrigger() {
		return TurnModification{}
	}
	filesChanged := h.tracker.Snapshot()
	nudge := fmt.Sprintf("%s\n\nFiles changed:\n%s", verificationNudge, formatFileList(filesChanged))
	return TurnModification{
		Modified: true,
		ExtraMessages: []llm.ChatMessage{{
			Role:    llm.RoleUser,
			Content: nudge,
		}},
		Reason: "verification auto-trigger",
	}
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
