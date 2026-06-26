# Turbo Thread C+F — Orchestrator Chunking + Phases + Produces/Consumes

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Transform the orchestrator from passive bus dispatcher to active step-transformer. Replace flat `maxPlanSteps` with phase-based decomposition where each phase declares `produces`/`consumes` artifacts. The orchestrator chunks oversized steps to fit executor context budgets and manages phase transitions.

**Architecture:**
1. **Expose model metadata** — `AgentRegistry.GetModelConfig(agentID)` returns the model's `ContextLimit` so the orchestrator can size steps.
2. **Planner emits phases** — `planMultiPhase` renders `decompose_spec.md`, calls the planner LLM, parses phases with 4-layer malformed-output defense.
3. **Produces/Consumes invariants** — `ArtifactStore` collects phase outputs; `checkPhaseReady` gates phase N+1 on phase N's declared produces.
4. **Proactive chunking** — `chunkToExecutorCapacity` estimates per-step token cost and splits oversized steps via `splitStep` (LLM call rendering `config/prompts/orchestrator/split.md`).
5. **Phase-level context reset** — `startNextPhase` begins phase N+1 with a fresh conversationID and injects consumes as structured context (no raw history propagation).
6. **Reactive re-chunking** — subscribe to `llm.context_compressed` events; cap at 5 splits per task.
7. **`maxPlanSteps` removal** — replaced by `max_steps_per_phase` (8) × `max_phases` (12). Deprecation warning on legacy key.

**Tech Stack:** Go, existing orchestrator + registry + plan store + bus, JSON5 config.

**Depends on:** Thread D (`PlanRequest.Mode` field, `decompose_spec.md` template, `plannerTemplateLoader`).

**Spec source:** `docs/superpowers/specs/2026-06-24-turbo-innovations-adoption-design.md` — Thread C+F.

---

## File Structure

