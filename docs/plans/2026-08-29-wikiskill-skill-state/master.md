# WikiSkill + SKILL.state Adoption — Master Plan

Status: PENDING
Created: 2026-08-29
Parent: none (root)

## Research References

This tree implements recommendations from two papers. Every commit message for
leaves in this tree MUST cite the relevant arXiv id (see Dispatch Protocol).
README.md must list both papers (leaf 06 adds the note).

- **WikiSkill: Compiling Agent Experience into Persistent Knowledge for Skill
  Evolution** — arXiv:2608.27454 (Tang, Rashtchian, Ferng, Tomkins, Juan, Vu;
  Google Research + Virginia Tech).
  Defines the three-layer workspace: immutable `raw/` execution traces, a
  persistent `wiki/` (patterns/, logs.md, skill-impact.md) that is NEVER rolled
  back, and a `skills/` layer that is gated and rolled back. Wiki Maintainer
  consolidates sampled traces (5 fail + 3 pass, 15k char cap) into pattern
  pages; Skill Proposer reads index + skill-impact history before proposing;
  inference agent must NOT see the wiki (§5.1 ablation: wiki-informed proposer
  +15.0 avg points; wiki access for the worker during evolution DEGRADES final
  skill quality).
- **SKILL.state: Scalable Long-Horizon Agent Skills** — arXiv:2608.26263
  (Badhe, Tiwari, Chung; Google + Purdue).
  Defines state-mode execution: per-step prompt is A_t = (P, Σ_t, O_t) — the
  immutable skill spec P, a structured execution state Σ, and the latest
  observation O only. The model emits (R_t, ΔΣ_t, a_t); the runtime validates
  ΔΣ against a schema, merges (⊕, null-deletion), executes a_t, and DISCARDS
  R_t. Prompt stays O(1); cumulative tokens O(T). Known failure modes on small
  models: state-key overwrite (68%), schema/type errors (20%), JSON slips (12%)
  — motivating grammar-constrained decoding (meept already has GBNF).

## Goal

Amend meept's existing skill-evolution system with the two papers' core
mechanisms, without replacing any user-facing skill surface:

1. **Raw layer (WikiSkill)** — persist success AND failure trajectories to an
   immutable trace store so the evolver can learn from real failures. Today
   `buildTrajectory` hardcodes `Success: true` and nothing persists traces.
2. **Wiki layer (WikiSkill)** — a durable, compounding markdown wiki
   (`~/.meept/wiki/`) replaces the dead in-memory pattern store
   (`patterns.json` writes are deprecated; StorePattern is RAM-only), plus a
   `skill-impact.md` ledger of accepted AND rejected proposals so later cycles
   do not repeat rejected edits.
3. **Trace-fed refinement (WikiSkill)** — evolver Pass A samples traces and
   reads the wiki/impact ledger instead of usage counters alone.
4. **State-mode execution (SKILL.state)** — opt-in RunWithSkill mode with an
   explicit Σ_t, validated ΔΣ merges, and reasoning discard, for long
   procedural skill runs.

Non-goals (explicit): no full-body skill injection (retrieval stays, max 5);
no validation-split score gate (verifier rubric + plan approval stay);
wiki/traces are NEVER injected into inference prompts (ContextInjector
untouched); `skills.evolver.enabled` and `skills.state.enabled` stay false by
default; the `selfimprove` code-fix cycle (detect→apply) is untouched.

## Architecture Overview

```
AgentLoop turn ──> TraceStore.Write (raw/, immutable, success + failure)   [01]
                     │
LearningPipeline.Judge/Distill ──> WikiStore.UpsertPattern (wiki/patterns/) [02]
                     │                  └─ index.md, logs.md
Evolver (6h scheduler) Pass A refine:
    reads wiki/index.md + skill-impact.md + Sample(traces) ──> proposal     [03]
    every verdict (accept OR reject) ──> WikiStore.AppendSkillImpact        [03]
    existing Verifier rubric + Writer/Versioner + plan approval unchanged   [03]
RunWithSkill: if skill frontmatter `state: true` and skills.state.enabled:   [04, 05]
    SkillStateRuntime: prompt = P + Σ_t + O_t; validate ΔΣ; merge; drop R_t;
    GBNF-constrained response when llm.GBNFConstrainedEnabled()
Config + daemon wiring in components.go (single wiring leaf)                 [05]
Docs: README research note, docs/workflows/skills.md, AGENTS.md invariants   [06]
```

