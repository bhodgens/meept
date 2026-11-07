# Voice Tier A + Qwen3-TTS + end-to-end wiring - master

> Root orchestrator. Dispatches leaves, reviews in-session (main model),
> commits after review. Builds on the completed 20261102-voice-sidecar tree
> (all 16 leaves landed). Spec base: docs/workflows/voice-sidecar.md rev 2
> as-built + the gap analysis from the 2026-11-06 session (pump/keys/barge-in
> wiring gaps, verified at file:line below).

## Meta

- Tree: docs/plans/20261106-voice-tierA-qwen3-tts/ (flat, repo convention)
- Execution backend: direct dispatch (delegate_task), isolation worktree
  `.hermes/worktrees/voice-tier-a`, branch `plan/voice-tier-a`
- Authoring date: 2026-11-06. Reconcile against the working diff before
  dispatch (changelog drift check); a parallel plan tree
  (local-playback sidecar mode) may be touching sibling files.

## Goal

Make the voice sidecar actually conversational end to end:

1. The model hears the caller: daemon->child PCM feeds the real Deepgram
   STT adapter, transcripts become prompt frames (finals start employee
   turns, partials feed preview).
2. Barge-in works: caller speech during playback sends the interrupt
   frame; the daemon cancels the in-flight turn; playback stops per call;
   the caller's finished utterance starts a fresh turn.
3. The model speaks with local Qwen3-TTS over streaming PCM (Tier A: the
   text seam, supervisor gate, and talk loop stay exactly as built; only
   the TTS provider behind the existing interface changes), while cloud
   OpenAI TTS remains selectable per config.

Out of scope (explicit): speech-to-speech (Tier B), the parallel plan's
local-playback sidecar mode, ONNX/jargo VAD (replaced by the STT-partial
barge-in trigger, see D1), any change to C2 sockets, splice, registry,
effects, or dial paths.

## Architecture

Unchanged cascade: caller audio -> C2 .in socket -> child STT (Deepgram)
-> prompt frames -> daemon talk loop (supervisor gate, sentence split)
-> say frames -> child TTS -> C2 .out socket -> splice -> Telnyx.

Three seams change:

- S1 Keys/config wire: the daemon currently never sends the C1 keys
  frame, and the child never constructs real providers from live config.
  The keys frame gains vendor/model/base_url/voice fields; the daemon
  emits it from VoiceConfig + broker secrets right after setup.
- S2 Child pipeline: pumpAudioIn feeds p.stt.Stream; a transcript
  goroutine emits prompts and drives the barge-in latch.
- S3 TTS adapter: one OpenAI-compatible streaming adapter serves both
  `openai` (keyed, api.openai.com) and `qwen3` (keyless, configurable
  base_url, raw PCM stream at 24 kHz resampled to the negotiated rate).

## Design Decisions (pre-authoring Q&A, resolved)

- D1 Barge-in detection: STT partial transcripts are the trigger, not a
  separate VAD. Deepgram already streams interims; the first non-empty
  partial while `p.speaking` trips `noteBargeIn` -> `emitInterrupt`.
  This removes the ONNX hard dependency for Tier A. DEVIATION from spec
  rev 2 ("a phone call without barge-in is refused", cmd/meept-voice
  ONNX probe): the ONNX probe stays non-fatal; jargo VAD remains a
  later upgrade. Rationale: zero new runtime deps, fully unit-testable.
- D2 Keyless local TTS: vendor `qwen3` accepts an empty key. Cloud
  vendors keep `ErrMissingTTSKey`/`ErrMissingSTTKey` (a missing key
  fails the CALL, not the child - unchanged contract).
- D3 Qwen3-TTS serving: out of repo scope. The adapter targets any
  OpenAI-compatible `/v1/audio/speech` streaming server (vLLM-Omni
  `vllm serve ... --omni`, or a community FastAPI/MLX server). The
  daemon config carries `tts_base_url`; the user deploys the server.
  Output contract used by the adapter: `stream=true` +
  `response_format="pcm"` = raw s16le mono 24 kHz chunks.
- D4 Vendor/model transport: the C1 keys frame carries vendor+model+
  base_url+voice (daemon->child unix socket, in-memory, never argv/env).
  Empty fields = today's hardcoded defaults, so old pairs stay
  compatible.
- D5 Factory signatures change (breaking, single repo, no external
  callers): factories take the full ProviderConfig. `sampleRate` moves
  into ProviderConfig (field exists; passed by constructProviders).

## Interface Contracts (frozen; do not paraphrase in leaves)

### C1: Extended keys frame (raw JSON line on the C1 text socket)

