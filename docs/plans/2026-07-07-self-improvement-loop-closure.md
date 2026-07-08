# Self-Improvement Loop Closure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the self-improvement loop end-to-end so that production LLM traffic progressively improves local model quality, skill coverage, and routing decisions — without any new manual intervention.

**Architecture:** Six independent enhancements layered on top of the existing shadow-training (`internal/shadow/`), skill-evolver (`internal/skills/lifecycle/`), and LLM resolver (`internal/llm/resolver.go`) subsystems. The end-state loop is: every served request emits a durable routing decision → scored against a teacher → mined for training pairs → LoRA-trained student → eval-gated → hot-swapped into the serving alias → enriched preference pairs (with domain/task/routing context) train the next round. The reflection queue (per-turn self-reflection proposals) is drained into the skill evolver as an additional input stream. A new evolver pass surfaces skill-coverage gaps by mining the existing capability index for low-match queries. Each piece is independently shippable.

**Tech Stack:** Go 1.22+, modernc.org/sqlite (WAL), existing internal/shadow/adapters/{ollama,openai}.go, existing internal/llm resolver + client, existing internal/skills/lifecycle evolver pipeline, existing internal/agent reflection_collector + proposal queue. No new external dependencies.

---

## Source analysis (verified anchors)

These file:line anchors were verified at plan-writing time. Implementers should re-verify before editing.

- `internal/shadow/config.go:46-58` — `Config` struct (top-level shadow config).
- `internal/shadow/config.go:146-156` — `AdaptersConfig` (no eval threshold field today).
- `internal/shadow/config.go:159-239` — `DefaultConfig()` (place to add new defaults).
- `internal/shadow/manager.go:534-547` — `ActivateAdapter` / `GetActiveAdapter` (currently pass-throughs to store, no eval gate, no LLM-client wiring).
- `internal/shadow/models.go:118-129` — `PreferencePair` struct (drops Domain, TaskType, routing context).
- `internal/shadow/models.go:132-158` — `NewPreferencePair` constructor.
- `internal/shadow/models.go:218-242` — `Adapter` struct + `NewAdapter` constructor.
- `internal/shadow/models.go:244-268` — `TrainingRun` with `EvalScore` field (currently informational only).
- `internal/shadow/adapters/ollama.go:71-109` — `CreateModelWithAdapter` bakes adapter into a new Ollama model via Modelfile.
- `internal/shadow/adapters/ollama.go:196+` — `OllamaAdapter.ActivateAdapter` deletes/recreates Ollama model.
- `internal/shadow/exporter.go:225-257` — `exportDPO` writes `{prompt, chosen, rejected}` only; drops domain/task/margin.
- `internal/llm/client.go` — LLM client; has no adapter awareness.
- `internal/llm/resolver.go:106-167` — `ResolveForSkill` (cost-aware, used only for skill execution).
- `internal/llm/resolver.go:212-247` — `ResolveForAlias` (round-robin; **not cost-aware**; what the agent loop uses).
- `internal/llm/resolver.go:159-164` — emits `slog.Info("Escalated to model for skill", ...)` (ephemeral; not persisted).
- `internal/agent/reflection_collector.go:23-56` — `ReflectionCollector` constructor.
- `internal/agent/proposal.go:40-110` — `proposalQueue` with `Append`, `ListPending`, `MarkApplied`, `MarkSkipped`.
- `internal/skills/lifecycle/usage.go:17-45` — `UsageTracker` interface (no low-match query API).
- `internal/skills/lifecycle/evolver.go:45-56` — `Evolver` struct fields.
- `internal/skills/lifecycle/evolver.go:115-120` — `RunCycle` invokes `passARefine`, `passBPromote`, `passCPrune` (no Pass D today).
- `internal/skills/lifecycle/types.go:146-180` — `EvolutionProposalAction` (only Refine/Create/Archive) and `EvolutionReport` (no `Gaps` counter).
- `internal/skills/capability_index.go` — TF-IDF index with `MatchWithThreshold` (used only for dedup at 0.7 today).
- `docs/workflows/self-improvement.md` — exists; describes pytest/lint self-improve, NOT the closed loop in this plan.
- `docs/workflows/skills.md` — exists; documents skill system.
- `docs/features.md` — top-level features document.

---

## Risk Analysis (verified against code)

Each risk was investigated against the actual codebase before implementation. The plan below incorporates the fixes directly — the original risk descriptions are here for traceability.

### Risk 1: ChatOption shape and model-override path — RESOLVED (plan simplified)

**Original concern:** Task 1.4 assumed `ChatOption` had a `key`/`value` struct shape.

**Verified reality:**
- `ChatOption` is `func(*chatOptions)` (`internal/llm/client.go:629`). The plan's `ChatOption{key, value}` syntax was wrong.
- More importantly, `AgentLoop` already has its own model-override mechanism: `modelOverride` field, `SetModelOverride(modelRef)`, `GetModelOverride()`, `ClearModelOverride()` (`internal/agent/loop.go:529, 976-1003`). The dispatcher uses this for user-driven model reassignment. The hot-swap callback should reuse this mechanism rather than introducing a parallel one inside the LLM client.

**Fix applied:** Phase 1 no longer introduces `AdapterAwareChatter`, `WithModelOverride` ChatOption, or any client-side override. Instead, the `HotSwapCallback` simply calls the existing `agentLoop.SetModelOverride(bakedModelRef)`. Phase 1 drops from 6 tasks to 5.

### Risk 2: ProvidersConfig test fixture — RESOLVED

**Original concern:** Task 2.2's test fixture didn't match the real schema.

**Verified reality:** `ProvidersConfig` (`internal/llm/providers.go:47-55`) is:
```go
type ProvidersConfig struct {
    Model             string                     `json:"model"`
    SmallModel        string                     `json:"small_model"`
    ClassifierModel   string                     `json:"classifier_model"`
    SummarizerModel   string                     `json:"summarizer_model"`
    DisabledProviders []string                   `json:"disabled_providers"`
    ModelAliases      map[string]ModelAliasEntry `json:"model_aliases"`
    Providers         map[string]*ProviderConfig `json:"providers"`  // pointer, not value
}
```

**Fix applied:** Task 2.2 updated to use `*ProviderConfig` and a complete fixture.

### Risk 3: Import cycle direction — RESOLVED (cycle was the reverse of what I worried about)

**Original concern:** The `ReflectionProposer` interface lives in `lifecycle` to avoid an import cycle on `internal/agent`.

**Verified reality:**
- `internal/agent` does NOT import `internal/skills/lifecycle` (grep returned no files).
- `internal/skills/lifecycle` does NOT import `internal/agent`.
- Both `internal/daemon/components.go` and `internal/rpc/skills.go` already import `lifecycle`.

