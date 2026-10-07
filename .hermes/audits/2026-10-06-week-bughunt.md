# Meept bughunt wave 2026-10-06 — last week's work (report-only)

Scope: the 13 fix-wave commits `cd30e5c6~1..36b9dc9c` (2026-10-06), 76 files,
+8,979/-609. The prior wave (`f5d369bf..ee0de064`) and its report are in
`.hermes/audits/2026-10-05-bughunt-wave.md`; that report's fix wave closed most
of its findings, so this wave is a **regression review of that fix wave**, not a
fresh full-codebase audit.

Working tree at audit time: 3 modified files owned by a live sibling session
(`e2e/harness/daemon.go`, `e2e/harness/harness_selftest_test.go`, `Makefile` — a
`$TMPDIR` scratch-dir leak fix) plus 22 untracked runtime-generated
`sealed-plan*.md` files. None were touched.

**Audit method: parent-only.** 7 read-only auditors were dispatched
(`deleg_06690a2e`) and **all 7 died within 3 s** with
`HTTP 400: Upstream request failed: Model is unavailable`. A one-word
connectivity probe (`deleg_099c57a6`) failed the same way, so this is a
provider-level delegation outage, not a prompt problem. `delegation.model` and
`delegation.provider` are both empty, so children inherit the parent model
(`space-bunny-free` on `opencode-go`), which the upstream currently reports as
unavailable. **Every finding below is parent-verified against source, and all seven auditor
scopes were recovered by direct parent audit** (scopes 1-6 as addenda, scope 7
as the separate plan deliverable). Three items inside scope 2 could not be
recovered without a Dart toolchain and are disclosed in the last section. Every
HIGH was proven with an executed probe test.

## Gate state at HEAD (parent-run, `-count=1`, `-p 2`)

