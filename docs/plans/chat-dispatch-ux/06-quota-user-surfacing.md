# Quota User Surfacing - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** A terminal quota error inside a specialist step job publishes an agent.quota_wait bus event so the user learns the real cause instead of "my tools aren't working."
- **Dependencies:** 03 (shares internal/daemon/components.go — dispatch only after 03 is committed; run AFTER 04 in the same wave)
- **Estimated Context:** 30K
- **Audit references:** finding F6 (2026-09-04: model told the user "my tools aren't working properly" after an agnes 429; real cause never surfaced)

## Goal

The primary loop parks quota waits via QuotaResumeWatcher
(handler.go:1557) and the goal loop has its own parker
(components.go:4043), but the step-job path
(AgentJobProcessor.Process → RunOnce) just returns "agent execution
failed: ..." — the tactical scheduler marks the step failed, review skips,
and the user never hears the word quota. This leaf publishes the existing
agent.quota_wait bus event (WS-classified agent_progress per AGENTS.md
invariant) when a step job fails on *llm.QuotaResetError, and stamps the
step's stored Result so the chat reply (leaf 01's C1 contract) carries the
quota message verbatim.

## Context

Error classification: llm.QuotaResetError implements NonRetryable;
AgentLoop's own quota branch (loop.go:4686-4714) already handles
rotation/parking for loop-level turns — this leaf is the JOB-level backstop
for when every candidate is blocked and the error escapes RunOnce. The
tactical failure path is tactical.go:1082-1088 (SetState StepFailed) and
the review-skip detection is stepHasError (review_manager.go:810) which
matches "error:" prefixes — the stored Result must stay parseable by it.

Key files to understand before implementing:
- internal/daemon/components.go - AgentJobProcessor.Process error path (7268-7286); the bus field available on the processor (check struct fields ~7060-7090)
- internal/agent/loop.go:4686-4714 - the QuotaResetError shape (ProviderID, ModelID, ResetAt, RetryAfter fields)
- internal/comm/http/server.go - topic prefix classification (agent.quota*) — read-only, for payload-shape reference
- internal/agent/tactical.go:1082-1088 - step failure marking

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/daemon/components.go — AgentJobProcessor.Process error path:
//   var quotaErr *llm.QuotaResetError
//   if errors.As(err, &quotaErr) { publish agent.quota_wait with payload:
//     "class": "quota_wait",
//     "conversation_id" / "session_id": step's linked session (from
//        taskStore linked sessions or stepPayload; empty-string keys omitted),
//     "agent_id": job.AgentID, "task_id": job.TaskID,
//     "unblock_at": RFC3339(quotaErr.ResetAt or now+RetryAfter),
//     "message": lowercase user-facing one-liner }
//   AND wrap the returned error so the step Result records:
//     "quota wait: <provider>/<model> is rate-limited until <time>. your
//      request is saved and will need a re-ask once the limit resets."
//   (keep "error:" shape compatibility for stepHasError — prefix the wrap
//   with the existing "agent execution failed:" text, append the quota
//   sentence).
// No new bus topic. No new config.
// Owner: 06. Consumers: 10 (e2e asserts quota message on 429-shaped failure).
```

### What This Leaf Consumes

```
// llm.QuotaResetError (errors.As target), bus.MessageBus.Publish
// taskStore.GetByID(...).LinkedSessions for addressing
```

## Tasks

### Task 1: quota classification + event publish

**Objective:** Process error path classifies quota errors and publishes the event.

**Files:**
- Modify: `internal/daemon/components.go` (Process error branch 7269-7275)
- Test: `internal/daemon/agent_job_processor_test.go` (created by leaf 03;
  coordinate through orchestrator if 03 has not landed its helper names)

**Step 1: Write failing test**

```go
func TestAgentJobProcessor_QuotaErrorPublishesEvent(t *testing.T) {
	p := newTestAgentJobProcessor(t) // with bus + taskStore; agent loop stub
	// whose RunOnce returns a *llm.QuotaResetError{ProviderID:"agnes",
	// ModelID:"agnes-2.5-flash", RetryAfter: 30*time.Minute}
	// (check llm package for the constructor/fields; build the real type)
	sub := busTestSubscriber(t, p.bus, "agent.quota_wait") // test helper
	_, err := p.Process(ctx, jobWithStepPayload(t))
	if err == nil {
		t.Fatal("expected error")
	}
	events := sub.Drain()
	if len(events) != 1 {
		t.Fatalf("got %d quota_wait events, want 1", len(events))
	}
	payload := events[0].Payload
	if payload["class"] != "quota_wait" {
		t.Errorf("class = %v", payload["class"])
	}
	if payload["agent_id"] != "coder" {
		t.Errorf("agent_id = %v", payload["agent_id"])
	}
	if _, ok := payload["unblock_at"].(string); !ok {
		t.Errorf("unblock_at missing or not a string: %v", payload["unblock_at"])
	}
}
```

(Write the test in English — the error message above contains a stray
non-ASCII check to replace with `missing or not a string`.)

**Step 2: verify failure** — `go test -p 2 ./internal/daemon/ -run TestAgentJobProcessor_QuotaErrorPublishesEvent -v` → FAIL (no events).

**Step 3: implement** — in the error branch:

```go
response, err := agentLoop.RunOnce(ctx, prompt, conversationID)
if err != nil {
	var quotaErr *llm.QuotaResetError
	if errors.As(err, &quotaErr) {
		p.publishQuotaWait(job, stepPayload.TaskID, quotaErr) // new small method
		err = fmt.Errorf("agent execution failed: %w (quota wait: %s/%s is rate-limited; re-ask after the limit resets)",
			err, quotaErr.ProviderID, quotaErr.ModelID)
	}
	p.logger.Error("Agent execution failed", ...)
	return nil, err
}
```

publishQuotaWait: compute unblockAt (ResetAt, else now+RetryAfter), build
the payload per the contract (two-value map assertions, omit empty keys),
Publish on "agent.quota_wait", log Info. Nil-bus guard (test builds without
bus in other tests — keep them green).

**Step 4: verify pass** — same run → PASS; package suite green.

### Task 2: user-visible Result stamp

**Objective:** The failed step's stored Result carries the quota sentence so leaf-01 replies quote it.

**Files:**
- Modify: same error path (the returned error text IS stored as step Result
  by the tactical failure path — verify this data flow; if the Result is
  set elsewhere, stamp there instead and document)
- Test: extend the Task 1 test — after Process fails, drive the tactical
  failure marking (or assert on the returned error string if the store
  write happens in tactical) and assert the step Result contains
  "quota wait:" and the provider/model.

**Steps:** failing test → implement (likely zero code if the wrap above
flows through) → pass. Record the actual flow in Deviations.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or documented below)
- [ ] No scope creep
- [ ] Topic is exactly "agent.quota_wait" — no new prefix
- [ ] Non-quota errors byte-identical behavior
- [ ] `go build ./...` clean; daemon package suite green

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] errors.As classification, nil-bus safe
- [ ] Payload keys match contract; RFC3339 timestamp
- [ ] Error wrap keeps stepHasError compatibility ("error:" indicators —
  verify "quota wait:" doesn't break the check; extend indicators only if needed)
- [ ] No new topics/config
- [ ] No scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- AGENTS.md quota invariants apply: NEVER feed QuotaResetError into
  Resolver.RecordAliasFailure; this leaf only reports — no retry, no block
  mutation (the loop already did that at 4696-4704 before escaping).
- The message wording is user-facing: lowercase, short, no jargon.
- components.go is hot after 03 and 04: keep hunks tight and sequential.
