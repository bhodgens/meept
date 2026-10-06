# Meept feature-cluster parity analysis (clusters shipped since 2026-09-05)

- **Host repo:** `/Users/caimlas/git/meept` @ `ee0de064` ("docs: sync workflow/reference docs + AGENTS.md with agent-routing tree", Mon Oct 5 2026). READ-ONLY: nothing in the repo was modified.
- **Competitor clones:** `/var/folders/mf/1mvt9vbx7q3f9ynln1p79cfr0000gn/T/meept-compare/{atomic-agent,duckagent,frontier-agent,hermes,oh-my-pi,opencode,prime-agent}`
- **Date window:** 821 commits in `--since=2026-09-05`; each cluster's introducing commit is cited below.
- **Sources of truth:** meept source + config schema defaults, and competitor source greps. `docs/feature-comparison-matrix.md` was **not** consulted for any verdict (per task instruction).

**Verdict key:** `NOVEL` = no competitor has the behavior · `PARITY` = a named competitor has a comparable capability · `WEAKER` = meept absent, or only superficially present.

**Parity weighting.** The five clusters carry different parity values. Cluster 2's "output filter" and Cluster 5's "plan compiler" are the two names most likely to produce false NOVEL verdicts from grep hits alone — both have superficially-matching competitors (see §2, §5).

---

## Verdict table

| Cluster | Meept default-on at default settings? | Best competitor match | Verdict |
|---|---|---|---|
| **1. Async turn lifecycle + watchdog** | **Yes** — async is the contract; the blocking `chat` RPC is opt-in | `hermes` — `gateway/run_turn.py:3468` watchdog thread + `gateway/run.py:2740` synthetic abort; run-id → keyed event stream `gateway/platforms/api_server_runs.py:235-236` | **PARITY** |
| **2. Output filters as a rewriting pipeline** | **Yes** — `Enabled: true` (`internal/config/schema.go:2864`) | `hermes` — ordered multi-listener rewrite hook `gateway/model_tools.py:854-866` | **PARITY** |
| **3. Quickplan + allotment + embedding prefilter** | **No** — prefilter `enabled: false` (`config/meept.json5:677`); `session_state_upgrade: false` (`:701`) | `atomic-agent` — embedding cosine gate short-circuiting an LLM call `src/memory/retrieve/query-rewriter-runner.ts:141` | **PARITY** |
| **4. Memory confidence bands + calibration** | **Yes** — active on the ambient-extraction path | none — `oh-my-pi` bands exist but confidence is *derived from* the band, not measured | **NOVEL** |
| **5. Plan compiler + tier routing + critique loop** | **No** — `plan_compiler_enabled` default false (`internal/config/schema.go:405`); self-seal default false (`:28` of `plan_critique_loop.go`) | none — all 7 clones' plans are model-authored free text | **NOVEL** (opt-in) |

---

## 1. Async turn lifecycle + watchdog — PARITY

### (a) Meept side (source)

The TUI submit path is handle-returning and never blocks on agent work.

`internal/tui/models/turn.go:1-8`:
> `// Async turn lifecycle for the chat view (async-turn-migration leaf 04).`
> `//  1. fires chat.submit via the RPCClient interface (returns the ack`
> `//     immediately — never agent work),`
> `//  2. registers the turn in the chat model's turnRouter keyed by turn_id,`
> `//  3. spawns awaitTurnCmd — a tea.Cmd goroutine that never blocks Update —`

The ack carries only a receipt, and the reply arrives later on a different channel:

`internal/tui/models/turn.go:41-46`:
> `// TurnSubmitAck is the parsed "chat.submit" RPC ack. It carries ONLY the`
> `// submission receipt — agent work has not happened yet and the final reply`
> `// arrives later on the turn.terminal bus event.`

`internal/agent/turn_watchdog.go:10-13`:
> `// TurnWatchdog is the async-turn liveness reaper (async-turn-migration`
> `// leaf 06). Every pass it asks the TurnRegistry for turns whose last`
> `// progress is older than the stale threshold and, for each, emits a`
> `// turn.terminal event with status=failed and handler_case=turn_reaped,`

The reaper emits a **synthetic terminal event** so a client is never left hanging:

`internal/agent/turn_watchdog.go:136-143`:
> `w.safeEmit(TurnTerminalEvent{`
> `        ConversationID: rec.ConversationID,`
> `        TurnID:         rec.TurnID,`
> `        Status:         "failed",`
> `        Error:          fmt.Sprintf("turn reaped: no progress for %s", staleAfter),`
> `        HandlerCase:    "turn_reaped",`

