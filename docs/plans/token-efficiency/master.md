# Token Efficiency Program - Master Orchestrator

> **For the executing agent:** You are the ROOT orchestrator of this tree.
> Dispatch children per the Dispatch Protocol. Review every child in-session
> (main model, never a delegated reviewer). Commit ONLY after review passes,
> staging exact file paths. Implementation agents never commit.

## Meta

- **Repo:** /Users/caimlas/git/meept (Go daemon + CLI + Flutter UI)
- **Origin:** 2026-10-31 pi.dev (earendil-works/pi @ 8ffea8a1) token-efficiency audit. Evidence: `docs/research/token-efficiency-2026-10/pi-evidence.md`.
- **Backend:** Direct dispatch (delegate_task). Test policy: repo AGENTS.md e2e rule - new features get hermetic e2e suites (`e2e/suites/<name>/` + `e2e/manifest.json` path_map); existing unit tests may be updated only for bug fixes.

## Goal and Architecture

Reduce meept's per-turn and per-task token cost by closing three verified gaps
against the pi harness, then decide (on measured data) two larger shapes:

1. **Branch 01 - Output caps + tool-layer spill.** Shell truncation keeps the
   HEAD 50KB today (`internal/tools/builtin/shell.go:510,719`); builds and test
   runs lose the tail where the answer is, and the discarded bytes are gone.
   Pi keeps the TAIL and spills full output to a re-readable file
   (`packages/coding-agent/src/core/tools/truncate.ts:11-12`, `bash.ts:255`).
   Fix: tail-first truncation + spill pointer at the TOOL layer (before context),
   reusing `internal/llm/spill.go` SpillService, with TTL cleanup.
2. **Branch 02 - Codemode sandbox tool.** Pi runs model-written scripts in a
   QuickJS sandbox; nested tool calls never enter LLM context - only the script
   return value does (`packages/codemode/README.md:3`). Meept has no equivalent
   (verified: zero hits for codemode/goja/quickjs/nested-tool in internal/).
   Research-gated: runtime spike decides the Go sandbox (goja vs yaegi vs
   wazero+quickjs) before any implementation leaf dispatches.
3. **Branch 03 - As-needed tool definitions (DESIGN-GATED).** Measure first
   (token audit leaf), then choose between: keep indexed stubs; search-not-
   enumerate (meta-tool discovery, Hermes-style); orchestrator-predicted
   per-episode subsets; hybrid. No implementation leaves until the user ratifies
   a shape via Open Questions Q1/Q2.
4. **Branch 04 - Skill routing without the cap.** Today: fuzzy MatchAll against
   the SYSTEM PROMPT text (wrong signal), capped at 5, fallback returns ALL
   cached skills (`internal/agent/context_injector.go:15,121-166`). Pi lists
   every skill as one line (name/description/location) with no cap and lets the
   model read SKILL.md on demand (`skills.ts:358-388`). Fix the match signal,
   remove the cap, adopt pi-shape one-line listing; embedding-rank is a gated
   follow-up for large libraries.

Hard constraint across all branches: **prompt-cache stability**. Meept
stabilizes the tool prefix for cache hits (`internal/agent/loop.go:4442-4448`,
`StabilizeToolPrefix`) and marks a static/dynamic boundary
(`internal/agent/prompt.go:323` PromptCacheBoundary). Any dynamic tool/skill
list must change only at episode boundaries or via declared deltas - never
per-turn churn that invalidates the KV-cache prefix (session-affinity work).

## Interface Contracts

### C1 - Shell truncation result shape (branch 01)

```go
// internal/tools/builtin/shell.go - ShellResult gains one field:
type ShellResult struct {
    // ... existing fields (ExitCode, Stdout, Stderr, Truncated ...) ...
    // FullOutputPath is set when Truncated is true: absolute path of the
    // spilled full output (stdout+stderr interleaved as captured).
    FullOutputPath string `json:"full_output_path,omitempty"`
}
```
Truncation keeps the LAST MaxOutputSize bytes, cut on a line boundary, with a
header note: `... (truncated, N bytes total; first M bytes omitted; full output: <path>)`.

### C2 - Spill path convention (branch 01)

Tool-layer spills live under `$MEEPT_HOME/spill/tools/<session_id>/<ulid>.txt`
(distinct from the compaction spill dir so TTL policies can differ). IDs come
from `pkg/id.Generate()` (never time.Now().UnixNano / math/rand - repo
invariant, predid analyzer). Sweep policy: delete files older than 24h at
daemon startup and every 6h; cap total spill/tools size at 512MB (oldest-first
eviction). Sweep lives in `internal/llm/spill.go` (pure functions) wired from
`internal/daemon` (lifecycle).

### C3 - Codemode sandbox API (branch 02, pinned after spike confirms runtime)

