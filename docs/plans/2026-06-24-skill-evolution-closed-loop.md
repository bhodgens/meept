# Meept Skill Evolution: Full Closed-Loop Adoption

**Source spec:** User-provided plan (closed-loop skill evolution, SkillClaw-informed).
**Supersedes:** `docs/plans/skill-evolution-decision-framework.md` (never implemented).

**Build order:** Phase 1 → Phase 3 → Phase 4 → Phase 2 (per spec; 3 and 4 are
parallelizable after 1 lands, 2 ties everything together).

**Status (2026-06-24):** All four phases VERIFIED complete via three-pass contract-driven verification. 56 tests pass in `internal/skills/lifecycle/`, `go build ./...` clean, `go vet ./...` clean. User-facing surfaces wired: `meept skills stats|archive|restore|history|evolve`, `skills.stats|archive|restore|history|evolve` RPC, `GET /api/v1/skills/stats`, `POST /api/v1/skills/{slug}/{archive,restore}`, `POST /api/v1/skills/evolve`.

---

## Phase 1: Usage tracker + writer foundation

**Goal:** measure per-skill effectiveness and provide atomic skill file writes.

**Provides:**
- `internal/skills/lifecycle/types.go` — shared types `Outcome` (enum: Positive/Negative/Neutral), `UsageStats`, `SkillEvent`
- `internal/skills/lifecycle/usage.go` — `type UsageTracker interface { RecordInjection(skillName string) error; RecordOutcome(skillName string, outcome Outcome, sessionID string) error; GetStats(skillName string) (*UsageStats, error); GetAllStats() (map[string]*UsageStats, error); GetLowPerformers(threshold float64, minInjections int) ([]*UsageStats, error); Close() error }` and `func NewUsageTracker(dbPath string, logger *slog.Logger) (*UsageTrackerImpl, error)` — SQLite at `~/.meept/skills.db`
- `internal/skills/lifecycle/writer.go` — `type Writer struct{...}` with `func NewWriter(skillsDir string, logger *slog.Logger) *Writer` and methods `WriteSkill(name string, content string) error`, `ArchiveSkill(name string) error`, `RestoreSkill(name string) error`, `ReadSkill(name string) (string, error)` — atomic write to `~/.meept/skills/<name>/SKILL.md`
- `internal/agent/loop.go` — new field `usageTracker lifecycle.UsageTracker` at field ~429 (next to `learningPipeline`) and option `func WithUsageTracker(ut lifecycle.UsageTracker) LoopOption` (nil-guarded)
- `internal/agent/prompts/baseline.go` — new function `BuildSkillsPromptSectionTracked(skills []SkillInfo) (prompt string, injected []string)` (does NOT remove `BuildSkillsPromptSection`; adds new variant returning the names of skills surfaced)
- `cmd/meept/skills.go` — new subcommands `stats`, `archive <name>`, `restore <name>` calling `skills.stats`, `skills.archive`, `skills.restore` RPC methods
- `internal/rpc/skills.go` — new RPC handlers `skills.stats`, `skills.archive`, `skills.restore` registered via `RegisterSkillsHandlers` (need UsageTracker + Writer passed in)
- `internal/daemon/components.go` — `Components.SkillUsageTracker` and `Components.SkillWriter` fields; construction in `initialize()` (near line 596 where LearningPipeline is built); wiring into agent loop via `WithUsageTracker` (near line 740); lifecycle close near line 2704

**Consumes:**
- `internal/skills.Registry` — Writer's `ArchiveSkill`/`RestoreSkill` call `Registry.Unregister`/`Register` (re-`ParseSkillFile` on restore) so the registry stays in sync with disk
- `internal/security/engine.go` plain SQLite pattern — `NewUsageTracker` mirrors its `sql.Open("sqlite", ...?_journal_mode=WAL&_busy_timeout=5000)`, `SetMaxOpenConns(1)`, schema initialization
- `internal/agent/loop.go` field 428 `learningPipeline LearningPipeline` — new `usageTracker` field placed next to it; `WithUsageTracker` option follows the `WithLearningPipeline` pattern at line 686 (with nil guard)
- `internal/daemon/components.go` `c.LearningPipeline` (line 596) — the new tracker+writer are constructed in the same block, gated on `cfg.Skills.Enabled` (not `cfg.SelfImprove.Enabled`, since skill tracking should work even when self-improve is off)

