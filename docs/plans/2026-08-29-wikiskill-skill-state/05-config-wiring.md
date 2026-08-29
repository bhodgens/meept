# Config, Daemon Wiring, RunWithSkill Hook — Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing a
> file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ./master.md
- **Scope:** Config sections (`skills.wiki`, `skills.state`), daemon
  construction of TraceStore/WikiStore + evolver/loop options, and the
  RunWithSkill state-mode hook.
- **Dependencies:** 02 (WikiStore), 03 (evolver options), 04 (runtime +
  LoopOption). Wave C.
- **Estimated Context:** ~55K
- **Concurrency Group:** C (after B)
- **Research reference:** arXiv:2608.27454 + arXiv:2608.26263. Commit message
  MUST cite both ids.

## Goal

Nothing from leaves 01-04 activates without this leaf. It:

1. Adds `[skills.wiki]` (default enabled=true — the store is inert until
   wired) and `[skills.state]` (default enabled=false) config.
2. Constructs TraceStore + WikiStore in components.go, wires
   `LearningPipeline.SetWikiStore`, passes `WithTraceProvider`/`WithWikiStore`
   to the evolver (only when the evolver is enabled), and
   `WithSkillStateRuntime` on the agent loop (only when state enabled).
3. Adds the RunWithSkill hook: `skill.State && runtime != nil` routes to
   state mode. General chat is untouched.

## Context

Key files to understand before implementing:

- internal/config/schema.go — SkillsConfig (~1810-1824), SkillsEvolverConfig
  (1829), DefaultConfig() skills block (~2440-2465). SIBLING-EDIT HAZARD:
  re-grep your additions before reporting; if a sibling removed them, re-apply.
- internal/daemon/components.go — learning pipeline construction (~866),
  agent options block (~1076-1096), evolver construction (~6407-6452).
  This file is huge and shared: make ONLY the additions below, run
  `git diff internal/daemon/components.go` before reporting, and annotate any
  foreign hunks you see (do not touch them).
- internal/agent/loop.go:2256 — RunWithSkill. This leaf owns ONLY the small
  hook region; leaf 01 owned the triggerLearning region (already merged by
  the time this leaf dispatches).
- internal/agent/loop.go:883 — WithUsageTracker option shape to copy for
  WithSkillStateRuntime.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/config/schema.go (inside SkillsConfig):
type SkillsWikiConfig struct {
    Enabled bool   `json:"enabled" toml:"enabled"` // default true
    Dir     string `json:"dir"     toml:"dir"`     // default "~/.meept/wiki"
}
type SkillsStateConfig struct {
    Enabled       bool `json:"enabled"         toml:"enabled"`         // default false
    MaxStateChars int  `json:"max_state_chars" toml:"max_state_chars"` // default 2000
}
// SkillsConfig gains: Wiki SkillsWikiConfig; State SkillsStateConfig
// (+ defaults in DefaultConfig; aligned json/toml tag columns).

// internal/daemon/components.go — new Components fields (grep the struct):
//   TraceStore  *selfimprove.TraceStore
//   WikiStore   *selfimprove.WikiStore
// plus construction + wiring per master §Contract 5.

// internal/agent — option + hook (leaf 04 may already define the option;
// this leaf defines it if absent and ALWAYS owns the hook):
func WithSkillStateRuntime(r *SkillStateRuntime) LoopOption // nil guard