```go
// internal/codemode (NEW package)
type Sandbox struct { /* runtime-specific */ }
type Options struct {
    Timeout     time.Duration // wall-clock cap; interrupt mid-script
    MemoryLimit uint64        // bytes; script aborts past this
    Tools       []ToolBridge  // injected callable tools
}
type ToolBridge struct {
    Name        string
    Description string
    InputSchema map[string]any // JSON schema for the model
    Call        func(ctx context.Context, argsJSON string) (string, error)
}
type Result struct {
    OK     bool
    Output string // captured text()/log output, clamped to 50KB
    Value  string // JSON-serialized return value, clamped to 50KB
    Err    string
}
func New(opts Options) (*Sandbox, error)
func (s *Sandbox) Execute(ctx context.Context, script string) (Result, error)
func (s *Sandbox) Close() error
```
Agent-facing tool `codemode` (builtin): input `{script: string, timeout_s?: int}`;
output = Result serialized; nested tool calls appear NOWHERE in LLM context.
Config gate: `[agent.tools] codemode_enabled` default **false** (opt-in
defaults invariant - docs/agents/opt-in-defaults.md).

### C4 - Skill listing shape (branch 04)

One line per cached skill, no cap, matched signal = latest user task text:
```
- <name>: <description> [load: file_read <absolute SKILL.md path>]
```
Skills with frontmatter `disable-model-invocation: true` (NEW key, mirrors pi)
are omitted from the listing. The "fallback returns all cached" branch is
removed - listing is always all-visible-skills; relevance ranking (when added)
only orders lines, never hides them below a cap.

## Child Index

| # | Child | Type | Depends on | Est. context | Concurrency |
|---|-------|------|-----------|--------------|-------------|
| 01 | `01-output-spill/orchestrator.md` | branch | none | - | wave 1 |
| 02 | `02-codemode/orchestrator.md` | branch | none (spike gates its own children) | - | wave 1 |
| 03 | `03-dynamic-tools/orchestrator.md` | branch (design-gated) | none | - | wave 1 (audit leaf only) |
| 04 | `04-skill-routing/orchestrator.md` | branch | none | - | wave 2 (after Q3/Q4 answered) |

Branches 01 and 02 are independent and dispatch concurrently. Branch 03's
audit leaf is safe to run any time; its implementation leaves DO NOT EXIST
until Open Questions Q1/Q2 are answered. Branch 04 leaf 01 is safe now
(bug-fix class); leaf 02 is gated on Q4.

## Dispatch Protocol

1. Read the child orchestrator doc; dispatch its leaves per ITS protocol.
2. Test gate first: every code branch runs its 00T gate leaf to RED before
   any implementation leaf dispatches (see Test Gate).
3. Batch independent leaves up to `delegation.max_concurrent_children`.
4. Review each returned leaf IN-SESSION (main model): read changed files, run
   the leaf's verify commands, check contracts C1-C4, grep for artifacts.
5. On pass: `git add <exact paths>` then commit
   `feat(<scope>): <leaf name> (<tree>/NN-leaf)`. Never `git add -A`
   (working tree carries unrelated session work).
6. Update the Completion Tracking Table after every transition; status
   trickles up. Re-dispatch resets status to IN_PROGRESS with a note.
7. Every dispatch context includes: "Do NOT commit. Do NOT run git add.
   Tests: `go test -p 2` (port-exhaustion invariant). macOS host."
8. New package dirs (internal/codemode) require e2e suite + manifest
   path_map entry in the SAME commit (pre-commit check 18/18).

## Test Gate

| Feature | Gate leaf | Unit test target | E2E test target | Status |
|---------|-----------|------------------|-----------------|--------|
| shell tail truncation + spill pointer | 01-output-spill/00T | internal/tools/builtin/shell_test.go (existing file, bug-fix class) | e2e shell suite (existing or new area) | PENDING |
| spill TTL sweep | 01-output-spill/00T | internal/llm/spill_test.go (existing file if present) | covered by sweep unit + startup e2e | PENDING |
| codemode sandbox | 02-codemode/00T | NONE (new feature = e2e only, repo policy) | e2e/suites/codemode/ + manifest path_map | PENDING |
| skill listing (no cap, task signal) | 04-skill-routing/00T | internal/agent/context_injector tests (existing, bug-fix class) | n/a initially | PENDING |

Coverage converges upward: on any delta between gate tests and plan, the
LESSER artifact updates to reflect the GREATER.

## Coding Conventions

- Go; gofmt; no new root-level artifacts (pre-commit-build blocks them).
- Errors wrapped with fmt.Errorf("%w"); no ignored errors, no bare panic.
- IDs via pkg/id.Generate() only (predid analyzer enforced).
- No mutex held across I/O (mutexio analyzer).
- Every Set* gets a nil guard (setters_test.go enforces).
- Tests run with `-p 2` on this host (ephemeral port exhaustion invariant).
- UI text lowercase (TUI/GUI parity rule) if any surface is touched.
- Docs: feature -> docs/workflows/<pkg>.md; update AGENTS.md package table
  when internal/codemode lands (same commit).
- Config additions: JSON5 templates in config/ + internal/config/schema.go
  with validation; new toggles default OFF unless the leaf says otherwise.

## Completion Tracking Table

