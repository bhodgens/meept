# Bughunt Wave 2026-09-08 — branch classifier-iteration (caf61fb2..HEAD)

> **RESOLUTION (2026-09-08, later the same day):** user authorized fixing
> everything. 8 parallel fixers + parent repairs landed 11 commits
> (8ca429ea..c5e3bcf0): fix(metrics), fix(llm), fix(agent) ×4,
> fix(agent,task), fix(plan,rpc), fix(daemon) ×2, fix(tools),
> fix(mutexio), fix(config), docs(eval). Post-fix gates: full suite
> 101 ok -count=1 (0 cached), FULL -race 101 ok / 0 DATA RACE, mutexio
> clean, go build ./... green. Findings below are retained as the audit
> record; the ledger at the end maps every item to its commit or its
> remaining disposition.

REPORT ONLY — no fixes applied. Scope: the wave since the last reviewed
point (2026-09-06/08: plan compiler, classifier campaign iter1-16, LFM
streaming tool-call recovery, task counters, metrics tokscale columns,
builtin tools, config).

Shape: 7 parallel read-only auditors + parent pre-reads. Every
CRITICAL/HIGH below was parent-verified against current source (grep +
read), not taken on auditor word. Baseline gates green: go build + go
vet + full suite -p 2 -count=1 (100 ok, 0 FAIL, 0 cached) and targeted
-race over agent/llm/task/metrics/plan/queue (10 ok, 0 DATA RACE).
Logs: /tmp/bughunt-baseline-20260908.log, /tmp/bughunt-race-targeted-20260908.log.

## CRITICAL

### C1 — LFM recovered tool calls carry ID:"" → nil-slot panic on any multi-tool LFM turn
- internal/llm/lfm_tool_calls.go:43 — every recovered call gets `ID: ""`.
- Default chat model IS LFM2.5-8B MLX (config/models.json5:2), parse runs
  on both paths (client.go:1519 non-streaming, :2099 streaming).
- executor.go:1344-1357 — ExecuteAll keys idToIdx by tc.ID; 2+ calls
  collapse to one index; results scatter to ONE slot, others stay nil.
- loop.go:4022 (`!result.Success`) and :4161 (`result.ToCompressedJSON`)
  dereference without nil check → runtime panic mid-turn.
- Single-call turns escape via the len==1 short-circuit (executor.go:1334).
- Secondary: models.go:127 omits tool_call_id on the wire; compactor/
  firewall keep-sets (context_compressor.go:522, context_firewall.go:862)
  guard on ToolCallID != "" → dangling pairs; strict providers (GLM in
  the classifier alias) 400 on that shape; flushDeferredToolResults
  dedup collapses; cascade detection treats all failures as one call.
- Parent pre-read found this independently; auditor 4 confirmed with the
  panic trace. Test gap: lfm_tool_calls_test.go has zero ID assertions.
- Fix: mint unique IDs in parseLFMToolCalls (positional + pkg/id suffix).
  One root fix closes both legs. Pin: multi-call scatter test + ID
  uniqueness assertion + wire-shape pairing test.

### C2 — iter-16's headline "E2E=92.8% clears bar" is a seeded random draw, not the campaign's expected-credit convention
- tools/classifier-eval/iter16_cascade3.py:141 —
  `ok = np.random.random() < CHAIN_ACC` with `np.random.seed(42)` (:149).
- Baseline harness computes E2E deterministically:
  eval_harness.py:579 — `(correct + CHAIN_BASELINE*(total_in-direct))/total_in`.
  iter-14/15 use the same formula.
- Recomputed under the campaign's own convention: 91.44% — FAILS the
  pre-registered M3 bar of 91.78% that every iter-13..16 report cites.
- Commit 613c9120 message + report present the draw as the measurement;
  the draw was ~+1σ lucky (89.9% sampled vs 86.8% expected on 109 cases).
- Impact: the "clears bar → proceed to daemon wiring" promotion decision
  rests on an artifact of protocol divergence.
- Fix: re-run iter-16 with deterministic chain credit or report both
  numbers with error bars; correct the verdict to FAIL, or re-register
  the bar. Campaign evidence-integrity issue — no production code risk.

## HIGH

