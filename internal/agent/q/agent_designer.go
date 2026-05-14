package q

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"unicode"

	"github.com/caimlas/meept/internal/config"
)

// AgentDesigner generates new agent specifications based on research findings.
type AgentDesigner struct {
	logger *slog.Logger
	config AgentDesignerConfig
}

// AgentDesignerConfig holds configuration for the AgentDesigner.
type AgentDesignerConfig struct {
	// TemplateDir is the directory containing agent templates
	TemplateDir string
}

// NewAgentDesigner creates a new AgentDesigner.
func NewAgentDesigner(logger *slog.Logger, designerCfg AgentDesignerConfig) *AgentDesigner {
	return &AgentDesigner{
		logger: logger,
		config: designerCfg,
	}
}

// DesignAgent generates a new agent specification based on pattern reports and research.
func (d *AgentDesigner) DesignAgent(pattern PatternReport, research *ResearchReport, analyses []*SessionAnalysis) *AgentDesign {
	// Extract requirements from research
	requirements := d.extractRequirements(research, analyses)

	// Determine agent role and purpose
	role, purpose := d.determineRoleAndPurpose(pattern, requirements)

	// Determine tooling requirements
	tools := d.determineTooling(analyses)

	// Determine capability requirements
	capabilities := d.determineCapabilities(pattern, research)

	// Derive constraints from pattern analysis
	constraints := d.deriveConstraints(analyses)

	// Generate system prompt sections
	promptSections := d.generatePromptSections(pattern, research, requirements)

	return &AgentDesign{
		ID:                   d.generateAgentID(pattern),
		Name:                 d.generateAgentName(pattern),
		Role:                 role,
		Purpose:              purpose,
		Model:                d.recommendModel(pattern, analyses),
		AdditionalTools:      tools,
		Capabilities:         capabilities,
		Constraints:          constraints,
		SystemPromptSections: promptSections,
	}
}

// extractRequirements extracts requirements from research findings.
func (d *AgentDesigner) extractRequirements(research *ResearchReport, analyses []*SessionAnalysis) []string {
	requirements := make([]string, 0)

	// Analyze what the specialist should handle
	intentFrequency := make(map[string]int)
	for _, a := range analyses {
		for _, intent := range a.Intents {
			intentFrequency[intent]++
		}
	}

	// Top intents become requirements
	for intent, count := range intentFrequency {
		if count >= 3 {
			requirements = append(requirements, fmt.Sprintf("Handle %s tasks efficiently", intent))
		}
	}

	// Add requirements based on root cause
	if strings.Contains(research.RootCause, "capability") {
		requirements = append(requirements, "Possess specialized domain knowledge")
	}
	if strings.Contains(research.RootCause, "tool") {
		requirements = append(requirements, "Demonstrate tool proficiency")
	}

	return requirements
}

// determineRoleAndPurpose determines the agent's role and purpose statement.
func (d *AgentDesigner) determineRoleAndPurpose(pattern PatternReport, requirements []string) (agentRole, purposeStmt string) {
	intent := pattern.AffectedIntent
	if intent == "" {
		intent = "specialized"
	}

	role := config.AgentRoleExecutor
	if pattern.PatternType == "high_rejection_rate" {
		role = config.AgentRoleReviewer
	}

	var purpose strings.Builder
	fmt.Fprintf(&purpose, "You are a %s specialist %s agent. ", intent, role)
	purpose.WriteString("Your responsibilities:\n")

	reqCount := minInt(len(requirements), 3)
	for i := range reqCount {
		fmt.Fprintf(&purpose, "%d. %s\n", i+1, requirements[i]) //nolint:gosec // index bounded by minInt(len(requirements), 3) above
	}
	if reqCount == 0 {
		fmt.Fprintf(&purpose, "1. Execute %s tasks with high quality\n", intent)
	}

	fmt.Fprintf(&purpose, "\nYou do NOT handle: tasks outside %s domain, general chat, unrelated requests", intent)

	return role, purpose.String()
}

