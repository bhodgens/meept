# Employee contact channels - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 9 (3 branches, 6 leaves)
- **Scope:** Employees contact a person over multiple channels (Telnyx voice, Signal voice, Signal message, Telnyx SMS) with per-person fallback policy; Signal voice/msg ride a signal-cli sidecar subprocess; SMS rides the existing Telnyx account.

## Goal

Today the only way a meept employee reaches a human is the Telnyx `voice_call` tool. This tree adds Signal voice calls (issue #119), Signal text messaging (#120), Telnyx SMS (#121), and a per-person contact registry with an ordered fallback chain (#118) so one employee action - "reach this person with this message" - tries channels in configured order (e.g. phone -> signal voice -> signal msg -> SMS) and reports per-channel outcomes. The ended-state wiring gap that all outcome detection reads (#123) is fixed first. Slack async voice clips (#122) and the Zoom legs (#115/#116/#117) are tracked but OUT of this tree (Zoom held pending its own build-vs-buy decision; Slack clips are a small follow-on).

External facts this tree relies on (investigated 2026-11-08, reports archived at `.research/`):

- signal-cli >= v0.13.0 ships 1:1 voice calling (PR #1932): json-rpc methods `startCall`/`acceptCall`/`hangupCall`/`listCalls`/`subscribeCallEvents`, `callEvent` notifications, socket-audio mode via `signal-call-tunnel` (RingRTC). One process per Signal account; one live call per account. Docs incomplete - the Rust tunnel source is the framing authority.
- Signal addressing key = Signal UUID (observed from envelopes); phone E.164 works for SMS and as discovery.
- Telnyx messaging: same API key as Call Control; separate Messaging Profile required; webhooks use the same Ed25519 `telnyx-signature` scheme; `message.received` inbound, `message.finalized` terminal DLR. US 10DLC needs brand+campaign registration - toll-free verified is the fast compliance path.
- Signal outbound messaging is rate-limited/abuse-gated: throttle + allowlist mandatory; privacy-setting send failures are channel-unavailable, not daemon bugs.

## Architecture

Four new subsystems, three of them legs or channels inside the existing voice/messaging seams:

1. **Contact foundation** (`internal/contact`): per-person registry (phone E.164 + nullable signal UUID + per-contact channel policy) and the fallback chain engine. The chain is employee-facing: one tool action, ordered channel attempts, per-attempt failure detection, no double-notification (chain execution id dedupe), effects + audit rows per attempt.
2. **Signal sidecar leg** (`internal/signal` + subprocess supervision): a signal-cli json-rpc daemon subprocess per Signal account, supervised like voicechild. Two channels ride it: voice calls (audio bridged into the sidecar's per-call PCM pairs, STT-partial barge-in, effects+audit before startCall) and text messaging (notification stream -> session persistence).
3. **Telnyx SMS** (`internal/telnyx` messaging client + webhook receiver): reuse the existing Telnyx signature verifier; new messaging profile config; DLR correlation feeds the chain's failure detection.
4. **Prereq fix**: phone-leg ended-state hold registration (#123) so outcome detection reads honest call rows.

The voice sidecar child (jargo, `cmd/meept-voice`) does NOT change for the Signal leg: it sees PCM on the C2 pair and JSON on C1. The Signal leg is a daemon-side media adapter exactly like the local leg is.

## Interface Contracts

### C1: Contact record + store

```
package contact // internal/contact/types.go, internal/contact/store.go

type Channel string // "telnyx_voice" | "signal_voice" | "signal_msg" | "telnyx_sms"

type Record struct {
    ID          string    // stable person id (pkg/id.Generate at create)
    DisplayName string
    Phone       string    // E.164, normalized at write; required
    SignalUUID  string    // empty until observed; settable by config
    Policy      []Channel // ordered fallback chain; empty = employee default
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type Store interface {
    Put(ctx context.Context, rec Record) error
    Get(ctx context.Context, id string) (Record, error)   // ErrUnknownContact when absent
    GetByPhone(ctx context.Context, e164 string) (Record, error)
    GetBySignalUUID(ctx context.Context, uuid string) (Record, error)
    List(ctx context.Context) ([]Record, error)
    Delete(ctx context.Context, id string) error
}
// SQLite-backed (pattern: internal/voice/callstore.go); schema migration table `contacts`.
// Owner: 01-contact-foundation (leaf 01)
// Consumers: 02 (chain), 11 (signal legs), 12 (sms), employee tool wiring
```

### C2: Channel attempt seam (the chain -> channel adapter)

```
package contact // internal/contact/chain.go

// ChannelAttempt is the seam every channel implements. Attempt is
// SYNCHRONOUS for message channels (returns final outcome) and
// call-attempt-shaped for voice (returns after terminal/no-answer, not
// after a full conversation - a connected voice call is NOT a chain
// failure; the chain releases and the call proceeds on its own session).
type ChannelAttempt interface {
    Channel() Channel
    // Attempt returns (outcome, err). err == nil means the channel
    // CONNECTED/DELIVERED (voice answered, message finalized delivered).
    // Named outcomes for telemetry: "answered" | "delivered" | "no_answer"
    // | "unavailable" | "busy" | "failed".
    Attempt(ctx context.Context, rec Record, message string) (outcome string, err error)
}

type ChainRunner struct{ /* attempts injected per channel */ }
func NewChainRunner(attempts map[Channel]ChannelAttempt, delays map[Channel]time.Duration) *ChainRunner
func (c *ChainRunner) Run(ctx context.Context, rec Record, message string, override []Channel) ChainReport
// ChainReport: per-attempt {channel, outcome, err, duration_ms}, chain id,
// dedupe: ONE chain execution id is written to effects/audit for ALL attempts
// of that run; each attempt ALSO writes its own attempt row keyed by that id.
// Owner: 01-contact-foundation (leaf 01/02)
// Consumers: employee tool wiring (leaf 02), channel leaves (11, 12)
```

### C3: Signal json-rpc client

```
package signal // internal/signal/rpc.go (client) + internal/signal/supervisor.go

type Client struct{ /* unix json-rpc conn */ }
func Dial(socketPath string) (*Client, error)
// Request-id mutex + notification demux goroutine. JSON-RPC 2.0.
func (c *Client) StartCall(recipient string, audioMode CallAudioMode) (callID string, err error)
func (c *Client) AcceptCall(callID string) error
func (c *Client) HangupCall(callID string) error
func (c *Client) ListCalls() ([]CallInfo, error)
func (c *Client) SubscribeCallEvents(subscriptionID string) error
func (c *Client) Send(recipient string, message string, attachments []string) (ts int64, err error)
func (c *Client) Typing(recipient string, stop bool) error
// Notifications: method "callEvent" -> CallEvent{Envelope sender UUID, callID, State};
// method "envelope" -> inbound DataMessage{sourceUuid, body, attachments, timestamp}.
// States (per PR #1932): RINGING_INCOMING | RINGING_OUTGOING | CONNECTING |
// CONNECTED | RECONNECTING | ENDED (reason carries busy/timeout/declined).
// Supervisor: wraps the child-process pattern (internal/voice/childproc.go
// spawn/token discipline is the reference; signal-cli gets pid file + restart
// + idle supervision through llm.RuntimeProcess wrapping like voice child).
// Owner: 10-signal-sidecar (leaf 10)
// Consumers: 11 (voice + msg legs on top of it)
```

### C4: Signal voice leg registry entry

```
// internal/voice/dial.go: Leg gains "signal" (LegSignal). Zero value STAYS
// phone; the Leg matrix in docs/workflows/voice-sidecar.md extends per leg.
// Signal leg: AcquireLeg(ccid, employeeID, "signal:<uuid>", "signal");
// NormalizeE164 NEVER runs on signal targets (mirror the local:<label> rule,
// dial.go:624-633). ccid = "signal-" + pkg/id.Generate().
// Audio: daemon-side adapter bridges signal-cli socket audio <-> the C2 pair;
// child chunks stay L16 s16le@16k 640B/20ms (existing rule - resample IN the
// adapter). Barge-in: STT-partial (phone posture; NEVER RMS).
// Policy: effects reservation + audit row BEFORE StartCall (phone-outbound
// posture); allowlist by Signal UUID.
// Owner: 11-signal-voice-leg
// Consumers: registry tests, voice_wire integration
```

### C5: Telnyx messaging webhook events

```
package telnyx // internal/telnyx/messaging.go (client) + webhook handler in internal/comm/http

type Message struct {
    ID        string
    From      string // E.164
    To        string // E.164
    Text      string
    Direction string // "inbound" | "outbound"
    Status    string // for DLR: "queued"|"sent"|"delivered"|"failed"
    ReceivedAt time.Time
}
func (c *Client) SendSMS(ctx context.Context, from, to, text string) (id string, err error)
// Webhook: POST payload event types "message.received" (inbound text) and
// "message.finalized" (terminal DLR). Signature = the EXISTING Ed25519
// telnyx-signature verifier (reuse; do not write a second one).
// Owner: 12-telnyx-sms
// Consumers: 01/02 (DLR feeds ChannelAttempt for telnyx_sms)
```

### C6: Employee-facing contact tool

```
// internal/tools/builtin/contact.go - tool name "contact"
// Args: {person: string (contact id / phone / signal uuid), message: string,
//        channels: []string (optional override; empty = record policy or
//        employee default)}
// Behavior: resolve record -> chain -> attempts -> ChainReport rendered as
// per-channel outcome lines. Voice connect does NOT block the tool result:
// "answered" is terminal for the chain; the live call continues on its own
// session (goal parked by the existing call binding machinery).
// Owner: 01-contact-foundation (leaf 02)
// Consumers: dispatcher/tool registry wiring (leaf 02)
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-gate-tests.md | leaf (test gate) | none | 40K | - |
| 10 | 10-signal-sidecar.md | leaf | none | 60K | A |
| 11 | 11-signal-legs.md | branch | 10, 01 | 45K | B |
| 12 | 12-telnyx-sms.md | leaf | 01 | 45K | B |
| 01 | 01-contact-foundation.md | leaf | none (00T gate) | 55K | A |
| 02 | 02-contact-wiring.md | leaf | 01, 12, 11 | 40K | C |

Dispatch order: 00T gate first (Phase 0). Then concurrency group A (10, 01) together. Then group B (11 branch children, 12) once 01+10 are REVIEWED. Then C (02) last - it wires everything.

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Test Gate

Dispatch 00T-gate-tests.md first: tests only, fail RED for
missing-implementation reasons. Validate in-session (never delegate
review) against the Test Gate matrix; apply the delta protocol; commit
gate tests; set children to TESTS_VALIDATED. Then implementation may
dispatch. See references/tests-first-gate.md.

### Phase 1: Dispatch Concurrency Group A

Dispatch same-letter children simultaneously via `delegate_task`: goal =
"Implement all tasks from [leaf]"; context = full leaf text + contracts +
conventions + relevant source INLINED; include the no-commit, no-git-add,
no-read_file, write-once-and-stop rules.

### Phase 2: Review and Commit Each Child

Review in-session (the main model - never a delegated reviewer). Check
leaf spec, contracts, Review Checklist; run tests; gate-test diff must be
empty. Pass: commit + status REVIEWED. Gaps: re-dispatch with findings,
max 3 cycles, then escalate.

### Phase 3: Integration Review

After ALL children REVIEWED: verify contracts between children, run the
full suite, check no import cycles. Gaps: re-dispatch the failing child
with contract fixes (uncommitted). Pass: normalize formatting, run the
line-number corruption check, commit integration changes, set children
COMPLETE, close the associated GitHub issues with evidence.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (signatures, types, paths)
- [ ] All specified files created/modified at exact paths
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification
      (any such diff = automatic NEEDS_FIXES)
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions
- [ ] No scope creep; no obvious bugs or security issues
- [ ] No debug artifacts (print debugging, TODOs, placeholders,
      commented-out code)
- [ ] No line-number corruption (`     N|` prefixes) in source files

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language:** Go (module github.com/caimlas/meept); stdlib-first; unix sockets not TCP for local IPC.
- **Secrets:** provider credentials ONLY via `Broker.Value`; key material never in logs/env/argv; new `Broker.Value` callers MUST be added to `scripts/audit-secrets-value-callers.py` in the same commit.
- **Errors:** named sentinel errors (`ErrXxx`) at package level; wrap with `%w`; no ignored errors (pre-commit enforced - capture and `Warn`).
- **IDs:** `pkg/id.Generate()` NEVER `time.Now().UnixNano()`/`math/rand` (predid analyzer enforced).
- **Mutexes:** never hold across I/O (mutexio analyzer enforced).
- **JSON:** explicit json tags on every wire struct.
- **Tests:** table-driven `_test.go` beside source; e2e suites under `e2e/suites/<name>/` with manifest entry (no new unit-test-file features without e2e coverage per AGENTS.md).
- **No telnyx imports in signal-leg code** and no signal imports in telnyx code (leg isolation mirrors the local-leg carrier pin, internal/voice/local.go:14-15).
- **Formatting:** gofmt before reporting.

## Requirement Traceability

| Req | Source (user words, verbatim when possible) | Plan element (leaf/task/contract) | Verified by |
|-----|---------------------------------------------|-----------------------------------|-------------|
| R1 | "a single employee has the option to contact someone via multiple methods if necessary - eg. if they don't answer their phone, try signal, or vice versa" | C2 ChainRunner + C6 contact tool (01-contact-foundation) | chain e2e test |
| R2 | "Include signal messaging" | C3 Client.Send + leaf 11 (issue #120) | msg e2e test |
| R3 | "Signal voice calls" | C4 leg + leaf 11 (issue #119) | voice-leg e2e test |
| R4 | "SMS contact through telnyx" | C5 + leaf 12 (issue #121) | sms e2e test |
| R5 | "per-person" fallback policy (per-contact channel order) | C1 Record.Policy (leaf 01) | store/chain unit tests |
| R6 | honest outcome detection | prereq ended-state fix (issue #123) in leaf 01 T0 | e2e assertion upgrade |
| R7 | Zoom integration options investigated, held | issues #115/#116/#117 created + spike reports posted (done 2026-11-08) | GH issue comments |
| R8 | Slack voice considered | issue #122 (async clips; real-time rejected - no API) | GH issue #122 |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-gate-tests | TESTS_VALIDATED | 1 | RED verified (unit) + skip-pass e2e; 2 matrix deltas applied (F4 test path, F7 scope); commit 3083e8f7 |
| 01-contact-foundation | REVIEWED | 1 | gate GREEN; T0 both unit + e2e verified; 1 gate assertion delta (ada/Ada) fixed in-session; commit ff2c8eb9 |
| 10-signal-sidecar | REVIEWED | 1 | gate GREEN x3 + race; sun_path TMPDIR env fix; Dial 1-arg per gate; commit ff2c8eb9 |
| 11-signal-legs (branch) | PENDING | 0 | awaits 01+10 |
| 12-telnyx-sms | PENDING | 0 | awaits 01 |
| 02-contact-wiring | PENDING | 0 | awaits 01+11+12 |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: implementation children stay PENDING until the branch's
test-gate leaves reach TESTS_VALIDATED.

## Integration Test Plan

- `go build ./...` clean.
- `go test -p 2 ./internal/contact/... ./internal/signal/... ./internal/voice/...` green.
- `make e2e-fast-area AREA=contact-chain`: full fallback chain hermetic test - fake telnyx voice (no-answer) -> fake signal voice (busy) -> fake signal msg (delivered) stops the chain; per-attempt effects/audit rows share one chain id.
- `make e2e-fast-area AREA=signal-sidecar`: signal-cli stub (scripted json-rpc + callEvent + PCM) through the real adapters to a fake child pair.
- `make e2e-fast-area AREA=telnyx-sms`: webhook receiver signature-verified inbound + DLR correlation.
- `make graphs` fresh (new bus topics/webhooks if any) and `make graphs-check` green.
- Line-number corruption grep: `grep -rn '^\s\+[0-9]\+|' internal/contact internal/signal` returns nothing.

## Test Gate

Test leaves: 00T-gate-tests.md - dispatched in
Phase 0, validated by the control agent (orchestrator, in-session).

| # | Feature (from plan) | Source (leaf/spec section) | Unit tests | E2E tests | Status |
|---|---------------------|----------------------------|-----------|-----------|--------|
| F1 | contact store CRUD + E.164 normalize | C1 | contact/store_test.go | contact-chain scenario | PENDING |
| F2 | chain runner ordered fallback + dedupe | C2 | contact/chain_test.go | contact-chain scenario | PENDING |
| F3 | signal json-rpc client + demux | C3 | signal/rpc_test.go | signal-sidecar scenario | PENDING |
| F4 | signal voice leg registry + splice adapter | C4 | voice/registry_leg_test.go additions | signal-sidecar scenario | PENDING |
| F5 | signal msg send/receive | #120 | signal/msg_test.go | signal-sidecar scenario | PENDING |
| F6 | telnyx SMS send + webhook + DLR | C5 | telnyx/messaging_test.go | telnyx-sms scenario | PENDING |
| F7 | phone-leg ended-state fix (#123) | leaf 01 T0 | daemon voice wiring test | voice-local suite assertion upgrade | PENDING |
| F8 | contact tool surface | C6 | builtin/contact_test.go | contact-chain scenario | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: [date, feature ID, direction, one-line reason]

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda (dated, appended to the dispatch brief). Never edit a
dispatched leaf document mid-execution.

## Notes

- The 2026-11-08 research reports are archived under `.research/` in this tree directory; leaf agents needing external API detail read those files, not the web.
- signal-cli is an EXTERNAL SUBPROCESS (AGPL as subprocess is allowed per repo rules); never vendor or link it.
- US SMS compliance (10DLC vs toll-free) is an OPERATIONS decision outside this tree; leaf 12 implements the client and documents the setup, toll-free recommended.
- Zoom (#115/#116/#117) and Slack (#122) are deliberately out of scope here; #115's RTMS observe leg is the recommended NEXT tree after this one (see the spike verdict comment on #115).
- The contact registry intentionally does NOT model presence/availability (no "is the person at their desk"); fallback is attempt-driven, detected at call/message time.
