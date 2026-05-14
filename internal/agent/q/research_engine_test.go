package q

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

var researchLogger = slog.Default()

func TestResearchEngineDetermineResearchType(t *testing.T) {
	tests := []struct {
		patternType  string
		wantResearch string
	}{
		{patternType: "model_misconfiguration", wantResearch: ResearchTypeModelFit},
		{patternType: "high_error_rate", wantResearch: ResearchTypeBehavioral},
		{patternType: "high_rejection_rate", wantResearch: ResearchTypeBehavioral},
		{patternType: "high_tool_failure_rate", wantResearch: ResearchTypeTooling},
		{patternType: "wrong_agent_assignment", wantResearch: ResearchTypeCapability},
		{patternType: "repeated_failure", wantResearch: ResearchTypeCapability},
		{patternType: "unknown_type", wantResearch: ResearchTypeImplementation},
	}

	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{})

	for _, tt := range tests {
		t.Run(tt.patternType, func(t *testing.T) {
			pattern := PatternReport{PatternType: tt.patternType}
			got := eng.determineResearchType(pattern)
			if got != tt.wantResearch {
				t.Errorf("determineResearchType(%q) = %q, want %q", tt.patternType, got, tt.wantResearch)
			}
		})
	}
}

func TestResearchEngineComputeConfidence(t *testing.T) {
	tests := []struct {
		name          string
		baseConf      float64
		evidenceCount int
		evidenceTypes []string
		wantMin       float64
		wantMax       float64
	}{
		{
			name:          "low confidence, no evidence",
			baseConf:      0.5,
			evidenceCount: 0,
			wantMin:       0.5,
			wantMax:       0.5,
		},
		{
			name:          "medium confidence with transcript evidence",
			baseConf:      0.6,
			evidenceCount: 2,
			evidenceTypes: []string{"transcript", "transcript"},
			wantMin:       0.7,
			wantMax:       0.85,
		},
		{
			name:          "high confidence with tool_call evidence",
			baseConf:      0.8,
			evidenceCount: 3,
			evidenceTypes: []string{"tool_call", "transcript", "error_log"},
			wantMin:       1.0,
			wantMax:       1.0,
		},
		{
			name:          "clamped at 1.0 max",
			baseConf:      1.0,
			evidenceCount: 10,
			wantMin:       1.0,
			wantMax:       1.0,
		},
	}

	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := PatternReport{Confidence: tt.baseConf}

			evidence := make([]EvidenceLink, 0, tt.evidenceCount)
			for i := range tt.evidenceCount {
				typ := "transcript"
				if i < len(tt.evidenceTypes) {
					typ = tt.evidenceTypes[i]
				}
				evidence = append(evidence, EvidenceLink{Type: typ})
			}

			confidence := eng.computeConfidence(pattern, evidence)
			if confidence < tt.wantMin || confidence > tt.wantMax {
				t.Errorf("computeConfidence() = %.2f, wanted in [%.2f, %.2f]", confidence, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestResearchEngineGenerateRecommendations(t *testing.T) {
	pattern := PatternReport{
		PatternType:       "wrong_agent_assignment",
		Confidence:        0.85,
		AffectedIntent:    "debugging",
		AffectedAgent:     "coder",
		SessionCount:      20,
		RecommendedAction: "create_agent",
	}

	researchReport := &ResearchReport{
		RootCause: "No current agent has specialized capability for intent pattern: debugging",
	}

	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{})
	recs := eng.generateRecommendations(pattern, researchReport.RootCause, nil)

	if len(recs) == 0 {
		t.Error("generateRecommendations() returned no recommendations")
	}

	if recs[0].Type != "new_agent" {
		t.Errorf("expected recommendation type 'new_agent', got %q", recs[0].Type)
	}

	if recs[0].Priority != "high" {
		t.Errorf("expected priority 'high', got %q", recs[0].Priority)
	}
}

func TestResearchEngineDeterminePriority(t *testing.T) {
	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{})

	tests := []struct {
		confidence float64
		sessions   int
		want       string
	}{
		{0.9, 15, "high"},
		{0.85, 11, "high"},
		{0.7, 6, "medium"},
		{0.65, 6, "medium"},
		{0.61, 4, "low"},
		{0.5, 2, "low"},
		{0.3, 1, "low"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := eng.determinePriority(tt.confidence, tt.sessions)
			if got != tt.want {
				t.Errorf("determinePriority(%.1f, %d) = %q, want %q", tt.confidence, tt.sessions, got, tt.want)
			}
		})
	}
}