| File | Responsibility |
|------|----------------|
| `config/prompts/orchestrator/split.md` | NEW — template for LLM-driven step splitting |
| `internal/agent/artifacts.go` | NEW — `Artifact` type (shared with Thread B's `StepHandoff`), `ArtifactStore` |
| `internal/agent/registry.go` | MODIFY — add `GetModelConfig(agentID)` method; export `SelectAgentForHint` on TacticalScheduler via wrapper (or accept the existing unexported `selectAgent`) |
| `internal/agent/orchestrator.go` | MODIFY — add `registry`, `templateReg`, `artifacts` fields; `chunkToExecutorCapacity`, `splitStep`, `checkPhaseReady`, `startNextPhase`, `renderPhaseStartup`; subscribe to `llm.context_compressed` |
| `internal/agent/strategic.go` | MODIFY — `planMultiPhase` real implementation; `PlanPhaseSpec`, `plannerPhaseOutput` types; phase-aware `TaskStep.Phase` assignment; replace stub from Thread D |
| `internal/agent/tactical.go` | MODIFY — `selectAgent` exposed via `SelectAgentForHint` (or wrapper) for orchestrator use |
| `internal/plan/plan.go` | MODIFY — add `Produces []Artifact`, `Consumes []Artifact` to `PlanPhase` |
| `internal/plan/store_sqlite.go` | MODIFY — persist `produces`/`consumes` JSON columns in `plan_phases` |
| `internal/plan/writer.go` | MODIFY — render `**Produces:**` / `**Consumes:**` blocks in plan.md |
| `internal/plan/manager.go` | MODIFY — `Synthesize` handles multi-phase plans from both LLM and human-authored sources uniformly |
| `internal/agent/handler.go` | MODIFY — forward phase metadata via plan bus events |
| `internal/daemon/components.go` | MODIFY — wire `registry` + `templateReg` + `artifacts` into orchestrator |
| `internal/config/schema.go` | MODIFY — declare `MaxStepsPerPhase`, `MaxPhases` (already added in Thread D's Task 6); remove `MaxPlanSteps` from sample config (deprecation warning) |
| `config/meept.json5` | MODIFY — drop `max_plan_steps` from sample, document `max_steps_per_phase`/`max_phases` (already in Thread D) |
| `internal/agent/orchestrator_chunking_test.go` | NEW — tests for chunking + split + budget |
| `internal/agent/orchestrator_phases_test.go` | NEW — tests for phase transitions + artifact store + checkPhaseReady |
| `internal/agent/strategic_multiphase_test.go` | NEW — tests for `planMultiPhase` parsing + repair |
| `internal/tui/` | MODIFY — plan view shows phase panel (NEW widget) |
| `ui/flutter_ui/lib/features/plans/` | MODIFY — phase list with produces/consumes |
| `internal/comm/http/api_handlers.go` | MODIFY — `GET /api/v1/plans/{id}/phases` |
| `internal/services/plan_service.go` | MODIFY — `Phases(planID)` retrieval method |

**Note:** `Artifact` is defined here in `internal/agent/artifacts.go` and imported by Thread B's `StepHandoff` (different package boundary — both live in `internal/agent`). Single source of truth.

---

## Task 1: `Artifact` type and `ArtifactStore`

**Files:**
- Create: `internal/agent/artifacts.go`
- Create: `internal/agent/artifacts_test.go`

- [ ] **Step 1: Write the failing test**

`internal/agent/artifacts_test.go`:

```go
package agent

import (
	"testing"
)

func TestArtifactStore_AddAndHas(t *testing.T) {
	s := newArtifactStore()
	s.Add(Artifact{Name: "auth.go", Kind: "file"}, "step-1")
	if !s.Has("auth.go") {
		t.Error("Has(auth.go) = false; want true")
	}
	if !s.IsProducedBy("auth.go", "step-1") {
		t.Error("IsProducedBy wrong")
	}
	if s.IsProducedBy("auth.go", "step-2") {
		t.Error("IsProducedBy wrong for non-producer")
	}
}

func TestArtifactStore_Get(t *testing.T) {
	s := newArtifactStore()
	a := Artifact{Name: "design", Kind: "decision", Description: "use JWT"}
	s.Add(a, "step-1")
	got, ok := s.Get("design")
	if !ok {
		t.Fatal("not found")
	}
	if got.Description != "use JWT" {
		t.Errorf("desc = %q", got.Description)
	}
}

func TestArtifactStore_ConcurrentSafe(t *testing.T) {
	s := newArtifactStore()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			s.Add(Artifact{Name: "x", Kind: "file"}, "step-1")
		}
		close(done)
	}()
	for i := 0; i < 100; i++ {
		s.Has("x")
	}
	<-done
}

func TestCheckPhaseReady_MissingRequired(t *testing.T) {
	s := newArtifactStore()
	phase := &PlanPhaseSpec{
		Name: "Phase 2",
		Consumes: []Artifact{
			{Name: "missing.go", Kind: "file", Required: true},
		},
	}
	err := checkPhaseReady(phase, s)
	if err == nil {
		t.Fatal("want error for missing required consume")
	}
}

func TestCheckPhaseReady_OptionalMissingOK(t *testing.T) {
	s := newArtifactStore()
	phase := &PlanPhaseSpec{
		Name: "Phase 2",
		Consumes: []Artifact{
			{Name: "optional.go", Kind: "file", Required: false},
		},
	}
	if err := checkPhaseReady(phase, s); err != nil {
		t.Errorf("want nil; got %v", err)
	}
}

func TestCheckPhaseReady_RequiredPresent(t *testing.T) {
	s := newArtifactStore()
	s.Add(Artifact{Name: "auth.go", Kind: "file"}, "step-1")
	phase := &PlanPhaseSpec{
		Name: "Phase 2",
		Consumes: []Artifact{
			{Name: "auth.go", Kind: "file", Required: true},
		},
	}
	if err := checkPhaseReady(phase, s); err != nil {
		t.Errorf("want nil; got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestArtifactStore -v`
Expected: FAIL — `undefined: newArtifactStore`.

- [ ] **Step 3: Write minimal implementation**

`internal/agent/artifacts.go`:

```go
package agent

import (
	"fmt"
	"sync"
)

// Artifact represents a produced or consumed work-product declared by a
// phase. Shared between PlanPhaseSpec (planner output), PlanPhase (persisted
// record), and StepHandoff (Thread B).
type Artifact struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"` // file|interface|schema|decision|test_suite
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// IsValidKind returns true if the kind is one of the supported values.
func (a Artifact) IsValidKind() bool {
	switch a.Kind {
	case "file", "interface", "schema", "decision", "test_suite":
		return true
	}
	return false
}

// artifactStore tracks produced artifacts per task. The orchestrator owns
// one instance per active task; cleared on task completion. All methods
// are goroutine-safe.
type artifactStore struct {
	mu        sync.RWMutex
	artifacts map[string]Artifact    // by name
	producers map[string]map[string]struct{} // name → set of step IDs that produced it
}

func newArtifactStore() *artifactStore {
	return &artifactStore{
		artifacts: make(map[string]Artifact),
		producers: make(map[string]map[string]struct{}),
	}
}

func (s *artifactStore) Add(a Artifact, producerStepID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.artifacts[a.Name] = a
	if s.producers[a.Name] == nil {
		s.producers[a.Name] = make(map[string]struct{})
	}
	s.producers[a.Name][producerStepID] = struct{}{}
}

func (s *artifactStore) Has(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.artifacts[name]
	return ok
}

func (s *artifactStore) Get(name string) (Artifact, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.artifacts[name]
	return a, ok
}

func (s *artifactStore) IsProducedBy(name, stepID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set, ok := s.producers[name]
	if !ok {
		return false
	}
	_, ok = set[stepID]
	return ok
}

// checkPhaseReady returns an error if any required consume is missing
// from the store. Optional consumes are best-effort.
func checkPhaseReady(phase *PlanPhaseSpec, store *artifactStore) error {
	for _, c := range phase.Consumes {
		if c.Required && !store.Has(c.Name) {
			return fmt.Errorf("phase %q requires %q but it wasn't produced",
				phase.Name, c.Name)
		}
	}
	return nil
}
```

`PlanPhaseSpec` is defined in Task 3 below. Forward-declare a placeholder here to satisfy the test compile; the real definition lands in Task 3. Alternatively, move Task 1's `checkPhaseReady` test cases into Task 3's test file. For cleanliness, do the latter: keep `internal/agent/artifacts.go` focused on `Artifact` + `artifactStore`, and define `checkPhaseReady` + `PlanPhaseSpec` together in Task 3.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/agent/ -run TestArtifactStore -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/artifacts.go internal/agent/artifacts_test.go
git commit -m "feat(orchestrator): add Artifact type and thread-safe ArtifactStore"
```

---

## Task 2: `GetModelConfig` + `SelectAgentForHint`

**Files:**
- Modify: `internal/agent/registry.go`
- Modify: `internal/agent/tactical.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/agent/registry_test.go`:

```go
func TestAgentRegistry_GetModelConfig(t *testing.T) {
	// Construct registry with a test resolver + an agent spec
	// ... use existing test fixtures ...
	reg := newTestRegistry(t)
	cfg, err := reg.GetModelConfig("coder")
	if err != nil {
		t.Fatalf("GetModelConfig: %v", err)
	}
	if cfg.ContextLimit <= 0 {
		t.Errorf("ContextLimit = %d; want > 0", cfg.ContextLimit)
	}
}

func TestAgentRegistry_GetModelConfig_UnknownAgent(t *testing.T) {
	reg := newTestRegistry(t)
	_, err := reg.GetModelConfig("nonexistent")
	if err == nil {
		t.Fatal("want error for unknown agent")
	}
}
```

If `newTestRegistry` doesn't exist, use the existing registry test helper (search for `NewAgentRegistry` in `registry_test.go`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestAgentRegistry_GetModelConfig -v`
Expected: FAIL — `undefined: GetModelConfig`.

- [ ] **Step 3: Write minimal implementation**

In `internal/agent/registry.go`, the `AgentRegistry` already holds `resolver *llm.Resolver` (line 57) and `specs map[string]*AgentSpec` (line 36). Add:

```go
// GetModelConfig returns the model configuration for the given agent,
// or the resolver's default if the agent has no explicit model.
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
```

If `Resolver.Default()` doesn't exist, add it to `internal/llm/resolver.go`:

```go
// Default returns the default model config, or nil if none configured.
func (r *Resolver) Default() *ModelConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.defaultModel != nil {
		return r.defaultModel
	}
	if len(r.models) > 0 {
		for _, m := range r.models {
			return m
		}
	}
	return nil
}
```

Verify `Resolver` field/struct names (might be `r.models map[string]*ModelConfig` or similar — inspect actual file before writing).

In `internal/agent/tactical.go`, expose `selectAgent` as `SelectAgentForHint` (thin wrapper, preserves call sites):

```go
// SelectAgentForHint picks an executor agent ID for a given tool hint.
// Exported so the orchestrator can size chunks against the right executor.
func (ts *TacticalScheduler) SelectAgentForHint(toolHint string) string {
	return ts.selectAgent(&task.TaskStep{ToolHint: toolHint})
}
```

(Refactor `selectAgent` to take a `toolHint string` directly if simpler — both call sites are inside the package.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/agent/ -run TestAgentRegistry_GetModelConfig -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/registry.go internal/llm/resolver.go internal/agent/tactical.go internal/agent/registry_test.go
git commit -m "feat(registry): expose GetModelConfig + SelectAgentForHint for orchestrator sizing"
```

---

## Task 3: `PlanPhaseSpec`, `plannerPhaseOutput`, `planMultiPhase`

**Files:**
- Modify: `internal/agent/strategic.go`
- Modify: `internal/agent/strategic_multiphase_test.go` (new)

- [ ] **Step 1: Define types**

In `internal/agent/strategic.go` (after `plannerOutput` at line 58):

```go
// PlanPhaseSpec is a planner-declared phase. Distinct from plan.PlanPhase
// (the persisted record) — this is the LLM's output shape before
// validation/persistence.
type PlanPhaseSpec struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Steps       []plannerStep `json:"steps"`
	Produces    []Artifact   `json:"produces"`
	Consumes    []Artifact   `json:"consumes"`
	DependsOn   []int        `json:"depends_on,omitempty"`
}

type plannerPhaseOutput struct {
	Phases []PlanPhaseSpec `json:"phases"`
}
```

- [ ] **Step 2: Write the failing test**

`internal/agent/strategic_multiphase_test.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestParsePhaseOutput_Valid(t *testing.T) {
	raw := `{"phases":[{"name":"P1","description":"x","steps":[{"description":"a","tool_hint":"code"}],"produces":[{"name":"f","kind":"file","required":true}],"consumes":[],"depends_on":[]}]}`
	out, err := parsePhaseOutput(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(out.Phases) != 1 {
		t.Fatalf("got %d phases; want 1", len(out.Phases))
	}
	if out.Phases[0].Produces[0].Name != "f" {
		t.Errorf("produce name = %q", out.Phases[0].Produces[0].Name)
	}
}

func TestParsePhaseOutput_RepairDanglingConsumes(t *testing.T) {
	raw := `{"phases":[{"name":"P1","steps":[],"produces":[],"consumes":[{"name":"ghost","required":true}],"depends_on":[]}]}`
	out, err := parsePhaseOutput(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Dangling consume should be dropped or marked not-required
	if len(out.Phases[0].Consumes) > 0 && out.Phases[0].Consumes[0].Required {
		t.Errorf("dangling required consume should be repaired to optional or dropped")
	}
}

func TestParsePhaseOutput_RepairInvalidKind(t *testing.T) {
	raw := `{"phases":[{"name":"P1","steps":[],"produces":[{"name":"x","kind":"banana","required":true}],"consumes":[],"depends_on":[]}]}`
	out, err := parsePhaseOutput(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if out.Phases[0].Produces[0].Kind != "file" {
		t.Errorf("invalid kind should be repaired to 'file'; got %q", out.Phases[0].Produces[0].Kind)
	}
}

func TestParsePhaseOutput_EmptyPhasesDropped(t *testing.T) {
	raw := `{"phases":[{"name":"","steps":[],"produces":[],"consumes":[],"depends_on":[]},{"name":"P1","steps":[{"description":"x"}],"produces":[],"consumes":[],"depends_on":[]}]}`
	out, err := parsePhaseOutput(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(out.Phases) != 1 {
		t.Errorf("empty phases should be dropped; got %d", len(out.Phases))
	}
}

func TestParsePhaseOutput_CapsPhaseCount(t *testing.T) {
	var phases []map[string]any
	for i := 0; i < 50; i++ {
		phases = append(phases, map[string]any{
			"name":  "P",
			"steps": []map[string]any{{"description": "x"}},
		})
	}
	raw, _ := json.Marshal(map[string]any{"phases": phases})
	out, err := parsePhaseOutput(string(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(out.Phases) > 12 {
		t.Errorf("phase count = %d; want <= 12", len(out.Phases))
	}
}

// parsePhaseOutput is the lenient-parse + validate + auto-repair entry point.
// Lives in strategic.go; tested here.
var parsePhaseOutput = parsePhaseOutputImpl

// Add stub so tests compile; the real impl is in strategic.go.
var parsePhaseOutputImpl = func(raw string) (*plannerPhaseOutput, error) {
	return parsePhaseOutputFn(raw)
}

// parsePhaseOutputFn is the actual function. Defined in strategic.go.
var parsePhaseOutputFn func(string) (*plannerPhaseOutput, error)

func init() {
	// Placate compiler when strategic.go hasn't defined the fn yet.
	if parsePhaseOutputFn == nil {
		parsePhaseOutputFn = func(s string) (*plannerPhaseOutput, error) {
			return nil, nil
		}
	}
}
```

**Simpler approach:** skip the indirection. Write `parsePhaseOutput` directly in `strategic.go` in Step 3 and have the test call it. Drop the `var` stubs from the test.

- [ ] **Step 3: Write minimal implementation**

In `internal/agent/strategic.go`:

```go
// parsePhaseOutput extracts phases from planner LLM output and runs
// validate-and-repair: drop empty phases, cap count, repair invalid enum
// kinds to "file", drop dangling consumes references (or downgrade to
// optional), repair out-of-range depends_on indices.
func (sp *StrategicPlanner) parsePhaseOutput(raw string) (*plannerPhaseOutput, error) {
	jsonStr := ExtractJSON(raw)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in phase planner output")
	}
	var out plannerPhaseOutput
	if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
		return nil, fmt.Errorf("parse phase JSON: %w", err)
	}
	// Repair pass
	filtered := out.Phases[:0]
	for _, p := range out.Phases {
		if p.Name == "" && len(p.Steps) == 0 {
			continue
		}
		// Repair invalid kinds
		for i := range p.Produces {
			if !p.Produces[i].IsValidKind() {
				p.Produces[i].Kind = "file"
			}
		}
		for i := range p.Consumes {
			if !p.Consumes[i].IsValidKind() {
				p.Consumes[i].Kind = "file"
			}
		}
		// Drop dangling depends_on indices
		validDeps := p.DependsOn[:0]
		for _, idx := range p.DependsOn {
			if idx >= 0 && idx < len(out.Phases) {
				validDeps = append(validDeps, idx)
			}
		}
		p.DependsOn = validDeps
		filtered = append(filtered, p)
	}
	out.Phases = filtered
	// Cap phase count
	if sp.maxPhases > 0 && len(out.Phases) > sp.maxPhases {
		out.Phases = out.Phases[:sp.maxPhases]
	}
	// Repair dangling consumes: if a consume references a name no phase
	// produces, downgrade to optional.
	producedNames := make(map[string]struct{})
	for _, p := range out.Phases {
		for _, a := range p.Produces {
			producedNames[a.Name] = struct{}{}
		}
	}
	for i := range out.Phases {
		for j := range out.Phases[i].Consumes {
			if _, ok := producedNames[out.Phases[i].Consumes[j].Name]; !ok {
				out.Phases[i].Consumes[j].Required = false
			}
		}
	}
	if len(out.Phases) == 0 {
		return nil, fmt.Errorf("planner produced no valid phases")
	}
	return &out, nil
}
```

Add `maxPhases int` field to `StrategicPlanner` (default 12) + `MaxPhases int` to `StrategicPlannerConfig`. Set default in constructor.

- [ ] **Step 4: Implement `planMultiPhase`**

In `internal/agent/strategic.go`, replace the Thread D stub:

```go
func (sp *StrategicPlanner) planMultiPhase(ctx context.Context, req PlanRequest) ([]*task.TaskStep, error) {
	plannerLoop, err := sp.registry.Get(config.AgentIDPlanner)
	if err != nil {
		return nil, fmt.Errorf("planner agent not available: %w", err)
	}

	// Build context section (same logic as planSinglePhase — extract helper if DRY)
	contextSection := sp.buildContextSection(req)

	prompt, err := sp.templateLoader.render("planner/decompose_spec.md", map[string]any{
		"MaxStepsPerPhase": sp.maxStepsPerPhase,
		"MaxPhases":        sp.maxPhases,
		"ContextSection":   contextSection,
		"Input":            req.Input,
	})
	if err != nil {
		return nil, fmt.Errorf("render decompose_spec template: %w", err)
	}

	planCtx, cancel := context.WithTimeout(ctx, sp.plannerTimeout)
	defer cancel()

	conversationID := fmt.Sprintf("plan-%s-%s", req.TaskID, id.Generate(""))
	output, err := plannerLoop.RunOnce(planCtx, prompt, conversationID)
	if err != nil {
		return nil, fmt.Errorf("planner failed: %w", err)
	}

	parsed, err := sp.parsePhaseOutput(output)
	if err != nil {
		// Layer C: one retry with feedback
		retryPrompt := prompt + "\n\nPrevious attempt failed:\n" + err.Error()
		output2, retryErr := plannerLoop.RunOnce(planCtx, retryPrompt, conversationID+"-retry")
		if retryErr != nil {
			return nil, fmt.Errorf("planner retry failed: %w (original: %v)", retryErr, err)
		}
		parsed, err = sp.parsePhaseOutput(output2)
		if err != nil {
			return nil, fmt.Errorf("planner produced malformed phases after retry: %w", err)
		}
	}

	// Flatten phases into TaskSteps. Each step gets PhaseID = phase.Name.
	// Inter-phase dependencies: first step of phase N+1 depends on last
	// step of phase N.
	var steps []*task.TaskStep
	var prevPhaseLastStepID string
	for phaseIdx, phase := range parsed.Phases {
		var stepIDsInPhase []string
		for stepIdx, ps := range phase.Steps {
			seq := phaseIdx*1000 + stepIdx // stable sequence across phases
			step := task.NewTaskStep(req.TaskID, ps.Description, seq)
			step.ToolHint = ps.ToolHint
			step.Phase = phase.Name
			// Cap per-phase steps
			if sp.maxStepsPerPhase > 0 && len(stepIDsInPhase) >= sp.maxStepsPerPhase {
				break
			}
			// Within-phase dependencies (0-indexed → step IDs)
			for _, depIdx := range ps.DependsOn {
				if depIdx >= 0 && depIdx < len(stepIDsInPhase) {
					step.DependsOn = append(step.DependsOn, stepIDsInPhase[depIdx])
				}
			}
			// Inter-phase dependency: first step of phase N+1 depends on
			// last step of phase N (unless this is phase 0).
			if stepIdx == 0 && prevPhaseLastStepID != "" && len(step.DependsOn) == 0 {
				step.DependsOn = append(step.DependsOn, prevPhaseLastStepID)
			}
			steps = append(steps, step)
			stepIDsInPhase = append(stepIDsInPhase, step.ID)
		}
		if len(stepIDsInPhase) > 0 {
			prevPhaseLastStepID = stepIDsInPhase[len(stepIDsInPhase)-1]
		}
	}

	if len(steps) == 0 {
		return nil, fmt.Errorf("planner produced no executable steps")
	}

	// Persist phase metadata to plan store (if available) for visibility.
	if sp.planPhaseSink != nil {
		sp.planPhaseSink(req.TaskID, parsed.Phases)
	}

	return steps, nil
}
```

Add `maxStepsPerPhase int` + `MaxStepsPerPhase int` fields. Add `planPhaseSink func(taskID string, phases []PlanPhaseSpec)` field + `WithPlanPhaseSink` option (set by `components.go` to write to `plan.Store`).

- [ ] **Step 5: Run tests**

Run: `go test ./internal/agent/ -run TestParsePhaseOutput -v`
Expected: PASS.

Run: `go build ./...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/strategic.go internal/agent/strategic_multiphase_test.go
git commit -m "feat(planner): implement planMultiPhase with 4-layer malformed-output defense"
```

---

## Task 4: `split.md` template + `chunkToExecutorCapacity`

**Files:**
- Create: `config/prompts/orchestrator/split.md`
- Modify: `internal/agent/planner_template.go` (register fallback)
- Modify: `internal/agent/orchestrator.go`
- Create: `internal/agent/orchestrator_chunking_test.go`

- [ ] **Step 1: Create `config/prompts/orchestrator/split.md`**

Exact content (verbatim from spec §Thread A → "split.md"):

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

- [ ] **Step 2: Register fallback in `plannerTemplateLoader`**

In `internal/agent/planner_template.go` `NewDaemonPlannerTemplateLoader`, add:

```go
	l.fallbacks["orchestrator/split.md"] = defaultSplitFallback()
```

Add fallback const mirroring the markdown body (omit the exemplar for brevity; see Task 1 of Thread A's plan for the pattern).

- [ ] **Step 3: Write the failing test**

`internal/agent/orchestrator_chunking_test.go`:

```go
package agent

import (
	"context"
	"testing"
)

func TestEstimateStepTokens_Heuristic(t *testing.T) {
	step := &task.TaskStep{Description: "refactor the auth middleware", ToolHint: "code"}
	cfg := &llm.ModelConfig{ContextLimit: 32000}
	cost := estimateStepTokens(step, cfg)
	if cost <= 0 {
		t.Errorf("cost = %d; want > 0", cost)
	}
	// tool output budget for "code" is 8K; description ~10 tokens; cost should be ~8K
	if cost < 7000 || cost > 9000 {
		t.Errorf("cost = %d; want ~8000", cost)
	}
}

func TestExecutorBudget_PercentOfContext(t *testing.T) {
	cfg := &llm.ModelConfig{ContextLimit: 32000}
	b := executorBudget(cfg)
	if b != 12800 { // 40%
		t.Errorf("budget = %d; want 12800", b)
	}
}

func TestToolOutputBudget(t *testing.T) {
	cases := map[string]int{
		"code": 8000, "debug": 4000, "git": 1000,
		"chat": 1000, "unknown": 2000,
	}
	for hint, want := range cases {
		got := toolOutputBudget(hint)
		if got != want {
			t.Errorf("toolOutputBudget(%q) = %d; want %d", hint, got, want)
		}
	}
}
```

- [ ] **Step 4: Write minimal implementation**

In `internal/agent/orchestrator.go`:

```go
// executorBudget returns 40% of the model's context limit — the
// per-step token budget for chunking decisions.
func executorBudget(modelCfg *llm.ModelConfig) int {
	if modelCfg == nil || modelCfg.ContextLimit <= 0 {
		return 12000 // safe default
	}
	return int(float64(modelCfg.ContextLimit) * 0.40)
}

// toolOutputBudget returns an estimated upper bound on tool output size
// per tool-hint class. Used by estimateStepTokens.
func toolOutputBudget(toolHint string) int {
	switch toolHint {
	case "code", "refactor":
		return 8000
	case "debug", "fix":
		return 4000
	case "git", "commit":
		return 1000
	case "chat":
		return 1000
	default:
		return 2000
	}
}

// estimateStepTokens returns a rough estimate of the tokens consumed by a
// single step: description + accumulated context + tool output budget.
func estimateStepTokens(step *task.TaskStep, modelCfg *llm.ModelConfig) int {
	desc := EstimateTokenCountHeuristic(step.Description)
	acc := EstimateTokenCountHeuristic(step.AccumulatedContext)
	toolBudget := toolOutputBudget(step.ToolHint)
	return desc + acc + toolBudget
}
```

If `EstimateTokenCountHeuristic` doesn't exist, add it (rough: `len(s)/4`):

```go
// EstimateTokenCountHeuristic returns a rough token count for a string.
// Uses the standard "~4 chars per token" approximation.
func EstimateTokenCountHeuristic(s string) int {
	return len(s) / 4
}
```

- [ ] **Step 5: Add `chunkToExecutorCapacity` and `splitStep`**

```go
// chunkToExecutorCapacity walks a task's steps and splits any that exceed
// the executor's budget into sub-steps. Per-task split counter caps at 5
// to prevent cascading splits.
func (o *Orchestrator) chunkToExecutorCapacity(ctx context.Context, taskID string) error {
	steps, err := o.stepStore.GetByTask(taskID)
	if err != nil {
		return err
	}
	splitsThisTask := 0
	const maxSplitsPerTask = 5
	for _, step := range steps {
		if splitsThisTask >= maxSplitsPerTask {
			o.logger.Warn("Per-task split cap reached", "task_id", taskID, "cap", maxSplitsPerTask)
			return nil
		}
		executorID := o.tactical.SelectAgentForHint(step.ToolHint)
		modelCfg, err := o.registry.GetModelConfig(executorID)
		if err != nil {
			continue // fall back to ContextFirewall at runtime
		}
		budget := executorBudget(modelCfg)
		cost := estimateStepTokens(step, modelCfg)
		if cost > budget {
			subSteps, splitErr := o.splitStep(ctx, step, budget, modelCfg)
			if splitErr != nil {
				o.logger.Warn("splitStep failed; leaving step oversized",
					"step_id", step.ID, "error", splitErr)
				continue
			}
			if err := o.stepStore.ReplaceWithSubSteps(step.ID, subSteps); err != nil {
				o.logger.Warn("ReplaceWithSubSteps failed", "step_id", step.ID, "error", err)
				continue
			}
			splitsThisTask++
		}
	}
	return nil
}

func (o *Orchestrator) splitStep(ctx context.Context, step *task.TaskStep, budget int, modelCfg *llm.ModelConfig) ([]*task.TaskStep, error) {
	if o.templateReg == nil {
		return nil, fmt.Errorf("template registry not wired")
	}
	executorID := o.tactical.SelectAgentForHint(step.ToolHint)
	prompt, err := o.templateReg.render("orchestrator/split.md", map[string]any{
		"BudgetTokens":    budget,
		"StepDescription": step.Description,
		"ToolHint":        step.ToolHint,
		"ExecutorID":      executorID,
		"ContextLimit":    modelCfg.ContextLimit,
	})
	if err != nil {
		return nil, err
	}
	// Use classifier/cheap model for splitting
	splitLLM, err := o.registry.Get(config.AgentIDPlanner) // or a dedicated chunker agent
	if err != nil {
		return nil, err
	}
	conversationID := fmt.Sprintf("split-%s-%s", step.TaskID, step.ID)
	splitCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	output, err := splitLLM.RunOnce(splitCtx, prompt, conversationID)
	if err != nil {
		return nil, fmt.Errorf("split LLM call failed: %w", err)
	}
	jsonStr := ExtractJSON(output)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON in split output")
	}
	var parsed struct {
		SubSteps []plannerStep `json:"sub_steps"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil, fmt.Errorf("parse split JSON: %w", err)
	}
	if len(parsed.SubSteps) == 0 {
		return nil, fmt.Errorf("split produced no sub-steps")
	}
	if len(parsed.SubSteps) > 5 {
		parsed.SubSteps = parsed.SubSteps[:5]
	}
	// Build sub-steps preserving dependencies
	var subSteps []*task.TaskStep
	var prevID string
	for i, ss := range parsed.SubSteps {
		sub := task.NewTaskStep(step.TaskID, ss.Description, step.Sequence+i+1)
		sub.ToolHint = ss.ToolHint
		sub.Phase = step.Phase
		sub.TaskID = step.TaskID
		// Preserve original dependencies on the first sub-step
		if i == 0 {
			sub.DependsOn = step.DependsOn
		} else if prevID != "" {
			// Chain sub-steps: each depends on prior
			sub.DependsOn = []string{prevID}
		}
		subSteps = append(subSteps, sub)
		prevID = sub.ID
	}
	return subSteps, nil
}
```

The orchestrator needs `registry`, `templateReg`, and `stepStore` fields. Add them (see Task 6 wiring).

- [ ] **Step 6: Run tests**

Run: `go test ./internal/agent/ -run "TestEstimateStepTokens|TestExecutorBudget|TestToolOutputBudget" -v`
Expected: PASS.

Run: `go build ./...`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add config/prompts/orchestrator/split.md internal/agent/planner_template.go internal/agent/orchestrator.go internal/agent/orchestrator_chunking_test.go
git commit -m "feat(orchestrator): add proactive chunkToExecutorCapacity + splitStep"
```

---

## Task 5: Phase transition — `startNextPhase` + context reset

**Files:**
- Modify: `internal/agent/orchestrator.go`
- Create: `internal/agent/orchestrator_phases_test.go`

- [ ] **Step 1: Write the failing test**

```go
package agent

import (
	"context"
	"testing"
)

func TestStartNextPhase_AssignsFreshConversationIDs(t *testing.T) {
	o, store := newTestOrchestrator(t)
	// Seed two phases in the store; phase 1 done, phase 2 pending.
	phase1 := &plan.PlanPhase{ID: "p1", PlanID: "task-x", Name: "Phase 1", Sequence: 0, State: plan.PhaseStateDone}
	phase2 := &plan.PlanPhase{ID: "p2", PlanID: "task-x", Name: "Phase 2", Sequence: 1, State: plan.PhaseStatePending}
	step := &task.TaskStep{ID: "s1", TaskID: "task-x", Phase: "Phase 2", Description: "x"}
	store.CreateStep(step)

	err := o.startNextPhase(context.Background(), "task-x", phase1.Name)
	if err != nil {
		t.Fatalf("startNextPhase: %v", err)
	}
	got, _ := store.GetStep("s1")
	if !strings.HasPrefix(got.ConversationID, "phase-") {
		t.Errorf("ConversationID = %q; want phase-* prefix", got.ConversationID)
	}
	if got.AccumulatedContext == "" {
		t.Errorf("AccumulatedContext should include phase startup context")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestStartNextPhase -v`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

In `internal/agent/orchestrator.go`:

```go
// startNextPhase transitions a task from a completed phase to the next.
// It assigns fresh conversationIDs (no raw history propagation), injects
// consumes artifacts + original user request + plan summary as structured
// context, and gates on checkPhaseReady.
func (o *Orchestrator) startNextPhase(ctx context.Context, taskID, completedPhaseName string) error {
	if o.planManager == nil {
		return fmt.Errorf("plan manager not wired")
	}
	// 1. Find next phase
	phases, err := o.planManager.GetPhasesByTask(taskID)
	if err != nil {
		return fmt.Errorf("get phases: %w", err)
	}
	var nextPhase *plan.PlanPhase
	foundCompleted := false
	for i := range phases {
		if foundCompleted {
			nextPhase = phases[i]
			break
		}
		if phases[i].Name == completedPhaseName {
			foundCompleted = true
		}
	}
	if nextPhase == nil {
		return nil // no next phase; task may be complete
	}
	// 2. Find the phase spec (from plan store or task metadata) to get consumes
	phaseSpec, err := o.getPlanPhaseSpec(taskID, nextPhase.Name)
	if err != nil {
		o.logger.Warn("Could not load phase spec for context injection",
			"phase", nextPhase.Name, "error", err)
		phaseSpec = &PlanPhaseSpec{Name: nextPhase.Name}
	}
	// 3. Gate on consumes readiness
	if err := checkPhaseReady(phaseSpec, o.artifacts); err != nil {
		return fmt.Errorf("phase not ready: %w", err)
	}
	// 4. Build startup context
	startupCtx := o.renderPhaseStartup(phaseSpec, o.artifacts)
	// 5. Update steps: fresh conversationID + startup context
	steps, err := o.stepStore.GetByPhase(nextPhase.Name)
	if err != nil {
		return fmt.Errorf("get steps by phase: %w", err)
	}
	for _, step := range steps {
		step.ConversationID = fmt.Sprintf("phase-%s-%s", nextPhase.ID, step.ID)
		step.AccumulatedContext = startupCtx
		if err := o.stepStore.Update(step); err != nil {
			return fmt.Errorf("update step %s: %w", step.ID, err)
		}
	}
	o.logger.Info("Phase transition",
		"task_id", taskID,
		"from", completedPhaseName,
		"to", nextPhase.Name,
		"steps", len(steps),
	)
	return nil
}

// renderPhaseStartup builds the structured context injected into the first
// step of a new phase. Contains: original user request summary, consumed
// artifacts, follow-up hints. NO raw history from prior phases.
func (o *Orchestrator) renderPhaseStartup(phase *PlanPhaseSpec, store *artifactStore) string {
	var sb strings.Builder
	sb.WriteString("## Phase: " + phase.Name + "\n\n")
	if phase.Description != "" {
		sb.WriteString(phase.Description + "\n\n")
	}
	if len(phase.Consumes) > 0 {
		sb.WriteString("## Inputs from prior phases\n\n")
		for _, c := range phase.Consumes {
			art, ok := store.Get(c.Name)
			if !ok {
				if c.Required {
					sb.WriteString(fmt.Sprintf("- MISSING: %s (required)\n", c.Name))
				}
				continue
			}
			sb.WriteString(fmt.Sprintf("- %s (%s): %s\n", art.Name, art.Kind, art.Description))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
```

`o.artifacts` is a per-task `*artifactStore`. Add it to `Orchestrator`. `getPlanPhaseSpec` reads from `plan.Manager` (or returns a synthetic spec from `plan.PlanPhase` if produces/consumes aren't separately stored).

`GetPhasesByTask` may need to be added to `plan.Manager` (it currently has `GetPhases(planID)`).

- [ ] **Step 4: Run tests**

Run: `go test ./internal/agent/ -run TestStartNextPhase -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/orchestrator.go internal/agent/orchestrator_phases_test.go
git commit -m "feat(orchestrator): implement phase transition with context reset"
```

---

## Task 6: Wire orchestrator fields in `components.go`

**Files:**
- Modify: `internal/agent/orchestrator.go` (struct + OrchestratorDeps)
- Modify: `internal/daemon/components.go`
- Modify: `internal/agent/strategic.go` (add `planPhaseSink` wiring)

- [ ] **Step 1: Extend `Orchestrator` struct and `OrchestratorDeps`**

In `internal/agent/orchestrator.go`:

```go
type Orchestrator struct {
	// ... existing ...
	registry    *AgentRegistry
	templateReg *plannerTemplateLoader
	artifacts   *artifactStore // per-task; reset on task completion
}

type OrchestratorDeps struct {
	// ... existing ...
	Registry    *AgentRegistry
	TemplateReg *plannerTemplateLoader
}
```

Update `NewOrchestrator` to assign these fields. Initialize `artifacts: newArtifactStore()`.

- [ ] **Step 2: Wire in `components.go`**

In `internal/daemon/components.go` at the `NewOrchestrator` call (line 1706):

```go
	c.Orchestrator = agent.NewOrchestrator(agent.OrchestratorDeps{
		Strategic:           strategicPlanner,
		Tactical:            tacticalScheduler,
		// ... existing ...
		Registry:    c.AgentRegistry,
		TemplateReg: agent.NewDaemonPlannerTemplateLoader("config/prompts"),
		Bus:         msgBus,
		Logger:      logger.With("component", "orchestrator"),
		FenceChecker: c.FenceChecker,
	})
```

Wire `planPhaseSink` on the planner:

```go
	strategicPlanner.SetPlanPhaseSink(func(taskID string, phases []agent.PlanPhaseSpec) {
		if c.PlanManager == nil {
			return
		}
		// Persist phases to plan store. Convert PlanPhaseSpec → plan.PlanPhase
		// and call CreatePhase for each.
		for i, p := range phases {
			phaseRecord := &plan.PlanPhase{
				ID:         fmt.Sprintf("phase-%s-%d", taskID, i),
				PlanID:     taskID,
				Name:       p.Name,
				Sequence:   i,
				TotalSteps: len(p.Steps),
				State:      plan.PhaseStatePending,
				Produces:   p.Produces,
				Consumes:   p.Consumes,
			}
			_ = c.PlanManager.CreatePhase(context.Background(), phaseRecord)
		}
	})
```

`SetPlanPhaseSink` is a setter on `StrategicPlanner`. Add it. Must be nil-guarded (per CLAUDE.md).

- [ ] **Step 3: Reset `artifacts` on task completion**

In the orchestrator's `handleJobCompleted` (or wherever task completion is detected), add:

```go
	o.artifacts = newArtifactStore() // reset for next task
```

If multiple tasks run concurrently, `artifacts` should be `map[string]*artifactStore` (taskID → store). For MVP with sequential task completion, the single-instance reset is acceptable; the spec says "per-task (cleared on task completion)."

- [ ] **Step 4: Build and test**

Run: `go build ./...`
Expected: clean.

Run: `go test ./internal/agent/ -v -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/orchestrator.go internal/daemon/components.go internal/agent/strategic.go
git commit -m "feat(orchestrator): wire registry + templateReg + planPhaseSink"
```

---

## Task 7: Plan store — persist produces/consumes

**Files:**
- Modify: `internal/plan/plan.go`
- Modify: `internal/plan/store_sqlite.go`
- Modify: `internal/plan/writer.go`

- [ ] **Step 1: Extend `PlanPhase`**

In `internal/plan/plan.go`:

```go
type PlanPhase struct {
	// ... existing fields ...
	Produces []agent.Artifact `json:"produces,omitempty" db:"-"`
	Consumes []agent.Artifact `json:"consumes,omitempty" db:"-"`
}
```

**Problem:** `plan` is a lower-level package than `agent`. Importing `agent.Artifact` creates an import cycle. Two options:
1. Move `Artifact` to `internal/plan` (cleaner dependency direction).
2. Define a local `plan.Artifact` type and convert at the boundary.

Choose option 1: **move `Artifact` from `internal/agent/artifacts.go` to `internal/plan/artifacts.go`**. Update `agent.artifactStore` to use `plan.Artifact`. Update all references.

- [ ] **Step 2: SQLite persistence**

In `internal/plan/store_sqlite.go`, the `plan_phases` table (line 80) gains two columns:

```sql
ALTER TABLE plan_phases ADD COLUMN produces TEXT;
ALTER TABLE plan_phases ADD COLUMN consumes TEXT;
```

Use the migration pattern from existing schema (search for `CREATE TABLE` and migrations). `CreatePhase` serializes `Produces`/`Consumes` to JSON. `GetPhases` deserializes.

- [ ] **Step 3: Writer renders produces/consumes**

In `internal/plan/writer.go` `WritePlanMarkdown` (line 12), after each phase header:

```markdown
## Phase N: <name> [state]

**Produces:**
- `<name>` (<kind>) — <description>

**Consumes:**
- `<name>` (<kind>, required) — <description>

### Steps
1. step description
```

- [ ] **Step 4: Test**

Run: `go test ./internal/plan/ -v -count=1`
Expected: PASS (add tests for `GetPhases` round-trips produces/consumes if not present).

- [ ] **Step 5: Commit**

```bash
git add internal/plan/plan.go internal/plan/store_sqlite.go internal/plan/writer.go internal/plan/artifacts.go internal/agent/artifacts.go internal/agent/orchestrator.go
git commit -m "feat(plan): persist produces/consumes artifacts in plan_phases; render in plan.md"
```

---

## Task 8: `max_plan_steps` deprecation + reactive re-chunking subscription

**Files:**
- Modify: `internal/config/schema.go`
- Modify: `config/meept.json5`
- Modify: `internal/agent/orchestrator.go`

- [ ] **Step 1: Deprecation warning**

In `internal/config/schema.go` `DefaultConfig`, keep `MaxPlanSteps` field for backward-compat but add a loader that warns:

```go
// In whatever function loads + validates config:
if cfg.Orchestrator.MaxPlanSteps != 0 && cfg.Orchestrator.MaxPlanSteps != 10 {
	slog.Warn("orchestrator.max_plan_steps is deprecated; use max_steps_per_phase + max_phases. Ignoring value.",
		"value", cfg.Orchestrator.MaxPlanSteps)
}
```

Remove `max_plan_steps` from `config/meept.json5` sample.

- [ ] **Step 2: Subscribe to `llm.context_compressed`**

In `internal/agent/orchestrator.go` `Start`, add a bus subscription:

```go
topics["llm.context_compressed"] = o.handleContextCompressed
```

Handler:

```go
func (o *Orchestrator) handleContextCompressed(ctx context.Context, msg *models.BusMessage) {
	var data struct{ TaskID, StepID string }
	if json.Unmarshal(msg.Payload, &data) == nil && data.TaskID != "" {
		o.logger.Info("Context compressed for step; flagging for potential re-chunking",
			"task_id", data.TaskID, "step_id", data.StepID)
		// Future: re-run chunkToExecutorCapacity for subsequent steps.
		// For now: log only — chunking is proactive in Task 4.
	}
}
```

- [ ] **Step 3: Build + test**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add internal/config/schema.go config/meept.json5 internal/agent/orchestrator.go
git commit -m "feat(config): deprecate max_plan_steps; subscribe orchestrator to context_compressed"
```

---

## Task 9: HTTP API + Flutter plan view

**Files:**
- Modify: `internal/services/plan_service.go`
- Modify: `internal/comm/http/api_handlers.go`
- Modify: `internal/comm/http/server.go`
- Modify: `ui/flutter_ui/lib/features/plans/`
- Modify: `internal/tui/` (plan view)

- [ ] **Step 1: Add `Phases(planID)` to `PlanService`**

In `internal/services/plan_service.go`:

```go
func (s *PlanService) Phases(ctx context.Context, planID string) ([]*plan.PlanPhase, error) {
	if s.store == nil {
		return nil, fmt.Errorf("plan store not available")
	}
	return s.store.GetPhases(ctx, planID)
}
```

- [ ] **Step 2: HTTP endpoint**

In `internal/comm/http/server.go`:

```go
mux.HandleFunc("GET /api/v1/plans/{id}/phases", s.handlePlanPhases)
```

In `api_handlers.go`:

```go
func (s *Server) handlePlanPhases(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Plan == nil {
		s.writeError(w, http.StatusServiceUnavailable, "plan service not available")
		return
	}
	id := r.PathValue("id")
	phases, err := s.services.Plan.Phases(r.Context(), id)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"phases": phases})
}
```

- [ ] **Step 3: CLI/TUI renders phases**

In `cmd/meept/plans.go` `meept plans show <id>`, fetch phases via RPC and print with produces/consumes blocks. Pattern follows existing `plans show`.

TUI: add a phase panel widget to the plan view. If no plan view widget exists (the Explore agent confirmed none exists), add a minimal one to `internal/tui/components/plan_view.go` showing phase list.

- [ ] **Step 4: Flutter plan view**

In `ui/flutter_ui/lib/features/plans/`, render phases with produces/consumes:

```dart
// In plan detail view:
Column(
  children: phases.map((phase) => PhaseCard(
    name: phase.name,
    produces: phase.produces,
    consumes: phase.consumes,
    state: phase.state,
  )).toList(),
)
```

- [ ] **Step 5: Commit**

```bash
git add internal/services/plan_service.go internal/comm/http/ cmd/meept/plans.go ui/flutter_ui/lib/features/plans/ internal/tui/components/plan_view.go
git commit -m "feat(plans): expose phases via HTTP/CLI/TUI/Flutter with produces/consumes"
```

---

## Self-Review

**Spec coverage (Thread C+F):**
- ✅ Piece 1: Expose model metadata (`GetModelConfig`) — Task 2
- ✅ Piece 2: Planner emits phases (`PlanPhaseSpec`, `plannerPhaseOutput`, `planMultiPhase`) — Task 3
- ✅ Piece 3: Produces/Consumes invariants (`ArtifactStore`, `checkPhaseReady`) — Tasks 1, 5
- ✅ Piece 4: Proactive chunking (`chunkToExecutorCapacity`, `splitStep`, `estimateStepTokens`) — Task 4
- ✅ Piece 5: Reactive re-chunking (subscribe to `llm.context_compressed`) — Task 8 Step 2 (stub for future)
- ✅ `maxPlanSteps` replacement (`max_steps_per_phase`, `max_phases`) — Task 8
- ✅ Orchestrator struct changes (`registry`, `templateReg`, `artifacts`) — Task 6
- ✅ Phase-level context reset (`startNextPhase`, `renderPhaseStartup`) — Task 5
- ✅ Plan store produces/consumes — Task 7
- ✅ 4-layer malformed-output defense — Task 3 (parse+repair) + Layer C retry + Layer D fallback in `Plan()` switch (Thread D's plan)
- ✅ Wiring (CLI/TUI/Flutter/HTTP) — Task 9

**Type consistency:**
- `Artifact` moved to `internal/plan/artifacts.go` mid-plan to avoid import cycle. Verified `agent.artifactStore` + `agent.PlanPhaseSpec` both reference `plan.Artifact`. `StepHandoff.Artifacts` (Thread B) will also use `plan.Artifact`.
- `maxPhases`, `maxStepsPerPhase` field names used consistently in planner, config, and test.

**Red flags:**
- `plan` package import cycle: handled by moving `Artifact` to `plan` package in Task 7.
- `Orchestrator.artifacts` as single instance breaks under concurrent tasks. Documented as MVP; full per-task map is a follow-up.
- Reactive re-chunking is logged-only in Task 8 Step 2; full implementation is a follow-up. Spec acknowledges this as a "three triggers" feature; proactive chunking (Task 4) covers the main case.
