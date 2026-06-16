package agent

import (
	"fmt"
	"time"
)

// TeamPreset defines a pre-configured team template for common multi-agent workflows.
type TeamPreset struct {
	// Name is the unique identifier for this preset (e.g. "hyperplan", "security_research").
	Name string `json:"name"`
	// Description is a human-readable description of what this preset does.
	Description string `json:"description"`
	// LeadAgent is the agent ID that orchestrates and synthesizes results.
	LeadAgent string `json:"lead_agent"`
	// Roster is the list of specialist agent IDs.
	Roster []string `json:"roster"`
	// MaxConcurrent limits parallel execution.
	MaxConcurrent int `json:"max_concurrent"`
	// PromptTemplate is a Go text/template string that receives the task description
	// and assigns each roster member their specialist role.
	PromptTemplate string `json:"prompt_template"`
}

// builtInPresets is the registry of available team presets.
var builtInPresets = map[string]TeamPreset{
	"hyperplan": {
		Name:          "hyperplan",
		Description:   "5 diverse critic agents review a plan simultaneously from different perspectives",
		LeadAgent:     "planner",
		Roster:        []string{"analyst", "coder", "debugger", "planner", "analyst"},
		MaxConcurrent: 5,
		PromptTemplate: `## Hyperplan: Multi-Perspective Plan Review

### Task
%s

### Your Role
You are one of 5 specialist critics reviewing this plan. Each critic examines the plan from a unique perspective.

### Critic Roles
- Critic 1 (analyst): Critical Reviewer -- Evaluate the plan for logical consistency, completeness, and alignment with stated goals.
- Critic 2 (coder): Implementation Feasibility -- Assess technical feasibility, identify technical risks, and evaluate resource requirements.
- Critic 3 (debugger): Security Review -- Examine the plan for security implications, potential vulnerabilities, and compliance concerns.
- Critic 4 (planner): Edge Case Analyst -- Identify edge cases, boundary conditions, failure modes, and scenarios the plan may not cover.
- Critic 5 (analyst): Alternative Approaches -- Propose alternative strategies, optimizations, or fundamentally different approaches that may achieve the same goals more effectively.

### Instructions
1. Analyze the plan thoroughly from your assigned perspective.
2. Provide specific, actionable feedback with concrete examples where possible.
3. Rate the plan on a scale of 1-10 from your perspective, with justification.
4. List your top 3 findings (strengths, weaknesses, or suggestions).
5. Keep your output focused and structured.
`,
	},
	"security_research": {
		Name:          "security_research",
		Description:   "3 hunters find vulnerabilities + 2 PoC engineers write proof-of-concept exploits",
		LeadAgent:     "analyst",
		Roster:        []string{"coder", "coder", "coder", "debugger", "debugger"},
		MaxConcurrent: 5,
		PromptTemplate: `## Security Research Team

### Task
%s

### Your Role
You are a member of a security research team conducting a coordinated audit.

### Hunter Roles (coders 1-3)
- Hunter 1: Injection Point Discovery -- Find all potential injection vectors (SQL, XSS, command injection, template injection, etc.). Trace data flows from user input to sinks.
- Hunter 2: Authentication & Authorization Bypass -- Review auth logic, session management, token handling, and privilege escalation paths. Check for IDOR, broken access control, and insecure defaults.
- Hunter 3: Input Validation & Sanitization -- Audit all input validation, parsing, and sanitization code. Check for type confusion, buffer handling, encoding issues, and deserialization attacks.

### PoC Engineer Roles (debuggers 1-2)
- PoC Engineer 1: Exploit Development -- Write proof-of-concept exploits for any findings from the hunters. Create minimal, reproducible exploit code or test cases.
- PoC Engineer 2: Exploit Validation -- Validate exploits against the target system or codebase. Verify that reported vulnerabilities are actually exploitable. Assess severity and impact.

### Instructions
1. Focus exclusively on your assigned role and scope.
2. Document all findings with file paths, line numbers, and severity ratings (Critical/High/Medium/Low).
3. For hunters: prioritize systematic coverage over depth in any single area.
4. For PoC engineers: provide working exploit code or clear reproduction steps.
5. Report findings in a structured format suitable for aggregation.
`,
	},
}

// GetTeamPreset returns a preset by name. Returns an error if the preset is not found.
func GetTeamPreset(name string) (*TeamPreset, error) {
	preset, ok := builtInPresets[name]
	if !ok {
		return nil, fmt.Errorf("unknown team preset %q: available presets are %v", name, availablePresetNames())
	}
	return &preset, nil
}

// ListTeamPresets returns all available team presets.
func ListTeamPresets() []TeamPreset {
	presets := make([]TeamPreset, 0, len(builtInPresets))
	for _, p := range builtInPresets {
		presets = append(presets, p)
	}
	return presets
}

// ApplyPreset converts a preset into a TeamConfig with the task description embedded
// in the prompt template.
func ApplyPreset(presetName string, taskDescription string) (*TeamConfig, error) {
	preset, err := GetTeamPreset(presetName)
	if err != nil {
		return nil, err
	}

	// Render the prompt template with the task description
	// (The rendered prompt is accessible via PresetPrompt; ApplyPreset focuses on TeamConfig.)
	_ = fmt.Sprintf(preset.PromptTemplate, taskDescription)

	// Copy roster to avoid shared mutable state
	roster := make([]string, len(preset.Roster))
	copy(roster, preset.Roster)

	return &TeamConfig{
		LeadAgent:     preset.LeadAgent,
		Roster:        roster,
		MaxConcurrent: preset.MaxConcurrent,
		MemberTimeout: 5 * time.Minute,
		// Store rendered prompt as additional context -- the driver's
		// buildMemberPrompt can incorporate this.
	}, nil
}

// PresetPrompt returns the rendered prompt template for a preset with the given task description.
func PresetPrompt(presetName string, taskDescription string) (string, error) {
	preset, err := GetTeamPreset(presetName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(preset.PromptTemplate, taskDescription), nil
}

// availablePresetNames returns a sorted list of preset names.
func availablePresetNames() []string {
	names := make([]string, 0, len(builtInPresets))
	for name := range builtInPresets {
		names = append(names, name)
	}
	return names
}
