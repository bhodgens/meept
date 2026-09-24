package agent

import "strings"

// replanDigestMaxChars is the hard ceiling on the replan request payload the
// escalation path hands to the strategic planner. The 2026-09-18 e2e run
// showed a replan loop embedding the FULL prior step result (and, transitively,
// the whole conversation) into each fresh plan request: 27 unbacked-claims
// nudges and context regrowth until llama.cpp returned 500
// "Context size has been exceeded". The planner needs the failure's shape —
// task description, step list, one-line reason — never the transcript.
const replanDigestMaxChars = 2000

// buildReplanDigest renders a compact, bounded replan summary for the
// strategic planner. It carries: the task description, the completed/remaining
// step split, and a FIRST-LINE-ONLY failure reason truncated to ~400 chars.
// It deliberately never includes the failing step's result text or any
// conversation transcript. The output is capped at replanDigestMaxChars, so
// callers can pin the bound without re-measuring.
func buildReplanDigest(taskDesc string, completed, remaining []string, failureReason string) string {
	var sb strings.Builder
	// The literal "RE-PLAN" marker is load-bearing: replan fallback steps
	// carry req.Input verbatim and downstream logic/tests identify replan
	// steps by this marker. Keep it as the digest's first token.
	sb.WriteString("RE-PLAN: ")
	addBounded := func(label, s string, max int) {
		if s == "" {
			return
		}
		sb.WriteString(label)
		sb.WriteString(truncateRunes(s, max, ""))
		sb.WriteByte('\n')
	}

	addBounded("Task: ", taskDesc, 500)
	if len(completed) > 0 {
		sb.WriteString("Completed steps (do not redo):\n")
		for _, d := range completed {
			sb.WriteString("  - ")
			sb.WriteString(truncateRunes(firstLine(d), 150, ""))
			sb.WriteByte('\n')
		}
	}
	if len(remaining) > 0 {
		sb.WriteString("Remaining (uncompleted) steps to retry or finish:\n")
		for _, d := range remaining {
			sb.WriteString("  - ")
			sb.WriteString(truncateRunes(firstLine(d), 150, ""))
			sb.WriteByte('\n')
		}
	}
	sb.WriteString("Failure: ")
	sb.WriteString(truncateRunes(firstLine(failureReason), 400, "…"))
	sb.WriteByte('\n')

	out := sb.String()
	if len(out) <= replanDigestMaxChars {
		return out
	}
	return truncateRunes(out, replanDigestMaxChars-1, "…")
}

// failureStepErrorMaxChars caps each failed step's error text inside the
// replan failure block (issue #58 capability 2). The planner must see the
// CONCRETE failure — which tool/args produced which error — but a full
// step result can be kilobytes of transcript (F-B4), so the text is
// bounded per step.
const failureStepErrorMaxChars = 400

// failureBlockMaxChars is the hard ceiling on the whole failure block so
// many failed steps cannot regrow the replan prompt unbounded.
const failureBlockMaxChars = 2000

// stepFailure describes one concrete step failure for the replan context.
type stepFailure struct {
	Description string
	Agent       string
	Error       string
}

// buildFailureBlock renders the structured "## Previous attempt failed"
// block attached to a failure-triggered replan's plan request context.
// Per failed step it carries the step description, the executing agent,
// and the step's error text truncated to failureStepErrorMaxChars — the
// concrete tool/args/error shape the digest's first-line-only "Failure:"
// line deliberately omits (e2e 2026-09-23 GiqWsG: the planner repeated
// `task_create: name is missing` schema-invalid calls because replans
// never saw the dead plan's failing calls). Returns "" when there are no
// failures; callers attach the block only when non-empty. Output is
// capped at failureBlockMaxChars.
func buildFailureBlock(failures []stepFailure) string {
	if len(failures) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Previous attempt failed\n")
	sb.WriteString("The previous plan's step(s) failed with these concrete errors; plan steps and tool arguments that avoid repeating them:\n")
	for _, f := range failures {
		sb.WriteString("- Step: ")
		sb.WriteString(truncateRunes(firstLine(f.Description), 200, "…"))
		sb.WriteByte('\n')
		if f.Agent != "" {
			sb.WriteString("  Agent: ")
			sb.WriteString(firstLine(f.Agent))
			sb.WriteByte('\n')
		}
		if f.Error != "" {
			sb.WriteString("  Error: ")
			sb.WriteString(truncateRunes(f.Error, failureStepErrorMaxChars, "…"))
			sb.WriteByte('\n')
		}
	}
	out := sb.String()
	if len(out) <= failureBlockMaxChars {
		return out
	}
	return truncateRunes(out, failureBlockMaxChars-1, "…")
}

// TestBuildReplanDigest_BoundedWithLargeStepResult is the F-B4 pin: with a
// multi-kilobyte step result as the failure reason, the digest must stay
// under replanDigestMaxChars and must not contain the result body.
