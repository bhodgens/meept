# Leaf 01 - paralinguistic dead-air guard (class-based, not list-based)

## Defect (pinned by live transcript)

whisper emits bracket tokens for non-speech sounds; leaf 02's
nonSpeechTranscript matches only an EXACT LIST, so the live call
local-376ee373022f7663 dispatched "(farting)", "(mouse clicking)",
"(squeaking)", "[clears throat]", "(inhales deeply)" as user prompts and
the companion answered each. The guard must classify by SHAPE.

## Contract

A FINAL transcript dispatches an employee turn only if it contains real
words OUTSIDE bracket artifacts. Definition:

1. Strip every maximal run matching one bracketed token:
   regex `[\[(][^\]\n]{1,60}[\])]` (case-insensitive) from the text.
2. Also strip whisper artifacts: exact "[BLANK_AUDIO]", "<blank_audio>",
   "you", "yeah" ONLY when the remaining stripped text is empty (do not
   strip fillers that coexist with real speech - "yeah how are you" is
   REAL).
3. Trim; remove punctuation-only residue (regex `^[\p{P}\p{S}\s]*$`).
4. If nothing remains -> NOT real speech: drop the final (no prompt
   frame, no evidence stamp) and COUNT it.

Keep: the existing exact-set fast path (nonSpeechTranscript), the
one-shot evidence consumption, the duplicate-final dedupe. The new class
check WRAPS them: isRealSpeech(text) bool, single choke point child-side
in consumeTranscripts' final branch (internal/voicechild/pipeline.go
~:366-390).

Partials: an all-bracket partial must NOT stamp barge-in evidence either
(check the partial branch: evidence stamp is finals-only today - verify
and pin it).

## Tests

- Table >=15 tokens: the 5 verbatim from the live transcript above, plus
  "(laughing)", "(crosstalk)", "(upbeat music)", "(silence)", "(wind)",
  "(bell ringing)", "[music]", "(applause)", "(gasps)", "(clears throat)",
  "(static)".
- Mixed keeps: "hey (crosstalk) what's that" REAL; "yeah" REAL (it's
  speech); "How's it going?" REAL; "(inhales deeply)" NOT; "[BLANK_AUDIO]"
  NOT; " ." NOT; "" NOT; "mmm" -> treat as REAL (it's a vocalization a
  human might intend; only bracketed/blank forms drop).
- Fixture: replay the 12-event local-376ee373022f7663 transcript through
  the classifier: exactly the 5 bracket turns drop, the 3 real user turns
  pass ("Still here.", "Yeah, how are you doing today?", plus check).
- consumeTranscripts with a bracket final emits ZERO prompt frames
  (existing frame-observe harness, driveFinal style).
- One INFO log line per dropped final naming the reason
  ("voicechild: non-speech final dropped", text-sha or short preview,
  call_id) - telemetry leaf 02 also counts these; keep the log at the
  child so link runs see it.

## Gates
go test -count=1 ./internal/voicechild/ green; gofmt/vet. Commit NOT
(orchestrator). Worktree only.
