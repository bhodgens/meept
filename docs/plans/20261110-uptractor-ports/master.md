# Uptractor Ports — Master Orchestrator

Date: 2026-11-10 · Repo: meept · Source repo: ~/git/uptractor-jobfile (branches `fix/coordinator-missed-hangup-watchdog`, `fix/speech-robustness`, `claude/uptractor-repo-organization-5jusi4`).

## Goal

Port the GENERIC (non-Uptractor-domain) capabilities and bugfixes from the
uptractor-jobfile phone stack into meept: the live-call reaper + Telnyx
is_alive probe + ignored-event classification, the fuzzy-resolution module and
utterance normalization, the structured LLM call brief, guided intake for the
secretary employee, Telnyx call recording, the sidecar eval harness, and the
speech research docs. Everything Uptractor-domain-specific (jobview dashboard,
follow_up_sms/crm_note brief fields, dispatch board audience rules) lives in
the sibling repo `meept-uptractor` and is NOT part of this tree.

## Assumptions (owner did not answer clarifying form; recommended defaults taken — veto before dispatch)

- A1: One forest, two trees. This tree = generic ports into meept. Sibling
  tree = ~/git/meept-uptractor/docs/plans/20261110-dashboard/.
- A2: Call brief schema in meept is GENERIC (summary, action_items,
  commitments, risk_flags, transcript, status, error). Uptractor-domain fields
  (follow_up_sms, crm_note) ride an opaque `ext JSON` column that
  meept-uptractor defines and interprets; meept never reads it.
- A3: Recording ports WITH the consent gate (announcement text pinned before
  recording may be enabled; all-party-consent states).

## Architecture Overview

- `internal/voice/telnyx/` gains `GetCallStatus` (GET /calls/{id}, is_alive;
  404 => Known=false) and typed `IgnoredEventError` for non-lifecycle call.*
  webhook events (ack quietly, log debug; non-call.* events stay hard errors).
- `internal/voice/` gains a reaper goroutine in the Manager: probes sessions
  older than MinAge, releases them via the EXISTING `FailOutcome` funnel
  (manager.go:539 — same releaseOnce path, frozen 20261109; the reaper adds an
  input edge, never a second teardown path; it does NOT go through
  HandleCallEvent, whose hangup branch hard-codes reason "call.hangup" at
  manager.go:417), enforces MaxDuration, and emits a `CallEventErrored`
  (reason "reaped:hangup-lost" / "reaped:max-duration") on the CallLeg bus so
  faults are observable. The reaper keeps its OWN {ccid: startedAt} map fed
  by the existing AddAnsweredObserver/AddTerminalObserver lanes — the Manager
  gains no new session state (its maps carry no timestamps).
- `internal/voice/` gains the structured call brief: stub row at answer
  (state 'live' already exists in callstore), finalized brief at release,
  idempotent across webhook replay and restart; brief JSON stored in a new
  additive `brief TEXT` column on voice_calls (guarded CREATE/ALTER migration)
  plus `ext TEXT` (opaque). Commitments extracted from the brief are filed as
  ledger rows (kind='commitment', source='voice', source_ref=ccid) via the
  existing ledger Append API — never direct SQL.
- `internal/util/fuzzy/` (new small package, stdlib-only): EditDistance,
  Similarity (token-aware, per-token edit distance weighted), FuzzyFind
  (threshold, sorted desc). Go port of src/lib/agent/fuzzy.ts; the canonical
  test table from docs/SPEECH-ROBUSTNESS.md ships verbatim as table tests.
- `internal/voicechild/` gains utterance normalization (NormalizeUtterance:
  repeated-word collapse, filler-run collapse, noise-token drop with a
  preserve-list hook) applied to STT finals BEFORE the LLM turn; preserve-list
  is caller-supplied (employee context names/subjects), not hardcoded.
- `internal/employee/` + `internal/voice/`: guided intake mode as an employee
  behavior preset (identify caller -> one question at a time -> read-back ->
  confirm-then-save), gated by employee config; ambiguous fuzzy matches REFUSE
  and list candidates (never auto-pick below exact match).
- Recording: telnyx client `StartRecording` (dual-channel mp3) on answer +
  `call.recording.saved` URL capture into the call record, behind config
  `voice.record_calls` (default off) AND a consent-announcement pin.
- Eval harness: port accuracy + prompt-leak evals as a persistent lane
  (`make test-voice-eval`) driving the real sidecar/voicechild with recorded
  WAV fixtures; failures are loud, fixtures are committed.

## Interface Contracts

