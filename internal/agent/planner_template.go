package agent

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// plannerTemplateLoader finds markdown templates in the 4-tier prompts
// hierarchy and renders them via Go text/template. It is deliberately
// separate from internal/templates.Registry, which uses $1 positional
// substitution and must not be disturbed.
type plannerTemplateLoader struct {
	tiers     []string          // ordered: project → user → system → bundled
	fallbacks map[string]string // relative path → fallback body
	logger    *slog.Logger
}

func newPlannerTemplateLoader(tiers ...string) *plannerTemplateLoader {
	if len(tiers) == 0 {
		home, _ := os.UserHomeDir()
		tiers = []string{
			".meept/prompts",
			filepath.Join(home, ".meept", "prompts"),
			filepath.Join(home, ".config", "meept", "prompts"),
			"config/prompts",
		}
	}
	return &plannerTemplateLoader{
		tiers:     tiers,
		fallbacks: make(map[string]string),
		logger:    slog.Default(),
	}
}

// render loads the named template (e.g., "planner/decompose.md") from the
// first tier that contains it, strips YAML frontmatter, and executes it
// as a text/template against data. If no tier has the file and a fallback
// is registered, the fallback is used. Returns an error if neither.
func (l *plannerTemplateLoader) render(name string, data any) (string, error) {
	for _, tier := range l.tiers {
		path := filepath.Join(tier, name)
		body, err := os.ReadFile(path)
		if err == nil {
			return l.execute(string(body), data)
		}
	}
	if fb, ok := l.fallbacks[name]; ok {
		return l.execute(fb, data)
	}
	return "", fmt.Errorf("planner template %q not found in any tier and no fallback registered", name)
}

func (l *plannerTemplateLoader) execute(body string, data any) (string, error) {
	body = stripYAMLFrontmatter(body)
	tmpl, err := template.New("planner").Parse(body)
	if err != nil {
		return "", fmt.Errorf("parse planner template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute planner template: %w", err)
	}
	return buf.String(), nil
}

// stripYAMLFrontmatter removes a leading "---\n...\n---\n" block from body.
// If no frontmatter is present, body is returned unchanged.
func stripYAMLFrontmatter(body string) string {
	const marker = "---"
	if !strings.HasPrefix(body, marker+"\n") && !strings.HasPrefix(body, marker+"\r\n") {
		return body
	}
	// Find the closing marker on its own line.
	rest := body[len(marker):]
	if strings.HasPrefix(rest, "\r\n") {
		rest = rest[2:]
	} else {
		rest = rest[1:]
	}
	idx := strings.Index(rest, "\n"+marker+"\n")
	if idx < 0 {
		idx = strings.Index(rest, "\r\n"+marker+"\r\n")
		if idx < 0 {
			return body // malformed; return as-is
		}
		return rest[idx+len("\r\n"+marker+"\r\n"):] // skip "\r\n---\r\n"
	}
	// Skip the closing marker line entirely.
	return rest[idx+len("\n"+marker+"\n"):]
}

// defaultDecomposeFallback and defaultInterviewFallback mirror the legacy
// const templates so behavior is preserved when no bundled markdown file is
// found (e.g., tests running without config/ on disk). Kept in sync with
// config/prompts/planner/{decompose,interview}.md.
//
// The legacy const bodies use {{.Field}} syntax (translated from the
// original %s/%d format specifiers to match the new template engine).

func defaultDecomposeFallback() string {
	return plannerPromptTemplateLegacy
}

func defaultInterviewFallback() string {
	return plannerPromptTemplateLegacyInterview
}

// defaultDecomposeSpecFallback mirrors config/prompts/planner/decompose_spec.md
// so the spec_plan fallback path works without the bundled markdown file on
// disk (e.g., tests running in a temp dir). Kept in sync with the bundled file.
func defaultDecomposeSpecFallback() string { return decomposeSpecFallbackBody }

// defaultSplitFallback mirrors config/prompts/orchestrator/split.md so the
// chunkToExecutorCapacity fallback path works without the bundled markdown
// file on disk. Kept in sync with the bundled file.
func defaultSplitFallback() string { return splitFallbackBody }

// defaultHandoffFallback mirrors config/prompts/orchestrator/handoff.md so the
// generateHandoff fallback path works without the bundled markdown file on disk.
// Kept in sync with the bundled file.
func defaultHandoffFallback() string { return handoffFallbackBody }

// defaultReflectionTurnFallback mirrors config/prompts/reflection/turn.md so the
// ReflectTurn fallback path works without the bundled markdown file on disk.
// Kept in sync with the bundled file.
func defaultReflectionTurnFallback() string { return reflectionTurnFallbackBody }

// defaultReflectionSessionFallback mirrors config/prompts/reflection/session.md so the
// ReflectInactiveSessions fallback path works without the bundled markdown file on disk.
// Kept in sync with the bundled file.
func defaultReflectionSessionFallback() string { return reflectionSessionFallbackBody }