func TestResearchEngineAnalyzeCapabilityGap(t *testing.T) {
	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{})

	pattern := PatternReport{
		MisconfigurationType: "capability_gap",
		AffectedIntent:       "debugging",
	}

	sessions := []*SessionAnalysis{
		{AgentID: "coder", DifficultyScore: 0.7},
		{AgentID: "chat", DifficultyScore: 0.8},
		{AgentID: "analyst", DifficultyScore: 0.5},
	}

	rootCause, evidence := eng.analyzeCapabilityGap(pattern, sessions)

	if len(evidence) < 2 {
		t.Errorf("expected >= 2 evidence links for capability gap, got %d", len(evidence))
	}

	if rootCause == "" {
		t.Error("rootCause should not be empty")
	}
}

func TestResearchEngineAnalyzeCapabilityGapSingleAgent(t *testing.T) {
	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{})

	pattern := PatternReport{
		MisconfigurationType: "capability_gap",
		AffectedIntent:       "debugging",
	}

	sessions := []*SessionAnalysis{
		{AgentID: "coder", DifficultyScore: 0.7},
	}

	rootCause, evidence := eng.analyzeCapabilityGap(pattern, sessions)

	if rootCause == "" {
		t.Error("rootCause should not be empty even for single agent")
	}
	_ = evidence
}

func TestResearchEngineAnalyzeModelMisconfiguration(t *testing.T) {
	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{})

	pattern := PatternReport{AffectedIntent: "planning"}

	sessions := []*SessionAnalysis{
		{Duration: 5 * time.Minute, SessionID: "s1"},
		{Duration: 10 * time.Minute, SessionID: "s2"},
		{Duration: 60 * time.Minute, SessionID: "s3"},
		{Duration: 8 * time.Minute, SessionID: "s4"},
	}

	rootCause, evidence := eng.analyzeModelMisconfiguration(pattern, sessions)

	if rootCause == "" {
		t.Error("rootCause should not be empty")
	}
	if len(evidence) == 0 {
		t.Error("expected evidence entries for long sessions")
	}
}

func TestResearchEngineAnalyzeModelNoVariance(t *testing.T) {
	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{})

	pattern := PatternReport{AffectedIntent: "planning"}

	sessions := []*SessionAnalysis{
		{Duration: 5 * time.Minute},
		{Duration: 5 * time.Minute},
		{Duration: 5 * time.Minute},
	}

	rootCause, _ := eng.analyzeModelMisconfiguration(pattern, sessions)
	if rootCause == "" {
		t.Error("rootCause should not be empty")
	}
}

func TestResearchEngineAnalyzeToolDeficiency(t *testing.T) {
	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{})

	pattern := PatternReport{AffectedIntent: "shell_execute"}

	sessions := []*SessionAnalysis{
		{
			ToolCalls: []ToolCallRecord{
				{ToolName: "shell_execute", Success: false},
				{ToolName: "shell_execute", Success: false},
				{ToolName: "shell_execute", Success: false},
			},
		},
		{
			ToolCalls: []ToolCallRecord{
				{ToolName: "shell_execute", Success: false},
			},
		},
	}

	rootCause, evidence := eng.analyzeToolDeficiency(pattern, sessions)

	if rootCause == "" {
		t.Error("rootCause should not be empty")
	}
	if len(evidence) == 0 {
		t.Error("expected evidence for tool deficiency")
	}
}

