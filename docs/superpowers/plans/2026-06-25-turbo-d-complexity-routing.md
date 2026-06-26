# Turbo Thread D — Complexity Routing

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the planner's `shouldDecompose` + `shouldUsePairSession` + ambiguity-only interview gate with a single dispatcher-synthesized `SuggestedMode` field (`direct` | `plan` | `spec_plan` | `spec_pair`). Dispatcher owns synthesis via a pure function; thresholds stay separate and become config-file-exposed. Planner switches on `PlanRequest.Mode`.

**Architecture:**
1. Add `SuggestedMode` field to `TrueIntentAnalysis`, `Intent`, `DispatchResult`, and `PlanRequest`.
2. Add `IntentType.SuggestedMode()` method as the rule-based fallback.
3. Add pure function `suggestMode(intentType, analysis, input) string` in the dispatcher, called between `classifyIntent` and `routeToPlan`.
4. Replace `StrategicPlanner.Plan` heuristics with a `switch req.Mode` block; preserve empty-mode behavior via `inferLegacyMode`.
5. Add `decompose_spec.md` bundled template (used by `spec_plan` mode — actual `planMultiPhase` implementation lives in Thread C+F's plan; this thread only adds the template file so the routing layer can reference `spec_plan` without breaking).
6. Expose `dispatcher.ambiguity_threshold` and `planner.interview_ambiguity_threshold` via `meept.json5`.
7. Surface mode in `FormatEnhancedAsyncTaskAck` and Flutter ACK bubble.

**Tech Stack:** Go, existing dispatcher + planner + intent analyzer, JSON5 config.

**Depends on:** Thread A (markdown template loader — `decompose_spec.md` uses the same loader).

**Spec source:** `docs/superpowers/specs/2026-06-24-turbo-innovations-adoption-design.md` — Thread D.

---

## File Structure

| File | Responsibility |
|------|----------------|
| `config/prompts/planner/decompose_spec.md` | NEW — bundled template for multi-phase decomposition (referenced by `planMultiPhase`, implemented in Thread C+F) |
| `internal/agent/intent.go` | MODIFY — add `IntentType.SuggestedMode()` method |
| `internal/agent/intent_analyzer.go` | MODIFY — add `SuggestedMode` field; update LLM prompt + parser |
| `internal/agent/dispatcher.go` | MODIFY — add `suggestMode` pure function; add `SuggestedMode` to `Intent` and `DispatchResult`; call `suggestMode` in `ClassifyAndRoute` |
| `internal/agent/strategic.go` | MODIFY — add `Mode` field to `PlanRequest`; add `Plan()` mode switch; add `inferLegacyMode`; convert `interviewAmbiguity` to config-driven field; remove `shouldDecompose`/`shouldUsePairSession`/`ConductInterview` ambiguity gate (replaced by `shouldInterview`) |
| `internal/agent/handler.go` | MODIFY — `publishPlanRequest` forwards `Mode` + `TrueAnalysis` into `PlanRequest` |
| `internal/agent/handler.go` (FormatEnhancedAsyncTaskAck) | MODIFY — surface mode label in ACK |
| `internal/config/schema.go` | MODIFY — add `AmbiguityThreshold` + `InterviewAmbiguityThreshold` fields |
| `config/meept.json5` | MODIFY — document new keys with defaults |
| `internal/daemon/components.go` | MODIFY — wire new thresholds from config |
| `internal/agent/dispatcher_test.go` | MODIFY — tests for `suggestMode` |
| `internal/agent/strategic_test.go` | MODIFY — tests for mode switch + `inferLegacyMode` |
| `internal/agent/intent_analyzer_test.go` | MODIFY — test `SuggestedMode` parse/validation |
| `internal/tui/handlers/task_events.go` | MODIFY — display mode in ACK |
| `ui/flutter_ui/lib/features/chat/` | MODIFY — mode label in ACK bubble |
| `ui/flutter_ui/lib/features/settings/` | MODIFY — threshold settings |

**Note on `planMultiPhase` / `planPairSession`:** These methods do not yet exist. For Thread D, the `Plan()` switch dispatches to them but they can be thin wrappers that fall back to `planSinglePhase` for now. Thread C+F's plan implements the real `planMultiPhase`; the existing `createPairSessionPlan` (which `shouldUsePairSession` calls today) is renamed `planPairSession`.

---

## Task 1: Add `decompose_spec.md` template

**Files:**
- Create: `config/prompts/planner/decompose_spec.md`

- [ ] **Step 1: Create the file**

Exact content (verbatim from spec §Thread A → "decompose_spec.md"):

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

- [ ] **Step 2: Register fallback for `decompose_spec.md` in `plannerTemplateLoader`**

In `internal/agent/planner_template.go`, add a third fallback in `NewDaemonPlannerTemplateLoader`:

```go
	l.fallbacks["planner/decompose_spec.md"] = defaultDecomposeSpecFallback()
```

And add the fallback const (mirror the markdown above):

```go
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

func defaultDecomposeSpecFallback() string { return decomposeSpecFallbackBody }
```

- [ ] **Step 3: Commit**

```bash
git add config/prompts/planner/decompose_spec.md internal/agent/planner_template.go
git commit -m "feat(planner): add decompose_spec.md template for spec_plan mode"
```

---

## Task 2: `IntentType.SuggestedMode()` and tests

**Files:**
- Modify: `internal/agent/intent.go`
- Modify: `internal/agent/intent_test.go` (create if absent)

- [ ] **Step 1: Write the failing test**

`internal/agent/intent_test.go`:

```go
package agent

import "testing"

func TestIntentType_SuggestedMode(t *testing.T) {
	cases := []struct {
		in   IntentType
		want string
	}{
		{IntentChat, "direct"},
		{IntentRecall, "direct"},
		{IntentStatus, "direct"},
		{IntentReport, "direct"},
		{IntentPlatform, "direct"},
		{IntentSearch, "direct"},
		{IntentCode, "plan"},
		{IntentDebug, "plan"},
		{IntentGit, "plan"},
		{IntentToolUse, "plan"},
		{IntentSecurity, "plan"},
		{IntentCompound, "spec_pair"},
		{IntentPlan, "spec_plan"},
		{IntentArchitect, "spec_plan"},
		{IntentUnknown, "plan"}, // default
	}
	for _, c := range cases {
		got := c.in.SuggestedMode()
		if got != c.want {
			t.Errorf("%s.SuggestedMode() = %q; want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestIntentType_SuggestedMode -v`
Expected: FAIL — `c.in.SuggestedMode undefined`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/agent/intent.go` (after the existing `Category()` method, around line 92):

```go
// SuggestedMode returns the default planning mode for this intent type.
// Modes: "direct" (no LLM decomposition), "plan" (single-phase LLM plan),
// "spec_plan" (multi-phase LLM plan with Produces/Consumes), "spec_pair"
// (pair session).
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestIntentType_SuggestedMode -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/intent.go internal/agent/intent_test.go
git commit -m "feat(intent): add IntentType.SuggestedMode() rule-based fallback"
```

---

## Task 3: `suggestMode` pure function and tests

**Files:**
- Modify: `internal/agent/dispatcher.go`
- Modify: `internal/agent/dispatcher_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/agent/dispatcher_test.go`:

```go
func TestSuggestMode(t *testing.T) {
	cases := []struct {
		name       string
		intentType IntentType
		analysis   *TrueIntentAnalysis
		input      string
		want       string
	}{
		{name: "compound forces spec_pair", intentType: IntentCompound, analysis: nil, input: "x", want: "spec_pair"},
		{name: "analysis spec_plan wins", intentType: IntentCode, analysis: &TrueIntentAnalysis{SuggestedMode: "spec_plan"}, input: "refactor the auth subsystem", want: "spec_plan"},
		{name: "analysis invalid falls back", intentType: IntentCode, analysis: &TrueIntentAnalysis{SuggestedMode: "garbage"}, input: "refactor the auth subsystem", want: "plan"},
		{name: "short input downgrades plan→direct", intentType: IntentCode, analysis: &TrueIntentAnalysis{SuggestedMode: ""}, input: "fix typo", want: "direct"},
		{name: "short input does not downgrade spec_plan", intentType: IntentCode, analysis: &TrueIntentAnalysis{SuggestedMode: "spec_plan"}, input: "fix", want: "spec_plan"},
		{name: "default fallback for unknown", intentType: IntentUnknown, analysis: nil, input: "something longer than fifty characters total", want: "plan"},
		{name: "empty analysis + long input uses rule", intentType: IntentDebug, analysis: nil, input: "investigate the production outage that happened yesterday at 3am", want: "plan"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := suggestMode(c.intentType, c.analysis, c.input)
			if got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestValidateMode(t *testing.T) {
	cases := []struct {
		in   string
		want string // normalized, "" if invalid
	}{
		{"direct", "direct"},
		{"plan", "plan"},
		{"spec_plan", "spec_plan"},
		{"spec_pair", "spec_pair"},
		{"SPEC_PLAN", ""}, // case-sensitive
		{"", ""},
		{"garbage", ""},
	}
	for _, c := range cases {
		got := validateMode(c.in)
		if got != c.want {
			t.Errorf("validateMode(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestSuggestMode -v`
Expected: FAIL — `undefined: suggestMode`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/agent/dispatcher.go` (near the existing routing helpers, e.g., after `classifyIntent`):

```go
// validModes is the set of accepted SuggestedMode values.
var validModes = map[string]struct{}{
	"direct":    {},
	"plan":      {},
	"spec_plan": {},
	"spec_pair": {},
}

// validateMode returns the mode if valid, empty string otherwise.
func validateMode(s string) string {
	if _, ok := validModes[s]; ok {
		return s
	}
	return ""
}

// suggestMode synthesizes the planning mode from intent type, optional
// analyzer suggestion, and input length. Pure function — unit-testable
// without a dispatcher.
//
// Priority:
//  1. IntentCompound → "spec_pair" (forced)
//  2. analysis.SuggestedMode (if valid)
//  3. intentType.SuggestedMode() (rule-based fallback)
//  4. Short-input downgrade: if input < 50 chars, mode is "direct"
//     (unless analysis explicitly overrode to spec_plan)
func suggestMode(intentType IntentType, analysis *TrueIntentAnalysis, input string) string {
	if intentType == IntentCompound {
		return "spec_pair"
	}
	analysisMode := ""
	if analysis != nil {
		analysisMode = validateMode(analysis.SuggestedMode)
	}
	if analysisMode != "" {
		// Short-input downgrade does NOT override an explicit spec_plan
		// suggestion from the analyzer.
		if analysisMode == "spec_plan" {
			return "spec_plan"
		}
		// For other analyzer-suggested modes, apply short-input downgrade.
		if len(input) < 50 {
			return "direct"
		}
		return analysisMode
	}
	mode := intentType.SuggestedMode()
	if mode != "spec_plan" && len(input) < 50 {
		return "direct"
	}
	return mode
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/ -run TestSuggestMode -v && go test ./internal/agent/ -run TestValidateMode -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/dispatcher.go internal/agent/dispatcher_test.go
git commit -m "feat(dispatcher): add suggestMode + validateMode pure functions"
```

---

## Task 4: Add `SuggestedMode` field to data model

**Files:**
- Modify: `internal/agent/intent_analyzer.go`
- Modify: `internal/agent/dispatcher.go`
- Modify: `internal/agent/strategic.go`

- [ ] **Step 1: Add `SuggestedMode` to `TrueIntentAnalysis`**

In `internal/agent/intent_analyzer.go`, the struct (lines 16-23) gains:

```go
type TrueIntentAnalysis struct {
	Goal               string   `json:"goal"`
	Ambiguity          float64  `json:"ambiguity"`
	Scope              string   `json:"scope"`
	Category           string   `json:"category"`
	SuggestedQuestions []string `json:"suggested_questions"`
	Confidence         float64  `json:"confidence"`
	SuggestedMode      string   `json:"suggested_mode,omitempty"`
}
```

Update the LLM system prompt in `analyzeTrueIntent` (lines 61-72): add `suggested_mode` to the documented JSON output schema with values `direct|plan|spec_plan|spec_pair` and a one-line description ("`direct` for trivial/lookup questions; `plan` for single-component work; `spec_plan` for multi-file or multi-phase work; `spec_pair` for compound requests").

Update `parseAnalysis` (lines 95-127): after parsing JSON, validate `SuggestedMode` via `validateMode(...)`. If invalid, set it to empty string (the rule-based fallback in `suggestMode` handles empty).

- [ ] **Step 2: Add `SuggestedMode` to `Intent` and `DispatchResult`**

In `internal/agent/dispatcher.go`:

```go
type Intent struct {
	// ... existing fields ...
	SuggestedMode string `json:"suggested_mode,omitempty"`
}

type DispatchResult struct {
	// ... existing fields ...
	SuggestedMode string `json:"suggested_mode,omitempty"`
}
```

- [ ] **Step 3: Add `Mode` to `PlanRequest`**

In `internal/agent/strategic.go`:

```go
type PlanRequest struct {
	// ... existing fields ...
	Mode string `json:"mode,omitempty"`
}
```

- [ ] **Step 4: Call `suggestMode` in `ClassifyAndRoute`**

In `internal/agent/dispatcher.go` `ClassifyAndRoute` (line 435), after `classifyIntent` returns the intent, before routing:

```go
	// Synthesize planning mode (Thread D).
	intent.SuggestedMode = suggestMode(IntentType(intent.Type), analysis, input)
```

Where `analysis` is the `*TrueIntentAnalysis` from the IntentGate step earlier in `ClassifyAndRoute`. If `analysis` is non-nil, also propagate the analyzer's suggestion: `analysis.SuggestedMode` (already populated by the analyzer's parse step).

When building `DispatchResult` later in the function, propagate: `result.SuggestedMode = intent.SuggestedMode`.

- [ ] **Step 5: Forward `Mode` and `TrueAnalysis` into `PlanRequest`**

In `internal/agent/handler.go` `publishPlanRequest` (line 733):

```go
func (h *ChatHandler) publishPlanRequest(result *DispatchResult, sessionID string) {
	req := PlanRequest{
		TaskID:       result.Task.ID,
		SessionID:    sessionID,
		Input:        result.Task.Description,
		Intent:       result.Intent.Type,
		Mode:         result.SuggestedMode,
		TrueAnalysis: result.Intent.TrueAnalysis,
	}
	// ... rest unchanged ...
}
```

- [ ] **Step 6: Build and run**

Run: `go build ./...`
Expected: clean build.

Run: `go test ./internal/agent/ -v -count=1`
Expected: PASS (any tests that previously asserted `Intent` field counts or JSON shapes may need updating).

- [ ] **Step 7: Commit**

```bash
git add internal/agent/intent_analyzer.go internal/agent/dispatcher.go internal/agent/strategic.go internal/agent/handler.go
git commit -m "feat(routing): thread SuggestedMode through analyzer→dispatcher→planner"
```

---

## Task 5: Planner mode switch + `inferLegacyMode` + `shouldInterview`

**Files:**
- Modify: `internal/agent/strategic.go`
- Modify: `internal/agent/strategic_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/agent/strategic_test.go`:

```go
func TestStrategicPlanner_inferLegacyMode(t *testing.T) {
	cases := []struct {
		name string
		req  PlanRequest
		want string
	}{
		{name: "compound → spec_pair", req: PlanRequest{Intent: string(IntentCompound), IsCompound: true}, want: "spec_pair"},
		{name: "chat intent → direct", req: PlanRequest{Intent: string(IntentChat), Input: "what's the weather"}, want: "direct"},
		{name: "code intent + long input → plan", req: PlanRequest{Intent: string(IntentCode), Input: strings.Repeat("a", 150)}, want: "plan"},
		{name: "plan intent → spec_plan", req: PlanRequest{Intent: string(IntentPlan)}, want: "spec_plan"},
		{name: "empty intent + short input → direct", req: PlanRequest{Intent: "", Input: "hi"}, want: "direct"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sp := &StrategicPlanner{simpleInputMaxChars: 100, pairInputMinChars: 200}
			got := sp.inferLegacyMode(c.req)
			if got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestStrategicPlanner_shouldInterview(t *testing.T) {
	sp := &StrategicPlanner{interviewAmbiguity: 0.6}
	cases := []struct {
		mode string
		req  PlanRequest
		want bool
	}{
		{mode: "direct", req: PlanRequest{}, want: false},
		{mode: "spec_plan", req: PlanRequest{}, want: true},
		{mode: "plan", req: PlanRequest{TrueAnalysis: &TrueIntentAnalysis{Ambiguity: 0.3}}, want: false},
		{mode: "plan", req: PlanRequest{TrueAnalysis: &TrueIntentAnalysis{Ambiguity: 0.7}}, want: true},
		{mode: "spec_pair", req: PlanRequest{}, want: false},
	}
	for _, c := range cases {
		got := sp.shouldInterview(c.req, c.mode)
		if got != c.want {
			t.Errorf("mode=%s got %v want %v", c.mode, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestStrategicPlanner_inferLegacyMode -v`
Expected: FAIL — `sp.inferLegacyMode undefined`.

- [ ] **Step 3: Write minimal implementation**

In `internal/agent/strategic.go`:

1. **Rename** `createPairSessionPlan` → `planPairSession` (just rename; body unchanged). Update the single call site in `Plan` that referenced the old name.

2. **Rename** `generatePlan` → `planSinglePhase` (just rename; update call site).

3. **Add** `inferLegacyMode`:

```go
// inferLegacyMode reconstructs a mode for empty-Mode requests, preserving
// the pre-Thread-D heuristics during rollout. Once all callers populate
// Mode, this becomes dead code.
func (sp *StrategicPlanner) inferLegacyMode(req PlanRequest) string {
	if req.IsCompound {
		return "spec_pair"
	}
	it := IntentType(req.Intent)
	switch it {
	case IntentChat, IntentRecall, IntentStatus, IntentReport, IntentPlatform, IntentSearch:
		return "direct"
	case IntentPlan, IntentArchitect:
		return "spec_plan"
	default:
		if len(req.Input) < sp.simpleInputMaxChars {
			return "direct"
		}
		return "plan"
	}
}
```

4. **Add** `shouldInterview`:

```go
// shouldInterview decides whether to conduct a design interview before
// decomposition, based on the planning mode.
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

5. **Refactor** the top of `Plan(ctx, req)` (line 339) to switch on mode. Replace the existing `shouldUsePairSession` + `shouldDecompose` + `ConductInterview` ambiguity gate with:

```go
func (sp *StrategicPlanner) Plan(ctx context.Context, req PlanRequest) error {
	sp.logger.Info("Starting strategic planning",
		"task_id", req.TaskID, "session_id", req.SessionID,
		"intent", req.Intent, "mode", req.Mode,
	)

	// Set task state to planning
	t, err := sp.taskStore.GetByID(req.TaskID)
	if err != nil || t == nil {
		return fmt.Errorf("task not found: %s", req.TaskID)
	}
	t.SetState(task.StatePlanning)
	if err := sp.taskStore.Update(t); err != nil {
		sp.logger.Error("Failed to update task state to planning", "error", err)
	}

	parentMemoryRefs := t.MemoryRefs

	mode := req.Mode
	if mode == "" {
		mode = sp.inferLegacyMode(req)
	}

	var steps []*task.TaskStep
	switch mode {
	case "direct":
		steps = sp.createFallbackSteps(req, parentMemoryRefs)
	case "plan":
		if sp.shouldInterview(req, mode) {
			pctx, interviewErr := sp.ConductInterview(ctx, req)
			// ... same interview flow handling as today ...
			if interviewErr == nil && pctx != nil && !pctx.InterviewCompleted {
				return sp.awaitInterviewAnswers(ctx, t, req, pctx, parentMemoryRefs)
			}
			if pctx != nil && pctx.InterviewCompleted {
				req.PlanningCtx = pctx
			}
		}
		var err error
		steps, err = sp.planSinglePhase(ctx, req)
		if err != nil {
			steps = sp.createFallbackSteps(req, parentMemoryRefs)
		}
	case "spec_plan":
		// spec_plan always interviews.
		pctx, interviewErr := sp.ConductInterview(ctx, req)
		if interviewErr == nil && pctx != nil && !pctx.InterviewCompleted {
			return sp.awaitInterviewAnswers(ctx, t, req, pctx, parentMemoryRefs)
		}
		if pctx != nil && pctx.InterviewCompleted {
			req.PlanningCtx = pctx
		}
		var err error
		steps, err = sp.planMultiPhase(ctx, req) // implemented in Thread C+F plan; for now falls back to planSinglePhase
		if err != nil {
			sp.logger.Warn("Multi-phase plan failed, falling back to single-phase", "error", err)
			steps, err = sp.planSinglePhase(ctx, req)
			if err != nil {
				steps = sp.createFallbackSteps(req, parentMemoryRefs)
			}
		}
	case "spec_pair":
		pairSteps, pairErr := sp.planPairSession(ctx, req, parentMemoryRefs)
		if pairErr != nil {
			sp.logger.Error("Failed to create pair session plan, falling back", "error", pairErr)
			steps = sp.createFallbackSteps(req, parentMemoryRefs)
		} else {
			steps = pairSteps
		}
	default:
		steps = sp.createFallbackSteps(req, parentMemoryRefs)
	}

	// Inject parent MemoryRefs to first step
	if len(steps) > 0 && len(parentMemoryRefs) > 0 {
		for _, ref := range parentMemoryRefs {
			steps[0].AddMemoryRef(ref)
		}
	}

	// Approval gate
	if sp.requiresApproval(req, steps) {
		return sp.awaitUserApproval(ctx, t, steps, req)
	}

	// Persist + schedule (same as today's tail)
	for _, step := range steps {
		if err := sp.stepStore.Create(step); err != nil {
			return fmt.Errorf("failed to persist steps: %w", err)
		}
	}
	spec := GenerateSpecFromSteps(steps)
	StoreSpecInTask(t, spec)
	t.TotalJobs = len(steps)
	t.SetState(task.StateExecuting)
	if err := sp.taskStore.Update(t); err != nil {
		sp.logger.Error("Failed to update task after planning", "error", err)
	}
	promoted, err := sp.stepStore.PromoteReadySteps(req.TaskID)
	if err != nil {
		sp.logger.Error("Failed to promote ready steps", "error", err)
	}
	sp.publishEvent("task.planned", map[string]any{
		KeyTaskID: req.TaskID, "session_id": req.SessionID,
		"total_steps": len(steps), "ready_steps": len(promoted), "mode": mode,
	})
	sp.publishEvent("orchestrator.schedule", map[string]any{KeyTaskID: req.TaskID})
	return nil
}
```

6. **Add a stub `planMultiPhase`** that Thread C+F will replace:

```go
// planMultiPhase is the multi-phase decomposition entry point. Full
// implementation lands in Thread C+F; for now we delegate to
// planSinglePhase so spec_plan mode degrades gracefully.
func (sp *StrategicPlanner) planMultiPhase(ctx context.Context, req PlanRequest) ([]*task.TaskStep, error) {
	sp.logger.Warn("planMultiPhase not yet implemented (Thread C+F); falling back to planSinglePhase",
		"task_id", req.TaskID,
	)
	return sp.planSinglePhase(ctx, req)
}
```

7. **Add `awaitInterviewAnswers`** helper that wraps the existing "publish task.interview + store pctx on task metadata + return nil" flow (extracted from current `Plan` lines 429-451 so both `plan` and `spec_plan` branches can use it):

```go
func (sp *StrategicPlanner) awaitInterviewAnswers(ctx context.Context, t *task.Task, req PlanRequest, pctx *plan.PlanningContext, parentMemoryRefs []string) error {
	sp.publishEvent("task.interview", map[string]any{
		KeyTaskID: req.TaskID, "session_id": req.SessionID,
		"questions": pctx.InterviewQuestions, "ambiguities": pctx.Ambiguities,
	})
	if pctxJSON, err := json.Marshal(pctx); err == nil {
		t.Metadata = json.RawMessage(pctxJSON)
		if err := sp.taskStore.Update(t); err != nil {
			sp.logger.Warn("Failed to update task with planning context", "error", err)
		}
	}
	sp.logger.Info("Interview questions sent, awaiting user answers",
		"task_id", req.TaskID, "question_count", len(pctx.InterviewQuestions),
	)
	return nil
}
```

8. **Delete** `shouldDecompose` and `shouldUsePairSession` methods.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/ -run TestStrategicPlanner_inferLegacyMode -v && go test ./internal/agent/ -run TestStrategicPlanner_shouldInterview -v`
Expected: PASS.

Run: `go test ./internal/agent/ -v -count=1`
Expected: PASS (existing planner tests may need updates if they called `shouldDecompose`/`shouldUsePairSession` directly — search and rewrite or delete those).

Run: `go build ./...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/strategic.go internal/agent/strategic_test.go
git commit -m "feat(planner): replace shouldDecompose/pair heuristics with PlanRequest.Mode switch"
```

---

## Task 6: Config-file thresholds

**Files:**
- Modify: `internal/config/schema.go`
- Modify: `config/meept.json5`
- Modify: `internal/daemon/components.go`
- Modify: `internal/agent/strategic.go` (constructor accepts the new field)

- [ ] **Step 1: Extend `OrchestratorConfig` in `schema.go`**

In `internal/config/schema.go`, the `OrchestratorConfig` struct (line 1248):

```go
type OrchestratorConfig struct {
	MaxPlanSteps             int     `json:"max_plan_steps"`
	MaxResearchSteps         int     `json:"max_research_steps"`
	PlannerTimeout           int     `json:"planner_timeout"`
	TokenBudgetAlert         int     `json:"token_budget_alert"`
	MaxHandoffSteps          int     `json:"max_handoff_steps"`
	HandoffUseAmendment      bool    `json:"handoff_use_amendment"`
	AmbiguityThreshold       float64 `json:"ambiguity_threshold"`         // dispatcher gate
	InterviewAmbiguityThreshold float64 `json:"interview_ambiguity_threshold"` // planner gate
	MaxStepsPerPhase         int     `json:"max_steps_per_phase"` // Thread C+F consumes; declared here for forward-compat
	MaxPhases                int     `json:"max_phases"`          // Thread C+F consumes; declared here for forward-compat
}
```

Update `DefaultConfig` (lines 1728-1731) to add:

```go
		AmbiguityThreshold:          0.6,
		InterviewAmbiguityThreshold: 0.6,
		MaxStepsPerPhase:            8,
		MaxPhases:                   12,
```

- [ ] **Step 2: Document in `config/meept.json5`**

In the `orchestrator` block (lines 521-530):

```json5
  "orchestrator": {
    "max_plan_steps": 10,                  // deprecated (Thread C+F removes)
    "max_research_steps": 3,
    "planner_timeout": 120,
    "token_budget_alert": 5000,
    "max_handoff_steps": 5,
    "handoff_use_amendment": true,
    "ambiguity_threshold": 0.6,            // dispatcher: blocks routing when analyzer ambiguity ≥ this
    "interview_ambiguity_threshold": 0.6,  // planner: conducts interview for plan-mode when ≥ this
    "max_steps_per_phase": 8,              // Thread C+F: per-phase step cap
    "max_phases": 12,                      // Thread C+F: phase count soft cap
  },
```

- [ ] **Step 3: Wire thresholds through `StrategicPlannerConfig`**

In `internal/agent/strategic.go` `StrategicPlannerConfig`:

```go
type StrategicPlannerConfig struct {
	// ... existing fields ...
	InterviewAmbiguity float64 // 0 = use default 0.6
}
```

In `NewStrategicPlanner`, replace the hardcoded `interviewAmbiguity: interviewAmbiguityThreshold`:

```go
	interviewAmb := cfg.InterviewAmbiguity
	if interviewAmb == 0 {
		interviewAmb = interviewAmbiguityThreshold // legacy const, kept as default source
	}
	return &StrategicPlanner{
		// ...
		interviewAmbiguity: interviewAmb,
		// ...
	}
```

- [ ] **Step 4: Update `components.go` to pass config values**

In `internal/daemon/components.go` `NewStrategicPlanner` call (line 1597):

```go
		strategicPlanner := agent.NewStrategicPlanner(agent.StrategicPlannerConfig{
			// ... existing ...
			InterviewAmbiguity: cfg.Orchestrator.InterviewAmbiguityThreshold,
			TemplateLoader:     agent.NewDaemonPlannerTemplateLoader("config/prompts"),
		})
```

Pass `AmbiguityThreshold` to the IntentAnalyzer at its construction site (search for `NewIntentAnalyzer` in `components.go` and add the option `agent.WithAmbiguityThreshold(cfg.Orchestrator.AmbiguityThreshold)`).

- [ ] **Step 5: Build and test**

Run: `go build ./...`
Expected: clean.

Run: `go test ./internal/config/... ./internal/agent/... -v -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/config/schema.go config/meept.json5 internal/agent/strategic.go internal/daemon/components.go
git commit -m "feat(config): expose ambiguity_threshold + interview_ambiguity_threshold via meept.json5"
```

---

## Task 7: ACK message surfaces mode

**Files:**
- Modify: `internal/agent/handler.go` (`FormatEnhancedAsyncTaskAck` at line 1074)
- Modify: `internal/tui/handlers/task_events.go`
- Modify: `ui/flutter_ui/lib/features/chat/` (find ACK rendering widget)

- [ ] **Step 1: Update `FormatEnhancedAsyncTaskAck`**

In `internal/agent/handler.go`, the `FormatEnhancedAsyncTaskAck` function (line 1074) builds a markdown string. Add a mode label near the `**plan:**` line:

```go
func (h *ChatHandler) FormatEnhancedAsyncTaskAck(
	result *DispatchResult,
	steps []TaskStepSummary,
	estimatedMinutes int,
	planRef string,
) string {
	// ... existing body ...

	modeLabel := modeToLabel(result.SuggestedMode) // "executing directly" / "planned (N steps)" / ...

	var sb strings.Builder
	sb.WriteString("## starting task\n\n")
	sb.WriteString(fmt.Sprintf("**task:** %s\n", result.Task.Description))
	sb.WriteString(fmt.Sprintf("**id:** %s\n", result.Task.ID))
	sb.WriteString(fmt.Sprintf("**mode:** %s\n", modeLabel))
	// ... rest of existing body ...
}

func modeToLabel(mode string) string {
	switch mode {
	case "direct":
		return "executing directly"
	case "plan":
		return "planned"
	case "spec_plan":
		return "spec-planned (multi-phase)"
	case "spec_pair":
		return "pair session"
	default:
		return "planned"
	}
}
```

- [ ] **Step 2: Propagate `SuggestedMode` through the ACK path**

The async-dispatch site at `handler.go:680` calls `FormatEnhancedAsyncTaskAck(result, steps, ...)`. Verify `result.SuggestedMode` is populated by Task 4 Step 4. No additional wiring needed if Task 4 is complete.

- [ ] **Step 3: TUI ACK displays mode**

The TUI already renders the ACK reply text from the handler. The `**mode:**` line added in Step 1 will appear automatically. No TUI change required for text mode; but if there's a structured ACK view, add `SuggestedMode` to whatever struct it consumes. Search:

```bash
grep -rn "FormatEnhancedAsyncTaskAck\|starting task" internal/tui/
```

If a structured field exists, populate it. If not (text-only rendering), skip — the `**mode:**` line suffices.

- [ ] **Step 4: Flutter ACK surfaces mode**

In `ui/flutter_ui/lib/features/chat/`, find the ACK bubble widget. The daemon sends `reply` text via the chat RPC; the `**mode:**` markdown line will render in the existing markdown renderer. If there's a structured field for task metadata, add `suggestedMode`:

```dart
// In the ACK bubble widget:
Text(
  _modeLabel(task.suggestedMode),
  style: theme.textTheme.bodySmall,
);
```

Add helper:
```dart
String _modeLabel(String? mode) {
  switch (mode) {
    case 'direct': return 'executing directly';
    case 'plan': return 'planned';
    case 'spec_plan': return 'spec-planned (multi-phase)';
    case 'spec_pair': return 'pair session';
    default: return 'planned';
  }
}
```

If the Flutter chat view does not currently parse structured ACK fields, leave a `// TODO: surface mode in ACK bubble` comment and skip — text markdown is sufficient for v1.

- [ ] **Step 5: Build and verify**

Run: `go build ./...`
Expected: clean.

Run: `(cd ui/flutter_ui && flutter analyze)` if Flutter SDK available; otherwise skip.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/handler.go ui/flutter_ui/lib/features/chat/
git commit -m "feat(ack): surface SuggestedMode in async task ACK"
```

---

## Task 8: HTTP API for thresholds

**Files:**
- Modify: `internal/comm/http/api_handlers.go`
- Modify: `internal/comm/http/server.go`

- [ ] **Step 1: Add `GET/PUT /api/v1/config/orchestrator`**

Follow the existing pattern for `/api/v1/config/client` (find it in `api_handlers.go`). Add handlers that read/write `cfg.Orchestrator`:

```go
func (s *Server) handleConfigOrchestratorGet(w http.ResponseWriter, r *http.Request) {
	cfg := s.configLoader()
	s.writeJSON(w, http.StatusOK, cfg.Orchestrator)
}

func (s *Server) handleConfigOrchestratorPut(w http.ResponseWriter, r *http.Request) {
	var oc config.OrchestratorConfig
	if err := json.NewDecoder(r.Body).Decode(&oc); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	cfg := s.configLoader()
	cfg.Orchestrator = oc
	if err := s.configSaver(cfg); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, cfg.Orchestrator)
}
```

Register routes in `server.go` near the existing config routes:

```go
mux.HandleFunc("GET /api/v1/config/orchestrator", s.handleConfigOrchestratorGet)
mux.HandleFunc("PUT /api/v1/config/orchestrator", s.handleConfigOrchestratorPut)
```

If `s.configLoader`/`s.configSaver` don't exist, find the existing pattern (`GET /api/v1/config/client`) and follow it.

- [ ] **Step 2: Test the endpoint**

Run the daemon and:
```bash
curl -s http://localhost:8081/api/v1/config/orchestrator | jq .
curl -sX PUT http://localhost:8081/api/v1/config/orchestrator -d '{"ambiguity_threshold":0.7,"interview_ambiguity_threshold":0.65}' | jq .
```

- [ ] **Step 3: Commit**

```bash
git add internal/comm/http/api_handlers.go internal/comm/http/server.go
git commit -m "feat(http): add GET/PUT /api/v1/config/orchestrator for threshold tuning"
```

---

## Self-Review

**Spec coverage (Thread D):**
- ✅ Mode taxonomy (`direct`/`plan`/`spec_plan`/`spec_pair`) — Task 2
- ✅ `IntentType.SuggestedMode()` — Task 2
- ✅ `TrueIntentAnalysis.SuggestedMode` field + LLM prompt update — Task 4
- ✅ `Intent.SuggestedMode` + `DispatchResult.SuggestedMode` — Task 4
- ✅ `PlanRequest.Mode` — Task 4
- ✅ `suggestMode` pure function called in `ClassifyAndRoute` — Task 4
- ✅ Planner mode switch + `inferLegacyMode` — Task 5
- ✅ `shouldInterview` replaces ambiguity gate — Task 5
- ✅ `decompose_spec.md` template — Task 1
- ✅ Config-file thresholds — Task 6
- ✅ ACK surfaces mode — Task 7
- ✅ HTTP API for thresholds — Task 8
- ✅ Flutter settings exposes thresholds — Task 7 Step 4 + Task 8 (settings UI is the same endpoint)

**Removals:** `shouldDecompose`, `shouldUsePairSession`, `ConductInterview`'s ambiguity+scope gate — Task 5.

**Type consistency:** `Mode` field name, `suggestMode`/`validateMode`/`inferLegacyMode`/`shouldInterview`/`modeToLabel` function names — used consistently.

**Red flags:**
- `planMultiPhase` is stubbed in this thread; Thread C+F's plan implements it. Confirmed stub degrades to `planSinglePhase`.
- `IntentArchitect` was added to `IntentType.SuggestedMode` mapping — verified against `intent.go` (line 60: `IntentArchitect` exists).
- The short-input downgrade uses `len(input) < 50`. Verified matches spec §Thread D `suggestMode` pseudocode.
- `IntentPair` is NOT in the mode taxonomy. Pair sessions are entered via `IntentCompound` (spec_pair) or explicit collaboration routing. This matches spec: "compound intents → spec_pair. Reuses existing pair-session code path."
