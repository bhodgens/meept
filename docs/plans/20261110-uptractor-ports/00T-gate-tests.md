# Gate Tests — Implementation Leaf (00T)

> **For the implementing agent:** You are the implementer for this leaf.
> Write tests ONLY, no implementation. Tests must fail RED for
> missing-implementation reasons (undefined symbols, not panics in setup).
> Do NOT delete, skip, or weaken any test. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing a
> file, do NOT read it back. Report what you built, files touched, deviations.

## Meta

- **Parent:** ./master.md
- **Scope:** The full RED test suite gating every implementation leaf of this tree (master Test Gate matrix).
- **Dependencies:** none
- **Estimated Context:** 90K
- **Concurrency Group:** solo (runs first)

## Goal

Write every gate test named in the master's Test Gate matrix so implementation
leaves only make failing tests pass. Tests pin contracts C1–C7 exactly.

## Context

Source of truth for ported behavior: ~/git/uptractor-jobfile branch
`fix/coordinator-missed-hangup-watchdog` (phone/core/internal/telnyx/callstatus.go,
phone/core/internal/callsvc/reaper.go) and branch `fix/speech-robustness`
(src/lib/agent/fuzzy.ts, docs/SPEECH-ROBUSTNESS.md canonical table). Read those
files (terminal cat) and port their TEST TABLES verbatim where they exist
(reaper_test.go, callstatus_test.go are already written in Go — adapt names to
meept packages, keep every case).

- internal/voice/telnyx/client.go — existing client shape (NewClient(baseURL, apiKey), postCall)
- internal/voice/manager.go:218 releaseOnce, :271 fireRelease, :321 HandleCallEvent
- internal/voice/callstore.go:41 voice_calls schema, :92 NewCallStore, :198 SweepStaleLive (func; :178 is the doc comment)
- internal/ledger/store.go:316 Append(ctx, DraftRecord)
- internal/voicechild/pipeline.go — STT final -> LLM turn path (where Normalize hooks)

## Verified Anchors

- internal/voice/manager.go:218 — `func (m *Manager) releaseOnce` — reaper must reuse this funnel
- internal/voice/manager.go:321 — `func (m *Manager) HandleCallEvent` — synthetic hangup entry
- internal/voice/telnyx/callcontrol.go:77 — `func ParseCallEvent` — where IgnoredEventError classification lands
- internal/voice/callstore.go:92 — `func NewCallStore` — brief column migration point
- internal/ledger/store.go:316 — `func (s *Store) Append` — commitment filing path

## Interface Contracts (From Parent)

### What This Leaf Exposes

Test files only (all must compile-fail RED, never build-tagged out):

```
internal/voice/telnyx/callstatus_test.go        (C1)
internal/voice/telnyx/ignored_event_test.go     (C2)
internal/voice/reaper_test.go                   (C3)
internal/voice/callbrief_test.go                (C4)
internal/voice/brief_commitments_test.go        (C7)
internal/util/fuzzy/fuzzy_test.go               (C5)
internal/voicechild/normalize_test.go           (C6)
internal/voiceintake/guided_intake_test.go       (intake)
internal/voice/recording_test.go                (recording)
```

### What This Leaf Consumes