func TestResearchEngineAnalyzePromptDeficiency(t *testing.T) {
	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{})

	pattern := PatternReport{AffectedAgent: "coder"}

	sessions := []*SessionAnalysis{
		{RevisionCycles: 3},
		{RevisionCycles: 2},
		{RevisionCycles: 5},
	}

	rootCause, evidence := eng.analyzePromptDeficiency(pattern, sessions)

	if rootCause == "" {
		t.Error("rootCause should not be empty")
	}
	if len(evidence) == 0 {
		t.Error("expected evidence entries for high revisions")
	}
}

func TestResearchEngineAnalyzeHighErrorRate(t *testing.T) {
	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{})

	pattern := PatternReport{AffectedAgent: "coder"}

	sessions := []*SessionAnalysis{
		{AnomalyFlags: []string{"long_duration", "high_iterations"}},
		{AnomalyFlags: []string{"high_revisions"}},
		{AnomalyFlags: []string{}},
	}

	rootCause, evidence := eng.analyzeHighErrorRate(pattern, sessions)

	if rootCause == "" {
		t.Error("rootCause should not be empty")
	}
	if len(evidence) != 3 {
		t.Errorf("expected 3 evidence entries, got %d", len(evidence))
	}
}

func TestResearchEngineAnalyzeGeneric(t *testing.T) {
	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{})

	pattern := PatternReport{}

	sessions := []*SessionAnalysis{
		{SessionID: "s1", DifficultyScore: 0.5},
		{SessionID: "s2", DifficultyScore: 0.6},
	}

	rootCause, evidence := eng.analyzeGeneric(pattern, sessions)

	if rootCause == "" {
		t.Error("rootCause should not be empty")
	}
	if len(evidence) != 2 {
		t.Errorf("expected 2 evidence entries, got %d", len(evidence))
	}
}

func TestResearchEngineApplyCausalAttribution(t *testing.T) {
	tests := []struct {
		name                 string
		misconfigurationType string
	}{
		{"capability gap", "capability_gap"},
		{"model misconfiguration", "model_misconfiguration"},
		{"tool deficiency", "tool_deficiency"},
		{"prompt deficiency", "prompt_deficiency"},
		{"high error rate", "high_error_rate"},
		{"default/generic", "unknown_type"},
	}

	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := PatternReport{MisconfigurationType: tt.misconfigurationType}
			rootCause, _ := eng.applyCausalAttribution(context.Background(), pattern, nil)

			if rootCause == "" {
				t.Errorf("rootCause should not be empty for %s", tt.name)
			}
		})
	}
}