// NewDaemonPlannerTemplateLoader constructs a loader with the standard 4 tiers
// and pre-registers fallbacks for the planner templates. The bundledPromptsPath
// is used as the lowest-priority tier (typically "config/prompts" relative to
// the daemon working directory).
func NewDaemonPlannerTemplateLoader(bundledPromptsPath string) *plannerTemplateLoader {
	home, _ := os.UserHomeDir()
	l := &plannerTemplateLoader{
		tiers: []string{
			".meept/prompts",
			filepath.Join(home, ".meept", "prompts"),
			filepath.Join(home, ".config", "meept", "prompts"),
			bundledPromptsPath,
		},
		fallbacks: make(map[string]string),
		logger:    slog.Default(),
	}
	l.fallbacks["planner/decompose.md"] = defaultDecomposeFallback()
	l.fallbacks["planner/interview.md"] = defaultInterviewFallback()
	l.fallbacks["planner/decompose_spec.md"] = defaultDecomposeSpecFallback()
	l.fallbacks["orchestrator/split.md"] = defaultSplitFallback()
	l.fallbacks["orchestrator/handoff.md"] = defaultHandoffFallback()
	l.fallbacks["reflection/turn.md"] = defaultReflectionTurnFallback()
	l.fallbacks["reflection/session.md"] = defaultReflectionSessionFallback()
	return l
}

// Legacy const bodies preserved verbatim for fallback. These are duplicated
// from the original consts in strategic.go before deletion; kept here so
// fallback behavior is testable without the bundled markdown files.
const plannerPromptTemplateLegacy = `You are a task planner. Decompose the following request into discrete, executable steps.
Each step should be a single unit of work that can be assigned to a specialist agent.

Available tool hints (use these to indicate what kind of agent should handle each step):
- "code" or "refactor" → coding specialist
- "debug" or "fix" → debugging specialist
- "analyze" or "research" → analysis specialist
- "git" or "commit" → git operations specialist
- "plan" → further planning/decomposition
- "chat" → general conversation

Output ONLY valid JSON in this exact format (no markdown, no explanation):
{
  "steps": [
    {"description": "step description", "tool_hint": "code", "depends_on": []},
    {"description": "step description", "tool_hint": "code", "depends_on": [0]},
    {"description": "step description", "tool_hint": "git", "depends_on": [0, 1]}
  ]
}

The "depends_on" field uses 0-based step indices. Steps with empty depends_on can run in parallel.
HARD CONSTRAINT: You MUST output AT MOST {{.MaxSteps}} steps. Do not exceed this limit.
Be specific and actionable.

{{.ContextSection}}

Request to decompose:
{{.Input}}`

const plannerPromptTemplateLegacyInterview = `You are a project planning interviewer. Based on the user's request and intent analysis below, generate 2-4 targeted interview questions to resolve ambiguities before task decomposition.

Your questions should cover:
1. Specific scope boundaries (what is in vs. out of scope)
2. Constraints and preferences (technology, performance, timeline)
3. Priority or ordering of requirements
4. Specific ambiguities identified in the analysis

Rules:
- Generate ONLY valid JSON, no markdown, no explanation
- Keep questions concise and actionable
- Each question should have a clear, specific focus
- Maximum 4 questions, minimum 2

Output format:
{"questions": ["question 1", "question 2", ...]}

Request: {{.Request}}

Intent analysis:
- Goal: {{.Goal}}
- Ambiguity: {{.Ambiguity}}
- Scope: {{.Scope}}
- Category: {{.Category}}
- Confidence: {{.Confidence}}
- Identified ambiguities: {{.Ambiguities}}`

// decomposeSpecFallbackBody mirrors config/prompts/planner/decompose_spec.md.
// It uses {{.MaxStepsPerPhase}}, {{.MaxPhases}}, {{.ContextSection}}, and
// {{.Input}} placeholders.
const decomposeSpecFallbackBody = `You are a task planner producing a multi-phase plan for substantive work.
Each phase is a coherent unit of work with explicit input/output contracts.

Output ONLY valid JSON in this exact format:
{
  "phases": [
    {
      "name": "Phase 1: <short name>",
      "description": "<what this phase accomplishes>",
      "steps": [
        {"description": "...", "tool_hint": "code", "depends_on": []}
      ],
      "produces": [
        {"name": "<artifact-name>", "kind": "file", "description": "...", "required": true}
      ],
      "consumes": [],
      "depends_on": []
    }
  ]
}

Rules:
- produces.kind must be one of: file, interface, schema, decision, test_suite
- Each phase should have between 1 and {{.MaxStepsPerPhase}} steps
- Maximum {{.MaxPhases}} phases

{{.ContextSection}}

Request to decompose:
{{.Input}}`

