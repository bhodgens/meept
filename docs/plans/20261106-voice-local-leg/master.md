# Voice local leg plan tree - master

> Root orchestrator. Dispatches leaves, reviews in-session (main model),
> commits after review. Source spec: docs/workflows/voice-sidecar.md (rev 2)
> as AMENDED by this tree (leaf 12 writes the amendment into the spec).
> Prior tree (implemented): docs/plans/20261102-voice-sidecar/.

## Meta

- Tree: docs/plans/20261106-voice-local-leg/ (flat, repo convention)
- Execution backend: direct dispatch (delegate_task), isolation worktree
- Parent invariant doc: internal/voice/AGENTS.md (update via leaf 12)
- User decisions baked in (2026-11-06):
  - Phone (Telnyx) and local voice enabled CONCURRENTLY, not alternatively.
    Different employees use different legs for different work.
  - The outbound voice path is per-call configurable: the calling agent
    selects the leg (phone|local/direct-TTS) and the TTS voice.
  - Mic capture reuses the existing ffmpeg-recorder pattern (internal/stt);
    no new audio capture library.
  - Local session number shape: "local:<hostname>" (label override allowed).
  - Providers for the local leg: local whisper.cpp STT + local piper TTS as
    SUBPROCESSES (GPL as subprocess is allowed; never vendored/linked).

## RELATIONSHIP TO THE TIER A TREE (frozen split; do not duplicate)

A parallel plan tree exists: docs/plans/20261106-voice-tierA-qwen3-tts/
(branch plan/voice-tier-a, all leaves PENDING at authoring time). It owns,
and THIS tree must reuse, not rebuild:

- tierA C1: extended raw `keys` frame carrying stt_vendor/stt_model/
  stt_base_url/tts_vendor/tts_model/tts_base_url/tts_voice (the per-call
  voice/model selection wire the user asked for).
- tierA C2/C3: providers.ProviderConfig {Vendor,Model,BaseURL,Voice,
  SampleRate,Language} + factory signatures `STT/TTSFactory(cfg, key)`.
  THIS tree's provider selection rides ProviderConfig - no new profile
  struct.
- tierA leaf 03: daemon emits the keys frame in routeFrame EventSetup
  (after talk-loop registration), secrets via Broker.
- tierA leaf 05/06: pumpAudioIn -> STT.Stream -> prompt frames; STT-
  partial barge-in trigger (their D1 decision replaces ONNX VAD for
  Tier A phone legs).

What tierA does NOT close (verified from its leaf texts): nothing creates
the child pipeline in production. Its emission point is routeFrame
EventSetup, but EventSetup arrives from the child, and nothing in
production makes the child build the pipeline and send setup. THIS tree
supplies that missing trigger: the C1 `open` frame (CL1 below), sent by
the daemon right after the AudioPair exists. Order becomes: open -> child
creates pipeline + dials pair + sends setup -> daemon registers loop ->
tierA sends keys -> child builds real providers. Neither tree breaks the
other.

Dependency: dispatch this tree's waves B+ only after tierA is COMPLETE (or
re-dispatch tierA's PENDING leaves first). Waves A of both trees are
independent and may interleave. This tree's leaf 02/03/04 are deltas
against tierA, sized accordingly.

Remaining gap this tree still owns for barge-in: tierA D1 triggers on STT
partials; whisper (leaf 10, segmented adapter) emits finals only, and the
local leg needs barge-in with local STT -> leaf 02 keeps the RMS-VAD
trigger, gated to Local==true only (phone legs keep tierA's partial
trigger; RMS never runs there).

## RECONCILE BEFORE DISPATCHING ANY LEAF

The parallel work IS the tierA tree above (docs/20261106-voice-tierA-
qwen3-tts/, branch plan/voice-tier-a, all leaves PENDING at authoring
time; small uncommitted test-hardening diffs to internal/voicechild/
*_test.go and internal/voice/daybrief.go also sit in the working tree).
Before wave A:

1. `git log --oneline -10 -- internal/voice internal/voicechild cmd/meept-voice`
   and `git status --porcelain` against the live tree.
