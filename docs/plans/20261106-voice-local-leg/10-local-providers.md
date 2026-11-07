# Local providers: whisper STT + piper TTS - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. Do
> NOT commit — the orchestrator handles git. Explore with search_files or
> terminal cat, never read_file. Do NOT read back files you just wrote.

## Meta

- **Parent:** master.md
- **Scope:** Two new child-side provider adapters speaking the existing
  voicechild.STTProvider / TTSProvider interfaces: whisper.cpp (STT) and
  piper (TTS), both as SUBPROCESSES.
- **Dependencies:** tierA C2 (providers.ProviderConfig already
  carries Vendor/Model/BaseURL/Voice/SampleRate - this leaf consumes it
  unchanged, no schema edits)
- **Estimated Context:** 60K
- **Concurrency Group:** B
- **Files this leaf owns:** internal/voicechild/providers/stt_whisper.go,
  internal/voicechild/providers/tts_piper.go, providers.go (switch rows
  only), stt_whisper_test.go, tts_piper_test.go

## Goal

voice.stt_provider="whisper" and voice.tts_provider="piper" work in the
child with no cloud keys. These make the local leg fully offline and give
the phone leg a self-hosted pair too. The local leg's whole test story
(leaf 11) depends on adapters that run hermetically - so BOTH adapters must
run against a fake binary in tests (a tiny shell/Go helper script that
stands in for whisper-cli / piper), never requiring the real binaries in
CI.

## Context

Key files:
- internal/voicechild/pipeline.go - the two interfaces (Stream chan of
  Transcript; Speak(text) -> io.Reader of s16le PCM at target rate).
- internal/voicechild/providers/providers.go - NewSTT/NewTTS vendor switch,
  ProviderConfig, the no-key rule (TestNoKeyLogs pins logging to
  vendor+model).
- internal/stt/whisper.go (daemon package, DIFFERENT interface - read it
  only for the whisper-cli subprocess flags, do NOT import internal/stt
  from the child: layering; copy the flag knowledge).
- docs/workflows/tts.md - piper model layout (~/.meept/tts/voices/*.onnx)
  and the piper CLI invocation the daemon uses.
- License rule: whisper.cpp (MIT) and piper (Open Source MIT-family) run
  as subprocesses invoked from PATH or a config path; NEVER vendored or
  linked. GPL-family would also be acceptable only as subprocess - record
  each binary's license (read LICENSE via the install path or cite the
  upstream) in the completion report.
- STT design (segmented, not streaming): whisper.cpp has no true
  streaming API. Strategy: VAD-free energy segmentation in the adapter -
  accumulate silence-detected utterances (simple energy gate, ~500 ms
  trailing silence) into a WAV, run whisper-cli, emit partial=false
  nothing and final=true per utterance. Latency: utterance end -> transcript
  ~= model time; acceptable for local/test legs. The interface supports it:
  one final Transcript per utterance, then reset the buffer. Document the
  trade-off in the adapter's doc comment.
- TTS design: Speak(text) runs `piper -m <model> [-f voice] --output-raw`
  (stdout = s16le at the model's native rate, commonly 22050), resample to
  the negotiated rate with the existing resample.go helper, stream through.

## Interface Contracts (From Parent)

Vendor names frozen (tierA keys-frame field values): "whisper"
(STT), "piper" (TTS). ProviderConfig mapping:
SttModel -> whisper model path (resolved: absolute, or under
$MEEPT_HOME/models/stt/<model> when bare; empty -> default "base" path
attempt, named error if absent). TtsModel -> piper .onnx path (resolved:
absolute, or under $MEEPT_HOME/tts/voices per docs/workflows/tts.md; empty
-> named error). TtsVoice -> piper -f config path override or multi-speaker
id; empty = model default. Binary names: whisper-cli (PATH; override via
voice.stt_bin? NO - keep this leaf zero-config: PATH lookup with
MEEPT_HOME/env honoring; note config-path follow-up as a finding). piper:
PATH lookup. Missing binary -> named constructor error (call fails, child
stays up - the providers_wiring contract).

## Tasks

| Task ID | Objective | Files | Verify | RED reason | PASS |
|---|---|---|---|---|---|
| T1 | Test: whisper adapter with a FAKE binary (testhelper script emitting a fixed transcript) turns buffered audio into one final Transcript per utterance; energy gate emits no transcript on silence | stt_whisper_test.go (+ testdata helper script) | go test -p 2 ./internal/voicechild/providers/ -run TestWhisper | no adapter | PASS |
| T2 | Test: piper adapter with a FAKE binary (emits deterministic PCM on stdout) yields s16le Reader at the negotiated rate (22050 -> 16000 resample asserted by length ratio); Speak errors surface as TTS stream errors, not process death | tts_piper_test.go | -run TestPiper | no adapter | PASS |
| T3 | Test: missing binary -> named constructor error for both; TestNoKeyLogs still green (construction logs vendor+model only) | both tests | -run 'TestWhisperMissing|TestPiperMissing|TestNoKeyLogs' | - | PASS |
| T4 | Implement both adapters + vendor switch rows | stt_whisper.go, tts_piper.go, providers.go | T1-T3 | - | PASS |
| T5 | Subprocess hygiene: ctx cancel kills the child process group; no orphan on adapter close | both | go test -p 2 -race ./internal/voicechild/providers/ + pgrep sweep in T1 test | - | PASS |
| T6 | gofmt/vet | - | go vet ./internal/voicechild/... | - | clean |

## Interface Contract (What This Leaf Exposes)

providers.NewSTT(cfg{Vendor:"whisper"}, key="" , rate) and
providers.NewTTS(cfg{Vendor:"piper", Voice: ...}, key="", rate) behavior;
switch rows for both vendor names.

## Self-Verification Checklist

- [ ] No import of internal/stt or internal/tts from the child (grep)
- [ ] No jargo needed for these two (they're subprocesses); import pins
      unaffected
- [ ] Key material rule N/A (no keys) - constructor must not request any
- [ ] License recorded in report; binaries NOT vendored
- [ ] ASCII only

## Review Checklist

- [ ] Utterance segmentation cannot grow unbounded buffers (cap + drop
      policy pinned by a test)
- [ ] Resample path matches the phone-leg rate contract (s16le mono)
- [ ] Subprocess stdin/stdout pipes always closed (no fd leak under -race)
