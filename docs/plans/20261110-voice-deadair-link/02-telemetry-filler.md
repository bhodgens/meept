# Leaf 02 - turn telemetry, transcript audit rows, delayed filler

## Contracts

A. TELEMETRY (why did the agent talk?): structured slog at component=voice
   sub=talk, every line carries ccid + turn seq:
   - prompt dispatched: kind=prompt_dispatch seq=N text_preview<=40ch
     words=N (word count of the REAL text after guard strip - recompute
     cheaply or accept raw count).
   - draft cancelled: kind=draft_cancel seq=N reason=barge-in|supersede|teardown.
   - filler spoken: kind=filler line="…" after_ms=N (only fires post-grace,
     see C) OR kind=filler_skipped (turn answered inside grace).
   - sentence states: reuse existing supervisor logs; ADD kind=sentence
     allow|deny|fallback seq=N idx=I.
   - turn complete: kind=turn_final seq=N reply_words=N ms=N.
   - reply into transcript (binding.go already appends user/assistant rows
     at :306-308 - keep) PLUS a system row for guard drops:
     b. GUARD AUDIT: the CHILD's dropped-final count + last reasons
     (leaf 01 drops) are logged child-side; the DAEMON adds one INFO line
     per turn dispatched carrying the same seq the child would need -
     keep it simple: daemon logs dispatch (a), child logs drops (leaf 01
     already required). calls.db sees: user/assistant rows (existing) +
     NO rows for bracket-only finals (they never dispatch) - the loop
     becomes invisible BY DESIGN; the "why" questions are answered by the
     logs above. Add nothing to calls.db for drops (transcript is the
     conversation, not the reject firehose).
   C. DELAYED FILLER (user: "'give me a second on that' often, often
   immediately"): today talkloop.go:455 speaks filler IMMEDIATELY on every
   final prompt (spawnPipeline path). Change: arm a time.AfterFunc grace
     (voice.filler_grace_ms, config, default 2500); fire filler ONLY if the
     turn has still not produced its first allowed sentence. Cancel the
     timer when: first sentence goes out (playVerdict allow), the draft is
     cancelled, or the turn completes. Round-robin fillerLine() unchanged.
     Silence-prompt filler (talkloop.go:393, the 45 s dead-air nudge) keeps
     firing immediately (that IS the point there).
   Implementation notes: TalkLoopConfig gets FillerGrace time.Duration;
     schema.go voice block gets filler_grace_ms int (default 2500; comment
     in config/meept.json5 template); wire in internal/daemon
     (voice_wiring.go constructs TalkLoopConfig - find the construction
     site and extend). seq counter: atomic on TalkLoop, incremented at
     spawnPipeline, carried in a small struct alongside t.gen (existing
     pipeline struct gains seq int).

## Tests

- slog test-handler style used by existing talkloop tests (check
  talkloop_test.go for the harness): prompt dispatch emits
  prompt_dispatch; answer-inside-grace emits filler_skipped and ZERO filler
  says; slow turn (fake Turns.Run sleeping > grace) emits filler after_ms
  ~= grace; cancel inside grace emits draft_cancel reason=supersede and no
  filler fires late (timer stopped).
- config: schema round-trip for filler_grace_ms + template parse (mirror
  existing greeting/filler template tests - grep "greeting" in
  internal/config tests).

## Gates
go test -count=1 ./internal/voice/ ./internal/config/ green; gofmt/vet.
Touches: internal/voice/talkloop.go (+tests), internal/config/schema.go,
internal/daemon/voice_wiring.go, config/meept.json5. DO NOT touch
cmd/meept/voice.go or internal/voicechild (leaf 01 owns the latter; wave
B owns the former).