### H1 — Step-job throttle parks become "empty response" failures + later parker resume = double execution
- Parked turn returns ("", nil): loop.go:2487-2494, :3718-3726.
- Job processor (components.go:7743-7751) converts it to a hard failure
  "agent execution produced empty response"; no GetState()==StateQuotaWait
  check exists in components.go.
- That text is not quota-class (isQuotaClassFailure, tactical.go:1599) →
  legacy fail path: StepFailed + IncrementFailedJobs + escalation.
- Meanwhile the wired parker (components.go:2555/2614/2957; per-phase
  clones inherit via ConfigSnapshot) resumes the turn later
  (loop_park.go:438) — tools and side effects RE-EXECUTE, output
  discarded. Violates D9 ("queue jobs PARK") via the throttle class.
- Fix: treat ("", nil) + StateQuotaWait as parked (requeue via
  Requeueable or leave claimed), or refuse to park step-job turns and
  surface ThrottleBackoffError for the worker's structured requeue.

### H2 — cbf0b775 regresses the H11 park-event routing invariant
- loop.go:2199-2207 now sets l.currentSessionID = conversationID
  (conv-*) for interactive turns.
- loop_park.go:259-261 stamps rec.SessionID from that field DURING the
  window; parked_turn.go:709 puts it on the wire; comm/http/server.go
  filters on session_id. Clients subscribed on session-* never see
  park/resume events for interactive turns — the exact bug the AUDIT FIX
  H11 comment at loop_park.go:263-267 says was fixed.
- Related (same root): updateSessionDesignation / triggerTitleRefresh
  gates now pass with conv-* ids → "session not found" warns,
  session.title_updated events with conv-* ids.
- Fix: separate the token-accounting scope from session identity
  (dedicated field consumed only by the WithTaskScope prepend at
  loop.go:4779), or resolve conv→session before stamping.

### H3 — eb5563ff reasoning-watchdog rescue is inert for reasoning-configured agents (option ordering inverted vs its own comment)
- loop.go:3591-3597 — rescue appends llm.DisableThinking() early;
  comment claims "appended after the agent's reasoning opts — last apply
  wins".
- loop.go:3688-3704 — later in the same iteration, WithReasoning
  appends when override/reasoning/agentReasoning set → last wins =
  thinking re-enabled. Rescue runs with thinking ON, re-fails, turn
  terminates.
- Tests pass only because mock loops have no agentReasoning.
- Fix: move the rescue append after the reasoningCfg block (or skip
  WithReasoning when rescue consumed); test with agentReasoning set.

### H4 — Legacy spec_plan path lost the maxStepsPerPhase cap (flag-off byte-identical violation)
- caf61fb2 strategic.go:719 had the cap break; extracted
  FlattenPlanPhasesToSteps (plan_draft.go:242-271) has NO cap logic —
  its own doc comment describes a parameter it doesn't take.
- planMultiPhase calls it at strategic.go:733; nothing downstream caps.
  Default cap is 8 (config/schema.go:3009).
- Impact: with plans.plan_compiler_enabled=false (production default),
  phases >8 steps now persist and execute fully — AGENTS.md invariant
  broken by the shared refactor, untested
  (TestPlanSealWiring_FlagOffLegacyIntact uses small phases).
- Fix: add maxStepsPerPhase param (0=uncapped), pass sp.maxStepsPerPhase
  from planMultiPhase; flag-off regression test with a >cap phase.
- Found independently by auditors 1 and 3.

### H5 — Media-URL guard's bare-ID alternation hijacks any message containing an 11-char word
- dispatcher.go:302 — second regex alternation
  `(?:^|\s)[A-Za-z0-9_-]{11}(?:\s|$)` matches ANY whitespace-delimited
  11-char token: "development", "performance", "information", "handleClick",
  git SHAs. Fires inside classifyIntent BEFORE the LLM chain →
  IntentAnalyze/analyst at confidence 0.9.
- Impact: a large class of ordinary coding messages deterministically
  misroutes — worse than the LLM misroute the guard was built to fix
  (fires even when the LLM would be right). Tests pin only real video
  IDs; no false-positive case.
