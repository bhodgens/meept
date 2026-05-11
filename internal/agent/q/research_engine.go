package q

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/memory/memvid"
)

// ResearchEngine performs deep-dive analysis on identified problems.
type ResearchEngine struct {
	memvidClient *memvid.Client
	logger       *slog.Logger
	config       ResearchEngineConfig
}

// ResearchEngineConfig holds configuration for the ResearchEngine.
type ResearchEngineConfig struct {
	AnalysisTimeoutMinutes int
}

// NewResearchEngine creates a new ResearchEngine.
func NewResearchEngine(client *memvid.Client, logger *slog.Logger, config ResearchEngineConfig) *ResearchEngine {
	return &ResearchEngine{
		memvidClient: client,
		logger:       logger,
		config:       config,
	}
}

// ResearchTypes defines the types of research analysis.
const (
	ResearchTypeBehavioral     = "behavioral"
	ResearchTypeImplementation = "implementation"
	ResearchTypeTooling        = "tooling"
	ResearchTypeCapability     = "capability"
	ResearchTypeModelFit       = "model_fit"
)

// ConductResearch performs deep-dive analysis on a pattern report.
func (e *ResearchEngine) ConductResearch(ctx context.Context, pattern PatternReport, analyses []*SessionAnalysis) *ResearchReport {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(e.config.AnalysisTimeoutMinutes)*time.Minute)
	defer cancel()

	report := &ResearchReport{
		ID:              fmt.Sprintf("research_%s", pattern.ID),
		PatternReportID: pattern.ID,
		ResearchType:    e.determineResearchType(pattern),
		CreatedAt:       time.Now(),
	}

	// Apply causal attribution decision tree
	rootCause, evidence := e.applyCausalAttribution(ctx, pattern, analyses)

	report.RootCause = rootCause
	report.EvidenceChain = evidence
	report.ConfidenceScore = e.computeConfidence(pattern, evidence)
	report.Recommendations = e.generateRecommendations(pattern, rootCause, analyses)

	return report
}

// determineResearchType determines the type of research based on pattern.
func (e *ResearchEngine) determineResearchType(pattern PatternReport) string {
	switch pattern.PatternType {
	case "model_misconfiguration":
		return ResearchTypeModelFit
	case "high_error_rate", "high_rejection_rate":
		return ResearchTypeBehavioral
	case "high_tool_failure_rate":
		return ResearchTypeTooling
	case "wrong_agent_assignment", "repeated_failure":
		return ResearchTypeCapability
	default:
		return ResearchTypeImplementation
	}
}

// applyCausalAttribution applies the causal attribution decision tree.
// Decision tree:
// 1. Wrong agent? -> Task required capability not in agent's purpose
// 2. Wrong model? -> Model lacks required capability (code, reasoning, tool_use)
// 3. Missing tool? -> Tool call failed with "not found" or capability gap
// 4. Bad prompt? -> Agent output shows confusion or goes off-track
// 5. Missing memory? -> Relevant memories exist but not injected
func (e *ResearchEngine) applyCausalAttribution(ctx context.Context, pattern PatternReport, analyses []*SessionAnalysis) (string, []EvidenceLink) {
	switch pattern.MisconfigurationType {
	case "capability_gap":
		return e.analyzeCapabilityGap(pattern, analyses)
	case "model_misconfiguration":
		return e.analyzeModelMisconfiguration(pattern, analyses)
	case "tool_deficiency":
		return e.analyzeToolDeficiency(pattern, analyses)
	case "prompt_deficiency":
		return e.analyzePromptDeficiency(pattern, analyses)
	case "high_error_rate":
		return e.analyzeHighErrorRate(pattern, analyses)
	default:
		return e.analyzeGeneric(pattern, analyses)
	}
}

// analyzeCapabilityGap analyzes capability gap issues.
func (e *ResearchEngine) analyzeCapabilityGap(pattern PatternReport, analyses []*SessionAnalysis) (string, []EvidenceLink) {
	evidence := make([]EvidenceLink, 0)

	// Check if multiple agents struggle with same intent
	agentStruggles := make(map[string]int)
	for _, a := range analyses {
		if a.DifficultyScore > 0.6 {
			agentStruggles[a.AgentID]++
		}
	}

	if len(agentStruggles) >= 2 {
		for agentID, count := range agentStruggles {
			evidence = append(evidence, EvidenceLink{
				Type:        "transcript",
				Reference:   agentID,
				Description: fmt.Sprintf("Agent %s struggled with %d sessions of this type", agentID, count),
			})
		}

		rootCause := fmt.Sprintf("No current agent has specialized capability for intent pattern: %s. Multiple agents show difficulty scores > 0.6", pattern.AffectedIntent)
		return rootCause, evidence
	}

	return "Capability gap detected but evidence is inconclusive", evidence
}

