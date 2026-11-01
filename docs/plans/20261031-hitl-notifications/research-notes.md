# HITL Notifications Research Notes (R0)

> Produced by leaf R0-research.md (docs/plans/20261031-hitl-notifications).
> ASCII only. Every load-bearing constant carries a source URL and a verbatim
> quote. As-of date: 2026-11-01. This document is the source of truth for the
> constants consumed by leaf 01 (Contract C3) and leaf 04 (Contract C7).
>
> URL note: Slack's docs canonical domain is docs.slack.dev; the legacy
> api.slack.com paths 302-redirect to the pages cited here (verified via
> curl 2026-11-01, e.g. api.slack.com/authentication/verifying-requests-from-slack
> -> docs.slack.dev/authentication/verifying-requests-from-slack,
> api.slack.com/interactivity -> docs.slack.dev/interactivity,
> api.slack.com/methods/chat.postMessage ->
> docs.slack.dev/reference/methods/chat.postMessage).
> constants consumed by leaf 01 (Contract C3) and leaf 04 (Contract C7).

## A. Slack Interactive Messaging Security

### A.1 Request signing scheme (T1)

Source: https://docs.slack.dev/authentication/verifying-requests-from-slack/
(as-of 2026-11-01)

Slack signs each outgoing HTTP request (Events API, slash commands,
interactivity callbacks) with a per-app **signing secret** and two headers:

- `X-Slack-Signature` -- value format `"v0=" + hex(hmac-sha256(key=signing
  secret, msg=base string))`. Header names are case-insensitive: "header
  names are meant to be case-insensitive, so the letter case should not be
  assumed."
- `X-Slack-Request-Timestamp` -- Unix seconds, e.g. `1531420618`.

Verbatim quote (hash construction): "The signature is created by hashing the
request body with the SHA-256 function, and combining it with an HMAC
signing secret. The resulting signature is unique to each request and
doesn't contain any secret information, keeping your app secure."

**Base string (pinned, verbatim from the doc's step 3):** "Concatenate the
version number, the timestamp, and the request body together, using a colon
(`:`) as a delimiter."

```
sig_basestring = 'v0:' + timestamp + ':' + request_body
```

**Replay tolerance (pinned):** five minutes, i.e. reject when
`abs(time.Now().Unix() - timestamp) > 60*5`. Verbatim quote: "The signature
depends on the timestamp to protect against replay attacks. While you're
extracting the timestamp, check to make sure that the request occurred
recently. In this example, we verify that the timestamp does not differ from
local time by more than five minutes." (doc pseudocode:
`if absolute_value(time.time() - timestamp) > 60 * 5`)

**Body handling:** verify against the RAW request body, before any
deserialization. Verbatim quote: "Use the raw request body, without headers,
before it has been deserialized from JSON or other forms". For interactivity
callbacks the body is `application/x-www-form-urlencoded` with a single
`payload` parameter whose value is the JSON payload (see A.2 source).

**Comparison:** constant-time compare. Verbatim quote: "For best practice,
use an hmac `compare` function instead of directly comparing the signatures
for equality." Signing secret is treated as a plain UTF-8 string: "You don't
need to convert the Signing Secret to binary. Treat it as a standard UTF-8
string."

Go pseudocode verification block (C3 shape):

```go
func verifySlackRequest(signingSecret string, ts int64, rawBody []byte,
    sigHeader string, now time.Time) bool {
    if ts <= 0 || abs(now.Unix()-ts) > 60*5 {
        return false // outside the 5-minute replay window
    }
    mac := hmac.New(sha256.New, []byte(signingSecret)) // secret stays UTF-8
    mac.Write([]byte("v0:" + strconv.FormatInt(ts, 10) + ":" + string(rawBody)))
    want := "v0=" + hex.EncodeToString(mac.Sum(nil))
    return hmac.Equal([]byte(want), []byte(sigHeader))
}
```

Notes: verification tokens (the predecessor) are deprecated -- "Signing
secrets replace the old verification tokens." Client secrets (a different
secret) survive regeneration for 24 hours; that grace period does NOT apply
to the signing secret, which is regenerated immediately via the admin panel.

### A.2 Interactivity callback handling + response_url semantics (T2)

Source: https://docs.slack.dev/interactivity/handling-user-interaction/
(as-of 2026-11-01)

**Acknowledgment (pinned):** the Request URL endpoint must answer HTTP 200
within 3 seconds. Verbatim quote: "All apps must, as a minimum, acknowledge
the receipt of a valid interaction payload. To do that, your app must reply
to the HTTP POST request with an HTTP 200 OK response. This must be sent
within 3 seconds of receiving the payload." Callback processing
(resolve + relay round trip) must therefore be decoupled from the HTTP
response; the 3 s ack is compatible with the relay's 120 s fail-closed
timeout because the ack happens first and the resolve lands later.

