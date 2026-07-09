package learning

import "testing"

func TestScoreExample(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		traj     ResearchTrajectory
		minScore float64
		maxScore float64
	}{
		{
			name:     "bare trajectory",
			traj:     ResearchTrajectory{},
			minScore: 0.5,
			maxScore: 0.5,
		},
		{
			name: "successful task with research used",
			traj: ResearchTrajectory{
				TaskOutcome: TaskOutcome{Success: true},
				ToolCalls: []ToolCallRecord{
					{Used: true},
				},
			},
			minScore: 0.85,
			maxScore: 0.85,
		},
		{
			name: "everything positive",
			traj: ResearchTrajectory{
				TaskOutcome: TaskOutcome{Success: true, UserFeedback: "positive"},
				ToolCalls: []ToolCallRecord{
					{Used: true},
					{Used: true},
				},
			},
			minScore: 1.0,
			maxScore: 1.0,
		},
		{
			name: "multi-hop only",
			traj: ResearchTrajectory{
				ToolCalls: []ToolCallRecord{
					{Used: false},
					{Used: false},
				},
			},
			minScore: 0.6,
			maxScore: 0.6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ScoreExample(tt.traj)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("ScoreExample() = %f, want between %f and %f", score, tt.minScore, tt.maxScore)
			}
		})
	}
}

func TestScoreExampleNeverExceedsOne(t *testing.T) {
	t.Parallel()

	traj := ResearchTrajectory{
		TaskOutcome: TaskOutcome{Success: true, UserFeedback: "positive"},
		ToolCalls: []ToolCallRecord{
			{Used: true},
			{Used: true},
			{Used: true},
		},
	}
	score := ScoreExample(traj)
	if score > 1.0 {
		t.Errorf("score %f exceeds 1.0", score)
	}
}
