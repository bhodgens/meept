# Leaf 03 — Child: Render Misses + Serve Cached Clips

> **DISPATCH INSTRUCTION:** Implement this leaf end-to-end with TDD. Work in
> the isolation worktree (path given in your dispatch context). Do NOT run
> `git commit` or `git add` — the orchestrator commits after review. Do NOT commit. Run the
> exact verify commands at the bottom; report results + file list.

## Meta

- **Parent:** orchestrator.md (phase1-clips)
- **Depends on:** 01 (clipstore), 02 contract (frame shapes via this leaf's own voiceframe edits — see ordering note)
- **Estimated context:** ~70K
- **Parallel group:** Wave B. If wave-B file conflict on `internal/voiceframe/frame.go` appears (02 consumes vs 03 edits), 03 goes FIRST and 02 re-dispatches against the landed structs.
- **Requirements:** R1, R3 (child half), R6

## Goal

Voice child (`internal/voicechild` + `internal/voiceframe`): accept.

## Scope

Voice child (`internal/voicechild` + `internal/voiceframe`): accept
`clip_render` frames, synthesize misses through the installed
`TTSProvider`, persist them via `clipstore`, and — when a `say` frame
carries `clip_id` with a store hit — play cached PCM through the EXACT
same playback lane synthesized audio uses (pacing, barge-in clear,
turn-final flush, `played` edge reporting all unchanged).

## Tasks

1. `internal/voiceframe/frame.go` — additive fields per root Contract C3
   (quote it verbatim): `ClipID string \`json:"clip_id,omitempty"\`` on the
   say frame; new `clip_render` / `clip_ready` event structs. `Last *bool`
   semantics untouched (frame.go:19 comment stays true: only last:true
   starts a turn — clip frames never carry it).
2. `internal/voicechild/pipeline.go` —
   - Handle `clip_render`: for each `{key,text}` not `store.Has(key)`,
     queue a background render job through the existing synth worker
     discipline (reuse the synthJob goroutine pattern at pipeline.go:1511
     so renders cannot starve playback: dedicated ctx, bounded concurrency
     = 1 render at a time, cancel on call teardown). Write via
     `clipstore.Put` with meta.SampleRate = provider's native rate
     (qwen3: 24000, same value tts_qwen3 streams at). Emit `clip_ready`
     per success/failure. Render errors are DEBUG-logged + reported
     ok:false; the turn lane is never touched.
   - Say path (pipeline.go:1571 area): if `frame.ClipID != ""` and
     `store.Has`, open `store.Get` and feed the pcm reader into the SAME
     chunking/resample path Speak's reader feeds (resample.go from
     meta.SampleRate to negotiated rate). On miss or open error: today's
     `tts.Speak(WithTTSContext(...), frame.Text)` — byte-identical
     fallback.
   - NO opportunistic backfill on fallback synth (orchestrator review
     item: decided out of scope; flag in code with a one-line comment
     only if you feel the urge).
3. `internal/voicechild/child.go` / `providers_wiring.go` — construct the
   store once at setup; identity (vendor/model/voice) is already in
   ProviderConfig (providers_wiring.go:95), so the render job computes
   ClipKey with the SAME inputs the daemon used (Contract C1 pinned in
   both code paths; a byte-for-byte agreement test lives in 00T).
4. Log line: filler playback logs `clip=true` next to the existing
   pipeline logging so the live smoke (root Integration Test Plan step 4)
   can assert cached playback.

## Interface Contract (exposes to siblings)

The frame structs per C3 (02 imports them, never redefines). Behavior:
say-with-clip_id is a pure optimization — text still rides the frame and
stays the transcript source (R6: transcripts unchanged, clip is audio-only).

## Self-Verification Checklist

- [ ] `go build ./internal/voicechild/... ./internal/voiceframe/...`
- [ ] `go test -p 2 ./internal/voicechild/... ./internal/voiceframe/... -count=1` green
- [ ] Test: say with clip_id hit ⇒ zero Speak() calls (fake provider
      records calls), audio bytes == stored pcm.
- [ ] Test: say with clip_id miss ⇒ Speak() called once with frame.Text
      (today's path).
- [ ] Test: clip_render for an existing key ⇒ no synth work (fake provider
      call count 0).
- [ ] Barge-in test: cached clip mid-flight cleared by `clear` exactly like
      synthesized audio (reuse an existing pipeline barge-in test shape).
- [ ] `gofmt -l` empty.

## Review Checklist (for the orchestrator)

- [ ] Clip text (not audio) remains the transcript; C1 turn semantics
      (last:true) untouched — internal/voice/AGENTS.md invariants hold.
- [ ] Render concurrency = 1; no render queue survives call teardown
      (ctx cancel test).
- [ ] No mutex across file I/O or Speak (mutexio clean).
