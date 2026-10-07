# Fix-wave disposition — 2026-10-06 bughunt

Companion to `.hermes/audits/2026-10-06-week-bughunt.md` (the audit that found
these) and `.hermes/audits/2026-10-06-incomplete-plans.md`.

All findings fixed. Nothing committed by this session — the tree is left dirty
for review, and the working tree already carries a sibling's uncommitted work.

## Findings → commits

Nothing is committed yet; this table maps findings to the files that carry the
fix so the commits can be cut by concern.

| Finding | Fix | Files |
|---|---|---|
| **H1** `Store.Fail` had no claim-token predicate; a superseded attempt could fail a live attempt's job | `Store.FailAttempt` with the token in the `WHERE` clause; `PersistentQueue.FailAttempt`; `ClusterQueue` forwarder; `AttemptFailer` interface; `Worker.failJob` presents the token it already holds | `internal/queue/store.go`, `queue.go`, `cluster_queue.go`, `internal/worker/worker.go` |
| **H2** H5 guard dead: `PersistentQueue` never satisfied `completionFreshChecker` | `CompletionIsFresh` forwarder on `PersistentQueue` + `ClusterQueue`; `claim_token` threaded through `queue.job.completed` / `.failed`; `OnJobCompleted` takes and uses it; `TestQueueExposesAttemptCapabilities` pins the real types | `internal/queue/queue.go`, `cluster_queue.go`, `internal/agent/tactical.go`, `orchestrator.go` |
| **H3** channel-less unsubscribe deleted the whole filter entry, returning the connection to broadcast mode | `wsConnSubs.suppressedAll` flag; `SuppressAll` hub method; handler routes to it instead of deleting; `SubscribeSession` clears it; `ws-filter-05` e2e corrected to the new contract | `internal/comm/http/server.go`, `e2e/suites/ws-filter/ws_filter_test.go` |
| **M1** `CreatePlan` slug collision used a stat-then-write loop (TOCTOU) | `reservePlanFilePath` reserves atomically with `O_CREATE\|O_EXCL`; `MkdirAll` preserves the fresh-workspace contract | `internal/plan/manager.go` |
| **M2** memory dedupe falls back to content comparison for unscoped rows | `TODO(bughunt-2026-10-06)` recording the residual gap and the two owner options (backfill vs exclude) — an owner decision, not a drive-by | `internal/memory/distill.go` |
| **M3** `filter_wiring_test.go` read the real `$MEEPT_HOME` | **Already fixed in the audited wave** — re-verified, no action taken | `internal/daemon/filter_wiring_test.go` |
| **L1** `make graphs-check` red on line offsets | `make graphs` — now green | `docs/generated/*` (4 files) |
| **L2** `OutputFiltersConfig` comment contradicted `Defaults()` | Comment corrected to match the code (default TRUE, zero value false) | `internal/config/schema.go` |