**Reachability:** user-facing on two surfaces. RPC handlers registered in `internal/rpc`; the TUI drives the lifecycle (`internal/tui/models/turn.go`, `internal/tui/events.go`), and the CLI async path is exercised end-to-end by `cmd/meept/chat_async_wiring_test.go:75` — `// The path must use chat.submit and must NOT use the legacy chat RPC.`

**Default-on:** yes. The async contract is the shipped default; the blocking RPC is opt-in and off.

`config/meept.json5:747`:
> `    "sync_chat_enabled": false,`

`internal/config/schema.go:408` (`plan_compiler_enabled`, sibling knob) and the invariant doc `internal/agent/AGENTS.md` state it directly:
> `orchestrator.sync_chat_enabled=true`, default false

Introducing commit: `58b02287` — `feat(agent): turn watchdog — reaper emits failed terminal events for stalled async turns (async-turn-migration leaf 06)`.

### (b) Competitor side

**`hermes` — full behavioral match (verified by direct read).**

Separate handle and event-stream routes, keyed on the run id:

`gateway/platforms/api_server_runs.py:235-236`:
> `("POST", "/v1/runs", self._handle_runs), ("GET", "/v1/runs/{run_id}", self._handle_get_run),`
> `("GET", "/v1/runs/{run_id}/events", self._handle_run_events),`

A watchdog thread per turn:

`gateway/run_turn.py:3468-3472`:
> `threading.Thread(`
> `    target=_watch_gateway_turn_inactivity,`
> `    kwargs={` 
> `        "agent_holder": agent_holder, "timeout": _agent_timeout, "poll_interval": 5.0,`

Stall → synthetic terminal, recorded and mapped to a run status:

`gateway/run.py:2740-2741`:
> `from hermes_cli.observability.shared_metrics_process import record_watchdog_turn_abort`
> `record_watchdog_turn_abort(agent)`

`gateway/platforms/api_server_runs.py:193`: `status = "cancelled" if interrupted else "completed" if finished else "failed"`.

Other clones, checked for the same two-part behavior:

- `opencode` — handle + SSE, **no** server-side turn timeout. `packages/protocol/src/groups/session.ts:213` returns `SessionInput.Admitted`; `packages/server/src/handlers/event.ts:40` serves `text/event-stream`. Grep for `watchdog|reaper|sweeper|stalled|staleTurn|leaseExpiry|turnTimeout|orphan` over `packages/server/src`, `core/src/session`, `core/src/event` → 0 hits. The 15s heartbeat (`event.ts:37`) is transport liveness only.
- `oh-my-pi` — keyed waiter map but caller-side timeout only: `packages/coding-agent/src/modes/rpc/rpc-client.ts:336` `#promptResultWaiters = new Map<string, (result: RpcPromptResultFrame) => void>();`, `rpc-mode.ts:11` "Prompt completion: one `prompt_result` per accepted prompt, correlated by the command `id`"; `rpc-client.ts:1402` `reject(new Error("Timeout waiting for prompt_result…"))`.
- `atomic-agent` — fire-and-forget (`route-tasks.ts:60,67`) but self-documented as not stream-first: `src/tasks/task-store.ts:451-453` "No background sweeper; recovery is a one-shot pass".
- `frontier-agent`, `duckagent`, `prime-agent` — blocking. `duckagent/gateway/mod.rs:2171-2172` blocks on `run_api_user_message(...)` and mints the id only afterward; `prime-agent/worker/input.rs:159-161` `if !wait { return response_success(None, "prompt", None); }`.

**Absence method:** grep -rIn across each clone's source tree excluding `node_modules/`, `.git/`, `vendor/`, `dist/`, `build/`, test dirs; per-clone dirs enumerated — frontier-agent (`frontier_agent/`, `apodex/`, `workflows/`, `tools/`, `plugins/`), duckagent (`src/`, 161 files, `src/sandbox/vendor/` excluded), prime-agent (`crates/`, `pa-agent/`, `pa-tui/`, `worker/`).

### (c) Verdict

**PARITY.** `hermes` implements both halves: a handle-returning submit with a separate id-keyed event stream, *and* a server-side reaper that emits a terminal event for a stalled turn. Meept's distinguishing detail is scope, not existence — the reaper is deliberately mark-and-continue (`turn_watchdog.go:14-17`: "the reaper never re-dispatches work and never cancels in-flight goroutines"), whereas hermes hard-interrupts the agent (`run.py:2736` `request_hard_interrupt(agent, _INTERRUPT_REASON_TIMEOUT, …)`). Similar class, different failure semantics. Not a parity win.

---

## 2. Output filters as a pipeline — PARITY

### (a) Meept side (source)

A tri-state post-step filter contract that can **rewrite the step result**, not just fail it:

`internal/validator/filter.go:9-26`:
> `// FilterOutcome is the tri-state outcome of an output filter: the result`
> `// continues unchanged, is rewritten, or is rejected.`
> …
> `// FilterRewrite indicates Output replaces the result.`
> `FilterRewrite`
> `// FilterFail indicates the result is rejected; Reason feeds rework.`

Advisory is a fourth outcome (linters over mixed prose/code):

`internal/validator/filter.go:21-26`:
> `// FilterAdvisory indicates a problem was detected and logged but the`
> `// result continues unchanged — the verdict is advisory (linters over`
> `// mixed prose/code step output, runs 33-36: a narration sentence`
> `// inside a ```js fence is a labeling mistake, not a turn-fatal`

Rewrite is real, not declared — two filters return a replacement value:

`internal/validator/filter_json.go:75`:
> `return FilterResult{Outcome: FilterRewrite, Filter: self, Output: formatted}`

`internal/validator/filter_lint.go:211`:
> `return FilterResult{Outcome: FilterRewrite, Filter: self, Output: rewritten}`

The builtin set is host-adaptive, so a fresh install never fails on a missing binary:

`internal/validator/filter_builtin.go:34-36`:
> `var knownBuiltinFilters = []string{`
> `    "json_format", "language_en", "lint_go", "lint_python", "lint_js",`
> `}`

**Reachability:** `internal/daemon/filter_wiring.go:57` constructs them into the daemon step path, and `cmd/meept/agents_filter_display.go:16` renders the live state to the user:
> `// output filters: enabled, filters: json_format, language_en, max passes: 2, retries: 2`

**Default-on:** yes. `internal/config/schema.go:2855-2865`:
> `OutputFilters: OutputFiltersConfig{`
> `    // Enabled defaults to TRUE (2026-09-22 decision): the`
> `    // filter stage is part of the shipped validation pipeline.`
> `    Enabled:          true,`
> `    MaxPasses:        2,`
> `    MaxFilterRetries: 2,`
> `},`

Guard test `internal/config/output_filters_config_test.go:17`: `t.Error("Daemon.OutputFilters.Enabled = false; want true (enabled-by-default)")`.

> ⚠️ **Stale doc comment found (not acted on).** The struct doc directly above the same field still says the opposite: `internal/config/schema.go:688-690` reads `// Enabled defaults to FALSE: the filter stage is opt-in until config turns it on, keeping the completion path byte-identical for existing installs.` The default was flipped to true on 2026-09-22; only the initializer comment was updated. The same file also exposes a CLI string that is now unreachable: `cmd/meept/agents_filter_display.go:24` `return "\noutput filters: off (daemon default disabled)"`. Not modified — repo is read-only for this task.

Introducing commits: `58589a32` (leaf 01, interface+chain), `deee3627` (leaf 02, builtins), `25d1db94` (`feat(validator,daemon): enabled-by-default filters + lint_python/lint_js; e2e harness (issue #55 follow-up)`).

### (b) Competitor side

**`hermes` — ordered, multi-listener, rewriting.** `gateway/model_tools.py:854`:
> `"""transform_tool_result: plugins may replace the final result string."""`

`:857` "Runs after post_tool_call and before the result enters context. Fail-open; first string return wins", `:866` `return next((r for r in hook_results if isinstance(r, str)), result)`. A real rewriter ships with it: `plugins/security-guidance/__init__.py:127` `return result + "\n\n" + _format_warning_block(findings)`.

**`oh-my-pi`** — ordered merge chain, `extensibility/hooks/tool-wrapper.ts:97` `content: resultResult.content ?? result.content`; `hooks/runner.ts:302-303` "a later handler's defined field wins; undefined leaves earlier patches intact".

**`opencode`** — mutates one shared object by reference, `plugin/index.ts:288-291` `for (const hook of s.hooks) { … fn(input, output) } return output`.

**`frontier-agent`** — single post-processor, `workflows/stateful_react_agent/_runtime.py:581-582` `return self._compact_bash(content, budget)`; wired at `nodes/main_agent.py:1096` `tool_result_post_processor=ReactToolResultPostProcessor(),`.

**`prime-agent`** — one optional hook, `pa-agent/src/types.rs:602-607` `pub struct AfterToolCallResult { content, details, is_error, terminate }`, merged `agent_loop/tool_call.rs:270-272`.

**`atomic-agent`** — inline if/else filters on the completion, `step-executor.ts:1517` `const fabricated = fabricationOf(completion);`.

**`duckagent` — ABSENT.** `grep -rIn -E 'output_filter|post_process|postprocess|result_filter|after_step|post_step|post_tool|after_tool|transform_output|validate_output|sanitize_output|rewrite.{0,20}result'` over all 150 non-vendor `.rs` under `duckagent/src/` (`agent.rs`, `tools.rs`, `capabilities/`, `gateway/`, `client/`, `session.rs`; `src/sandbox/vendor/` excluded) → **0 hits**; re-run independently returned empty. `capabilities/registry.rs` `execute` returns the capability's raw `Result<String>` post-audit with no rewrite stage.

