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

Measured problem: filler lines ride the live TTS lane (`internal/voice/talkloop.go:1156` → `t.say()` → child `Speak()`), blocking 0.7–4.8 s per synthesis (pinned in `internal/voicechild/providers/tts_qwen3_test.go` notes). Two user-visible defects follow: (a) perceived dead air while the employee turn runs, (b) the 3-second-clone voice drifts to a different timbre on short utterances (filler/greeting/nudge are the shortest text the lane speaks). Decisions locked with the user on 2026-11-13:

1. **Cached filler audio (R1):** every line the filler-class lanes can speak is pre-synthesized once per (voice, model, text) and served as PCM from a disk cache. Zero live synth for filler.
2. **Dynamic generation, never hand-made (R3):** clips are generated in the background at voice-child start and on config reload; stored under `$MEEPT_HOME/voice_clips/`; invalidated by key (voice/model/text/reference-audio hash change ⇒ new key ⇒ regenerate).
3. **Sidecar-only (R4):** generation and caching run ONLY when the voice sidecar is actually configured and running (voice child up, TTS vendor/model resolved via the keys lane). A configured `tts_voice` with no sidecar / no voice agents must generate nothing and write nothing.
4. **Contextual pools (R2):** filler selection becomes lane-aware — "cut off?" lines for fragments, "just a sec" variants for working turns, "hello?"/"still there?" variants for dead air, bye variants for farewells. Finite configured pools only (every pool line is cacheable; no free-form generated filler).
5. **Farewell lane (R5):** new — a spoken goodbye clip on farewell intent / call end, replacing today's silent C1 `end`.
6. **Partial context warm (R7):** `last:false` partial prompts, which already reach the daemon (`internal/voiceframe/frame.go:29`, `internal/voicechild/pipeline.go:1069`), trigger a max_tokens=1 warm request per session so the llama.cpp-family prefix cache (session-affinity `prompt_cache_key`, already default-on, `internal/llm/client.go:725`) holds the conversation + in-flight user text before the real turn. Warm never runs tools, never starts an employee turn, is quota-safe, and its failures are silent (R8).

Non-goals: speculative full-answer drafts (Path B-heavy), Path B speech-to-speech providers, changes to the supervisor gate contract, changes to C2 audio framing.

## Architecture

Phase 1 data flow (numbers are leaves):

```
daemon VoiceWiring.Start / config-reload (leaf 02)
  -> enumerate filler-class pools from resolved config
  -> compute clip keys (Contract C1 formula, leaf 01 helper)
  -> diff vs $MEEPT_HOME/voice_clips/ (leaf 01 store)
  -> C1 "clip_render" frame {key,text}[] to the voice child (leaf 02->03)
child (leaf 03)
  -> for each miss: live Speak() once, write PCM + manifest via store,
     never blocking the turn lane
talkloop (leaf 05/06)
  -> filler/nudge/fragment/farewell say frames carry clip_id (Contract C2 wire field)
child playback (leaf 04)
  -> clip_id present + store hit: stream cached PCM into the utterance
     lane (same pacing/barge-in/drain paths as synthesized audio)
  -> clip_id present + miss, or absent: today's live Speak(text) path
```

Phase 2 data flow:

```
routeFrame last:false partial (daemon, existing)
  -> PartialWarm debounce per (session, employee) (leaf p2-02)
  -> llm.Warm(ctx, alias, messages) (leaf p2-01): non-streaming,
     max_tokens=1, session-affinity header, Warm-marked, never surfaced
final last:true turn (existing path, unchanged)
  -> same prefix + session key => provider prompt-cache hit => prefill off
     the critical path
```

The `t.say()` choke in `internal/voice/talkloop.go` stays the single origin of every audible line (echo_roster.go:136 invariant preserved): clips are an attribute of a say frame, never a second audio path around it.

## Interface Contracts

### C1: Clip key formula (pinned — both branches' consumers copy verbatim)

```
clipKey = hex(sha256(
    "v1" \x1f text \x1f tts_vendor \x1f tts_model \x1f tts_voice \x1f hex(sha256(reference_audio_bytes))))
```

