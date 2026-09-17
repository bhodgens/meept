# Migrate turn.terminal and agent.quota_wait to Typed Topics - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify - write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Declare typed topics for `turn.terminal` and `agent.quota_wait`, promote any anonymous payload structs to named types, and migrate their publish + subscribe call sites to `bus.PublishT`/`bus.SubscribeT`.
- **Dependencies:** 01-topic-generic.md (COMPLETE - `internal/bus/topic.go` exists with Topic[T], PublishT, SubscribeT)
- **Estimated Context:** 60K
- **Concurrency Group:** B

## Goal

These two topics have documented misclassification scars (blank-bubble class bugs; the async-turn migration built turn.terminal; quota events must render as progress, never chat bubbles). They become the first typed topics. Exactly two topics migrate in this leaf - no others.

## Context

Publish sites to find and migrate (verify with grep - line numbers may have drifted):

- `internal/agent/handler.go` ~line 1584: `h.bus.Publish("turn.terminal", msg)`
- `internal/agent/loop.go` and/or `internal/agent/quota_resume.go`: publishes of `agent.quota_wait` (search the exact string)
- Subscribers: grep `Subscribe(` with `"turn.terminal"` and `"agent.quota_wait"` across `internal/` (TUI, GUI bridge, comm relay, metrics collector are candidates). EVERY subscriber of these two exact topics migrates in this leaf. Wildcard subscribers (`agent.*`) do NOT count and do NOT migrate.

Key files:

- `internal/bus/topic.go` - the wrappers from leaf 01 (consumed, not modified)
- `internal/agent/handler.go`, `internal/agent/loop.go`, `internal/agent/quota_resume.go` - publish sites
- Wherever the subscriber(s) live - find them; do not assume

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/bus/topics.go  (new file; see Task 1 for placement decision)
package bus  // OR pkg/models - see Task 1

// Exact payload types depend on what the current call sites marshal.
// The leaf MUST locate them (Step 1 of Task 1) and use those types.
var TopicTurnTerminal = NewTopic[<TurnTerminalPayloadType>]("turn.terminal")
var TopicAgentQuotaWait = NewTopic[<QuotaWaitPayloadType>]("agent.quota_wait")
```

- Placement decision: `internal/bus/topics.go` if the payload types live in packages bus can import without a cycle (they live in `internal/agent` or `pkg/models`). `internal/bus` importing `internal/agent` is ALMOST CERTAINLY a cycle (agent imports bus). Expected outcome: declarations go in `pkg/models/topics.go` (payload structs are already referenced by bus consumers via pkg/models) OR alongside the payload structs in their owning package. DECIDE BY LOOKING: find where the payload structs live, find what imports what, pick the acyclic location, record the choice + rationale in your report. The orchestrator records it in the tracking table and leaves 03-05 follow it.
- If the publish sites marshal anonymous structs or map[string]any: promote to a named, exported, json-tagged struct in the OWNING package (internal/agent) and use it at both ends. Promotion in scope. Renaming an existing exported type is NOT in scope - if promotion seems to require a rename, STOP and report.
- Scope discipline (binding): if either payload turns out to be caller-defined/open-ended (map[string]any with divergent shapes per caller), do NOT force a single struct. Leave that topic on the raw path, add a `// RAW TOPIC (open-ended payload):` comment at the declaration site you would have used, and report it. That is a valid leaf outcome.

### What This Leaf Consumes

```go
// From 01-topic-generic.md (committed):
bus.NewTopic[T](name string) Topic[T]
bus.PublishT[T](b *MessageBus, t Topic[T], source string, payload T) int
bus.SubscribeT[T](b *MessageBus, id string, t Topic[T], fn func(T)) *Subscriber
```

## Tasks

### Task 1: Locate payload types + decide declaration placement

**Objective:** Pin the exact payload structs for both topics and the acyclic home for their Topic declarations.

**Files:**
- Create or modify: `internal/bus/topics.go` OR `pkg/models/topics.go` OR owning package - per your findings

**Step 1: Read current content (docs-only-style exploration, use terminal cat / search_files)**

- `terminal("grep -n 'turn.terminal' internal/ -r --include='*.go'" | grep -v _test)` - enumerate ALL publish and subscribe sites
- Same for `agent.quota_wait`
- For each publish site, read ~30 lines around it: what type is `msg` built from? That is your payload type. If `NewBusMessage(..., someStructLiteral{...})` with an inline literal - that is a promotion candidate.
- For each subscribe site: what struct does it unmarshal into? Publisher's type and subscriber's type MUST unify - if they differ but are field-compatible, unify on the publisher's (promote if needed) and migrate the subscriber to decode the shared type. If they differ semantically (different fields actually used), STOP on that topic and report - it is open-ended, leave raw.

**Step 2: Confirm old state**

Record in your report (before changing anything): every call site (file:line), the payload type each uses today, the subscriber decode type, and your placement decision with the import-graph reasoning (run `go list -deps` or inspect imports to confirm the cycle assumption).

**Step 3: Write/rewrite content**