**Key distinction verified:** none of the six present competitors lints **files on disk** as the primary behavior — they all operate on the in-flight result value, which is the behavior in question. Competitors differ in *shape* (ordered chain vs by-ref mutation vs single hook vs inline), not presence.

### (c) Verdict

**PARITY.** The rewrite-a-step-result capability is widespread (6/7 clones). Meept's differentiated edges are narrower and real: the **tri-state + advisory** outcome vocabulary, the **host-adaptive empty-list** resolution, and filters that reason about *mixed prose/code* step output (`filter.go:21-26`) rather than pure tool output. But presence-wise this is saturated — it is not a parity win, and a matrix row claiming NOVEL here would be wrong.

---

## 3. Quickplan + allotment + embedding prefilter — PARITY

### (a) Meept side (source)

**Quickplan** is a distinct intent lane with plan-execution depth: `internal/agent/dispatcher.go:866-867`:
> `case IntentPlan, IntentQuickPlan:`
> `    // quickplan is autonomous plan-execution: same depth bar as plan`

A one-way, evidence-driven upgrade into that lane: `internal/agent/dispatcher.go:1700` `d.logger.Info("LLM verdict upgraded to quickplan by orchestration cue evidence",`.

**Embedding prefilter** is a genuine cheap-stage-short-circuits-LLM design — `config/meept.json5:671-676`:
> `// Stage-0 classifier prefilter (docs/workflows/classifier-prefilter.md):`
> `// embedding cosine gate that direct-routes confident inputs before the`
> `// analyzer + router LLM calls. Disabled by default; requires a local`
> `// embeddings endpoint (scripts/embed_server.py) + centroids`

with a fail-safe abstention, `config/meept.json5:687-688`:
> `// Unhealthy => the prefilter abstains and the LLM chain runs (fail`
> `// safe). Default on: pure Go, deterministic, no network dependency;`

**Reachability:** dispatcher-internal. Quickplan is produced by the classifier and consumed at `dispatcher.go:866`; the session-evidence upgrade is wired as an injectable seam (`dispatcher.go:357` "Zero value = off = the gate is inert and dispatch behavior is byte-identical to legacy"). It is reachable through normal chat dispatch, but there is no dedicated CLI/TUI/GUI control — the only CLI reference is a test (`cmd/meept/lanes_test.go`). Flag-controlled by config, not by a user surface.

**Default-on: NO — nothing in this cluster is.** All three knobs ship off:
- `config/meept.json5:677` `"enabled": false,` (classifier_prefilter)
- `config/meept.json5:701` `"session_state_upgrade": false,` (one-way upgrade; also `internal/config/schema.go:3376`)
- `config/meept.json5:747` `"sync_chat_enabled": false`

Introduced by `1b669f65` — `feat(quickplan): dispatcher routing, clarify-resume, session context (leaf 02)`.

### (b) Competitor side

**`atomic-agent` — PRESENT, and it is the same design.** Two-stage cascade with a cosine prefilter gating an LLM call.

`src/memory/retrieve/embedding-gate.ts:10`:
> `Embedding-based rewriter gate (config v20). Lazy-warms unit-normalized exemplar vectors, then classifies each message by max cosine similarity against the corpus.`

The gate decision, `embedding-gate.ts:74`:
> `if (maxScore >= threshold) {`

The short-circuit itself — the LLM prompt is built only *after* the gate returns, `src/memory/retrieve/query-rewriter-runner.ts:141-149`:
> `const referential = await gate.check({...});`
> `if (!referential) { record("skipped_not_referential", ...); return raw; }`

Runtime wiring: `src/runtime/bootstrap.ts:2617` `gate = createEmbeddingGate({...})`, fail-soft to a heuristic gate at `:2627`.

**`opencode` — not a match.** `small_model` exists but selects a cheaper *LLM* for one task; `packages/web/src/content/docs/config.mdx:375` "The `small_model` option configures a separate model for lightweight tasks like title generation." Sole call site `packages/opencode/src/session/prompt.ts:220`.

**`oh-my-pi` — not a match.** `packages/coding-agent/src/tools/jfind/cascade.ts:2` is a TF-IDF→LLM cascade, but the LLM is the only semantic stage; no cheap stage decides the route. `crates/pi-predict/src/smollm/mod.rs:1` is a real 135M small model used as a *replacement* engine, not a pre-classifier.