**Anti-completion signals:**
- `return nil$` in `RecordInjection` / `RecordOutcome` in `internal/skills/lifecycle/usage.go` — must execute SQL, not no-op
- `// TODO` in `internal/skills/lifecycle/`
- `would run|should run|placeholder|stub` in `internal/skills/lifecycle/`
- Absence of `internal/skills/lifecycle/usage_test.go`
- Absence of `internal/skills/lifecycle/writer_test.go`
- Absence of `WithUsageTracker` in `internal/agent/loop.go`
- Absence of `BuildSkillsPromptSectionTracked` in `internal/agent/prompts/baseline.go`

**Behavioral acceptance:**
- Test: `go test ./internal/skills/lifecycle/... -run 'TestUsageTracker|TestWriter' -v`
- Asserts (usage): after 10 `RecordInjection` + 8 `RecordOutcome(Positive)` + 2 `RecordOutcome(Negative)`, `GetStats` returns `InjectCount=10, PositiveCount=8, NegativeCount=2, Effectiveness=0.8`
- Asserts (writer): `WriteSkill("test-skill", "<content>")` creates `~/.meept/skills/test-skill/SKILL.md`; `ArchiveSkill("test-skill")` moves it to `~/.meept/skills.archived/test-skill/`; `RestoreSkill` moves it back; `ReadSkill` returns written content
- Asserts (negative): `RecordOutcome` on an unknown skill name does NOT panic (upserts a row with inject_count=0)
- Test file: `internal/skills/lifecycle/usage_test.go`, `internal/skills/lifecycle/writer_test.go`

---

**Files:**
- Create: `internal/skills/lifecycle/types.go`
- Create: `internal/skills/lifecycle/usage.go`
- Create: `internal/skills/lifecycle/writer.go`
- Create: `internal/skills/lifecycle/usage_test.go`
- Create: `internal/skills/lifecycle/writer_test.go`
- Modify: `internal/agent/loop.go` (add `usageTracker` field ~429, `WithUsageTracker` option ~693, record injections+outcomes in `triggerLearning` ~1762)
- Modify: `internal/agent/prompts/baseline.go:178` (add `BuildSkillsPromptSectionTracked` variant)
- Modify: `internal/rpc/skills.go:219` (add `skills.stats`, `skills.archive`, `skills.restore` handlers; update `RegisterSkillsHandlers` signature to accept tracker+writer)
- Modify: `cmd/meept/skills.go:13` (add `stats`, `archive`, `restore` subcommands)
- Modify: `internal/daemon/components.go` (add fields ~135, construct ~596, wire ~740, close ~2704, update `RegisterSkillsHandlers` call site)
- Modify: `internal/comm/http/server.go` + `internal/comm/http/api_handlers.go` (REST mirrors: `GET /api/v1/skills/stats`, `POST /api/v1/skills/{slug}/archive`, `POST /api/v1/skills/{slug}/restore`)