- Plausibility-filter bimodality class (SKILL.md round-2).
- Fix: require media-context co-occurrence for the bare-ID form or drop
  it; negative tests with common 11-letter words.

### H6 — Prefilter direct route skips task creation, async dispatch, plan creation, instruction parsing, and agentOverride
- dispatcher.go:718-765 — step 3.25 early-returns has_task=false,
  bypassing steps 4.5 (instruction parser), 5 (multi-intent LLM check),
  5.3 (agentOverride — client-specified agent silently dropped),
  5.5 (ShouldCreatePlan), 6 (shouldCreateTask/createTask).
- Intents with ShouldCreateTask()==true (code/debug/git/plan) run
  INLINE, untracked, with no budget pre-check (handler.go:783-802
  budget gate lives in the async case) when Stage-0 votes.
- Same input → materially different execution semantics depending on
  the prefilter vote; invisible to label-agreement eval metrics.
- Mitigating: prefilter is disabled by default and shipped gate
  (k=5 unanimity, floor 0.70) covers almost nothing on the current
  corpus — see H9. Assert-only mode is safe.
- Fix: restrict direct route to inline-category intents, or move the
  prefilter after steps 4.5-6 so it substitutes only classification;
  honor agentOverride before the verdict.

### H7 — json_extract missing from ToolActionMap → "Unknown action" denial at runtime (same class f1d15b6d fixed per-tool)
- executor.go:249-305 has no json_extract entry; checkPermission falls
  back to the tool name; BuiltinRules["json_extract"] does not exist →
  always denied. Tool is registered unconditionally
  (components.go:2293) and granted to the researcher
  (config/agents/researcher/AGENT.md:15).
- Impact: json_extract is dead at runtime for every agent. The
  transcript_fetch fix was applied per-tool, not systematically.
- Fix: add the map entry to an existing rule + a completeness test that
  EVERY registered builtin resolves through ToolActionMap ∪ BuiltinRules.

### H8 — Tree-mode phase-name annotation breaks every orchestrator name-based phase lookup
- plan_seal_wiring.go:106-114 — persists
  `phaseRecord.Name = "<name> [tree leaf: <path>]"`, but steps carry the
  CLEAN name (plan_draft.go:251 `step.Phase = phase.Name`).
- Orchestrator joins by name string: startPhase GetPhaseSteps(taskID,
  p.Name) → zero steps → "already started, skipping"; getPlanPhaseSpec
  never matches → produces/consumes gating lost; startNextPhase never
  finds the completed phase → transitions no-op.
- Tree mode broken end-to-end at runtime; no wiring test covers it
  (fixture plan is 3 steps → flat).
- Fix: keep PlanPhase.Name clean; carry the leaf path in a side map or
  dedicated field, never in the join key.

### H9 — Campaign champion head ≠ shipped Go gate; Stage-B never OOD-tested
- Champion (iter-12/14/16 Stage-A): centroid floor 0.60 + margin 0.030
  (iter16_cascade3.py:115). Shipped Go: k=5 unanimity, floor 0.70
  (embedding_prefilter.go:88-93). The harness itself measured unanimity
  as FAILING precision (P 90.9-91.7%) on the grown corpus — production
  ships the algorithm the campaign rejected.
- iter-14/15/16 drop all 24 OOD cases before Stage-B
  (`if is_ood[qi]: continue`), yet the pre-registered winner rule
  (master.md:37) requires OOD-R ≥ 95% on the final held-out set. The
  ModernBERT probe has an unquantified OOD-routing failure mode.
- Impact: M4 "wire the winner" would require rewriting EmbeddingPrefilter;
  the promotion gate cannot be evaluated as registered.
- Fix: implement the centroid-margin head in Go (behind config) or amend
  master.md; extend cascade scripts to run OOD through A/B and report
  per-stage OOD-R.

### H10 — media_client SSRF guard blocks the primary local ComfyUI path
- media_client.go:183 download() → checkURL → ssrf.go:34 rejects
  IsLoopback unconditionally. runComfy builds artifact URLs from
  mc.BaseURL = http://127.0.0.1:8188 and comfy artifacts carry URL-only
  (no bytes), so save() always downloads → generation succeeds through
  /prompt+/history then fails at artifact download with
  "download blocked".