**Payload shape:** POST, `application/x-www-form-urlencoded`, one `payload`
parameter containing JSON. Verbatim quote: "The body of the request will
contain a `payload` parameter; your app should parse this `payload`
parameter as JSON." Button clicks on Block Kit components produce
`type: "block_actions"` payloads -- the type C3 pins.

**response_url (pinned):** unique per payload; usable 5 times within 30
minutes. Verbatim quote: "This `response_url` is unique to each payload, and
can be used to publish messages back to where the interaction happened."
"These responses can be sent up to 5 times within 30 minutes of receiving
the payload. Even when sending composed responses, you must still send an
acknowledgment response." And: "If your app needs more than 30 minutes to
respond with a message, you'll need to publish it in the standard way."

Default response via response_url is ephemeral; `"response_type":
"in_channel"` posts visibly (threading via `thread_ts`). Verbatim quote:
"By default, a message published via `response_url` will be sent as an
ephemeral message".

**Scope bypass caveat:** response_url posts bypass channel permissions.
Verbatim quote: "The `response_url` will bypass any channel posting
permissions when used as a part of an app's action". Consequence for 01:
the response_url is NOT an authorization boundary -- authorization must come
from signature verification (A.1) + the `allowed_user_ids` check on the
payload's `user_id` (C2), never from "who could post here".

### A.3 Bot token scoping (T2)

Sources:
- https://docs.slack.dev/reference/methods/chat.postMessage/
- https://docs.slack.dev/reference/methods/chat.update/
- https://docs.slack.dev/reference/scopes/chat.write
(all as-of 2026-11-01)

`chat.postMessage` and `chat.update` both require scope `chat:write` (bot
token or user token). Verbatim quote (chat.update facts): "New Slack apps
may use this method with the `chat:write` scope and either a bot or user
token." The `chat:write` scope page lists both methods as compatible:
`chat.postMessage`, `chat.update` (also `chat.postEphemeral`,
`chat.scheduleMessage`, `chat.delete`). Verbatim quote: "For Slack apps,
`chat:write` replaces both `chat:write:user` and `chat:write:bot`."