Wiki layout (frozen):

```
~/.meept/wiki/
  index.md              # one line per pattern: PROBLEM + ROOT CAUSE + FIX
  logs.md               # append-only evolution log
  skill-impact.md       # append-only ledger: every proposal verdict + diff
  patterns/<slug>.md    # one pattern page per LearnedPattern
  traces/<yyyy-mm-dd>/<trace-id>.json   # immutable raw trajectories
```

## Interface Contracts (frozen across children)

### Contract 1: TraceStore (internal/selfimprove/trace_store.go)

```go
package selfimprove

// TraceStep mirrors agent.TrajectoryStep for persistence.
type TraceStep struct {
    Action  string `json:"action"`
    Input   string `json:"input,omitempty"`
    Output  string `json:"output,omitempty"`
    Success bool   `json:"success"`
}

// TraceRecord is one immutable execution trajectory (arXiv:2608.27454 raw/).
// Written exactly once; never modified or deleted by v1 (no pruning).
type TraceRecord struct {
    ID             string      `json:"id"`              // pkg/id.Generate()
    SessionID      string      `json:"session_id"`      // conversation id
    Domain         string      `json:"domain,omitempty"`
    Outcome        string      `json:"outcome"`         // "success" | "failure"
    Error          string      `json:"error,omitempty"` // error text when failure
    InjectedSkills []string    `json:"injected_skills,omitempty"`
    Steps          []TraceStep `json:"steps"`
    Summary        string      `json:"summary,omitempty"` // final response
    CreatedAt      time.Time   `json:"created_at"`
}

type TraceStore struct { /* dir-rooted */ }

func NewTraceStore(dir string, logger *slog.Logger) *TraceStore
// Write persists as <dir>/traces/<yyyy-mm-dd>/<id>.json via tmp+rename
// (atomic-write pattern, same as lifecycle/writer.go). Returns the path.
func (ts *TraceStore) Write(rec *TraceRecord) (string, error)
// Sample walks newest-first, buckets by Outcome, caps each record's step
// text at maxChars, and returns up to maxFails failures then maxPasses
// successes. Errors never abort a Sample (logged, skipped).
func (ts *TraceStore) Sample(maxFails, maxPasses, maxChars int) ([]TraceRecord, error)
```

Owner: 01. Consumers: 03 (via TraceProvider), 04 (state runtime), 05 (wiring).

### Contract 2: WikiStore (internal/selfimprove/wiki.go)

```go
package selfimprove

// SkillImpactEntry is one append-only ledger row in skill-impact.md
// (arXiv:2608.27454 §3.2.4: proposal metadata, diff, score, verdict).
type SkillImpactEntry struct {
    Time      time.Time `json:"time"`
    Action    string    `json:"action"`       // refine | create | archive
    SkillName string    `json:"skill_name"`
    Diff      string    `json:"diff,omitempty"` // candidate content or diff
    Score     float64   `json:"score"`          // verifier overall average
    Accepted  bool      `json:"accepted"`
    Reason    string    `json:"reason,omitempty"`
}

type WikiStore struct { /* dir-rooted */ }

func NewWikiStore(dir string, logger *slog.Logger) *WikiStore
// UpsertPattern writes patterns/<slug>.md (slug = domain + "-" +
// first 12 hex of ContentHash; stable across restarts, dedupe-friendly),
// then refreshes index.md. Atomic tmp+rename per file.
func (w *WikiStore) UpsertPattern(p *LearnedPattern) (string, error)
func (w *WikiStore) RebuildIndex() error
func (w *WikiStore) AppendLog(entry string) error            // logs.md
func (w *WikiStore) AppendSkillImpact(e SkillImpactEntry) error
func (w *WikiStore) ReadSkillImpact() (string, error)
// LoadPatterns re-parses pattern pages back into LearnedPattern values
// so LearningPipeline.Initialize can repopulate the in-memory map.
func (w *WikiStore) LoadPatterns() ([]*LearnedPattern, error)
```

LearningPipeline integration (same leaf, internal/selfimprove/learning.go):

```go
// Nil-guarded setter (repo Set* rule).
func (lp *LearningPipeline) SetWikiStore(ws *WikiStore)
// StorePattern: existing in-memory behavior unchanged; additionally calls
// ws.UpsertPattern + RebuildIndex when ws != nil. Wiki I/O errors are
// logged at Warn and never fail StorePattern.
// Initialize: after loadPatterns, when ws != nil, LoadPatterns and insert
// patterns whose ID is absent from the map (restart survival).
```