`reference_audio_bytes` is the clone reference the configured TTS voice uses; for non-clone voices use the empty string's pre-image (hash of zero bytes). Owner: leaf p1-01 exposes `func ClipKey(text, vendor, model, voice string, refHash []byte) string`.

### C2: Store layout (leaf p1-01 owner; child + daemon read)

```
dir:  config.MeeptPath("voice_clips")            // honors MEEPT_HOME (internal/config/home.go:42)
pcm:  <clipKey>.pcm                                raw s16le PCM at the vendor's native output rate (24000 for qwen3; child resamples via existing resample.go path)
meta: <clipKey>.json  {key, text, vendor, model, voice, sample_rate, bytes, created_at}
```

No index file; the store is content-addressed. Presence of a valid `<key>.pcm` with a matching meta `bytes` is the ONLY cache-hit condition. Corrupt/short file = miss (delete, regenerate).

### C3: C1 frame additions (voiceframe; BOTH daemon-side and child-side leaves pin this exactly)

```json
{"event":"say","call_id":"...","text":"Just a sec.","last":false,"clip_id":"<64 hex>"}
{"event":"clip_render","call_id":"<ccid>","clips":[{"key":"<64 hex>","text":"Just a sec."}]}
{"event":"clip_ready","call_id":"<ccid>","key":"<64 hex>","ok":true}
```

