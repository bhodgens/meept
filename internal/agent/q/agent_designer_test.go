package q

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

var designerLogger = slog.Default()

func makeAnalysis(sID, agentID string, intents []string, duration time.Duration,
	iterations, revisions, tokens int, outcome string, difficulty float64,
) *SessionAnalysis {
	return &SessionAnalysis{
		SessionID:       sID,
		AgentID:         agentID,
		Intents:         intents,
		Duration:        duration,
		IterationCount:  iterations,
		RevisionCycles:  revisions,
		TokenUsage:      tokens,
		Outcome:         outcome,
		DifficultyScore: difficulty,
		ToolCalls: []ToolCallRecord{
			{ToolName: "file_read", Success: true},
			{ToolName: "shell_execute", Success: true},
		},
		StartTime: time.Now().Add(-duration),
		EndTime:   time.Now(),
	}
}

func makeAnalysisOnly(sID, agentID string, intents []string, duration time.Duration,
	iterations, revisions, tokens int, outcome string, difficulty float64,
) *SessionAnalysis {
	return &SessionAnalysis{
		SessionID:       sID,
		AgentID:         agentID,
		Intents:         intents,
		Duration:        duration,
		IterationCount:  iterations,
		RevisionCycles:  revisions,
		TokenUsage:      tokens,
		Outcome:         outcome,
		DifficultyScore: difficulty,
		StartTime:       time.Now().Add(-duration),
		EndTime:         time.Now(),
	}
}

func TestAgentDesignerDesignAgent(t *testing.T) {
	pattern := PatternReport{
		PatternType:          "wrong_agent_assignment",
		Confidence:           0.8,
		AffectedIntent:       "debugging",
		MisconfigurationType: "capability_gap",
		SessionCount:         10,
	}

	research := &ResearchReport{
		RootCause:    "No current agent has specialized capability",
		ResearchType: ResearchTypeCapability,
	}

	analyses := []*SessionAnalysis{
		makeAnalysisOnly("s1", "coder", []string{"debugging"}, 20*time.Minute, 15, 5, 5000, "failed", 0.8),
		makeAnalysisOnly("s2", "coder", []string{"debugging"}, 25*time.Minute, 18, 6, 6000, "failed", 0.85),
	}

	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	design := designer.DesignAgent(pattern, research, analyses)

	if design.ID == "" {
		t.Error("design ID should not be empty")
	}
	if design.Name == "" {
		t.Error("design name should not be empty")
	}
	if design.Role == "" {
		t.Error("design role should not be empty")
	}
	if design.Purpose == "" {
		t.Error("design purpose should not be empty")
	}
	if len(design.Capabilities) == 0 {
		t.Error("design should have capabilities")
	}
	if len(design.SystemPromptSections) == 0 {
		t.Error("design should have prompt sections")
	}
}

func TestAgentDesignerDesignAgentHighRejection(t *testing.T) {
	pattern := PatternReport{
		PatternType:          "high_rejection_rate",
		Confidence:           0.7,
		AffectedIntent:       "code_review",
		MisconfigurationType: "prompt_deficiency",
	}

	research := &ResearchReport{}
	analyses := []*SessionAnalysis{
		makeAnalysisOnly("s1", "coder", []string{"code_review"}, 10*time.Minute, 5, 3, 2000, "completed", 0.5),
	}

	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	design := designer.DesignAgent(pattern, research, analyses)

	if design.Role != "reviewer" {
		t.Errorf("expected role 'reviewer' for high_rejection_rate, got %q", design.Role)
	}
}

func TestAgentDesignerDesignAgentEmptyPattern(t *testing.T) {
	pattern := PatternReport{
		PatternType: "model_misconfiguration",
	}

	research := &ResearchReport{}
	analyses := []*SessionAnalysis{
		makeAnalysisOnly("s1", "coder", []string{"general"}, 10*time.Minute, 5, 1, 2000, "completed", 0.3),
	}

	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	design := designer.DesignAgent(pattern, research, analyses)

	if !strings.Contains(design.ID, "specialist") {
		t.Errorf("expected default intent in ID for empty AffectedIntent, got %q", design.ID)
	}
}

