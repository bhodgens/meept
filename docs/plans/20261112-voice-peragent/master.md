# Per-agent voice controls + backchannel soften (A/B) - master

> Worktree .hermes/worktrees/voice-peragent, branch plan/voice-peragent
> (off main f379bfab). Two leaves, parallel agents, DISJOINT file fences.

## Leaf 01 - per-employee voice acoustic controls (user directive)

Make every already-shipped + newly-added noise control configurable PER
AGENT via voice.employee_leg rows, extending the existing tri-state
override pattern (FreshSession/GuidedIntake pointers; row value > global):

Already global, now per-row:
- turn_confirm_ms, min_turn_words, filler_grace_ms, silence_prompt,
  silence_prompt_seconds, silence_hangup_after_seconds.

New controls (the agreed improvements to the word gate), all per-row +
global fallback:
- exempt_phrases []string: short-final gate never drops a stripped final
  whose text matches an exempt phrase (default shipped set: "stop",
  "cancel", "yes", "no", "thanks", "thank you", "okay", "ok", "mm-hm",
  "mmhm", "uh-huh", "go ahead"). Case-insensitive, whole-residue match.
- energy_gate bool + min_rms int: the short-final gate ALSO requires the
  utterance's average mic RMS below min_rms (far-field/TV fragments are
  quiet); a loud near-field short utterance (you saying "Thank you.")
  passes. RMS measured child-side over the aggregated window's PCM
  (the STT adapter has the segment audio; carry max/mean into the gate
  decision WITHOUT new frame fields if the pipeline already sees it,
  else one additive frame field is acceptable - voiceframe additive
  optional keys are backward-compatible).
- fragment_decay_s int (default 0=off): after K consecutive gate-drops,
  raise the effective N by min(K, 3) for decay_s seconds (TV chatter
  detected -> stricter); any dispatched turn resets. Bounded, stateless-
  safe across calls.
- Contextual exemption (improvement #1): a short final arriving within
  reply_exempt_s (default 15, per-row) of THIS call's last spoken
  agent turn is a REPLY, not background - the gate skips it. The talk
  loop already knows turn-final times; carry a per-call timestamp into
  the child via a tiny keys-independent frame? NO - simpler: the gate
  already runs in the child; the CHILD knows its own say activity
  (speaking flag + last say write time). Use that (no new wire traffic).

Plumbing shape (follow the exact existing pattern):
- config: EmployeeLegConfig gains the pointer/typed fields (nil = inherit
  global; []string nil = inherit; bool via *bool).
- internal/voice/profile.go: VoiceProfile carries resolved values;
  ResolveProfile merges row > global (mirror FreshSession handling).
- internal/daemon/voice_keys.go sendKeysFrame: reads the resolved
  profile (it ALREADY calls ResolveProfile at :103) and sends the new
  selectors; keys wire fields additive.
- child: keys.go applyKeys -> pipeline fields (minTurnWords pattern).
- barge_in_rms rides the per-call OPEN frame (child-side per call) -
  profile-able too: add voice.barge_in_rms per-row override feeding the
  open-frame value (verify current construction point in local.go
  sendOpenFrame; the value is per-call already, so this is a
  resolution-source change, not a plumbing change).
- TalkLoop-side values (filler_grace_ms, silence_*): construction site
  voice_wiring.go ~:918 reads w.cfg globals; switch to the resolved
  profile for the call's employee (WireVoiceEmployee -> the loop's
  config). Verify where TalkLoopConfig is built per call and that the
  profile is reachable there.

Docs (mandatory, same commits): config/meept.json5 template comments for
every new/row key; docs/workflows/voice.md a "Per-employee voice
controls" table (key, type, default, global equivalent, one-line meaning
+ when to tune); internal/voice/AGENTS.md keys-frame bullet + a short
"per-employee acoustic controls" invariant paragraph; docs/reference/
generated/config.md via make docs-generate; voicechild.md gate section
update for exempt/energy/decay behavior.

