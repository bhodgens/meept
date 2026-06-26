# Turbo Innovations Adoption Design

**Date:** 2026-06-24
**Status:** Design
**Origin:** Comparative analysis of [tobihagemann/turbo](https://github.com/tobihagemann/turbo) workflow vs. meept's planning pipeline.

## Purpose

Adopt the architectural strengths of Turbo's planning workflow (puzzle-piece markdown skills, mechanical context hygiene, graded complexity routing, shells with Produces/Consumes invariants, immediate self-reflection) into meept's runtime-backed planning system. The end state preserves meept's daemon/event-driven/SQLite advantages while closing real gaps the comparison exposed.

Six independent threads merge into a coherent planning architecture evolution:

| Thread | Title | Centerpiece |
|--------|-------|-------------|
| A | Markdown puzzle-piece | Make the 2 remaining compiled-in planner templates runtime-overridable markdown |
| B | Context isolation | Structured handoff + per-task-per-agent loops + phase-level context reset |
| C+F | Orchestrator chunking + phases + Produces/Consumes | Orchestrator becomes active step-transformer; phases with produces/consumes replace flat `maxPlanSteps` cap |
| D | Complexity routing | Dispatcher synthesizes a graded mode signal (direct/plan/spec_plan/spec_pair) |
| E | Immediate self-reflection | Per-turn reflection writes proposals to a queue; skills replace patterns.json; `/remember` tool/command |

Q Agent rework is **deferred to a separate spec** (`docs/superpowers/specs/YYYY-MM-DD-q-agent-rework-design.md`, TBD).

## Background — What the Comparison Actually Showed

Verified findings (cited against current source):

### Already markdown (no recompile needed)
- Agent system prompts: `config/agents/*/AGENT.md` (3-tier override: `.meept/` → `~/.meept/` → `~/.config/meept/`)
- Prompt components: `config/prompts/*.md` (4-tier override)
- Skills: `config/skills/*/SKILL.md` (3-tier override)
- Global rules: `RULES.md` (auto-discovered)

### Still compiled-in Go consts
- `plannerPromptTemplate` (`internal/agent/strategic.go:82`) — decomposition task instruction
- `interviewPromptTemplate` (`internal/agent/strategic.go:55`) — interview question generator
- `DefaultConstitution`/`DefaultRestrictions`/`DefaultPurpose` (`internal/agent/prompt.go:10-14`) — last-resort fallbacks only

### Current orchestrator is passive
`internal/agent/orchestrator.go` is a bus event dispatcher with thin pass-through handlers. No step transformation, no model-metadata access, no chunking logic. The intended architecture (planner decomposes intent → orchestrator chunks to executor capacity → executors run) **does not exist**.

### Chunking gap is double
1. No component does chunking
2. The information needed to do it isn't exposed (`AgentRegistry.resolver` is unexported; `GetSpec()` returns model string only)

### `maxPlanSteps=10` is arbitrary
No rationale in commits, comments, or audit docs. Fully configurable via `orchestrator.max_plan_steps`. Silent truncation when exceeded. Flat cap — 50-file refactor and a typo fix face the same ceiling.

### Context isolation is partial
- Per-conversationID isolation works: planner uses `plan-<taskID>-<random>`, executor uses `step-<taskID>-<stepID>` — separate histories.
- Step-to-step context propagation is lossy: `propagateContextToNextSteps` (`tactical.go:1318`) appends a 500-char truncated result.
- One shared `*AgentLoop` per agent ID (`registry.go:38`); isolation relies on conversationID discipline.
- No phase-level context reset.

### Self-improvement is heavier than Turbo's but partially wired
- **Learning Pipeline** (`learning.go`) fires after every agent turn (`loop.go:1582`), writes patterns to `patterns.json`, injects into future prompts via `ContextInjector`.
- **Missing:** no CLAUDE.md/AGENT.md writing, no end-of-session trigger, no mid-task reflection, trajectory is text-only (no tool calls/results). Q Agent is CLI-only and daemon-unwired.
- **Q Agent is purely heuristic** — no LLM, no model config, hardcoded rules in `internal/agent/q/`. Skill creation writes templated strings, not LLM-generated content.

### Complexity routing is duplicated, not layered
Two independent 0.6 thresholds:
- `defaultAmbiguityThreshold` (`intent_analyzer.go:13`) — dispatcher blocks routing
- `interviewAmbiguityThreshold` (`strategic.go:62`) — planner conducts interview

Plus `shouldDecompose` fast-path (chat/report/recall bypass) + ambiguity ≥0.6 interview + compound→pair session. No graded routing anywhere; the overlap is accidental.

## Architecture

### Division of labor (target)

```
Dispatcher.ClassifyAndRoute
  ├─ AnalyzeTrueIntent → TrueIntentAnalysis (now with SuggestedMode hint)
  ├─ classifyIntent → IntentType
  ├─ suggestMode(intent, analysis, input) → "direct"|"plan"|"spec_plan"|"spec_pair"  [Thread D]
  └─ routeToPlan / routeToAgent based on mode
       ↓ (forwards PlanRequest.Mode)
Planner.Plan(req with Mode)
  ├─ switch req.Mode
  │   direct     → createFallbackSteps
  │   plan       → generatePlan (single-phase, using markdown-overridable template)  [Thread A]
  │   spec_plan  → generatePhasePlan (multi-phase with Produces/Consumes)            [Thread C+F]
  │   spec_pair  → pair session (existing compound-intent behavior)
  └─ Emits steps + planner sizing hints
       ↓ (orchestrator.plan bus event)
Orchestrator.handlePlanRequest  [Thread C — now ACTIVE]
  ├─ Calls planner.Plan()
  ├─ Post-planning chunking pass:
  │   - registry.GetModelConfig(executorID) → context limit
  │   - For each step: estimate token cost vs executor budget
  │   - Split oversized steps into sub-steps
  ├─ Phase boundary management [Thread C+F]:
  │   - Phase N completes → collect produces into ArtifactStore
  │   - Phase N+1 starts with fresh conversation, consumes phase N's produces
  │   - Structured handoff replaces 500-char truncation  [Thread B-a]
  └─ Publish scheduled steps to TacticalScheduler
       ↓
TacticalScheduler.ScheduleReadySteps (mostly unchanged)
  └─ Workers claim and execute (per-task-per-agent loops)  [Thread B-b]
       ↓
Executor AgentLoop
  ├─ Fresh conversation per step (existing convention, structurally enforced per task)
  ├─ Receives structured handoff from prior phase  [Thread B-a]
  └─ Runs with markdown-loaded AGENT.md + prompt components + skills  [Thread A extends existing]

--- Parallel pipeline ---

Self-Reflection  [Thread E]
  ├─ Per-turn: trajectory (now rich: tool calls + results) → reflection LLM
  │           → proposal in .meept/improvements.md
  ├─ Periodic (30 min timer, NOT session-end): deeper reflection → CLAUDE.md/AGENT.md proposals
  ├─ Explicit /remember tool AND user slash command: immediate proposal
  ├─ ContextInjector loads skills instead of patterns.json (patterns deprecated)
  └─ Proposals land in .meept/improvements.md → user approves via /implement-improvements

--- Separate plan ---

Q Agent rework (deferred)
  ├─ Dedicated `config/agents/q/AGENT.md` with LLM access
  ├─ Daily scheduler, daemon-wired
  ├─ Creates/updates SKILL.md with format validation + retry-until-valid
  └─ Reports to user via TUI/CLI/Flutter notifications
```

## Thread A — Markdown Puzzle-Piece

### Goal

Make the two remaining compiled-in planner templates runtime-overridable via markdown files, closing the last gap in meept's markdown-everywhere story.

### Template files

```
config/prompts/planner/
  decompose.md      ← replaces plannerPromptTemplate
  interview.md      ← replaces interviewPromptTemplate
```

Discovery tiers (existing, unchanged): `.meept/prompts/` → `~/.meept/prompts/` → `~/.config/meept/prompts/` → `config/prompts/` (bundled defaults).

### Template engine

`text/template` (Go stdlib). Consistent with existing `internal/templates/` registry. Future-proofs against more complex substitutions.

### File contents

#### `config/prompts/planner/decompose.md`

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

#### `config/prompts/planner/interview.md`

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

#### `config/prompts/planner/decompose_spec.md` (Thread D adds this)

```markdown
---
name: planner.decompose_spec
description: Multi-phase decomposition with Produces/Consumes invariants (spec_plan mode)
---

You are a task planner producing a multi-phase plan for substantive work.
Each phase is a coherent unit of work with explicit input/output contracts.

Decompose the request into phases. Each phase contains steps and declares:
- produces: artifacts (files, interfaces, decisions, schemas, test suites) the phase guarantees
- consumes: artifacts from earlier phases that this phase depends on
- depends_on: 0-based phase indices this phase depends on

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
    },
    {
      "name": "Phase 2: ...",
      "produces": [],
      "consumes": [
        {"name": "<artifact-name>", "kind": "file", "description": "...", "required": true}
      ],
      "depends_on": [0]
    }
  ]
}

Rules:
- produces.kind must be one of: file, interface, schema, decision, test_suite
- consumes can only reference artifacts produced by an earlier phase
- Each phase should have between 1 and {{.MaxStepsPerPhase}} steps
- Maximum {{.MaxPhases}} phases
- Phases with empty depends_on can run in parallel (rare for spec_plan)

{{.ContextSection}}

Request to decompose:
{{.Input}}
```

#### `config/prompts/orchestrator/split.md` (Thread C adds this)

```markdown
---
name: orchestrator.split
description: Instruction to split an oversized step into sub-steps that fit executor context budget
---

You are an execution orchestrator. The following step is too large for one agent invocation.
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
- Maximum 5 sub-steps per split
```

#### `config/prompts/orchestrator/handoff.md` (Thread B-a adds this)

```markdown
---
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
- Maximum 10 files_modified, 5 decisions, 5 artifacts, 5 follow_up_hints, 10 tool_highlights
```

#### `config/prompts/reflection/turn.md` (Thread E adds this)

```markdown
---
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
```

#### `config/prompts/reflection/session.md` (Thread E adds this)

```markdown
---
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
```

### Changes required

1. **`config/prompts/planner/decompose.md`** (new) — content above
2. **`config/prompts/planner/interview.md`** (new) — content above
3. **`internal/agent/strategic.go`** —
   - Remove `plannerPromptTemplate` and `interviewPromptTemplate` consts
   - Add `templateReg *templates.Registry` field to `StrategicPlanner` and `StrategicPlannerConfig`
   - Replace `fmt.Sprintf(plannerPromptTemplate, ...)` with `sp.renderTemplate("planner.decompose", data)`
   - Fallback: if template not found, use an inline const (mirrors `DefaultConstitution` pattern)
4. **`internal/daemon/components.go`** — wire existing `templates.Registry` into `NewStrategicPlanner`
5. **`internal/templates/`** — extend loader to scan `config/prompts/planner/` if not already covered
6. **Tests** — `internal/agent/strategic_test.go` verifies both templates render correctly when overridden at project-local tier

### Wiring (per CLAUDE.md NON-NEGOTIABLE requirement)

- **CLI:** `meept config prompts planner decompose` opens the template in editor; `meept config prompts list` shows available templates
- **TUI:** config editor section for prompts shows planner templates with edit option
- **GUI:** Flutter settings page exposes template list with edit/save (calls HTTP API)
- **HTTP API:** `GET/PUT /api/v1/prompts/{path}` for template read/write
- **Agent:** planner agent uses the templates transparently (no explicit wiring beyond the render call)

## Thread D — Complexity Routing

### Goal

Replace the planner's `shouldDecompose` + `shouldUsePairSession` + ambiguity-only interview gate with a single dispatcher-synthesized `SuggestedMode` field. Dispatcher owns synthesis; thresholds stay separate and configurable.

### Mode taxonomy

| Mode | Decomposition shape | Interview? | Example |
|------|--------------------|-----------|---------|
| `direct` | Single fallback step, no LLM decomposition | No | "what's the weather" |
| `plan` | LLM decomposition, flat step list, single phase | Only if ambiguity ≥ threshold | "refactor auth.go to use new middleware" |
| `spec_plan` | LLM decomposition into phases with Produces/Consumes invariants | Yes (or strong hint) | "rebuild the search subsystem for multi-tenant" |
| `spec_pair` | Pair session (actor/reviewer over shared bus topic) | No | compound intents ("fix the bug then deploy the fix"). Reuses existing pair-session code path unchanged; this plan does not modify pair sessions. |

### Data model changes

```go
// internal/agent/intent_analyzer.go
type TrueIntentAnalysis struct {
    // existing fields...
    SuggestedMode string `json:"suggested_mode,omitempty"`
}

// internal/agent/intent.go
func (t IntentType) SuggestedMode() string {
    switch t {
    case IntentChat, IntentRecall, IntentStatus, IntentReport, IntentPlatform, IntentSearch:
        return "direct"
    case IntentCode, IntentDebug, IntentGit, IntentToolUse, IntentSecurity:
        return "plan"
    case IntentCompound:
        return "spec_pair"
    case IntentPlan, IntentArchitect:
        return "spec_plan"
    default:
        return "plan"
    }
}

// internal/agent/dispatcher.go
type Intent struct {
    // existing fields...
    SuggestedMode string `json:"suggested_mode,omitempty"`
}

type DispatchResult struct {
    // existing fields...
    SuggestedMode string `json:"suggested_mode,omitempty"`
}

// internal/agent/strategic.go
type PlanRequest struct {
    // existing fields...
    Mode string `json:"mode,omitempty"`
}
```

### Dispatcher synthesis (pure function, unit-testable)

```go
// internal/agent/dispatcher.go
func suggestMode(intentType IntentType, analysis *TrueIntentAnalysis, input string) string {
    if intentType == IntentCompound {
        return "spec_pair"
    }
    fallback := intentType.SuggestedMode()
    analysisMode := ""
    if analysis != nil {
        analysisMode = validateMode(analysis.SuggestedMode)
    }
    mode := analysisMode
    if mode == "" {
        mode = fallback
    }
    if len(input) < 50 && mode != "spec_plan" && analysisMode == "" {
        mode = "direct"
    }
    return mode
}
```

Called between step 9 (`classifyIntent`) and step 11 (`routeToPlan`) in `ClassifyAndRoute`.

### Planner consumption

```go
// internal/agent/strategic.go
func (sp *StrategicPlanner) Plan(ctx context.Context, req PlanRequest) (*PlanResult, error) {
    mode := req.Mode
    if mode == "" {
        mode = sp.inferLegacyMode(req)  // backward-compat for empty-mode requests
    }
    switch mode {
    case "direct":
        return sp.createFallbackSteps(ctx, req)
    case "plan":
        return sp.planSinglePhase(ctx, req)
    case "spec_plan":
        return sp.planMultiPhase(ctx, req)
    case "spec_pair":
        return sp.planPairSession(ctx, req)
    default:
        return sp.planSinglePhase(ctx, req)
    }
}

func (sp *StrategicPlanner) shouldInterview(req PlanRequest, mode string) bool {
    switch mode {
    case "spec_plan":
        return true
    case "plan":
        return req.TrueAnalysis != nil &&
            req.TrueAnalysis.Ambiguity >= sp.interviewAmbiguity
    default:
        return false
    }
}
```

**Removals:** `shouldDecompose`, `shouldUsePairSession`, `ConductInterview`'s ambiguity+scope gate. `inferLegacyMode` preserves behavior for empty-mode requests during rollout.

### Threshold configurability

**`meept.json5` additions:**
```json5
{
  dispatcher: {
    ambiguity_threshold: 0.6,  // blocks routing when ambiguity ≥ this
  },
  planner: {
    interview_ambiguity_threshold: 0.6,  // conducts interview for plan-mode
  },
}
```

Both made config-file-exposed (currently only IntentAnalyzer has a Go-API builder; neither has a config path). Distinct semantic roles:
- `dispatcher.ambiguity_threshold` — blocks routing when we can't even classify
- `planner.interview_ambiguity_threshold` — conducts design interview for substantive work

### Wiring

- **CLI:** `meept config dispatcher` and `meept config planner` TUI sections expose the new thresholds
- **TUI:** ACK message in chat view shows mode via `FormatEnhancedAsyncTaskAck` (`handler.go:656`): "executing directly" / "planned (N steps)" / "spec-planned (N phases)" / "pair session"
- **GUI:** Flutter ACK bubble renders the same mode label; settings page exposes thresholds
- **HTTP API:** `GET/PUT /api/v1/config/dispatcher`, `GET/PUT /api/v1/config/planner`
- **Agent:** dispatcher populates `Intent.SuggestedMode` + `DispatchResult.SuggestedMode`; handler forwards into `PlanRequest.Mode`

### Changes summary

| File | Change |
|------|--------|
| `intent_analyzer.go` | Add `SuggestedMode` field; update LLM prompt + validator |
| `intent.go` | Add `IntentType.SuggestedMode()` method |
| `dispatcher.go` | Add `suggestMode` pure function; call in `ClassifyAndRoute` |
| `strategic.go` | Replace heuristics with mode switch; add `inferLegacyMode`; convert interview threshold to config-driven field |
| `handler.go` | Forward mode into `PlanRequest.Mode` |
| `config/schema.go` | Add `AmbiguityThreshold` + `InterviewAmbiguityThreshold` |
| `meept.json5` | Document new keys with defaults |
| `cmd/meept/config_*.go` | TUI editor sections for new keys |
| `config/prompts/planner/decompose_spec.md` | New template (content above) |
| `strategic_test.go`, `dispatcher_test.go` | Update tests |
| `internal/tui/*` | Display mode in ACK |
| `ui/flutter_ui/lib/features/chat/` | Mode label in ACK bubble |
| `ui/flutter_ui/lib/features/settings/` | Threshold settings |

## Thread C+F — Orchestrator Chunking + Phases + Produces/Consumes

### Goal

Transform the orchestrator from passive bus dispatcher to active step-transformer. Replace flat `maxPlanSteps` with phase-based decomposition. Produces/Consumes invariants become first-class.

### Piece 1: Expose model metadata

```go
// internal/agent/registry.go
func (r *AgentRegistry) GetModelConfig(agentID string) (*llm.ModelConfig, error) {
    r.mu.RLock()
    spec, ok := r.specs[agentID]
    r.mu.RUnlock()
    if !ok {
        return nil, fmt.Errorf("agent %q not found", agentID)
    }
    if spec.Model == "" {
        return r.resolver.Default(), nil
    }
    return r.resolver.ResolveRef(spec.Model)
}

// internal/agent/orchestrator.go
func executorBudget(modelCfg *llm.ModelConfig) int {
    // 40% of context limit — conservative per-step budget leaving room
    // for accumulated context + tool output. Slightly more generous than
    // ContextFirewall's 0.30 iteration ratio because planned steps should
    // fit comfortably rather than size to the firewall's tighter limit.
    return int(float64(modelCfg.ContextLimit) * 0.40)
}
```

### Piece 2: Planner emits phases

New types (planner's output shape; distinct from existing `plan.PlanPhase` persisted record):

```go
// internal/agent/strategic.go
type Artifact struct {
    Name        string `json:"name"`         // "auth_middleware.go", "auth-design"
    Kind        string `json:"kind"`         // file|interface|schema|decision|test_suite
    Description string `json:"description"`
    Required    bool   `json:"required"`
}

type PlanPhaseSpec struct {
    Name         string          `json:"name"`
    Description  string          `json:"description"`
    Steps        []plannerStep   `json:"steps"`
    Produces     []Artifact      `json:"produces"`
    Consumes     []Artifact      `json:"consumes"`
    DependsOn    []int           `json:"depends_on,omitempty"`
}

type plannerPhaseOutput struct {
    Phases []PlanPhaseSpec `json:"phases"`
}
```

New planner method:

```go
// internal/agent/strategic.go
func (sp *StrategicPlanner) planMultiPhase(ctx context.Context, req PlanRequest) (*PlanResult, error) {
    // 1. Conduct interview (per shouldInterview, always for spec_plan)
    // 2. Render decompose_spec.md template (Thread A)
    // 3. Call planner LLM, expect plannerPhaseOutput JSON
    // 4. Parse + validate + auto-repair (see Risk #1 mitigations)
    // 5. Create TaskSteps per phase, grouped via PhaseID field on TaskStep
    // 6. Set inter-phase dependencies (phase 2 step 1 depends_on phase 1 last step)
    // 7. Persist phase metadata to plan store
    // 8. Publish task.planned with phase summary
}
```

`TaskStep` gains `PhaseID string` field. Inter-phase dependencies are step-level dependencies between last step of phase N and first step of phase N+1.

### Piece 3: Produces/Consumes invariants

Adapting Turbo's shells concept. Key difference from Turbo: invariants are **machine-readable and enforced at scheduling time**, not just documented.

- **Produced artifacts** are emitted by step completion events. The step's `StepHandoff.Artifacts` (Thread B-a) is scanned for matches against declared produces. Matches are added to task's `ArtifactStore`.
- **Consumed artifacts** are injected into consuming step's prompt context. Phase N+1 receives phase N's produces as structured context, not raw history.
- **Enforcement:** `checkPhaseReady` returns error if required consumes are missing → orchestrator re-plans or warns.

```go
// internal/agent/orchestrator.go
type ArtifactStore struct {
    mu       sync.RWMutex
    artifacts map[string]Artifact // by name
    producers map[string][]string // artifact name → step IDs that produced it
}

func (s *ArtifactStore) Add(a Artifact, producerStepID string) { ... }
func (s *ArtifactStore) Has(name string) bool { ... }
func (s *ArtifactStore) Get(name string) (Artifact, bool) { ... }

func (o *Orchestrator) checkPhaseReady(phase *PlanPhaseSpec, store *ArtifactStore) error {
    for _, consumed := range phase.Consumes {
        if consumed.Required && !store.Has(consumed.Name) {
            return fmt.Errorf("phase %q requires %q but it wasn't produced",
                phase.Name, consumed.Name)
        }
    }
    return nil
}
```

### Piece 4: Orchestrator proactive chunking

```go
// internal/agent/orchestrator.go
func (o *Orchestrator) chunkToExecutorCapacity(ctx context.Context, taskID string) error {
    steps, err := o.stepStore.GetByTask(taskID)
    if err != nil { return err }
    for _, step := range steps {
        executorID := o.tactical.SelectAgentForHint(step.ToolHint)
        modelCfg, err := o.registry.GetModelConfig(executorID)
        if err != nil { continue }  // fall back to ContextFirewall at runtime
        budget := executorBudget(modelCfg)
        cost := estimateStepTokens(step, modelCfg)
        if cost > budget {
            subSteps, err := o.splitStep(ctx, step, budget, modelCfg)
            if err != nil { continue }
            o.stepStore.ReplaceWithSubSteps(step.ID, subSteps)
        }
    }
    return nil
}

func estimateStepTokens(step *task.TaskStep, modelCfg *llm.ModelConfig) int {
    // Rough heuristic: description + accumulated context + tool output budget
    desc := EstimateTokenCountHeuristic(step.Description)
    acc := EstimateTokenCountHeuristic(step.AccumulatedContext)
    toolBudget := toolOutputBudget(step.ToolHint) // code:8K debug:4K git:1K
    return desc + acc + toolBudget
}

func (o *Orchestrator) splitStep(ctx context.Context, step *task.TaskStep, budget int, modelCfg *llm.ModelConfig) ([]*task.TaskStep, error) {
    // Render config/prompts/orchestrator/split.md template
    // LLM call: "this step is too big for X budget, split into sub-steps that fit"
    // Parse sub-steps, preserve dependencies, cap at 5 sub-steps
}
```

### Piece 5: Orchestrator reactive re-chunking

Three reactive triggers:

1. **Executor handoff requests** — orchestrator can split current step instead of just adding new ones
2. **Step result oversized** — summarize/distill before propagating (Thread B-a handoff replaces truncation)
3. **ContextFirewall compression events** — subscribe to `llm.context_compressed` bus topic; heavy compression on step X means similar future steps should be pre-split

Per-task split counter with cap (5 splits per task) prevents cascading splits.

### `maxPlanSteps` replacement

`max_plan_steps` config key removed from sample configs. Replaced with:

- **Per-phase step cap** (`planner.max_steps_per_phase`, default 8) — natural unit of work
- **Phase count soft cap** (`planner.max_phases`, default 12) — catches runaway spec-planning
- **Token budget per step** (computed from executor model, not directly configurable)

Total possible steps: 12 phases × 8 steps = 96, vs. current flat 10. Structural boundaries (phase resets, produces/consumes gates) make this safe.

If user has `max_plan_steps` set, log deprecation warning and ignore value.

### Orchestrator struct changes

```go
// internal/agent/orchestrator.go
type Orchestrator struct {
    // existing fields...
    strategic   *StrategicPlanner
    tactical    *TacticalScheduler
    pairManager *PairManager
    planManager *plan.Manager
    registry    *AgentRegistry        // NEW — for GetModelConfig
    templateReg *templates.Registry   // NEW — for split.md rendering
    artifacts   *ArtifactStore        // NEW — per-task (cleared on task completion)
    bus         *bus.MessageBus
    logger      *slog.Logger
}
```

Wired in `internal/daemon/components.go` at orchestrator construction.

### Phase-level context reset (ties to Thread B-c)

When a phase completes:
1. Collect phase N's produced artifacts into `ArtifactStore`
2. Start phase N+1 steps with **fresh conversationID**: `phase-<phaseID>-<stepID>` (new naming convention)
3. Inject consumes artifacts + original user request + plan summary as structured context
4. Phase N's raw history, tool outputs, intermediate reasoning are **not** propagated

```go
// internal/agent/orchestrator.go
func (o *Orchestrator) startNextPhase(ctx context.Context, taskID, completedPhaseID string) error {
    nextPhase, err := o.planStore.GetNextPhase(taskID, completedPhaseID)
    if err != nil { return err }
    if err := o.checkPhaseReady(nextPhase, o.artifacts); err != nil { return err }
    startupCtx := o.renderPhaseStartup(nextPhase, o.artifacts)
    steps, _ := o.stepStore.GetByPhase(nextPhase.ID)
    for _, step := range steps {
        step.ConversationID = fmt.Sprintf("phase-%s-%s", nextPhase.ID, step.ID)
        step.AccumulatedContext = startupCtx
        o.stepStore.Update(step)
    }
    return nil
}
```

### Plan store and plan.md updates

```go
// internal/plan/plan.go
type PlanPhase struct {
    // existing fields...
    Produces []Artifact `json:"produces,omitempty"` // NEW
    Consumes []Artifact `json:"consumes,omitempty"` // NEW
}
```

`WritePlanMarkdown` renders phases with produces/consumes blocks for human review:

```markdown
## Phase 1: Refactor auth middleware
**Produces:**
- `internal/auth/middleware.go` (file) — new middleware interface
- "auth-middleware-design" (decision) — chosen approach

**Consumes:** none

### Steps
1. Extract interface from existing middleware
2. Implement new middleware
3. Update tests

## Phase 2: Migrate callers
**Consumes:**
- `internal/auth/middleware.go` (file, required) — new interface

### Steps
1. Update handler A
2. Update handler B
```

### Risk #1 mitigations: malformed phases

The output schema is more complex than flat steps. Four-layer defense:

**Layer A — Tight prompt with exemplar (cheapest, biggest impact).** `decompose_spec.md` ships with a complete worked JSON example in the prompt itself. Empirically drops malformed-output rates from ~30% to <5%.

**Layer B — Lenient parser with strict validator + auto-repair.** Two-stage parsing: extract JSON leniently, unmarshal strictly, then validate-and-repair (drop empty phases, cap count, repair invalid enum kinds to "file", drop dangling consumes references, repair out-of-range depends_on indices).

**Layer C — Retry-with-feedback loop.** If parse fails entirely, one retry with the error message appended to the prompt. LLMs fix their own mistakes ~70% of the time when shown the error.

**Layer D — Graceful fallback.** If all retries fail, fall back to single-phase flat decomposition. User sees spec_plan executed as regular flat plan instead of multi-phase. Worse fidelity, work proceeds. Logged for diagnosis.

Expected outcome: ~95% of spec_plan requests produce well-formed phases; ~5% fall back to flat plan with warning log. Zero hard failures.

### Changes summary

| File | Change |
|------|--------|
| `internal/agent/registry.go` | Add `GetModelConfig(agentID)` method |
| `internal/agent/orchestrator.go` | Add `registry`, `templateReg`, `artifacts` fields; `chunkToExecutorCapacity`, `splitStep`, `checkPhaseReady`, `startNextPhase`, `renderPhaseStartup`; subscribe to `llm.context_compressed` |
| `internal/agent/strategic.go` | Add `planMultiPhase`, `PlanPhaseSpec`, `Artifact`, `plannerPhaseOutput`; planner emits phases with produces/consumes |
| `internal/agent/tactical.go` | `selectAgent` factored as `SelectAgentForHint`; phase-aware step scheduling |
| `internal/plan/plan.go` | Add `Produces`, `Consumes` to `PlanPhase`; add `ArtifactStore` type |
| `internal/plan/store_sqlite.go` | Persist produces/consumes JSON in `plan_phases` |
| `internal/plan/writer.go` | Render produces/consumes in plan.md |
| `internal/plan/manager.go` | `Synthesize` handles multi-phase plans from both plan.md and LLM sources uniformly |
| `internal/agent/handler.go` | Forward phase metadata via plan bus events |
| `internal/daemon/components.go` | Wire `registry` + `templateReg` into orchestrator |
| `config/schema.go` | Replace `MaxPlanSteps` with `MaxStepsPerPhase`, `MaxPhases` |
| `config/meept.json5` | Update config template (no `max_plan_steps`) |
| `config/prompts/orchestrator/split.md` | New template (content above) |
| `config/prompts/planner/decompose_spec.md` | New template (content above) |
| `internal/agent/orchestrator_*_test.go` | New tests |
| `internal/tui/` | Phase panel component in plan view |
| `ui/flutter_ui/lib/features/plans/` | Phase list with produces/consumes |
| `internal/comm/http/api_handlers.go` | `GET /api/v1/plans/{id}/phases` |
| `internal/services/plan_service.go` | Phase retrieval service |

### Wiring

- **CLI:** `meept plans show <id>` renders phases with produces/consumes
- **TUI:** plan view shows phase panel; phase transitions highlighted
- **GUI:** Flutter plan view renders phases
- **HTTP API:** `/api/v1/plans/{id}/phases` GET
- **Agent:** tactical scheduler uses phase metadata for dependency resolution

## Thread B — Context Isolation

### B-a: Structured handoff (replaces 500-char truncation)

New types:

```go
// internal/agent/handoff.go (new file)
type StepHandoff struct {
    StepID          string          `json:"step_id"`
    StepDescription string          `json:"step_description"`
    Summary         string          `json:"summary"`
    FilesModified   []FileChange    `json:"files_modified"`
    Decisions       []Decision      `json:"decisions"`
    Artifacts       []Artifact      `json:"artifacts"`
    FollowUpHints   []string        `json:"follow_up_hints"`
    ToolHighlights  []ToolHighlight `json:"tool_highlights"`
    ErrorCode       string          `json:"error_code,omitempty"`
}

type FileChange struct {
    Path    string `json:"path"`
    Change  string `json:"change"` // created|modified|deleted
    Summary string `json:"summary"`
}

type Decision struct {
    Name      string `json:"name"`
    Rationale string `json:"rationale"`
}

type ToolHighlight struct {
    Tool    string `json:"tool"`
    Summary string `json:"summary"`
}
```

**Generation: separate pass (option ii).** After executor completes, separate LLM call (cheap classifier model, 500-token budget) summarizes tool calls / file changes / decisions into handoff struct. ~1 second latency per step. Uses `config/prompts/orchestrator/handoff.md` template.

```go
// internal/agent/orchestrator.go
func (o *Orchestrator) generateHandoff(ctx context.Context, step *task.TaskStep, conv *Conversation) (*StepHandoff, error) {
    // 1. Gather tool calls + results from conversation
    // 2. Render handoff.md template
    // 3. Call classifier-model LLM, expect JSON StepHandoff
    // 4. Parse + validate (reuse auto-repair patterns from Thread C)
}
```

Propagation replaces `propagateContextToNextSteps`. Dependent steps receive prior step's `StepHandoff` rendered as markdown (~1-2KB instead of 500 chars).

### B-b: Per-task-per-agent loops (maximum optimization)

Change `AgentRegistry.loops` keying from `map[string]*AgentLoop` (agentID) to `map[string]map[string]*AgentLoop` (agentID → taskID → loop).

**Maximum optimization:** share `PromptBuilder` (~3 KB) and `FilteredToolRegistry` (~1 KB) per agentID — they don't vary per task. Per-task loops keep only genuinely per-task state.

```go
// internal/agent/registry.go
type agentSharedState struct {
    promptBuilder *PromptBuilder
    filteredTools ToolRegistry
    spec          *AgentSpec
}

type AgentRegistry struct {
    // existing fields...
    agentState map[string]*agentSharedState            // per-agentID shared state
    loops      map[string]map[string]*AgentLoop        // agentID → taskID → loop
}

func (r *AgentRegistry) GetForTask(agentID, taskID string) (*AgentLoop, error) {
    // Lazy-create agentSharedState on first access for this agentID
    // Lazy-create loop on first access for this (agentID, taskID)
    // Loop shares state.promptBuilder, state.filteredTools
}

func (r *AgentRegistry) ReleaseTaskLoops(taskID string) {
    // Called by orchestrator on task completion
    // Iterates all agentID buckets, removes taskID entries
}
```

**Memory cost:** per-task-per-agent loop drops from ~7 KB to ~3 KB. Worst case 180 loops (10 tasks × 18 agents) = ~540 KB. Essentially free against a baseline measured in MB (dominated by shared `ConversationStore`).

Existing `Get(agentID)` becomes backward-compat wrapper using synthetic `"_default"` task ID. Non-task callers (CLI one-shots, manual RPCs) continue to work unchanged.

### B-c: Phase-level context reset

Depends on Thread C+F's phases. Covered in Thread C+F's `startNextPhase`.

### Updated conversationID conventions

| Context | conversationID format | Source |
|---------|----------------------|--------|
| Direct dispatch | session/thread conversationID | unchanged |
| Planner | `plan-<taskID>-<random>` | unchanged |
| Interview | `interview-<taskID>-<random>` | unchanged |
| Non-phased step | `step-<taskID>-<stepID>` | unchanged |
| Phased step | `phase-<phaseID>-<stepID>` | NEW |

### Changes summary

| File | Change |
|------|--------|
| `internal/agent/handoff.go` | NEW — `StepHandoff` types + render helper |
| `internal/agent/orchestrator.go` | `generateHandoff`, `startNextPhase`, `renderPhaseStartup`; subscribe to phase completion events; replace `propagateContextToNextSteps` with handoff-based propagation |
| `internal/agent/tactical.go` | Deprecate `propagateContextToNextSteps` (fallback); use handoff when available |
| `internal/agent/registry.go` | `agentSharedState`, nested `loops` map; `GetForTask`, `ReleaseTaskLoops`; keep `Get` as backward-compat wrapper |
| `internal/agent/orchestrator.go` | Call `registry.ReleaseTaskLoops(taskID)` on task completion |
| `internal/daemon/components.go` | `AgentJobProcessor.Process` uses `GetForTask` with the job's task ID |
| `internal/agent/strategic.go` | Phased steps get `ConversationID` set per new convention |
| `config/prompts/orchestrator/handoff.md` | NEW template (content above) |
| `internal/agent/orchestrator_handoff_test.go` | NEW tests |
| `internal/agent/orchestrator_phase_reset_test.go` | NEW tests |
| `internal/agent/registry_test.go` | Tests for task-scoped loops + cleanup |
| `internal/agent/loop_test.go` | Test two tasks get distinct loops even for same agentID |

### Wiring

- **CLI:** `meept plans show <id>` shows phase-level handoff summary
- **TUI:** plan view shows handoff summary on phase transition
- **GUI:** Flutter plan view renders handoff summaries
- **HTTP API:** `/api/v1/plans/{id}/handoffs` GET (phase-level handoffs for debugging)
- **Agent:** transparent (orchestrator produces handoffs; executors consume via AccumulatedContext)

## Thread E — Immediate Self-Reflection

### Goal

Bring Turbo-style immediate self-reflection to meept. Replace `patterns.json` with meept skills (SKILL.md). Wire Q Agent into the daemon with daily periodic skill creation, reporting to the user. Richer trajectory capture. Propose-only authorization for user-facing files.

**Note:** Q Agent rework itself (LLM access, dedicated `config/agents/q/AGENT.md`, skill creation path) is **deferred to a separate plan** (`docs/superpowers/specs/YYYY-MM-DD-q-agent-rework-design.md`, TBD). Thread E references but does not block on Q rework.

### Architecture

```
Agent Loop (per turn)
    ↓
After-turn hook (loop.go:1582)
    ↓
[NEW] ReflectionCollector
    ├─ Build rich trajectory (tool calls + results + errors + retries)
    ├─ Per-turn reflection LLM call (cheap model)
    │   → operational lesson proposal
    └─ Output: ReflectionProposal { type, target, change, justification, confidence }
    ↓
[NEW] ProposalRouter
    ├─ Routes proposals to .meept/improvements.md (propose-only)
    ├─ Auto-applies proposals under .meept/skills/auto/ (low-risk, auto-generated skills)
    └─ Logs all proposals for audit
    ↓
[NEW] /remember tool AND slash command
    └─ Both agent and user can invoke; creates manual proposal immediately
    ↓
[NEW] Periodic reflection timer (every 30 min)
    └─ Examines inactive sessions (>=15 min since last activity)
        → deeper reflection LLM call → higher-quality proposals
    ↓
[NEW] Daemon-wired Q Agent (daily — separate plan for full rework)
    ├─ References Q rework plan; in this plan we only reserve the integration point
    └─ Q skill proposals land in same .meept/improvements.md queue
    ↓
[EXISTING] /implement-improvements command (CLI/TUI/Flutter)
    └─ User reviews queue → applies approved proposals
```

### Component 1: Richer trajectory

```go
// internal/agent/trajectory.go (new file)
type Trajectory struct {
    UserInput      string           `json:"user_input"`
    Steps          []TrajectoryStep `json:"steps"`
    FinalResponse  string           `json:"final_response"`
    SessionID      string           `json:"session_id"`
    AgentID        string           `json:"agent_id"`
    Outcome        string           `json:"outcome"` // success|partial|failure
    Duration       time.Duration    `json:"duration"`
}

type TrajectoryStep struct {
    Kind       string `json:"kind"`              // assistant_message|tool_call|tool_result|error
    Content    string `json:"content"`
    ToolName   string `json:"tool_name,omitempty"`
    ToolResult string `json:"tool_result,omitempty"` // 500-char truncated
    ErrorCode  string `json:"error_code,omitempty"`
    RetryOf    string `json:"retry_of,omitempty"`
}
```

Truncation: assistant messages 1000 chars, tool results 500 chars, errors 300 chars. Trajectory cap: 50 steps.

### Component 2: ReflectionCollector

```go
// internal/agent/reflection_collector.go (new file)
type ReflectionCollector struct {
    classifier   *llm.Client
    deeper       *llm.Client
    templateReg  *templates.Registry
    proposalPath string
    logger       *slog.Logger
}

func (rc *ReflectionCollector) ReflectTurn(ctx context.Context, traj Trajectory) error {
    // 1. Render config/prompts/reflection/turn.md
    // 2. Call classifier LLM, expect JSON proposal or null
    // 3. If proposal && confidence >= 0.6: append to .meept/improvements.md
    // 4. Publish reflection.proposal_added bus event
}

func (rc *ReflectionCollector) ReflectInactiveSessions(ctx context.Context) {
    // Called by 30-min timer
    // 1. Query sessions with lastActivity < now - 15min AND not yet reflected
    // 2. For each: gather all trajectories from session
    // 3. Render session.md
    // 4. Call deeper LLM with 0-3 proposals expected (confidence >= 0.7)
    // 5. Append each to .meept/improvements.md
    // 6. Mark session as reflected
}
```

### Component 3: Proposal queue file

`.meept/improvements.md` (project-local, gitignored by default):

```markdown
# Improvement Proposals

<!-- Auto-generated by reflection. Review and apply with /implement-improvements -->

## [pending] 2026-06-24-skill-create-001 — Create skill: rust-borrow-checker-debugging
- **Type:** skill_create
- **Target:** .meept/skills/rust-borrow-checker-debugging/SKILL.md
- **Confidence:** 0.78
- **Source:** turn:session-abc123
- **Justification:** User hit borrow checker errors 4 times. Pattern: sharing mutable refs across closures.
- **Proposed content:** <full SKILL.md markdown>

## [pending] 2026-06-24-agent-prompt-001 — Update coder AGENT.md
- **Type:** agent_prompt
- **Target:** config/agents/coder/AGENT.md
- **Confidence:** 0.65
- **Source:** session:session-def456
- **Justification:** Coder agent forgot to run `go vet` after edits across 6 sessions.
- **Proposed change:** Add to restrictions: "After every file edit, run `go vet` on the changed package."
```

### Component 4: `/remember` — both agent tool AND user slash command

**Agent tool** (`internal/tools/builtin/remember.go`):
```go
type RememberTool struct { ... }
// Agent invokes /remember "always check go.sum before committing vendor changes"
// → creates ReflectionProposal with source "manual:/remember"
```

**User slash command** (`internal/tui/command_handler.go` or equivalent):
```
/remember <text>   → same proposal creation path
```

Both write to `.meept/improvements.md` with source `manual:/remember`.

### Component 5: Skills replace patterns.json

- `ContextInjector` (`context_injector.go:48`) loads relevant skills instead of patterns
- `LearningPipeline.Distill` and `StorePattern` stop writing to patterns.json
- `triggerLearning` call at `loop.go:1582` is replaced by `ReflectionCollector.ReflectTurn`
- Consolidation logic (dedup, contradiction detection, confidence decay) applies to skill metadata (`confidence`, `last_used`, `success_count` fields on skill frontmatter)
- **No migration of patterns.json** — existing file left in place but ignored; users can delete it

### Component 6: End-of-session trigger — TIMER ONLY

No reliance on session-end events. 30-minute timer queries for inactive sessions:

```go
// internal/daemon/components.go
reflectionTicker := time.NewTicker(30 * time.Minute)
go func() {
    for {
        select {
        case <-reflectionTicker.C:
            rc.ReflectInactiveSessions(ctx)
        case <-daemonShutdown:
            reflectionTicker.Stop()
            return
        }
    }
}()
```

Rationale (per user direction): "there is no meaningful 'ended' session." Timer-based reflection on inactivity is more reliable than lifecycle events that may never fire.

### Component 7: `/implement-improvements` command

Processes `.meept/improvements.md` queue:
1. Lists pending proposals (CLI: `meept improvements list`; TUI/Flutter: dedicated screen)
2. For each: show diff, ask user (y/N/edit/skip)
3. On "y": apply (write SKILL.md, edit AGENT.md, etc.)
4. On "edit": open in `$EDITOR`, then apply
5. Mark as `[applied]` in queue file with timestamp

### Authorization model

| Target | Default | Configurable |
|--------|---------|-------------|
| `.meept/improvements.md` (queue) | Auto-write | `reflection.auto_queue = true` |
| SKILL.md under `.meept/skills/auto/` | Auto-write | `reflection.auto_skill = true` |
| SKILL.md (new, from proposal) | Propose-only | `reflection.skill_proposals_only = true` |
| AGENT.md | **Always propose-only** | never overridable |
| CLAUDE.md | **Always propose-only** | never overridable |
| config/prompts/*.md | **Always propose-only** | never overridable |

User can globally override with `reflection.auto_apply_all = true` for skill/auto targets, but **AGENT.md, CLAUDE.md, and config/prompts/*.md are always propose-only regardless** — `auto_apply_all` does not override these.

### Changes summary

| File | Change |
|------|--------|
| `internal/agent/reflection_collector.go` | NEW — ReflectionCollector |
| `internal/agent/trajectory.go` | NEW — Trajectory types + builder |
| `internal/agent/loop.go` | Replace `triggerLearning` (line 1582) with `ReflectionCollector.ReflectTurn`; enrich trajectory |
| `internal/agent/context_injector.go` | Load skills instead of patterns |
| `internal/selfimprove/learning.go` | Stop pattern writes; keep consolidation logic, apply to skills |
| `internal/tools/builtin/remember.go` | NEW — `/remember` agent tool |
| `internal/tui/command_handler.go` | `/remember` slash command handler |
| `internal/daemon/components.go` | Wire ReflectionCollector + 30-min timer |
| `cmd/meept/implement_improvements.go` | NEW — CLI command |
| `internal/comm/http/api_handlers.go` | `GET/POST /api/v1/reflection/proposals` |
| `internal/services/reflection_service.go` | NEW — service layer for proposals |
| `config/prompts/reflection/turn.md` | NEW template (content above) |
| `config/prompts/reflection/session.md` | NEW template (content above) |
| `config/meept.json5` | Add reflection config block |
| `internal/agent/reflection_test.go` | NEW tests |
| `internal/tui/improvements/` | NEW TUI screen for proposal review |
| `ui/flutter_ui/lib/features/reflection/` | NEW Flutter reflection panel |
| `ui/flutter_ui/lib/features/notifications/` | Notification banners for reflection proposals |

### Wiring

- **CLI:** `meept improvements list/apply/skip`; `meept config reflection`
- **TUI:** `/remember` slash command; `/implement-improvements` review screen; notification toast on proposal captured
- **GUI:** Flutter reflection panel (list, apply, edit); notification banner
- **HTTP API:** `GET/POST /api/v1/reflection/proposals`, `POST /api/v1/reflection/remember`
- **Agent:** `/remember` tool registered; loop calls `ReflectionCollector.ReflectTurn` after each turn

## Q Agent Rework — Separate Plan (Placeholder)

Per user direction, the Q Agent rework is its own spec. Scope of the separate plan:

1. Create `config/agents/q/AGENT.md` — dedicated Q agent definition
2. Add `llm.Chatter` to Q Agent constructor + `model` field to `QAgentConfig`
3. Rework skill creation path: LLM-generated skills with format validation
4. Rework skill update path: preserve frontmatter, version bump
5. Daemon-wire with daily scheduler
6. User notification via bus event + TUI/CLI/Flutter
7. Parse-retry loop (5 attempts, no graceful fallback — Q runs in background, has time)
8. `meept q status` reporting
9. Flutter UI for Q Agent notifications

**Current Q Agent state:** purely heuristic, no LLM, no model config, hardcoded rules in `internal/agent/q/`. CLI-only, daemon-unwired.

Thread E reserves the integration point (Q proposals land in `.meept/improvements.md`) but does not implement Q Agent changes.

Placeholder spec path: `docs/superpowers/specs/YYYY-MM-DD-q-agent-rework-design.md` (to be brainstormed separately).

## Implementation Sequence

```
Phase 1: Thread A (markdown templates)        — no dependencies
Phase 2: Thread D (complexity routing)        — depends on Thread A (mode-specific templates)
Phase 3: Thread C+F (orchestrator chunking    — depends on Thread D (mode signal)
         + phases + produces/consumes)
Phase 4: Thread B (context isolation)         — B-c depends on Thread C+F;
                                                B-a (handoff) and B-b (per-task loops)
                                                parallel with Phase 3
Phase 5: Thread E (self-reflection)           — independent; needs markdown templates (Thread A)
                                                for reflection prompts; doesn't depend on C+D+F+B

Parallel track: Q Agent rework                — separate plan
```

### Cross-thread integration points

| Integration | Where threads meet | Implementation note |
|-------------|-------------------|---------------------|
| Mode-aware templates | A → D | decompose.md (plan), decompose_spec.md (spec_plan), interview.md all in `config/prompts/planner/` |
| Phase metadata in handoff | C+F → B | `StepHandoff.Artifacts` and `PlanPhaseSpec.Produces/Consumes` share `Artifact` type. Define once. |
| Phase-aware context reset | C+F → B-c | `startNextPhase` operates on `PlanPhase` records produced by `planMultiPhase` |
| Mode-aware executor sizing | D → C | spec_plan mode triggers multi-phase decomposition, which triggers orchestrator chunking |
| Skills replace patterns | E (internal) | Cutover in one atomic change within Thread E |
| Q Agent as skill source | E → Q rework | Q proposals land in same `.meept/improvements.md` queue |
| Per-task loops + handoff | B-b + B-a | Both touch `AgentJobProcessor.Process`; land together |

### Files touched by multiple threads (coordination points)

| File | A | D | C+F | B | E |
|------|---|---|-----|---|---|
| `internal/agent/strategic.go` | ✓ | ✓ | ✓ | | |
| `internal/agent/orchestrator.go` | | | ✓ | ✓ | |
| `internal/agent/registry.go` | | | ✓ | ✓ | |
| `internal/daemon/components.go` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `internal/agent/loop.go` | | | | ✓ | ✓ |
| `internal/config/schema.go` | | ✓ | ✓ | | ✓ |
| `config/meept.json5` | | ✓ | ✓ | | ✓ |

## Verification Strategy

| Thread | Verification |
|--------|--------------|
| A | `meept config prompts planner decompose` shows markdown source; editing `.meept/prompts/planner/decompose.md` changes planner behavior |
| D | `meept chat "what's X"` → mode=direct ACK; `meept chat "/plan refactor auth"` → mode=plan; `meept chat "rebuild the search subsystem"` → mode=spec_plan |
| C+F | Plan with multiple phases renders in `meept plans show <id>` with produces/consumes blocks; oversized step gets split (visible in logs) |
| B | Phase 2's prompt doesn't contain phase 1's tool outputs (only consumes); two concurrent tasks' loops are distinct Go objects |
| E | `.meept/improvements.md` accumulates proposals after sessions; `/implement-improvements` applies them; `/remember "..."` works as both agent tool and user slash command |

## Migration / Deprecation

- `max_plan_steps` config key: removed from sample configs. If user has it set, log deprecation warning, ignore value.
- `patterns.json`: not migrated. Daemon stops writing to it; existing file ignored.
- `plannerPromptTemplate` / `interviewPromptTemplate` consts: removed; bundled `config/prompts/planner/*.md` files become source of truth.
- `shouldDecompose` / `shouldUsePairSession` / `ConductInterview` ambiguity gate: removed; replaced by mode switch + `shouldInterview`. Legacy path preserved via `inferLegacyMode` for empty-mode requests during rollout.

## Risks

| # | Risk | Mitigation |
|---|------|-----------|
| 1 | Planner LLM produces malformed phase JSON | 4-layer defense: exemplar prompt + lenient parser/repair + retry-with-feedback + graceful fallback (detailed in Thread C+F) |
| 2 | LLM analyzer misjudges mode | Rule-based fallback covers when SuggestedMode empty/invalid; input-length downgrade catches "what is X" mis-classified |
| 3 | `spec_plan` depends on Thread C+F | If C+F hasn't landed, `planMultiPhase` falls back to single-phase. Documented degradation |
| 4 | Removing `shouldDecompose` breaks tests | `inferLegacyMode` preserves behavior for empty-mode requests during rollout |
| 5 | Token estimation is rough | ContextFirewall still runs as safety net; chunking is best-effort |
| 6 | Phase dependencies create serialization | Only mark `Required: true` consumes; optional consumes allow best-effort parallelism |
| 7 | Orchestrator becomes stateful | All chunking state lives in existing stepStore/planStore, not on orchestrator struct. Orchestrator remains a transformer |
| 8 | Reactive re-chunking cascades | Per-task split counter with cap (5 splits per task) |
| 9 | Handoff generation adds latency (~1s/step) | Use cheap classifier model; only generate handoff for steps with dependents |
| 10 | Handoff LLM produces low-quality summaries | Include original tool call data as fallback; allow dependents to request full history via explicit tool (future) |
| 11 | Phase reset loses useful context | Explicit `produces` discipline in planner template; `FollowUpHints` in handoff flags important context |
| 12 | Reflection LLM floods queue with noise | Confidence threshold (0.6 turn / 0.7 session); per-session proposal cap (3) |
| 13 | Auto-applying CLAUDE.md with garbage | AGENT.md, CLAUDE.md, config/prompts/*.md are always propose-only regardless of `auto_apply_all` |

## Out of Scope

- Q Agent rework internals (LLM access, dedicated agent definition, skill creation path) — separate plan
- Flutter UI specifics for Q Agent notifications — Q rework plan
- Per-task-per-agent loop keying of ConversationStore (it stays shared)
- Removing the existing plan.md-driven phase path (PlanManager). Both LLM-driven and human-authored multi-phase plans coexist
- Changing the Session abstraction (stays as user-facing coordinator)
- Touching pair sessions, collaboration engine, or steering heuristics
- Migrating existing patterns.json content
