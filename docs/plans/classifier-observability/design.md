# Design: Classifier Outcome Metrics + Correction-Capture Pipeline

Status: design (no code authored)
Branch context: classifier-iteration
Successor to the completed classifier-observability tree
(master.md leaves 01-03, master.md:112-114) and the classifier-iteration
campaign (docs/plans/classifier-iteration/). Goal: close the loop so
classification accuracy improves in-loop from persisted outcomes, not
only from offline gold replay.

## 0. Verified current state (all claims file:line)

- One structured log line per dispatch: "Dispatched request"
  (internal/agent/dispatcher.go:984) and the prefilter direct-route
  variant (internal/agent/dispatcher.go:806). Fields include agent,
  intent_type, confidence, classification_method.
- Classification method is recorded in-memory only:
  recordClassificationMethod (internal/agent/dispatcher.go:2674),
  recordAgent (:2689), recordIntentType (:2703), surfaced via the
  "dispatcher.stats" RPC handleStatsQuery (:2158-2167). Last-100
  fallback ring: FallbackDetails append (:2729), FallbackEntry struct
  (:2806). Nothing survives process restart.
- A persistent per-dispatch record ALREADY EXISTS:
  `dispatch_log` table (internal/metrics/store.go:314-328) with
  session_id, input_summary, intent_type, agent_id, confidence,
  classifier_method, handler_case, task_id, has_parts, error; indexes
  at store.go:344-346; written by Store.RecordDispatch via the
  DispatchEntry struct (store.go:840-853, INSERT at :855-864). The
  dispatcher writes it when wired: recordDispatch
  (internal/agent/dispatcher.go:3022-3085, metrics write at :3071-3084),
  entry point RecordDispatch (:3015-3020) called by ChatHandler after
  the handler switch (internal/agent/handler.go:853-854). Store wiring:
  Dispatcher.SetMetricsStore (dispatcher.go:2995-3000),
  ChatHandler.SetMetricsStore (handler.go:1652-1657).
- GAPS: no input hash (input_summary is raw message text), no model
  provenance column, no Door-1 margin, no outcome/correction column,
  no consumer.

## 1. Metrics persistence

Extend `dispatch_log` in place; do NOT add a parallel table. It already
has the right grain (one row per classified dispatch), the right keys
(session_id, task_id), and 30-day retention in the purge list
(internal/metrics/store.go:557; DefaultRetentionDays=30 store.go:28).

Schema delta (ALTER TABLE ADD COLUMN, tolerate-duplicate-column pattern
proven at store.go:370-388 for llm_calls; any post-ALTER index must be
created after the columns exist, per the ordering bug documented at
store.go:390-397):

    ALTER TABLE dispatch_log ADD COLUMN input_hash TEXT NOT NULL DEFAULT '';
    ALTER TABLE dispatch_log ADD COLUMN model TEXT NOT NULL DEFAULT '';
    ALTER TABLE dispatch_log ADD COLUMN margin REAL;               -- NULL = not Door 1
    ALTER TABLE dispatch_log ADD COLUMN turn_no INTEGER NOT NULL DEFAULT 0;
    ALTER TABLE dispatch_log ADD COLUMN outcome TEXT NOT NULL DEFAULT 'pending';
    ALTER TABLE dispatch_log ADD COLUMN corrected_agent TEXT NOT NULL DEFAULT '';
    CREATE INDEX idx_dispatch_log_hash ON dispatch_log(input_hash);
    CREATE INDEX idx_dispatch_log_outcome ON dispatch_log(outcome);

Column semantics:

- input_hash: SHA-256 of the user message keyed with a per-install
  salt (see S4). Replaces input_summary as the join/dedup key. First
  16 hex chars stored is sufficient (collision risk negligible at
  campaign corpus sizes).
- model: classifier provenance, sourced from Intent.Model
  (internal/agent/dispatcher.go:110-117, populated at :446 from
  cfg.ClassifierModel; leaf 01 of the previous tree). Empty for
  deterministic doors -- that IS the honest provenance.
- margin: Door 1 kNN margin (see S3b).
- turn_no: monotonic per-session dispatch counter, enables the
  N-turn re-route window.
- outcome / corrected_agent: written by the capture layer (S2).

Code touch points (Go-side, leaf-sized):

- DispatchEntry struct (internal/metrics/store.go:840-853) gains
  InputHash, Model, Margin, TurnNo, Outcome, CorrectedAgent fields
  (db tags matching the new columns; keep HasPartsInt db:"-"
  conversion pattern at :850-851).
- Store.RecordDispatch INSERT (store.go:855-864) column list extended.
- Dispatcher.recordDispatch (internal/agent/dispatcher.go:3024-3085)
  computes input_hash (it has the raw input via the handler call chain)
  and copies Intent.Model; the errStr assembly (:3054-3057) stays but
  is scrubbed/truncated (S4).
- input_summary stops being persisted: the raw summary is produced at
  internal/agent/handler.go:702 via extractSummary
  (internal/agent/dispatcher.go:2665-2672 -- first sentence or first
  100 chars of the verbatim user message). This is raw user text in
  the DB today; S4 removes it. The struct field remains for
  compatibility but is written as "" going forward.