// analyzeModelMisconfiguration analyzes model misconfiguration issues.
func (e *ResearchEngine) analyzeModelMisconfiguration(pattern PatternReport, analyses []*SessionAnalysis) (string, []EvidenceLink) {
	evidence := make([]EvidenceLink, 0)

	// Check duration variance
	var longSessions, shortSessions []*SessionAnalysis
	avgDuration := e.averageDurationFromAnalyses(analyses)

	for _, a := range analyses {
		if a.Duration > avgDuration*2 {
			longSessions = append(longSessions, a)
		} else if a.Duration < avgDuration/2 {
			shortSessions = append(shortSessions, a)
		}
	}

	if len(longSessions) > 0 && len(shortSessions) > 0 {
		for _, s := range longSessions {
			evidence = append(evidence, EvidenceLink{
				Type:        "transcript",
				Reference:   s.SessionID,
				Description: fmt.Sprintf("Long session (%v) - may indicate model struggling", s.Duration),
			})
		}

		rootCause := fmt.Sprintf("Model assigned to agent lacks efficiency for task type %s. Duration variance indicates capability mismatch", pattern.AffectedIntent)
		return rootCause, evidence
	}

	return "Model may be misconfigured but variance could be due to task complexity", evidence
}

// analyzeToolDeficiency analyzes tool deficiency issues.
func (e *ResearchEngine) analyzeToolDeficiency(pattern PatternReport, analyses []*SessionAnalysis) (string, []EvidenceLink) {
	evidence := make([]EvidenceLink, 0)

	// Check for repeated tool call failures
	failedTools := make(map[string]int)
	for _, a := range analyses {
		for _, tc := range a.ToolCalls {
			if !tc.Success {
				failedTools[tc.ToolName]++
			}
		}
	}

	for toolName, count := range failedTools {
		if count >= 3 {
			evidence = append(evidence, EvidenceLink{
				Type:        "tool_call",
				Reference:   toolName,
				Description: fmt.Sprintf("Tool %s failed %d times across sessions", toolName, count),
			})
		}
	}

	if len(evidence) > 0 {
		rootCause := fmt.Sprintf("Tool %s has high failure rate. May need implementation fix or replacement", pattern.AffectedIntent)
		return rootCause, evidence
	}

	return "Tool deficiency detected but specific cause unclear", evidence
}

// analyzePromptDeficiency analyzes prompt deficiency issues.
func (e *ResearchEngine) analyzePromptDeficiency(pattern PatternReport, analyses []*SessionAnalysis) (string, []EvidenceLink) {
	evidence := make([]EvidenceLink, 0)

	// Check for high revision cycles
	for _, a := range analyses {
		if a.RevisionCycles > 2 {
			evidence = append(evidence, EvidenceLink{
				Type:        "transcript",
				Reference:   a.SessionID,
				Description: fmt.Sprintf("Session had %d revision cycles - possible prompt confusion", a.RevisionCycles),
			})
		}
	}

	if len(evidence) > 0 {
		rootCause := fmt.Sprintf("Agent %s shows high rejection rate. System prompt may lack clear output format requirements or quality standards", pattern.AffectedAgent)
		return rootCause, evidence
	}

	return "Prompt deficiency suspected but needs transcript analysis", evidence
}

// analyzeHighErrorRate analyzes high error rate issues.
func (e *ResearchEngine) analyzeHighErrorRate(pattern PatternReport, analyses []*SessionAnalysis) (string, []EvidenceLink) {
	// Analyze error patterns
	errorTypes := make(map[string]int)
	for _, a := range analyses {
		for _, flag := range a.AnomalyFlags {
			errorTypes[flag]++
		}
	}

	evidence := make([]EvidenceLink, 0, len(errorTypes))
	for errorType, count := range errorTypes {
		evidence = append(evidence, EvidenceLink{
			Type:        "error_log",
			Reference:   errorType,
			Description: fmt.Sprintf("Anomaly %s occurred %d times", errorType, count),
		})
	}

	rootCause := fmt.Sprintf("Agent %s shows elevated error rate. Review agent specification for missing instructions or inadequate constraints", pattern.AffectedAgent)
	return rootCause, evidence
}

