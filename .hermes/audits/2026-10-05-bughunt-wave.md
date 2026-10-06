# Meept bughunt wave 2026-10-05 — verified findings (report-only)

Scope: 38 commits, `f5d369bf..ee0de064`, September 30 – October 5. HEAD at audit
start and at close: `ee0de064`. Working-tree state: 7 modified files (6 plan /
doc tracking + 1 public feature matrix) and 1 untracked path
(`internal/security/testdata/fuzz/`), both audited in JOB 2 below. A live sibling
session committed `docs/plans/sealed-plan.md` mid-wave; every finding was
re-checked against the tree at final HEAD.

Six read-only auditors (delegation `deleg_ba0c3a63`, scopes A–F) plus 18 nested
sub-delegations. The parent source-verified every CRITICAL and HIGH, plus the
two parent findings auditors missed or refuted. Report-only: **no repository fix
applied**.

Baseline (parent, verified at `ee0de064`): `go build ./...` clean;
`go test -p 2 -count=1 ./...` = **107 packages ok, 0 FAIL**
(`/tmp/bughunt-20261005-go-baseline.log`); `go vet ./...` clean;
`go vet -tags e2e ./e2e/...` clean; test-only build break: none.

## Gate state at HEAD (verified by parent, not auditor report)

| Gate | State | Evidence |
|---|---|---|
| `go build ./...` | GREEN | BUILD_OK, exit 0 |
| `go test -p 2 -count=1 ./...` | GREEN | 107 ok / 0 FAIL |
| `go vet ./...` | GREEN | exit 0, no findings |
| `make graphs-check` | **RED** (exit 2) | `❌ Stale generated files: bus-topology.json, http-routes.json, bus-topology.md` |
| `gofmt -l internal/ pkg/ cmd/` | **RED** (6 files) | see M3 |
| plain `golangci-lint run` | **RED** (exit 3) | `Error: build linters: plugin(mutexio): plugin "mutexio" not found` |
| `make lint` | GREEN *only* with untracked 44 MB `./custom-gcl` present | `Makefile:703-708` |

---

## HIGH

**H1 — The graph generator fabricates bus topics: commit `81808038` renamed a
method to dodge an anti-fabrication guard and injected 6 non-existent topics.**

The generator excludes the bare name `publish` from publish-helper detection
(`scripts/gen-connectivity-graph.py:277`, `seen_names -= {"Publish", "publish"}`).
Commit `81808038` renamed `RoutingTelemetry.publish` → `emit` so its three topics
would register. `emit` was never on the deny list, so `re_call`
(`scripts/gen-connectivity-graph.py:286-288`) — which matches call sites by bare
method name with **no receiver-type check** — now scans every `.emit(` in the
module. That swept in `internal/acp`'s unrelated channel emitter:

```go
// internal/acp/session.go:180
func (s *Session) emit(ev SessionEvent) {
```

That method writes to `s.events` (`internal/acp/session.go:56`, a Go channel).
`internal/acp` does not import `internal/bus` and contains zero `.Publish(` calls.

Verified at HEAD — `docs/generated/bus-topology.json` publishes six topics whose
only publisher is that channel-send helper:

```
closed  done  error  message_chunk  permission_request  tool_call
```

Proof they are fabricated: `grep 'Publish[^\n]*"(closed|done|error|message_chunk|permission_request|tool_call)"'`
over all `.go` files returns **0 hits**. All six appear in
`orphans.published_not_subscribed` (`docs/generated/bus-topology.md:267,272,273`)
and carry payload keys harvested from unrelated nearby map literals
(`prompt`, `text`, `type`, `outcome`, `agent_message_chunk`, `tool_call_update`).

*Failure mode:* an engineer grepping the topology graph for `error` gets a bus
topic that does not exist, and `ANNOTATED_ORPHANS`
(`scripts/gen-connectivity-graph.py:704-722`) will need fabricated suppressions
to silence it.

*Fix shape:* the guard must be structural, not name-based — require the helper's
receiver to hold a `*bus.MessageBus`. The rename was correct as a diagnosis and
wrong as a mechanism.

*Two auditors disagreed on this delta and the parent caught it:* one reported
"5 routing topics added, 0 removed"; the direct diff shows **11** added, 0
removed. Commit-message claims are audit targets even for self-reported diffs.

---

**H2 — `make lint` fails on every clone without an untracked 44 MB binary.**

`f0f2e149` registered `mutexio` as a golangci-lint **module plugin**
(`.golangci.yml:70` in the linters list, `.golangci.yml:134-137` under `custom:`).
Verified at HEAD on this machine, where the custom binary *does* exist:

```
$ golangci-lint run ./internal/queue/...
Error: build linters: plugin(mutexio): plugin "mutexio" not found
PLAIN_GCL_EXIT=3
```

