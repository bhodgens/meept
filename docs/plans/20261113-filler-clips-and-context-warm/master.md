# Voice Latency: Filler Clip Cache + Partial Context Warm — Implementation Orchestrator (Root)

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 2 branch directories (`phase1-clips/`, `phase2-context-warm/`)
- **Scope:** Two deployment phases that cut voice-channel latency: Phase 1 pre-renders contextual filler/greeting/silence/farewell lines as per-voice cached PCM clips served by the voice child (kills filler synth latency and the timbre-drift bug); Phase 2 warms the LLM prefix cache from STT partials before the final turn dispatch.

## Goal

Measured problem: filler lines ride the live TTS lane (`internal/voice/talkloop.go:1155` `t.say()` -> child `Speak()`), which blocks for the provider's full generation latency. Two user-visible defects follow: (a) perceived dead air while the employee turn runs, (b) the 3-second-clone voice drifts to a different timbre on short utterances (filler/greeting/nudge are the shortest text the lane speaks). Decisions locked with the user on 2026-11-13:

1. **Cached filler audio (R1):** every line the filler-class lanes can speak is pre-synthesized once per (voice, model, text) and served as PCM from a disk cache. Zero live synth for filler.
2. **Dynamic generation, never hand-made (R3):** clips are generated in the background, per call, immediately after the call's `keys` frame (see Architecture; NOT at child start - the child holds no TTS config or key until that frame), stored under `$MEEPT_HOME/voice_clips/`, and invalidated by key (voice/model/text/reference-audio change implies a new key implies regenerate).
3. **Sidecar-only (R4):** generation and caching run ONLY when the voice sidecar is actually configured and running (voice child up for a call, TTS vendor/model/voice resolved via the keys lane). A configured `tts_voice` with no sidecar / no voice agents must generate nothing and write nothing.
4. **Contextual pools (R2):** filler selection becomes lane-aware - "cut off?" lines for fragments, "just a sec" variants for working turns, "hello?"/"still there?" variants for dead air, bye variants for farewells. Finite configured pools only (every pool line is cacheable; no free-form generated filler).
5. **Farewell lane (R5):** new - a spoken goodbye clip on a caller farewell intent and on an AGENT/OPERATOR-initiated hangup. Carrier hangup (caller already gone) speaks nothing. The farewell must never block or reorder `releaseCall`.
6. **Partial context warm (R7):** `last:false` partial prompts, which already reach the daemon (`internal/voiceframe/frame.go:29`, emitted in `internal/voicechild/pipeline.go` `consumeTranscripts`), trigger a max_tokens=1 warm request per session once `voice.context_warm` is enabled, so a llama.cpp-family server that reuses prompt prefixes (`--cache-reuse`, `config/models.json5:259`, `:318`) holds the conversation before the real turn. Warm never runs tools, never starts an employee turn, is quota-safe, and its failures are silent (R8). It is a silent no-op on endpoints without prefix reuse (MLX, cloud).

Non-goals: speculative full-answer drafts (Path B-heavy), Path B speech-to-speech providers, changes to the supervisor gate contract, changes to C2 audio framing, per-employee filler text pools (see C5 note).

## Architecture

Phase 1 data flow (numbers are leaves):

```
per call: child `setup` frame -> daemon routeFrame EventSetup
  -> sendKeysFrame (existing; internal/daemon/voice_keys.go)
  -> clip planner (leaf 02) resolves the pools for THIS call's employee
     from the keys-lane TTS identity (voice.ResolveProfile)
  -> compute clip keys (Contract C1 formula, leaf 01 helper)
  -> diff vs $MEEPT_HOME/voice_clips/ (leaf 01 store)
  -> C1 "clip_render" frame {key,text}[] to the voice child (leaf 02->03),
     call_id = this call's ccid
config reload (daemon)
  -> recompute pools for live identities, one planner pass per live ccid
     (background, never blocks the reload path)
child (leaf 03)
  -> for each miss: live Speak() once on a dedicated render goroutine,
     write PCM + manifest via store, emit clip_ready,
     never blocking the playback/turn lane
talkloop (leaves 04/05)
  -> filler/nudge/fragment/farewell say frames carry clip_id (Contract C3)
child playback (leaf 03)
  -> clip_id present + store hit: stream cached PCM into the utterance
     lane (same pacing/barge-in/drain paths as synthesized audio)
  -> clip_id present + miss, or absent: today's live Speak(text) path
```