2. Re-verify each finding F1-F4 below against current HEAD. A finding that
   is already fixed on HEAD marks its leaf SUPERSEDED (or narrows it per the
   reconciliation rules); do not re-implement landed work.
3. Record every SUPERSEDED/NARROWED verdict in the tracking table with the
   commit SHA that landed the work.

## Goal

Add a LOCAL voice leg to the voice sidecar: a real-time voice conversation
with an employee that uses local client audio (mic + speaker) instead of a
Telnyx phone call. Same child, same talk loop, same supervisor gate, same
session/binding/day-brief machinery. Purposes:

1. Local testing of the whole voice stack without outbound calls or cloud
   STT/TTS.
2. Agent interaction over voice for any same-user client (CLI now, TUI as
   part 2).
3. Concurrent legs: Telnyx stays fully functional; the two legs share one
   child, one text server, one registry, one call store.

## Findings this tree fixes (verified 2026-11-06, file:line)

- F1: internal/voicechild/pipeline.go pumpAudioIn() reads daemon->child PCM
  and drops it; no STT feed in production ("Stub STT consumes nothing yet").
- F2: no production caller of STTProvider.Stream; transcripts never emitted
  (emitPrompt has test callers only).
- F3: noteBargeIn() has no production caller; interrupt never fires from
  real audio.
