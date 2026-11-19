# Voice sidecar bughunt: local interactive response (2026-11-18)

Repo: `meept`, branch `main`, base HEAD `8b0856a2`.
Scope: audio/voice sidecar (`cmd/meept-voice`, `internal/voicechild`,
`internal/voiceframe`, daemon local leg `internal/voice`, wiring
`internal/daemon/voice_wiring.go`). Focus: bugs that degrade interactive
voice response for LOCAL use.

## Method

1. Baseline gates: `go build` of all voice packages; `go test -p 2 -count=1`
   on voiceframe, voicechild, providers, voice, clipstore, eval, telnyx. All green.
2. Four parallel READ-ONLY auditors (pipeline, daemon local leg, providers/codec,
   supervisor/wiring) with disjoint file ownership. No auditor edited files.
3. Parent source-verified every finding before any edit. Auditor reports are
   self-reports; each finding was re-read at `file:line` in the real tree.
4. Fixes landed with regression tests. Full gate matrix + `-race` re-run after.

## Findings and disposition

| # | Severity | Finding (file:line) | Verdict | Fix |
|---|----------|---------------------|---------|-----|
| F1 | HIGH | Piper native rate read from wrong sidecar path — `internal/voicechild/providers/tts_piper.go:110` | CONFIRMED | FIXED: read `<model>.onnx.json` (canonical, per docs/workflows/tts.md:113,207); keep stripped-`.onnx` `voice.json` as fallback. Test helper now writes canonical name; added `TestPiperModelRateSidecarNames`. |
| F2 | HIGH | Long replies silently truncated when supervisor off (`ConnectSentences`) — `internal/voice/talkloop.go:1667-1672` | CONFIRMED | FIXED: `splitRunesCapped` emits all reply text in bounded rune chunks, ordered, last chunk final; fast-first-audio split preserved. Added `checkStale` helper. Test `TestTalkLoopConnectSentencesLongReplyNotTruncated`. |
| F3 | HIGH | Configured supervisor gate silently fails open with no log WARN — `internal/daemon/voice_wiring.go:610-612` | CONFIRMED (severity adjusted) | FIXED (safe form): added WARN when `SupervisorEnabled=true` and no chatter. Did NOT set `Enabled:true` with nil chatter — that would panic at `supervisor.go:206 s.chatter.Ask`; documented allow-all posture kept. |
| F4 | HIGH | Synchronous ONNX analyzer build on the single C1 dispatch loop delays `setup` + blocks other calls — `internal/voicechild/open.go:49-81`, `child.go:376-377` | CONFIRMED | FIXED: `handleOpen` now sends `setup` first, then starts both ONNX factories on goroutines via `pipeline.initAnalyzersAsync`. `close()` cancels then waits on `analyzerInit` so no session leaks on fast teardown; per-analyzer install-once guards prevent late re-arm. Pin: `TestOpenSetupNotBlockedBySlowAnalyzer`. Invariant documented in `internal/voice/AGENTS.md`. |
| F5 | MED | Per-call ONNX session, no reuse — `cmd/meept-voice/main.go:110-125` | CONFIRMED (cost certain, magnitude machine-dependent) | NOT FIXED — orthogonal to F4 (scheduling vs session reuse); jargo v0.6.1 exposes `vad.NewSileroModel()` shared session. Fold into a separate reuse change. |
| F6 | MED | Non-English local legs: ASCII-only echo provenance — `internal/voicechild/echo_window.go:108-132`, `internal/util/fuzzy/fuzzy.go:31-44` | CONFIRMED (requires non-ASCII text) | NOT FIXED — needs Unicode-aware normalization in both echo provenance and the shared fuzzy scorer; cross-cutting change beyond the interactive-response hot path. Recorded as next step. |
| F7 | MED | Filler TOCTOU: filler can speak after answer write — `internal/voice/talkloop.go:1821-1835` | CONFIRMED (narrow interleaving window) | FIXED: latch `utteranceOut`/stop-filler BEFORE the synchronous `say` write; roll back on send failure. |
| F8 | MED | Dead raw-transcript swap branch — `internal/daemon/voice_wiring.go:1345-1347` | CONFIRMED (evidence quality, not latency) | NOT FIXED — out of scope (operator evidence, not interactive response); the dead code is inverted relative to its comment and needs a product decision on raw-fidelity semantics. |
| F9 | MED | Supervisor `Stats()` has no live caller — `internal/voice/supervisor.go:341` | CONFIRMED | NOT FIXED — observability wiring needs a product surface decision (`voice.local.status` vs periodic log). |
| F10 | MED | Mixed local/UTC clocks in telemetry vs transcript display — `internal/voice/events.go:194`, `internal/voice/binding.go:298+`, `cmd/meept/voice.go:1179+` | CONFIRMED (impact depends on daemon TZ) | NOT FIXED — display-side only; phase `Ms` values use one clock. Recorded as next step. |
| F11 | MED | First response can block up to hardcoded 30 s on cold child start — `voice_wiring.go:415`, `childproc.go:312` | CONFIRMED | NOT FIXED — needs config knob (`voice.child_start_timeout_ms`) + boot warm; product decision on default. |
| F12 | LOW | Duplicate C1 interrupt latch/gate-bump per barge-in — `internal/voice/localsplice.go:1133-1135,1157-1173` | CONFIRMED (conservative effects, duplicate log) | FIXED: `HandleInterrupt` skips latch/bump/drain when `LatchBargeIn` already fired for the same barge-in, while still sending clear + CancelDraft. |
| F13 | LOW | qwen3 pcm-stream truncated declared frame treated as clean EOF — `internal/voicechild/providers/pcmstream.go:77-85` | CONFIRMED (edge case) | FIXED: return `io.ErrUnexpectedEOF` when a declared frame's payload is cut short. Test `TestPCMStreamFrameReaderRejectsTruncatedPayload`. |
| F14 | LOW | Punctuated discourse tokens bypass core() normalization — `internal/voicechild/normalize.go:164,176,184` | CONFIRMED (inconsistency; low audible impact) | NOT FIXED — prompt-quality tweak, not interactive-response path. |
| F15 | LOW | `said` events stamped at send, before playback — `internal/voice/events.go:60-64` | CONFIRMED, intentional + CLI-mitigated | NOT FIXED — shipping CLI has `speakerClock` `played` marker; raw RPC consumers need a new `played` telemetry kind. |
| X1 | (rejected) | Resampler discards delayed tail on EOF — `internal/voicechild/providers/resample.go:126-133` | REFUTED | Reverted. jargo v0.6.x already emits in-pace complete audio; `Flush()` adds 58 frames of ZERO ringing, not lost speech. Existing pin `TestResamplingReaderOddReadsPreserveAlignment` enforces exactly 48000 frames for 3 s. My trial flush broke that pin; the change and its test were reverted before final gates. |