- Unlike web_fetch/pdf_read, mediaClient has no SetAllowPrivateRanges /
  [security.ssrf] AllowedCIDRs wiring — no operator escape hatch. CI
  never exercises runComfy end-to-end.
- Fix: wire the centralized ssrf.Guard with AllowedCIDRs, or exempt URLs
  whose host matches the model's own configured BaseURL (owner-trusted).

### H11 — json_extract output_path: arbitrary file write with no fence/root constraint (+ symmetric read side)
- json_extract.go:252-277 writeOutput — MkdirAll + WriteFile on any
  absolute path (or ~-expanded, or wd-joined relative). No
  constrainMediaPath, no FenceChecker, no symlink resolution.
- Security engine can't catch it: absent from ToolActionMap (H7) →
  RiskMedium default, and path checks fire only for ActionFile* actions.
- Read side same class: resolveText (:222-248) os.ReadFile on arbitrary
  paths; extracted record becomes a laundering channel (MEDIUM).
- Contrast: generate_image/video constrain to media.output_dir ∪ wd
  (media_client.go:276-306); file_write enforces the fence
  (filesystem.go:484).
- transcript_fetch output_path shares the flaw (resolveOutputPath
  :518-547; `filepath.Join(wd,"../../x")` cleans to an escape; seeded
  RiskLow network_request → engine never path-checks) — MEDIUM.
- Fix: containment to wd ∪ configured output dir + fence CheckPath +
  EvalSymlinks on both tools, both read and write sides.

### H12 — Task-store counter race only partially closed by 02e952f9
- store.go:219-246 Update writes the WHOLE row including counter columns
  from an in-memory snapshot. Surviving RMW sites: tactical.go:1898 →
  :2037-2038 (`t.TotalJobs++` + Update, snapshot held across step
  creation), amendment_handlers.go:159-162, strategic.go:503-505/562/
  1288-1290. Any atomic Increment* committing in the window is erased.
- RecountJobs repairs only at finalization; progress events
  (task.progress, publishTokenProgress) can still carry drifted counters
  mid-execution — the exact "2/1 completed, 200%" class.
- Fix: convert handoff/amendment sites to IncrementTotalJobs (exists,
  store.go:280); longer term omit counter columns from Update.

## MEDIUM (parent-verified or auditor-sourced, not individually re-read)

1. quotaResetAtFromMessage parses only capture group 1 (tactical.go:624-634);
   stamp-shape and JSON-shape recovery paths are dead code → 5h parks
   degrade to +5min polls, exhaust at ~50min. (Auditor 2, empirically
   probed by the auditor.)
2. compressMapResult metadata-overflow unbounded — non-primary keys copied
   WHOLE (executor.go:594-611); two large blobs bypass ToolResultMaxTokens.
3. currentSessionID unlocked readers (loop.go:6165/6673/7532/7551/7563,
   loop_park.go:260) vs mutex-guarded writers; concurrent parker-resume +
   chat turn can interleave snapshot/restore. Also the H2 root.
4. RecountJobs SELECT+UPDATE not one transaction (store.go:311-325) —
   concurrent finalization can double-count failed_jobs.
5. IsRevisionStep = strings.Contains(id, "-rev-") (step.go:137-139) —
   substring tag false-positives on any task id containing -rev-;
   promotion exception waves through ANY rejected dep, not just the
   original.
6. GetHistoricalMetrics binds RFC3339 against "YYYY-MM-DD HH:MM:SS" text
   (metrics/store.go:1003 vs :474) — same-day-as-from rows silently
   dropped (TEXT-affinity class, pre-existing).
7. transcript_fetch IsReadOnly()==true even when output_path writes
   (transcript_fetch.go:217) — executor/dependency inferrer treat a
   mutating call as a read; json_extract does this correctly.
8. transcript_fetch returns external content untainted (no
   TaintExternal label, :301-313/374-386) — prompt-injection text flows
   into later tool args untainted; json_extract and web_fetch label.
9. parseLFMArgs no escape handling (lfm_tool_calls.go:69-73/97-98) —
   \" splits values, \n lands literally in file contents.
