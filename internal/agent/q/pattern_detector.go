package q

import (
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
)

// PatternDetector identifies recurring problems across sessions.
type PatternDetector struct {
	logger *slog.Logger
	config PatternDetectorConfig
}

// PatternDetectorConfig holds configuration for the PatternDetector.
type PatternDetectorConfig struct {
	MinSessionsForPattern      int
	HighErrorRateThreshold     float64
	HighRejectionRateThreshold float64
	DurationVarianceThreshold  float64
}

// NewPatternDetector creates a new PatternDetector.
func NewPatternDetector(logger *slog.Logger, config PatternDetectorConfig) *PatternDetector {
	return &PatternDetector{
		logger: logger,
		config: config,
	}
}

// DetectPatterns analyzes session analyses to find patterns.
func (d *PatternDetector) DetectPatterns(analyses []*SessionAnalysis) []PatternReport {
	reports := make([]PatternReport, 0, len(analyses))

	// Group analyses by agent
	byAgent := d.groupByAgent(analyses)

	// Group analyses by intent
	byIntent := d.groupByIntent(analyses)

	// Detect model misconfiguration patterns
	reports = append(reports, d.detectModelMisconfiguration(byAgent)...)

	// Detect high error rate patterns
	reports = append(reports, d.detectHighErrorRate(byAgent)...)

	// Detect wrong agent assignment patterns
	reports = append(reports, d.detectWrongAgentAssignment(byIntent)...)

	// Detect high tool failure rate patterns
	reports = append(reports, d.detectHighToolFailureRate(analyses)...)

	// Detect high rejection rate patterns
	reports = append(reports, d.detectHighRejectionRate(byAgent)...)

	// Detect repeated failure patterns
	reports = append(reports, d.detectRepeatedFailure(analyses)...)

	// Detect skill opportunity patterns (repetitive deterministic tasks)
	reports = append(reports, d.detectSkillOpportunity(analyses)...)

	// Filter by minimum sessions and confidence
	return d.filterReports(reports)
}

// groupByAgent groups analyses by agent ID.
func (d *PatternDetector) groupByAgent(analyses []*SessionAnalysis) map[string][]*SessionAnalysis {
	byAgent := make(map[string][]*SessionAnalysis)
	for _, a := range analyses {
		if a.AgentID != "" {
			byAgent[a.AgentID] = append(byAgent[a.AgentID], a)
		}
	}
	return byAgent
}

// groupByIntent groups analyses by dominant intent.
func (d *PatternDetector) groupByIntent(analyses []*SessionAnalysis) map[string][]*SessionAnalysis {
	byIntent := make(map[string][]*SessionAnalysis)
	for _, a := range analyses {
		if len(a.Intents) > 0 {
			for _, intent := range a.Intents {
				byIntent[intent] = append(byIntent[intent], a)
			}
		}
	}
	return byIntent
}

// detectModelMisconfiguration detects when the same task type has 3x duration variance across models.
func (d *PatternDetector) detectModelMisconfiguration(byAgent map[string][]*SessionAnalysis) []PatternReport {
	reports := make([]PatternReport, 0)

	for agentID, sessions := range byAgent {
		if len(sessions) < d.config.MinSessionsForPattern {
			continue
		}

		// Group by intent and compute duration stats
		intentDurations := make(map[string][]time.Duration)
		for _, s := range sessions {
			for _, intent := range s.Intents {
				intentDurations[intent] = append(intentDurations[intent], s.Duration)
			}
		}

		for intent, durations := range intentDurations {
			if len(durations) < 3 {
				continue
			}

			// Compute variance
			avg := d.averageDuration(durations)
			variance := d.durationVariance(durations, avg)

			// Check if variance exceeds threshold
			if variance > d.config.DurationVarianceThreshold {
				reports = append(reports, PatternReport{
					ID:                   fmt.Sprintf("model_misconfig_%s_%s", agentID, intent),
					PatternType:          PatternModelMisconfiguration,
					Confidence:           min(1.0, variance/d.config.DurationVarianceThreshold),
					RecommendedAction:    ActionReassignModel,
					MisconfigurationType: PatternModelMisconfiguration,
					AffectedAgent:        agentID,
					AffectedIntent:       intent,
					SessionCount:         len(durations),
					MetricBaseline:       avg.Seconds(),
					MetricObserved:       variance,
					Evidence:             d.buildDurationEvidence(sessions, intent),
					CreatedAt:            time.Now(),
				})
			}
		}
	}

	return reports
}

