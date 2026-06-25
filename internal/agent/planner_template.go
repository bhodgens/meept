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