- F4: real providers are constructed only in handleKeys (providers_wiring.go);
  no daemon-side producer of the keys frame exists, and nobody sends the C1
  setup frame in production. The real child runs on stubs forever.
  (tierA leaves 02/03 fix the keys half; F4's SETUP-producer half is THIS
  tree's leaf 03/04 - nothing else ever makes the child build a pipeline.)
- F5: cmd/meept-voice/main.go hardcodes deepgram/openai vendors; no config
  or per-call provider selection. (tierA D4/D5 + its leaf 04 land the
  wire; THIS tree's leaf 04 makes the content per-call/per-agent.)
- F6: e2e/suites/voice-lifecycle names its full-loop skips on exactly these
  gaps.

## Interface Contracts (frozen; leaves must not paraphrase)

### CL1 C1 frame additions (internal/voiceframe/frame.go, additive only)

New event constant: `EventOpen = "open"` (daemon -> child, per call, sent
right after the AudioPair exists, for BOTH legs). The child creates the
pipeline, dials the C2 audio pair, stores the call identity, and answers
with the existing `setup` frame (C1 setup unchanged; the daemon's routeFrame
already arms the talk loop on setup; tierA then sends keys there).

New Frame fields (all `omitempty`, zero value = today's behavior
byte-exact):

```
Local      bool   `json:"local,omitempty"`
BargeInRms int    `json:"barge_in_rms,omitempty"`
```

Open frame shape (daemon->child, phone leg AND local leg). Identity fields
reuse the existing Frame fields from/to/direction/codec/sample_rate - the
open line just sets them:

```
{"call_id":"<ccid>","event":"open","from":"<id>","to":"<id>",
 "direction":"inbound|outbound|local","codec":"L16","sample_rate":16000,
 "local":true,"barge_in_rms":3000}
```

Rules: vendors/models/voices NEVER ride the open frame - they ride the
tierA keys frame (ProviderConfig wire). The open frame carries only call
identity + leg marker + the RMS threshold the local leg needs before any
keys arrive. Existing wire tests (byte-exact prompt/say lines) must still
pass.

### CL2 Child provider construction (internal/voicechild)

- tierA owns the vendor wire: keys frame + ProviderConfig + factory
  signatures. THIS tree adds NO profile struct and NO factory change.
- `open` stores only identity + local bool + bargeInRms on the pipeline.
- constructProviders timing (tierA behavior, unchanged): keys frame at
  EventSetup constructs providers; local vendors ride the SAME keys frame
  with empty keys (tierA D2 keyless rule; whisper/piper need no key).
- What THIS tree changes at tierA's emission point (leaf 04, delta): the
  keys-frame content becomes PER CALL instead of global - the daemon
  resolves a voice.Profile (leg + per-employee defaults + the voice_call
  tool's `voice` arg) and maps it onto the tierA keys-frame fields
  (stt_vendor/stt_model/tts_vendor/tts_model/tts_voice; tts_base_url from
  config). Precedence per CL4. Local vendors construct with "" keys
  (tierA D2). Missing cloud key still fails THAT call only.
- cmd/meept-voice's hardcoded deepgram/openai closures: tierA leaf 04
  makes factories dispatch on cfg.Vendor - verify at dispatch; if landed,
  F5 is done.

### CL3 Child audio loop (F1/F2/F3 closure, internal/voicechild/pipeline.go)

- F1/F2 (pumpAudioIn -> STT.Stream -> prompt frames) are OWNED by tierA
  leaf 05; this contract restates the PASS state for reviewers only.
  Send failures drop the transcript and log (never block).
- Barge-in trigger matrix: phone leg = tierA leaf 06 STT-partial trigger
  (their D1; the ONNX probe stays non-fatal there); local leg = RMS VAD,
  because the whisper adapter (leaf 10) emits finals only, so its partials
  can never trip the tierA latch. While p.speaking && Local, playback()
  computes RMS of each outgoing 640 B s16le chunk; sustained RMS >
  BargeInRms across a 200 ms window (10 chunks) triggers noteBargeIn()
  and one interrupt frame per barge-in (F3). RMS never runs when
  Local==false.
- No new dependencies; energy VAD is ~30 lines of integer math.

### CL4 Leg + voice selection (config -> frame -> tool)

Config (all under `voice`, additive, defaults preserve today byte-identically):

```
default_leg        "phone"          // phone | local
tts_model          ""               // global TTS model (openai voice id etc.)
tts_voice          ""               // global TTS voice
stt_model          ""
local.enabled      false            // local leg master switch (opt-in)
local.stt_provider ""               // default "whisper"; empty = voice.stt_provider
local.tts_provider ""               // default "piper"; empty = voice.tts_provider
local.tts_model    ""
local.tts_voice    ""
local.barge_in_rms 3000             // s16 RMS threshold, local leg only
local.name         ""               // session label; empty = hostname
employee_leg       []                // per-agent defaults (OQ4):
  // {employee, leg, tts_voice, tts_model} so different employees default
  // to different legs/voices with no tool arg (the concurrent-legs case)
```

Selection precedence (frozen):
  TTS voice/model: voice_call arg > voice.employee_leg[employee] >
  per-leg config (voice.local.* for local, voice.* for phone) >
  provider default.
  Leg: voice_call arg > voice.employee_leg[employee].leg >
  voice.default_leg.
LLM pin unchanged (voice.voice_model + employee Model - do not conflate
the LLM model with the TTS voice; tierA already separates them).
"local" == the direct-TTS leg: agent speech is synthesized and played on
the client device; no Telnyx anywhere on the call.
Resolution happens in ONE place: voice.ResolveProfile(cfg, employeeID,
toolVoiceOverride, leg) -> Profile{SttVendor,SttModel,TtsVendor,TtsModel,
TtsVoice,BargeInRms} (leaf 04), consumed by BOTH legs' emission sites.
tierA's global emission call site becomes
ResolveProfile(cfg, employeeID, "", "phone") and gets today's values
back, so tierA behavior is byte-identical.

### CL5 Local leg lifecycle (internal/voice/local.go, new)

```
type LocalLeg struct{ /* registry, child, textServer, manager, binding deps */ }
func NewLocalLeg(cfg LocalLegConfig, deps LocalDeps) (*LocalLeg, error)
// Open registers the call and returns the audio pair paths for the client.
func (l *LocalLeg) Open(ctx context.Context, employeeID, label string) (LocalCall, error)
type LocalCall struct { CallID, SessionID, InPath, OutPath string } // <ccid>.in/out
func (l *LocalLeg) End(ccid, reason string)
```

Flow (Open):

1. Gate: voice.enabled && voice.local.enabled; employee resolves; else
   refuse with a named error. ccid = "local-" + pkg/id.Generate().
2. target = "local:" + label (label = cfg.Name or os.Hostname()).
3. registry.Acquire(ccid, employeeID, target, leg="local") (CL6).
   Local legs DO skip effects reservation and the dial audit row (no paid
   side effect, no external party) - this difference is deliberate and
   pinned by a test.
4. AudioPair opened in local mode (one listener each, one accept each).
5. Manager.TrackOutbound(ccid) so FailOutcome/WaitCall/release fan-out
   work unchanged.
6. Send open frame (CL1, local:true, barge_in_rms), then the keys line
   built from ResolveProfile(...) through the same sender seam tierA leaf
   03 adds (the local leg emits its own keys line - same wire shape,
   different trigger site; byte-exact fields).
7. Run LocalSplice (CL7) in a goroutine.
8. binding.OnCallStart (direction "local") + day brief at the SAME site the
   phone path does it - leaf 07 must grep for the OnCallStart call site and
   mirror it (direction "local", number "local:<label>").
9. Teardown: any terminal event -> Manager.FailOutcome(ccid, outcome,
   reason); the existing release fan-out (childproc note, loop cancel,
   registry release, binding end) is unchanged. No carrier POST anywhere.

### CL6 Registry leg awareness (internal/voice/dial.go)

- Instance gains `Leg string` ("phone"|"local", zero "phone" for
  compatibility of composite literals in existing tests).
- Acquire normalizes target ONLY for leg "phone" (local targets contain
  colons; NormalizeE164 must never mangle "local:...").
- Employee-one-live-call and cap rules unchanged (shared across legs).

### CL7 LocalSplice (internal/voice/localsplice.go, new)

Simpler sibling of splice.RunSplice for the local leg:

- child .out -> speaker sink, paced 20 ms per frame (same pacing rule;
  the client buffer underruns are the local crackle equivalent).
- mic source -> child .in, chunk-sized 640 B L16@16k. No reorder window
  (unix sockets preserve order). No transcode (L16 only; the local leg
  negotiates L16@16000 only and REFUSES anything else).
- barge-in: interrupt for THIS ccid -> clear to child + CancelDraftFn +
  stop writes; no Telnyx clear-frame write (there is no carrier).
- backpressure: socket write error ends the leg with a named error.

### CL8 Control plane for clients (RPC + CLI)

- RPC methods (internal/rpc/voice_local.go, pattern: RegisterHandler):
  `voice.local.open {employee_id,label}` -> {call_id, in_path, out_path,
  session_id}; `voice.local.end {call_id}`; `voice.local.status {}` ->
  live local calls (call_id, employee_id, since).
- Audio never rides RPC: the client dials the C2 unix pair directly
  (same-user trust, same model as the RPC socket).
- CLI: `meept voice local --employee <id> [--in <wav>] [--out <wav>]
  [--list]`: raw s16le 16k between ffmpeg avfoundation capture (mic) /
  internal/tts player (speaker) and the pair. `--in/--out` file mode makes
  a scripted, mic-free, deterministic leg for CI and demos.
- `meept voice status` shows live calls for BOTH legs (registry Live()).

### CL9 e2e + un-skip

- New suite e2e/suites/voice-local + manifest path_map entry (C10 rule).
- voice-lifecycle: the named skips whose blocker was "no setup producer /
  stub child TTS" become runnable with the real child + fake Telnyx +
  local-leg-shaped fake client. Skips whose blocker was live model turns
  stay skipped honestly.

## Child Index

| # | Leaf | Files (primary) | Depends on | Wave |
|---|------|-----------------|-----------|------|
| 01 | Frame (open/local/rms) + config schema incl. employee_leg + template + configui | internal/voiceframe/frame.go, internal/config/schema.go, config/meept.json5, internal/configui/sections_voice.go | none | A |
| 02 | Local-leg RMS barge-in (tierA owns STT feed/prompts) | internal/voicechild/pipeline.go (+tests) | 01,03 (+tierA 05/06) | B |
| 03 | Child open handler (pipeline create, pair dial, setup answer) | internal/voicechild/child.go, pipeline.go fields (+tests) | 01 (+tierA 02) | B |
| 04 | Daemon open producer + per-call profile resolution at tierA's keys site | internal/daemon/voice_wiring.go, internal/voice/frame.go, internal/voice/profile.go (new resolver) | 01 (+tierA 03) | B |
| 05 | LocalSplice + AudioPair local mode | internal/voice/localsplice.go, internal/voice/socket.go (+tests) | none | A |
| 06 | Registry leg field + local target passthrough | internal/voice/dial.go (+ fakes in dial_test.go, voice_wiring_test.go) | none | A |
| 07 | LocalLeg lifecycle (open/end/day brief/release wiring) | internal/voice/local.go (+test), internal/daemon/voice_wiring.go glue | 02,03,04,05,06 | C |
| 08 | RPC control plane + `meept voice` CLI | internal/rpc/voice_local.go, cmd/meept/voice.go, components registration | 07 | D |
| 09 | voice_call tool: leg + voice args | internal/tools/builtin/voice_call.go (+test), toolprov factory glue | 07 | D |
| 10 | Local providers: whisper STT + piper TTS subprocess adapters | internal/voicechild/providers/stt_whisper.go, tts_piper.go, providers.go (+tests) | 01 | B |
| 11 | e2e suite voice-local + manifest + honest un-skips | e2e/suites/voice-local/*, e2e/manifest.json, voice-lifecycle skip edits | 07,08,10 | E |
| 12 | Docs + AGENTS.md + spec amendment + graphs | docs/workflows/voice-sidecar.md, docs/workflows/voice.md, internal/voice/AGENTS.md, root AGENTS.md | 11 | E |
| 13 | (Part 2, optional) TUI voice session command | internal/tui (command + audio shim) | 08 | F |

Concurrency: waves A and B parallelize within the wave. Serial rules:
- leaf 04 then leaf 07 (both edit internal/daemon/voice_wiring.go)
- leaf 07 then leaves 08/09 (wiring + tool depend on the leg API)
- components.go edits appear ONLY in leaf 08 (serialize against any other
  session touching components.go first)
- leaf 12 after everything (docs cite landed code)

## Dispatch Protocol

Per child, in order:

1. Dispatch the implementation leaf via delegate_task. Context MUST carry:
   leaf path, this master's relevant CL contract VERBATIM, the findings
   list if the leaf fixes one, repo rules (GOTOOLCHAIN pinned by Makefile;
   go test -p 2; ASCII-only new files - grep for em-dashes before
   finishing; no ignored errors `_, _ =`; every Set* nil-guarded; typed-nil
   guard on concrete-pointer into interface field; unix socket paths under
   the 103-byte sun_path limit; jargo import pins stay green), the exact
   verify commands, and: "Do NOT commit. Do NOT run git add. Do NOT touch
   files owned by other leaves (named in the leaf's Meta). Explore with
   search_files or terminal cat, never read_file. Do NOT read back files
   you just wrote."
2. Review IN-SESSION (main model, never a delegated reviewer): read the
   diff, run the leaf's verify commands, check CL contracts byte-exact,
   check for artifacts (TODO/FIXME/debug prints), U1000 sweep on staged
   package, `grep -rcE '^\s+[0-9]+\|' <touched files>` == 0.
3. On gaps: re-dispatch with specific feedback, reset status IN_PROGRESS.
   Max 3 iterations, then finish in-session.
4. Commit: stage EXPLICIT file paths (never a package dir - sibling WIP),
   message references the leaf doc, verify with git show --name-only.
   NEVER commit files the parallel session owns (check git status first).
5. Update the tracking table after every transition.

## Coding Conventions

- Go, gofmt, GOTOOLCHAIN via Makefile. Tests: `go test -p 2 ./<scope>/...`.
- voiceframe stays stdlib-only; jargo stays child-only (import_test.go,
  pin_test.go must stay green in every child-side leaf).
- New frame keys byte-exact per CL1; a wrong key silently breaks the child.
- Errors wrapped %w; no bare panic; pkg/id.Generate() for ids; no
  os.Getwd fallbacks; config paths via config.MeeptPath.
- Secrets: cloud keys still only via the keys frame into child memory.
  Local providers need NO keys - whisper/piper take no key.
- Local leg NEVER calls any telnyx.* function or Broker.Value("telnyx_api_key").
- ASCII only. UI text lowercase.

## Completion Tracking Table

| # | Leaf | Wave | Status | Notes |
|---|------|------|--------|-------|
| 01 | frame-config | A | DONE cb8c8672 | gates re-run by orchestrator |
| 02 | local-rms-bargein | B | PENDING | tierA 05/06 land first |
| 03 | child-open-handler | B | PENDING | F4 setup-producer half |
| 04 | open-producer-profile | B | PENDING | serialize before leaf 07 |
| 05 | local-splice | A | DONE 57abc139 | orchestrator fixed clear-frame newline |
| 06 | registry-leg | A | DONE a72d293f | 4-arg shape = AcquireLeg (Go: no overload) |
| 07 | local-leg | C | PENDING | |
| 08 | rpc-cli | D | PENDING | only components.go toucher |
| 09 | tool-leg | D | PENDING | |
| 10 | local-providers | B | PENDING | GPL subprocess rule |
| 11 | e2e | E | PENDING | manifest same commit |
| 12 | docs | E | PENDING | AGENTS.md same commit |
| 13 | tui-voice (part 2) | F | PENDING (optional) | parity deviation documented |

## Integration Test Plan

After wave B (child side): unit - a fake STT fed through the C2 .in socket
produces prompt frames with last:true on the text socket, and say->playback
during Local==true with injected loud input produces an interrupt frame.

After wave C: scratch daemon with voice.local.enabled=true, whisper/piper
present: `meept voice local --employee <id> --in ref.wav --out out.wav`
runs a scripted two-turn conversation end to end (final prompt -> employee
turn -> supervisor gate -> say -> audible PCM in out.wav). out.wav non-empty
and the call row + transcript land in calls.db. This is the acceptance
drill for the whole tree.

After wave E: `make e2e-fast-area AREA=voice-local` green; voice-lifecycle
skips only remain for the documented live-model reasons; `make graphs-check`
fresh; `make lint-ci` findings attributable to this tree at zero.

## Review Checklist

- [ ] CL1 frame keys match the contract character-for-character
- [ ] Existing C1 wire tests still byte-exact (child_test.go prompt pins)
- [ ] local=false paths behave byte-identically to today (no config, no
      local.enabled -> phone-only behavior)
- [ ] Local leg never touches telnyx client or telnyx secret
- [ ] Registry employee-mutex + cap rules hold ACROSS legs
- [ ] keys frame still required for cloud vendors; local vendors construct
      at open without keys
- [ ] No TODO/FIXME/debug artifacts; U1000 clean
- [ ] e2e manifest path_map updated with any new package dir
- [ ] Docs + AGENTS.md updated in the same commit as behavior (leaf 12)

## Open Questions

- OQ1 (part 2 scope): TUI push-to-talk (leaf 13) ships WITHOUT Flutter GUI
  parity, deviating from the parity rule with justification: the voice
  control surface was already deferred phone-side (spec Open points), and
  audio routing in the Flutter web target is a separate project. Confirm
  the deviation, or cut leaf 13.
- OQ2: local leg max simultaneous sessions - recommend the shared
  voice.max_concurrent_calls cap (no separate knob). Confirm.
- OQ3: the tierA tree is all-PENDING on branch plan/voice-tier-a.
  Recommend executing tierA leaves 01,02,03,05,06 FIRST (they are this
  tree's foundation), then this tree. This tree's wave A (leaves 01,05,06)
  may run in parallel with tierA waves - no file overlap. Running this
  tree first forces leaf 04 to author the keys frame itself: duplicate
  work against a live plan.
- OQ4: per-agent leg defaults - a voice.employee_leg config list (recommend:
  simple, config-UI-able, zero package coupling) vs fields on the employee
  definition/constitution (cleaner agent-profile home, but touches
  internal/employee + constitution schema + RPC shape). Confirm.
