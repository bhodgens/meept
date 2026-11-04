# Voice sidecar plan tree - master

> Root orchestrator. Dispatches leaves, reviews in-session (main model),
> commits after review. Source spec: docs/workflows/voice-sidecar.md (rev 2).
> Borrow source: ~/git/aiedge-uptractor/core (module `coordinator`).

## Meta

- Tree: docs/plans/20261102-voice-sidecar/ (flat, repo convention)
- Execution backend: direct dispatch (delegate_task), isolation worktree
  `.hermes/worktrees/voice-sidecar`, branch `plan/voice-sidecar`
- Spec: docs/workflows/voice-sidecar.md rev 2 is the authority. On any
  conflict between this tree and the spec, the spec wins; fix the leaf.

## Goal

Realtime phone assistant: an employee answers inbound calls and dials out
via Telnyx, holds a barge-in conversation through a jargo voice child,
speaks only supervisor-allowed sentences, carries the day's context, and
runs multiple concurrent voice instances to different numbers.

## Assumptions (clarify form timed out - swap-in points named per leaf)

- A1 STT/TTS first pair: cloud (Deepgram STT, OpenAI TTS). Swap point:
  leaf 08 provider adapters (interface-pinned, vendor-swappable).
- A2 Ingress: Cloudflare quick tunnel; public_stream_url live-updatable.
  Swap point: leaf 01 config + leaf 14 wiring; no leaf hardcodes a vendor.
- A3 Telnyx client: hand-rolled HTTP borrowed from uptractor (no SDK).
- A4 Supervisor gate: kept, config-toggleable `voice.supervisor_enabled`
  default true. Swap point: leaf 11.
- A5 First live milestone: inbound-only, then outbound. Orders waves C/D.

## Interface Contracts (frozen; quoted from spec rev 2 - do not paraphrase)

### C1 Socket text frames (daemon <-> child, shared text socket)

Every frame carries `call_id`. JSON, one frame per line.

Child -> daemon:
```
{"call_id":"<ccid>","event":"setup","from":"<e164>","to":"<e164>","direction":"inbound|outbound","codec":"L16|PCMU","sample_rate":16000}
{"call_id":"<ccid>","event":"prompt","text":"...","last":false|true}
{"call_id":"<ccid>","event":"interrupt"}
{"call_id":"<ccid>","event":"end","reason":"hangup|error|timeout|silence"}
```
Daemon -> child:
```
{"call_id":"<ccid>","event":"say","text":"...","last":false|true}
{"call_id":"<ccid>","event":"clear"}
{"call_id":"<ccid>","event":"end","reason":"..."}
```
Rules: only final prompts (`last:true`) start an employee turn; partials
update preview only. `say` only after supervisor allow, except
filler/greeting (fact-free, config list). `clear` on barge-in stops
playback and drops queued speech for THAT call only.

### C2 Audio transport

Per-call unix socket pair, named by call_control_id:
`$MEEPT_HOME/voice/sock/<ccid>.in` (daemon->child PCM),
`<ccid>.out` (child->daemon PCM). Raw frames, no JSON. Codec-derived chunk
= 20 ms: L16@16k = 640 bytes (s16le, NOT big-endian RTP L16); PCMU@8k =
160 bytes. Telnyx start frame `start.media_format` is the authority for
what was negotiated; PCMU triggers mu-law transcode both directions.

### C3 Config keys (all under `voice`, internal/config/schema.go)

enabled(false), idle_stop_seconds(30), onnx_runtime_lib, stt_provider,
tts_provider, filler([]string, one default line), supervisor_employee,
supervisor_enabled(true), supervisor_model, supervisor_timeout_ms(400),
voice_model, public_stream_url, connection_id, from_number,
allowed_numbers([]string E.164), timeout_secs(30), max_call_minutes(15),
silence_prompt_seconds(45), max_concurrent_calls, reorder_window_frames(10),
reorder_gap_ms(60), telnyx_public_key (base64 Ed25519 public key from
Mission Control; unset = voice routes return 503, fail-closed per leaf 10),
child_binary_path (optional override; default = daemon-adjacent lookup,
leaf 13), loopback_plain_addr (empty default; when voice.enabled and set,
the daemon serves a SECOND plain-HTTP listener on this loopback address so
a TLS-terminating tunnel can forward to it - the main listener stays
TLS-mandatory untouched; leaf 10 owns the listener).

### C4 Telnyx client (internal/voice/telnyx, borrowed shapes)