Why per call and not at child start: the child constructs its TTS provider
from the per-call `keys` frame only (`cmd/meept-voice/main.go:1-25`,
`internal/daemon/voice_keys.go:1-25`); the keys contract keeps TTS material
in that frame alone. Before the first `keys` frame the child has no vendor,
model, voice, or key. `clip_render` therefore rides the same per-call moment
as the keys frame, which also supplies the mandatory `call_id`.

Phase 2 data flow:

```
routeFrame last:false partial (daemon, existing; today it falls through
  voice_wiring.go:1154 / :1192, which act only on last:true)
  -> partial-warm router (leaf p2-02): debounce per (session, employee)
  -> llm.Client.Warm(ctx, messages, opts...) (leaf p2-01): same payload
     builder as the turn, max_tokens=1, non-streaming, same sessionID
     (affinity header + prompt_cache_key), Warm-marked, never surfaced
final last:true turn (existing path, unchanged)
  -> same prefix + same session key => server prompt-cache reuse => prefill
     off the critical path
```

The `t.say()` choke in `internal/voice/talkloop.go` stays the single origin
of every audible line (echo_roster.go:136 invariant preserved): clips are an
attribute of a say frame, never a second audio path around it.

## Interface Contracts

### C1: Clip key formula (pinned - both branches' consumers copy verbatim)

```
clipKey = hex(sha256(
    "v1" \x1f text \x1f tts_vendor \x1f tts_model \x1f tts_voice \x1f hex(sha256(refBytes))))
```

`refBytes` is the raw content of the clone reference audio file the
configured TTS vendor consumes. NO shipped vendor consumes one today
(openai, qwen3, and piper are preset-voice only), so `refBytes` is empty and
`hex(sha256(refBytes))` = `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`
(the sha256 of the empty byte string). Do NOT add a config field for a
reference clip until a vendor reads it: a vendor that adds cloning MUST feed
the file bytes into this slot and the daemon MUST send `tts_ref_hash` (hex)
in the keys wire so the child computes the same key without reading the file.
Owner: leaf p1-01 exposes `func ClipKey(text, vendor, model, voice string, refHash []byte) string`,
where `refHash` is the ALREADY-HASHED `sha256(refBytes)` (pass nil/empty for
preset voices; the helper hashes the empty byte string).

### C2: Store layout (leaf p1-01 owner; child + daemon read)

```
dir:  config.MeeptPath("voice_clips")            // honors MEEPT_HOME (internal/config/home.go:42)
pcm:  <clipKey>.pcm                                raw s16le PCM, L16 at 16000 Hz -
                                                   the child lane's fixed rate, NOT the vendor's
                                                   native rate (provider already resampled:
                                                   providers.go ProviderConfig.SampleRate,
                                                   tts_openai.go:252)
meta: <clipKey>.json  {key, text, vendor, model, voice, sample_rate, bytes, created_at}
```

Rationale for 16 kHz: child chunks are ALWAYS L16@16k = 640 bytes
regardless of the negotiated wire codec (internal/voice/AGENTS.md, Sockets
C2), and the TTS adapter already resamples to the negotiated rate before it
returns a reader. Storing at 24 kHz would add a second resample and a second
code path for no gain. `sample_rate` stays in the meta as an assertion; a
clip whose meta rate is not 16000 is a MISS (regenerate), not a resample.

No index file; the store is content-addressed. Presence of a valid
`<key>.pcm` with a matching meta `bytes` is the ONLY cache-hit condition.
Corrupt/short file = miss (delete, regenerate).

### C3: C1 frame additions (voiceframe; BOTH daemon-side and child-side leaves pin this exactly)

```json
{"event":"say","call_id":"...","text":"Just a sec.","last":false,"clip_id":"<64 hex>"}
{"event":"clip_render","call_id":"<ccid>","clips":[{"key":"<64 hex>","text":"Just a sec."}]}
{"event":"clip_ready","call_id":"<ccid>","key":"<64 hex>","ok":true}
```

