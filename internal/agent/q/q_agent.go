// Package q provides the Q Agent (Quartermaster) - a meta-agent for agent creation and optimization.
package q

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory/memvid"
)

// QAgent is the main orchestrator for Q Agent analysis.
//nolint:revive // stutter with package name is intentional for API clarity
type QAgent struct {
	logger          *slog.Logger
	config          config.QAgentConfig
	memvidClient    *memvid.Client
	sessionAnalyzer *SessionAnalyzer
	patternDetector *PatternDetector
	researchEngine  *ResearchEngine
	agentDesigner   *AgentDesigner
	skillDesigner   *SkillDesigner
	impactEstimator *ImpactEstimator
	reviewer        *ReviewerValidator
}

// NewQAgent creates a new Q Agent orchestrator.
func NewQAgent(logger *slog.Logger, cfg config.QAgentConfig, memvidClient *memvid.Client) *QAgent {
	sessionAnalyzer := NewSessionAnalyzer(
		memvidClient,
		logger,
		SessionAnalyzerConfig{
			SessionIdleTriggerHours: cfg.SessionIdleTriggerHours,
		},
	)

	patternDetector := NewPatternDetector(
		logger,
		PatternDetectorConfig{
			MinSessionsForPattern:      cfg.MinSessionsForPattern,
			HighErrorRateThreshold:     cfg.HighErrorRateThreshold,
			HighRejectionRateThreshold: cfg.HighRejectionRateThreshold,
			DurationVarianceThreshold:  cfg.DurationVarianceThreshold,
		},
	)

	researchEngine := NewResearchEngine(
		memvidClient,
		logger,
		ResearchEngineConfig{
			AnalysisTimeoutMinutes: cfg.AnalysisTimeoutMinutes,
		},
	)

	agentDesigner := NewAgentDesigner(
		logger,
		AgentDesignerConfig{},
	)

	skillDesigner := NewSkillDesigner(
		logger,
		SkillDesignerConfig{
			SkillsDir: "~/.meept/skills",
		},
	)

	impactEstimator := NewImpactEstimator(
		logger,
		ImpactEstimatorConfig{
			WeeklySessionsEstimate: cfg.MinSessionsForPattern * 2,
			AverageTokenCost:       0.001,
			LaborCostPerMinute:     0.50,
		},
	)

	reviewer := NewReviewerValidator(
		logger,
		memvidClient,
	)

	return &QAgent{
		logger:          logger,
		config:          cfg,
		memvidClient:    memvidClient,
		sessionAnalyzer: sessionAnalyzer,
		patternDetector: patternDetector,
		researchEngine:  researchEngine,
		agentDesigner:   agentDesigner,
		skillDesigner:   skillDesigner,
		impactEstimator: impactEstimator,
		reviewer:        reviewer,
	}
}

