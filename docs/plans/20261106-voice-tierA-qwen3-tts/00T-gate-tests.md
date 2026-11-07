# Voice Tier A gate tests - Test Leaf (00T)

> **For the implementing agent:** You are the test author for this leaf.
> Write TESTS ONLY. No implementation. Every test must fail RED today for
> a missing-implementation reason (compile failure of a missing symbol
> counts as RED only if the leaf spec names the exact symbol; otherwise
> write tests that compile against existing symbols and assert the NEW
> behavior). Do NOT delete, skip, or weaken any existing test. Do NOT
> commit - the orchestrator handles all git operations after review. Do
> NOT use read_file on existing source files - explore with search_files
> or terminal cat. Write each file once; do not read it back.

## Meta

- **Parent:** ../master.md
- **Scope:** The full unit test set for features F1-F6, written before
  implementation, validated against the master Test Gate matrix.
- **Dependencies:** none
- **Estimated Context:** 40K
- **Concurrency Group:** - (Phase 0, before all implementation)

## Goal

Encode the acceptance criteria of contracts C1-C5 as failing tests so
implementation leaves 01-06 make them pass without reinterpretation.
Tests live in the packages under test (Go convention: same dir, _test.go).

## Context

- internal/voicechild/providers/ - provider adapters; fakes use an
  httptest server + Transport swap (see providers/tts_test.go for the
  established pattern). Never hit live vendors in CI.
- internal/voicechild/ - child pipeline; tests drive frames through
  Child.handleLine paths (see child_test.go, providers_wiring_test.go).
- internal/daemon/ - VoiceWiring; test with a fake secrets.Broker and a
  recording TextServer seam (see voice_wiring.go for TextServer
  construction; if TextServer is not seam-injectable, assert via a
  handler-capture seam documented in the test's comment and report the
  deviation).
- RED rule: a test that cannot compile because the contract type does
  not exist yet (e.g. ProviderConfig.BaseURL) is acceptable RED - name
  the symbol in the failure comment so the orchestrator can classify it.

## Verified Anchors

- internal/voicechild/providers/providers.go:41-46 - ProviderConfig fields today (Vendor, Model, SampleRate, Language only - BaseURL/Voice are NEW)
- internal/voicechild/providers_wiring.go:15-18 - STTFactory/TTSFactory signatures today (key, sampleRate) - changing per C3
- internal/voicechild/providers_wiring.go:54-75 - keysWire struct today (stt_key/tts_key only) - C1 adds fields
- internal/voicechild/pipeline.go:122-144 - pumpAudioIn drops PCM today
- internal/voicechild/pipeline.go:259-267 - noteBargeIn latch (exists; needs a caller)
- internal/voice/daemon (voice_wiring.go:384-400) - routeFrame EventSetup path - keys emission lands here
- internal/config/schema.go:5233-5259 - VoiceConfig fields today - 01 adds STTModel, TTSModel, TTSBaseURL, TTSVoice

## Interface Contracts (From Parent)

### What This Leaf Exposes

New test files + test functions only (no production symbols):

```
internal/voicechild/providers/keys_test.go     (extend providers_wiring_test.go instead if cleaner)
internal/voicechild/providers/tts_qwen3_test.go
internal/voicechild/pipeline_stt_test.go
internal/daemon/voice_keys_emit_test.go
internal/config/voice_fields_test.go
```

### What This Leaf Consumes

- C1-C5 contract shapes exactly as frozen in ../master.md (test against
  THOSE; on any delta between contract and reality at validation time,
  report DELTA - do not silently adjust the test).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | F1 keys decode: extended keys frame stores vendor/model/base_url/voice on the pipeline; empty fields = defaults | Test: internal/voicechild/providers_wiring_test.go (extend) | `go test ./internal/voicechild/ -run TestKeysFrameExtendedFields` | keysWire has no new fields; pipeline has no config store | sendRaw keys line with stt_vendor/tts_vendor/tts_base_url/tts_voice/tts_model; then factory receives ProviderConfig with those values; absent fields arrive as "" | Given child with recording factories; When keys frame with all fields; Then cfg fields match exactly |