**`hermes`, `duckagent`, `frontier-agent`, `prime-agent` — ABSENT.** hermes's only "prefilter" is a SQL row-count guard (`hermes_state_sessions.py:1750` "`: message_count = 0` stays as a cheap prefilter"); frontier-agent's escalation is web-fetch engine fallback (`plugins/tools/web_fetch.py:495`), not classification.

**Absence method:** grep -rIn with the Cap3 pattern set (`prefilter|short_circuit|fast_path|two_stage|cascade|escalat|tfidf|cheap.*classif|small_model|distil`) over `src/`, `packages/`, `crates/`, `agent/`, `workflows/` in every clone, excluding `node_modules/`, `.git/`, `vendor/`, `target/`, `dist/`, `build/`, `.venv/`, `__pycache__/`, test/spec/eval/bench paths.

### (c) Verdict

**PARITY.** `atomic-agent` independently shipped the same architecture — cosine prefilter → skip → expensive path, with the same fail-soft posture. On top of that, **meept's version is not on by default** (`enabled: false`), so at default settings this cluster delivers nothing that `atomic-agent` does not already deliver by default. The genuinely distinct piece is the *session-evidence upgrade* into quickplan — one-way, monotonic (schema.go:2563: "One-way: session evidence never downgrades a quickplan") — which no competitor has, but it is opt-in and has no user-facing surface.

---

## 4. Memory confidence bands + calibration — NOVEL

### (a) Meept side (source)

The distinguishing behavior: the calibration data **changed the policy**, and the code says so. Rejection is by a *floor band*, not by the old single threshold:

`internal/agent/rejected_candidates.go:111-117`:
> `// splitFiltered applies the three-band confidence disposal (corrected`
> `// calibration design): below rejectBelow is hard-dropped (reason "reject_band"`
> `// — overwhelmingly decoy activations); everything else is stored as status=auto`
> `// for librarian review, regardless of the legacy ConfidenceThreshold (the`
> `// threshold no longer drops — the calibration data showed the middle band is`
> `// 33% correct, so hard-dropping it loses real memory). Category excludes and`
> `// max_per_turn still gate.`

The band is enforced in a switch, with a distinct reason code per cause: `internal/agent/rejected_candidates.go:118-131`:
> `case c.Confidence < rejectBelow:`
> `    rejected = append(rejected, rejectRec(c, "reject_band"))`
> `case catExcluded(c.Category, excludedCat):`
> `    rejected = append(rejected, rejectRec(c, "category"))`

The calibration dataset itself — predicted confidence paired with the realized verdict:

`internal/memory/calibration_log.go:14-22`:
> `// calibrationRecord is one line of claim_verdicts.jsonl: a human/librarian`
> `// verdict on an auto-claim, paired with the claim's extraction-time`
> `// confidence. This is the confidence-calibration dataset: confidence vs`
> `// human verdict on live traffic.`

with per-candidate rejection reasons so the log records *why* each item was dropped (`rejected_candidates.go:107-110`: "plus a rejection reason per dropped candidate so the calibration log records WHY each one was dropped").

Size-capped rotation, crash-safe rename:

`internal/agent/rejected_candidates.go:84-87`:
> `// Size-cap rotation: keep the newest data plus one prior generation so`
> `// the calibration log cannot grow unbounded (crash-safe rename, see`
> `// memory.RotateIfNeeded).`
> `if err := memory.RotateIfNeeded(path, memory.MaxCalibrationLogBytes); err != nil {`

`internal/memory/rotate.go:20-22`:
> `// RotateIfNeeded rotates path to path+".1" when the file currently exceeds`
> `// maxBytes. The previous .1 generation is overwritten, so at most two`

The feedback loop is **closed**: `internal/agent/epistemic_hook.go:98` calls `splitFiltered(...)` on the live extraction path, and the rejection log it writes is the dataset that justified the floor. A dedicated `memory_model` slot (`internal/config/config.go:373` `MemoryModel string \`json:"memory_model"\``) carries extraction/summarization.

**Reachability:** daemon-wired on the ambient extraction path (`internal/agent/epistemic_hook.go:98` → `splitFiltered`); `NewCalibrationLogger` (`internal/memory/calibration_log.go:37`) is constructed from daemon composition. Operates on every ambient memory extraction — reachable through normal chat with no flag.

**Default-on:** yes — `splitFiltered` is on the live path, not behind a knob. Confidence is per-candidate at extraction (`internal/memory/epistemic_ambient.go:18` `Confidence float64  // 0.0-1.0`).

### (b) Competitor side

**No clone implements this.** Best partial is `oh-my-pi`, which has discrete bands but no measured feedback — and importantly the *inverse* dependency.

`packages/mnemopi/src/core/veracity-consolidation.ts:4-10`:
> `export const VERACITY_WEIGHTS = Object.freeze({ stated: 1.0, inferred: 0.7, tool: 0.5, imported: 0.6, unknown: 0.8, });`

