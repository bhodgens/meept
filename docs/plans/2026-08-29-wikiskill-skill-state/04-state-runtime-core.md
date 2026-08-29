# SKILL.state Runtime Core — Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing a
> file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ./master.md
- **Scope:** State-mode skill execution core (arXiv:2608.26263): per-step
  prompt = P + Σ_t + O_t, validated ΔΣ merge with null-deletion, reasoning
  discard, trace logging; plus the `state:` frontmatter flag on skills.
- **Dependencies:** 01-trace-store.md interface ONLY (agent-package mirror
  type — no cross-package import; both may be in flight).
- **Estimated Context:** ~65K
- **Concurrency Group:** B
- **Research reference:** arXiv:2608.26263 (SKILL.state) §3 runtime +
  §5.7 error taxonomy (GBNF motivation) + §7 limitations. Commit message MUST
  cite the arXiv id.

## Goal

`RunWithSkill` (internal/agent/loop.go:2256) runs skills as append-only
conversation — the exact runtime the paper replaces. For long procedural
skills, prompts grow O(T²) and stale history outvotes fresh observations.
This leaf adds `SkillStateRuntime` with an explicit Σ:

- Per step the model sees ONLY: skill body (P), current Σ JSON, latest
  observation (O_t). It returns structured JSON: an action and a state patch.
- The runtime validates the patch against a fixed schema (unknown keys
  dropped; `null` deletes a key — ⊕ null-deletion), merges, executes the
  action, and feeds the tool result back as the next observation.
- Intermediate reasoning never persists into the next prompt.
- The full step log goes to the loop's TraceWriter when wired.

This leaf does NOT touch the general chat path, RunWithSkill itself, or
compaction (leaf 05 owns the hook). Nothing activates without leaf 05's
config + flag.

## Context

Key files to understand before implementing:

- internal/agent/loop.go:2256-2335 — RunWithSkill: how skills execute today
  (system prompt = skill body, reasoningCycle loop). Read for tool-execution
  and conversation conventions; do NOT modify in this leaf.
- internal/agent/loop.go:718-738 — `lifecycleUsageTracker` + outcome types:
  pattern for narrow local interfaces avoiding cross-package imports.
- internal/skills/registry.go (or wherever `Skill` is defined — grep
  `type Skill struct`) — the metadata struct this leaf extends with `State`.
- internal/skills/parser (grep `ParseSkillText`/frontmatter parsing) — where
  YAML frontmatter fields are decoded; `State` joins them.
- internal/llm/gbnf.go + client.go:754-774 — `GBNFConstrainedEnabled()` and
  how chat options attach grammar constraints (`WithGrammar`); reuse, do not
  reinvent.
- internal/agent/pair_session_test.go — shared `contains` helper and test
  conventions in this package; grep before redeclaring helpers.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/skills — metadata addition
// SkillMetadata / Skill gain:
//   State bool `yaml:"state"`  // frontmatter `state: true`

package agent

// StateField describes one Σ key. v1 ships exactly the default coding schema
// (arXiv:2608.26263 §3.1: one schema per domain, authored once in code).
type StateField struct {
    Name string // files_touched | tests_run | errors | next_step
    Type string // "array" | "string"
    Desc string
}
func DefaultStateSchema() []StateField

type SkillStateConfig struct {
    MaxStateChars int // prompt budget for the Σ JSON block; default 2000
    MaxIterations int // step cap; default 25
}

type SkillStateRuntime struct { /* llm chatter + config + logger + traceWriter */ }
func NewSkillStateRuntime(loop *AgentLoop, cfg SkillStateConfig, logger *slog.Logger) *SkillStateRuntime

// Run executes skill in state mode (see Goal). Terminates when the model
// returns an answer action, MaxIterations is hit (returns last answer or an
// error with the step count), or ctx is cancelled.
func (r *SkillStateRuntime) Run(ctx context.Context, skill *skills.Skill, input, conversationID string) (string, error)

