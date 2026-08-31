// Package learning implements the LoRA learning pipeline for capturing
// agent research trajectories, scoring them, and producing domain-routed
// training datasets.
package learning

import "time"

// ResearchTrajectory captures a complete research action.
type ResearchTrajectory struct {
	ID          string           `json:"id"`
	SessionID   string           `json:"session_id"`
	Domain      string           `json:"domain"` // code, debugging, api_research
	Intent      string           `json:"intent"` // Original user intent
	Query       string           `json:"query"`  // Research query
	ToolCalls   []ToolCallRecord `json:"tool_calls"`
	Synthesis   string           `json:"synthesis"` // Agent's final answer
	TaskOutcome TaskOutcome      `json:"task_outcome"`
	Timestamp   time.Time        `json:"timestamp"`
}

// ToolCallRecord records a single tool invocation within a trajectory.
type ToolCallRecord struct {
	Tool    string `json:"tool"`
	Query   string `json:"query"`
	Results int    `json:"results_count"`
	Used    bool   `json:"used"` // Was this result actually used?
}

// TaskOutcome records whether the overall task succeeded.
type TaskOutcome struct {
	Success      bool    `json:"success"`
	Quality      float64 `json:"quality"`  // 0.0-1.0
	UserFeedback string  `json:"feedback"` // Optional user feedback
}

// TrainingExample is the JSONL format for training.
type TrainingExample struct {
	Instruction string          `json:"instruction"`
	Input       string          `json:"input"`
	Output      string          `json:"output"`
	Metadata    ExampleMetadata `json:"metadata"`
}

// ExampleMetadata holds provenance metadata for a training example.
type ExampleMetadata struct {
	Source       string   `json:"source"` // "agent_research"
	Domain       string   `json:"domain"`
	SessionID    string   `json:"session_id"`
	ToolPath     []string `json:"tool_path"`
	QualityScore float64  `json:"quality_score"`
	Timestamp    string   `json:"timestamp"`
}

// WasResearchUsed returns true if any ToolCall in ToolCalls has Used=true.
func (t *ResearchTrajectory) WasResearchUsed() bool {
	for _, tc := range t.ToolCalls {
		if tc.Used {
			return true
		}
	}
	return false
}