Confidence is derived *from* the band, never the reverse: `:330` `const baseConfidence = weight * 0.5;`. Discrete read cutoff exists (`:411` `WHERE subject = ? AND confidence >= ? AND superseded_by IS NULL`, callers pass `minConfidence = 0.5`), and there is a Bayesian update at `:276-278` `bayesianUpdate(currentConfidence, veracity)` — but it is driven by the same **fixed** weights, not by recorded predicted-vs-actual outcomes. `grep -rInE 'calibrat|isotonic|brier|\bece\b'` over `packages/mnemopi` + `packages/coding-agent` → zero memory-calibration hits. Notably, `crates/pi-predict/src/{ngram,smollm}/mod.rs:189` *reports* a negative result: "A calibrated per-(prefix, word) feedback bias measured no gain."

- **`atomic-agent` — spec only, zero implementation.** `grep -rIn 'confidence' src/memory/*.ts src/memory/**/*.ts` → 5 hits, all about voting weight (`src/memory/voting/vote-parser.ts:21` "invariant 18: the model cannot pump its own confidence by…"). No `confidence` column in `memory-schema.ts`. The design exists **only in an unimplemented doc**: `INTENT_FABRIC_V1.md:89` self-declares "No confidence-calibrated prediction surface"; `:159` `confidence = σ(α·signalDiversity + β·signalRecency + γ·signalCount + δ·LLM-vote)`; `:660` `features TEXT NOT NULL, -- JSON snapshot of confidence inputs at decision time`. **No runtime code reads any of it.**
- **`hermes` — ABSENT.** `grep -rInE 'calibrat|isotonic|brier|reliability_diagram|expected_calibration|\bece\b'` over `agent/`, `tools/`, `hermes_state*.py`, `hermes_cli/` → 20 hits, every one token-cost accounting (`image_token_cost.py:103` `calibrate_from_usage`) or context-compressor recalibration (`context_compressor.py:3455`). Zero confidence hits in `agent/memory_manager.py`, `tools/memory_tool.py`.
- **`opencode`, `duckagent`, `frontier-agent`, `prime-agent` — ABSENT.** Same calibration pattern set → zero source hits in all four. frontier-agent's 6 hits are token-gauge comments plus a `reliability_score` field (`workflows/_shared/research/evidence.py:35`) scored by an LLM Critic — not measured. No calibration JSONL anywhere: `grep -rIn jsonl` filtered to `conf|calib|score|pred` returns only eval-runner plumbing.