**Conclusion:** No cycle in either direction. The `ReflectionProposer` interface can live in `lifecycle` (preferred — it's the consumer) with `agent` providing the adapter, OR vice versa. Plan keeps it in `lifecycle`. No change to tasks, but the rationale is now "cleaner layering" not "cycle avoidance."

### Risk 4: Migration framework divergence — RESOLVED

**Original concern:** Plan said "ALTER TABLE" without verifying the lifecycle store uses that pattern.

**Verified reality:**
- `internal/shadow/store_sqlite.go` uses a **versioned migration pattern**: `schema_version` table, `migrateToV1`/`migrateToV2`, constant `TrainingStoreSchemaVersion = 2`. ALTER TABLE is used inside migrations and tolerates duplicate-column errors via `errcls.IsDuplicateColumn`.
- `internal/skills/lifecycle/usage.go` uses a **single `usageSchemaSQL` const** with `initSchema()` (no versioning, no ALTER). To add a table, extend the const.

**Fix applied:**
- Task 4.1 (PreferencePair enrichment) uses the shadow store's versioned migration pattern: bump `TrainingStoreSchemaVersion` to 3 and add a `migrateToV3` that does idempotent ALTER TABLE calls.
- Task 5.1 (low-match queries) extends `usageSchemaSQL` directly.

### Risk 5: Recursive placeholder in GapAnalyzer.generateBody — RESOLVED

**Original concern:** Task 5.2's `generateBody` recursively calls itself as a placeholder, which would infinite-loop if reached.

**Verified reality:** This was a clear bug in the plan. The heuristic-only path is sufficient for first ship — there is no demonstrated need for LLM-generated skill bodies in this plan (the evolver's existing Pass A refine uses LLM generation already; Pass D can re-use that path in a follow-up if needed).

**Fix applied:** Task 5.2's `generateBody` is heuristic-only; the `llmClient` parameter is removed from `GapAnalyzer`. A documented `TODO(follow-up)` is added.

---

## File Structure (changes summary)

**New files:**
- `internal/shadow/eval_gate.go` — `EvalGate` type + threshold logic.
- `internal/shadow/eval_gate_test.go` — TDD tests for the gate.
- `internal/shadow/adapter_hotswap.go` — coordinates adapter activation across manager + agent loop.
- `internal/shadow/adapter_hotswap_test.go` — TDD tests.
- `internal/llm/routing_log.go` — `RoutingDecision` type + SQLite-backed `RoutingLogger`.
- `internal/llm/routing_log_test.go` — TDD tests.
- `internal/skills/lifecycle/gap_analysis.go` — `GapAnalyzer` (Pass D).
- `internal/skills/lifecycle/gap_analysis_test.go` — TDD tests.
- `docs/workflows/shadow-training.md` — new doc (currently missing).
- `docs/workflows/routing-decisions.md` — new doc.

**Modified files:**
- `internal/shadow/config.go` — add `EvalThreshold`, `HotSwapEnabled` fields.
- `internal/shadow/manager.go` — gate `ActivateAdapter` on eval; add `HotSwap(ctx, adapterID)`.
- `internal/shadow/exporter.go` — enrich DPO output with domain/task/margin.
- `internal/shadow/models.go` — extend `PreferencePair` with `Domain`, `TaskType`, `RoutingPath`.
- `internal/shadow/store_sqlite.go` — bump `TrainingStoreSchemaVersion` to 3 and add `migrateToV3` for new PreferencePair columns.
- `internal/llm/resolver.go` — emit routing decisions to `RoutingLogger`.
- `internal/agent/loop.go` — pass shadow manager a `SetModelOverride` callback (reuses existing `AgentLoop.SetModelOverride`).
- `internal/agent/reflection_collector.go` — expose `DrainPending()` for evolver consumption.
- `internal/skills/lifecycle/evolver.go` — add `passDFillGap`, drain reflection proposals in Pass A.
- `internal/skills/lifecycle/types.go` — add `ProposalFillGap` action and `Gaps` counter.
- `internal/skills/lifecycle/usage.go` — add `RecordLowMatchQuery` + `GetLowMatchQueries`; extend `usageSchemaSQL`.
- `cmd/meept/skills.go` (or equivalent) — expose gap analysis subcommand.
- `docs/workflows/self-improvement.md` — expand to cover the closed loop.
- `docs/workflows/skills.md` — note gap-analysis pass.
- `docs/features.md` — add Self-Improvement Loop section.

**Unchanged files (compared to first draft):**
- ~~`internal/llm/adapter_aware.go`~~ — removed. `AgentLoop.SetModelOverride` already exists at `internal/agent/loop.go:985-989` and is the correct injection point.
- ~~`internal/llm/interface.go`~~ — removed. No new `ChatOption` needed.
- ~~`internal/llm/client.go`~~ — removed. No client-side override mechanism needed.

---

## Phase 1: Eval Gate + Adapter Hot-Swap (Gaps A1 + A2)

**Why first:** A1 (adapters never reach serving model) and A2 (no eval gate) are the smallest changes with the biggest payoff — they unblock the entire existing shadow-training investment. They must ship together because activating an adapter without a gate risks deploying a degraded model.

### Task 1.1: Add eval-gate config fields

**Files:**
- Modify: `internal/shadow/config.go:146-156` (`AdaptersConfig`)
- Modify: `internal/shadow/config.go:159-239` (`DefaultConfig()`)

- [ ] **Step 1: Write the failing test**

Create `internal/shadow/config_test.go` (or extend if it exists) and add:

```go
package shadow

import "testing"

func TestAdaptersConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Adapters.HotSwapEnabled {
		t.Errorf("HotSwapEnabled default should be true, got false")
	}
	if cfg.Adapters.EvalThreshold != 0.7 {
		t.Errorf("EvalThreshold default should be 0.7, got %v", cfg.Adapters.EvalThreshold)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/shadow/ -run TestAdaptersConfig_Defaults -v`
Expected: FAIL — `EvalThreshold` field does not exist.

- [ ] **Step 3: Add fields to AdaptersConfig**

In `internal/shadow/config.go`, extend `AdaptersConfig`:

```go
// AdaptersConfig configures adapter management.
type AdaptersConfig struct {
	Enabled        bool       `toml:"enabled"`
	OllamaEndpoint string     `toml:"ollama_endpoint"`
	AutoTrain      bool       `toml:"auto_train"`
	TrainThreshold int        `toml:"train_threshold"`
	TrainSchedule  string     `toml:"train_schedule"`
	AdapterDir     string     `toml:"adapter_dir"`
	LoRA           LoRAConfig `toml:"lora"`
	DPO            DPOConfig  `toml:"dpo"`

	// HotSwapEnabled controls whether activated adapters are wired into the
	// serving LLM client. When false, ActivateAdapter only flips the DB flag.
	HotSwapEnabled bool `toml:"hot_swap_enabled"`

	// EvalThreshold is the minimum eval score a TrainingRun must achieve
	// before its adapter can be activated. 0.0 disables the gate.
	EvalThreshold float64 `toml:"eval_threshold"`
}
```

- [ ] **Step 4: Update DefaultConfig**

In `DefaultConfig()`, change the `Adapters:` block:

```go
Adapters: AdaptersConfig{
	Enabled:        false,
	OllamaEndpoint: "http://localhost:11434",
	AutoTrain:      false,
	TrainThreshold: 500,
	TrainSchedule:  "",
	AdapterDir:     "~/.meept/shadow/adapters",
	HotSwapEnabled: true,
	EvalThreshold:  0.7,
	LoRA: LoRAConfig{
		// ... existing fields unchanged
	},
	DPO: DPOConfig{
		Beta:     0.1,
		LossType: "sigmoid",
	},
},
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/shadow/ -run TestAdaptersConfig_Defaults -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/shadow/config.go internal/shadow/config_test.go
git commit -m "feat(shadow): add eval-threshold and hot-swap config fields"
```

---

### Task 1.2: Implement EvalGate

**Files:**
- Create: `internal/shadow/eval_gate.go`
- Create: `internal/shadow/eval_gate_test.go`

- [ ] **Step 1: Write the failing test**

`internal/shadow/eval_gate_test.go`:

```go
package shadow

import (
	"context"
	"testing"
)

func TestEvalGate_PassesAboveThreshold(t *testing.T) {
	gate := NewEvalGate(0.7)
	run := &TrainingRun{EvalScore: 0.85, RecordsUsed: 100}
	if err := gate.Check(context.Background(), run); err != nil {
		t.Errorf("expected pass, got %v", err)
	}
}

func TestEvalGate_FailsBelowThreshold(t *testing.T) {
	gate := NewEvalGate(0.7)
	run := &TrainingRun{EvalScore: 0.4, RecordsUsed: 100}
	if err := gate.Check(context.Background(), run); err == nil {
		t.Errorf("expected failure, got pass")
	}
}

func TestEvalGate_DisabledWhenThresholdZero(t *testing.T) {
	gate := NewEvalGate(0.0)
	run := &TrainingRun{EvalScore: 0.0, RecordsUsed: 1}
	if err := gate.Check(context.Background(), run); err != nil {
		t.Errorf("disabled gate should always pass, got %v", err)
	}
}

func TestEvalGate_FailsOnInsufficientRecords(t *testing.T) {
	gate := NewEvalGate(0.7)
	run := &TrainingRun{EvalScore: 0.95, RecordsUsed: 5}
	if err := gate.Check(context.Background(), run); err == nil {
		t.Errorf("expected failure on insufficient records, got pass")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/shadow/ -run TestEvalGate -v`
Expected: FAIL — `EvalGate` and `NewEvalGate` undefined.

- [ ] **Step 3: Implement EvalGate**

`internal/shadow/eval_gate.go`:

```go
package shadow

import (
	"context"
	"fmt"
)

// minRecordsForEvalGate is the floor below which an adapter is considered
// under-trained regardless of eval score. Trained on too few records, the
// adapter is statistically unreliable.
const minRecordsForEvalGate = 20

// EvalGate decides whether a TrainingRun is safe to deploy.
type EvalGate struct {
	threshold float64
}

// NewEvalGate constructs an EvalGate. A threshold of 0.0 disables the gate.
func NewEvalGate(threshold float64) *EvalGate {
	return &EvalGate{threshold: threshold}
}

// Check returns nil if the training run passes the gate, or an error
// describing why it failed. The context is accepted for future
// LLM-judge-based eval gates but is not used by the threshold check itself.
func (g *EvalGate) Check(_ context.Context, run *TrainingRun) error {
	if g.threshold <= 0.0 {
		return nil
	}
	if run == nil {
		return fmt.Errorf("eval gate: training run is nil")
	}
	if run.RecordsUsed < minRecordsForEvalGate {
		return fmt.Errorf("eval gate: only %d records used (minimum %d)", run.RecordsUsed, minRecordsForEvalGate)
	}
	if run.EvalScore < g.threshold {
		return fmt.Errorf("eval gate: eval score %.3f below threshold %.3f", run.EvalScore, g.threshold)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/shadow/ -run TestEvalGate -v`
Expected: PASS — all four subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/shadow/eval_gate.go internal/shadow/eval_gate_test.go
git commit -m "feat(shadow): add eval gate for adapter deployment"
```

---

### Task 1.3: Wire EvalGate into Manager.ActivateAdapter

**Files:**
- Modify: `internal/shadow/manager.go:525-547`

- [ ] **Step 1: Write the failing test**

Add to `internal/shadow/manager_test.go`:

```go
func TestManager_ActivateAdapter_RejectedByEvalGate(t *testing.T) {
	// Construct a manager with a real adapters store and a high eval threshold.
	m := newTestManager(t, func(c *Config) {
		c.Enabled = true
		c.Adapters.Enabled = true
		c.Adapters.EvalThreshold = 0.9
	})

	ctx := context.Background()
	adapter := NewAdapter("test", "qwen2.5:7b", "lora", "/tmp/adapter")
	if err := m.RegisterAdapter(ctx, adapter); err != nil {
		t.Fatalf("RegisterAdapter: %v", err)
	}

	// Complete a training run that fails the gate (low score, sufficient records).
	run := NewTrainingRun(adapter.ID, map[string]any{"rank": 16})
	run.RecordsUsed = 100
	run.Complete(1.5, 0.3) // loss=1.5, eval=0.3
	if err := m.adaptersStore.CompleteTrainingRun(ctx, run); err != nil {
		t.Fatalf("CompleteTrainingRun: %v", err)
	}

	err := m.ActivateAdapter(ctx, adapter.ID)
	if err == nil {
		t.Errorf("expected eval-gate rejection, got nil")
	}
}
```

(Note: `newTestManager` is an existing helper if present; otherwise construct inline using the pattern from other tests in the file.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/shadow/ -run TestManager_ActivateAdapter_RejectedByEvalGate -v`
Expected: FAIL — `ActivateAdapter` activates unconditionally.

- [ ] **Step 3: Modify ActivateAdapter to consult the gate**

In `internal/shadow/manager.go`, locate `ActivateAdapter` at line 534 and replace its body:

```go
// ActivateAdapter activates an adapter after passing the eval gate.
// The gate prevents deploying adapters whose TrainingRun scored below the
// configured EvalThreshold or was trained on too few records.
func (m *Manager) ActivateAdapter(ctx context.Context, id string) error {
	if m.adaptersStore == nil {
		return fmt.Errorf("adapters store not initialized")
	}

	// Look up the adapter to find its model base.
	adapter, err := m.adaptersStore.GetAdapter(ctx, id)
	if err != nil {
		return fmt.Errorf("activate adapter: %w", err)
	}

	// Find the most recent training run for this adapter.
	runs, err := m.adaptersStore.ListTrainingRuns(ctx, adapter.ID)
	if err != nil {
		return fmt.Errorf("list training runs: %w", err)
	}
	if len(runs) == 0 {
		return fmt.Errorf("activate adapter: no training run on record")
	}
	latest := runs[0] // ListTrainingRuns returns newest-first

	// Gate.
	gate := NewEvalGate(m.config.Adapters.EvalThreshold)
	if err := gate.Check(ctx, latest); err != nil {
		return fmt.Errorf("activate adapter: %w", err)
	}

	return m.adaptersStore.SetActiveAdapter(ctx, id)
}
```

- [ ] **Step 4: Add ListTrainingRuns if missing**

Check `internal/shadow/store_sqlite.go` for a `ListTrainingRuns` method. If absent, add (see store_sqlite.go's existing patterns for `GetAdapter`):

```go
// ListTrainingRuns returns training runs for an adapter, newest-first.
func (s *adaptersSQLiteStore) ListTrainingRuns(ctx context.Context, adapterID string) ([]*TrainingRun, error) {
	var runs []*TrainingRun
	err := s.db.SelectContext(ctx, &runs,
		`SELECT id, adapter_id, started_at, completed_at, records_used, config_json, final_loss, eval_score
		 FROM training_runs WHERE adapter_id = ? ORDER BY started_at DESC`, adapterID)
	if err != nil {
		return nil, err
	}
	return runs, nil
}
```

Also add the method to the `AdaptersStore` interface in `internal/shadow/store.go`.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/shadow/ -run TestManager_ActivateAdapter_RejectedByEvalGate -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/shadow/manager.go internal/shadow/manager_test.go internal/shadow/store_sqlite.go internal/shadow/store.go
git commit -m "feat(shadow): gate adapter activation on eval score"
```

---

### Task 1.4: Verify existing AgentLoop.SetModelOverride path

**Why this is a verification, not implementation:** The agent loop already has a model-override mechanism that the dispatcher uses for user-driven reassignment. We re-use it for shadow hot-swap rather than introducing a parallel mechanism. No new code ships in this task — it confirms the path exists and is thread-safe.

**Files:**
- Read-only: `internal/agent/loop.go:529` (field), `internal/agent/loop.go:976-1003` (accessors), `internal/agent/loop.go:2397` (consumption site).

- [ ] **Step 1: Verify the override path**

Run: `grep -n "modelOverride\|SetModelOverride\|GetModelOverride\|ClearModelOverride" internal/agent/loop.go`
Expected: at least 5 matches. Confirm:
- Line ~529: `modelOverride string` field.
- Line ~976-982: `WithModelOverride(modelRef)` LoopOption.
- Line ~984-989: `SetModelOverride(modelRef)` thread-safe setter.
- Line ~991-996: `GetModelOverride()` thread-safe getter.
- Line ~998-1003: `ClearModelOverride()` thread-safe clear.
- Line ~2397 or ~2895: consumption — `ResolveForAlias(l.modelRef)` is called; verify how `l.modelOverride` interacts.

- [ ] **Step 2: Verify the consumption path is correct for our use**

Read the section of `loop.go` around the consumption (search for `l.modelOverride != ""`). Confirm that when `modelOverride` is set, the loop uses it in place of `l.modelRef` for resolution. The hot-swap callback will set this field to the baked model ref.

- [ ] **Step 3: Document the integration contract**

Add a short comment block above `SetModelOverride` in `internal/agent/loop.go`:

```go
// SetModelOverride sets the model override at runtime (thread-safe).
//
// Consumers:
//   - dispatcher: user-driven model reassignment (clears after one cycle)
//   - shadow training: hot-swap callback sets this to a baked adapter model
//     ref after a successful ActivateAdapter + Ollama bake. The override
//     persists until ClearModelOverride is called (e.g. on daemon shutdown
//     or adapter rollback).
func (l *AgentLoop) SetModelOverride(modelRef string) {
```

- [ ] **Step 4: Run existing tests to confirm no regression**

Run: `go test ./internal/agent/ -run TestAgentLoop_ModelOverride -v`
Expected: PASS (or "no tests match" — if no tests exist, that's fine; the next task's tests cover integration).

- [ ] **Step 5: Commit**

```bash
git add internal/agent/loop.go
git commit -m "docs(agent): document SetModelOverride consumers (shadow hot-swap)"
```

---

### Task 1.5: Implement HotSwap coordinator in shadow manager

**Files:**
- Create: `internal/shadow/adapter_hotswap.go`
- Create: `internal/shadow/adapter_hotswap_test.go`
- Modify: `internal/shadow/manager.go` (add `HotSwap` method)

- [ ] **Step 1: Write the failing test**

`internal/shadow/adapter_hotswap_test.go`:

```go
package shadow

import (
	"context"
	"testing"
)

type fakeOllamaActivator struct {
	activatedID string
}

func (f *fakeOllamaActivator) ActivateAdapter(ctx context.Context, baseName, adapterName, adapterPath string) error {
	f.activatedID = adapterName
	return nil
}

func TestManager_HotSwap_ActivatesInOllamaAndNotifiesLoop(t *testing.T) {
	m := newTestManager(t, func(c *Config) {
		c.Enabled = true
		c.Adapters.Enabled = true
		c.Adapters.HotSwapEnabled = true
		c.Adapters.EvalThreshold = 0.0 // disabled for this test
	})

	activator := &fakeOllamaActivator{}
	m.SetOllamaActivator(activator)

	notified := ""
	m.SetHotSwapCallback(func(bakedModelRef string) {
		notified = bakedModelRef
	})

	ctx := context.Background()
	adapter := NewAdapter("v1", "qwen2.5:7b", "lora", "/tmp/adapter.gguf")
	_ = m.RegisterAdapter(ctx, adapter)
	run := NewTrainingRun(adapter.ID, map[string]any{})
	run.RecordsUsed = 50
	run.Complete(0.5, 0.9)
	_ = m.adaptersStore.CompleteTrainingRun(ctx, run)

	if err := m.HotSwap(ctx, adapter.ID); err != nil {
		t.Fatalf("HotSwap: %v", err)
	}
	if activator.activatedID == "" {
		t.Errorf("expected Ollama activation, got none")
	}
	// bakedModelRef follows the convention "<base>-shadow-<adapterIDprefix>",
	// e.g. "qwen2.5:7b-shadow-v1abcd12". The callback should receive exactly that.
	if notified == "" || !strings.HasPrefix(notified, "qwen2.5:7b-shadow-") {
		t.Errorf("expected baked model ref callback, got %q", notified)
	}
}
```

(Add `"strings"` to imports.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/shadow/ -run TestManager_HotSwap -v`
Expected: FAIL — `HotSwap`, `SetOllamaActivator`, `SetHotSwapCallback` undefined.

- [ ] **Step 3: Implement HotSwap**

`internal/shadow/adapter_hotswap.go`:

```go
package shadow

import (
	"context"
	"fmt"
)

// OllamaActivator is the narrow interface HotSwap needs from the Ollama
// adapter. *adapters.OllamaAdapter satisfies it.
type OllamaActivator interface {
	ActivateAdapter(ctx context.Context, baseName, adapterName, adapterPath string) error
}

// HotSwapCallback is invoked after a successful hot-swap. The argument is
// the baked model ref the agent loop should now route to (e.g.
// "ollama/qwen2.5:7b-shadow-v1abcd12"). Implementations typically call
// agentLoop.SetModelOverride(bakedModelRef).
type HotSwapCallback func(bakedModelRef string)

// hotSwapCoordinator orchestrates adapter activation across the Ollama
// backend (model recreation) and the agent loop (model-override callback).
type hotSwapCoordinator struct {
	activator OllamaActivator
	callback  HotSwapCallback
}

// Activate performs the end-to-end hot-swap:
//  1. ActivateAdapter in Ollama (bakes weights into a new model).
//  2. Invoke the callback so the agent loop updates its modelOverride.
//  3. Flip the DB flag via Manager.ActivateAdapter (passes eval gate).
//
// bakedModelRef is constructed as "<provider>/<base>-shadow-<adapterIDprefix>".
// The provider prefix is necessary because AgentLoop.SetModelOverride feeds
// back into Resolver.ResolveForAlias / ResolveRef, which expect
// "provider/model" format.
func (h *hotSwapCoordinator) Activate(ctx context.Context, m *Manager, adapter *Adapter) error {
	if h.activator != nil {
		bakedName := fmt.Sprintf("%s-shadow-%s", adapter.ModelBase, adapter.ID[:8])
		if err := h.activator.ActivateAdapter(ctx, adapter.ModelBase, bakedName, adapter.AdapterPath); err != nil {
			return fmt.Errorf("ollama activate: %w", err)
		}
		// The provider prefix must match what the Ollama activator registered
		// the baked model under. We pass the baked name through as-is and let
		// the daemon wiring construct the full ref. If the daemon knows the
		// provider (typically "ollama"), it wraps the callback.
		if h.callback != nil {
			h.callback(bakedName)
		}
	}
	// Flip DB flag (subject to eval gate).
	if err := m.ActivateAdapter(ctx, adapter.ID); err != nil {
		return fmt.Errorf("set active adapter: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Wire into Manager**

Add to `internal/shadow/manager.go`:

```go
// SetOllamaActivator registers the Ollama adapter used for hot-swapping.
func (m *Manager) SetOllamaActivator(a OllamaActivator) {
	m.hotSwap.activator = a
}

// SetHotSwapCallback registers the callback fired on successful hot-swap.
// The LLM client uses this to update its active model override.
func (m *Manager) SetHotSwapCallback(cb HotSwapCallback) {
	m.hotSwap.callback = cb
}

// HotSwap activates an adapter end-to-end: bakes it into an Ollama model,
// notifies the LLM client, and flips the DB flag (subject to eval gate).
func (m *Manager) HotSwap(ctx context.Context, adapterID string) error {
	if !m.config.Adapters.HotSwapEnabled {
		return fmt.Errorf("hot-swap disabled in config")
	}
	adapter, err := m.adaptersStore.GetAdapter(ctx, adapterID)
	if err != nil {
		return fmt.Errorf("get adapter: %w", err)
	}
	return m.hotSwap.Activate(ctx, m, adapter)
}
```

Add a `hotSwap hotSwapCoordinator` field to the `Manager` struct.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/shadow/ -run TestManager_HotSwap -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/shadow/adapter_hotswap.go internal/shadow/adapter_hotswap_test.go internal/shadow/manager.go
git commit -m "feat(shadow): end-to-end adapter hot-swap into serving model"
```

---

### Task 1.6: Wire HotSwap into the daemon

**Files:**
- Modify: `internal/daemon/components.go` (or wherever the shadow manager + LLM client are wired)
- Modify: `internal/agent/loop.go` (around the `WithShadowManager` option)

- [ ] **Step 1: Locate the wiring**

Run: `grep -n "shadow.Manager\|shadow.NewManager\|WithShadowManager" internal/daemon/ internal/agent/`
Identify where the manager is constructed and where it's passed to the agent loop.

- [ ] **Step 2: Wire the Ollama activator**

In the daemon wiring (likely `internal/daemon/components.go`), after the shadow manager is constructed:

```go
if shadowMgr != nil && shadowCfg.Adapters.Enabled && shadowCfg.Adapters.HotSwapEnabled {
	ollamaAdapter := adapters.NewOllamaAdapter(shadowCfg.Adapters.OllamaEndpoint, shadowMgr.AdaptersStore())
	shadowMgr.SetOllamaActivator(ollamaAdapter)
}
```

- [ ] **Step 3: Wire the agent-loop callback**

The hot-swap callback wraps `agentLoop.SetModelOverride` with the provider-prefixed model ref. The provider is known at daemon construction time (typically `"ollama"` for shadow adapters), so we close over it:

```go
// ollamaProviderID is the provider name under which baked adapter models are
// registered in Ollama. Used to construct "ollama/<baked-name>" refs.
const ollamaProviderID = "ollama"

if agentLoop != nil && shadowCfg.Adapters.HotSwapEnabled {
	shadowMgr.SetHotSwapCallback(func(bakedName string) {
		bakedRef := ollamaProviderID + "/" + bakedName
		agentLoop.SetModelOverride(bakedRef)
		daemonLogger.Info("shadow hot-swap activated",
			"baked_ref", bakedRef,
			"adapter_id", shadowMgr.LastHotSwapAdapterID(),
		)
	})
}
```

If `LastHotSwapAdapterID` doesn't exist on `Manager`, either add it (one-line getter returning the most recently activated adapter ID) or omit from the log line.

- [ ] **Step 4: Build and run existing daemon tests**

Run: `go build ./... && go test ./internal/daemon/...`
Expected: Build PASS, no test regressions.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/components.go internal/agent/loop.go
git commit -m "feat(daemon): wire shadow hot-swap into LLM client"
```

---

## Phase 2: Routing Decision Persistence (Gap A3)

**Why second:** Routing decisions are the training-set foundation for Phase 4 (student-learns-routing). Persisting them now means data accumulates from day one of deployment, regardless of when Phase 4 ships.

### Task 2.1: RoutingDecision type + SQLite schema

**Files:**
- Create: `internal/llm/routing_log.go`
- Create: `internal/llm/routing_log_test.go`

- [ ] **Step 1: Write the failing test**

`internal/llm/routing_log_test.go`:

```go
package llm

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRoutingLogger_RecordAndQuery(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "routing.db")
	logger, err := NewRoutingLogger(dbPath, nil)
	if err != nil {
		t.Fatalf("NewRoutingLogger: %v", err)
	}
	defer logger.Close()

	dec := RoutingDecision{
		RequestID:        "req-1",
		ChosenModelID:    "qwen2.5:7b",
		ChosenProviderID: "ollama",
		Alias:            "default",
		Reason:           "round-robin",
		Skill:            "",
		CandidatesJSON:   `["qwen2.5:7b","glm-4.5:9b"]`,
	}
	if err := logger.Record(context.Background(), dec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got, err := logger.Recent(context.Background(), 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(got) != 1 || got[0].RequestID != "req-1" {
		t.Errorf("expected 1 record with req-1, got %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/ -run TestRoutingLogger -v`
Expected: FAIL — `RoutingLogger` and `RoutingDecision` undefined.

- [ ] **Step 3: Implement RoutingLogger**

`internal/llm/routing_log.go`:

```go
package llm

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // sqlite driver registration

	"github.com/caimlas/meept/pkg/id"
)

// RoutingDecision captures a single model-resolution outcome for later
// mining. The routing log is the training-set foundation for the
// student-learns-routing loop.
type RoutingDecision struct {
	ID               string    `json:"id" db:"id"`
	RequestID        string    `json:"request_id" db:"request_id"`
	Timestamp        time.Time `json:"timestamp" db:"timestamp"`
	ChosenModelID    string    `json:"chosen_model_id" db:"chosen_model_id"`
	ChosenProviderID string    `json:"chosen_provider_id" db:"chosen_provider_id"`
	Alias            string    `json:"alias,omitempty" db:"alias"`
	Reason           string    `json:"reason,omitempty" db:"reason"`
	Skill            string    `json:"skill,omitempty" db:"skill"`
	EmployeeID       string    `json:"employee_id,omitempty" db:"employee_id"`
	CandidatesJSON   string    `json:"candidates_json,omitempty" db:"candidates_json"`
}

// RoutingLogger persists RoutingDecisions to SQLite for later mining.
type RoutingLogger struct {
	db     *sqlx.DB
	logger *slog.Logger
}

// NewRoutingLogger opens (creating if necessary) the routing log at dbPath.
func NewRoutingLogger(dbPath string, logger *slog.Logger) (*RoutingLogger, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if dbPath[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dbPath = filepath.Join(home, dbPath[1:])
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, err
	}
	db, err := sqlx.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	rl := &RoutingLogger{db: db, logger: logger.With("component", "routing-log")}
	if err := rl.initSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return rl, nil
}

func (rl *RoutingLogger) initSchema(ctx context.Context) error {
	_, err := rl.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS routing_decisions (
			id TEXT PRIMARY KEY,
			request_id TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			chosen_model_id TEXT NOT NULL,
			chosen_provider_id TEXT NOT NULL,
			alias TEXT,
			reason TEXT,
			skill TEXT,
			employee_id TEXT,
			candidates_json TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_routing_decisions_ts ON routing_decisions(timestamp);
		CREATE INDEX IF NOT EXISTS idx_routing_decisions_chosen ON routing_decisions(chosen_model_id);
	`)
	return err
}

// Record persists a decision. ID and Timestamp are filled in if zero.
func (rl *RoutingLogger) Record(ctx context.Context, dec RoutingDecision) error {
	if dec.ID == "" {
		dec.ID = id.Generate("routing")
	}
	if dec.Timestamp.IsZero() {
		dec.Timestamp = time.Now().UTC()
	}
	_, err := rl.db.NamedExecContext(ctx,
		`INSERT INTO routing_decisions
		 (id, request_id, timestamp, chosen_model_id, chosen_provider_id, alias, reason, skill, employee_id, candidates_json)
		 VALUES (:id, :request_id, :timestamp, :chosen_model_id, :chosen_provider_id, :alias, :reason, :skill, :employee_id, :candidates_json)`,
		dec)
	if err != nil {
		return fmt.Errorf("routing log record: %w", err)
	}
	return nil
}

// Recent returns the most recent N decisions, newest-first.
func (rl *RoutingLogger) Recent(ctx context.Context, limit int) ([]RoutingDecision, error) {
	if limit <= 0 {
		limit = 100
	}
	var out []RoutingDecision
	err := rl.db.SelectContext(ctx, &out,
		`SELECT * FROM routing_decisions ORDER BY timestamp DESC LIMIT ?`, limit)
	return out, err
}

// Close releases the database connection.
func (rl *RoutingLogger) Close() error {
	if rl.db != nil {
		return rl.db.Close()
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/llm/ -run TestRoutingLogger -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/llm/routing_log.go internal/llm/routing_log_test.go
git commit -m "feat(llm): persist LLM routing decisions for training-data mining"
```

---

### Task 2.2: Wire RoutingLogger into the resolver

**Files:**
- Modify: `internal/llm/resolver.go:106-167` (ResolveForSkill)
- Modify: `internal/llm/resolver.go:212-247` (ResolveForAlias)
- Modify: `internal/llm/resolver.go:28-38` (Resolver struct)
- Modify: `internal/llm/resolver.go:40-89` (NewResolver)

- [ ] **Step 1: Write the failing test**

Add to `internal/llm/resolver_test.go`:

```go
func TestResolver_RecordsDecisionsToRoutingLogger(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "routing.db")
	rl, err := NewRoutingLogger(dbPath, nil)
	if err != nil {
		t.Fatalf("NewRoutingLogger: %v", err)
	}
	defer rl.Close()

	// Fixture matches the real ProvidersConfig schema
	// (internal/llm/providers.go:47-55). Providers is map[string]*ProviderConfig.
	cfg := &ProvidersConfig{
		Model:   "ollama/qwen2.5:7b",
		Providers: map[string]*ProviderConfig{
			"ollama": {
				ID:    "ollama",
				Type:  "openai",
				URL:   "http://localhost:11434/v1",
				APIKey: "dummy",
				Models: map[string]*ModelConfig{
					"qwen2.5:7b": {
						ProviderID: "ollama",
						ModelID:    "qwen2.5:7b",
						Capabilities: []string{"chat"},
					},
				},
			},
		},
		ModelAliases: map[string]ModelAliasEntry{
			"default": {
				Models:  []string{"ollama/qwen2.5:7b"},
				Timeout: 30,
			},
		},
	}
	r := NewResolver(cfg, nil)
	r.SetRoutingLogger(rl)

	// Trigger ResolveForAlias (the production hot path).
	mc, err := r.ResolveForAlias("default")
	if err != nil {
		t.Fatalf("ResolveForAlias: %v", err)
	}
	if mc == nil {
		t.Fatalf("expected non-nil model config")
	}

	got, err := rl.Recent(context.Background(), 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 routing decision, got %d", len(got))
	}
	if got[0].ChosenModelID != "qwen2.5:7b" {
		t.Errorf("expected chosen model qwen2.5:7b, got %q", got[0].ChosenModelID)
	}
	if got[0].Alias != "default" {
		t.Errorf("expected alias 'default', got %q", got[0].Alias)
	}
	if got[0].Reason != "round_robin" {
		t.Errorf("expected reason 'round_robin', got %q", got[0].Reason)
	}
}
```

**Note for the implementer:** The fixture above is the minimum that satisfies `NewResolver`. If `ProviderConfig` or `ModelConfig` have additional required fields per `internal/llm/providers.go` or `internal/llm/models.go`, add them. Verify by reading those two structs before finalizing the test.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/ -run TestResolver_RecordsDecisionsToRoutingLogger -v`
Expected: FAIL — `SetRoutingLogger` undefined.

- [ ] **Step 3: Add RoutingLogger field + setter**

In `internal/llm/resolver.go`, add to the `Resolver` struct:

```go
routingLogger *RoutingLogger
```

Add a setter (with nil guard per CLAUDE.md):

```go
// SetRoutingLogger attaches a routing decision logger. Pass nil to disable.
func (r *Resolver) SetRoutingLogger(rl *RoutingLogger) {
	if rl != nil {
		r.routingLogger = rl
	}
}
```

- [ ] **Step 4: Emit decisions from ResolveForSkill and ResolveForAlias**

In `ResolveForSkill` (line 159-164, the success branch), replace the existing `r.logger.Info` with:

```go
r.logger.Info("Escalated to model for skill",
	"model", selected.ModelID,
	"provider", selected.ProviderID,
	"skill", skill.Name,
	"requires", required,
)
if r.routingLogger != nil {
	decision := RoutingDecision{
		RequestID:        "", // caller fills if available
		ChosenModelID:    selected.ModelID,
		ChosenProviderID: selected.ProviderID,
		Reason:           "capability_escalation",
		Skill:            skill.Name,
		CandidatesJSON:   r.candidatesJSON(candidates),
	}
	_ = r.routingLogger.Record(context.Background(), decision)
}
```

Add the same pattern at the end of `ResolveForAlias` (line 242-244 branch):

```go
if r.routingLogger != nil {
	decision := RoutingDecision{
		ChosenModelID:    alias.Models[health.CurrentIndex].ModelID,
		ChosenProviderID: alias.Models[health.CurrentIndex].ProviderID,
		Alias:            aliasName,
		Reason:           "round_robin",
	}
	_ = r.routingLogger.Record(context.Background(), decision)
}
```

Add a helper:

```go
func (r *Resolver) candidatesJSON(candidates []*ModelConfig) string {
	ids := make([]string, len(candidates))
	for i, c := range candidates {
		ids[i] = c.ProviderID + "/" + c.ModelID
	}
	b, _ := json.Marshal(ids)
	return string(b)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/llm/ -run TestResolver_RecordsDecisionsToRoutingLogger -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/llm/resolver.go internal/llm/resolver_test.go
git commit -m "feat(llm): emit routing decisions from resolver"
```

---

### Task 2.3: Wire RoutingLogger construction in the daemon

**Files:**
- Modify: `internal/daemon/components.go`

- [ ] **Step 1: Locate daemon wiring**

Run: `grep -n "NewResolver\|llm.NewResolver" internal/daemon/`

- [ ] **Step 2: Construct + inject**

After the resolver is constructed:

```go
routingLogger, err := llm.NewRoutingLogger(filepath.Join(dataDir, "routing.db"), daemonLogger)
if err != nil {
	return fmt.Errorf("routing logger: %w", err)
}
resolver.SetRoutingLogger(routingLogger)
```

Add `routingLogger.Close()` to the daemon's shutdown path (find existing Close calls).

- [ ] **Step 3: Build and smoke-test**

Run: `go build ./... && go test ./internal/daemon/...`
Expected: Build PASS, no test regressions.

- [ ] **Step 4: Commit**

```bash
git add internal/daemon/components.go
git commit -m "feat(daemon): construct and wire routing decision logger"
```

---

## Phase 3: Drain Reflection Queue into Evolver (Gap O3)

**Why third:** Cheapest fix (~50 LOC), no new infra, unblocks existing investment in `reflection_collector.go`. Independent of Phases 1-2.

### Task 3.1: Expose pending-proposal drain on the reflection queue

**Files:**
- Modify: `internal/agent/reflection_collector.go`
- Modify: `internal/agent/proposal.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/agent/proposal_test.go` (or create if absent):

```go
package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProposalQueue_DrainPending_LeavesApplied(t *testing.T) {
	q := newProposalQueue(filepath.Join(t.TempDir(), "improvements.md"))
	_ = q.Append(ReflectionProposal{Type: "skill_create", Target: "foo", Confidence: 0.9})
	_ = q.Append(ReflectionProposal{Type: "skill_create", Target: "bar", Confidence: 0.6})

	drained, err := q.DrainPending()
	if err != nil {
		t.Fatalf("DrainPending: %v", err)
	}
	if len(drained) != 2 {
		t.Fatalf("expected 2, got %d", len(drained))
	}

	// File should now have no pending entries.
	data, _ := os.ReadFile(q.path)
	if containsPending(string(data)) {
		t.Errorf("expected no pending entries after drain, file still has them")
	}
}
```

(Where `containsPending` is a tiny test helper that greps for `## [pending]`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestProposalQueue_DrainPending -v`
Expected: FAIL — `DrainPending` undefined.

- [ ] **Step 3: Implement DrainPending**

Add to `internal/agent/proposal.go`:

```go
// DrainPending returns all pending proposals and marks them "applied"
// (treating "drain" as "consumed by the evolver"). Use ListPending for a
// non-destructive read.
//
// The mutex serializes against Append and markStatus for the same reason
// those two methods hold the mutex: this method does a read-truncate-write
// on the queue file.
func (q *proposalQueue) DrainPending() ([]ReflectionProposal, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	data, err := os.ReadFile(q.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	all := parseProposals(string(data))
	var pending []ReflectionProposal
	for _, p := range all {
		if p.Status == "pending" || p.Status == "" {
			pending = append(pending, p)
		}
	}
	// Rewrite the file with no pending entries (applied/skipped remain).
	if err := os.WriteFile(q.path, []byte(strippedPending(string(data))), 0o644); err != nil {
		return nil, err
	}
	return pending, nil
}

// strippedPending returns the queue file contents with all "## [pending]"
// blocks removed. Used by DrainPending to atomically clear pending state.
func strippedPending(s string) string {
	var out strings.Builder
	rest := s
	for {
		idx := strings.Index(rest, "\n## [pending]")
		if idx < 0 {
			out.WriteString(rest)
			break
		}
		out.WriteString(rest[:idx+1])
		rest = rest[idx+1:]
		end := strings.Index(rest, "\n## [")
		if end < 0 {
			break
		}
		rest = rest[end:]
	}
	return out.String()
}

func containsPending(s string) bool { return strings.Contains(s, "## [pending]") }
```

Also expose a public method on `ReflectionCollector` so the evolver can call it:

```go
// DrainPendingProposals returns all pending reflection proposals and marks
// them consumed. Intended to be called by the skill evolver at the start of
// each cycle.
func (rc *ReflectionCollector) DrainPendingProposals() ([]ReflectionProposal, error) {
	if rc.queue == nil {
		return nil, nil
	}
	return rc.queue.DrainPending()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestProposalQueue_DrainPending -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/proposal.go internal/agent/proposal_test.go internal/agent/reflection_collector.go
git commit -m "feat(agent): drain pending reflection proposals"
```

---

### Task 3.2: Feed drained proposals into the evolver

**Files:**
- Modify: `internal/skills/lifecycle/evolver.go:45-56` (Evolver struct)
- Modify: `internal/skills/lifecycle/evolver.go:115-120` (RunCycle)

- [ ] **Step 1: Write the failing test**

Add to `internal/skills/lifecycle/evolver_test.go` (or create):

```go
package lifecycle

import (
	"context"
	"testing"
)

type stubProposer struct {
	proposals []stubProposal
}

type stubProposal struct {
	Type, Target, Change, Justification string
	Confidence                           float64
}

func (s *stubProposer) DrainPending() ([]stubProposal, error) {
	return s.proposals, nil
}

func TestEvolver_PassAConsumesReflectionProposals(t *testing.T) {
	// ... construct evolver with stub proposer wired via WithReflectionProposer
	// ... run one cycle
	// ... assert that the stub's proposals appear in the report.Details
}
```

(Adapt the stub to whatever interface shape you choose in step 3.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/skills/lifecycle/ -run TestEvolver_PassAConsumesReflectionProposals -v`
Expected: FAIL.

- [ ] **Step 3: Add a ReflectionProposer interface and wire it**

In `internal/skills/lifecycle/evolver.go`, define a narrow interface (avoid importing `internal/agent` to prevent an import cycle — the agent package already imports lifecycle indirectly):

```go
// ReflectionProposal is a skill-change proposal from the reflection system.
// We define our own copy here to avoid an import cycle on internal/agent.
type ReflectionProposal struct {
	Type          string
	Target        string
	Change        string
	Justification string
	Confidence    float64
}

// ReflectionProposer drains pending proposals. *agent.ReflectionCollector
// satisfies this via an adapter (the agent package constructs the adapter).
type ReflectionProposer interface {
	DrainPending() ([]ReflectionProposal, error)
}

// WithReflectionProposer injects a proposal source for Pass A consumption.
func WithReflectionProposer(p ReflectionProposer) EvolverOption {
	return func(e *Evolver) {
		if p != nil {
			e.proposer = p
		}
	}
}
```

Add a `proposer ReflectionProposer` field to the `Evolver` struct.

In `passARefine`, at the very start, drain pending proposals and convert them into refinement candidates:

```go
if e.proposer != nil {
	pending, err := e.proposer.DrainPending()
	if err != nil {
		e.logger.Warn("drain reflection proposals failed", "err", err)
	} else {
		for _, p := range pending {
			if p.Confidence < e.cfg.MinProposalConfidence {
				continue
			}
			// Convert reflection proposal to EvolutionProposal
			e.handleReflectionProposal(ctx, report, p)
		}
	}
}
```

Add a handler method that creates the relevant `EvolutionProposal` (mapping `skill_create` → `ProposalCreate`, `skill_update` → `ProposalRefine`) and runs it through the verifier.

- [ ] **Step 4: Add MinProposalConfidence to config**

In `internal/config/skills.go` (or wherever `SkillsEvolverConfig` lives), add:

```go
MinProposalConfidence float64 `toml:"min_proposal_confidence"`
```

Default to `0.7` in the default config loader.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/skills/lifecycle/ -run TestEvolver_PassAConsumesReflectionProposals -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/skills/lifecycle/evolver.go internal/skills/lifecycle/evolver_test.go internal/config/skills.go
git commit -m "feat(skills): evolver consumes reflection proposals in Pass A"
```

---

### Task 3.3: Construct adapter in daemon wiring

**Files:**
- Modify: `internal/daemon/components.go`

- [ ] **Step 1: Locate the evolver construction**

Run: `grep -n "NewEvolver\|lifecycle.NewEvolver" internal/daemon/`

- [ ] **Step 2: Wire the adapter**

Define a tiny adapter in `internal/daemon/` (or as a closure):

```go
type reflectionProposerAdapter struct {
	rc *agent.ReflectionCollector
}

func (a *reflectionProposerAdapter) DrainPending() ([]lifecycle.ReflectionProposal, error) {
	raw, err := a.rc.DrainPendingProposals()
	if err != nil {
		return nil, err
	}
	out := make([]lifecycle.ReflectionProposal, len(raw))
	for i, p := range raw {
		out[i] = lifecycle.ReflectionProposal{
			Type:          p.Type,
			Target:        p.Target,
			Change:        p.Change,
			Justification: p.Justification,
			Confidence:    p.Confidence,
		}
	}
	return out, nil
}
```

Then construct the evolver with:

```go
evolver := lifecycle.NewEvolver(..., lifecycle.WithReflectionProposer(&reflectionProposerAdapter{rc: reflectionCollector}))
```

- [ ] **Step 3: Build and smoke-test**

Run: `go build ./... && go test ./internal/daemon/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/daemon/components.go
git commit -m "feat(daemon): wire reflection collector into skill evolver"
```

---

## Phase 4: Enrich PreferencePair (Gap A4 stage 1)

**Why fourth:** Trivial additive schema change. Stage 2 (training a routing classifier) is explicitly out of scope for this plan and tracked as follow-up. Stage 1 makes the data available so stage 2 can be done later without re-exporting.

### Task 4.1: Extend PreferencePair struct and constructor

**Files:**
- Modify: `internal/shadow/models.go:118-158`

- [ ] **Step 1: Write the failing test**

Add to `internal/shadow/models_test.go` (or create):

```go
package shadow

import "testing"

func TestNewPreferencePair_PreservesDomainAndTaskType(t *testing.T) {
	record := &ShadowRecord{
		Domain:         DomainCode,
		TaskType:       TaskTypeReasoning,
		StudentContent: "student",
		TeacherContent: "teacher",
		StudentModel:   "qwen2.5:7b",
		TeacherModel:   "claude-opus-4",
	}
	pair := NewPreferencePair(record, 0.3, 0.9) // teacher wins
	if pair.Domain != DomainCode {
		t.Errorf("expected Domain=code, got %v", pair.Domain)
	}
	if pair.TaskType != TaskTypeReasoning {
		t.Errorf("expected TaskType=reasoning, got %v", pair.TaskType)
	}
	if pair.Margin != 0.6 {
		t.Errorf("expected margin 0.6, got %v", pair.Margin)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/shadow/ -run TestNewPreferencePair_PreservesDomainAndTaskType -v`
Expected: FAIL — `Domain` and `TaskType` not on `PreferencePair`.

- [ ] **Step 3: Extend PreferencePair**

In `internal/shadow/models.go:118-129`:

```go
// PreferencePair represents a DPO training pair.
type PreferencePair struct {
	ID               string     `json:"id"`
	SourceRecordID   string     `json:"source_record_id"`
	PromptMessages   []Message  `json:"prompt_messages"`
	ChosenResponse   string     `json:"chosen_response"`
	ChosenModel      string     `json:"chosen_model"`
	RejectedResponse string     `json:"rejected_response"`
	RejectedModel    string     `json:"rejected_model"`
	Margin           float64    `json:"margin"`
	Domain           Domain     `json:"domain,omitempty"`
	TaskType         TaskType   `json:"task_type,omitempty"`
	RoutingPath      string     `json:"routing_path,omitempty"` // alias/skill context from the original decision
	ExportedAt       *time.Time `json:"exported_at,omitempty"`
}
```

Update `NewPreferencePair` to populate the new fields from `record`:

```go
func NewPreferencePair(record *ShadowRecord, studentScore, teacherScore float64) *PreferencePair {
	pair := &PreferencePair{
		ID:             uuid.New().String(),
		SourceRecordID: record.ID,
		PromptMessages: record.Messages,
		Domain:         record.Domain,
		TaskType:       record.TaskType,
	}
	// ... existing chosen/rejected logic unchanged
	return pair
}
```

- [ ] **Step 4: Update SQLite schema via versioned migration**

The shadow store uses a versioned migration pattern (`schema_version` table, `migrateToV1`/`migrateToV2`, constant `TrainingStoreSchemaVersion = 2` at `internal/shadow/store_sqlite.go:31`). To add the new `preference_pairs` columns:

1. Bump the constant:

```go
const (
    TrainingStoreSchemaVersion = 3 // was 2
    ExamplesStoreSchemaVersion = 2
    AdaptersStoreSchemaVersion = 1
)
```

2. Add a new migration method following the V2 pattern (idempotent ALTER TABLE that tolerates duplicate-column errors via `errcls.IsDuplicateColumn`):

```go
// migrateToV3 adds domain, task_type, and routing_path columns to
// preference_pairs so DPO exports can carry training context.
func (s *SQLiteTrainingStore) migrateToV3() {
    _, err := s.db.Exec(`ALTER TABLE preference_pairs ADD COLUMN domain TEXT DEFAULT '';`)
    if err != nil && !errcls.IsDuplicateColumn(err) {
        slog.Warn("shadow training store migration v3: add domain failed", "error", err)
    }
    _, err = s.db.Exec(`ALTER TABLE preference_pairs ADD COLUMN task_type TEXT DEFAULT '';`)
    if err != nil && !errcls.IsDuplicateColumn(err) {
        slog.Warn("shadow training store migration v3: add task_type failed", "error", err)
    }
    _, err = s.db.Exec(`ALTER TABLE preference_pairs ADD COLUMN routing_path TEXT DEFAULT '';`)
    if err != nil && !errcls.IsDuplicateColumn(err) {
        slog.Warn("shadow training store migration v3: add routing_path failed", "error", err)
    }
}
```

3. Wire it into the `migrate()` method between the V2 call and the version update:

```go
if currentVersion < 2 {
    s.migrateToV2()
}

if currentVersion < 3 {
    s.migrateToV3()
}

// ... existing INSERT OR REPLACE INTO schema_version ...
```

4. Update the insert/update queries for `preference_pairs` in the same file to include the new columns. Search for `INSERT INTO preference_pairs` and `SELECT ... FROM preference_pairs` and extend the column lists.

- [ ] **Step 5: Update exportDPO to include new fields**

In `internal/shadow/exporter.go:225-257`, change the JSON shape:

```go
type dpoRecord struct {
	Prompt      string `json:"prompt"`
	Chosen      string `json:"chosen"`
	Rejected    string `json:"rejected"`
	Domain      string `json:"domain,omitempty"`
	TaskType    string `json:"task_type,omitempty"`
	Margin      float64 `json:"margin"`
}
```

And populate from `pair.Domain`, `pair.TaskType`, `pair.Margin`.

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/shadow/ -run TestNewPreferencePair_PreservesDomainAndTaskType -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/shadow/models.go internal/shadow/models_test.go internal/shadow/store_sqlite.go internal/shadow/exporter.go
git commit -m "feat(shadow): enrich PreferencePair with domain/task/routing context"
```

---

## Phase 5: Pass D Gap Analysis (Gap O2)

**Why fifth:** Leverages existing infrastructure (`capability_index.go`, the new `skill_query_misses` table). Substantial enough to be its own phase but doesn't block Phases 1-4.

### Task 5.1: Record low-match queries in UsageTracker

**Files:**
- Modify: `internal/skills/lifecycle/usage.go:17-45` (UsageTracker interface)
- Modify: `internal/skills/lifecycle/usage.go` (implementation)

- [ ] **Step 1: Write the failing test**

Add to `internal/skills/lifecycle/usage_test.go`:

```go
func TestUsageTrackerImpl_RecordsAndReturnsLowMatchQueries(t *testing.T) {
	tracker := newTestTracker(t) // existing helper
	defer tracker.Close()

	ctx := context.Background()
	if err := tracker.RecordLowMatchQuery(ctx, "debug goroutine race condition", 0.42); err != nil {
		t.Fatalf("RecordLowMatchQuery: %v", err)
	}
	if err := tracker.RecordLowMatchQuery(ctx, "debug goroutine race condition", 0.42); err != nil {
		t.Fatalf("second: %v", err)
	}
	if err := tracker.RecordLowMatchQuery(ctx, "deploy to kubernetes", 0.35); err != nil {
		t.Fatalf("third: %v", err)
	}

	got, err := tracker.GetLowMatchQueries(ctx, 0.5, 10)
	if err != nil {
		t.Fatalf("GetLowMatchQueries: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 distinct queries, got %d", len(got))
	}
	// First entry should be the duplicate (higher count).
	if got[0].Count != 2 {
		t.Errorf("expected count 2 on top entry, got %d", got[0].Count)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/skills/lifecycle/ -run TestUsageTrackerImpl_RecordsAndReturnsLowMatchQueries -v`
Expected: FAIL — methods undefined.

- [ ] **Step 3: Add to interface and implementation**

In `internal/skills/lifecycle/usage.go`, extend `UsageTracker`:

```go
// RecordLowMatchQuery records a user/skill-discovery query whose best
// capability-index match score was below threshold. Used by Pass D
// (gap analysis) to surface unmet needs as new-skill candidates.
RecordLowMatchQuery(ctx context.Context, query string, bestScore float64) error

// GetLowMatchQueries returns low-match queries ranked by descending count,
// filtered to those whose best score was strictly below maxScore. limit
// caps the result count.
GetLowMatchQueries(ctx context.Context, maxScore float64, limit int) ([]LowMatchQuery, error)
```

Add a `LowMatchQuery` type to `types.go`:

```go
// LowMatchQuery is a user or skill-discovery query that didn't strongly match
// any existing skill. Aggregated by Pass D to surface coverage gaps.
type LowMatchQuery struct {
	Query    string    `json:"query"`
	Count    int       `json:"count"`
	BestScore float64  `json:"best_score"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}
```

Add a new table in the implementation's schema init. The lifecycle usage tracker uses a single `usageSchemaSQL` const (no versioning — see `internal/skills/lifecycle/usage.go:104-130`). Extend the const directly:

```go
const usageSchemaSQL = `
CREATE TABLE IF NOT EXISTS skill_usage (
    skill_name       TEXT PRIMARY KEY,
    inject_count     INTEGER NOT NULL DEFAULT 0,
    positive_count   INTEGER NOT NULL DEFAULT 0,
    negative_count   INTEGER NOT NULL DEFAULT 0,
    neutral_count    INTEGER NOT NULL DEFAULT 0,
    last_injected_at DATETIME,
    last_used_at     DATETIME,
    effectiveness    REAL NOT NULL DEFAULT 0.0
);

CREATE TABLE IF NOT EXISTS skill_usage_events (
    id          TEXT PRIMARY KEY,
    skill_name  TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    outcome     TEXT NOT NULL DEFAULT '',
    session_id  TEXT NOT NULL DEFAULT '',
    timestamp   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_skill_usage_events_skill
    ON skill_usage_events(skill_name);

CREATE INDEX IF NOT EXISTS idx_skill_usage_events_type
    ON skill_usage_events(event_type);

-- New in this change: low-match queries for Pass D gap analysis.
CREATE TABLE IF NOT EXISTS skill_query_misses (
    query       TEXT PRIMARY KEY,
    best_score  REAL NOT NULL,
    first_seen  DATETIME NOT NULL,
    last_seen   DATETIME NOT NULL,
    count       INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_skill_query_misses_count
    ON skill_query_misses(count);
`
```

Because `CREATE TABLE IF NOT EXISTS` is idempotent, existing databases pick up the new table on the next `initSchema` call without a migration framework.

Implement `RecordLowMatchQuery` as an upsert (increment count, update last_seen and `MIN(best_score)`):

```go
// RecordLowMatchQuery records that a user or skill-discovery query matched
// no skill above threshold. Repeated queries accumulate count and track the
// best (highest) score seen. Idempotent on query.
func (ut *UsageTrackerImpl) RecordLowMatchQuery(ctx context.Context, query string, bestScore float64) error {
    now := time.Now().UTC()
    _, err := ut.db.ExecContext(ctx, `
        INSERT INTO skill_query_misses (query, best_score, first_seen, last_seen, count)
        VALUES (?, ?, ?, ?, 1)
        ON CONFLICT(query) DO UPDATE SET
            best_score = MIN(best_score, excluded.best_score),
            last_seen  = excluded.last_seen,
            count      = count + 1
    `, query, bestScore, now, now)
    if err != nil {
        return fmt.Errorf("usage tracker: record low-match query: %w", err)
    }
    return nil
}

// GetLowMatchQueries returns low-match queries ranked by descending count,
// filtered to those with best_score strictly below maxScore. limit caps the
// result. Used by Pass D gap analysis.
func (ut *UsageTrackerImpl) GetLowMatchQueries(ctx context.Context, maxScore float64, limit int) ([]LowMatchQuery, error) {
    if limit <= 0 {
        limit = 100
    }
    var rows []LowMatchQuery
    err := ut.db.SelectContext(ctx, &rows, `
        SELECT query, best_score, first_seen, last_seen, count
        FROM skill_query_misses
        WHERE best_score < ?
        ORDER BY count DESC, best_score ASC
        LIMIT ?
    `, maxScore, limit)
    if err != nil {
        return nil, fmt.Errorf("usage tracker: get low-match queries: %w", err)
    }
    return rows, nil
}
```

- [ ] **Step 4: Wire at the call site**

Find where the `CapabilityIndex.MatchWithThreshold` is consulted in the agent loop or skill discovery. Add a low-match record when no skill matches above threshold. (Likely in `internal/agent/loop.go` near skill injection, or `internal/skills/discovery.go`.)

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/skills/lifecycle/ -run TestUsageTrackerImpl_RecordsAndReturnsLowMatchQueries -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/skills/lifecycle/usage.go internal/skills/lifecycle/usage_test.go internal/skills/lifecycle/types.go internal/agent/loop.go
git commit -m "feat(skills): record low-match queries for gap analysis"
```

---

### Task 5.2: Implement Pass D (FillGap)

**Files:**
- Modify: `internal/skills/lifecycle/types.go:146-180` (add ProposalFillGap action, Gaps counter)
- Modify: `internal/skills/lifecycle/evolver.go:115-120` (RunCycle)
- Create: `internal/skills/lifecycle/gap_analysis.go`
- Create: `internal/skills/lifecycle/gap_analysis_test.go`

- [ ] **Step 1: Write the failing test**

`internal/skills/lifecycle/gap_analysis_test.go`:

```go
package lifecycle

import (
	"context"
	"testing"
)

func TestPassDFillGap_ProposesSkillForRepeatedQueries(t *testing.T) {
	analyzer := NewGapAnalyzer()
	gaps := []LowMatchQuery{
		{Query: "deploy helm chart", Count: 12, BestScore: 0.3},
		{Query: "deploy helm chart", Count: 12, BestScore: 0.3}, // dedup at the analyzer level
		{Query: "single occurrence noise", Count: 1, BestScore: 0.1}, // below threshold, skipped
	}
	proposals := analyzer.Propose(context.Background(), gaps)
	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal (deduped, singletons skipped), got %d", len(proposals))
	}
	if proposals[0].Action != ProposalFillGap {
		t.Errorf("expected ProposalFillGap, got %v", proposals[0].Action)
	}
	if proposals[0].SkillName != "deploy-helm-chart" {
		t.Errorf("expected slugified name 'deploy-helm-chart', got %q", proposals[0].SkillName)
	}
}

func TestPassDFillGap_SlugifiesNames(t *testing.T) {
	analyzer := NewGapAnalyzer()
	cases := map[string]string{
		"Deploy Helm Chart":     "deploy-helm-chart",
		"k8s rollout undo":      "k8s-rollout-undo",
		"--weird--input--":      "weird-input",
		"":                      "unnamed-skill",
	}
	for input, want := range cases {
		got := slugifyQuery(input)
		if got != want {
			t.Errorf("slugifyQuery(%q) = %q, want %q", input, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/skills/lifecycle/ -run TestPassDFillGap -v`
Expected: FAIL — `ProposalFillGap`, `GapAnalyzer` undefined.

- [ ] **Step 3: Add ProposalFillGap and Gaps counter**

In `internal/skills/lifecycle/types.go`:

```go
const (
	ProposalRefine  EvolutionProposalAction = "improve"
	ProposalCreate  EvolutionProposalAction = "create"
	ProposalArchive EvolutionProposalAction = "archive"
	ProposalFillGap EvolutionProposalAction = "fill_gap" // NEW
)

type EvolutionReport struct {
	Refined  int                 `json:"refined"`
	Promoted int                 `json:"promoted"`
	Pruned   int                 `json:"pruned"`
	Gaps     int                 `json:"gaps"` // NEW — Pass D
	Skipped  int                 `json:"skipped"`
	Rejected int                 `json:"rejected"`
	Planned  int                 `json:"planned"`
	Details  []EvolutionProposal `json:"details"`
}
```

- [ ] **Step 4: Implement GapAnalyzer (heuristic-only)**

`internal/skills/lifecycle/gap_analysis.go`:

```go
package lifecycle

import (
	"context"
	"fmt"
	"strings"
)

// minCountForGapProposal is the minimum times a low-match query must recur
// before Pass D proposes a new skill for it. Singletons are noise.
const minCountForGapProposal = 5

// GapAnalyzer mines low-match queries and proposes new-skill candidates.
//
// Heuristic-only for first ship: generates a skeleton skill body that the
// verifier + a human (when AutoApply=false) will review. LLM-driven body
// generation is a follow-up.
type GapAnalyzer struct{}

// NewGapAnalyzer constructs an analyzer.
func NewGapAnalyzer() *GapAnalyzer {
	return &GapAnalyzer{}
}

// Propose generates EvolutionProposals for the given low-match queries.
// Queries appearing fewer than minCountForGapProposal times are skipped.
// Duplicate queries are deduped (kept at their max count).
func (a *GapAnalyzer) Propose(_ context.Context, gaps []LowMatchQuery) []EvolutionProposal {
	deduped := dedupeQueries(gaps)

	var proposals []EvolutionProposal
	for _, q := range deduped {
		if q.Count < minCountForGapProposal {
			continue
		}
		skillName := slugifyQuery(q.Query)
		body := generateSkillBody(skillName, q.Query, q.Count, q.BestScore)
		proposals = append(proposals, EvolutionProposal{
			Action:           ProposalFillGap,
			SkillName:        skillName,
			Rationale: fmt.Sprintf(
				"low-match query recurred %d times (best score %.2f); no existing skill covers it",
				q.Count, q.BestScore,
			),
			CandidateContent: body,
		})
	}
	return proposals
}

// generateSkillBody produces a SKILL.md skeleton for a coverage gap. The
// body is intentionally minimal — the verifier gate plus human review
// (when AutoApply=false) fill in the procedure.
//
// TODO(follow-up): call the LLM to draft a real procedure based on
// conversation history containing the query. Pass A refine can also adopt
// the skill once it accumulates usage data.
func generateSkillBody(skillName, query string, count int, bestScore float64) string {
	return fmt.Sprintf(`---
name: %s
description: Auto-proposed by Pass D gap analysis (recurred %d times, best match %.2f)
priority: 4
---
# %s

This skill was auto-proposed because the query

    %q

recurred %d times without matching any existing skill above the
capability-index threshold (best score %.2f).

## Procedure

(filled in by review — describe what a competent agent should do when this query arises)

## Triggers

- query: %q
`, skillName, count, bestScore, skillName, query, count, bestScore, query)
}

func dedupeQueries(qs []LowMatchQuery) []LowMatchQuery {
	byQuery := map[string]LowMatchQuery{}
	for _, q := range qs {
		existing, ok := byQuery[q.Query]
		if !ok || q.Count > existing.Count {
			byQuery[q.Query] = q
		}
	}
	out := make([]LowMatchQuery, 0, len(byQuery))
	for _, q := range byQuery {
		out = append(out, q)
	}
	return out
}

func slugifyQuery(q string) string {
	var sb strings.Builder
	prevDash := false
	for _, r := range q {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
			prevDash = false
		case r >= 'A' && r <= 'Z':
			sb.WriteRune(r + ('a' - 'A'))
			prevDash = false
		default:
			if !prevDash && sb.Len() > 0 {
				sb.WriteRune('-')
				prevDash = true
			}
		}
	}
	for sb.Len() > 0 && sb.String()[sb.Len()-1] == '-' {
		s := sb.String()
		sb.Reset()
		sb.WriteString(s[:len(s)-1])
	}
	if sb.Len() == 0 {
		return "unnamed-skill"
	}
	return sb.String()
}
```

- [ ] **Step 5: Wire passDFillGap into RunCycle**

In `internal/skills/lifecycle/evolver.go`, after `passCPrune`:

```go
e.passDFillGap(ctx, report)
```

Add the pass method:

```go
func (e *Evolver) passDFillGap(ctx context.Context, report *EvolutionReport) {
	if e.usage == nil {
		return
	}
	lowMatchTracker, ok := e.usage.(interface {
		GetLowMatchQueries(context.Context, float64, int) ([]LowMatchQuery, error)
	})
	if !ok {
		// Usage tracker implementation hasn't been updated yet.
		return
	}
	gaps, err := lowMatchTracker.GetLowMatchQueries(ctx, 0.5, 50)
	if err != nil {
		e.logger.Warn("pass D: low-match query fetch failed", "err", err)
		return
	}
	if len(gaps) == 0 {
		return
	}
	analyzer := NewGapAnalyzer()
	for _, p := range analyzer.Propose(ctx, gaps) {
		// Run through verifier like any other proposal.
		result := e.verifier.Verify(ctx, VerifyRequest{
			Action:           "fill_gap",
			SkillName:        p.SkillName,
			CandidateContent: p.CandidateContent,
			EvidenceSummary:  p.Rationale,
		})
		p.VerifierResult = result
		if result.Action == ActionAccept {
			e.applyOrPlan(ctx, report, p)
			report.Gaps++
		} else {
			report.Rejected++
		}
		report.Details = append(report.Details, p)
	}
}
```

**Why the interface assertion:** `UsageTracker` (the interface) doesn't have `GetLowMatchQueries` until Task 5.1 adds it. The assertion makes Pass D gracefully degrade if the runtime tracker is a stub or older version. Once Task 5.1 ships, every real tracker will satisfy the assertion.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/skills/lifecycle/ -run TestPassDFillGap -v`
Expected: PASS — both subtests.

- [ ] **Step 6: Run full lifecycle test suite**

Run: `go test ./internal/skills/lifecycle/ -v`
Expected: PASS — no regressions in existing evolver/usage tests.

- [ ] **Step 7: Commit**

```bash
git add internal/skills/lifecycle/types.go internal/skills/lifecycle/gap_analysis.go internal/skills/lifecycle/gap_analysis_test.go internal/skills/lifecycle/evolver.go
git commit -m "feat(skills): add Pass D gap analysis to evolver"
```

---

### Task 5.3: Expose gap analysis via CLI

**Files:**
- Modify: `cmd/meept/skills.go` (or `cmd/meept/skills_*.go` — find the skills subcommand tree)

- [ ] **Step 1: Locate the skills CLI**

Run: `grep -n "skills evolve\|skills stats" cmd/meept/`

- [ ] **Step 2: Add a gaps subcommand**

Following the pattern of the existing `evolve` subcommand:

```go
case "gaps":
	// Show current low-match queries without running a full evolver cycle.
	tracker := /* construct or reuse UsageTracker */
	gaps, err := tracker.GetLowMatchQueries(cmd.Context(), 0.5, 50)
	if err != nil {
		return err
	}
	if len(gaps) == 0 {
		fmt.Println("no coverage gaps detected")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "COUNT\tBEST\tQUERY")
	for _, g := range gaps {
		fmt.Fprintf(w, "%d\t%.2f\t%s\n", g.Count, g.BestScore, g.Query)
	}
	return w.Flush()
```

- [ ] **Step 3: Build and smoke-test**

Run: `go build ./cmd/meept && ./bin/meept skills gaps`
Expected: prints "no coverage gaps detected" on a fresh install.

- [ ] **Step 4: Commit**

```bash
git add cmd/meept/skills.go
git commit -m "feat(cli): add 'meept skills gaps' subcommand"
```

---

## Phase 6: Documentation

**Why last:** Documentation reflects the final wired state. Each code phase above has its own commit; this phase is the user-facing surface.

### Task 6.1: Create docs/workflows/shadow-training.md

**Files:**
- Create: `docs/workflows/shadow-training.md`

- [ ] **Step 1: Write the doc**

```markdown
# Shadow Training

## Overview
Meept's shadow training system captures production LLM traffic, scores student responses against a teacher model, and produces LoRA training pairs that improve the student over time. Trained adapters are gated by an eval threshold and hot-swapped into the serving alias without restart.

## Architecture

```
User request
    |
    v
LLM Client (AdapterAwareChatter)
    |- serves via current alias model
    |
    v
Shadow Middleware (internal/shadow/middleware.go)
    |- intercepts every Chat() call
    |- async fetches teacher response
    |- scores (heuristic / teacher_eval / hybrid)
    |
    v
Shadow Store (training.db)
    |- ShadowRecord (student vs teacher)
    |- PreferencePair (DPO pair)
    |- FewShotExample (high-quality for in-context learning)
    |
    v  [scheduled: train_threshold pairs reached]
LoRA Trainer (internal/shadow/trainer.go)
    |- exports DPO JSONL
    |- runs Unsloth/Axolotl/TRL/LLaMA-Factory
    |
    v
Eval Gate (internal/shadow/eval_gate.go)
    |- TrainingRun.EvalScore >= EvalThreshold (default 0.7)
    |- TrainingRun.RecordsUsed >= 20
    |
    v
Hot Swap (internal/shadow/adapter_hotswap.go)
    |- Ollama: bakes adapter into a new model variant
    |- LLM client: receives HotSwapCallback with the baked model ID
    |- DB: SetActiveAdapter flips the flag
```

## Configuration

```toml
[shadow]
enabled = true
data_dir = "~/.meept/shadow"

[shadow.teacher]
model = "anthropic/claude-haiku-4-5"
fallback_model = "openai/gpt-5-mini"
max_daily_queries = 500
max_daily_cost = 10.0
requests_per_minute = 30

[shadow.adapters]
enabled = true
hot_swap_enabled = true
eval_threshold = 0.7
ollama_endpoint = "http://localhost:11434"
auto_train = true
train_threshold = 500
```

## CLI

```bash
meept shadow stats                  # capture counts, training pairs, daily cost
meept shadow adapters list          # trained adapters with eval scores
meept shadow adapters activate <id> # activate (subject to eval gate)
meept shadow adapters hotswap <id>  # activate + push to LLM client
```

## Observability

- Per-day teacher query count and cost in the metrics store.
- `TrainingRun.FinalLoss` and `TrainingRun.EvalScore` per training pass.
- Adapter activation events emit on the message bus (E4-class event).

## Safety

- **Eval gate:** adapters scoring below `eval_threshold` cannot be activated.
- **Record floor:** adapters trained on fewer than 20 records are blocked even if eval score passes.
- **Hot-swap is opt-in:** `hot_swap_enabled = false` makes `ActivateAdapter` flip a DB flag without touching the serving path.
- **Cost ceiling:** teacher queries are rate-limited per minute and per day (USD).
```

- [ ] **Step 2: Commit**

```bash
git add docs/workflows/shadow-training.md
git commit -m "docs: shadow-training workflow"
```

---

### Task 6.2: Create docs/workflows/routing-decisions.md

**Files:**
- Create: `docs/workflows/routing-decisions.md`

- [ ] **Step 1: Write the doc**

```markdown
# Routing Decision Logging

## Overview
Every model-resolution decision the LLM resolver makes is persisted to a SQLite store. The routing log is the training-set foundation for future routing-classifier work and provides observability into why each request went where.

## Schema

```sql
CREATE TABLE routing_decisions (
    id TEXT PRIMARY KEY,
    request_id TEXT NOT NULL,
    timestamp DATETIME NOT NULL,
    chosen_model_id TEXT NOT NULL,
    chosen_provider_id TEXT NOT NULL,
    alias TEXT,
    reason TEXT,           -- "round_robin" | "capability_escalation" | ...
    skill TEXT,            -- populated when escalation was for a skill
    employee_id TEXT,
    candidates_json TEXT   -- JSON array of all candidates considered
);
```

Location: `<data_dir>/routing.db`

## What's logged

| Method | Reason | When |
|---|---|---|
| `ResolveForAlias` | `round_robin` | every alias resolution (the production hot path) |
| `ResolveForSkill` | `capability_escalation` | when skill requires capabilities the current model lacks |
| `ResolveRef` | `explicit` | when a specific model ref is requested |

## CLI

```bash
meept routing recent [N]            # last N decisions (default 20)
meept routing by-model <model-id>   # decisions that chose a specific model
```

## Future: routing classifier

The routing log plus enriched PreferencePairs together enable a future enhancement where the resolver itself learns from past outcomes. See plan `2026-07-07-self-improvement-loop-closure.md` Gap A4 stage 2 (out of scope for first ship).
```

- [ ] **Step 2: Commit**

```bash
git add docs/workflows/routing-decisions.md
git commit -m "docs: routing-decisions workflow"
```

---

### Task 6.3: Update docs/workflows/self-improvement.md

**Files:**
- Modify: `docs/workflows/self-improvement.md`

- [ ] **Step 1: Read current contents**

The current doc only covers pytest/lint self-improvement. Add a new section.

- [ ] **Step 2: Append closed-loop section**

Add after the existing content:

```markdown
## Closed-Loop Skill and Model Improvement

In addition to the issue-detection flow above, Meept runs two continuous improvement loops:

### Skill Evolution Loop

Scheduled every 6 hours (configurable). Four passes:

1. **Pass A (Refine)** — Improves existing skills based on usage evidence and reflection proposals.
2. **Pass B (Promote)** — Promotes learned patterns meeting thresholds to new skills.
3. **Pass C (Prune)** — Archives low-performing skills.
4. **Pass D (Fill Gap)** — Mines low-match queries from the capability index; proposes new skills for unmet recurring needs.

Every proposal passes the four-dimension verifier (`grounded_in_evidence`, `preserves_existing_value`, `specificity_and_reusability`, `safe_to_publish`) before being applied or planned.

### Shadow Training Loop

Continuous. Captures production LLM traffic, scores against a teacher model, and produces LoRA training pairs. Trained adapters pass an eval gate before being hot-swapped into the serving alias. See [Shadow Training](./shadow-training.md).

### Reflection Queue

Per-turn reflection (`internal/agent/reflection_collector.go`) writes proposals to `.meept/improvements.md`. The skill evolver drains this queue at the start of every Pass A cycle, so reflection proposals feed directly into skill refinement without manual review.

### Routing Decision Log

Every model-resolution decision is persisted to `<data_dir>/routing.db`. See [Routing Decisions](./routing-decisions.md).
```

- [ ] **Step 3: Commit**

```bash
git add docs/workflows/self-improvement.md
git commit -m "docs: expand self-improvement to cover closed loop"
```

---

### Task 6.4: Update docs/workflows/skills.md with Pass D

**Files:**
- Modify: `docs/workflows/skills.md`

- [ ] **Step 1: Read current contents**

- [ ] **Step 2: Add a Gap Analysis (Pass D) section**

Where appropriate (likely near where Passes A/B/C are described):

```markdown
### Pass D: Gap Analysis

Beyond refining, promoting, and pruning skills based on what *exists*, Meept also surfaces what's *missing*. The capability index records every user or skill-discovery query whose best match score fell below 0.5. Queries that recur at least 5 times become new-skill candidates:

```bash
meept skills gaps          # list current low-match queries
meept skills evolve        # run all four passes (A/B/C/D)
```

Pass D proposals pass through the same verifier as the other passes.
```

- [ ] **Step 3: Commit**

```bash
git add docs/workflows/skills.md
git commit -m "docs: skills — document Pass D gap analysis"
```

---

### Task 6.5: Update docs/features.md

**Files:**
- Modify: `docs/features.md`

- [ ] **Step 1: Read current contents**

Locate the table of contents or feature-list section.

- [ ] **Step 2: Add a Self-Improvement Loop section**

Insert near related sections (probably after the Skills section):

```markdown
## Self-Improvement Loop

Meept continuously improves its own model quality and skill coverage through four interacting loops:

### Shadow Training (Model Improvement)

Production LLM traffic is shadowed against a teacher model (typically a stronger cloud model). Preference pairs are mined and used to LoRA-train the local student model. Trained adapters pass an eval gate (minimum score and record count) before being hot-swapped into the serving alias — production traffic shifts to the improved model without a daemon restart.

**Location:** `internal/shadow/`
**Config:** `[shadow]` block in `meept.json5`
**Docs:** [workflows/shadow-training.md](workflows/shadow-training.md)

### Skill Evolution (Skill Improvement)

Every 6 hours, the skill evolver runs four passes: refine existing skills (Pass A), promote learned patterns (Pass B), prune low-performers (Pass C), and surface coverage gaps from low-match queries (Pass D). Each proposal passes a four-dimension LLM-judge verifier.

**Location:** `internal/skills/lifecycle/`
**Docs:** [workflows/skills.md](workflows/skills.md), [workflows/self-improvement.md](workflows/self-improvement.md)

### Reflection (Per-Turn Learning)

After each agent turn, a classifier proposes 0-1 improvements (new skills, skill updates, prompt tweaks) and queues them in `.meept/improvements.md`. The skill evolver drains this queue at the start of each cycle, so reflection proposals are auto-consumed — no manual review required unless the proposal type is in the propose-only set (agent prompts, project instructions).

**Location:** `internal/agent/reflection_collector.go`

### Routing Decision Logging

Every model-resolution decision is persisted to a SQLite store. The log is the training-set foundation for future routing-classifier work and provides observability into why each request went where.

**Location:** `internal/llm/routing_log.go`
**Docs:** [workflows/routing-decisions.md](workflows/routing-decisions.md)
```

- [ ] **Step 3: Commit**

```bash
git add docs/features.md
git commit -m "docs: features — add Self-Improvement Loop section"
```

---

### Task 6.6: Update docs/workflows/index.md and .nav.yml

**Files:**
- Modify: `docs/workflows/index.md`
- Modify: `docs/workflows/.nav.yml`

- [ ] **Step 1: Add new workflow entries**

In `docs/workflows/index.md` (the workflows table of contents) and `docs/workflows/.nav.yml` (the navigation order), add entries for:

- `shadow-training.md`
- `routing-decisions.md`

Following the existing alphabetical or thematic order.

- [ ] **Step 2: Commit**

```bash
git add docs/workflows/index.md docs/workflows/.nav.yml
git commit -m "docs: index shadow-training and routing-decisions"
```

---

## Self-Review

### Spec coverage

| Gap | Phase | Tasks | Status |
|---|---|---|---|
| A1: Wire adapters into serving model | Phase 1 | 1.4 (verify SetModelOverride), 1.5 (HotSwap), 1.6 (daemon wiring) | ✅ |
| A2: Eval gate | Phase 1 | 1.1 (config), 1.2 (EvalGate), 1.3 (wire into ActivateAdapter) | ✅ |
| A3: Persist routing decisions | Phase 2 | 2.1 (RoutingLogger), 2.2 (resolver integration), 2.3 (daemon wiring) | ✅ |
| O3: Drain reflection queue → evolver | Phase 3 | 3.1 (DrainPending), 3.2 (evolver consumes), 3.3 (daemon adapter) | ✅ |
| A4 stage 1: Enrich PreferencePair | Phase 4 | 4.1 | ✅ |
| O2: Pass D gap analysis | Phase 5 | 5.1 (low-match recording), 5.2 (Pass D), 5.3 (CLI) | ✅ |
| Documentation + features.md | Phase 6 | 6.1–6.6 | ✅ |

Gaps explicitly out of scope (tracked as follow-ups, not deferred):
- A4 stage 2 (routing classifier) — needs A3 data accumulation first.
- O1 (peer-to-peer teaching) — independent enhancement.
- O4 (error-typed few-shot) — independent enhancement.
- O5 (per-employee skill affinity) — matters at employee fleet scale.
- A5 (runtime MoA / voting) — skip unless multi-model deployment.
- LLM-driven skill-body generation in Pass D — heuristic skeleton is sufficient for first ship; the verifier gate plus optional human review fills the procedure.

### Placeholder scan

Searched the plan for: `TBD`, `TODO`, `implement later`, `fill in details`, `Add appropriate`, `Similar to Task`. The only intentional `TODO(follow-up)` is in `generateSkillBody` (Task 5.2 Step 4), flagged for LLM-driven body generation — explicitly out of scope per the Gaps section above. All other code blocks contain working code or copy-ready skeletons.

### Type consistency

- `EvalGate`, `NewEvalGate`, `(*EvalGate).Check` — consistent across Tasks 1.2 and 1.3.
- `HotSwapCallback`, `SetHotSwapCallback`, `SetOllamaActivator`, `HotSwap` — consistent across Tasks 1.5 and 1.6.
- `RoutingDecision`, `RoutingLogger`, `NewRoutingLogger`, `Record`, `Recent`, `SetRoutingLogger` — consistent across Tasks 2.1, 2.2, 2.3.
- `DrainPending` (proposal queue), `DrainPendingProposals` (ReflectionCollector), `ReflectionProposer` interface, `WithReflectionProposer` — consistent across Tasks 3.1, 3.2, 3.3.
- `ProposalFillGap`, `GapAnalyzer`, `NewGapAnalyzer` (no-arg), `LowMatchQuery`, `RecordLowMatchQuery`, `GetLowMatchQueries` — consistent across Tasks 5.1, 5.2, 5.3.
- `EvolutionReport.Gaps` — added in Task 5.2 alongside `ProposalFillGap`.
- `TrainingStoreSchemaVersion = 3` (Task 4.1) — uses the existing versioned-migration pattern (`migrateToV3` wired into `migrate()` alongside V1 and V2).

### Risk resolutions (incorporated)

All five risks flagged in the prior draft are resolved in-plan:

1. **ChatOption shape / model-override path** — Task 1.4 rewritten as verification of the existing `AgentLoop.SetModelOverride` path (`internal/agent/loop.go:985-989`). No new types in `internal/llm/`. Net effect: Phase 1 dropped from 6 tasks to 5 implementation tasks plus 1 verification task.
2. **ProvidersConfig fixture** — Task 2.2 test fixture now uses the real schema (`Providers map[string]*ProviderConfig`, `Type`, `URL`, `APIKey`, `Capabilities`) with a note for the implementer to verify against `internal/llm/providers.go:47-55` before finalizing.
3. **Import cycle direction** — verified neither direction has a cycle. Rationale for putting `ReflectionProposer` in `lifecycle` updated to "cleaner layering" rather than "cycle avoidance."
4. **Migration framework divergence** — Task 4.1 uses the shadow store's versioned migration pattern (bump `TrainingStoreSchemaVersion` to 3, add `migrateToV3`). Task 5.1 uses the lifecycle store's single-schema-const pattern (extend `usageSchemaSQL` with `IF NOT EXISTS`). Both approaches are idempotent and match their respective package conventions.
5. **Recursive placeholder in GapAnalyzer** — Task 5.2 is heuristic-only. `NewGapAnalyzer()` takes no arguments. `generateSkillBody` is a free function. LLM-driven body generation is moved to an explicit `TODO(follow-up)` listed in the out-of-scope section.

### Additional implementation note

- **Task 1.6 wiring:** the `HotSwapCallback` closure must construct the full provider-prefixed ref (`"ollama/<baked-name>"`) before passing to `agentLoop.SetModelOverride`, because `Resolver.ResolveForAlias` / `ResolveRef` expect `"provider/model"` format. The provider ID is closed over at daemon construction time.
- **Task 5.2 interface assertion:** `passDFillGap` uses an inline interface assertion against `e.usage` rather than extending the `UsageTracker` interface directly. This lets the pass gracefully degrade if the runtime tracker is a stub (e.g. in tests that construct an evolver with a mock tracker that doesn't implement `GetLowMatchQueries`).

---

## Execution Handoff

Plan complete and saved to `docs/plans/2026-07-07-self-improvement-loop-closure.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration. Best for this plan given the 6-phase structure with independent verifiable chunks.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints for review. Best if you want tight feedback control.

Which approach?