| Child | Status | Commit | Notes |
|-------|--------|--------|-------|
| 01-output-spill (branch) | COMPLETE | f71bbe8e | both leaves green (01-shell-tail-spill + 02-spill-sweep); tail-keep truncation + FullOutputPath + SweepToolSpills + daemon startup/6h ticker wiring in spill_wiring.go |
| 02-codemode (branch) | COMPLETE | 04a650d7 | spike GO (goja), 01-sandbox-core af2f4bde + 02-codemode-tool 2876a4a5 + 03-e2e-suite 04a650d7; nested-call measurement 334B vs 4175B (92.0% saved) |
| 03-dynamic-tools (branch) | COMPLETE | 78c8aeed | all 7 leaves green (01/02/03/04/05/06/07); value mechanism wired (loop.go scopes the LLM tool payload to the per-conversation active set + loop-side tool_view activation); 00T gate 4-cap + flag-off byte-identical; starvation canary + cache-continuity cases re-armed and GREEN (was t.Skip - payload shrank to active set, task_create absent pre-promotion then present post-tool_view); docs/workflows/dynamic-tools.md + opt-in-defaults + agents.md frontmatter carry the 5.3 warning |
| 04-skill-routing (branch) | COMPLETE | 6a4aca81 | 01-skill-listing COMPLETE (C4 no-cap listing + disable-model-invocation; 4 TestSkillsListing* GREEN, 6169fe4a); 02-embedding-rank COMPLETE (Q4 answered option 3 - always rank; see row below, 6a4aca81 + gendoc 8fa8915e) |
| 04-skill-routing/02-embedding-rank | COMPLETE | 6a4aca81 | Q4 answered option 3 (always rank): embedding-rank ORDER of the no-cap listing at episode start, frozen per episode, never hide/drop/cap; alphabetical fallback on any embedding failure; opt-in skills.ranking config (default OFF) + daemon embedder wiring + injector SetEpisodeTask plumbing; T1-T5 gate tests GREEN (-count=10 clean) |

## Integration Test Plan

After all active branches reach COMPLETE:
1. `make test` (canonical full suite, short mode, -p 2) green.
2. `make e2e-fast` green (includes the new codemode suite).
3. `make lint-ci` green (analyzers + audit scripts).
4. `make graphs && make graphs-check` (new tool/wiring may touch bus/RPC).
5. Manual token delta measurement: re-run the branch-03 audit script before
   and after; record cold-start and per-turn tool-definition token counts in
   docs/research/token-audit follow-up. Target evidence, not vibes.
6. Cache-stability check: run two consecutive turns in one session against a
   local llama.cpp endpoint; confirm prompt-cache hit on turn 2 (log line
   "prefix cache invalidated" must NOT appear when only skill listing order
   changed).

## Review Checklist

- [ ] Each leaf: tasks implemented, gate tests untouched, verify commands re-run by reviewer
- [ ] Contracts C1-C4 match exactly (field names, JSON tags, paths)
- [ ] No predictable IDs (predid), no I/O under mutex (mutexio)
- [ ] No scope creep; no debug artifacts; no TODO without ticket ref
- [ ] New packages: AGENTS.md table + e2e manifest updated in same commit
- [ ] Commits staged with explicit paths only (shared working tree)

## Open Questions

(Gated decisions - asked of the user 2026-10-31; answers pin branch 03/04 shapes.)

- **Q1 (tool routing shape):** ANSWERED 2026-10-31 (user): **hybrid D** -
  episode-start predicted subset with full schemas + tool_search/tool_view
  fallback for misses. Reframed as a single knob (see 03 orchestrator,
  Ratified Shape): "up-front full-schema budget" - B is D with budget 0.
- **Q2 (prediction authority):** ANSWERED 2026-10-31 (user): **deterministic
  intent->toolset map** fed by the existing intent analyzer labels; classifier
  tool-head deferred until the map proves coarse. Model-tier routing ALSO
  ratified: per-model/per-agent `tool_exposure` config (default D; B opt-in
  for frontier-class models), resolved through the existing
  EffectiveSchemaMode precedence seam (internal/agent/loop.go:1434-1437).
  REFINED same day (user): B is capability-GATED - a `search` setting on a
  non-frontier active model silently downgrades to D (warn log), including
  mid-session switches to a lesser model; switching back to a frontier model
  restores B at the next episode boundary. Unknown models = non-frontier
  (fail-safe). Full rule: 03 orchestrator "Capability gate on `search`".
- **Q3 (skill match signal):** Confirm: match skills against the latest user
  task text (+agent role), not the system prompt base. Any need to keep the
  prompt-base signal as secondary?
- **Q4 (skill ranking at scale):** ANSWERED (user, 2026-11-05): **option 3 -
  always rank** - the no-cap one-line-per-skill listing is ordered by
  embedding similarity to the latest task text with NO threshold gate and no
  100-skill trigger point. Ordering ONLY: never hide, drop, or cap a skill
  (branch 04 leaf 02 shipped it; degradation path = alphabetical).