- C1 CallStatus: `func (c *Client) GetCallStatus(ctx context.Context, callControlID string) (CallStatus, error)`; `type CallStatus struct { Known, Alive bool }`. 404 => Known=false, Alive=false, err=nil. Empty id => error. (Ported verbatim in shape from phone/core/internal/telnyx/callstatus.go.)
- C2 IgnoredEvent: `type IgnoredEventError struct{ EventType string }`; `func IsIgnoredEvent(err error) bool` (errors.As). Only for `call.*` non-lifecycle events; non-call.* remain plain errors.
- C3 ReaperConfig: `type ReaperConfig struct { Status CallStatusChecker; Interval, DeadAir, MaxDuration time.Duration }` — SEMANTICS per owner 2026-11-11: the reaper releases a call only after DEAD AIR — no caller-side audio/response for `DeadAir` (default 60s), NOT merely age-since-answer. The reaper's {ccid: lastActivity} map is fed by the answered observer AND refreshed on every caller-side audio/turn event; a probe fires only when now-lastActivity > DeadAir. MaxDuration (default 30m, from call start) remains the absolute cap. Interval default 30s (values frozen from phone/core/internal/callsvc/reaper.go). `func (m *Manager) RunReaper(ctx context.Context, cfg ReaperConfig)` — no-op + Error log when Status nil. CallStatusChecker is a STANDALONE interface satisfied by *telnyx.Client; do NOT extend telnyx.CallController (adding GetCallStatus there breaks every existing test fake). Reaper releases via `FailOutcome(ccid, outcome, reason)` (manager.go:539) so the frozen reason strings ride the release; no Manager state or constructor change. Config keys: voice.reaper_interval_s / voice.reaper_dead_air_s / voice.reaper_max_call_minutes (0 => defaults; negative disables the reaper).
- C4 Brief JSON (frozen wire shape; matches Uptractor README-callbrief record):
  `{"status":"in_progress|completed|failed|abandoned","error":"...","from":"+1...","to":"+1...","brief":{"transcript":"","summary":"","action_items":[],"commitments":[],"risk_flags":[]},"ext":{...}}`
  Go type `voice.CallBrief` mirrors this; `ext` is `json.RawMessage`, opaque to meept.
- C5 Fuzzy: `package fuzzy`; `func EditDistance(a, b string) int`; `func Similarity(a, b string) float64`; `func FuzzyFind[T any](query string, candidates []T, getName func(T) string, threshold float64) []T` (sorted descending; epsilon comparisons per Pitfall 26). Thresholds used by callers: jobs/subjects 0.60, short descriptions 0.55 (frozen from SPEECH-ROBUSTNESS.md).
- C6 Normalize: `func NormalizeUtterance(text string, preserve func(token string) bool) string` in internal/voicechild (exported for tests); idempotent (Normalize(Normalize(x)) == Normalize(x)).
- C7 Ledger commitment filing: brief commitments file as `ledger.Append(kind='commitment', source='voice', source_ref=ccid, subject=<resolved subject>)`; exactly-once per (ccid, commitment-index) — dedupe by querying existing source_ref rows before append.

## Test Gate

Coverage matrix (gate leaf 00T writes ALL of these BEFORE any impl dispatch; tests must fail RED for missing-implementation reasons):

| Feature | Unit test | E2E/integration test | Status |
|---|---|---|---|
| C1 GetCallStatus | internal/voice/telnyx callstatus_test.go (200 alive/dead, 404, empty id, HTTP error) | — | PENDING |
| C2 IgnoredEvent | telnyx webhook parse tests (call.analyzed ignored+acked; non-call.* hard error) | — | PENDING |
| C3 Reaper | internal/voice reaper_test.go (DEAD-AIR semantics per owner: call with fresh caller activity NOT reaped regardless of age; call silent > DeadAir reaped via FailOutcome reason "reaped:hangup-lost"; MaxDuration force-hangup; nil Status no-op; defaults 30s/60s/30m) | manager-level: tracked session + fake Status => releaseOnce fired exactly once, CallEventErrored on bus | PENDING |
| C4 Brief | callstore brief column migration idempotence; CallBrief JSON round-trip; stub-at-answer/final-at-release | replayed hangup webhook => still exactly one brief; restart mid-call => abandoned brief | PENDING |
| Commitments->ledger | dedupe exactly-once per (ccid,index) | — | PENDING |
| C5 Fuzzy | canonical table from SPEECH-ROBUSTNESS.md verbatim ("Meadow Rich"->Meadow Ridge; "summer planes"->no match; etc.) | — | PENDING |
| C6 Normalize | filler collapse, known-name preservation, idempotence | voicechild pipeline: garbled final reaches LLM normalized | PENDING |
| Guided intake | refuse-and-list on ambiguous fuzzy; confirm-then-save gate | — | PENDING |
| Recording | StartRecording request shape; recording.saved capture; disabled-by-default; consent pin required | — | PENDING |
| Eval harness | lane runs, fails on planted prompt-leak fixture | — | PENDING |