- [ ] **Step 1.1:** Create `internal/skills/lifecycle/types.go` with shared types: `Outcome` (enum with `Positive`, `Negative`, `Neutral` constants), `UsageStats` struct (SkillName, InjectCount, PositiveCount, NegativeCount, NeutralCount, LastInjectedAt, LastUsedAt, Effectiveness), `SkillEvent` struct.
- [ ] **Step 1.2:** Create `internal/skills/lifecycle/usage.go` with `UsageTracker` interface and `UsageTrackerImpl`. Follow `security/engine.go` pattern: `sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")`, `SetMaxOpenConns(1)`, `sqlx.NewDb`. Schema: two tables (`skill_usage` aggregate + `skill_usage_events` log) per spec. `RecordInjection` = upsert inject_count + insert event. `RecordOutcome` = upsert count + update effectiveness + insert event. `GetStats`/`GetAllStats`/`GetLowPerformers` = SELECT queries. `Close` = close DB.
- [ ] **Step 1.3:** Create `internal/skills/lifecycle/writer.go` with `Writer` struct. Skills dir layout: `<skillsDir>/<name>/SKILL.md`. `WriteSkill` = MkdirAll + write to `.tmp` + `os.Rename`. `ArchiveSkill` = move to `<skillsDir>.archived/<name>/` (create archive dir if needed). `RestoreSkill` = reverse move. `ReadSkill` = `os.ReadFile`. Follow CLAUDE.md mutex rule: no I/O under lock (Writer has no mutex, but document this).
- [ ] **Step 1.4:** Add `usageTracker lifecycle.UsageTracker` field to `AgentLoop` (loop.go ~429, after `learningPipeline`). Add `WithUsageTracker` option (~693, after `WithLearningPipeline`) with nil guard: `if ut != nil { l.usageTracker = ut }`.
- [ ] **Step 1.5:** Modify `triggerLearning` in loop.go (~1726). After `judgment` is obtained from `learningPipeline.Judge`, translate to `Outcome`: `judgment.ShouldLearn && judgment.Quality >= 0.7` → `Positive`, `!judgment.ShouldLearn` → `Negative`, else `Neutral`. For each skill name in the turn's injected list (track via a new `lastInjectedSkills []string` field on AgentLoop, populated when the prompt is built), call `usageTracker.RecordOutcome`. Wrap in `if l.usageTracker != nil`.
- [ ] **Step 1.6:** Add `BuildSkillsPromptSectionTracked(skills []SkillInfo) (string, []string)` to `internal/agent/prompts/baseline.go`. Same body as `BuildSkillsPromptSection` but also returns the slice of skill names that were included in the prompt. Update the dispatcher/loop to use this variant when a usage tracker is wired, and store the returned names on the loop's `lastInjectedSkills` field for the outcome-recording in step 1.5. When `BuildSkillsPromptSection` (untracked) is still called, also call `usageTracker.RecordInjection` for each skill so injection counts still increment even without outcome tracking.
- [ ] **Step 1.7:** Create `internal/skills/lifecycle/usage_test.go` and `writer_test.go` with table-driven tests per the behavioral acceptance criteria. Use `t.TempDir()` for DB and skills dir.
- [ ] **Step 1.8:** Add RPC handlers `skills.stats`, `skills.archive`, `skills.restore` to `internal/rpc/skills.go`. Update `RegisterSkillsHandlers` signature to `func RegisterSkillsHandlers(server *Server, registry *skills.Registry, executor *skills.Executor, tracker lifecycle.UsageTracker, writer *lifecycle.Writer)`. Each handler: unmarshal params, call tracker/writer method, marshal result. Add nil-guards (return error if tracker/writer nil).
- [ ] **Step 1.9:** Add CLI subcommands to `cmd/meept/skills.go`: `stats [skill-name]`, `archive <name>`, `restore <name>`. Each connects to daemon, calls the new RPC method, prints result. `stats` with no arg calls `skills.stats` with empty name (returns all); with an arg returns single skill.
- [ ] **Step 1.10:** Wire in `internal/daemon/components.go`: add `SkillUsageTracker *lifecycle.UsageTrackerImpl` and `SkillWriter *lifecycle.Writer` to Components struct (~135). Construct in `initialize()` near line 596 (gated on `cfg.Skills.Enabled`). Wire `WithUsageTracker(c.SkillUsageTracker)` into agent loop opts (~740). Update the `RegisterSkillsHandlers` call site to pass tracker+writer. Add `c.SkillUsageTracker.Close()` to `stopComponents` (~2704). Construct writer with `~/.meept/skills/` (expand `~`).
- [ ] **Step 1.11:** Add REST mirrors in `internal/comm/http/`: `GET /api/v1/skills/stats` (optional `?name=`), `POST /api/v1/skills/{slug}/archive`, `POST /api/v1/skills/{slug}/restore`. These dispatch via the `s.rpcCall` pattern (same as MCP catalog endpoints) to avoid holding direct tracker/writer refs.
- [ ] **Step 1.12:** Run `go build ./...` and `go test ./internal/skills/lifecycle/... -v`. Fix any issues.

