// Package q provides the Q Agent (Quartermaster) - a meta-agent for agent creation and optimization.
// It analyzes session transcripts to identify opportunities for creating new specialized agents
// or improving existing ones.
package q

import (
	"time"
)

// SessionAnalysis represents the analysis of a completed session.
type SessionAnalysis struct {
	SessionID       string
	StartTime       time.Time
	EndTime         time.Time
	Duration        time.Duration
	IterationCount  int
	AgentSwitches   int
	RevisionCycles  int
	TokenUsage      int
	ToolCalls       []ToolCallRecord
	DifficultyScore float64    // 0.0-1.0
	AnomalyFlags    []string   // "long_duration", "high_iterations", "agent_thrashing"
	Intents         []string   // Intent types observed
	Outcome         string     // "completed", "failed", "abandoned"
	AgentID         string     // Primary agent that handled the session
}

// ToolCallRecord represents a single tool invocation during a session.
type ToolCallRecord struct {
	ToolName    string
	Timestamp   time.Time
	Success     bool
	Duration    time.Duration
	ErrorMessage string `json:",omitempty"`
}

// PatternReport represents a detected pattern across sessions.
type PatternReport struct {
	ID              string
	PatternType     string             // "model_misconfiguration", "high_error_rate", "wrong_agent", "high_tool_failure", "high_rejection", "repeated_failure"
	Confidence      float64            // 0.0-1.0
	Evidence        []PatternEvidence
	RecommendedAction string           // "create_agent", "update_spec", "add_skill", "reassign_model"
	MisconfigurationType string        // Type of misconfiguration detected
	AffectedAgent   string
	AffectedIntent  string
	SessionCount    int
	MetricBaseline  float64
	MetricObserved  float64
	CreatedAt       time.Time
}

// PatternEvidence represents supporting evidence for a pattern.
type PatternEvidence struct {
	SessionID   string
	Metric      string
	Value       float64
	Description string
}

// ResearchReport represents a deep-dive analysis report.
type ResearchReport struct {
	ID                string
	PatternReportID   string
	ResearchType      string // "behavioral", "implementation", "tooling", "capability", "model_fit"
	RootCause         string
	EvidenceChain     []EvidenceLink
	ConfidenceScore   float64
	Recommendations   []Recommendation
	CreatedAt         time.Time
}

// EvidenceLink links to supporting evidence.
type EvidenceLink struct {
	Type        string // "transcript", "tool_call", "error_log", "memory"
	Reference   string // ID or path
	Description string
}

// Recommendation represents an actionable recommendation.
type Recommendation struct {
	Type             string // "new_agent", "new_skill", "update_spec", "add_tool", "update_prompt"
	Title            string
	Description      string
	Priority         string // "high", "medium", "low"
	ExpectedImpact   string
	Implementation   ImplementationDetails
}

// ImplementationDetails describes how to implement a recommendation.
type ImplementationDetails struct {
	FilesToCreate  []FileSpec
	FilesToModify  []FileModification
	Commands       []string
	AgentSpec      *AgentDesign // For new agent recommendations
	SkillSpec     *SkillDesign // For new skill recommendations
}

// FileSpec represents a file to be created.
type FileSpec struct {
	Path    string
	Content string
}

// FileModification represents a file to be modified.
type FileModification struct {
	Path        string
	Section     string
	NewContent  string
	LineNumber  int
}

// AgentDesign represents a generated agent specification.
type AgentDesign struct {
	ID                   string
	Name                 string
	Role                 string // "executor", "reviewer", "specialist"
	Purpose              string
	Model                string
	AdditionalTools      []string
	Capabilities         []string
	Constraints          AgentConstraints
	SystemPromptSections []string
}

// AgentConstraints represents operational limits for an agent.
type AgentConstraints struct {
	MaxIterations       int
	TimeoutSeconds      int
	MaxTokensPerTurn    int
	MaxMemoryRefs       int
	Temperature         *float64
}

// ImpactEstimate represents the estimated impact of a recommendation.
type ImpactEstimate struct {
	RecommendationID  string
	MetricType        string // "time_saved", "error_reduction", "iteration_reduction"
	BaselineValue     float64
	ExpectedValue     float64
	ImprovementPercent float64
	WeeklyImpact      string // Human-readable impact description
	Confidence        float64
}

// AnalysisResult represents the complete result of a Q Agent analysis run.
type AnalysisResult struct {
	ID                 string
	AnalyzedAt         time.Time
	SessionsAnalyzed   int
	PatternsDetected   []PatternReport
	ResearchReports    []ResearchReport
	Recommendations    []Recommendation
	ImpactEstimates    []ImpactEstimate
	Summary            string
	Status             string // "completed", "partial", "failed"
	ErrorMessage       string `json:",omitempty"`
}

// SessionStore defines the interface for accessing session data.
type SessionStore interface {
	GetCompletedSessions(since time.Time, limit int) ([]SessionData, error)
	GetSessionTranscript(sessionID string) ([]Message, error)
}

// SessionData represents metadata about a completed session.
type SessionData struct {
	SessionID  string
	StartTime  time.Time
	EndTime    time.Time
	Intents    []string
	AgentID    string
	Outcome    string
	Metrics    SessionMetrics
}

// SessionMetrics holds session performance metrics.
type SessionMetrics struct {
	Duration       time.Duration
	Iterations     int
	TokenUsage     int
	ToolCalls      int
	AgentSwitches  int
	Errors         int
	Revisions      int
}

// Message represents a conversation message.
type Message struct {
	Role      string
	Content   string
	Timestamp time.Time
	ToolCalls []ToolCall `json:",omitempty"`
}

// ToolCall represents a tool invocation in a message.
type ToolCall struct {
	Name      string
	Arguments string
	Result    string `json:",omitempty"`
	Success   bool
}

// SkillDesign represents a generated skill specification (Claude Code compatible).
type SkillDesign struct {
	ID              string
	Name            string
	Description     string
	TriggerKeywords []string
	Tools           []string
	ShellCommands   []string
	SystemPrompt    string
}
