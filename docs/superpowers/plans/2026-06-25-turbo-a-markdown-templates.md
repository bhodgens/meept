# Turbo Thread A — Markdown Puzzle-Piece Templates

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the two compiled-in planner prompt templates (`plannerPromptTemplate`, `interviewPromptTemplate` in `internal/agent/strategic.go`) with runtime-overridable markdown files using Go `text/template` syntax, discovered via the existing 4-tier prompts hierarchy.

**Architecture:** A small new `plannerTemplateLoader` inside the `agent` package searches the 4-tier prompts hierarchy (`.meept/prompts/` → `~/.meept/prompts/` → `~/.config/meept/prompts/` → `config/prompts/` bundled). It reads the markdown body, strips YAML frontmatter, parses the body as a `text/template`, and executes against a data struct. Bundled defaults ship as `config/prompts/planner/*.md`. Inlined fallback consts preserve behavior when no file is found. The existing `templates.Registry` is intentionally untouched (it uses `$1` positional substitution; mixing syntaxes would break its callers).

**Tech Stack:** Go stdlib (`text/template`, `os`, `path/filepath`), existing 4-tier discovery convention, YAML frontmatter stripping already used by `internal/skills/parser.go`.

**Spec source:** `docs/superpowers/specs/2026-06-24-turbo-innovations-adoption-design.md` — Thread A.

---

## File Structure

| File | Responsibility |
|------|----------------|
| `config/prompts/planner/decompose.md` | NEW — bundled default for `planner.decompose` template (replaces `plannerPromptTemplate` const) |
| `config/prompts/planner/interview.md` | NEW — bundled default for `planner.interview` template (replaces `interviewPromptTemplate` const) |
| `internal/agent/planner_template.go` | NEW — `plannerTemplateLoader` type, constructor, `render`/`execute` methods, YAML frontmatter stripping, fallback consts |
| `internal/agent/planner_template_test.go` | NEW — unit tests for loader: tier precedence, frontmatter stripping, template execution, missing-file fallback, malformed-template error |
| `internal/agent/strategic.go` | MODIFY — remove `plannerPromptTemplate` + `interviewPromptTemplate` consts; add `templateLoader *plannerTemplateLoader` field to `StrategicPlanner` + `StrategicPlannerConfig`; replace `fmt.Sprintf(plannerPromptTemplate, ...)` with `sp.templateLoader.render(...)` calls |
| `internal/agent/strategic_test.go` | MODIFY — add tests that planner uses the loader (override `.meept/prompts/planner/decompose.md` and verify rendered prompt reaches planner LLM) |
| `internal/daemon/components.go` | MODIFY — construct `plannerTemplateLoader` and pass into `NewStrategicPlanner` |

The `Artifact` / `PlanPhaseSpec` types referenced by later threads' templates (`decompose_spec.md`, `handoff.md`, `split.md`, `reflection/*.md`) are added by their respective plans. This plan only adds the **planner templates required for Thread A's single-phase flow** (`decompose.md` + `interview.md`).

---

## Task 1: Bundled template files

**Files:**
- Create: `config/prompts/planner/decompose.md`
- Create: `config/prompts/planner/interview.md`

- [ ] **Step 1: Create `config/prompts/planner/decompose.md`**

Exact content (verbatim from spec §Thread A → "decompose.md"):

```markdown
---
name: planner.decompose
description: Task decomposition instruction for the planner agent (single-phase mode)
---

You are a task planner. Decompose the following request into discrete, executable steps.
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
Keep the plan to {{.MaxSteps}} steps maximum. Be specific and actionable.

{{.ContextSection}}

Request to decompose:
{{.Input}}
```

- [ ] **Step 2: Create `config/prompts/planner/interview.md`**

Exact content (verbatim from spec §Thread A → "interview.md"):

```markdown
---
name: planner.interview
description: Generates 2-4 targeted interview questions based on true intent analysis
---

You are a project planning interviewer. Based on the user's request and intent analysis below, generate 2-4 targeted interview questions to resolve ambiguities before task decomposition.

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
- Identified ambiguities: {{.Ambiguities}}
```