## Files changed (this session, 8 files)

- `internal/voicechild/providers/tts_piper.go` + `tts_piper_test.go` (F1)
- `internal/voicechild/providers/pcmstream.go` + `pcmstream_test.go` (F13)
- `internal/voice/talkloop.go` + `talkloop_test.go` (F2, F7)
- `internal/voice/localsplice.go` (F12)
- `internal/daemon/voice_wiring.go` (F3)

NOT touched: 11 pre-existing modified/untracked files owned by a sibling agent
(TUI/Flutter/ws_schema: `internal/tui/*`, `internal/comm/http/ws_schema_drift_test.go`,
`schemas/ws_events.schema.json`, `ui/flutter_ui/lib/*`). They remain modified in
the working tree, unrelated to this work.

## Verification (real command output)

- Baseline: `go test -p 2 -count=1 ./internal/voiceframe/... ./internal/voicechild/... ./internal/voice/...`
  -> exit 0, 7 packages ok (voiceframe, voicechild, providers, voice, clipstore, eval, telnyx).
- Post-fix full matrix: `go test -p 2 -count=1 ./cmd/meept-voice/... ./internal/voiceframe/... ./internal/voicechild/... ./internal/voice/... ./internal/daemon/...`
  -> exit 0, 8 packages ok (+ daemon 37.6s).
- Race: `go test -p 2 -race -count=1 -run 'TestTalkLoopConnect|TestPiper|TestPCMStream|BargeIn|Interrupt|Latch|Filler|Silence' ./internal/voice/ ./internal/voicechild/providers/`
  -> exit 0, both packages ok.
- `gofmt -l` on all 8 changed files -> empty (clean).
- Build: `go build ./internal/voice/ ./internal/daemon/ ./cmd/meept-voice/` -> OK.

## Ledger: what was NOT done and why

1. ONNX session reuse across calls is now a SEPARATE follow-up (F5): use
   `vad.NewSileroModel()` shared session so repeat local calls do not pay
   per-call session creation. F4 (dispatch-loop blocking) is fixed.
2. F6 (Unicode echo provenance): requires changing shared fuzzy normalization;
   affects non-English local legs only, so deferred from the hot-path fix wave.
3. F8/F9/F10/F11/F14/F15: evidence quality, observability, display clock,
   config-default, prompt-quality, and intentional-telemetry items. Each needs
   a surface or product decision; none silently dropped.
4. No live call was placed. Per repo practice the audio gate is the user's
   listen test; synthetic gates (unit + race) only prove code contracts, not
   audible quality.
5. No commit was made. Per repo rule (small per-concern commits, explicit
   paths) the user decides commit timing; the sibling's dirty files must not
   be swept into a commit.

## Next steps (ranked)

1. Run a live `meept voice local` listen gate on the 5 fixed behaviors,
   especially piper pitch (F1), long-reply completeness (F2), and fast
   first response on a cold child (F4).
2. For non-English local use, land Unicode-aware echo provenance (F6) with
   `fuzzy.NormalizeToken` parity.
3. Choose surfaces for F8-F11 (raw transcript, supervisor stats, UTC display,
   configurable child start timeout) and land as separate concern commits.
4. Run a live `meept voice local` listen gate on the 5 fixed behaviors,
   especially piper pitch (F1) and long-reply completeness (F2).