func TestResearchEngineAverageDurationFromAnalyses(t *testing.T) {
	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{})

	tests := []struct {
		name     string
		sessions []*SessionAnalysis
		want     time.Duration
	}{
		{name: "nil input", sessions: nil, want: 0},
		{name: "empty slice", sessions: []*SessionAnalysis{}, want: 0},
		{name: "single session", sessions: []*SessionAnalysis{{Duration: 5 * time.Minute}}, want: 5 * time.Minute},
		{name: "multiple sessions", sessions: []*SessionAnalysis{{Duration: 5 * time.Minute}, {Duration: 10 * time.Minute}, {Duration: 15 * time.Minute}}, want: 10 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eng.averageDurationFromAnalyses(tt.sessions)
			if got != tt.want {
				t.Errorf("averageDurationFromAnalyses() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResearchEngineConductResearch(t *testing.T) {
	ctx := context.Background()
	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{AnalysisTimeoutMinutes: 30})

	pattern := PatternReport{
		ID:                   "test_pattern_1",
		PatternType:          "wrong_agent_assignment",
		Confidence:           0.85,
		AffectedIntent:       "debugging",
		RecommendedAction:    "create_agent",
		MisconfigurationType: "capability_gap",
		SessionCount:         10,
	}

	sessions := []*SessionAnalysis{
		{AgentID: "coder", DifficultyScore: 0.7},
		{AgentID: "chat", DifficultyScore: 0.8},
	}

	report := eng.ConductResearch(ctx, pattern, sessions)

	if report.ID == "" {
		t.Error("report ID should not be empty")
	}
	if report.ResearchType == "" {
		t.Error("researchType should not be empty")
	}
	if report.ConfidenceScore <= 0 {
		t.Errorf("confidenceScore should be > 0, got %.2f", report.ConfidenceScore)
	}
	if report.RootCause == "" {
		t.Error("rootCause should not be empty")
	}
}

func TestResearchEngineRecommendNewAgent(t *testing.T) {
	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{})

	pattern := PatternReport{
		AffectedIntent: "debugging",
		Confidence:     0.9,
		SessionCount:   15,
	}

	rec := eng.recommendNewAgent(pattern, "Need specialized debug agent")

	if rec.Type != "new_agent" {
		t.Errorf("expected type 'new_agent', got %q", rec.Type)
	}
	if rec.Priority != "high" {
		t.Errorf("expected priority 'high', got %q", rec.Priority)
	}
	if len(rec.Implementation.FilesToCreate) == 0 {
		t.Error("expected FilesToCreate for new_agent recommendation")
	}
}

func TestResearchEngineRecommendSpecUpdate(t *testing.T) {
	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{})

	pattern := PatternReport{
		AffectedAgent: "coder",
		Confidence:    0.7,
		SessionCount:  5,
	}

	rec := eng.recommendSpecUpdate(pattern, "Agent spec needs improvement")

	if rec.Type != "update_spec" {
		t.Errorf("expected type 'update_spec', got %q", rec.Type)
	}
	if len(rec.Implementation.FilesToModify) == 0 {
		t.Error("expected FilesToModify for update_spec recommendation")
	}
}

func TestResearchEngineRecommendModelReassignment(t *testing.T) {
	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{})

	pattern := PatternReport{
		AffectedIntent:    "planning",
		MetricObserved:    4.0,
		RecommendedAction: "reassign_model",
	}

	rec := eng.recommendModelReassignment(pattern, "Model lacks efficiency")

	if rec.Type != "reassign_model" {
		t.Errorf("expected type 'reassign_model', got %q", rec.Type)
	}
	if len(rec.Implementation.Commands) == 0 {
		t.Error("expected Commands for model reassignment")
	}
}

func TestResearchEngineRecommendNewTool(t *testing.T) {
	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{})

	pattern := PatternReport{
		AffectedIntent:    "shell_execute",
		RecommendedAction: "add_tool",
	}

	rec := eng.recommendNewTool(pattern, "Tool has high failure rate")

	if rec.Type != "add_tool" {
		t.Errorf("expected type 'add_tool', got %q", rec.Type)
	}
	if rec.Priority != "high" {
		t.Errorf("expected priority 'high', got %q", rec.Priority)
	}
}

func TestResearchEngineGenerateAgentSpecContent(t *testing.T) {
	eng := NewResearchEngine(nil, researchLogger, ResearchEngineConfig{})

	pattern := PatternReport{AffectedIntent: "debugging"}

	content := eng.generateAgentSpecContent(pattern)

	if content == "" {
		t.Fatal("generateAgentSpecContent() returned empty content")
	}
	if !containsStrQ(content, "debugging_specialist") {
		t.Error("expected agent ID 'debugging_specialist' in content")
	}
	if !containsStrQ(content, "debugging Specialist Agent") {
		t.Error("expected agent name in content")
	}
	if !containsStrQ(content, "Scope and Boundaries") {
		t.Error("expected Scope and Boundaries section")
	}
	if !containsStrQ(content, "Quality Standards") {
		t.Error("expected Quality Standards section")
	}
	if !containsStrQ(content, "Escalation Triggers") {
		t.Error("expected Escalation Triggers section")
	}
}