// RunAnalysis runs a complete Q Agent analysis cycle.
func (q *QAgent) RunAnalysis(ctx context.Context) (*AnalysisResult, error) {
	q.logger.Info("starting Q Agent analysis cycle")

	sessions, err := q.fetchCompletedSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch sessions: %w", err)
	}

	if len(sessions) == 0 {
		return &AnalysisResult{
			AnalyzedAt:       time.Now(),
			SessionsAnalyzed: 0,
			Status:           "completed",
			Summary:          "No completed sessions found for analysis",
		}, nil
	}

	q.logger.Info("fetched sessions for analysis", "count", len(sessions))

	sessionIDs := make([]string, 0, len(sessions))
	for _, s := range sessions {
		sessionIDs = append(sessionIDs, s.SessionID)
	}

	analyses, err := q.sessionAnalyzer.AnalyzeMultipleSessions(ctx, sessionIDs)
	if err != nil {
		q.logger.Warn("session analysis encountered errors", "error", err)
	}

	q.logger.Info("completed session analysis", "analyzed", len(analyses))

	patterns := q.patternDetector.DetectPatterns(analyses)
	q.logger.Info("detected patterns", "count", len(patterns))

	researchReports := make([]*ResearchReport, 0, len(patterns))
	for _, pattern := range patterns {
		report := q.researchEngine.ConductResearch(ctx, pattern, analyses)
		researchReports = append(researchReports, report)
	}

	designs := make([]*AgentDesign, 0)
	for i, pattern := range patterns {
		if i < len(researchReports) && len(researchReports[i].Recommendations) > 0 {
			design := q.agentDesigner.DesignAgent(pattern, researchReports[i], analyses)
			designs = append(designs, design)
		}
	}

	impactEstimates := make([]*ImpactEstimate, 0)
	for i, pattern := range patterns {
		if i < len(researchReports) && len(researchReports[i].Recommendations) > 0 {
			for _, rec := range researchReports[i].Recommendations {
				estimate := q.impactEstimator.EstimateImpact(pattern, researchReports[i], rec)
				impactEstimates = append(impactEstimates, estimate)
			}
		}
	}

	// Validate recommendations through reviewer
	ctxReviewer, cancel := context.WithTimeout(ctx, 5*time.Minute)
	initialRecs := make([]Recommendation, 0)
	for _, rr := range researchReports {
		if rr != nil {
			initialRecs = append(initialRecs, rr.Recommendations...)
		}
	}
	validationResults, err := q.reviewer.ValidateRecommendations(ctxReviewer, initialRecs, researchReports)
	cancel()
	if err != nil {
		q.logger.Warn("recommendation validation failed", "error", err)
	}

	// Filter out rejected recommendations
	validatedRecs := make([]Recommendation, 0)
	for i, rec := range initialRecs {
		if i < len(validationResults) && validationResults[i].Status != "rejected" {
			validatedRecs = append(validatedRecs, rec)
			if err := q.reviewer.LogValidationResult(ctx, rec, validationResults[i]); err != nil {
				q.logger.Debug("failed to log validation result", "error", err)
			}
		} else if i < len(validationResults) {
			q.logger.Info("recommendation rejected by reviewer",
				"title", rec.Title,
				"reason", validationResults[i].Feedback)
		}
	}

	result := q.compileResults(analyses, patterns, researchReports, designs, impactEstimates, validatedRecs)

	if err := q.saveArtifacts(result, designs); err != nil {
		q.logger.Warn("failed to save analysis artifacts", "error", err)
	}

	if err := q.logOutcome(result); err != nil {
		q.logger.Warn("failed to log outcome", "error", err)
	}

	q.logger.Info("Q Agent analysis cycle completed",
		"sessions", result.SessionsAnalyzed,
		"patterns", len(patterns),
		"recommendations", len(result.Recommendations),
	)

	return result, nil
}

// fetchCompletedSessions fetches completed sessions from memvid.
func (q *QAgent) fetchCompletedSessions(ctx context.Context) ([]SessionData, error) {
	cutoffTime := time.Now().Add(-time.Duration(q.config.SessionIdleTriggerHours) * time.Hour)

	memories, err := q.memvidClient.Search(ctx, fmt.Sprintf("session:complete before:%s", cutoffTime.Format(time.RFC3339)), 100)
	if err != nil {
		return nil, fmt.Errorf("memvid search failed: %w", err)
	}

	sessions := make([]SessionData, 0, len(memories))
	for _, mem := range memories {
		session := q.parseSessionData(mem)
		if session.SessionID != "" {
			sessions = append(sessions, session)
		}
	}

	return sessions, nil
}