Owner: 02. Consumers: 03 (skill-impact ledger), 05 (wiring).

### Contract 3: Evolver trace/wiki options (internal/skills/lifecycle/evolver.go)

```go
// TraceProvider decouples the evolver from the concrete store (tests stub it).
type TraceProvider interface {
    Sample(maxFails, maxPasses, maxChars int) ([]selfimprove.TraceRecord, error)
}

func WithTraceProvider(tp TraceProvider) EvolverOption   // nil-guarded
func WithWikiStore(ws *selfimprove.WikiStore) EvolverOption // nil-guarded

// Package constants (arXiv:2608.27454 Appendix C; deliberately NOT config in v1):
const (
    traceSampleMaxFails = 5
    traceSampleMaxPass  = 3
    traceSampleMaxChars = 15000
    impactLedgerMaxChars = 20000 // cap on skill-impact.md content in prompts
)

// Pass A behavior (passARefine): when both options are wired, the refine
// prompt PREPENDS, in this order: (1) skill-impact.md content — instruction:
// "do not repeat rejected proposals"; (2) sampled traces as
// [outcome=<s|f>] blocks. When wiki is wired, processProposal appends a
// SkillImpactEntry for EVERY verifier verdict (accept AND reject).
```

Owner: 03. Consumers: 05 (daemon wiring passes real TraceStore/WikiStore).

### Contract 4: State-mode runtime (internal/agent/skill_state.go + skills frontmatter)

```go
// internal/skills — SkillMetadata / Skill gain:
//   State bool `yaml:"state"`  // frontmatter `state: true` opts the skill
//                              // into state-mode execution

package agent

// StateField is one Σ key (arXiv:2608.26263 §3.1: one schema per domain,
// authored once). v1 ships exactly this default coding schema.
type StateField struct {
    Name string // "files_touched" | "tests_run" | "errors" | "next_step"
    Type string // "array" | "string"
    Desc string
}
func DefaultStateSchema() []StateField

type SkillStateConfig struct {
    MaxStateChars int // prompt budget for Σ JSON; default 2000
    MaxIterations int // step cap; default skill.MaxIterations or 25
}

type SkillStateRuntime struct { /* loop + config + logger */ }
func NewSkillStateRuntime(loop *AgentLoop, cfg SkillStateConfig, logger *slog.Logger) *SkillStateRuntime

// Run executes the skill in state mode. Per step:
//   prompt  = skill body (P) + Σ_t JSON + latest observation O_t
//   model   = JSON {action:{tool,args}|{answer:text}, state_patch:{...}}
//   runtime = validate patch against schema (unknown keys dropped, null
//             deletes per ⊕ null-deletion), merge, execute tool via the
//             loop executor (O_{t+1} = tool output), DISCARD reasoning.
// The full step log is appended to the loop's TraceStore when wired (nil-safe).
// When llm.GBNFConstrainedEnabled() the ΔΣ response request attaches a GBNF
// grammar for the response shape (constant in this file).
func (r *SkillStateRuntime) Run(ctx context.Context, skill *skills.Skill, input, conversationID string) (string, error)

// Pure helpers (unit-tested directly):
func validateStatePatch(patch map[string]any, schema []StateField) (clean map[string]any, dropped []string)
func mergeState(old map[string]any, patch map[string]any, schema []StateField) map[string]any
func buildStatePrompt(skillBody string, state map[string]any, observation string, maxChars int) string
```

Owner: 04. Consumers: 05 (RunWithSkill hook + wiring).

### Contract 5: Config + wiring + hook (leaf 05)

```go
// internal/config/schema.go — inside SkillsConfig:
type SkillsWikiConfig struct {
    Enabled bool   `json:"enabled" toml:"enabled"` // default true
    Dir     string `json:"dir"     toml:"dir"`     // default "~/.meept/wiki"
}
type SkillsStateConfig struct {
    Enabled       bool `json:"enabled"        toml:"enabled"`        // default false
    MaxStateChars int  `json:"max_state_chars" toml:"max_state_chars"` // default 2000
}
// SkillsConfig gains: Wiki SkillsWikiConfig; State SkillsStateConfig.
// Defaults added in DefaultConfig() alongside the existing Evolver block.
```