// analyzeGeneric performs generic analysis when specific type is unclear.
func (e *ResearchEngine) analyzeGeneric(pattern PatternReport, analyses []*SessionAnalysis) (string, []EvidenceLink) {
	evidence := make([]EvidenceLink, 0, minInt(len(analyses), 5))

	// Collect general evidence
	for _, a := range analyses[:minInt(len(analyses), 5)] {
		evidence = append(evidence, EvidenceLink{
			Type:        "transcript",
			Reference:   a.SessionID,
			Description: fmt.Sprintf("Session difficulty: %.2f, duration: %v", a.DifficultyScore, a.Duration),
		})
	}

	return "Pattern detected but root cause requires further investigation", evidence
}

// computeConfidence computes confidence score based on evidence quality.
func (e *ResearchEngine) computeConfidence(pattern PatternReport, evidence []EvidenceLink) float64 {
	baseConfidence := pattern.Confidence

	// Boost confidence with more evidence
	evidenceBonus := minFloat(0.2, float64(len(evidence))*0.05)

	// Boost confidence with high-quality evidence types
	qualityBonus := 0.0
	for _, ev := range evidence {
		switch ev.Type {
		case "transcript":
			qualityBonus += 0.05
		case "tool_call":
			qualityBonus += 0.1
		case "error_log":
			qualityBonus += 0.08
		}
	}

	return minFloat(1.0, baseConfidence+evidenceBonus+qualityBonus)
}

// generateRecommendations generates recommendations based on root cause.
func (e *ResearchEngine) generateRecommendations(pattern PatternReport, rootCause string, analyses []*SessionAnalysis) []Recommendation {
	recommendations := make([]Recommendation, 0)

	switch pattern.RecommendedAction {
	case "create_agent":
		recommendations = append(recommendations, e.recommendNewAgent(pattern, rootCause))
	case "update_spec":
		recommendations = append(recommendations, e.recommendSpecUpdate(pattern, rootCause))
	case "reassign_model":
		recommendations = append(recommendations, e.recommendModelReassignment(pattern, rootCause))
	case "add_tool":
		recommendations = append(recommendations, e.recommendNewTool(pattern, rootCause))
	case "add_skill":
		recommendations = append(recommendations, e.recommendNewSkill(pattern, rootCause))
	}

	return recommendations
}

// recommendNewAgent generates a recommendation for creating a new agent.
func (e *ResearchEngine) recommendNewAgent(pattern PatternReport, rootCause string) Recommendation {
	return Recommendation{
		Type:           "new_agent",
		Title:          fmt.Sprintf("Create specialist agent for %s", pattern.AffectedIntent),
		Description:    fmt.Sprintf("Evidence suggests need for specialized agent: %s", rootCause),
		Priority:       e.determinePriority(pattern.Confidence, pattern.SessionCount),
		ExpectedImpact: fmt.Sprintf("Expected to reduce difficulty score by 40%% for %d sessions/week", pattern.SessionCount/4),
		Implementation: ImplementationDetails{
			FilesToCreate: []FileSpec{
				{
					Path:    fmt.Sprintf("~/.meept/agents/%s_specialist/AGENT.md", pattern.AffectedIntent),
					Content: e.generateAgentSpecContent(pattern),
				},
			},
		},
	}
}

// recommendSpecUpdate generates a recommendation for updating agent specification.
func (e *ResearchEngine) recommendSpecUpdate(pattern PatternReport, rootCause string) Recommendation {
	return Recommendation{
		Type:           "update_spec",
		Title:          fmt.Sprintf("Update %s agent specification", pattern.AffectedAgent),
		Description:    fmt.Sprintf("Agent spec needs improvement: %s", rootCause),
		Priority:       e.determinePriority(pattern.Confidence, pattern.SessionCount),
		ExpectedImpact: fmt.Sprintf("Expected to reduce rejection rate from %.0f%% to 10%%", pattern.MetricObserved*100),
		Implementation: ImplementationDetails{
			FilesToModify: []FileModification{
				{
					Path:       fmt.Sprintf("~/.meept/agents/%s/AGENT.md", pattern.AffectedAgent),
					Section:    "Quality Standards",
					LineNumber: 0, // Will be determined during implementation
				},
			},
		},
	}
}

// recommendModelReassignment generates a recommendation for model reassignment.
func (e *ResearchEngine) recommendModelReassignment(pattern PatternReport, rootCause string) Recommendation {
	return Recommendation{
		Type:           "reassign_model",
		Title:          fmt.Sprintf("Reassign model for %s tasks", pattern.AffectedIntent),
		Description:    fmt.Sprintf("Current model shows %dx variance for this task type", int(pattern.MetricObserved)),
		Priority:       "medium",
		ExpectedImpact: fmt.Sprintf("Expected to reduce duration variance by %.0f%%", (pattern.MetricObserved-1)*100),
		Implementation: ImplementationDetails{
			Commands: []string{
				fmt.Sprintf("meept models update %s --task-type %s", pattern.AffectedAgent, pattern.AffectedIntent),
			},
		},
	}
}

