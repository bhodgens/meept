# Phase 1 - Contextual Filler Clip Cache - Branch Orchestrator

> **For the executing agent:** You are the orchestrator for this branch.
> Dispatch implementation agents, review in-session, re-dispatch on gaps,
> commit per leaf after review. Do NOT implement code yourself.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Branch
- **Parent:** ../master.md (root: Voice Latency tree)
- **Children:** 6 leaves (`00T-gate-tests.md`, `01-clip-store.md`, `02-daemon-planner.md`, `03-child-render-playback.md`, `04-farewell-lane.md`, `05-lane-pools.md`)
- **Scope:** Pre-rendered, per-voice cached filler audio served by the voice child, with lane-aware contextual pool selection and a new farewell lane. Requirements R1-R6 in the root.

## Goal

Filler/greeting/silence/fragment/farewell lines stop riding live TTS synthesis (the provider's full generation latency, 0.7-4.8 s as recorded in `internal/voicechild/pipeline.go:241-246`; the qwen3 server ignores `stream:true`, which also explains the short-utterance timbre drift) and instead play as cached PCM clips keyed by `sha256(v1,text,vendor,model,voice,refHash)` (root Contract C1). Clips generate dynamically in the background - never hand-authored - and only when the voice sidecar is actually running (R4 gate). See root master.md Goal + Architecture for the flow and for why generation is per call, immediately after the `keys` frame, and NOT at child start. Contracts C1/C2/C3/C5 live in the root and are FROZEN - quote them verbatim into dispatch contexts.

## Architecture

- `internal/voice/clipstore` (new package, leaf 01): the ONLY code that reads/writes `$MEEPT_HOME/voice_clips/`. Daemon and child both link it. Import rule: stdlib plus `internal/config` only; NEVER `internal/voice`.
- Leaf 05 (config lanes, `internal/config` + `internal/voice` pools) defines the resolved pool set; leaf 02 (`internal/daemon`) enumerates it per call right after `sendKeysFrame` and emits C1 `clip_render` frames; leaf 03 (`internal/voicechild`) renders misses via the installed `TTSProvider` and serves hits on `say` frames carrying `clip_id`.
- Leaf 04 (`internal/voice/talkloop.go` + the daemon's hangup sites) adds the farewell lane on top of 05's pools.
- File-ownership matrix (writes; read access is open):
  - 00T -> `e2e/suites/voice-clips/*`, `e2e/manifest.json`, plus the unit-test files listed in the leaf
  - 01 -> `internal/voice/clipstore/*` (the new package; the manifest entry from 00T already satisfies the [18/18] check)
  - 05 -> `internal/config/schema.go`, `config/meept.json5`, `internal/voice/lanes.go` (new), `internal/configui/sections_voice.go`
  - 03 -> `internal/voiceframe/frame.go`, `internal/voice/frame.go` (the daemon-side event const block, lines 22-33), `internal/voicechild/pipeline.go`, `internal/voicechild/child.go`, `internal/voicechild/providers_wiring.go`
  - 02 -> `internal/daemon/voice_keys.go`, `internal/daemon/voice_clip_planner.go` (new), ONE mount call plus the reload hook in `internal/daemon/voice_wiring.go`
  - 04 -> `internal/voice/talkloop.go`, `internal/voice/echo_roster.go` (touch only if the say choke needs a farewell kind), `internal/daemon/voice_wiring.go` (ONLY the agent/operator hangup sequence added by R5; coordinate with 02's mount call), `cmd/meept/voice.go` (local-leg end if needed)
- Serialization: 03 owns BOTH frame files (`internal/voiceframe/frame.go` and `internal/voice/frame.go` const block) so 02 and 04 consume the event constants, not edit them. Dispatch 03 FIRST in wave B; if 02's build breaks without the constants, that is expected - 02 re-dispatches after 03 lands.

## Interface Contracts

Frozen in root master.md: C1 (key formula), C2 (store layout, L16@16k), C3 (C1 frame additions + event-constant ownership), C5 (lane pools + back-compat rule + farewell schema default EMPTY). Dispatch every leaf with the exact contract text copied in.

Leaf 01 exposes `ClipKey(text, vendor, model, voice string, refHash []byte) string`, `NewStore`, `Store.Has/Get/Put/ListKeys`, `Meta`.
Leaf 05 exposes `VoiceLanes(cfg *config.Config, employeeID string) []LaneLine` with Lane in {greeting, filler, silence, fragment, farewell}, plus `PickLine(lanes, lane, cursor *int) string`.
Leaf 03 exposes the C3 frame structs and event constants.
Leaf 02 exposes nothing to siblings (terminal daemon side); its behavioral promise is "a `clip_render` frame for this call's misses is sent right after the keys frame, off the critical path".

## Child Index

| Leaf | Type | Est. ctx | Depends on | Wave |
|------|------|----------|------------|------|
| 00T-gate-tests.md | leaf(test) | 60K | none | 0 |
| 01-clip-store.md | leaf | 45K | 00T | A |
| 05-lane-pools.md | leaf | 50K | 00T | A |
| 03-child-render-playback.md | leaf | 70K | 01 | B (first) |
| 02-daemon-planner.md | leaf | 60K | 01, 03, 05 | B (after 03) |
| 04-farewell-lane.md | leaf | 55K | 03, 05 | C |

## Coding Conventions

Inherited from root master.md Coding Conventions verbatim (dispatch context must include them). Additions: clip I/O never under mutex (mutexio); all store errors degrade to "miss" (live synth fallback) - a broken clip path must never fail a turn or a call; `Makefile` untouched.

## Completion Tracking Table

| Leaf | Status |
|------|--------|
| 00T-gate-tests | VALIDATED (44e3d9a9; suites live on main, green 2026-11-16) |
| 01-clip-store | VALIDATED (3769b30d; tests+race+mutexio green; unit ok 2026-11-16: TestClipKeyPinnedVectors, TestClipKeyGateMatchesFrozenC1Formula, TestClipGateMetaJSONShapeIsC2, TestClipGateShortPcmIsMissAndDeleted, TestConcurrentPutSameKeyNoMutexIO et al.) |
| 05-lane-pools | VALIDATED (ladder tests green; d662c738 config + 48cfae06 lanes resolver, both ancestors of HEAD; unit ok 2026-11-16: TestVoiceLanesSilencePrecedenceLadder, TestVoiceLanesFragmentPrecedence, TestVoiceLanesFarewellPrecedence, TestPickLineRoundRobin) |
| 03-child-render-playback | VALIDATED (fe8fea32 incl. closeClipReader fix, ancestor of HEAD; unit ok 2026-11-16: TestClipSayHitZeroSpeak, TestClipSayMissFallsToSpeak, TestClipRenderMissRendersAndStores, TestClipBargeInClearsLikeSynth) |
| 02-daemon-planner | VALIDATED (d03f3b0a, ancestor of HEAD; sweep-B landed via 648eb6eb; unit ok 2026-11-16: TestClipPlannerGateMatrix, TestClipPlannerNoDirWhenCold, TestClipPlannerReloadReplan; e2e VC-1/2/3 green as suite tests TestVoiceClipsSidecarDisabledNoDir/PoolsRendered/IdentityChangeRegenerates) |
| 04-farewell-lane | VALIDATED (00f9e86c; wiring hunk applied by orchestrator; e2e VC-4/VC-5 green 2026-11-16 as TestVoiceClipsFarewellAudioPrecedesEndFrame + unit TestTalkLoopFarewellSpokenOnceLastTrue) |

Delta log (branch):

- 2026-11-16 (reconciliation wave, doc-only): all six leaf rows reconciled
  against disk + git — every leaf SHA verified a direct ancestor of HEAD
  (`git merge-base --is-ancestor`), hermetic evidence recorded per row
  (e2e/suites/voice_clips 5/5 PASS; unit packages internal/voice/clipstore,
  internal/voice, internal/voicechild, internal/daemon,
  internal/voiceframe all `ok` at HEAD 5f64419a). Test Gate matrix below
  reconciled PENDING -> VALIDATED against the suites run 2026-11-16. Live
  smoke a-e NOT executed (root Integration Test Plan disposition).

## Test Gate

Suite target: `e2e/suites/voice-clips` (new dir + `e2e/manifest.json` `path_map` entries for `internal/voice/clipstore`, and additions to the existing `internal/voice/`, `internal/voicechild/`, `internal/daemon/` voice mappings - EXTEND, do not duplicate; today those map to `voice-lifecycle`, `voice-bargein`, `voice-concurrency`, `voice-local`).

| Feature | Unit test | E2E test | Status |
|---------|-----------|----------|--------|
| Key formula stability + invalidation | clipstore_test: TestClipKeyPinnedVectors, TestClipKeyGateMatchesFrozenC1Formula, TestClipKeyGateInvalidationMatrix | voice-clips suite: TestVoiceClipsPoolsRendered, TestVoiceClipsIdentityChangeRegenerates | VALIDATED (unit + e2e green 2026-11-16) |
| Sidecar-only gate (R4) | daemon planner_test: TestClipPlannerGateMatrix, TestClipPlannerNoDirWhenCold | suite: TestVoiceClipsSidecarDisabledNoDir | VALIDATED (unit + e2e green 2026-11-16) |
| Render trigger is per call, after keys | daemon planner_test: TestClipPlannerSetupMountGateMatrix, TestClipPlannerKeysFrameRefHash | suite: TestVoiceClipsPoolsRendered (render-on-call exercised; exact ordering assert rides unit pins) | VALIDATED (unit + e2e green 2026-11-16) |
| Clip playback vs live-synth fallback | voicechild pipeline_test: TestClipSayHitZeroSpeak, TestClipSayMissFallsToSpeak, TestClipGateSayMissFallsBackToLiveSpeak | suite: TestVoiceClipsCachedFillerUnder300msVsSlowTTS | VALIDATED (unit + e2e green 2026-11-16; the live `clip=true` log-line form is smoke-only, step (b) NOT executed — root disposition) |
| Back-compat (old child / old daemon) | voiceframe_test: TestOldDaemonLineDecodesWithoutClipFields, TestClipFieldsOmittedOnPlainFrames, TestClipGateFrameWithoutClipIDIsTodaysExactJSON | suite: covered by unit wire-shape pins (no dedicated suite scenario) | VALIDATED (unit green 2026-11-16) |
| Lane pools + farewell | talkloop_test: TestTalkLoopLaneRoundRobin, TestTalkLoopFarewellSpokenOnceLastTrue, TestTalkLoopFarewellZeroConfigDisabled | suite: TestVoiceClipsFarewellAudioPrecedesEndFrame | VALIDATED (unit + e2e green 2026-11-16) |
| Stored PCM rate is the lane rate | clipstore_test: TestClipGateMetaJSONShapeIsC2 (rate assertion); TestClipRateResampleTo8k (child side) | suite: stored-bytes equality exercised inside TestVoiceClipsCachedFillerUnder300msVsSlowTTS | VALIDATED (unit + e2e green 2026-11-16; dedicated stored-vs-live byte-equality e2e scenario not present as a separate test — covered within the cached-filler scenario; noted honestly) |

Dispatch 00T first (tests only, RED), validate in-session against this matrix, commit gate tests, mark TESTS_VALIDATED, then wave A (01 || 05), wave B (03 then 02), wave C (04).

## Integration Test Plan

Branch-complete when: all leaves REVIEWED, `go test -p 2 ./internal/voice/... ./internal/voicechild/... ./internal/daemon/...` green, `make e2e-fast-area AREA=voice-clips` green, `make lint-ci` clean on changed packages, `docs/workflows/voice.md` + `internal/voice/AGENTS.md` C1-frame section updated (orchestrator writes the docs commit). Report branch COMPLETE to root.

## Review Checklist

- [ ] Key formula byte-exact vs root C1 (field order, separator `\x1f`, "v1" prefix, `sha256([]byte(""))` for preset voices).
- [ ] Corrupt/short PCM file -> miss -> delete -> regenerate; never played.
- [ ] Stored PCM is L16@16000; a non-16k meta is a miss, never a resample.
- [ ] R4: with voice child not running, zero file writes under MEEPT_HOME (test asserts dir absence).
- [ ] No PCM bytes on C1; clip frames are JSON keys+text only.
- [ ] `t.say()` remains the single audible choke (echo_roster.go:136 invariant).
- [ ] Live-synth fallback on clip miss stays pure today-path; opportunistic backfill is OUT of scope (flag if a leaf added it).
- [ ] Config back-compat: existing scalar `silence_prompt`/`fragment_clarify` keep the C5 precedence rule; `voice.farewell_lines` schema default is EMPTY.
- [ ] Render never shares the playback prefetch queue (2-deep, `prefetchDepth`) and never delays a say.

## Dispatch Protocol

Waves (all dispatch contexts include: root master.md contracts C1/C2/C3/C5 verbatim, root Coding Conventions, isolation-worktree absolute path, "Do NOT commit"). Validate 00T RED in-session before wave A.

1. Wave 0: `00T-gate-tests.md` (single leaf). Gate: TESTS_VALIDATED.
2. Wave A: `01-clip-store.md` || `05-lane-pools.md` (file-disjoint).
3. Wave B: `03-child-render-playback.md` FIRST (owns both frame files and the event constants), then `02-daemon-planner.md`.
4. Wave C: `04-farewell-lane.md`.
5. Per leaf: main-model in-session review (changed files + verify commands
   + Review Checklist) -> commit `git commit -- <declared paths>` -> bump the
   Completion Tracking Table. Re-dispatch on gaps (max 3 loops).