## Child Index

| # | Leaf | Scope | Deps | Est. context |
|---|---|---|---|---|
| 00T | 00T-gate-tests.md | All gate tests above, RED | none | 90K |
| 01 | 01-telnyx-status-and-ignored-events.md | C1+C2 | 00T | 70K |
| 02 | 02-call-reaper.md | C3 + errored events | 01 | 85K |
| 03 | 03-fuzzy-and-normalize.md | C5+C6 | 00T | 75K |
| 04 | 04-call-brief-and-commitments.md | C4+C7 | 00T | 95K |
| 05 | 05-guided-intake.md | guided intake for the SECRETARY (internal/voiceintake pkg; scoped to the secretary employee definition, not a generic employee knob) | 03 | 80K |
| 06 | 06-call-recording.md | recording + consent gate | 01 | 70K |
| 07 | 07-sidecar-eval-harness.md | persistent eval lane | 00T | 75K |
| 08 | 08-research-docs-import.md | docs-only: 4 research docs -> docs/research/ | none | 35K |

## Dispatch Protocol

Execute per the hierarchical-planning skill. Isolation worktree:
`git worktree add .hermes/worktrees/20261110-uptractor-ports -b plan/20261110-uptractor-ports`.
Wave order: 00T first (control agent validates gate tests -> TESTS_VALIDATED ->
commit). Then wave 1: leaves 01, 03, 08 (file-disjoint: telnyx/, util+fuzzy/,
docs/). Wave 2: 02, 04, 06, 07 (02 depends on 01; 04 touches callstore+ledger
wiring; 06 touches telnyx client + manager answer path — SERIALIZE 06 after 02
because both edit internal/voice/manager.go). Wave 3: 05 (depends on 03+04).
Every dispatch context includes: leaf path, this master's contracts C1-C7,
"Do NOT commit / Do NOT git add", corruption rule (write once, never read
back), exact verify commands. Orchestrator (main model) reviews in-session,
commits per leaf with pathspec, updates the tracking table.

## Coding Conventions

- Go 1.24+, gofmt, staticcheck clean (pre-commit hooks run repo-wide gates;
  stale lint blocks — run `make lint` scope before commit).
- SQLite stores: never query while Rows/Tx open (MaxOpenConns(1) self-deadlock
  class); drain+close first.
- No new dependencies without necessity; modernc driver for any new SQL.
- slog structured logging; no fmt.Println.
- Tests: table-driven, testify not required; -p 2 for package sweeps.
- Frozen shapes (releaseOnce funnel, CallLeg enum, telnyx.CallController) are
  additive-only: new observer/input edges, never a second teardown path;
  GetCallStatus goes on *Client + a standalone CallStatusChecker, NOT onto
  CallController.
- Package naming: internal/intake is TAKEN (GitHub intake-factory) — guided
  intake is internal/voiceintake; internal/voice/eval coexists with the
  existing top-level internal/eval (name-similar, no conflict — import paths
  must be explicit).
- internal/voice deliberately does not import internal/ledger today; leaf 04
  keeps commitment filing in daemon wiring (internal/daemon) to preserve that
  posture.

## Completion Tracking Table

