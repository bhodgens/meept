# Model-Slot Fairness — Acquisition Audit (tree 04 leaf 03, Task 1)

> Audit date: 2026-09-01, against HEAD `962e9924` (tree 02 leaf 03).
> All line numbers are PRE-CHANGE (this leaf modifies some of the cited
> sites in its Task 3; cited numbers reflect the audited state).

## Question

Does model-concurrency acquisition have a wait list that interactive
acquires can jump? Map every acquire/release site and every candidate
gate; determine whether waiting requests are enumerable.

## Sites inventoried

### 1. The request-path gate: `Client.concurrencySemaphore` (base Client only)

| # | Site | File:line (pre-change) | Role | Blocking? | Wait list? | Scope |
|---|------|------------------------|------|-----------|-----------|-------|
| 1a | Field declaration | `internal/llm/client.go:116-118` | `concurrencySemaphore chan struct{}` — buffered channel used as a semaphore | — | — | per-Client instance (one resolved model config) |
| 1b | Construction, option path | `internal/llm/client.go:314-322` (`WithConcurrencyLimit`, `make` at :320) | builds the channel when `maxConcurrency > 0` | — | — | model |
| 1c | Construction, config path | `internal/llm/client.go:338-341` (inside `NewClient`, `make` at :340) | builds the channel when `config.MaxConcurrency > 0` — the PRODUCTION path | — | — | model |
| 1d | Acquire, non-streaming | `internal/llm/client.go:1055` (inside `doRequest`) | calls `acquireConcurrencyLimit(ctx)`; `defer release()` | blocking (select send vs `ctx.Done()`) | **NO** | model |
| 1e | Acquire, streaming | `internal/llm/client.go:1579` (inside `doStreamRequest`) | calls `acquireConcurrencyLimit(ctx)`; on ctx-cancel also releases the RPM slot; `defer release()` | blocking | **NO** | model |
| 1f | Acquire/release mechanics | `internal/llm/client.go:1980-1997` (`acquireConcurrencyLimit`) | nil gate → no-op release; grant = buffered send `c.concurrencySemaphore <- struct{}{}`; release = blocking receive `<-c.concurrencySemaphore` | blocking with ctx-cancel | **NO** | model |

**Wait-list verdict for site 1: there is NO enumerable wait list.** A
buffered channel holds at most `cap` tokens; once full, additional
senders park inside the Go runtime's internal sudog queue. That queue is
not addressable from Go code: waiters cannot be counted, inspected, or
reordered, and there is no priority hook — a sender wakes strictly in
runtime FIFO order. Proven from source: the only operations on the
channel in the entire repo (excluding `.bak` copies and worktrees) are
the four listed above — `make`, send-grant, receive-release, nil-check.
No slice/heap/priority select loop anywhere references the semaphore.

Note on retry interaction: each retry attempt of a logical request
re-enters `doRequest`/`doStreamRequest` and therefore re-acquires the
gate fresh (the release is deferred at attempt scope inside the called
function). Priority at this gate therefore applies per attempt, which
is the desired behavior — a retried interactive turn re-jumps.

### 2. `internal/llm/inuse.go` — RULED OUT (boot-time only)

`BuildModelsInUse` (inuse.go:35) computes the provider/model set that
RuntimeManager uses to skip starting unused local endpoints at daemon
boot (consumer asserted in runtime_manager_test.go:429+). It is a pure
set computation over agent definitions, config slots, aliases, and the
disabled list. It has **no acquire/release, no channel, no mutex, and no
request-path role**. Not a gate site; excluded from the two-lane swap.

### 3. `internal/llm/providers.go:54` — CONFIG ONLY

`ModelDef.MaxConcurrency` (providers.go:54) is a models.json5 field. It
is copied into `ModelConfig.MaxConcurrency` at providers.go:420 and
enforced exclusively via site 1c (per-Client semaphore construction).
There is **no provider-level gate** anywhere in the repo: no code
acquires a semaphore keyed by provider. The effective enforcement scope
is per model-instance Client, which is the model level. Per the leaf's
Notes: gate ONLY at the model level for this leaf (that is where the
only gate exists anyway); provider-level aggregation/priority is a
follow-up if ever needed.

### 4. Other clients — NOT gated (pre-existing scope fact)