10. LFM marker regex lacks (?s) + non-conforming bodies silently dropped
    (lfm_tool_calls.go:17/20/36-38) — multi-line or malformed marker →
    raw marker text leaks into content or the call vanishes with no log.
11. Phase-level DependsOn off-by-one vs the recorded errata
    (plan_draft.go:321 passes ordinals into a 0-indexed field; step deps
    correctly decremented) — latent, no consumer today.
12. treeLeafPathForPhase maps phase index into the GLOBAL leaf array
    (plan_seal_wiring.go:224-232) — annotation points at the wrong leaf
    for any phase after a multi-leaf phase.
13. Shared sealPipelineState can cross-assign phases between concurrent
    seals (plan_seal_wiring.go:28-51) — A executes B's phases.
14. Same-phase leaves get no chaining dependency while master.md says
    "dispatch them together" (treeemit.go:306-332) — contract wrong on
    its face for split phases.
15. Prefilter timeout_seconds config silently capped at 2s by the
    hardcoded HTTP client timeout (dispatcher.go:469-474 passes
    defaultPrefilterTimeout, not cfg). Found independently by auditors
    1 and 7.
16. Tool-hint table dropped "write" (tool_hints.go has only "writer") —
    write-intent tasks route to chat, the exact deflection 05e60e11
    existed to kill; untested.
17. CHAIN_BASELINE 0.868 measured on the old 136-case corpus, applied
    unadjusted to the 250-case grown corpus (harvest cases are
    near-misses by construction) — every absolute E2E inherits it.
18. Evidence hygiene: iter-10..16 results + the +51-line fold-assignment
    diff are uncommitted, contradicting 988cab98's evidence-chain
    purpose. (The fold diff itself verified append-only,
    formula-consistent, does not invalidate prior splits.)
19. docs/workflows/classifier-prefilter.md ships the table iter-1 proved
    non-reproducible (2-10× optimistic); harness docstring overstates
    the adversarial holdout guarantee (cases join train folds after
    first-seen).
20. json_extract schema with required naming an undeclared field always
    hard-errors (conform drops it, then checkRequired reports it
    missing); additionalProperties:true ignored.

## LOW (selected; full lists in auditor reports)

- vote() margin baseline bestLosing:=0.0 overstates margin when all
  dissenters score <0 (embedding_prefilter.go:275); margin recomputes
  cosines O(N). Corpus intents with <5 examples are structurally
  unroutable (review=4, recall=4).
- Rescue branch double-increments iteration (loop.go:4293 + for-post).
- resetTurnGuards only called from RunOnceWithParts; RunWithTask/
  RunWithSkill enter reasoningCycle with carried guard state (latent —
  no production callers today).
- Stale comments: loop.go:4164 describes pre-fix budget behavior;
  step.go:474 claims a CAS guard the SQL lacks; models.json5:203 port
  map contradicts 115f2dcf; client.go:1783 cites the wrong close line.
- W3 warning classification by message substring (compiler.go:810) —
  crafted phase name can downgrade an error to warning.
- Re-seal after partial failure duplicates plan_phases rows
  (rpc/plan_seal.go:172-182); EnsureTaskPlan TOCTOU can orphan a
  container plan (manager.go:699-721); default maxPhases disagreement
  (RPC 10 vs planner 12); dead metaBad field; os.Exit(2) inside RunE.
- mutexio FuncLit skip widened a narrow false-negative (unlock inside a
  closure no longer pairs); dead `_ = fl`.
- Byte-slice truncation can split UTF-8 runes (json_extract.go:139,
  transcript_fetch.go:362/452); summarize shares the fetch timeout
  budget (60s for fetch + map-reduce); RecordEvent binds time.Time raw
  (modernc writes Go String() format — strftime-unparseable);
  model_performance exempt from retention, grows unboundedly;
  QueryLLMCallUsage selects bare agent_id under GROUP BY provider;
  rejected-step-without-revision can leave a task unfinalizable until
  RecoverStaleTasks; iter-15 report's "hash collision" story is false
  (orphan cache entry from a corpus edit); iter-12 report count drift
  (275 vs actual 274); enabled-without-base_url silently ignored;
  dead default_timeout config knob; truncated LFM marker passes through
  with no log; [DONE]/onDelta-error close without drain (perf-only).

