package learning

import "math"

// ScoreExample computes a heuristic quality score in [0.0, 1.0] for a
// research trajectory based on task success, research usage, multi-hop
// reasoning, and user feedback.
func ScoreExample(traj ResearchTrajectory) float64 {
	score := 0.5 // Base score

	// Task success bonus
	if traj.TaskOutcome.Success {
		score += 0.2
	}

	// Research was used bonus
	if traj.WasResearchUsed() {
		score += 0.15
	}

	// Multi-hop reasoning bonus
	if len(traj.ToolCalls) > 1 {
		score += 0.1
	}

	// User feedback (applied via `meept learning feedback`).
	switch traj.TaskOutcome.UserFeedback {
	case FeedbackPositive:
		score += 0.15
	case FeedbackNegative:
		score -= 0.2
	}

	if score < 0 {
		return 0
	}
	return math.Min(score, 1.0)
}