`AnthropicClient` (anthropic.go:43) and `CodexClient` (codex.go:64) are
separate structs that do NOT embed the base `Client` and have no
concurrency field. Their transports (`AnthropicClient.doRequest`
anthropic.go:1105, `CodexClient.doRequest` codex.go:314) never call
`acquireConcurrencyLimit`. Today only the base Client's OpenAI-compat
non-streaming path (client.go:1055) and streaming path (client.go:1579)
gate. This leaf preserves that exact scope: the slotGate replaces the
channel inside the base Client only; no new gating is introduced on the
Anthropic/Codex transports (that would be a behavior change outside this
leaf's mandate). Recorded as a pre-existing asymmetry, not introduced
here.

## Outcome decision: **Outcome 2** (slotGate)

Per master.md Contract 3 and the leaf's recommendation:

- The audit confirms the premise: acquisition is a raw buffered-channel
  send with **no wait list** (Outcome 2's precondition). Outcome 1 (two-
  lane acquire on an existing wait list) has no structure to attach to.
- Queue-layer priority (tree 04 leaf 02, COMMITTED d7d2cddc/35c79887)
  orders which queued JOB claims first, but chat turns bypass the queue
  and queue jobs still contend at `MaxConcurrency` with chat turns and
  with each other. Without slot-level priority, an interactive turn can
  wait behind every background waiter already parked in the runtime's
  channel queue — the "concurrent connection per model" requirement is
  not met by the queue layer alone.
- Therefore: replace the raw channel with `slotGate` (mutex + two FIFO
  lanes + per-waiter grant channels acting as the condition broadcast),
  same acquire/release semantics, zero-alloc uncontended fast path,
  interactive priority with a 3-interactive-then-1-background starvation
  guard, ctx-cancel dequeues.

## Priority-input seam (frozen): `ChatOption llm.WithPriority(interactive bool)`

Chosen over a request-context field. Rationale:

- The repo's established per-request option pattern is `ChatOption` →
  `chatOptions` (WithGrammar/WithRawGrammar/taskID/sessionID all follow
  it). Priority rides the identical path: `WithPriority` sets
  `chatOpts.priority`; `Chat`/`ChatWithProgress`/`ChatWithDeltaCallback`
  already thread `chatOpts` into `doRequest`/`doStreamRequest`, which
  pass it to `acquireConcurrencyLimit(ctx, priority)`.
- Least surface: one `chatOptions` field + one option func + one bool
  parameter on the two gated transports. No context-key machinery, no
  interface changes (`Chatter`/streaming-chatter signatures unchanged —
  options are already variadic `ChatOption`).
- Zero behavior change for priority-less callers: `chatOptions.priority`
  defaults to `false` (zero value); `false` maps to the background lane
  with plain FIFO — byte-identical ordering semantics to today's channel.
- Priority SOURCE (per master Contract 3 "consumes"): the CALLING
  TURN/session, not the queue job. Chat turns are the interactive
  callers (`internal/agent` chat dispatch); specialist/goal/queue work
  is background (D11's two-tier rule — no third tier).

## Production wiring note (explicit scope boundary)

This leaf delivers the client-side seam and gate. The one-line call-site
that marks chat turns interactive (`llm.WithPriority(true)` at the
agent-loop chat dispatch, `internal/agent/loop.go:4389`/`:4257`
chatWithFailover/chatWithFailoverRaw) lives in `internal/agent`, which
is OUTSIDE this leaf's Task 3 Files list and inside tree-02-adjacent
hot territory. Until that dispatch lands, no production caller passes
priority and every caller takes the background lane — exactly today's
behavior. The wiring is queued for orchestrator dispatch; tests at the
client boundary prove the mechanism end-to-end via the public `Chat`
API.

## Change plan bound by this audit (Task 3)

- `client.go:118` field → `concurrencyGate *slotGate` (channel removed —
  no second mechanism left alongside).
- `client.go:320` and `:340` constructions → `newSlotGate(...)` behind
  the SAME conditions (`> 0`), same exported `WithConcurrencyLimit`
  name/semantics.
- `client.go:1055/1579` acquire sites pass `chatOpts.priority`;
  `acquireConcurrencyLimit(ctx, priority)` delegates to the gate.
- `Reconfigure` keeps today's behavior: the limit is fixed at
  construction and is not rebuilt on config swap (pre-existing fact,
  preserved).