Daemon wiring (internal/daemon/components.go, near the evolver block at ~6410):
- `TraceStore` + `WikiStore` constructed when `cfg.Skills.Wiki.Enabled`.
- `c.LearningPipeline.SetWikiStore(ws)` when both exist.
- Evolver options gain `lifecycle.WithTraceStore(...)`-shaped wiring via
  TraceProvider adapter + `lifecycle.WithWikiStore(ws)` (only when evolver enabled).
- `agent.WithSkillStateRuntime(...)` LoopOption when `cfg.Skills.State.Enabled`.

```go
// internal/agent — LoopOption + hook:
func WithSkillStateRuntime(r *SkillStateRuntime) LoopOption // nil-guarded
// RunWithSkill (loop.go:2256), BEFORE conv.SetSystemPrompt:
//   if skill.State && l.skillStateRuntime != nil {
//       return l.skillStateRuntime.Run(ctx, skill, input, conversationID)
//   }
```

Owner: 05. Consumers: none (terminal wiring leaf).

## Child Index

| # | Document | Type | Est. context | Depends on | Wave |
|---|----------|------|--------------|------------|------|
| 01 | 01-trace-store.md | leaf | ~50K | none | A |
| 02 | 02-wiki-store.md | leaf | ~55K | none | A |
| 03 | 03-evolver-trace-fed.md | leaf | ~55K | 01, 02 | B |
| 04 | 04-state-runtime-core.md | leaf | ~65K | 01 (interface only) | B |
| 05 | 05-config-wiring.md | leaf | ~55K | 02, 03, 04 | C |
| 06 | 06-docs-research.md | leaf | ~35K | all | D |

Concurrency: Wave A dispatches 01 + 02 together. Wave B dispatches 03 + 04
together (disjoint packages: lifecycle vs skills+agent). 05 and 06 are
sequential. Loop.go file ownership: 01 owns the triggerLearning/buildTrajectory
region; 05 owns the RunWithSkill region only. Schema.go ownership: 05 only.

## Dispatch Protocol

For each child, in wave order:

1. **Dispatch** via `delegate_task` with: the full leaf document, the frozen
   Interface Contracts above verbatim, the Coding Conventions block, and:
   - "Do NOT commit. Do NOT run git add. Write code, run tests, report results only."
   - "Explore with search_files or terminal cat — never feed read_file
     line-numbered output into write_file. After writing a file, do NOT read
     it back to verify."
   - "All UI-visible text lowercase. Never use time.Now().UnixNano() or
     math/rand for IDs — use pkg/id.Generate()."
2. **Review in-session** (main model, never a delegated reviewer): read the
   changed files, verify contracts match exactly, run the leaf's test commands,
   check the Review Checklist below.
3. **Re-dispatch** with specific feedback on gaps (max 3 cycles), then escalate.
4. **Commit** (orchestrator only) with the paper citation in the message:
   - 01: `feat(skills): immutable trace store for evolution evidence (arXiv:2608.27454 raw layer)`
   - 02: `feat(skills): persistent wiki store + pattern pages (arXiv:2608.27454 wiki layer)`
   - 03: `feat(skills): trace-fed refine + skill-impact ledger (arXiv:2608.27454 §3.2)`
   - 04: `feat(agent): skill.state runtime core (arXiv:2608.26263 §3)`
   - 05: `feat(skills): wiki/state config, daemon wiring, RunWithSkill hook (arXiv:2608.27454, 2608.26263)`
   - 06: `docs(skills): wiki/state docs + research references (arXiv:2608.27454, 2608.26263)`
5. **Update** the Completion Tracking Table after every transition.

## Review Checklist

The orchestrator verifies each child in-session:

- [ ] Compiles: `go build ./...`; leaf tests pass with `-race -count=1`
- [ ] `gofmt -l` clean on exactly the files the leaf touched
- [ ] Contract types/signatures match master §Interface Contracts exactly
- [ ] mutexio: no file I/O under mutex (collect-under-lock, operate after)
- [ ] predid: IDs from pkg/id.Generate(); no time/rand
- [ ] Set* methods have nil guards
- [ ] No debug prints, TODOs, placeholders, commented-out code, unused exports
- [ ] No line-number corruption (`grep -rcE '^\s+[0-9]+\|'` on touched files)
- [ ] Wiki/trace content never reaches ContextInjector / BuildSystemPrompt
- [ ] Defaults preserved: evolver.enabled=false, state.enabled=false,
      wiki writes are additive and nil-safe when disabled