---

## Phase 3: Skill verifier gate

**Goal:** no skill change goes live without a quality check. Modeled on SkillClaw's `skill_verifier.py`.

**Provides:**
- `internal/skills/lifecycle/verifier.go` — `type Verifier struct{...}`, `func NewVerifier(llmClient *llm.Client, logger *slog.Logger) *Verifier`, `func (v *Verifier) Verify(ctx context.Context, req VerifyRequest) (*VerificationResult, error)`
- `internal/skills/lifecycle/types.go` (augment) — `VerifyRequest` struct (Action, CandidateContent, CurrentContent, EvidenceSummary), `VerificationResult` struct (Action accept/reject, Score, Reasons, Dimensions with four floats)
- `internal/skills/lifecycle/evolver.go` dependency: evolver (Phase 2) calls `Verifier.Verify` before applying any skill change

**Consumes:**
- Phase 1's `UsageStats` type — `VerifyRequest.EvidenceSummary` includes usage stats from the tracker
- `internal/llm.Client.Chat` — verifier sends a single LLM prompt with the 4-dimension rubric and parses JSON response (follows the pattern in `internal/selfimprove/learning.go:283-309` Judge method)

**Anti-completion signals:**
- `return &VerificationResult{Action: "accept"}` hardcoded in `Verify` — must call LLM
- `// TODO` in `internal/skills/lifecycle/verifier.go`
- `would run|should run|placeholder|stub` in verifier
- Absence of `internal/skills/lifecycle/verifier_test.go`
- `Score: 0\.0` as sole scoring logic in verifier (must compute from dimensions)

**Behavioral acceptance:**
- Test: `go test ./internal/skills/lifecycle/... -run TestVerifier -v`
- Asserts: when all 4 dimensions ≥ 0.75 in the mock LLM response → `Action == "accept"`
- Asserts: when any dimension < 0.5 → `Action == "reject"`
- Asserts: when LLM unavailable (nil client) → heuristic fallback scores 0.5 on all dims, action depends on threshold
- Test file: `internal/skills/lifecycle/verifier_test.go`

---

**Files:**
- Create: `internal/skills/lifecycle/verifier.go`
- Create: `internal/skills/lifecycle/verifier_test.go`

- [ ] **Step 3.1:** Create `internal/skills/lifecycle/verifier.go`. `Verifier` struct holds `llmClient *llm.Client` and logger. `Verify` builds a prompt with the 4-dimension rubric (grounded_in_evidence, preserves_existing_value, specificity_and_reusability, safe_to_publish), sends to LLM, parses JSON response. Reject if any dim < 0.5 or overall avg < `minScore` (default 0.75, configurable via `Verifier.minScore` field set by constructor option). When `llmClient == nil`, use heuristic fallback: score 0.5 on all dims (neutral, leans reject under default threshold).
- [ ] **Step 3.2:** Augment `types.go` with `VerifyRequest` and `VerificationResult` structs per the Provides block.
- [ ] **Step 3.3:** Create `verifier_test.go` with a mock LLM client (or a `verifyHeuristic` path testable without LLM). Test the threshold logic: all dims ≥ 0.75 → accept; any dim < 0.5 → reject; overall avg 0.6 (between 0.5 and 0.75) → reject.
- [ ] **Step 3.4:** Run `go test ./internal/skills/lifecycle/... -run TestVerifier -v`.

---

## Phase 4: Version bundles + deduplication

**Goal:** every skill change is reversible; duplicates are detected.

**Provides:**
- `internal/skills/lifecycle/versioner.go` — `type Versioner struct{...}`, `func NewVersioner(skillsDir string, logger *slog.Logger) *Versioner`, `func (v *Versioner) Snapshot(name string) (string, error)` (returns bundle SHA-256), `func (v *Versioner) History(name string) ([]VersionEntry, error)`, `func (v *Versioner) Restore(name string, version int) error`
- `internal/skills/lifecycle/types.go` (augment) — `VersionEntry` struct (Version int, ContentSHA string, Timestamp time.Time, Action string, TreeSHA256 string)
- Writer integration: `Writer.WriteSkill` calls `Versioner.Snapshot(name)` before overwriting if a Versioner is set (via `Writer.SetVersioner(v *Versioner)` with nil guard)
- Dedup: `Writer.WriteSkill` checks content SHA against `~/.meept/skills/.sha-index.json`; if match found, skips write and logs
- CLI: `meept skills history <name>` and `meept skills restore <name> --version=N`

