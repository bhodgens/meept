# Voice TTS/STT optimization campaign - Root Orchestrator

> **For the executing agent:** You are the root orchestrator. Dispatch branch
> implementations, review in-session, re-dispatch on gaps, track completion.
> Do NOT implement code. Do NOT commit until a child passes review.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 4 branches
- **Scope:** Ten code-verified voice TTS/STT optimizations, all additive or behavior-preserving by default.

## Goal

Reduce first-audio latency, remove wasted synthesis work, and improve
endpointing accuracy in the meept voice sidecar. Ship each change behind
an opt-in flag or as a byte-identical internal refactor. Preserve the
current behavior for every configuration that does not set a new knob.

Companion findings index (outside this tree, to satisfy the tree checker):
`docs/plans/20261118-voice-tts-stt-optimizations-findings.md`.

## Architecture

The voice sidecar splits into a daemon (`internal/voice`, `internal/daemon`)
and a child process (`internal/voicechild`, `cmd/meept-voice`) joined by
C1 JSON text frames over one socket and C2 raw PCM socket pairs. Config
flows daemon -> `KeysFrame` (C1 keys line) -> child factories -> vendor
adapters. Every new tuning knob crosses that seam as an additive,
omitempty field with a legacy default. Branches: foundation (Smart Turn),
STT path, TTS path, context warm. Worktree execution only.

## Interface Contracts

### C1: KeysFrame additive selector block

```
// internal/voicechild/keys.go:31
type KeysFrame struct {
    // existing fields unchanged
    TurnConfirmMs int              // existing; 900 default
    // NEW, all omitempty on wire, zero/empty = legacy:
    SmartTurnMode      int    // 0=off(default), 1=kill-switch(no model), 2=consume-verdict
    WhisperServerAddr  string // ""=exec-per-utterance (legacy)
    DeepgramEndpointMs int    // 0=off (timer only); positive = fast path
    Vocabulary         []string // nil/empty=no priming
    Qwen3ChunkSize     int    // 0=default 4
    ClipPrewarmDelayMs int    // 0=legacy (immediate); positive=defer ms after played edge
}
// Owner: 01-foundation/01-keys-contract.md
// Consumers: 01-foundation/02-smart-turn.md, 02-stt-path/*, 03-tts-path/*
```

### C2: VoiceConfig additive config block

```
// internal/config/schema.go:5622 (VoiceConfig)
SmartTurnMode      int      `json:"smart_turn_mode"`       // 0 default
WhisperServerAddr  string   `json:"whisper_server_addr"`   // "" default
DeepgramEndpointMs int      `json:"deepgram_endpoint_ms"`  // 0 default
RecognitionVocabulary []string `json:"recognition_vocabulary"` // nil default
Qwen3ChunkSize     int      `json:"qwen3_chunk_size"`      // 0 => 4
ClipPrewarmDelayMs int      `json:"clip_prewarm_delay_ms"` // 0 default
ContextWarmWindowMs int     `json:"context_warm_window_ms"` // 0 => turn_confirm_ms
// Owner: 01-foundation/01-keys-contract.md
// Consumers: all branches
```

### C3: Transcript endpoint flag

```
// internal/voicechild/pipeline.go:46
type Transcript struct {
    // existing fields unchanged
    Endpoint bool // NEW: true when Deepgram speech_final / UtteranceEnd arrives
}
// Owner: 02-stt-path/03-deepgram-endpoint.md
// Consumers: 02-stt-path/03-deepgram-endpoint.md only
```

### C4: Stream control rule (invariant)

