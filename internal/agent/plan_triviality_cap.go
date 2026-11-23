package agent

// Triviality escape cap (06-planning/03 T4, F13c/P4): a trivial input
// whose planner output over-decomposes (the frozen P4 shape: a "what is
// 2+2?" question planned in plan mode produced a 21-step decomposition)
// must be capped BEFORE the approval gate counts steps -- 2+2 never asks
// for approval.
//
// The heuristic is deliberately CONSERVATIVE (leaf review checklist): it
// fires only when the INPUT itself is trivial (isTrivialQuestion, the
// 04-clutter/02 arithmetic-question heuristic -- structural checks only,
// never an LLM call) AND the planned decomposition exceeds one step. A
// short but genuine artifact request ("create a file named notes.txt
// with the summary") fails isTrivialQuestion's imperative-verb veto, so
// real work keeps its plan and gates normally.
//
// The cap keeps the FIRST step only: a trivial question needs a single
// step (its answer), and the first step is the one carrying the session
// context / prompt-fidelity injection the execution path relies on.

import (
	"github.com/caimlas/meept/internal/task"
)

// capTrivialPlanSteps reduces an over-decomposed plan for a trivial
// input to a single step before the approval gate counts it. Complex
// inputs pass through untouched (nil-safe on steps).
func (sp *StrategicPlanner) capTrivialPlanSteps(req PlanRequest, steps []*task.TaskStep) []*task.TaskStep {
	if len(steps) <= 1 {
		return steps
	}
	if !isTrivialQuestion(req.Input) {
		return steps
	}
	sp.recordMetric("strategic_planner.triviality_cap", 1, map[string]string{
		"intent": req.Intent,
	})
	sp.logger.Info("Triviality escape: capped over-decomposed plan to a single step",
		"task_id", req.TaskID,
		"steps_before", len(steps),
	)
	return steps[:1]
}