**Consumes:**
- Phase 1's `Writer` — Versioner is injected into Writer via setter; Writer calls `Snapshot` before overwrite
- Phase 1's `WriteSkill`, `ArchiveSkill`, `RestoreSkill` — all snapshot before mutating

**Anti-completion signals:**
- `return "", nil` in `Snapshot` — must read current file + compute SHA + write version bundle
- `// TODO` in `internal/skills/lifecycle/versioner.go`
- `would run|should run|placeholder|stub` in versioner
- Absence of `internal/skills/lifecycle/versioner_test.go`
- Absence of `SetVersioner` method on `Writer`

**Behavioral acceptance:**
- Test: `go test ./internal/skills/lifecycle/... -run TestVersioner -v`
- Asserts: `Snapshot("skill-a")` when SKILL.md exists → creates `versions/v1/SKILL.md` + `versions/v1/bundle.json`, returns non-empty SHA
- Asserts: `History("skill-a")` returns entries in version order (v1, v2, ...)
- Asserts: `Restore("skill-a", 1)` reverts SKILL.md to v1 content
- Asserts (dedup): `WriteSkill("skill-b", sameContentAsSkillA)` when SHA index has that hash → returns `ErrDuplicateContent`, does not write
- Test file: `internal/skills/lifecycle/versioner_test.go`

---

**Files:**
- Create: `internal/skills/lifecycle/versioner.go`
- Create: `internal/skills/lifecycle/versioner_test.go`
- Modify: `internal/skills/lifecycle/writer.go` (add `SetVersioner`, call `Snapshot` before writes, check SHA index)
- Modify: `internal/skills/lifecycle/types.go` (add `VersionEntry`, `ErrDuplicateContent`)
- Modify: `cmd/meept/skills.go` (add `history <name>`, `restore <name> --version=N` subcommands)
- Modify: `internal/rpc/skills.go` (add `skills.history` RPC handler)
- Modify: `internal/daemon/components.go` (construct Versioner, inject into Writer)

- [ ] **Step 4.1:** Create `internal/skills/lifecycle/versioner.go`. `Versioner` holds `skillsDir` + logger. `Snapshot(name)`: read current `<skillsDir>/<name>/SKILL.md`, compute `content_sha` (SHA-256 of content), compute `tree_sha256` (SHA-256 over `path\0sha256\0size\n` for SKILL.md — single-file bundle for now), copy to `<skillsDir>/<name>/versions/v<N>/SKILL.md`, write `versions/v<N>/bundle.json` manifest `{version, content_sha, timestamp, action, tree_sha256}`. Prune oldest beyond 20 entries. Return `tree_sha256`.
- [ ] **Step 4.2:** `History(name)`: glob `versions/v*/bundle.json`, parse, return sorted by version.
- [ ] **Step 4.3:** `Restore(name, version)`: read `versions/v<N>/SKILL.md`, write back to `<skillsDir>/<name>/SKILL.md` (atomic). Does NOT call Snapshot (restoring IS the snapshot application).
- [ ] **Step 4.4:** Augment `types.go` with `VersionEntry` and `ErrDuplicateContent = errors.New("duplicate skill content")`.
- [ ] **Step 4.5:** Modify `Writer` to add `versioner *Versioner` field + `SetVersioner(v *Versioner)` method (nil guard per CLAUDE.md setter rule). In `WriteSkill`: if `versioner != nil` and skill already exists, call `versioner.Snapshot(name)` before overwrite. Compute SHA of new content, check against `shaIndex` (load `~/.meept/skills/.sha-index.json` lazily); if match, return `ErrDuplicateContent`. On successful write, update SHA index.
- [ ] **Step 4.6:** Add `history` and `restore --version=N` CLI subcommands. Add `skills.history` RPC handler. Wire in daemon.
- [ ] **Step 4.7:** Create `versioner_test.go` with tests per behavioral acceptance.
- [ ] **Step 4.8:** Run `go test ./internal/skills/lifecycle/... -v`.

