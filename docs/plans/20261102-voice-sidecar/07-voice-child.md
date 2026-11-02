# Voice child skeleton - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Gate test 00T T9 (no-jargo import
> pin) applies to the daemon side. Do NOT commit. Do NOT use read_file on
> existing source files - explore with search_files or terminal cat. Write
> once, do not read back.

## Meta

- **Parent:** master.md
- **Scope:** cmd/meept-voice binary + internal/voicechild: jargo pipeline per call, C1/C2 socket client side, interrupt emission, say/clear handling. Provider adapters are STUBBED here (leaf 08 fills them)
- **Dependencies:** leaf 03 (socket/frame types - shared module, child imports internal/voice frame codec only)
- **Estimated Context:** 90K
- **Concurrency Group:** B
- **Files this leaf owns:** cmd/meept-voice/main.go, internal/voicechild/*.go; go.mod (jargo dep)

## Goal

The separate binary that owns ears, turn-taking, barge-in, mouth. One
jargo pipeline per live call, multiplexed over the shared text socket
(every frame carries call_id) and per-call audio socket pairs. Child asks
the daemon for provider keys at start (leaf 08 wires real fetch; skeleton
defines the request frame path).

## Context

jargo (github.com/gojargo/jargo, BSD-2): a bot is a chain of processors -
transport input, VAD (Silero), STT, turn-taking (Smart Turn v3), LLM, TTS,
transport output. This child REPLACES jargo's LLM and TTS stages with
meept-bridge processors:
- MeeptSTT output (final transcripts) -> Frame{event:"prompt", last:true}
  on the text socket; partials -> last:false.
- incoming Frame{event:"say"} -> jargo TTS stage -> audio out to the
  per-call AudioPair .in socket (daemon reads and paces to Telnyx).
- Frame{event:"clear"} -> drop queued TTS for that call.
- jargo VAD/turn-taking detects caller speech during playback ->
  Frame{event:"interrupt"} (daemon then clears Telnyx + cancels drafts).
ONNX Runtime is required for VAD/turn models (jargo loads it at runtime;
missing lib = child exits non-zero with the missing path logged - daemon
refuses the call, spec rule). Arm64 native build on this Mac; never the
amd64 Docker image.
The child does NOT import internal/voice (daemon package) except the frame
codec: move Frame/EncodeFrame/DecodeFrame/ChunkBytes to a shared
internal/voiceframe package in THIS leaf (leaf 03 created them under
internal/voice; relocate with a type alias left behind so leaf 03 tests
keep passing - coordinate: this leaf owns the move, orchestrator sequences
it after leaf 03 COMPLETE).
Pipeline-per-call: map[ccid]*pipeline in the child; setup frame creates,
end frame tears down. Concurrent calls = concurrent pipelines (jargo is
concurrent by design: independent processors, interruptions are frames).
Key fetch: at start the child sends Frame{event:"setup"} per call; the
daemon replies with provider keys over the text socket ONLY if the child
authenticated with its start token (token passed via stdin at spawn -
never argv, never env; leaf 13 spawns). Skeleton defines the
Frame{event:"keys"} daemon->child shape: {"call_id":"","event":"keys","stt_key":"...","tts_key":"..."} - keys never logged; the frame type gets a
String() that redacts.

## Verified Anchors

- github.com/gojargo/jargo README (fetched 2026-11): pipeline.New(procs...), pipeline.NewWorker, transports WebRTC/WebSocket/local; VAD+turn ONNX; Twilio phone example exists (do NOT port)
- internal/voice/frame.go (leaf 03) - Frame codec to relocate
- master.md C1/C2 - frame + socket contracts

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/voiceframe (relocated codec; internal/voice keeps aliases)
Frame, EncodeFrame, DecodeFrame, ChunkBytes  // unchanged shapes
// internal/voicechild
type Child struct{ ... }
func New(cfg ChildConfig) *Child           // text socket path, audio dir, start token from stdin
func (c *Child) Run(ctx context.Context) error
// stub provider seams (leaf 08 implements):
type STTProvider interface{ Stream(ctx context.Context, pcm <-chan []byte) (<-chan Transcript, error) }
type TTSProvider interface{ Speak(ctx context.Context, text string) (io.ReadCloser, error) }
type Transcript struct{ Text string; Final bool }
```

### What This Leaf Consumes

- C1/C2 (master.md); leaf 03 frame codec (relocated here)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Relocate frame codec to internal/voiceframe; alias in internal/voice; leaf-03 tests still green | Create: internal/voiceframe/*.go; Modify: internal/voice/frame.go | go test -p 2 ./internal/voice/ ./internal/voiceframe/ | - | PASS | Given relocation; When old tests run; Then green via aliases |
| T2 | jargo dep + Child skeleton: connect text socket, read stdin token, handle setup/end, pipeline map with stub STT/TTS | Create: cmd/meept-voice/main.go, internal/voicechild/child.go; Modify: go.mod | go build ./cmd/meept-voice/ | - | compiles | Given fake daemon socket; When setup frame; Then pipeline registered for ccid |
| T3 | Prompt emission: stub STT final -> Frame{prompt,last:true} with call_id; partial -> last:false | Create: internal/voicechild/pipeline.go | go test -p 2 ./internal/voicechild/ -run TestPromptFrames | - | PASS | Given stub transcript final; When processed; Then exact C1 frame on socket |
| T4 | Say/clear: Frame{say} -> stub TTS -> PCM written to AudioPair .in; Frame{clear} -> queued audio dropped, writes stop | (same) | go test -p 2 ./internal/voicechild/ -run TestSayClear | - | PASS | Given say then clear; When processed; Then .in socket got first chunk only |
| T5 | Interrupt emission: stub VAD-during-playback signal -> Frame{interrupt} once per barge-in | (same) | go test -p 2 ./internal/voicechild/ -run TestInterrupt | - | PASS | Given playback active + caller speech; When detected; Then interrupt frame |
| T6 | Keys frame redaction: String()/log of keys frame shows no key material | Create: internal/voicechild/keys.go | go test -p 2 ./internal/voicechild/ -run TestKeysRedaction | - | PASS | Given keys frame; When formatted; Then redacted |
| T7 | Import pin: internal/voice and internal/voiceframe contain no jargo import (gate test T9 passes) | - | go test -p 2 ./internal/voice/ -run TestNoJargoImport | - | PASS | Given the tree; When scanned; Then jargo only under cmd/meept-voice + internal/voicechild |
| T8 | Format/vet/race | - | gofmt -l cmd/meept-voice internal/voicechild internal/voiceframe; go vet ./...; go test -p 2 -race ./internal/voicechild/ | - | clean | - |

## Self-Verification Checklist

- [ ] jargo confined to child packages (import pin green)
- [ ] Stub providers behind the leaf-08 interfaces; no vendor code here
- [ ] Start token via stdin only (grep: never os.Args, never env for the token)
- [ ] Keys never logged (redaction test green)
- [ ] Concurrent pipelines: two ccids in one child, -race clean
- [ ] ASCII-only

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** paths
2. **Verify command + output:** last 20 lines
3. **Deviations:** none / list (jargo API drift named explicitly)

## Review Checklist (For Review Agent)

- [ ] C1 frame shapes byte-exact on the wire (capture a round-trip)
- [ ] Relocation kept leaf-03 tests untouched and green
- [ ] No telnyx visibility in the child (grep)
- [ ] ONNX-missing behavior: exit non-zero + path logged, no secrets

Output: APPROVED or specific gaps.

## Notes

jargo's API is young; pin the exact version in go.mod and record the
processor-chain wiring in code comments. If jargo's WebSocket transport
fits better than raw sockets for the audio pair, keep the unix-socket C2
contract anyway - the daemon side is fixed.
