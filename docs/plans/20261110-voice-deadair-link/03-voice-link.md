# Leaf 03 - voice link: two employees converse over voice (test harness)

## Goal (user directive D)

`meept voice link --a companion --b secretary --seconds 90` opens TWO
local-leg calls in the daemon and cross-wires their audio: A's spoken PCM
becomes B's microphone and vice versa. Real TTS -> real socket PCM -> real
STT -> real employee turns on both sides. No human, no ffmpeg, no oto.
This is the loop-testing harness for jitter + dead-air acceptance.

## Architecture (in-daemon bridge; the CLI never touches audio)

- LocalLeg.Open(ctx, employeeID, label) returns LocalCall with InPath/
  OutPath pair socket paths (internal/voice/local.go:260,433-441) - the
  exact files the CLI dials. The link therefore needs NO LocalLeg change:
  open two calls with labels "link-a"/"link-b", then in-process dial
  A.InPath (write side = A's mic), A.OutPath (read side = A's speakers)
  etc. and CROSS-CONNECT pumps: copy A.OutPath -> B.InPath and
  B.OutPath -> A.InPath byte-exact. Each leg sees precisely what a human
  client would.
- New file internal/voice/link.go (package voice): LinkManager.Open(ctx,
  cfg LocalLegDeps-ish, a, b string) (*Link, error); Close() ends both
  calls (LocalLeg.End, idempotent). Acquire happens inside LocalLeg.Open
  (registry rules apply: one live per employee -> a==b is a named
  boot-time refusal ErrLinkSameEmployee checked up front; targets
  "link:..." differ from phone/local: labels so per-target is clear).
- Link-mode overrides (documented as deliberate, pinned in tests):
  barge-in RMS DISABLED on both legs (machine speech must not self-cancel
  the reply chain; pass the zero knob the pipeline already honors -
  verify local && bargeInRms > 0 guard); greetings STILL play (fact-free)
  - that is the seed turn. Silence reaper (45 s) stays ENABLED: it is the
  dead-air circuit breaker, and the acceptance run must prove it fires or
  never has to.
- RPC: internal/rpc/voice_link.go with VoiceLinkSeams func fields (NEVER
  imports internal/voice; the daemon adapts in
  internal/daemon/voice_link_rpc.go - mirror voice_local.go's shape):
  voice.link.open {employee_a, employee_b} -> {link_id, a_ccid, b_ccid};
  voice.link.close {link_id}; voice.link.status {link_id} -> turn counts
  per side (read the call store transcripts).
- CLI: cmd/meept/voice.go new subcommand `voice link` (flags --a required,
  --b required, --seconds default 60): open, poll status every 5 s
  printing alternating turn counts, close at deadline, then print BOTH
  transcripts (calls.db) compactly. Exit 0 when both sides have >=2
  assistant turns; exit 1 with the reason otherwise (this doubles as the
  automated acceptance check).

## Wave dependency

Land AFTER leaves 01+02 (the guard + delayed filler make the link
coherent). Touches cmd/meept/voice.go (nobody else does in this tree).

## Tests

- Unit (internal/voice, in-process pipes + fake child? NO - the child is
  a real subprocess): hermetic variant: LinkManager.Open with injected
  fake splice (if the seam exists) OR test the pipe-cross wiring alone:
  write PCM to A's speaker pipe, assert it arrives on B's mic pipe
  byte-exact (the bridge's whole contract). Registry refuse tests: a==b
  error, busy employee error, close idempotent, double-close nil.
- RPC seam tests mirroring voice_local_test.go (no internal/voice import
  check included).
- CLI: flag validation (missing --b error, non-positive seconds error).
- E2E (voice-local suite extension, the REAL acceptance): boot scratch
  daemon with piper TTS + whisper STT configured local-only (existing
  fixtures in that suite already drive a local call end-to-end - reuse
  them; if the suite's child is a fakechild without audio, the link e2e
  must run the REAL meept-voice binary - check e2e/harness for the
  real-child precedent first). 90 s link: assert (1) both transcripts
  reach >=2 assistant turns, (2) zero bracket-only user rows, (3) daemon
  log shows filler_skipped or filler after_ms>=grace (no immediate
  filler storm), (4) per-utterance child cadence read_max_ms < 250 and
  pacer pace_stalls=0 WITHIN utterances (the between-utterance idle is
  expected - filter by span).

## Gates
go test -count=1 -p 2 ./internal/voice/ ./internal/rpc/ ./internal/daemon/ ./cmd/meept/ green; gofmt/vet. Report the e2e run tail verbatim.