// recommendNewTool generates a recommendation for adding a new tool.
func (e *ResearchEngine) recommendNewTool(pattern PatternReport, rootCause string) Recommendation {
	return Recommendation{
		Type:           "add_tool",
		Title:          fmt.Sprintf("Create or fix tool: %s", pattern.AffectedIntent),
		Description:    fmt.Sprintf("Tool has high failure rate: %s", rootCause),
		Priority:       "high",
		ExpectedImpact: "Expected to reduce tool failure rate from 20%% to <5%%",
		Implementation: ImplementationDetails{
			FilesToCreate: []FileSpec{
				{
					Path:    fmt.Sprintf("internal/tools/%s.go", pattern.AffectedIntent),
					Content: "// Tool implementation placeholder\n",
				},
			},
		},
	}
}

// recommendNewSkill generates a recommendation for creating a new skill.
func (e *ResearchEngine) recommendNewSkill(pattern PatternReport, rootCause string) Recommendation {
	skillID := fmt.Sprintf("skill_%s", pattern.AffectedIntent)
	return Recommendation{
		Type:           "new_skill",
		Title:          fmt.Sprintf("Create Claude Code skill: %s", pattern.AffectedIntent),
		Description:    fmt.Sprintf("Deterministic task pattern detected: %s", rootCause),
		Priority:       e.determinePriority(pattern.Confidence, pattern.SessionCount),
		ExpectedImpact: fmt.Sprintf("Expected to automate %d repetitive sessions/week", pattern.SessionCount/4),
		Implementation: ImplementationDetails{
			SkillSpec: &SkillDesign{
				ID:              skillID,
				Name:            fmt.Sprintf("%s Skill", pattern.AffectedIntent),
				Description:     fmt.Sprintf("Automated skill for handling %s tasks", pattern.AffectedIntent),
				TriggerKeywords: []string{pattern.AffectedIntent},
			},
		},
	}
}

// determinePriority determines recommendation priority.
func (e *ResearchEngine) determinePriority(confidence float64, sessionCount int) string {
	if confidence > 0.8 && sessionCount > 10 {
		return "high"
	} else if confidence > 0.6 && sessionCount > 5 {
		return "medium"
	}
	return "low"
}

// generateAgentSpecContent generates AGENT.md content for a new specialist agent.
func (e *ResearchEngine) generateAgentSpecContent(pattern PatternReport) string {
	var buf strings.Builder

	fmt.Fprintf(&buf, `---
id: %s_specialist
name: %s Specialist Agent
role: executor
purpose: |
  You are a %s specialist agent. Your responsibilities:
  1. Handle all tasks related to %s
  2. Apply domain-specific expertise for efficient resolution
  3. Escalate tasks outside your specialty to the dispatcher

  You do NOT handle: general chat, tasks unrelated to %s

additional_tools:
  - memory_search
  - file_read
  - shell_execute
capabilities:
  - reasoning
  - tool_use
max_iterations: 15
timeout_seconds: 300
temperature: 0.3
---

# %s Specialist Agent Baseline Instructions

## Scope and Boundaries
You specialize in %s tasks. When you receive a task:
1. Verify it falls within your specialty
2. If yes, proceed with execution
3. If no, escalate to dispatcher immediately

## Required Output Format
Always structure responses as:
- Summary: Brief overview of what was done
- Details: Step-by-step execution
- Result: Final outcome

## Escalation Triggers
Escalate when:
- Task requires capabilities you don't have
- Multiple retry attempts fail
- User requests human review

## Quality Standards
- Complete tasks in minimal iterations
- Document all tool calls and their results
- Verify results before reporting success
`, pattern.AffectedIntent, pattern.AffectedIntent, pattern.AffectedIntent, pattern.AffectedIntent, pattern.AffectedIntent, pattern.AffectedIntent, pattern.AffectedIntent)

	return buf.String()
}

// Helper functions

func (e *ResearchEngine) averageDurationFromAnalyses(analyses []*SessionAnalysis) time.Duration {
	if len(analyses) == 0 {
		return 0
	}
	var total time.Duration
	for _, a := range analyses {
		total += a.Duration
	}
	return total / time.Duration(len(analyses))
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