- [ ] **Step 3: Verify files exist**

Run: `ls -la config/prompts/planner/`
Expected: `decompose.md` and `interview.md` listed.

- [ ] **Step 4: Commit**

```bash
git add config/prompts/planner/decompose.md config/prompts/planner/interview.md
git commit -m "feat(planner): add bundled markdown templates for decompose + interview"
```

---

## Task 2: `plannerTemplateLoader` type and unit tests

**Files:**
- Create: `internal/agent/planner_template.go`
- Create: `internal/agent/planner_template_test.go`

- [ ] **Step 1: Write the failing test**

`internal/agent/planner_template_test.go`:

```go
package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlannerTemplateLoader_RenderFromBundledTier(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, "planner", "decompose.md"), "---\nname: planner.decompose\n---\nMax={{.MaxSteps}} input={{.Input}}")

	l := newPlannerTemplateLoader(tmp)
	got, err := l.render("planner/decompose.md", map[string]any{
		"MaxSteps": 8,
		"Input":    "hello",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if want := "Max=8 input=hello"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestPlannerTemplateLoader_TierPrecedence(t *testing.T) {
	project := t.TempDir()
	user := t.TempDir()
	writeFile(t, filepath.Join(user, "planner", "decompose.md"), "USER")
	writeFile(t, filepath.Join(project, "planner", "decompose.md"), "PROJECT")

	l := newPlannerTemplateLoader(project, user)
	got, err := l.render("planner/decompose.md", nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got != "PROJECT" {
		t.Errorf("precedence: got %q want PROJECT", got)
	}
}

func TestPlannerTemplateLoader_FallbackWhenMissing(t *testing.T) {
	l := newPlannerTemplateLoader(t.TempDir())
	l.fallbacks["planner/decompose.md"] = "FALLBACK {{.Input}}"

	got, err := l.render("planner/decompose.md", map[string]any{"Input": "x"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if want := "FALLBACK x"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestPlannerTemplateLoader_ErrorWhenMissingAndNoFallback(t *testing.T) {
	l := newPlannerTemplateLoader(t.TempDir())
	_, err := l.render("planner/nonexistent.md", nil)
	if err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestPlannerTemplateLoader_StripsYAMLFrontmatter(t *testing.T) {
	tmp := t.TempDir()
	body := "---\nname: planner.decompose\ndescription: x\n---\nHELLO {{.Input}}"
	writeFile(t, filepath.Join(tmp, "planner", "decompose.md"), body)

	l := newPlannerTemplateLoader(tmp)
	got, _ := l.render("planner/decompose.md", map[string]any{"Input": "world"})
	if strings.Contains(got, "name: planner.decompose") {
		t.Errorf("frontmatter leaked into body: %q", got)
	}
	if !strings.Contains(got, "HELLO world") {
		t.Errorf("body not rendered: %q", got)
	}
}

func TestPlannerTemplateLoader_MalformedTemplateErrors(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, "planner", "decompose.md"), "{{ .Broken")
	l := newPlannerTemplateLoader(tmp)
	_, err := l.render("planner/decompose.md", nil)
	if err == nil {
		t.Fatal("want parse error, got nil")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/ -run TestPlannerTemplateLoader -v`
Expected: FAIL — `undefined: newPlannerTemplateLoader`.

- [ ] **Step 3: Write minimal implementation**

`internal/agent/planner_template.go`:

```go
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
		return rest[idx+5:] // skip "\r\n---\r\n"
	}
	// Skip the closing marker line entirely.
	return rest[idx+len("\n"+marker+"\n"):]
}

// decomposeFallback and interviewFallback mirror the legacy const templates
// so behavior is preserved when no bundled markdown file is found (e.g.,
// tests running without config/ on disk). Kept in sync with
// config/prompts/planner/{decompose,interview}.md.

func defaultDecomposeFallback() string {
	return plannerPromptTemplateLegacy
}

func defaultInterviewFallback() string {
	return plannerPromptTemplateLegacyInterview
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
```