---

## Phase 2: Skill evolver (refine existing + promote patterns)

**Goal:** a scheduled process that reads usage stats + learned patterns and decides what to do, gated by the Phase 3 verifier.

**Provides:**
- `internal/skills/lifecycle/evolver.go` — `type Evolver struct{...}`, `func NewEvolver(opts ...) *Evolver`, `func (e *Evolver) RunCycle(ctx context.Context) (*EvolutionReport, error)`
- `internal/skills/lifecycle/types.go` (augment) — `EvolutionReport` struct (Refined, Promoted, Pruned, Skipped counts + per-action details), `EvolutionProposal` struct
- `internal/skills/lifecycle/evolver_scheduler.go` — `type EvolverScheduler struct{...}` following `internal/selfimprove/scheduler.go` pattern (`time.Ticker` + `Start(ctx)`/`Stop()`)
- `internal/config/schema.go` — `SkillsEvolverConfig` struct added to `SkillsConfig`
- Daemon wiring: `Components.SkillEvolver` + `Components.SkillEvolverSched`; scheduler started/stopped in daemon lifecycle

**Consumes:**
- Phase 1's `UsageTracker` — `RunCycle` calls `GetAllStats()` and `GetLowPerformers(0.2, 10)` for Pass A and Pass C
- Phase 1's `Writer` — `RunCycle` calls `WriteSkill` to apply approved changes; `ArchiveSkill` for pruning
- Phase 1's `BuildSkillsPromptSectionTracked` injection list — (indirectly; the tracker already has the data)
- Phase 3's `Verifier.Verify` — every proposal (improve/create/archive) passes through `Verify` before apply
- Phase 4's `Versioner.Snapshot` — called automatically by `Writer.WriteSkill` before any overwrite
- `internal/selfimprove.LearningPipeline.Retrieve` — Pass B calls `Retrieve(ctx, "", "all", 100)` to get active patterns for promotion
- `internal/skills.CapabilityIndex.MatchWithThreshold` — Pass B checks if a pattern is already covered by an existing skill (threshold 0.7)
- `internal/plan.PlanManager.CreatePlan` — when `AutoApply == false`, proposals become plans via the existing plan system

**Anti-completion signals:**
- `return &EvolutionReport{}, nil` in `RunCycle` — must execute the three passes
- `// TODO` in `internal/skills/lifecycle/evolver.go`
- `would run|should run|placeholder|stub` in evolver
- Absence of `internal/skills/lifecycle/evolver_test.go`
- Absence of `SkillsEvolverConfig` in `internal/config/schema.go`
- Absence of `SkillEvolver` in `internal/daemon/components.go`
- `Verify(` absent from `evolver.go` (must call verifier gate)

**Behavioral acceptance:**
- Test: `go test ./internal/skills/lifecycle/... -run TestEvolver -v`
- Asserts: Pass A — a skill with inject_count=10, effectiveness=0.5, with a mock LLM returning `action: "improve_skill"` → evolver produces an `EvolutionProposal` with action `improve`, passes through verifier, if accepted calls `Writer.WriteSkill`
- Asserts: Pass B — a pattern with UseCount=10, Confidence=0.9, Status=active, not covered by existing skill → evolver produces a create proposal
- Asserts: Pass C — a skill with inject_count=15, effectiveness=0.1 → evolver produces an archive proposal; if verifier approves, `Writer.ArchiveSkill` is called
- Asserts: when `AutoApply=false` → `PlanManager.CreatePlan` called with `skill_evolution` type; no direct WriteSkill/ArchiveSkill
- Test file: `internal/skills/lifecycle/evolver_test.go`

---

**Files:**
- Create: `internal/skills/lifecycle/evolver.go`
- Create: `internal/skills/lifecycle/evolver_scheduler.go`
- Create: `internal/skills/lifecycle/evolver_test.go`
- Modify: `internal/skills/lifecycle/types.go` (add `EvolutionReport`, `EvolutionProposal`)
- Modify: `internal/config/schema.go` (add `SkillsEvolverConfig`)
- Modify: `internal/daemon/components.go` (construct evolver + scheduler, wire into lifecycle)