## Coding Conventions

- Go stdlib style; wrap errors with `%w` and a lowercase context prefix.
- Atomic file writes: `.tmp` file then `os.Rename` (match lifecycle/writer.go).
- No I/O under mutex (mutexio analyzer enforces).
- IDs via `pkg/id.Generate()` (predid analyzer).
- Nil guards in every Set*/With* option.
- Two-value type assertions on map[string]any payload values.
- Tests live beside the code; selfimprove tests are INTERNAL package
  (`package selfimprove`) — reuse existing fixtures, grep before redeclaring.
- Config edits: struct tags with aligned json/toml columns AND a default in
  DefaultConfig(); re-grep your additions before reporting (sibling-edit races).
- AGENTS.md is updated in the final integration commit (leaf 06 drafts it).

## Completion Tracking Table

| Child | Status | Notes |
|-------|--------|-------|
| 01-trace-store.md | PENDING | |
| 02-wiki-store.md | PENDING | |
| 03-evolver-trace-fed.md | PENDING | |
| 04-state-runtime-core.md | PENDING | |
| 05-config-wiring.md | PENDING | |
| 06-docs-research.md | PENDING | |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

After all children COMPLETE:

1. Full build + focused suites:
   `go build ./... && go test ./internal/selfimprove/ ./internal/skills/... ./internal/skills/lifecycle/ ./internal/agent/ -race -count=1`
2. Repo gates: `make lint-ci` (golangci-lint + mutexio + predid + audit scripts).
3. Wiki smoke (scratch config, `skills.evolver.enabled=true`,
   `skills.wiki.enabled=true`, `auto_apply=false`): run 3-4 chats (make one
   fail), run `./bin/meept skills evolve`, verify
   `~/.meept/wiki/{index.md,skill-impact.md,logs.md}` exist, skill-impact.md
   carries at least one verdict row. Restart the daemon, run evolve again,
   verify pattern pages survived the restart (Pass B sees them).
4. Failure-trace smoke: force a failing turn; verify a trace JSON with
   `"outcome":"failure"` exists under `~/.meept/wiki/traces/`.
5. State-mode smoke (`skills.state.enabled=true`, skill with `state: true`):
   run it; verify per-step prompts stay bounded (log line per step with Σ size),
   reasoning is absent from step N+1's prompt, and the full step log lands in
   traces.
6. Negative: `skills.wiki.enabled=false` → no `~/.meept/wiki` writes;
   `skills.state.enabled=false` → RunWithSkill path byte-identical to today.
7. `make graphs` (no new bus topics expected); AGENTS.md reviewed per repo rule.

## Open Questions

- Q1 Trace retention: paper has no pruning. v1 keeps traces forever; if size
  becomes a problem, add a 90-day prune pass to TraceStore later. Default: no prune.
- Q2 State schema authoring: v1 hardcodes DefaultStateSchema in code and opts
  in via frontmatter `state: true`. Per-skill `state.schema.json` files are a
  possible v2; decide when a second domain needs a different schema.
- Q3 Cluster sync of the wiki: out of scope v1 (papers are single-node). If
  pooled clusters need a shared wiki, route through the existing gossip
  channel design, NOT backup_sync file moves.
- Q4 Pass B promote source: v1 keeps `learning.Retrieve` (now wiki-backed via
  restart survival). Promoting directly from wiki pages instead is a possible
  follow-up; do not change Pass B in this tree.

## Notes

- The evolver is OFF by default (`skills.evolver.enabled=false`,
  internal/config/schema.go:2455). This tree does not flip that default.
- WikiSkill §5.1 is the binding constraint for reviewers: if any diff makes
  wiki/trace content reachable from ContextInjector, BuildSystemPrompt, or any
  inference-path prompt builder, REJECT the leaf.
- The paper's validation-split score gate is deliberately NOT ported. The
  verifier rubric + plan approval stay the gate (see Goal non-goals).
- SKILL.state §7: never force state mode on audit/debug/provenance sessions —
  those tasks ARE the history. The hook is per-skill opt-in only.
- Paper texts cached at:
  `~/.hermes/cache/web/arxiv.org-4af1808202.md` (WikiSkill) and
  `~/.hermes/cache/web/arxiv.org-ce10c44d91.md` (SKILL.state) — re-fetch from
  arXiv if missing (ids above).
