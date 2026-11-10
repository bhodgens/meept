# Voice dead-air guard + telemetry + two-agent voice link - master

> Root orchestrator. Worktree .hermes/worktrees/voice-deadair-link, branch
> plan/voice-deadair-link (off main d42becdd: includes the full chop-hunt
> instrumentation set - child cadence, pacer summary, CLI trace, drip probe).

## Evidence base (user-reported, live calls 2026-11-09)

calls.db local-376ee373022f7663 transcript shows the dead-air loop:
whisper's PARALINGUISTIC bracket tokens - "(farting)", "(mouse clicking)",
"(squeaking)", "[clears throat]", "(inhales deeply)" - were dispatched as
user prompts and the companion ANSWERED EACH ONE ("I hear you taking a
breath..."). leaf 02's filter (3a755a84) covers only an exact-match set
([BLANK_AUDIO], (sighs), (laughs), (no speech), (background noise)) -
whisper emits dozens of such tokens; the class, not the list, must be
matched. "If it's not text, the voice agent should not respond."

## User directives (contracts)

A. DEAD-AIR GUARD: final transcripts that carry no real words (bracket
   tokens only, punctuation, whitespace, whisper artifacts like
   "you know" repeated-empty heuristics) must NEVER dispatch an employee
   turn. Single choke point child-side (leaf 02 precedent).
B. TELEMETRY: thorough voice-turn logging so loop behavior is
   self-evident: every candidate prompt classified (dispatched vs dropped
   + REASON), turn start/cancel(barge-in/supersede)+reason, filler
   emitted/withdrawn, reply sentences spoken/suppressed, per-call summary.
   Structured slog lines under component=voice with ccid + seq, plus
   transcript rows for the guard events (role "system") so calls.db
   answers "why did it talk" after the fact.
C. FILLER FINDING (answer to user question): "One moment." / "Give me a
   second on that." are NOT saved clips - they are the voice.filler config
   lines, synthesized through Qwen3-TTS like every other utterance, and
   the talk loop speaks filler[0] IMMEDIATELY on each dispatched prompt.
   Fix: emit filler only when the turn exceeds a small grace window
   (default ~2.5 s), round-robin as today.
D. TWO-AGENT VOICE LINK: a test mode where two employees converse over
   voice with no human: agent A's spoken PCM feeds agent B's mic and vice
   versa (real TTS -> real whisper STT -> real turns). `meept voice link
   --a companion --b secretary [--seconds 90]`. Link-mode legs suppress
   RMS barge-in (machine speech must not self-cancel the reply chain) and
   the greeting is fact-free so it still plays. This is the harness the
   user wants for jitter testing: iterate (leaf 05+) until a link session
   shows clear turns, no jitter (cadence numbers), no dead-air loop.

## Leaves

| # | Leaf | Files (primary) | Depends | Wave |
|---|------|-----------------|---------|------|
| 01 | paralinguistic dead-air guard (class-based non-speech) | internal/voicechild/pipeline.go (+tests) | - | A |
| 02 | turn telemetry + transcript audit + delayed filler | internal/voice/talkloop.go, internal/voice/local.go(call-row seam), internal/daemon/voice_wiring.go, internal/config/schema.go (filler_grace_ms) | - | A |
| 03 | voice link: two-agent PCM bridge + CLI subcommand | internal/voice/link.go (new), internal/rpc/voice_link.go (new), internal/daemon/voice_link_rpc.go (new), cmd/meept/voice.go | 01,02 (runtime behavior) | B |
| 04 | docs + config template + AGENTS contracts | docs/workflows/voice.md, internal/voice/AGENTS.md, config/meept.json5 | 01-03 | C |
| 05+ | jitter/link iteration fixes (dispatch per findings) | TBD by 03's first link run | 03 | D |

## Verification gates

- Leaf 01: table tests for the token class (>=15 real whisper tokens);
  live-call transcript replay: the 6 bracket-only turns above classify as
  drops (test fixture embeds them verbatim).
- Leaf 02: unit: filler suppressed inside grace, emitted after; one
  structured line per lifecycle event (assert via slog test handler).
- Leaf 03: hermetic-ish: link two employees against local runtimes
  (whisper+piper or qwen if up) for 60 s; assert BOTH transcripts reach
  >=2 real turns each with zero bracket-only turns; cadence summary shows
  pace_stalls=0 beyond grace.
- go test -p 2 ./internal/voicechild/ ./internal/voice/ ./internal/rpc/
  ./internal/daemon/ ./cmd/meept/ green per touch; voice e2e areas green.
- Final acceptance (user's words): "2 voice agents which can communicate
  with each other clearly via voice with no jitter" - measured, not
  heard: CLI trace read_gap_avg <= 25 ms during speech spans, zero
  pacer-stall WARNs within an utterance, transcripts show coherent
  alternating turns.

## Sibling-collision notes

- Branch base includes today's voice commits (71f85347..d42becdd); the
  main checkout has sibling waves dirty in internal/agent/delegate* -
  NEVER touch. cmd/meept/voice.go is ours (voice surface); leaf 03
  touches it AFTER 02 lands (serial wave B) to avoid same-file collision.
- ~/.meept live config changes (filler_grace_ms) are orchestrator-side.

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| 01 | PENDING | |
| 02 | PENDING | |
| 03 | PENDING | |
| 04 | PENDING | |
| 05 | PENDING | |