- [ ] **Step 2.1:** Augment `types.go` with `EvolutionReport` (counts: Refined, Promoted, Pruned, Skipped, Rejected; details []EvolutionProposal) and `EvolutionProposal` (Action, SkillName, Rationale, CandidateContent, VerifierResult).
- [ ] **Step 2.2:** Add `SkillsEvolverConfig` to `internal/config/schema.go`: `Enabled bool`, `Interval time.Duration` (default 6h), `MinInjections int` (default 5), `MinEffectiveness float64` (default 0.2 — prune threshold), `PatternPromotionConfidence float64` (default 0.7), `PatternPromotionUseCount int` (default 5), `AutoApply bool` (default false). Add to `SkillsConfig` as `Evolver SkillsEvolverConfig` field. Add defaults in `DefaultConfig()`.
- [ ] **Step 2.3:** Create `internal/skills/lifecycle/evolver.go`. `Evolver` struct holds: usage tracker, learning pipeline (as `*selfimprove.LearningPipeline`), writer, registry, capability index, verifier, llmClient, plan manager (nullable), config, logger. `RunCycle` does three passes:
  - **Pass A (refine):** For each skill in registry with `inject_count >= cfg.MinInjections`, gather usage stats + recent events + related patterns. Build LLM prompt (system: "refine existing skill" rubric). Parse JSON `{action, rationale, skill}`. If `skip`, continue. If `improve_skill`/`optimize_description`, build `EvolutionProposal`, call `Verifier.Verify`. If accept + `AutoApply` → `Writer.WriteSkill`. If accept + `!AutoApply` → `PlanManager.CreatePlan`.
  - **Pass B (promote):** Call `learning.Retrieve(ctx, "", "all", 100)`. Filter: `UseCount >= cfg.PatternPromotionUseCount && Confidence >= cfg.PatternPromotionConfidence && Status == active && CreatedAt > now-14d`. For each, check `capabilityIndex.MatchWithThreshold(pattern.Description, 0.7, 1)` — if covered, skip. Else build create proposal, verify, apply/plan.
  - **Pass C (prune):** Call `usage.GetLowPerformers(cfg.MinEffectiveness, 10)`. For each, build archive proposal, verify, apply/plan.
- [ ] **Step 2.4:** Create `internal/skills/lifecycle/evolver_scheduler.go` — port `internal/selfimprove/scheduler.go` struct pattern: `EvolverScheduler` with `evolver *Evolver`, `interval`, `stopCh`, `Start(ctx)` goroutine, `Stop()`.
- [ ] **Step 2.5:** Wire in `internal/daemon/components.go`: add `SkillEvolver *lifecycle.Evolver` and `SkillEvolverSched *lifecycle.EvolverScheduler` fields. Construct after skills+learning are initialized (~line 600). Start scheduler in `Start()` (~line 2011), stop in `stopComponents()` (~line 2700). Only construct if `cfg.Skills.Evolver.Enabled`.
- [ ] **Step 2.6:** Create `evolver_test.go` with mock LLM client, stub tracker/writer/registry. Test the three passes per behavioral acceptance. Test `AutoApply=false` path produces a plan (mock `PlanManager` or skip if nil and assert no WriteSkill called).
- [ ] **Step 2.7:** Run `go test ./internal/skills/lifecycle/... -v` and `go build ./...`.

---

## Verification (end-to-end)

**After all phases, run:**
```bash
go test ./internal/skills/lifecycle/... -v
go test ./internal/agent/... -run TestUsageTracking -v
go build -o bin/meept-daemon ./cmd/meept-daemon
go build -o bin/meept ./cmd/meept
```

**Manual smoke test (optional):**
```bash
make go-daemon
./bin/meept skills stats  # should show empty or existing skills
./bin/meept skills list
./bin/meept chat "use the debug-systematically skill to diagnose this error: foo"  # surfaces a skill
./bin/meept skills stats debug-systematically  # inject_count should increment
```