// splitFallbackBody mirrors config/prompts/orchestrator/split.md.
const splitFallbackBody = `You are an execution orchestrator. The following step is too large for one agent invocation.
Split it into sub-steps that each fit within {{.BudgetTokens}} tokens of executor context.

Original step:
- Description: {{.StepDescription}}
- Tool hint: {{.ToolHint}}
- Executor agent: {{.ExecutorID}}
- Executor model context limit: {{.ContextLimit}}

Output ONLY valid JSON:
{
  "sub_steps": [
    {"description": "...", "tool_hint": "code", "depends_on": []},
    {"description": "...", "tool_hint": "code", "depends_on": [0]}
  ]
}

Rules:
- Sub-steps must collectively accomplish the original step's intent
- Each sub-step should fit in {{.BudgetTokens}} tokens including tool output
- Preserve the original step's tool hint unless a sub-step genuinely needs a different agent
- Maximum 5 sub-steps per split`

// handoffFallbackBody mirrors config/prompts/orchestrator/handoff.md.
// Template placeholders: {{.StepDescription}}, {{.ToolHint}}, {{.ConversationExcerpt}}.
const handoffFallbackBody = `---
name: orchestrator.handoff
description: Summarizes a completed step's tool calls and outputs into a structured handoff for downstream steps
---

You are a step-completion summarizer. Produce a structured handoff document so downstream
steps can continue the work without seeing the full conversation history.

Step that just completed:
- Description: {{.StepDescription}}
- Tool hint: {{.ToolHint}}

Conversation excerpt (tool calls + results from this step):
{{.ConversationExcerpt}}

Output ONLY valid JSON:
{
  "summary": "<2-4 sentence natural-language summary of what was accomplished>",
  "files_modified": [
    {"path": "<file>", "change": "created|modified|deleted", "summary": "<one-line description>"}
  ],
  "decisions": [
    {"name": "<decision-name>", "rationale": "<why>"}
  ],
  "artifacts": [
    {"name": "<artifact-name>", "kind": "file|interface|schema|decision|test_suite", "description": "..."}
  ],
  "follow_up_hints": ["<watch out for X>", "<consider Y for next step>"],
  "tool_highlights": [
    {"tool": "<tool-name>", "summary": "<one-line summary of call + result>"}
  ],
  "error_code": ""
}

Rules:
- Leave error_code empty unless the step failed; on failure, set error_code and skip other fields
- Truncate per-entry text: paths full, summaries 200 chars, descriptions 300 chars
- Maximum 10 files_modified, 5 decisions, 5 artifacts, 5 follow_up_hints, 10 tool_highlights`

// reflectionTurnFallbackBody mirrors config/prompts/reflection/turn.md.
// Template placeholders: {{.AgentID}}, {{.UserInput}}, {{.Outcome}}, {{.TrajectoryJSON}}.
const reflectionTurnFallbackBody = `---
name: reflection.turn
description: Per-turn reflection that extracts operational lessons from a single agent turn
---

You are a self-reflection assistant. Examine this agent turn and extract 0 or 1 concrete
operational lessons that would help future agent invocations.

A good lesson is:
- Specific and actionable ("always run go vet after editing .go files"), not abstract
- Generalizable beyond this specific task
- Based on something that worked OR something that failed

Agent: {{.AgentID}}
User input: {{.UserInput}}
Outcome: {{.Outcome}}

Trajectory:
{{.TrajectoryJSON}}

Output ONLY valid JSON. If no clear lesson, output {"proposal": null}.
Otherwise:
{
  "proposal": {
    "type": "skill_create|skill_update|agent_prompt|project_instruction|prompt_component",
    "target": "<file path or skill name>",
    "change": "<proposed modification — full markdown for skills, rule text for instructions>",
    "justification": "<one sentence why>",
    "confidence": 0.0
  }
}

Rules:
- type=skill_create: target is a path like .meept/skills/<name>/SKILL.md, change is full markdown
- type=agent_prompt: target is config/agents/<id>/AGENT.md, change is the new restriction text
- type=project_instruction: target is CLAUDE.md, change is the rule to add
- confidence < 0.6 → output null instead (don't waste review queue)
`

// reflectionSessionFallbackBody mirrors config/prompts/reflection/session.md.
// Template placeholders: {{.SessionID}}, {{.AgentID}}, {{.TurnCount}}, {{.LastActivity}}, {{.TurnsJSON}}.
const reflectionSessionFallbackBody = `---
name: reflection.session
description: Periodic reflection that examines multiple turns from an inactive session to extract deeper lessons
---

You are a self-reflection assistant performing deeper analysis on a recently-inactive session.

Examine the turns below and extract 0-3 higher-quality lessons about agent behavior, prompt
quality, or workflow patterns.

Session: {{.SessionID}}
Agent: {{.AgentID}}
Total turns: {{.TurnCount}}
Last activity: {{.LastActivity}}

Turns (oldest first):
{{.TurnsJSON}}

Output ONLY valid JSON:
{
  "proposals": [
    {
      "type": "skill_create|skill_update|agent_prompt|project_instruction|prompt_component",
      "target": "<file path or skill name>",
      "change": "<proposed modification>",
      "justification": "<one sentence why>",
      "confidence": 0.0
    }
  ]
}

Rules:
- Maximum 3 proposals (highest-quality only)
- Confidence < 0.7 → drop the proposal
- Prefer cross-turn patterns over single-turn observations
- type=skill_create proposals should describe the trigger condition in the skill description
`
