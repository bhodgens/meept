# Leaf 01 - recallable voice playback (kill the talk-over)

## Contract

A barge-in/cancel must SILENCE the speaker within ~20 ms: no audio handed
to the player before the cancel may continue playing after it, and the CLI
may not buffer more than a bounded lookahead of not-yet-played audio.

## Verified facts (do not re-derive)

- oto v3.4 (`github.com/ebitengine/oto/v3@v3.4.0`) `*Player` has `Pause()`,
  `Play()`, `SetVolume`, `IsPlaying()`, `Err()`.
- internal/tts/player.go: `AudioPlayer.Play` creates a NEW oto player per
  burst, calls Play(), never retains it; `Stop()` returns nil and is a
  documented no-op ("playback runs to completion").
- internal/voice/clientaudio.go: `OtoPlayer.Clear()` calls that no-op
  Stop(); `SpeakerSink` buffers a burst then hands WAVs to the Player.
- cmd/meept/voice.go live loop: reads the .c.out socket and writes+Flush per
  read - unbounded: kernel socket buffer + sink backlog hold seconds of
  audio that keep playing after a cancel.

## Implementation

1. `internal/tts/player.go`: AudioPlayer tracks `cur *oto.Player`. Play()
   pauses any previous player before starting the new one. Stop() pauses
   `cur` (idempotent; nil-safe). Document: one live burst at a time; a
   paused player is abandoned (GC's concern, matching how new players were
   previously created).
2. `internal/voice/clientaudio.go`: SpeakerSink.Close/Clear path: on Clear,
   drop the buffered partial burst AND call the player's Stop so the current
   burst goes silent. Player interface unchanged (Play/Clear/Close).
3. CLI bounded playout (cmd/meept/voice.go live loop): cap in-flight
   handoffs - read the socket into a small queue drained by ONE writer
   goroutine that stops taking new bursts when a "muted" epoch advances.
   Mute signal source: the daemon already sends C1 `clear` on barge-in, but
   the CLI does not see C1 - it sees only PCM. Therefore mute locally: when
   ReadFromChild... (the CLI cannot observe clears; DO NOT invent a new RPC
   mid-leaf). Simplest conforming bound: drain each read into the sink but
   keep at most 2 pending bursts (~80 ms at the daemon's 20 ms pacing x
   chunk size... choose a concrete bound with a comment: 3 bursts / ~150 ms
   is the ceiling) - the pacing socket backpressure then stalls the daemon
   outbound loop, which stops at barge-in (latch) anyway. The hard stop of
   whatever the speakers already hold is step 1's Pause - that is what kills
   the tail.
4. Keep file mode (--out WAV) EXACTLY as is: no pause semantics needed
   (nothing plays to speakers).

## Tests

- internal/tts: with a fake-injectable seam OR (if oto can't run headless
  in CI) unit-test the bookkeeping: Play() twice retains only the latest
  player; Stop() pauses cur and is nil-safe. Mirror existing tts test
  style (player has an interface seam? if not, extract `otoPlayerIface`
  privately - do NOT widen public API).
- internal/voice: SpeakerSink.Clear drops buffered bytes (fake player
  records Play calls; assert no Play after Clear for pre-Clear writes).

## Gates
go test -count=1 ./internal/tts/ ./internal/voice/ ./cmd/meept/ green;
gofmt/vet clean. No commits (orchestrator commits).