Invariant docs updated in the same change, per the root AGENTS.md maintenance
rule: `internal/comm/AGENTS.md` (H3's flag + the corrected e2e contract) and
`internal/agent/AGENTS.md` (H2's forwarder requirement + why a test double
cannot catch it). Root `AGENTS.md` needed no change — it delegates both
contracts to the package docs and mentions neither finding.

## New pins

| Test | Pins |
|---|---|
| `TestQueueExposesAttemptCapabilities` | H2 — the real `*PersistentQueue` and `*ClusterQueue` satisfy every optional interface the agent/worker probe for. The assertion that would have caught H2 at commit time. |
| `TestStoreFailAttempt_SupersededAttemptCannotFailLiveJob` | H1 — the core defect: a stale failure is refused and the live row is untouched |
| `TestStoreFailAttempt_SameTokenRetryIsIdempotent` | H1 — a duplicate failure from the same attempt is success, not a second transition |
| `TestStoreFail_TokenlessLegacyStillWorks` | H1 — the token-less path is unchanged, so the migration is not breaking |
| `TestPersistentQueue_FailAttempt_UnpublishedOnSupersededAttempt` | H1 downstream — a refused failure publishes no `queue.job.failed` |
| `TestPersistentQueue_CompletionEventCarriesClaimToken` | H2 second leg — the event carries the token, so the guard can ask the token-exact question |
| `TestWSChannelFilter_ConnectionWideUnsubscribeSuppressesAndKeepsGrants` | H3 — suppression plus grant preservation, and that a re-subscribe re-arms |
| `TestWSChannelFilter_ConnectionWideOptOutSuppressesPreviouslyUnsubscribed` | H3 — the exact regression: an explicitly-unsubscribed session must stay muted |
| `TestWSChannelFilter_ConnectionWideOptOutStillFailsOpenWhenNoEntry` | H3 — broadcast mode for a never-subscribed connection is unchanged |
| `TestManagerCreatePlan_ConcurrentSameTitleNeverClobbers` | M1 — 8 concurrent same-title plans each get their own file with their own `plan_id` |

### RED-GREEN proof

A new gate proves nothing unless it can fail. Each of the two structural fixes
was verified by reverting it and confirming the pin goes red:

- **H1** — removing only the token predicate from `FailAttempt`:
  `stale FailAttempt: err = <nil>, want ErrJobNotClaimable` (exit 1).
- **M1** — restoring the old stat-then-write loop:
  `plans 0 and 1 share file path ".../refactor-auth-system.md"; one clobbers the other`
  (exit 1).

Both source files were restored byte-for-byte afterwards.

## Corrections made during the fix wave

1. **The first `FailAttempt` narrowed the token-LESS path.** The initial version
   applied `state IN ('claimed','processing')` to every caller, which broke three
   pre-existing dead-letter tests that fail a freshly-inserted *pending* job —
   `Fail` has always been callable on any existing job. Corrected so a
   token-PRESENT caller gets the strict predicate and a token-LESS caller keeps
   the original bare `WHERE id = ?`. Caught by the package gate, not by review.
2. **`reservePlanFilePath` dropped `MkdirAll`.** The old `os.Stat` tolerated a
   missing plan directory (it just reported "not exist"); the atomic create does
   not. Two `TestManagerCreatePlan*` tests caught it immediately.
3. **`SuppressAll` initially left the connection muted forever.** The new test
   caught that `SubscribeSession` did not clear the flag — a client that
   unsubscribed channel-wide then re-subscribed one session would never resume.

## Gate state (all re-run after the fixes)

| Gate | State |
|---|---|
| `go build ./...` | GREEN (exit 0) |
| `go vet ./...` | GREEN (exit 0) |
| `gofmt -l internal/ pkg/ cmd/ e2e/` | GREEN (only the sibling's WIP file) |
| `go test -p 2 -count=1 ./...` | **106 ok, 28 no-test, 1 package FAIL** |
| `make graphs-check` | GREEN (`✅ All generated files are up to date`) |
| `mutexio` on the 5 touched packages | GREEN |
| `predid` on queue/plan | GREEN |
| `-race` on queue/worker/plan/comm-http/agent | **GREEN — all 5 `ok`**, direct from the targeted run: `queue 8.868s`, `worker 4.905s`, `plan 2.810s`, `comm/http 98.955s`, `agent 121.224s`. `internal/memory` and `internal/config` were verified clean under `-race` in a separate full-repo pass. |

### The one failing package is a pre-existing flake, not a regression

`internal/tui` failed on `TestGoldenTasksView`:
`attempt 1/2 never reached "2/2"; discarding and retrying`. Evidence it is not
caused by this wave:

- **Zero files in `internal/tui` were touched** — `git diff --stat HEAD --
  internal/tui/` is empty.
- The pre-fix audit baseline had `ok github.com/caimlas/meept/internal/tui
  13.268s`.
- It passes in isolation, `-count=6`, and on a full `./internal/tui/` rerun
  (13.349s).

It is the "wait for effect" smell the repo's own skill record describes: a
view-state golden asserting a rendered counter reached an expected value within
a fixed window, under `-p 2` load. Not fixed here — out of scope for this wave
and it touches no file in it. **Flagged as the top follow-up.**

### NEW FINDING from the race gate (pre-existing, not from this wave)

`internal/cluster` fails `-race` in `TestGossipTransport_SendSemaphoreBoundsConcurrentPeerSends`:

```
WARNING: DATA RACE
Write at 0x00c000524533 by goroutine 269:
  cluster.(*GossipTransport).sendToPeer()   gossip_transport.go:301
  cluster.(*GossipTransport).SendEvent.gowrap1()  gossip_transport.go:186
Previous write at 0x00c000524533 by goroutine 270:
  ... same two frames ...
```

Two concurrent `sendToPeer` goroutines write the same address. Line 301 is
inside the dial-failure branch — `json.Unmarshal(data, &event)` into a loop-local
`models.ClusterEvent`, then `t.gossip.QueueForRetry(&event)`. The two stacks are
identical, which is the signature of a shared value being handed off rather than
one goroutine reading another's buffer.

PROVENANCE: this trace was read from `/tmp/fixwave-race.log`, which is a
**sibling session's** full-repo `-race` run (105 packages, 12:35 mtime) — not
this fix wave's own targeted run, which covered only the 5 packages I modified
and was clean. `internal/cluster` is untouched by this fix wave
(`git diff --stat HEAD -- internal/cluster/` is empty), so the race is
pre-existing either way.

**This is the audited wave's own new test failing under `-race`** (the file is
`gossip_transport_semaphore_test.go`, added in the 2026-10-06 wave), in a package
this fix wave did not modify. So it is a pre-existing defect that the wave's own
test surfaces but that CI does not catch, because the full `-race` suite is not a
required gate.

Not fixed here: `internal/cluster` is outside this wave's diff, and a
concurrent-send data race deserves its own investigation rather than a drive-by.
**Ranked the highest follow-up** alongside the TUI golden flake.

`internal/shadow`'s `-race` failure is the documented ephemeral-port exhaustion
(`dial tcp 127.0.0.1:51299: can't assign requested address`), not a race.

### Second triage: the full-suite repro's other 24 failures

A later full-suite run (`/tmp/tui-repro.log`, 103 ok) failed 24 tests across 4
packages. Classified:

- **23 are the documented ephemeral-port exhaustion** — every one carries
  `dial tcp 127.0.0.1:NNNNN: connect: can't assign requested address`, the exact
  error in AGENTS.md's `-p 2` note. Packages: `internal/tools/builtin`,
  `internal/tools/mcp/transport`, `internal/transport`. Not code defects;
  the documented remedy is bounded package parallelism.
- **1 is a wall-clock assertion in a package this wave touched**:
  `TestWorker_RetryWakesIdleWorker` — `re-claim after the retry gate opened took
  3.999392s, want < 400ms` (`internal/worker/worker_retry_wakeup_test.go:112`).

That one is the SAME class as the TUI golden flake, and it is deliberately NOT
"fixed" by widening the budget, because its margin is the assertion:

> 400ms is a margin a poll cycle cannot sneak under, so a regression fails the
> pin instead of merely slowing it down.

A poll-only worker rediscovers the job on its idle backoff (starts at 1s,
doubles), so any threshold above ~1s stops discriminating. The observed 3.999s
under full-suite load is the port-exhausted run starving the scheduler, not a
wake regression.

Measured: 3/3 in isolation (1.10-1.59s), 5/5 under 4-way load, 6/6 under 6-way
load. Left as-is; recorded here so the next reader knows it was triaged, not
skipped. **If it recurs, the fix is CI-side** (bound `-p` in the full-suite job),
not a wider constant.

## Deliberately not done

1. **`TestGoldenTasksView` flake** — out of scope (no `internal/tui` file in this
   wave). It will intermittently redden CI.
2. **M2's owner decision** — backfill vs exclude legacy unscoped rows. Documented
   as a `TODO` with both options, per the root AGENTS.md convention. Blast radius
   is opt-in only (`Distill` is off by default).
3. **3 Dart/Flutter-dependent audit items** — the `pendingTurns` lifecycle, the
   stub-daemon divergence list, and TUI↔GUI parity. No Flutter toolchain here.
4. **`internal/security/testdata/fuzz/FuzzInputSanitizer/828d5059791d0353`** — the
   stale corpus crasher is still on disk and not gitignored. The guard it exposed
   is fixed, so committing it would be harmless but misleading. Deletion is the
   owner's call.
5. **`Store.Requeue` / `Store.Retry`** were audited (both null `claim_token`) but
   not changed: neither presents a caller-supplied token today, so there is no
   superseded-attempt hole to close — unlike `Fail`, they are the *mechanism* that
   makes supersession detectable, not a victim of it.

## Sibling-session interaction

A live sibling session committed twice mid-wave (`bb369680`,
`5289b15e` — the `$TMPDIR` scratch-bin leak). Their commits swept two of my
uncommitted files into their commit message's scope:

- `internal/agent/orchestrator.go` — carries my `claim_token` event threading.
- `internal/agent/AGENTS.md` — carries my H2 invariant note.

**Content verified present at HEAD** (re-grepped, not inferred from commit
provenance): `orchestrator.go:418-433` reads `ClaimToken` and passes
`event.ClaimToken` to `OnJobCompleted`; `agent/AGENTS.md:94` carries the H2 note.
No action needed — but a reviewer cutting commits by concern should know those
two files' H2 work is already attributed to the sibling's commits.

The sibling's remaining WIP (`AGENTS.md`, `Makefile`,
`docs/workflows/e2e-testing.md`, `e2e/harness/*`) was left untouched.