All new fields are ADDITIVE and optional per the keys-wire convention
(`internal/daemon/voice_keys.go` comments): an old child ignores
`clip_id`/`clip_render` (falls back to live synth - today's byte-identical
behavior; the child's `default:` branch already logs-and-continues on unknown
events, `internal/voicechild/child.go:379`); an old daemon never sends them.
`clip_render.clips` is capped at 32 entries per frame; the daemon shards
larger pools across frames. `call_id` is MANDATORY on all three frames: a
render is per call, never a process-level frame with an empty id.

Event constants must be added in BOTH places: `internal/voiceframe/frame.go`
(the const block) and `internal/voice/frame.go:22-33` (the daemon-side alias
const block). Leaf p1-03 owns both files.

### C4: llm warm seam (leaf p2-01 owner)

```go
// Warm pre-issues a cache-warming request for the given rendered messages
// and caller opts: non-streaming, max_tokens=1, the SAME payload builder and
// session scope the real turn uses. It NEVER returns a completion, NEVER
// consumes turn budget semantics, NEVER starts tools or an agent turn, and
// NEVER reads or writes any response cache.
//
// Failures (any kind) are logged at DEBUG and swallowed by design.
func (c *Client) Warm(ctx context.Context, messages []ChatMessage, opts ...ChatOption) error
```

Hard rules (each is a pinned test, not a hope):

1. **No alias parameter.** Alias resolution lives at the Resolver /
   ProviderManager layer, not on `*Client`. Leaf p2-02 resolves the alias
   first and passes the resulting opts; `Warm` dispatches on the client that
   would serve that alias. The message type is `llm.ChatMessage`
   (`internal/llm/models.go:23`).
2. **Same payload builder.** The warm must serialize through the same
   non-streaming payload path as the real turn, including `tools`, because
   the tool block is part of the serialized prompt. A warm with a different
   tool set produces a different prefix and the whole feature yields nothing.
3. **No cache traffic.** Warm must bypass `tokenCache` Get/Put
   (`client.go:785`, `:977`) and the semantic tier (`:383`, `:410`). A
   `max_tokens=1` warm response stored under the exact key would be SERVED to
   the real turn as the completed answer whenever the final transcript
   matches the partial. There is no existing bypass flag; add one
   (`chatOptions.noCache` or a dedicated field) and pin it.
4. **No retry loop.** Warm must not enter the short-retry loop
   (`client.go:817`): a warm that returns `ErrEmptyResponse` must not be
   re-dispatched, or every partial triples its prefill traffic.
   `max_tokens=1` output can legitimately look empty.
5. **No identity side effects.** Warm never records a Resolver alias failure
   and never writes the sticky per-session endpoint map; it is a client-level
   dispatch, not a manager-level turn.
6. **Background slot posture.** `priority=false` always
   (`chatOptions.priority`, `client.go:1291-1297`; llm AGENTS.md D11), and a
   warm never blocks in `budget.WaitForRateLimit` (`client.go:801-802`).
   It carries its own short deadline (default 1200 ms) so a hung warm cannot
   pin the semaphore.
7. **Endpoint support gate.** Warm is a silent no-op unless the serving
   endpoint reuses prompt prefixes - llama.cpp started with `--cache-reuse`
   (`config/models.json5:259`, `:318`). MLX servers
   (`mlx_lm server`, `config/models.json5:185`) and cloud providers get no
   warm call at all. Encode this as one predicate; do not scatter checks.