// detectHighErrorRate detects agents with error rate > 2x platform average.
func (d *PatternDetector) detectHighErrorRate(byAgent map[string][]*SessionAnalysis) []PatternReport {
	reports := make([]PatternReport, 0)

	for agentID, sessions := range byAgent {
		if len(sessions) < d.config.MinSessionsForPattern {
			continue
		}

		// Count sessions with errors
		errorCount := 0
		for _, s := range sessions {
			if s.AnomalyFlags != nil && containsAnomaly(s.AnomalyFlags, "high_revisions") {
				errorCount++
			}
		}

		errorRate := float64(errorCount) / float64(len(sessions))

		if errorRate > d.config.HighErrorRateThreshold {
			reports = append(reports, PatternReport{
				ID:                   fmt.Sprintf("high_error_%s", agentID),
				PatternType:          PatternHighErrorRate,
				Confidence:           min(1.0, errorRate/d.config.HighErrorRateThreshold),
				RecommendedAction:    ActionUpdateSpec,
				MisconfigurationType: PatternHighErrorRate,
				AffectedAgent:        agentID,
				SessionCount:         len(sessions),
				MetricBaseline:       d.config.HighErrorRateThreshold,
				MetricObserved:       errorRate,
				Evidence:             d.buildErrorEvidence(sessions),
				CreatedAt:            time.Now(),
			})
		}
	}

	return reports
}

// detectWrongAgentAssignment detects tasks requiring capabilities the agent lacks.
func (d *PatternDetector) detectWrongAgentAssignment(byIntent map[string][]*SessionAnalysis) []PatternReport {
	reports := make([]PatternReport, 0)

	for intent, sessions := range byIntent {
		if len(sessions) < d.config.MinSessionsForPattern {
			continue
		}

		// Check for high difficulty scores with multiple agents
		agentDifficulty := make(map[string]float64)
		for _, s := range sessions {
			agentDifficulty[s.AgentID] += s.DifficultyScore
		}

		// If multiple agents struggle with same intent, might need specialist
		strugglingAgents := 0
		for _, avgDifficulty := range agentDifficulty {
			if avgDifficulty > 0.6 {
				strugglingAgents++
			}
		}

		if strugglingAgents >= 2 {
			reports = append(reports, PatternReport{
				ID:                   fmt.Sprintf("wrong_agent_%s", intent),
				PatternType:          "wrong_agent_assignment",
				Confidence:           min(1.0, float64(strugglingAgents)*0.4),
				RecommendedAction:    "create_agent",
				MisconfigurationType: "capability_gap",
				AffectedIntent:       intent,
				SessionCount:         len(sessions),
				Evidence:             d.buildIntentEvidence(sessions, intent),
				CreatedAt:            time.Now(),
			})
		}
	}

	return reports
}

// detectHighToolFailureRate detects tool call failure rate > 20%.
func (d *PatternDetector) detectHighToolFailureRate(analyses []*SessionAnalysis) []PatternReport {
	reports := make([]PatternReport, 0)

	// Aggregate tool call stats by tool name
	toolStats := make(map[string]struct {
		total    int
		failures int
	})

	for _, a := range analyses {
		for _, tc := range a.ToolCalls {
			stats := toolStats[tc.ToolName]
			stats.total++
			if !tc.Success {
				stats.failures++
			}
			toolStats[tc.ToolName] = stats
		}
	}

	for toolName, stats := range toolStats {
		if stats.total < 5 {
			continue
		}

		failureRate := float64(stats.failures) / float64(stats.total)

		if failureRate > 0.2 {
			reports = append(reports, PatternReport{
				ID:                   fmt.Sprintf("high_tool_failure_%s", toolName),
				PatternType:          "high_tool_failure_rate",
				Confidence:           min(1.0, failureRate/0.2),
				RecommendedAction:    ActionAddTool,
				MisconfigurationType: "tool_deficiency",
				AffectedIntent:       toolName,
				SessionCount:         stats.total,
				MetricBaseline:       0.2,
				MetricObserved:       failureRate,
				CreatedAt:            time.Now(),
			})
		}
	}

	return reports
}

