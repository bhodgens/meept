# Push notifications with per-key dedup - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** User-facing notifications for quota episodes: per-provider-key
  dedup, escalation ladder delivery, and the WS event classification so
  browser/mobile clients render quota events as agent_progress.
- **Dependencies:** 05 (tracker emits agent.quota_wait events)
- **Estimated Context:** 60K
- **Concurrency Group:** E

## Goal

The user hears about a quota wall once per provider-key per episode, gets the
12h/20h escalations, and never gets spammed per retry. WS clients must not
render these events as blank chat bubbles.

## Context

Notifications flow through `internal/services` push service (VERIFY actual
subscription mechanism — it may subscribe to bus topics directly or via a
notification service in internal/services). The WS surface is
`internal/comm/http/server.go` `transformBusEventToWS`, which maps bus topics
to frontend event types: ONLY chat_message / chat.response topics become
`type: "chat_message"`; everything else becomes `agent_progress`. A new topic
that is not classified would fall into the default bucket — confirm the
default is agent_progress (then classification is a no-op but MUST be tested
and pinned) or explicitly map it.

Key files to understand before implementing:

- `internal/services/` push/notification service (find bus subscription).
- `internal/comm/http/server.go` — transformBusEventToWS + its tests.
- `internal/agent/quota_episode.go` (leaf 05) — event payload contract.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// File: internal/services/<notification file chosen after exploration>

// QuotaNotifier subscribes to bus topic "agent.quota_wait" and delivers
// push notifications:
//   - reason="quota_blocked" + escalation==""  -> notify (first hit)
//   - escalation=="12h"|"20h"                  -> notify (escalation text)
//   - escalation=="24h"                        -> notify (action required)
//   - reason="quota_cleared"                   -> notify (recovery)
// Dedup: per credential_key per episode. An episode starts at
// quota_blocked(esc="") and ends at quota_cleared; within an episode the
// SAME escalation tier never notifies twice (defensive — tracker already
// fires tiers once; dedup is the safety net for bus redelivery).
// Notification text (lowercase, per UI convention):
//   blocked:  "quota limit reached on <provider>/<model> — resets in ~<dur>.
//              <n> task(s) affected. will resume automatically."
//   12h:      "still quota-blocked on <provider>/<model> (12h)."
//   20h:      "quota still blocked on <provider>/<model> (20h) — action
//              recommended."
//   24h:      "quota blocked on <provider>/<model> for 24h — manual
//              action required. agent blocked."
//   cleared:  "quota recovered on <provider>/<model> — resumed."
```

```
// File: internal/comm/http/server.go
// transformBusEventToWS: topic "agent.quota_wait" -> type "agent_progress".
// (Add an explicit case or verify+pin the default classification with a
// test — blank chat bubbles are the failure mode we are preventing.)
```

### What This Leaf Consumes

```
// leaf 05 payload keys (see 05 contract): agent_id, task_id, from, to,
// reason, provider_id, credential_key, model_id, unblock_at, escalation,
// fallback_model
```

## Tasks

### Task 1: QuotaNotifier with dedup

**Objective:** Subscribe, deliver, dedup per key+episode.

**Files:**
- Create: `internal/services/quota_notifier.go` (or modify the existing
  notification service file if one owns bus subscriptions)
- Test: `internal/services/quota_notifier_test.go`

**Step 1: Write failing test**

Fake bus + fake push sink. Feed payloads: (a) quota_blocked(esc="") ->
one notification; (b) repeat quota_blocked(esc="") same key -> suppressed;
(c) different credential_key -> delivered; (d) esc 12h/20h/24h -> delivered
once each; repeated tiers suppressed; (e) quota_cleared -> delivered, dedup
state resets (next quota_blocked notifies again); (f) malformed payload
(missing keys) -> skipped without panic; (g) affected-task count rendered
from task_id presence (single vs "n tasks" — count distinct task_ids seen
for the key within the episode; if the tracker cannot provide counts, the
notification omits the count line rather than guessing — document choice).

**Step 2: Run test to verify failure**

Run: `go test ./internal/services/ -run TestQuotaNotifier -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Map[credentialKey]episodeState guarded by mutex; deliver outside lock.

**Step 4: Run test to verify pass**

Run: `go test ./internal/services/ -run TestQuotaNotifier -v`
Expected: PASS

### Task 2: Notifier wiring

**Objective:** Daemon constructs QuotaNotifier alongside the push service.

**Files:**
- Modify: the daemon wiring site for services/push (search where the push
  service subscribes to bus topics)
- Test: same test file (construction + subscription assertion)

**Step 1: Write failing test**

Constructing the service stack subscribes QuotaNotifier to agent.quota_wait
exactly once (assert via fake bus registration count).

**Step 2: Run test to verify failure**

Run: `go test ./internal/services/ -run TestQuotaNotifierWiring -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Mirror the existing push-service wiring pattern.

**Step 4: Run test to verify pass**

Run: `go test ./internal/services/ -run TestQuotaNotifier -v`
Expected: PASS

### Task 3: WS classification

**Objective:** agent.quota_wait renders as agent_progress on WS clients.

**Files:**
- Modify: `internal/comm/http/server.go` (transformBusEventToWS)
- Test: `internal/comm/http/quota_ws_test.go` (or extend the existing
  transform test file — find it)

**Step 1: Write failing test**

Feed a bus event with topic agent.quota_wait through transformBusEventToWS:
assert type == "agent_progress" and payload fields survive (agent_id,
unblock_at visible to the client). If the function has a topic-allowlist,
add the topic there; pin with a table entry.

**Step 2: Run test to verify failure**

Run: `go test ./internal/comm/http/ -run TestQuotaWSTransform -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Classification only. Do not touch the session_id/conversation_id fallback
logic (AGENTS.md invariant).

**Step 4: Run test to verify pass**

Run: `go test ./internal/comm/http/ -run 'TestQuotaWSTransform' -v` then the
existing transform tests:
`go test ./internal/comm/http/ -run TestTransform -count=1 -v`

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep — only what the tasks specify
- [ ] No notification contains key material (fingerprint only)
- [ ] WS change cannot affect chat_message classification

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly (dedup scope = credential_key)
- [ ] Malformed payloads never panic
- [ ] Escalation text matches contract (lowercase)
- [ ] transformBusEventToWS: chat_message paths untouched

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The tracker (leaf 05) already fires each tier once; the notifier dedup is
  defense against bus redelivery and notifier restarts mid-episode. In-memory
  dedup is sufficient (notification state need not survive restarts — a
  restarted daemon re-notifies once, acceptable).
- The "affected tasks" count: only claim what the payload carries. If
  task_id is empty (agent-level episode, no task), omit the count line.