**Absence method:** full pattern set above per clone over source dirs; markdown/doc hits explicitly excluded from credit unless backed by runtime code (which is how atomic-agent's INTENT_FABRIC_V1.md was correctly demoted to spec-only).

### (c) Verdict

**NOVEL.** No competitor rejects or down-weights ambient-extracted memories on *measured* confidence with a feedback loop. `oh-my-pi` is the only clone with bands, and it runs the causality backwards (band → fixed confidence), so it cannot learn. `atomic-agent` has a fully specified design document for exactly this and no implementation. Meept's differentiator is the closed loop: rejected candidates are logged with per-cause reasons and extraction-time confidence, and that dataset demonstrably **rewrote the policy** — the legacy threshold stopped dropping, because calibration showed the middle band was 33% correct.

---

## 5. Plan compiler + tier routing + critique loop — NOVEL (opt-in)

### (a) Meept side (source)

A real compiler: deterministic, pure, whole-document error collection. `internal/plan/compiler.go:15-22`:
> `// CompileSealed parses a sealed plan-dialect v1 markdown document into`
> `// phase specs per docs/workflows/plan-dialect.md. Pure: string in,`
> `// struct out; no I/O, no clocks, no global mutable state.`
> `//`
> `// All problems are collected in one pass (never just the first)`

The plan is hashed and sealed against that hash — `compiler.go:44-46`:
> `sum := sha256.Sum256([]byte(markdown))`
> `cp.Hash = hex.EncodeToString(sum[:])`

Structural checks that a free-text plan could never satisfy: `compiler.go:29-33` runs `checkEnvelope` → `checkPhaseStructure` → `checkArtifacts` → `checkDependsOnLines` → `checkSteps`, then `detectCycle(doc, &problems)` (`:38`) for DAG acyclicity. Warnings are structurally classified, never inferred from message text (`compiler.go:52-53`).

The tree emitter is a second deterministic stage: `internal/plan/treeemit.go:71-74`
> `// EmitTree renders a CompiledPlan as the hierarchical-planning-style tree`
> `func EmitTree(cp *CompiledPlan, opts TreeEmitOptions) (*EmittedTree, error) {`

The critique loop with **self-seal** — draft → critique → refine → seal, no human required on the automatic path:

`internal/agent/plan_critique_loop.go:3-6`:
> `// TierComplex critique loop (tiered-iteration leaf 02).`
> `//`
> `// For TierComplex requests: draft the plan in the brainstorm dialect,`
> `// critique it against PlanCritiqueInput evidence, refine, and self-seal`
> `// when clean — no human required on the automatic path.`

Self-seal persists and executes without human approval: `internal/agent/plan_critique_loop.go:480-483`:
> `if err := sp.SealDraft(req.TaskID, compiled.Hash); err != nil {`
> `if err := sp.SealPlan(ctx, req.TaskID, PhaseSpecsFromPlan(compiled.Phases), nil); err != nil {`

Degradation is honest, never silent (`plan_critique_loop.go:15-18`): rounds exhausted with open blocking items → seals anyway with a `## Known Risks` section; planner transport failure → legacy single-shot path with a Warn; `TierComplex never hard-fails the task because the fancy path is down` (`:23`).

**Reachability:** yes, on a user surface — `internal/rpc/plan_seal.go:93-94`:
> `server.RegisterHandler("plan.seal", h.handleSeal)`
> `server.RegisterHandler("plan.draft", h.handleDraft)`

plus the documented CLI `meept plans list/show/approve/reject/confirm <id>` (root `AGENTS.md`). Tier routing carries escalation-level metadata (`internal/agent/escalation_test.go:47` `MaxEscalationLevels: 3`; `internal/metrics/collector.go:384` `c.store.Record("review.escalation_rate", 1, nil)`).

**Default-on: NO — both halves are opt-in.**
- `internal/config/schema.go:404-408`:
> `// Draft→seal→compile planning pipeline (default false;`
> `// legacy JSON spec_plan path used when false). See`
> `// docs/workflows/agent-orchestration.md (plan compiler pipeline`
> `// section).`
> `PlanCompilerEnabled bool \`json:"plan_compiler_enabled" toml:"plan_compiler_enabled"\``

  Guard test `internal/config/plans_config_test.go:100`: `if cfg.Plans.PlanCompilerEnabled {`.
- Self-seal: `internal/agent/plan_critique_loop.go:27-29`:
> `// Self-seal is gated by SetSelfSealEnabled (plans.self_seal_enabled,`
> `// default FALSE — ships dark). Flag off: quick_plan TierComplex keeps`

  Corroborated by the root `AGENTS.md` invariant: `plans.plan_compiler_enabled` are opt-in with default false; flipping a default is a product decision.

Introducing commit: `0fa7f332` — `feat(agent,config): TierComplex critique loop with flag-gated self-seal (leaf 2)`.

### (b) Competitor side

**Deterministic plan compiler — ABSENT in all 7.** Grep -rInE for `plan_compiler|compilePlan|compile_plan|planEmitter|plan_emitter|treeemit|emitPlan|emit_plan|compile.*(tree|dag)|emit.*(phase|steps|task_list|commands)|phaseGraph|PlanDAG|PlanNode|plan_dag|plan_node|serialize.*plan` over `--include=*.ts,*.go,*.py,*.rs`, excluding `node_modules .git vendor dist build target .venv __pycache__ .next out`, plus a second pass dropping `_test.|/tests?/|.test.|_spec.|/spec/|/evals?/|/bench`. Every hit was unrelated:
- `atomic-agent` — `src/cli/uninstall-command.ts:237 function renderPlan(` is an uninstall-wizard text renderer; `src/tui/uninstall/uninstall-orchestrator.ts:10` "Turn a resolved plan into the strings the modal renders."
- `duckagent` — zero hits, all patterns.
- `frontier-agent` — one hit, `apodex/observers.py:595` comment: "The plan tool renders as a checklist panel instead of a raw result" — *display* of model-authored `todo_write` output.
- `hermes` — `hermes_cli/local_runtime/bootstrap.py:294` `plan_presets` = LLM provider tiers; `agent/skill_commands.py:367` "lets the cache planner break there" (prompt cache boundary).
- `oh-my-pi` — `src/modes/print-mode.ts:176` "deterministic way out of plan mode" (adjective); `snapcompact.ts:1993` "One planned frame" (video frames).
- `opencode` — `packages/llm/src/schema/events.ts:310` "plan rendering" in a comment.
- `prime-agent` — `pa-tui/src/interactive/headless.rs:11` "Headless plan" = TUI step sequence.

**Critique loop with self-seal — ABSENT; both critic implementations are the inverse.** Neither self-seals; both are human-gated.
- `frontier-agent` — `apodex/observers.py:537-539`: `async def _review_plan(self, plan: str)` … `"""Human-approval gate for exit_plan_mode: show the plan, ask the user, and unlock edits only on approval (otherwise revise, stay planning)."""` Then `:545` `decision = await self.approver.confirm(...)`.
- `oh-my-pi` — a real critic, but it critiques *primary work*, not a plan artifact: `src/advisor/runtime.ts:393` "advisor knows to withhold critique on partial work". Its plan review is human-picked: `src/modes/interactive-mode.ts:5264` `this.#planReviewOverlay = overlay` with `onPick: choice => finish(choice)`. `--plan-yolo` (`print-mode.ts:181`) is a **user-supplied flag**, not a self-seal.

**Tier routing for planning depth — ABSENT everywhere.** No hits for `planning_depth|planningDepth|plan_depth|classify.*complexity.*plan|complexity.*(route|tier)`. frontier-agent's `workflows/agent_team/observers/planning_gate.py:22 planning_max_turns: int = 40` is a flat cap, not a tier, and `:52 force_finish_planning(...)` is budget exhaustion, not a critic pass.

**Quickplan — ABSENT in all 7.** Zero non-test hits for `quickplan|quick_plan|fast_plan|lite_plan|plan_mode` (the `plan_mode` hits are oh-my-pi's human-reviewed plan *edit* mode and frontier-agent's `PlanState` gate — both the full expensive path).

**Allotment — PARTIAL (3 clones), but scoped to tasks/goals, never to a *plan*.** `duckagent/src/session.rs:846-849` `&& let Some(token_budget) = meta.goal_token_budget / && token_budget > 0 / && meta.goal_tokens_used >= token_budget` → `GoalStatus::BudgetLimited`; `prime-agent/crates/pa-daemon/src/goal_state_persist.rs:148` `"tokenBudget": 2000`; `atomic-agent/src/tools/fusion/worker-runner.ts:537` `const stepBudget = clampTaskBudget(task.maxSteps, options.workerMaxSteps);`.

**Session digest — PARTIAL, and it is compaction, not a rolling digest.** `opencode/packages/core/src/session/compaction.ts:47` `SUMMARY_UPDATE_INSTRUCTIONS = "The <prior-summary> summarizes everything that happened before the <conversation>. Construct a new summary that combines both."` All `digest` grep hits elsewhere are **hash digests** (`hermes/pm/store.py:305 tree_digest`, `opencode/packages/core/src/util/hash.ts:5`, etc.) and were not counted.

### (c) Verdict

**NOVEL, but opt-in — and the matrix must record it that way.** No clone compiles a sealed plan deterministically into an executable form; no clone self-seals a plan after a critic pass; no clone routes planning depth by tier. Every competitor plan is model-authored free text or a rendered checklist. Two honest caveats that cap the parity value at default settings:

1. `plan_compiler_enabled` is **default false** — at defaults, meept still runs the legacy JSON `spec_plan` path, so a competitor matches the *default* behavior.
2. Self-seal is **default false** ("ships dark"), and even when on, plan mode still presents the human seal request (`plan_critique_loop.go:31-33`: "self-seal is the DEFAULT the user can override, not a bypass"). So the self-seal win applies to `quick_plan` TierComplex only.

The cluster is defensible as novel architecture; it is not a shipped-default advantage. Allotment and digest are near-parity and should be scored individually, not bundled with the compiler.

---

## Observations

- **`internal/config/schema.go:688-690` — stale doc contradicts code.** The `OutputFiltersConfig` comment says `Enabled defaults to FALSE: the filter stage is opt-in`, but the initializer at `:2864` sets `Enabled: true` (2026-09-22 decision). Not acted on: read-only task; it is a one-line comment fix and should ride the next validator commit.
- **`cmd/meept/agents_filter_display.go:24` — unreachable CLI string.** `return "\noutput filters: off (daemon default disabled)"` can no longer render under shipped defaults. Same fix cycle.
- **`oh-my-pi` `veracity-consolidation.ts` inverts the confidence relationship** (band → fixed confidence, `:330` `weight * 0.5`). A bad `tool`-sourced extraction therefore inherits a hard 0.35 floor with no mechanism to earn below it — the opposite of a calibrated system. Flagged because it is the closest competitor artifact and someone may otherwise credit it as parity.
- **`atomic-agent/INTENT_FABRIC_V1.md:89,159,660` is an unimplemented Cluster-4 design** (σ-weighted confidence, feature JSON snapshots, banded thresholds). It is the most directly transferable competitor artifact found in this sweep, and its `No confidence-calibrated prediction surface` self-declaration is the cleanest statement of why Cluster 4 is novel.
- **Cluster 5's novelty is gated twice** (`plan_compiler_enabled` false, `self_seal_enabled` false). Any matrix row marked NOVEL should carry "(opt-in)" or it will overstate the default-settings position.