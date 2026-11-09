# Leaf 02 - non-speech filter + self-interrupt evidence fix

## Defect contracts (live call local-2d84c384fd93f447, 2026-11-09)

A. whisper's no-speech markers became USER prompts: transcript rows show
   "[BLANK_AUDIO]" (three times) and "(sighs)" answered by real turns
   ("I see you've sent a blank audio signal..."). consumeTranscripts drops
   only text == "".
B. The RMS barge-in gate (rmsSttEvidenceWindow = 10s, set by ANY final
   transcript) re-authorizes interrupts from STALE evidence: during a long
   in-flight turn, a final that already dispatched THAT turn authorizes
   another trip, cancelling the turn it produced (five barge-in kills in
   the call; near-duplicate answers in the transcript).

## Implementation

1. Child-side non-speech filter (internal/voicechild/pipeline.go,
   consumeTranscripts): before dispatching any final, drop transcripts that
   are only whisper's non-speech markers - exact set: [BLANK_AUDIO],
   (sighs)/(sigh), <blank_audio>, "(background noises)" variants
   (case-insensitive, trimmed, a named func nonSpeechTranscript(text) bool
   + table test). Keep partials passing as preview. Also collapse a final
   IDENTICAL to the last final within the evidence window (whisper repeats
   on re-voicing tails) - dedupe on exact text match, named + tested.
2. Evidence freshness (same file): lastFinalAt is CONSUMED by the
   RMS trip it authorizes: on an authorized trip, clear lastFinalAt (single
   use). New speech sets it again via consumeTranscripts. This makes "any
   loud sound during the 10 s tail of a dispatched utterance" stop
   re-tripping while genuine new utterances still interrupt correctly.
3. Talk loop (internal/voice/talkloop.go): a NEW final prompt that arrives
   while the SAME text's turn is in flight is a repeat - ignore exact-duplicate
   consecutive finals (guard already exists? verify before adding; if the
   child dedupe covers it, no daemon change - prefer one choke point:
   child-side). Document the decision in code.

## Tests (internal/voicechild, pipeline_rmsgate style fixtures)

- nonSpeechTranscript table: [BLANK_AUDIO], (sighs), "[blank audio]",
  plain "Hello" NOT dropped.
- final "[BLANK_AUDIO]" through consumeTranscripts -> zero prompt frames.
- duplicate final within window -> one prompt frame, not two.
- authorized RMS trip clears evidence: second trip without a new final is
  suppressed (extend TestRMSTripWithRecentSTTEvidence with a second trip).

## Gates
go test -count=1 ./internal/voicechild/ ./internal/voice/ green; gofmt/vet.
Depends on leaf 01 landing (same files touched - SERIALIZE after 01 commit).
