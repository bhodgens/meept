# Bughunt Wave 2026-09-10 — branch classifier-iteration (c5e3bcf0..HEAD)

REPORT ONLY — no fixes applied. Scope: the wave since the 2026-09-08 audit's
last fix commit (`c5e3bcf0`): quickplan-mode tree (4 leaves), allotment tree
(leaves 01+02), identity-gated alias success (1336741e), ambiguity
short-circuit (be78cfb9), iters 19-20 + tfidf-veto sweep, adjudication/docs.

Shape: 5 parallel read-only auditors + parent pre-reads. Every CRITICAL/HIGH
and every MED below was parent-verified against source at HEAD `cf527d44`
(the tree moved mid-wave — see Ledger). Two auditors independently found C1.

Baseline gates:
- `go build ./...` green.
- Full suite `-p 2 -count=1` at wave start: 100 ok + 1 FAIL
  (`internal/agent [build failed]`) caused solely by sibling WIP; that WIP
  landed mid-wave as 4ba3ad41 and the refreshed gate is green:
  `go test -count=1 ./internal/agent/ ./internal/daemon/` ok (27.4s / 16.0s).
- No -race this wave (read-only audit; race gate runs before any fix wave).

## CRITICAL

### C1 — quickplan→orchestrator pipeline is dead code; quickplan turns run on a chat loop and orphan their task
- No producer of an `IntentQuickPlan` ever sets `RequiresPlanning`:
  fallback `internal/agent/dispatcher.go:1244-1250`, semantic match
  `:1211-1215` (field absent; only keyword producers set it at :2563/:2586).
- Dispatch reads the struct field, not the method:
  `dispatcher.go:2932` `it.ShouldDispatchAsync(result.Intent.RequiresPlanning)`;
  `IntentQuickPlan.ShouldDispatchAsync` returns its argument
  (`intent.go:209-210`) — so always false. The LLM classifier whitelists
  quickplan out (`llm_classifier.go:613-628`) and the prefilter verdict is
  H6-suppressed (`dispatcher.go:813`, `ShouldCreateTask()==true`), so the
  fallback/semantic producers are the ONLY live quickplan sources.
- Handler gate `handler.go:765` (`ShouldDispatchAsync(result) && result.Task != nil`)
  never opens → `route_to_agent` → `resolveAgent("orchestrator", …)` →
  `registry.Get("orchestrator")` fails (no `config/agents/orchestrator/AGENT.md`;
  fallback to chat template at `dispatcher.go:2255-2258`).
- `ShouldCreateTask()` is true → a task is created and never planned by
  anyone. Everything downstream of `publishPlanRequest` — SessionContext
  attach (`handler.go:1091`), approval bypass (`strategic.go:1299-1306`),
  orchestrator upgrade instruction (`strategic.go:923-929`) — is reachable
  only from direct unit tests. Auditor 5 proved it live with a probe test
  against real `ClassifyAndRoute` (probe deleted after).
- Fix shape: set `RequiresPlanning: true` on both producers (mirroring the
  IntentPlan keyword pattern), or make the quickplan case unconditional like
  `code`. **Fix C1 together with M2 (below) — C1 alone routes gibberish
  clarify answers into autonomous quickplan execution.**

## HIGH

### H1 — continuation prefix stacks unboundedly on re-batched waves (landed in 4ba3ad41, verified at HEAD)
- `ScheduleReadySteps` (`internal/agent/tactical.go:435-445`) runs
  `flattenWithContinuations` + `persistContinuations` on EVERY call, but the
  dep gate lives in `scheduleStep`, not `GetReadySteps` — blocked
  continuation steps re-appear in later waves and get re-batched.
- `flattenWithContinuations` (`tactical.go:560-579`) has no guard against an
  existing `[continuation` marker (grep confirms zero marker checks), and
  descriptions are persisted. Traceable with the leaf's own numbers: window
  6144 → allotment 1536 → 7×512-token steps batch 3/3/1; after batch 0
  drains, steps 3-6 re-batch 3/1 → step 6 becomes
  `[continuation 1/2] [continuation 2/3] aaaa…`. Stacks unboundedly for long
  tasks on small-window models; descriptions are user-visible and durable.
- Fix shape: skip/strip already-marked steps in `flattenWithContinuations`,
  or exclude continuation-labeled steps from re-flattening.

### H2 — tfidf-veto "wins" claim: inflated, undisclosed route count, no committed artifact
- Commit `4cf71b07` message: "ALT-5 tfidf-veto: 87.35% — BEATS the chain
  floor." Recompute from the script's own formula (`alt_methods_sweep.py:53-58`):
  0.8735 is uniquely `(2 + 46×0.868)/48` — the veto routes **2 of 48 cases
  (4.2% coverage)**; 95.8% of the score is chain credit at the stale 0.868
  constant (staleness flagged in `eval_harness.py:67-77`). The +0.55pt margin
  over 86.8% is one flipped route. Route count disclosed nowhere.