// detectHighRejectionRate detects coder rejection by reviewer > 30% of time.
func (d *PatternDetector) detectHighRejectionRate(byAgent map[string][]*SessionAnalysis) []PatternReport {
	reports := make([]PatternReport, 0)

	for agentID, sessions := range byAgent {
		if len(sessions) < d.config.MinSessionsForPattern {
			continue
		}

		// Count high revision cycles as proxy for rejection
		rejectionCount := 0
		for _, s := range sessions {
			if s.RevisionCycles > 2 {
				rejectionCount++
			}
		}

		rejectionRate := float64(rejectionCount) / float64(len(sessions))

		if rejectionRate > d.config.HighRejectionRateThreshold {
			reports = append(reports, PatternReport{
				ID:                   fmt.Sprintf("high_rejection_%s", agentID),
				PatternType:          "high_rejection_rate",
				Confidence:           min(1.0, rejectionRate/d.config.HighRejectionRateThreshold),
				RecommendedAction:    "update_spec",
				MisconfigurationType: "prompt_deficiency",
				AffectedAgent:        agentID,
				SessionCount:         len(sessions),
				MetricBaseline:       d.config.HighRejectionRateThreshold,
				MetricObserved:       rejectionRate,
				Evidence:             d.buildRejectionEvidence(sessions),
				CreatedAt:            time.Now(),
			})
		}
	}

	return reports
}

// detectRepeatedFailure detects same intent failing 3+ times with same agent.
func (d *PatternDetector) detectRepeatedFailure(analyses []*SessionAnalysis) []PatternReport {
	reports := make([]PatternReport, 0)

	// Group by intent + agent
	intentAgentKey := make(map[string][]*SessionAnalysis)
	for _, a := range analyses {
		for _, intent := range a.Intents {
			key := fmt.Sprintf("%s:%s", intent, a.AgentID)
			intentAgentKey[key] = append(intentAgentKey[key], a)
		}
	}

	for key, sessions := range intentAgentKey {
		if len(sessions) < 3 {
			continue
		}

		// Count failures (outcome = "failed" or high difficulty)
		failureCount := 0
		for _, s := range sessions {
			if s.Outcome == "failed" || s.DifficultyScore > 0.8 {
				failureCount++
			}
		}

		if failureCount >= 3 {
			reports = append(reports, PatternReport{
				ID:                   fmt.Sprintf("repeated_failure_%s", key),
				PatternType:          "repeated_failure",
				Confidence:           min(1.0, float64(failureCount)/float64(len(sessions))+0.3),
				RecommendedAction:    "create_agent",
				MisconfigurationType: "capability_gap",
				AffectedAgent:        sessions[0].AgentID,
				AffectedIntent:       sessions[0].Intents[0],
				SessionCount:         len(sessions),
				Evidence:             d.buildFailureEvidence(sessions),
				CreatedAt:            time.Now(),
			})
		}
	}

	return reports
}

// filterReports filters reports by minimum sessions and confidence.
func (d *PatternDetector) filterReports(reports []PatternReport) []PatternReport {
	filtered := make([]PatternReport, 0)

	for _, r := range reports {
		if r.SessionCount >= d.config.MinSessionsForPattern && r.Confidence >= 0.5 {
			filtered = append(filtered, r)
		}
	}

	// Sort by confidence descending
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Confidence > filtered[j].Confidence
	})

	return filtered
}

// Helper functions

func (d *PatternDetector) averageDuration(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	var total time.Duration
	for _, dur := range durations {
		total += dur
	}
	return total / time.Duration(len(durations))
}

func (d *PatternDetector) durationVariance(durations []time.Duration, avg time.Duration) float64 {
	if len(durations) < 2 {
		return 0
	}

	var sumSquaredDiff float64
	for _, dur := range durations {
		diff := float64(dur - avg)
		sumSquaredDiff += diff * diff
	}

	variance := sumSquaredDiff / float64(len(durations)-1)
	stddev := sqrt(variance)

	return stddev / float64(avg)
}

func (d *PatternDetector) buildDurationEvidence(sessions []*SessionAnalysis, intent string) []PatternEvidence {
	evidence := make([]PatternEvidence, 0, len(sessions))
	for _, s := range sessions {
		for _, i := range s.Intents {
			if i == intent {
				evidence = append(evidence, PatternEvidence{
					SessionID:   s.SessionID,
					Metric:      "duration",
					Value:       s.Duration.Seconds(),
					Description: fmt.Sprintf("Session took %v to complete", s.Duration),
				})
			}
		}
	}
	return evidence
}