- Unit tests mirror internal/metrics/llm_calls_test.go patterns
  (TestRecordLLMCall_WindowedAggregation llm_calls_test.go:26) and the
  purge control-table test pattern in
  docs/plans/20260906-tokscale-ingest/02-retention-and-verify.md:105-127.

## 2. Outcome capture

Two signals, both observable at existing call sites; no new user-facing
behavior.

Signal A -- implicit re-route correction (primary):

Claim: when the classifier picks door D with agent A1 for turn N of
session S, and the user's next in-scope dispatch in the same session
lands on agent A2 != A1 via a different classifier method within N
turns (small window, default 3), turn N's classification was probably
wrong -- the user re-asked.

Mechanism: ChatHandler calls RecordDispatch AFTER the handler switch
resolves handlerCase (internal/agent/handler.go:705-851 selects the
case; :853-854 passes it in). So at recordDispatch
(internal/agent/dispatcher.go:3024) we already know, for the CURRENT
dispatch, the final agent (result.AgentID, :3036) and handler case.
Implementation:

1. On each classified dispatch, UPDATE the most recent prior
   'pending' row for the same session_id: if
   (current.agent_id != prior.agent_id) AND
   (turn_no - prior.turn_no <= N) AND prior was a work-intent
   (task-creating or async path -- the same classes the H6 gate
   excludes from Door 1, dispatcher.go:783-798), set
   prior.outcome='corrected', prior.corrected_agent=current.agent_id.
   Otherwise set prior.outcome='ok'.
2. Rows that never get a successor (session end, 30-day purge) stay
   'pending' and are excluded from accuracy denominators.
3. clarifications and non-classified paths already carry empty
   classifier_method by design (dispatcher.go:3046-3052) -- they are
   skipped as correction *sources* but still count as session
   continuation.

Per-session intent history already exists in memory
(SessionTracker.RecordIntent, internal/agent/session_tracker.go:77-82;
TrackerSessionState.IntentHistory :26-33) but is restart-volatile --
the SQL dispatch_log is the durable source of truth; do not depend on
the tracker.

Signal B -- explicit failure/replan correction:

When a dispatched task fails and the system re-plans, the original
route is observably wrong-or-insufficient:

- EscalationManager.Escalate (internal/agent/escalation.go:98) ->
  triggerReplan (:248-253) publishes the escalation event with kind
  "replan"; FailureContext carries TaskID (:34-55). Call site: step
  failure path internal/agent/tactical.go:1431-1442
  (ts.escalationManager.Escalate at :1440).
- Quickplan degradation: quick_plan mode falls back to fallback steps
  when planSinglePhase fails (internal/agent/strategic.go:419-427).

Mechanism: at those two hooks, UPDATE dispatch_log SET
outcome='failed_replan' WHERE task_id = failure.TaskID (dispatch_log
already stores task_id, store.go:325; recordDispatch extracts it at
dispatcher.go:3042-3044). This reuses the existing index surface; if
lookup-by-task proves hot, add idx_dispatch_log_task.

"no I meant X" free-text correction parsing is explicitly OUT of scope
for this design (NLP-dependent, low precision); Signal A is the
mechanical proxy.

## 3. Harvest pipeline

New tool tools/classifier-eval/harvest_outcomes.py, sibling of the
existing harvest_hermes.py (which established the UNTRACKED-output
pattern, tools/classifier-eval/harvest_hermes.py:1-10). Runs nightly
(local launchd/cron; meept has precedent for periodic daemons in
internal/daemon/components.go). Reads ~/.meept/metrics.db read-only.

(a) Correction corpus candidates:

- SELECT corrected rows (outcome='corrected' or 'failed_replan'),
  grouped by (input_hash, corrected_agent).
- The DB has no message text (by design, S4). The harvest emits a
  candidate sheet of {hash, session_id, ts, classifier choice,
  corrected_agent}. A companion LOCAL-ONLY join step (same machine as
  the daemon; output gitignored, like replay-gold.local.json5 --
  campaign hard rule 6: verbatim transcript text never enters git,
  classifier-iteration-campaign skill) recovers candidate text from
  local session transcripts by session_id + timestamp for the
  adjudication sheet format the user already works with (message +
  silver label + system answer + blank decision line).
- User adjudicates; accepted cases are appended to the adversarial
  corpus with provenance fields added_in: "harvest-YYYYMMDD",
  source: "live-session" -- same shape as iter18_cases.json5:8 --
  after the dedup guard: cosine > 0.95 to ANY existing case rejects
  (proven implementation tools/classifier-eval/iter7_harvest.py:67-69,
  embed server per campaign environment). scripts/
  build_prefilter_centroids.py rebuilds Door-1 centroids from any
  corpus afterwards.

(b) Near-miss abstention harvest (decision: store margin at dispatch
time):

