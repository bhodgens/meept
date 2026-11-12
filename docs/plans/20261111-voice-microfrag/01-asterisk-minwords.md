# Leaf 01 - non-speech asterisk class + configurable min-words turn gate

## Defects (live call local-0627960006a7f3d9, 2026-11-11 21:17, background TV audio)

A. whisper emits asterisk-wrapped markers ("*unintelligible*", "*static*",
   "*music*", "*laughter*"). The dead-air class guard strips
   `[\[(]...[\])]` runs only, so a bare `*unintelligible*` final survived
   isRealSpeech and DISPATCHED seq=1 as a user prompt.
B. TV fragments dispatch real turns: 2-word tails ("The other",
   "This is...") every few seconds each start an employee reply. Aggrega-
   tion (900 ms) cannot merge them (they arrive 3-8 s apart) and word
   filters are wrong by default (operator commands like "Stop." are 1
   word). Root behavior is environmental (loud room), so the fix is an
   OPERATOR-TUNABLE gate, default off.

## Fixes

1. internal/voicechild/pipeline.go bracketedTokenRe: add the asterisk run
   `(?i)\*[^*\n]{1,60}\*` (merged alternation with the existing bracket
   class so nesting/unclosed rules carry over: a lone `*` with no closer
   keeps its residue REAL - errs toward hearing the caller). Extend
   TestIsRealSpeechClassTable: "*unintelligible*", "*static*", "*laughter*
   *applause*", "hey *music* what's that" keeps (mixed -> real); bare "*"
   real; "hello *" real.
2. voice.min_turn_words int (default 0 = disabled; schema + template
   comment: "drop final transcripts with fewer than N real words after
   non-speech stripping - tunes out background-fragment chatter in loud
   rooms; 1-word commands like 'Stop.' need N<=1 (default)"). Keys wire:
   min_turn_words selector (same shape as turn_confirm_ms: *int, absent ->
   0 legacy). Child gate in consumeTranscripts FINAL branch AFTER
   isRealSpeech, BEFORE aggregation: wordCount(trimmed) < N -> INFO
   'voicechild: short final dropped' (words, preview) + continue (no
   dispatch, no evidence stamp). Count = whitespace-separated tokens of
   the STRIPPED text (brackets gone first).
3. Docs: voicechild.md + internal/voice/AGENTS.md one-line each; template
   key + comment.

## Tests
- class table pins (1).
- min-words: N=2 drops "The other" + "This is..." (as LIVE), keeps "Stop."
  when N=1; N=0 byte-identical legacy (existing pins unchanged);
  interaction with aggregation (a dropped short final must NOT seed the
  pending buffer); evidence/dedupe untouched (drops never stamp lastFinalAt).
- keys selector absent->0, explicit 2 works (mirror TestVoiceKeysEmission-
  TurnConfirmSelector).

## Gates
go test -count=1 -p 2 ./internal/voicechild/ ./internal/config/
./internal/daemon/; -race voicechild; gofmt/vet. Worktree
.hermes/worktrees/voice-microfrag ONLY, no commits.
