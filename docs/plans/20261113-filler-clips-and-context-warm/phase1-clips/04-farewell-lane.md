# Leaf 04 - Farewell Lane + Clip-Carrying Say Frames in the Talk Loop

> **DISPATCH INSTRUCTION:** Implement this leaf end-to-end with TDD. Work in
> the isolation worktree (path given in your dispatch context). Do NOT run
> `git commit` or `git add` - the orchestrator commits after review. Do NOT commit. Run the
> exact verify commands at the bottom; report results + file list.

## Meta

- **Parent:** orchestrator.md (phase1-clips)
- **Depends on:** 03 (clip_id frame field + event constants), 05 (farewell pool + PickLine)
- **Estimated context:** ~55K
- **Parallel group:** Wave C (after 02+03 REVIEWED)
- **Requirements:** R2 (wiring half), R5, R6

## Goal

Talk loop (`internal/voice/talkloop.go`): every filler-class line the loop
speaks now carries its `clip_id`, and the farewell lane speaks a goodbye on
the two paths where a listener still exists.

## Scope

Talk loop (`internal/voice/talkloop.go`): every filler-class line the
loop speaks now carries its `clip_id`; lane-aware line selection replaces
the ad-hoc picks; the new farewell lane speaks one goodbye clip on (a) a
caller farewell intent and (b) an agent/operator-initiated hangup.

## Release-path constraints (read before designing the farewell)

Verified against the code:

- `releaseCall` sends the C1 `end` frame as its FIRST sink
  (`internal/daemon/voice_wiring.go:1421-1432`), then drops the loop and
  cancels its context.
- The child's `handleEnd` drops the pipeline and closes it immediately
  (`internal/voicechild/child.go:446-454`), so any say frame still queued is
  dead audio.
- `internal/voice/AGENTS.md` pins: the end-frame send must NEVER block
  teardown and must NOT move behind the loop cancel.
- A carrier `hangup` event arrives AFTER the caller has left. A farewell
  clip on that path is spoken to nobody.

Consequence: do NOT insert a wait inside `releaseCall`, and do NOT reorder
the end frame behind the loop cancel. A farewell that blocks release breaks
a pinned invariant and delays slot release plus goal unpark.

## Tasks

1. Lane-aware selection. Replace the ad-hoc picks with `voice.PickLine`:
   - filler: `fillerLine()` (`talkloop.go:549-553`, keep the `fillerIdx`
     round-robin semantics through `PickLine`'s cursor),
   - greeting: `greetingLine()` (`talkloop.go:537-541`; empty greeting falls
     back to the first filler line, as today),
   - silence nudge: `SilencePromptLine` / `voice.silence_prompt_lines`,
   - fragment clarify: `FragmentClarify` / `voice.fragment_clarify_lines`.
   Behavior with a single-line pool must equal today exactly - pin it with a
   test that runs the legacy path and the lane path and compares the spoken
   line sequence.
2. Clip-carrying say. Add `sayClip(ccid, text, clipID string, last bool)`.
   `say` stays the single choke: `sayClip` delegates to `say` with the clip
   id carried on the frame (`echo_roster.RecordSpoken` keeps firing, so the
   cross-call echo roster is unaffected). A clip id is emitted ONLY for
   filler-class lines (greeting, filler, silence, fragment, farewell);
   never for reply sentences.
   `clipID = clipstore.ClipKey(text, id.Vendor, id.Model, id.Voice, id.RefHash)`
   where `id` is the TTS identity the daemon wiring already resolved for
   this call (`voice.ResolveProfile`). Add it to `TalkLoopConfig` as an
   additive field; a nil/zero identity means no clip id, which is exactly
   today's wire shape.
3. Farewell on caller intent (R5a). When the caller's last utterance matches
   the bye-phrase set (case-insensitive, whole-phrase: "bye", "goodbye",
   "talk later", "that's all, thanks", plus a configured set), and no turn is
   in flight, speak one farewell line via `sayClip(..., last=true)`. This
   happens INSIDE the live call, before any teardown, so no release-path
   change is needed. Never speak it twice per call (once-guard on the loop).
4. Farewell on agent/operator hangup (R5b). Where the daemon already decides
   to end a call on the agent's or the operator's behalf
   (`w.tel.HangupCall` call sites, `internal/daemon/voice_wiring.go:1235`
   and `:1255`, plus the `voice local` end path), sequence: send the farewell
   `sayClip` frame, wait for the child's C1 `played` edge OR 1200 ms,
   whichever comes first, THEN call the existing hangup/release. Do this on
   a goroutine owned by that path; `releaseCall` itself is UNCHANGED and
   keeps its current first sink.
   A played-waiter seam DOES NOT EXIST YET and this leaf adds it: the
   `EventPlayed` branch (`voice_wiring.go:906-920`) currently consumes the
   frame and only arms the local splice (`voice.LocalSpliceFor(ccid).MarkFinalPlayed()`,
   `internal/voice/localsplice.go:1368`), which is nil on a phone leg. Add a
   minimal, nil-safe per-ccid waiter (a channel or callback registry) that
   the same branch notifies in addition to the splice - never instead of it,
   and never by letting the frame reach the talk loop. If the edge never
   arrives (old child, no waiter), the 1200 ms bound releases anyway.
   Carrier hangup (caller already gone): speak nothing.
5. Telemetry: extend the existing "turn filler" log line with
   `lane=<filler|greeting|silence|fragment|farewell> clip=<bool>` so the
   live smoke and `scripts/voice-gap-analyze.py` can key off it. Keep the
   existing `line` and `after_ms` fields.

## Interface Contract (exposes to siblings)

None downstream; consumes 03's `ClipID` field plus event constants and 05's
pickers. The behavior the 00T suite asserts: filler-class says carry
`clip_id`; a farewell on an agent-initiated end is followed by `played`
before `end`; `releaseCall` still sends `end` first.

## Self-Verification Checklist

- [ ] `go build ./internal/voice/... ./internal/daemon/...`
- [ ] `go test -p 2 ./internal/voice/... ./internal/daemon/... -count=1` green
- [ ] Unit: single-line pools imply the same lines and order as legacy
      round-robin (table test).
- [ ] Unit: farewell fires once per call; the phrase rule matches
      "bye"/"goodbye"/"talk later" but NOT "good one, now check X".
- [ ] Unit: reply sentences never carry clip_id (only filler-class lanes).
- [ ] Unit: `say` still calls `RecordSpoken` for clip-bearing frames.
- [ ] Integration: agent-initiated end path sends say(farewell) then waits
      for played then calls release; assert `releaseCall` ordering unchanged
      when the farewell is skipped.
- [ ] Timeout: a fake child that never emits `played` still ends the call
      within the 1200 ms bound.
- [ ] `gofmt -l` empty.

## Review Checklist (for the orchestrator)

- [ ] Release fan-out still exactly-once (slot, goal unpark, effects) - the
      farewell adds no second teardown path and no wait inside `releaseCall`.
- [ ] The end-frame send is still `releaseCall`'s first sink and cannot block.
- [ ] No fact-bearing text in any farewell/clip lane (the supervisor-gate
      exemption covers fact-free pools only).
- [ ] The played-waiter seam is additive: the `EventPlayed` branch still arms
      the local splice for every call, the frame still never reaches the talk
      loop, and a phone leg (nil splice) still gets its waiter notification.
- [ ] Clip identity nil-safe: a call whose wiring supplies no identity
      behaves byte-identically to today.
- [ ] `t.say()` remains the single audible choke (echo_roster.go:136).