// Pure helpers — unit-tested directly, no LLM needed:
func validateStatePatch(patch map[string]any, schema []StateField) (clean map[string]any, dropped []string)
func mergeState(old map[string]any, patch map[string]any, schema []StateField) map[string]any
func buildStatePrompt(skillBody string, state map[string]any, observation string, maxChars int) string
```

Wire shapes (frozen):

```
Model response (strict JSON, no prose):
  {"action": {"tool": "<name>", "args": {...}}}        // execute tool
  {"action": {"answer": "<final text>"}, "state_patch": {...}}
  {"action": {"tool": "...", "args": {...}}, "state_patch": {...}}

state_patch semantics (⊕ null-deletion):
  {"files_touched": ["a.go"]}          // replace array
  {"errors": null}                     // delete key
  unknown keys → dropped (validateStatePatch reports them)
Type coercion: wrong-typed values dropped with the reason logged.
```

### What This Leaf Consumes

- `skills.Skill` (Body, Name, MaxIterations) — read-only.
- The loop's LLM client + tool executor. Reuse whatever RunWithSkill uses to
  execute a tool call (grep the loop for the executor call); if it is not
  cleanly callable, add a minimal `toolRunner` function-param on
  SkillStateRuntime injected from the loop (document in Deviations).
- `agent.TraceWriter` mirror (leaf 01) — optional injection; nil-safe.

## Tasks

### Task 1: `state:` frontmatter on Skill

**Objective:** Parse `state: true` from SKILL.md frontmatter.

**Files:**
- Modify: internal/skills metadata struct + frontmatter parser (grep first:
  `type SkillMetadata struct`, `type Skill struct`)
- Test: the existing parser test file (grep `ParseSkillText` tests) — add cases

**Step 1: Write failing test**

```go
func TestParseSkillText_StateFlag(t *testing.T) {
    s, err := ParseSkillText("---\nname: t\ndescription: d\nstate: true\n---\nbody")
    if err != nil { t.Fatal(err) }
    if !s.State { t.Fatal("state flag not parsed") }
    s2, _ := ParseSkillText("---\nname: t\ndescription: d\n---\nbody")
    if s2.State { t.Fatal("state must default false") }
}
```

**Step 2: Run test to verify failure**

Run: `go test ./internal/skills/ -run TestParseSkillText_StateFlag -v`
Expected: FAIL — field undefined

**Step 3: Write minimal implementation**

Add the `State bool \`yaml:"state"\`` field alongside existing frontmatter
fields (match their tag style — check whether the repo uses yaml or a custom
decoder; adapt tag style, keep the frozen name `state`).

**Step 4: Run test to verify pass**

Run: `go test ./internal/skills/ -race -count=1 -v`
Expected: PASS

### Task 2: Schema + pure patch helpers

**Objective:** validateStatePatch / mergeState / buildStatePrompt exactly per
contract.

**Files:**
- Create: `internal/agent/skill_state.go`
- Test: `internal/agent/skill_state_test.go`

**Step 1: Write failing test**

```go
func TestMergeState_NullDeletesUnknownDropped(t *testing.T) {
    schema := DefaultStateSchema()
    old := map[string]any{"files_touched": []any{"a.go"}, "errors": "x", "rogue": 1}
    patch := map[string]any{
        "files_touched": []any{"a.go", "b.go"},
        "errors":        nil,      // delete
        "rogue2":        "nope",   // unknown → dropped
    }
    clean, dropped := validateStatePatch(patch, schema)
    if _, ok := clean["rogue2"]; ok { t.Fatal("unknown key survived") }
    if len(dropped) != 1 || dropped[0] != "rogue2" { t.Fatalf("dropped: %v", dropped) }
    got := mergeState(old, clean, schema)
    if _, ok := got["errors"]; ok { t.Fatal("null did not delete") }
    ft, ok := got["files_touched"].([]any)
    if !ok || len(ft) != 2 { t.Fatalf("array not replaced: %#v", got["files_touched"]) }
    if _, ok := got["rogue"]; ok { t.Fatal("pre-existing rogue key must be dropped from state too") }
}
```

Plus `TestBuildStatePrompt_Bounded`: skill body 100 chars, state 50 keys, max
chars small ⇒ prompt length ≤ body + budget + observation + fixed overhead,
contains `CURRENT STATE` and `LATEST OBSERVATION` markers.

**Step 2: Run test to verify failure**

Run: `go test ./internal/agent/ -run 'TestMergeState|TestBuildStatePrompt' -v`
Expected: FAIL — undefined

