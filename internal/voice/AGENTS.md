# internal/voice/AGENTS.md

Guidance for AI agents working in `internal/voice/` (and, by contract,
its child-side siblings `internal/voiceframe`, `internal/voicechild`,
`cmd/meept-voice`). Referenced from the root AGENTS.md; the full voice
sidecar contract lives here. Update both in the same commit per the
root maintenance rule. User-facing docs: `docs/workflows/voice-sidecar.md`
(spec rev 2 + as-built), `docs/workflows/voice.md`,
`docs/workflows/voicechild.md`. Plan tree: `docs/plans/20261102-voice-sidecar/`.

## Frames (C1): one JSON object per line, every frame carries call_id

- The shared text socket hosts ONE child connection; every pipeline is
  keyed by call_id. Frame JSON keys are BYTE-EXACT (`call_id`, `event`,
  `text`, `last`, `from`, `to`, `direction`, `codec`, `sample_rate`) -
  a wrong key silently breaks the child. The codec lives in
  `internal/voiceframe` (stdlib-only; shared by daemon and child; see
  below).
- `Last` is `*bool`: an omitted `last` (setup, interrupt, end) must stay
  distinguishable from an explicit false. Only final prompts
  (`last:true`) start an employee turn; partials update preview only.
- DTMF frames decode as a counted no-op - a voicemail-tree "press 1"
  must never error the call.
- The child sends setup/prompt/interrupt/end; the daemon sends
  say/clear/end. A `say` frame is sent ONLY after the supervisor allows
  the sentence; filler/greeting lines (fact-free, config list) are the
  sole exception, emitted by the daemon talk loop (greeting on connect,
  filler immediately on each final prompt - order pinned by
  TestTalkLoopGreetingAndFiller).

## Sockets (C2): per-call unix pairs, codec-derived chunks

- `$MEEPT_HOME/voice/sock/<ccid>.in` (daemon->child PCM) and
  `<ccid>.out` (child->daemon PCM); raw frames, no JSON. Chunk size is
  codec-derived, 20 ms: L16@16k = 640 bytes (s16le, NOT big-endian RTP
  L16), PCMU@8k = 160 bytes. The Telnyx start frame
  (`start.media_format`) is the authority for what was negotiated; PCMU
  triggers mu-law transcode both directions.
- Unix socket paths bind only under the kernel sun_path limit (103
  bytes on macOS). Production paths live under `$MEEPT_HOME` and are
  fine; e2e sandboxes shorten the test process TMPDIR for this (leaf
  15). Do not move the socket dir deeper without re-checking the limit.

## Media splice: reorder, pacing, barge-in

- Reorder window (`reorder_window_frames`, default 10) with silence
  fill; frames arriving within `reorder_gap_ms` (default 60) are
  sequenced, not forwarded in arrival order (uptractor deviation).
- Playback paces 20 ms per frame outbound. Child chunks are ALWAYS
  L16@16k = 640 bytes (`ChunkBytes("L16", 16000)`) regardless of the
  negotiated wire codec (audit H2: the wire codec must NEVER size child
  chunks). The wire codec selects only the transcode direction
  (L16 = byte-identical passthrough, PCMU = mu-law 8 kHz up/downsample),
  and the wiring REFUSES a codec/rate mismatch (L16@!=16000, PCMU@!=8000)
  rather than silently mis-resampling (bughunt 2026-11-07 voice F-3).
- Barge-in is PER CALL and per BARGE-IN: `clear` stops playback and drops
  queued drafts for THAT call_id only; other calls' pipelines are untouched
  (pinned by TestBargeInPerCall and the voice-bargein e2e suite). The
  latch is per barge-in, not per call: the child pipeline re-opens playback
  on the next say (playGen retirement kills the interrupted stream), and
  the splice re-arms when fresh child PCM arrives (bughunt 2026-11-07
  voice F-1 — both latches used to be once-per-call, muting the agent
  after its first interrupt). One interrupt VAD latch per barge-in in the
  child pipeline.

## Release fan-out