// parseSessionData parses a SessionData from memvid memory.
func (q *QAgent) parseSessionData(mem memvid.MemoryResult) SessionData {
	session := SessionData{
		Metrics: SessionMetrics{},
	}

	if id := mem.Memory.ID; id != "" {
		session.SessionID = id
	}
	if intents, ok := mem.Memory.Metadata["intents"].([]string); ok {
		session.Intents = intents
	}
	if agentID, ok := mem.Memory.Metadata["agent_id"].(string); ok {
		session.AgentID = agentID
	}
	if outcome, ok := mem.Memory.Metadata["outcome"].(string); ok {
		session.Outcome = outcome
	}
	if startTime, ok := mem.Memory.Metadata["start_time"].(string); ok {
		if t, err := time.Parse(time.RFC3339, startTime); err == nil {
			session.StartTime = t
		}
	}
	if endTime, ok := mem.Memory.Metadata["end_time"].(string); ok {
		if t, err := time.Parse(time.RFC3339, endTime); err == nil {
			session.EndTime = t
		}
	}
	if duration, ok := mem.Memory.Metadata["duration_seconds"].(float64); ok {
		session.Metrics.Duration = time.Duration(duration) * time.Second
	}
	if iterations, ok := mem.Memory.Metadata["iterations"].(float64); ok {
		session.Metrics.Iterations = int(iterations)
	}
	if tokenUsage, ok := mem.Memory.Metadata["token_usage"].(float64); ok {
		session.Metrics.TokenUsage = int(tokenUsage)
	}
	if toolCalls, ok := mem.Memory.Metadata["tool_calls"].(float64); ok {
		session.Metrics.ToolCalls = int(toolCalls)
	}
	if agentSwitches, ok := mem.Memory.Metadata["agent_switches"].(float64); ok {
		session.Metrics.AgentSwitches = int(agentSwitches)
	}
	if errors, ok := mem.Memory.Metadata["errors"].(float64); ok {
		session.Metrics.Errors = int(errors)
	}
	if revisions, ok := mem.Memory.Metadata["revisions"].(float64); ok {
		session.Metrics.Revisions = int(revisions)
	}

	return session
}

// compileResults compiles the analysis result.
func (q *QAgent) compileResults(
	analyses []*SessionAnalysis,
	patterns []PatternReport,
	researchReports []*ResearchReport,
	_ []*AgentDesign,
	impactEstimates []*ImpactEstimate,
	recommendations []Recommendation,
) *AnalysisResult {
	researchReportValues := make([]ResearchReport, 0, len(researchReports))
	for _, rr := range researchReports {
		if rr != nil {
			researchReportValues = append(researchReportValues, *rr)
		}
	}

	impactEstimateValues := make([]ImpactEstimate, 0, len(impactEstimates))
	for _, ie := range impactEstimates {
		if ie != nil {
			impactEstimateValues = append(impactEstimateValues, *ie)
		}
	}

	result := &AnalysisResult{
		ID:               fmt.Sprintf("analysis_%s", time.Now().Format("2006-01-02")),
		AnalyzedAt:       time.Now(),
		SessionsAnalyzed: len(analyses),
		PatternsDetected: patterns,
		ResearchReports:  researchReportValues,
		Recommendations:  recommendations,
		ImpactEstimates:  impactEstimateValues,
		Status:           "completed",
	}

	result.Summary = q.generateSummary(result)
	return result
}

// generateSummary generates a human-readable summary.
func (q *QAgent) generateSummary(result *AnalysisResult) string {
	summary := fmt.Sprintf("Analyzed %d sessions. ", result.SessionsAnalyzed)

	if len(result.PatternsDetected) == 0 {
		summary += "No significant patterns detected. All systems nominal."
		return summary
	}

	summary += fmt.Sprintf("Found %d improvement opportunities: ", len(result.PatternsDetected))

	parts := make([]string, 0)
	limit := minInt(len(result.PatternsDetected), 3)
	for i, pattern := range result.PatternsDetected[:limit] {
		parts = append(parts, fmt.Sprintf("(%d) %s - %.0f%% confidence", i+1, pattern.PatternType, pattern.Confidence*100))
	}

	return summary + strings.Join(parts, ", ")
}

