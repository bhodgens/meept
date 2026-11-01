# Durable-Execution Patterns for LLM Agent Workflows - Pinned Findings

Plan: docs/plans/20261031-turn-journal
Leaf: R0-research.md (research leaf; finding IDs freeze at REVIEWED)
Method: direct web_search this session. Every URL below was returned by a
live search run while writing this doc. Later leaves of this tree cite this
doc by path; the F/D IDs are stable citation anchors.

Verdict summary: the turn journal belongs to the DB-embedded durable-
execution family (DBOS / SQLite execution-log): a step-status table in the
same local SQLite the daemon already runs. External-server engines
(Temporal, Restate) are rejected on operating-footprint grounds. Journaling
happens at STEP boundaries (turn granularity); recorded steps are SKIPPED,
never re-executed; the unavoidable "executed but unrecorded" crash window is
covered by idempotency (the queue's claim tokens), not by a narrower window.

## F1: External-server durable execution is a mismatch for a single-node daemon

Temporal's architecture is a multi-service server (Frontend, History,
Matching, Worker services) with a required external database, plus SDK
workers polling it; its own production checklist says self-hosting demands
significant ongoing engineering, scaling, upgrade, and control-plane work.

- https://docs.temporal.io/encyclopedia/architecture/temporal-architecture
- https://docs.temporal.io/self-hosted-guide/production-checklist

Restate is a separate log-centric runtime (replicated "Bifrost" log +
partition processors with RocksDB state) the app's services stream to over
HTTP/2 - again a server process beside the application.

- https://docs.restate.dev/references/architecture
- https://docs.restate.dev/guides/request-lifecycle

Resonate is lighter (one binary whose state lives in your Postgres/SQLite)
but still a separate server beside the app, speaking its own protocol.

- https://www.resonatehq.io/durable-execution

Meept's daemon is a single Go process over local SQLite (parks.db,
effects.db); the prior Temporal proposal was REJECTED on exactly this
ground: running and operating a separate server/store beside the app buys
cluster-scale durability a single-node personal daemon cannot use.

## F2: DB-embedded durable execution gives the same resume guarantee with zero new infrastructure

DBOS implements durable workflows as a library: workflow inputs and step
outputs are checkpointed into an application-owned database (Postgres; the
Python guide defaults to SQLite). Recovery at startup scans for incomplete
workflows and re-runs each from its last completed step, consulting the
checkpoint before every step. "There's no separate orchestration server and
no infrastructure required besides" the database.

- https://docs.dbos.dev/architecture
- https://docs.dbos.dev/python/programming-guide
- https://docs.dbos.dev/why-dbos ("Temporal implements durable workflows in
  a heavyweight orchestration service, whereas DBOS implements them in a
  [database]-backed library")

The SQLite execution-log pattern makes the schema explicit: one row per
step keyed by PRIMARY KEY (flowId, step) with status, parameters, and
return_value; replay returns a COMPLETE row's recorded value instead of
re-executing. Gunnar Morling's Persistasaurus notes SQLite is "a great
choice ... when building a self-contained agentic system" - meept's exact
situation. turnjournal.db is this family.

- https://www.morling.dev/blog/building-durable-execution-engine-with-sqlite/

## F3: LLM calls are non-deterministic, slow, and costly - journaled results must be reused, never re-invoked

Agent-grade durable runtimes make memoization of completed steps the core
mechanism. Inngest: "step-level memoization ... an LLM call that originally
returned 'use the search tool' will return that same result on replay - the
call isn't re-made, its stored result is injected"; the agent picks up at
step 7 of 12, not step 1.

- https://www.inngest.com/docs/learn/durable-agents

FastAIAgent checkpoints at turn boundaries so a turn-boundary crash does not
redo prior turns, and turn-boundary resume re-issues only the in-flight LLM
call; completed tool calls replay from saved args/results.

- https://docs.fastaiagent.net/durability/concepts/

Because an LLM re-call can return a different answer, re-invoking a
completed step would change the replay path. DBOS states the same rule
generically: a checkpointed step "is never re-executed."

- https://docs.dbos.dev/architecture

## F4: Quarantine of side effects - recorded steps are SKIPPED; unrecorded non-determinism diverges replay

All surveyed engines require wrapping non-deterministic operations so their
results are journaled; anything unwrapped (clock, RNG, a raw effect)
executes again on replay and diverges. DBOS: do non-deterministic work in a
step, never in the workflow body. Restate: "non-deterministic operations
(database calls, HTTP requests, UUID generation) must be wrapped"
(ctx.run). The execution log then holds the side effect's recorded result,
which replay returns without re-firing.