- A call end fans out through `Manager.fireRelease`: hangup events (and
  the no-answer timer via `FailOutcome(ccid, OutcomeNoAnswer, ...)`)
  release the registry slot, unpark the employee goal, and complete the
  outbound effects reservation exactly once. `FailOutcome` exists so
  no-answer is distinct from hard failure (leaf 05/11); use it for any
  new terminal-not-failure outcome.
- `call.answered` for an untracked id is refused; hangup for an unknown
  id is nil (idempotent teardown). `OnMediaStart` is idempotent.
- Outbound legs are NEVER AnswerCall'd (404 aborts the session);
  `call.answered` triggers StartStreaming only.

## Supervisor gate (C6): fail-open per sentence, never parked

- Every candidate sentence is checked on the fast endpoint
  (`voice.supervisor_model`) with a hard `supervisor_timeout_ms`
  timeout. Timeout/error/quota sends the fallback line and the call
  CONTINUES - a voice call is never quota-parked. Sentence N+1 is
  checked while N plays.
- Nil supervisor chatter (unresolved fast model) degrades the gate to
  allow-all with a logged warning (leaf 14 wiring); that is the only
  sanctioned degrade. Missing telnyx credentials, by contrast, degrade
  the whole voice block to disabled at boot (see secrets below).

## Secrets lanes: token in daemon only, keys to child over the socket

- `Broker.Value("telnyx_api_key")` resolves in-daemon, in-process only
  (allowlisted in scripts/audit-secrets-value-callers.py - add any new
  Value caller in the same commit). The token never reaches a log line,
  env entry, argv, or the child.
- Provider STT/TTS keys reach the child via the C1 `keys` frame on the
  text socket at call start; they live in child MEMORY only (redacting
  String/LogValue in internal/voicechild/keys.go), never disk, env,
  argv, or logs. A missing broker/secret DEGRADES the boot to
  voice-disabled with a loud WARN (scratch boots must still come up);
  every call path stays refused without credentials.

## Child confinement: jargo NEVER leaks daemon-side

- jargo is imported ONLY by `cmd/meept-voice` and
  `internal/voicechild`. `internal/voice` and `internal/voiceframe`
  must not import it - pinned by compile-time import checks
  (internal/voice/import_test.go, internal/voiceframe/pin_test.go).
  Keep those pins green; do not "temporarily" import jargo in daemon
  code. jargo is pinned at v0.1.0 (v0.2.0+ needs go 1.27); API drifts
  are documented in internal/voicechild/jargo.go.
- `internal/voiceframe` is dependency-free (stdlib only): both sides
  share the codec and neither may drag the other's world in through it.
- The child never imports telnyx code and never sees the Telnyx token.
- The child reads its one-time start token from STDIN (never
  argv/env); the supervised-spawn path preserves that stdin seam
  (leaf 13/14). Keep any new spawn wrapper stdin-compatible.

## One live call per employee (C8)

- The registry enforces one live call per employee (invoke mutex) and
  one live call per target number globally; `max_concurrent_calls`
  caps instances (cap <= 0 means unlimited; DefaultConfig sets 2).
  N concurrent calls need N phone employees. The employee `voice_call`
  tool (internal/tools/builtin/voice_call.go) and the inbound path both
  Acquire through the registry - inbound Acquires
  (ccid, voice.default_employee, from) in the spawn seam (leaf 15 fix);
  an empty default_employee is a named boot error.
- Outbound dials reserve internal/effects keyed (employee_id, target,
  request_id) BEFORE the POST and write the audit row BEFORE DialOut;
  a retry never rings twice.

## Process lifecycle (C-lifecycle, leaf 13)

- Child spawn WRAPS llm.RuntimeProcess (pid files, kill logic, orphan
  sweep reuse); do not hand-roll a second mechanism. Idle stop after
  `idle_stop_seconds`; parent death is covered by the daemon
  supervisor mode; only the startup sweep reaps orphans (mirrors the
  root daemon invariants).
- Child RSS is logged at spawn, first call, and idle stop.