```
{"call_id":"<ccid>","event":"keys",
 "stt_key":"<deepgram key or empty>",
 "tts_key":"<openai key or empty>",
 "stt_vendor":"deepgram","stt_model":"","stt_base_url":"",
 "tts_vendor":"qwen3","tts_model":"Qwen/Qwen3-TTS-12Hz-0.6B-CustomVoice",
 "tts_base_url":"http://127.0.0.1:8880","tts_voice":"vivian"}
// Owner: 02-keys-frame-extension.md (child decode/store)
// Consumers: 03-daemon-keys-emission.md (producer), 04-qwen3-tts-adapter.md
// Rules: all new fields OPTIONAL (absent = ""); unknown fields ignored
// by old children; event stays the raw string "keys" (not in
// voiceframe.Frame - decoded raw child-side, exactly as today).
```

### C2: ProviderConfig (internal/voicechild/providers/providers.go)

```
type ProviderConfig struct {
    Vendor     string // "deepgram" (STT) | "openai" | "qwen3" (TTS)
    Model      string // empty = vendor default
    BaseURL    string // empty = vendor default (openai: api.openai.com)
    Voice      string // TTS voice name; empty = vendor default
    SampleRate int    // negotiated rate (s16le mono PCM)
    Language   string // BCP-47 tag; empty = vendor default
}
// Owner: 01-config.md defines fields, 02 carries them, 04 consumes.
// Keyless rule: TTS Vendor "qwen3" MUST construct and speak with key "".
```

### C3: Factory signatures (internal/voicechild/providers_wiring.go)

```
type STTFactory func(cfg ProviderConfig, key string) (STTProvider, error)
type TTSFactory func(cfg ProviderConfig, key string) (TTSProvider, error)
// constructProviders passes p's config (stored by applyKeys) + key.
// cmd/meept-voice/main.go factories dispatch on cfg.Vendor.
```

### C4: Daemon keys emission point + secret names

```
When: VoiceWiring.routeFrame EventSetup, AFTER the talk loop registers
      (same goroutine, once per call; idempotent setups do not re-send).
How:  TextServer.Send of the raw JSON line (C1 shape). TextServer.Send
      is mutex-serialized (internal/voice/socket.go:175-198).
Secrets: Broker.Value("deepgram_api_key"), Broker.Value("openai_api_key")
      - both new audit-secrets-value-callers.py allowlist rows, same
      commit. Missing cloud key = empty string sent (child factory fails
      the call; child and other calls stay up). qwen3 sends "".
Config->wire mapping (01 defines fields):
      voice.stt_provider->stt_vendor, voice.stt_model->stt_model,
      voice.tts_provider->tts_vendor, voice.tts_model->tts_model,
      voice.tts_base_url->tts_base_url, voice.tts_voice->tts_voice.
```

### C5: Child transcript + barge-in trigger

