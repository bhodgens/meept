# Leaf 04 — Farewell Lane + Clip-Carrying Say Frames in the Talk Loop

> **DISPATCH INSTRUCTION:** Implement this leaf end-to-end with TDD. Work in
> the isolation worktree (path given in your dispatch context). Do NOT run
> `git commit` or `git add` — the orchestrator commits after review. Do NOT commit. Run the
> exact verify commands at the bottom; report results + file list.

## Meta

- **Parent:** orchestrator.md (phase1-clips)
- **Depends on:** 03 (clip_id frame field), 05 (farewell pool + PickLine)
- **Estimated context:** ~55K
- **Parallel group:** Wave C (after 02+03 REVIEWED)
- **Requirements:** R2 (wiring half), R5, R6

## Goal

Talk loop (`internal/voice/talkloop.go`): every filler-class line the.

## Scope

Talk loop (`internal/voice/talkloop.go`): every filler-class line the
loop speaks now carries its `clip_id`, and the new farewell lane speaks a
goodbye before the call's C1 `end` frame.

## Tasks

1. Lane-aware selection: the existing filler pick (fillerText round-robin,
   talkloop.go:1127-1165) moves to `voice.PickLine(lanes, LaneFiller, ...)`;
   greeting, silence-prompt (the audio-aware nudge lane), and
   fragment-clarify sites resolve from their lanes (05's VoiceLanes per
   employee). Behavior with a single-line pool must equal today exactly.
2. Every `t.say(...)` call for a filler-class line gains the clip_id:
   `t.say` gains an option/variant `sayClip(ccid, text, clipID string, final bool)`;
   the clipID = `clipstore.ClipKey(text, id.Vendor, id.Model, id.Voice,
   id.RefHash)` where `id` is the TTS identity the daemon wiring already
   resolved for the call (pass it into TalkLoop's cfg at construction —
   additive field, nil-safe default = no clip_id = today's path).
3. Farewell lane (R5): on call teardown — both the hangup path (daemon
   `end` frame emit site, see internal/voice/AGENTS.md "hangups fan out
   release exactly once") and a detected farewell intent on the caller's
   LAST utterance (cheap rule: case-insensitive match on a configured/
   default bye-phrase set in the caller text; no LLM) — speak one farewell
   line via `sayClip` with `final:true` semantics, THEN proceed with the
   existing release fan-out. Ordering rule: the farewell say frame must be
   emitted before the C1 `end` frame; the child's existing turn-final
   flush + played-drain machinery guarantees the audio completes before
   teardown kills the socket (verify with a pipeline test; if the current
   end-path races playback, gate the end emit on the `played` edge with a
   bounded timeout — 1500 ms — then end anyway; never hang the call).
   Supervisor-gate posture: farewell is fact-free filler-class, same lane
   as greeting (AGENTS.md: "fact-free filler/greeting excepted").
4. Telemetry: extend the existing "turn filler" log with
   `lane=<filler|greeting|silence|fragment|farewell> clip=<bool>` (the
   live smoke + gap analysis script key off this).

## Interface Contract (exposes to siblings)

None downstream; consumes 03's `sayClip` frame field + 05's pickers. The
`end`-after-farewell ordering behavior is what the 00T suite asserts.

## Self-Verification Checklist

- [ ] `go build ./internal/voice/...`
- [ ] `go test -p 2 ./internal/voice/... -count=1` green
- [ ] Unit: single-line pools ⇒ same lines, same order as legacy round-robin.
- [ ] Unit: farewell fires once per call; caller-text rule matches
      "bye"/"goodbye"/"talk later" but NOT "good one, now check X".
- [ ] Unit: denied/empty turn paths do not double-speak farewell + fallback.
- [ ] Pipeline test: farewell audio completes (played edge) before end
      frame kills the leg (or the 1500 ms bounded gate fires).
- [ ] `gofmt -l` empty.

## Review Checklist (for the orchestrator)

- [ ] Release fan-out still exactly-once (slot, goal unpark, effects) —
      the farewell must not add a second teardown path (AGENTS.md hangup
      invariant).
- [ ] No fact-bearing text in any farewell/clip lane (supervisor gate
      exemption is ONLY for the fact-free pools).
- [ ] Clip identity nil-safe: a call whose child predates this feature
      (no identity plumbed) behaves byte-identically to today.