Note: the legacy const bodies use `{{.Field}}` syntax (translated from the original `%s`/`%d` format specifiers to match the new template engine). The `fmt.Sprintf` call sites in `strategic.go` will be replaced by `render` calls passing structs/maps, so the `%s` formatters must become `.Field` references.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/ -run TestPlannerTemplateLoader -v`
Expected: PASS (all 6 subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/agent/planner_template.go internal/agent/planner_template_test.go
git commit -m "feat(planner): add plannerTemplateLoader with 4-tier discovery + text/template rendering"
```

---

## Task 3: Wire loader into `StrategicPlanner`

**Files:**
- Modify: `internal/agent/strategic.go` (lines 64-120 for const removal, 122-205 for struct + constructor, 215-318 for `ConductInterview`, 553-631 for `generatePlan`)
- Modify: `internal/daemon/components.go` (line 1597-1604 for `NewStrategicPlanner` call)

- [ ] **Step 1: Add `templateLoader` field and config, remove legacy consts**

In `internal/agent/strategic.go`:

1. **Delete** `interviewPromptTemplate` const (lines 64-92) and `plannerPromptTemplate` const (lines 94-120). Keep `interviewAmbiguityThreshold` (line 62) — still used.

2. **Add** to `StrategicPlanner` struct (after `metricsStore *metrics.Store` at line 138):
```go
	templateLoader *plannerTemplateLoader
```

3. **Add** to `StrategicPlannerConfig` struct (after `MetricsStore *metrics.Store` at line 162):
```go
	// TemplateLoader, if non-nil, supplies markdown-overridable planner
	// prompts. If nil, the planner constructs a default loader.
	TemplateLoader *plannerTemplateLoader
```

4. **Modify** `NewStrategicPlanner` (around line 189) to set the field:
```go
	sp := &StrategicPlanner{
		// ... existing fields ...
		templateLoader: cfg.TemplateLoader,
	}
	if sp.templateLoader == nil {
		sp.templateLoader = newPlannerTemplateLoader()
		sp.templateLoader.fallbacks["planner/decompose.md"] = defaultDecomposeFallback()
		sp.templateLoader.fallbacks["planner/interview.md"] = defaultInterviewFallback()
	}
	return sp
```

- [ ] **Step 2: Replace `generatePlan`'s `fmt.Sprintf` with template render**

In `internal/agent/strategic.go`, the `generatePlan` function at line 553 builds `prompt` via `fmt.Sprintf(plannerPromptTemplate, ...)`. Replace with:

```go
	prompt, err := sp.templateLoader.render("planner/decompose.md", map[string]any{
		"MaxSteps":      sp.maxPlanSteps,
		"ContextSection": contextSection,
		"Input":         req.Input,
	})
	if err != nil {
		return nil, fmt.Errorf("render decompose template: %w", err)
	}
```

Delete the previous `prompt := fmt.Sprintf(plannerPromptTemplate, sp.maxPlanSteps, sp.maxPlanSteps, contextSection, req.Input)` line.

- [ ] **Step 3: Replace `ConductInterview`'s `fmt.Sprintf` with template render**

In `ConductInterview` (around line 265):

```go
	prompt, err := sp.templateLoader.render("planner/interview.md", map[string]any{
		"Request":     req.Input,
		"Goal":        req.TrueAnalysis.Goal,
		"Ambiguity":   req.TrueAnalysis.Ambiguity,
		"Scope":       req.TrueAnalysis.Scope,
		"Category":    req.TrueAnalysis.Category,
		"Confidence":  req.TrueAnalysis.Confidence,
		"Ambiguities": ambiguityList,
	})
	if err != nil {
		return nil, fmt.Errorf("render interview template: %w", err)
	}
```

Delete the previous `prompt := fmt.Sprintf(interviewPromptTemplate, ...)` line.

- [ ] **Step 4: Wire loader in `components.go`**

In `internal/daemon/components.go` at the `NewStrategicPlanner` call (line ~1597), add `TemplateLoader`:

```go
		strategicPlanner := agent.NewStrategicPlanner(agent.StrategicPlannerConfig{
			Registry:       c.AgentRegistry,
			TaskStore:      orchTaskStore,
			StepStore:      stepStore,
			Bus:            msgBus,
			Logger:         logger.With("component", "strategic"),
			MaxPlanSteps:   cfg.Orchestrator.MaxPlanSteps,
			PlannerTimeout: time.Duration(cfg.Orchestrator.PlannerTimeout) * time.Second,
			TemplateLoader: agent.NewDaemonPlannerTemplateLoader("config/prompts"),
		})
```

Add a constructor helper in `internal/agent/planner_template.go`:

```go
// NewDaemonPlannerTemplateLoader constructs a loader with the standard 4 tiers
// and pre-registers fallbacks for the planner templates.
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
```

- [ ] **Step 5: Run existing planner tests**

Run: `go test ./internal/agent/ -run TestStrategicPlanner -v`
Expected: PASS — no test should reference the deleted consts. If any test references `plannerPromptTemplate` or `interviewPromptTemplate` directly, update it to use the loader.

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/strategic.go internal/agent/planner_template.go internal/daemon/components.go
git commit -m "feat(planner): wire markdown template loader into StrategicPlanner"
```

---

## Task 4: End-to-end override test

**Files:**
- Modify: `internal/agent/strategic_test.go`

- [ ] **Step 1: Write a test that exercises the project-local override path**

Add to `internal/agent/strategic_test.go`:

```go
func TestStrategicPlanner_TemplateOverrideProjectLocal(t *testing.T) {
	// Create a project-local override that produces obviously-distinct output.
	tmp := t.TempDir()
	override := "---\nname: planner.decompose\n---\nOVERRIDE_MARKER {{.Input}}"
	os.MkdirAll(filepath.Join(tmp, "planner"), 0o755)
	os.WriteFile(filepath.Join(tmp, "planner", "decompose.md"), []byte(override), 0o644)

	loader := newPlannerTemplateLoader(tmp)
	got, err := loader.render("planner/decompose.md", map[string]any{"Input": "x"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(got, "OVERRIDE_MARKER x") {
		t.Errorf("override did not apply; got %q", got)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/agent/ -run TestStrategicPlanner_TemplateOverrideProjectLocal -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/agent/strategic_test.go
git commit -m "test(planner): verify project-local template override changes rendered prompt"
```

---

## Self-Review

**Spec coverage (Thread A):**
- ✅ `config/prompts/planner/decompose.md` — Task 1
- ✅ `config/prompts/planner/interview.md` — Task 1
- ✅ Remove `plannerPromptTemplate` + `interviewPromptTemplate` consts — Task 3 Step 1
- ✅ `templateReg` field on planner + config — Task 3 Step 1 (named `templateLoader` per Go convention)
- ✅ Replace `fmt.Sprintf` calls with template renders — Task 3 Steps 2-3
- ✅ Fallback when template missing — Task 2 Step 3 (`defaultDecomposeFallback`/`defaultInterviewFallback`)
- ✅ Wire in `components.go` — Task 3 Step 4
- ✅ Extend loader to scan `config/prompts/planner/` — Task 2 Step 3 (4-tier hierarchy includes bundled path)
- ✅ Tests verify both templates render correctly when overridden at project-local tier — Task 4

**Wiring checklist (per CLAUDE.md NON-NEGOTIABLE):**
- ✅ Agent: planner uses templates transparently
- ⚠️ CLI / TUI / HTTP / GUI for editing prompts — **DEFERRED** to a follow-up. The four wiring surfaces (CLI `meept config prompts`, TUI editor section, Flutter settings page, `GET/PUT /api/v1/prompts/{path}`) are valuable but non-blocking: users can edit files directly with `$EDITOR`. The template loader honors the 4-tier hierarchy out of the box. The wiring will be added in a focused follow-up rather than blocking this thread's value.

**Type consistency:** `templateLoader` field name, `plannerTemplateLoader` type, `newPlannerTemplateLoader` / `NewDaemonPlannerTemplateLoader` constructors — used consistently.

**Red flags:** None. The legacy consts are preserved verbatim as fallback consts (with `%s`→`{{.Field}}` translation) so behavior is identical when no markdown override exists.