```
No I/O under playMu. No new lock nested with playMu or turnMu.
Analyzer calls (turnMu) never hold playMu.
Clip render yield check uses existing playMu-protected fields only.
// Owner: every leaf
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|--------------|-------------|
| 00 | 00T-root-e2e.md | leaf (test) | none | 25K | A |
| 01 | 01-foundation/orchestrator.md | branch | 00 | 35K | B |
| 02 | 02-stt-path/orchestrator.md | branch | 01 | 40K | C |
| 03 | 03-tts-path/orchestrator.md | branch | 01 | 40K | C |
| 04 | 04-context-warm/orchestrator.md | branch | 01 | 25K | C |

Same-letter children dispatch together. Branch impl leaves serialize where
they share a file (see Dispatch Protocol).

## Dispatch Protocol

1. Create the isolation worktree BEFORE wave 1:
   `git worktree add .hermes/worktrees/voice-tts-stt-optimizations -b plan/voice-tts-stt-optimizations`.
   Ensure `.hermes/worktrees/` is gitignored in the primary repo.
2. Phase 0: dispatch `00T-root-e2e.md` with all branch test leaves in parallel.
   Brief: tests only, fail RED for missing-implementation reasons, do NOT
   commit. Validate IN-SESSION: run each test, map to coverage matrix, apply
   delta protocol (lesser artifact updates to greater). Commit gate tests
   with `--no-verify` only after recording the manual gate commands.
3. Phase 1: dispatch `01-foundation` implementation. It owns the shared
   config + keys seam. No other branch may edit `schema.go`, `voice_keys.go`,
   or `keys.go` until foundation is COMPLETE.
4. Phase 2: after foundation COMPLETE, dispatch STT, TTS, and context-warm
   branches. Branches are file-disjoint except `sections_voice.go` and
   `voice_wiring.go`; SERIALIZE any leaf that touches either file. Add an
   explicit file-ownership note to that leaf's dispatch brief.
5. Per leaf: review IN-SESSION (main model, never a delegated reviewer).
   Read changed files, verify contracts, run tests, confirm gate-test diff is
   empty. Pass: stage exact paths, commit, status REVIEWED. Fail: re-dispatch
   with specific gaps, max 3 cycles, then escalate.
6. Phase 3: after all four branches COMPLETE, run integration gates, check
   no import cycles, merge `plan/voice-tts-stt-optimizations` into main in
   the primary checkout, re-run the gate set scoped to the merged diff,
   remove the worktree and branch.

Implementation dispatch shape: `delegate_task` with
`tasks: [{goal: <one-line imperative>, context: <full leaf brief + inlined
contracts + do-not-commit + corruption rules>}]`.

## Review Checklist

- [ ] All leaf tasks implemented
- [ ] C1 keys fields additive, omitempty, legacy defaults exact
- [ ] C2 config fields additive, legacy defaults exact
- [ ] C3 Transcript.Endpoint only used inside STT branch
- [ ] C4 lock rule held (no I/O under playMu; no nested locks)
- [ ] Gate tests pass GREEN and are unmodified by implementers
- [ ] No scope creep, no debug artifacts, no line-number corruption
- [ ] AGENTS.md + docs/workflows updated for every changed surface

## Coding Conventions

- Go 1.27; `go.mod` pins GOTOOLCHAIN. Run `gofmt` before reporting.
- No `read_file` on existing source during implementation; use search_files/cat.
- Write once, do not read the file back.
- Wrap errors with `%w`; return early. No bare `panic(err)`.
- No ignored errors; no `_ = ` in non-test additions (pre-commit blocks it).
- Tests: table-driven Go tests in the package; hermetic e2e suites only.
- `go test -p 2 ./...` (port exhaustion on macOS without -p 2).
- ASCII only in source and docs. UI text lowercase.
- Never log key material, tokens, or secrets.

## Requirement Traceability

| Req | Source (user words, verbatim when possible) | Plan element (leaf/task/contract) | Verified by |
|-----|---------------------------------------------|-----------------------------------|-------------|
| R1 | "review meept's tts/stt path and tell me where, if anywhere, we can make optimizations and refinements based on our skills" | Findings index F1-F10 + all branch leaves | root e2e + branch e2e |
| R2 | "make a hierarchial plan to address all issues" | This tree; F1-F10 each mapped to one leaf | coverage review at Phase 0 |
| R3 | "with subagent, also do #4" (skill patch) | COMPLETED before authoring; youtube-content skill patched and verified | `skill_view(name='youtube-content')` |
| R4 | Prior turn: "keep everything opt-in or byte-identical by default" | C1/C2 contracts; every leaf T-x default row | per-leaf default-preservation test |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|--------------|
| 00T-root-e2e | COMPLETE | 10 e0475e9d: gate committed, RED confirmed, now GREEN |
| 01-foundation | COMPLETE | 1 | all 4 children COMPLETE; seam + F1 landed (23a908b3, f10eaf5c) owns shared seam; must complete before others |
| 02-stt-path | COMPLETE | 1 | all 5 children COMPLETE; F2/F4/F5 landed (5ebd58b5, f10eaf5c, d0041a7f) gated on foundation |
| 03-tts-path | COMPLETE | 1 | all 7 children COMPLETE; F3/F6/F8/F9/F10 landed (52800c19, dd53a70c, 5ebd58b5) gated on foundation |
| 04-context-warm | COMPLETE | 1 | all 3 children COMPLETE; F7 landed (463b4faf) gated on foundation |

## Integration Test Plan

```bash
cd .hermes/worktrees/voice-tts-stt-optimizations
gofmt -l internal cmd   # expect empty
go test -p 2 ./internal/config/... ./internal/daemon/... ./internal/voice/... ./internal/voicechild/...
go test -p 2 ./...
make e2e-fast-area AREA=voice_clips
make e2e-fast-area AREA=context_warm
make graphs-check
```
Expected: all green; graphs fresh; no new orphan bus topics; gate tests
unmodified by implementers.

## Test Gate

Root e2e leaf `00T-root-e2e.md` covers cross-branch contract behavior:
all new keys fields absent -> byte-identical wire shape; all new config
fields absent -> legacy defaults. Branch coverage matrices live in each
branch orchestrator. Every row must reach VALIDATED before that branch's
implementation dispatch.

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| R1 | Keys/additive config defaults | 01-foundation/01 | `TestKeysFrameLegacyDefaults` | root e2e key wire | VALIDATED |
| R2 | All F1-F10 addressed | findings index | branch matrices | branch e2e | VALIDATED |

Delta protocol: update the LESSER artifact; coverage converges upward;
record every resolution here with date, feature ID, direction, reason.

## Open Questions

- Q1: F1 direction - consume the Smart Turn verdict (latency win, needs
  false-cutoff fixtures) or remove the dead analyzer (resource win, zero
  behavior change)? RECOMMENDATION: consume behind opt-in mode 2; keep
  mode 1 as immediate kill-switch. Decide before Phase 1.
- Q2: F2 whisper persistent server requires a whisper.cpp binary with
  `--server` support. Verify the installed binary supports it before
  dispatching F2; otherwise mark BLOCKED and ship the exec-mode fast path.
- Q3: F4 Deepgram endpoint false-cutoff risk needs hesitation/backchannel
  fixtures before enabling. Keep default 0 (timer only).

## Notes

- All latency gains are UNMEASURED. Mechanism and direction only.
- Root worktree branch: plan/voice-tts-stt-optimizations.