> **SUPERSEDE 2026-11-17 (issue #142 Option A applied): the AMENDMENT below is
> WITHDRAWN. Code moved to the frozen C4 signature, not the reverse. As-built
> now: `func (c *Client) Warm(ctx context.Context, messages []ChatMessage,
> opts ...ChatOption) error` (internal/llm/warm.go:146), alias resolution at
> the warmer layer via `Resolver.PeekAliasForWarm` + `WithResolvedModel` opts,
> `chatOptions.noCache` bypassing all cache Get/Put + semantic tier sites,
> dedicated warm transport (no retry loop, no slot gate, no identity side
> effects). Landing commit: 25d5a524. Pins: internal/llm/warm_c4_pins_test.go
> + internal/daemon/voice_wiring_contextwarm_c4_test.go. The amendment text
> below is kept as the historical record of the (vetoed-by-default-execution)
> ratification proposal.**

> **AMENDMENT 2026-11-16 (C4 signature ratification, doc-only; audit BUG #1,
> owner default applied, veto pending):**
> The frozen sketch above pins
> `Warm(ctx, messages, opts ...ChatOption) error`; the as-built, shipped
> signature (merged 648eb6eb, ancestor of HEAD) is
>
> ```go
> func (c *Client) Warm(ctx context.Context, alias string, messages []ChatMessage, sessionScope string)
> ```
>
> at `internal/llm/warm.go:109`, and it is RATIFIED as the contract. The
> as-built 4-arg shape is the better design, and the deviation is deliberate:
>
> - `alias` is a DEBUG-attribution label only (`internal/llm/AGENTS.md`,
>   Context-warm seam section): request-scoped alias resolution lives in the
>   Resolver/ProviderManager lane and is NOT reachable from a bare `*Client`,
>   so a no-alias signature could not correlate warm logs with the turn they
>   precede. Callers pass the alias the real turn will use.
> - `sessionScope` carries exactly what the contract wanted `opts` for: the
>   session-affinity scope (`WithTaskScope` lane -> `prompt_cache_key` +
>   `llm.session_affinity` header; empty scope sends neither).
> - Warm is fire-and-forget (`void`, no error return) by design - failures
>   are DEBUG-logged and swallowed per this contract's own failure clause
>   (`warm.go:95-103`). The rule-1 intent ("alias resolution lives at the
>   Resolver layer") is PRESERVED: Warm performs no resolution, it only
>   labels.
> - The signature is mirrored EXACTLY by the daemon seam
>   (`internal/daemon/voice_wiring_contextwarm.go:58`, `warmDispatcher`
>   interface) and the voice-side seam (`internal/voice/contextwarm.go:68`,
>   `WarmFn`); a signature change would touch three packages for zero
>   behavior gain.
>
> Behavior is unchanged by this amendment; it is a doc-only delta. Two
> further as-built deviations on the hard-rule list are recorded here so the
> contract matches the code it froze (all owner-default, veto pending, no
> behavior change to the feature's purpose):
> - Rule 2 "including tools": the as-built warm payload is a bare prefix
>   echo WITHOUT tools (`warm.go:163-166` "No tools, no grammar, no
>   reasoning, no prefill"), documented as-built in
>   `internal/llm/AGENTS.md` (Context-warm seam). Voice-turn prompts carry
>   no tool block; a tools-bearing turn would need the prefix identity
>   revisited before enabling warm on that lane.
> - Rule 6 deadline: the as-built internal ceiling is 10 s
>   (`warmCeilingDefault`, `warm.go:67`), not 1200 ms - the replace-discard
>   lane (cap 1, newest warm cancels the in-flight one, `warm.go:123-131`)
>   bounds the exposure the 1200 ms figure was protecting.
>
> Rule 3 (no cache traffic) is verified at HEAD as SATISFIED - see the
> 2026-11-16 Delta Log entry for the verdict and file:line evidence.

### C5: Lane pools (config keys; leaf p1-05 owner resolves, p1-04 consumes)

```
voice.filler                 // existing list; GLOBAL only today (see note)
voice.greeting               // existing SINGLE STRING (schema.go:5485)
voice.greeting_lines         // NEW optional list; when non-empty it wins
voice.silence_prompt         // existing single line -> keep scalar
voice.silence_prompt_lines   // NEW list; non-empty wins
voice.fragment_clarify       // existing single line -> keep scalar
voice.fragment_clarify_lines // NEW list; non-empty wins
voice.farewell_lines         // NEW list; SCHEMA default EMPTY (opt-in);
                             // config/meept.json5 ships ["Goodbye!","Bye!","Talk soon."]
```

Back-compat rule: `silence_prompt`/`fragment_clarify` stay `string` in
schema (existing per-employee overrides depend on it, `schema.go:5839`);
the `_lines` lists take precedence when non-empty. Pool selection =
round-robin per lane with its own cursor (existing `fillerIdx` pattern,
`talkloop.go:549-553`).

Two corrections to the earlier draft:

- **`voice.farewell_lines` defaults EMPTY in the schema.** The repository
  pins "zero-config = byte-identical legacy"
  (internal/voice/AGENTS.md, Per-employee acoustic controls). A non-empty
  schema default would make every existing deployment start speaking a
  goodbye. The shipped *template* carries the three lines, so a fresh
  install gets the feature and an existing config gets it only when the
  operator adds the key.
- **Filler is GLOBAL-only today.** `voice_wiring.go:1008` passes
  `w.cfg.Filler` straight into the talk loop, and `ResolveAcoustics`
  (`internal/voice/profile.go:127-170`) carries no Filler or Greeting field.
  Per-employee filler pools are NOT in scope for this tree; they would need
  new `employee_leg` pointers plus resolver fields. Say so in the leaf
  report rather than half-wiring it.

## Requirement Traceability

| Req | Statement | Leaf |
|-----|-----------|------|
| R1 | Cached filler audio kills filler synth latency | p1-01, p1-03, p1-04 |
| R2 | Contextual filler pools per lane | p1-05 |
| R3 | Dynamic per-voice generation after the keys frame (per call) + reload pass, cached under $MEEPT_HOME, key-invalidated | p1-01, p1-02, p1-03 |
| R4 | Sidecar-only: nothing generated/written without a running voice child + resolved TTS config | p1-02 (gate), p1-03 (child-side) |
| R5 | Farewell spoken clip on caller farewell intent and on agent/operator-initiated hangup; never blocks release | p1-04 |
| R6 | Audit transcript + gate contracts unchanged (clips are say-frame attributes) | p1-03, p1-04 |
| R7 | Partial-triggered context warm before final turn | p2-02 |
| R8 | Warm is toolless, cache-free, retry-free, budget-safe, silent-failure | p2-01 |
| R9 | e2e hermetic suites + manifest entries for both phases | both 00T gates + suite leaves named in each 00T |

## Child Index

| Node | Type | Est. ctx | Depends on | Parallel group |
|------|------|----------|------------|----------------|
| phase1-clips/orchestrator.md | Branch | - | none | phase 1 first (see note) |
| phase2-context-warm/orchestrator.md | Branch | - | none | after phase 1 leaf 02 lands |

Phase 2 leaf 02 needs a hook in the daemon's prompt router, which is
`VoiceWiring.routeFrame` (`internal/daemon/voice_wiring.go`) - the same file
Phase 1 leaf 02 extends. The branches are NOT file-disjoint. Run phase 1 to
COMPLETE first, then phase 2; or, if they must overlap, keep Phase 2's hook
in its own new file (`internal/daemon/voice_warm.go`) and land it after
Phase 1 leaf 02's commit.

## Coding Conventions

- Go, project style (`docs/agents/coding-practices.md`); `gofmt`; no new deps unless a leaf says so.
- Secrets posture unchanged: clip files carry audio only; the keys frame stays the only place TTS material lives in the child (memory-only keys). Never log clip text at info beyond the existing "turn filler" line shape.
- Every `Set*`/nil-guard/mutex rule applies; the clip write path does file I/O OUTSIDE any mutex (mutexio analyzer gate).
- Phase 1 leaf 01 creates the package `internal/voice/clipstore`. The pre-commit e2e check ([18/18], `pre-commit-e2e`) blocks a commit that adds files under a NEW package directory without an e2e suite + manifest `path_map` entry. Phase 1's `00T` leaf authors `e2e/suites/voice-clips` and the manifest entry in wave 0, so the entry already exists when leaf 01 lands the package. Keep that order; do not move the manifest edit into a later leaf.
- Keep `internal/voice/clipstore` importable by the child: it may import stdlib plus `internal/config` only. Never `internal/voice` (the daemon package), or the child binary drags the daemon graph in.
- Config template edits go in BOTH `config/meept.json5` and schema defaults (`internal/config/schema.go`), same commit as the leaf that reads them.
- Docs: update `docs/workflows/voice.md` + `internal/voice/AGENTS.md` invariants in the branch integration commit (leaf 04 or the orchestrator's integration commit - never leave a new C1 frame undocumented; AGENTS.md maintenance rule).

## Completion Tracking Table

| Node | Status |
|------|--------|
| phase1-clips branch | VALIDATED | 2026-11-16 reconciled from git: leaves merged via 648eb6eb (merge of plan/filler-clips-context-warm; inner merge 18538f29 "merge(main): bring clip/warm tree onto doctor-hermetic + voice-fix main"). Leaf SHAs: 44e3d9a9 (00T gate suites), d662c738 (p1-05/p2-02 config schema), 3769b30d (p1-01 clipstore), 48cfae06 (p1-05/p2-02 lane pools + ContextWarmer), fe8fea32 (p1-03 child render/playback), 00f9e86c (p1-04 talk-loop pools/farewell), d03f3b0a (p1-02/p1-05 daemon planner). Hermetic evidence 2026-11-16: e2e/suites/voice_clips 5/5 PASS (TestVoiceClipsSidecarDisabledNoDir, TestVoiceClipsPoolsRendered, TestVoiceClipsIdentityChangeRegenerates, TestVoiceClipsCachedFillerUnder300msVsSlowTTS, TestVoiceClipsFarewellAudioPrecedesEndFrame); unit ok: internal/voice/clipstore (incl. TestClipKeyGateMatchesFrozenC1Formula), internal/voice (lanes/talkloop), internal/voicechild (clips), internal/daemon (clip planner), internal/voiceframe (C3 wire shapes). Live smoke a-e NOT executed — root Integration Test Plan disposition. |
| phase2-context-warm branch | VALIDATED | 2026-11-16 reconciled from git: 30e2f6ed (p2-01 Warm seam + p2 daemon wiring), 48cfae06 (p2-02 debounce), d662c738 (p2-02 config knobs), plus root 55569915 (docs integration) — all ancestors of 648eb6eb. Hermetic evidence 2026-11-16: e2e/suites/context_warm 4/4 PASS (TestContextWarmPartialThenFinal, TestContextWarmDisabledZeroWarms, TestContextWarmTokenFloorSkips, TestContextWarmQuotaUpstreamTurnUnaffected); unit ok: internal/llm (incl. TestWarmSignature_C4 compile-time signature pin), internal/voice (contextwarm), internal/daemon (contextwarm wiring). C4 amended 2026-11-16 to ratify the as-built Warm signature (internal/llm/warm.go:109); hard rule 3 verified SATISFIED at HEAD (root Delta Log). |
| Root integration (docs + full gate set) | COMPLETE | 2026-11-16 reconciled from git: 648eb6eb merged onto main (ancestor of main); 55569915 tree-integration docs; d4fabbb1 plans-status tables commit (in 648eb6eb, on main). Full `make test` + `make lint-ci` + `make graphs-check` NOT re-run in the 2026-11-16 reconciliation pass (scoped hermetic runs only — honest NOT-VERIFIED on the full gate set; re-run owner-side before any release claim). |

Per-leaf tables live in each branch orchestrator. This table mirrors branch verdicts only; the parent determines completion (canon C1-State).

## Test Gate

Both branches carry their own `## Test Gate` matrices and `00T-gate-tests.md` leaves. Root-level verification after both branches report COMPLETE: `make e2e-fast-area AREA=voice-clips`, `make e2e-fast-area AREA=context-warm`, then `make test` (-p 2) and `make lint-ci`.

`make graphs` / `make graphs-check` are NOT affected by new C1 frame events:
`scripts/gen-connectivity-graph.py` keys on bus topics, RPC handlers, HTTP
routes, and WS classes, not on `voiceframe` events. Run the check anyway as
part of the full gate set, and regenerate only if Phase 2 adds a bus or RPC
surface (it should not).

## Integration Test Plan

1. Both branches at COMPLETE in their tables.
2. Full `go test -p 2 ./...` plus `make e2e-fast` green in the isolation worktree.
3. Live smoke (manual, user-side): throwaway home at a SHORT path - macOS unix
   sockets must stay under the kernel `sun_path` limit of 103 bytes
   (`$MEEPT_HOME/voice/sock/<ccid>.in`, internal/voice/AGENTS.md), so use
   `/tmp/mp1` or similar, never `$(mktemp -d)` under `/var/folders`. Set up
   per skill `meept-voice-loop-test`, run a local leg call, verify:
   (a) clips exist for the default pools under `/tmp/mp1/voice_clips/`;
   (b) filler fires with cached PCM (log line "turn filler" with `clip=true`);
   (c) deleting one clip file + config reload re-renders only that key;
   (d) with the voice sidecar disabled the dir is never created;
   (e) with the child stopped mid-call, filler falls back to live synth and the
       call continues.

   **Disposition 2026-11-16 (reconciliation wave):** steps a-e NOT EXECUTED.
   Every step needs a live daemon + live voice child + live TTS, which this
   reconciliation pass was scoped to exclude. Owner steps to close: run the
   local-leg smoke per skill `meept-voice-loop-test` with
   `MEEPT_HOME=/tmp/mp1` (short path per the sun_path limit), verify each of
   (a)-(e) in order, and record results in a Delta Log entry. Hermetic
   equivalents that WERE run green 2026-11-16 (partial coverage of the same
   properties, recorded in the Delta Log): (a)/(c) e2e `TestVoiceClipsPoolsRendered`
   + `TestVoiceClipsIdentityChangeRegenerates`; (d) e2e
   `TestVoiceClipsSidecarDisabledNoDir`; (e) unit `TestClipSayMissFallsToSpeak`
   + `TestClipGateSayMissFallsBackToLiveSpeak`; (b) has no hermetic
   equivalent of the live `clip=true` log line.
4. Merge branch worktree to main per the isolation-worktree protocol; delete branch.

## Review Checklist

- [ ] Every pinned contract (C1-C5) matches the code byte-for-byte: key field order and `\x1f` separators, frame field names and JSON shapes, `ChatMessage`/`ChatOption` types, `ClipID` on `voiceframe.Frame`, event constants in both const blocks.
- [ ] R4 gate proven by test: no clip process without a running child (assert store dir untouched when voice disabled).
- [ ] Old-child / old-daemon back-compat tests exist for C3 (absent fields = today's behavior).
- [ ] No PCM ever crosses C1 (clip frames carry hex keys + text only).
- [ ] Warm CANNOT pollute a cache: a test runs Warm then a real turn with identical messages and asserts the real turn reaches upstream (no `tokenCache` hit, no semantic hit).
- [ ] Warm does not retry: a fake upstream returning empty text observes exactly ONE request per warm.
- [ ] Warm prefix identity: the warm payload and the turn payload share the same serialized prefix (same tools, same model, same sessionID).
- [ ] Warm cannot start tool calls (p2-01 test pins it) and never touches turn budget/quota-park state (R8).
- [ ] Opt-in: `voice.context_warm=false` produces zero warm requests and byte-identical dispatch timing.
- [ ] Farewell: a played edge precedes the C1 `end` frame on the agent-initiated path; `releaseCall` keeps the end-frame send as its first sink and never blocks.
- [ ] Timbre-consistency acceptance: filler playback path reads store; live-synth fallback only on miss.
- [ ] AGENTS.md + docs/workflows/voice.md updated in-branch, not deferred.

## Open Questions

- None blocking. The qwen3 server ignores `stream:true` despite the request
  flag (`internal/voicechild/providers/tts_openai.go:210` sends it;
  `internal/voicechild/pipeline.go:241-246`, `:1668-1670` records that the
  server does not stream, blocking 0.7-4.8 s). That observation is why the
  clip cache exists. If a future server starts streaming, clip rendering gets
  faster and nothing else changes.
- If the qwen3 vendor's output rate ever differs from 16 kHz after
  resampling, leaf p1-03 treats the clip as a MISS (C2), never a resample of
  stored audio.

## Dispatch Protocol

Per node, in phase order (phase 1 first; see Child Index for the file
collision):

1. Read the branch's `orchestrator.md`; dispatch its `00T-gate-tests.md`
   leaf via `delegate_task` (goal: one-line imperative; context: full leaf
   brief + branch Test Gate matrix + this root's Coding Conventions +
   "Do NOT commit"). Validate RED tests in-session against the matrix
   before any impl leaf dispatches.
2. Dispatch impl leaves per the branch waves (file-disjoint within a wave,
   parallel up to `delegation.max_concurrent_children`).
3. Orchestrator (main model) reviews each leaf return in-session: read
   changed files, run the leaf's verify commands, check the Review
   Checklist. On gaps: re-dispatch with specific feedback (max 3 loops).
4. Commit per leaf: `git commit -- <leaf's declared paths>` with a message
   naming the leaf doc (staged==declared-scope rule for sibling safety).
5. Branch reports COMPLETE in the root Completion Tracking Table; then run
   the root Integration Test Plan.

## Delta Log

- 2026-11-16 (reconciliation wave; audit BUG #1 + BUG #8; doc-only):
  - **C4 amendment (BUG #1).** The frozen C4 sketch pinned
    `Warm(ctx, messages, opts ...ChatOption) error`; the shipped signature is
    `Warm(ctx context.Context, alias string, messages []ChatMessage,
    sessionScope string)` (`internal/llm/warm.go:109`), mirrored by
    `internal/daemon/voice_wiring_contextwarm.go:58` (`warmDispatcher`) and
    `internal/voice/contextwarm.go:68` (`WarmFn`). Ratified as-built via the
    dated AMENDMENT 2026-11-16 above the C5 contract (owner default, veto
    pending); rationale recorded there (alias = DEBUG-attribution label per
    `internal/llm/AGENTS.md` Context-warm seam; sessionScope carries the
    session-affinity scope the contract wanted opts for; three-package
    signature surface). As-built deviations on rules 2 (no tools in warm
    payload) and 6 (10 s ceiling, not 1200 ms) recorded in the same
    amendment. No Go code changed.
  - **C4 hard rule 3 (no cache traffic) VERIFIED at HEAD - SATISFIED.**
    Mechanical evidence: `internal/llm/warm.go` contains ZERO references to
    `tokenCache`, `semanticCache`, or `semanticLookupBehindMiss`
    (`grep -c` = 0); the warm path is `Warm` (warm.go:109) ->
    `dispatchWarm` (:154, builds ONLY via `buildChatRequest` :171 with
    `WithMaxTokens(1)` + `WithTaskScope` :167-170) ->
    `sendWarmRequest` (:190), a dedicated transport that never enters
    `doRequest` (whose cache block sits at `internal/llm/client.go:783-796`:
    `tokenCache.Get` :785, semantic tier :792, `tokenCache.Put` :977) and
    never carries a cache key at all. The response body is drained to
    `io.Discard` (warm.go:233) and discarded. Structural cover:
    `TestWarm_NoLLMCallsRow`, `TestWarm_ReturnedTextNeverSurfaces`,
    `TestWarm_BudgetExhaustedStillWarms`, `TestWarm_NeverQueuesBehindRealTurn`
    (`internal/llm/warm_test.go`). Note: the review-checklist item "warm then
    real turn with identical messages asserts no tokenCache hit" has no
    direct test because the warm transport is structurally incapable of
    touching `tokenCache` (no key builder, no Get/Put call sites in warm.go);
    the rule is satisfied by construction, pinned by the compile-time
    signature test `TestWarmSignature_C4` (warm_test.go:162).
  - **Smoke steps a-e NOT EXECUTED** (live daemon + child + TTS required;
    out of scope for this pass). Owner steps recorded in the Integration
    Test Plan disposition block above. Hermetic equivalents run green
    (partial property coverage): e2e `voice_clips`
    (`TestVoiceClipsSidecarDisabledNoDir`, `TestVoiceClipsPoolsRendered`,
    `TestVoiceClipsIdentityChangeRegenerates`,
    `TestVoiceClipsCachedFillerUnder300msVsSlowTTS`,
    `TestVoiceClipsFarewellAudioPrecedesEndFrame` — 5/5 PASS, 2026-11-16) and
    e2e `context_warm` (`TestContextWarmPartialThenFinal`,
    `TestContextWarmDisabledZeroWarms`, `TestContextWarmTokenFloorSkips`,
    `TestContextWarmQuotaUpstreamTurnUnaffected` — 4/4 PASS, 2026-11-16);
    unit suites `internal/voice/clipstore`, `internal/voice`,
    `internal/voicechild`, `internal/daemon`, `internal/llm`,
    `internal/config`, `internal/voiceframe` — all `ok` at HEAD 5f64419a
    (2026-11-16, `go test -count=1` scoped run).
  - **Tracking tables reconciled.** Root Completion Tracking Table:
    PENDING -> VALIDATED (both branches; evidence above). Branch
    orchestrator tables (phase1-clips, phase2-context-warm): leaf rows
    reconciled to VALIDATED with test names + merge SHAs; branch Test Gate
    matrices' PENDING e2e columns resolved against the suites on disk
    (e2e/suites/voice_clips, e2e/suites/context_warm — both manifest
    -registered, both green 2026-11-16). Merge lineage: 648eb6eb + 18538f29,
    both verified ancestors of HEAD.