func TestAgentDesignerExtractRequirements(t *testing.T) {
	research := &ResearchReport{
		RootCause: "agent lacks capability for this task",
	}

	analyses := []*SessionAnalysis{
		{Intents: []string{"debug", "code", "debug"}},
		{Intents: []string{"debug", "code", "debug"}},
		{Intents: []string{"debug", "code", "debug"}},
	}

	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	reqs := designer.extractRequirements(research, analyses)

	if len(reqs) == 0 {
		t.Error("expected some requirements for intents with count >= 3")
	}

	hasCapability := false
	for _, r := range reqs {
		if strings.Contains(r, "specialized domain knowledge") {
			hasCapability = true
		}
	}
	if !hasCapability {
		t.Error("expected capability requirement from root cause")
	}
}

func TestAgentDesignerExtractRequirementsTool(t *testing.T) {
	research := &ResearchReport{
		RootCause: "agent lacks tool proficiency",
	}

	analyses := []*SessionAnalysis{
		{Intents: []string{"debug", "code", "debug"}},
		{Intents: []string{"debug", "code", "debug"}},
		{Intents: []string{"debug", "code", "debug"}},
	}

	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	reqs := designer.extractRequirements(research, analyses)

	hasToolReq := false
	for _, r := range reqs {
		if strings.Contains(r, "tool proficiency") {
			hasToolReq = true
		}
	}
	if !hasToolReq {
		t.Error("expected tool requirement from root cause")
	}
}

func TestAgentDesignerDetermineTooling(t *testing.T) {
	analyses := []*SessionAnalysis{
		{
			ToolCalls: []ToolCallRecord{
				{ToolName: "file_read", Success: true},
				{ToolName: "file_read", Success: true},
				{ToolName: "file_read", Success: true},
			},
		},
		{
			ToolCalls: []ToolCallRecord{
				{ToolName: "memory_search", Success: true},
				{ToolName: "memory_store", Success: true},
			},
		},
	}

	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	tools := designer.determineTooling(analyses)

	if !contains(tools, "memory_search") {
		t.Error("expected 'memory_search' in tools")
	}
	if !contains(tools, "memory_store") {
		t.Error("expected 'memory_store' in tools")
	}
}

func TestAgentDesignerDetermineToolingFrequentFailures(t *testing.T) {
	analyses := []*SessionAnalysis{
		{
			ToolCalls: []ToolCallRecord{
				{ToolName: "bad_tool", Success: false},
				{ToolName: "bad_tool", Success: false},
				{ToolName: "bad_tool", Success: false},
				{ToolName: "bad_tool", Success: false},
				{ToolName: "bad_tool", Success: true},
			},
		},
	}

	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	tools := designer.determineTooling(analyses)

	if contains(tools, "bad_tool") {
		t.Error("bad_tool with >80% failure rate should not be included")
	}
}

func TestAgentDesignerDetermineCapabilities(t *testing.T) {
	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})

	tests := []struct {
		patternType string
		needCap     string
	}{
		{"model_misconfiguration", "domain_knowledge"},
		{"wrong_agent_assignment", "specialized_expertise"},
		{"repeated_failure", "specialized_expertise"},
		{"high_tool_failure_rate", "technical_proficiency"},
		{"unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.patternType, func(t *testing.T) {
			pattern := PatternReport{
				PatternType: tt.patternType,
			}

			caps := designer.determineCapabilities(pattern, &ResearchReport{})

			if !contains(caps, "reasoning") || !contains(caps, "tool_use") {
				t.Errorf("expected base capabilities 'reasoning' and 'tool_use', got %v", caps)
			}

			if tt.needCap != "" && !contains(caps, tt.needCap) {
				t.Errorf("expected capability %q for pattern %q, got %v", tt.needCap, tt.patternType, caps)
			}
		})
	}
}