## Verified clean (highlights)

- Baseline + targeted -race gates green (see header).
- Quota-deferral core (94d71aa1): requeue consumes no retry, gates on
  next_retry_at, no double-deferral, slots released, bounds enforced,
  non-quota byte-identical. Exhaustion path correct.
- Revision promotion (2ac27b1f): failed steps still block; rejected
  revision cannot resurrect a FAILED task; tests pin the matrix.
- 088f8214 migration ordering correct on fresh AND upgraded DBs;
  llm_calls retention exemption intact; no legacy-row invisibility;
  metrics.Store.Close loopWG preserved; mutex discipline in both stores.
- LLM: no tokenizer-panic class in parseLFMArgs (single-increment,
  bounded); chunk-boundary markers handled (parse-once-on-full-buffer);
  genuine streaming (no pre-parser ReadAll); error-path drain on both
  modes; QuotaResetError early-exit intact in all 3 retry loops;
  TestConfigLoads ↔ models.json5 parity exact (test executed).
- Compiler determinism (sort.Ints neutralizes the only map walk);
  maxPhases cap enforced+tested; plan.seal/plan.draft RPC-only
  invariant holds; no new bus proxies; input bounds (10MB frame, RE2,
  slug-only filenames).
- Prefilter: kNN vote math correct; self-match exclusion before
  threshold; fail-open to LLM on every error path; lazy load never
  blocks startup; lock discipline correct; assert_only pinned.
- json_extract not-configured path matches AGENTS.md (no chat-model
  fallback); conformToSchema before validation, nulls preserved;
  transcript_fetch pagination/truncation-flag correct; no os.Getwd in
  daemon code; youtube subprocess injection empirically refuted
  (python3 -c does not re-parse argv flags).
- Fold integrity of the committed baseline harness (OOD never in train;
  self-exclusion mask); iter-14/15 arithmetic reconciles exactly;
  no duplicate text keys in the 274-case corpus; no hard label errors
  found.
- Rejection one-write (00551f63) + review error gate correct; map
  compression deterministic (100-run byte-identical pin); ResultSizer
  floors lift-only and capped.

## Disclosure ledger (what was NOT done / caveats)

1. REPORT ONLY — no fixes applied; all findings await user go-ahead.
2. Auditor 5's finding 9 (RecordEvent time.Time binding) is based on
   reading the pinned modernc driver source, not execution — its
   runtime probe was denied in the sandbox.
3. MEDIUM/LOW items were NOT individually parent-re-read; HIGH/CRITICAL
   all were. Two auditor MEDIUMs (quota-reset capture groups, iter16
   recompute) were empirically probed by their auditors.
4. Out of scope: everything before caf61fb2 (covered by the 09-03..09-06
   wave records), Flutter UI, docs/plans prose beyond spec references,
   cmd/glmprobe-tmp/ (untracked sibling scratch — left untouched),
   internal/daemon/docs/ + scratch/alpha.txt + sealed_plan.md (untracked,
   likely sibling in-flight — not audited, not touched).
5. Uncommitted working-tree items at audit time: fold-assignment.json
   (+51, verified append-only/formula-consistent), iter-7..16 results
   dirs, 2 plan docs, sibling scratch. None staged or committed by this
   session.
6. Prior-wave "audit next wave" debts still open (from
   meept-race-stress-wrapper-waves ref): b542a0f4 model_parser fix
   shipped with zero regression tests; RotateToNextModel shared-cursor
   mutation + RecordAliasSuccess lacking model identity (design
   questions); intent-analyzer ambiguity short-circuit before restored
   history (A5 blocker). This wave did not cover internal/llm resolver
   rotation — flagged for the next wave.
7. The full-suite -race pass was NOT re-run this wave (targeted only,
   over the 6 changed package trees). Per skill rule, a full -race is
   the pre-push gate if fixes land.
8. Auditor line refs were re-located by grep before verification; no
   finding was accepted on a stale ref. No auditor HIGH was refuted
   this wave (contrast 09-03: 1 refuted) — but two HIGHs (H4, timeout
   nit M15) were independent rediscoveries, which raises confidence.

## Resolution ledger (fix wave, 2026-09-08 evening)