Create the topics file with the two declarations using the real payload types.

**Step 4: Verify**

Run: `go build ./internal/... ./pkg/...`
Expected: compiles (declarations unused so far - if staticcheck/unused-var gates complain about package-level vars, that is fine for now, the gate runs in CI not in go build; leaf 02's Task 3 wires the consumers).

### Task 2: Migrate turn.terminal

**Objective:** Publisher + all exact-topic subscribers of turn.terminal use the typed API.

**Files:**
- Modify: `internal/agent/handler.go` (publish site ~1584)
- Modify: every exact-topic subscriber file found in Task 1
- Test: a focused test proving typed delivery over this topic

**Step 1: Write failing test**

In the package of the subscriber (or a small integration test in internal/bus if the subscriber is the comm relay), write a test that publishes via `bus.PublishT(b, bus.TopicTurnTerminal, ...)` (import path per Task 1 decision) and asserts the production subscriber receives the decoded typed payload. If the subscriber is wired deep (TUI model), test at the nearest seam: assert the callback/decoding layer with a typed publish into a real MessageBus.

**Step 2: Run test to verify failure**

Run: `go test -p 2 -short ./internal/agent/ -run TestTurnTerminalTyped -v` (adjust package)
Expected: FAIL (site still raw - test asserts the new path)

**Step 3: Write implementation**

- Publisher: replace `NewBusMessage` + `b.Publish("turn.terminal", msg)` with `bus.PublishT(h.bus, <pkg>.TopicTurnTerminal, h.sourceName, typedPayload)`. Preserve the exact source string and MessageType semantics - if the raw site used a MessageType other than event, extend nothing; PublishT fixes MessageType=event internally. If the original message used a non-event MessageType, STOP and report - that is a contract deviation needing the orchestrator.
- Subscribers: replace channel-read + json.Unmarshal with `bus.SubscribeT(b, id, <pkg>.TopicTurnTerminal, func(p Type) {...})`, moving the old handler body into fn. Preserve Unsubscribe/lifetime handling exactly.

**Step 4: Run test to verify pass**

Expected: PASS. Then `go build ./... && go test -p 2 -short ./internal/agent/ ./internal/comm/... ./internal/tui/...` - all green.

### Task 3: Migrate agent.quota_wait + full verification

**Objective:** Same migration for quota_wait; prove no regression in the AGENTS.md-classified event path.

**Files:**
- Modify: the quota_wait publish site(s) (loop.go / quota_resume.go)
- Modify: quota_wait subscriber(s)
- Test: same package test file as Task 2

**Step 1: Write failing test**

Mirror Task 2: typed publish -> production subscriber decodes -> assert fields (quota state fields the TUI/WS relay actually reads).

**Step 2: Run test to verify failure**

Expected: FAIL.

**Step 3: Write implementation**

Same pattern as Task 2. Preserve behavior: quota_wait must still reach the WS relay classified as agent_progress (that classification is leaf 03's work - here only the bus delivery must be unchanged; the WS test in internal/comm/http, if one exists, must stay green unchanged).

**Step 4: Run test to verify pass**

Full verification:
- `go build ./...`
- `go test -p 2 -short ./internal/...` (full internal tree, short mode)
- `grep -rn '"turn.terminal"' internal/ --include='*.go' | grep -v _test | grep -v topics` - every remaining hit must be in the topics declaration file or a comment; same for `"agent.quota_wait"`. If any functional call site remains raw, migrate it or justify it in the report (e.g. it is the WS relay's prefix match, which reads topic strings by design and is leaf 03's scope).

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] Both topics declared as typed Topic vars in the chosen acyclic location
- [ ] Publisher and ALL exact-topic subscribers migrated; zero remaining functional raw call sites for these two topics
- [ ] Placement decision + import-cycle reasoning + call-site inventory in the report
- [ ] No `map[string]any` wrapped in a Topic; any open-ended finding left raw with a RAW TOPIC comment
- [ ] `go build ./...` and `go test -p 2 -short ./internal/...` green
- [ ] gofmt clean on all touched files
- [ ] No debug artifacts, no TODOs

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Exactly two topics migrated - no others (grep confirms)
- [ ] All call sites inventoried in the report match what was changed
- [ ] Payload types unified (or topic left raw with justification per scope rule)
- [ ] Subscriber lifetime handling (Unsubscribe, goroutine) preserved
- [ ] WS relay tests and TUI tests pass unchanged
- [ ] Contracts: declaration names exactly `TopicTurnTerminal`, `TopicAgentQuotaWait`; topic name strings unchanged
- [ ] No line-number corruption; gofmt clean

Output: APPROVED or specific gaps with file + line references.

## Notes

- The WS relay (internal/comm/http/server.go) matches topics by string prefix - it is a subscriber-adjacent consumer but its prefix table is leaf 03's scope. Do not touch server.go in this leaf.
- `chat.request`/`chat.response` are request/response patterns - explicitly out of scope, do not migrate even if you notice them.
- If metrics collector's wildcard subscriptions surface in your grep, they are out of scope (wildcards stay raw).