- No results artifact was committed (`4cf71b07` adds only the script; output
  was stdout-only) — 87.35/84.56/76.07/79.13 are unreproducible from git.
  Comparison itself is apples-to-apples (same 48 cases, same denominator).
- Champion-vs-shipped is clean: shipped prefilter has zero tfidf code;
  `docs/workflows/classification-architecture.md:137-147` honestly marks it
  "queued — not shipped yet."

## MEDIUM

### M1 — analyzer can essentially never emit category "quickplan"; PendingMode seeding is near-dead
`intent_analyzer.go:150-161` system prompt lists six categories (no
"quickplan") and four modes (no "quick_plan"); `validCategories`
(`:339-345`) accepts quickplan but the model is never told it exists. The
leaf-02 clarify-seeding path (`dispatcher.go:1433-1435`) fires only if the
model spontaneously emits an unlisted value.

### M2 — the "still ambiguous after clarification" follow-up branch is dead; the gate's own record destroys its precondition
`buildClarificationResult` now records the clarify intent
(`dispatcher.go:1462-1469`, new in d3ace332); `ResumeAfterClarification`
rebuilds the digest AFTER that record (`:1539`), so `LastIntentType=="clarify"`
makes `IsEmpty()` false and the A5 gate (`:1547`) can never re-fire — for
exactly the context-less sessions clarification exists for. A gibberish
second answer no longer gets a follow-up question; it routes (into chat
while C1 stands; into autonomous quickplan once C1 is fixed).
Fix shape: exclude the clarify marker from emptiness on the resume path.

### M3 — quickplan resume through routeToPlan drops the mode and strands the plan
`dispatcher.go:1586-1588` / `:948` can route a quickplan intent into
`routeToPlan` (`:1390-1416`) when plans config says always-plan. The result
carries no `SuggestedMode`, and its "plan created" response terminates as a
direct_response (`handler.go:712`) — no `orchestrator.plan` published, draft
plan dormant, preserved `quick_plan` mode gone.

### M4 — clarify-resume re-classifies a truncated original input
`getPendingClarification` rebuilds `OriginalInput` from
`lastIntent.Summary` = first sentence / 100 chars (`dispatcher.go:3332-3340`,
used at `:1642-1661`). The resume contract funnels the truncated string into
the re-analysis and the executed plan (`combinedInput` becomes `req.Input`).
Pre-existing, now load-bearing for quickplan resume.

### M5 — train/test contamination: replay case in the training corpus with a conflicting label
`testdata/eval/classifier-adversarial-corpus.json5:165`
"implement the plan using subagents" → `code` (iteration-18) vs adjudicated
replay → `quickplan`, contradicting adjudication rule #3
(`results/adjudication-record.md:13-15`). Sits in every training set
(stage-A centroids, B probe, tfidf `clf.fit`). Anti-inflation direction, but
falsifies iter-19's "dedup vs replay-gold verbatim text" claim
(`results/iter-19/report.md:57-58`).

### M6 — iter-20 headline row is internally inconsistent; 83.97% is selection-on-test
- `results/iter-20/report.md:13` + commit `820c016f` say "B 13/10";
  recompute (5+10+28×0.868)/48 = **0.8188**. The JSON's 0.8397 requires
  **B 13/11** — one of the two is a transcription error (every other row
  recomputes exactly).
- `iter20c_sweep.py:144-152` sweeps 13 tau×guard policies on the same 48
  replay cases and headlines the max — no held-out split — and the v2b guard
  regex (shipped verbatim as `quickplan_cue.go:17-24`) is largely verbatim
  n-grams of the training anchors.

### M7 — centroid builder default corpus ≠ eval corpus; runtime index unestablished
`scripts/build_prefilter_centroids.py:31` `DEFAULT_CORPUS =
classifier-test-corpus.json5` (base only, 139 cases, zero quickplan) while
every eval number uses base+adversarial (369). A default rebuild produces a
runtime index the eval never measured. (`~/.meept/classifier_prefilter_centroids.json`
does not exist and the prefilter is config-default-disabled — currently
inert, so no runtime number inherits the claims.)

### M8 — hintAgentFallback diverges from the authoritative hint table (landed in 4ba3ad41, verified at HEAD)
`tactical.go:536-548` maps `debug→coder`, `git→coder`, `research→analyst`,
`review→coder`; `config/tool_hints.go:54-65` maps `debug→debugger`,
`git→committer`, `research→researcher`, and has no `review` entry. A debug
step's batch is sized by the coder's context window instead of the
debugger's. The comment claiming `selectAgent` "needs a receiver and may
log" is inaccurate — it is a pure table lookup. Call `config.ToolHintAgent`.

