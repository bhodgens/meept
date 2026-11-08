# RPC control plane + voice CLI - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. Do
> NOT commit — the orchestrator handles git. Explore with search_files or
> terminal cat, never read_file. Do NOT read back files you just wrote.
> This is the ONLY leaf allowed to edit internal/daemon/components.go in
> this tree; check `git status` before starting and serialize with any
> other live session that has components.go dirty.

## Meta

- **Parent:** master.md
- **Scope:** Expose the local leg to clients: RPC methods
  voice.local.open/end/status, registration wiring, and the `meept voice
  local` CLI command (mic/speaker via ffmpeg+player, plus --in/--out file
  mode). Part 1 of "any client can use local voice".
- **Dependencies:** leaf 07 (LocalLeg API)
- **Estimated Context:** 85K
- **Concurrency Group:** D
- **Files this leaf owns:** internal/rpc/voice_local.go (new),
  internal/rpc/voice_local_test.go (new), internal/daemon/rpc_wiring (the
  RegisterAll site for voice_local - grep `RegisterHandler(` callers),
  cmd/meept/voice.go (new), cmd/meept commands registration file (grep
  `AddCommand` in cmd/meept), internal/voice/clientaudio.go (new, ffmpeg
  capture + player shim)

## Goal

A user (or another agent's shell) runs:

```
meept voice local --employee eve --in greeting.wav --out reply.wav
```

and holds a real voice conversation end-to-end on one machine with no
carrier and no cloud provider. With no --in/--out the shim captures the
mic (ffmpeg avfoundation, the internal/stt Recorder pattern) and plays
through internal/tts's oto player.

## Context

Key files:
- internal/rpc/acp.go - handler + RegisterHandler pattern to copy.
- internal/daemon - where RPC handlers get registered (grep acp.go's
  Register caller); add the voice_local register there.
- cmd/meept/ - existing command registration (grep AddCommand); copy the
  agents-list command's RPC client shape (client call + table print).
- internal/stt/recorder.go - ffmpeg subprocess capture pattern; NOTE it
  records to a temp WAV file; the local leg needs RAW s16le 16k mono on a
  pipe: ffmpeg -f avfoundation -i ":default" -f s16le -ar 16000 -ac 1 -
  (macOS). Windows/Linux capture stays UNIMPLEMENTED with a named error
  ("mic capture unsupported on GOOS; use --in file mode") - the file
  modes keep CI and Linux users unblocked.
- internal/tts/player.go - oto AudioPlayer: Play(audioData []byte) takes
  WAV/PCM. For a live stream, write chunks to a temp WAV per sentence?
  No - simplest contract that holds: buffer per playback burst between
  clear-events into one WAV (44-byte header + s16le@16k), hand to
  AudioPlayer.Play; barge-in cancels by closing the player context if the
  API allows, else let the burst finish (accepted limitation, note it).
- Audio path: client connects BOTH unix pair sockets (InPath = client
  writes mic bytes there; OutPath = client reads speaker bytes). Two net
  dials, not one conn - matches AudioPair's two-listener design.

## Interface Contracts (From Parent)

CL8 (verbatim) - RPC shapes:
`voice.local.open` req {employee_id, label} -> {call_id, in_path, out_path,
session_id}; `voice.local.end` req {call_id} -> {ok}; `voice.local.status`
req {} -> {calls:[{call_id, employee_id, target, since_rfc3339}]}.
Errors: named strings for disabled-leg, unknown-employee, busy, duplicate.
status reads LocalLeg.Live() + registry Live() (phone rows shown too, with
leg). CLI `meept voice status` prints both legs; `meept voice local
--list` lists employees for convenience. No audio over RPC.

## Tasks

| Task ID | Objective | Files | Verify | RED reason | PASS |
|---|---|---|---|---|---|
| T1 | Handler tests with a fake LocalLeg: open ok / disabled / unknown employee / busy map to named errors; end idempotent; status lists legs | internal/rpc/voice_local_test.go | go test -p 2 ./internal/rpc/ -run TestVoiceLocal | no file | PASS |
| T2 | Implement handlers + register site | voice_local.go, daemon register site | T1 | - | PASS |
| T3 | clientaudio.go: RawMicCapture (ffmpeg pipe -> io.Reader, stop via ctx) + SpeakerSink (burst WAV -> oto Play; Clear() drops pending burst) | internal/voice/clientaudio.go | go test -p 2 ./internal/voice/ -run TestClientAudio (file-mode reader + sink burst logic with a fake player) | - | PASS |
| T4 | CLI: voice local with --in/--out (file mode: pure io.Copy both directions, exits cleanly when the call ends), --employee required unless --list; graceful: SIGINT -> voice.local.end | cmd/meept/voice.go + registration | go build ./cmd/meept && ./bin? use: go run ./cmd/meept voice local --list (against no daemon -> clean connection-refused error) | no command | PASS |
| T5 | CLI live mode: capture+sink wired when not file mode; GOOS guard | cmd/meept/voice.go | go build ./cmd/meept; manual smoke recorded in report if mic permitted | - | PASS |
| T6 | voice status table both legs | cmd/meept/voice.go | go build + unit test of the row formatter | - | PASS |
| T7 | gofmt/vet | - | go vet ./internal/rpc/... ./cmd/meept/... ./internal/voice/... | - | clean |

## Interface Contract (What This Leaf Exposes)

- RPC method names + shapes (CL8) - GUI/TUI parity work (leaf 13 and any
  future client) consumes them; document them in the completion report for
  leaf 12's docs.
- voice.RawMicCapture / voice.SpeakerSink for leaf 13 reuse.

## Self-Verification Checklist

- [ ] RPC never returns an absolute path outside $MEEPT_HOME
- [ ] CLI prints lowercase UI text (repo UI rule)
- [ ] ffmpeg subprocess killed on ctx cancel (no orphan ffmpeg - check
      process table in the test)
- [ ] components.go diff limited to the register call (reviewer: verify)

## Review Checklist

- [ ] CL8 shapes byte-match the JSON tags
- [ ] File mode is deterministic and CI-safe (no mic needed)
- [ ] No secrets in logs (there are none; confirm)
- [ ] ASCII only; no artifacts
