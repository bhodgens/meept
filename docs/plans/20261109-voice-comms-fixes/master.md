# Voice comms fixes + companion refactor - master

> Root orchestrator. Dispatches leaves into the isolation worktree, reviews
> in-session, commits per leaf on the worktree branch. Built on the
> fresh-session tree (COMPLETE: leaves 01-04, commits c8650bc0, 6cf83a70,
> 90fc1778, f3037b09) and today's barge-in gate (4738bcfa).

## Meta

- Tree: docs/plans/20261109-voice-comms-fixes/ (this dir, worktree branch)
- Worktree: .hermes/worktrees/voice-comms-fixes, branch plan/voice-comms-fixes
- Evidence base: live call local-2d84c384fd93f447 (2026-11-09 14:54-14:57,
  daemon log /tmp/meept-real-daemon.log + calls.db transcript) analyzed in
  session; findings below are the defect contracts.

## Findings driving this tree (from the live transcript + log)

1. TALK-OVER: oto playback cannot be recalled - AudioPlayer.Play spawns an
   untracked oto player per burst; Stop() is a documented no-op
   (internal/tts/player.go:42-63). Combined with an unbounded CLI playout
   path (reads + per-read flush, cmd/meept/voice.go live loop ~:440-460),
   the unix-socket buffer holds up to seconds of audio after a barge-in.
   The transcript proves it: four near-duplicate capability answers within
   20 s, each cancelled turn's AUDIO still playing under the next.
2. NON-SPEECH PROMPTS: whisper's silence marker "[BLANK_AUDIO]" (real
   whisper.cpp output, not a constant in our code) and "(sighs)" were
   dispatched as user prompts; the agent answered "I see you've sent a
   blank audio signal". consumeTranscripts only drops text == ""
   (internal/voicechild/pipeline.go ~:312).
3. SELF-INTERRUPT: the RMS barge-in evidence window (rmsSttEvidenceWindow
   10 s, internal/voicechild/pipeline.go) is authorized by ANY recent
   final - including one already dispatched - so during long replies the
   agent's own tail (post-#1) or a prior utterance's final re-trips and
   cancels the in-flight turn. Five barge-in lines in the call.
4. REFLECTION 401 STORM: every post-turn reflection logs
   "classifier call failed: HTTP 401 ... Token not provided (request id:
   20261109...)" - the request-id shape is the AGNES cloud gateway, not the
   loopback prompt-router sidecar. Twelve in one call. internal/agent/
   reflection_collector.go:121 -> ClassifierClient (classifier alias:
   prompt-router head, rotating through members until agnes; every member
   failing locally = 401 at the cloud tail). Wastes rotation ladder + time
   per voice turn; pollutes logs.
5. COMPANION PERSONA MISSING: replies were the DEFAULT agent persona
   ("file management, code development" markdown lists), not the
   spoken-word companion. Definition must be refactored (user directive,
   below) AND the voice reply path must not emit markdown/agent-tool-list
   prose; the day-brief "[prior calls today]" injection also gets answered
   AS a request ("I don't see any prior work completed...").

## Companion refactor (user directive, contract)

- Helpful + KNOWLEDGEABLE ABOUT WHAT THE OPERATOR HAS BEEN WORKING ON.
- STARTS THE CONVERSATION: first utterance after the greeting is a
  "How can I help?"-class offer (persona prompt; greeting config already
  exists: voice.greeting / employee-aware later if needed - OUT of scope:
  greeting stays global config).
- ZERO system-agent tools: no file/shell/web/read of the workspace; tools
  array restricted to READ-ONLY SELF/PLATFORM REPORTING so it can truthfully
  answer "what have you been doing / what's running": the implementer picks
  from the EXISTING builtin registry (candidates to verify: memory_search,
  skills_list, agent/platform read-only status tools, activity overview
  tool if registered - grep internal/tools/builtin for Name() of read-only
  status/report tools). NOTHING that writes/executes/dials.
- Constitution: tier_2_propose stays; tools_forbidden keeps voice_call +
  adds every mutating tool; risk ceiling low; spoken-only charter text
  updated. Escalation triggers kept.

## Leaves

| # | Leaf | Files (primary) | Depends on | Wave |
|---|------|-----------------|-----------|------|
| 01 | recallable playback: player cancel + bounded CLI playout | internal/tts/player.go, internal/voice/clientaudio.go, cmd/meept/voice.go | - | A |
| 02 | non-speech filter + self-interrupt evidence fix | internal/voicechild/pipeline.go, internal/voice/talkloop.go | 01 | B |
| 03 | reflection-401: root-cause the rotation, repoint reflection off the cloud tail | internal/agent/reflection_collector.go, internal/daemon/components.go wiring, ~/.meept/models.json5 (live, orchestrator-side) | - | A |
| 04 | companion employee refactor (template + persona test) | config/employees/companion.json5, internal/daemon/voice_persona_test.go | - | A |
| 05 | docs + integration gates + live re-hire/verification | docs/workflows/voice.md, voice-sidecar.md, internal/voice/AGENTS.md | 01-04 | C |

## Verification gates

- go test -p 2 ./internal/tts/ ./internal/voice/ ./internal/voicechild/ ./internal/agent/ ./internal/daemon/ ./cmd/meept/ green per wave-touch.
- Leaf 03 acceptance: a live voice turn produces ZERO new "reflection failed"
  401s in the daemon log; reflection still runs (log evidence of a
  successful reflection OR a named non-401 outcome).
- Leaf 05: operator live call - single greeting, no double-answer overlap,
  silence does not prompt replies, companion answers "what am I working
  on" from read-only platform state. (Orchestrator runs; user ear-check.)
- e2e: extend voice-local suite where seams allow hermetic proof (02
  filter test is unit-level in voicechild; 01 cancel test in tts pkg with
  a fake player or real-oto skip on CI headless).

## Sibling-collision notes (checked before dispatch)

- Main checkout has ACTIVE sibling waves: internal/agent/delegate*,
  gate_park_wiring, mcp/proxy/bus/tui files dirty. The worktree isolates
  this tree; NEVER touch those paths; internal/agent/ edits in leaf 03 are
  scoped to reflection_collector.go only (delegate* untouched).
- voicechild/pipeline.go: last landed by our own commits; no sibling edits
  known. binding.go fresh-session seam landed (6cf83a70); do not regress.
- ~/.meept/models.json5 is USER-owned config (a sibling patched it once
  today). Leaf 03 reports the needed config change; orchestrator applies
  it and says so.

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| 01 | PENDING | |
| 02 | PENDING | |
| 03 | PENDING | |
| 04 | PENDING | |
| 05 | PENDING | |

Statuses: PENDING -> IN_PROGRESS -> REVIEWED -> DONE | BLOCKED.