### M9 — mixed-agent waves sized by one agent's window
`allotmentAgentID` (`tactical.go:518-532`) takes the first step's AgentID
for the whole wave; `assignStepAgent` documents heterogeneous explicit
AgentIDs (pair actor/reviewer). Batch sized on agent A can overflow agent B's
smaller window. No guard, no test.

### M10 — dead allotment math in the committed tree pre-4ba3ad41; now wired but untested at the boundaries
Leaf 01 shipped alone (a4c1744e) with zero production callers until the
sibling's mid-wave 4ba3ad41 wired it. Residual (LOW-MED): `allotment_test.go`
never tests `MaxBatchSteps > 0`, exact-fit boundary, allotment <
MinStepTokens, negative allotment, or the `MinStepTokens=0` zero-cost
degenerate.

## LOW

- L1 `modeToLabel` has no `quick_plan` case (`handler.go:1495-1507`) —
  approval-free quickplan ACKs display "mode: planned".
- L2 `suggestReasoningForIntent` omits quickplan (`dispatcher.go:671-684`).
- L3 magic string `first == "quickplan"` (`embedding_prefilter.go:263`)
  instead of `string(IntentQuickPlan)` — a constant rename silently kills
  the cue guard.
- L4 `Dispatcher.SetPlanManager` missing from `setters_test.go` nil-guard
  regression set (`setters_test.go:87-91` covers Orchestrator/RalphLoop only).
- L5 `loop.go:5021` stays alias-wide although `servedModel` is provably
  non-nil there (sibling failure sites pass it). Commit rationale "serving
  config may be nil" is factually wrong for this site. One-line follow-up.
- L6 dead nil-check in `allotmentAgentID` (`tactical.go:524-525`): `.ToolHint`
  is dereferenced on the line above the `ts != nil` guard.
- L7 `AllotmentCfg` zero-check means single-knob overrides silently revert
  every other field to defaults (`tactical.go` NewTacticalScheduler).
- L8 quickplan wave count rides the 30-min-TTL in-memory sessionTracker
  (`session_digest.go:211-224`) — resets on restart while plan/task sections
  survive in SQLite. Also `handler.go:1092` uses `context.Background()`.
- L9 cue-regex false positives (verified by pattern-run): "tasks 3 and 4
  mean in the roadmap", "sort these files in order of size", "pull the
  plan.md from the drive", "carry out the trash", "raking leaves is autumn
  work", "do A, then B". Cue-suppressed votes are log-indistinguishable from
  mixed neighborhoods (`embedding_prefilter.go:350-354`).
- L10 keyword classifier hijacks explicit quickplan phrasing to IntentPlan
  (interview + approval gate) whenever the LLM chain fails:
  `dispatcher.go:2503` keyword table has no quickplan row and `Classify` is
  `strings.Contains` best-match — "carry out the plan, no check-ins" scores
  as `plan`.
- L11 `createFallbackSteps` stamps `ToolHint="quickplan"` (`strategic.go:1072`),
  which has no `toolHintAgent` entry → fallback steps deflect to chat — the
  exact class 05e60e11/7ae0a2ec fixed for every other hint.
- L12 ambiguity-gate unit test pins a local copy of the gate expression, not
  the code (`dispatcher_ambiguity_gate_test.go:60-63,88-90`) — a regression
  in the real gate cannot fail these tests.
- L13 iter-19/20 reports undercount corpus by one ("314→338→370" vs actual
  313→337→369); `q1-q4-response.md:10` labels the v2c row "tau 0.147" (old
  iter-17 tau; v2c is 0.152); iter-19 JSON omits the misses list the report
  narrates.
- L14 INFO: `modelConfig` on LLMClassifier/IntentAnalyzer is unsynchronized
  (pre-existing; the identity gate adds readers, not writers).
- L15 INFO: replan paths (`strategic.go:1296-1300`, `escalation.go:275-280`)
  bypass quickplan — failed quickplan tasks replan through legacy `plan`.
- L16 INFO: `requiresApproval`'s quick_plan PlanningCtx check is
  dead-but-harmless (the branch never runs an interview).

## Explicitly verified clean

- Resolver identity gate (1336741e): all 4 identity-bearing call sites pass
  the same model as RecordAliasFailure; lazy-clear contract intact (sweeps
  delete only expired entries, identity-independent); zero new AliasHealth
  fields; rotation fully under `r.mu`; quota/endpoint sentinel errors and
  precedence untouched; `internal/llm` suite green.