// determineTooling determines required tools based on session analysis.
func (d *AgentDesigner) determineTooling(analyses []*SessionAnalysis) []string {
	toolUsage := make(map[string]int)
	toolFailures := make(map[string]int)

	for _, a := range analyses {
		for _, tc := range a.ToolCalls {
			toolUsage[tc.ToolName]++
			if !tc.Success {
				toolFailures[tc.ToolName]++
			}
		}
	}

	tools := make([]string, 0)

	// Add frequently used successful tools
	for tool, count := range toolUsage {
		failures := toolFailures[tool]
		if count >= 3 && float64(failures)/float64(count) < 0.2 {
			tools = append(tools, tool)
		}
	}

	// Ensure baseline tools are available
	baselineTools := []string{"memory_search", "memory_store"}
	for _, bt := range baselineTools {
		if !contains(tools, bt) {
			tools = append(tools, bt)
		}
	}

	return tools
}

// determineCapabilities determines required capabilities.
func (d *AgentDesigner) determineCapabilities(pattern PatternReport, research *ResearchReport) []string {
	capabilities := make([]string, 0)

	// Default capabilities
	capabilities = append(capabilities, "reasoning", "tool_use")

	// Add capabilities based on pattern type
	switch pattern.PatternType {
	case PatternModelMisconfiguration:
		// May need specific model capabilities
		capabilities = append(capabilities, "domain_knowledge")
	case "wrong_agent_assignment", "repeated_failure":
		capabilities = append(capabilities, "specialized_expertise")
	case "high_tool_failure_rate":
		capabilities = append(capabilities, "technical_proficiency")
	}

	// Add capabilities from research
	if research.ResearchType == ResearchTypeCapability {
		capabilities = append(capabilities, "advanced_reasoning")
	}

	return capabilities
}

// deriveConstraints derives operational constraints from session analysis.
func (d *AgentDesigner) deriveConstraints(analyses []*SessionAnalysis) AgentConstraints {
	if len(analyses) == 0 {
		return AgentConstraints{
			MaxIterations:    25,
			TimeoutSeconds:   300,
			MaxTokensPerTurn: 4096,
			MaxMemoryRefs:    20,
			Temperature:      ptrFloat(0.3),
		}
	}

	// Compute average metrics
	var totalIterations, totalTokens, totalDuration int
	for _, a := range analyses {
		totalIterations += a.IterationCount
		totalTokens += a.TokenUsage
		totalDuration += int(a.Duration.Seconds())
	}

	avgIterations := totalIterations / len(analyses)
	avgTokens := totalTokens / len(analyses)
	avgDuration := totalDuration / len(analyses)

	// Set constraints based on averages with headroom
	return AgentConstraints{
		MaxIterations:    maxInt(avgIterations+5, 25),
		TimeoutSeconds:   maxInt(avgDuration+60, 300),
		MaxTokensPerTurn: maxInt(avgTokens+1000, 4096),
		MaxMemoryRefs:    20,
		Temperature:      ptrFloat(0.3),
	}
}

// generatePromptSections generates system prompt sections.
func (d *AgentDesigner) generatePromptSections(pattern PatternReport, research *ResearchReport, requirements []string) []string {
	sections := make([]string, 0, 5)

	// Section 1: Scope and Boundaries
	// Section 2: Required Output Format
	// Section 3: Escalation Triggers
	// Section 4: Quality Standards
	// Section 5: Workflow Steps
	sections = append(sections,
		d.generateScopeSection(pattern),
		d.generateOutputFormatSection(),
		d.generateEscalationSection(),
		d.generateQualityStandardsSection(research),
		d.generateWorkflowSection(requirements),
	)

	return sections
}