| T2 | F2 daemon emission: EventSetup triggers exactly one keys line carrying config mapping + broker keys; empty cloud keys send "" not skip | Test: internal/daemon/voice_keys_emit_test.go | `go test ./internal/daemon/ -run TestVoiceKeysEmission` | routeFrame never sends keys | setup frame -> recording sender sees one line with event "keys", tts_vendor from config, stt_key from fake broker; duplicate setup -> still one | Given VoiceWiring with fake broker + config; When routeFrame(setup) twice; Then exactly one keys line, fields per C4 mapping |
| T3 | F3 qwen3 adapter: keyless construction, streaming first-chunk, resample to negotiated rate | Test: internal/voicechild/providers/tts_qwen3_test.go | `go test ./internal/voicechild/providers/ -run TestQwen3TTS` | NewTTS rejects unknown vendor "qwen3" | httptest server streams PCM slowly; Speak returns reader yielding first chunk before body ends; output bytes resampled 24000->16000 (length ratio ~1.5, assert within tolerance); NewTTS(vendor=qwen3, key="") succeeds; vendor=openai with key="" still fails ErrMissingTTSKey | Given fake OpenAI-compatible /v1/audio/speech streaming s16le 24k; When Speak("hi") on 16k-negotiated adapter; Then incremental reads succeed and rate matches |
| T4 | F4 pump wiring: audio read from .in feeds STT; final transcript emits prompt last:true; partial emits prompt last:false; empty text dropped | Test: internal/voicechild/pipeline_stt_test.go | `go test ./internal/voicechild/ -run TestPipelineSTTWiring` | pumpAudioIn drops chunks; Stream never called | fake STT (records chunks, emits scripted Transcript{Final}) -> written bytes appear at fake STT; prompts appear on text socket with correct last flag; empty-text transcript -> no frame | Given pipeline with fake STT + real sockets; When N bytes written to .in and transcripts scripted; Then STT got the bytes, socket got the prompts |
| T5 | F5 barge-in trigger: partial transcript while speaking -> interrupt frame emitted exactly once; no interrupt when not speaking; latch resets after playback restart | Test: internal/voicechild/pipeline_stt_test.go (same file, separate funcs) | `go test ./internal/voicechild/ -run TestBargeInViaPartial` | noteBargeIn has no transcript caller | Given speaking pipeline; When partial arrives; Then one interrupt frame; second partial -> no second interrupt; after say restarts playback + partial -> interrupt again | see Then column |
| T6 | F6 config fields: VoiceConfig gains STTModel, TTSModel, TTSBaseURL, TTSVoice with toml+json tags; template documents them | Test: internal/config/voice_fields_test.go | `go test ./internal/config/ -run TestVoiceTTSFields` | fields absent | struct has 4 new string fields with tags `stt_model`, `tts_model`, `tts_base_url`, `tts_voice`; defaults "" | Given VoiceConfig{}; When fields set + marshaled; Then tags round-trip |

Expand each row into failing tests following the house test style of the
target package. Compile-level RED is acceptable for T1 (new fields),
T3 (new vendor), T6 (new fields) - annotate the missing symbol name.

## Self-Verification Checklist

- [ ] All six features have at least one failing test
- [ ] Every test compiles except where a contract symbol is genuinely absent (each such case named in a comment)
- [ ] No live vendor calls; fakes only
- [ ] No weakening of existing tests
- [ ] gofmt clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** test files created/extended
2. **Verify commands + outputs:** the six commands, last lines each
3. **RED classification:** per test: compiles-and-fails vs missing-symbol
4. **Deviations:** any seam that required a deviation (e.g. TextServer injection)

## Review Checklist (For Review Agent)

- [ ] Tests assert the C1-C5 contract values exactly (field names, wire keys, last-flag semantics, once-per-barge-in)
- [ ] No production code changed (`git diff --stat` shows only _test.go)
- [ ] RED reasons classified correctly
- [ ] Fakes follow house pattern (httptest/Transport swap, recording senders)

Output: APPROVED or specific gaps with file + line references.

## Notes

- If routeFrame's TextServer is a concrete field (it is:
  voice_wiring.go:301), T2 may capture frames by pointing TextServer's
  socket path into a temp dir with a fake child connection - see
  internal/voice/socket_test.go for the pattern.
- Keep total test additions focused: ~150-250 lines per package max.