func TestAgentDesignerDetermineCapabilitiesWithResearch(t *testing.T) {
	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	pattern := PatternReport{PatternType: "unknown"}
	research := &ResearchReport{ResearchType: ResearchTypeCapability}

	caps := designer.determineCapabilities(pattern, research)
	if !contains(caps, "advanced_reasoning") {
		t.Error("expected 'advanced_reasoning' from capability research type")
	}
}

func TestAgentDesignerDeriveConstraints(t *testing.T) {
	analyses := []*SessionAnalysis{
		makeAnalysisOnly("s1", "coder", []string{"debug"}, 10*time.Minute, 5, 1, 5000, "completed", 0.3),
		makeAnalysisOnly("s2", "coder", []string{"debug"}, 20*time.Minute, 10, 2, 8000, "completed", 0.5),
	}

	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	constraints := designer.deriveConstraints(analyses)

	if constraints.MaxIterations < 25 {
		t.Errorf("MaxIterations should be at least 25 after clamping, got %d", constraints.MaxIterations)
	}
	if constraints.TimeoutSeconds < 300 {
		t.Errorf("TimeoutSeconds should be at least 300 after clamping, got %d", constraints.TimeoutSeconds)
	}
	if constraints.MaxTokensPerTurn < 4096 {
		t.Errorf("MaxTokensPerTurn should be at least 4096 after clamping, got %d", constraints.MaxTokensPerTurn)
	}
	if constraints.MaxMemoryRefs != 20 {
		t.Errorf("expected MaxMemoryRefs 20, got %d", constraints.MaxMemoryRefs)
	}
	if constraints.Temperature == nil {
		t.Error("Temperature should not be nil")
	} else if *constraints.Temperature != 0.3 {
		t.Errorf("expected temperature 0.3, got %.1f", *constraints.Temperature)
	}
}

func TestAgentDesignerGenerateAgentID(t *testing.T) {
	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})

	tests := []struct {
		intent string
		want   string
	}{
		{"debugging", "debugging_specialist"},
		{"Code Review", "code_review_specialist"},
		{"", "specialist_specialist"},
		{"debug-debug", "debug_debug_specialist"},
	}

	for _, tt := range tests {
		t.Run(tt.intent, func(t *testing.T) {
			pattern := PatternReport{AffectedIntent: tt.intent}
			id := designer.generateAgentID(pattern)
			if id != tt.want {
				t.Errorf("generateAgentID(%q) = %q, want %q", tt.intent, id, tt.want)
			}
		})
	}
}

func TestAgentDesignerGenerateAgentName(t *testing.T) {
	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})

	tests := []struct {
		intent string
		want   string
	}{
		{"debugging", "Debugging Specialist Agent"},
		{"code_review", "Code_review Specialist Agent"},
		{"", "Specialist Specialist Agent"},
	}

	for _, tt := range tests {
		t.Run(tt.intent, func(t *testing.T) {
			pattern := PatternReport{AffectedIntent: tt.intent}
			name := designer.generateAgentName(pattern)
			if name != tt.want {
				t.Errorf("generateAgentName(%q) = %q, want %q", tt.intent, name, tt.want)
			}
		})
	}
}

func TestAgentDesignerRecommendModel(t *testing.T) {
	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})

	tests := []struct {
		name          string
		difficulty    float64
		toolNames     []string
		wantModel     string
	}{
		{"high difficulty", 0.8, []string{"memory_search"}, ""},
		{"has file tool", 0.3, []string{"file_read", "shell_execute"}, "coder"},
		{"low difficulty", 0.2, []string{"memory_search"}, "fast"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analyses := []*SessionAnalysis{
				{DifficultyScore: tt.difficulty},
			}
			for _, tn := range tt.toolNames {
				analyses[0].ToolCalls = append(analyses[0].ToolCalls, ToolCallRecord{ToolName: tn})
			}

			model := designer.recommendModel(PatternReport{}, analyses)
			if model != tt.wantModel {
				t.Errorf("want model %q, got %q", tt.wantModel, model)
			}
		})
	}
}