func (d *PatternDetector) buildErrorEvidence(sessions []*SessionAnalysis) []PatternEvidence {
	evidence := make([]PatternEvidence, 0)
	for _, s := range sessions {
		if containsAnomaly(s.AnomalyFlags, "high_revisions") {
			evidence = append(evidence, PatternEvidence{
				SessionID:   s.SessionID,
				Metric:      "revisions",
				Value:       float64(s.RevisionCycles),
				Description: fmt.Sprintf("Session had %d revision cycles", s.RevisionCycles),
			})
		}
	}
	return evidence
}

func (d *PatternDetector) buildIntentEvidence(sessions []*SessionAnalysis, intent string) []PatternEvidence {
	evidence := make([]PatternEvidence, 0)
	for _, s := range sessions {
		for _, i := range s.Intents {
			if i == intent {
				evidence = append(evidence, PatternEvidence{
					SessionID:   s.SessionID,
					Metric:      "difficulty",
					Value:       s.DifficultyScore,
					Description: fmt.Sprintf("Difficulty score: %.2f", s.DifficultyScore),
				})
			}
		}
	}
	return evidence
}

func (d *PatternDetector) buildRejectionEvidence(sessions []*SessionAnalysis) []PatternEvidence {
	evidence := make([]PatternEvidence, 0)
	for _, s := range sessions {
		if s.RevisionCycles > 2 {
			evidence = append(evidence, PatternEvidence{
				SessionID:   s.SessionID,
				Metric:      "revision_cycles",
				Value:       float64(s.RevisionCycles),
				Description: fmt.Sprintf("%d revision cycles indicates rejection", s.RevisionCycles),
			})
		}
	}
	return evidence
}

func (d *PatternDetector) buildFailureEvidence(sessions []*SessionAnalysis) []PatternEvidence {
	evidence := make([]PatternEvidence, 0)
	for _, s := range sessions {
		if s.Outcome == "failed" || s.DifficultyScore > 0.8 {
			evidence = append(evidence, PatternEvidence{
				SessionID:   s.SessionID,
				Metric:      "outcome",
				Value:       s.DifficultyScore,
				Description: fmt.Sprintf("Session outcome: %s, difficulty: %.2f", s.Outcome, s.DifficultyScore),
			})
		}
	}
	return evidence
}

func containsAnomaly(flags []string, target string) bool {
	return slices.Contains(flags, target)
}

func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	return math.Sqrt(x)
}

// detectSkillOpportunity detects repetitive deterministic tasks suitable for skill automation.
// Skills are preferred when:
// - Same shell commands executed repeatedly
// - Deterministic output (no LLM reasoning needed)
// - High success rate but time-consuming
func (d *PatternDetector) detectSkillOpportunity(analyses []*SessionAnalysis) []PatternReport {
	reports := make([]PatternReport, 0)

	// Also look for repeated tool call sequences
	intentCommands := make(map[string]int)
	for _, a := range analyses {
		toolKey := strings.Join(extractToolNames(a.ToolCalls), "-")
		if toolKey != "" {
			intentCommands[toolKey]++
		}
	}

	for toolPattern, count := range intentCommands {
		if count >= 5 {
			reports = append(reports, PatternReport{
				ID:                   fmt.Sprintf("skill_opportunity_%s", toolPattern),
				PatternType:          "skill_opportunity",
				Confidence:           min(1.0, float64(count)/10.0),
				RecommendedAction:    "add_skill",
				MisconfigurationType: "automation_opportunity",
				AffectedIntent:       toolPattern,
				SessionCount:         count,
				MetricBaseline:       5,
				MetricObserved:       float64(count),
				Evidence: []PatternEvidence{
					{
						Metric:      "repetition_count",
						Value:       float64(count),
						Description: fmt.Sprintf("Same tool pattern executed %d times", count),
					},
				},
				CreatedAt: time.Now(),
			})
		}
	}

	return reports
}

// extractToolNames extracts tool names from tool call records.
func extractToolNames(calls []ToolCallRecord) []string {
	names := make([]string, 0, len(calls))
	for _, tc := range calls {
		names = append(names, tc.ToolName)
	}
	return names
}