FENCE (leaf 01): internal/config/schema.go, internal/voice/profile.go,
internal/voice/local.go (open-frame resolution only), internal/daemon/
voice_keys.go, internal/daemon/voice_wiring.go (TalkLoopConfig
construction site), internal/voicechild/keys.go, internal/voicechild/
pipeline.go (gate internals), config/meept.json5, docs listed above, +
their test files. MUST NOT touch: internal/voice/link.go,
internal/voice/talkloop.go (except NOTHING there - talkloop consumers
get values via TalkLoopConfig only), cmd/meept/*, e2e/*.

## Leaf 02 - #130 backchannel soften, A/B toggle (user: keep #129 open)

voice.backchannel_soften bool (default FALSE = today's behavior,
byte-identical coordinator) + per-row override (*bool). When ON:
- tail pad 250 ms -> softTailPad (const 700 ms inside link.go,
  documented);
- fallback margin 12 s -> softMargin 15 s;
- NEW: floor release when the HOLDER is mid-reply (a reDispatched chain
  or an unfinalized say queue) DEFERS the peer flush only if the parked
  prompt is itself a SHORT backchannel (<=2 words - reuse child-side
  gate semantics? NO - daemon side: word-count the parked text preview
  the HoldPrompt already carries); a full question still flushes on
  release as today. Effect: "mm-hm" reactions no longer interrupt the
  tail, while real questions keep latency.
- Backchannel replies are NEVER suppressed - they still dispatch and
  speak (operator requirement); only their timing vs the peer tail
  softens.

A/B validation (the operator asked for exactly this): with the toggle
off vs on, 90 s recorded link runs + scripts/voice-gap-analyze.py;
baseline exists (link-9ee64c5fe7d651e9: 11.5 s overlap / 120 s). The
AGENT delivers code + unit pins + the exact operator procedure; the
parent runs the A/B after merge/deploy (agent must NOT restart daemons).

FENCE (leaf 02): internal/voice/link.go (incl. the new LinkDeps.
BackchannelSoften field it consumes), internal/voice/link_test.go, and
internal/daemon/voice_link_rpc.go (the ONE line passing
w.cfg/s.vcfg.BackchannelSoften into LinkDeps - this file is leaf 02-only,
leaf 01 does not touch it). NO config-file/schema changes: the schema
field lands via leaf 01's pre-declared list; if leaf 01 hasn't landed it
when you start, read the value from a package var seam in link.go
(default false) + report the one-line wiring the parent adds at merge.
docs/workflows/voice.md belongs to leaf 01 ONLY; leaf 02 documents in
link.go comments + reports doc lines for the parent to paste.

## Pre-declared schema fields (leaf 01 implements ALL, including #130's)

EmployeeLegConfig + VoiceConfig (tri-state pointers where noted):
- BackchannelSoften *bool (global default false)
- ExemptPhrases []string (global default set above)
- EnergyGate *bool (default false), MinRMS int (default 0=unused)
- FragmentDecayS int (default 0=off)
- ReplyExemptS int (default 15)
- plus per-row copies of: TurnConfirmMs *int, MinTurnWords *int,
  FillerGraceMs *int, SilencePrompt string (row non-empty wins),
  SilencePromptSeconds *int, SilenceHangupAfterSeconds *int,
  BargeInRms *int (open-frame value).

## Verification gates (both leaves)

- go build ./... ; go test -count=1 -p 2 -timeout 600s ./internal/voice/
  ./internal/voicechild/ ./internal/config/ ./internal/daemon/; -race on
  voicechild+voice; gofmt/vet touched clean; make graphs + docs-check.
- Leaf 01 unit pins: profile resolution matrix (row > global > default)
  for every key; keys-frame additive wire test; gate behavior per
  control (exempt phrase passes; energy gate passes loud / drops quiet;
  decay escalates then resets; reply-exempt near agent speech passes);
  legacy byte-compat: zero config rows = today's exact behavior.
- Leaf 02 unit pins: toggle OFF = existing coordinator tests unmodified
  and green; toggle ON: pad/margin values apply, short parked flushes
  deferred during holder chain, full question still flushes on release.