func TestAgentDesignerGeneratePromptSections(t *testing.T) {
	pattern := PatternReport{AffectedIntent: "debugging"}
	research := &ResearchReport{RootCause: "agent confusion"}

	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	sections := designer.generatePromptSections(pattern, research, nil)

	if len(sections) != 5 {
		t.Errorf("expected 5 prompt sections, got %d", len(sections))
	}
}

func TestAgentDesignerGenerateScopeSection(t *testing.T) {
	pattern := PatternReport{AffectedIntent: "debugging"}
	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	section := designer.generateScopeSection(pattern)

	if !strings.Contains(section, "debugging") {
		t.Error("scope section should mention intent")
	}
	if !strings.Contains(section, "## Scope and Boundaries") {
		t.Error("scope section should have header")
	}
}

func TestAgentDesignerGenerateScopeSectionEmptyIntent(t *testing.T) {
	pattern := PatternReport{}
	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	section := designer.generateScopeSection(pattern)

	if !strings.Contains(section, "specialized") {
		t.Error("should use 'specialized' as default intent")
	}
}

func TestAgentDesignerGenerateOutputFormatSection(t *testing.T) {
	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	section := designer.generateOutputFormatSection()

	if !strings.Contains(section, "## Required Output Format") {
		t.Error("missing output format header")
	}
	if !strings.Contains(section, "### Summary") {
		t.Error("missing Summary section")
	}
	if !strings.Contains(section, "### Execution Details") {
		t.Error("missing Execution Details section")
	}
}

func TestAgentDesignerGenerateEscalationSection(t *testing.T) {
	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	section := designer.generateEscalationSection()

	if !strings.Contains(section, "## Escalation Triggers") {
		t.Error("missing escalation triggers header")
	}
	if !strings.Contains(section, "Wrong Specialty") {
		t.Error("missing wrong specialty trigger")
	}
}

func TestAgentDesignerGenerateQualityStandardsSection(t *testing.T) {
	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})

	// With rejection root cause -> detailed quality standards
	research := &ResearchReport{RootCause: "agent prompt confusion and rejection"}
	section := designer.generateQualityStandardsSection(research)

	if !strings.Contains(section, "## Quality Standards") {
		t.Error("missing quality standards header")
	}
	if !strings.Contains(section, "Accuracy First") {
		t.Error("should have Accuracy First for rejection-related root cause")
	}

	// Without rejection -> basic quality standards
	research2 := &ResearchReport{RootCause: "general issues"}
	section2 := designer.generateQualityStandardsSection(research2)

	if !strings.Contains(section2, "## Quality Standards") {
		t.Error("missing quality standards header")
	}
	if !strings.Contains(section2, "Completeness") {
		t.Error("should have Completeness for basic quality standards")
	}
}

func TestAgentDesignerGenerateWorkflowSection(t *testing.T) {
	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	section := designer.generateWorkflowSection(nil)

	if !strings.Contains(section, "## Workflow Steps") {
		t.Error("missing workflow steps header")
	}
	if !strings.Contains(section, "Step 1: Understand Requirements") {
		t.Error("missing step 1")
	}
	if !strings.Contains(section, "Step 5: Report") {
		t.Error("missing step 5")
	}
}

func TestAgentDesignerGenerateRoleAndPurpose(t *testing.T) {
	tests := []struct {
		patternType     string
		affectedIntent  string
		wantRole        string
	}{
		{"wrong_agent_assignment", "debugging", "executor"},
		{"high_rejection_rate", "code_review", "reviewer"},
		{"model_misconfiguration", "", "executor"},
	}

	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})

	for _, tt := range tests {
		t.Run(tt.patternType, func(t *testing.T) {
			pattern := PatternReport{
				PatternType:      tt.patternType,
				AffectedIntent:   tt.affectedIntent,
			}

			role, purpose := designer.determineRoleAndPurpose(pattern, []string{})

			if role != tt.wantRole {
				t.Errorf("expected role %q, got %q", tt.wantRole, role)
			}
			if !strings.Contains(purpose, tt.wantRole) {
				t.Errorf("purpose should mention role %q", tt.wantRole)
			}
		})
	}
}