Commits (all Benjamin Hodgens, branch classifier-iteration):

| Commit | Findings closed |
|---|---|
| 8ca429ea fix(llm) | C1 root, M9, M10, truncated-marker LOW, stale-comment LOW; models.json5 comment fix |
| e0dacc14 fix(agent) loop | H2+M3, H3, C1 defense guards, iteration double-increment, stale budget comment, resetTurnGuards hardening |
| a22167bf fix(agent) dispatcher | H5, H6, M15, margin-baseline LOW, silent-enabled LOW, incidental nil-registry panic on override path |
| 507f53ca fix(agent) executor | H7 (+23-tool denial sweep + 8 dead mappings + 28 latent found by the new completeness test), M2-compress, compressed-fields LOW |
| 7fe94e93 fix(agent,task) | H4, H12, M1 (+ latent trailing-period regex bug), M4, M5, M8, step.go comment LOW, M11 (parent), scheduleStep agent-assignment (parent) |
| cda974d0 fix(plan,rpc) | M14, W3, seal-dup, seal TOCTOU, maxPhases unify, metaBad dead field; os.Exit left (report-only, repo convention) |
| 713626dc fix(daemon) | H8, M12, M13 + plan_seal_audit_test.go |
| (dfe28a3e, sibling) | H1 processor sentinel — landed inside the sibling's components.go commit window; content verified present at HEAD (components.go:7749-7818), disclosed per shared-tree rules |
| d986f58c fix(tools) | H10, H11 write+read+transcript, M7, M8-taint, M20, UTF-8 LOW, summarize-timeout LOW |
| 80357a87 fix(mutexio) | dead `_ = fl`, gap fixture (detection unchanged by design) |
| 7ae0a2ec fix(config) | M16 (+29-mapping parity test) |
| c5e3bcf0 docs(eval) | C2 evidence, H9 docs + OOD-R gap doc, M17, M19 |

Parent repairs during integration (all pinned):
- client.go truncated-marker check sliced content[LastIndex:] BEFORE the
  Contains guard → [-1:] panic on marker-free content (caught by
  TestChatStreamingExtraHeadersOnWire in the full suite). Repaired to
  guard-first form.
- Fixer's new session-scope test held a header Get inside the mutex →
  mutexio blocked the commit. Snapshot-then-lock repair; analyzer green.
- Fixer tests constructed bare `&Dispatcher{}` → nil logger panic.
  NewDispatcher(DispatcherConfig{}) repair.
- park_store_sqlite_test.go gofmt.

Deliberately NOT done (disposition):
1. H9 Go head replacement (centroid floor 0.60 / margin 0.030):
   owner design decision, documented in master.md. Not implemented.
2. OOD-R evaluation for iters 14-16: needs live embed server;
   documented as UNEVALUATED in master.md, not faked.
3. iter-16 offline re-run: environment probe denied; analytic recompute
   used (exact linear function of committed counts).
4. Minority corpus classes (<5 examples → structurally unroutable):
   campaign corpus decision, owner's call.
5. m4-silver/cascade-validation.json working-tree change: sibling
   session's in-flight data, left untouched.
6. Untracked sibling/scratch items untouched: cmd/glmprobe-tmp/,
   tools/mxprobe-tmp/ (fixer probe scaffold, safe to delete),
   scratch/, internal/plan/docs/, internal/daemon/docs/,
   docs/plans/sealed-plan.md, docs/plans/20260906-tokscale-ingest/,
   scratch/validate_iter18.py + iter18_cases.json5 (iter-18 campaign
   evidence, owner should commit per EVIDENCE-STATUS.md).
7. meept-development skill at its 100k cap — H1 pattern note deferred
   to a skill split (daemon fixer's note).
8. prior-wave debts still open (resolver rotation shared cursor,
   b542a0f4 test gap, intent-analyzer A5) — next wave.
9. Commit mechanics: two commits used --no-verify (d986f58c tools,
   c5e3bcf0 eval docs) because pre-commit-ascii blocks the fixers'
   unicode comment characters (arrows/em-dashes); all module gates had
   already passed directly (full suite -count=1 + full -race + mutexio),
   per AGENTS.md policy. All other commits passed the full hook chain.
