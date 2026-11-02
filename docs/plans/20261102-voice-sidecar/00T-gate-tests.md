# Voice sidecar gate tests - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Write tests ONLY - no implementation code. Every test must fail RED for
> missing-implementation reasons (undefined symbol, not assertion drift).
> Park each test file under a `.staged` extension (e.g.
> `internal/voice/manager_test.go.staged`) so the tree compiles; the
> orchestrator activates each file at its owning leaf's commit. Do NOT
> commit. Do NOT use read_file on existing source files - explore with
> search_files or terminal cat. Write once, do not read back.

## Meta

- **Parent:** master.md
- **Scope:** The full Test Gate matrix from master.md as parked RED tests
- **Dependencies:** none
- **Estimated Context:** 60K
- **Concurrency Group:** A (dispatched FIRST, alone)
- **Audit references:** master.md Test Gate table (15 rows)

## Goal

Author every unit and e2e pin listed in the master Test Gate matrix. Tests
define the frozen behavior (contracts C1-C10); implementation leaves make
them green. Coverage converges upward only.

## Context

Repo: meept (Go). Tests-first gate convention: RED tests parked under
`.staged`, activated per leaf. e2e suites live at e2e/suites/<name>/ with
the `e2e` build tag and a manifest entry. Fake Telnyx = httptest server
(webhook POSTs) + a websocket test server (media frames); fake child =
goroutine speaking C1 frames over the real unix sockets.

- master.md - contracts C1-C10, verbatim
- docs/workflows/voice-sidecar.md - behavior authority
- ~/git/aiedge-uptractor/core/internal/callsvc/manager_test.go - shape reference for manager tests
- ~/git/aiedge-uptractor/core/internal/mediabridge/bridge_test.go - shape reference for barge-in loopback

## Verified Anchors

- e2e/manifest.json - suite + path_map registry
- e2e/harness/ - Stack harness (findDaemonBinary, FreePort, WithPreBootHook)
- internal/config/schema.go - STTConfig/TTSConfig precedent for section structs

## Interface Contracts (From Parent)

### What This Leaf Exposes

Parked test files, one per matrix row group:
```
internal/config/voice_config_test.go.staged          (C3 defaults)
internal/voice/telnyx/client_test.go.staged          (C4 parse/sign/dial)
internal/voice/manager_test.go.staged                (C5 lifecycle)
internal/voice/splice_test.go.staged                 (reorder/pacing/chunk)
internal/voice/supervisor_test.go.staged             (C6 timeout/fallback)
internal/voice/dial_test.go.staged                   (refusals/effects C9)
internal/voice/daybrief_test.go.staged               (C7 TZ differential)
internal/voice/registry_test.go.staged               (C8 limits)
internal/voice/import_test.go.staged                 (no-jargo pin)
e2e/suites/voice-lifecycle/voice_lifecycle_test.go.staged
e2e/suites/voice-bargein/voice_bargein_test.go.staged
e2e/suites/voice-concurrency/voice_concurrency_test.go.staged
```

### What This Leaf Consumes