- Ambiguity gate A5 (be78cfb9) itself: digest nil-guards, WorkingDirectory
  excluded from IsEmpty, degrades to pre-tree behavior when stores fail.
- Allotment pure math (allotment.go): division/zero/negative guards, greedy
  fill, oversize solo batch, order preservation, determinism — all pinned.
- Eval convention parity: every wave script uses the baseline
  `e2e = (correct + CHAIN_BASELINE×indirect)/total` over full n=48;
  chain credit is a constant, never a draw; torch seeds pinned; every
  committed headline hand-recomputed from committed stage counts (iter19
  0.7941 ✓, m4-silver 0.8468 ✓, v2 0.6753 ✓, v2b 0.8369 ✓).
- Fold cache: 314 cached folds formula-consistent, zero drift since
  c5e3bcf0; +56 new cases uncached but unconsumed (deterministic on next use).
- Intent-method completeness: quickplan registered in every
  switch/enum/table that routes intents (Category, SuggestedMode,
  DefaultAgent, RequiresPlanning, ShouldCreateTask, ShouldDispatchAsync,
  SteeringHeuristicTable, IsValidIntentType, SemanticIndex, validModes);
  suggestMode never short-input-downgrades it.
- Prefilter embedding math: zero-norm guards, dimension checks, k-shortage,
  selfMatchCutoff, margin init — sound; concurrency per mutexio rules.
- session/conversation id handling in the new session-context block: keyed
  on the handler's conversationID consistently; WS fallback happens upstream.
- Message-pairing (guard-insert severing): no new in-loop conversation
  mutations; dedup-vs-writer-transform: cue guard evaluates raw input only.
- plan/manager.go GetPlansForSession hunk: indexed query, ordered
  updated_at DESC matching the consumer scan; single implementor.

## Promotion-claim verdicts

| Claim | Verdict |
|---|---|
| iter-19 "79.4% on adjudicated gold replay" | convention-parity OK; honest regression framing; caveats: uncommitted ruler (gitignored), 1 contaminated train case (M5), n=48 ±1 case ≈ ±2.2pt |
| iter-20 "plateau at 84% replay" | INFLATED (mildly) / INTERNALLY INCONSISTENT — max-of-13 selected on the eval set (M6), B 13/10 vs JSON mismatch (M6) |
| tfidf-veto "wins" | INFLATED + UNVERIFIABLE — 2/48 routes + chain credit; no artifact (H2); shipped code claims nothing |

## Mid-wave tree movement (re-verification note)

The sibling session committed 4ba3ad41 + cf527d44 (allotment leaf 02) while
auditors ran. Auditor 2 audited the leaf-02 diff in working-tree form; three
of its findings were resolved by the landing (test build break,
ExecutorModelRef field, daemon context-window provider — now in
components.go) and the off-by-one was settled as N = len(batches) per the
commit message. All remaining findings were re-verified by the parent at
cf527d44 (C1, H1, M8, M9, L6, L7 grep+read at HEAD; refreshed
agent+daemon gates green). The build break that appeared in the wave-start
baseline was attributable to that same unlanded WIP and no longer exists.

## Disclosure ledger

- Tree moved mid-wave (2 sibling commits); every finding re-verified at
  final HEAD `cf527d44` before this report. Late-report staleness:
  none outstanding.
- Auditor 5 created and deleted two transient probe test files inside
  internal/agent/ to prove C1 live; `git status --porcelain -- internal/`
  verified clean afterward (pre-existing untracked `internal/*/docs/` only).
- Auditor 4 wrote one scratch script to /tmp (outside the repo).
- Working-tree modifications NOT touched by this wave (sibling property,
  left as-is): `tools/classifier-eval/results/m4-silver/cascade-validation.json`
  (a 13-class re-run overwriting the committed iter-17 artifact — see L13/
  auditor-4 F11; evidence files should not be silently rewritten),
  `config/agents/coder/AGENT.md`.
- NOT audited this wave: meept.dev/index.html (site fix ba961ef7),
  gitignore/adjudication doc commits, classifier-eval scripts belonging to
  the prior audited wave (iters 1-18), untracked scratch/ + docs/plans
  working-tree files, .claude/worktrees/ stale twins (noted by auditor 3).
- No -race run (read-only wave; race gate belongs to the fix wave that
  follows any go-ahead).
- Baseline at wave start: 100 ok + 1 attributable sibling-WIP FAIL; final
  refreshed gates: agent + daemon -count=1 green at HEAD.

## Fix-order note (if authorized)

C1 and M2 MUST land together or in that order: C1 alone (RequiresPlanning
set) turns the currently-benign dead-follow-up branch (M2) into "gibberish
clarify answers execute autonomously." H1 should land before any production
use of small-window models. M8 is a two-line table fix with a pin test.