// generateScopeSection generates the scope and boundaries section.
func (d *AgentDesigner) generateScopeSection(pattern PatternReport) string {
	intent := pattern.AffectedIntent
	if intent == "" {
		intent = "specialized"
	}

	return fmt.Sprintf(`## Scope and Boundaries

You specialize in %s tasks. When receiving a task:

1. **Verify Scope**: Check if the task falls within your specialty
2. **Accept**: If within scope, proceed with execution using your specialized approach
3. **Escalate**: If outside scope, immediately escalate to dispatcher

**In Scope**:
- %s-related tasks and questions
- Tasks requiring %s domain expertise
- Troubleshooting %s issues

**Out of Scope**:
- General conversation
- Tasks unrelated to %s
- Requests requiring capabilities you don't possess`, intent, intent, intent, intent, intent)
}

// generateOutputFormatSection generates the required output format section.
func (d *AgentDesigner) generateOutputFormatSection() string {
	return `## Required Output Format

Always structure your responses as follows:

### Summary
Brief 1-2 sentence overview of what was accomplished.

### Execution Details
Step-by-step breakdown:
1. First action taken and why
2. Second action (if needed)
3. Continue for each significant step

### Results
Clear statement of the outcome with any relevant outputs or artifacts.

### Next Steps (if applicable)
Any follow-up actions needed or recommendations for the user.`
}

// generateEscalationSection generates the escalation triggers section.
func (d *AgentDesigner) generateEscalationSection() string {
	return `## Escalation Triggers

Escalate to dispatcher immediately when:

1. **Wrong Specialty**: Task is outside your domain expertise
2. **Capability Gap**: Task requires tools or capabilities you don't have
3. **Repeated Failure**: Same approach fails 3+ times
4. **User Request**: User explicitly asks for different agent type
5. **Ambiguous Intent**: Cannot determine what user needs after clarification attempt

When escalating, include:
- Brief explanation of why you're escalating
- Any work already attempted
- Recommendation for which agent might help`
}

// generateQualityStandardsSection generates the quality standards section.
func (d *AgentDesigner) generateQualityStandardsSection(research *ResearchReport) string {
	// Customize based on research findings
	if strings.Contains(research.RootCause, "prompt") || strings.Contains(research.RootCause, "rejection") {
		return `## Quality Standards

**Accuracy First**:
- Verify results before reporting success
- Double-check outputs match user requirements
- If uncertain, run validation checks

**Efficiency**:
- Aim to complete tasks in minimal iterations
- Reuse successful patterns from previous work
- Batch related operations when possible

**Documentation**:
- Record all tool calls and their results
- Note any assumptions made during execution
- Flag potential issues for user awareness

**Review Checklist** (before marking complete):
- [ ] Task accomplishes stated goal
- [ ] All outputs verified
- [ ] No obvious errors or edge cases missed
- [ ] User can immediately use the results`
	}

	return `## Quality Standards

**Completeness**: Finish tasks fully - no partial implementations
**Correctness**: Verify results work as intended
**Efficiency**: Minimize iterations while maintaining quality
**Communication**: Clear, concise updates on progress`
}

// generateWorkflowSection generates the workflow disclosure section.
func (d *AgentDesigner) generateWorkflowSection(_ []string) string {
	var workflow strings.Builder
	workflow.WriteString("## Workflow Steps\n\n")
	workflow.WriteString("Follow this systematic approach:\n\n")

	workflow.WriteString("### Step 1: Understand Requirements\n")
	workflow.WriteString("- Parse user request to identify specific needs\n")
	workflow.WriteString("- Ask clarifying questions if intent is ambiguous\n\n")

	workflow.WriteString("### Step 2: Plan Approach\n")
	workflow.WriteString("- Outline steps needed to complete the task\n")
	workflow.WriteString("- Identify tools and resources required\n\n")

	workflow.WriteString("### Step 3: Execute\n")
	workflow.WriteString("- Implement solution following best practices\n")
	workflow.WriteString("- Provide progress updates for longer tasks\n\n")

	workflow.WriteString("### Step 4: Verify\n")
	workflow.WriteString("- Test or validate results\n")
	workflow.WriteString("- Confirm task completion meets requirements\n\n")

	workflow.WriteString("### Step 5: Report\n")
	workflow.WriteString("- Summarize what was accomplished\n")
	workflow.WriteString("- Provide any relevant outputs or artifacts\n")

	return workflow.String()
}