```
 pumpAudioIn feeds a PCM chan into p.stt.Stream (called once, after
 providers construct). One goroutine consumes transcripts:
   - final transcript  -> emitPrompt(ctx, ccid, text, true)  (starts turn)
   - partial transcript-> emitPrompt(ctx, ccid, text, false) (preview)
                         AND, if p.noteBargeIn() returns true,
                         emitInterrupt(ctx, ccid) - exactly once per
                         barge-in (existing latch in noteBargeIn).
   - empty-text transcripts (Deepgram silence results) are dropped.
 Order: noteBargeIn BEFORE emitInterrupt; emitPrompt AFTER both.
 Teardown: pipeline.close cancels ctx; Stream session dies with it.
// Owner: 05-stt-pipeline-wiring.md (pump+prompt), 06-bargein-trigger.md
// (latch wiring). Consumers: existing talk loop (unchanged).
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-gate-tests.md | leaf (tests) | none | 40K | - |
| 01 | 01-config.md | leaf | none | 35K | A |
| 02 | 02-keys-frame-extension.md | leaf | none | 45K | A |
| 03 | 03-daemon-keys-emission.md | leaf | 01 (fields), 02 (wire shape) | 45K | B |
| 04 | 04-qwen3-tts-adapter.md | leaf | none (C2 pinned here) | 45K | A |
| 05 | 05-stt-pipeline-wiring.md | leaf | 02 | 45K | C |
| 06 | 06-bargein-trigger.md | leaf | 05 | 35K | C |
| 07 | 07-e2e-docs.md | leaf | 01-06 | 35K | D |

Group A dispatches together (00T first, gate validated, then A).
03 needs 01+02 merged for its verify run; 05 needs 02; 06 needs 05;
07 runs last against the integrated worktree.

## Dispatch Protocol

Full recipe: skill `hierarchical-plan-execution`. Per canon: test gate
before implementation; orchestrator (main model) reviews in-session,
never delegates review; implementation agents never commit.

### Phase 0: Test Gate

1. Dispatch 00T-gate-tests.md as one subagent: "Write tests ONLY, no
   implementation. Tests must fail RED for missing-implementation
   reasons. Do NOT commit."
2. Validate returned tests in-session against the Test Gate matrix
   below; apply the delta protocol (update the LESSER artifact).
3. Commit gate tests (orchestrator, in the worktree). Mark the tree
   TESTS_VALIDATED. Implementation may dispatch.

### Phase 1: Concurrency Group A (01, 02, 04 in parallel)

Dispatch via delegate_task. Context per dispatch: the leaf text IN FULL,
the contracts it consumes (C1-C5 as relevant), the coding conventions
below, and: "Do NOT commit. Do NOT run git add. Do NOT use read_file on
existing sources (search_files/terminal cat only). Write once, never
read back. Work in <worktree absolute path>."

### Phase 2: Review and commit each child

In-session review per the Review Checklist. Verify with the leaf's
exact commands in the worktree. Pass: stage ONLY the leaf's files
(`git commit -- <paths>` - this repo's index is sibling-racy), message
`feat(voice): <leaf subject> (20261106-voice-tierA leaf NN)`, mark
REVIEWED. Gaps: re-dispatch with findings, max 3 cycles, then escalate.

### Phase 3: Groups B, C, D

Sequential by dependency: 03 after 01+02 commit; 05 after 02; 06 after
05; 07 last. Same dispatch/review/commit loop.

### Phase 4: Integration review

After 07 REVIEWED, in the worktree:

1. `go build ./... && go vet ./internal/voicechild/... ./internal/voice/... ./internal/daemon/... ./cmd/meept-voice/`
2. `go test -p 2 ./internal/voicechild/... ./internal/voice/... ./internal/daemon/...`
3. `make e2e-fast-area AREA=voice-bargein` and `AREA=voice-lifecycle`
4. `python3 scripts/audit-secrets-value-callers.py` (allowlist rows green)
5. `make graphs && make graphs-check` (only if bus/RPC/HTTP/WS surfaces
   changed - expected: no)
6. Full `make test` if any step above is red.

Merge to main at top-level completion only; remove the worktree.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (C1-C5 signatures, field names, wire shapes)
- [ ] All specified files created/modified at exact paths
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification
- [ ] Tests written and passing (TDD followed)
- [ ] Secrets rules: no key in logs/env/argv; LogValue redaction intact;
      new Broker.Value callers are allowlisted in the same change
- [ ] Code follows project conventions; no typed-nil interface boxing
- [ ] No scope creep; no debug artifacts, TODOs, placeholders
- [ ] No line-number corruption (`     N|` prefixes) in source files

Output: APPROVED or a list of specific gaps with file + line references.

## Coding Conventions

- **Language:** Go (module github.com/caimlas/meept); stdlib-first;
  internal/voiceframe stays dependency-free.
- **Naming:** exported only where a seam requires it; receiver letters
  match package (c *Child, p *pipeline, t *TalkLoop, s *runSplice).
- **Errors:** wrap with %w and a package-prefix context; named sentinel
  errors (ErrMissingTTSKey pattern) for leaf-documented failure modes.
- **Mutexes:** never hold across I/O (mutexio analyzer enforces);
  playMu guards pipeline playback state only.
- **Secrets:** key material only via the C1 keys frame or Broker.Value
  in-daemon; redacting String/LogValue on any carrying struct; NEVER
  log, never argv/env.
- **Tests:** table-driven where shape varies; fake vendors via
  httptest Transport swap (never live vendors in CI); unit tests beside
  the code; new behavioral coverage rides the existing voice e2e suites.
- **Comments:** explain why + contract refs (C1/C2, leaf/audit IDs),
  matching house style in internal/voice.
- **Formatting:** gofmt; run `gofmt -l` on touched files before
  reporting.

## Requirement Traceability

| Req | Source (user words) | Plan element | Verified by |
|-----|---------------------|--------------|-------------|
| R1 | "use local Qwen3-TTS with streaming" | 04 adapter (C2 keyless, stream pcm 24k->negotiated rate), 01 config fields, 03 emission | providers tts tests (fake streaming server, incremental first-chunk assert) |
| R2 | "fully wire everything end-to-end, close those gaps" | 02 keys wire, 03 daemon emission, 05 pump->STT->prompt, 06 barge-in trigger | gate tests: wiring_test (pump feeds fake STT, prompt frames emitted), keys tests both sides |
| R3 | "if the model starts talking, the person responds, it pauses and responds based on that" (prior turn) | 06 trigger -> existing handleInterrupt path (talkloop.go:553) -> clear -> new prompt spawns fresh turn | child tests (partial-during-speak emits interrupt once); voice-bargein e2e |
| R4 | "parallel plan implementing additional sidecar mode (local playback)" | No leaf assumes Telnyx output; all child changes ride the C2 .out socket + C1 frames both modes share | integration review: leaf diffs touch no splice/telnyx files except docs |
| R5 | Tier A shape: text seam + supervisor gate preserved | No leaf modifies talkloop sentence flow or supervisor; keys/emission are additive | review checklist: talkloop.go untouched (except no changes expected) |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-gate-tests | PENDING | 0 | |
| 01-config | PENDING | 0 | |
| 02-keys-frame-extension | PENDING | 0 | |
| 03-daemon-keys-emission | PENDING | 0 | |
| 04-qwen3-tts-adapter | PENDING | 0 | |
| 05-stt-pipeline-wiring | PENDING | 0 | |
| 06-bargein-trigger | PENDING | 0 | |
| 07-e2e-docs | PENDING | 0 | |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED |
IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. Unit + package: `go test -p 2 ./internal/voicechild/... ./internal/voice/... ./internal/daemon/...`
2. Secrets audit: `python3 scripts/audit-secrets-value-callers.py`
3. e2e: `make e2e-fast-area AREA=voice-bargein`, `AREA=voice-lifecycle`,
   `AREA=voice-concurrency`
4. Live smoke (local, manual, user-gated): config `tts_provider:"qwen3"`,
   `tts_base_url` at the user's Qwen3-TTS server, place a call, verify
   first audio < ~1 s after say, barge-in pauses playback, reply
   reflects the interruption. Recorded in docs/workflows/voice-sidecar.md
   as-built notes by leaf 07.

## Test Gate

Test leaves: 00T-gate-tests.md - dispatched in Phase 0, validated by the
control agent (orchestrator, in-session).

| # | Feature (from plan) | Source | Unit tests | E2E tests | Status |
|---|---------------------|--------|-----------|-----------|--------|
| F1 | Extended keys frame decode/store | C1, leaf 02 | providers_wiring_test (vendor/base_url/voice fields, defaults) | n/a (wire is socket-level, covered by unit) | PENDING |
| F2 | Daemon keys emission from config+broker | C4, leaf 03 | daemon voice_wiring keys test (fake broker + recording TextServer) | voice-lifecycle (keys line observed) | PENDING |
| F3 | qwen3 streaming TTS adapter | C2, leaf 04 | providers tts_qwen3_test (httptest streaming PCM, keyless, resample) | n/a (vendor faked in CI) | PENDING |
| F4 | pump->STT->prompt wiring | C5, leaf 05 | child_test/pipeline test (fake STT transcripts -> emitted prompt frames, partials preview-only) | voice-lifecycle | PENDING |
| F5 | Barge-in via partial during playback | C5, leaf 06 | pipeline test (partial while speaking -> interrupt frame once; not while idle) | voice-bargein | PENDING |
| F6 | Config fields for vendor/model/base_url/voice | leaf 01 | config schema/template test | n/a | PENDING |

Statuses: PENDING | DELTA (resolution in progress) | VALIDATED

Delta log: (empty at authoring)

## Open Questions

- Qwen3-TTS server endpoint details (exact voice names, language field
  semantics) depend on which server the user deploys (vLLM-Omni vs
  FastAPI/MLX). The adapter treats them as plain OpenAI-compatible
  /v1/audio/speech; if the chosen server needs an extra body field,
  extend openAICompatibleTTSRequest in leaf 04's file as a dated
  addendum, not a contract change.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda. Never edit a dispatched leaf document mid-execution.

## Notes

- Parallel-plan coordination (R4): the local-playback tree adds a second
  egress mode. This tree's child-side changes (keys fields, STT pump,
  TTS adapter, barge-in trigger) are mode-agnostic - both modes consume
  the same C1 frames and C2 sockets. If that tree lands first, reconcile
  pipeline.go playGen/speaking state against its playback changes before
  dispatching 05/06 (changelog drift check, Pitfall 22).
- The daemon currently NEVER sends the keys frame (verified: no producer
  of `stt_key` exists outside internal/voicechild; grep 2026-11-06), so
  providers never construct in production and every call runs on stub
  STT/TTS today. Leaf 03 is the gap closer for that half; leaf 05 for
  the pump half.
- Anchors below were grep-verified 2026-11-06. If the working diff moved
  them, trust the symbol names, re-grep, and ride a dated addendum.