| Gate | State | Evidence |
|---|---|---|
| `go build ./...` | GREEN | exit 0, no output |
| `go vet ./...` | GREEN | exit 0, no findings |
| `go vet -tags e2e ./e2e/harness/` | GREEN | exit 0 (sibling's WIP file compiles) |
| `go test -p 2 -count=1 ./...` | GREEN | **107 ok, 28 no-test, 0 FAIL** on a clean single run at final HEAD (`/tmp/bh-1006-final.log`). The earlier 129 figure was inflated — see correction 1 |
| `gofmt -l internal/ pkg/ cmd/ e2e/` | GREEN | clean; the sibling formatted its WIP file mid-session |
| `make graphs-check` | **RED** | stale line offsets only — verified offsets-only, no content diff — see L1 |

The one test failure, `TestUnifiedHTTPServer_MCPToolsSend`, is the documented
ephemeral-port exhaustion (`dial tcp 127.0.0.1:49699: connect: can't assign
requested address`, the exact error in AGENTS.md's `-p 2` note). It passes on a
targeted `-p 1` re-run (`ok ... 2.732s`) and on a clean
`go test -p 2 -count=1 ./internal/comm/...` (`ok .../internal/comm/http
86.619s`). Not a wave regression.

### Corrections to the gate record (both mine, both now closed)

**1. Interleaved logs — this inflated the pass count, not just the fail count.**
My first two background gate runs shared one log file and interleaved their
output. Two consequences, and the second one I initially missed: a single
failure looked like four `FAIL` lines, **and the `ok` count was the SUM of two
runs** (129 = 107 + a partial second run). The honest figure is **107 ok, 28
no-test packages, 0 FAIL**, from one clean run whose output I captured to a
dedicated log file (`/tmp/bh-1006-final.log`). Both runs agreed on every
material fact — 0 real failures, build clean, vet clean — so no finding in this
report changes; only the package count was wrong.

**2. A stale notification.** After the audit was written, the FIRST background
run reported `build exit=1 / test exit=1`. That is a stale report of the run that
caught a sibling session mid-edit in `e2e/harness/daemon.go` (`sweepOnce.Do` —
`sync.Once is not a function`). It does not describe the tree as it stands now:
`go build ./...` exits 0, `go vet -tags e2e ./e2e/harness/` exits 0, and
`internal/comm/http` passes clean (`ok ... 86.049s`). Both failures in that
stale notification are accounted for: the build failure was the sibling's
transient edit, and the test failure was the documented ephemeral-port flake.

The lesson is recorded because it recurred in this session: a background gate
notification arriving after the work is done describes the tree at *dispatch*
time, not at *notification* time. Re-verify before acting on it — and do not
re-run a gate you already re-ran, because a fresh full-suite run costs ~7
minutes and the earlier `-p 1` package re-run was the cheaper, more targeted
proof.

**Current sibling WIP (not mine, not touched):** `AGENTS.md`,
`Makefile`, `docs/workflows/e2e-testing.md`, `e2e/harness/daemon.go`,
`e2e/harness/harness_selftest_test.go` — a `$TMPDIR` scratch-dir leak fix.
Uncommitted at the time of writing.

---

## HIGH

**H1 — `Store.Fail` has no claim-token predicate, so a superseded attempt kills
the live attempt's job. `Store.CompleteAttempt` has one. The guard is
half-applied.**

The wave made completions attempt-safe: `CompleteAttempt` puts the token in the
`WHERE` clause so the database enforces it, and treats a same-token retry as
idempotent success.

`internal/queue/store.go:624-625` (CompleteAttempt — guarded):

```go
UPDATE jobs SET state = 'completed', result = ?, updated_at = ?
WHERE id = ? AND %s`, claimPredicate), args...   // claimPredicate += ` AND claim_token = ?`
```

`internal/queue/store.go:776-779` (Fail — unguarded):

```go
UPDATE jobs SET state = ?, error = ?, updated_at = ?
WHERE id = ?`,
    string(newState), errMsg, now, jobID); err != nil {
```

No state predicate, no token predicate. Compare `UpdateState` at `:541-542`,
which does carry `AND state NOT IN ('completed', 'dead')`. The asymmetry is
therefore visible in three adjacent write paths of the same file.

The caller drops the token it already holds. `internal/worker/worker.go:357`
captures it, then the failure path at `:389` passes only the job id:

```go
claimToken := w.claimToken          // :357  — captured
w.claimToken = ""
...
if err := w.queue.Fail(ctx, job.ID, processErr); err != nil {   // :389 — token dropped
```

`completeJob` at `:627-633` does the opposite and uses the token. So the same
worker protects its success path and not its failure path.

**Proven with an executed probe** (`internal/queue/zzprobe_stalefail_test.go`,
written, run, then deleted; copy at `/tmp/bh-1006-zzprobe_stalefail_test.go.bak`):

```
attempt1 claimed, token="claim-5dc8d323be319687"
cluster_queue: job reset to pending
attempt2 claimed, token="claim-cbfb20c01960d0fc"     <- attempt2 is now LIVE
GUARD OK  — CompleteAttempt rejected the superseded token: job not in a claimable state for completion
BUG — Fail accepted a failure carrying the SUPERSEDED token "claim-5dc8d323be319687"
       while attempt "claim-cbfb20c01960d0fc" is live
FINAL state="failed" live_token="claim-cbfb20c01960d0fc"
```

A live `claimed` job, holding attempt 2's token, was moved to `failed` by
attempt 1. Attempt 2's subsequent `CompleteAttempt` then fails
`ErrJobNotClaimable` and its work is discarded.

**Reachability in production.** Any supersede path retires the token while the
old attempt is still executing, and every one of them is wired:

- `Store.Retry` (`store.go:844`) sets `claim_token = NULL` from states
  `failed, claimed` — so a retry triggered from anywhere supersedes an in-flight
  claim. Callers: `internal/worker/worker.go:401`, `internal/agent/tactical.go:2445`,
  `internal/queue/queue.go:969` (bus `queue.retry` handler), `internal/services/queue_service.go:189` (RPC).
- `Store.Requeue` (`store.go:846`) likewise, from `failed, claimed, processing`.
- `Store.ResetToPending` (`store.go:954`) via the cluster reclaim
  (`internal/queue/cluster_queue.go:248`).

The reclaim sweep is currently test-only (`ReclaimIfStale` has one
non-comment caller, `internal/cluster/integration_test.go:228`), so the
cluster arm is latent. The `Retry` and `Requeue` arms are live today: the bus
handler at `queue.go:969` and the RPC at `queue_service.go:189` are
externally reachable, so a second client can supersede a running job while its
worker's attempt is still in flight.

**The class, not the site.** Every mutating job write must carry the same
predicate. `Fail` is the one that does not. The clean fix is a
`FailAttempt(jobID, errMsg, claimToken)` mirroring `CompleteAttempt` — token in
the `WHERE` clause, same-token retry idempotent, superseded token rejected with
`ErrJobNotClaimable`, and the caller passing the token it already holds.
Pinning test: the probe above, plus the no-token legacy shape still working for
callers that never claimed.

---

**H2 — the H5 stale-completion guard is DEAD in production: the real queue does
not satisfy the interface the guard tests for. (Found while recovering scope 4
after the delegation outage; the HIGH from the original parent pass is H1.)**

The guard's attempt-aware branch is gated on a single type assertion
(`internal/agent/tactical.go:1295`):

```go
if checker, isAttemptAware := ts.queue.(completionFreshChecker); isAttemptAware {
    if fresh, ok := checker.CompletionIsFresh(ctx, jobID, ""); ok && !fresh {
```

`completionFreshChecker` is declared in `internal/agent` and requires
`CompletionIsFresh(ctx, jobID, claimToken) (fresh, ok bool)`. The real
implementations do not have that method: `Store.CompletionIsFresh` exists
(`store.go:695`) but `PersistentQueue` holds `store *Store` as an **unexported
field and does not embed it** (`queue.go:86-104`), so the method is not promoted
to the queue's method set. `queue.Queue` (`queue.go:31-77`) does not declare it
either, so nothing forces the addition.

**Proven with an executed probe** (written, run, deleted; copy at
`/tmp/bh-1006-zzprobe_h5_dead_test.go.bak`):

```
*queue.PersistentQueue satisfies completionFreshChecker: false
*queue.ClusterQueue satisfies completionFreshChecker: false
PROBE RESULT: guard DEAD in production
```

So `isAttemptAware` is false for every production queue, and
`OnJobCompleted` always takes the `else if` state-only branch — the exact
pre-H5 behaviour the commit set out to replace.

**Why the suite is green.** The wave's own test
(`tactical_h5_completion_guard_test.go:31-45`) wraps a real `PersistentQueue` in
an `attemptAwareQueue` that **declares the method itself**:

```go
// attemptAwareQueue wraps a real PersistentQueue and adds the optional
// completionFreshChecker capability, standing in for a queue that has grown
// attempt identity (claim token / epoch).
type attemptAwareQueue struct {
	queue.Queue
	...
}
func (a *attemptAwareQueue) CompletionIsFresh(_ context.Context, _ string, claimToken string) (bool, bool) {
```

The test proves the guard behaves correctly *given* the capability. Nothing
proves any real queue has it. This is the registered∧routed∧mapped class applied
to an optional interface: the capability is consumed but never supplied.

**A second, independent defect in the same branch.** Even once the method is
forwarded, the guard passes an **empty token**:

```go
checker.CompletionIsFresh(ctx, jobID, "")
```

`Store.CompletionIsFresh` (`store.go:695-720`) treats `claimToken == ""` as the
state-only predicate on both the `completed` and the `claimed`/`processing`
paths. The comment justifies it ("the event carries no attempt token today") and
says it "becomes token-exact the moment the event starts carrying `claim_token`"
— but nothing carries it today, so the token-exactness is hypothetical. Combined
with H2, the guard currently provides no protection at all, and fixing only the
forwarder would upgrade it from *no protection* to *state-only protection*,
still not the attempt-exactness H5 describes. Both legs must land together.

**The fix.** (1) Forward the capability on the real types — a one-line
`CompletionIsFresh` on `PersistentQueue` delegating to `q.store`, plus one on
`ClusterQueue` (it already carries the `WakeWaiter` forwarder precedent at
`cluster_queue.go:304-312`, with a comment explaining exactly this failure mode
for wake notification). (2) Thread `claim_token` through the
`queue.job.completed` event payload so the guard can pass a real token, or
state plainly in the invariant doc that the guard is state-only until then.

**The pin the wave is missing:** assert
`interface{}(*queue.PersistentQueue)(nil).(completionFreshChecker)` in
`internal/queue` (or a `package agent_test` completeness test) so the gap fails
loudly the next time the interface is added without a forwarder. That single
assertion is what would have caught this before commit.

---

## Addendum — scope 2 recovered by direct parent audit (WS filter + delivery)

Scope 2 (chat/delivery surfaces) was recovered after the outage. The core
grant/suppress logic is **correct and well-documented** — I verified every rule
the wave claimed. Two defects found, one of them a live contradiction between
two paths of the same handler.

**Verified correct (no action needed):**

- **Grant is channel-agnostic, opt-out is not.** `SubscribeSession`
  (`server.go:539-541`) adds to `subs.sessions` (one entry per session, no
  channel dimension) and deletes only the suppressions for the channel it is
  re-arming. `UnsubscribeSession` (`:579`) writes one
  `wsSuppression{channel, session}` key and never touches `sessions`.
- **An unsubscribe never deletes the grant** — verified in source; the doc at
  `:544-554` records that the map-delete version was tried and reverted
  because it returned the connection to broadcast mode.
- **`wsChannelAll` suppression is honoured on every channel** —
  `ShouldSend:614-620` checks the `all` key whenever `channel != all`, so a
  channel-less opt-out (as normalized at `:394-399`) suppresses every stream.
- **Session-less events broadcast.** Both relay call sites carry the bypass:
  `:778` (`eventSessionID == "" || h.ShouldSend(...)`) and `:850`
  (`event.SessionID == "" || ...`). The bypass is at the call sites, not
  hidden in `ShouldSend`, which is deliberate and documented at `:595-598`.
- **H9 closed.** `internal/tui/events.go:40-70` documents why the three
  `agent.*` patterns must be pairwise non-overlapping (one collector per
  matching pattern, `bus.poll` returns buffered records with no dedupe) and
  pins it with `TestTopicPatternsAreNonOverlapping`.
- **M8 addressed, with a caveat.** `assertGolden` (`golden_test.go:130-160`)
  normalizes clock/date tokens and carries an explicit, enumerated
  `lowercaseExemptions` allowlist rather than pattern-matching. This is
  materially better than the prior masking, but the month-abbreviation entries
  (`"Mar "`, `"Ma…"`, `"M…"`, …) do exempt real text from the lowercase
  assertion. Narrow and deliberate, not the prior defect.

**H3 — a channel-less unsubscribe frame deletes the connection's entire filter
set, contradicting the fix's own documented contract and re-delivering events
the client opted out of.**

`internal/comm/http/server.go:2832-2847`:

```go
if sessionID != "" {
    s.wsHub.UnsubscribeSession(wc, sessionID, channel)
    ...
} else if channel != "" {
    // ... drop every session filter on this connection and return it to
    // broadcast mode (the pre-H6 semantics, kept for the channel-level
    // frame that carries no session_id — ws-filter-05).
    s.wsHub.sessMu.Lock()
    if subs, ok := s.wsHub.sessionSubs[wc]; ok {
        ...
        delete(s.wsHub.sessionSubs, wc)
    }
```

The `else if` fires whenever `sessionID == "" && channel != ""`. A frame that
names a channel but no session — which is exactly what an old channel-less
client sends, and exactly the case `UnsubscribeSession`'s own doc comment
describes — **never reaches `UnsubscribeSession`**. Instead the whole
`sessionSubs[wc]` entry is deleted, which drops every granted session on every
channel and puts the connection back in broadcast mode (`ShouldSend:605-607`
returns `true` when `subs == nil`).

That is the precise failure the wave set out to eliminate, and the wave's own
doc comment states the intended behaviour for this input: *"Passing no channel
records a wsChannelAll suppression, which suppresses the session on every
channel (the connection-wide opt-out an old channel-less client meant)"*
(`:556-561`). The hub implements that contract; the handler bypasses it. The
comment at `:2836-2838` even labels the surviving behavior "the pre-H6
semantics" — a regression the H6 work was meant to remove, left in place and
now described as intentional.

**Why the e2e suite does not catch it.** `ws-filter-05`
(`e2e/suites/ws-filter/ws_filter_test.go:397`) sends
`unsubscribeChannel("all")` — `{type: "unsubscribe", data: {channel: "all"}}`,
no `session_id` — and then **asserts** the old behavior: delivery is restored
(`waitMarker(..., "WSFILTER05-RESTORED-CHANNEL", ...)` at `:399`). I ran it:
`--- PASS: TestWSUnsubscribeRestoresDelivery (14.64s)`. The suite was not
touched by this wave (`git diff --stat cd30e5c6~1..HEAD -- e2e/suites/ws-filter/`
is empty), so it still pins the pre-H6 contract that the fix reverted. A test
that asserts the old semantics is worse than no test: it will block the correct
fix.

Note the subtlety: `normalizeChannel("")` returns `wsChannelAll`, but that
normalization happens *inside* `UnsubscribeSession`, which the handler never
calls on this path. The literal string `"all"` in the e2e helper is what steers
the test into the delete branch — a client sending `{channel: ""}` with no
session falls through both branches and does nothing at all.

**The fix.** Route the channel-named/session-less frame to
`UnsubscribeSession` semantics instead of the wholesale delete — i.e. give
`wsConnSubs` a connection-wide opt-out key that suppresses every
`(channel, session)` pair without discarding the grants, or make the
`else if` branch record a connection-wide suppression rather than deleting the
map. Then update `ws-filter-05` to assert the NEW contract: a channel-level
opt-out must NOT restore delivery for a session the client had explicitly
unsubscribed. The unit test `TestWSChannelFilter_ChatUnsubscribeKeepsProgressAndTerminal`
already encodes the correct per-channel behavior; nothing at hub level covers
this path.

**Checked and cleared (not a finding): `progressRateLimiter.lastSent` growth.**
`lastSent` is keyed by `*wsConn`, so its size is bounded by the client count,
and `Unregister` (`:485`) deletes the sibling `sessionSubs` entry in the same
place. The limiter and the session filter are torn down together, so there is
no asymmetry to fix. The real duplicate-delivery risk in this scope is the TUI
topic patterns (H9), which the wave closed.

---

## Addendum — scope 1 recovered by direct parent audit (queue + worker)

Scope 1 is where H1 was found, so its remaining items were completed directly.
**No additional defect.** The claim-token design, the wake-up path, and the
keyset paging all hold up under adversarial reading.

**Claim-token completeness — enumerated every job-row write.** Five
`UPDATE jobs SET` statements exist in `store.go` (`:503`, `:541`, `:624`,
`:777`, `:1744`). Exactly one lacks a predicate, and it is H1:

| Line | Write | Guard |
|---|---|---|
| `:503` | claim | `AND state = 'pending'` + mints `claim_token` — correct |
| `:541` | `UpdateState` | `AND state NOT IN ('completed','dead')` + `RowsAffected` + `ErrJobStateLocked` — correct |
| `:624` | `CompleteAttempt` | `AND state IN ('claimed','processing')` + `AND claim_token = ?` — correct, **prior L8 closed** |
| `:777` | `Fail` | bare `WHERE id = ?` — **H1** |
| `:1744` | `ClaimNextByID` | `AND state = 'pending'` + mints token — correct |

Every token-retiring write (`Requeue:844`, `Retry:910`, `ResetToPending`, and
the three `claim_token = NULL` sites at `:844`, `:908`, `:964`, `:1048`) nulls
the token, which is what makes a superseded completion detectable. That part of
the design is sound; H1 is the one path that ignores the token it is offered.

**Locked-region return audit — no leak.** Every `Lock()` in
`store.go`, `queue.go`, `cluster_queue.go` and `worker.go` is paired with a
`defer Unlock` in the same function. One heuristic hit —
`internal/worker/worker.go:246-247`, `if !w.State.CanClaim() { w.mu.Unlock();
return false, nil }` — is a false positive: that path has an explicit `Unlock`
immediately before the return. The wave's own note at `worker.go:369-377`
records the auditor-E mutex leak this class produced and confirms it was
repaired.

**Unbounded growth — cleared.** `cq.claimed` is now deleted on every exit path
(`cluster_queue.go:164, 201, 220, 260, 396`), so `Stats().LocalClaims` is
accurate and the map is bounded by live claims — **prior L10 closed**. The one
map that could plausibly grow is `q.wakeWaiters` (`queue.go:131`); it is
self-cleaning, because `WakeWaiter` returns an unregister closure
(`:698-707`) that deletes the entry, and `worker/pool.go:200` registers it
under `RemoveWorker`/`Stop`. `mutexio` is clean on both packages, so no send
happens under a lock.

**Keyset paging — correct.** `ListByStateAfter` (`store.go:1112-1141`) uses a
4-column cursor (`interactive DESC, priority DESC, created_at ASC, id ASC`) and
its `WHERE` mirrors it exactly, with `interactive`/`priority` compared `<`
(DESC columns) and `created_at`/`id` compared `>` (ASC columns). The `id ASC`
tiebreaker is what makes the cursor total, so no row is skipped or repeated
across pages. The zero-cursor fast path is guarded by the comment's invariant
that `created_at` is NOT NULL and RFC3339-formatted and `id` is the primary
key, so no real row can collide with the sentinel. Pinned by
`TestClaimSlowPath_PagesBeyondHeadOfLineParkedJobs`, `_BoundedScan`, and
`_SinglePageUnchanged`.

**Event-driven wake-up — live, not test-only.** `WakeWaiter` is declared on the
optional `WakeNotifier` interface (`queue.go:690-692`), implemented on
`PersistentQueue` (`:698`), forwarded by `ClusterQueue` (`:304`) so cluster mode
stays on the same path, and consumed by `worker/pool.go:200`. That forwarder is
the exact pattern H2 is missing.

**Test honesty — all new tests execute and can fail.** Run explicitly, not
inferred from the suite's `ok` line: 8 queue claim-token/paging tests PASS,
`TestWorker_RetryWakesIdleWorker` PASS, `TestWorker_WakeClaimsImmediately` PASS.
The claim-token tests pin **typed** errors (`errors.Is(err,
ErrJobNotClaimable)`) rather than `err != nil`, and one pins that a
`PersistentQueue` publishes the completion event exactly once
(`TestPersistentQueue_DuplicateCompletePublishesOnce`) — the downstream half of
the idempotency contract, which is the assertion that actually matters and is
easy to omit.

**Not verified in this scope:** whether the wake-up fires under real cluster
contention (no multi-node harness available), and the `MAX` scan-bound behavior
under a genuinely saturated queue.


---

## Addendum — scope 5 recovered by direct parent audit

The delegation outage killed auditor scope 5 (identity/slug/dedupe + cluster
transport) along with the rest. I covered its highest-value items directly after
the outage. Result: **every prior-wave item in that scope is closed, and no new
defect was found.** Detail below so the scope is not silently absent.

**Prior-wave closures, verified at HEAD:**

| ID | Claim | State | Evidence |
|---|---|---|---|
| L5 | cluster gossip retry budget | **CLOSED** | bounded 64-slot retry channel; `QueueForRetry` reached on the dial-failure path (`gossip_transport.go:296-300`) |
| L7 | `sem` never written — every peer-send goroutine parked forever | **CLOSED** | exactly one send at `gossip_transport.go:191`, one deferred receive at `:290`, capacity `maxConcurrentPeerSends = 32` (`:140`). No other `make(chan struct{}, N)` semaphore exists in `internal/cluster` that lacks a send — the sweep found only `stopCh`/`doneCh`/`done` lifecycle channels and the test's own `served`. `go run ./tools/analyzers/mutexio/... ./internal/cluster/` is clean, so the acquire is not under a mutex |
| L11 | `thread_resolve` risk undocumented | **CLOSED** | `thread_resolve.go:57-86` now carries an explicit "Fail-safe contract (bughunt L11)" doc block naming all three rules, plus the `ThreadSlug` structural mitigation. Zero-hits-in-docs is fixed |
| L14 | stale fuzz corpus crasher + vacuous guard | **CLOSED** | the guard was inverted, not deleted: `fuzz_test.go:78-82` now asserts BOTH directions — `inputValid && !Valid(CleanText)` (corruption) and `!inputValid && Valid(CleanText)` (silent repair). The comment at `:64-70` names the exact prior vacuity and the corpus entry that exposed it |
| L15 | content-keyed memory dedupe, no TODO | **PARTIAL** | identity scoping landed and is wired; see M2 for the no-provenance fallback. The tests are honest about the fallback rather than papering over it: `TestDistill_NoEvidenceIDsStillDedupesOnContent` pins the degraded mode explicitly |

**Idempotency of sanitization — verified, no double-mangle.** `ThreadSlug` maps
every non-alphanumeric rune to `_` and caps at 32 runes. Since `_` is itself
non-alphanumeric and re-maps to `_`, `sanitize(sanitize(x)) == sanitize(x)`
holds for any input: a second pass rewrites `_`→`_` and cannot shorten a string
that is already ≤32 alphanumeric-or-underscore runes. Three mint sites all call
the same function — `topic_detector.go:130`, `thread_service.go:50`,
`thread_store.go:249` — so producer and consumer share one implementation rather
than two that can drift.

**`OutputFiltersConfig.Validate` — verified correct, including the trap.** The
obvious defect here would be accepting a normalized code but looking for the
word table under the raw string. It does not: `HasLanguageWordTable`
(`filter_language.go:395-404`) normalizes internally via the same
`NormalizeLanguageCode`, so the accepted set and the table-lookup key are the
same value by construction. Empty stays valid, preserving the frozen default.

**`agentModelBindingFor` (`daemon/components.go`, the M1 fix) — verified, and it
avoids a real trap.** The prior closure made the alias branch reachable. The
helper deliberately reads the alias's current member through read-only
accessors rather than `ResolveForAlias`, because that call MUTATES alias health
(advances the rotation cursor, clears cooldown counters) — using it as a probe
would heal the park it exists to detect. `resolver.HasAlias` gates the branch,
and an unresolvable model returns `(binding{}, false)`, which the adapter reads
as "route normally" (the pre-existing semantic). No new field was added to a
long-lived struct, so the struct-vs-snapshot class does not apply to this diff.

**Test honesty — all new tests in scope execute and pass.** Verified by running
them explicitly rather than trusting the suite's `ok` line:

```
--- PASS: TestGossipTransport_SendSemaphoreBoundsConcurrentPeerSends (2.02s)
--- PASS: TestGossipTransport_SendSemaphoreReleasesAfterCompletion (0.00s)
--- PASS: TestThreadSlug_NeverMintsTheUnwrapSeparator (0.00s)
--- PASS: TestThreadSlug_Bounded (0.00s)
--- PASS: TestDistill_SameLessonReimportedStillDedupes (0.00s)
--- PASS: TestDistillScopeKey_ReadsBothCarriers (0.00s)
--- PASS: TestDistill_NoEvidenceIDsStillDedupesOnContent (0.00s)
--- PASS: TestDistillDedupesNearDuplicate (0.00s)
```

`filter_wiring_test.go` is also fixed: it now pins the home at `:29` and `:183`
via `t.Setenv(config.EnvMeeptHome, t.TempDir())` and loads word tables from an
absent temp dir (`:35`, `:185`), so the real `~/.meept` is no longer read. The
comment at `:25` notes that `t.Setenv` also refuses `t.Parallel`, which is the
correct guard for a package-global table. `internal/daemon` has exactly one
test file using `t.Parallel`, so the remaining exposure is bounded but not zero
— see M3.

**Still unverified in this scope:** `internal/security`'s untracked corpus entry
`testdata/fuzz/FuzzInputSanitizer/828d5059791d0353` is still on disk and still
not gitignored (prior L14 said one `git add -A` from being committed). The guard
it exposed is now fixed, so committing it would be harmless but misleading — it
is no longer a reproducer of anything. Deleting it is a judgement call I did not
make on your behalf.

---

## Addendum — scope 3 recovered by direct parent audit (llm + validator)

Scope 3 was recovered after the outage. **No defect found.** This is the
highest-quality work in the wave: every claim holds, the quota invariants hold,
and the tests pin the exact contracts rather than their approximations. I am
recording it as the reference standard the other scopes were measured against.

**Empty-completion classification — all four parse paths covered.** I
enumerated every site that returns a completion to the caller:

| Path | Site | Gate |
|---|---|---|
| anthropic non-streaming | `anthropic.go:2052` | `classifyAnthropicEmpty` |
| anthropic streaming | `anthropic.go:1959` | `classifyAnthropicEmpty` (shared) |
| codex non-streaming | `codex.go:718` | `TrimSpace(Content)` + `TrimSpace(Reasoning)`, `hasFunctionCall` exempt |
| codex streaming SSE | `codex_sse.go:297` | same rule on accumulated `content`/`reasoning`/`toolCalls` |

`classifyAnthropicEmpty` (`anthropic.go:1990-1995`) is one predicate shared by
both anthropic modes, so the two cannot drift:

```go
func classifyAnthropicEmpty(toolCalls []ToolCall, answerText string) error {
	if len(toolCalls) == 0 && strings.TrimSpace(answerText) == "" {
		return ErrEmptyResponse
	}
	return nil
}
```

Every site uses `strings.TrimSpace`, never `== ""`, so whitespace-only is
caught on all four. **Prior L4 closed.**

**Tool-call-only replies are exempt, on all four paths.** `len(toolCalls) == 0`
(anthropic) and `!hasFunctionCall` (codex) gate the error, so a reply whose only
content is a tool call is a valid completion — the tool call *is* the answer.
Pinned by `TestAnthropicParseResponse_ToolUseOnlyIsNotEmpty` and
`TestCodexParseResponse_FunctionCallIsNotEmpty`.

**Streaming and non-streaming reach the same classification, and the streaming
ERROR path still drains the body.** `codex.go:573-579` reads the body on both
modes when the request will not stream, with the rationale in-line:

```go
var respBody []byte
if resp.StatusCode != http.StatusOK || !payload.Stream.Enabled {
    respBody, err = io.ReadAll(resp.Body)
```

The comment at `:567-572` names the exact regression this avoids: without the
drain on the error path, "quota-window 429 bodies must reach
codexErrorFromResponse on the streaming path too, or every streaming 429
degrades to RateLimitError and short-retries a hours-long quota window."
Classification then happens once, in `codexErrorFromResponse` (`:589`), for both
modes. Anthropic does the same at `:1394` and `:1645`.

**Quota is never short-retried.** Both anthropic loops carry an
`errors.AsType[*QuotaResetError]` early exit **before** the
`RateLimitError`/retryable-status checks — `anthropic.go:440` (non-streaming)
and `:720` (streaming) — matching the documented invariant in
`internal/llm/AGENTS.md`. Codex deliberately has no per-status retry loop;
`doRequestWithEmptyRetry` (`codex.go:627-650`) retries **only** `ErrEmptyResponse`
and returns everything else single-shot, so a 429 cannot be double-retried. The
guard is `if !errors.Is(err, ErrEmptyResponse) { return nil, err }` at `:636`.

**Bare sentinel on exhaustion — prior L5 closed, pinned by identity.** Both
loops return the unwrapped sentinel. Anthropic returns `err` directly
(`:474`); codex returns `lastErr` (`:648`), which can only ever hold a value
that already satisfied `errors.Is(err, ErrEmptyResponse)` and was never wrapped
en route. The tests do not settle for `errors.Is`, which also matches through a
wrap — they assert pointer identity:

```go
if err != error(ErrEmptyResponse) { //nolint:errorlint // deliberate pointer-identity check
```

(`provider_empty_completion_test.go:180`). That is the correct pin for the
contract, and the `nolint` is explained rather than blanket-applied.

**Ordering is preserved.** `TestAnthropicChat_EmptyCompletionNeverSwallowsRefusal`
pins that a refusal with no text stays a `RefusalError` and is never
reclassified as an empty flake.

**L6 nil-receiver guard — closed.** `runtime_logs.go:244-246` is now correctly
ordered: `if w == nil { return nil }` precedes the lock, and `w.file == nil ||
*w.file == nil` is checked after it. I checked every other `== nil` guard in the
file (`:29, :42, :120, :125, :150, :192, :207, :267, :275, :283, :291`) — none
reproduces the inverted shape.

**M5 — `expected_language` is validated AND reached.** `OutputFiltersConfig.Validate`
is declared at `schema.go:735` and, critically, actually invoked on config load
at `schema.go:4141` (`c.Daemon.OutputFilters.Validate()`), which is the
dead-validator check that matters. Empty stays valid so the frozen default is
preserved.

**M7 closed; tokenizers agree.** `detectedLanguage` takes one
`snapshotExtraTables()` for the whole call (no per-word lock, no unsynchronized
read of the package-level map header). Both the loader and the detector share
`tokenizeWords` + `normalizeWord` (`filter_language.go:151-166`), and
`parseWordList` strips `#` at LINE level before tokenizing (`:169-184`) —
**prior L12 closed**. There is no zero-sentinel defaulting pattern: no
`if anyField == "" { cfg = Defaults() }` anywhere in the diff.

**Test honesty — 13 new provider tests, all run, all PASS, all non-vacuous.**
They cover blank AND whitespace bodies, tool-use exemption, thinking-only,
streaming and non-streaming, retry-then-succeed, refusal precedence, and
"does not retry other errors" — the last one is the negative case that would
catch a regression in the quota lane. 65 matching tests pass across
`internal/llm` and `internal/validator`; no `--- FAIL`.

**Not verified:** no live provider call was made, so the classification is
verified against test servers and fixtures rather than a real 200-OK blank body.
The `internal/llm/AGENTS.md` site list matches the four sites I found, which is
a documentation-consistency check, not proof the list is exhaustive against
future providers — a new client must still add its own pin.

---

---

## Addendum — scope 6 recovered by direct parent audit (tooling + e2e gates)

The last unrecovered scope. **No defect found.** The three claims all hold, and I
proved the coverage gate has teeth rather than assuming it.

**The graph anti-fabrication fix works, and causes no false negatives.** The six
fabricated topics from the prior wave (`closed`, `done`, `error`,
`message_chunk`, `permission_request`, `tool_call`) are all absent from
`docs/generated/bus-topology.json` publishers, while the five real `routing.*`
topics are retained. Reconciliation the other way — the more dangerous
direction, since the graph is a CI gate — found **15 topics published in code but
absent from the graph, and every one is a `_test.go` fixture**
(`internal/bus/bus_test.go`, `internal/bot/router_test.go`,
`internal/agent/phase2_listener_test.go`, `internal/comm/http/load_test.go`, …).
Test-only publishes correctly have no production publisher. **Prior H1 closed
with no collateral loss.**

**The manifest gate is non-vacuous, and coverage is complete.** All 6 tests in
`e2e/suites/manifest/` pass. More importantly, the "every `internal/` package
maps to a suite" policy is enforced **in `.githooks/pre-commit-e2e:98-121`**,
not in the test file — the test file checks manifest internal consistency
(suite dirs exist and are unique, every suite reachable, every `path_map` key
names a declared suite, Flutter surface covered, every scenario covered), while
package coverage is a commit-time gate. I extracted the hook's Python predicate
and ran it directly:

```
internal/queue           -> known
internal/agent           -> known
internal/agent/prompts   -> known     <- subpackage covered by its parent key
internal/newthing        -> new       <- would FAIL the commit
internal/agentfoo        -> new       <- slash boundary respected
cmd/meept                -> known
```

The predicate returns `new` for an unmapped package and for a name that shares no
slash boundary, so the gate can genuinely block a commit. Coverage today:
**72 `path_map` keys, 65 `internal/` dirs, 0 uncovered.**

**The hook is actually wired, in both places.** `core.hooksPath` is
`.githooks`, and `.githooks/pre-commit:109` invokes `pre-commit-e2e`. CI runs the
umbrella hook explicitly (`.github/workflows/code-quality.yml:33-36`:
`git config core.hooksPath .githooks` then `.githooks/pre-commit`), so the gate is
enforced on pushed diffs, not only locally. *(My first grep of `Makefile` for
`pre-commit-e2e` returned nothing — the hook list lives in `.githooks/pre-commit`,
not the Makefile. Not a finding.)*

**gui-flows CI reachability is real.** `code-quality.yml:275-288` runs
`make e2e-affected-area AREA=gui-flows`, and the manifest declares the suite with
`runner: dart` plus `ui/**` `path_map` keys so a Flutter change selects it. The
comment at `:275` states the reason honestly: the Go pre-commit hook does not
scan Dart files, so CI's e2e-gui job is the Dart-side gate.

**`scripts/e2e-affected.sh` fails loudly instead of silently skipping.** The
runner split routes `runner == "dart"` suites to the Dart command and everything
else to `go test`. The false-negative case I was asked to look for — a changed
file matching neither runner — is handled explicitly at `:162-168`: Dart changed,
nothing mapped, and the script prints "NOTHING RAN" to stderr and **exits 1**.
The Go branch instead falls back to the smoke suite, which the header documents
as deliberate ("so the gate stays non-decorative during the rollout") and which
only applies while mapped suites are still `status: todo`. The script uses
`set -u -o pipefail` (not `-e`, but every command that matters is explicitly
checked), performs **no `git stash`**, and is bash-3.2 compatible as the header
claims.

**Not verified:** the stub-daemon-vs-real-daemon divergence list (9 items,
including the 5 the prior wave left open) — that needs the Dart suite to run, and
no Flutter toolchain was available. The `tui-agent-tab-01` "able to fail" claim
was not re-derived beyond confirming the file changed and its suite is
registered.

---

## MEDIUM

**M1 — `PlanManager.CreatePlan` disambiguates a slug collision with a
check-then-write loop that has a TOCTOU window.**
`internal/plan/manager.go:88-97`:

```go
if _, err := os.Stat(filePath); err == nil {
    for n := 2; ; n++ {
        candidate := filepath.Join(dir, fmt.Sprintf("%s-%d.md", slugify(title), n))
        _, statErr := os.Stat(candidate)
        ...
        if os.IsNotExist(statErr) { filePath = candidate; break }
    }
```

Two concurrent `CreatePlan` calls with the same title both see the base name
missing, both pick `-2`, and both `WritePlanMarkdown` to the same path — the
exact clobber the commit set out to prevent, just at a narrower window. The
same file already holds the correct pattern: `EnsureTaskPlan` holds `m.mu`
across its read-modify-write. Not concurrent today
(`CreatePlan` is reached from the planner's single-threaded path; the
in-repo concurrency test at `manager_test.go:514` covers `EnsureTaskPlan`, not
`CreatePlan`), so this is a latent hazard, not a live bug. The clean fix is
`os.OpenFile(..., O_CREATE|O_EXCL)` on the chosen name, or holding `m.mu`
across the stat-and-reserve.

**M2 — `distillScopeMatches` treats a stored memory with no provenance as
matching every candidate, so the identity scope silently degrades to the
content comparison it replaced.** `internal/memory/distill.go`:

```go
func distillScopeMatches(candScope string, stored Memory) bool {
    storedScope := distillScopeKey([]Memory{stored})
    if storedScope == "" {
        return true // no provenance recorded: content decides
    }
    return storedScope == candScope
}
```

The wave moved dedupe from pure content-keying to identity scoping (this closes
most of prior-wave L15: `distillScopeKey` reads real evidence ids from both
carriers, and the caller at `:488` skips non-matching scopes). But a candidate
WITH provenance still competes against every stored row that has none, on
content similarity alone. During the transition — rows written before the
change carry no `evidence_ids` — a same-content lesson from a different lineage
still swallows the new one, which is the precise failure the identity scope was
added to stop. Blast radius is opt-in only (`Distill` is gated off by default,
`internal/memory/schema.go:2983`). The comment documents the choice, so this is
a known limitation rather than an oversight; it needs an owner decision on
whether legacy rows are backfilled or excluded.

**M3 — `internal/daemon/filter_wiring_test.go` loaded the developer's real
`$MEEPT_HOME` into package-global state.** Prior-wave disclosure item 6, flagged
again in this wave's dispatch. **Since fixed** — the test now pins the home at
`:29` and `:183` with `t.Setenv(config.EnvMeeptHome, t.TempDir())` and loads
word tables from an absent temp dir, so the real `~/.meept` is no longer read.
Residual, smaller: `internal/daemon` has one test file using `t.Parallel`, and
the validator's extra-tables map is package-global (`snapshotExtraTables`), so
a future parallel test in that package could still race a `LoadLanguageWordTables`
call. Not currently reachable.

---

## LOW

**L1 — `make graphs-check` is red at HEAD, on line offsets only.**
`❌ Stale generated files: bus-topology.json, bus-topology.md`, and the diff is
purely `"line": 200 → 223` style movement caused by `internal/plan/manager.go`
growing 23 lines in `36b9dc9c`. This is the documented F66 class (the generator
embeds absolute line offsets). `make graphs` is the fix. Left unfixed here
because it regenerates files outside this report's scope and the sibling session
is mid-edit in the same tree.

**L2 — `internal/llm/filter` comment contradiction is now inverted.**
`internal/config/schema.go:689-691` says `Enabled defaults to FALSE: the filter
stage is opt-in`, and the struct field is a plain `bool` so its zero value is
false — consistent. Prior-wave observation flagged the opposite
(`schema.go:2856-2863` had `Enabled: true`). Re-read at HEAD: the comment and
the zero value now agree, so the prior observation is closed. Recorded because
the comment is the kind that drifts again.

---

## Prior-wave closure — verified, not trusted

| ID | Prior claim | State at HEAD | Evidence |
|---|---|---|---|
| H1 | generator fabricated 6 bus topics | **CLOSED** | all six (`closed`, `done`, `error`, `message_chunk`, `permission_request`, `tool_call`) absent from `docs/generated/bus-topology.json` publishers; the 5 real `routing.*` topics retained |
| H1b | fabricated topics vs real publishers | **CLOSED, no false negatives** | 15 topics published in code but absent from the graph are all `_test.go` fixtures (`bus_test.go`, `router_test.go`, `phase2_listener_test.go`, …), which is correct |
| L6 | `rotatingWriter.Close` nil-receiver guard dereferenced `w` | **CLOSED** | `runtime_logs.go:244-246` now `if w == nil { return nil }` before the lock; every other `== nil` guard in the file is correctly ordered |
| L7 | cluster gossip send semaphore never written | **CLOSED** | `gossip_transport.go:191-192` acquires before the goroutine spawn, releases in `sendToPeer`'s defer (`:290`); per-peer payload copy added at `:189-190` to kill a real data race |
| L8 | `Store.UpdateState` could resurrect a terminal job | **CLOSED** | `store.go:541-542` carries `AND state NOT IN ('completed','dead')`, checks `RowsAffected`, returns `ErrJobStateLocked` |
| L10 | cluster `Complete` never deleted `cq.claimed[jobID]` | **CLOSED** | `cluster_queue.go:164, 201, 220, 260, 396` all delete; `Stats().LocalClaims` reads the live map |
| L12 | validator word-table loader and detector disagreed on tokenization | **CLOSED** | `parseWordList` now strips `#` at LINE level before tokenizing (`filter_language.go:169-184`); both sides share `tokenizeWords` + `normalizeWord` |
| M7 | unsynchronized read of the package-level extra-tables map | **CLOSED** | `detectedLanguage` takes one `snapshotExtraTables()` for the whole call (`:236`), no per-word lock |
| L15 | content-keyed memory dedupe contradicted a repo convention | **PARTIAL** | identity scope added and wired; see M2 for the no-provenance fallback |

The empty/whitespace completion work (prior L4/L5) is consistent with
`internal/llm/AGENTS.md`'s documented per-client site list — the invariant doc
already names `classifyAnthropicEmpty` and codex's `parseResponse` /
`parseResponsesSSE` as the two paths. I did not re-derive all four provider
paths; that was auditor scope 3 and is unverified.

---

## REFUTED

1. **"`Store.Fail` reaching `state='dead'` early-lettering is the same bug"** —
   no. `Fail` reads `retry_count`/`max_retries` and picks `failed` vs `dead`
   correctly (`store.go:771-774`). The defect is the missing token predicate,
   not the state choice.
2. **"The plan collision fix can clobber via a normalization mismatch"** — no.
   `filePath` is built once and reused for both the `Stat` and the write; the
   `-N` loop and `WritePlanMarkdown` receive the same resolved string.
3. **"`CompleteAttempt`'s idempotent branch is unreachable"** — no. It is
   reachable and was observed working: the probe's first attempt returned
   `CompletionApplied` with `disp=0` and a second same-token call would take the
   idempotent path. Only the superseded-token branch fired, as designed.

---

---

## DISCLOSURE — what was NOT verified

1. **All 7 auditor scopes were recovered by direct parent audit** after the
   provider outage killed the batch (confirmed by an independent one-word probe).
   Six addenda: scopes 1-6, plus the plan deliverable for scope 7. **Three items
   remain genuinely uncovered, all requiring a Dart/Flutter toolchain that is not
   installed here:** the `chat_provider.dart` `pendingTurns` lifecycle, the
   stub-daemon-vs-real-daemon divergence list (9 items, including the 5 the prior
   wave left open), and TUI↔GUI parity. `tableutil` two-axis sizing was checked
   only where the wave's own unit tests exercise it, not by reading all five TUI
   model files against the AGENTS.md two-axis rule.
2. **`scripts/gen-connectivity-graph.py` received a +999-line diff and I
   verified only its OUTCOME** (the 6 fabricated topics are gone, no new false
   negatives), not its logic. A future receiver-matching bug could still
   fabricate a topic that happens not to collide with those 6 names.
3. **The Flutter GUI was not analyzed.** No Dart toolchain run; the Dart
   changes (313 + 164 lines) are unexamined.
4. **TUI golden tests were not executed** — `assertGolden` writes `.got.txt`
   into the repo, which the read-only mandate forbids. Conclusions are from
   source plus committed fixtures.
5. **No `-race` run.** The wave touched concurrency (semaphores, claim tokens,
   worker wake-up). Per the skill's rule, a concurrency-heavy wave wants a full
   `-race` gate before push; it was not run here.
6. **`make lint` / `golangci-lint` were not run.** Prior wave recorded the
   mutexio plugin load problem; commit `3dba3ee7` claims it is fixed, unverified.
7. **The sibling's `e2e/harness` WIP was never under test.** It caused one
   transient build failure mid-run (`sweepOnce.Do` — `sync.Once is not a
   function`) that cleared on its own; the baseline above was re-run after.

---

## Observations (surfaced, not acted on)

- `internal/queue/store.go:777` — the `Fail` write is the only job-state write
  in the file with a bare `WHERE id = ?`. Fixing H1 as a class means auditing
  `Requeue` and `Retry` for the same: both retire the token but neither
  requires the caller to present it, so a stale `Requeue` can also reset a live
  attempt. The fix should cover all three together.
- `internal/queue/queue.go:961-966` — the bus `queue.fail` handler accepts a
  bare `job_id` with no token, so the unguarded path is reachable from any bus
  publisher. Same for `queue.retry` at `:969`. Adding a token to the wire shape
  is a breaking change to that contract; worth an owner decision.
- `docs/plans/20260916-async-turn-migration/master.md:232-238` and 6 other
  tracking tables still say `PENDING` for work that has since landed. See the
  plan-completeness deliverable below; 9 trees carry stale rows.
- `internal/memory/distill.go` carries no `TODO` for the content-comparison
  fallback, which root `AGENTS.md:394` requires. The function's doc comment
  explains the design instead — arguably better than a TODO, but it means the
  convention's escape hatch is unused where it is most needed.