Rationale: kNNVote.Margin (winner floor minus best losing neighbor,
internal/agent/embedding_prefilter.go:220-225, computed at :309) is
currently only logged on the direct-route path (:363-365) and is not
carried onto the returned Intent (:367-373). Abstentions return nil
from Match (:316) with the margin discarded -- exactly the rows we
need. Storing at dispatch time is the only complete option; back-
computing from logged vectors later would require re-embedding every
message (costly, non-deterministic across model versions).

Implementation: the prefilter block
(internal/agent/dispatcher.go:767-798) records one metrics row per
Match -- routed, suppressed by the H6 gates (:783-797), assert-only
(:769-776), or abstain -- with margin, asserted intent, and decision.
Minimal Go change: EmbeddingPrefilter.Match emits
(v.Confidence, v.Margin, verdict) through a callback/return struct so
the dispatcher can persist margin even when pi == nil. Nightly job
buckets margin near the threshold (DefaultPrefilterThreshold 0.70
gate, embedding_prefilter.go:83-88) to surface the near-miss band as
future corpus/adjudication candidates -- the same confusion-harvest ->
label-defect loop that beat gate tuning every iteration
(classifier-iteration-campaign skill, measurement protocol).

(c) Per-door accuracy dashboards:

- SQL views over dispatch_log (no new tables):
  accuracy by classifier_method over rows with outcome != 'pending',
  correction rate by session, margin histogram for Door 1, fallback
  frequency trend (replacing the in-memory-only FallbackDetails ring,
  dispatcher.go:2729/:2806, whose last-100 window dies on restart).
- Optional RPC surface: extend handleStatsQuery
  (internal/agent/dispatcher.go:2158-2167) with an outcome block, or
  leave the RPC as-is and expose views via sqlite3 directly. Prefer
  the latter for phase 1; the RPC extension is a leaf-local bonus.
- The empty classifier_method bucket keeps its documented meaning
  "non-classified path" (dispatcher.go:3046-3052) and is excluded
  from per-door accuracy.

## 4. Privacy

MUST NEVER be persisted raw (all already exist somewhere today --
this section is mostly a removal hardening):

- Message text: input_summary currently stores the first sentence or
  100 chars of the verbatim user message (handler.go:702,
  dispatcher.go:2665-2672). Discontinued in S1.
- Names/emails/keys appearing inside messages: same removal covers
  them; hash keys cannot be reversed for high-entropy content.
- Error strings: recordDispatch stores dispatchErr.Error() verbatim
  (dispatcher.go:3054-3057) -- LLM/transport errors can embed URL
  paths, model names, or request fragments. Truncate to 200 chars and
  drop anything matching an obvious key/token shape.
- Session/conversation IDs: retained (needed for Signal A); they are
  local opaque IDs.

Hashing scheme: input_hash = SHA-256(salt_id || "\x00" || message),
store first 16 hex chars. Unsalted hashes of short prompts
("commit this") are dictionary-attackable, so the salt is required.
Salt lifecycle: random 32 bytes generated once per install, stored in
the meept config directory next to metrics.db (default
~/.meept/, store.go:25), referenced by a salt_id column so a monthly
rotation is possible later without breaking old rows. Rotation is
optional; hash dedup is only ever compared within a salt.

Retention: keep dispatch_log in the standard 30-day purge list
(store.go:557). Nightly harvest consumes rows within a day, so 30
days is ample; if quarterly drift analysis is ever wanted, move ONLY
dispatch_log to a second, longer-cutoff purge loop (precedent for
per-table retention tiers: ModelPerformanceRetentionDays=90,
store.go:30-35; llm_calls is fully exempt per the tokscale Contract D
comment at store.go:33-34 and 554-556). Default recommendation: do
not extend; correction-derived corpus rows live in git as sanitized
adjudicated cases, not in the DB.

## 5. Phasing (leaf tree outline -- 4 leaves, do not author leaf docs)

- L1 schema-and-persistence: store.go migration columns + DispatchEntry
  extension + recordDispatch writes hash/model/turn_no + drop raw
  input_summary + error scrubbing + tests. No behavior change outside
  the metrics row. (Files: internal/metrics/store.go,
  internal/agent/dispatcher.go, llm_calls-style tests.)
- L2 door1-margin-capture: Match returns/forwards the kNN vote verdict
  incl. margin; dispatcher persists margin for routed + abstained +
  suppressed verdicts; log line gains margin fields. Independent of
  L1 outcome columns (only needs the margin column from L1).
- L3 outcome-capture: re-route detector UPDATE in recordDispatch
  (Signal A) + failure/replan hooks (Signal B at
  internal/agent/tactical.go:1440 and internal/agent/strategic.go:425)
  writing outcome/corrected_agent. Depends on L1 columns.
- L4 harvest-and-dashboards: tools/classifier-eval/harvest_outcomes.py
  + local text-join step (gitignored) + SQL views + corpus-append
  workflow honoring the cosine>0.95 dedup guard and added_in/source
  provenance; optional centroid rebuild step via
  scripts/build_prefilter_centroids.py. Depends on L1-L3 data but
  ships value with whatever rows exist.

Dependency graph: L1 -> {L2, L3} -> L4. L2 and L3 are parallelizable.