- https://docs.dbos.dev/architecture
- https://docs.restate.dev/develop/ts/durable-steps
- https://docs.restate.dev/guides/request-lifecycle ("the SDK checks the
  journal ... If it finds a previous result, it skips executing that
  action")

Implication adopted by this tree: recorded steps are SKIPPED, never
re-executed, and completion dedup stays with the queue's existing claim
tokens (internal/queue/queue.go:491,539 - CompleteAttempt /
CompletionIsFresh: stale/duplicate completions are dropped via the
attempt-exact token predicate, and the queue owns that rule). The journal
must not re-implement the token/state freshness question.

## F5: The "executed but not yet recorded" crash window is unavoidable - mitigate with idempotency, not a narrower window

Morling's SQLite engine names it directly: "if the system crashes after a
step has been executed but before the result can be recorded in the log,
that step would be repeated ... it may be a good idea to add idempotency
keys to the requests." Exactly-once with external side effects is
impossible in general (the effect and its record cannot commit atomically
across a boundary); the durable fix is at-least-once execution plus
duplicate-tolerant effects / consumer-side dedup on a stable key.

- https://www.morling.dev/blog/building-durable-execution-engine-with-sqlite/
- https://learn.microsoft.com/en-us/azure/architecture/patterns/idempotent-consumer
- https://www.cs.princeton.edu/courses/archive/spr24/cos418/docs/L4-rpc-failure.pdf

The journal therefore shrinks neither the window nor the retry policy
claims; it guarantees resume position, and meept's existing effects ledger
(internal/effects) plus queue claim tokens carry the duplicate-tolerance
duty.

## F6: Journal growth needs a retention / GC story for closed runs

An append-only journal grows unboundedly. Every production engine ships a
pruning story: Temporal's per-namespace Retention Period cleans up closed
workflow executions (and Continue-As-New resets unbounded histories), DBOS
recommends configuring a retention policy for workflow history, and
dedup-record guidance says retention must be bounded or dedup tables
accumulate forever.

- https://docs.temporal.io/temporal-service/temporal-server
- https://docs.temporal.io/design-patterns/continue-as-new
- https://docs.dbos.dev/production/checklist
- https://learn.microsoft.com/en-us/azure/architecture/patterns/idempotent-consumer

turnjournal.db must prune journal rows for closed/completed turns (keeping
whatever resume-position metadata survives closure) or it becomes a slow
leak.

## D1: Journal at STEP boundaries (turn granularity), not full event-sourcing of every LLM call

Full event-sourcing (Temporal's Event History: every event of every
workflow) maximizes fidelity but costs unbounded history size and replay
time - Temporal itself caps it (50K events / 50MB) and adds Continue-As-New
to survive it. Agent runtimes that journal at turn/tool boundaries (Inngest
memoized steps; FastAIAgent turn:N and turn:N/tool:X checkpoints) get
next-step resume with one row per step. A turn-level ledger with one row per
turn (or per step of a turn) is sufficient for the daemon's resume needs
and keeps state far smaller; conversation message history already preserves
the LLM-facing transcript separately.

- https://docs.temporal.io/design-patterns/continue-as-new
- https://temporal.io/blog/very-long-running-workflows
- https://www.inngest.com/docs/learn/durable-agents
- https://docs.fastaiagent.net/durability/concepts/

## D2: Recovery = deterministic control flow + journaled results; the resume path consults the journal, never live state alone

DBOS's recovery contract: restart the workflow from its checkpointed
inputs; before each step, check for a checkpoint - if present, return the
checkpointed output instead of executing; determinism of the workflow
function plus checkpointed steps makes the replay path identical. Restate's
replay does the same with its execution journal. The daemon's turn-resume
must therefore re-derive its position from turnjournal.db rows (status +
recorded results) rather than re-deciding from volatile in-memory or
live-queue state; queue-side freshness questions still route through the
queue's claim-token predicate (F4).

- https://docs.dbos.dev/architecture
- https://docs.restate.dev/guides/request-lifecycle
- https://www.inngest.com/docs/learn/durable-agents (memoized replay
  reproduces the exact dynamic path)

## D3: Rollout default follows meept's opt-in rule - `daemon.turn_journal_enabled` defaults false

docs/agents/opt-in-defaults.md: opt-in features ship default-false and
"flipping a default is a product decision." The turn journal follows the
same lane: `daemon.turn_journal_enabled` (default false), flipping later is
an explicit product decision, and the journal rides the daemon's existing
local-SQLite conventions (WAL + busy-timeout DSN form used by
park/session/queue stores; see internal/agent/park_store_sqlite.go:121-129)
rather than introducing a new storage substrate (F2: zero new
infrastructure is the family's whole point).

- https://docs.dbos.dev/python/programming-guide (durability as an
  application-library choice over an existing local DB, not new infra)