func TestAgentDesignerGenerateFullAgentFile(t *testing.T) {
	design := &AgentDesign{
		ID:      "debug_specialist",
		Name:    "Debug Specialist Agent",
		Role:    "executor",
		Purpose: "You are a debug specialist agent.",
		Model:   "coder",
		AdditionalTools: []string{"file_read", "memory_search"},
		Capabilities:      []string{"reasoning", "tool_use"},
		Constraints: AgentConstraints{
			MaxIterations:    20,
			TimeoutSeconds:   300,
			MaxTokensPerTurn: 8192,
			Temperature:      ptrFloat(0.3),
		},
		SystemPromptSections: []string{
			"## Scope\nYou specialize in debugging\n",
			"## Workflow\nFollow steps\n",
		},
	}

	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	content := designer.GenerateFullAgentFile(design)

	if !strings.Contains(content, "---") {
		t.Error("missing YAML frontmatter")
	}
	if !strings.Contains(content, "id: debug_specialist") {
		t.Error("missing agent ID in frontmatter")
	}
	if !strings.Contains(content, "model: coder") {
		t.Error("missing model in frontmatter")
	}
	if !strings.Contains(content, "additional_tools:") {
		t.Error("missing additional_tools")
	}
	if !strings.Contains(content, "capabilities:") {
		t.Error("missing capabilities")
	}
	if !strings.Contains(content, "temperature: 0.3") {
		t.Error("missing temperature")
	}
	if !strings.Contains(content, "## Scope") {
		t.Error("missing scope section")
	}
}

func TestAgentDesignerGenerateFullAgentFileNoModel(t *testing.T) {
	design := &AgentDesign{
		ID:      "fast_agent",
		Name:    "Fast Agent",
		Role:    "executor",
		Purpose: "fast agent",
		Model:   "", // no model
		Constraints: AgentConstraints{
			MaxIterations: 10,
			TimeoutSeconds: 60,
		},
	}

	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	content := designer.GenerateFullAgentFile(design)

	if strings.Contains(content, "model:") {
		t.Error("should not include model line when model is empty")
	}
}

func TestAgentDesignerGenerateFullAgentFileNoCapabilities(t *testing.T) {
	design := &AgentDesign{
		ID:      "simple_agent",
		Name:    "Simple Agent",
		Role:    "executor",
		Purpose: "simple",
		Constraints: AgentConstraints{
			MaxIterations: 5,
		},
	}

	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	content := designer.GenerateFullAgentFile(design)

	if strings.Contains(content, "capabilities:") {
		t.Error("should not include capabilities when empty")
	}
}

func TestAgentDesignerGenerateFullAgentFileNoTools(t *testing.T) {
	design := &AgentDesign{
		ID:      "no_tools_agent",
		Name:    "No Tools Agent",
		Role:    "executor",
		Purpose: "no tools",
		Constraints: AgentConstraints{
			MaxIterations: 5,
		},
	}

	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	content := designer.GenerateFullAgentFile(design)

	if strings.Contains(content, "additional_tools:") {
		t.Error("should not include additional_tools when empty")
	}
}

func TestAgentDesignerDesignAgentEmptyAnalyses(t *testing.T) {
	pattern := PatternReport{AffectedIntent: "debugging"}
	research := &ResearchReport{}

	designer := NewAgentDesigner(designerLogger, AgentDesignerConfig{})
	design := designer.DesignAgent(pattern, research, nil)

	if design.ID == "" {
		t.Error("design ID should not be empty even with no analyses")
	}
}

func TestAgentDesignerIndent(t *testing.T) {
	result := indent("hello\nworld", "  ")
	expected := "  hello\n  world"
	if result != expected {
		t.Errorf("indent() = %q, want %q", result, expected)
	}
}

func TestAgentDesignerIndentEmptyLines(t *testing.T) {
	result := indent("hello\n\nworld", "  ")
	// empty lines should not get prefix
	if result != "  hello\n\n  world" {
		t.Errorf("indent() = %q, expected empty lines to not be prefixed", result)
	}
}