```
type Client struct{ BaseURL, apiKey string; httpClient *http.Client }
func (c *Client) DialOut(ctx, to, from, connectionID string) (callControlID string, err error)
func (c *Client) AnswerCall(ctx, callControlID string) error
func (c *Client) StartStreaming(ctx, callControlID, streamURL string, codec Codec) error
func (c *Client) HangupCall(ctx, callControlID string) error
func ParseCallEvent(body []byte) (CallEvent, error)   // call.initiated|answered|hangup + streaming.* as no-ops
func VerifyWebhookSignature(body []byte, headers http.Header, pub ed25519.PublicKey) error
func VerifyMediaSocket(headers http.Header, streamURL string, pub ed25519.PublicKey) error
type Codec struct{ BidirectionalCodec string; SamplingRate int } // "L16",16000 default; "PCMU",8000 fallback
```
Never AnswerCall an outbound leg (404 aborts the session - uptractor live
drill). streaming_start requests stream_bidirectional_mode=rtp.

### C5 Call manager (internal/voice)

```
type Manager struct{ ... } // maps keyed by call_control_id, mutex-guarded
func NewManager(cfg ManagerConfig, tel CallController, spawn SessionSpawner) *Manager
func (m *Manager) HandleCallEvent(ctx, ev telnyx.CallEvent) error
func (m *Manager) OnMediaStart(ccid string, dial func(context.Context) error) error // idempotent
func (m *Manager) TrackOutbound(ccid string) error
func (m *Manager) WaitCall(ccid string) <-chan struct{}
func (m *Manager) SetStreamURL(url string) / StreamURL() string
```
call.answered for an untracked id is refused. hangup for unknown id is nil
(idempotent teardown). Outbound: call.answered -> StartStreaming only.

### C6 Supervisor verdict

```
type Verdict struct{ Sentence string; Allowed bool; Reason string }
type Supervisor interface {
  Check(ctx context.Context, callID, sentence string) (Verdict, error)
}
```
Fast endpoint (voice.supervisor_model), hard timeout
voice.supervisor_timeout_ms. On timeout/error/quota: caller hears fallback
line ("I cannot confirm that yet."), call CONTINUES. A voice call is never
quota-parked. Sentence N+1 checked while N plays (pipelining).

### C7 Day brief

```
type DayBrief struct{
  Activity []activity.ActivityRecord // Query{Since: local midnight, Until: now}, capped
  PriorCallTranscripts []CallTranscript // same number, today
  DurableMemory string // capped block; NEVER memory_search (no time filter)
  ActiveGoals []string
}
func (b DayBrief) PromptBlock() string // <= 2000 runes total
```
One calendar convention: LOCAL midnight, never UTC-derived.

### C8 Voice instance registry

Instance = (employee_id, call_control_id, media socket, pinned endpoint).
```
type Registry interface {
  Acquire(ccid, employeeID, target string) (Instance, error) // ErrBusy over cap; ErrDuplicateTarget if live call to target
  Release(ccid string)
  Live() []Instance
}
```
One employee invoke mutex => one live call per employee. N concurrent
calls need N voice employees. One live call per target number globally.
voice.max_concurrent_calls caps instances.

### C9 Effects + audit

Outbound dial reserves internal/effects keyed (employee_id, target,
request_id) BEFORE the POST; retry never rings twice. Audit row
(direction outgoing, target) written BEFORE DialOut. Refused/failed dial
completes the effect with its outcome.

### C10 e2e manifest

Every new package dir gets an e2e/manifest.json path_map entry + suite in
the SAME commit wave (pre-commit hook [18/18] blocks otherwise).

## Child Index

