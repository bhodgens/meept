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

1. `internal/voiceframe/frame.go` AND `internal/voice/frame.go` — additive
   fields per root Contract C3 (quote it verbatim). `internal/voiceframe/frame.go`
   gets `ClipID string \`json:"clip_id,omitempty"\`` on `Frame`, plus
   `Clips []ClipRequest` (with `ClipRequest{Key, Text string}`) and
   `Key string` / `OK bool` for `clip_ready`. `internal/voice/frame.go:22-33`
   is the daemon-side alias const block: add `EventClipRender` and
   `EventClipReady` there too, or the daemon cannot name the events.
   `Last *bool` semantics untouched (the frame.go:19 comment stays true:
   only last:true starts a turn — clip frames never carry it).
2. `internal/voicechild/pipeline.go` —
   - Handle `clip_render`: for each `{key,text}` not `store.Has(key)`, run a
     render job on ONE dedicated render goroutine per call (bounded
     concurrency = 1, its own ctx, cancelled on call teardown). Do NOT reuse
     the playback prefetch queue (`prefetchDepth` = 2, pipeline.go:1511
     area): a render is seconds of work and would starve a live sentence.
     Write via `clipstore.Put` with `meta.SampleRate = 16000`.
     Why 16000: the provider adapter already resamples to the negotiated
     rate before it returns the reader (`providers.go` ProviderConfig
     carries the negotiated output rate; `tts_openai.go:252` streams through
     the resampler), and the child lane is fixed L16@16k = 640-byte chunks
     (internal/voice/AGENTS.md, Sockets C2). Store exactly what the reader
     yields. Emit `clip_ready` per success/failure with `ok`.
     Render errors are DEBUG-logged + reported `ok:false`; the turn lane is
     never touched.
   - Say path (pipeline.go:1571 area): if `frame.ClipID != ""` and
     `store.Has`, open `store.Get` and feed the pcm reader into the SAME
     chunking/write path a Speak reader feeds — RAW, no second resample. A
     meta `sample_rate` other than 16000 is a MISS (leaf 01 rejects the
     write; on read, treat it as miss and let the next render replace it).
     On miss or open error: today's `tts.Speak(WithTTSContext(...),
     frame.Text)` — byte-identical fallback.
   - NO opportunistic backfill on fallback synth (decided out of scope; a
     one-line comment is enough).
3. `internal/voicechild/child.go` / `providers_wiring.go` — construct the
   store once at setup; compute `ClipKey` from the identity the keys frame
   carried (vendor/model/voice plus `tts_ref_hash`, defaulting to the empty
   digest), i.e. the SAME inputs the daemon used (Contract C1 pinned in both
   code paths; a byte-for-byte agreement test lives in 00T).
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
- [ ] Test: the stored clip bytes equal the bytes the live lane would write
      for the same text (proves no second resample and the 16 kHz rate).
- [ ] Test: a clip whose meta sample_rate != 16000 is treated as a MISS.
- [ ] Test: render work never consumes the playback prefetch depth (a live
      say still synthesizes while a render is running).
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