All new fields are ADDITIVE and optional per the keys-wire convention (`internal/daemon/voice_keys.go` comments): an old child ignores `clip_id`/`clip_render` (falls back to live synth — today's byte-identical behavior); an old daemon never sends them. `clip_render.clips` is capped at 32 entries per frame; the daemon shards larger pools across frames.

### C4: llm warm seam (leaf p2-01 owner)

```go
// Warm pre-issues a cache-warming request for the given alias and rendered
// messages: non-streaming, max_tokens=1, same session-affinity scope as the
// real turn. It NEVER returns a completion, NEVER consumes turn budget,
// NEVER starts tools or an agent turn. Failures (any kind) are logged at
// DEBUG and swallowed by design.
func (c *Client) Warm(ctx context.Context, alias string, messages []Message, sessionScope string)
```

### C5: Lane pools (config keys; leaf p1-05 owner resolves, p1-06 consumes)

```
voice.filler           // existing working-turn pool (keep; multiple lines now fully honored)
voice.greeting         // existing
voice.silence_prompt   // existing single line -> accept list (deprecated-shape rule below)
voice.fragment_clarify // existing single line -> accept list
voice.farewell         // NEW pool: ["Goodbye!","Bye!","Talk soon."] default non-empty
```

Tri-state/back-compat rule: `silence_prompt`/`fragment_clarify` stay `string` in schema (existing per-employee overrides depend on it); NEW list keys `voice.silence_prompt_lines`, `voice.fragment_clarify_lines`, `voice.farewell_lines` take precedence when non-empty. Pool selection = round-robin per lane with its own cursor (existing fillerIdx pattern).

## Requirement Traceability

| Req | Statement | Leaf |
|-----|-----------|------|
| R1 | Cached filler audio kills filler synth latency | p1-01, p1-03, p1-04 |
| R2 | Contextual filler pools per lane | p1-05 |
| R3 | Dynamic per-voice generation at start/reload, cached under $MEEPT_HOME, key-invalidated | p1-01, p1-02 |
| R4 | Sidecar-only: nothing generated/written without a running voice child + resolved TTS config | p1-02 (gate), p1-03 (child-side) |
| R5 | Farewell spoken clip on call end | p1-06 |
| R6 | Audit transcript + gate contracts unchanged (clips are say-frame attributes) | p1-04, p1-06 |
| R7 | Partial-triggered context warm before final turn | p2-02 |
| R8 | Warm is toolless, budget-free, silent-failure | p2-01 |
| R9 | e2e hermetic suites + manifest entries for both phases | both 00T gates + suite leaves named in each 00T |

## Child Index

| Node | Type | Est. ctx | Depends on | Parallel group |
|------|------|----------|------------|----------------|
| phase1-clips/orchestrator.md | Branch | — | none | both phases independent |
| phase2-context-warm/orchestrator.md | Branch | — | none | both phases independent |

Dispatch each branch per its own orchestrator. Branches share NO files and can execute concurrently.

## Coding Conventions

- Go, project style (`docs/agents/coding-practices.md`); `gofmt`; no new deps unless a leaf says so.
- Secrets posture unchanged: clip files carry audio only; the keys frame stays the only place TTS material lives in the child (memory-only keys). Never log clip text at info beyond the existing "turn filler" line shape.
- Every `Set*`/nil-guard/mutex rule applies; the clip write path does file I/O OUTSIDE any mutex (mutexio analyzer gate).
- New packages are banned by the e2e policy check [18/18] unless a suite + manifest entry lands in the same commit — both phases extend EXISTING packages and the `voice`-adjacent e2e area; each 00T leaf owns its suite/manifest entry.
- Config template edits go in BOTH `config/meept.json5` and schema defaults (`internal/config/schema.go`), same commit as the leaf that reads them.
- Docs: update `docs/workflows/voice.md` + `internal/voice/AGENTS.md` invariants in the branch integration commit (leaf 04 or the orchestrator's integration commit — never leave a new C1 frame undocumented; AGENTS.md maintenance rule).

## Completion Tracking Table

| Node | Status |
|------|--------|
| phase1-clips branch | PENDING |
| phase2-context-warm branch | PENDING |
| Root integration (docs + full gate set) | PENDING |

Per-leaf tables live in each branch orchestrator. This table mirrors branch verdicts only; the parent determines completion (canon C1-State).

## Test Gate

Both branches carry their own `## Test Gate` matrices and `00T-gate-tests.md` leaves. Root-level verification after both branches report COMPLETE: `make e2e-fast-area AREA=voice-clips`, `make e2e-fast-area AREA=context-warm`, then `make test` (-p 2) and `make graphs-check` (new C1 frames land in the bus/WS topology generator if `make graphs` keys on frame event strings — regenerate if red).

## Integration Test Plan

1. Both branches at COMPLETE in their tables.
2. Regenerate connectivity graphs (`make graphs`); commit `docs/generated/*` if the new C1 events or the warm seam change any layer export.
3. Full `go test -p 2 ./...` plus `make e2e-fast` green in the isolation worktree.
4. Live smoke (manual, user-side): `MEEPT_HOME=$(mktemp -d)` throwaway home, `make dev-key` equivalent setup per skill `meept-voice-loop-test`, run a local leg call, verify from `$MEEPT_HOME/voice_clips/` that (a) clips exist for the default pools, (b) filler fires with cached PCM (log line "turn filler" with `clip=true`), (c) deleting one clip file + config reload re-renders only that key, (d) with `voice` sidecar disabled the dir is never created.
5. Merge branch worktree to main per the isolation-worktree protocol; delete branch.

## Review Checklist

- [ ] Every pinned contract (C1-C5) matches the code byte-for-byte (key formula order, frame field names, JSON shapes).
- [ ] R4 gate proven by test: no clip process without a running child (assert store dir untouched when voice disabled).
- [ ] Old-child / old-daemon back-compat tests exist for C3 (absent fields = today's behavior).
- [ ] No PCM ever crosses C1 (clip frames carry hex keys + text only).
- [ ] Warm requests cannot start tool calls (p2-01 test pins it) and never touch budget/quota counters (R8).
- [ ] Timbre-consistency acceptance: filler playback path reads store; live-synth fallback only on miss.
- [ ] AGENTS.md + docs/workflows/voice.md updated in-branch, not deferred.

## Open Questions

- None blocking. If the qwen3 vendor's native output rate turns out to be per-voice rather than fixed 24 kHz, leaf p1-03 records the actual rate in the meta and p1-04 resamples from meta.sample_rate (contract already carries the field).

## Dispatch Protocol

Per node, in phase order (branches are independent; run in parallel or in
sequence at the user's discretion):

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