// saveArtifacts saves analysis artifacts to disk.
func (q *QAgent) saveArtifacts(result *AnalysisResult, designs []*AgentDesign) error {
	dir := expandPath(q.config.AnalysisDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create analysis directory: %w", err)
	}

	reportPath := filepath.Join(dir, fmt.Sprintf("%s_analysis.json", result.ID))
	reportData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}
	if err := os.WriteFile(reportPath, reportData, 0644); err != nil {
		return fmt.Errorf("failed to write report: %w", err)
	}
	q.logger.Info("saved analysis report", "path", reportPath)

	for _, rec := range result.Recommendations {
		if rec.Type == "new_skill" && rec.Implementation.SkillSpec != nil {
			skillDir := filepath.Join(dir, "skills", rec.Implementation.SkillSpec.ID)
			if err := os.MkdirAll(skillDir, 0755); err != nil {
				q.logger.Warn("failed to create skill directory", "error", err)
				continue
			}
			skillFile := filepath.Join(skillDir, "SKILL.md")
			content := q.skillDesigner.GenerateFullSkillFile(rec.Implementation.SkillSpec)
			if err := os.WriteFile(skillFile, []byte(content), 0644); err != nil {
				q.logger.Warn("failed to write skill file", "error", err)
			}
			q.logger.Info("saved skill", "path", skillFile)
		}
	}

	for _, design := range designs {
		agentDir := filepath.Join(dir, "agents", design.ID)
		if err := os.MkdirAll(agentDir, 0755); err != nil {
			return fmt.Errorf("failed to create agent directory: %w", err)
		}

		agentFile := filepath.Join(agentDir, "AGENT.md")
		content := q.agentDesigner.GenerateFullAgentFile(design)
		if err := os.WriteFile(agentFile, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write agent spec: %w", err)
		}
		q.logger.Info("saved agent specification", "path", agentFile)
	}

	return nil
}

// logOutcome logs the analysis outcome to the outcomes log.
func (q *QAgent) logOutcome(result *AnalysisResult) error {
	logPath := expandPath(q.config.OutcomesLog)

	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create outcomes directory: %w", err)
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open outcomes log: %w", err)
	}
	defer f.Close()

	entry := map[string]any{
		"timestamp":         result.AnalyzedAt,
		"analysis_id":       result.ID,
		"sessions_analyzed": result.SessionsAnalyzed,
		"patterns_detected": len(result.PatternsDetected),
		"recommendations":   len(result.Recommendations),
		"status":            result.Status,
		"summary":           result.Summary,
	}

	encoder := json.NewEncoder(f)
	if err := encoder.Encode(entry); err != nil {
		return fmt.Errorf("failed to encode outcome: %w", err)
	}

	return nil
}

// GetStatus returns the current status of Q Agent analysis.
func (q *QAgent) GetStatus(ctx context.Context) (*QAgentStatus, error) {
	memvidHealthy := q.memvidClient.IsAvailable(ctx)

	sessionCount := 0
	if memvidHealthy {
		memories, err := q.memvidClient.Search(ctx, "session:", 1)
		if err == nil && len(memories) > 0 {
			sessionCount = len(memories)
		}
	}

	return &QAgentStatus{
		Enabled:       q.config.Enabled,
		MemvidHealthy: memvidHealthy,
		SessionCount:  sessionCount,
		AnalysisDir:   q.config.AnalysisDir,
		OutcomesLog:   q.config.OutcomesLog,
		Config:        q.config,
	}, nil
}

// QAgentStatus represents the status of the Q Agent.
//nolint:revive // stutter with package name is intentional for API clarity
type QAgentStatus struct {
	Enabled       bool
	MemvidHealthy bool
	SessionCount  int
	AnalysisDir   string
	OutcomesLog   string
	Config        config.QAgentConfig
}

func expandPath(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return path
	}

	if path == "~" {
		return homeDir
	}
	return filepath.Join(homeDir, path[2:])
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
