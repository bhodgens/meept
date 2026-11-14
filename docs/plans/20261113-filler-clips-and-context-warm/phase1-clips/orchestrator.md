# Phase 1 — Contextual Filler Clip Cache — Branch Orchestrator

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

Filler/greeting/silence/fragment/farewell lines stop riding live TTS synthesis (0.7–4.8 s Speak() blocking, short-utterance timbre drift) and instead play as cached PCM clips keyed by `sha256(v1,text,vendor,model,voice,refHash)` (root Contract C1). Clips generate dynamically in the background at child start / config reload — never hand-authored — and only when the voice sidecar is actually running (R4 gate). See root master.md Goal + Architecture for the flow diagram; the contracts C1/C2/C3 live there and are FROZEN — quote them verbatim into dispatch contexts.

## Architecture

- `internal/voice/clipstore` (new package, leaf 01): the ONLY code that reads/writes `$MEEPT_HOME/voice_clips/`. Daemon and child both link it (shared Go package; the child binary is built from this repo).
- Leaf 05 (config lanes, `internal/config` + `internal/voice` pools) defines the resolved pool set; leaf 02 (`internal/daemon`) enumerates it and emits C1 `clip_render` frames; leaf 03 (`internal/voicechild`) renders misses via the installed `TTSProvider` and serves hits on `say` frames carrying `clip_id`.
- Leaf 04 (`internal/voice/talkloop.go` + `internal/voiceframe` end-frame path) adds the farewell lane on top of 05's pools.
- File-ownership matrix (writes; read access is open):
  - 01 → `internal/voice/clipstore/*`
  - 05 → `internal/config/schema.go`, `config/meept.json5`, `internal/voice/lanes.go` (new), `internal/configui/sections_voice.go`
  - 02 → `internal/daemon/voice_wiring.go`, `internal/daemon/voice_keys.go`, `internal/daemon/voice_clip_planner.go` (new)
  - 03 → `internal/voicechild/pipeline.go`, `internal/voicechild/child.go`, `internal/voiceframe/frame.go`
  - 04 → `internal/voice/talkloop.go`, `internal/voice/echo_roster.go` (touch only if the say choke needs a farewell kind)
- Serialization: 03 owns `voiceframe/frame.go` field additions (`ClipID`, clip_render event) so 02 consumes, not edits, them — dispatch 03's frame-struct edit FIRST inside wave B if conflict appears; otherwise 02 and 03 meet via Contract C3 pinned verbatim in both briefs.

## Interface Contracts

Frozen in root master.md: C1 (key formula), C2 (store layout), C3 (C1 frame additions), C5 (lane pools + back-compat rule). Dispatch every leaf with the exact contract text copied in. Leaf 01 exposes `ClipKey`, `Store.Get/Put/HasDir`; leaf 05 exposes `VoiceLanes(cfg) []LaneLine{Lane, Text}` where Lane ∈ {greeting, filler, silence, fragment, farewell}; leaf 02 exposes nothing to siblings (terminal daemon side); leaf 03 consumes both.

## Child Index

| Leaf | Type | Est. ctx | Depends on | Wave |
|------|------|----------|------------|------|
| 00T-gate-tests.md | leaf(test) | 60K | none | 0 |
| 01-clip-store.md | leaf | 45K | 00T | A |
| 05-lane-pools.md | leaf | 50K | 00T | A |
| 02-daemon-planner.md | leaf | 60K | 01,03,05 | B |
| 03-child-render-playback.md | leaf | 70K | 01 | B |
| 04-farewell-lane.md | leaf | 55K | 03,05 | C |

## Coding Conventions

Inherited from root master.md Coding Conventions verbatim (dispatch context must include them). Additions: clip I/O never under mutex (mutexio); all store errors degrade to "miss" (live synth fallback) — a broken clip path must never fail a turn or a call; `Makefile` untouched.

## Completion Tracking Table

| Leaf | Status |
|------|--------|
| 00T-gate-tests | PENDING |
| 01-clip-store | PENDING |
| 05-lane-pools | PENDING |
| 02-daemon-planner | PENDING |
| 03-child-render-playback | PENDING |
| 04-farewell-lane | PENDING |

## Test Gate

Suite target: `e2e/suites/voice-clips` (new dir + `e2e/manifest.json` path_map entries for `internal/voice/clipstore`, `internal/voice`, `internal/voicechild`, `internal/daemon` additions — check existing voice-area mapping first and EXTEND, don't duplicate).

| Feature | Unit test | E2E test | Status |
|---------|-----------|----------|--------|
| Key formula stability + invalidation | clipstore_test | voice-clips suite: clip absent → rendered → replayed | PENDING |
| Sidecar-only gate (R4) | daemon planner_test | suite: sidecar disabled → store dir never created | PENDING |
| Clip playback vs live-synth fallback | voicechild pipeline_test | suite: local leg, filler fires, log `clip=true` | PENDING |
| Back-compat (old child / old daemon) | voiceframe_test | suite: frame without clip_id == today shape | PENDING |
| Lane pools + farewell | talkloop_test | suite: farewell clip before end frame | PENDING |

Dispatch 00T first (tests only, RED), validate in-session against this matrix, commit gate tests, mark TESTS_VALIDATED, then wave A (01 ∥ 05), wave B (02 ∥ 03), wave C (04).

## Integration Test Plan

Branch-complete when: all leaves REVIEWED, `go test -p 2 ./internal/voice/... ./internal/voicechild/... ./internal/daemon/...` green, `make e2e-fast-area AREA=voice-clips` green, `make lint-ci` clean on changed packages, `docs/workflows/voice.md` + `internal/voice/AGENTS.md` C1-frame section updated (orchestrator writes the docs commit). Report branch COMPLETE to root.

## Review Checklist

- [ ] Key formula byte-exact vs root C1 (field order, separator \x1f, "v1" prefix).
- [ ] Corrupt/short PCM file → miss → delete → regenerate; never played.
- [ ] R4: with voice child not running, zero file writes under MEEPT_HOME (test asserts dir absence).
- [ ] No PCM bytes on C1; clip frames are JSON keys+text only.
- [ ] `t.say()` remains the single audible choke (echo_roster.go:136 invariant).
- [ ] Live-synth fallback on clip miss still populates the cache opportunistically? NO (decided: fallback stays pure today-path; opportunistic backfill deferred — flag if a leaf added it).
- [ ] Config back-compat: existing scalar `silence_prompt`/`fragment_clarify` keep precedence rule C5.

## Dispatch Protocol

Waves (all dispatch contexts include: root master.md contracts C1/C2/C3/C5
verbatim, root Coding Conventions, isolation-worktree absolute path, "Do NOT
commit"). Validate 00T RED in-session before wave A.

1. Wave 0: `00T-gate-tests.md` (single leaf). Gate: TESTS_VALIDATED.
2. Wave A: `01-clip-store.md` ∥ `05-lane-pools.md` (file-disjoint).
3. Wave B: `02-daemon-planner.md` ∥ `03-child-render-playback.md`.
   File-ownership override: 03 owns voiceframe/frame.go edits; if 02's
   build breaks without them, run 03 first and re-dispatch 02.
4. Wave C: `04-farewell-lane.md`.
5. Per leaf: main-model in-session review (changed files + verify commands
   + Review Checklist) → commit `git commit -- <declared paths>` → bump the
   Completion Tracking Table. Re-dispatch on gaps (max 3 loops).