- C1–C7 from master.md (exact signatures; tests reference the future symbols).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state |
|---|---|---|---|---|---|
| T1 | C1 tests: 200 alive, 200 dead, 404=>Known=false/Alive=false/nil err, empty id err, HTTP 500 err; httptest server | Create internal/voice/telnyx/callstatus_test.go | `go test ./internal/voice/telnyx -run CallStatus` | undefined: GetCallStatus/CallStatus | compiles after leaf 01, all pass |
| T2 | C2 tests: ParseCallEvent("call.analyzed") => IsIgnoredEvent true; ("call.hangup") => normal; ("sms.inbound") => plain error NOT ignorable | Create internal/voice/telnyx/ignored_event_test.go | `go test ./internal/voice/telnyx -run Ignored` | undefined: IgnoredEventError | pass after leaf 01 |
| T3 | C3 tests: DEAD-AIR semantics (owner-ratified 2026-11-11): call with fresh caller activity NOT reaped regardless of age; call silent > DeadAir => released via FailOutcome exactly once with CallEventErrored on bus reason "reaped:hangup-lost"; MaxDuration => FailOutcome reason "reaped:max-duration"; nil Status => no goroutine work, Error logged; defaults Interval 30s / DeadAir 60s / MaxDuration 30m; reaper's own {ccid: lastActivity} map seeded at answered observer, refreshed on caller-side audio/turn events (no Manager state change) | Create internal/voice/reaper_test.go | `go test ./internal/voice -run Reaper` | undefined: ReaperConfig/RunReaper | pass after leaf 02 |
| T4 | C4 tests: NewCallStore adds brief+ext columns idempotently (open twice, no error, old rows get ''); CallBrief JSON round-trip with frozen shape incl. opaque ext; stub-at-answer status in_progress; final-at-release status completed; replayed hangup => one brief; restart with live row => SweepStaleLive marks failed AND brief status abandoned per O1 decision | Create internal/voice/callbrief_test.go | `go test ./internal/voice -run Brief` | undefined: CallBrief | pass after leaf 04 |
| T5 | C7 tests: brief with 2 commitments => 2 ledger rows kind=commitment source=voice source_ref=ccid; re-finalize => still 2 (dedupe); zero commitments => zero rows | Create internal/voice/brief_commitments_test.go | `go test ./internal/voice -run Commitments` | undefined symbols | pass after leaf 04 |
| T6 | C5 tests: canonical SPEECH-ROBUSTNESS.md table verbatim (Meadow Ridge/Rich/Ridg, summer planes no-match, Oak Street/St, siding, "mark the siding done", roof flashing) + EditDistance known pairs + FuzzyFind sort order + float epsilon comparisons | Create internal/util/fuzzy/fuzzy_test.go | `go test ./internal/util/fuzzy` | package does not exist | pass after leaf 03 |
| T7 | C6 tests: "the the the"=>"the"; "yeah yeah yeah"=>"yeah"; "um um um"=>"um"; preserve-list keeps unknown proper noun; idempotence property over all canonical fixtures | Create internal/voicechild/normalize_test.go | `go test ./internal/voicechild -run Normalize` | undefined: NormalizeUtterance | pass after leaf 03 |
| T8 | Intake tests: ambiguous fuzzy (two candidates >= threshold) => refuse result listing both; single match => proceed; save blocked until explicit confirm flag | Create internal/voiceintake/guided_intake_test.go | `go test ./internal/voiceintake -run Intake` | undefined | pass after leaf 05 |
| T9 | Recording tests: config default off => no StartRecording call on answer; on + consent pin set => StartRecording(dual-channel mp3) at answer; on + no consent pin => refuse + Error log; recording.saved webhook => URL stored on call record | Create internal/voice/recording_test.go | `go test ./internal/voice -run Recording` | undefined | pass after leaf 06 |
| T10 | Eval harness test: `make test-voice-eval` target exists and runs; planted prompt-leak fixture fails the lane; clean fixture passes | Create magefiles or Makefile target + eval fixture dir per leaf 07 brief | `make test-voice-eval` | target missing | pass after leaf 07 |

Given/When/Then: every table test case must state precondition (fixture/httptest
response), action (call under test), observable (return value, bus event, DB
row). Copy the uptractor Go tests' structure where they exist — do not
reinvent case sets.

## Self-Verification Checklist

- [ ] Every master Test Gate matrix row has a test file + named test functions
- [ ] All tests fail RED for undefined-symbol reasons (run verify commands; record output)
- [ ] No implementation code written (only _test.go files + the T10 Makefile stub if needed)
- [ ] Canonical fuzzy table and reaper cases ported verbatim from source repo
- [ ] No test skips, no t.Skip, no build tags

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** every created path
2. **Verify command + output:** each RED run, last 10 lines
3. **Deviations:** none / list with rationale

## Review Checklist (For Review Agent)

- [ ] Matrix coverage complete; canonical tables match source repo verbatim
- [ ] RED for the right reason (compile-fail on missing symbols, not setup bugs)
- [ ] Contract signatures in tests match master C1–C7 exactly
- [ ] No implementation smuggled in

Output: APPROVED or specific gaps with file + line references.

## Notes

- reaper_test.go in the uptractor repo already uses a fake CallStatusChecker —
  reuse that pattern; meept's Manager needs a constructor seam for the fake
  (leaf 02 provides it; the gate test may reference the future seam).
- Tests touching DB use t.TempDir(); never the real ~/.meept.