// RunWithSkill, immediately BEFORE conv.SetSystemPrompt(skill.Body):
//   if skill.State && l.skillStateRuntime != nil {
//       return l.skillStateRuntime.Run(ctx, skill, input, conversationID)
//   }
```

### What This Leaf Consumes

- `selfimprove.NewTraceStore/NewWikiStore` (01/02).
- `lifecycle.WithTraceProvider/WithWikiStore` (03) — via a small adapter
  struct in the daemon package implementing TraceProvider by delegating to
  `*selfimprove.TraceStore` (the interface is satisfied structurally; the
  adapter keeps the daemon import surface explicit).
- `agent.NewSkillStateRuntime` (04).

## Tasks

### Task 1: Config sections + defaults

**Objective:** `[skills.wiki]` and `[skills.state]` with frozen defaults.

**Files:**
- Modify: `internal/config/schema.go`
- Test: `internal/config/schema_test.go` (grep for the existing skills
  defaults test; extend, don't redeclare)

**Step 1: Write failing test**

```go
func TestDefaultConfig_SkillsWikiState(t *testing.T) {
    c := DefaultConfig()
    if !c.Skills.Wiki.Enabled { t.Fatal("wiki default must be enabled") }
    if c.Skills.Wiki.Dir != "~/.meept/wiki" { t.Fatalf("dir: %q", c.Skills.Wiki.Dir) }
    if c.Skills.State.Enabled { t.Fatal("state default must be disabled") }
    if c.Skills.State.MaxStateChars != 2000 { t.Fatalf("max chars: %d", c.Skills.State.MaxStateChars) }
}
```

**Step 2: Run test to verify failure**

Run: `go test ./internal/config/ -run TestDefaultConfig_SkillsWikiState -v`
Expected: FAIL — fields undefined

**Step 3: Write minimal implementation**

Two structs + two fields on SkillsConfig + defaults in DefaultConfig()
beside the Evolver block. Aligned tag columns per repo convention. NO gendoc
markers required for these (evolver has none) — match siblings.

**Step 4: Run test to verify pass**

Run: `go test ./internal/config/ -race -count=1 -v`
Expected: PASS

### Task 2: Daemon wiring

**Objective:** Components construct + wire stores and options; nil-safe when
disabled.

**Files:**
- Modify: `internal/daemon/components.go`
- Test: `internal/daemon/components_wiki_test.go` (new; keep it unit-shaped —
  construct only the wiki/trace wiring under temp dirs, NOT full NewComponents)

**Step 1: Write failing test**

```go
func TestWikiWiring_OffByDefaultLeavesNil(t *testing.T) {
    // minimal config with skills.wiki.enabled=false ⇒ wiki fields nil,
    // no directory created.
}
func TestWikiWiring_OnCreatesStoresAndSetWikiStore(t *testing.T) {
    // enabled=true ⇒ TraceStore+WikiStore non-nil, pointed at cfg dir
    // (expand ~ via the existing homedir helper — grep components for how
    // other paths expand), and LearningPipeline.SetWikiStore received it.
    // If LearningPipeline isn't constructed in the unit fixture, assert via
    // a seam: extract the wiring into a small testable helper
    // (wireSkillKnowledgeStores(cfg, lp, logger) (*TraceStore, *WikiStore))
    // and test THAT. Prefer the helper.
}
```

**Step 2: Run test to verify failure**

Run: `go test ./internal/daemon/ -run TestWikiWiring -v`
Expected: FAIL — helper undefined

**Step 3: Write minimal implementation**

- Helper `wireSkillKnowledgeStores` (daemon pkg): builds stores when
  `cfg.Skills.Wiki.Enabled` (MkdirAll the dir), calls
  `lp.SetWikiStore(ws)` when lp non-nil, returns both. Call it near the
  learning-pipeline construction (~866). Store results on Components.
- Evolver block (~6421): when stores exist, append
  `lifecycle.WithTraceProvider(&traceStoreProvider{ts: c.TraceStore})` and
  `lifecycle.WithWikiStore(c.WikiStore)` to evolverOpts.
- Loop options (~1076): when `cfg.Skills.State.Enabled`,
  `agent.WithSkillStateRuntime(agent.NewSkillStateRuntime(c.AgentLoop, ...))`
  — CAREFUL: AgentLoop is constructed AFTER this block; construct the runtime
  right after the loop is built instead (grep the loop-construction site and
  place the option there; document placement in Deviations if it moves from
  the ~1076 block).

**Step 4: Run test to verify pass**

Run: `go test ./internal/daemon/ -run TestWikiWiring -race -count=1 -v`
Expected: PASS

### Task 3: RunWithSkill hook

**Objective:** Opt-in state mode dispatch, byte-identical default path.

**Files:**
- Modify: `internal/agent/loop.go` (RunWithSkill, ~2256) + option (if 04 did
  not add it)
- Test: `internal/agent/skill_state_hook_test.go`

**Step 1: Write failing test**

```go
func TestRunWithSkill_StateModeDispatch(t *testing.T) {
    // loop with a stub SkillStateRuntime recording the call.
    // skill with State=true ⇒ stub called, conversation prompt untouched.
    // skill with State=false ⇒ stub NOT called (normal path ran or errored
    // on missing LLM — assert stub-not-called is the discriminating check).
}
```

**Step 2: Run test to verify failure**

Run: `go test ./internal/agent/ -run TestRunWithSkill_StateModeDispatch -v`
Expected: FAIL — option/dispatch undefined

**Step 3: Write minimal implementation**

Per master §Contract 5 exact 3-line hook. Nil runtime ⇒ unchanged behavior
even when skill.State is true (log Debug once). This is the ONLY edit this
leaf makes inside RunWithSkill.

**Step 4: Run test to verify pass**

Run: `go test ./internal/agent/ -run TestRunWithSkill -race -count=1 -v`
Expected: PASS

### Task 4: Self-verification sweep

- `go build ./...`
- `go test ./internal/config/ ./internal/daemon/ ./internal/agent/ -race -count=1`
- `gofmt -l` touched files
- `git diff internal/daemon/components.go internal/config/schema.go` —
  confirm ONLY your hunks; annotate foreign hunks in your report.
- Defaults preserved: diff confirms evolver.enabled still false,
  state.enabled false, wiki enabled but inert without wiring.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing (`-race -count=1`)
- [ ] Contracts satisfied exactly (config field names, hook placement)
- [ ] Files at exact specified paths; no deviations (or documented)
- [ ] Disabled flags ⇒ byte-identical runtime behavior
- [ ] Foreign hunks in shared files annotated, untouched

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list with rationale — expected: runtime
construction placement adjacent to loop construction]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Contracts match master §Contract 5 exactly
- [ ] wiki.enabled=true alone creates NO behavioral change (store is written
      only via wired paths)
- [ ] state.enabled=false ⇒ RunWithSkill identical to HEAD
- [ ] No sibling hunks swept into the diff
- [ ] No debug artifacts; gofmt clean

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- components.go and schema.go are concurrently edited by other sessions in
  this repo. Read fresh, patch small, verify your hunks survive (re-grep
  `SkillsWikiConfig` / `wireSkillKnowledgeStores` before reporting).
- Do NOT flip any default in this leaf. Adoption is opt-in by config.
- The `traceStoreProvider` adapter is 5 lines in the daemon package — do not
  promote it to lifecycle.