| Leaf | Status | Notes |
|---|---|---|
| 00T | TESTS_VALIDATED | Gate committed in worktree 2026-11-11; 58 tests, all RED undefined-symbol; DEAD-AIR semantics pinned (short explicit DeadAir in race tests, defaults pinned numerically); "summer planes" verbatim; C7 pins FileCommitments/FileCommitmentsFromBrief voice-side seam (daemon wiring lands in leaf 04); recording pins RecordingPolicy + frozen mp3/dual wire shape; Makefile test-voice-eval RED stub + demo-utterance.wav fixture staged |
| 01 | REVIEWED → COMPLETE | All telnyx gate tests green; webhook handler ACKs ignored events (debug, 2xx) and keeps 400 for misrouted. Deviation accepted: recording.go + RecordingSavedEvent ported early because the 00T recording gate tests live in the same package and blocked compilation — leaf 06's telnyx-client half (T1) is pre-landed; its manager/consent/callstore work (T2-T4) remains. Reverted an unrelated go.mod pflag promote (tidy side-effect, not this leaf's) |
| 02 | REVIEWED → COMPLETE | All 8 reaper gate tests PASS (dead-air discriminator, race exactly-once, bus event, defaults). Mechanism per C3+audit: own {ccid:lastActivity} map (answered observer + new NotifyCallActivity lane from routeFrame final-prompt), FailOutcome release with frozen reasons, standalone CallStatusChecker, no constructor/state change; call_errors table + LogCallError; voice_wiring + ReaperIntervalS/ReaperDeadAirS/ReaperMaxCallMinutes (0=default, negative=off). Note: probe fires each sweep; DeadAir gates the RELEASE (gate-pinned). Leaf 06's manager/callstore half remains; leaf 04's callbrief/brief_commitments gate tests still RED by plan (block ./internal/voice full-suite compile until leaf 04 lands — expected) |
| 03 | REVIEWED → COMPLETE | fuzzy 9/9 + full voicechild suite green; binding stores RAW, dedupe compares raw, LLM prompt normalized (ctx transport WithRawTranscript). Gate-test corrections accepted (dated in-file): EditDistance constants corrected to true arithmetic 3/1/4 (doc's narrative numbers were wrong — verified by computation); FuzzyFind returns []Match[T]{Item,Score,Name} (type-param embedding illegal in Go); Similarity frozen as top-two query-token vector (literal TS mean contradicts the canonical table — documented in package header) |
| 04 | REVIEWED → COMPLETE | Full ./internal/voice suite green (24 brief/commitment/recording gate tests PASS, 0 fail) — compile block cleared. SQL-layer idempotent finalize (only ''/in_progress overwritten — no replay window); CommitmentFiler seam with HasCommitment dedupe + daemon ledger adapter (subject call:<ccid>, payload {idx,text}); wireBriefObservers post-release goroutine (failures WARN-only, never delay release); O1 mapping via reaped:* reason prefix; SweepStaleLive abandonStubs with json_valid guard (fixed a pre-existing daemon test json_extract-on-blank crash). Deviations accepted: non-omitempty error/ext (gate-pinned exact 6-key marshal); explicit HasCommitment seam method; leaf-06 RecordingPolicy/recording_url prerequisites pre-landed (compile requirement — leaf 06 keeps manager/consent/wiring work) |
| 05 | PENDING | |
| 06 | REVIEWED → COMPLETE | All 4 recording gate tests PASS + full voice suite green. Manager additive fields + recording.saved case + answer-path start after streaming start; fail-closed wiring (record_calls + empty pin ⇒ Error + unarmed); phone-leg-only posture (same degrade as reaper); RecordCalls/RecordConsentPin config knobs. Deviation accepted: record_start failures Error-logged and swallowed (a 500 would trigger Telnyx redelivery mid-call). Pre-landed halves from leaves 01/04 confirmed and consumed |
| 07 | PENDING | |
| 08 | REVIEWED → COMPLETE | All 4 docs byte-identical below provenance headers (diff-verified against source branch); committed |

## Integration Test Plan

After all leaves REVIEWED: (1) `go build ./... && make lint && go test -p 2
./internal/voice ./internal/voice/telnyx ./internal/voicechild ./internal/util/fuzzy ./internal/employee`;
(2) e2e: fake-Telnyx harness — answer a call, drop the hangup webhook, verify
the reaper ends it within Interval+MinAge with exactly one errored event, one
finalized abandoned brief, and commitments filed once in the ledger; (3) eval
lane green; (4) merge worktree to main per skill step 4.

## Review Checklist

- [ ] Contracts C1-C7 implemented exactly (signatures, defaults, thresholds verbatim from source repo)
- [ ] releaseOnce remains the single terminal funnel; reaper feeds synthetic hangup, not a parallel teardown
- [ ] Brief idempotence proven across replay AND restart
- [ ] Fuzzy canonical table passes verbatim
- [ ] No Uptractor-domain leakage into meept (no follow_up_sms/crm_note typed fields; ext stays opaque)
- [ ] Recording default-off and consent-gated
- [ ] Gate tests untouched by implementers

## Open Questions

- O1: Should reaped-call briefs say "abandoned" (Uptractor) or "failed"
  (meept SweepStaleLive outcome)? Rec: keep both distinct — startup sweep =
  'failed' (process died), live reaper = 'abandoned' (hangup lost). Affects C4
  status enum only; both values already in the frozen enum.
- O2: RESOLVED per owner 2026-11-11 — guided intake is scoped to the
  SECRETARY employee definition (not a generic employee knob); reaper uses
  DEAD-AIR semantics (DeadAir 60s default) instead of age-since-answer.
  Reaped-call briefs: startup sweep = 'failed' (process died), live reaper =
  'abandoned' (hangup lost) — both values in the frozen enum.