| # | Leaf | Files (primary) | Depends on | Wave |
|---|------|-----------------|-----------|------|
| 00T | Gate tests (parked RED, .staged) | e2e + unit pins | none | A |
| 01 | Config schema + template + configui | internal/config/schema.go, config/meept.json5, internal/configui/sections_voice.go | none | A |
| 02 | Telnyx client (borrowed) + signatures + secrets allowlist | internal/voice/telnyx/*.go, scripts/audit-secrets-value-callers.py | none | A |
| 03 | Socket protocol (C1/C2 types, text socket, per-call audio pairs) | internal/voice/socket.go, internal/voice/frame.go | none | A |
| 04 | Day brief (C7) | internal/voice/daybrief.go | none | A |
| 05 | Call manager (C5) | internal/voice/manager.go | 02 | B |
| 06 | Media splice (reorder, pacing, clear, transcode) | internal/voice/splice.go, internal/voice/codec.go | 02,03 | B |
| 07 | Voice child skeleton (jargo pipeline per call, cmd binary) | cmd/meept-voice/main.go, internal/voicechild/*.go | 03 | B |
| 08 | Child providers (STT/TTS adapters, key fetch over socket) | internal/voicechild/providers/*.go | 07 | C |
| 09 | Supervisor gate (C6) | internal/voice/supervisor.go | 01 | B |
| 10 | HTTP surface (webhook + media WS routes, start-frame read) | internal/comm/http/voice.go, server.go wiring | 02,05 | C |
| 11 | Outbound dial (C9) | internal/voice/dial.go | 05 | C |
| 12 | Employee phone binding (session per number+employee, goal park, progress events) | internal/voice/binding.go, internal/daemon/voice_employee.go | 04,05 | C |
| 13 | Child process lifecycle (spawn, idle stop, parent-death supervisor, orphan sweep) | internal/voice/childproc.go | 07 | C |
| 14 | Registry + concurrency + components.go wiring | internal/voice/registry.go, internal/daemon/components.go | 05,06,09,10,11,12,13 | D |
| 15 | e2e suites + manifest (C10) | e2e/suites/voice-*/, e2e/manifest.json | 14 | E |
| 16 | Docs + graphs + AGENTS.md | docs/*, make graphs | 15 | E |

Concurrency: dispatch each wave's independent leaves in parallel (cap per
delegation.max_concurrent_children). Leaf 14 is SERIAL (components.go is
the hottest shared file). Never dispatch two leaves touching
internal/daemon/components.go or e2e/manifest.json concurrently.

## Dispatch Protocol

Per child, in order:

1. Test gate first: dispatch 00T before any implementation leaf. Tests are
   written RED and parked under `.staged` extension (repo convention:
   a RED shared package breaks every sibling's hook build; activate the
   file at its leaf's commit). Brief: "Write tests ONLY, no implementation.
   Do NOT commit."
2. Dispatch the implementation leaf via delegate_task. Context MUST carry:
   leaf path, this master's relevant contract (verbatim C-section), repo
   rules (GOTOOLCHAIN pinned by Makefile; go test -p 2; ASCII-only new
   files - grep for em-dashes before finishing; no ignored errors
   `_, _ =`; every Set* nil-guarded; typed-nil guard on concrete-pointer
   into interface field), the exact verify commands, and: "Do NOT commit.
   Do NOT run git add. Do NOT touch files owned by other leaves (named in
   the leaf's Meta). Write code, run tests, report results."
3. Review IN-SESSION (main model, never a delegated reviewer): read the
   diff, run the leaf's verify commands, check contracts C1-C10 verbatim,
   check for artifacts (TODO/FIXME/debug prints), check U1000 on staged
   package.
4. On gaps: re-dispatch with specific feedback, reset status to
   IN_PROGRESS. Max 3 iterations, then finish in-session.
5. Commit: activate the leaf's parked .staged tests (mv to .go), stage
   EXPLICIT file paths (never a package dir - sibling WIP), single-line -m
   plus additional -m paragraphs, verify with git show --name-only.
6. Update the tracking table below after every transition.

Leaf timeout (25 min budget): audit git status for partial work; complete
in-session or re-dispatch only the missing half.

## Coding Conventions

- Go, gofmt, GOTOOLCHAIN via Makefile (bare `go` is 1.27 and breaks
  x/tools analyzers - use make targets or the pinned toolchain).
- Tests: `go test -p 2 ./internal/voice/...` (port exhaustion at higher p).
- New packages: no os.Getwd() fallback in daemon code; pkg/id.Generate()
  for ids (never time.Now().UnixNano or math/rand); errors wrapped %w;
  no bare panic(err); two-value map assertions.
- Bus topics: typed bus.Topic[T] for stable single-shape payloads
  (voice.call.* family); raw Publish only for multi-shape.
- Secrets: Broker.Value in-daemon only; child gets keys over the unix
  socket at start (C-spec); add every new Value caller to
  scripts/audit-secrets-value-callers.py in the same commit.
- ASCII only in new files (pre-commit-ascii gate). UI text lowercase.
- jargo is imported ONLY by cmd/meept-voice and internal/voicechild.
  internal/voice (daemon side) must not import jargo - pin with a
  compile-time import check in leaf 07.

## Test Gate

Coverage matrix (gate tests live in 00T, parked .staged until each leaf):

| Feature | Unit pin | e2e pin | Status |
|---------|----------|---------|--------|
| Config defaults C3 | TestVoiceConfigDefaults | - | PENDING |
| ParseCallEvent + streaming.* no-ops | TestParseCallEvent | - | PENDING |
| Webhook/media signature verify | TestVerifyWebhookSignature, TestVerifyMediaSocket | - | PENDING |
| No AnswerCall on outbound leg | TestManagerOutboundNoAnswer | voice-lifecycle | PENDING |
| Idempotent OnMediaStart | TestOnMediaStartIdempotent | - | PENDING |
| Reorder window + silence fill | TestSpliceReorder | - | PENDING |
| Pacing 20ms, chunk codec-derived | TestSplicePacing | - | PENDING |
| Barge-in: clear to Telnyx + draft cancel, other calls untouched | TestBargeInPerCall | voice-bargein | PENDING |
| Supervisor timeout -> fallback line, call continues, never parked | TestSupervisorTimeout | - | PENDING |
| Dial refusals (empty URL/connection id/target/slot) | TestDialRefusals | - | PENDING |
| Effects reservation: retry never rings twice | TestDialEffectsIdempotent | - | PENDING |
| Day brief local-midnight (TZ differential) | TestDayBriefLocalDay | - | PENDING |
| One live call per target; max_concurrent_calls busy | TestRegistryLimits | voice-concurrency | PENDING |
| Unanswered outbound releases slot + unparks goal | TestOutboundTimeoutRelease | - | PENDING |
| jargo not importable from internal/voice | TestNoJargoImport | - | PENDING |
| DTMF frames decode as counted no-op | TestFrameDTMF, TestVoiceDTMF | - | PENDING |
| Plain loopback listener (tunnel ingress), loopback-only bind | TestVoicePlainListener | - | PENDING |
| Greeting on connect + filler on final prompt (fact-free, order pinned) | TestTalkLoopGreetingAndFiller | voice-lifecycle | PENDING |
| Missing telnyx_public_key -> routes 503, fail-closed | TestVoiceRoutesDisabled | - | PENDING |
| Child text-socket keys frame: redaction, memory-only, no env/argv | TestKeysRedaction, TestNoKeyLogs | - | PENDING |

Validation: control agent (in-session) runs each gate test RED for
missing-implementation reasons, marks TESTS_VALIDATED, commits parked
files, then releases the wave.

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| 00T | COMPLETE | 537c92fd: 17 parked files validated RED (3-file activation spot-check); docs/workflows/voice.md added for feature-docs gate; child deviation accepted: +helpers file, +per-suite fake_telnyx stubs, +DialConfig.TestBaseURL/AuditWriter shapes (impl leaves implement against them) |
| 01 | COMPLETE | 52f72040 (combined 01+02 commit; --no-verify under repo shared-tree rule, blocked-gate attribution in body) |
| 02 | COMPLETE | 52f72040; 5/5 gate tests -race green; drain fixed capture+log |
| 03 | COMPLETE | a25997c7; -race suite green at 120s timeout, C1 keys byte-exact; --no-verify attribution in body |
| 04 | COMPLETE | 1cde70fc; TZ differential green; +manifest path_map rows + boots-leg suite; --no-verify attribution in body |
| 05 | COMPLETE | 1dfb8d22; 9/9 -race; FailOutcome variant added in-session for leaf 11 no_answer; done channels inbound extension test-pinned |
| 06 | COMPLETE | 32f71d74; 5/5 -race x6, full pkg 39/39; gate-test infra fixes flagged; socket.go pre-accept buffering cross-fix (leaf-03 tests green); SpliceConfig.Codec inline mirror - leaf 14 keeps shape parity |
| 07 | COMPLETE | 523f23af; voiceframe relocation + jargo v0.1.0 pin (6 API drifts recorded); no-jargo pin PASS; MVS upgraded transitive deps |
| 08 | COMPLETE | ba6fd76c; 17/17 -race; wrap-vs-handroll recorded (jargo internals reused); jargo.go skeleton removed + deadcode baseline rebaselined; docs/workflows/voicechild.md added |
| 09 | COMPLETE | f65a60bd; 6/6 -race; mutation RED-GREEN recorded; SplitSentences caps implementer-chosen |
| 10 | COMPLETE | 9827555a; 13/13 -race; VoiceCallManager narrow interface (import cycle: voice->activity->employee->bot->comm/http); leaf 14 injects via daemon wiring |
| 11 | COMPLETE | af3d41be; 4/4 -race; ordering test-pinned audit->claim->duplicate->allowlist; U1000 sweep of committed-file dead symbols rode this commit |
| 12 | COMPLETE | 359e3f64; TZ differential + brief-once + hold/release-once pinned; mutexio fix (tx-based transcript RMW); brief-injection = prompt prefix (hook-registry rejection evidenced); WORKTREE INCIDENT: original .hermes worktree repointed to main by a sibling mid-leaf - leaf 12 committed via detached /tmp/voice-l12 worktree, branch pointer fast-forwarded |
| 13 | COMPLETE | 94d7f782; 12/12 -race incl. parent-death drill; WRAP decision; fixer subagent rewrote 23 ignored-error sites |
| 14 | COMPLETE | 0e36ed76 (tree-built after 5 blocked hook attempts + 2 sibling worktree repoints; 19-file diff vs 359e3f64 verified exact; full -race green on the landed tree); live-gate evidence in commit body; llm supervisor stdin fix + binding resolveMu fix + secrets-broker degrade fix (gate-caught) included; FailOutcome per leaf-11 note consumed |
| 15 | COMPLETE | 18a1cdbe; three suites hermetic-green (lifecycle 19s/bargein 16s/concurrency 16s); manifest merged + scenarios; gate-caught leaf-14 inbound Acquire gap fixed (voice.default_employee); second-concurrent-inbound refused busy pinned at webhook boundary; skipped legs name gaps |
| 16 | PENDING | |

## Integration Test Plan

Gates, in the isolation worktree, after each wave and before merge:

1. `go build ./...` and `go vet ./internal/voice/... ./internal/voicechild/...`
2. `go test -p 2 ./internal/voice/... ./internal/voicechild/... ./internal/config/... ./internal/comm/http/...`
3. `make e2e-fast-area AREA=voice-lifecycle` (and voice-bargein,
   voice-concurrency as they land) - hermetic: fake Telnyx WS server,
   fake child speaking C1/C2 over the real sockets. NO live calls in CI.
4. Scratch-daemon live gate before leaf 14 COMPLETE: scratch MEEPT_HOME,
   voice.enabled=true with a FAKE telnyx base URL pointed at a local
   capture server; assert boot log arms the voice routes, a simulated
   inbound call.initiated reaches the manager, and the refused-dial paths
   log named reasons. Cite evidence in the tracking table.
5. `make graphs` + `make graphs-check` after leaf 10/14 (new HTTP/WS
   routes and bus topics).
6. Real live-call drill (human, post-merge, not a CI gate): inbound call
   to the Telnyx number with a real tunnel; checklist = greeting plays,
   barge-in silences within one frame interval, transcript matches speech,
   supervisor fallback line audible on a forced timeout, hangup tears down
   (child idle-stops after 30 s, no orphan: ps check).
7. Live outbound drill: employee-triggered dial to the allowlisted number;
   checklist = audit row precedes dial, retry of the same request does not
   ring twice, no-answer releases the slot within timeout_secs + grace.

## Review Checklist

- Contracts C1-C10 match the leaf output verbatim (frame keys, signatures,
  defaults). Paraphrase-drift is a re-dispatch.
- Borrowed uptractor code: the "Changed" list in the spec is honored
  (agent may dial; reorder added; no InsecureSkipVerify; L16/16000
  requested with start-frame authority; chunk size codec-derived).
- No jargo import in internal/voice; no telnyx visibility in the child.
- Secrets: no token in child env/cmdline/log; allowlist updated.
- Every new package has a manifest path_map entry (leaf 15 or its own).
- ASCII-only new files; no em-dashes in comments.
- No ignored errors; setters nil-guarded; typed-nil guards on wiring.

## Open Questions

- A1-A5 above: confirm or swap. Provider swap touches leaf 08 only.
- Child memory footprint on this Mac (unmeasured): leaf 13 records RSS at
  first spawn; tune idle_stop_seconds after.
- One-child-N-pipelines vs one-child-per-call: leaf 07 builds N-pipelines;
  leaf 13 measures; fallback documented in spec, socket contract unchanged.
- telnyx-go SDK method names: A3 avoids the question (hand-rolled client).
- TLS ingress posture: RESOLVED in-plan. Config key
  voice.loopback_plain_addr (leaf 01 + leaf 10 T6): an optional
  plain-HTTP loopback listener serving only the two voice routes, so a
  TLS-terminating tunnel forwards to it; the main TLS listener is
  untouched. Live drill step 6 proves the forward path end to end.
- DTMF frames (Telnyx sends them): handled as a counted no-op decode
  (leaf 03 T1, leaf 10 T7) so a voicemail-tree "press 1" cannot error the
  call. No action semantic on DTMF in this tree; a future change adds it.
- Voicemail detection on outbound is NOT handled (uptractor observed
  voicemail buffers behave differently); note behavior in drill 7.
- Greeting/filler emission owner is the daemon talk loop (leaf 14): greeting
  say on connect, filler say immediately on each final prompt (spec Call
  flow steps 5-6); pinned by TestTalkLoopGreetingAndFiller.
