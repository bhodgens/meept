package agent

// Step prompt path fidelity (03-context/02 T3, F-NEW-9; gate T5
// TestStepPromptFidelity).
//
// The orchestrator step the executor receives must carry the FULL user
// prompt's explicit details. The planner LLM's decomposition can drop
// them: the live 8B emitted "validate the requested file and report its
// contents" for a prompt that named an explicit file path, and the
// executing loop -- whose prompt is the step description -- answered
// with a clarification question for info already in the prompt.
//
// ensureStepPromptFidelity re-injects the verbatim user request into the
// first step when the decomposition lost an explicit path the input
// names. It is additive and idempotent: steps that already carry every
// explicit path are untouched; direct-mode steps (req.Input verbatim)
// never trigger it.

import (
	"strings"

	"github.com/caimlas/meept/internal/task"
)

// stepFidelityMarker delimits the re-injected original request inside a
// step description (idempotence guard on replan paths).
const stepFidelityMarker = "## Original user request"

// explicitPathTokens extracts absolute-path-looking tokens from the
// user input. A token qualifies when it starts with "/" (Unix absolute)
// or a Windows drive prefix and is longer than the bare root -- the
// same shape a user pastes into a prompt ("/tmp/x/y.txt").
func explicitPathTokens(input string) []string {
	var paths []string
	for _, tok := range strings.Fields(input) {
		tok = strings.Trim(tok, "\"'`()[]{}<>,;")
		if len(tok) < 2 {
			continue
		}
		if strings.HasPrefix(tok, "/") {
			paths = append(paths, tok)
			continue
		}
		if len(tok) > 3 && tok[1] == ':' && (tok[2] == '\\' || tok[2] == '/') &&
			((tok[0] >= 'a' && tok[0] <= 'z') || (tok[0] >= 'A' && tok[0] <= 'Z')) {
			paths = append(paths, tok)
		}
	}
	return paths
}

// ensureStepPromptFidelity appends the verbatim user request to the
// first step when the planner's decomposition dropped an explicit path
// the full input names. No-op when the input names no path, every path
// already appears in the step, or the marker is present.
func (sp *StrategicPlanner) ensureStepPromptFidelity(req PlanRequest, step *task.TaskStep) {
	if step == nil || req.Input == "" {
		return
	}
	if strings.Contains(step.Description, stepFidelityMarker) {
		return
	}
	paths := explicitPathTokens(req.Input)
	if len(paths) == 0 {
		return
	}
	missing := false
	for _, p := range paths {
		if !strings.Contains(step.Description, p) {
			missing = true
			break
		}
	}
	if !missing {
		return
	}
	step.Description = step.Description + "\n\n" + stepFidelityMarker +
		"\n\n" + req.Input
	sp.logger.Info("Original user request re-injected into step (planner dropped an explicit path)",
		"task_id", req.TaskID,
		"step_id", step.ID,
		"paths", len(paths),
	)
}