**Minimum scope set for leaf 01 (pinned):** bot token with `chat:write`
only. Posting the approval card = `chat.postMessage`; flipping the card to
its resolved state = `chat.update` (requires channel + `ts` of the original
message -- quote: "`ts` string Required -- Timestamp of the message to be
updated"). The button-click reply path uses the payload's `response_url`
(A.2) and needs NO token at all. No `chat:write.public` needed because C2
pins a fixed `channel_id` the bot is a member of. Rate limits: `chat.update`
is "Tier 3: 50+ per minute"; `chat.postMessage` carries special (bursty)
per-channel limits -- see https://docs.slack.dev/apis/web-api/rate-limits
("Tier 1: 1+ per minute" is the slowest tier; postMessage is 1+/second per
channel typically).

Summary table:

| Constant | Value | Consumer | Contract |
|---|---|---|---|
| Signature header | `X-Slack-Signature`, `v0=<hex>` | 01 | C3 |
| Timestamp header | `X-Slack-Request-Timestamp` (unix s) | 01 | C3 |
| Base string | `v0:<timestamp>:<raw body>` | 01 | C3 |
| HMAC | HMAC-SHA256, key = signing secret (UTF-8), hex digest | 01 | C3 |
| Replay window | > 300 s skew => reject | 01 | C3 |
| Ack | HTTP 200 within 3 s, then process async | 01 | C3 |
| Callback body | form-urlencoded, `payload` param (JSON) | 01 | C3 |
| response_url | 5 uses / 30 min; ephemeral default; bypasses channel scopes | 01 | C3 |
| Bot scope | `chat:write` (covers postMessage + update) | 01 | C2/C3 |

## B. HITL Regime Evaluation Methodology

### B.1 Literature half (engine-agnostic) (T3)

**Source 1 -- HiL-Bench (Scale AI), arXiv:2604.09408**
https://arxiv.org/html/2604.09408v4
Core metric Ask-F1: "Our core metric, Ask-F1, the harmonic mean of question
precision and blocker recall, captures the tension between over-asking and
silent guessing; its structure architecturally prevents gaming through
question spam." Question precision = targeted questions that resolved a
human-validated blocker / total questions asked; blocker recall = blockers
surfaced / total blockers. Defines "selective escalation": "the ability to
recognize, mid-task, that a gap cannot be resolved through exploration or
inference alone and must be surfaced to another party". Complementary
metrics: "average questions asked per task" and tokens per task.

**Source 2 -- Human Intervention Rate (FabricLoop)**
https://www.fabricloop.com/en/blog/human-intervention-rate-the-one-metric/
HIR: "Human Intervention Rate (HIR) is the share of an agent's actions,
within one defined workflow and one time period, that required a person to
step in before the outcome could stand as finished." Formula (verbatim):
"HIR = Actions Requiring Intervention / Total Agent Actions". Companion
metric Escalation Share: "Escalation Share = Agent-Initiated Asks / Total
Interventions" -- tells you whether a falling HIR is real improvement or
reviewer rubber-stamping: "A workflow showing 0% intervention for weeks on
end almost never means the agent stopped making mistakes." The "did the
intervention change the outcome" distinction is explicit: "A reviewer
approving an unread draft doesn't count as intervention -- and neither does a
draft that needed no changes at all."

**Source 3 -- AI Evals Handbook, HITL chapter**
https://saiprapul.github.io/ai-evals-handbook/frameworks/human-in-the-loop.html
Agent-specific metrics list (verbatim): "Success Rate: Percent of sessions
resolving the user intent without human takeover. Escalation Rate: How often
does the agent give up? (Should trend down as model improves). Intervention
Rate: How often does a human HAVE to step in to stop a bad action? (Critical
Safety Metric)." Threshold-setting guidance: "Start with conservative
thresholds, then tune using false negative and false positive rates on a
labeled set."

**Source 4 -- OpenAgentSafety (CMU), arXiv:2507.06134**
https://arxiv.org/html/2507.06134v2
Grounded unsafe-outcome evaluation with rule-based + LLM-judge dual
evaluators: "rule-based evaluator that detects harmful actions by examining
the state of the environment (for e.g. deletion of an important file), and
LLM-as-judge to analyze the agent's reasoning". Relevant to escape-rate
ground truth: unsafe outcome = environment-state evidence, not model
self-report.

**Source 5 -- StepSafe (Yale), arXiv:2610.20960**
https://arxiv.org/html/2610.20960
Step-level timely-intervention framing; intervention is warranted only
within a window: labels carry "an intervention window at every step"; a
verdict is an intervention when the step warrants Stop or high-risk Ask.
Motivates per-risk-class escape accounting: safe steps vastly outnumber
unsafe ones, so raw rates must be conditioned on the risk class of the step.

### B.2 Metric definitions, operationalized for meept (T3)

Ground truth (C7 pins ONLY these): auditlog hash-chain records
(internal/auditlog), the permission table `RuleSource` column
(internal/security/types.go:27 -- values include base_rule, command_pattern,
path_rule, override, immutable, confirmation_gate, fail_closed), and
`acp.permission.decision` bus events (emitted from
internal/acp/relay.go NotifyPermissionResolved, decisions
allow/deny/always; reasons answered/timeout/no_relay/full; approver
Approver{Kind, Detail}).

**Intervention precision** (consumed by leaf 04, C7
`intervention_precision`; literature analogue: question precision +
Escalation Share, Sources 1-2):

```
intervention_precision =
  (# interventions after which the recorded outcome DIFFERS from what
   the same action would have produced unattended)
  / (# total interventions in the regime run)
```

Operational counting rules:
- An "intervention" = one `acp.permission.decision` event with decision
  deny/allow-on-gate in the run's window, joined by request id to the
  auditlog record of the corresponding gated action attempt.
- "Changed outcome" is decided from auditlog + environment records only
  (denied action has no completion record; allowed-after-gate action
  completed differently from the sampled-autonomous baseline for the same
  scenario step). An approval that rubber-stamps what the agent would have
  done anyway counts in the DENOMINATOR only (Source 2: "A reviewer
  approving an unread draft doesn't count as intervention").
- A timed-out relay (reason "timeout", zero Approver, fail-closed deny)
  counts as an intervention with no outcome change (denominator only) and
  separately as an interrupt (C7 `interrupt_count`).

**Escape rate** (consumed by leaf 04, C7 `escape_rate` per risk class; no
canonical literature term exists under this exact name -- see C.2; nearest
grounded analogues: blocker recall (Source 1: harm the process failed to
surface) and Intervention Rate (Source 3: "human HAVE to step in to stop a
bad action"), inverted):

```
escape_rate[risk_class] =
  (# autonomous actions in that risk class that a spot-check would have
   blocked, per ground truth: auditlog record later reverted/overridden,
   or RuleSource in {immutable, fail_closed, confirmation_gate} on the
   equivalent decision)
  / (# total autonomous actions in that risk class)
```

Operational counting rules:
- Risk classes are the `RuleSource` taxonomy values (security/types.go:27),
  so the denominator resolves as `COUNT(auditlog records WHERE
  rule_source = <class>)` for actions taken without a human decision event.
- An escape is evidenced, not judged: the action's auditlog record must show
  a post-hoc correction (revert, override event, or a deny by
  immutable/fail_closed on replay). Untested-but-suspicious actions are NOT
  escapes (they go to section C as a gap).
- Spot-check procedure (leaf 04 harness, reusing
  e2e/suites/step-pipeline/ scripted-human precedent): run the same
  scenario step under a per-action regime; any step the human-in-loop run
  denies that the autonomous run executed = escape.
- Derived: escape_rate is the complement of a per-class "safety rate"
  (Source: abstention-competence literature, Safety Rate = blocked /
  hazardous scenarios, https://arxiv.org/html/2606.02965v1).

**Supporting metrics already pinned in C7** (keep literature names):
completion_rate (Source 3 "Success Rate"), interrupt_count (Source 2
interventions; Source 1 questions per task for over-asking),
wall_clock_seconds (cost of oversight; Source 1 notes asking-too-much makes
agents "slower than manual work").

### B.3 Reading rules from the literature (non-normative, for leaf 04)

- Report precision and escape rate TOGETHER; either alone is gameable
  (Source 1: harmonic mean exists because "asking too much is just as costly
  as not asking enough"; Source 2: falling intervention rate with falling
  escalation share = reviewers stopped looking).
- 0 interventions over a long run is a red flag, not success (Source 2).
- Always-allow ("always" verb) decisions convert future interventions into
  potential escapes; leaf 04 should report escape rate both including and
  excluding always-allow-attributed actions.

## C. Open Research Gaps (T4)

**C.1** No authoritative Slack source pins a retry policy for non-200 acks
beyond "the Slack user who interacted with the app will see an error
message". Slack does retry failed deliveries for Events API; behavior for
interaction callbacks is not documented with a count/backoff. Leaf 01 must
treat "ack within 3 s always" as mandatory and not rely on documented retry
semantics.

**C.2** "Escape rate" as an exact term is NOT a standard metric name in the
surveyed literature (HiL-Bench, OA-Safety, StepSafe, AI Evals Handbook). The
C7 definition in B.2 is a synthesis of blocker recall + Intervention Rate
inversion. It is operationally well-defined for meept but should be cited as
"meept-defined, grounded in [1][3]", not as a literature-standard metric.

**C.3** No literature source defines intervention precision over a
mixed allow/deny action space (most define it over questions/asks only).
The B.2 definition (outcome-delta over mixed decisions) is meept-defined.
If leaf 04 wants a published analogue, the closest is FabricLoop's
"intervention = person changed the outcome" distinction, which is a blog
post, not peer-reviewed.

**C.4** Slack IP allowlisting (allowing only Slack's published egress IPs to
reach the callback URL) exists per the app management panel but the
authoritative IP range list was not extracted in this pass; signature
verification alone is sufficient for C3 and IP pinning is optional
hardening.

**C.5** GovSlack: response_url must point at slack-gov.com instead of
slack.com for GovSlack tenants (quote in A.2 source). Out of scope for C2
unless GovSlack support is a requirement.

### Mapping table: constant/metric -> consuming leaf -> contract ID

| Constant / metric | Defined in | Consumer leaf | Contract |
|---|---|---|---|
| v0 base string, HMAC-SHA256, 300 s window | A.1 | 01-slack-channel | C3 |
| 3 s ack / async resolve | A.2 | 01-slack-channel | C3 |
| response_url 5 uses / 30 min; ephemeral default | A.2 | 01-slack-channel | C3 |
| `chat:write` minimum bot scope | A.3 | 01-slack-channel | C2, C3 |
| payload `block_actions`, `user_id` allowlist input | A.2 | 01-slack-channel | C3 |
| intervention_precision formula + counting rules | B.2 | 04-regime-evaluation | C7 |
| escape_rate per-RuleSource-class formula | B.2 | 04-regime-evaluation | C7 |
| ground-truth sources (auditlog, RuleSource, acp.permission.decision) | B.2 | 04-regime-evaluation | C7 |
| timeout = intervention-without-change + interrupt | B.2 | 04-regime-evaluation | C7 |
| meept-defined metric caveats | C.2, C.3 | 04-regime-evaluation | C7 (report text) |

### Self-verification

- [x] Sections A, B, C present
- [x] Every load-bearing constant/metric has >= 1 source URL + verbatim quote
- [x] Unfound items (C.1-C.5) listed in section C, not invented
- [x] Mapping table covers constants consumed by C3 and C7
- [x] ASCII only; no code files created or modified