// generateAgentID generates an agent ID from the pattern.
func (d *AgentDesigner) generateAgentID(pattern PatternReport) string {
	intent := pattern.AffectedIntent
	if intent == "" {
		intent = "specialist"
	}

	// Sanitize intent for use as ID
	intent = strings.ToLower(intent)
	intent = strings.ReplaceAll(intent, " ", "_")
	intent = strings.ReplaceAll(intent, "-", "_")

	return fmt.Sprintf("%s_specialist", intent)
}

// generateAgentName generates a human-readable agent name.
func (d *AgentDesigner) generateAgentName(pattern PatternReport) string {
	intent := pattern.AffectedIntent
	if intent == "" {
		intent = "Specialist"
	}

	// Capitalize first letter (strings.Title is deprecated, use unicode.ToUpper for first char)
	name := strings.ToLower(intent)
	if name != "" {
		runes := []rune(name)
		runes[0] = unicode.ToUpper(runes[0])
		name = string(runes)
	}
	return fmt.Sprintf("%s Specialist Agent", name)
}

// recommendModel recommends an appropriate model based on task patterns.
func (d *AgentDesigner) recommendModel(_ PatternReport, analyses []*SessionAnalysis) string {
	// Check if task requires reasoning
	requiresReasoning := false
	requiresCode := false

	for _, a := range analyses {
		if a.DifficultyScore > 0.7 {
			requiresReasoning = true
		}
		for _, tc := range a.ToolCalls {
			if strings.Contains(tc.ToolName, "file") || strings.Contains(tc.ToolName, "shell") {
				requiresCode = true
			}
		}
	}

	if requiresCode {
		return "coder" // Use coder model alias
	} else if requiresReasoning {
		return "" // Empty means use default (reasoning-capable)
	}

	return "fast" // Cheap model for simple tasks
}

// Helper functions

func contains(slice []string, item string) bool {
	return slices.Contains(slice, item)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func ptrFloat(f float64) *float64 {
	return &f
}

// GenerateFullAgentFile generates a complete AGENT.md file content.
func (d *AgentDesigner) GenerateFullAgentFile(design *AgentDesign) string {
	var buf strings.Builder

	// YAML frontmatter
	buf.WriteString("---\n")
	fmt.Fprintf(&buf, "id: %s\n", design.ID)
	fmt.Fprintf(&buf, "name: %s\n", design.Name)
	fmt.Fprintf(&buf, "role: %s\n", design.Role)
	fmt.Fprintf(&buf, "purpose: |\n%s\n", indent(design.Purpose, "  "))

	if design.Model != "" {
		fmt.Fprintf(&buf, "model: %s\n", design.Model)
	}

	if len(design.AdditionalTools) > 0 {
		buf.WriteString("additional_tools:\n")
		for _, tool := range design.AdditionalTools {
			fmt.Fprintf(&buf, "  - %s\n", tool)
		}
	}

	if len(design.Capabilities) > 0 {
		buf.WriteString("capabilities:\n")
		for _, capName := range design.Capabilities {
			fmt.Fprintf(&buf, "  - %s\n", capName)
		}
	}

	fmt.Fprintf(&buf, "max_iterations: %d\n", design.Constraints.MaxIterations)
	fmt.Fprintf(&buf, "timeout_seconds: %d\n", design.Constraints.TimeoutSeconds)
	fmt.Fprintf(&buf, "max_tokens_per_turn: %d\n", design.Constraints.MaxTokensPerTurn)
	if design.Constraints.Temperature != nil {
		fmt.Fprintf(&buf, "temperature: %.1f\n", *design.Constraints.Temperature)
	}

	buf.WriteString("---\n\n")

	// Baseline instructions header
	fmt.Fprintf(&buf, "# %s Baseline Instructions\n\n", design.Name)

	// System prompt sections
	for _, section := range design.SystemPromptSections {
		buf.WriteString(section)
		buf.WriteString("\n\n")
	}

	return buf.String()
}

// indent indents each line of a string.
func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}