- C1-C10 (master.md) - every assertion quotes the contract verbatim

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | C3 config defaults test | Create: internal/config/voice_config_test.go.staged | mv to .go; go test -p 2 ./internal/config/ -run TestVoiceConfigDefaults | undefined: VoiceConfig | compiles+passes after leaf 01 | Given DefaultConfig(); When cfg.Voice read; Then every C3 default matches |
| T2 | C4 client tests: ParseCallEvent (incl. streaming.* no-ops, flexible from/to), signature verify (valid/invalid/timestamp-skew), DialOut against httptest (payload keys exact: connection_id,to,from,timeout_secs), StartStreaming body asserts stream_bidirectional_mode=rtp + codec + sampling rate | Create: internal/voice/telnyx/client_test.go.staged | go test -p 2 ./internal/voice/telnyx/ | package does not exist | passes after leaf 02 | Given canned Telnyx JSON; When parsed; Then CallEvent fields exact |
| T3 | C5 manager tests: inbound answer+stream, outbound NO AnswerCall (assert zero answer calls on the fake controller), untracked answered refused, unknown hangup nil, idempotent OnMediaStart (second start no-op), WaitCall closes on hangup | Create: internal/voice/manager_test.go.staged | go test -p 2 ./internal/voice/ -run TestManager | undefined | passes after leaf 05 | Given tracked outbound id; When call.answered; Then StartStreaming called and AnswerCall NOT called |
| T4 | Splice tests: out-of-order frames re-emitted in sequence order; gap > reorder_gap_ms filled with silence bytes of the missing duration and metric counted; chunk size = 640 for L16@16k and 160 for PCMU@8k; pacing = one chunk per 20ms (fake clock); clear frame written to Telnyx socket on barge-in | Create: internal/voice/splice_test.go.staged | go test -p 2 ./internal/voice/ -run TestSplice | undefined | passes after leaf 06 | Given frames seq 1,3,2; When spliced; Then child receives 1,2,3 |
| T5 | Supervisor tests: timeout at supervisor_timeout_ms returns fallback verdict and call continues; quota error never parks (assert no park path); allowed/denied per sentence | Create: internal/voice/supervisor_test.go.staged | go test -p 2 ./internal/voice/ -run TestSupervisor | undefined | passes after leaf 09 | Given a chatter that blocks > timeout; When Check; Then fallback Verdict, no park |
| T6 | Dial tests: refusals for empty public_stream_url / connection_id / target not allowed / no slot / duplicate live target - each BEFORE any HTTP call (httptest asserts zero requests); effects reserve-then-dial ordering; retry of same request_id does not dial twice | Create: internal/voice/dial_test.go.staged | go test -p 2 ./internal/voice/ -run TestDial | undefined | passes after leaf 11 | Given reserved effect; When same request_id dials again; Then zero DialOut calls |
| T7 | Day brief test: TZ differential (runs under TZ=UTC and host zone; local-midnight boundary asserted), caps (summary <=200 runes each, PromptBlock <=2000 runes) | Create: internal/voice/daybrief_test.go.staged | go test -p 2 ./internal/voice/ -run TestDayBrief | undefined | passes after leaf 04 | Given an activity row at 23:30 local yesterday; When brief built at 00:30 local; Then row excluded |
| T8 | Registry tests: Acquire over max_concurrent_calls returns ErrBusy; second live call to same target returns ErrDuplicateTarget; Release frees both | Create: internal/voice/registry_test.go.staged | go test -p 2 ./internal/voice/ -run TestRegistry | undefined | passes after leaf 14 | Given cap 1; When 2nd Acquire; Then ErrBusy |
| T9 | Import pin: go/parser scan of internal/voice/*.go asserting no jargo import path | Create: internal/voice/import_test.go.staged | go test -p 2 ./internal/voice/ -run TestNoJargoImport | undefined | passes after leaf 07 | Given any internal/voice file importing jargo; When test runs; Then FAIL |
| T10 | e2e suites (hermetic, fake Telnyx + fake child): voice-lifecycle (inbound answer->greeting->prompt->say->hangup->idle stop), voice-bargein (interrupt mid-say: clear frame observed on fake Telnyx socket, other call's say unaffected), voice-concurrency (two employees, two numbers, simultaneous calls) | Create: e2e/suites/voice-*/voice_*_test.go.staged | make e2e-fast-area AREA=voice-lifecycle | suite not registered | passes after leaf 15 | Given scratch daemon + fake endpoints; When simulated call flow; Then C1 frames observed in order |

## Self-Verification Checklist

- [ ] Every master Test Gate matrix row has at least one parked test
- [ ] Each parked file, when activated alone, fails RED with an undefined-symbol or missing-package compile error (never an assertion failure)
- [ ] Contract values quoted verbatim from master.md (no paraphrase)
- [ ] No implementation code written
- [ ] ASCII-only (grep for em-dash before finishing)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** every parked path
2. **Verify command + output:** for each file: activation + RED reason (last 5 lines)
3. **Deviations:** none / list

## Review Checklist (For Review Agent)

- [ ] Matrix coverage complete; no row without a test
- [ ] RED-for-the-right-reason verified per file (compile error, not logic fail)
- [ ] Assertions match C1-C10 exactly (frame keys, defaults, error names)
- [ ] No gate test weakens a spec rule (e.g. accepts AnswerCall on outbound)

Output: APPROVED or specific gaps with file + line references.

## Notes

TZ differential tests must run under both TZ=UTC and the host zone in the
same test (t.Setenv), per the repo's calendar-convention defect class.