**Step 3: Write minimal implementation**

Per frozen wire shapes. buildStatePrompt renders Σ as compact JSON (keys in
schema order; value cap per key 400 chars with `...[truncated]`), total Σ
block capped at maxStateChars. Fixed section markers so tests and the trace
log can find them.

**Step 4: Run test to verify pass**

Run: `go test ./internal/agent/ -run 'TestMergeState|TestBuildStatePrompt' -v`
Expected: PASS

### Task 3: SkillStateRuntime.Run step loop

**Objective:** The paper's Algorithm 1: prompt → response → validate → merge
→ execute → observe; reasoning discarded.

**Files:**
- Create: `internal/agent/skill_state.go` (Run + response parsing)
- Test: `internal/agent/skill_state_test.go`

**Step 1: Write failing test**

Use a stub LLM chatter (narrow interface, same shape as lifecycle's
llmChatter) returning scripted responses:

```go
func TestSkillStateRun_ToolThenAnswer(t *testing.T) {
    // resp1: tool action + patch; resp2: answer + patch.
    // stub toolRunner records executed tools, returns "tool output 1".
    // Assert: final answer == resp2's answer; tool executed once;
    // the SECOND prompt (captured by stub) does NOT contain resp1's raw
    // content (reasoning discarded) but DOES contain the patched state.
}
func TestSkillStateRun_MaxIterations(t *testing.T) {
    // all responses are tool actions; MaxIterations=3 ⇒ error mentioning 3 steps.
}
```

**Step 2: Run test to verify failure**

Run: `go test ./internal/agent/ -run TestSkillStateRun -v`
Expected: FAIL — undefined NewSkillStateRuntime

**Step 3: Write minimal implementation**

- Response parsing: `markdown.ExtractJSON` (used by verifier.go:175) then
  strict field checks; malformed ⇒ one retry with a corrective system note,
  then error (paper §7 rollback-retry; do NOT corrupt Σ).
- GBNF: when `llm.GBNFConstrainedEnabled()`, attach the grammar option for
  the response shape (constant grammar string in this file; reuse
  llm.WithGrammar — grep its exact signature first).
- Trace: build a leaf-01 mirror record with one step per iteration
  (Action="state_step", Input=prompt digest, Output=observation), write via
  the injected TraceWriter when non-nil; write errors log only.
- Σ initialization: all schema keys present with zero values (empty array /
  empty string) so merges are total.

**Step 4: Run test to verify pass**

Run: `go test ./internal/agent/ -run 'TestSkillStateRun' -race -count=1 -v`
Expected: PASS

### Task 4: Self-verification sweep

- `go build ./...`
- `go test ./internal/agent/ ./internal/skills/ -race -count=1`
- `gofmt -l` touched files
- `go run ./tools/analyzers/mutexio/... ./internal/agent/ ./internal/skills/`
- `go run ./tools/analyzers/predid/... ./internal/agent/`
- Confirm RunWithSkill and reasoningCycle are untouched (diff review).

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing (`-race -count=1`)
- [ ] Contracts satisfied exactly (types, wire shapes, defaults)
- [ ] Files at exact specified paths; no deviations (or documented)
- [ ] Reasoning-discard proven by the ToolThenAnswer test
- [ ] No general-chat-path changes; RunWithSkill untouched

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list with rationale — expected: toolRunner
injection shape if the loop's executor is not directly callable]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Contracts match master §Contract 4 exactly
- [ ] Σ never corrupted on malformed responses (retry-then-error)
- [ ] MaxIterations enforced; ctx cancellation respected mid-loop
- [ ] GBNF attach only behind GBNFConstrainedEnabled()
- [ ] No prompt/trace leakage into non-state-mode paths; no debug artifacts

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Paper §7: state mode is WRONG for audit/debug/provenance tasks — history is
  the deliverable there. The hook (leaf 05) is per-skill opt-in; never default
  it on for built-in skills in this tree.
- Error taxonomy (§5.7): small models drop keys (68%). mergeState must treat a
  missing key as "unchanged" — only explicit null deletes. This is the single
  most important semantic in the file; test it.
- Keep the runtime free of conversation-store access: state mode does NOT read
  or write Conversation history. Σ is the only carrier.