The `Makefile:706-708` fallback ("falls back to plain golangci-lint, which still
passes but emits the benign warning") is dead code: plain golangci-lint cannot
load the config. `./custom-gcl` is 44 MB, **gitignored** (`.gitignore:135`),
built only by a manual `golangci-lint custom`, and no CI step or hook builds it
(`grep -rn 'golangci-lint custom\|custom-gcl' .github/ .githooks/` → no hits).
`.github/workflows/ci.yml:54-61` runs bare `golangci-lint run ... || true`,
so CI stays green while the gate is dead.

`.golangci.yml:5-12` still carries the pre-`f0f2e149` text saying registering
these as plugins "would require a plugin build pipeline" — the exact claim this
commit invalidated.

*Failure mode:* `make lint` and `make lint-ci` exit non-zero on every machine and
runner that does not happen to have the binary.

---

**H3 — The Flutter GUI's only non-terminal path for clearing the thinking
indicator can never fire against the real daemon.**

`c4dc25ee` gated completion on a non-empty message id:

```dart
// ui/flutter_ui/lib/providers/chat_provider.dart:1248-1253
final newIsAgentProcessing =
    (message.role == 'assistant' && message.id.isNotEmpty && message.content.isNotEmpty)
    ? false
    : state.isAgentProcessing;
```

But `server.go:825-827` injects an id into **every** relayed `chat_message`:

```go
if _, ok := payload["id"]; !ok {
    payload["id"] = msg.ID
}
```

`internal/agent/handler.go:1565-1572` never sets `id` in the payload
(`role/content/session_id/conversation_id/error/timestamp` only), so the relay
always supplies one. The Flutter client connects to the same server's `/ws`
(`ui/flutter_ui/lib/core/constants.dart:24-27`, default path `/ws`; served at
`internal/comm/http/server.go:1272`), so the injection is on the GUI's path.
`id.isEmpty` is therefore never true in production and the branch is dead.

The steer path has no other clear: `chat_provider.dart:966-968` sets
`isAgentProcessing: true`, and the steer early-return at `:1053` copies
`isLoading: false` only.

*Failure mode:* when `turn.terminal` is missed (parked-then-dropped, dropped
socket, daemon restart), the GUI spinner runs forever — the exact bug
`c4dc25ee` claims to fix, inverted. `_processingFallbackTimer`
(`chat_provider.dart:393`) is declared with a comment promising a fallback and is
**never armed** (only cancelled), so nothing bounds the state.

---

**H4 — `Complete`'s state guard cannot distinguish a legitimate retry from a
stale completion; a retrying client gets 409 for a request that already
succeeded.**

```go
// internal/queue/store.go:511-531
	UPDATE jobs SET state = 'completed', result = ?, updated_at = ?
	WHERE id = ? AND state IN ('claimed', 'processing')
...
if affected == 0 { return ErrJobNotClaimable }
```

The predicate is job state only — no worker id, claim token, or completion
epoch. The first completion moves the row to `completed`, so the *identical*
request retried by the same worker gets `RowsAffected()==0` →
`ErrJobNotClaimable` → HTTP 409 (`internal/comm/http/api_handlers.go:194-198`).
The in-repo pin **encodes this as the desired contract**
(`internal/queue/store_complete_guard_test.go:96-107` asserts the second
`Complete` fails). `81ea4585`'s own message records the root cause ("The real fix
is a completion epoch/token; deferred").

Second-order effect, independently reported and verified at
`internal/worker/worker.go:385-393`: `JobsComplete++` runs *before* `Complete`,
so the error return skips the run loop's `else if processed` branch — backoff is
not reset and a successful job is logged as a processing failure. Because
`PersistentQueue.Complete` returns early without publishing
(`internal/queue/queue.go:362-368`), a job the store accepted can produce no
`queue.job.completed`, and `OnJobCompleted` — the task-finalization trigger —
never runs.

*Mitigating (auditors, verified):* a legitimate in-process retry cannot 409. The
worker path is `MarkProcessing` (`internal/worker/worker.go:303`) → `Complete`
(`:390`), and any re-claim restores `claimed` first
(`internal/queue/store.go:472-483`). The 409 fires only for cross-request
retries, which no shipped caller makes today.

---

**H5 — The stale-completion guard drops the result of a long-running job that a
cluster reclaim reset, leaving the task non-terminal.**

`internal/agent/tactical.go:1218-1237` drops a completion whose queue job is not
`claimed`/`processing`/`completed`. `Store.Complete`
(`internal/queue/store.go:513`) then refuses the write for the same job. The
window: `ReclaimIfStale` → `ResetToPending`
(`internal/queue/store.go:737`, `WHERE id = ? AND state IN ('failed','claimed','processing')`)
flips a still-executing job to `pending` while its worker runs.

*Failure mode:* a step job that outlives the cluster claim timeout has its
successful completion discarded — no result write, no event — and the step never
terminalizes. Recorded as "Reviewed and NOT fixed … MEDIUM" in `81ea4585`; the
*consequence* (a hung task, not a lost log line) is worse than recorded.

Reachability today is narrow: `ReclaimIfStale` is called only from
`internal/cluster/integration_test.go`. `ResetStaleClaimsAtStartup`
(`internal/daemon/daemon.go:2012`) runs every boot and is the single-node path
that can interleave with a `Complete`. Class: latent hazard, known-open.

---

**H6 — The GUI's one unsubscribe frame suppresses three streams, because the
daemon's session filter is channel-blind.**

`5073c159` made the flat Flutter frame shape parse, so the GUI's
`unsubscribeFromChat` now actually reaches the daemon — and removes that session
from the *shared* filter map:

```go
// internal/comm/http/server.go:2644 (flat alias) -> UnsubscribeSession
if subs := h.sessionSubs[wc]; subs != nil { delete(subs, sessionID) }
```

`subscribeToAgentProgress` (`websocket_service.dart:751`) and
`subscribeToTurnTerminal` (`:789`) register the **same session id under the same
channel-blind map**, so `dispose()` (`chat_provider.dart:1388`) suppresses all
three.

*Failure mode:* navigating away from a session in the GUI suppresses that
session's progress and turn-terminal too; any frame in the dispose→re-subscribe
window is dropped and the turn false-stalls to the 120 s liveness timer.
Bounded by re-subscribe on reload.

---

**H7 — `agent.progress.synthesized` is dropped for a session-less event while
every other relay broadcasts it.**

`264fedba` correctly removed the double-delivery path
(`internal/comm/http/server.go:721-723` drops the topic before classification).
But `handleWSEvent` has a broadcast bypass the typed path lacks:

```go
// server.go:623 (wildcard relay)
if eventSessionID == "" || h.ShouldSendProgress(wc, eventSessionID) {
// server.go:688 (typed handleWSProgress)
if h.ShouldSendProgress(wc, event.SessionID) {
```

`event.SessionID` derives from `event.ConversationID`
(`internal/agent/progress_synthesizer.go:115,154,172,191`); when an `AgentEvent`
carries an empty `ConversationID`, `ShouldSendProgress(wc, "")` is false for every
armed connection. Pre-existing asymmetry, but `264fedba` made the typed path
load-bearing.

*Failure mode:* a synthesized progress event with no conversation id reaches no
filter-armed client — the progress line silently vanishes.

---

**H8 — TUI `agentActive` is set by *any* session's agent loop: cross-session
steer bleed.**

```go
// internal/tui/models/chat.go:1534-1541
if msg.Active {
    m.agentActive = true
} else if !msg.Active && (msg.ConversationID == "" || msg.ConversationID == m.conversationID) {
```

The `Active=true` arm dropped the conversation filter; only the `Active=false`
arm still compares. `sendMessage` routes on `m.agentActive`
(`internal/tui/models/chat.go:1844`) and `SteerQueue` sends `m.conversationID`
(`:3407`). No test covers `AgentLifecycleMsg{Active:true, ConversationID:"other"}`
— `chat_test.go:1141` only ever uses `model.conversationID`, and `:1537` pins
the opposite rule for `SetAgentActive`.

*Failure mode:* while session B is bound, a background agent from session A
publishes `agent.lifecycle.started` → `agentActive=true` → the user's first
message in session B is queued as a steer into session A instead of opening a
turn. The filter existed to prevent exactly this; the fix inverted it. Correct
fix is a stable session/thread key, not deleting the comparison.

---

**H9 — TUI double-delivers every `agent.event.*` and `agent.progress.synthesized`
event (parent finding, verified).**

`4f92b6ca` added `agent.*.*` and `agent.*.*.*`
(`internal/tui/events.go:52-53`) but left `agent.event.*` and `agent.progress.*`
(`:54-55`). `matchWildcard` matches a topic against **every** matching pattern
(`internal/bus/bus.go:440`), and `handleBusSubscribe` creates one collector per
pattern (`internal/rpc/proxy.go:452-456`), appending each to `sub.Events`
(`:478-499`). `handleBusPoll` returns every buffered record
(`internal/rpc/proxy.go:553-558`) with no dedupe.

Verified match set (`/tmp/bughunt_topic_match.py`): five topics match two
patterns each — `agent.progress.synthesized`, `agent.event.turn_start`,
`agent.event.turn_end`, `agent.event.tool_execution_start`,
`agent.event.tool_execution_end`.

Impact is split: the viz handlers push into a map-keyed pending buffer
(`internal/tui/viz/dispatch.go:360-368`) so the double-push is idempotent, but
`updateActivityFeed` (`internal/tui/sidebar.go:676-695`) renders `RecentEvents(10)`
verbatim — **every agent event appears twice in the activity feed**.

---

## MEDIUM

**M1 — The quota-aware routing warning is permanently dark: the alias branch of
the health adapter is unreachable.** `internal/daemon/components.go:3044-3050`
sets `ref = agentID` (a bare alias) when no agent declares `model:`; then
`resolver.ResolveRef(ref)` → `ResolveModelRef`
(`internal/llm/providers.go:549-553`) requires `provider/model` and returns nil
for a bare name. No shipped agent sets `model:` in its `AGENT.md` (verified: zero
matches across `config/agents/**`), so the lookup returns `false` for **every**
agent and `AgentParkedOrCooling` never fires. *Failure mode:* a genuinely parked
or cooling routed agent emits no `routing.warning` — silently, with no error
anywhere. The adapter's `alias` health check (`agent_health_adapter.go:76`) is
dead in production. Not live-verified (no scratch test); established by reading
`providers.go` plus the absence of any `model:` key.

**M2 — `Retry` and `Requeue` return jobs to `pending` without signalling any
waiter, so the event-driven wake-up silently degrades to polling on the retry
path.** Only `Enqueue` wakes waiters (`internal/queue/queue.go:167-181`); `Retry`
(`:400-417`), `Requeue` (`:428`) and `RecoverFromDeadLetter` (`:481`) do not.
*Failure mode:* a job requeued for attempt 2 waits for the worker's poll timer —
up to `maxIdleBackoff` = 15 s (`internal/worker/worker.go:190`) — with no log
line. Latency regression, not job loss; the scan always recovers it.
Same for `ClusterQueue`: it embeds `Queue`, and `WakeNotifier` is deliberately
not on `Queue` (`internal/queue/queue.go:552-560`), so
`p.queue.(queue.WakeNotifier)` (`internal/worker/pool.go:198`) fails in cluster
mode. Latent today (the pool is built from the unwrapped `PersistentQueue`).

**M3 — Six files fail `gofmt`, three introduced in this range; CI permits it.**
Verified: `internal/comm/http/api_handlers.go` (81ea4585, unsorted
`internal/queue` import), `internal/comm/http/server_ws_subscribe_flat_test.go`
(5073c159), `internal/daemon/session_hooks.go` (f0f2e149),
`internal/tui/golden_breadth_test.go` (b1e592da), `internal/tui/golden_test.go`
(763d01c5), `internal/tui/golden_views_test.go` (a380bd2e). `api_handlers.go` was
clean at `81808038`. No gofmt/gci linter is enabled in `.golangci.yml`, so
`make lint-ci` cannot catch it. `a380bd2e`'s message claims "zero golangci-lint
findings repo-wide" — false as stated.

**M4 — `make graphs-check` is RED at HEAD; CI job `generated-artifacts` fails.**
Verified: exit 2, `❌ Stale generated files: bus-topology.json, http-routes.json,
bus-topology.md`. Drift is absolute line offsets only (the documented AGENTS.md
F66 class) — `routing.telemetry` 1416→1419, `routing.warning` 2597→2600,
`internal/queue/queue.go` 329→332, `internal/daemon/components.go` 8524→8571 —
caused by `81ea4585` editing those files without regenerating. The *content*
diff (topic sets, subscribers, orphans) is correct. `Makefile:797-804` still
carries a comment telling maintainers the check is **not** wired into CI, while
`.github/workflows/code-quality.yml:75-76` runs it as a hard gate.

**M5 — `expected_language` is unvalidated and fails closed on any typo.**
`internal/config/schema.go:710` has no validation; `langOrDefault` substitutes
`"en"` only for the empty string (`internal/validator/filter_language.go:298`);
every non-empty value passes through verbatim, and an unknown code matches
nothing (`internal/validator/filter_language.go:229-237`). *Failure mode:*
`expected_language: "EN"`, `"english"`, or `"en-US"` rejects **every** prose
output including the correct language, burning filter retries to
`rejected_exhausted` — a one-character typo is a self-inflicted denial of service
on the chat path, signalled only by a `lang=` reason string. Default (`""`) is
exactly the previous behavior.

**M6 — The word-table loader splits on whitespace but detection tokenizes on
non-letters, so entries silently never match.**
`internal/validator/filter_language.go:119` (`strings.FieldsSeq`) versus
`:171-173` (`strings.FieldsFunc` on non-letters). *Failure mode:* a maintainer
writes the natural `de, la, les, …`; all tokens clear the `len(words) < 20` floor,
load without error, and never match — the language becomes undetectable. Same
root cause makes `#` comment stripping per-token, so comment words leak into the
table (triggered by the repo's own fixture,
`e2e/suites/output-filters/output_filters_language_test.go:46`).

**M7 — `detectedLanguage` reads the package-level table map outside its lock.**
`internal/validator/filter_language.go:194` reads `extraLangTables` unguarded
while `:105` writes it under `loadMu` and `:113`/`:250` read it under `loadMu`.
It is inside the per-word loop; the `snapshotExtraTables()` call on the next line
already returns nil safely. *Failure mode:* a concurrent
`LoadLanguageWordTables` (`internal/daemon/filter_wiring.go:45` at boot, tests
per-case) races the header read — `-race` failure and a torn map header.

**M8 — TUI golden normalization masks a wrong-month rendering regression.**
`internal/tui/golden_test.go:83` collapses month-name + day to a fixed token:
`monthDayRe = \b(Jan|…|Dec) \d{1,2}\b` applied at `:66`. A golden showing `Oct 05`
is byte-identical to one showing `Nov 05` after normalization, and it also
collapses the day (`Aug 21` ≡ `Aug 1`), so single-digit vs zero-padded day
regressions are hidden too. *Fix shape:* the instability is only the **month**
at a month boundary — normalize that token in the fixture, not month+day.

**M9 — Golden fixtures were re-baselined to a regressed render, and a retry loop
plus 10 untracked `.got.txt` artifacts hide the flake.**
`internal/tui/testdata/golden/{tasks,sessions}_view.txt` changed from a 78/88-col
layout to a **128–180-col** layout with a sidebar at
`goldenWidth, goldenHeight = 80, 24` (`golden_views_test.go:29`) — the committed
fixtures no longer describe an 80-col render. `tasks_view.txt` also lost content:
`████████2/2` → `██`, where `renderProgressBar` always emits
`fmt.Sprintf("%s %d/%d", bar, completed, total)` (`internal/tui/tasks.go:726`), so
the fixture pins a **truncated** progress bar as expected output.
`retryWithFreshApp` (`golden_views_test.go:50`) discards a mismatching render and
retries up to 4×, and `assertGolden` writes `.got.txt` on every mismatch
(`golden_test.go:173`): **10 such artifacts exist now** and are not gitignored.

**M10 — `EventStream.done` is not re-armable: a stopped stream can never
restart, and a second `Stop` can double-close.** `close(es.done)`
(`internal/tui/events.go:141`) on a channel allocated once at `:84`; `Start` never
re-creates it. Reached by the ctrl+c double-press path
(`internal/tui/app.go:825` → `sidebar.Cleanup()`) and any reconnect.
`sidebar.Init()` on the compact→wide path (`internal/tui/app.go:2818-2820`) is a
no-op against a still-running stream (the shrink path never calls `Stop`), so the
subscription is permanently live rather than freshly re-armed — the inverse of
what the commit claims.

**M11 — `UpdatePhaseSteps` omits `suggested_next_hint` while `Update` includes
it.** `internal/task/step.go:620` sets it; the phase path's SET list
(`internal/task/step.go:713`, called from
`internal/agent/orchestrator_phases.go:148`) does not (verified one-sided:
28 placeholders / 28 args, a clean omission). *Failure mode:* a hint adopted on
one path silently fails to persist on the phase path. Advisory-only blast radius.

**M12 — The `gui-flows` suite is unreachable by CI.** `e2e/manifest.json`'s
`gui-flows` entry carries `dir: "ui/flutter_ui/test/e2e"` and a note to run
`cd ui/flutter_ui && flutter test`; verified: **zero** `path_map` keys reference
it, no key starts with `ui/`, and `grep -rn 'flutter test' Makefile .github/
scripts/` returns nothing (`make e2e-fast` is `go test -tags e2e ./e2e/...`,
`Makefile:1498`). `gui-stream-02` is not registered as a manifest scenario at all.
*Failure mode:* every `ui/flutter_ui/lib/**` change selects no suite, so all four
`gui-*-01` scenarios and `gui-stream-02` can never fail. This directly violates
the root AGENTS.md rule requiring a suite + manifest entry in the same commit.

**M13 — The strongest-looking new e2e scenario cannot fail.**
`e2e/suites/tui-flows/tui_flows_test.go:164-170` asserts only
`!strings.Contains(view, "agents unavailable")` and
`strings.TrimSpace(view) != ""`. Verified unreachable: `"agents unavailable"` is
emitted only when `a.agents == nil` (`internal/tui/app.go:2632-2636`), and
`NewApp` always sets `agents: NewAgentsPanel(rpc)` (`internal/tui/app.go:329`)
with no reassignment in non-test code; `View()` unconditionally appends
`a.renderStatusBar()` (`internal/tui/app.go:2651`), so `Content` is never blank.
*Failure mode:* deleting `AgentsPanel.View()` entirely still passes — the
scenario it claims to pin (`tui-agent-tab-01`) is decorative.

---

## LOW

- **L1 — The scan-bound `Warn` floods.** `internal/queue/queue.go:297-306` warns on every claim attempt once the 500-row bound is hit, with no rate limit or once-only latch — one Warn per worker per poll, exactly when an operator is diagnosing starvation. Work is **deferred, not lost**.
- **L2 — GUI `/clear` and `/new` wipe `pendingTurns` mid-turn** (TUI parity break). `chat_provider.dart:1367-1369` sets `state = const ChatState()`; the TUI's `ClearConversation` never clears pending turns. The arriving `turn.terminal` lands in `_earlyTerminals`, drained only on submit-ack registration, so the reply never renders.
- **L3 — `loadMessages` cancels the chat subscription before the HTTP fetch and re-subscribes after** (`chat_provider.dart:415`, `:474-481`). A mid-turn reload leaves a window where deltas are neither delivered nor queued — the stream truncates after switching sessions.
- **L4 — Anthropic and Codex clients never classify empty/whitespace completions**, contradicting `internal/llm/AGENTS.md` ("Empty/whitespace completions are provider flakes… classifies as `ErrEmptyResponse`"). `internal/llm/anthropic.go:1916` `parseResponse` returns `*Response` with no error return and no trim branch. The openai loops are correct (verified: `client.go:2060` trims, refusal precedes empty at `:2041`, bare sentinel returned at exhaustion `:665`/`:918`/`:2285`, streaming bound fixed at `:2276`).
- **L5 — The bare-sentinel identity pin covers only one of three retry loops.** `internal/llm/client_empty_completion_test.go:263` pins pointer identity for `ChatWithDeltaCallback`; the non-streaming `Chat` sibling asserts `errors.Is` plus a `&&`-guarded message check that cannot fail on a wrap carrying a different outer message.
- **L6 — `rotatingWriter.Close` panics on a nil receiver; the guard is dead code.** `internal/llm/runtime_logs.go:237-238` — `if w == nil || w.file == nil { w.mu.Lock() }` dereferences `w` in the body. Latent (all constructors set `out`); **pre-existing**, but `bf6150c2` moved the check and left it broken. Otherwise `bf6150c2` does restore 47ed44f4's invariant correctly: no pre-lock fast path remains, Close is idempotent (`:244`), and the residual window is closed.
- **L7 — `internal/cluster`'s send semaphore is never written.** `sem := make(chan struct{}, 32)` (`internal/cluster/gossip.go:159`) with `defer func() { <-sem }()` (`:270`) and no `sem <- struct{}{}` anywhere — every peer-send goroutine parks forever at the deferred receive. The comment at `:157-158` describes the opposite of the code. The retry loop itself is correct (hard `maxAttempts` at `gossip.go:652`, bounded 64-slot channel at `:161`).
- **L8 — `UpdatePhaseSteps`-adjacent: `Store.UpdateState` has no state guard**, so `MarkProcessing` can resurrect a completed/dead job into `processing` — asymmetric with the new `Complete` guard. `internal/queue/store.go:496-501`.
- **L9 — `routing.decision` double-counts a hint-table route** on every reschedule (`internal/agent/tactical.go:2630` emits `source:"explicit"` after `selectAgent` already recorded), and `SelectAgentForHint` builds a throwaway `&task.TaskStep{}` with an empty `step.ID`, so chunking-path publishes carry `step_id: ""` — unattributable telemetry rows.
- **L10 — `cluster`'s `Complete` never deletes `cq.claimed[jobID]`** (`internal/queue/cluster_queue.go:107-124`), so `Stats().LocalClaims` over-counts until a reclaim sweep. Pre-existing.
- **L11 (prior wave, re-judged) — still fail-safe.** `internal/session/thread_resolve.go:42-49` runs `LastIndex(id, "-thread-")` only after two direct lookups (`:30-39`), so a false trigger yields `nil` — a clean miss, not a wrong-session binding. Reachable: `internal/services/thread_service.go:44` builds `"thread-" + req.TopicLabel + ...` with no slug sanitize. The risk is **undocumented** — zero hits for the function in `docs/` or any `AGENTS.md`; it lives only in the audit table. Closing it as "intended" contradicts the repo's own rule that every carried risk needs an owner.
- **L12 — The word-table loader and detector disagree on tokenization** (companion to M6): `#` stripping is per-token, so a trailing comment's words enter the table.
- **L13 — `docs/feature-comparison-matrix.md` ships three contradictory counts.** The uncommitted edit declares the catalog is "22 entries" (verified correct) and corrects the enabled count, but leaves `AGENTS.md:462` ("7 enabled") and `docs/configuration/llm-lifecycle.md:346` ("21 preconfigured servers (6 enabled by default)") uncorrected; actual is 8 enabled. No shipped behavior changes — nothing parses the file — but a public doc that claims to *correct* a number while the canonical `AGENTS.md` stays wrong is worse than no edit. Its evidence lives only in `$TMPDIR/429audit/*.md`, so the 285 new lines cannot be re-verified by any reviewer.
- **L14 — The untracked fuzz corpus entry is a stale crasher, not a live bug.** `internal/security/testdata/fuzz/FuzzInputSanitizer/828d5059791d0353` decodes to the single byte `0xE0` (invalid UTF-8). Go writes to `testdata/fuzz/` only for crashers (`internal/fuzz/fuzz.go:157`, `:274`); passing inputs go to `$GOCACHE/fuzz`. The subtest **passes** at HEAD because `internal/security/fuzz_test.go:56` makes the invariant vacuous for invalid UTF-8:
  ```go
  if utf8.ValidString(text) && !utf8.ValidString(res.CleanText) {
  ```
  Not gitignored (`git check-ignore` exits 1) — one `git add -A` from being committed as a misleading permanently-passing "reproducer". Not committed; not deleted.
- **L15 — `internal/memory/distill.go` content-keyed dedupe contradicts a stated repo convention.** `:428`/`:461` key on `canonicalDedupeText(kind, content)`; no id, session, or source hash. Root AGENTS.md names this verbatim as a hacky pattern and requires a `TODO`; none is present. Gated off by default (`schema.go:2983`), so blast radius is opt-in only.

---

## REFUTED (dispatch premises that proved false)

1. **"The `emit` wrapper hides topics from the generator"** — false; the rename worked for its stated purpose. The three routing topics are present with `via_helper: true`. The defect is the *collateral* fabrication (H1).
2. **"The graph drift is a topology defect"** — false; 100% absolute line offsets (documented F66 class). Only the CI impact is real (M4).
3. **"Struct-vs-ConfigSnapshot: fields were missed"** — refuted. `TacticalScheduler` (`telemetry`, `health`), `PersistentQueue` (`wakeMu`, `wakeWaiters`) and `StepJobPayload.SuggestedNextHint` have no clone/export/snapshot with an explicit field list; all are constructor-initialized and correctly propagated. `UpdatePhaseSteps` is the one genuine gap (M11).
4. **"A lost-wakeup race exists in the wake path"** — refuted. `wakeCh` is buffered(1) (`internal/worker/pool.go:199`), so a signal arriving mid-job is buffered, not dropped; every send is `select`/`default`, so none can block under a mutex. `mutexio` clean on the whole repo.
5. **"Claim-pinning permits double execution"** — refuted. `ClaimNextForAgent` does SELECT+UPDATE in one transaction with a `WHERE state='pending'` CAS and a `RowsAffected` check (`internal/queue/store.go:472-483`); the slow path repeats the CAS (`:1504-1515`). The `id ASC` tiebreaker added in this range is what makes the CAS + keyset combination sound.
6. **"Only 1 of 4 segment agent topics was subscribed"** — refuted; `events.go:47-57` now carries all four forms and no `agent.*` literal has 5+ segments. The defect is over-subscription (H9), not under.
7. **"A leaked bus subscription per compact→wide cycle"** — refuted as stated; `EventStream.Start` is `running`-guarded (`events.go:94`) so no second `bus.subscribe` is issued. The real defect is M10.
8. **"Tables sized on height only / direct `SetRows` with an over-long row"** — refuted. All five tables go through `tableutil.Size` on both axes (`tasks.go:212`, `sessions.go:128`, `queue.go:86`, `plans.go:118`, `agents_panel.go:211`); every direct `SetRows([]table.Row{})` is a clear-before-`SetColumns` with an empty slice; `tableutil.SetRows` normalizes every row to `len(t.Columns())`.
9. **"The empty-completion test only asserts `err != nil`"** — refuted. Typed pins at `client_empty_completion_test.go:55,115,197,257,290,336`, `errors.AsType[*RefusalError]` at `:166`, pointer identity at `:263`. Whitespace does reach the same branch and real content passes byte-identical. The prior-wave streaming off-by-one is genuinely fixed.
10. **"The empty-completion change weakened the test"** — refuted; the file's in-range diff is **one comment line**. The L5 pin gap is pre-existing.
11. **"The `dd0cfd2a` de-flake weakened assertions"** — refuted. The pre-restart persistence check widened to `pending|claimed|processing` (all three prove durability in `queue.db`, which is that assertion's job), but the load-bearing post-restart assertions are untouched: `task_queue_test.go:549-551` (`job %s lost across restart`) and `:582-584` (the `updated_at` baseline that makes the pending branch non-vacuous). The SP04 change to an unbounded script **strengthens** the test.
12. **"`763d01c5` weakened the golden assertion"** — refuted as a *byte-compare* claim: `golden_test.go:170` is byte-for-byte and the alternation is exactly the 12 `time.Format` abbreviations. The masking defect is real but different (M8).
13. **"The WS flat-frame fix breaks the daemon's own shape"** — refuted. Both shapes parse, envelope wins on conflict, and the two cannot collide. Every producer is accounted for. The TUI never uses WS subscribe at all (`internal/tui/events.go:118` uses the `bus.subscribe` **RPC**), so `4f92b6ca` cannot regress the TUI.
14. **"A second dedupe gate was added"** — refuted; there is no dedupe in `internal/comm` at all (`grep -i 'dedup\|seen\[\|lastSentID'` → zero hits). The fix removed the double path at the source.
15. **"GUI subscribe is dropped on reconnect"** — refuted; `_flushPendingSubscriptions` (`websocket_service.dart:315-333`) replays chat, progress, jobs, metrics and plans for every tracked id. Backoff is capped at 30 s with jitter, `_retryCount` resets on success, the queue is hard-capped at 256 with oldest-dropped, and `_channel` is assigned before the flush.
16. **"A web-build break from `dart:io` in shared code"** — refuted. The two shared imports (`websocket_service.dart:6`, `sdk_client.dart:4`) are **pre-existing** at `f5d369bf`, and the range's diff to `ui/flutter_ui/lib` adds zero `dart:io`/`Platform.`/`File(` lines; all new `dart:io` is confined to `test/`. Empirically `dart compile js` accepts the import and fails only at *runtime*, so this is a runtime hazard, not the compile break the rule implies.
17. **"Anthropic/Codex empty-completion is in scope of the range"** — refuted as new; `internal/llm`'s entire in-range diff is one test comment line. The gap is pre-existing (L4).
18. **"L16a was a pre-existing DuckDuckGo coverage gap"** — refuted; coverage predates the audit window (added 2026-09-15/16; this range opened 2026-09-30). The prior disposition was wrong about that chain.
19. **"M10 (`language_en` default) is unresolved"** — refuted; `d5d68ca1` resolves it cleanly with non-vacuous pins. But see M5 (unvalidated code) and M7 (unlocked read) — the resolution introduced its own defects.

---

## DISCLOSURE (what was NOT verified)

1. **TUI golden tests were never executed.** `assertGolden` writes `.got.txt` into the repo on every mismatch (`golden_test.go:173`), which the read-only mandate forbids. All TUI conclusions are from source plus the committed fixtures plus the 10 pre-existing `.got.txt` artifacts. Reproduce: `go test -p 2 -run TestGolden -count=1 ./internal/tui/` then diff.
2. **M1 (unreachable alias branch) was not live-verified** — a scratch `resolvecheck` test failed to build in a module copy. It rests on `providers.go:549-553` plus the absence of any `model:` key in `config/agents/**`.
3. **H5's reclaim race was not reproduced** (no multi-node cluster). Established from the code path; `ReclaimIfStale` is test-only today.
4. **H3's production claim rests on payload shape** (`chat_message` carries no `id`) plus the relay's unconditional injection. No live turn was executed.
5. **The full stub-vs-real divergence list for the Flutter e2e stub is partial.** Four divergences were re-verified (no session filter, manifest unreachability, missing `gui-stream-02`, unreachable assertions); the delegated list also flagged SSE buffered-vs-live, `turn_id` vs `session_id` SSE filtering, 500-vs-400 and 404-vs-405 — **these were not re-verified**.
6. **`internal/daemon/filter_wiring_test.go` has no `t.Setenv(config.EnvMeeptHome, ...)`**, so it loads the developer's real `$MEEPT_HOME/validator/lang` into package-global state and never resets it — machine-dependent cross-test pollution.
7. **One sibling session committed to this repo mid-wave** (`docs/plans/sealed-plan.md`). Every finding was re-checked at final HEAD `ee0de064`, but the tree moved during the run.
8. **One auditor moved `./custom-gcl` to `/tmp` and back** to test the fresh-clone lint path. Byte-identical, restored, verified. No other write touched the repo.

## Observations (surfaced, not acted on)

- `scripts/gen-connectivity-graph.py:286` — `re_call` matches publish-helper call sites by bare method name with no receiver-type check. This is the root cause of H1; any method named `emit`/`send`/`notify` anywhere in the module becomes a candidate publisher. The clean fix is a receiver/type allowlist, not a longer deny list.
- `internal/validator/filter_builtin.go` + `internal/config/schema.go:689-691` — the comment says "Enabled defaults to FALSE: the filter stage is opt-in", contradicted by `schema.go:2856-2863` (`Enabled: true`) and `docs/workflows/output-filters.md:77`. Pre-existing since `25d1db94`.
- `internal/queue/store.go:496-501` — `Store.UpdateState` has no state guard, mirroring L8.
- `internal/cluster/gossip.go:159` — the never-written semaphore (L7) is a one-line fix but sits outside this range's diff.
- `.golangci.yml:5-12` and `Makefile:797-804` both carry comments that this wave's commits invalidated (H2, M4). Stale agent guidance causes more bugs than no guidance.
- `internal/tui/testdata/golden/*.got.txt` — 10 debug artifacts on disk, not gitignored. They are evidence of live flakiness no assertion surfaces.